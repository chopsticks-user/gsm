package store

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chopsticks-user/gsm/internal/fsutil"
	"github.com/chopsticks-user/gsm/internal/proc"
)

func write(t *testing.T, path, data string) {
	t.Helper()
	os.MkdirAll(filepath.Dir(path), 0o755)
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
}

func countObjects(t *testing.T, s Store) int {
	n := 0
	filepath.WalkDir(filepath.Join(s.Dir, "objects"), func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			n++
		}
		return nil
	})
	return n
}

func setup(t *testing.T) (Store, string) {
	d := t.TempDir()
	saves := filepath.Join(d, "saves")
	write(t, filepath.Join(saves, "Level.sav"), "level v1")
	write(t, filepath.Join(saves, "Players", "p1.sav"), "player")
	return Store{Dir: filepath.Join(d, "store")}, saves
}

func TestSnapshotDedupAndRestore(t *testing.T) {
	s, saves := setup(t)
	meta := Record{Instance: "main", Game: "palworld", Trigger: "manual"}
	r1, err := s.Snapshot(saves, meta)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Snapshot(saves, meta); err != nil {
		t.Fatal(err)
	}
	if n := countObjects(t, s); n != 2 {
		t.Fatalf("two identical snapshots stored %d objects, want 2", n)
	}

	write(t, filepath.Join(saves, "Level.sav"), "level v2")
	write(t, filepath.Join(saves, "extra.sav"), "new")
	safe, err := s.Restore(r1, saves, Record{Instance: "main", Trigger: "restore"})
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(saves, "Level.sav")); string(b) != "level v1" {
		t.Fatalf("Level.sav = %q", b)
	}
	if _, err := os.Stat(filepath.Join(saves, "extra.sav")); err == nil {
		t.Fatal("file absent from the snapshot survived the restore")
	}
	if len(safe.Files) != 3 {
		t.Fatalf("safety snapshot has %d files, want 3", len(safe.Files))
	}
	all, _ := s.List("main")
	if len(all) != 3 {
		t.Fatalf("List = %d records, want 3", len(all))
	}
	if got, err := s.Get("main", r1.ID[:4]); err != nil || got.ID != r1.ID {
		t.Fatalf("Get by prefix: %v, %v", got.ID, err)
	}
}

func TestCorruptObjectLeavesLiveSavesUntouched(t *testing.T) {
	s, saves := setup(t)
	r1, err := s.Snapshot(saves, Record{Instance: "main"})
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range r1.Files {
		if f.Path == "Level.sav" {
			if err := os.WriteFile(s.objectPath(f.Hash), []byte("garbage"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	write(t, filepath.Join(saves, "Level.sav"), "live")
	if _, err := s.Restore(r1, saves, Record{Instance: "main"}); err == nil {
		t.Fatal("want error")
	}
	if b, _ := os.ReadFile(filepath.Join(saves, "Level.sav")); string(b) != "live" {
		t.Fatalf("live save changed to %q", b)
	}
	if _, err := os.Stat(saves + ".restore"); err == nil {
		t.Fatal("staging directory left behind")
	}
}

func TestRecordlessSnapshotDoesNotExist(t *testing.T) {
	s, saves := setup(t)
	if _, err := s.Snapshot(saves, Record{Instance: "main"}); err != nil {
		t.Fatal(err)
	}
	// Simulate a crash before the record write: objects exist, the record doesn't.
	recs, _ := filepath.Glob(filepath.Join(s.recordDir("main"), "*.json"))
	for _, r := range recs {
		os.Remove(r)
	}
	if all, err := s.List("main"); err != nil || len(all) != 0 {
		t.Fatalf("List = %v, %v", all, err)
	}
}

func TestInterruptedSwapIsRepaired(t *testing.T) {
	s, saves := setup(t)
	r1, _ := s.Snapshot(saves, Record{Instance: "main"})
	// Crash between the two renames: saves moved to saves.old, new dir not in place.
	if err := os.Rename(saves, saves+".old"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Restore(r1, saves, Record{Instance: "main"}); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(saves, "Level.sav")); string(b) != "level v1" {
		t.Fatalf("Level.sav = %q", b)
	}
}

func TestStaleLockIsTakenOver(t *testing.T) {
	s, _ := setup(t)
	os.MkdirAll(s.Dir, 0o755)
	b, _ := json.Marshal(proc.State{PID: os.Getpid(), Start: "not-my-start-time"})
	write(t, filepath.Join(s.Dir, "lock"), string(b))
	unlock, err := s.Lock(time.Second)
	if err != nil {
		t.Fatal(err)
	}
	// A live holder blocks a second taker.
	if _, err := s.Lock(300 * time.Millisecond); err == nil {
		t.Fatal("second lock succeeded while the first is held")
	}
	unlock()
	unlock2, err := s.Lock(time.Second)
	if err != nil {
		t.Fatal(err)
	}
	unlock2()
}

// gsm publishes its lock complete, so an empty or partial lock can only be
// left by a crash or an older gsm, and is taken over at once.
func TestUnreadableLockIsTakenOver(t *testing.T) {
	for name, content := range map[string]string{"empty": "", "garbage": `{"pid":12`} {
		t.Run(name, func(t *testing.T) {
			s, _ := setup(t)
			if err := os.MkdirAll(s.Dir, 0o755); err != nil {
				t.Fatal(err)
			}
			write(t, filepath.Join(s.Dir, "lock"), content)
			unlock, err := s.Lock(300 * time.Millisecond)
			if err != nil {
				t.Fatal(err)
			}
			unlock()
		})
	}
}

// The lock is never visible empty, and taking and releasing it leaves only
// the store's own folders behind.
func TestLockIsPublishedCompleteAndCleansUp(t *testing.T) {
	s, _ := setup(t)
	unlock, err := s.Lock(time.Second)
	if err != nil {
		t.Fatal(err)
	}
	var owner proc.State
	b, err := os.ReadFile(filepath.Join(s.Dir, "lock"))
	if err != nil || json.Unmarshal(b, &owner) != nil || owner.PID != os.Getpid() {
		t.Fatalf("lock content %q (read error %v) doesn't name this process", b, err)
	}
	unlock()
	entries, err := os.ReadDir(s.Dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "lock") {
			t.Errorf("%s left in the store after unlock", e.Name())
		}
	}
}

// A server can rewrite a save after the walk saw it and before it is hashed.
// The record must still describe one version of the file, and restore must
// accept it, on both the new-object and the existing-object path.
func TestSnapshotOfFileRewrittenBeforeHashIsConsistent(t *testing.T) {
	for _, existing := range []bool{false, true} {
		t.Run(map[bool]string{false: "new object", true: "existing object"}[existing], func(t *testing.T) {
			s, saves := setup(t)
			save := filepath.Join(saves, "Level.sav")
			newer := "a longer, newer save"
			if existing { // store the newer content once, so the second snapshot dedups it
				write(t, save, newer)
				if _, err := s.Snapshot(saves, Record{Instance: "main"}); err != nil {
					t.Fatal(err)
				}
			}
			write(t, save, "old")
			s.beforeHash = func(p string) {
				if p == save {
					write(t, save, newer)
				}
			}
			rec, err := s.Snapshot(saves, Record{Instance: "main"})
			if err != nil {
				t.Fatal(err)
			}
			s.beforeHash = nil
			sum := sha256.Sum256([]byte(newer))
			f := rec.Files[0]
			if f.Hash != hex.EncodeToString(sum[:]) || f.Size != int64(len(newer)) {
				t.Fatalf("record has size %d and hash %s..., want size %d with the hash of the same bytes", f.Size, f.Hash[:8], len(newer))
			}
			// The failure the user saw: restore rejecting the record.
			write(t, save, "something else")
			if _, err := s.Restore(rec, saves, Record{Instance: "main"}); err != nil {
				t.Fatalf("restore of the live snapshot: %v", err)
			}
			if got := read(t, save); got != newer {
				t.Fatalf("restored %q, want %q", got, newer)
			}
		})
	}
}

// A file removed after the walk listed it (a temp file renamed away) fails
// the snapshot with gsm's "try again", not a raw "no such file".
func TestSnapshotOfFileVanishedMidWalkSaysTryAgain(t *testing.T) {
	s, saves := setup(t)
	tmp := filepath.Join(saves, "Level.sav.tmp")
	write(t, tmp, "half written")
	s.beforeHash = func(p string) {
		if p == tmp {
			os.Remove(tmp)
		}
	}
	_, err := s.Snapshot(saves, Record{Instance: "main"})
	if err == nil || !strings.Contains(err.Error(), "try again") {
		t.Fatalf("err = %v, want a try-again error", err)
	}
}

func read(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// A store and records from v0.1.0 carry no format and read as version 1;
// the first lock records the layout version. Newer ones are refused.
func TestFormatVersions(t *testing.T) {
	s, saves := setup(t)
	rec, err := s.Snapshot(saves, Record{Instance: "main", Trigger: "manual"})
	if err != nil {
		t.Fatal(err)
	}
	if rec.Format != RecordFormat {
		t.Fatalf("record format %d", rec.Format)
	}
	write(t, filepath.Join(s.Dir, "snapshots", "main", "old.json"), `{"id":"0ld","instance":"main","files":[]}`)
	if all, err := s.List("main"); err != nil || len(all) != 2 {
		t.Fatalf("List = %v, %v", all, err)
	}
	unlock, err := s.Lock(time.Second)
	if err != nil {
		t.Fatal(err)
	}
	unlock()
	if b, _ := os.ReadFile(filepath.Join(s.Dir, "format")); string(b) != "1\n" {
		t.Fatalf("store/format = %q", b)
	}

	write(t, filepath.Join(s.Dir, "snapshots", "main", "new.json"), `{"format":2,"id":"n3w","instance":"main","files":[]}`)
	if _, err := s.List("main"); !errors.Is(err, fsutil.ErrTooNew) {
		t.Fatalf("List with a newer record = %v", err)
	}
	os.Remove(filepath.Join(s.Dir, "snapshots", "main", "new.json"))

	write(t, filepath.Join(s.Dir, "format"), "2\n")
	if _, err := s.List("main"); !errors.Is(err, fsutil.ErrTooNew) {
		t.Fatalf("List in a newer store = %v", err)
	}
	if _, err := s.Lock(time.Second); !errors.Is(err, fsutil.ErrTooNew) {
		t.Fatalf("Lock in a newer store = %v", err)
	}
}

// Restore takes any unique prefix of a snapshot ID.
func TestGetByPrefix(t *testing.T) {
	s, saves := setup(t)
	r1, err := s.Snapshot(saves, Record{Instance: "main"})
	if err != nil {
		t.Fatal(err)
	}
	r2, err := s.Snapshot(saves, Record{Instance: "main"})
	if err != nil {
		t.Fatal(err)
	}
	n := 1 // the shortest prefix of r1's ID that r2's doesn't share
	for strings.HasPrefix(r2.ID, r1.ID[:n]) {
		n++
	}
	if got, err := s.Get("main", r1.ID[:n]); err != nil || got.ID != r1.ID {
		t.Fatalf("Get(%q) = %v, %v; want %s", r1.ID[:n], got.ID, err, r1.ID)
	}
	// IDs are hex, so of 17 snapshots two must share their first character.
	first := map[byte]bool{r1.ID[0]: true, r2.ID[0]: true}
	shared := ""
	if r1.ID[0] == r2.ID[0] {
		shared = r1.ID[:1]
	}
	for shared == "" {
		r, err := s.Snapshot(saves, Record{Instance: "main"})
		if err != nil {
			t.Fatal(err)
		}
		if first[r.ID[0]] {
			shared = r.ID[:1]
		}
		first[r.ID[0]] = true
	}
	if _, err := s.Get("main", shared); err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("a prefix of two IDs: err = %v", err)
	}
	if _, err := s.Get("main", "zzzz"); err == nil || !strings.Contains(err.Error(), `no snapshot "zzzz"`) || !strings.Contains(err.Error(), "gsm save list main") {
		t.Fatalf("an unknown ID: err = %v", err)
	}
}
