package main

import (
	"errors"
	"flag"
	"fmt"
	iofs "io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"text/tabwriter"

	"github.com/chopsticks-user/gsm/internal/addon"
	"github.com/chopsticks-user/gsm/internal/fsutil"
	"github.com/chopsticks-user/gsm/internal/game"
	"github.com/chopsticks-user/gsm/internal/gitrepo"
	"github.com/chopsticks-user/gsm/internal/instance"
	"github.com/chopsticks-user/gsm/internal/steam"
)

func cmdGame(e env, args []string) error {
	if len(args) == 1 && args[0] == "list" {
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "GAME\tTITLE\tPLATFORMS")
		for _, g := range game.All() {
			fmt.Fprintf(w, "%s\t%s\t%s\n", g.Name, g.Title, strings.Join(g.Platforms(), ", "))
		}
		return w.Flush()
	}
	if len(args) == 2 && args[0] == "show" {
		g, err := game.Get(args[1])
		if err != nil {
			return err
		}
		fmt.Printf("name:          %s\ntitle:         %s\nsteam app:     %s\nplatforms:     %s\ndefault port:  %d\n",
			g.Name, g.Title, g.SteamAppID, strings.Join(g.Platforms(), ", "), g.DefaultPort)
		fmt.Printf("args:          %s\nsettings:      %s\nstop timeout:  %s\n",
			strings.Join(g.Args, " "), settingsLine(g), g.StopTimeout)
		return nil
	}
	return errors.New("usage: gsm game list | show <game>")
}

func cmdList(e env, args []string) error {
	all, err := instance.List(e.home)
	if err != nil {
		return err
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tGAME\tPORT\tADDONS")
	for _, i := range all {
		fmt.Fprintf(w, "%s\t%s\t%d\t%s\n", i.Name, i.Game, i.Port, strings.Join(i.Addons, ","))
	}
	return w.Flush()
}

func cmdCreate(e env, args []string) error {
	fs := flag.NewFlagSet("create", flag.ContinueOnError)
	port := fs.Int("port", 0, "game port (default: the game's, or the next free one)")
	var sets multiFlag
	fs.Var(&sets, "set", "setting k=v (repeatable)")
	pos, err := parse(fs, args)
	if err != nil {
		return err
	}
	if err := wantArgs(pos, 2, "create <game> <name> [--port N] [--set k=v]"); err != nil {
		return err
	}
	g, err := game.Get(pos[0])
	if err != nil {
		return err
	}
	if !g.Supports(runtime.GOOS) {
		return fmt.Errorf("%s ships no dedicated server for %s (supported: %s)",
			g.Title, runtime.GOOS, strings.Join(g.Platforms(), ", "))
	}
	name := pos[1]
	if err := instance.ValidName(name); err != nil {
		return err
	}

	settings := map[string]string{}
	keys := g.SetKeys()
	for _, s := range sets {
		k, v, ok := strings.Cut(s, "=")
		if !ok || !slices.Contains(keys, k) {
			return fmt.Errorf("bad --set %q (%s --set keys: %s; use --port for the port)",
				s, g.Name, strings.Join(keys, ", "))
		}
		settings[k] = v
	}

	// Mkdir (not MkdirAll) on the instance dir fails if the name is taken,
	// including by another game, which keeps names unique across games.
	dir := filepath.Join(instance.Root(e.home), name)
	if err := os.MkdirAll(instance.Root(e.home), 0o755); err != nil {
		return err
	}
	if err := os.Mkdir(dir, 0o755); err != nil {
		if errors.Is(err, iofs.ErrExist) {
			return fmt.Errorf("instance %q already exists", name)
		}
		return err
	}
	existing, err := instance.List(e.home)
	if err != nil {
		os.Remove(dir)
		return err
	}
	var used []int
	for _, i := range existing {
		used = append(used, i.Port)
	}
	want := *port
	if want == 0 {
		want = g.DefaultPort
	}
	p := instance.NextPort(want, used)
	if p != want {
		fmt.Printf("port %d is used by another instance, using %d\n", want, p)
	}

	inst := instance.Instance{
		Name: name,
		Dir:  dir,
		Config: instance.Config{
			Game: g.Name, Port: p, Settings: settings,
			Retention: instance.Retention{Keep: 50},
		},
	}
	if len(inst.Settings) == 0 {
		inst.Settings = nil
	}
	if err := setUpInstance(inst, g); err != nil {
		// Nothing in the new directory belongs to the user yet.
		os.RemoveAll(inst.Dir)
		return err
	}
	fmt.Printf("created %s (%s, port %d)\nstart it with: gsm start %s\n", name, g.Title, p, name)
	return nil
}

func setUpInstance(inst instance.Instance, g game.Game) error {
	for _, d := range []string{inst.ConfigDir(), inst.SavesDir(), inst.ServerDir(), filepath.Dir(inst.LogFile())} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return err
		}
	}
	fmt.Printf("installing %s with steamcmd (this can take a while)...\n", g.Title)
	if err := steam.Install(inst.ServerDir(), g.SteamAppID, os.Stdout); err != nil {
		return err
	}
	if g.DefaultConfig != "" {
		if empty, err := fsutil.IsEmptyDir(inst.ConfigDir()); err != nil {
			return err
		} else if empty {
			src := filepath.Join(inst.ServerDir(), g.DefaultConfig)
			if _, err := os.Stat(src); err == nil {
				if err := fsutil.CopyFile(src, filepath.Join(inst.ConfigDir(), g.DefaultConfigName)); err != nil {
					return err
				}
			}
		}
	}
	if err := linkData(inst, g); err != nil {
		return err
	}
	if err := inst.Save(); err != nil {
		return err
	}
	if err := inst.WriteGitignore(); err != nil {
		return err
	}
	repo, err := gitrepo.Init(inst.Dir)
	if err != nil {
		return err
	}
	_, err = repo.CommitAll("create " + inst.Name)
	return err
}

// linkData links config/ and saves/ into the paths the server expects, so
// the install stays disposable.
func linkData(inst instance.Instance, g game.Game) error {
	l := g.Launch[runtime.GOOS]
	if err := fsutil.LinkTo(filepath.Join(inst.ServerDir(), filepath.FromSlash(l.ConfigDir)), inst.ConfigDir()); err != nil {
		return err
	}
	return fsutil.LinkTo(filepath.Join(inst.ServerDir(), filepath.FromSlash(l.SavesDir)), inst.SavesDir())
}

func cmdDoctor(e env, args []string) error {
	var problems []string
	ok := func(format string, a ...any) { fmt.Printf("ok       "+format+"\n", a...) }
	bad := func(format string, a ...any) {
		msg := fmt.Sprintf(format, a...)
		fmt.Println("MISSING  " + msg)
		problems = append(problems, msg)
	}
	tool := func(name, why string) {
		if p, err := exec.LookPath(name); err == nil {
			ok("%s (%s)", name, p)
		} else {
			bad("%s: %s", name, why)
		}
	}
	fmt.Printf("home     %s\n", e.home)
	tool("git", "needed for config history")
	tool(steam.Command, "needed to install and update servers")
	if steam.IsNixOS() {
		tool("steam-run", "needed to run Steam servers on NixOS")
		userHome, _ := os.UserHomeDir()
		if p, err := steam.FindSteamclient(userHome); err == nil {
			ok("steamclient.so (%s)", p)
		} else {
			bad("%v", err)
		}
	}
	all, err := instance.List(e.home)
	if err != nil {
		return err
	}
	needed := map[string]bool{}
	for _, i := range all {
		for _, a := range i.Addons {
			needed[a] = true
		}
	}
	for a := range needed {
		if p := addon.Problems(a); len(p) > 0 {
			for _, msg := range p {
				bad("%s", msg)
			}
		} else {
			ok("addon %s", a)
		}
	}
	if len(problems) > 0 {
		return fmt.Errorf("%d problem(s) found", len(problems))
	}
	return nil
}

// settingsLine describes what an instance of g can set at create.
func settingsLine(g game.Game) string {
	keys := g.SetKeys()
	if len(keys) == 0 {
		return "none; port with --port"
	}
	return strings.Join(keys, ", ") + " (set with --set k=v); port with --port"
}
