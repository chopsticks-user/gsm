package game

import (
	"slices"
	"testing"
)

func TestRenderArgs(t *testing.T) {
	g := Game{Args: []string{"-port={port}", "-fixed", "{a}-{b}"}}
	got, err := g.RenderArgs(map[string]string{"port": "8211", "a": "x", "b": "y"})
	if err != nil || !slices.Equal(got, []string{"-port=8211", "-fixed", "x-y"}) {
		t.Fatalf("got %v, %v", got, err)
	}
	if _, err := g.RenderArgs(map[string]string{"port": "1"}); err == nil {
		t.Fatal("want error for missing placeholder values")
	}
}

func TestPalworld(t *testing.T) {
	g, err := Get("palworld")
	if err != nil {
		t.Fatal(err)
	}
	if g.Supports("darwin") || !g.Supports("linux") || !g.Supports("windows") {
		t.Fatalf("platforms: %v", g.Platforms())
	}
	if !slices.Equal(g.Placeholders(), []string{"port", "players"}) {
		t.Fatalf("placeholders: %v", g.Placeholders())
	}
	if _, err := Get("nope"); err == nil {
		t.Fatal("want error for unknown game")
	}
}

func TestSetKeysLeaveOutPort(t *testing.T) {
	if got := Palworld.SetKeys(); !slices.Equal(got, []string{"players"}) {
		t.Fatalf("SetKeys = %v, want [players]", got)
	}
}
