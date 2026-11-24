//go:build linux

package e2e

import (
	"os"
	"strings"
	"testing"

	"github.com/chopsticks-user/gsm/tests/internal/harness"
)

// TestEnshroudedUnderWine runs Enshrouded's Windows server through Wine:
// its one JSON config file is linked into the install with the instance's
// port, each instance gets its own Wine prefix, and stop sends SIGINT,
// the only signal it saves on.
func TestEnshroudedUnderWine(t *testing.T) {
	t.Parallel()
	h := harness.New(t)
	cfg := h.Inst("main", "config", "enshrouded_server.json")
	save := h.Inst("main", "saves", harness.EnshroudedSave)

	// The fake steamcmd installs Enshrouded only as the Windows build.
	harness.MustContain(t, h.OK("create", "enshrouded", "main"), "created main (Enshrouded, port 15637)")
	harness.MustContain(t, h.OK("create", "enshrouded", "second"), "port 15638")
	for name, port := range map[string]string{"main": "15637", "second": "15638"} {
		file := h.Inst(name, "config", "enshrouded_server.json")
		if got, want := harness.ReadFile(t, file), "{\n  \"queryPort\": "+port+"\n}\n"; got != want {
			t.Fatalf("%s's config = %q, want %q", name, got, want)
		}
		if dest, err := os.Readlink(h.Inst(name, "server", "enshrouded_server.json")); err != nil || dest != file {
			t.Fatalf("%s's install links its config to %q (%v), want %s", name, dest, err, file)
		}
	}

	harness.MustContain(t, h.OK("start", "main"), "main: started")
	harness.WaitFor(t, "the first save", func() bool { _, err := os.Stat(save); return err == nil })
	harness.MustContain(t, h.OK("logs", "main"), "WINEPREFIX="+h.Inst("main", "wine"), `"queryPort": 15637`)

	harness.MustContain(t, h.OK("stop", "main"), "main: stopped", "main: snapshot")
	if got := harness.ReadFile(t, save); !strings.HasPrefix(got, "final save") {
		t.Fatalf("save after stop = %q, want the shutdown save SIGINT triggers", got)
	}
	h.SnapshotID("main", "stop")

	// instance.toml owns the port: a hand edit is put back on the next start.
	if err := os.WriteFile(cfg, []byte("{\n  \"queryPort\": 1\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	h.OK("start", "main")
	harness.MustContain(t, harness.ReadFile(t, cfg), `"queryPort": 15637`)
	h.OK("stop", "main")
}
