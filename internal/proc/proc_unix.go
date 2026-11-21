//go:build unix

package proc

import (
	"errors"
	"os/exec"
	"syscall"
)

// detach starts the server in a new session, so closing the terminal doesn't
// kill it. The PID then doubles as the process group ID.
func detach(c *exec.Cmd) { c.SysProcAttr = &syscall.SysProcAttr{Setsid: true} }

// Signal the whole group (negative PID): PalServer.sh and steam-run start the
// real binary as a child, and signalling only the parent would leave it running.
func terminate(pid int, sig syscall.Signal) error { return ignoreGone(syscall.Kill(-pid, sig)) }
func kill(st State) error                         { return ignoreGone(syscall.Kill(-st.PID, syscall.SIGKILL)) }

// groupAlive reports whether any process in the group still exists.
func groupAlive(st State) bool {
	// A live process with this PID but another start time means the PID was
	// reused, which the OS never does while the old group lives.
	if t, err := StartTime(st.PID); err == nil && t != st.Start {
		return false
	}
	return syscall.Kill(-st.PID, 0) == nil
}

func ignoreGone(err error) error {
	if errors.Is(err, syscall.ESRCH) {
		return nil
	}
	return err
}

// HandleHelper is a no-op on Unix; see the Windows version.
func HandleHelper()          {}
func afterStart(State) error { return nil } // a new session (detach) already groups the server's processes
