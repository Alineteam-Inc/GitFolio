package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// The runner's display language is English; a Korean or Japanese Windows gives ko or ja.
func TestDeviceLanguagesWindows(t *testing.T) {
	if got := deviceLanguages(); len(got) != 1 || (got[0] != "en" && got[0] != "ko" && got[0] != "ja") {
		t.Errorf("deviceLanguages() = %v", got)
	}
}

// TestTaskSchedulerWindows registers the daily sync with the real Task Scheduler under a test name,
// reads it back and removes it.
func TestTaskSchedulerWindows(t *testing.T) {
	old := winTaskName
	winTaskName = fmt.Sprintf(`GitFolioTest\Sync%d`, os.Getpid())
	defer func() { winTaskName = old }()
	dir := t.TempDir()
	logTo, err := schedule(dir, `C:\Program Files\GitFolio\gitfolio.exe`, 9, 5)
	if err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command("schtasks", "/Query", "/TN", winTaskName, "/XML").Output()
	if err != nil {
		t.Fatalf("task not registered: %v", err)
	}
	x := string(out)
	for _, want := range []string{"T09:05:00", "<StartWhenAvailable>true</StartWhenAvailable>", "cmd.exe", `C:\Program Files\GitFolio\gitfolio.exe`, logTo} {
		if !strings.Contains(x, want) && !strings.Contains(x, strings.ReplaceAll(want, `"`, "&quot;")) {
			t.Errorf("registered task lacks %q:\n%s", want, x)
		}
	}
	if err := unschedule(dir); err != nil {
		t.Fatal(err)
	}
	if exec.Command("schtasks", "/Query", "/TN", winTaskName).Run() == nil {
		t.Error("task still registered after unschedule")
	}
	if err := unschedule(dir); err != nil { // nothing registered: not an error
		t.Error(err)
	}
}

// A lock file that another program (a virus scanner) still has open when its run removes it is deleted
// only once that program closes it, and until then Windows refuses to create one in its place: withLock
// waits that out instead of failing.
func TestLockWhileDeleted(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "lock")
	if err := os.WriteFile(p, []byte("1"), 0o600); err != nil {
		t.Fatal(err)
	}
	name, _ := syscall.UTF16PtrFromString(p)
	h, err := syscall.CreateFile(name, syscall.GENERIC_READ,
		syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE|syscall.FILE_SHARE_DELETE, nil, syscall.OPEN_EXISTING, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(p); err != nil {
		t.Fatal(err)
	}
	// Which of the two this Windows does shows in CI's list as a skipped subtest.
	if f, err := os.OpenFile(p, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600); err == nil {
		f.Close()
		os.Remove(p)
		t.Run("removed at once", func(t *testing.T) { t.Skip() })
	} else if errors.Is(err, os.ErrPermission) {
		t.Run("refused while being deleted", func(t *testing.T) { t.Skip(err) })
	} else {
		t.Errorf("creating it while it is being deleted: %v", err)
	}
	go func() { time.Sleep(200 * time.Millisecond); syscall.CloseHandle(h) }()
	if err := withLock(dir, func() error { return nil }); err != nil {
		t.Errorf("withLock while the lock is being deleted: %v", err)
	}
}
