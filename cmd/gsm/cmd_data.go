package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"slices"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/chopsticks-user/gsm/internal/addon"
	"github.com/chopsticks-user/gsm/internal/game"
	"github.com/chopsticks-user/gsm/internal/gitrepo"
	"github.com/chopsticks-user/gsm/internal/instance"
	"github.com/chopsticks-user/gsm/internal/proc"
	"github.com/chopsticks-user/gsm/internal/steam"
	"github.com/chopsticks-user/gsm/internal/store"
)

const lockWait = 30 * time.Second

func cmdConfig(e env, args []string) error {
	fs := flag.NewFlagSet("config", flag.ContinueOnError)
	msg := fs.String("m", "", "commit message")
	pos, err := parse(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 2 {
		return errors.New("usage: gsm config diff|log|commit <name> [-m msg]")
	}
	i, err := e.load(pos[1])
	if err != nil {
		return err
	}
	repo, err := gitrepo.Init(i.Dir)
	if err != nil {
		return err
	}
	var out string
	switch pos[0] {
	case "diff":
		out, err = repo.Diff()
	case "log":
		out, err = repo.Log()
	case "commit":
		m := *msg
		if m == "" {
			m = "config change"
		}
		var ok bool
		if ok, err = repo.CommitAll(m); err == nil {
			out = "nothing to commit\n"
			if ok {
				out = "committed " + repo.Head() + "\n"
			}
		}
	case "edit", "restore", "remote":
		return fmt.Errorf("config %s is not implemented yet", pos[0])
	default:
		return errors.New("usage: gsm config diff|log|commit <name> [-m msg]")
	}
	if err != nil {
		return err
	}
	fmt.Print(out)
	return nil
}

// snapshotMeta fills the record fields every snapshot shares.
func snapshotMeta(i instance.Instance, trigger, msg string) store.Record {
	r := store.Record{Instance: i.Name, Game: i.Game, Trigger: trigger, Message: msg}
	r.ConfigRev = gitrepo.Repo{Dir: i.Dir}.Head()
	if g, err := game.Get(i.Game); err == nil {
		r.ServerBuild, _ = steam.BuildID(i.ServerDir(), g.SteamAppID)
	}
	return r
}

func takeSnapshot(e env, i instance.Instance, trigger, msg string) (store.Record, error) {
	unlock, err := e.store.Lock(lockWait)
	if err != nil {
		return store.Record{}, err
	}
	defer unlock()
	return e.store.Snapshot(i.SavesDir(), snapshotMeta(i, trigger, msg))
}

func autoSnapshot(e env, i instance.Instance, trigger string) error {
	r, err := takeSnapshot(e, i, trigger, "")
	if err != nil {
		return fmt.Errorf("automatic snapshot failed: %w", err)
	}
	fmt.Printf("%s: snapshot %s (%d files, %s)\n", i.Name, r.ID, len(r.Files), humanSize(r.Size()))
	return nil
}

func cmdSave(e env, args []string) error {
	fs := flag.NewFlagSet("save", flag.ContinueOnError)
	msg := fs.String("m", "", "snapshot message")
	pos, err := parse(fs, args)
	if err != nil {
		return err
	}
	if len(pos) < 2 {
		return errors.New("usage: gsm save snapshot <name> [-m msg] | list <name> | restore <name> <id>")
	}
	i, err := e.load(pos[1])
	if err != nil {
		return err
	}
	switch pos[0] {
	case "snapshot":
		_, ok, err := proc.Running(i.StateFile(), i.Name)
		if err != nil {
			return err
		}
		if ok {
			fmt.Fprintf(os.Stderr, "warning: %s is running and %s has no save-flush method; the snapshot may miss recent progress\n", i.Name, i.Game)
		}
		r, err := takeSnapshot(e, i, "manual", *msg)
		if err != nil {
			return err
		}
		fmt.Printf("%s: snapshot %s (%d files, %s)\n", i.Name, r.ID, len(r.Files), humanSize(r.Size()))
		return nil
	case "list":
		recs, err := e.store.List(i.Name)
		if err != nil {
			return err
		}
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "ID\tCREATED\tTRIGGER\tFILES\tSIZE\tCONFIG\tBUILD\tMESSAGE")
		for _, r := range recs {
			fmt.Fprintf(w, "%s\t%s\t%s\t%d\t%s\t%s\t%s\t%s\n", r.ID, r.Created.Local().Format("2006-01-02 15:04:05"),
				r.Trigger, len(r.Files), humanSize(r.Size()), orNone(r.ConfigRev), orNone(r.ServerBuild), r.Message)
		}
		return w.Flush()
	case "restore":
		if len(pos) != 3 {
			return errors.New("usage: gsm save restore <name> <id>")
		}
		if strings.TrimSpace(pos[2]) == "" { // before the lock wait; store.Get checks too
			return fmt.Errorf("a snapshot ID is required (see: gsm save list %s)", i.Name)
		}
		_, ok, err := proc.Running(i.StateFile(), i.Name)
		if err != nil {
			return err
		}
		if ok {
			return fmt.Errorf("stop the server first: gsm stop %s", i.Name)
		}
		unlock, err := e.store.Lock(lockWait)
		if err != nil {
			return err
		}
		defer unlock()
		rec, err := e.store.Get(i.Name, pos[2])
		if err != nil {
			return err
		}
		safe, err := e.store.Restore(rec, i.SavesDir(), snapshotMeta(i, "restore", "before restoring "+rec.ID))
		if safe.ID != "" {
			fmt.Printf("%s: safety snapshot %s\n", i.Name, safe.ID)
		}
		if err != nil {
			return fmt.Errorf("restore failed, live saves unchanged: %w", err)
		}
		fmt.Printf("%s: restored snapshot %s from %s\n", i.Name, rec.ID, rec.Created.Local().Format("2006-01-02 15:04:05"))
		return nil
	case "show", "diff", "prune", "pin", "tag", "annotate", "verify":
		return fmt.Errorf("save %s is not implemented yet", pos[0])
	}
	return errors.New("usage: gsm save snapshot <name> [-m msg] | list <name> | restore <name> <id>")
}

func cmdAddon(e env, args []string) error {
	if len(args) != 3 || (args[0] != "add" && args[0] != "remove") {
		return errors.New("usage: gsm addon add|remove <name> <addon>")
	}
	i, err := e.load(args[1])
	if err != nil {
		return err
	}
	a := args[2]
	if err := addon.Check(a); err != nil {
		return err
	}
	_, running, err := proc.Running(i.StateFile(), i.Name)
	if err != nil {
		return err
	}
	if args[0] == "add" {
		if i.HasAddon(a) {
			fmt.Printf("%s: addon %s already added\n", i.Name, a)
			return nil
		}
		i.Addons = append(i.Addons, a)
		if err := i.Save(); err != nil {
			return err
		}
		fmt.Printf("%s: added addon %s\n", i.Name, a)
		if p := addon.Problems(a); len(p) > 0 {
			fmt.Fprintf(os.Stderr, "warning: %s\n", strings.Join(p, "; "))
		}
		if running {
			return addon.Start(a, i.Name)
		}
		return nil
	}
	if !i.HasAddon(a) {
		return fmt.Errorf("%s has no addon %s", i.Name, a)
	}
	i.Addons = slices.DeleteFunc(i.Addons, func(x string) bool { return x == a })
	if err := i.Save(); err != nil {
		return err
	}
	fmt.Printf("%s: removed addon %s\n", i.Name, a)
	return addon.Stop(a, i.Name)
}

func humanSize(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}
