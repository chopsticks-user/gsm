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
func kill(pid int) error                          { return ignoreGone(syscall.Kill(-pid, syscall.SIGKILL)) }

// groupAlive reports whether any process in the group still exists.
func groupAlive(pid int) bool { return syscall.Kill(-pid, 0) == nil }

func ignoreGone(err error) error {
	if errors.Is(err, syscall.ESRCH) {
		return nil
	}
	return err
}

// HandleHelper is a no-op on Unix; see the Windows version.
func HandleHelper()        {}
func afterStart(int) error { return nil } // a new session (detach) already groups the server's processes
