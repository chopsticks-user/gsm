//go:build unix

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// The end-to-end test drives the real CLI through a whole server lifecycle
// without Steam or a real game. This test binary plays every part, picked by
// GSM_E2E_ROLE: gsm itself, a fake steamcmd that lays out a Palworld-shaped
// install, and a fake server that writes saves and exits on SIGTERM. Tiny
// shell shims named steamcmd, steam-run and podman put it on PATH, so gsm
// runs exactly as in production. The shims need a POSIX shell, hence unix.

const fakeWorld = "0/E2EWORLD"

func TestMain(m *testing.M) {
	switch os.Getenv("GSM_E2E_ROLE") {
	case "gsm":
		main() // exits 1 on error; returns on success
		os.Exit(0)
	case "steamcmd":
		os.Exit(fakeSteamcmd(os.Args[1:]))
	case "server":
		os.Exit(fakeServer())
	}
	os.Exit(m.Run())
}

// fakeSteamcmd handles "+force_install_dir DIR ... +app_update ID validate".
func fakeSteamcmd(args []string) int {
	var dir string
	for i, a := range args {
		if a == "+force_install_dir" && i+1 < len(args) {
			dir = args[i+1]
		}
	}
	if dir == "" {
		fmt.Fprintln(os.Stderr, "fake steamcmd: no +force_install_dir")
		return 1
	}
	build := os.Getenv("GSM_E2E_BUILD")
	if build == "" {
		build = "100"
	}
	exe, _ := os.Executable()
	files := map[string]string{
		"steamapps/appmanifest_2394010.acf": fmt.Sprintf("\"AppState\"\n{\n\t\"buildid\"\t\t\"%s\"\n}\n", build),
		// Like the real PalServer.sh, it runs the server as a child, not exec.
		"PalServer.sh":                "#!/bin/sh\nGSM_E2E_ROLE=server \"" + exe + "\" \"$@\"\n",
		"DefaultPalWorldSettings.ini": "[/Script/Pal.PalGameWorldSettings]\nOptionSettings=(ServerName=\"Default\")\n",
	}
	for name, content := range files {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		if err := os.WriteFile(p, []byte(content), 0o755); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
	}
	fmt.Println("Success! App '2394010' fully installed.")
	return 0
}

// fakeServer rewrites a save every 100 ms through the linked saves folder and
// writes a final save when asked to stop, like a game saving on shutdown.
func fakeServer() int {
	fmt.Println("Running fake server with", strings.Join(os.Args[1:], " "))
	save := filepath.Join("Pal", "Saved", "SaveGames", fakeWorld, "Level.sav")
	if err := os.MkdirAll(filepath.Dir(save), 0o755); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGTERM, os.Interrupt)
	for n := 0; ; n++ {
		select {
		case <-stop:
			os.WriteFile(save, []byte(fmt.Sprintf("final save after %d ticks\n", n)), 0o644)
			return 0
		case <-time.After(100 * time.Millisecond):
			os.WriteFile(save, []byte(fmt.Sprintf("tick %d\n", n)), 0o644)
		}
	}
}

type e2e struct {
	t    *testing.T
	home string // $GSM_HOME
	env  []string
}

func newE2E(t *testing.T) *e2e {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	// On NixOS gsm refuses a home under /tmp (steam-run hides /tmp from the
	// server), and t.TempDir() is there, so use the user's cache dir instead.
	if _, err := os.Stat("/etc/NIXOS"); err == nil {
		cache, err := os.UserCacheDir()
		if err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(cache, 0o755); err != nil {
			t.Fatal(err)
		}
		if root, err = os.MkdirTemp(cache, "gsm-e2e-"); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { os.RemoveAll(root) })
	}
	bin := filepath.Join(root, "bin")
	fakeHome := filepath.Join(root, "home")
	shims := map[string]string{
		"steamcmd":  "#!/bin/sh\nGSM_E2E_ROLE=steamcmd exec \"" + exe + "\" \"$@\"\n",
		"steam-run": "#!/bin/sh\nexec \"$@\"\n",
		"podman":    "#!/bin/sh\nexit 1\n", // no container ever exists or starts
	}
	for name, content := range shims {
		p := filepath.Join(bin, name)
		if err := os.MkdirAll(bin, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	// A non-empty steamclient.so where gsm looks for it on NixOS.
	so := filepath.Join(fakeHome, ".local", "share", "Steam", "linux64", "steamclient.so")
	if err := os.MkdirAll(filepath.Dir(so), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(so, []byte("fake library"), 0o644); err != nil {
		t.Fatal(err)
	}
	e := &e2e{t: t, home: filepath.Join(root, "gsm")}
	for _, kv := range os.Environ() {
		if k, _, _ := strings.Cut(kv, "="); k == "PATH" || k == "HOME" || k == "GSM_HOME" || k == "XDG_DATA_HOME" {
			continue
		}
		e.env = append(e.env, kv)
	}
	// Under -race every child process would sleep 1s at exit (GORACE atexit_sleep_ms).
	e.env = append(e.env, "GORACE=atexit_sleep_ms=0")
	e.env = append(e.env, "GSM_E2E_ROLE=gsm", "HOME="+fakeHome, "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	// Whatever happens, don't leave fake servers running on the machine.
	t.Cleanup(func() { e.run(nil, "stop", "--all") })
	return e
}

// run runs gsm with args and extra env, returning stdout+stderr.
func (e *e2e) run(extraEnv []string, args ...string) (string, error) {
	exe, _ := os.Executable()
	c := exec.Command(exe, append([]string{"--home", e.home}, args...)...)
	c.Env = append(append([]string{}, e.env...), extraEnv...)
	out, err := c.CombinedOutput()
	return string(out), err
}

func (e *e2e) ok(args ...string) string {
	e.t.Helper()
	out, err := e.run(nil, args...)
	if err != nil {
		e.t.Fatalf("gsm %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return out
}

func (e *e2e) fails(args ...string) string {
	e.t.Helper()
	out, err := e.run(nil, args...)
	if err == nil {
		e.t.Fatalf("gsm %s succeeded, want an error\n%s", strings.Join(args, " "), out)
	}
	return out
}

func (e *e2e) inst(name string, parts ...string) string {
	return filepath.Join(append([]string{e.home, "instances", name}, parts...)...)
}

func mustContain(t *testing.T, out string, want ...string) {
	t.Helper()
	for _, w := range want {
		if !strings.Contains(out, w) {
			t.Fatalf("output lacks %q:\n%s", w, out)
		}
	}
}

func readFile(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func (e *e2e) status() map[string]statusRow {
	e.t.Helper()
	var rows []statusRow
	if err := json.Unmarshal([]byte(e.ok("status", "--json")), &rows); err != nil {
		e.t.Fatal(err)
	}
	m := map[string]statusRow{}
	for _, r := range rows {
		m[r.Name] = r
	}
	return m
}

// snapshotID returns the ID of the newest snapshot of name with trigger.
func (e *e2e) snapshotID(name, trigger string) string {
	e.t.Helper()
	id := ""
	for _, line := range strings.Split(e.ok("save", "list", name), "\n") {
		if f := strings.Fields(line); len(f) > 3 && f[3] == trigger {
			id = f[0]
		}
	}
	if id == "" {
		e.t.Fatalf("no %s snapshot of %s", trigger, name)
	}
	return id
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func TestLifecycleEndToEnd(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not found")
	}
	e := newE2E(t)
	save := e.inst("main", "saves", fakeWorld, "Level.sav")
	settings := e.inst("main", "config", "PalWorldSettings.ini")

	// create installs, seeds the config and links the data into the install.
	mustContain(t, e.ok("create", "palworld", "main", "--set", "players=4"), "created main (Palworld, port 8211)")
	mustContain(t, readFile(t, settings), `ServerName="Default"`)
	for _, link := range []string{"Pal/Saved/Config/LinuxServer", "Pal/Saved/SaveGames"} {
		if fi, err := os.Lstat(e.inst("main", "server", link)); err != nil || fi.Mode()&os.ModeSymlink == 0 {
			t.Fatalf("%s is not a link into the instance (%v)", link, err)
		}
	}
	// A second instance gets the next free port.
	mustContain(t, e.ok("create", "palworld", "second"), "using 8212", "port 8212")

	// start runs the server detached; it writes saves through the link.
	mustContain(t, e.ok("start", "main"), "main: started")
	waitFor(t, "the first save", func() bool { _, err := os.Stat(save); return err == nil })
	st := e.status()
	if !st["main"].Running || st["main"].PID == 0 || st["main"].Port != 8211 {
		t.Fatalf("status of main = %+v, want running on 8211", st["main"])
	}
	if st["second"].Running || st["second"].Port != 8212 {
		t.Fatalf("status of second = %+v, want stopped on 8212", st["second"])
	}
	firstPID := st["main"].PID

	// config diff shows an uncommitted edit; restart commits it.
	if err := os.WriteFile(settings, []byte("[/Script/Pal.PalGameWorldSettings]\nOptionSettings=(ServerName=\"e2e\")\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustContain(t, e.ok("config", "diff", "main"), `+OptionSettings=(ServerName="e2e")`)

	// A snapshot of a running server works, with a warning.
	mustContain(t, e.ok("save", "snapshot", "main", "-m", "while running"), "no save-flush method", "main: snapshot")

	mustContain(t, e.ok("restart", "main"), "main: stopped", "committed config changes", "main: started")
	mustContain(t, e.ok("config", "log", "main"), "auto: before start")
	if syscall.Kill(-firstPID, 0) == nil {
		t.Fatalf("the first server's process group %d survived restart", firstPID)
	}
	waitFor(t, "a save from the restarted server", func() bool { return strings.HasPrefix(readFile(t, save), "tick") })

	// Restore refuses while the server runs and leaves the saves alone.
	before := readFile(t, e.inst("main", "instance.toml"))
	mustContain(t, e.fails("save", "restore", "main", e.snapshotID("main", "manual")), "stop the server first")
	if readFile(t, e.inst("main", "instance.toml")) != before {
		t.Fatal("a refused restore changed the instance")
	}

	// stop asks the group to exit, takes a snapshot and leaves no process.
	pid := e.status()["main"].PID
	mustContain(t, e.ok("stop", "main"), "main: stopped", "main: snapshot")
	if e.status()["main"].Running {
		t.Fatal("main still running after stop")
	}
	if syscall.Kill(-pid, 0) == nil {
		t.Fatalf("process group %d survived stop", pid)
	}
	final := readFile(t, save)
	if !strings.HasPrefix(final, "final save") {
		t.Fatalf("save after stop = %q, want the server's shutdown save", final)
	}
	stopID := e.snapshotID("main", "stop")

	// restore takes a safety snapshot and brings the snapshot's bytes back.
	if err := os.WriteFile(save, []byte("corrupted"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustContain(t, e.ok("save", "restore", "main", stopID), "safety snapshot", "restored snapshot "+stopID)
	if got := readFile(t, save); got != final {
		t.Fatalf("restored save = %q, want %q", got, final)
	}
	e.snapshotID("main", "restore")
	if _, err := os.Stat(e.inst("main", "saves.restore")); err == nil {
		t.Fatal("saves.restore left behind")
	}

	// update snapshots first, reinstalls and reports the build change.
	out, err := e.run([]string{"GSM_E2E_BUILD=101"}, "update", "main")
	if err != nil {
		t.Fatalf("gsm update: %v\n%s", err, out)
	}
	mustContain(t, out, "updated build 100 -> 101")
	e.snapshotID("main", "update")
	if fi, err := os.Lstat(e.inst("main", "server", "Pal/Saved/SaveGames")); err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("saves link lost after update (%v)", err)
	}

	// Addons are recorded in instance.toml and shown in status.
	e.ok("addon", "add", "main", "playit")
	mustContain(t, readFile(t, e.inst("main", "instance.toml")), "playit")
	if a := e.status()["main"].Addons; len(a) != 1 || a[0] != "playit:stopped" {
		t.Fatalf("addons in status = %v, want [playit:stopped]", a)
	}
	e.ok("addon", "remove", "main", "playit")
	if strings.Contains(readFile(t, e.inst("main", "instance.toml")), "playit") {
		t.Fatal("playit still in instance.toml after remove")
	}
}
