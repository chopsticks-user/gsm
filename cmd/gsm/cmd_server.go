package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"

	"github.com/chopsticks-user/gsm/internal/addon"
	"github.com/chopsticks-user/gsm/internal/game"
	"github.com/chopsticks-user/gsm/internal/gitrepo"
	"github.com/chopsticks-user/gsm/internal/instance"
	"github.com/chopsticks-user/gsm/internal/proc"
	"github.com/chopsticks-user/gsm/internal/steam"
)

func cmdStart(e env, args []string) error {
	fs := flag.NewFlagSet("start", flag.ContinueOnError)
	all := fs.Bool("all", false, "start every instance")
	pos, err := parse(fs, args)
	if err != nil {
		return err
	}
	insts, err := targets(e, pos, *all, "start <name>... | --all")
	if err != nil && len(insts) == 0 {
		return err
	}
	var failed []string
	for _, i := range insts {
		if err := start(i); err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", i.Name, err)
			failed = append(failed, i.Name)
		}
	}
	if len(failed) > 0 {
		err = errors.Join(err, fmt.Errorf("failed to start: %s", strings.Join(failed, ", ")))
	}
	return err
}

// prepare does everything start and run share before launching: commit
// config so the run maps to an exact revision, platform setup and links.
// It returns the command to launch and the config revision.
func prepare(i instance.Instance) (*exec.Cmd, string, error) {
	g, err := game.Get(i.Game)
	if err != nil {
		return nil, "", err
	}
	if !g.Supports(runtime.GOOS) {
		return nil, "", fmt.Errorf("%s has no server for %s", g.Title, runtime.GOOS)
	}
	repo, err := gitrepo.Init(i.Dir)
	if err != nil {
		return nil, "", err
	}
	if ok, err := repo.CommitAll("auto: before start"); err != nil {
		return nil, "", err
	} else if ok {
		fmt.Printf("%s: committed config changes\n", i.Name)
	}
	if steam.IsNixOS() {
		// steam-run gives the server a fresh, empty /tmp.
		if strings.HasPrefix(i.Dir, "/tmp/") {
			return nil, "", fmt.Errorf("on NixOS, steam-run hides /tmp, so the server can't see %s; use a --home outside /tmp", i.Dir)
		}
		if err := steam.SetupNixOS(); err != nil {
			return nil, "", err
		}
	}
	// update with validate may have replaced the links; LinkTo is a no-op when they're fine.
	if err := linkData(i, g); err != nil {
		return nil, "", err
	}
	argv, err := g.RenderArgs(i.ArgValues(g))
	if err != nil {
		return nil, "", err
	}
	launcher := filepath.Join(i.ServerDir(), filepath.FromSlash(g.Launch[runtime.GOOS].Command))
	if _, err := os.Stat(launcher); err != nil {
		return nil, "", fmt.Errorf("server not installed (%v); run: gsm update %s", err, i.Name)
	}
	name := launcher
	if steam.IsNixOS() {
		name, argv = "steam-run", append([]string{launcher}, argv...)
	}
	c := exec.Command(name, argv...)
	c.Dir = i.ServerDir()
	return c, repo.Head(), nil
}

// openLog appends to the instance log with a header for this run.
// shortcut: the log grows forever; rotate it once logs get large.
func openLog(i instance.Instance, rev string) (*os.File, error) {
	if err := os.MkdirAll(filepath.Dir(i.LogFile()), 0o755); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(i.LogFile(), os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o644)
	if err != nil {
		return nil, err
	}
	fmt.Fprintf(f, "\n=== gsm start %s, config %s ===\n", time.Now().Format(time.RFC3339), rev)
	return f, nil
}

func start(i instance.Instance) error {
	st, ok, err := proc.Running(i.StateFile(), i.Name)
	if err != nil {
		return err
	}
	if ok {
		fmt.Printf("%s: already running (pid %d)\n", i.Name, st.PID)
		return nil
	}
	c, rev, err := prepare(i)
	if err != nil {
		return err
	}
	logf, err := openLog(i, rev)
	if err != nil {
		return err
	}
	defer logf.Close()
	c.Stdout, c.Stderr = logf, logf
	st, err = proc.Launch(c, i.StateFile(), i.Name)
	if err != nil {
		return err
	}
	// Reap the launched process if it exits, so a dead one doesn't linger as
	// a zombie that still counts as a group member.
	go c.Wait()
	// Catch servers that die right away (missing library, port in use...).
	time.Sleep(2 * time.Second)
	if !proc.ServerAlive(st) {
		proc.Clear(i.StateFile())
		return fmt.Errorf("server exited right after start; see: gsm logs %s", i.Name)
	}
	fmt.Printf("%s: started (pid %d, port %d, config %s)\n", i.Name, st.PID, i.Port, rev)
	startAddons(i)
	return nil
}

func startAddons(i instance.Instance) {
	for _, a := range i.Addons {
		if err := addon.Start(a, i.Name); err != nil {
			fmt.Fprintf(os.Stderr, "%s: addon %s failed to start, server keeps running: %v\n", i.Name, a, err)
		} else {
			fmt.Printf("%s: addon %s running\n", i.Name, a)
		}
	}
}

func stopAddons(i instance.Instance) {
	for _, a := range i.Addons {
		if err := addon.Stop(a, i.Name); err != nil {
			fmt.Fprintf(os.Stderr, "%s: addon %s: %v\n", i.Name, a, err)
		}
	}
}

func cmdStop(e env, args []string) error {
	fs := flag.NewFlagSet("stop", flag.ContinueOnError)
	all := fs.Bool("all", false, "stop every instance")
	pos, err := parse(fs, args)
	if err != nil {
		return err
	}
	insts, err := targets(e, pos, *all, "stop <name>... | --all")
	if err != nil && len(insts) == 0 {
		return err
	}
	var failed []string
	for _, i := range insts {
		if err := stop(e, i); err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", i.Name, err)
			failed = append(failed, i.Name)
		}
	}
	if len(failed) > 0 {
		err = errors.Join(err, fmt.Errorf("failed to stop: %s", strings.Join(failed, ", ")))
	}
	return err
}

func stop(e env, i instance.Instance) error {
	st, ok, err := proc.Running(i.StateFile(), i.Name)
	if err != nil {
		return err
	}
	stopAddons(i)
	if !ok {
		fmt.Printf("%s: not running\n", i.Name)
		return proc.Clear(i.StateFile())
	}
	g, err := game.Get(i.Game)
	if err != nil {
		return err
	}
	fmt.Printf("%s: stopping (up to %s)...\n", i.Name, g.StopTimeout)
	killed, warning, err := proc.Stop(st, g.StopTimeout)
	if warning != nil {
		fmt.Fprintf(os.Stderr, "%s: warning: %v\n", i.Name, warning)
	}
	if err != nil {
		return err
	}
	if err := proc.Clear(i.StateFile()); err != nil {
		return err
	}
	if killed {
		fmt.Printf("%s: did not exit in time, killed\n", i.Name)
	} else {
		fmt.Printf("%s: stopped\n", i.Name)
	}
	return autoSnapshot(e, i, "stop")
}

func cmdRestart(e env, args []string) error {
	if err := wantArgs(args, 1, "restart <name>"); err != nil {
		return err
	}
	i, err := e.load(args[0])
	if err != nil {
		return err
	}
	if err := stop(e, i); err != nil {
		return err
	}
	return start(i)
}

// cmdRun runs the server in the foreground with output on the terminal.
// Ctrl+C runs the same stop sequence as gsm stop.
func cmdRun(e env, args []string) error {
	if err := wantArgs(args, 1, "run <name>"); err != nil {
		return err
	}
	i, err := e.load(args[0])
	if err != nil {
		return err
	}
	st, ok, err := proc.Running(i.StateFile(), i.Name)
	if err != nil {
		return err
	}
	if ok {
		return fmt.Errorf("already running in the background (pid %d)", st.PID)
	}
	g, err := game.Get(i.Game)
	if err != nil {
		return err
	}
	c, rev, err := prepare(i)
	if err != nil {
		return err
	}
	logf, err := openLog(i, rev)
	if err != nil {
		return err
	}
	defer logf.Close()
	// A non-file writer makes Go copy output through a pipe, which works even
	// though the server runs in its own session / console.
	out := io.MultiWriter(os.Stdout, logf)
	c.Stdout, c.Stderr = out, out

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sig)

	st, err = proc.Launch(c, i.StateFile(), i.Name)
	if err != nil {
		return err
	}
	done := make(chan struct{})
	go func() { c.Wait(); close(done) }()
	startAddons(i)
	fmt.Printf("%s: running in the foreground (pid %d), Ctrl+C to stop\n", i.Name, st.PID)

	select {
	case <-done:
		fmt.Printf("%s: server exited\n", i.Name)
	case <-sig:
		fmt.Printf("\n%s: stopping (up to %s)...\n", i.Name, g.StopTimeout)
		_, warning, err := proc.Stop(st, g.StopTimeout)
		if warning != nil {
			fmt.Fprintf(os.Stderr, "%s: warning: %v\n", i.Name, warning)
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", i.Name, err)
		}
		<-done
	}
	stopAddons(i)
	if err := proc.Clear(i.StateFile()); err != nil {
		return err
	}
	return autoSnapshot(e, i, "stop")
}

type statusRow struct {
	Name    string   `json:"name"`
	Game    string   `json:"game"`
	Port    int      `json:"port"`
	Running bool     `json:"running"`
	PID     int      `json:"pid,omitempty"`
	Addons  []string `json:"addons"`
}

func cmdStatus(e env, args []string) error {
	fs := flag.NewFlagSet("status", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "print JSON")
	if _, err := parse(fs, args); err != nil {
		return err
	}
	all, listErr := instance.List(e.home)
	if listErr != nil && len(all) == 0 {
		return listErr
	}
	errs := []error{listErr}
	rows := []statusRow{}
	for _, i := range all {
		st, ok, err := proc.Running(i.StateFile(), i.Name)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", i.Name, err))
			continue
		}
		r := statusRow{Name: i.Name, Game: i.Game, Port: i.Port, Running: ok, Addons: []string{}}
		if ok {
			r.PID = st.PID
		}
		for _, a := range i.Addons {
			state := "stopped"
			if addon.Running(a, i.Name) {
				state = "running"
			}
			r.Addons = append(r.Addons, a+":"+state)
		}
		rows = append(rows, r)
	}
	if *asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return errors.Join(append(errs, enc.Encode(rows))...)
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tGAME\tPORT\tSTATE\tPID\tADDONS")
	for _, r := range rows {
		state, pid := "stopped", "-"
		if r.Running {
			state, pid = "running", fmt.Sprint(r.PID)
		}
		fmt.Fprintf(w, "%s\t%s\t%d\t%s\t%s\t%s\n", r.Name, r.Game, r.Port, state, pid, strings.Join(r.Addons, ","))
	}
	return errors.Join(append(errs, w.Flush())...)
}

func cmdLogs(e env, args []string) error {
	fs := flag.NewFlagSet("logs", flag.ContinueOnError)
	follow := fs.Bool("f", false, "keep printing new output")
	pos, err := parse(fs, args)
	if err != nil {
		return err
	}
	if err := wantArgs(pos, 1, "logs <name> [-f]"); err != nil {
		return err
	}
	i, err := e.load(pos[0])
	if err != nil {
		return err
	}
	f, err := os.Open(i.LogFile())
	if err != nil {
		return fmt.Errorf("no log yet: %w", err)
	}
	defer f.Close()
	off, err := io.Copy(os.Stdout, f)
	if err != nil || !*follow {
		return err
	}
	// Follow by polling, which works the same on every OS.
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt)
	for {
		select {
		case <-sig:
			return nil
		case <-time.After(500 * time.Millisecond):
		}
		fi, err := f.Stat()
		if err != nil {
			return err
		}
		if fi.Size() < off { // truncated
			off = 0
		}
		if fi.Size() > off {
			if _, err := f.Seek(off, io.SeekStart); err != nil {
				return err
			}
			n, err := io.Copy(os.Stdout, f)
			off += n
			if err != nil {
				return err
			}
		}
	}
}

func cmdUpdate(e env, args []string) error {
	if err := wantArgs(args, 1, "update <name>"); err != nil {
		return err
	}
	i, err := e.load(args[0])
	if err != nil {
		return err
	}
	_, ok, err := proc.Running(i.StateFile(), i.Name)
	if err != nil {
		return err
	}
	if ok {
		return fmt.Errorf("stop the server first: gsm stop %s", i.Name)
	}
	g, err := game.Get(i.Game)
	if err != nil {
		return err
	}
	if err := autoSnapshot(e, i, "update"); err != nil {
		return err
	}
	before, _ := steam.BuildID(i.ServerDir(), g.SteamAppID)
	if err := steam.Install(i.ServerDir(), g.SteamAppID, os.Stdout); err != nil {
		return err
	}
	if err := linkData(i, g); err != nil {
		return err
	}
	after, _ := steam.BuildID(i.ServerDir(), g.SteamAppID)
	if before == after {
		fmt.Printf("%s: already up to date (build %s)\n", i.Name, after)
	} else {
		fmt.Printf("%s: updated build %s -> %s\n", i.Name, orNone(before), after)
	}
	return nil
}

func orNone(s string) string {
	if s == "" {
		return "none"
	}
	return s
}
