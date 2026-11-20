package fsutil

import (
	"os"
	"path/filepath"
	"testing"
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
