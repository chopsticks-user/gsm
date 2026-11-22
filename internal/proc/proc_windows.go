//go:build windows

package proc

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"syscall"
	"time"
	"unsafe"
)

const (
	createNoWindow   = 0x08000000
	stillActive      = 259
	ctrlBreakEvent   = 1
	queryLimitedInfo = 0x1000
	helperArg        = "__gsm-ctrl-break"
	holderArg        = "__gsm-job-holder"

	createSuspended      = 0x4
	processTerminate     = 0x0001
	processSetQuota      = 0x0100
	processSuspendResume = 0x0800
	jobQuery             = 0x0004
	jobTerminate         = 0x0008
	jobBasicAccounting   = 1 // JobObjectBasicAccountingInformation
	jobExtendedLimits    = 9 // JobObjectExtendedLimitInformation
	jobBreakawayOK       = 0x800
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
	procSetJobInfo          = kernel32.NewProc("SetInformationJobObject")
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
// as the Ctrl+Break helper, it does that job and exits; as a job holder (see
// joinJob), it keeps the job until the job empties, then exits.
func HandleHelper() {
	if len(os.Args) == 3 && os.Args[1] == holderArg {
		holdJob(os.Args[2])
	}
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
// finds the job by a name made of the server's PID and start time, which
// can't collide after PID reuse. Names are per login session (Local\): from
// another session, or for a server started without a job, gsm falls back to
// taskkill.
func jobName(st State) *uint16 {
	p, _ := syscall.UTF16PtrFromString(`Local\gsm-server-` + strconv.Itoa(st.PID) + "-" + st.Start)
	return p
}

// afterStart puts the suspended server into its job, so no child can start
// outside it, then resumes it. The job is best effort; resuming is not.
func afterStart(st State) error {
	ph, err := syscall.OpenProcess(processTerminate|processSetQuota|processSuspendResume, false, uint32(st.PID))
	if err != nil {
		return err
	}
	defer syscall.CloseHandle(ph)
	joinJob(st, ph)
	if r, _, _ := procNtResumeProcess.Call(uintptr(ph)); r != 0 {
		return fmt.Errorf("resume server: NTSTATUS %#x", r)
	}
	return nil
}

func joinJob(st State, ph syscall.Handle) {
	sa := syscall.SecurityAttributes{InheritHandle: 1} // so the holder can inherit it
	sa.Length = uint32(unsafe.Sizeof(sa))
	job, _, _ := procCreateJobObject.Call(uintptr(unsafe.Pointer(&sa)), uintptr(unsafe.Pointer(jobName(st))))
	if job == 0 {
		return
	}
	defer syscall.CloseHandle(syscall.Handle(job))
	allowBreakaway(syscall.Handle(job))
	if r, _, _ := procAssignToJob.Call(job, uintptr(ph)); r == 0 {
		return
	}
	// A name lasts only while a handle is open, and the launched process may
	// exit before its children, so a holder outside the job keeps one open
	// until the job's last process exits (see HandleHelper).
	exe, err := os.Executable()
	if err != nil {
		return
	}
	h := exec.Command(exe, holderArg, strconv.FormatUint(uint64(job), 10))
	h.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags:              syscall.CREATE_NEW_PROCESS_GROUP | createNoWindow,
		AdditionalInheritedHandles: []syscall.Handle{syscall.Handle(job)},
	}
	if h.Start() == nil {
		h.Process.Release()
	}
}

// holdJob is the holder: it keeps the inherited job handle, and so the job's
// name, until the job has no live process.
func holdJob(arg string) {
	h, err := strconv.ParseUint(arg, 10, 64)
	if err != nil {
		os.Exit(2)
	}
	for jobActive(syscall.Handle(h)) {
		time.Sleep(2 * time.Second)
	}
	os.Exit(0)
}

func openJob(st State, access uint32) syscall.Handle {
	h, _, _ := procOpenJobObject.Call(uintptr(access), 0, uintptr(unsafe.Pointer(jobName(st))))
	return syscall.Handle(h)
}

func jobActive(job syscall.Handle) bool {
	var info struct {
		TotalUserTime, TotalKernelTime, PeriodUserTime, PeriodKernelTime           int64
		TotalPageFaults, TotalProcesses, ActiveProcesses, TotalTerminatedProcesses uint32
	}
	r, _, _ := procQueryJob.Call(uintptr(job), jobBasicAccounting, uintptr(unsafe.Pointer(&info)), unsafe.Sizeof(info), 0)
	return r != 0 && info.ActiveProcesses > 0
}

// kill ends the server two ways, since neither covers everything: taskkill
// /T walks the tree by parent PID (reaching children that broke away from the
// job, while their parent lives), then terminating the job reaches orphans.
// Without a findable job, taskkill alone is all there is.
func kill(st State) error {
	err := exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(st.PID)).Run()
	if job := openJob(st, jobTerminate); job != 0 {
		defer syscall.CloseHandle(job)
		if r, _, _ := procTerminateJob.Call(uintptr(job), 1); r != 0 {
			return nil
		}
	}
	return err
}

// groupAlive reports whether the server's job still has a live process.
// Without a findable job it reports false.
func groupAlive(st State) bool {
	job := openJob(st, jobQuery)
	if job == 0 {
		return false
	}
	defer syscall.CloseHandle(job)
	return jobActive(job)
}

// allowBreakaway lets a child started with CREATE_BREAKAWAY_FROM_JOB leave
// the job instead of failing to start: Palworld's launcher starts its server
// that way, and in a job without this it hangs without one. kill's taskkill
// /T still reaches such a child while its parent lives.
func allowBreakaway(job syscall.Handle) {
	var info struct {
		PerProcessUserTimeLimit, PerJobUserTimeLimit int64
		LimitFlags                                   uint32
		MinimumWorkingSetSize, MaximumWorkingSetSize uintptr
		ActiveProcessLimit                           uint32
		Affinity                                     uintptr
		PriorityClass, SchedulingClass               uint32
		IoCounters                                   [6]uint64
		ProcessMemoryLimit, JobMemoryLimit           uintptr
		PeakProcessMemoryUsed, PeakJobMemoryUsed     uintptr
	}
	info.LimitFlags = jobBreakawayOK
	procSetJobInfo.Call(uintptr(job), jobExtendedLimits, uintptr(unsafe.Pointer(&info)), unsafe.Sizeof(info))
}
