package main

import (
	"errors"
	"os"
	"syscall"
	"unsafe"
)

type state struct{}

func before() state { return state{} }

var getProcessMemoryInfo = syscall.NewLazyDLL("kernel32.dll").NewProc("K32GetProcessMemoryInfo")

// processMemoryCountersEx2 is PROCESS_MEMORY_COUNTERS_EX2, of Windows 10
// 1809 and later.
type processMemoryCountersEx2 struct {
	cb                         uint32
	PageFaultCount             uint32
	PeakWorkingSetSize         uintptr
	WorkingSetSize             uintptr
	QuotaPeakPagedPoolUsage    uintptr
	QuotaPagedPoolUsage        uintptr
	QuotaPeakNonPagedPoolUsage uintptr
	QuotaNonPagedPoolUsage     uintptr
	PagefileUsage              uintptr
	PeakPagefileUsage          uintptr
	PrivateUsage               uintptr
	PrivateWorkingSetSize      uintptr
	SharedCommitUsage          uint64
}

const (
	processTerminate               = 0x0001
	processQueryLimitedInformation = 0x1000
)

// usage counts the app's descendants: WebView2's browser process, and the
// renderer, GPU and utility processes it runs.
func usage(pid int, _ state) (app, total uint64, procs []int, err error) {
	procs = append([]int{pid}, descendants(pid)...)
	for i, p := range procs {
		ws, err := privateWorkingSet(p)
		if err != nil {
			if i == 0 {
				return 0, 0, nil, err
			}
			continue
		}
		if i == 0 {
			app = ws
		}
		total += ws
	}
	return app, total, procs, nil
}

// privateWorkingSet returns the pages of pid in memory that no other
// process shares.
func privateWorkingSet(pid int) (uint64, error) {
	h, err := syscall.OpenProcess(processQueryLimitedInformation, false, uint32(pid))
	if err != nil {
		return 0, err
	}
	defer syscall.CloseHandle(h)
	var c processMemoryCountersEx2
	c.cb = uint32(unsafe.Sizeof(c))
	if ok, _, err := getProcessMemoryInfo.Call(uintptr(h), uintptr(unsafe.Pointer(&c)), uintptr(c.cb)); ok == 0 {
		return 0, err
	}
	return uint64(c.PrivateWorkingSetSize), nil
}

// descendants returns the processes pid started, and theirs.
func descendants(pid int) []int {
	snap, err := syscall.CreateToolhelp32Snapshot(syscall.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil
	}
	defer syscall.CloseHandle(snap)
	children := map[int][]int{}
	var e syscall.ProcessEntry32
	e.Size = uint32(unsafe.Sizeof(e))
	for err := syscall.Process32First(snap, &e); err == nil; err = syscall.Process32Next(snap, &e) {
		children[int(e.ParentProcessID)] = append(children[int(e.ParentProcessID)], int(e.ProcessID))
	}
	// A parent's ID may have been reused by a process it did not start.
	seen := map[int]bool{pid: true}
	var out []int
	for queue := children[pid]; len(queue) > 0; queue = queue[1:] {
		if seen[queue[0]] {
			continue
		}
		seen[queue[0]] = true
		out = append(out, queue[0])
		queue = append(queue, children[queue[0]]...)
	}
	return out
}

// stop ends the app and the processes it ran, as Windows has no signal
// to ask it to quit, and returns once all have exited, with those they
// started since usage looked. TerminateProcess only starts ending a
// process, and the next run's WebView2 could otherwise meet this run's
// browser still exiting, which holds the user data folder they share.
func stop(_ *os.Process, procs []int) error {
	// A handle on a process keeps its ID from going to another process,
	// which descendants would take for one of the app's.
	held := map[int]syscall.Handle{}
	defer func() {
		for _, h := range held {
			syscall.CloseHandle(h)
		}
	}()
	app := procs[0]
	for range 10 {
		var ending []syscall.Handle
		for _, pid := range procs {
			if _, ok := held[pid]; ok {
				continue
			}
			h, err := syscall.OpenProcess(processTerminate|syscall.SYNCHRONIZE, false, uint32(pid))
			if err != nil {
				continue // gone already
			}
			held[pid] = h
			ending = append(ending, h)
		}
		if len(ending) == 0 {
			return nil
		}
		for _, h := range ending {
			syscall.TerminateProcess(h, 1)
		}
		for _, h := range ending {
			if ev, _ := syscall.WaitForSingleObject(h, 10_000); ev != syscall.WAIT_OBJECT_0 {
				return errors.New("a process of the app still runs 10s after it was ended")
			}
		}
		if _, ok := held[app]; !ok {
			return nil // its ID may be another process's now
		}
		procs = descendants(app)
	}
	return errors.New("the app's processes kept starting others as they were ended")
}
