package main

import (
	"testing"

	"github.com/chopsticks-user/gsm/internal/game"
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
