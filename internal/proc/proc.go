// Package proc launches servers as detached processes and finds them again
// without a daemon. A server is identified by a state file holding its PID,
// the process start time and the instance name. The PID alone is not enough
// because the OS reuses PIDs, and the binary name is not enough because two
// instances of one game run the same binary.
package proc

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"syscall"
	"time"

	"github.com/chopsticks-user/gsm/internal/fsutil"
)

// Format is the version of state.json this gsm writes and the newest it
// reads. A file without one was written by v0.1.0 and is version 1.
const Format = 1

type State struct {
	Format   int    `json:"format,omitempty"` // omitted in the store lock, versioned by store/format
	PID      int    `json:"pid"`
	Start    string `json:"start_time"`
	Instance string `json:"instance"`
}

// Alive reports whether the process st describes is still the one that was
// started: same PID and same start time.
func Alive(st State) bool {
	t, err := StartTime(st.PID)
	return err == nil && t == st.Start
}

// ServerAlive reports whether a server launched by Launch is still running.
// The launched process may exit while the server it started keeps running
// (steam-run does this), so a surviving process group (a Job Object on
// Windows) counts too; groupAlive makes sure it is still this server's.
func ServerAlive(st State) bool {
	if t, err := StartTime(st.PID); err == nil && t == st.Start {
		return true
	}
	return groupAlive(st)
}

// Running reads the state file and reports whether that instance's server
// is running. A missing, foreign or stale state file means not running. A
// state file from a newer gsm is an error: its server may be running, and
// calling it stopped could start a second one.
func Running(stateFile, instance string) (State, bool, error) {
	var st State
	b, err := os.ReadFile(stateFile)
	if err != nil || json.Unmarshal(b, &st) != nil {
		return st, false, nil
	}
	if err := fsutil.CheckFormat(stateFile, st.Format, Format); err != nil {
		return st, false, err
	}
	if st.Instance != instance {
		return st, false, nil
	}
	return st, ServerAlive(st), nil
}

// Launch starts c detached from the terminal and records it in stateFile.
// The caller sets c.Stdout/Stderr; stdin stays the null device.
func Launch(c *exec.Cmd, stateFile, instance string) (State, error) {
	detach(c)
	if err := c.Start(); err != nil {
		return State{}, err
	}
	t, err := StartTime(c.Process.Pid)
	if err != nil {
		c.Process.Kill()
		return State{}, err
	}
	st := State{Format: Format, PID: c.Process.Pid, Start: t, Instance: instance}
	if err := afterStart(st); err != nil {
		c.Process.Kill()
		return State{}, err
	}
	b, _ := json.Marshal(st)
	if err := fsutil.WriteAtomic(stateFile, b, 0o644); err != nil {
		// Don't leave a server gsm can't find. SIGTERM, not the game's signal: the
		// server has only just started, and a kill follows anyway. Reap it as it
		// exits: an unreaped zombie still counts as a group member, and Stop would
		// wait out both its timeouts.
		go c.Wait()
		Stop(st, syscall.SIGTERM, 5*time.Second)
		return State{}, err
	}
	return st, nil
}

// terminateGroup is terminate, replaceable in tests.
var terminateGroup = terminate

// Stop asks the server's process group to exit with sig (Ctrl+Break on
// Windows), waits up to timeout, then kills it. It reports whether a kill
// was needed. A stop request that fails is returned as warning, not err: it
// may have been delivered anyway, so Stop still waits the full timeout (the
// server may be saving) and then force-stops.
func Stop(st State, sig syscall.Signal, timeout time.Duration) (killed bool, warning, err error) {
	if terr := terminateGroup(st.PID, sig); terr != nil {
		warning = fmt.Errorf("stop request failed: %w", terr)
	}
	if waitGone(st, timeout) {
		return false, warning, nil
	}
	if err := kill(st); err != nil && ServerAlive(st) {
		return true, warning, err
	}
	if !waitGone(st, 10*time.Second) {
		return true, warning, errors.New("server still running after kill")
	}
	return true, warning, nil
}

func waitGone(st State, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for {
		if !ServerAlive(st) {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// Clear removes the state file; a missing file is fine.
func Clear(stateFile string) error {
	if err := os.Remove(stateFile); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}
