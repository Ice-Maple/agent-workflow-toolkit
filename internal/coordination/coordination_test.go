package coordination

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"agent-workflow-toolkit/internal/storage"
)

func TestGateEpochAndDisableKeepConvergenceLookup(t *testing.T) {
	root := t.TempDir()
	manager, err := New(filepath.Join(root, "machine"), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	stateRoot := filepath.Join(root, "business-state")
	initial, err := manager.Status("")
	if err != nil {
		t.Fatal(err)
	}
	if initial.FrameworkEnabled || initial.GateEpoch != 0 || initial.EffectiveMode != ModeDirect {
		t.Fatalf("initial status = %#v", initial)
	}
	enabled, err := manager.SetFrameworkEnabled(true)
	if err != nil {
		t.Fatal(err)
	}
	if !enabled.FrameworkEnabled || enabled.GateEpoch != 1 {
		t.Fatalf("enabled state = %#v", enabled)
	}

	locks, err := manager.AcquireRunLocks(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	entry := storage.ActiveRunEntry{RunID: "run-1", Workspace: filepath.Join(root, "workspace"), StateDir: stateRoot, GateEpoch: enabled.GateEpoch, AttemptID: "attempt-1", InstanceID: "instance-1"}
	if err := manager.RegisterRunUnderLock(entry); err != nil {
		_ = locks.Release()
		t.Fatal(err)
	}
	if err := locks.Release(); err != nil {
		t.Fatal(err)
	}

	disabled, err := manager.Disable()
	if err != nil {
		t.Fatal(err)
	}
	if disabled.FrameworkEnabled || disabled.GateEpoch != 2 || len(disabled.Runs) != 1 || disabled.Runs[0].Status != "not_contacted" {
		t.Fatalf("disable result = %#v", disabled)
	}
	if _, _, err := manager.AcquireRunLocksForRun("run-1", enabled.GateEpoch); err == nil {
		t.Fatal("old epoch unexpectedly retained admission after disable")
	}

	convergence, resolved, err := manager.AcquireRunLocksForConvergence("run-1")
	if err != nil {
		t.Fatal(err)
	}
	if resolved.RunID != entry.RunID || resolved.AttemptID != entry.AttemptID {
		_ = convergence.Release()
		t.Fatalf("resolved convergence entry = %#v", resolved)
	}
	if err := convergence.Release(); err != nil {
		t.Fatal(err)
	}
	attemptLocks, attemptEntry, found, err := manager.AcquireRunLocksForAttempt("attempt-1")
	if err != nil || !found {
		t.Fatalf("attempt convergence lookup: locks=%#v entry=%#v found=%t err=%v", attemptLocks, attemptEntry, found, err)
	}
	if err := attemptLocks.Release(); err != nil {
		t.Fatal(err)
	}
}

func TestWorkspaceModeIsMachineLocalAndStatusDegradesFramework(t *testing.T) {
	manager, err := New(filepath.Join(t.TempDir(), "machine"), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	workspace := filepath.Join(t.TempDir(), "workspace")
	if err := os.MkdirAll(workspace, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.SetWorkspaceMode(workspace, ModeFramework); err != nil {
		t.Fatal(err)
	}
	status, err := manager.Status(workspace)
	if err != nil {
		t.Fatal(err)
	}
	if status.WorkspaceMode != ModeFramework || status.EffectiveMode != ModeDirect || !status.Degraded {
		t.Fatalf("disabled framework status = %#v", status)
	}
	if _, err := manager.SetFrameworkEnabled(true); err != nil {
		t.Fatal(err)
	}
	status, err = manager.Status(workspace)
	if err != nil {
		t.Fatal(err)
	}
	if status.EffectiveMode != ModeFramework || status.Degraded {
		t.Fatalf("enabled framework status = %#v", status)
	}
}
