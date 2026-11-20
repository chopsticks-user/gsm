package proc

import (
	"errors"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/chopsticks-user/gsm/internal/fsutil"
)

// TestMain doubles as a fake game server: the tests re-run this test binary
// with GSM_FAKE_SERVER set, which works the same on every OS.
func TestMain(m *testing.M) {
	HandleHelper()
	switch os.Getenv("GSM_FAKE_SERVER") {
	case "obedient": // exits on SIGTERM / Ctrl+Break
		c := make(chan os.Signal, 1)
		signal.Notify(c, syscall.SIGTERM, os.Interrupt)
		<-c
		os.Exit(0)
	case "stubborn": // ignores the stop signal, so only a kill ends it
		signal.Ignore(syscall.SIGTERM, os.Interrupt)
		time.Sleep(time.Hour)
		os.Exit(0)
	case "wrapper": // like PalServer.sh: starts the real server as a child and waits
		c := exec.Command(os.Args[0])
		c.Env = append(os.Environ(), "GSM_FAKE_SERVER=stubborn")
		if err := c.Start(); err != nil {
			os.Exit(1)
		}
		os.WriteFile(os.Getenv("GSM_CHILD_PID_FILE"), []byte(strconv.Itoa(c.Process.Pid)), 0o644)
		if os.Getenv("GSM_WRAPPER_EXITS") == "" {
			c.Wait()
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func launch(t *testing.T, mode, instance string, extraEnv ...string) (State, string) {
	t.Helper()
	stateFile := filepath.Join(t.TempDir(), "state.json")
	c := exec.Command(os.Args[0])
	c.Env = append(append(os.Environ(), "GSM_FAKE_SERVER="+mode), extraEnv...)
	st, err := Launch(c, stateFile, instance)
	if err != nil {
		t.Fatal(err)
	}
	// Reap the child, as init does for real servers after gsm exits.
	go c.Wait()
	t.Cleanup(func() { kill(st.PID) })
	time.Sleep(300 * time.Millisecond) // let it install its signal handlers
	return st, stateFile
}

func TestStartThenRunning(t *testing.T) {
	st, sf := launch(t, "obedient", "main")
	if got, ok, _ := Running(sf, "main"); !ok || got.PID != st.PID {
		t.Fatalf("Running = %v, %v", got, ok)
	}
	if _, ok, _ := Running(sf, "other"); ok {
		t.Fatal("state file of instance main matched instance other")
	}
}

func TestStopObedient(t *testing.T) {
	st, sf := launch(t, "obedient", "main")
	killed, _, err := Stop(st, 5*time.Second)
	if err != nil || killed {
		t.Fatalf("killed=%v err=%v", killed, err)
	}
	if _, ok, _ := Running(sf, "main"); ok {
		t.Fatal("still running")
	}
}

func TestStopStubbornEndsWithKill(t *testing.T) {
	st, _ := launch(t, "stubborn", "main")
	killed, _, err := Stop(st, time.Second)
	if err != nil || !killed {
		t.Fatalf("killed=%v err=%v", killed, err)
	}
	if Alive(st) {
		t.Fatal("still alive")
	}
}

func TestReusedPIDCountsAsStopped(t *testing.T) {
	st, _ := launch(t, "obedient", "main")
	st.Start = "1" // same PID, different start time: what PID reuse looks like
	if Alive(st) || ServerAlive(st) {
		t.Fatal("mismatched start time treated as alive")
	}
}

func TestStoppingOneInstanceLeavesTheOther(t *testing.T) {
	a, _ := launch(t, "obedient", "a")
	b, _ := launch(t, "obedient", "b")
	if _, _, err := Stop(a, 5*time.Second); err != nil {
		t.Fatal(err)
	}
	if !Alive(b) {
		t.Fatal("stopping a also stopped b")
	}
}

func failTerminate(t *testing.T) {
	orig := terminateGroup
	terminateGroup = func(int) error { return errors.New("send Ctrl+Break: attach failed") }
	t.Cleanup(func() { terminateGroup = orig })
}

// If the stop request fails (on Windows, the Ctrl+Break helper can't attach
// to the console), Stop still waits out the timeout, then force-stops, and
// reports the failed request as a warning rather than an error.
func TestStopForcesWhenRequestFails(t *testing.T) {
	st, _ := launch(t, "obedient", "main")
	failTerminate(t)
	killed, warning, err := Stop(st, time.Second)
	if err != nil || !killed {
		t.Fatalf("killed=%v err=%v, want a kill without error", killed, err)
	}
	if warning == nil || !strings.Contains(warning.Error(), "attach failed") {
		t.Fatalf("warning = %v, want the failed request", warning)
	}
	if Alive(st) {
		t.Fatal("still alive")
	}
}

// A request that fails because the server is already gone is a clean stop.
func TestStopRequestFailsServerAlreadyGone(t *testing.T) {
	st, _ := launch(t, "obedient", "main")
	kill(st.PID)
	if !waitGone(st, 5*time.Second) {
		t.Fatal("fake server didn't die")
	}
	failTerminate(t)
	killed, _, err := Stop(st, time.Second)
	if err != nil || killed {
		t.Fatalf("killed=%v err=%v, want a clean stop", killed, err)
	}
}

// A state file without a format (v0.1.0) still reads; a newer one is an
// error, not "stopped", so gsm never starts a second server beside it.
func TestStateFormat(t *testing.T) {
	sf := filepath.Join(t.TempDir(), "state.json")
	os.WriteFile(sf, []byte(`{"pid":1,"start_time":"x","instance":"main"}`), 0o644)
	if _, _, err := Running(sf, "main"); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(sf, []byte(`{"format":2,"pid":1,"start_time":"x","instance":"main"}`), 0o644)
	if _, _, err := Running(sf, "main"); !errors.Is(err, fsutil.ErrTooNew) {
		t.Fatalf("err = %v, want ErrTooNew", err)
	}
}
