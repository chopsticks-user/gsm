//go:build unix

package proc

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// The wrapper's child ignores SIGTERM; Stop must still end it by killing the
// whole group, not just the wrapper.
func TestStopKillsWholeGroup(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "child")
	st, _ := launch(t, "wrapper", "main", "GSM_CHILD_PID_FILE="+pidFile)
	var child int
	for i := 0; i < 50 && child == 0; i++ {
		b, _ := os.ReadFile(pidFile)
		child, _ = strconv.Atoi(strings.TrimSpace(string(b)))
		time.Sleep(50 * time.Millisecond)
	}
	if child == 0 {
		t.Fatal("wrapper never started its child")
	}
	childStart, err := StartTime(child)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := Stop(st, time.Second); err != nil {
		t.Fatal(err)
	}
	if Alive(State{PID: child, Start: childStart}) {
		t.Fatal("child survived")
	}
}

// Like steam-run: the launched process exits while its child keeps running.
// The server must still count as running, and Stop must still end it.
func TestLeaderExitsChildKeepsRunning(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "child")
	st, sf := launch(t, "wrapper", "main", "GSM_CHILD_PID_FILE="+pidFile, "GSM_WRAPPER_EXITS=1")
	for i := 0; i < 100 && Alive(st); i++ {
		time.Sleep(50 * time.Millisecond)
	}
	if Alive(st) {
		t.Fatal("wrapper should have exited")
	}
	if _, ok, _ := Running(sf, "main"); !ok {
		t.Fatal("server reported stopped while its child runs")
	}
	if _, _, err := Stop(st, time.Second); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := Running(sf, "main"); ok {
		t.Fatal("still running after Stop")
	}
}
