//go:build linux

package integration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chopsticks-user/gsm/internal/store"
	"github.com/chopsticks-user/gsm/tests/internal/harness"
)

// The store lock serializes gsm processes: a snapshot waits for another
// process's lock and writes nothing until it gets it, and snapshots started
// together all succeed.
func TestStoreLockAcrossProcesses(t *testing.T) {
	t.Parallel()
	h := harness.New(t)
	h.OK("create", "palworld", "main")
	save := h.Inst("main", "saves", harness.World, "Level.sav")
	if err := os.MkdirAll(filepath.Dir(save), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(save, []byte("my world"), 0o644); err != nil {
		t.Fatal(err)
	}
	records := func() int {
		m, _ := filepath.Glob(filepath.Join(h.Dir, "store", "snapshots", "main", "*.json"))
		return len(m)
	}

	// This test process plays another gsm holding the lock.
	unlock, err := store.Store{Dir: filepath.Join(h.Dir, "store")}.Lock(time.Second)
	if err != nil {
		t.Fatal(err)
	}
	waiter := h.Command(nil, "save", "snapshot", "main", "-m", "waited")
	out, err := waiter.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := waiter.Start(); err != nil {
		unlock()
		t.Fatal(err)
	}
	done := make(chan error, 1)
	var output strings.Builder
	go func() {
		b := make([]byte, 4096)
		for {
			n, err := out.Read(b)
			output.Write(b[:n])
			if err != nil {
				break
			}
		}
		done <- waiter.Wait()
	}()
	select {
	case err := <-done:
		unlock()
		t.Fatalf("gsm save snapshot didn't wait for the lock (err %v):\n%s", err, output.String())
	case <-time.After(time.Second):
	}
	if n := records(); n != 0 {
		unlock()
		t.Fatalf("%d snapshot records written while another process held the lock", n)
	}
	unlock()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("gsm save snapshot after the lock was released: %v\n%s", err, output.String())
		}
	case <-time.After(10 * time.Second):
		waiter.Process.Kill()
		t.Fatal("gsm save snapshot still waiting after the lock was released")
	}
	harness.MustContain(t, output.String(), "main: snapshot")

	// Four snapshots at once take the lock in turn.
	const n = 4
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		go func() {
			o, err := h.Run(nil, "save", "snapshot", "main")
			if err != nil {
				err = &runError{err, o}
			}
			errs <- err
		}()
	}
	for i := 0; i < n; i++ {
		if err := <-errs; err != nil {
			t.Error(err)
		}
	}
	if t.Failed() {
		t.FailNow()
	}
	if got := records(); got != n+1 {
		t.Fatalf("%d snapshot records, want %d", got, n+1)
	}
	ids := map[string]bool{}
	for _, line := range strings.Split(h.OK("save", "list", "main"), "\n")[1:] {
		if f := strings.Fields(line); len(f) > 0 {
			ids[f[0]] = true
		}
	}
	if len(ids) != n+1 {
		t.Fatalf("snapshot IDs %v, want %d distinct", ids, n+1)
	}
	if _, err := os.Stat(filepath.Join(h.Dir, "store", "lock")); !os.IsNotExist(err) {
		t.Fatalf("store/lock left behind: %v", err)
	}

	// The store stayed consistent: the newest snapshot restores.
	if err := os.WriteFile(save, []byte("corrupted"), 0o644); err != nil {
		t.Fatal(err)
	}
	harness.MustContain(t, h.OK("save", "restore", "main", h.SnapshotID("main", "manual")), "restored snapshot")
	if got := harness.ReadFile(t, save); got != "my world" {
		t.Fatalf("restored save = %q", got)
	}
}

type runError struct {
	err error
	out string
}

func (e *runError) Error() string { return "gsm save snapshot: " + e.err.Error() + "\n" + e.out }
