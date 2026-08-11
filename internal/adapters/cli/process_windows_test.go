//go:build windows

package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestRunnerKillsRealProcessTree(t *testing.T) {
	childPIDFile := filepath.Join(t.TempDir(), "child.pid")
	handle, err := NewRunner().Start(t.Context(), Command{
		Executable: os.Args[0],
		Dir:        t.TempDir(),
		Environment: []string{
			"CLI_PROCESS_HELPER=1",
			"CLI_PROCESS_HELPER_MODE=tree-parent",
			"CLI_PROCESS_CHILD_PID_FILE=" + childPIDFile,
		},
	})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	t.Cleanup(func() {
		_ = exec.Command("taskkill", "/PID", strconv.Itoa(handle.PID()), "/T", "/F").Run()
	})

	childPID := waitForWindowsChildPID(t, childPIDFile)
	defer func() {
		if windowsProcessExists(childPID) {
			_ = exec.Command("taskkill", "/PID", strconv.Itoa(childPID), "/T", "/F").Run()
		}
	}()

	cancel := handle.Cancel()
	result := handle.Wait()
	if !cancel.Requested || !cancel.Confirmed || result.ExitCode == 0 || !result.Cancelled || !result.KillConfirmed {
		t.Fatalf("tree cancellation evidence = cancel=%#v result=%#v", cancel, result)
	}
	if !waitForWindowsProcessExit(childPID, 3*time.Second) {
		t.Fatalf("child process %d survived process-tree cancellation", childPID)
	}
}

func waitForWindowsChildPID(t *testing.T, path string) int {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		raw, err := os.ReadFile(path)
		if err == nil {
			pid, parseErr := strconv.Atoi(strings.TrimSpace(string(raw)))
			if parseErr == nil && pid > 0 && windowsProcessExists(pid) {
				return pid
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("child pid file %q was not published", path)
	return 0
}

func waitForWindowsProcessExit(pid int, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if !windowsProcessExists(pid) {
			return true
		}
		time.Sleep(25 * time.Millisecond)
	}
	return !windowsProcessExists(pid)
}

func windowsProcessExists(pid int) bool {
	if pid < 1 {
		return false
	}
	output, err := exec.Command("tasklist", "/FI", fmt.Sprintf("PID eq %d", pid), "/FO", "CSV", "/NH").CombinedOutput()
	if err != nil {
		return false
	}
	return strings.Contains(string(output), fmt.Sprintf("\"%d\"", pid))
}
