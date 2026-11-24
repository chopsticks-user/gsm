package instance

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/chopsticks-user/gsm/internal/fsutil"
	"github.com/chopsticks-user/gsm/internal/game"
	"github.com/chopsticks-user/gsm/internal/gitrepo"
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
	write := func(name, content string) {
		t.Helper()
		p := filepath.Join(Root(home), name, "instance.toml")
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// v0.1.0 wrote no format: it reads as version 1.
	write("old", "game = \"palworld\"\nport = 8211\n")
	if _, err := Load(home, "old"); err != nil {
		t.Fatal(err)
	}
	// A newer gsm's file is refused by Load; List returns the rest and names it.
	write("new", "format = 2\ngame = \"palworld\"\n")
	if _, err := Load(home, "new"); !errors.Is(err, fsutil.ErrTooNew) {
		t.Fatalf("Load = %v, want ErrTooNew", err)
	}
	all, err := List(home)
	if !errors.Is(err, fsutil.ErrTooNew) || len(all) != 1 || all[0].Name != "old" {
		t.Fatalf("List = %v, %v", all, err)
	}
}

// The instance's git repo tracks config only: saves, the install, logs,
// the Wine prefix, state and restore leftovers must never reach git.
func TestGitTracksOnlyConfig(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	// Keep the developer's git config (signing, hooks) out of the commit.
	global := filepath.Join(t.TempDir(), "gitconfig")
	if err := os.WriteFile(global, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", global)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	in := Instance{Name: "main", Dir: t.TempDir(), Config: Config{Game: "palworld", Port: 8211}}
	for _, f := range []string{
		"config/PalWorldSettings.ini", "config/sub/extra.ini",
		"saves/0/W/Level.sav", "server/PalServer.sh", "logs/server.log", "wine/drive_c/x",
		"state.json", "saves.restore/Level.sav", "saves.old/Level.sav",
	} {
		p := filepath.Join(in.Dir, filepath.FromSlash(f))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := in.Save(); err != nil {
		t.Fatal(err)
	}
	if err := in.WriteGitignore(); err != nil {
		t.Fatal(err)
	}
	repo, err := gitrepo.Init(in.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.CommitAll("create main"); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command("git", "-C", in.Dir, "ls-files").Output()
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Fields(string(out))
	want := []string{".gitignore", "config/PalWorldSettings.ini", "config/sub/extra.ini", "instance.toml"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("tracked files = %q, want %q", got, want)
	}
}
