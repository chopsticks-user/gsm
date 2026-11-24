// Package store is the shared, content-addressed snapshot store for saves.
//
//	store/objects/<2-hex>/<sha256>                zstd-compressed file contents
//	store/snapshots/<instance>/<timestamp>-<id>.json
//	store/lock
//	store/format                                  layout version, see Format
//
// Objects are keyed by the hash of the raw bytes, so identical files are
// stored once across all snapshots and instances. Every write goes through a
// temp file and a rename, and a snapshot's record is written last: a snapshot
// exists only once its record does.
package store

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/klauspost/compress/zstd"

	"github.com/chopsticks-user/gsm/internal/fsutil"
	"github.com/chopsticks-user/gsm/internal/proc"
)

type File struct {
	Path string      `json:"path"` // relative, always "/"
	Size int64       `json:"size"`
	Mode fs.FileMode `json:"mode"`
	Hash string      `json:"hash"`
}

// Format is the store layout version (store/format) and RecordFormat the
// snapshot record version that this gsm writes and the newest it reads. A
// store or record without one was written by v0.1.0 and is version 1.
const (
	Format       = 1
	RecordFormat = 1
)

type Record struct {
	Format      int       `json:"format"`
	ID          string    `json:"id"`
	Instance    string    `json:"instance"`
	Game        string    `json:"game"`
	Created     time.Time `json:"created"`
	Message     string    `json:"message,omitempty"`
	Trigger     string    `json:"trigger"` // manual, stop, update, restore
	ConfigRev   string    `json:"config_rev,omitempty"`
	ServerBuild string    `json:"server_build,omitempty"`
	Tags        []string  `json:"tags,omitempty"`
	Pinned      bool      `json:"pinned"`
	Files       []File    `json:"files"`
}

func (r Record) Size() (n int64) {
	for _, f := range r.Files {
		n += f.Size
	}
	return n
}

type Store struct {
	Dir string
	// beforeHash, if set, runs right before a file is hashed; tests use it to
	// rewrite the file at the worst moment.
	beforeHash func(path string)
}

func (s Store) objectPath(hash string) string {
	return filepath.Join(s.Dir, "objects", hash[:2], hash)
}

func (s Store) recordDir(instance string) string {
	return filepath.Join(s.Dir, "snapshots", instance)
}

// checkFormat refuses a store laid out by a newer gsm. With write set, it
// records this gsm's layout version in a store that has none yet.
func (s Store) checkFormat(write bool) error {
	path := filepath.Join(s.Dir, "format")
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		if !write {
			return nil
		}
		return fsutil.WriteAtomic(path, []byte(strconv.Itoa(Format)+"\n"), 0o644)
	}
	if err != nil {
		return err
	}
	v, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	return fsutil.CheckFormat(path, v, Format)
}

// Lock takes the store's write lock, waiting up to wait for another gsm
// process to finish. The lock is published complete (written to a temp file,
// then hard-linked into place, which fails if it exists), so a live holder's
// lock is never empty or partial. A lock whose owner no longer runs, or that
// can't be parsed (a crash or an older gsm left it), is taken over.
func (s Store) Lock(wait time.Duration) (unlock func(), err error) {
	if err := os.MkdirAll(s.Dir, 0o755); err != nil {
		return nil, err
	}
	// The lock's own format is part of the store layout, so check that first.
	if err := s.checkFormat(false); err != nil {
		return nil, err
	}
	path := filepath.Join(s.Dir, "lock")
	t, err := proc.StartTime(os.Getpid())
	if err != nil {
		return nil, err
	}
	me, _ := json.Marshal(proc.State{PID: os.Getpid(), Start: t})
	tmp, err := os.CreateTemp(s.Dir, "lock.tmp-*")
	if err != nil {
		return nil, err
	}
	defer os.Remove(tmp.Name())
	_, werr := tmp.Write(me)
	if cerr := tmp.Close(); werr == nil {
		werr = cerr
	}
	if werr != nil {
		return nil, werr
	}
	deadline := time.Now().Add(wait)
	for {
		err := os.Link(tmp.Name(), path)
		if err == nil {
			unlock := func() { os.Remove(path) }
			// Recorded under the lock, so it can't race a newer gsm's migration.
			if err := s.checkFormat(true); err != nil {
				unlock()
				return nil, err
			}
			return unlock, nil
		}
		if !errors.Is(err, fs.ErrExist) {
			return nil, fmt.Errorf("create snapshot store lock: %w", err)
		}
		var owner proc.State
		b, rerr := os.ReadFile(path)
		if errors.Is(rerr, fs.ErrNotExist) {
			continue // released between Link and ReadFile
		}
		if rerr != nil {
			return nil, fmt.Errorf("read snapshot store lock: %w", rerr)
		}
		if json.Unmarshal(b, &owner) != nil || !proc.Alive(owner) {
			// stale: its owner crashed, or a crash or older gsm left it unreadable
			if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
				return nil, fmt.Errorf("remove stale snapshot store lock: %w", err)
			}
			continue
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("snapshot store is locked by another gsm process (pid %d)", owner.PID)
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// Snapshot stores every file under dir and writes a record built from meta.
// The caller must hold the lock.
func (s Store) Snapshot(dir string, meta Record) (Record, error) {
	rec := meta
	rec.Format = RecordFormat
	rec.Created = time.Now().UTC()
	rec.ID = newID()
	rec.Files = []File{}
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !d.Type().IsRegular() {
			return fmt.Errorf("%s is not a regular file; refusing to snapshot it silently", path)
		}
		// Size and mode come from the read that was hashed, not from the walk:
		// a running server may rewrite or replace the file in between.
		hash, size, mode, err := s.putObject(path)
		if errors.Is(err, fs.ErrNotExist) {
			// Removed since the walk listed it (a temp file renamed away).
			return fmt.Errorf("%s changed while it was being saved; try again", path)
		}
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, path)
		rec.Files = append(rec.Files, File{Path: filepath.ToSlash(rel), Size: size, Mode: mode, Hash: hash})
		return nil
	})
	if err != nil {
		return Record{}, err
	}
	b, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return Record{}, err
	}
	if err := os.MkdirAll(s.recordDir(rec.Instance), 0o755); err != nil {
		return Record{}, err
	}
	name := rec.Created.Format("20060102T150405Z") + "-" + rec.ID + ".json"
	return rec, fsutil.WriteAtomic(filepath.Join(s.recordDir(rec.Instance), name), b, 0o644)
}

func newID() string {
	b := make([]byte, 4)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// putObject stores path's contents and returns their hash, size and mode,
// all from one open of the file. Files already in the store are only read
// once, to hash them.
func (s Store) putObject(path string) (string, int64, fs.FileMode, error) {
	if s.beforeHash != nil {
		s.beforeHash(path)
	}
	hash, size, mode, err := hashFile(path)
	if err != nil {
		return "", 0, 0, err
	}
	obj := s.objectPath(hash)
	if _, err := os.Stat(obj); err == nil {
		return hash, size, mode, nil
	}
	if err := os.MkdirAll(filepath.Dir(obj), 0o755); err != nil {
		return "", 0, 0, err
	}
	in, err := os.Open(path)
	if err != nil {
		return "", 0, 0, err
	}
	defer in.Close()
	tmp, err := os.CreateTemp(filepath.Dir(obj), ".tmp-*")
	if err != nil {
		return "", 0, 0, err
	}
	defer os.Remove(tmp.Name()) // no-op after the rename
	enc, err := zstd.NewWriter(tmp)
	if err != nil {
		tmp.Close()
		return "", 0, 0, err
	}
	// Hash again while compressing: if the file changed since the first
	// read, the object would not match its name.
	h := sha256.New()
	if _, err := io.Copy(enc, io.TeeReader(in, h)); err != nil {
		enc.Close()
		tmp.Close()
		return "", 0, 0, err
	}
	if err := enc.Close(); err != nil {
		tmp.Close()
		return "", 0, 0, err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return "", 0, 0, err
	}
	if err := tmp.Close(); err != nil {
		return "", 0, 0, err
	}
	if hex.EncodeToString(h.Sum(nil)) != hash {
		return "", 0, 0, fmt.Errorf("%s changed while it was being saved; try again", path)
	}
	return hash, size, mode, os.Rename(tmp.Name(), obj)
}

// hashFile returns the SHA-256 of path, the number of bytes hashed and the
// file's permission bits, all from one open file, so they describe the same
// version of it even if it is replaced meanwhile.
func hashFile(path string) (string, int64, fs.FileMode, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", 0, 0, err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return "", 0, 0, err
	}
	h := sha256.New()
	n, err := io.Copy(h, f)
	if err != nil {
		return "", 0, 0, err
	}
	return hex.EncodeToString(h.Sum(nil)), n, fi.Mode().Perm(), nil
}

// List returns an instance's snapshots, oldest first.
func (s Store) List(instance string) ([]Record, error) {
	if err := s.checkFormat(false); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(s.recordDir(instance))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []Record
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".json") || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(s.recordDir(instance), e.Name()))
		if err != nil {
			return nil, err
		}
		var r Record
		if err := json.Unmarshal(b, &r); err != nil {
			return nil, fmt.Errorf("%s: %w", e.Name(), err)
		}
		if err := fsutil.CheckFormat(filepath.Join(s.recordDir(instance), e.Name()), r.Format, RecordFormat); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	sort.Slice(out, func(a, b int) bool { return out[a].Created.Before(out[b].Created) })
	return out, nil
}

// Get finds a snapshot by ID or unique ID prefix. A blank ID is an error,
// not a prefix of every ID.
func (s Store) Get(instance, id string) (Record, error) {
	if strings.TrimSpace(id) == "" {
		return Record{}, fmt.Errorf("a snapshot ID is required (see: gsm save list %s)", instance)
	}
	all, err := s.List(instance)
	if err != nil {
		return Record{}, err
	}
	var found []Record
	for _, r := range all {
		if strings.HasPrefix(r.ID, id) {
			found = append(found, r)
		}
	}
	switch len(found) {
	case 0:
		return Record{}, fmt.Errorf("no snapshot %q for %s (see: gsm save list %s)", id, instance, instance)
	case 1:
		return found[0], nil
	}
	return Record{}, fmt.Errorf("snapshot ID %q is ambiguous", id)
}

// Restore replaces savesDir with the contents of rec, without risking the
// current saves: it first snapshots them with safety as metadata, writes rec
// into savesDir.restore, verifies every hash, and only then swaps directories.
// The caller must hold the lock and make sure the server is stopped.
func (s Store) Restore(rec Record, savesDir string, safety Record) (Record, error) {
	if err := finishSwap(savesDir); err != nil {
		return Record{}, err
	}
	safe, err := s.Snapshot(savesDir, safety)
	if err != nil {
		return Record{}, fmt.Errorf("safety snapshot: %w", err)
	}
	staging := savesDir + ".restore"
	if err := os.RemoveAll(staging); err != nil {
		return safe, err
	}
	for _, f := range rec.Files {
		if err := s.extract(f, filepath.Join(staging, filepath.FromSlash(f.Path))); err != nil {
			os.RemoveAll(staging)
			return safe, err
		}
	}
	if err := os.MkdirAll(staging, 0o755); err != nil { // a snapshot of empty saves has no files
		return safe, err
	}
	old := savesDir + ".old"
	if err := os.Rename(savesDir, old); err != nil {
		return safe, err
	}
	if err := os.Rename(staging, savesDir); err != nil {
		os.Rename(old, savesDir)
		return safe, err
	}
	return safe, os.RemoveAll(old)
}

// finishSwap repairs a restore interrupted between its two renames.
func finishSwap(savesDir string) error {
	old := savesDir + ".old"
	if _, err := os.Stat(old); err != nil {
		return nil
	}
	if _, err := os.Stat(savesDir); errors.Is(err, fs.ErrNotExist) {
		return os.Rename(old, savesDir)
	}
	return os.RemoveAll(old)
}

// extract writes one file from the store to dst and checks its hash.
func (s Store) extract(f File, dst string) error {
	in, err := os.Open(s.objectPath(f.Hash))
	if err != nil {
		return fmt.Errorf("%s: object missing: %w", f.Path, err)
	}
	defer in.Close()
	dec, err := zstd.NewReader(in)
	if err != nil {
		return err
	}
	defer dec.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode|0o200)
	if err != nil {
		return err
	}
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(out, h), dec)
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return fmt.Errorf("%s: %w", f.Path, err)
	}
	if n != f.Size || hex.EncodeToString(h.Sum(nil)) != f.Hash {
		return fmt.Errorf("%s: stored copy is corrupt (hash mismatch)", f.Path)
	}
	return nil
}
