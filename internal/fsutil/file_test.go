package fsutil

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLinkFileTo(t *testing.T) {
	setup := func(t *testing.T) (path, target string) {
		d := t.TempDir()
		return filepath.Join(d, "server", "game.json"), filepath.Join(d, "config", "game.json")
	}
	linked := func(t *testing.T, path, target string) {
		t.Helper()
		if dest, err := os.Readlink(path); err != nil || dest != target {
			t.Fatalf("%s -> %q, %v; want %s", path, dest, err, target)
		}
	}
	t.Run("fresh, target missing", func(t *testing.T) {
		path, target := setup(t)
		if err := LinkFileTo(path, target); err != nil {
			t.Fatal(err)
		}
		linked(t, path, target)
		// The server creates the file through the dangling link.
		if err := os.WriteFile(path, []byte("x"), 0o644); err != nil || read(t, target) != "x" {
			t.Fatalf("write through link: %v", err)
		}
	})
	t.Run("real file moves into empty target", func(t *testing.T) {
		path, target := setup(t)
		write(t, path, "mine")
		if err := LinkFileTo(path, target); err != nil {
			t.Fatal(err)
		}
		linked(t, path, target)
		if read(t, target) != "mine" {
			t.Fatal("content lost")
		}
	})
	t.Run("both exist", func(t *testing.T) {
		path, target := setup(t)
		write(t, path, "a")
		write(t, target, "b")
		if err := LinkFileTo(path, target); err == nil || !strings.Contains(err.Error(), "merge them by hand") {
			t.Fatalf("err = %v", err)
		}
		if read(t, path) != "a" || read(t, target) != "b" {
			t.Fatal("touched a file")
		}
	})
	t.Run("wrong link replaced, right link kept", func(t *testing.T) {
		path, target := setup(t)
		other := filepath.Join(filepath.Dir(target), "other.json")
		write(t, other, "o")
		os.MkdirAll(filepath.Dir(path), 0o755)
		if err := os.Symlink(other, path); err != nil {
			t.Fatal(err)
		}
		for range 2 {
			if err := LinkFileTo(path, target); err != nil {
				t.Fatal(err)
			}
			linked(t, path, target)
		}
		if read(t, other) != "o" {
			t.Fatal("old link target touched")
		}
	})
	t.Run("directory at path", func(t *testing.T) {
		path, target := setup(t)
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := LinkFileTo(path, target); err == nil {
			t.Fatal("want error")
		}
	})
}

func TestSetJSONNumber(t *testing.T) {
	path := filepath.Join(t.TempDir(), "game.json")
	// Missing: created with just the key.
	if err := SetJSONNumber(path, "queryPort", 15637); err != nil {
		t.Fatal(err)
	}
	if got := read(t, path); got != "{\n  \"queryPort\": 15637\n}\n" {
		t.Fatalf("created %q", got)
	}
	// Existing, formatted: only the value's bytes change, wherever the key is.
	for _, c := range []struct{ in, want string }{
		{"{\n\t\"name\": \"x\",\n\t\"queryPort\":   15637 ,\n\t\"nested\": {\"queryPort\": 1}\n}",
			"{\n\t\"name\": \"x\",\n\t\"queryPort\":   15700 ,\n\t\"nested\": {\"queryPort\": 1}\n}"},
		{`{"nested":{"queryPort":1},"list":[1,{"a":"}"}],"queryPort":"15637"}`,
			`{"nested":{"queryPort":1},"list":[1,{"a":"}"}],"queryPort":15700}`},
		{"{\"queryPort\":15637}", "{\"queryPort\":15700}"},
		// Missing key: inserted first.
		{"{\n  \"name\": \"x\"\n}\n", "{\n  \"queryPort\": 15700,\n  \"name\": \"x\"\n}\n"},
		{"{}", "{\n  \"queryPort\": 15700}"},
	} {
		write(t, path, c.in)
		if err := SetJSONNumber(path, "queryPort", 15700); err != nil {
			t.Fatal(err)
		}
		if got := read(t, path); got != c.want {
			t.Errorf("in %q:\n got %q\nwant %q", c.in, got, c.want)
		}
	}
	// Invalid: error naming the file, file untouched.
	for _, bad := range []string{`{"queryPort": 1,}`, `[1]`, ``} {
		write(t, path, bad)
		if err := SetJSONNumber(path, "queryPort", 15700); err == nil || !strings.Contains(err.Error(), path) {
			t.Errorf("%q: err = %v", bad, err)
		}
		if read(t, path) != bad {
			t.Errorf("%q: file changed", bad)
		}
	}
}
