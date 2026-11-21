//go:build windows

package proc

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"syscall"
)

const (
	createNoWindow   = 0x08000000
	stillActive      = 259
	ctrlBreakEvent   = 1
	queryLimitedInfo = 0x1000
	helperArg        = "__gsm-ctrl-break"
)

var (
	kernel32                = syscall.NewLazyDLL("kernel32.dll")
	procAttachConsole       = kernel32.NewProc("AttachConsole")
	procFreeConsole         = kernel32.NewProc("FreeConsole")
	procGenerateConsoleCtrl = kernel32.NewProc("GenerateConsoleCtrlEvent")
)

// detach gives the server its own process group and a hidden console, so
// terminate can send it Ctrl+Break, Windows' closest thing to SIGTERM. A
// Windows child already outlives its parent, so there is no session to escape.
func detach(c *exec.Cmd) {
	c.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP | createNoWindow,
	}
}

// terminate sends Ctrl+Break to the server's process group. Doing that means
// detaching from our own console and attaching to the server's, so it runs
// in a short-lived copy of gsm (see HandleHelper) to leave this process's
// console untouched. sig is ignored: Windows has no signals.
func terminate(pid int, _ syscall.Signal) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	c := exec.Command(exe, helperArg, strconv.Itoa(pid))
	c.SysProcAttr = &syscall.SysProcAttr{CreationFlags: createNoWindow}
	if out, err := c.CombinedOutput(); err != nil {
		return fmt.Errorf("send Ctrl+Break: %v: %s", err, out)
	}
	return nil
}

// HandleHelper must run first in main (and TestMain). When gsm was started
// as the Ctrl+Break helper, it does that job and exits.
func HandleHelper() {
	if len(os.Args) != 3 || os.Args[1] != helperArg {
		return
	}
	pid, err := strconv.Atoi(os.Args[2])
	if err != nil {
		os.Exit(2)
	}
	procFreeConsole.Call()
	if r, _, err := procAttachConsole.Call(uintptr(pid)); r == 0 {
		fmt.Fprintln(os.Stderr, "attach to server console:", err)
		os.Exit(1)
	}
	// This helper is in a different process group, so the event doesn't reach it.
	if r, _, err := procGenerateConsoleCtrl.Call(ctrlBreakEvent, uintptr(pid)); r == 0 {
		fmt.Fprintln(os.Stderr, "GenerateConsoleCtrlEvent:", err)
		os.Exit(1)
	}
	os.Exit(0)
}

// shortcut: taskkill /T walks the tree by parent PID; put the server in a Job Object and terminate the job for a complete kill.
func kill(pid int) error {
	return exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(pid)).Run()
}

// groupAlive has no cheap equivalent on Windows; the start time check covers the parent.
func groupAlive(int) bool { return false }

// StartTime returns the process creation time from GetProcessTimes.
func StartTime(pid int) (string, error) {
	h, err := syscall.OpenProcess(queryLimitedInfo, false, uint32(pid))
	if err != nil {
		return "", err
	}
	defer syscall.CloseHandle(h)
	var code uint32
	if err := syscall.GetExitCodeProcess(h, &code); err != nil {
		return "", err
	}
	if code != stillActive {
		return "", errors.New("process exited")
	}
	var created, exited, kernel, user syscall.Filetime
	if err := syscall.GetProcessTimes(h, &created, &exited, &kernel, &user); err != nil {
		return "", err
	}
	return strconv.FormatInt(created.Nanoseconds(), 10), nil
}
