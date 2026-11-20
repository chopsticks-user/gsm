package game

import (
	"errors"
	"fmt"
	"io/fs"
	"regexp"
	"slices"
	"strconv"
	"strings"
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

// validate checks a game definition for mistakes that would otherwise only
// show when someone runs that game.
func validate(g Game) error {
	if !regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*$`).MatchString(g.Name) || g.Title == "" {
		return fmt.Errorf("bad name %q or empty title", g.Name)
	}
	if _, err := strconv.Atoi(g.SteamAppID); err != nil {
		return fmt.Errorf("steam app ID %q is not a number", g.SteamAppID)
	}
	if len(g.Launch) == 0 {
		return errors.New("no platform")
	}
	// Install-relative paths: "/"-separated, no "..", not absolute.
	relPath := func(what, p string) error {
		if !fs.ValidPath(p) || p == "." || strings.Contains(p, `\`) {
			return fmt.Errorf("%s %q must be a relative path inside the install, using /", what, p)
		}
		return nil
	}
	for goos, l := range g.Launch {
		if !slices.Contains([]string{"linux", "windows", "darwin"}, goos) {
			return fmt.Errorf("unknown platform %q", goos)
		}
		for what, p := range map[string]string{"command": l.Command, "config dir": l.ConfigDir, "saves dir": l.SavesDir} {
			if err := relPath(goos+" "+what, p); err != nil {
				return err
			}
		}
	}
	if g.DefaultConfig != "" {
		if err := relPath("default config", g.DefaultConfig); err != nil {
			return err
		}
		if g.DefaultConfigName == "" || strings.ContainsAny(g.DefaultConfigName, `/\`) {
			return fmt.Errorf("default config name %q must be a file name", g.DefaultConfigName)
		}
	}
	if g.DefaultPort < 1 || g.DefaultPort > 65535 {
		return fmt.Errorf("default port %d out of range", g.DefaultPort)
	}
	if g.StopTimeout <= 0 {
		return errors.New("stop timeout must be positive")
	}
	keys := g.Placeholders()
	for _, k := range keys {
		if _, ok := g.Defaults[k]; !ok && k != "port" {
			return fmt.Errorf("placeholder {%s} has no default", k)
		}
	}
	for k := range g.Defaults {
		if !slices.Contains(keys, k) || k == "port" {
			return fmt.Errorf("default %q is not a placeholder in args (port comes from the instance)", k)
		}
	}
	for _, a := range g.Args {
		// A brace left after removing valid placeholders is one RenderArgs would pass through.
		if strings.ContainsAny(placeholder.ReplaceAllString(a, ""), "{}") {
			return fmt.Errorf("arg %q has a malformed placeholder", a)
		}
	}
	return nil
}

func TestBuiltinGamesAreValid(t *testing.T) {
	seen := map[string]bool{}
	for _, g := range All() {
		if err := validate(g); err != nil {
			t.Errorf("%s: %v", g.Name, err)
		}
		if seen[g.Name] {
			t.Errorf("%s: defined twice", g.Name)
		}
		seen[g.Name] = true
	}
}

func TestValidateCatchesMistakes(t *testing.T) {
	bad := map[string]func(*Game){
		"escaping path": func(g *Game) {
			g.Launch = map[string]Launch{"linux": {Command: "../x.sh", ConfigDir: "c", SavesDir: "s"}}
		},
		"absolute path": func(g *Game) {
			g.Launch = map[string]Launch{"linux": {Command: "/bin/x", ConfigDir: "c", SavesDir: "s"}}
		},
		"backslash path": func(g *Game) {
			g.Launch = map[string]Launch{"windows": {Command: `a\x.exe`, ConfigDir: "c", SavesDir: "s"}}
		},
		"empty saves dir":     func(g *Game) { g.Launch = map[string]Launch{"linux": {Command: "x", ConfigDir: "c"}} },
		"unknown platform":    func(g *Game) { g.Launch = map[string]Launch{"plan9": {Command: "x", ConfigDir: "c", SavesDir: "s"}} },
		"no platform":         func(g *Game) { g.Launch = nil },
		"unknown placeholder": func(g *Game) { g.Args = append(g.Args, "-x={typo}") },
		"malformed":           func(g *Game) { g.Args = append(g.Args, "-x={Players}") },
		"unused default":      func(g *Game) { g.Defaults = map[string]string{"players": "4", "playres": "4"} },
		"port default":        func(g *Game) { g.Defaults = map[string]string{"players": "4", "port": "1"} },
		"port out of range":   func(g *Game) { g.DefaultPort = 70000 },
		"no stop timeout":     func(g *Game) { g.StopTimeout = 0 },
		"escaping config":     func(g *Game) { g.DefaultConfig = "../x.ini" },
		"config name path":    func(g *Game) { g.DefaultConfigName = "a/b.ini" },
		"bad app ID":          func(g *Game) { g.SteamAppID = "abc" },
		"bad name":            func(g *Game) { g.Name = "Pal World" },
	}
	for name, breakIt := range bad {
		g := Palworld
		g.Args = slices.Clone(g.Args)
		breakIt(&g)
		if validate(g) == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}
