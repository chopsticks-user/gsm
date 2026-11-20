package instance

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/chopsticks-user/gsm/internal/fsutil"
	"github.com/chopsticks-user/gsm/internal/game"
)

func TestValidName(t *testing.T) {
	for _, ok := range []string{"main", "test-2", "a_b", "0"} {
		if ValidName(ok) != nil {
			t.Errorf("%q rejected", ok)
		}
	}
	for _, bad := range []string{"", "Main", "../x", "a/b", "-x", "con:", "a b"} {
		if ValidName(bad) == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestNextPort(t *testing.T) {
	if got := NextPort(8211, []int{8211, 8212, 9000}); got != 8213 {
		t.Fatalf("got %d", got)
	}
	if got := NextPort(8211, nil); got != 8211 {
		t.Fatalf("got %d", got)
	}
}

func TestSaveLoadList(t *testing.T) {
	home := t.TempDir()
	in := Instance{Name: "main", Dir: filepath.Join(Root(home), "main"), Config: Config{
		Format: Format, Game: "palworld", Port: 8211, Settings: map[string]string{"players": "8"},
		Addons: []string{"playit"}, Retention: Retention{Keep: 20},
	}}
	if err := os.MkdirAll(in.Dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := in.Save(); err != nil {
		t.Fatal(err)
	}
	got, err := Load(home, "main")
	if err != nil || !reflect.DeepEqual(got, in) {
		t.Fatalf("got %+v, %v", got, err)
	}
	// A directory without instance.toml (half-created) is skipped.
	os.MkdirAll(filepath.Join(Root(home), "broken"), 0o755)
	all, err := List(home)
	if err != nil || len(all) != 1 || all[0].Name != "main" {
		t.Fatalf("List = %v, %v", all, err)
	}
	if _, err := Load(home, "nope"); err == nil {
		t.Fatal("want error")
	}
}

func TestArgValues(t *testing.T) {
	i := Instance{Config: Config{Port: 8311, Settings: map[string]string{"players": "4"}}}
	got := i.ArgValues(game.Palworld)
	if got["port"] != "8311" || got["players"] != "4" {
		t.Fatalf("got %v", got)
	}
	args, err := game.Palworld.RenderArgs(got)
	if err != nil || args[0] != "-port=8311" || args[1] != "-players=4" {
		t.Fatalf("args %v, %v", args, err)
	}
}

func TestFormatVersion(t *testing.T) {
	home := t.TempDir()
	toml := filepath.Join(Root(home), "main", "instance.toml")
	os.MkdirAll(filepath.Dir(toml), 0o755)
	// v0.1.0 wrote no format: it reads as version 1.
	os.WriteFile(toml, []byte("game = \"palworld\"\nport = 8211\n"), 0o644)
	if _, err := Load(home, "main"); err != nil {
		t.Fatal(err)
	}
	// A newer gsm's file is refused by Load and by List.
	os.WriteFile(toml, []byte("format = 2\ngame = \"palworld\"\n"), 0o644)
	if _, err := Load(home, "main"); !errors.Is(err, fsutil.ErrTooNew) {
		t.Fatalf("Load = %v, want ErrTooNew", err)
	}
	if _, err := List(home); !errors.Is(err, fsutil.ErrTooNew) {
		t.Fatalf("List = %v, want ErrTooNew", err)
	}
}
