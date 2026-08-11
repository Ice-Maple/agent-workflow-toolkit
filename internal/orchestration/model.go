// Package orchestration contains the provider-independent state model used by
// every execution path. It intentionally keeps provider, adapter, role and
// model values as strings so policy can evolve without a closed enum.
package orchestration

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type TaskStatus string

const (
	TaskActive                TaskStatus = "active"
	TaskAwaitingHumanDecision TaskStatus = "awaiting_human_decision"
	TaskCompleted             TaskStatus = "completed"
	TaskAborted               TaskStatus = "aborted"
)

type NodeStatus string

const (
	NodeProposed              NodeStatus = "proposed"
	NodePreflightBlocked      NodeStatus = "preflight_blocked"
	NodePrepared              NodeStatus = "prepared"
	NodeRunning               NodeStatus = "running"
	NodeAwaitingHumanDecision NodeStatus = "awaiting_human_decision"
	NodeApproved              NodeStatus = "approved"
	NodeRevisionRequested     NodeStatus = "revision_requested"
	NodeAborted               NodeStatus = "aborted"
)

type AttemptStatus string

const (
	AttemptPrepared          AttemptStatus = "prepared"
	AttemptLaunching         AttemptStatus = "launching"
	AttemptRunning           AttemptStatus = "running"
	AttemptCompleted         AttemptStatus = "completed"
	AttemptFailed            AttemptStatus = "failed"
	AttemptIncomplete        AttemptStatus = "incomplete"
	AttemptTimedOut          AttemptStatus = "timed_out"
	AttemptContaminated      AttemptStatus = "contaminated"
	AttemptCancelRequested   AttemptStatus = "cancel_requested"
	AttemptCancelled         AttemptStatus = "cancelled"
	AttemptCancelUnconfirmed AttemptStatus = "cancel_unconfirmed"
)

type ActionKind string

const (
	ActionLaunch  ActionKind = "launch"
	ActionObserve ActionKind = "observe"
	// ActionSend and ActionCleanup are reserved Adapter lifecycle primitives.
	// TaskGateway protocol v1 does not issue or accept them as HostActions.
	ActionSend       ActionKind = "send"
	ActionCancelHost ActionKind = "cancel_host"
	ActionCleanup    ActionKind = "cleanup"
)

type ActionStatus string

const (
	ActionPending  ActionStatus = "pending"
	ActionComplete ActionStatus = "completed"
	ActionExpired  ActionStatus = "expired"
	ActionUnknown  ActionStatus = "outcome_unknown"
)

type Binding struct {
	Role         string   `json:"role"`
	Adapter      string   `json:"adapter"`
	Model        string   `json:"model"`
	Reasoning    string   `json:"reasoning"`
	Permission   string   `json:"permission"`
	Workspace    string   `json:"workspace"`
	MCPAllowlist []string `json:"mcp_allowlist,omitempty"`
}

type ExpectedOutput struct {
	Format           string   `json:"format,omitempty"`
	ArtifactPath     string   `json:"artifact_path,omitempty"`
	Required         bool     `json:"required"`
	MinBytes         int      `json:"min_bytes,omitempty"`
	RequiredSections []string `json:"required_sections,omitempty"`
}

type Task struct {
	ID                string     `json:"task_id"`
	Title             string     `json:"title,omitempty"`
	Objective         string     `json:"objective"`
	Workspace         string     `json:"workspace"`
	CheckpointMode    string     `json:"checkpoint_mode"`
	Status            TaskStatus `json:"status"`
	CurrentRevisionID string     `json:"current_revision_id"`
	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at"`
}

type TaskRevision struct {
	ID               string    `json:"revision_id"`
	TaskID           string    `json:"task_id"`
	ParentRevisionID string    `json:"parent_revision_id,omitempty"`
	Instruction      string    `json:"instruction"`
	CreatedAt        time.Time `json:"created_at"`
}

type WorkflowNode struct {
	ID                    string         `json:"node_id"`
	TaskID                string         `json:"task_id"`
	RevisionID            string         `json:"revision_id"`
	Kind                  string         `json:"kind"`
	Role                  string         `json:"role,omitempty"`
	Status                NodeStatus     `json:"status"`
	CheckpointMode        string         `json:"checkpoint_mode"`
	PreflightStatus       string         `json:"preflight_status,omitempty"`
	PreflightCode         string         `json:"preflight_code,omitempty"`
	PreflightMCPIsolation string         `json:"preflight_mcp_isolation,omitempty"`
	CheckpointAttemptID   string         `json:"checkpoint_attempt_id,omitempty"`
	ApprovedBinding       *Binding       `json:"approved_binding,omitempty"`
	ExpectedOutput        ExpectedOutput `json:"expected_output,omitempty"`
	CreatedAt             time.Time      `json:"created_at"`
	UpdatedAt             time.Time      `json:"updated_at"`
}

type Attempt struct {
	ID                string            `json:"attempt_id"`
	TaskID            string            `json:"task_id"`
	NodeID            string            `json:"node_id"`
	RevisionID        string            `json:"revision_id"`
	Status            AttemptStatus     `json:"status"`
	Binding           Binding           `json:"binding"`
	ProviderSessionID string            `json:"provider_session_id,omitempty"`
	ProviderHandle    string            `json:"provider_handle,omitempty"`
	LaunchNonce       string            `json:"launch_nonce"`
	TaskCanary        string            `json:"task_canary"`
	ExecutionStarted  bool              `json:"execution_started"`
	SideEffectState   string            `json:"side_effect_state"`
	ReadOnlyIsolation bool              `json:"readonly_isolation"`
	MCPIsolation      string            `json:"mcp_isolation,omitempty"`
	CanaryVerified    bool              `json:"canary_verified"`
	TerminalEventID   string            `json:"terminal_event_id,omitempty"`
	RetryOf           string            `json:"retry_of,omitempty"`
	FallbackOf        string            `json:"fallback_of,omitempty"`
	Identity          map[string]string `json:"identity,omitempty"`
	Artifacts         []ArtifactRef     `json:"artifacts,omitempty"`
	CreatedAt         time.Time         `json:"created_at"`
	UpdatedAt         time.Time         `json:"updated_at"`
}

type HostAction struct {
	ID                   string            `json:"action_id"`
	AttemptID            string            `json:"attempt_id"`
	Kind                 ActionKind        `json:"kind"`
	Sequence             int               `json:"sequence"`
	Lease                string            `json:"lease"`
	ControllerInstanceID string            `json:"controller_instance_id,omitempty"`
	Parameters           map[string]any    `json:"parameters,omitempty"`
	ParametersHash       string            `json:"parameters_hash"`
	Status               ActionStatus      `json:"status"`
	Result               *HostActionResult `json:"result,omitempty"`
	IssuedAt             time.Time         `json:"issued_at"`
	ExpiresAt            time.Time         `json:"expires_at"`
}

type HostActionResult struct {
	Status            string            `json:"status"`
	Reason            string            `json:"reason,omitempty"`
	ProviderHandle    string            `json:"provider_handle,omitempty"`
	ProviderSessionID string            `json:"provider_session_id,omitempty"`
	Terminal          bool              `json:"terminal"`
	ExecutionStarted  *bool             `json:"execution_started,omitempty"`
	SideEffectState   string            `json:"side_effect_state,omitempty"`
	Identity          map[string]string `json:"identity,omitempty"`
	TaskCanary        string            `json:"task_canary,omitempty"`
	RawEventRefs      []string          `json:"raw_event_refs,omitempty"`
	FinalText         string            `json:"final_text,omitempty"`
}

type ArtifactRef struct {
	Path      string `json:"path"`
	Format    string `json:"format,omitempty"`
	Bytes     int64  `json:"bytes,omitempty"`
	SHA256    string `json:"sha256,omitempty"`
	Validated bool   `json:"validated"`
}

type CompletionDecision struct {
	ID        string        `json:"completion_decision_id"`
	AttemptID string        `json:"attempt_id"`
	Status    AttemptStatus `json:"status"`
	Reason    string        `json:"reason"`
	Evidence  []string      `json:"evidence,omitempty"`
	Artifacts []ArtifactRef `json:"artifacts,omitempty"`
	CreatedAt time.Time     `json:"created_at"`
}

type HumanDecision struct {
	ID           string    `json:"human_decision_id"`
	NodeID       string    `json:"node_id"`
	AttemptID    string    `json:"attempt_id,omitempty"`
	Decision     string    `json:"decision"`
	AuthorityRef string    `json:"authority_ref,omitempty"`
	Instruction  string    `json:"instruction,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
}

type GoalAuthorization struct {
	ID           string    `json:"goal_authority_id"`
	TaskID       string    `json:"task_id"`
	RevisionID   string    `json:"revision_id"`
	Scope        []string  `json:"scope"`
	UserEvidence string    `json:"user_authorization_evidence"`
	Active       bool      `json:"active"`
	CreatedAt    time.Time `json:"created_at"`
	RevokedAt    time.Time `json:"revoked_at,omitempty"`
	RevokeReason string    `json:"revoke_reason,omitempty"`
}

type Event struct {
	ID             string         `json:"event_id"`
	TaskID         string         `json:"task_id"`
	AttemptID      string         `json:"attempt_id,omitempty"`
	NodeID         string         `json:"node_id,omitempty"`
	CausalActionID string         `json:"causal_action_id,omitempty"`
	Sequence       int            `json:"sequence,omitempty"`
	Type           string         `json:"type"`
	Data           map[string]any `json:"data,omitempty"`
	CreatedAt      time.Time      `json:"created_at"`
}

func NewID(prefix string) string {
	var raw [12]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano())
	}
	return prefix + "-" + hex.EncodeToString(raw[:])
}

func HashParameters(parameters map[string]any) string {
	if parameters == nil {
		parameters = map[string]any{}
	}
	raw, _ := json.Marshal(parameters)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func NewTask(title, objective, workspace string) (Task, error) {
	if strings.TrimSpace(objective) == "" {
		return Task{}, errors.New("objective is required")
	}
	canonicalWorkspace, err := CanonicalDirectory(workspace)
	if err != nil {
		return Task{}, err
	}
	now := time.Now().UTC()
	task := Task{
		ID:             NewID("task"),
		Title:          strings.TrimSpace(title),
		Objective:      strings.TrimSpace(objective),
		Workspace:      canonicalWorkspace,
		CheckpointMode: "checkpointed",
		Status:         TaskActive,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	revision := NewID("revision")
	task.CurrentRevisionID = revision
	return task, nil
}

// CanonicalDirectory returns an existing directory's cleaned, resolved path.
// Task and binding identity checks use this helper so a symlink or relative
// spelling cannot silently select a different workspace.
func CanonicalDirectory(path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", errors.New("workspace is required")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve workspace path: %w", err)
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", fmt.Errorf("workspace is not accessible: %w", err)
	}
	if !info.IsDir() {
		return "", errors.New("workspace must be a directory")
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", fmt.Errorf("canonicalize workspace: %w", err)
	}
	return filepath.Clean(resolved), nil
}

func NewRevision(taskID, parentID, instruction string) (TaskRevision, error) {
	if strings.TrimSpace(taskID) == "" || strings.TrimSpace(instruction) == "" {
		return TaskRevision{}, errors.New("task_id and instruction are required")
	}
	return TaskRevision{ID: NewID("revision"), TaskID: taskID, ParentRevisionID: parentID, Instruction: strings.TrimSpace(instruction), CreatedAt: time.Now().UTC()}, nil
}

func NewEvent(taskID, eventType string, data map[string]any) Event {
	return Event{ID: NewID("event"), TaskID: taskID, Type: eventType, Data: data, CreatedAt: time.Now().UTC()}
}

func ValidateAttemptTransition(from, to AttemptStatus) error {
	if from == to {
		return nil
	}
	allowed := map[AttemptStatus][]AttemptStatus{
		AttemptPrepared:          {AttemptLaunching, AttemptFailed, AttemptIncomplete, AttemptCancelRequested},
		AttemptLaunching:         {AttemptRunning, AttemptCompleted, AttemptFailed, AttemptIncomplete, AttemptTimedOut, AttemptContaminated, AttemptCancelRequested},
		AttemptRunning:           {AttemptCompleted, AttemptFailed, AttemptIncomplete, AttemptTimedOut, AttemptContaminated, AttemptCancelRequested},
		AttemptCancelRequested:   {AttemptCancelled, AttemptCancelUnconfirmed, AttemptIncomplete, AttemptContaminated},
		AttemptCancelUnconfirmed: {AttemptIncomplete},
	}
	for _, candidate := range allowed[from] {
		if candidate == to {
			return nil
		}
	}
	return fmt.Errorf("invalid attempt transition %q -> %q", from, to)
}
