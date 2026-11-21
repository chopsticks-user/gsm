package main

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/chopsticks-user/gsm/internal/game"
	"github.com/chopsticks-user/gsm/internal/instance"
)

// game show must not list port among the --set keys: create rejects
// --set port=... and wants --port.
func TestSettingsLineKeepsPortOutOfSet(t *testing.T) {
	if got, want := settingsLine(game.Palworld), "players (set with --set k=v); port with --port"; got != want {
		t.Fatalf("settingsLine(palworld) = %q, want %q", got, want)
	}
	onlyPort := game.Game{Args: []string{"-port={port}"}}
	if got, want := settingsLine(onlyPort), "none; port with --port"; got != want {
		t.Fatalf("settingsLine(port only) = %q, want %q", got, want)
	}
}

// A game with one JSON config file in the install: linkData links it to
// config/<file> and keeps its port key equal to the instance's port.
func TestLinkDataConfigFile(t *testing.T) {
	g := game.Game{
		Launch:        map[string]game.Launch{runtime.GOOS: {Command: "srv", ConfigFile: "srv.json", SavesDir: "savegame"}},
		ConfigPortKey: "queryPort",
	}
	inst := instance.Instance{Name: "main", Dir: t.TempDir(), Config: instance.Config{Port: 15637}}
	cfg := filepath.Join(inst.ConfigDir(), "srv.json")
	readCfg := func() string {
		t.Helper()
		b, err := os.ReadFile(cfg)
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	if err := linkData(inst, g); err != nil {
		t.Fatal(err)
	}
	if dest, err := os.Readlink(filepath.Join(inst.ServerDir(), "srv.json")); err != nil || dest != cfg {
		t.Fatalf("link -> %q, %v", dest, err)
	}
	if got := readCfg(); got != "{\n  \"queryPort\": 15637\n}\n" {
		t.Fatalf("fresh config %q", got)
	}
	// The server fills in the file; a hand edit changes the port. The next
	// link puts the instance's port back and leaves the rest alone.
	if err := os.WriteFile(cfg, []byte("{\n  \"name\": \"x\",\n  \"queryPort\": 1\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	inst.Port = 15638
	if err := linkData(inst, g); err != nil {
		t.Fatal(err)
	}
	if got := readCfg(); got != "{\n  \"name\": \"x\",\n  \"queryPort\": 15638\n}\n" {
		t.Fatalf("relinked config %q", got)
	}
	// An invalid file fails and stays untouched.
	if err := os.WriteFile(cfg, []byte("{oops"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := linkData(inst, g); err == nil || readCfg() != "{oops" {
		t.Fatalf("invalid config: err %v, content %q", err, readCfg())
	}
}
