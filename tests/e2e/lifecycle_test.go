//go:build linux

package e2e

import (
	"os"
	"strings"
	"syscall"
	"testing"

	"github.com/chopsticks-user/gsm/tests/internal/harness"
)

// TestLifecycle drives one Palworld server through its whole life: create
// (with a port conflict), start, status, config history, a live snapshot,
// restart, stop, restore, update and addons.
func TestLifecycle(t *testing.T) {
	h := harness.New(t)
	save := h.Inst("main", "saves", harness.World, "Level.sav")
	settings := h.Inst("main", "config", "PalWorldSettings.ini")

	// create installs, seeds the config and links the data into the install.
	harness.MustContain(t, h.OK("create", "palworld", "main", "--set", "players=4"), "created main (Palworld, port 8211)")
	harness.MustContain(t, harness.ReadFile(t, settings), `ServerName="Default"`)
	for _, link := range []string{"Pal/Saved/Config/LinuxServer", "Pal/Saved/SaveGames"} {
		if fi, err := os.Lstat(h.Inst("main", "server", link)); err != nil || fi.Mode()&os.ModeSymlink == 0 {
			t.Fatalf("%s is not a link into the instance (%v)", link, err)
		}
	}
	// A second instance gets the next free port.
	harness.MustContain(t, h.OK("create", "palworld", "second"), "using 8212", "port 8212")

	// start runs the server detached; it writes saves through the link.
	harness.MustContain(t, h.OK("start", "main"), "main: started")
	harness.WaitFor(t, "the first save", func() bool { _, err := os.Stat(save); return err == nil })
	st := h.Status()
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
	harness.MustContain(t, h.OK("config", "diff", "main"), `+OptionSettings=(ServerName="e2e")`)

	// A snapshot of a running server works, with a warning.
	// The fake server rewrites its save every 100 ms, so gsm may say "try
	// again", as it would to a user; retry like one would.
	var out string
	for try := 0; ; try++ {
		var err error
		if out, err = h.Run(nil, "save", "snapshot", "main", "-m", "while running"); err == nil {
			break
		}
		if try == 4 || !strings.Contains(out, "try again") {
			t.Fatalf("gsm save snapshot: %v\n%s", err, out)
		}
	}
	harness.MustContain(t, out, "no save-flush method", "main: snapshot")

	harness.MustContain(t, h.OK("restart", "main"), "main: stopped", "committed config changes", "main: started")
	harness.MustContain(t, h.OK("config", "log", "main"), "auto: before start")
	if syscall.Kill(-firstPID, 0) == nil {
		t.Fatalf("the first server's process group %d survived restart", firstPID)
	}
	harness.WaitFor(t, "a save from the restarted server", func() bool { return strings.HasPrefix(harness.ReadFile(t, save), "tick") })

	// Restore refuses while the server runs and leaves the saves alone.
	before := harness.ReadFile(t, h.Inst("main", "instance.toml"))
	harness.MustContain(t, h.Fails("save", "restore", "main", h.SnapshotID("main", "manual")), "stop the server first")
	if harness.ReadFile(t, h.Inst("main", "instance.toml")) != before {
		t.Fatal("a refused restore changed the instance")
	}

	// stop asks the group to exit, takes a snapshot and leaves no process.
	pid := h.Status()["main"].PID
	harness.MustContain(t, h.OK("stop", "main"), "main: stopped", "main: snapshot")
	if h.Status()["main"].Running {
		t.Fatal("main still running after stop")
	}
	if syscall.Kill(-pid, 0) == nil {
		t.Fatalf("process group %d survived stop", pid)
	}
	final := harness.ReadFile(t, save)
	if !strings.HasPrefix(final, "final save") {
		t.Fatalf("save after stop = %q, want the server's shutdown save", final)
	}
	stopID := h.SnapshotID("main", "stop")

	// restore takes a safety snapshot and brings the snapshot's bytes back.
	if err := os.WriteFile(save, []byte("corrupted"), 0o644); err != nil {
		t.Fatal(err)
	}
	harness.MustContain(t, h.OK("save", "restore", "main", stopID), "safety snapshot", "restored snapshot "+stopID)
	if got := harness.ReadFile(t, save); got != final {
		t.Fatalf("restored save = %q, want %q", got, final)
	}
	h.SnapshotID("main", "restore")
	if _, err := os.Stat(h.Inst("main", "saves.restore")); err == nil {
		t.Fatal("saves.restore left behind")
	}

	// update snapshots first, reinstalls and reports the build change.
	out, err := h.Run([]string{"GSM_TEST_BUILD=101"}, "update", "main")
	if err != nil {
		t.Fatalf("gsm update: %v\n%s", err, out)
	}
	harness.MustContain(t, out, "updated build 100 -> 101")
	h.SnapshotID("main", "update")
	if fi, err := os.Lstat(h.Inst("main", "server", "Pal/Saved/SaveGames")); err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("saves link lost after update (%v)", err)
	}

	// Addons are recorded in instance.toml and shown in status.
	h.OK("addon", "add", "main", "playit")
	harness.MustContain(t, harness.ReadFile(t, h.Inst("main", "instance.toml")), "playit")
	if a := h.Status()["main"].Addons; len(a) != 1 || a[0] != "playit:stopped" {
		t.Fatalf("addons in status = %v, want [playit:stopped]", a)
	}
	h.OK("addon", "remove", "main", "playit")
	if strings.Contains(harness.ReadFile(t, h.Inst("main", "instance.toml")), "playit") {
		t.Fatal("playit still in instance.toml after remove")
	}
}
