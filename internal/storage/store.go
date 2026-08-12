// Package storage provides the small, file-backed persistence layer used by
// the Wave 1 core. Task snapshots are replaceable; events and request
// receipts are append-only records used to make replay and idempotency
// observable without introducing SQLite or a daemon.
package storage

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"agent-workflow-toolkit/internal/orchestration"
)

const (
	CurrentSchemaVersion   = 1
	CurrentProtocolVersion = 1

	RequestPending        = "pending"
	RequestCompleted      = "completed"
	RequestFailed         = "failed"
	RequestOutcomeUnknown = "outcome_unknown"
)

type State struct {
	SchemaVersion      int                                                    `json:"schema_version"`
	ProtocolVersion    int                                                    `json:"protocol_version"`
	WriterVersion      string                                                 `json:"writer_version"`
	Task               orchestration.Task                                     `json:"task"`
	Revisions          map[string]orchestration.TaskRevision                  `json:"revisions"`
	Nodes              map[string]orchestration.WorkflowNode                  `json:"nodes"`
	Attempts           map[string]orchestration.Attempt                       `json:"attempts"`
	Actions            map[string]orchestration.HostAction                    `json:"actions"`
	Completions        map[string]orchestration.CompletionDecision            `json:"completions"`
	HumanDecisions     []orchestration.HumanDecision                          `json:"human_decisions"`
	GoalAuthorizations map[string]orchestration.GoalAuthorization             `json:"goal_authorizations"`
	Events             []orchestration.Event                                  `json:"events"`
	CollaborationRuns  map[string]orchestration.CollaborationRun              `json:"collaboration_runs,omitempty"`
	StepRuns           map[string]orchestration.StepRun                       `json:"step_runs,omitempty"`
	InstanceRuns       map[string]orchestration.InstanceRun                   `json:"instance_runs,omitempty"`
	TechnicalReleases  map[string]orchestration.CollaborationTechnicalRelease `json:"technical_releases,omitempty"`
}

// CoordinationState is machine-local collaboration control state. It stays
// separate from task snapshots so global disable can be linearized without
// rewriting domain execution evidence.
type CoordinationState struct {
	SchemaVersion    int               `json:"schema_version"`
	FrameworkEnabled bool              `json:"framework_enabled"`
	GateEpoch        uint64            `json:"gate_epoch"`
	WorkspaceModes   map[string]string `json:"workspace_modes,omitempty"`
	UpdatedAt        time.Time         `json:"updated_at"`
}

type ActiveRunEntry struct {
	RunID              string    `json:"run_id"`
	Workspace          string    `json:"workspace"`
	StateDir           string    `json:"state_dir"`
	GateEpoch          uint64    `json:"gate_epoch"`
	AttemptID          string    `json:"attempt_id,omitempty"`
	InstanceID         string    `json:"instance_id,omitempty"`
	OwnerPID           int       `json:"owner_pid,omitempty"`
	OwnerStartMarker   string    `json:"owner_start_marker,omitempty"`
	OwnerEndpoint      string    `json:"owner_endpoint,omitempty"`
	OwnerCapability    string    `json:"owner_capability,omitempty"`
	LastActionID       string    `json:"last_action_id,omitempty"`
	LastActionSequence int       `json:"last_action_sequence,omitempty"`
	UpdatedAt          time.Time `json:"updated_at"`
	ShutdownStatus     string    `json:"shutdown_status,omitempty"`
}

type ActiveRunIndex struct {
	SchemaVersion int                       `json:"schema_version"`
	Runs          map[string]ActiveRunEntry `json:"runs"`
	UpdatedAt     time.Time                 `json:"updated_at"`
}

type RequestFailure struct {
	Code    string         `json:"code"`
	Message string         `json:"message"`
	Details map[string]any `json:"details,omitempty"`
}

type RequestReceipt struct {
	RequestID   string          `json:"request_id"`
	Operation   string          `json:"operation"`
	PayloadHash string          `json:"payload_hash"`
	Status      string          `json:"status"`
	Payload     json.RawMessage `json:"payload,omitempty"`
	Failure     *RequestFailure `json:"failure,omitempty"`
	UpdatedAt   time.Time       `json:"updated_at"`
}

type Store struct {
	root string
	mu   sync.Mutex
}

func New(root string) (*Store, error) {
	if strings.TrimSpace(root) == "" {
		return nil, errors.New("storage root is required")
	}
	root = filepath.Clean(root)
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, fmt.Errorf("create storage root: %w", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "tasks"), 0o700); err != nil {
		return nil, fmt.Errorf("create task storage directory: %w", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "requests"), 0o700); err != nil {
		return nil, fmt.Errorf("create request storage directory: %w", err)
	}
	if err := recoverRoot(root); err != nil {
		return nil, fmt.Errorf("recover interrupted storage writes: %w", err)
	}
	return &Store{root: root}, nil
}

func (s *Store) Root() string { return s.root }

func (s *Store) CoordinationPath() string { return filepath.Join(s.root, "coordination.json") }

func (s *Store) CoordinationLockPath() string { return filepath.Join(s.root, ".coordination.lock") }

func (s *Store) ActiveRunIndexPath() string { return filepath.Join(s.root, "active-runs.json") }

func (s *Store) ActiveRunIndexLockPath() string { return filepath.Join(s.root, ".active-runs.lock") }

func (s *Store) LoadCoordination() (CoordinationState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	raw, err := os.ReadFile(s.CoordinationPath())
	if errors.Is(err, os.ErrNotExist) {
		return CoordinationState{SchemaVersion: CurrentSchemaVersion, GateEpoch: 0, UpdatedAt: time.Now().UTC()}, nil
	}
	if err != nil {
		return CoordinationState{}, fmt.Errorf("load coordination state: %w", err)
	}
	var state CoordinationState
	if err := json.Unmarshal(raw, &state); err != nil {
		return CoordinationState{}, fmt.Errorf("decode coordination state: %w", err)
	}
	if state.SchemaVersion != CurrentSchemaVersion {
		return CoordinationState{}, fmt.Errorf("unsupported coordination schema %d; expected %d", state.SchemaVersion, CurrentSchemaVersion)
	}
	return state, nil
}

func (s *Store) SaveCoordination(state CoordinationState) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if state.SchemaVersion == 0 {
		state.SchemaVersion = CurrentSchemaVersion
	}
	if state.SchemaVersion != CurrentSchemaVersion {
		return fmt.Errorf("unsupported coordination schema %d; expected %d", state.SchemaVersion, CurrentSchemaVersion)
	}
	if state.UpdatedAt.IsZero() {
		state.UpdatedAt = time.Now().UTC()
	}
	raw, err := marshalSanitizedIndent(state)
	if err != nil {
		return fmt.Errorf("marshal coordination state: %w", err)
	}
	return atomicReplace(s.CoordinationPath(), raw, 0o600)
}

func (s *Store) LoadActiveRunIndex() (ActiveRunIndex, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	raw, err := os.ReadFile(s.ActiveRunIndexPath())
	if errors.Is(err, os.ErrNotExist) {
		return ActiveRunIndex{SchemaVersion: CurrentSchemaVersion, Runs: map[string]ActiveRunEntry{}, UpdatedAt: time.Now().UTC()}, nil
	}
	if err != nil {
		return ActiveRunIndex{}, fmt.Errorf("load active-run index: %w", err)
	}
	var index ActiveRunIndex
	if err := json.Unmarshal(raw, &index); err != nil {
		return ActiveRunIndex{}, fmt.Errorf("decode active-run index: %w", err)
	}
	if index.SchemaVersion != CurrentSchemaVersion {
		return ActiveRunIndex{}, fmt.Errorf("unsupported active-run index schema %d; expected %d", index.SchemaVersion, CurrentSchemaVersion)
	}
	if index.Runs == nil {
		index.Runs = map[string]ActiveRunEntry{}
	}
	return index, nil
}

func (s *Store) SaveActiveRunIndex(index ActiveRunIndex) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if index.SchemaVersion == 0 {
		index.SchemaVersion = CurrentSchemaVersion
	}
	if index.SchemaVersion != CurrentSchemaVersion {
		return fmt.Errorf("unsupported active-run index schema %d; expected %d", index.SchemaVersion, CurrentSchemaVersion)
	}
	if index.Runs == nil {
		index.Runs = map[string]ActiveRunEntry{}
	}
	if index.UpdatedAt.IsZero() {
		index.UpdatedAt = time.Now().UTC()
	}
	raw, err := marshalSanitizedIndent(index)
	if err != nil {
		return fmt.Errorf("marshal active-run index: %w", err)
	}
	return atomicReplace(s.ActiveRunIndexPath(), raw, 0o600)
}

func NewState(task orchestration.Task, revision orchestration.TaskRevision) State {
	return State{
		SchemaVersion:      CurrentSchemaVersion,
		ProtocolVersion:    CurrentProtocolVersion,
		WriterVersion:      currentWriterVersion(),
		Task:               task,
		Revisions:          map[string]orchestration.TaskRevision{revision.ID: revision},
		Nodes:              map[string]orchestration.WorkflowNode{},
		Attempts:           map[string]orchestration.Attempt{},
		Actions:            map[string]orchestration.HostAction{},
		Completions:        map[string]orchestration.CompletionDecision{},
		HumanDecisions:     []orchestration.HumanDecision{},
		GoalAuthorizations: map[string]orchestration.GoalAuthorization{},
		Events:             []orchestration.Event{},
		CollaborationRuns:  map[string]orchestration.CollaborationRun{},
		StepRuns:           map[string]orchestration.StepRun{},
		InstanceRuns:       map[string]orchestration.InstanceRun{},
		TechnicalReleases:  map[string]orchestration.CollaborationTechnicalRelease{},
	}
}

func (s *Store) Save(state State) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.saveUnlocked(state)
}

func (s *Store) saveUnlocked(state State) error {
	if err := validateStateVersion(state); err != nil {
		return err
	}
	if strings.TrimSpace(state.Task.ID) == "" {
		return errors.New("task id is required")
	}
	if state.Revisions == nil {
		state.Revisions = map[string]orchestration.TaskRevision{}
	}
	if state.Nodes == nil {
		state.Nodes = map[string]orchestration.WorkflowNode{}
	}
	if state.Attempts == nil {
		state.Attempts = map[string]orchestration.Attempt{}
	}
	if state.Actions == nil {
		state.Actions = map[string]orchestration.HostAction{}
	}
	if state.Completions == nil {
		state.Completions = map[string]orchestration.CompletionDecision{}
	}
	if state.GoalAuthorizations == nil {
		state.GoalAuthorizations = map[string]orchestration.GoalAuthorization{}
	}
	if state.CollaborationRuns == nil {
		state.CollaborationRuns = map[string]orchestration.CollaborationRun{}
	}
	if state.StepRuns == nil {
		state.StepRuns = map[string]orchestration.StepRun{}
	}
	if state.InstanceRuns == nil {
		state.InstanceRuns = map[string]orchestration.InstanceRun{}
	}
	if state.TechnicalReleases == nil {
		state.TechnicalReleases = map[string]orchestration.CollaborationTechnicalRelease{}
	}
	if state.HumanDecisions == nil {
		state.HumanDecisions = []orchestration.HumanDecision{}
	}
	if state.Events == nil {
		state.Events = []orchestration.Event{}
	}
	taskDir := s.taskDir(state.Task.ID)
	if err := os.MkdirAll(taskDir, 0o700); err != nil {
		return fmt.Errorf("create task directory: %w", err)
	}
	raw, err := marshalSanitizedIndent(state)
	if err != nil {
		return fmt.Errorf("marshal task snapshot: %w", err)
	}
	if err := atomicReplace(filepath.Join(taskDir, "snapshot.json"), raw, 0o600); err != nil {
		return fmt.Errorf("write task snapshot: %w", err)
	}
	return nil
}

func (s *Store) Load(taskID string) (State, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.loadUnlocked(taskID)
}

func (s *Store) loadUnlocked(taskID string) (State, error) {
	if strings.TrimSpace(taskID) == "" {
		return State{}, errors.New("task id is required")
	}
	if err := validateStorageKey(taskID); err != nil {
		return State{}, err
	}
	if err := recoverAtomicPath(filepath.Join(s.taskDir(taskID), "snapshot.json")); err != nil {
		return State{}, err
	}
	raw, err := os.ReadFile(filepath.Join(s.taskDir(taskID), "snapshot.json"))
	if err != nil {
		return State{}, fmt.Errorf("load task %q: %w", taskID, err)
	}
	var state State
	if err := json.Unmarshal(raw, &state); err != nil {
		return State{}, fmt.Errorf("decode task %q: %w", taskID, err)
	}
	normalizeStateMaps(&state)
	if err := validateStateVersion(state); err != nil {
		return State{}, fmt.Errorf("load task %q: %w", taskID, err)
	}
	if err := s.reconcileEventsUnlocked(&state); err != nil {
		return State{}, fmt.Errorf("reconcile task %q events: %w", taskID, err)
	}
	return state, nil
}

// normalizeStateMaps keeps snapshots written before the collaboration
// projection fields were introduced readable. Missing maps mean "no records"
// rather than a corrupt execution state.
func normalizeStateMaps(state *State) {
	if state.Revisions == nil {
		state.Revisions = map[string]orchestration.TaskRevision{}
	}
	if state.Nodes == nil {
		state.Nodes = map[string]orchestration.WorkflowNode{}
	}
	if state.Attempts == nil {
		state.Attempts = map[string]orchestration.Attempt{}
	}
	if state.Actions == nil {
		state.Actions = map[string]orchestration.HostAction{}
	}
	if state.Completions == nil {
		state.Completions = map[string]orchestration.CompletionDecision{}
	}
	if state.GoalAuthorizations == nil {
		state.GoalAuthorizations = map[string]orchestration.GoalAuthorization{}
	}
	if state.CollaborationRuns == nil {
		state.CollaborationRuns = map[string]orchestration.CollaborationRun{}
	}
	if state.StepRuns == nil {
		state.StepRuns = map[string]orchestration.StepRun{}
	}
	if state.InstanceRuns == nil {
		state.InstanceRuns = map[string]orchestration.InstanceRun{}
	}
	if state.TechnicalReleases == nil {
		state.TechnicalReleases = map[string]orchestration.CollaborationTechnicalRelease{}
	}
	if state.HumanDecisions == nil {
		state.HumanDecisions = []orchestration.HumanDecision{}
	}
	if state.Events == nil {
		state.Events = []orchestration.Event{}
	}
}

// reconcileEventsUnlocked closes the small crash window between the durable
// snapshot replacement and the append-only JSONL write. The snapshot and log
// are both treated as evidence: missing log records are appended from the
// snapshot, while log records that reached disk first are merged into the
// loaded snapshot and persisted. This is intentionally replay-oriented rather
// than a second mutable state machine.
func (s *Store) reconcileEventsUnlocked(state *State) error {
	path := filepath.Join(s.taskDir(state.Task.ID), "events.jsonl")
	logged, err := ReplayEvents(path)
	if err != nil {
		return err
	}
	snapshotIDs := make(map[string]struct{}, len(state.Events))
	for _, event := range state.Events {
		if event.ID != "" {
			snapshotIDs[event.ID] = struct{}{}
		}
	}
	logIDs := make(map[string]struct{}, len(logged))
	snapshotChanged := false
	for _, event := range logged {
		if event.ID != "" {
			logIDs[event.ID] = struct{}{}
		}
		if _, ok := snapshotIDs[event.ID]; !ok {
			state.Events = append(state.Events, event)
			snapshotIDs[event.ID] = struct{}{}
			snapshotChanged = true
		}
	}
	for _, event := range state.Events {
		if event.ID == "" {
			continue
		}
		if _, ok := logIDs[event.ID]; ok {
			continue
		}
		if err := appendJSONLine(path, event); err != nil {
			return err
		}
		logIDs[event.ID] = struct{}{}
	}
	if snapshotChanged {
		return s.saveUnlocked(*state)
	}
	return nil
}

func (s *Store) AppendEvent(taskID string, event orchestration.Event) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	state, err := s.loadUnlocked(taskID)
	if err != nil {
		return err
	}
	if event.TaskID == "" {
		event.TaskID = taskID
	}
	if event.TaskID != taskID {
		return fmt.Errorf("event task id %q does not match %q", event.TaskID, taskID)
	}
	if event.ID == "" {
		event.ID = orchestration.NewID("event")
	}
	event.Data = sanitizeMap(event.Data)
	state.Events = append(state.Events, event)
	if err := s.saveUnlocked(state); err != nil {
		return err
	}
	if err := appendEventIfMissing(filepath.Join(s.taskDir(taskID), "events.jsonl"), event); err != nil {
		return fmt.Errorf("append event: %w", err)
	}
	return nil
}

// SaveWithEvent persists the supplied snapshot and appends one event record.
// Callers may include the event in state.Events before calling this method; it
// will not be duplicated in the snapshot.
func (s *Store) SaveWithEvent(state State, event orchestration.Event) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if event.TaskID == "" {
		event.TaskID = state.Task.ID
	}
	if event.TaskID != state.Task.ID {
		return fmt.Errorf("event task id %q does not match %q", event.TaskID, state.Task.ID)
	}
	if event.ID == "" {
		event.ID = orchestration.NewID("event")
	}
	event.Data = sanitizeMap(event.Data)
	if err := os.MkdirAll(s.taskDir(state.Task.ID), 0o700); err != nil {
		return fmt.Errorf("create task directory: %w", err)
	}
	found := false
	for _, existing := range state.Events {
		if existing.ID == event.ID {
			found = true
			break
		}
	}
	if !found {
		state.Events = append(state.Events, event)
	}
	if err := s.saveUnlocked(state); err != nil {
		return err
	}
	if err := appendEventIfMissing(filepath.Join(s.taskDir(state.Task.ID), "events.jsonl"), event); err != nil {
		return fmt.Errorf("append event: %w", err)
	}
	return nil
}

func (s *Store) LoadReceipt(requestID string) (RequestReceipt, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if strings.TrimSpace(requestID) == "" {
		return RequestReceipt{}, false, errors.New("request id is required")
	}
	path := filepath.Join(s.root, "requests", hashKey(requestID)+".json")
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return RequestReceipt{}, false, nil
	}
	if err != nil {
		return RequestReceipt{}, false, err
	}
	var receipt RequestReceipt
	if err := json.Unmarshal(raw, &receipt); err != nil {
		return RequestReceipt{}, false, fmt.Errorf("decode request receipt: %w", err)
	}
	if receipt.Status == "" && len(receipt.Payload) > 0 {
		// Receipts written before request journaling were completed outcomes.
		receipt.Status = RequestCompleted
	}
	if err := validateReceipt(receipt); err != nil {
		return RequestReceipt{}, false, fmt.Errorf("validate request receipt: %w", err)
	}
	if len(receipt.Payload) > 0 {
		var compact bytes.Buffer
		if err := json.Compact(&compact, receipt.Payload); err != nil {
			return RequestReceipt{}, false, fmt.Errorf("normalize request receipt payload: %w", err)
		}
		receipt.Payload = compact.Bytes()
	}
	return receipt, true, nil
}

func (s *Store) SaveReceipt(receipt RequestReceipt) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if strings.TrimSpace(receipt.RequestID) == "" || strings.TrimSpace(receipt.Operation) == "" {
		return errors.New("request id and operation are required")
	}
	if receipt.Status == "" && len(receipt.Payload) > 0 {
		receipt.Status = RequestCompleted
	}
	if receipt.UpdatedAt.IsZero() {
		receipt.UpdatedAt = time.Now().UTC()
	}
	if err := validateReceipt(receipt); err != nil {
		return err
	}
	if len(receipt.Payload) > 0 {
		sanitized, err := sanitizeJSON(receipt.Payload)
		if err != nil {
			return fmt.Errorf("sanitize request receipt payload: %w", err)
		}
		receipt.Payload = sanitized
	}
	if receipt.Failure != nil {
		receipt.Failure.Details = sanitizeMap(receipt.Failure.Details)
	}
	raw, err := json.MarshalIndent(receipt, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal request receipt: %w", err)
	}
	path := filepath.Join(s.root, "requests", hashKey(receipt.RequestID)+".json")
	return atomicReplace(path, raw, 0o600)
}

func validateReceipt(receipt RequestReceipt) error {
	if strings.TrimSpace(receipt.RequestID) == "" || strings.TrimSpace(receipt.Operation) == "" || strings.TrimSpace(receipt.PayloadHash) == "" {
		return errors.New("request id, operation and payload hash are required")
	}
	switch receipt.Status {
	case RequestPending, RequestOutcomeUnknown:
		if len(receipt.Payload) > 0 || receipt.Failure != nil {
			return fmt.Errorf("request receipt status %q cannot contain an outcome", receipt.Status)
		}
	case RequestCompleted:
		if len(receipt.Payload) == 0 || receipt.Failure != nil {
			return errors.New("completed request receipt requires payload only")
		}
		if !json.Valid(receipt.Payload) {
			return errors.New("completed request receipt payload is invalid JSON")
		}
	case RequestFailed:
		if receipt.Failure == nil || strings.TrimSpace(receipt.Failure.Code) == "" || len(receipt.Payload) > 0 {
			return errors.New("failed request receipt requires failure only")
		}
	default:
		return fmt.Errorf("unsupported request receipt status %q", receipt.Status)
	}
	return nil
}

func validateStateVersion(state State) error {
	if state.SchemaVersion != CurrentSchemaVersion {
		return fmt.Errorf("unsupported storage schema %d; expected %d and use a version-specific state-dir", state.SchemaVersion, CurrentSchemaVersion)
	}
	if state.ProtocolVersion != CurrentProtocolVersion {
		return fmt.Errorf("unsupported state protocol %d; expected %d", state.ProtocolVersion, CurrentProtocolVersion)
	}
	if strings.TrimSpace(state.WriterVersion) == "" {
		return errors.New("state writer_version is required")
	}
	return nil
}

func currentWriterVersion() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "agent-workflow-toolkit/devel"
	}
	version := strings.TrimSpace(info.Main.Version)
	if version == "" || version == "(devel)" {
		version = "devel"
	}
	revision := ""
	dirty := false
	for _, setting := range info.Settings {
		switch setting.Key {
		case "vcs.revision":
			revision = setting.Value
		case "vcs.modified":
			dirty = setting.Value == "true"
		}
	}
	if revision != "" {
		version += "+" + revision
	}
	if dirty {
		version += ".dirty"
	}
	return "agent-workflow-toolkit/" + version
}

func (s *Store) taskDir(taskID string) string {
	return filepath.Join(s.root, "tasks", taskID)
}

func validateStorageKey(value string) error {
	if strings.TrimSpace(value) == "" || value == "." || value == ".." ||
		filepath.Base(value) != value || strings.ContainsAny(value, `/\\`) {
		return fmt.Errorf("invalid storage key %q", value)
	}
	return nil
}

func hashKey(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func appendJSONLine(path string, value any) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	encoder := json.NewEncoder(file)
	if err := encoder.Encode(value); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}

func appendEventIfMissing(path string, event orchestration.Event) error {
	existing, err := ReplayEvents(path)
	if err != nil {
		return err
	}
	for _, candidate := range existing {
		if candidate.ID == event.ID {
			return nil
		}
	}
	return appendJSONLine(path, event)
}

func atomicReplace(path string, raw []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".atomic-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(raw); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}

	backup := path + ".bak"
	journalPath := path + ".journal"
	journal := atomicJournal{Target: path, Temp: tmpPath, Backup: backup, Phase: "prepared"}
	if err := writeJournal(journalPath, journal); err != nil {
		return err
	}
	_ = os.Remove(backup)
	if _, err := os.Stat(path); err == nil {
		if err := os.Rename(path, backup); err != nil {
			_ = os.Remove(journalPath)
			return err
		}
		journal.Phase = "backup_created"
		if err := writeJournal(journalPath, journal); err != nil {
			_ = os.Rename(backup, path)
			return err
		}
	}
	if err := os.Rename(tmpPath, path); err != nil {
		if _, backupErr := os.Stat(backup); backupErr == nil {
			_ = os.Rename(backup, path)
		}
		_ = os.Remove(journalPath)
		return err
	}
	journal.Phase = "target_replaced"
	if err := writeJournal(journalPath, journal); err != nil {
		return err
	}
	_ = os.Remove(backup)
	_ = os.Remove(journalPath)
	return nil
}

type atomicJournal struct {
	Target string `json:"target"`
	Temp   string `json:"temp"`
	Backup string `json:"backup"`
	Phase  string `json:"phase"`
}

func writeJournal(path string, journal atomicJournal) error {
	raw, err := json.Marshal(journal)
	if err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if _, err := file.Write(raw); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}

// recoverAtomicPath resolves the only ambiguous states that can be left by a
// process interruption: a journal with an unfinished replacement, or a backup
// left while the target is absent. A complete target always wins over cleanup
// of stale temporary files because it is the newest durable snapshot.
func recoverAtomicPath(path string) error {
	journalPath := path + ".journal"
	raw, journalErr := os.ReadFile(journalPath)
	if journalErr == nil {
		var journal atomicJournal
		if err := json.Unmarshal(raw, &journal); err != nil {
			return fmt.Errorf("decode atomic journal %q: %w", journalPath, err)
		}
		if filepath.Clean(journal.Target) != filepath.Clean(path) {
			return fmt.Errorf("atomic journal target mismatch for %q", path)
		}
		if _, err := os.Stat(path); err == nil {
			_ = os.Remove(journal.Temp)
			_ = os.Remove(journal.Backup)
			return os.Remove(journalPath)
		}
		if _, err := os.Stat(journal.Backup); err == nil {
			if err := os.Rename(journal.Backup, path); err != nil {
				return fmt.Errorf("restore atomic backup: %w", err)
			}
			_ = os.Remove(journal.Temp)
			return os.Remove(journalPath)
		}
		if _, err := os.Stat(journal.Temp); err == nil {
			if err := os.Rename(journal.Temp, path); err != nil {
				return fmt.Errorf("promote atomic temp: %w", err)
			}
			return os.Remove(journalPath)
		}
		return os.Remove(journalPath)
	}
	if !errors.Is(journalErr, os.ErrNotExist) {
		return journalErr
	}
	backup := path + ".bak"
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		if _, backupErr := os.Stat(backup); backupErr == nil {
			if err := os.Rename(backup, path); err != nil {
				return fmt.Errorf("restore orphaned atomic backup: %w", err)
			}
		}
	}
	return nil
}

func recoverRoot(root string) error {
	return filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		if strings.HasSuffix(entry.Name(), ".journal") {
			return recoverAtomicPath(strings.TrimSuffix(path, ".journal"))
		}
		if strings.HasSuffix(entry.Name(), ".bak") {
			return recoverAtomicPath(strings.TrimSuffix(path, ".bak"))
		}
		return nil
	})
}

func sanitizeJSON(raw []byte) ([]byte, error) {
	var compact bytes.Buffer
	if err := json.Compact(&compact, raw); err != nil {
		return nil, err
	}
	if !mayContainSensitive(compact.Bytes()) {
		return compact.Bytes(), nil
	}
	var value any
	if err := json.Unmarshal(compact.Bytes(), &value); err != nil {
		return nil, err
	}
	safe := sanitizeValue(value, "")
	return json.Marshal(safe)
}

func mayContainSensitive(raw []byte) bool {
	lower := strings.ToLower(string(raw))
	for _, marker := range []string{"secret", "token", "password", "passwd", "api_key", "apikey", "credential", "authorization", "auth_header", "private_key", "access_key", "bearer "} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func marshalSanitizedIndent(value any) ([]byte, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var decoded any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return nil, err
	}
	return json.MarshalIndent(sanitizeValue(decoded, ""), "", "  ")
}

func sanitizeMap(input map[string]any) map[string]any {
	if input == nil {
		return nil
	}
	value := sanitizeValue(input, "")
	output, _ := value.(map[string]any)
	return output
}

func sanitizeValue(value any, key string) any {
	if isSensitiveKey(key) {
		return "[REDACTED]"
	}
	switch typed := value.(type) {
	case map[string]any:
		result := make(map[string]any, len(typed))
		for name, child := range typed {
			result[name] = sanitizeValue(child, name)
		}
		return result
	case []any:
		result := make([]any, len(typed))
		for index, child := range typed {
			result[index] = sanitizeValue(child, key)
		}
		return result
	case string:
		trimmed := strings.TrimSpace(typed)
		lower := strings.ToLower(trimmed)
		if strings.HasPrefix(lower, "bearer ") {
			return "[REDACTED]"
		}
		for _, marker := range []string{
			"api_key=", "apikey=", "access_token=", "refresh_token=", "client_secret=", "authorization: bearer", "password=",
			`"token"`, `"secret"`, `"password"`, `"authorization"`, `"api_key"`, `"access_token"`,
			"token:", "secret:", "password:", "authorization:", "api_key:",
		} {
			if strings.Contains(lower, marker) {
				return "[REDACTED]"
			}
		}
		return typed
	default:
		return value
	}
}

func isSensitiveKey(key string) bool {
	normalized := strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(strings.TrimSpace(key), "-", "_"), " ", "_"))
	if normalized == "authorization" || normalized == "auth_header" || normalized == "authentication_header" || normalized == "user_authorization_evidence" || normalized == "cookie" {
		return true
	}
	for _, marker := range []string{"secret", "token", "password", "passwd", "api_key", "apikey", "credential", "private_key", "access_key"} {
		if strings.Contains(normalized, marker) {
			return true
		}
	}
	return false
}

func ReplayEvents(path string) ([]orchestration.Event, error) {
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer file.Close()
	var events []orchestration.Event
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		var event orchestration.Event
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	if err := scanner.Err(); err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	return events, nil
}
