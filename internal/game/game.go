// Package game holds the built-in game definitions. They are compile-time Go
// values: adding a game means adding a value here and rebuilding.
package game

import (
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"
)

// Launch is how to start the server on one OS. Paths are relative to the
// install directory and use "/".
type Launch struct {
	Command   string
	ConfigDir string // where the server reads its config
	// ConfigFile is a single config file the server reads, for servers that
	// keep their config in one file instead of a folder. A launch has
	// ConfigDir or ConfigFile, not both.
	ConfigFile string
	SavesDir   string // where the server writes its saves
}

type Game struct {
	Name        string
	Title       string
	SteamAppID  string
	Launch      map[string]Launch // keyed by GOOS
	Args        []string          // each element may contain {placeholders}
	DefaultPort int
	Defaults    map[string]string // default values for Args placeholders
	StopTimeout time.Duration
	// ConfigPortKey is the top-level JSON key in a Launch.ConfigFile that
	// holds the port; gsm sets it to the instance's port before each start.
	ConfigPortKey string
	// DefaultConfig is a file in the install copied into config/ on create,
	// so users have something to edit. Empty means none.
	DefaultConfig     string
	DefaultConfigName string
}

// Supports reports whether the game ships a server for goos.
func (g Game) Supports(goos string) bool {
	_, ok := g.Launch[goos]
	return ok
}

func (g Game) Platforms() []string {
	ps := make([]string, 0, len(g.Launch))
	for p := range g.Launch {
		ps = append(ps, p)
	}
	sort.Strings(ps)
	return ps
}

var placeholder = regexp.MustCompile(`\{([a-z_]+)\}`)

// RenderArgs fills {placeholders} in g.Args from vals. An unknown
// placeholder is an error, so a typo never reaches the server silently.
func (g Game) RenderArgs(vals map[string]string) ([]string, error) {
	out := make([]string, 0, len(g.Args))
	for _, a := range g.Args {
		var missing []string
		r := placeholder.ReplaceAllStringFunc(a, func(m string) string {
			k := m[1 : len(m)-1]
			v, ok := vals[k]
			if !ok {
				missing = append(missing, k)
			}
			return v
		})
		if len(missing) > 0 {
			return nil, fmt.Errorf("no value for {%s} in %q", strings.Join(missing, "}, {"), a)
		}
		out = append(out, r)
	}
	return out, nil
}

// Placeholders lists the args template's placeholder names, port included.
func (g Game) Placeholders() []string {
	var ks []string
	for _, a := range g.Args {
		for _, m := range placeholder.FindAllStringSubmatch(a, -1) {
			if !slices.Contains(ks, m[1]) {
				ks = append(ks, m[1])
			}
		}
	}
	return ks
}

// SetKeys lists the names usable with create --set: every placeholder except
// port, which has its own flag.
func (g Game) SetKeys() []string {
	var ks []string
	for _, k := range g.Placeholders() {
		if k != "port" {
			ks = append(ks, k)
		}
	}
	return ks
}

var Palworld = Game{
	Name:       "palworld",
	Title:      "Palworld",
	SteamAppID: "2394010",
	Launch: map[string]Launch{
		"linux":   {Command: "PalServer.sh", ConfigDir: "Pal/Saved/Config/LinuxServer", SavesDir: "Pal/Saved/SaveGames"},
		"windows": {Command: "PalServer.exe", ConfigDir: "Pal/Saved/Config/WindowsServer", SavesDir: "Pal/Saved/SaveGames"},
	},
	Args:              []string{"-port={port}", "-players={players}", "-useperfthreads", "-NoAsyncLoadingThread", "-UseMultithreadForDS"},
	DefaultPort:       8211,
	Defaults:          map[string]string{"players": "16"},
	StopTimeout:       30 * time.Second,
	DefaultConfig:     "DefaultPalWorldSettings.ini",
	DefaultConfigName: "PalWorldSettings.ini",
}

var all = []Game{Palworld}

func All() []Game { return all }

func Get(name string) (Game, error) {
	for _, g := range all {
		if g.Name == name {
			return g, nil
		}
	}
	return Game{}, fmt.Errorf("unknown game %q (see: gsm game list)", name)
}
