//go:build linux

package e2e

import (
	"os"
	"strings"
	"testing"

	"github.com/chopsticks-user/gsm/tests/internal/harness"
)

// TestCrashOnStart: a server that dies at once fails start, leaves no state
// behind, and leaves its error in the log.
func TestCrashOnStart(t *testing.T) {
	t.Parallel()
	h := harness.New(t)
	h.OK("create", "palworld", "main")
	out, err := h.Run([]string{"GSM_TEST_CRASH=1"}, "start", "main")
	if err == nil {
		t.Fatalf("start of a crashing server succeeded:\n%s", out)
	}
	harness.MustContain(t, out, "server exited right after start; see: gsm logs main")
	if _, err := os.Stat(h.Inst("main", "state.json")); !os.IsNotExist(err) {
		t.Fatalf("state.json after a crash: %v", err)
	}
	if h.Status()["main"].Running {
		t.Fatal("a crashed server shows as running")
	}
	harness.MustContain(t, h.OK("logs", "main"), "=== gsm start ", ", config ", "fake server: fatal error: cannot bind the port")
	harness.MustContain(t, h.OK("start", "main"), "main: started")
}

// TestDeletedInstall: server/ is disposable. Without it start says what to
// run, and update brings it back with the saves and config untouched.
func TestDeletedInstall(t *testing.T) {
	t.Parallel()
	h := harness.New(t)
	save := h.Inst("main", "saves", harness.World, "Level.sav")
	settings := h.Inst("main", "config", "PalWorldSettings.ini")
	h.OK("create", "palworld", "main")
	h.OK("start", "main")
	harness.WaitFor(t, "the first save", func() bool { _, err := os.Stat(save); return err == nil })
	h.OK("stop", "main")
	if err := os.WriteFile(settings, []byte("[/Script/Pal.PalGameWorldSettings]\nOptionSettings=(ServerName=\"kept\")\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	wantSave, wantSettings := harness.ReadFile(t, save), harness.ReadFile(t, settings)

	if err := os.RemoveAll(h.Inst("main", "server")); err != nil {
		t.Fatal(err)
	}
	harness.MustContain(t, h.Fails("start", "main"), "server not installed", "run: gsm update main")
	// The failed start still committed the edit, as every start does first.
	harness.MustContain(t, h.OK("config", "log", "main"), "auto: before start")

	harness.MustContain(t, h.OK("update", "main"), "updated build none -> 100")
	if harness.ReadFile(t, save) != wantSave || harness.ReadFile(t, settings) != wantSettings {
		t.Fatal("the reinstall changed the saves or the config")
	}
	harness.MustContain(t, h.OK("start", "main"), "main: started")
	harness.WaitFor(t, "a save from the reinstalled server", func() bool { return strings.HasPrefix(harness.ReadFile(t, save), "tick") })
}

// TestClobberedSavesLink: when steamcmd replaces the saves link with a real
// folder holding files, gsm never picks one copy of the world over the
// other: update and start stop with an error and touch neither.
func TestClobberedSavesLink(t *testing.T) {
	t.Parallel()
	h := harness.New(t)
	save := h.Inst("main", "saves", harness.World, "Level.sav")
	stray := h.Inst("main", "server", "Pal", "Saved", "SaveGames", "0", "STOCK", "Level.sav")
	h.OK("create", "palworld", "main")
	if err := os.MkdirAll(h.Inst("main", "saves", harness.World), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(save, []byte("my world"), 0o644); err != nil {
		t.Fatal(err)
	}

	out, err := h.Run([]string{"GSM_TEST_CLOBBER=1"}, "update", "main")
	if err == nil {
		t.Fatalf("update over a clobbered saves link succeeded:\n%s", out)
	}
	harness.MustContain(t, out, "have files; merge them by hand first")
	h.SnapshotID("main", "update") // taken before steamcmd ran
	harness.MustContain(t, h.Fails("start", "main"), "have files; merge them by hand first")
	if h.Status()["main"].Running {
		t.Fatal("the server started on a clobbered saves link")
	}
	if harness.ReadFile(t, save) != "my world" || harness.ReadFile(t, stray) != "stock save" {
		t.Fatal("gsm changed one of the two copies")
	}

	// Once the user removes the stray folder, start links the saves again.
	if err := os.RemoveAll(h.Inst("main", "server", "Pal", "Saved", "SaveGames")); err != nil {
		t.Fatal(err)
	}
	harness.MustContain(t, h.OK("start", "main"), "main: started")
	if fi, err := os.Lstat(h.Inst("main", "server", "Pal", "Saved", "SaveGames")); err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("saves not linked again (%v)", err)
	}
}
