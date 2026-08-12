// Package coordination owns the machine-local collaboration gate and the
// active-run lookup index. It intentionally contains no Provider execution
// state; task snapshots remain the source of execution truth.
package coordination

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"agent-workflow-toolkit/internal/orchestration"
	"agent-workflow-toolkit/internal/storage"
)

const (
	ModeFramework = "framework"
	ModeDirect    = "direct"
	ModeSingle    = "single"
)

type WorkspaceMode struct {
	Workspace string `json:"workspace"`
	Mode      string `json:"mode"`
}

type Config struct {
	FrameworkEnabled bool            `json:"framework_enabled"`
	Workspaces       []WorkspaceMode `json:"workspaces,omitempty"`
}

type Status struct {
	FrameworkEnabled bool   `json:"framework_enabled"`
	GateEpoch        uint64 `json:"gate_epoch"`
	Workspace        string `json:"workspace,omitempty"`
	WorkspaceMode    string `json:"workspace_mode"`
	EffectiveMode    string `json:"effective_mode"`
	Degraded         bool   `json:"degraded"`
	Reason           string `json:"reason,omitempty"`
}

type Manager struct {
	store       *storage.Store
	lockTimeout time.Duration
}

// ActiveRunIndexLockPath returns the machine-local lock used by the public
// CLI facade before it can resolve a run to its state-dir.
func (m *Manager) ActiveRunIndexLockPath() string {
	if m == nil || m.store == nil {
		return ""
	}
	return m.store.ActiveRunIndexLockPath()
}

// LookupRun resolves a run without holding the state-dir lock. Callers that
// are about to mutate the run must immediately reacquire the complete
// gate -> index -> state-dir lock set and validate the entry again.
func (m *Manager) LookupRun(runID string) (storage.ActiveRunEntry, bool, error) {
	lock, err := storage.AcquireFileLock(m.store.ActiveRunIndexLockPath(), m.lockTimeout)
	if err != nil {
		return storage.ActiveRunEntry{}, false, err
	}
	defer lock.Release()
	return m.GetRunUnderLock(runID)
}

type LockSet struct {
	gate  *storage.RootLock
	index *storage.RootLock
	state *storage.RootLock
}

func (l *LockSet) Release() error {
	if l == nil {
		return nil
	}
	// Release in the reverse order of acquisition. Provider IPC must happen
	// after this method returns; these locks are only admission coordination.
	var err error
	if l.state != nil {
		err = errors.Join(err, l.state.Release())
	}
	if l.index != nil {
		err = errors.Join(err, l.index.Release())
	}
	if l.gate != nil {
		err = errors.Join(err, l.gate.Release())
	}
	return err
}

func New(root string, lockTimeout time.Duration) (*Manager, error) {
	if strings.TrimSpace(root) == "" {
		return nil, errors.New("coordination root is required")
	}
	store, err := storage.New(root)
	if err != nil {
		return nil, err
	}
	if lockTimeout <= 0 {
		lockTimeout = 5 * time.Second
	}
	return &Manager{store: store, lockTimeout: lockTimeout}, nil
}

func NewWithStore(store *storage.Store, lockTimeout time.Duration) (*Manager, error) {
	if store == nil {
		return nil, errors.New("coordination storage is required")
	}
	if lockTimeout <= 0 {
		lockTimeout = 5 * time.Second
	}
	return &Manager{store: store, lockTimeout: lockTimeout}, nil
}

func (m *Manager) Status(workspace string) (Status, error) {
	state, err := m.store.LoadCoordination()
	if err != nil {
		return Status{}, err
	}
	mode, err := m.workspaceMode(workspace)
	if err != nil {
		return Status{}, err
	}
	return statusFor(state, workspace, mode), nil
}

func (m *Manager) SetFrameworkEnabled(enabled bool) (storage.CoordinationState, error) {
	lock, err := storage.AcquireFileLock(m.store.CoordinationLockPath(), m.lockTimeout)
	if err != nil {
		return storage.CoordinationState{}, err
	}
	defer lock.Release()
	state, err := m.store.LoadCoordination()
	if err != nil {
		return storage.CoordinationState{}, err
	}
	if state.FrameworkEnabled != enabled {
		state.FrameworkEnabled = enabled
		state.GateEpoch++
	}
	state.UpdatedAt = time.Now().UTC()
	if err := m.store.SaveCoordination(state); err != nil {
		return storage.CoordinationState{}, err
	}
	return state, nil
}

func (m *Manager) SetWorkspaceMode(workspace, mode string) (WorkspaceMode, error) {
	canonical, err := canonicalWorkspace(workspace)
	if err != nil {
		return WorkspaceMode{}, err
	}
	mode = strings.ToLower(strings.TrimSpace(mode))
	if mode != ModeFramework && mode != ModeDirect && mode != ModeSingle {
		return WorkspaceMode{}, fmt.Errorf("unsupported workspace mode %q", mode)
	}
	lock, err := storage.AcquireFileLock(m.store.CoordinationLockPath(), m.lockTimeout)
	if err != nil {
		return WorkspaceMode{}, err
	}
	defer lock.Release()
	state, err := m.store.LoadCoordination()
	if err != nil {
		return WorkspaceMode{}, err
	}
	if state.WorkspaceModes == nil {
		state.WorkspaceModes = map[string]string{}
	}
	state.WorkspaceModes[canonical] = mode
	state.UpdatedAt = time.Now().UTC()
	if err := m.store.SaveCoordination(state); err != nil {
		return WorkspaceMode{}, err
	}
	return WorkspaceMode{Workspace: canonical, Mode: mode}, nil
}

func (m *Manager) Admission(epoch uint64) error {
	lock, err := storage.AcquireFileLock(m.store.CoordinationLockPath(), m.lockTimeout)
	if err != nil {
		return err
	}
	defer lock.Release()
	state, err := m.store.LoadCoordination()
	if err != nil {
		return err
	}
	if !state.FrameworkEnabled {
		return errors.New("framework is disabled")
	}
	if epoch != state.GateEpoch {
		return fmt.Errorf("framework gate epoch mismatch: run=%d current=%d", epoch, state.GateEpoch)
	}
	return nil
}

// CheckGateUnderLock is the non-locking counterpart used by a caller that
// already holds the documented gate -> index -> state-dir lock set.
func (m *Manager) CheckGateUnderLock(epoch uint64) error {
	state, err := m.store.LoadCoordination()
	if err != nil {
		return err
	}
	if !state.FrameworkEnabled {
		return errors.New("framework is disabled")
	}
	if epoch != state.GateEpoch {
		return fmt.Errorf("framework gate epoch mismatch: run=%d current=%d", epoch, state.GateEpoch)
	}
	return nil
}

// AcquireRunLocks implements the collaboration lock order:
// machine gate -> active-run index -> canonical state-dir. Callers must keep
// the returned locks only around admission/state mutation and release them
// before contacting a Provider or waiting for a host action.
func (m *Manager) AcquireRunLocks(stateDir string) (*LockSet, error) {
	if strings.TrimSpace(stateDir) == "" {
		return nil, errors.New("state-dir is required")
	}
	stateDir = filepath.Clean(stateDir)
	gate, err := storage.AcquireFileLock(m.store.CoordinationLockPath(), m.lockTimeout)
	if err != nil {
		return nil, err
	}
	index, err := storage.AcquireFileLock(m.store.ActiveRunIndexLockPath(), m.lockTimeout)
	if err != nil {
		_ = gate.Release()
		return nil, err
	}
	state, err := storage.AcquireRootLock(stateDir, m.lockTimeout)
	if err != nil {
		_ = index.Release()
		_ = gate.Release()
		return nil, err
	}
	return &LockSet{gate: gate, index: index, state: state}, nil
}

// AcquireRunLocksForRun resolves the active-run index while holding the gate
// and index locks, then acquires the referenced state-dir lock. This is the
// recovery path used when a later one-shot request only carries run ID and
// epoch.
func (m *Manager) AcquireRunLocksForRun(runID string, epoch uint64) (*LockSet, storage.ActiveRunEntry, error) {
	if strings.TrimSpace(runID) == "" {
		return nil, storage.ActiveRunEntry{}, errors.New("run_id is required")
	}
	gate, err := storage.AcquireFileLock(m.store.CoordinationLockPath(), m.lockTimeout)
	if err != nil {
		return nil, storage.ActiveRunEntry{}, err
	}
	index, err := storage.AcquireFileLock(m.store.ActiveRunIndexLockPath(), m.lockTimeout)
	if err != nil {
		_ = gate.Release()
		return nil, storage.ActiveRunEntry{}, err
	}
	state, err := m.store.LoadCoordination()
	if err != nil {
		_ = index.Release()
		_ = gate.Release()
		return nil, storage.ActiveRunEntry{}, err
	}
	if !state.FrameworkEnabled {
		_ = index.Release()
		_ = gate.Release()
		return nil, storage.ActiveRunEntry{}, fmt.Errorf("framework gate admission rejected: enabled=%t", state.FrameworkEnabled)
	}
	active, err := m.store.LoadActiveRunIndex()
	if err != nil {
		_ = index.Release()
		_ = gate.Release()
		return nil, storage.ActiveRunEntry{}, err
	}
	entry, ok := active.Runs[runID]
	if !ok {
		_ = index.Release()
		_ = gate.Release()
		return nil, storage.ActiveRunEntry{}, errors.New("collaboration run is not active")
	}
	if epoch == 0 {
		epoch = entry.GateEpoch
	}
	if entry.GateEpoch != epoch || epoch != state.GateEpoch || entry.ShutdownStatus != "" && entry.ShutdownStatus != "active" {
		_ = index.Release()
		_ = gate.Release()
		return nil, storage.ActiveRunEntry{}, fmt.Errorf("collaboration run is not active for the requested gate epoch: run=%d current=%d", epoch, state.GateEpoch)
	}
	stateLock, err := storage.AcquireRootLock(entry.StateDir, m.lockTimeout)
	if err != nil {
		_ = index.Release()
		_ = gate.Release()
		return nil, storage.ActiveRunEntry{}, err
	}
	return &LockSet{gate: gate, index: index, state: stateLock}, entry, nil
}

// AcquireRunLocksForConvergence is the post-disable counterpart of
// AcquireRunLocksForRun. It still serializes gate/index/state-dir access and
// verifies the durable run identity, but does not require the current machine
// gate to be enabled. It is only for already-issued action/result/terminal
// convergence; callers must not use it to create a new Attempt or action.
func (m *Manager) AcquireRunLocksForConvergence(runID string) (*LockSet, storage.ActiveRunEntry, error) {
	if strings.TrimSpace(runID) == "" {
		return nil, storage.ActiveRunEntry{}, errors.New("run_id is required")
	}
	gate, err := storage.AcquireFileLock(m.store.CoordinationLockPath(), m.lockTimeout)
	if err != nil {
		return nil, storage.ActiveRunEntry{}, err
	}
	index, err := storage.AcquireFileLock(m.store.ActiveRunIndexLockPath(), m.lockTimeout)
	if err != nil {
		_ = gate.Release()
		return nil, storage.ActiveRunEntry{}, err
	}
	active, err := m.store.LoadActiveRunIndex()
	if err != nil {
		_ = index.Release()
		_ = gate.Release()
		return nil, storage.ActiveRunEntry{}, err
	}
	entry, ok := active.Runs[runID]
	if !ok {
		_ = index.Release()
		_ = gate.Release()
		return nil, storage.ActiveRunEntry{}, errors.New("collaboration run is not active")
	}
	stateLock, err := storage.AcquireRootLock(entry.StateDir, m.lockTimeout)
	if err != nil {
		_ = index.Release()
		_ = gate.Release()
		return nil, storage.ActiveRunEntry{}, err
	}
	return &LockSet{gate: gate, index: index, state: stateLock}, entry, nil
}

// AcquireRunLocksForAttempt resolves an active run by its currently indexed
// Attempt. It returns found=false when the Attempt belongs to a legacy
// protocol-v1 task, allowing the caller to retain the old state-dir-only lock.
func (m *Manager) AcquireRunLocksForAttempt(attemptID string) (*LockSet, storage.ActiveRunEntry, bool, error) {
	if strings.TrimSpace(attemptID) == "" {
		return nil, storage.ActiveRunEntry{}, false, errors.New("attempt_id is required")
	}
	gate, err := storage.AcquireFileLock(m.store.CoordinationLockPath(), m.lockTimeout)
	if err != nil {
		return nil, storage.ActiveRunEntry{}, false, err
	}
	index, err := storage.AcquireFileLock(m.store.ActiveRunIndexLockPath(), m.lockTimeout)
	if err != nil {
		_ = gate.Release()
		return nil, storage.ActiveRunEntry{}, false, err
	}
	active, err := m.store.LoadActiveRunIndex()
	if err != nil {
		_ = index.Release()
		_ = gate.Release()
		return nil, storage.ActiveRunEntry{}, false, err
	}
	for _, entry := range active.Runs {
		if entry.AttemptID != attemptID {
			continue
		}
		stateLock, lockErr := storage.AcquireRootLock(entry.StateDir, m.lockTimeout)
		if lockErr != nil {
			_ = index.Release()
			_ = gate.Release()
			return nil, storage.ActiveRunEntry{}, false, lockErr
		}
		return &LockSet{gate: gate, index: index, state: stateLock}, entry, true, nil
	}
	_ = index.Release()
	_ = gate.Release()
	return nil, storage.ActiveRunEntry{}, false, nil
}

// AttemptStatusUnderLock returns the current machine-gate view for an indexed
// Attempt. It deliberately keeps found and active separate: after disable,
// already-issued actions still need a state-dir lock so their real result or
// cancellation can be recorded, but the caller must not issue new work.
// Callers must already hold the gate and active-run index locks.
func (m *Manager) AttemptStatusUnderLock(attemptID string) (storage.ActiveRunEntry, bool, bool, error) {
	if strings.TrimSpace(attemptID) == "" {
		return storage.ActiveRunEntry{}, false, false, errors.New("attempt_id is required")
	}
	state, err := m.store.LoadCoordination()
	if err != nil {
		return storage.ActiveRunEntry{}, false, false, err
	}
	entry, found, err := m.GetAttemptRunUnderLock(attemptID)
	if err != nil || !found {
		return storage.ActiveRunEntry{}, found, false, err
	}
	active := state.FrameworkEnabled && entry.GateEpoch == state.GateEpoch &&
		(entry.ShutdownStatus == "" || entry.ShutdownStatus == "active")
	return entry, true, active, nil
}

// RunStatusUnderLock returns the durable run entry and whether its current
// gate epoch is still allowed to create or advance work. It is intentionally
// usable for already-issued action convergence after a global disable.
// Callers must already hold the gate and active-run index locks.
func (m *Manager) RunStatusUnderLock(runID, stateDir string) (storage.ActiveRunEntry, bool, error) {
	if strings.TrimSpace(runID) == "" {
		return storage.ActiveRunEntry{}, false, errors.New("run_id is required")
	}
	state, err := m.store.LoadCoordination()
	if err != nil {
		return storage.ActiveRunEntry{}, false, err
	}
	entry, err := m.CheckRunIdentityUnderLock(runID, stateDir)
	if err != nil {
		return storage.ActiveRunEntry{}, false, err
	}
	active := state.FrameworkEnabled && entry.GateEpoch == state.GateEpoch &&
		(entry.ShutdownStatus == "" || entry.ShutdownStatus == "active")
	return entry, active, nil
}

// CheckRunUnderLock validates gate epoch and active-run identity. The caller
// must already hold the LockSet returned by AcquireRunLocks.
func (m *Manager) CheckRunUnderLock(runID string, epoch uint64, stateDir string) (storage.ActiveRunEntry, error) {
	if strings.TrimSpace(runID) == "" {
		return storage.ActiveRunEntry{}, errors.New("run_id is required")
	}
	state, err := m.store.LoadCoordination()
	if err != nil {
		return storage.ActiveRunEntry{}, err
	}
	if !state.FrameworkEnabled {
		return storage.ActiveRunEntry{}, errors.New("framework is disabled")
	}
	if epoch == 0 || epoch != state.GateEpoch {
		return storage.ActiveRunEntry{}, fmt.Errorf("framework gate epoch mismatch: run=%d current=%d", epoch, state.GateEpoch)
	}
	index, err := m.store.LoadActiveRunIndex()
	if err != nil {
		return storage.ActiveRunEntry{}, err
	}
	entry, ok := index.Runs[runID]
	if !ok {
		return storage.ActiveRunEntry{}, errors.New("collaboration run is not active")
	}
	if entry.GateEpoch != epoch || filepath.Clean(entry.StateDir) != filepath.Clean(stateDir) {
		return storage.ActiveRunEntry{}, errors.New("collaboration run admission does not match this state-dir or gate epoch")
	}
	if entry.ShutdownStatus != "" && entry.ShutdownStatus != "active" {
		return storage.ActiveRunEntry{}, fmt.Errorf("collaboration run is shutting down: %s", entry.ShutdownStatus)
	}
	return entry, nil
}

// CheckRunIdentityUnderLock validates only the durable run-to-state mapping.
// It intentionally does not require the machine gate to remain enabled, so a
// terminal completion that was already executed before disable can still be
// recorded without granting the run permission to create more work.
func (m *Manager) CheckRunIdentityUnderLock(runID, stateDir string) (storage.ActiveRunEntry, error) {
	if strings.TrimSpace(runID) == "" {
		return storage.ActiveRunEntry{}, errors.New("run_id is required")
	}
	if strings.TrimSpace(stateDir) == "" {
		return storage.ActiveRunEntry{}, errors.New("state_dir is required")
	}
	index, err := m.store.LoadActiveRunIndex()
	if err != nil {
		return storage.ActiveRunEntry{}, err
	}
	entry, ok := index.Runs[runID]
	if !ok {
		return storage.ActiveRunEntry{}, errors.New("collaboration run is not active")
	}
	if filepath.Clean(entry.StateDir) != filepath.Clean(stateDir) {
		return storage.ActiveRunEntry{}, errors.New("collaboration run does not match this state-dir")
	}
	return entry, nil
}

func (m *Manager) UpdateRunUnderLock(entry storage.ActiveRunEntry) error {
	if strings.TrimSpace(entry.RunID) == "" {
		return errors.New("run_id is required")
	}
	index, err := m.store.LoadActiveRunIndex()
	if err != nil {
		return err
	}
	if index.Runs == nil {
		index.Runs = map[string]storage.ActiveRunEntry{}
	}
	entry.UpdatedAt = time.Now().UTC()
	index.Runs[entry.RunID] = entry
	index.UpdatedAt = entry.UpdatedAt
	return m.store.SaveActiveRunIndex(index)
}

// RegisterRunUnderLock records a newly admitted run. The caller must already
// hold the gate and active-run index locks and must use the same gate epoch
// that was checked in the critical section.
func (m *Manager) RegisterRunUnderLock(entry storage.ActiveRunEntry) error {
	if err := m.CheckGateUnderLock(entry.GateEpoch); err != nil {
		return err
	}
	if strings.TrimSpace(entry.RunID) == "" || strings.TrimSpace(entry.StateDir) == "" {
		return errors.New("run_id and state_dir are required")
	}
	index, err := m.store.LoadActiveRunIndex()
	if err != nil {
		return err
	}
	if index.Runs == nil {
		index.Runs = map[string]storage.ActiveRunEntry{}
	}
	entry.UpdatedAt = time.Now().UTC()
	index.Runs[entry.RunID] = entry
	index.UpdatedAt = entry.UpdatedAt
	return m.store.SaveActiveRunIndex(index)
}

func (m *Manager) GetRunUnderLock(runID string) (storage.ActiveRunEntry, bool, error) {
	if strings.TrimSpace(runID) == "" {
		return storage.ActiveRunEntry{}, false, errors.New("run_id is required")
	}
	index, err := m.store.LoadActiveRunIndex()
	if err != nil {
		return storage.ActiveRunEntry{}, false, err
	}
	entry, ok := index.Runs[runID]
	return entry, ok, nil
}

func (m *Manager) GetAttemptRunUnderLock(attemptID string) (storage.ActiveRunEntry, bool, error) {
	if strings.TrimSpace(attemptID) == "" {
		return storage.ActiveRunEntry{}, false, errors.New("attempt_id is required")
	}
	index, err := m.store.LoadActiveRunIndex()
	if err != nil {
		return storage.ActiveRunEntry{}, false, err
	}
	for _, entry := range index.Runs {
		if entry.AttemptID == attemptID {
			return entry, true, nil
		}
	}
	return storage.ActiveRunEntry{}, false, nil
}

type DisableRunStatus struct {
	RunID  string `json:"run_id"`
	Status string `json:"status"`
}

type DisableResult struct {
	FrameworkEnabled bool               `json:"framework_enabled"`
	GateEpoch        uint64             `json:"gate_epoch"`
	Runs             []DisableRunStatus `json:"runs,omitempty"`
}

// Disable closes admission first and only reports contact/cancellation state
// that this local process can prove. With no resident bridge owner, active
// runs are deliberately returned as not_contacted.
func (m *Manager) Disable() (DisableResult, error) {
	gate, err := storage.AcquireFileLock(m.store.CoordinationLockPath(), m.lockTimeout)
	if err != nil {
		return DisableResult{}, err
	}
	defer gate.Release()
	indexLock, err := storage.AcquireFileLock(m.store.ActiveRunIndexLockPath(), m.lockTimeout)
	if err != nil {
		return DisableResult{}, err
	}
	defer indexLock.Release()
	state, err := m.store.LoadCoordination()
	if err != nil {
		return DisableResult{}, err
	}
	if state.FrameworkEnabled {
		state.FrameworkEnabled = false
		state.GateEpoch++
	}
	state.UpdatedAt = time.Now().UTC()
	if err := m.store.SaveCoordination(state); err != nil {
		return DisableResult{}, err
	}
	index, err := m.store.LoadActiveRunIndex()
	if err != nil {
		return DisableResult{}, err
	}
	result := DisableResult{FrameworkEnabled: false, GateEpoch: state.GateEpoch}
	for runID, entry := range index.Runs {
		if entry.ShutdownStatus == "" || entry.ShutdownStatus == "active" {
			entry.ShutdownStatus = "not_contacted"
			entry.UpdatedAt = time.Now().UTC()
			index.Runs[runID] = entry
		}
		result.Runs = append(result.Runs, DisableRunStatus{RunID: runID, Status: entry.ShutdownStatus})
	}
	index.UpdatedAt = time.Now().UTC()
	if err := m.store.SaveActiveRunIndex(index); err != nil {
		return DisableResult{}, err
	}
	return result, nil
}

func (m *Manager) CreateRun(entry storage.ActiveRunEntry) (storage.CoordinationState, error) {
	if strings.TrimSpace(entry.RunID) == "" || strings.TrimSpace(entry.StateDir) == "" {
		return storage.CoordinationState{}, errors.New("run_id and state_dir are required")
	}
	locks, err := m.AcquireRunLocks(entry.StateDir)
	if err != nil {
		return storage.CoordinationState{}, err
	}
	defer locks.Release()
	state, err := m.store.LoadCoordination()
	if err != nil {
		return storage.CoordinationState{}, err
	}
	if entry.GateEpoch == 0 {
		entry.GateEpoch = state.GateEpoch
	}
	if err := m.RegisterRunUnderLock(entry); err != nil {
		return storage.CoordinationState{}, err
	}
	return state, nil
}

func (m *Manager) RemoveRunUnderLock(runID string) error {
	if strings.TrimSpace(runID) == "" {
		return errors.New("run_id is required")
	}
	index, err := m.store.LoadActiveRunIndex()
	if err != nil {
		return err
	}
	delete(index.Runs, runID)
	index.UpdatedAt = time.Now().UTC()
	return m.store.SaveActiveRunIndex(index)
}

func (m *Manager) RemoveRun(runID string) error {
	if strings.TrimSpace(runID) == "" {
		return errors.New("run_id is required")
	}
	lock, err := storage.AcquireFileLock(m.store.ActiveRunIndexLockPath(), m.lockTimeout)
	if err != nil {
		return err
	}
	defer lock.Release()
	index, err := m.store.LoadActiveRunIndex()
	if err != nil {
		return err
	}
	delete(index.Runs, runID)
	index.UpdatedAt = time.Now().UTC()
	return m.store.SaveActiveRunIndex(index)
}

func (m *Manager) workspaceMode(workspace string) (string, error) {
	if strings.TrimSpace(workspace) == "" {
		return ModeDirect, nil
	}
	canonical, err := canonicalWorkspace(workspace)
	if err != nil {
		return "", err
	}
	state, err := m.store.LoadCoordination()
	if err != nil {
		return "", err
	}
	if mode := state.WorkspaceModes[canonical]; mode != "" {
		return mode, nil
	}
	return ModeDirect, nil
}

func canonicalWorkspace(path string) (string, error) {
	return orchestration.CanonicalDirectory(path)
}

func statusFor(state storage.CoordinationState, workspace, mode string) Status {
	effective := mode
	degraded := false
	reason := ""
	if mode == ModeFramework && !state.FrameworkEnabled {
		effective, degraded, reason = ModeDirect, true, "machine gate is disabled"
	}
	return Status{FrameworkEnabled: state.FrameworkEnabled, GateEpoch: state.GateEpoch, Workspace: workspace, WorkspaceMode: mode, EffectiveMode: effective, Degraded: degraded, Reason: reason}
}
