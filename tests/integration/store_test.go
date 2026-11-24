//go:build linux

package integration

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
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
	storeDir := filepath.Join(h.Dir, "store")
	held, err := store.Store{Dir: storeDir}.Lock(time.Second)
	if err != nil {
		t.Fatal(err)
	}
	unlock := sync.OnceFunc(held) // a second call must not delete a lock gsm took since
	t.Cleanup(unlock)
	var output bytes.Buffer // read only after Wait returns
	waiter := h.Command(nil, "save", "snapshot", "main", "-m", "waited")
	waiter.Stdout, waiter.Stderr = &output, &output
	if err := waiter.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- waiter.Wait() }()
	// Registered after the unlock cleanup, so it runs first.
	t.Cleanup(func() {
		select {
		case <-done:
		default:
			waiter.Process.Kill()
			<-done
		}
	})
	// The waiter's lock.tmp-* file shows it reached Lock: the test's own
	// Lock removed its temp file before returning.
	harness.WaitFor(t, "gsm save snapshot to reach the lock", func() bool {
		m, _ := filepath.Glob(filepath.Join(storeDir, "lock.tmp-*"))
		return len(m) > 0
	})
	select {
	case err := <-done:
		done <- err
		t.Fatalf("gsm save snapshot didn't wait for the lock (err %v):\n%s", err, output.String())
	case <-time.After(500 * time.Millisecond):
	}
	if n := records(); n != 0 {
		t.Fatalf("%d snapshot records written while another process held the lock", n)
	}
	unlock()
	select {
	case err := <-done:
		done <- err
		if err != nil {
			t.Fatalf("gsm save snapshot after the lock was released: %v\n%s", err, output.String())
		}
	case <-time.After(10 * time.Second):
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
				err = fmt.Errorf("gsm save snapshot: %v\n%s", err, o)
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
