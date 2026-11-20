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
	fi, err := os.Lstat(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
	case err != nil:
		return err
	case fi.Mode()&fs.ModeSymlink != 0:
		if dest, err := os.Readlink(path); err == nil && sameDir(dest, target) {
			return nil
		}
		if err := os.Remove(path); err != nil { // removes the link only
			return err
		}
	case fi.Mode().IsRegular():
		if _, err := os.Lstat(target); err == nil {
			return fmt.Errorf("both %s and %s exist; merge them by hand first", path, target)
		} else if !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		if err := os.Rename(path, target); err != nil {
			return err
		}
	default:
		return fmt.Errorf("%s is not a file", path)
	}
	if err := os.Symlink(target, path); err != nil {
		if runtime.GOOS == "windows" {
			// No hard-link fallback: gsm's atomic writes and git checkouts replace
			// the file, which would silently split a hard link into two copies.
			return fmt.Errorf("%w (a file symlink on Windows needs Developer Mode or admin rights)", err)
		}
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

// setTopLevel returns b with the value of the top-level key replaced by
// val, or val inserted as the first member if the key is missing.
func setTopLevel(b []byte, key string, val []byte) ([]byte, error) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(b, &obj); err != nil {
		return nil, fmt.Errorf("not a valid JSON object: %w", err)
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.Token() // "{", checked by Unmarshal above
	open := int(dec.InputOffset())
	for dec.More() {
		t, err := dec.Token()
		if err != nil {
			return nil, err
		}
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			return nil, err
		}
		if t == key {
			end := int(dec.InputOffset())
			start := end - len(raw)
			return append(append(append([]byte{}, b[:start]...), val...), b[end:]...), nil
		}
	}
	k, _ := json.Marshal(key)
	member := fmt.Sprintf("\n  %s: %s", k, val)
	if len(obj) > 0 {
		member += ","
	}
	return append(append(append([]byte{}, b[:open]...), member...), b[open:]...), nil
}
