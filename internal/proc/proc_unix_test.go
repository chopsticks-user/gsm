//go:build unix

package proc

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
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
	if _, _, err := Stop(st, syscall.SIGTERM, time.Second); err != nil {
		t.Fatal(err)
	}
	if Alive(State{PID: child, Start: childStart}) {
		t.Fatal("child survived")
	}
}

// A game's stop signal reaches the group: SIGINT stops a server that
// ignores SIGTERM without a kill.
func TestStopWithGameSignal(t *testing.T) {
	st, _ := launch(t, "sigint", "main")
	killed, _, err := Stop(st, syscall.SIGINT, 5*time.Second)
	if err != nil || killed {
		t.Fatalf("killed %v, err %v", killed, err)
	}
}
