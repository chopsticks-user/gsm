// Package instance manages named server instances on disk.
package instance

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"

	"github.com/BurntSushi/toml"

	"github.com/chopsticks-user/gsm/internal/fsutil"
	"github.com/chopsticks-user/gsm/internal/game"
)

// Config is instance.toml.
type Config struct {
	Game      string            `toml:"game"`
	Port      int               `toml:"port"`
	Settings  map[string]string `toml:"settings,omitempty"`
	Addons    []string          `toml:"addons,omitempty"`
	Retention Retention         `toml:"retention"`
}

// Retention is recorded now and enforced once prune exists.
type Retention struct {
	Keep int `toml:"keep"`
}

type Instance struct {
	Name string
	Dir  string
	Config
}

func (i Instance) ConfigDir() string { return filepath.Join(i.Dir, "config") }
func (i Instance) SavesDir() string  { return filepath.Join(i.Dir, "saves") }
func (i Instance) ServerDir() string { return filepath.Join(i.Dir, "server") }
func (i Instance) LogFile() string   { return filepath.Join(i.Dir, "logs", "server.log") }
func (i Instance) StateFile() string { return filepath.Join(i.Dir, "state.json") }
func (i Instance) TOMLPath() string  { return filepath.Join(i.Dir, "instance.toml") }

// gitignore makes the instance directory a git repo that tracks only
// config/ and instance.toml. Saves, the install, logs and state stay out.
const gitignore = "/*\n!/.gitignore\n!/instance.toml\n!/config/\n"

func Root(home string) string { return filepath.Join(home, "instances") }

var validName = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,62}$`)

// ValidName checks that name is safe as a directory name on every OS.
func ValidName(name string) error {
	if !validName.MatchString(name) {
		return fmt.Errorf("invalid instance name %q: use lowercase letters, digits, - and _ (max 63)", name)
	}
	return nil
}

func Load(home, name string) (Instance, error) {
	if err := ValidName(name); err != nil {
		return Instance{}, err
	}
	i := Instance{Name: name, Dir: filepath.Join(Root(home), name)}
	if _, err := toml.DecodeFile(i.TOMLPath(), &i.Config); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return Instance{}, fmt.Errorf("no instance named %q (see: gsm list)", name)
		}
		return Instance{}, fmt.Errorf("read %s: %w", i.TOMLPath(), err)
	}
	return i, nil
}

func List(home string) ([]Instance, error) {
	entries, err := os.ReadDir(Root(home))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []Instance
	for _, e := range entries {
		if !e.IsDir() || ValidName(e.Name()) != nil {
			continue
		}
		i, err := Load(home, e.Name())
		if err != nil {
			continue // a half-created instance has no instance.toml yet
		}
		out = append(out, i)
	}
	sort.Slice(out, func(a, b int) bool { return out[a].Name < out[b].Name })
	return out, nil
}

func (i Instance) Save() error {
	var buf bytes.Buffer
	if err := toml.NewEncoder(&buf).Encode(i.Config); err != nil {
		return err
	}
	return fsutil.WriteAtomic(i.TOMLPath(), buf.Bytes(), 0o644)
}

// WriteGitignore writes the .gitignore that limits the repo to config.
func (i Instance) WriteGitignore() error {
	return fsutil.WriteAtomic(filepath.Join(i.Dir, ".gitignore"), []byte(gitignore), 0o644)
}

// NextPort returns want, or the next port not in used.
func NextPort(want int, used []int) int {
	for slices.Contains(used, want) {
		want++
	}
	return want
}

// ArgValues returns the values for the game's args template: game defaults,
// overridden by the instance's settings, plus the port.
func (i Instance) ArgValues(g game.Game) map[string]string {
	v := map[string]string{}
	for k, d := range g.Defaults {
		v[k] = d
	}
	for k, s := range i.Settings {
		v[k] = s
	}
	v["port"] = strconv.Itoa(i.Port)
	return v
}

func (i Instance) HasAddon(name string) bool { return slices.Contains(i.Addons, name) }
