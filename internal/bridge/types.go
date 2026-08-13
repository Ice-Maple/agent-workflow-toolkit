package bridge

import (
	"time"

	"agent-workflow-toolkit/internal/orchestration"
)

const (
	ProviderClaude     = "claude"
	ProviderCursor     = "cursor"
	ProviderCodexCLI   = "codexcli"
	BridgeSchema       = "external-cli-bridge/v1"
	OwnerReceiptSchema = "external-cli-owner/v1"
)

// Profile is the machine-local execution profile selected by a frozen
// Binding. AdapterProfile contains only non-secret routing data; trust is an
// explicit local authorization bit and is never inferred from a prompt.
type Profile struct {
	orchestration.AdapterProfile
	TrustAuthorized bool `json:"trust_authorized"`
}

// Request is the verified core-issued action plus the frozen Attempt and
// machine-local profile used to execute it. Public callers should obtain
// Action and Attempt from the core bridge client instead of reconstructing
// either value from business input.
type Request struct {
	SchemaVersion           string                   `json:"schema_version"`
	AttemptID               string                   `json:"attempt_id"`
	ControllerInstanceID    string                   `json:"controller_instance_id"`
	CollaborationRunID      string                   `json:"collaboration_run_id,omitempty"`
	CollaborationInstanceID string                   `json:"collaboration_instance_id,omitempty"`
	GateEpoch               uint64                   `json:"gate_epoch,omitempty"`
	Action                  orchestration.HostAction `json:"action"`
	Attempt                 orchestration.Attempt    `json:"attempt"`
	Profile                 Profile                  `json:"profile"`
	ExpectedProfileHash     string                   `json:"expected_profile_hash,omitempty"`
	RolePromptHash          string                   `json:"role_prompt_hash,omitempty"`
}

type OwnerReceipt struct {
	SchemaVersion      string    `json:"schema_version"`
	AttemptID          string    `json:"attempt_id"`
	ActionID           string    `json:"action_id"`
	LastActionID       string    `json:"last_action_id,omitempty"`
	LastActionSequence int       `json:"last_action_sequence,omitempty"`
	Provider           string    `json:"provider"`
	ProfileID          string    `json:"profile_id"`
	ProfileHash        string    `json:"profile_hash"`
	Workspace          string    `json:"workspace"`
	OwnerPID           int       `json:"owner_pid"`
	OwnerStartMarker   string    `json:"owner_start_marker"`
	Endpoint           string    `json:"endpoint"`
	Capability         string    `json:"capability"`
	ProviderHandle     string    `json:"provider_handle,omitempty"`
	ProviderSessionID  string    `json:"provider_session_id,omitempty"`
	LastHeartbeat      time.Time `json:"last_heartbeat"`
	State              string    `json:"state"`
}
