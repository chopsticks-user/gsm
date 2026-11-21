//go:build windows

package proc

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"syscall"
	"unsafe"
)

const (
	createNoWindow   = 0x08000000
	stillActive      = 259
	ctrlBreakEvent   = 1
	queryLimitedInfo = 0x1000
	helperArg        = "__gsm-ctrl-break"

	createSuspended      = 0x4
	processTerminate     = 0x0001
	processDupHandle     = 0x0040
	processSetQuota      = 0x0100
	processSuspendResume = 0x0800
	jobAllAccess         = 0x1F001F
	jobQuery             = 0x0004
	jobTerminate         = 0x0008
	jobBasicAccounting   = 1 // JobObjectBasicAccountingInformation
	errorAlreadyExists   = 183
	duplicateSameAccess  = 2
)

var (
	kernel32                = syscall.NewLazyDLL("kernel32.dll")
	procAttachConsole       = kernel32.NewProc("AttachConsole")
	procFreeConsole         = kernel32.NewProc("FreeConsole")
	procGenerateConsoleCtrl = kernel32.NewProc("GenerateConsoleCtrlEvent")
	procCreateJobObject     = kernel32.NewProc("CreateJobObjectW")
	procOpenJobObject       = kernel32.NewProc("OpenJobObjectW")
	procAssignToJob         = kernel32.NewProc("AssignProcessToJobObject")
	procTerminateJob        = kernel32.NewProc("TerminateJobObject")
	procQueryJob            = kernel32.NewProc("QueryInformationJobObject")
	// Undocumented but long stable: exec.Cmd doesn't expose the main thread to ResumeThread.
	procNtResumeProcess = syscall.NewLazyDLL("ntdll.dll").NewProc("NtResumeProcess")
)

// detach gives the server its own process group and a hidden console, so
// terminate can send it Ctrl+Break, Windows' closest thing to SIGTERM. A
// Windows child already outlives its parent, so there is no session to escape.
func detach(c *exec.Cmd) {
	c.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP | createNoWindow | createSuspended, // resumed by afterStart
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

// A server's Job Object holds every process it starts, orphans included, so
// kill and groupAlive reach all of them. gsm has no daemon, so a later gsm
// finds the job by a name derived from the server's PID. Names are per login
// session (Local\): from another session, or for a server started without a
// job, gsm falls back to taskkill.
func jobName(pid int) *uint16 {
	p, _ := syscall.UTF16PtrFromString(`Local\gsm-server-` + strconv.Itoa(pid))
	return p
}

// afterStart puts the suspended server into its job, so no child can start
// outside it, then resumes it. The job is best effort; resuming is not.
func afterStart(pid int) error {
	ph, err := syscall.OpenProcess(processTerminate|processDupHandle|processSetQuota|processSuspendResume, false, uint32(pid))
	if err != nil {
		return err
	}
	defer syscall.CloseHandle(ph)
	joinJob(pid, ph)
	if r, _, _ := procNtResumeProcess.Call(uintptr(ph)); r != 0 {
		return fmt.Errorf("resume server: NTSTATUS %#x", r)
	}
	return nil
}

func joinJob(pid int, ph syscall.Handle) {
	job, _, err := procCreateJobObject.Call(0, uintptr(unsafe.Pointer(jobName(pid))))
	if job == 0 {
		return
	}
	defer syscall.CloseHandle(syscall.Handle(job))
	if err == syscall.Errno(errorAlreadyExists) {
		return // left by this PID's previous owner; never mix two servers in one job
	}
	if r, _, _ := procAssignToJob.Call(job, uintptr(ph)); r == 0 {
		return
	}
	// A name lasts only while a handle is open, so the server holds one
	// (inheritable, so children that inherit handles keep the name alive too).
	var dup syscall.Handle
	self, _ := syscall.GetCurrentProcess()
	syscall.DuplicateHandle(self, syscall.Handle(job), ph, &dup, 0, true, duplicateSameAccess)
}

func openJob(pid int, access uint32) syscall.Handle {
	h, _, _ := procOpenJobObject.Call(uintptr(access), 0, uintptr(unsafe.Pointer(jobName(pid))))
	return syscall.Handle(h)
}

// kill terminates the server's job, or falls back to taskkill /T, which
// walks the tree by parent PID and so misses orphaned children.
func kill(pid int) error {
	if job := openJob(pid, jobTerminate); job != 0 {
		defer syscall.CloseHandle(job)
		if r, _, _ := procTerminateJob.Call(uintptr(job), 1); r != 0 {
			return nil
		}
	}
	return exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(pid)).Run()
}

// groupAlive reports whether the server's job still has a live process.
// Without a findable job it reports false; the start time check covers the parent.
func groupAlive(pid int) bool {
	job := openJob(pid, jobQuery)
	if job == 0 {
		return false
	}
	defer syscall.CloseHandle(job)
	var info struct {
		TotalUserTime, TotalKernelTime, PeriodUserTime, PeriodKernelTime           int64
		TotalPageFaults, TotalProcesses, ActiveProcesses, TotalTerminatedProcesses uint32
	}
	r, _, _ := procQueryJob.Call(uintptr(job), jobBasicAccounting, uintptr(unsafe.Pointer(&info)), unsafe.Sizeof(info), 0)
	return r != 0 && info.ActiveProcesses > 0
}
