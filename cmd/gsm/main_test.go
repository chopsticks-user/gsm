package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/chopsticks-user/gsm/internal/game"
	"github.com/chopsticks-user/gsm/internal/instance"
)

// Mistyped commands fail with a message that says what to do, before
// anything runs.
func TestRunErrors(t *testing.T) {
	home := t.TempDir()
	in := instance.Instance{Name: "main", Dir: filepath.Join(instance.Root(home), "main"), Config: instance.Config{Game: "palworld", Port: 8211}}
	if err := os.MkdirAll(in.Dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := in.Save(); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		args []string
		want string
	}{
		{nil, "no command given"},
		{[]string{"frobnicate"}, `unknown command "frobnicate" (see: gsm --help)`},
		{[]string{"remove", "main"}, "remove is not implemented yet"},
		{[]string{"import"}, "import is not implemented yet"},
		{[]string{"remote"}, "remote is not implemented yet"},
		{[]string{"recover"}, "recover is not implemented yet"},
		{[]string{"config", "edit", "main"}, "config edit is not implemented yet"},
		{[]string{"config", "diff"}, "usage: gsm config diff|log|commit <name> [-m msg]"},
		{[]string{"save", "prune", "main"}, "save prune is not implemented yet"},
		{[]string{"save", "restore", "main"}, "usage: gsm save restore <name> <id>"},
		{[]string{"restart"}, "usage: gsm restart <name>"},
		{[]string{"run", "a", "b"}, "usage: gsm run <name>"},
		{[]string{"update"}, "usage: gsm update <name>"},
		{[]string{"logs"}, "usage: gsm logs <name> [-f]"},
		{[]string{"start"}, "usage: gsm start <name>... | --all"},
		{[]string{"stop", "main", "--all"}, "give instance names or --all, not both"},
		{[]string{"addon", "add", "main"}, "usage: gsm addon add|remove <name> <addon>"},
		{[]string{"addon", "add", "main", "ngrok"}, `unknown addon "ngrok"`},
		{[]string{"game", "show", "minecraft"}, `unknown game "minecraft"`},
	} {
		args := c.args
		if args != nil {
			args = append([]string{"--home", home}, args...)
		}
		if err := run(args); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("gsm %s: %v, want %q", strings.Join(c.args, " "), err, c.want)
		}
	}
}

// create checks everything it can before it writes anything.
func TestCreateRejectsWithoutWriting(t *testing.T) {
	supported := game.Palworld.Supports(runtime.GOOS)
	for _, c := range []struct {
		args []string
		want string
	}{
		{[]string{"minecraft", "main"}, `unknown game "minecraft"`},
		{[]string{"palworld", "Bad Name"}, "invalid instance name"},
		{[]string{"palworld", "main", "--set", "speed=2"}, `bad --set "speed=2"`},
		{[]string{"palworld", "main", "--set", "players"}, `bad --set "players"`},
		{[]string{"palworld"}, "usage: gsm create"},
	} {
		home := t.TempDir()
		want := c.want
		if !supported && c.args[0] == "palworld" && len(c.args) > 1 {
			want = "ships no dedicated server" // the platform check comes first
		}
		err := run(append([]string{"--home", home, "create"}, c.args...))
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("gsm create %s: %v, want %q", strings.Join(c.args, " "), err, want)
		}
		if entries, _ := os.ReadDir(home); len(entries) != 0 {
			t.Errorf("gsm create %s left %v in the home", strings.Join(c.args, " "), entries)
		}
	}
}

func TestHumanSize(t *testing.T) {
	for n, want := range map[int64]string{
		0:             "0 B",
		1023:          "1023 B",
		1024:          "1.0 KiB",
		1536:          "1.5 KiB",
		1024*1024 - 1: "1024.0 KiB",
		1024 * 1024:   "1.0 MiB",
		3 << 30:       "3.0 GiB",
		1536 * 1024:   "1.5 MiB",
	} {
		if got := humanSize(n); got != want {
			t.Errorf("humanSize(%d) = %q, want %q", n, got, want)
		}
	}
}
