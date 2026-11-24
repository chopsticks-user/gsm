//go:build linux

package integration

import (
	"os"
	"strings"
	"syscall"
	"testing"

	"github.com/chopsticks-user/gsm/tests/internal/harness"
)

// A launcher that starts the server in the background and exits, as
// steam-run does: gsm finds the server by its process group, not by the PID
// it launched, and stop ends the whole group.
func TestLauncherExitsServerKeepsRunning(t *testing.T) {
	t.Parallel()
	h := harness.New(t)
	save := h.Inst("main", "saves", harness.World, "Level.sav")
	h.OK("create", "palworld", "main")
	out, err := h.Run([]string{"GSM_TEST_DETACH=1"}, "start", "main")
	if err != nil {
		t.Fatalf("gsm start: %v\n%s", err, out)
	}
	harness.MustContain(t, out, "main: started")
	harness.WaitFor(t, "the first save", func() bool { _, err := os.Stat(save); return err == nil })
	st := h.Status()["main"]
	if !st.Running {
		t.Fatalf("status = %+v, want running", st)
	}
	if syscall.Kill(st.PID, 0) == nil {
		t.Fatalf("the launched process %d still exists; the test needs it gone", st.PID)
	}
	if syscall.Kill(-st.PID, 0) != nil {
		t.Fatalf("process group %d is gone, but status says running", st.PID)
	}

	harness.MustContain(t, h.OK("stop", "main"), "main: stopped", "main: snapshot")
	if syscall.Kill(-st.PID, 0) == nil {
		t.Fatalf("process group %d survived stop", st.PID)
	}
	if got := harness.ReadFile(t, save); !strings.HasPrefix(got, "final save") {
		t.Fatalf("save after stop = %q, want the server's shutdown save", got)
	}
}

// Several instances run at once, each on its own port; start --all is safe
// to repeat, and one broken instance doesn't keep the others from starting.
func TestStartStopAll(t *testing.T) {
	t.Parallel()
	h := harness.New(t)
	h.OK("create", "palworld", "a")
	h.OK("create", "palworld", "b")

	harness.MustContain(t, h.OK("start", "--all"), "a: started", "b: started")
	harness.MustContain(t, h.OK("logs", "a"), "-port=8211")
	harness.MustContain(t, h.OK("logs", "b"), "-port=8212")
	st := h.Status()
	if !st["a"].Running || !st["b"].Running || st["a"].PID == st["b"].PID {
		t.Fatalf("status = %+v, want a and b running as separate servers", st)
	}

	harness.MustContain(t, h.OK("start", "--all"), "a: already running", "b: already running")
	if again := h.Status(); again["a"].PID != st["a"].PID || again["b"].PID != st["b"].PID {
		t.Fatalf("a second start --all changed the servers: %+v, was %+v", again, st)
	}

	harness.MustContain(t, h.OK("stop", "--all"), "a: stopped", "b: stopped")
	if err := os.Remove(h.Inst("b", "server", "PalServer.sh")); err != nil {
		t.Fatal(err)
	}
	harness.MustContain(t, h.Fails("start", "--all"), "a: started", "b: server not installed", "failed to start: b")
	if st := h.Status(); !st["a"].Running || st["b"].Running {
		t.Fatalf("status = %+v, want a running and b stopped", st)
	}
}
