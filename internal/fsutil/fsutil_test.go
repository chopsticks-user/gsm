package fsutil

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"testing/iotest"
)

func write(t *testing.T, path, data string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestLinkToFreshPath(t *testing.T) {
	d := t.TempDir()
	path, target := filepath.Join(d, "server", "Saves"), filepath.Join(d, "saves")
	write(t, filepath.Join(target, "a.sav"), "world")
	if err := LinkTo(path, target); err != nil {
		t.Fatal(err)
	}
	if read(t, filepath.Join(path, "a.sav")) != "world" {
		t.Fatal("link does not reach target")
	}
	// Linking again is a no-op.
	if err := LinkTo(path, target); err != nil {
		t.Fatal(err)
	}
}

func TestLinkToMovesRealDirIntoEmptyTarget(t *testing.T) {
	d := t.TempDir()
	path, target := filepath.Join(d, "server", "Saves"), filepath.Join(d, "saves")
	write(t, filepath.Join(path, "a.sav"), "world")
	if err := LinkTo(path, target); err != nil {
		t.Fatal(err)
	}
	if read(t, filepath.Join(target, "a.sav")) != "world" {
		t.Fatal("data not moved into target")
	}
	if fi, _ := os.Lstat(path); fi.Mode()&os.ModeSymlink == 0 && fi.Mode()&os.ModeIrregular == 0 {
		t.Fatal("path is not a link")
	}
}

func TestLinkToAbortsWhenBothHaveFiles(t *testing.T) {
	d := t.TempDir()
	path, target := filepath.Join(d, "server", "Saves"), filepath.Join(d, "saves")
	write(t, filepath.Join(path, "a.sav"), "server copy")
	write(t, filepath.Join(target, "b.sav"), "user copy")
	if err := LinkTo(path, target); err == nil {
		t.Fatal("want error")
	}
	if read(t, filepath.Join(path, "a.sav")) != "server copy" || read(t, filepath.Join(target, "b.sav")) != "user copy" {
		t.Fatal("data was touched")
	}
}

func TestLinkToReplacesWrongLink(t *testing.T) {
	d := t.TempDir()
	path, target, other := filepath.Join(d, "server", "Saves"), filepath.Join(d, "saves"), filepath.Join(d, "other")
	write(t, filepath.Join(other, "keep"), "x")
	if err := LinkTo(path, other); err != nil {
		t.Fatal(err)
	}
	if err := LinkTo(path, target); err != nil {
		t.Fatal(err)
	}
	if read(t, filepath.Join(other, "keep")) != "x" {
		t.Fatal("old link target was damaged")
	}
	if _, err := os.Stat(filepath.Join(path, "keep")); err == nil {
		t.Fatal("link still points at the old target")
	}
}

func TestCopyFileOntoItselfThroughSymlinkedDir(t *testing.T) {
	d := t.TempDir()
	src := filepath.Join(d, "linux64", "steamclient.so")
	write(t, src, "library")
	if err := os.Symlink(filepath.Join(d, "linux64"), filepath.Join(d, "sdk64")); err != nil {
		t.Skip("symlinks unavailable:", err)
	}
	if err := CopyFile(src, filepath.Join(d, "sdk64", "steamclient.so")); err != nil {
		t.Fatal(err)
	}
	if read(t, src) != "library" {
		t.Fatal("source was truncated")
	}
}

func TestWriteAtomic(t *testing.T) {
	p := filepath.Join(t.TempDir(), "state.json")
	if err := WriteAtomic(p, []byte("one"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := WriteAtomic(p, []byte("two"), 0o644); err != nil {
		t.Fatal(err)
	}
	if read(t, p) != "two" {
		t.Fatal("wrong contents")
	}
	entries, _ := os.ReadDir(filepath.Dir(p))
	if len(entries) != 1 {
		t.Fatalf("temp files left behind: %v", entries)
	}
}

// A copy that fails partway (here: the source is a directory, so reading it
// fails after the destination was opened) must leave the old destination
// intact and no temp file behind.
func TestCopyFileFailureKeepsOldDestination(t *testing.T) {
	d := t.TempDir()
	dst := filepath.Join(d, "PalWorldSettings.ini")
	write(t, dst, "old settings")
	src := filepath.Join(d, "src")
	if err := os.Mkdir(src, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := CopyFile(src, dst); err == nil {
		t.Fatal("copying a directory succeeded")
	}
	if got := read(t, dst); got != "old settings" {
		t.Fatalf("destination = %q after a failed copy, want the old content", got)
	}
	entries, _ := os.ReadDir(d)
	if len(entries) != 2 {
		t.Fatalf("leftover files after a failed copy: %v", entries)
	}
}

// A write that fails after some bytes went out keeps the old file whole.
func TestWriteAtomicPartialFailureKeepsOldFile(t *testing.T) {
	d := t.TempDir()
	p := filepath.Join(d, "PalWorldSettings.ini")
	write(t, p, "old settings")
	r := io.MultiReader(strings.NewReader("half of the new"), iotest.ErrReader(errors.New("disk read error")))
	if err := writeAtomic(p, r, 0o644); err == nil {
		t.Fatal("write with a failing reader succeeded")
	}
	if got := read(t, p); got != "old settings" {
		t.Fatalf("file = %q after a failed write, want the old content", got)
	}
	if entries, _ := os.ReadDir(d); len(entries) != 1 {
		t.Fatalf("leftover files after a failed write: %v", entries)
	}
}

// Copying over an existing read-only destination (an earlier steamclient.so)
// replaces it with the new content and mode, leaving no temp file.
func TestCopyFileReplacesReadOnlyDestination(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows can't rename over a read-only file and has no Unix modes")
	}
	d := t.TempDir()
	src := filepath.Join(d, "linux64", "steamclient.so")
	write(t, src, "new library")
	if err := os.Chmod(src, 0o755); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(d, "sdk64", "steamclient.so")
	write(t, dst, "old library")
	if err := os.Chmod(dst, 0o444); err != nil {
		t.Fatal(err)
	}
	if err := CopyFile(src, dst); err != nil {
		t.Fatal(err)
	}
	if got := read(t, dst); got != "new library" {
		t.Fatalf("destination = %q, want the new content", got)
	}
	if fi, err := os.Stat(dst); err != nil || fi.Mode().Perm() != 0o755 {
		t.Fatalf("destination mode = %v (err %v), want 0755", fi.Mode().Perm(), err)
	}
	if entries, _ := os.ReadDir(filepath.Dir(dst)); len(entries) != 1 {
		t.Fatalf("leftover files after a copy: %v", entries)
	}
}

// A link that already points at target is left exactly as it is.
func TestLinkToKeepsCorrectLink(t *testing.T) {
	d := t.TempDir()
	path, target := filepath.Join(d, "server", "Saves"), filepath.Join(d, "saves")
	write(t, filepath.Join(target, "a.sav"), "world")
	if err := LinkTo(path, target); err != nil {
		t.Fatal(err)
	}
	before, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := LinkTo(path, target); err != nil {
		t.Fatal(err)
	}
	after, err := os.Lstat(path)
	if err != nil || !os.SameFile(before, after) {
		t.Fatalf("the link was replaced (err %v)", err)
	}
	if read(t, filepath.Join(target, "a.sav")) != "world" {
		t.Fatal("target changed")
	}
}

// A regular file where the link belongs is an error, and stays untouched.
func TestLinkToRefusesFile(t *testing.T) {
	d := t.TempDir()
	path, target := filepath.Join(d, "server", "Saves"), filepath.Join(d, "saves")
	write(t, path, "not a folder")
	err := LinkTo(path, target)
	if err == nil || !strings.Contains(err.Error(), "is a file, expected a directory") {
		t.Fatalf("err = %v", err)
	}
	if read(t, path) != "not a folder" {
		t.Fatal("the file was changed")
	}
}
