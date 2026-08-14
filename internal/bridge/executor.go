package bridge

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
	"time"

	"agent-workflow-toolkit/internal/gateway"
	"agent-workflow-toolkit/internal/orchestration"
)

type Executor struct {
	Core                 CoreClient
	BridgeStateDir       string
	BridgeExecutable     string
	AttemptID            string
	ControllerInstanceID string
	ActionID             string
	ActionKind           orchestration.ActionKind
	AfterActionSequence  int
	Profile              Profile
}

type ExecutionOutput struct {
	SchemaVersion string                          `json:"schema_version"`
	AttemptID     string                          `json:"attempt_id"`
	ActionID      string                          `json:"action_id"`
	ActionKind    orchestration.ActionKind        `json:"action_kind"`
	Owner         OwnerReceiptView                `json:"owner"`
	Result        *orchestration.HostActionResult `json:"result,omitempty"`
}

type OwnerReceiptView struct {
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
	ProviderHandle     string    `json:"provider_handle,omitempty"`
	ProviderSessionID  string    `json:"provider_session_id,omitempty"`
	LastHeartbeat      time.Time `json:"last_heartbeat"`
	State              string    `json:"state"`
}

func (e Executor) Execute(ctx context.Context) (ExecutionOutput, error) {
	if ctx == nil {
		return ExecutionOutput{}, errors.New("context is required")
	}
	if strings.TrimSpace(e.AttemptID) == "" || strings.TrimSpace(e.ControllerInstanceID) == "" {
		return ExecutionOutput{}, errors.New("attempt-id and controller-instance-id are required")
	}
	if err := validateProfile(e.Profile); err != nil {
		return ExecutionOutput{}, err
	}
	if e.AfterActionSequence < 0 {
		return ExecutionOutput{}, errors.New("after-action-sequence cannot be negative")
	}
	store, err := NewOwnerStore(e.BridgeStateDir)
	if err != nil {
		return ExecutionOutput{}, err
	}
	batch, err := e.Core.PollHostActions(ctx, gateway.PollHostActionsInput{AttemptID: e.AttemptID, AfterActionSequence: e.AfterActionSequence, ControllerInstanceID: e.ControllerInstanceID})
	if err != nil {
		return ExecutionOutput{}, err
	}
	attempt := batch.Attempt
	if attempt.ID == "" {
		attemptResult, getErr := e.Core.GetAttempt(ctx, gateway.GetAttemptInput{AttemptID: e.AttemptID})
		if getErr != nil {
			return ExecutionOutput{}, getErr
		}
		attempt = attemptResult.Attempt
	}
	if attempt.ID != e.AttemptID {
		return ExecutionOutput{}, errors.New("core Attempt identity does not match the requested attempt")
	}
	action, err := selectAction(batch.Actions, e.ActionID, e.ActionKind)
	if err != nil {
		return ExecutionOutput{}, err
	}
	request, err := e.requestFor(attempt, action, batch.CollaborationRunID, batch.CollaborationInstance, batch.GateEpoch)
	if err != nil {
		return ExecutionOutput{}, err
	}
	receipt, found, err := store.Load(attempt.ID)
	if err != nil {
		return ExecutionOutput{}, err
	}
	if action.Kind == orchestration.ActionLaunch && !found {
		executable := e.BridgeExecutable
		if strings.TrimSpace(executable) == "" {
			executable, err = os.Executable()
			if err != nil {
				return ExecutionOutput{}, fmt.Errorf("resolve bridge executable: %w", err)
			}
		}
		receipt, err = startOwnerProcess(ctx, executable, ownerBootstrap{Request: request, Core: e.Core, BridgeStateDir: e.BridgeStateDir, ControllerInstanceID: e.ControllerInstanceID}, store)
		if err != nil {
			return ExecutionOutput{}, err
		}
		return ExecutionOutput{SchemaVersion: BridgeSchema, AttemptID: attempt.ID, ActionID: action.ID, ActionKind: action.Kind, Owner: publicOwnerReceipt(receipt)}, nil
	}
	if !found {
		return ExecutionOutput{}, errors.New("stale_owner: no owner receipt exists; bridge will not relaunch the provider")
	}
	if err := validateOwnerForRequest(receipt, request); err != nil {
		return ExecutionOutput{}, err
	}
	if action.Kind == orchestration.ActionLaunch {
		if receipt.ActionID != action.ID {
			return ExecutionOutput{}, errors.New("owner_mismatch: launch action does not match the existing owner")
		}
		// The detached owner may have registered and returned ready before the
		// Provider publishes a session. A repeated public launch poll must join
		// that owner, not attempt IPC execution or manufacture a stale owner.
		return ExecutionOutput{SchemaVersion: BridgeSchema, AttemptID: attempt.ID, ActionID: action.ID, ActionKind: action.Kind, Owner: publicOwnerReceipt(receipt)}, nil
	}
	result, err := sendOwnerAction(ctx, receipt, action)
	if err != nil {
		return ExecutionOutput{}, err
	}
	updated, found, loadErr := store.Load(attempt.ID)
	if loadErr != nil {
		return ExecutionOutput{}, loadErr
	}
	if !found {
		updated = receipt
	}
	return ExecutionOutput{SchemaVersion: BridgeSchema, AttemptID: attempt.ID, ActionID: action.ID, ActionKind: action.Kind, Owner: publicOwnerReceipt(updated), Result: &result}, nil
}

func (e Executor) requestFor(attempt orchestration.Attempt, action orchestration.HostAction, runID, instanceID string, gateEpoch uint64) (Request, error) {
	if e.Profile.ID == "" || attempt.Binding.ProfileID == "" || e.Profile.ID != attempt.Binding.ProfileID {
		return Request{}, errors.New("adapter profile does not match the frozen Attempt binding")
	}
	if actionProfileID := actionString(action.Parameters, "adapter_profile_id"); actionProfileID != "" && actionProfileID != e.Profile.ID {
		return Request{}, errors.New("core action adapter profile does not match the selected profile")
	}
	expectedHash := actionString(action.Parameters, "adapter_profile_hash")
	if expectedHash != "" && !strings.EqualFold(expectedHash, e.Profile.ProfileHash) {
		return Request{}, errors.New("adapter profile hash mismatch")
	}
	if action.Kind == orchestration.ActionLaunch && expectedHash == "" {
		return Request{}, errors.New("core launch action does not contain an adapter profile hash")
	}
	if !samePath(attempt.Binding.Workspace, actionString(action.Parameters, "workspace")) && actionString(action.Parameters, "workspace") != "" {
		return Request{}, errors.New("core action workspace mismatch")
	}
	return Request{SchemaVersion: BridgeSchema, AttemptID: attempt.ID, ControllerInstanceID: e.ControllerInstanceID, CollaborationRunID: runID, CollaborationInstanceID: instanceID, GateEpoch: gateEpoch, Action: action, Attempt: attempt, Profile: e.Profile, ExpectedProfileHash: expectedHash, RolePromptHash: actionString(action.Parameters, "role_prompt_hash")}, nil
}

func selectAction(actions []orchestration.HostAction, actionID string, kind orchestration.ActionKind) (orchestration.HostAction, error) {
	var selected *orchestration.HostAction
	for index := range actions {
		action := actions[index]
		if actionID != "" && action.ID != actionID {
			continue
		}
		if kind != "" && action.Kind != kind {
			continue
		}
		if selected != nil {
			return orchestration.HostAction{}, errors.New("more than one pending host action matches the bridge request")
		}
		selected = &action
	}
	if selected == nil {
		return orchestration.HostAction{}, errors.New("no matching pending core host action")
	}
	return *selected, nil
}

func validateOwnerForRequest(receipt OwnerReceipt, request Request) error {
	if receipt.AttemptID != request.AttemptID || receipt.Provider != normalizeProvider(request.Attempt.Binding.Adapter) || receipt.ProfileID != request.Profile.ID || !strings.EqualFold(receipt.ProfileHash, request.Profile.ProfileHash) || !samePath(receipt.Workspace, request.Attempt.Binding.Workspace) {
		return errors.New("owner_mismatch: receipt does not match the frozen Attempt/profile/workspace")
	}
	if receipt.State != "starting" && receipt.State != "ready" && receipt.State != "running" {
		return fmt.Errorf("stale_owner: owner receipt is in %s state", receipt.State)
	}
	if !isLoopbackEndpoint(receipt.Endpoint) {
		return errors.New("owner_mismatch: owner endpoint is not loopback")
	}
	if receipt.OwnerPID <= 0 || strings.TrimSpace(receipt.OwnerStartMarker) == "" || strings.TrimSpace(receipt.Capability) == "" {
		return errors.New("owner_mismatch: owner process identity or capability is missing")
	}
	if running, err := processIdentityMatches(receipt.OwnerPID, receipt.OwnerStartMarker); err != nil || !running {
		return errors.New("stale_owner: owner PID/start marker no longer identifies the original process")
	}
	return nil
}

func isLoopbackEndpoint(endpoint string) bool {
	host, _, err := net.SplitHostPort(endpoint)
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func publicOwnerReceipt(receipt OwnerReceipt) OwnerReceiptView {
	return OwnerReceiptView{
		SchemaVersion: receipt.SchemaVersion, AttemptID: receipt.AttemptID, ActionID: receipt.ActionID,
		LastActionID: receipt.LastActionID, LastActionSequence: receipt.LastActionSequence, Provider: receipt.Provider,
		ProfileID: receipt.ProfileID, ProfileHash: receipt.ProfileHash, Workspace: receipt.Workspace,
		OwnerPID: receipt.OwnerPID, OwnerStartMarker: receipt.OwnerStartMarker, Endpoint: receipt.Endpoint,
		ProviderHandle: receipt.ProviderHandle, ProviderSessionID: receipt.ProviderSessionID,
		LastHeartbeat: receipt.LastHeartbeat, State: receipt.State,
	}
}
