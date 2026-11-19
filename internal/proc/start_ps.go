//go:build unix && !linux

package proc

import (
	"errors"
	"os/exec"
	"strconv"
	"strings"
)

// shortcut: shells out to ps (one-second resolution); use sysctl kern.proc.pid on macOS if PID reuse within a second ever matters.
func StartTime(pid int) (string, error) {
	out, err := exec.Command("ps", "-o", "lstart=", "-p", strconv.Itoa(pid)).Output()
	t := strings.TrimSpace(string(out))
	if err != nil || t == "" {
		return "", errors.New("process not found")
	}
	return t, nil
}
