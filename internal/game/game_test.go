package game

import (
	"errors"
	"fmt"
	"io/fs"
	"regexp"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"
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

var (
	validName  = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*$`)
	validAppID = regexp.MustCompile(`^[1-9][0-9]*$`)
	platforms  = []string{"linux", "windows", "darwin"} // the release targets
)

// relPath checks an install-relative path: "/"-separated, not absolute, no
// "..", and nothing Windows reads as a drive or stream (":").
func relPath(what, p string) error {
	if !fs.ValidPath(p) || p == "." || strings.ContainsAny(p, `\:`) {
		return fmt.Errorf("%s %q must be a relative path inside the install, using /", what, p)
	}
	return nil
}

// within reports whether p is dir or inside it.
func within(p, dir string) bool { return p == dir || strings.HasPrefix(p, dir+"/") }

// validate checks a game definition for mistakes that would otherwise only
// show when someone runs that game.
func validate(g Game) error {
	if !validName.MatchString(g.Name) || g.Title == "" {
		return fmt.Errorf("bad name %q or empty title", g.Name)
	}
	if !validAppID.MatchString(g.SteamAppID) {
		return fmt.Errorf("steam app ID %q is not a positive number", g.SteamAppID)
	}
	if len(g.Launch) == 0 {
		return errors.New("no platform")
	}
	keys := g.Placeholders()
	for _, goos := range g.Platforms() {
		l := g.Launch[goos]
		if !slices.Contains(platforms, goos) {
			return fmt.Errorf("unknown platform %q", goos)
		}
		if (l.ConfigDir == "") == (l.ConfigFile == "") {
			return fmt.Errorf("%s needs a config dir or a config file, not both", goos)
		}
		config, what := l.ConfigDir, "config dir"
		if l.ConfigFile != "" {
			config, what = l.ConfigFile, "config file"
		}
		for _, p := range [][2]string{{"command", l.Command}, {what, config}, {"saves dir", l.SavesDir}} {
			if err := relPath(goos+" "+p[0], p[1]); err != nil {
				return err
			}
		}
		// Both are replaced by links to user data, so they must not overlap.
		if within(config, l.SavesDir) || within(l.SavesDir, config) {
			return fmt.Errorf("%s %s %q and saves dir %q overlap", goos, what, config, l.SavesDir)
		}
		if g.DefaultConfig != "" && (within(g.DefaultConfig, config) || within(g.DefaultConfig, l.SavesDir)) {
			return fmt.Errorf("default config %q is inside a path that gets linked to user data", g.DefaultConfig)
		}
		if l.Wine && g.StopSignal == 0 {
			return fmt.Errorf("%s: a Wine launch needs a stop signal: SIGTERM kills Wine without letting the server save", goos)
		}
		if l.Wine && goos == "windows" {
			return errors.New("windows runs Windows servers natively, not through Wine")
		}
		if g.DefaultConfig != "" && l.ConfigFile != "" {
			return fmt.Errorf("%s: a default config can only seed a config dir; a config file is created by gsm or the server", goos)
		}
		if g.ConfigPortKey != "" && l.ConfigFile == "" {
			return fmt.Errorf("%s has a config port key but no config file", goos)
		}
		// Otherwise every instance would get the game's built-in port.
		if !slices.Contains(keys, "port") && g.ConfigPortKey == "" {
			return fmt.Errorf("%s: the port reaches the server neither through {port} nor a config port key", goos)
		}
	}
	if g.DefaultConfig != "" {
		if err := relPath("default config", g.DefaultConfig); err != nil {
			return err
		}
		if n := g.DefaultConfigName; !fs.ValidPath(n) || n == "." || strings.ContainsAny(n, `/\:`) {
			return fmt.Errorf("default config name %q must be a file name", n)
		}
	}
	if g.DefaultPort < 1 || g.DefaultPort > 65535 {
		return fmt.Errorf("default port %d out of range", g.DefaultPort)
	}
	if g.StopTimeout <= 0 {
		return errors.New("stop timeout must be positive")
	}
	for _, a := range g.Args {
		// A brace left after removing valid placeholders is one RenderArgs would pass through.
		if strings.ContainsAny(placeholder.ReplaceAllString(a, ""), "{}") {
			return fmt.Errorf("arg %q has a malformed placeholder", a)
		}
	}
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
	launch := func(goos string, l Launch) func(*Game) {
		return func(g *Game) { g.Launch = map[string]Launch{goos: l} }
	}
	// Each case breaks Palworld one way and names the error it must cause.
	for _, c := range []struct {
		want    string
		breakIt func(*Game)
	}{
		{"must be a relative path", launch("linux", Launch{Command: "../x.sh", ConfigDir: "c", SavesDir: "s"})},
		{"must be a relative path", launch("linux", Launch{Command: "/bin/x", ConfigDir: "c", SavesDir: "s"})},
		{"must be a relative path", launch("windows", Launch{Command: `a\x.exe`, ConfigDir: "c", SavesDir: "s"})},
		{"must be a relative path", launch("windows", Launch{Command: "C:/x.exe", ConfigDir: "c", SavesDir: "s"})},
		{"must be a relative path", launch("linux", Launch{Command: "x", ConfigDir: "c"})},
		{"overlap", launch("linux", Launch{Command: "x", ConfigDir: "Saved", SavesDir: "Saved/SaveGames"})},
		{"overlap", launch("linux", Launch{Command: "x", ConfigDir: "d", SavesDir: "d"})},
		{"unknown platform", launch("plan9", Launch{Command: "x", ConfigDir: "c", SavesDir: "s"})},
		{"no platform", func(g *Game) { g.Launch = nil }},
		{"has no default", func(g *Game) { g.Args = append(slices.Clone(g.Args), "-x={typo}") }},
		{"malformed", func(g *Game) { g.Args = append(slices.Clone(g.Args), "-x={Players}") }},
		{"not a placeholder", func(g *Game) { g.Defaults = map[string]string{"players": "4", "playres": "4"} }},
		{"not a placeholder", func(g *Game) { g.Defaults = map[string]string{"players": "4", "port": "1"} }},
		{"out of range", func(g *Game) { g.DefaultPort = 70000 }},
		{"stop timeout", func(g *Game) { g.StopTimeout = 0 }},
		{"must be a relative path", func(g *Game) { g.DefaultConfig = "../x.ini" }},
		{"linked to user data", func(g *Game) { g.DefaultConfig = "Pal/Saved/Config/LinuxServer/x.ini" }},
		{"not both", launch("linux", Launch{Command: "x", ConfigDir: "c", ConfigFile: "c.json", SavesDir: "s"})},
		{"not both", launch("linux", Launch{Command: "x", SavesDir: "s"})},
		{"must be a relative path", launch("linux", Launch{Command: "x", ConfigFile: "../c.json", SavesDir: "s"})},
		{"overlap", launch("linux", Launch{Command: "x", ConfigFile: "s/c.json", SavesDir: "s"})},
		{"no config file", func(g *Game) { g.ConfigPortKey = "queryPort" }},
		{"natively", func(g *Game) {
			g.StopSignal = syscall.SIGINT
			g.Launch = map[string]Launch{"windows": {Command: "x.exe", ConfigDir: "c", SavesDir: "s", Wine: true}}
		}},
		{"needs a stop signal", launch("linux", Launch{Command: "x.exe", ConfigDir: "c", SavesDir: "s", Wine: true})},
		{"only seed a config dir", launch("linux", Launch{Command: "x", ConfigFile: "c.json", SavesDir: "s"})},
		{"neither through {port}", func(g *Game) { g.Args = []string{"-players={players}"} }},
		{"must be a file name", func(g *Game) { g.DefaultConfigName = "a/b.ini" }},
		{"must be a file name", func(g *Game) { g.DefaultConfigName = ".." }},
		{"positive number", func(g *Game) { g.SteamAppID = "-5" }},
		{"bad name", func(g *Game) { g.Name = "Pal World" }},
	} {
		g := Palworld
		c.breakIt(&g)
		if err := validate(g); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("want error containing %q, got %v", c.want, err)
		}
	}
}

// A game configured by one JSON file, with the port in it and no args.
func TestValidateAcceptsConfigFileGame(t *testing.T) {
	g := Game{
		Name: "filegame", Title: "File Game", SteamAppID: "1",
		Launch:        map[string]Launch{"linux": {Command: "srv", ConfigFile: "srv.json", SavesDir: "savegame"}},
		ConfigPortKey: "queryPort", DefaultPort: 15637, StopTimeout: time.Second,
	}
	if err := validate(g); err != nil {
		t.Fatal(err)
	}
}

func TestEnshrouded(t *testing.T) {
	g, err := Get("enshrouded")
	if err != nil {
		t.Fatal(err)
	}
	if g.Supports("darwin") || !g.Launch["linux"].Wine || g.Launch["windows"].Wine {
		t.Fatalf("launch: %+v", g.Launch)
	}
	l, w := g.Launch["linux"], g.Launch["windows"]
	if l.Command != "enshrouded_server.exe" || l.ConfigFile != "enshrouded_server.json" || l.SavesDir != "savegame" ||
		w.Command != l.Command || w.ConfigFile != l.ConfigFile || w.SavesDir != l.SavesDir || g.ConfigPortKey != "queryPort" {
		t.Fatalf("paths: %+v, port key %q", g.Launch, g.ConfigPortKey)
	}
	if g.InstallPlatform("linux") != "windows" || g.InstallPlatform("windows") != "" {
		t.Fatal("install platform")
	}
	if g.Signal() != syscall.SIGINT {
		t.Fatalf("stop signal %v", g.Signal())
	}
	if args, err := g.RenderArgs(map[string]string{"port": "15637"}); err != nil || len(args) != 0 {
		t.Fatalf("args %v, %v", args, err)
	}
}
