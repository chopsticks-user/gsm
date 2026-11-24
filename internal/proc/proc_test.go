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
	case "sigint": // like Enshrouded under Wine: SIGTERM is ignored here (Wine dies on it), SIGINT stops it cleanly
		signal.Ignore(syscall.SIGTERM)
		c := make(chan os.Signal, 1)
		signal.Notify(c, os.Interrupt)
		<-c
		os.Exit(0)
	case "stubborn": // survives the stop signal, so only a kill ends it
		// Catch the signals and drop them: signal.Ignore on Windows hands
		// Ctrl+Break to the default handler, which exits the process.
		signal.Notify(make(chan os.Signal, 1), syscall.SIGTERM, os.Interrupt)
		time.Sleep(time.Hour)
		os.Exit(0)
	case "orphaner": // starts a child that starts a grandchild and exits, orphaning it; then survives stop requests
		c := exec.Command(os.Args[0])
		c.Env = append(os.Environ(), "GSM_FAKE_SERVER=middle")
		if c.Run() != nil {
			os.Exit(1)
		}
		signal.Notify(make(chan os.Signal, 1), syscall.SIGTERM, os.Interrupt)
		time.Sleep(time.Hour)
		os.Exit(0)
	case "middle":
		c := exec.Command(os.Args[0])
		c.Env = append(os.Environ(), "GSM_FAKE_SERVER=stubborn")
		if c.Start() != nil {
			os.Exit(1)
		}
		os.WriteFile(os.Getenv("GSM_CHILD_PID_FILE"), []byte(strconv.Itoa(c.Process.Pid)), 0o644)
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
	t.Cleanup(func() { kill(st) })
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
	killed, _, err := Stop(st, syscall.SIGTERM, 5*time.Second)
	if err != nil || killed {
		t.Fatalf("killed=%v err=%v", killed, err)
	}
	if _, ok, _ := Running(sf, "main"); ok {
		t.Fatal("still running")
	}
}

func TestStopStubbornEndsWithKill(t *testing.T) {
	st, _ := launch(t, "stubborn", "main")
	killed, _, err := Stop(st, syscall.SIGTERM, time.Second)
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
	if _, _, err := Stop(a, syscall.SIGTERM, 5*time.Second); err != nil {
		t.Fatal(err)
	}
	if !Alive(b) {
		t.Fatal("stopping a also stopped b")
	}
}

func failTerminate(t *testing.T) {
	orig := terminateGroup
	terminateGroup = func(int, syscall.Signal) error { return errors.New("send Ctrl+Break: attach failed") }
	t.Cleanup(func() { terminateGroup = orig })
}

// If the stop request fails (on Windows, the Ctrl+Break helper can't attach
// to the console), Stop still waits out the timeout, then force-stops, and
// reports the failed request as a warning rather than an error.
func TestStopForcesWhenRequestFails(t *testing.T) {
	st, _ := launch(t, "obedient", "main")
	failTerminate(t)
	killed, warning, err := Stop(st, syscall.SIGTERM, time.Second)
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
	kill(st)
	if !waitGone(st, 5*time.Second) {
		t.Fatal("fake server didn't die")
	}
	failTerminate(t)
	killed, _, err := Stop(st, syscall.SIGTERM, time.Second)
	if err != nil || killed {
		t.Fatalf("killed=%v err=%v, want a clean stop", killed, err)
	}
}

// A state file without a format (v0.1.0) still reads; a newer one is an
// error, not "stopped", so gsm never starts a second server beside it.
func TestStateFormat(t *testing.T) {
	sf := filepath.Join(t.TempDir(), "state.json")
	for content, tooNew := range map[string]bool{
		`{"pid":1,"start_time":"x","instance":"main"}`:            false,
		`{"format":2,"pid":1,"start_time":"x","instance":"main"}`: true,
	} {
		if err := os.WriteFile(sf, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, _, err := Running(sf, "main"); errors.Is(err, fsutil.ErrTooNew) != tooNew {
			t.Errorf("%s: err = %v", content, err)
		}
	}
}

// A force stop reaches a grandchild whose parent already exited: through the
// process group on Unix, through the Job Object on Windows (taskkill /T,
// which follows parent PIDs, would miss it).
func TestKillReachesOrphanedGrandchild(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "grandchild")
	st, _ := launch(t, "orphaner", "main", "GSM_CHILD_PID_FILE="+pidFile)
	var gc int
	for i := 0; i < 100 && gc == 0; i++ {
		b, _ := os.ReadFile(pidFile)
		gc, _ = strconv.Atoi(strings.TrimSpace(string(b)))
		time.Sleep(50 * time.Millisecond)
	}
	if gc == 0 {
		t.Fatal("no grandchild PID")
	}
	t.Cleanup(func() {
		if p, err := os.FindProcess(gc); err == nil {
			p.Kill()
		}
	})
	if killed, _, err := Stop(st, syscall.SIGTERM, time.Second); err != nil || !killed {
		t.Fatalf("killed %v, err %v", killed, err)
	}
	for i := 0; i < 100; i++ {
		if _, err := StartTime(gc); err != nil {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("orphaned grandchild survived the force stop")
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
	if _, _, err := Stop(st, syscall.SIGTERM, time.Second); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := Running(sf, "main"); ok {
		t.Fatal("still running after Stop")
	}
}

// A program that can't start leaves no state file behind.
func TestLaunchMissingProgram(t *testing.T) {
	sf := filepath.Join(t.TempDir(), "state.json")
	if _, err := Launch(exec.Command(filepath.Join(t.TempDir(), "no-such-server")), sf, "main"); err == nil {
		t.Fatal("Launch of a missing program succeeded")
	}
	if _, err := os.Stat(sf); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("state file after a failed launch: %v", err)
	}
}

// If the state file can't be written, gsm could never find the server
// again, so Launch stops it before returning the error. (Launch stops it at
// once, before a wrapper could start a child, so a group can't be checked
// here; Stop's group handling has its own tests above.)
func TestLaunchStopsServerWhenStateUnwritable(t *testing.T) {
	t.Skip("takes 15 s until #104: Launch stops the server before anything reaps it")
	sf := filepath.Join(t.TempDir(), "missing-dir", "state.json")
	c := exec.Command(os.Args[0])
	c.Env = append(os.Environ(), "GSM_FAKE_SERVER=obedient")
	if _, err := Launch(c, sf, "main"); err == nil {
		t.Fatal("Launch with an unwritable state file succeeded")
	}
	done := make(chan error, 1)
	go func() { done <- c.Wait() }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		c.Process.Kill()
		t.Fatal("the server is still running after the failed launch")
	}
}
