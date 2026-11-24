//go:build linux

package e2e

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
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

// startRun starts gsm run main in its own process group, as a terminal runs
// a foreground command, and returns it, its output and a wait that gives up
// after 45 s. A gsm run still going when the test ends is killed before
// New's cleanup stops its server.
func startRun(t *testing.T, h *harness.Home, env ...string) (*exec.Cmd, *syncBuffer, func() error) {
	t.Helper()
	out := &syncBuffer{}
	run := h.Command(env, "run", "main")
	run.Stdout, run.Stderr = out, out
	run.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := run.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- run.Wait() }()
	t.Cleanup(func() {
		select {
		case <-done:
		default:
			run.Process.Kill()
		}
	})
	return run, out, func() error {
		select {
		case err := <-done:
			done <- err // for the cleanup
			return err
		case <-time.After(45 * time.Second):
			return errors.New("gsm run didn't exit within 45 s")
		}
	}
}

// TestRunInForeground covers gsm run: output on the terminal and in the
// log, Ctrl+C running the stop sequence, a server exiting by itself, and
// refusing a server already running in the background.
func TestRunInForeground(t *testing.T) {
	t.Parallel()
	h := harness.New(t)
	save := h.Inst("main", "saves", harness.World, "Level.sav")
	h.OK("create", "palworld", "main")

	run, out, wait := startRun(t, h)
	// gsm catches Ctrl+C before it prints this, so the signal can't come early.
	harness.WaitFor(t, "gsm run to start the server", func() bool { return strings.Contains(out.String(), "running in the foreground") })
	harness.WaitFor(t, "the first save", func() bool { _, err := os.Stat(save); return err == nil })
	if !h.Status()["main"].Running {
		t.Fatal("status doesn't show the foreground server running")
	}
	// Like a terminal's Ctrl+C: SIGINT to gsm run's whole process group.
	if err := syscall.Kill(-run.Process.Pid, syscall.SIGINT); err != nil {
		t.Fatal(err)
	}
	if err := wait(); err != nil {
		t.Fatalf("gsm run after Ctrl+C: %v\n%s", err, out.String())
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
	_, out, wait = startRun(t, h, "GSM_TEST_EXIT_AFTER=3")
	if err := wait(); err != nil {
		t.Fatalf("gsm run with a server that exits: %v\n%s", err, out.String())
	}
	harness.MustContain(t, out.String(), "main: server exited", "main: snapshot")
	if got := harness.ReadFile(t, save); !strings.HasPrefix(got, "shutdown save") {
		t.Fatalf("save = %q, want the server's own shutdown save", got)
	}

	// gsm run won't take over a server started in the background.
	h.OK("start", "main")
	harness.MustContain(t, h.Fails("run", "main"), "already running in the background")
	h.OK("stop", "main")
}
