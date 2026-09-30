//go:build !windows

package main

import (
	"os"
	"strconv"
	"syscall"
	"time"
)

// pushPID is the git push process the pre-push hook runs in; the hook script passes it as $PPID.
func pushPID(arg string) int {
	pid, _ := strconv.Atoi(arg)
	return pid
}

// waitExit waits until process pid (the git push that ran the hook) is gone, at most max.
func waitExit(pid int, max time.Duration) {
	for deadline := time.Now().Add(max); time.Now().Before(deadline); time.Sleep(200 * time.Millisecond) {
		p, err := os.FindProcess(pid)
		if err != nil || p.Signal(syscall.Signal(0)) != nil {
			return
		}
	}
}
