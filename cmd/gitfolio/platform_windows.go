//go:build windows

package main

import (
	"os"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

// deviceLanguages returns the Windows display language (Settings › Time & language), as ko, ja or en.
func deviceLanguages() []string {
	id, _, _ := syscall.NewLazyDLL("kernel32.dll").NewProc("GetUserDefaultUILanguage").Call()
	switch id & 0x3ff { // primary language ID
	case 0x12:
		return []string{"ko"}
	case 0x11:
		return []string{"ja"}
	case 0x09:
		return []string{"en"}
	}
	return nil
}

// pushPID finds the git push process the pre-push hook runs in. Git for Windows runs hooks under its own
// sh, whose $PPID is not a Windows process ID, so this walks up from this process to the nearest git.exe.
// It has to run while the hook is still running; 0 means not found.
func pushPID(string) int {
	snap, err := syscall.CreateToolhelp32Snapshot(syscall.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return 0
	}
	defer syscall.CloseHandle(snap)
	type proc struct {
		parent uint32
		exe    string
	}
	procs := map[uint32]proc{}
	var e syscall.ProcessEntry32
	e.Size = uint32(unsafe.Sizeof(e))
	for err := syscall.Process32First(snap, &e); err == nil; err = syscall.Process32Next(snap, &e) {
		procs[e.ProcessID] = proc{e.ParentProcessID, syscall.UTF16ToString(e.ExeFile[:])}
	}
	pid := procs[uint32(os.Getpid())].parent
	for range 16 {
		p, ok := procs[pid]
		if !ok {
			return 0
		}
		if strings.EqualFold(p.exe, "git.exe") {
			return int(pid)
		}
		pid = p.parent
	}
	return 0
}

// waitExit waits until process pid (the git push that ran the hook) is gone, at most max.
func waitExit(pid int, max time.Duration) {
	p, err := os.FindProcess(pid) // on Windows this opens the process, so Wait works for any process
	if pid <= 0 || err != nil {
		return
	}
	done := make(chan struct{})
	go func() { p.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(max):
	}
}

// lockFile takes the system's lock on file p (LockFileEx), errLocked while another open file holds it.
// Closing the file, or the process ending, lets go of it. Opened so that others may delete it, as
// uninstall --purge-local does with the whole data folder.
func lockFile(p string) (*os.File, error) {
	name, err := syscall.UTF16PtrFromString(p)
	if err != nil {
		return nil, err
	}
	h, err := syscall.CreateFile(name, syscall.GENERIC_READ|syscall.GENERIC_WRITE,
		syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE|syscall.FILE_SHARE_DELETE, nil, syscall.OPEN_ALWAYS, syscall.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		return nil, err
	}
	const exclusive, failImmediately, lockViolation = 2, 1, syscall.Errno(33)
	var ol syscall.Overlapped
	r, _, err := syscall.NewLazyDLL("kernel32.dll").NewProc("LockFileEx").Call(uintptr(h), exclusive|failImmediately, 0, 1, 0, uintptr(unsafe.Pointer(&ol)))
	if r == 0 {
		syscall.CloseHandle(h)
		if err == lockViolation {
			return nil, errLocked
		}
		return nil, err
	}
	return os.NewFile(uintptr(h), p), nil
}

// enableColor turns on ANSI color handling in the Windows console (Windows 10 and later); false where
// the console cannot, so plain text is printed instead.
func enableColor() bool {
	h, err := syscall.GetStdHandle(syscall.STD_OUTPUT_HANDLE)
	if err != nil {
		return false
	}
	var mode uint32
	if syscall.GetConsoleMode(h, &mode) != nil {
		return false
	}
	const virtualTerminalProcessing = 0x0004
	r, _, _ := syscall.NewLazyDLL("kernel32.dll").NewProc("SetConsoleMode").Call(uintptr(h), uintptr(mode|virtualTerminalProcessing))
	return r != 0
}

// openTTY opens the console, also when stdout and stderr go elsewhere (a git hook).
func openTTY() (*os.File, error) { return os.OpenFile("CONOUT$", os.O_WRONLY, 0) }
