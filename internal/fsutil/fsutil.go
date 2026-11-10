// Package fsutil has the filesystem operations that must not lose data:
// linking user data into a server install, atomic writes and safe copies.
package fsutil

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

// LinkTo makes path a directory link to target (symlink on Unix, junction on
// Windows). A real directory already at path is moved into target, unless
// both have files, in which case nothing is touched. Ported from palworld.sh.
func LinkTo(path, target string) error {
	if err := os.MkdirAll(target, 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	fi, err := os.Lstat(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
	case err != nil:
		return err
	// Symlinks on Unix; Go reports Windows junctions as ModeIrregular.
	case fi.Mode()&(fs.ModeSymlink|fs.ModeIrregular) != 0:
		if dest, err := os.Readlink(path); err == nil && sameDir(dest, target) {
			return nil
		}
		if err := os.Remove(path); err != nil { // removes the link only, never its target
			return err
		}
	case fi.IsDir():
		entries, err := os.ReadDir(target)
		if err != nil {
			return err
		}
		if len(entries) > 0 {
			return fmt.Errorf("both %s and %s have files; merge them by hand first", path, target)
		}
		if err := os.Remove(target); err != nil {
			return err
		}
		// shortcut: Rename fails across filesystems; copy then delete if server/ and data ever live on different disks.
		if err := os.Rename(path, target); err != nil {
			return err
		}
	default:
		return fmt.Errorf("%s is a file, expected a directory", path)
	}
	return makeDirLink(target, path)
}

func sameDir(a, b string) bool {
	if filepath.Clean(a) == filepath.Clean(b) {
		return true
	}
	ai, err1 := os.Stat(a)
	bi, err2 := os.Stat(b)
	return err1 == nil && err2 == nil && os.SameFile(ai, bi)
}

// WriteAtomic writes data to path through a temp file and a rename, so a
// crash leaves either the old file or the new one, never half of one.
func WriteAtomic(path string, data []byte, perm fs.FileMode) error {
	f, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp) // no-op after a successful rename
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp, perm); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// CopyFile copies src to dst. Copying a file onto itself (directly or through
// a symlinked directory) is a no-op: opening dst for writing would truncate src.
func CopyFile(src, dst string) error {
	si, err := os.Stat(src)
	if err != nil {
		return err
	}
	if di, err := os.Stat(dst); err == nil && os.SameFile(si, di) {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, si.Mode().Perm())
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// IsEmptyDir reports whether dir has no entries. A missing dir counts as empty.
func IsEmptyDir(dir string) (bool, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return true, nil
	}
	return len(entries) == 0, err
}
