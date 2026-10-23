package tunnel

import (
	"os/exec"
	"syscall"
	"unsafe"
)

func setProcAttr(*exec.Cmd) {}

var (
	kernel32                    = syscall.NewLazyDLL("kernel32.dll")
	procCreateJobObjectW        = kernel32.NewProc("CreateJobObjectW")
	procSetInformationJobObject = kernel32.NewProc("SetInformationJobObject")
	procAssignProcessToJob      = kernel32.NewProc("AssignProcessToJobObject")
)

type jobBasicLimits struct {
	PerProcessUserTimeLimit int64
	PerJobUserTimeLimit     int64
	LimitFlags              uint32
	MinimumWorkingSetSize   uintptr
	MaximumWorkingSetSize   uintptr
	ActiveProcessLimit      uint32
	Affinity                uintptr
	PriorityClass           uint32
	SchedulingClass         uint32
}

type jobExtendedLimits struct {
	Basic                 jobBasicLimits
	IoInfo                [6]uint64
	ProcessMemoryLimit    uintptr
	JobMemoryLimit        uintptr
	PeakProcessMemoryUsed uintptr
	PeakJobMemoryUsed     uintptr
}

const (
	jobObjectExtendedLimitInformation = 9
	jobObjectLimitKillOnJobClose      = 0x2000
	processSetQuota                   = 0x0100
	processTerminate                  = 0x0001
)

// afterStart puts cloudflared in a kill-on-close job object whose handle
// this process holds until it exits, so Windows kills cloudflared even if
// this process is killed without a chance to call Stop. Best effort.
func afterStart(cmd *exec.Cmd) {
	job, _, _ := procCreateJobObjectW.Call(0, 0)
	if job == 0 {
		return
	}
	info := jobExtendedLimits{Basic: jobBasicLimits{LimitFlags: jobObjectLimitKillOnJobClose}}
	if ok, _, _ := procSetInformationJobObject.Call(job, jobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)), unsafe.Sizeof(info)); ok == 0 {
		syscall.CloseHandle(syscall.Handle(job))
		return
	}
	h, err := syscall.OpenProcess(processSetQuota|processTerminate, false, uint32(cmd.Process.Pid))
	if err != nil {
		syscall.CloseHandle(syscall.Handle(job))
		return
	}
	defer syscall.CloseHandle(h)
	procAssignProcessToJob.Call(job, uintptr(h))
	// The job handle is deliberately leaked: closing it kills cloudflared.
}
