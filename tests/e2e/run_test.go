//go:build linux

package e2e

import (
	"bytes"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chopsticks-user/gsm/tests/internal/harness"
)

// syncBuffer collects a running command's output for the test to read.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// TestRunInForeground covers gsm run: output on the terminal and in the
// log, Ctrl+C running the stop sequence, a server exiting by itself, and
// refusing a server already running in the background.
func TestRunInForeground(t *testing.T) {
	t.Parallel()
	h := harness.New(t)
	save := h.Inst("main", "saves", harness.World, "Level.sav")
	h.OK("create", "palworld", "main")

	var out syncBuffer
	run := h.Command(nil, "run", "main")
	run.Stdout, run.Stderr = &out, &out
	if err := run.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- run.Wait() }()
	// Registered after New's, so it runs first: a hung gsm run is killed
	// before stop --all stops its server.
	t.Cleanup(func() {
		select {
		case <-done:
		default:
			run.Process.Kill()
		}
	})
	// gsm catches Ctrl+C before it prints this, so the signal can't come early.
	harness.WaitFor(t, "gsm run to start the server", func() bool { return strings.Contains(out.String(), "running in the foreground") })
	harness.WaitFor(t, "the first save", func() bool { _, err := os.Stat(save); return err == nil })
	if !h.Status()["main"].Running {
		t.Fatal("status doesn't show the foreground server running")
	}
	if err := run.Process.Signal(os.Interrupt); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("gsm run after Ctrl+C: %v\n%s", err, out.String())
		}
	case <-time.After(45 * time.Second):
		t.Fatalf("gsm run didn't exit after Ctrl+C:\n%s", out.String())
	}
	harness.MustContain(t, out.String(), "Running fake server with -port=8211", "main: stopping", "main: snapshot")
	harness.MustContain(t, harness.ReadFile(t, h.Inst("main", "logs", "server.log")), "Running fake server with -port=8211")
	if got := harness.ReadFile(t, save); !strings.HasPrefix(got, "final save") {
		t.Fatalf("save after Ctrl+C = %q, want the shutdown save", got)
	}
	if _, err := os.Stat(h.Inst("main", "state.json")); !os.IsNotExist(err) {
		t.Fatalf("state.json after gsm run: %v", err)
	}
	h.SnapshotID("main", "stop")

	// A server that exits by itself ends gsm run, with a snapshot.
	o, err := h.Run([]string{"GSM_TEST_EXIT_AFTER=3"}, "run", "main")
	if err != nil {
		t.Fatalf("gsm run with a server that exits: %v\n%s", err, o)
	}
	harness.MustContain(t, o, "main: server exited", "main: snapshot")
	if got := harness.ReadFile(t, save); !strings.HasPrefix(got, "shutdown save") {
		t.Fatalf("save = %q, want the server's own shutdown save", got)
	}

	// gsm run won't take over a server started in the background.
	h.OK("start", "main")
	harness.MustContain(t, h.Fails("run", "main"), "already running in the background")
	h.OK("stop", "main")
}
