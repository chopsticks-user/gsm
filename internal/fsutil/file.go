package fsutil

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
)

// LinkFileTo makes path a symlink to the file target, with LinkTo's rules
// for files: a real file at path moves to target if target doesn't exist,
// and if both exist nothing is touched. target need not exist yet; the
// server may create it through the link.
func LinkFileTo(path, target string) error {
	for _, d := range []string{filepath.Dir(target), filepath.Dir(path)} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return err
		}
	}
	move := false
	fi, err := os.Lstat(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
	case err != nil:
		return err
	case fi.Mode()&fs.ModeSymlink != 0:
		if dest, err := os.Readlink(path); err == nil && samePath(dest, target) {
			return nil
		}
	case fi.Mode().IsRegular():
		if _, err := os.Lstat(target); err == nil {
			return fmt.Errorf("both %s and %s exist; merge them by hand first", path, target)
		} else if !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		move = true
	default:
		return fmt.Errorf("%s is not a file", path)
	}
	// Make the link under a temp name first, so a failure (no symlink rights
	// on Windows) changes nothing. Renaming it into place also replaces a
	// wrong link, and a crash in between is repaired by the next call.
	tmp := path + ".gsm-link"
	os.Remove(tmp) // left by a crash
	if err := os.Symlink(target, tmp); err != nil {
		if runtime.GOOS == "windows" {
			// No hard-link fallback: gsm's atomic writes and git checkouts replace
			// the file, which would silently split a hard link into two copies.
			return fmt.Errorf("%w (a file symlink on Windows needs Developer Mode or admin rights)", err)
		}
		return err
	}
	if move {
		// shortcut: Rename fails across filesystems; copy then delete if server/ and data ever live on different disks.
		if err := os.Rename(path, target); err != nil {
			os.Remove(tmp)
			return err
		}
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

// SetJSONNumber sets the top-level key in the JSON object file at path to n,
// changing only that value's bytes so the rest of the file, formatting
// included, stays as it was. A missing file is created holding just the
// key; an invalid one is an error and left alone.
func SetJSONNumber(path, key string, n int) error {
	num := []byte(strconv.Itoa(n))
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		k, _ := json.Marshal(key)
		return WriteAtomic(path, fmt.Appendf(nil, "{\n  %s: %s\n}\n", k, num), 0o644)
	}
	if err != nil {
		return err
	}
	out, err := setTopLevel(b, key, num)
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	if bytes.Equal(out, b) {
		return nil
	}
	fi, err := os.Stat(path)
	if err != nil {
		return err
	}
	return WriteAtomic(path, out, fi.Mode().Perm())
}

var utf8BOM = []byte("\xef\xbb\xbf")

// setTopLevel returns b with every value of the top-level key replaced by
// val (parsers differ on which duplicate wins), or val inserted as the
// first member if the key is missing. A leading UTF-8 BOM is kept.
func setTopLevel(b []byte, key string, val []byte) ([]byte, error) {
	body := bytes.TrimPrefix(b, utf8BOM)
	skip := len(b) - len(body)
	if err := json.Unmarshal(body, new(any)); err != nil {
		return nil, fmt.Errorf("not valid JSON: %w", err)
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	if t, _ := dec.Token(); t != json.Delim('{') {
		return nil, errors.New("not a JSON object")
	}
	open := skip + int(dec.InputOffset())
	var spans [][2]int
	members := 0
	for dec.More() {
		t, err := dec.Token()
		if err != nil {
			return nil, err
		}
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			return nil, err
		}
		members++
		if t == key {
			end := skip + int(dec.InputOffset())
			spans = append(spans, [2]int{end - len(raw), end})
		}
	}
	if len(spans) == 0 {
		k, _ := json.Marshal(key)
		member := fmt.Sprintf("\n  %s: %s", k, val)
		if members > 0 {
			member += ","
		}
		return slices.Concat(b[:open], []byte(member), b[open:]), nil
	}
	out := b
	for i := len(spans) - 1; i >= 0; i-- { // last first, so earlier offsets hold
		out = slices.Concat(out[:spans[i][0]], val, out[spans[i][1]:])
	}
	return out, nil
}
