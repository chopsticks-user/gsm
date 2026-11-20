// Command gsm manages self-hosted game servers.
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"runtime"
	"strings"

	"github.com/chopsticks-user/gsm/internal/home"
	"github.com/chopsticks-user/gsm/internal/instance"
	"github.com/chopsticks-user/gsm/internal/proc"
	"github.com/chopsticks-user/gsm/internal/store"
)

const usage = `usage: gsm [--home DIR] <command> [args]

  gsm doctor
  gsm game list | show <game>
  gsm create <game> <name> [--port N] [--set k=v]...
  gsm list
  gsm start|stop <name>... | --all
  gsm restart|run <name>
  gsm status [--json]
  gsm logs <name> [-f]
  gsm update <name>
  gsm config diff|log|commit <name> [-m msg]
  gsm save snapshot <name> [-m msg] | list <name> | restore <name> <id>
  gsm addon add|remove <name> <addon>
  gsm version | --version
`

// env is what every command needs.
type env struct {
	home  string
	store store.Store
}

func (e env) load(name string) (instance.Instance, error) { return instance.Load(e.home, name) }

type command func(e env, args []string) error

var commands = map[string]command{
	"doctor":  cmdDoctor,
	"game":    cmdGame,
	"create":  cmdCreate,
	"list":    cmdList,
	"start":   cmdStart,
	"stop":    cmdStop,
	"restart": cmdRestart,
	"run":     cmdRun,
	"status":  cmdStatus,
	"logs":    cmdLogs,
	"update":  cmdUpdate,
	"config":  cmdConfig,
	"save":    cmdSave,
	"addon":   cmdAddon,
}

var planned = []string{"remove", "import", "remote", "recover"}

func main() {
	proc.HandleHelper()
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "gsm:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	top := flag.NewFlagSet("gsm", flag.ContinueOnError)
	top.Usage = func() { fmt.Fprint(os.Stderr, usage) }
	homeFlag := top.String("home", "", "data directory (default $GSM_HOME or the per-OS location)")
	versionFlag := top.Bool("version", false, "print the version and exit")
	if err := top.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	// Before resolving the home, so the version works without a data dir.
	if *versionFlag || top.Arg(0) == "version" {
		fmt.Println(versionString())
		return nil
	}
	if top.NArg() == 0 {
		fmt.Fprint(os.Stderr, usage)
		return errors.New("no command given")
	}
	userHome, _ := os.UserHomeDir()
	h, err := home.Resolve(*homeFlag, runtime.GOOS, os.Getenv, userHome)
	if err != nil {
		return err
	}
	e := env{home: h, store: store.Store{Dir: h + string(os.PathSeparator) + "store"}}

	name, rest := top.Arg(0), top.Args()[1:]
	if cmd, ok := commands[name]; ok {
		return cmd(e, rest)
	}
	for _, p := range planned {
		if p == name {
			return fmt.Errorf("%s is not implemented yet", name)
		}
	}
	return fmt.Errorf("unknown command %q (see: gsm --help)", name)
}

// parse parses flags that may appear before, between or after positional
// arguments (the standard flag package stops at the first positional).
func parse(fs *flag.FlagSet, args []string) ([]string, error) {
	fs.SetOutput(os.Stderr)
	var pos []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		args = fs.Args()
		if len(args) == 0 {
			return pos, nil
		}
		pos, args = append(pos, args[0]), args[1:]
	}
}

func wantArgs(pos []string, n int, form string) error {
	if len(pos) != n {
		return fmt.Errorf("usage: gsm %s", form)
	}
	return nil
}

// targets resolves <name>... | --all into instances.
func targets(e env, names []string, all bool, form string) ([]instance.Instance, error) {
	if all {
		if len(names) > 0 {
			return nil, errors.New("give instance names or --all, not both")
		}
		return instance.List(e.home)
	}
	if len(names) == 0 {
		return nil, fmt.Errorf("usage: gsm %s", form)
	}
	var out []instance.Instance
	for _, n := range names {
		i, err := e.load(n)
		if err != nil {
			return nil, err
		}
		out = append(out, i)
	}
	return out, nil
}

// multiFlag collects repeated --set k=v flags.
type multiFlag []string

func (m *multiFlag) String() string     { return strings.Join(*m, ",") }
func (m *multiFlag) Set(v string) error { *m = append(*m, v); return nil }
