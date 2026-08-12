// Package gateway is the deterministic boundary between the versioned local
// protocol and the orchestration state. It creates attempts and host actions;
// it never invokes a provider itself.
package gateway

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"time"

	"agent-workflow-toolkit/internal/coordination"
	"agent-workflow-toolkit/internal/orchestration"
	"agent-workflow-toolkit/internal/policy"
	protocol "agent-workflow-toolkit/internal/protocol/v1"
	"agent-workflow-toolkit/internal/storage"
)

const (
	controlActionLeaseDuration = 5 * time.Minute
	observeActionLeaseDuration = 15 * time.Minute
)

type Error struct {
	Code    string
	Message string
	Details map[string]any
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	return e.Code + ": " + e.Message
}

func fail(code, message string, details map[string]any) *Error {
	return &Error{Code: code, Message: message, Details: details}
}

type CreateTaskInput struct {
	Title     string `json:"title,omitempty"`
	Objective string `json:"objective"`
	Workspace string `json:"workspace"`
}

type CreateTaskResult struct {
	Task     orchestration.Task         `json:"task"`
	Revision orchestration.TaskRevision `json:"revision"`
}

type ProposeNodeInput struct {
	TaskID         string                       `json:"task_id"`
	RevisionID     string                       `json:"revision_id,omitempty"`
	Kind           string                       `json:"kind"`
	Role           string                       `json:"role,omitempty"`
	ExpectedOutput orchestration.ExpectedOutput `json:"expected_output,omitempty"`
}

type ProposeNodeResult struct {
	Node orchestration.WorkflowNode `json:"node"`
}

type RunPreflightInput struct {
	NodeID  string                `json:"node_id"`
	Binding orchestration.Binding `json:"binding"`
}

type RunPreflightResult struct {
	Node      orchestration.WorkflowNode `json:"node"`
	Preflight policy.PreflightDecision   `json:"preflight"`
}

type PrepareAttemptInput struct {
	NodeID               string                 `json:"node_id"`
	ControllerInstanceID string                 `json:"controller_instance_id"`
	Binding              *orchestration.Binding `json:"binding,omitempty"`
	PreviousAttemptID    string                 `json:"previous_attempt_id,omitempty"`
	CollaborationRunID   string                 `json:"collaboration_run_id,omitempty"`
	InstanceRunID        string                 `json:"instance_run_id,omitempty"`
	GateEpoch            uint64                 `json:"gate_epoch,omitempty"`
}

type RetryAttemptInput struct {
	NodeID               string `json:"node_id"`
	PreviousAttemptID    string `json:"previous_attempt_id"`
	ControllerInstanceID string `json:"controller_instance_id"`
	ErrorCode            string `json:"error_code"`
	RetriesAlreadyUsed   int    `json:"retries_already_used"`
}

type FallbackAttemptInput struct {
	NodeID               string                `json:"node_id"`
	PreviousAttemptID    string                `json:"previous_attempt_id"`
	ControllerInstanceID string                `json:"controller_instance_id"`
	Binding              orchestration.Binding `json:"binding"`
	Approved             bool                  `json:"approved"`
}

type LaunchDecision struct {
	Attempt        orchestration.Attempt    `json:"attempt"`
	LaunchAction   orchestration.HostAction `json:"launch_action"`
	DispatchPrompt string                   `json:"dispatch_prompt"`
}

type PollHostActionsInput struct {
	AttemptID            string `json:"attempt_id"`
	AfterActionSequence  int    `json:"after_action_sequence,omitempty"`
	ControllerInstanceID string `json:"controller_instance_id"`
}

type HostActionBatch struct {
	AttemptID string                     `json:"attempt_id"`
	Actions   []orchestration.HostAction `json:"actions"`
}

type SubmitHostActionResultInput struct {
	AttemptID string                         `json:"attempt_id"`
	ActionID  string                         `json:"action_id"`
	Lease     string                         `json:"lease"`
	Result    orchestration.HostActionResult `json:"result"`
}

type ActionReceipt struct {
	Action  orchestration.HostAction   `json:"action"`
	Attempt orchestration.Attempt      `json:"attempt"`
	Next    []orchestration.HostAction `json:"next_host_actions,omitempty"`
}

type BindProviderHandleInput struct {
	AttemptID         string            `json:"attempt_id"`
	LaunchActionID    string            `json:"launch_action_id"`
	ProviderHandle    string            `json:"provider_handle"`
	ProviderSessionID string            `json:"provider_session_id,omitempty"`
	Identity          map[string]string `json:"identity"`
}

type AppendEventInput struct {
	AttemptID      string         `json:"attempt_id"`
	NodeID         string         `json:"node_id,omitempty"`
	CausalActionID string         `json:"causal_action_id,omitempty"`
	Sequence       int            `json:"sequence,omitempty"`
	Type           string         `json:"type"`
	Data           map[string]any `json:"data,omitempty"`
}

type CompletionInput struct {
	AttemptID          string `json:"attempt_id"`
	CollaborationRunID string `json:"collaboration_run_id,omitempty"`
	InstanceRunID      string `json:"instance_run_id,omitempty"`
	GateEpoch          uint64 `json:"gate_epoch,omitempty"`
}

type CreateCollaborationRunInput struct {
	TaskID           string                          `json:"task_id"`
	RevisionID       string                          `json:"revision_id,omitempty"`
	Workspace        string                          `json:"workspace"`
	EffectiveMode    orchestration.CollaborationMode `json:"effective_mode"`
	GateEpoch        uint64                          `json:"gate_epoch,omitempty"`
	Plan             orchestration.CollaborationPlan `json:"plan"`
	RolePromptHashes map[string]string               `json:"role_prompt_hashes,omitempty"`
	StateDir         string                          `json:"state_dir"`
}

type CreateCollaborationRunResult struct {
	Run       orchestration.CollaborationRun `json:"run"`
	StepRuns  []orchestration.StepRun        `json:"step_runs"`
	Instances []orchestration.InstanceRun    `json:"instances"`
}

type ResolveRoleInput struct {
	Role      string `json:"role"`
	Workspace string `json:"workspace"`
}

type ResolveRoleResult struct {
	Binding orchestration.Binding `json:"binding"`
}

type TechnicalReleaseInput struct {
	RunID      string `json:"run_id"`
	InstanceID string `json:"instance_id"`
	Reason     string `json:"reason"`
}

type TechnicalReleaseResult struct {
	Release   orchestration.CollaborationTechnicalRelease `json:"release"`
	Instance  orchestration.InstanceRun                   `json:"instance"`
	NextReady []orchestration.InstanceRun                 `json:"next_ready,omitempty"`
}

type CreateCollaborationRunOptions struct {
	CoordinationRoot string
}

type CompletionResult struct {
	Decision orchestration.CompletionDecision `json:"decision"`
	Node     orchestration.WorkflowNode       `json:"node"`
}

type GoalInput struct {
	TaskID       string   `json:"task_id"`
	Scope        []string `json:"scope"`
	UserEvidence string   `json:"user_authorization_evidence"`
}

type GoalResult struct {
	Authorization orchestration.GoalAuthorization `json:"authorization"`
}

type RevokeGoalInput struct {
	TaskID          string `json:"task_id"`
	GoalAuthorityID string `json:"goal_authority_id"`
	Reason          string `json:"reason"`
}

type HumanDecisionInput struct {
	NodeID       string `json:"node_id"`
	AttemptID    string `json:"attempt_id,omitempty"`
	Decision     string `json:"decision"`
	AuthorityRef string `json:"authority_ref"`
	Instruction  string `json:"instruction,omitempty"`
}

type HumanDecisionResult struct {
	Decision orchestration.HumanDecision `json:"decision"`
	Node     orchestration.WorkflowNode  `json:"node"`
	NewNode  *orchestration.WorkflowNode `json:"new_node,omitempty"`
}

type CancelInput struct {
	AttemptID string `json:"attempt_id"`
	Reason    string `json:"reason"`
}

type CancelResult struct {
	Attempt orchestration.Attempt    `json:"attempt"`
	Action  orchestration.HostAction `json:"cancel_action"`
}

type Gateway struct {
	store              *storage.Store
	config             policy.Config
	coordination       *coordination.Manager
	coordinationRoot   string
	coordinationLocked bool
	saveReceipt        func(storage.RequestReceipt) error
	mu                 sync.Mutex
}

func (g *Gateway) CreateCollaborationRun(input CreateCollaborationRunInput) (CreateCollaborationRunResult, error) {
	coord := g.coordination
	if coord == nil {
		var err error
		coord, err = coordination.NewWithStore(g.store, controlActionLeaseDuration)
		if err != nil {
			return CreateCollaborationRunResult{}, err
		}
	}
	if input.GateEpoch == 0 {
		return CreateCollaborationRunResult{}, fail("gate_epoch_required", "framework collaboration requires the current machine gate epoch", nil)
	}
	if !g.coordinationLocked {
		if err := coord.Admission(input.GateEpoch); err != nil {
			return CreateCollaborationRunResult{}, fail("collaboration_gate_rejected", err.Error(), nil)
		}
	}
	state, err := g.store.Load(input.TaskID)
	if err != nil {
		return CreateCollaborationRunResult{}, fail("unknown_task", "task was not found", nil)
	}
	if input.RevisionID == "" {
		input.RevisionID = state.Task.CurrentRevisionID
	}
	if input.RevisionID != state.Task.CurrentRevisionID {
		return CreateCollaborationRunResult{}, fail("revision_mismatch", "collaboration plan must target the current task revision", nil)
	}
	if input.EffectiveMode != orchestration.CollaborationFramework {
		return CreateCollaborationRunResult{}, fail("unsupported_collaboration_mode", "only framework mode creates a core collaboration run", nil)
	}
	if input.Plan.SchemaVersion != "collaboration/v1" {
		return CreateCollaborationRunResult{}, fail("invalid_collaboration_plan", "plan schema_version must be collaboration/v1", nil)
	}
	if len(input.Plan.Steps) == 0 {
		return CreateCollaborationRunResult{}, fail("invalid_collaboration_plan", "plan must contain at least one step", nil)
	}
	if strings.TrimSpace(input.StateDir) == "" {
		return CreateCollaborationRunResult{}, fail("state_dir_required", "state_dir is required for collaboration admission", nil)
	}
	if input.Workspace == "" {
		input.Workspace = state.Task.Workspace
	}
	if canonical := canonicalPathForCompare(input.Workspace); canonical != canonicalPathForCompare(state.Task.Workspace) {
		return CreateCollaborationRunResult{}, fail("workspace_mismatch", "collaboration workspace does not match task workspace", nil)
	}
	if err := validateCollaborationPlan(input.Plan); err != nil {
		return CreateCollaborationRunResult{}, fail("invalid_collaboration_plan", err.Error(), nil)
	}
	planHash := hashJSON(input.Plan)
	runID := orchestration.NewID("run")
	now := time.Now().UTC()
	run := orchestration.CollaborationRun{ID: runID, TaskID: state.Task.ID, RevisionID: input.RevisionID, PlanHash: planHash, RolePromptHashes: copyStringMap(input.RolePromptHashes), Plan: input.Plan, Workspace: state.Task.Workspace, EffectiveMode: input.EffectiveMode, GateEpoch: input.GateEpoch, SchedulerAuthorityID: orchestration.NewID("scheduler"), Status: orchestration.CollaborationRunActive, CreatedAt: now, UpdatedAt: now}
	stepRuns := make([]orchestration.StepRun, 0, len(input.Plan.Steps))
	instances := make([]orchestration.InstanceRun, 0)
	state.CollaborationRuns[run.ID] = run
	for _, spec := range input.Plan.Steps {
		count := spec.Count
		if count == 0 {
			count = 1
		}
		status := orchestration.StepRunWaiting
		if len(spec.After) == 0 {
			status = orchestration.StepRunReady
		}
		stepRun := orchestration.StepRun{ID: orchestration.NewID("step"), RunID: run.ID, StepID: spec.ID, After: append([]string(nil), spec.After...), RequestedCount: count, Status: status, CreatedAt: now, UpdatedAt: now}
		state.StepRuns[stepRun.ID] = stepRun
		stepRuns = append(stepRuns, stepRun)
		for index := 0; index < count; index++ {
			instance := orchestration.InstanceRun{ID: orchestration.NewID("instance"), RunID: run.ID, StepRunID: stepRun.ID, StepID: spec.ID, Role: spec.Role, Status: orchestration.InstanceWaiting, CreatedAt: now, UpdatedAt: now}
			if status == orchestration.StepRunReady {
				instance.Status = orchestration.InstanceReady
			}
			state.InstanceRuns[instance.ID] = instance
			stepRun.InstanceIDs = append(stepRun.InstanceIDs, instance.ID)
			instances = append(instances, instance)
		}
		state.StepRuns[stepRun.ID] = stepRun
	}
	state.CollaborationRuns[run.ID] = run
	event := orchestration.NewEvent(state.Task.ID, "collaboration.run.created", map[string]any{"run_id": run.ID, "plan_hash": run.PlanHash, "gate_epoch": run.GateEpoch})
	if err := g.saveEvents(state, event); err != nil {
		return CreateCollaborationRunResult{}, err
	}
	entry := storage.ActiveRunEntry{RunID: run.ID, Workspace: run.Workspace, StateDir: input.StateDir, GateEpoch: run.GateEpoch}
	if g.coordinationLocked {
		if err := coord.RegisterRunUnderLock(entry); err != nil {
			return CreateCollaborationRunResult{}, fail("collaboration_run_registration_failed", err.Error(), nil)
		}
	} else if _, err := coord.CreateRun(entry); err != nil {
		return CreateCollaborationRunResult{}, fail("collaboration_run_registration_failed", err.Error(), nil)
	}
	return CreateCollaborationRunResult{Run: run, StepRuns: stepRuns, Instances: instances}, nil
}

func (g *Gateway) RecordCollaborationTechnicalRelease(input TechnicalReleaseInput) (TechnicalReleaseResult, error) {
	if g.coordination != nil {
		if strings.TrimSpace(input.RunID) == "" {
			return TechnicalReleaseResult{}, fail("run_required", "run_id is required", nil)
		}
		// The operation itself resolves and validates the run-scoped authority
		// from core state. It never accepts an arbitrary authority string.
		if _, ok := g.findRun(input.RunID); !ok {
			return TechnicalReleaseResult{}, fail("unknown_collaboration_run", "collaboration run was not found", nil)
		}
	}
	state, err := g.findState(func(candidate storage.State) bool {
		_, ok := candidate.CollaborationRuns[input.RunID]
		return ok
	})
	if err != nil {
		return TechnicalReleaseResult{}, fail("unknown_collaboration_run", "collaboration run was not found", nil)
	}
	run, ok := state.CollaborationRuns[input.RunID]
	if !ok || run.Status != orchestration.CollaborationRunActive {
		return TechnicalReleaseResult{}, fail("collaboration_run_not_active", "collaboration run is not active", nil)
	}
	if strings.TrimSpace(input.InstanceID) == "" {
		return TechnicalReleaseResult{}, fail("instance_required", "instance_id is required", nil)
	}
	instance, ok := state.InstanceRuns[input.InstanceID]
	if !ok || instance.RunID != run.ID {
		return TechnicalReleaseResult{}, fail("unknown_collaboration_instance", "instance does not belong to the collaboration run", nil)
	}
	if instance.AttemptID == "" || instance.NodeID == "" {
		return TechnicalReleaseResult{}, fail("instance_not_executed", "instance is not mapped to a Gateway node and attempt", nil)
	}
	attempt, ok := state.Attempts[instance.AttemptID]
	if !ok || attempt.NodeID != instance.NodeID || attempt.Status != orchestration.AttemptCompleted {
		return TechnicalReleaseResult{}, fail("technical_release_ineligible", "instance Attempt is not a validated completed execution", nil)
	}
	node, ok := state.Nodes[instance.NodeID]
	if !ok || node.CheckpointAttemptID != attempt.ID || node.Status != orchestration.NodeAwaitingHumanDecision {
		return TechnicalReleaseResult{}, fail("technical_release_ineligible", "instance is not at its completion checkpoint", nil)
	}
	decision, ok := latestCompletionDecision(state, attempt.ID)
	if !ok || decision.Status != orchestration.AttemptCompleted || decision.Reason == "delivery_missing" {
		return TechnicalReleaseResult{}, fail("technical_release_ineligible", "only validated completion may receive technical release", nil)
	}
	if len(decision.Artifacts) == 0 && node.ExpectedOutput.Required {
		return TechnicalReleaseResult{}, fail("technical_release_ineligible", "required delivery is missing", nil)
	}
	stepRun, ok := state.StepRuns[instance.StepRunID]
	if !ok || !dependenciesReleased(state, run.ID, stepRun.After) {
		return TechnicalReleaseResult{}, fail("technical_release_dependency_blocked", "upstream collaboration steps are not technically released", nil)
	}
	for _, release := range state.TechnicalReleases {
		if release.InstanceID == instance.ID {
			return TechnicalReleaseResult{}, fail("technical_release_already_recorded", "instance already has a technical release", nil)
		}
	}
	now := time.Now().UTC()
	release := orchestration.CollaborationTechnicalRelease{ID: orchestration.NewID("release"), RunID: run.ID, StepID: instance.StepID, InstanceID: instance.ID, TaskID: run.TaskID, NodeID: instance.NodeID, AttemptID: attempt.ID, CompletionDecisionID: decision.ID, PlanHash: run.PlanHash, AuthorityID: run.SchedulerAuthorityID, Reason: input.Reason, Evidence: append([]string(nil), decision.Evidence...), CreatedAt: now}
	state.TechnicalReleases[release.ID] = release
	instance.Status = orchestration.InstanceTechnicallyDone
	instance.CompletionDecision = decision.ID
	instance.OutputRefs = append([]orchestration.ArtifactRef(nil), decision.Artifacts...)
	instance.UpdatedAt = now
	state.InstanceRuns[instance.ID] = instance
	node.Status = orchestration.NodeTechnicallyDelivered
	node.CheckpointAttemptID = ""
	node.UpdatedAt = now
	state.Nodes[node.ID] = node
	if allStepInstancesReleased(state, stepRun) {
		stepRun.Status = orchestration.StepRunCompleted
		stepRun.UpdatedAt = now
		state.StepRuns[stepRun.ID] = stepRun
	}
	ready := readyInstances(state, run.ID)
	event := orchestration.NewEvent(state.Task.ID, "collaboration.technical_release", map[string]any{"run_id": run.ID, "instance_id": instance.ID, "technical_release_id": release.ID, "authority_id": release.AuthorityID})
	event.NodeID, event.AttemptID = node.ID, attempt.ID
	if err := g.saveEvents(state, event); err != nil {
		return TechnicalReleaseResult{}, err
	}
	return TechnicalReleaseResult{Release: release, Instance: instance, NextReady: ready}, nil
}

func (g *Gateway) findRun(runID string) (orchestration.CollaborationRun, bool) {
	var run orchestration.CollaborationRun
	state, err := g.findState(func(candidate storage.State) bool {
		candidateRun, ok := candidate.CollaborationRuns[runID]
		if ok {
			run = candidateRun
		}
		return ok
	})
	return run, err == nil && state.Task.ID != ""
}

func validateCollaborationPlan(plan orchestration.CollaborationPlan) error {
	if plan.SchemaVersion != "collaboration/v1" {
		return errors.New("plan schema_version must be collaboration/v1")
	}
	seen := make(map[string]struct{}, len(plan.Steps))
	for index, step := range plan.Steps {
		if strings.TrimSpace(step.ID) == "" || strings.TrimSpace(step.Role) == "" || strings.TrimSpace(step.Task) == "" {
			return fmt.Errorf("step %d requires id, role and task", index)
		}
		if _, exists := seen[step.ID]; exists {
			return fmt.Errorf("duplicate step id %q", step.ID)
		}
		seen[step.ID] = struct{}{}
		if step.Count < 0 {
			return fmt.Errorf("step %q count cannot be negative", step.ID)
		}
	}
	for index := range plan.Steps {
		for _, dependency := range plan.Steps[index].After {
			if dependency == plan.Steps[index].ID {
				return fmt.Errorf("step %q cannot depend on itself", dependency)
			}
			if _, exists := seen[dependency]; !exists {
				return fmt.Errorf("step %q depends on unknown step %q", plan.Steps[index].ID, dependency)
			}
		}
	}
	visiting := map[string]bool{}
	visited := map[string]bool{}
	var visit func(string) error
	visit = func(id string) error {
		if visiting[id] {
			return fmt.Errorf("collaboration plan contains a dependency cycle at %q", id)
		}
		if visited[id] {
			return nil
		}
		visiting[id] = true
		for _, step := range plan.Steps {
			if step.ID != id {
				continue
			}
			for _, dependency := range step.After {
				if err := visit(dependency); err != nil {
					return err
				}
			}
		}
		delete(visiting, id)
		visited[id] = true
		return nil
	}
	for id := range seen {
		if err := visit(id); err != nil {
			return err
		}
	}
	return nil
}

func hashJSON(value any) string {
	raw, _ := json.Marshal(value)
	return hashBytes(raw)
}

func dependenciesReleased(state storage.State, runID string, dependencies []string) bool {
	for _, dependency := range dependencies {
		found := false
		for _, step := range state.StepRuns {
			if step.RunID == runID && step.StepID == dependency {
				found = true
				if !allStepInstancesReleased(state, step) {
					return false
				}
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func allStepInstancesReleased(state storage.State, step orchestration.StepRun) bool {
	if len(step.InstanceIDs) == 0 {
		return false
	}
	for _, instanceID := range step.InstanceIDs {
		instance, ok := state.InstanceRuns[instanceID]
		if !ok || instance.Status != orchestration.InstanceTechnicallyDone {
			return false
		}
	}
	return true
}

func readyInstances(state storage.State, runID string) []orchestration.InstanceRun {
	ready := make([]orchestration.InstanceRun, 0)
	for stepID, step := range state.StepRuns {
		if step.RunID != runID || step.Status == orchestration.StepRunCompleted || !dependenciesReleased(state, runID, step.After) {
			continue
		}
		step.Status = orchestration.StepRunReady
		step.UpdatedAt = time.Now().UTC()
		state.StepRuns[stepID] = step
		for _, instanceID := range step.InstanceIDs {
			instance := state.InstanceRuns[instanceID]
			if instance.Status == orchestration.InstanceWaiting {
				instance.Status = orchestration.InstanceReady
				instance.UpdatedAt = step.UpdatedAt
				state.InstanceRuns[instanceID] = instance
				ready = append(ready, instance)
			}
		}
	}
	sort.Slice(ready, func(i, j int) bool { return ready[i].ID < ready[j].ID })
	return ready
}

func (g *Gateway) prepareCollaborationGate(epoch uint64) error {
	coord, err := coordination.NewWithStore(g.store, controlActionLeaseDuration)
	if err != nil {
		return err
	}
	if !g.coordinationLocked {
		if err := coord.Admission(epoch); err != nil {
			return fail("collaboration_gate_rejected", err.Error(), nil)
		}
	}
	return nil
}

func New(store *storage.Store, config policy.Config) (*Gateway, error) {
	if store == nil {
		return nil, errors.New("storage is required")
	}
	config = policy.WithDefaults(config)
	if err := config.Validate(); err != nil {
		return nil, err
	}
	return &Gateway{store: store, config: config, saveReceipt: store.SaveReceipt}, nil
}

func NewWithCoordination(store *storage.Store, config policy.Config, manager *coordination.Manager) (*Gateway, error) {
	gateway, err := New(store, config)
	if err != nil {
		return nil, err
	}
	if manager == nil {
		return nil, errors.New("coordination manager is required")
	}
	gateway.coordination = manager
	return gateway, nil
}

func NewWithCoordinationRoot(store *storage.Store, config policy.Config, root string) (*Gateway, error) {
	manager, err := coordination.New(root, controlActionLeaseDuration)
	if err != nil {
		return nil, err
	}
	gateway, err := NewWithCoordination(store, config, manager)
	if err != nil {
		return nil, err
	}
	gateway.coordinationRoot = root
	return gateway, nil
}

// NewWithCoordinationLocked is used by the one-shot CLI after it has acquired
// the documented gate -> active-run index -> state-dir locks. Collaboration
// admission methods then perform only the non-locking checks and mutations in
// that critical section.
func NewWithCoordinationLocked(store *storage.Store, config policy.Config, manager *coordination.Manager) (*Gateway, error) {
	gateway, err := NewWithCoordination(store, config, manager)
	if err != nil {
		return nil, err
	}
	gateway.coordinationLocked = true
	return gateway, nil
}

func (g *Gateway) Handle(request protocol.Request) (json.RawMessage, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	payloadHash := hashBytes(request.Payload)
	if receipt, found, err := g.store.LoadReceipt(request.RequestID); err != nil {
		return nil, err
	} else if found {
		if receipt.Operation != request.Operation || receipt.PayloadHash != payloadHash {
			return nil, fail("request_id_reused", "request_id was already used with different content", nil)
		}
		switch receipt.Status {
		case storage.RequestCompleted:
			return receipt.Payload, nil
		case storage.RequestFailed:
			return nil, fail(receipt.Failure.Code, receipt.Failure.Message, receipt.Failure.Details)
		case storage.RequestPending, storage.RequestOutcomeUnknown:
			return nil, fail("request_outcome_unknown", "request was started but its durable outcome is not safely replayable", map[string]any{"status": receipt.Status})
		default:
			return nil, fail("request_receipt_invalid", "request receipt has an unsupported status", map[string]any{"status": receipt.Status})
		}
	}
	pending := storage.RequestReceipt{RequestID: request.RequestID, Operation: request.Operation, PayloadHash: payloadHash, Status: storage.RequestPending}
	if err := g.saveReceipt(pending); err != nil {
		return nil, fmt.Errorf("persist pending request journal: %w", err)
	}
	result, err := g.dispatch(request.Operation, request.Payload)
	if err != nil {
		if gatewayErr, ok := err.(*Error); ok {
			failure := storage.RequestReceipt{
				RequestID: request.RequestID, Operation: request.Operation, PayloadHash: payloadHash, Status: storage.RequestFailed,
				Failure: &storage.RequestFailure{Code: gatewayErr.Code, Message: gatewayErr.Message, Details: gatewayErr.Details},
			}
			if receiptErr := g.saveReceipt(failure); receiptErr != nil {
				return nil, errors.Join(err, fmt.Errorf("persist failed request outcome: %w", receiptErr))
			}
			return nil, err
		}
		_ = g.saveReceipt(storage.RequestReceipt{RequestID: request.RequestID, Operation: request.Operation, PayloadHash: payloadHash, Status: storage.RequestOutcomeUnknown})
		return nil, err
	}
	raw, err := json.Marshal(result)
	if err != nil {
		_ = g.saveReceipt(storage.RequestReceipt{RequestID: request.RequestID, Operation: request.Operation, PayloadHash: payloadHash, Status: storage.RequestOutcomeUnknown})
		return nil, fmt.Errorf("marshal gateway result: %w", err)
	}
	completed := storage.RequestReceipt{RequestID: request.RequestID, Operation: request.Operation, PayloadHash: payloadHash, Status: storage.RequestCompleted, Payload: raw}
	if err := g.saveReceipt(completed); err != nil {
		_ = g.saveReceipt(storage.RequestReceipt{RequestID: request.RequestID, Operation: request.Operation, PayloadHash: payloadHash, Status: storage.RequestOutcomeUnknown})
		return nil, fmt.Errorf("persist completed request outcome: %w", err)
	}
	return raw, nil
}

func (g *Gateway) dispatch(operation string, raw json.RawMessage) (any, error) {
	switch operation {
	case "createTask":
		var input CreateTaskInput
		if err := decodePayload(raw, &input); err != nil {
			return nil, err
		}
		return g.CreateTask(input)
	case "proposeNode":
		var input ProposeNodeInput
		if err := decodePayload(raw, &input); err != nil {
			return nil, err
		}
		return g.ProposeNode(input)
	case "runPreflight":
		var input RunPreflightInput
		if err := decodePayload(raw, &input); err != nil {
			return nil, err
		}
		return g.RunPreflight(input)
	case "prepareAttempt":
		var input PrepareAttemptInput
		if err := decodePayload(raw, &input); err != nil {
			return nil, err
		}
		return g.PrepareAttempt(input)
	case "createCollaborationRun":
		var input CreateCollaborationRunInput
		if err := decodePayload(raw, &input); err != nil {
			return nil, err
		}
		return g.CreateCollaborationRun(input)
	case "resolveRole":
		var input ResolveRoleInput
		if err := decodePayload(raw, &input); err != nil {
			return nil, err
		}
		return g.ResolveRole(input)
	case "recordCollaborationTechnicalRelease":
		var input TechnicalReleaseInput
		if err := decodePayload(raw, &input); err != nil {
			return nil, err
		}
		return g.RecordCollaborationTechnicalRelease(input)
	case "prepareRetryAttempt":
		var input RetryAttemptInput
		if err := decodePayload(raw, &input); err != nil {
			return nil, err
		}
		return g.PrepareRetryAttempt(input)
	case "prepareFallbackAttempt":
		var input FallbackAttemptInput
		if err := decodePayload(raw, &input); err != nil {
			return nil, err
		}
		return g.PrepareFallbackAttempt(input)
	case "pollHostActions":
		var input PollHostActionsInput
		if err := decodePayload(raw, &input); err != nil {
			return nil, err
		}
		return g.PollHostActions(input)
	case "submitHostActionResult":
		var input SubmitHostActionResultInput
		if err := decodePayload(raw, &input); err != nil {
			return nil, err
		}
		return g.SubmitHostActionResult(input)
	case "bindProviderHandle":
		var input BindProviderHandleInput
		if err := decodePayload(raw, &input); err != nil {
			return nil, err
		}
		return g.BindProviderHandle(input)
	case "appendEvent":
		var input AppendEventInput
		if err := decodePayload(raw, &input); err != nil {
			return nil, err
		}
		return g.AppendEvent(input)
	case "requestCompletion":
		var input CompletionInput
		if err := decodePayload(raw, &input); err != nil {
			return nil, err
		}
		return g.RequestCompletion(input)
	case "enableGoalMode":
		var input GoalInput
		if err := decodePayload(raw, &input); err != nil {
			return nil, err
		}
		return g.EnableGoalMode(input)
	case "revokeGoalMode":
		var input RevokeGoalInput
		if err := decodePayload(raw, &input); err != nil {
			return nil, err
		}
		return g.RevokeGoalMode(input)
	case "recordHumanDecision":
		var input HumanDecisionInput
		if err := decodePayload(raw, &input); err != nil {
			return nil, err
		}
		return g.RecordHumanDecision(input)
	case "requestCancel":
		var input CancelInput
		if err := decodePayload(raw, &input); err != nil {
			return nil, err
		}
		return g.RequestCancel(input)
	default:
		return nil, fail("unknown_operation", "operation is not implemented by the gateway", map[string]any{"operation": operation})
	}
}

func (g *Gateway) ResolveRole(input ResolveRoleInput) (ResolveRoleResult, error) {
	binding, err := g.config.ResolveRole(input.Role, input.Workspace)
	if err != nil {
		return ResolveRoleResult{}, fail("role_route_missing", err.Error(), nil)
	}
	return ResolveRoleResult{Binding: binding}, nil
}

func (g *Gateway) CreateTask(input CreateTaskInput) (CreateTaskResult, error) {
	task, err := orchestration.NewTask(input.Title, input.Objective, input.Workspace)
	if err != nil {
		return CreateTaskResult{}, fail("invalid_task", err.Error(), nil)
	}
	revision, err := orchestration.NewRevision(task.ID, "", task.Objective)
	if err != nil {
		return CreateTaskResult{}, fail("invalid_task", err.Error(), nil)
	}
	task.CurrentRevisionID = revision.ID
	state := storage.NewState(task, revision)
	event := orchestration.NewEvent(task.ID, "task.created", map[string]any{"revision_id": revision.ID})
	if err := g.saveEvents(state, event); err != nil {
		return CreateTaskResult{}, err
	}
	return CreateTaskResult{Task: task, Revision: revision}, nil
}

func (g *Gateway) ProposeNode(input ProposeNodeInput) (ProposeNodeResult, error) {
	state, err := g.loadTaskFromNode(input.TaskID, input.RevisionID)
	if err != nil {
		return ProposeNodeResult{}, err
	}
	if state.Task.Status != orchestration.TaskActive {
		return ProposeNodeResult{}, fail("checkpoint_required", "the task is waiting for a human decision before another node can be proposed", nil)
	}
	revisionID := input.RevisionID
	if revisionID == "" {
		revisionID = state.Task.CurrentRevisionID
	}
	if _, ok := state.Revisions[revisionID]; !ok {
		return ProposeNodeResult{}, fail("unknown_revision", "revision does not belong to task", nil)
	}
	role := strings.TrimSpace(input.Role)
	if role == "" {
		return ProposeNodeResult{}, fail("role_required", "workflow node role must be explicitly named", nil)
	}
	now := time.Now().UTC()
	kind := strings.TrimSpace(input.Kind)
	status := orchestration.NodeProposed
	preflightStatus, preflightCode := "", ""
	if kind == "" {
		// Preserve the fact that a caller proposed an unnamed node while making
		// the missing semantic boundary visible and permanently non-executable.
		kind = "unspecified"
		status = orchestration.NodePreflightBlocked
		preflightStatus, preflightCode = "blocked", "unspecified_node"
	}
	node := orchestration.WorkflowNode{ID: orchestration.NewID("node"), TaskID: state.Task.ID, RevisionID: revisionID, Kind: kind, Role: role, Status: status, CheckpointMode: state.Task.CheckpointMode, PreflightStatus: preflightStatus, PreflightCode: preflightCode, ExpectedOutput: input.ExpectedOutput, CreatedAt: now, UpdatedAt: now}
	state.Nodes[node.ID] = node
	event := orchestration.NewEvent(state.Task.ID, "node.proposed", map[string]any{"node_id": node.ID, "revision_id": revisionID, "kind": node.Kind, "preflight_code": preflightCode})
	event.NodeID = node.ID
	if err := g.saveEvents(state, event); err != nil {
		return ProposeNodeResult{}, err
	}
	return ProposeNodeResult{Node: node}, nil
}

func (g *Gateway) RunPreflight(input RunPreflightInput) (RunPreflightResult, error) {
	state, node, err := g.loadNode(input.NodeID)
	if err != nil {
		return RunPreflightResult{}, err
	}
	if state.Task.Status != orchestration.TaskActive || node.Status == orchestration.NodeAwaitingHumanDecision {
		return RunPreflightResult{}, fail("checkpoint_required", "the task or node is waiting for a human decision", nil)
	}
	decision := g.config.Preflight(state.Task, input.Binding)
	if node.Kind == "unspecified" {
		decision = policy.PreflightDecision{Status: "blocked", Code: "unspecified_node", Reason: "workflow node kind must be explicitly named", MCPIsolation: selectedMCPIsolation(g.config.MCPIsolation), Binding: input.Binding}
	} else if strings.TrimSpace(node.Role) == "" {
		decision = policy.PreflightDecision{Status: "blocked", Code: "node_role_required", Reason: "workflow node role must be explicitly named", MCPIsolation: selectedMCPIsolation(g.config.MCPIsolation), Binding: input.Binding}
	} else if strings.TrimSpace(input.Binding.Role) != node.Role {
		decision = policy.PreflightDecision{Status: "blocked", Code: "node_role_mismatch", Reason: "binding role must match the workflow node role", MCPIsolation: selectedMCPIsolation(g.config.MCPIsolation), Binding: input.Binding}
	}
	node.PreflightStatus = decision.Status
	node.PreflightCode = decision.Code
	node.PreflightMCPIsolation = selectedMCPIsolation(decision.MCPIsolation)
	node.UpdatedAt = time.Now().UTC()
	if decision.Status == "passed" {
		binding := decision.Binding
		node.ApprovedBinding = &binding
		node.Status = orchestration.NodePrepared
	} else {
		node.Status = orchestration.NodePreflightBlocked
	}
	state.Nodes[node.ID] = node
	event := orchestration.NewEvent(state.Task.ID, "node.preflight", map[string]any{"node_id": node.ID, "status": decision.Status, "code": decision.Code, "reason": decision.Reason, "mcp_isolation": node.PreflightMCPIsolation})
	event.NodeID = node.ID
	if err := g.saveEvents(state, event); err != nil {
		return RunPreflightResult{}, err
	}
	return RunPreflightResult{Node: node, Preflight: decision}, nil
}

func (g *Gateway) PrepareAttempt(input PrepareAttemptInput) (LaunchDecision, error) {
	state, node, err := g.loadNode(input.NodeID)
	if err != nil {
		return LaunchDecision{}, err
	}
	if node.Kind == "unspecified" {
		return LaunchDecision{}, fail("unspecified_node", "an explicitly named workflow node is required before prepareAttempt", nil)
	}
	if state.Task.Status != orchestration.TaskActive || node.Status == orchestration.NodeAwaitingHumanDecision {
		return LaunchDecision{}, fail("checkpoint_required", "the task or node is waiting for a human decision", nil)
	}
	if node.PreflightStatus != "passed" || node.ApprovedBinding == nil {
		return LaunchDecision{}, fail("preflight_required", "runPreflight must pass before prepareAttempt", nil)
	}
	if input.ControllerInstanceID == "" {
		return LaunchDecision{}, fail("controller_required", "controller_instance_id is required", nil)
	}
	if input.Binding != nil && !sameBinding(*input.Binding, *node.ApprovedBinding) {
		return LaunchDecision{}, fail("binding_mismatch", "approved binding cannot be changed during prepareAttempt", nil)
	}
	if input.CollaborationRunID != "" || input.InstanceRunID != "" || input.GateEpoch != 0 {
		if input.CollaborationRunID == "" || input.InstanceRunID == "" || input.GateEpoch == 0 {
			return LaunchDecision{}, fail("collaboration_linkage_required", "collaboration attempt admission requires run_id, instance_id and gate_epoch", nil)
		}
		run, ok := state.CollaborationRuns[input.CollaborationRunID]
		instance, instanceOK := state.InstanceRuns[input.InstanceRunID]
		if !ok || !instanceOK || run.Status != orchestration.CollaborationRunActive || instance.RunID != run.ID || instance.NodeID != "" && instance.NodeID != node.ID || run.GateEpoch != input.GateEpoch {
			return LaunchDecision{}, fail("collaboration_admission_rejected", "collaboration run, instance, node and gate epoch do not match", nil)
		}
		if instance.Status != orchestration.InstanceReady {
			return LaunchDecision{}, fail("collaboration_instance_not_ready", "collaboration instance is not ready", nil)
		}
		if err := g.prepareCollaborationGate(input.GateEpoch); err != nil {
			return LaunchDecision{}, err
		}
	}
	var retryOf string
	if input.PreviousAttemptID != "" {
		previous, ok := state.Attempts[input.PreviousAttemptID]
		if !ok || previous.NodeID != node.ID || previous.RevisionID != node.RevisionID {
			return LaunchDecision{}, fail("invalid_retry_lineage", "previous attempt must belong to the same node and revision", nil)
		}
		contaminationRelease := previous.Status == orchestration.AttemptContaminated && hasContaminationRelease(state, node.ID, previous.ID)
		nodeReady := node.Status == orchestration.NodeProposed || (contaminationRelease && node.Status == orchestration.NodePrepared)
		if !nodeReady || (!hasRetryApproval(state, node.ID) && !contaminationRelease) {
			return LaunchDecision{}, fail("manual_retry_approval_required", "a retry_node or release_contamination HumanDecision is required before manual retry", nil)
		}
		if strings.EqualFold(strings.TrimSpace(previous.SideEffectState), "unknown") {
			return LaunchDecision{}, fail("retry_not_allowed", "manual retry is blocked because the previous action outcome is unknown", nil)
		}
		if previous.Status != orchestration.AttemptFailed && previous.Status != orchestration.AttemptIncomplete && previous.Status != orchestration.AttemptTimedOut && previous.Status != orchestration.AttemptCancelled && previous.Status != orchestration.AttemptCancelUnconfirmed && !contaminationRelease {
			return LaunchDecision{}, fail("invalid_retry_lineage", "previous attempt is not in a retryable terminal state", nil)
		}
		retryOf = previous.ID
	} else if hasAnyAttemptForNode(state, node.ID, node.RevisionID) {
		return LaunchDecision{}, fail("retry_lineage_required", "a subsequent Attempt must reference its predecessor", nil)
	}
	launch, err := g.prepareAttempt(state, node, *node.ApprovedBinding, node.PreflightMCPIsolation, input.ControllerInstanceID, retryOf, "", "attempt.prepared")
	if err != nil {
		return LaunchDecision{}, err
	}
	if input.InstanceRunID != "" {
		instance := state.InstanceRuns[input.InstanceRunID]
		instance.NodeID = launch.Attempt.NodeID
		instance.AttemptID = launch.Attempt.ID
		instance.Binding = launch.Attempt.Binding
		instance.Status = orchestration.InstanceRunning
		instance.UpdatedAt = time.Now().UTC()
		state.InstanceRuns[input.InstanceRunID] = instance
		state.CollaborationRuns[input.CollaborationRunID] = state.CollaborationRuns[input.CollaborationRunID]
		if err := g.saveState(state); err != nil {
			return LaunchDecision{}, err
		}
	}
	return launch, nil
}

// PrepareRetryAttempt is the automatic retry entry point. It creates a new
// immutable Attempt and records its predecessor; it never rewrites the failed
// Attempt. The policy remains the authority for whether the recorded evidence
// is safe to retry.
func (g *Gateway) PrepareRetryAttempt(input RetryAttemptInput) (LaunchDecision, error) {
	state, node, err := g.loadNode(input.NodeID)
	if err != nil {
		return LaunchDecision{}, err
	}
	if node.PreflightStatus != "passed" || node.ApprovedBinding == nil {
		return LaunchDecision{}, fail("preflight_required", "runPreflight must pass before prepareRetryAttempt", nil)
	}
	if state.Task.Status != orchestration.TaskActive || node.Status == orchestration.NodeAwaitingHumanDecision {
		return LaunchDecision{}, fail("checkpoint_required", "the task or node is waiting for a human decision", nil)
	}
	if strings.TrimSpace(input.ControllerInstanceID) == "" {
		return LaunchDecision{}, fail("controller_required", "controller_instance_id is required", nil)
	}
	previous, ok := state.Attempts[input.PreviousAttemptID]
	if !ok || previous.NodeID != node.ID || previous.RevisionID != node.RevisionID {
		return LaunchDecision{}, fail("invalid_retry_lineage", "previous attempt must belong to the same node and revision", nil)
	}
	retriesUsed, retryCountErr := retryCount(state, previous.ID)
	if retryCountErr != nil {
		return LaunchDecision{}, fail("invalid_retry_lineage", retryCountErr.Error(), nil)
	}
	if !g.config.RetryAllowed(previous, input.ErrorCode, retriesUsed) {
		return LaunchDecision{}, fail("retry_not_allowed", "recorded attempt evidence does not satisfy retry policy", map[string]any{"error_code": input.ErrorCode, "retries_already_used": retriesUsed})
	}
	return g.prepareAttempt(state, node, *node.ApprovedBinding, node.PreflightMCPIsolation, input.ControllerInstanceID, previous.ID, "", "attempt.retry_prepared")
}

// PrepareFallbackAttempt creates a new Attempt only after the policy has
// accepted the explicitly registered candidate binding. Fallback never
// changes the original Attempt or silently changes its binding.
func (g *Gateway) PrepareFallbackAttempt(input FallbackAttemptInput) (LaunchDecision, error) {
	state, node, err := g.loadNode(input.NodeID)
	if err != nil {
		return LaunchDecision{}, err
	}
	if node.PreflightStatus != "passed" || node.ApprovedBinding == nil {
		return LaunchDecision{}, fail("preflight_required", "runPreflight must pass before prepareFallbackAttempt", nil)
	}
	if strings.TrimSpace(input.ControllerInstanceID) == "" {
		return LaunchDecision{}, fail("controller_required", "controller_instance_id is required", nil)
	}
	previous, ok := state.Attempts[input.PreviousAttemptID]
	if !ok || previous.NodeID != node.ID || previous.RevisionID != node.RevisionID {
		return LaunchDecision{}, fail("invalid_fallback_lineage", "previous attempt must belong to the same node and revision", nil)
	}
	if state.Task.Status != orchestration.TaskAwaitingHumanDecision || node.Status != orchestration.NodeAwaitingHumanDecision || node.CheckpointAttemptID != previous.ID {
		return LaunchDecision{}, fail("checkpoint_required", "fallback must use the Attempt currently held at the human checkpoint", nil)
	}
	if fallbackID, ok := fallbackAttemptFor(state, node.ID, previous.ID); ok {
		return LaunchDecision{}, fail("fallback_already_prepared", "the checkpoint Attempt already has a fallback Attempt; complete that lineage before preparing another fallback", map[string]any{"fallback_attempt_id": fallbackID})
	}
	if previous.Status != orchestration.AttemptFailed && previous.Status != orchestration.AttemptIncomplete && previous.Status != orchestration.AttemptTimedOut && previous.Status != orchestration.AttemptCancelUnconfirmed {
		return LaunchDecision{}, fail("invalid_fallback_lineage", "previous attempt is not in a fallback-eligible terminal state", nil)
	}
	if strings.EqualFold(previous.SideEffectState, "unknown") {
		return LaunchDecision{}, fail("fallback_not_allowed", "fallback is blocked because the previous action outcome is unknown", nil)
	}
	approved := input.Approved
	if g.config.FallbackMode == "ask" {
		if !hasFallbackApproval(state, node.ID, previous.ID) {
			return LaunchDecision{}, fail("fallback_approval_required", "fallback requires a recorded human approve decision", nil)
		}
		approved = true
	}
	preflight := g.config.Preflight(state.Task, input.Binding)
	if preflight.Status != "passed" {
		return LaunchDecision{}, fail("fallback_preflight_blocked", preflight.Reason, map[string]any{"code": preflight.Code})
	}
	decision := g.config.DecideFallback(previous.Binding, preflight.Binding, approved)
	if decision.Status != "passed" {
		return LaunchDecision{}, fail("fallback_not_allowed", decision.Reason, map[string]any{"code": decision.Code, "status": decision.Status})
	}
	return g.prepareAttempt(state, node, preflight.Binding, preflight.MCPIsolation, input.ControllerInstanceID, "", previous.ID, "attempt.fallback_prepared")
}

func (g *Gateway) prepareAttempt(state storage.State, node orchestration.WorkflowNode, binding orchestration.Binding, mcpIsolation, controllerID, retryOf, fallbackOf, eventType string) (LaunchDecision, error) {
	for _, existing := range state.Attempts {
		if existing.NodeID != node.ID || existing.RevisionID != node.RevisionID {
			continue
		}
		switch existing.Status {
		case orchestration.AttemptPrepared, orchestration.AttemptLaunching, orchestration.AttemptRunning, orchestration.AttemptCancelRequested:
			return LaunchDecision{}, fail("attempt_in_flight", "the node already has an in-flight Attempt", map[string]any{"attempt_id": existing.ID})
		}
	}
	now := time.Now().UTC()
	attempt := orchestration.Attempt{ID: orchestration.NewID("attempt"), TaskID: state.Task.ID, NodeID: node.ID, RevisionID: node.RevisionID, Status: orchestration.AttemptLaunching, Binding: binding, LaunchNonce: orchestration.NewID("nonce"), SideEffectState: "none", ReadOnlyIsolation: binding.Permission == "read-only", MCPIsolation: selectedMCPIsolation(mcpIsolation), RetryOf: retryOf, FallbackOf: fallbackOf, CreatedAt: now, UpdatedAt: now}
	attempt.TaskCanary = attempt.TaskID + ":" + attempt.ID + ":" + attempt.LaunchNonce
	state.Attempts[attempt.ID] = attempt
	parameters := map[string]any{"task_id": state.Task.ID, "attempt_id": attempt.ID, "launch_nonce": attempt.LaunchNonce, "task_canary": attempt.TaskCanary, "binding": attempt.Binding, "expected_output": node.ExpectedOutput, "mcp_isolation": attempt.MCPIsolation}
	if retryOf != "" {
		parameters["retry_of"] = retryOf
	}
	if fallbackOf != "" {
		parameters["fallback_of"] = fallbackOf
	}
	action := g.issueAction(state, attempt, orchestration.ActionLaunch, parameters, controllerID)
	state.Actions[action.ID] = action
	event := orchestration.NewEvent(state.Task.ID, eventType, map[string]any{"attempt_id": attempt.ID, "node_id": node.ID, "action_id": action.ID, "retry_of": retryOf, "fallback_of": fallbackOf, "mcp_isolation": attempt.MCPIsolation})
	event.AttemptID, event.NodeID = attempt.ID, node.ID
	if err := g.saveEvents(state, event); err != nil {
		return LaunchDecision{}, err
	}
	prompt := fmt.Sprintf("我将发送当前任务给 %s（%s / %s / reasoning=%s）进行处理。", attempt.Binding.Role, attempt.Binding.Adapter, attempt.Binding.Model, attempt.Binding.Reasoning)
	return LaunchDecision{Attempt: attempt, LaunchAction: action, DispatchPrompt: prompt}, nil
}

func (g *Gateway) PollHostActions(input PollHostActionsInput) (HostActionBatch, error) {
	state, attempt, err := g.loadAttempt(input.AttemptID)
	if err != nil {
		return HostActionBatch{}, err
	}
	if strings.TrimSpace(input.ControllerInstanceID) == "" {
		return HostActionBatch{}, fail("controller_required", "controller_instance_id is required", nil)
	}
	if input.AfterActionSequence < 0 {
		return HostActionBatch{}, fail("invalid_action_sequence", "after_action_sequence cannot be negative", nil)
	}
	previousStatus := attempt.Status
	actions := make([]orchestration.HostAction, 0)
	expiredEvents := make([]orchestration.Event, 0)
	now := time.Now().UTC()
	for _, action := range state.Actions {
		if action.AttemptID != attempt.ID || action.Sequence <= input.AfterActionSequence || action.Status != orchestration.ActionPending {
			continue
		}
		if attempt.Status == orchestration.AttemptCancelRequested && action.Kind != orchestration.ActionCancelHost {
			continue
		}
		if !knownActionKind(action.Kind) {
			return HostActionBatch{}, fail("unknown_action_kind", "core state contains an unsupported host action kind", map[string]any{"kind": action.Kind})
		}
		if !action.ExpiresAt.IsZero() && now.After(action.ExpiresAt) {
			action.Status = orchestration.ActionExpired
			state.Actions[action.ID] = action
			attempt.Status = orchestration.AttemptIncomplete
			attempt.SideEffectState = "unknown"
			attempt.UpdatedAt = now
			event := g.eventFor(state, "action.lease_expired", attempt.ID, action.ID, map[string]any{"action_id": action.ID, "kind": action.Kind, "sequence": action.Sequence})
			expiredEvents = append(expiredEvents, event)
			continue
		}
		if action.ControllerInstanceID != "" && action.ControllerInstanceID != input.ControllerInstanceID {
			continue
		}
		actions = append(actions, action)
	}
	if len(expiredEvents) > 0 {
		if err := validateAttemptStatusTransition(previousStatus, attempt.Status); err != nil {
			return HostActionBatch{}, err
		}
		state.Attempts[attempt.ID] = attempt
		if err := g.saveEvents(state, expiredEvents...); err != nil {
			return HostActionBatch{}, err
		}
	}
	return HostActionBatch{AttemptID: attempt.ID, Actions: sortActions(actions)}, nil
}

func (g *Gateway) SubmitHostActionResult(input SubmitHostActionResultInput) (ActionReceipt, error) {
	state, attempt, err := g.loadAttempt(input.AttemptID)
	if err != nil {
		return ActionReceipt{}, err
	}
	previousStatus := attempt.Status
	action, ok := state.Actions[input.ActionID]
	if !ok || action.AttemptID != attempt.ID {
		return ActionReceipt{}, fail("unknown_action", "action was not issued by core", nil)
	}
	if !knownActionKind(action.Kind) {
		return ActionReceipt{}, fail("unknown_action_kind", "core state contains an unsupported host action kind", map[string]any{"kind": action.Kind})
	}
	if action.Status == orchestration.ActionComplete {
		if action.Lease != input.Lease {
			return ActionReceipt{}, fail("action_lease_mismatch", "action lease does not match", nil)
		}
		if action.Result != nil && input.Result.Status != "" && !sameActionResult(*action.Result, input.Result) {
			return ActionReceipt{}, fail("action_result_reused", "completed action was submitted with different result content", nil)
		}
		return ActionReceipt{Action: action, Attempt: attempt}, nil
	}
	if action.Status != orchestration.ActionPending {
		return ActionReceipt{}, fail("action_outcome_unknown", "action is no longer safely executable", nil)
	}
	if action.Lease != input.Lease {
		return ActionReceipt{}, fail("action_lease_mismatch", "action lease does not match", nil)
	}
	if strings.TrimSpace(input.Result.Status) == "" {
		return ActionReceipt{}, fail("invalid_action_result", "result.status is required", nil)
	}
	input.Result.SideEffectState = strings.ToLower(strings.TrimSpace(input.Result.SideEffectState))
	if !action.ExpiresAt.IsZero() && time.Now().UTC().After(action.ExpiresAt) {
		action.Status = orchestration.ActionUnknown
		state.Actions[action.ID] = action
		attempt.Status = orchestration.AttemptIncomplete
		attempt.SideEffectState = "unknown"
		attempt.UpdatedAt = time.Now().UTC()
		state.Attempts[attempt.ID] = attempt
		event := g.eventFor(state, "action.outcome_unknown", attempt.ID, action.ID, map[string]any{"action_id": action.ID, "kind": action.Kind})
		if err := g.saveEvents(state, event); err != nil {
			return ActionReceipt{}, err
		}
		return ActionReceipt{}, fail("action_outcome_unknown", "action lease expired before its result was recorded", nil)
	}
	action.Result = &input.Result
	action.Status = orchestration.ActionComplete
	state.Actions[action.ID] = action
	if input.Result.ProviderHandle != "" {
		if attempt.ProviderHandle != "" && attempt.ProviderHandle != input.Result.ProviderHandle {
			attempt.Status = orchestration.AttemptContaminated
		} else {
			attempt.ProviderHandle = input.Result.ProviderHandle
		}
	}
	if input.Result.ProviderSessionID != "" {
		if attempt.ProviderSessionID != "" && attempt.ProviderSessionID != input.Result.ProviderSessionID {
			attempt.Status = orchestration.AttemptContaminated
		} else {
			attempt.ProviderSessionID = input.Result.ProviderSessionID
		}
	}
	if input.Result.Identity != nil {
		if !identityCompatible(attempt, input.Result.Identity) {
			attempt.Status = orchestration.AttemptContaminated
		} else {
			attempt.Identity = copyStringMap(input.Result.Identity)
		}
	}
	if input.Result.TaskCanary != "" {
		if input.Result.TaskCanary != attempt.TaskCanary {
			attempt.Status = orchestration.AttemptContaminated
		} else {
			attempt.CanaryVerified = true
		}
	}
	if input.Result.ExecutionStarted != nil {
		attempt.ExecutionStarted = *input.Result.ExecutionStarted
	}
	if input.Result.SideEffectState != "" {
		attempt.SideEffectState = strings.ToLower(strings.TrimSpace(input.Result.SideEffectState))
	}
	failureLike := input.Result.Status == "failed" || input.Result.Status == "error" || input.Result.Status == "timed_out" || input.Result.Status == "timeout"
	if (action.Kind == orchestration.ActionLaunch || action.Kind == orchestration.ActionObserve) && failureLike && (input.Result.ExecutionStarted == nil || input.Result.SideEffectState == "") {
		// Missing execution evidence is unknown, never an implicit "none".
		// This prevents a failed launch from entering the automatic pre-start
		// retry branch merely because Attempt defaults were conservative.
		attempt.SideEffectState = "unknown"
	}
	evidenceIncomplete := false
	if action.Kind == orchestration.ActionLaunch || action.Kind == orchestration.ActionObserve {
		if !failureLike {
			if input.Result.SideEffectState == "" || (action.Kind == orchestration.ActionLaunch && input.Result.ExecutionStarted == nil) || (input.Result.ProviderHandle == "" && attempt.ProviderHandle == "") || len(input.Result.Identity) == 0 || !identityHasWorkspace(input.Result.Identity) {
				evidenceIncomplete = true
			}
			if action.Kind == orchestration.ActionLaunch && input.Result.TaskCanary == "" {
				evidenceIncomplete = true
			}
			if input.Result.Identity != nil && !identityHasSession(input.Result.Identity) {
				evidenceIncomplete = true
			}
		}
	} else if input.Result.ExecutionStarted == nil && (input.Result.Status == "failed" || input.Result.Status == "error") {
		// An unclassified failure is not safe retry evidence. Preserve the
		// distinction from an explicit execution_started:false report.
		attempt.SideEffectState = "unknown"
	}
	if evidenceIncomplete && attempt.Status != orchestration.AttemptContaminated {
		if previousStatus != orchestration.AttemptCancelRequested {
			attempt.Status = orchestration.AttemptIncomplete
		}
		attempt.SideEffectState = "unknown"
	}
	attempt.UpdatedAt = time.Now().UTC()
	contaminationCheckpoint := attempt.Status == orchestration.AttemptContaminated
	if contaminationCheckpoint {
		freezeContamination(&state, attempt)
	}
	next := make([]orchestration.HostAction, 0)
	switch action.Kind {
	case orchestration.ActionLaunch:
		if attempt.Status == orchestration.AttemptContaminated || attempt.Status == orchestration.AttemptIncomplete || attempt.Status == orchestration.AttemptCancelRequested {
			// Preserve the provider result, but do not issue a follow-up action.
		} else if input.Result.Status == "timed_out" || input.Result.Status == "timeout" {
			attempt.Status = orchestration.AttemptTimedOut
		} else if input.Result.Status == "failed" || input.Result.Status == "error" {
			attempt.Status = orchestration.AttemptFailed
		} else if input.Result.Terminal {
			attempt.Status = orchestration.AttemptCompleted
		} else {
			attempt.Status = orchestration.AttemptRunning
			observe := g.issueAction(state, attempt, orchestration.ActionObserve, map[string]any{"provider_handle": attempt.ProviderHandle}, action.ControllerInstanceID)
			state.Actions[observe.ID] = observe
			next = append(next, observe)
		}
	case orchestration.ActionObserve:
		if attempt.Status == orchestration.AttemptContaminated || attempt.Status == orchestration.AttemptIncomplete || attempt.Status == orchestration.AttemptCancelRequested {
		} else if input.Result.Status == "timed_out" || input.Result.Status == "timeout" {
			attempt.Status = orchestration.AttemptTimedOut
		} else if input.Result.Status == "failed" || input.Result.Status == "error" {
			attempt.Status = orchestration.AttemptFailed
		} else if input.Result.Terminal {
			attempt.Status = orchestration.AttemptCompleted
		} else {
			attempt.Status = orchestration.AttemptRunning
		}
	case orchestration.ActionCancelHost:
		if attempt.Status == orchestration.AttemptContaminated {
			// Preserve contamination discovered while acknowledging cancellation.
		} else if input.Result.Status == "cancelled" || input.Result.Status == "stopped" {
			attempt.Status = orchestration.AttemptCancelled
		} else {
			attempt.Status = orchestration.AttemptCancelUnconfirmed
		}
	}
	state.Attempts[attempt.ID] = attempt
	if err := validateAttemptStatusTransition(previousStatus, attempt.Status); err != nil {
		return ActionReceipt{}, err
	}
	event := g.eventFor(state, "host_action.result", attempt.ID, action.ID, map[string]any{
		"action_id":                  action.ID,
		"kind":                       action.Kind,
		"status":                     input.Result.Status,
		"reason":                     input.Result.Reason,
		"terminal":                   input.Result.Terminal,
		"identity_present":           len(input.Result.Identity) > 0,
		"provider_session_present":   input.Result.ProviderSessionID != "" || identityHasSession(input.Result.Identity),
		"task_canary_present":        input.Result.TaskCanary != "",
		"execution_evidence_present": input.Result.ExecutionStarted != nil && input.Result.SideEffectState != "",
		"mcp_isolation":              action.Parameters["mcp_isolation"],
		"contamination_checkpoint":   contaminationCheckpoint,
		"raw_event_refs":             append([]string(nil), input.Result.RawEventRefs...),
	})
	if input.Result.Terminal && (action.Kind == orchestration.ActionLaunch || action.Kind == orchestration.ActionObserve) && attempt.Status == orchestration.AttemptCompleted {
		attempt.TerminalEventID = event.ID
		state.Attempts[attempt.ID] = attempt
		event.Data["terminal_event_id"] = event.ID
	}
	if err := g.saveEvents(state, event); err != nil {
		return ActionReceipt{}, err
	}
	return ActionReceipt{Action: action, Attempt: attempt, Next: next}, nil
}

func (g *Gateway) BindProviderHandle(input BindProviderHandleInput) (map[string]any, error) {
	state, attempt, err := g.loadAttempt(input.AttemptID)
	if err != nil {
		return nil, err
	}
	if attempt.Status == orchestration.AttemptContaminated {
		return nil, fail("attempt_contaminated", "provider handle cannot be bound to a contaminated Attempt; prepare a new released lineage", nil)
	}
	launchAction, ok := state.Actions[input.LaunchActionID]
	if !ok || launchAction.AttemptID != attempt.ID || launchAction.Kind != orchestration.ActionLaunch || launchAction.Status != orchestration.ActionComplete {
		return nil, fail("invalid_launch_action", "provider handle must be bound to a completed core launch action", nil)
	}
	if input.ProviderHandle == "" {
		return nil, fail("invalid_provider_handle", "provider_handle is required", nil)
	}
	if launchAction.Result == nil || (launchAction.Result.TaskCanary != "" && launchAction.Result.TaskCanary != attempt.TaskCanary) {
		if err := validateAttemptStatusTransition(attempt.Status, orchestration.AttemptContaminated); err != nil {
			return nil, err
		}
		attempt.Status = orchestration.AttemptContaminated
		attempt.UpdatedAt = time.Now().UTC()
		state.Attempts[attempt.ID] = attempt
		freezeContamination(&state, attempt)
		event := g.eventFor(state, "provider.identity_mismatch", attempt.ID, input.LaunchActionID, map[string]any{"reason": "launch action canary does not match the Attempt", "contamination_checkpoint": true})
		if err := g.saveEvents(state, event); err != nil {
			return nil, err
		}
		return nil, fail("identity_mismatch", "launch action canary does not match the Attempt", nil)
	}
	if !identityCompatible(attempt, input.Identity) {
		if err := validateAttemptStatusTransition(attempt.Status, orchestration.AttemptContaminated); err != nil {
			return nil, err
		}
		attempt.Status = orchestration.AttemptContaminated
		attempt.UpdatedAt = time.Now().UTC()
		state.Attempts[attempt.ID] = attempt
		freezeContamination(&state, attempt)
		event := g.eventFor(state, "provider.identity_mismatch", attempt.ID, input.LaunchActionID, map[string]any{"reason": "provider identity does not match the Attempt binding", "contamination_checkpoint": true})
		if err := g.saveEvents(state, event); err != nil {
			return nil, err
		}
		return nil, fail("identity_mismatch", "provider identity does not match the Attempt binding", nil)
	}
	attempt.ProviderHandle = input.ProviderHandle
	attempt.ProviderSessionID = input.ProviderSessionID
	attempt.Identity = copyStringMap(input.Identity)
	state.Attempts[attempt.ID] = attempt
	event := g.eventFor(state, "provider.handle_bound", attempt.ID, input.LaunchActionID, map[string]any{"provider_handle": input.ProviderHandle, "provider_session_id": input.ProviderSessionID})
	if err := g.saveEvents(state, event); err != nil {
		return nil, err
	}
	return map[string]any{"attempt_id": attempt.ID, "provider_handle": attempt.ProviderHandle, "identity": attempt.Identity}, nil
}

func (g *Gateway) AppendEvent(input AppendEventInput) (map[string]any, error) {
	state, _, err := g.loadAttempt(input.AttemptID)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(input.Type) == "" {
		return nil, fail("invalid_event", "type is required", nil)
	}
	event := g.eventFor(state, input.Type, input.AttemptID, input.CausalActionID, input.Data)
	event.NodeID, event.Sequence = input.NodeID, input.Sequence
	if err := g.saveEvents(state, event); err != nil {
		return nil, err
	}
	return map[string]any{"event_id": event.ID, "task_id": event.TaskID, "sequence": event.Sequence}, nil
}

func (g *Gateway) RequestCompletion(input CompletionInput) (CompletionResult, error) {
	state, attempt, err := g.loadAttempt(input.AttemptID)
	if err != nil {
		return CompletionResult{}, err
	}
	node, ok := state.Nodes[attempt.NodeID]
	if !ok {
		return CompletionResult{}, fail("unknown_node", "node was not found", nil)
	}
	if node.Status == orchestration.NodeAwaitingHumanDecision && node.CheckpointAttemptID == attempt.ID {
		return CompletionResult{}, fail("completion_already_recorded", "the current Attempt already has a human checkpoint; record request_revision or abort instead of recomputing completion", map[string]any{"attempt_id": attempt.ID})
	}
	if input.CollaborationRunID != "" || input.InstanceRunID != "" {
		if input.CollaborationRunID == "" || input.InstanceRunID == "" {
			return CompletionResult{}, fail("collaboration_linkage_required", "collaboration completion requires run_id and instance_id", nil)
		}
		instance, ok := state.InstanceRuns[input.InstanceRunID]
		if !ok || instance.RunID != input.CollaborationRunID || instance.AttemptID != attempt.ID {
			return CompletionResult{}, fail("collaboration_admission_rejected", "completion linkage does not match the Attempt", nil)
		}
	}
	decision := orchestration.CompletionDecision{ID: orchestration.NewID("completion"), AttemptID: attempt.ID, Status: attempt.Status, CreatedAt: time.Now().UTC()}
	if attempt.Status != orchestration.AttemptCompleted {
		decision.Status = orchestration.AttemptIncomplete
		decision.Reason = latestAttemptOutcomeReason(state, attempt.ID)
		if decision.Reason == "" {
			decision.Reason = "attempt_not_terminal_completed"
		}
	} else if attempt.ProviderHandle == "" || len(attempt.Identity) == 0 {
		decision.Status = orchestration.AttemptIncomplete
		decision.Reason = "identity_or_provider_handle_missing"
	} else if !attempt.CanaryVerified {
		decision.Status = orchestration.AttemptIncomplete
		decision.Reason = "task_canary_unverified"
	} else if attempt.TerminalEventID == "" || !hasTerminalEvent(state, attempt) {
		decision.Status = orchestration.AttemptIncomplete
		decision.Reason = "terminal_event_missing"
	} else {
		if node.ExpectedOutput.Required {
			artifact, artifactErr := validateExpectedArtifact(node.ExpectedOutput, state.Task.Workspace)
			if artifactErr != nil {
				decision.Status = orchestration.AttemptIncomplete
				decision.Reason = "delivery_missing"
			} else {
				attempt.Artifacts = appendOrReplaceArtifact(attempt.Artifacts, artifact)
				state.Attempts[attempt.ID] = attempt
				decision.Artifacts = []orchestration.ArtifactRef{artifact}
			}
		}
		if decision.Status == orchestration.AttemptCompleted {
			decision.Reason = "terminal_event_identity_and_output_validated"
			decision.Evidence = []string{"terminal_action_result", "provider_identity", "task_canary", "launch_ticket"}
			if len(decision.Artifacts) > 0 {
				decision.Evidence = append(decision.Evidence, "expected_output_artifact")
			}
			node.Status = orchestration.NodeAwaitingHumanDecision
			node.CheckpointAttemptID = attempt.ID
			state.Task.Status = orchestration.TaskAwaitingHumanDecision
		}
	}
	if decision.Status != orchestration.AttemptCompleted && terminalAttemptStatus(attempt.Status) && attempt.Status != orchestration.AttemptContaminated {
		node.Status = orchestration.NodeAwaitingHumanDecision
		node.CheckpointAttemptID = attempt.ID
		state.Task.Status = orchestration.TaskAwaitingHumanDecision
	}
	state.Completions[decision.ID] = decision
	state.Nodes[node.ID] = node
	event := g.eventFor(state, "completion.recorded", attempt.ID, "", map[string]any{"completion_decision_id": decision.ID, "status": decision.Status, "reason": decision.Reason})
	event.NodeID = node.ID
	if err := g.saveEvents(state, event); err != nil {
		return CompletionResult{}, err
	}
	return CompletionResult{Decision: decision, Node: node}, nil
}

func (g *Gateway) EnableGoalMode(input GoalInput) (GoalResult, error) {
	if !g.config.GoalEnabled {
		return GoalResult{}, fail("goal_disabled", "goal mode is disabled by policy", nil)
	}
	state, err := g.store.Load(input.TaskID)
	if err != nil {
		return GoalResult{}, fail("unknown_task", "task was not found", nil)
	}
	if len(input.Scope) == 0 || strings.TrimSpace(input.UserEvidence) == "" {
		return GoalResult{}, fail("goal_authorization_required", "scope and user authorization evidence are required", nil)
	}
	if err := policy.ValidateGoalScope(input.Scope); err != nil {
		return GoalResult{}, fail("goal_scope_invalid", err.Error(), nil)
	}
	scopeMatchesNode := false
	for _, node := range state.Nodes {
		if node.RevisionID == state.Task.CurrentRevisionID && policy.GoalScopeAllows(input.Scope, node) {
			scopeMatchesNode = true
			break
		}
	}
	if !scopeMatchesNode {
		return GoalResult{}, fail("goal_scope_invalid", "goal scope does not match an explicit node in the current task revision", nil)
	}
	scope := make([]string, len(input.Scope))
	for index, value := range input.Scope {
		scope[index] = strings.TrimSpace(value)
	}
	authorization := orchestration.GoalAuthorization{ID: orchestration.NewID("goal"), TaskID: state.Task.ID, RevisionID: state.Task.CurrentRevisionID, Scope: scope, UserEvidence: input.UserEvidence, Active: true, CreatedAt: time.Now().UTC()}
	state.GoalAuthorizations[authorization.ID] = authorization
	event := g.eventFor(state, "goal.enabled", "", "", map[string]any{"goal_authority_id": authorization.ID, "revision_id": authorization.RevisionID})
	if err := g.saveEvents(state, event); err != nil {
		return GoalResult{}, err
	}
	return GoalResult{Authorization: authorization}, nil
}

func (g *Gateway) RevokeGoalMode(input RevokeGoalInput) (GoalResult, error) {
	state, err := g.store.Load(input.TaskID)
	if err != nil {
		return GoalResult{}, fail("unknown_task", "task was not found", nil)
	}
	authorization, ok := state.GoalAuthorizations[input.GoalAuthorityID]
	if !ok {
		return GoalResult{}, fail("unknown_goal_authority", "goal authority was not found", nil)
	}
	if !authorization.Active {
		return GoalResult{Authorization: authorization}, nil
	}
	authorization.Active = false
	authorization.RevokedAt = time.Now().UTC()
	authorization.RevokeReason = input.Reason
	state.GoalAuthorizations[authorization.ID] = authorization
	event := g.eventFor(state, "goal.revoked", "", "", map[string]any{"goal_authority_id": authorization.ID, "reason": input.Reason})
	if err := g.saveEvents(state, event); err != nil {
		return GoalResult{}, err
	}
	return GoalResult{Authorization: authorization}, nil
}

func (g *Gateway) RecordHumanDecision(input HumanDecisionInput) (HumanDecisionResult, error) {
	state, node, err := g.loadNode(input.NodeID)
	if err != nil {
		return HumanDecisionResult{}, err
	}
	if input.Decision != "approve" && input.Decision != "request_revision" && input.Decision != "retry_node" && input.Decision != "abort" && input.Decision != "release_contamination" {
		return HumanDecisionResult{}, fail("invalid_human_decision", "unsupported decision", nil)
	}
	if strings.TrimSpace(input.AuthorityRef) == "" {
		return HumanDecisionResult{}, fail("authority_required", "authority_ref is required", nil)
	}
	if !strings.HasPrefix(input.AuthorityRef, "goal:") && !strings.HasPrefix(input.AuthorityRef, "user-input:") {
		return HumanDecisionResult{}, fail("invalid_authority_ref", "authority_ref must reference user-input or core goal authority", nil)
	}
	if node.Status != orchestration.NodeAwaitingHumanDecision {
		return HumanDecisionResult{}, fail("checkpoint_required", "node is not awaiting human decision", nil)
	}
	contaminated := hasUnreleasedContamination(state, node.ID)
	if contaminated && input.Decision != "release_contamination" && input.Decision != "request_revision" && input.Decision != "abort" {
		return HumanDecisionResult{}, fail("contamination_release_required", "contaminated execution requires an explicit release_contamination, request_revision or abort decision", nil)
	}
	if input.Decision == "retry_node" {
		checkpointAttempt, ok := state.Attempts[node.CheckpointAttemptID]
		if !ok || checkpointAttempt.NodeID != node.ID {
			return HumanDecisionResult{}, fail("retry_not_allowed", "retry_node requires the current checkpoint Attempt", nil)
		}
		if checkpointAttempt.Status == orchestration.AttemptCompleted {
			return HumanDecisionResult{}, fail("retry_not_allowed", "a completed Attempt cannot be retried; use request_revision or abort for missing delivery", map[string]any{"attempt_id": checkpointAttempt.ID})
		}
	}
	fallbackApproval := false
	if input.Decision == "approve" && strings.TrimSpace(input.AttemptID) != "" {
		if !validFallbackApproval(state, node, input.AttemptID) {
			return HumanDecisionResult{}, fail("invalid_fallback_approval", "attempt-bound approve must reference the failed Attempt at the current checkpoint", nil)
		}
		fallbackApproval = true
	}
	if input.Decision == "approve" && !fallbackApproval && node.CheckpointAttemptID != "" {
		checkpointAttempt, ok := state.Attempts[node.CheckpointAttemptID]
		latestCompletion, completionFound := latestCompletionDecision(state, node.CheckpointAttemptID)
		if ok && checkpointAttempt.NodeID == node.ID && checkpointAttempt.Status == orchestration.AttemptCompleted && completionFound && latestCompletion.Reason == "delivery_missing" {
			return HumanDecisionResult{}, fail("delivery_revision_required", "delivery_missing cannot be approved or retried in place; use request_revision or abort", map[string]any{"attempt_id": checkpointAttempt.ID})
		}
		if !ok || checkpointAttempt.NodeID != node.ID || checkpointAttempt.Status != orchestration.AttemptCompleted || !completionFound || latestCompletion.Status != orchestration.AttemptCompleted {
			return HumanDecisionResult{}, fail("fallback_attempt_required", "a failed or incomplete checkpoint requires an attempt-bound fallback approval, not ordinary approval", nil)
		}
	}
	if input.Decision == "release_contamination" {
		attempt, ok := state.Attempts[input.AttemptID]
		if !ok || attempt.NodeID != node.ID || attempt.Status != orchestration.AttemptContaminated || !hasUnreleasedContaminationForAttempt(state, node.ID, attempt.ID) {
			return HumanDecisionResult{}, fail("contamination_not_found", "release_contamination requires an unreleased contaminated Attempt for this node", nil)
		}
		if !strings.HasPrefix(input.AuthorityRef, "user-input:") {
			return HumanDecisionResult{}, fail("contamination_release_authority_required", "contamination release requires direct user-input authority", nil)
		}
	}
	if strings.HasPrefix(input.AuthorityRef, "goal:") {
		goalID := strings.TrimPrefix(input.AuthorityRef, "goal:")
		goal, ok := state.GoalAuthorizations[goalID]
		if !ok || !goal.Active || goal.RevisionID != state.Task.CurrentRevisionID || goal.RevisionID != node.RevisionID || !policy.GoalScopeAllows(goal.Scope, node) {
			return HumanDecisionResult{}, fail("invalid_goal_authority", "goal authority is not valid for this task revision", nil)
		}
	}
	if input.Decision == "request_revision" && strings.TrimSpace(input.Instruction) == "" {
		return HumanDecisionResult{}, fail("revision_instruction_required", "instruction is required for request_revision", nil)
	}
	decision := orchestration.HumanDecision{ID: orchestration.NewID("decision"), NodeID: node.ID, AttemptID: input.AttemptID, Decision: input.Decision, AuthorityRef: input.AuthorityRef, Instruction: input.Instruction, CreatedAt: time.Now().UTC()}
	state.HumanDecisions = append(state.HumanDecisions, decision)
	var newNode *orchestration.WorkflowNode
	switch input.Decision {
	case "approve":
		if fallbackApproval {
			// An attempt-bound approve authorizes only the candidate fallback.
			// Keep the checkpoint active so no downstream node can be proposed
			// until that fallback itself reaches completion and is approved.
			node.Status = orchestration.NodeAwaitingHumanDecision
			state.Task.Status = orchestration.TaskAwaitingHumanDecision
		} else {
			node.Status = orchestration.NodeApproved
			node.CheckpointAttemptID = ""
			state.Task.Status = orchestration.TaskActive
		}
	case "retry_node":
		node.Status = orchestration.NodeProposed
		node.CheckpointAttemptID = ""
		state.Task.Status = orchestration.TaskActive
	case "release_contamination":
		// Releasing contamination is intentionally distinct from approving a
		// normal completion. It reopens the workflow only after direct human
		// acknowledgement and forces a fresh preflight before this node can be
		// prepared again.
		node.PreflightStatus = ""
		node.PreflightCode = ""
		node.PreflightMCPIsolation = ""
		node.CheckpointAttemptID = ""
		node.ApprovedBinding = nil
		if hasUnreleasedContamination(state, node.ID) {
			node.Status = orchestration.NodeAwaitingHumanDecision
			state.Task.Status = orchestration.TaskAwaitingHumanDecision
		} else {
			node.Status = orchestration.NodeProposed
			state.Task.Status = orchestration.TaskActive
		}
	case "abort":
		node.Status = orchestration.NodeAborted
		node.CheckpointAttemptID = ""
		state.Task.Status = orchestration.TaskAborted
	case "request_revision":
		revision, revisionErr := orchestration.NewRevision(state.Task.ID, state.Task.CurrentRevisionID, input.Instruction)
		if revisionErr != nil {
			return HumanDecisionResult{}, fail("invalid_revision", revisionErr.Error(), nil)
		}
		state.Revisions[revision.ID] = revision
		state.Task.CurrentRevisionID = revision.ID
		node.Status = orchestration.NodeRevisionRequested
		created := node
		created.ID = orchestration.NewID("node")
		created.RevisionID = revision.ID
		created.Status = orchestration.NodeProposed
		created.PreflightStatus = ""
		created.PreflightCode = ""
		created.PreflightMCPIsolation = ""
		created.CheckpointAttemptID = ""
		created.ApprovedBinding = nil
		created.CreatedAt = time.Now().UTC()
		created.UpdatedAt = created.CreatedAt
		state.Nodes[created.ID] = created
		newNode = &created
		state.Task.Status = orchestration.TaskActive
	}
	state.Nodes[node.ID] = node
	event := g.eventFor(state, "human.decision", decision.AttemptID, "", map[string]any{"node_id": node.ID, "attempt_id": decision.AttemptID, "decision": decision.Decision, "new_node_id": nodeID(newNode)})
	event.NodeID = node.ID
	if err := g.saveEvents(state, event); err != nil {
		return HumanDecisionResult{}, err
	}
	return HumanDecisionResult{Decision: decision, Node: node, NewNode: newNode}, nil
}

func (g *Gateway) RequestCancel(input CancelInput) (CancelResult, error) {
	state, attempt, err := g.loadAttempt(input.AttemptID)
	if err != nil {
		return CancelResult{}, err
	}
	if attempt.Status != orchestration.AttemptPrepared && attempt.Status != orchestration.AttemptLaunching && attempt.Status != orchestration.AttemptRunning {
		return CancelResult{}, fail("cancel_not_allowed", "attempt is not cancellable", nil)
	}
	if err := validateAttemptStatusTransition(attempt.Status, orchestration.AttemptCancelRequested); err != nil {
		return CancelResult{}, err
	}
	attempt.Status = orchestration.AttemptCancelRequested
	action := g.issueAction(state, attempt, orchestration.ActionCancelHost, map[string]any{"provider_handle": attempt.ProviderHandle, "reason": input.Reason}, "")
	state.Attempts[attempt.ID] = attempt
	state.Actions[action.ID] = action
	event := g.eventFor(state, "cancel.requested", attempt.ID, action.ID, map[string]any{"reason": input.Reason, "action_id": action.ID})
	if err := g.saveEvents(state, event); err != nil {
		return CancelResult{}, err
	}
	return CancelResult{Attempt: attempt, Action: action}, nil
}

func (g *Gateway) loadNode(nodeID string) (storage.State, orchestration.WorkflowNode, error) {
	if strings.TrimSpace(nodeID) == "" {
		return storage.State{}, orchestration.WorkflowNode{}, fail("node_required", "node_id is required", nil)
	}
	// Node IDs are not task IDs. The first Wave 1 storage layout is task-scoped,
	// so find the owning task by scanning the small local task directory.
	state, err := g.findState(func(candidate storage.State) bool { _, ok := candidate.Nodes[nodeID]; return ok })
	if err != nil {
		return storage.State{}, orchestration.WorkflowNode{}, err
	}
	node := state.Nodes[nodeID]
	return state, node, nil
}

func (g *Gateway) loadAttempt(attemptID string) (storage.State, orchestration.Attempt, error) {
	if strings.TrimSpace(attemptID) == "" {
		return storage.State{}, orchestration.Attempt{}, fail("attempt_required", "attempt_id is required", nil)
	}
	state, err := g.findState(func(candidate storage.State) bool {
		_, ok := candidate.Attempts[attemptID]
		return ok
	})
	if err != nil {
		return storage.State{}, orchestration.Attempt{}, fail("unknown_attempt", "attempt was not found", nil)
	}
	attempt, ok := state.Attempts[attemptID]
	if !ok {
		return storage.State{}, orchestration.Attempt{}, fail("unknown_attempt", "attempt was not found", nil)
	}
	return state, attempt, nil
}

func (g *Gateway) loadTaskFromNode(taskID, revisionID string) (storage.State, error) {
	if strings.TrimSpace(taskID) == "" {
		return storage.State{}, fail("task_required", "task_id is required", nil)
	}
	state, err := g.store.Load(taskID)
	if err != nil {
		return storage.State{}, fail("unknown_task", "task was not found", nil)
	}
	if revisionID != "" {
		if _, ok := state.Revisions[revisionID]; !ok {
			return storage.State{}, fail("unknown_revision", "revision does not belong to task", nil)
		}
	}
	return state, nil
}

func (g *Gateway) findState(predicate func(storage.State) bool) (storage.State, error) {
	entries, err := filepath.Glob(filepath.Join(g.store.Root(), "tasks", "*", "snapshot.json"))
	if err != nil {
		return storage.State{}, err
	}
	for _, path := range entries {
		taskID := filepath.Base(filepath.Dir(path))
		state, loadErr := g.store.Load(taskID)
		if loadErr == nil && predicate(state) {
			return state, nil
		}
	}
	return storage.State{}, fail("unknown_node", "node was not found", nil)
}

func (g *Gateway) saveEvents(state storage.State, events ...orchestration.Event) error {
	for _, event := range events {
		state.Events = append(state.Events, event)
		if err := g.store.SaveWithEvent(state, event); err != nil {
			return err
		}
	}
	return nil
}

func (g *Gateway) eventFor(state storage.State, eventType, attemptID, actionID string, data map[string]any) orchestration.Event {
	event := orchestration.NewEvent(state.Task.ID, eventType, data)
	event.AttemptID, event.CausalActionID = attemptID, actionID
	return event
}

func (g *Gateway) issueAction(state storage.State, attempt orchestration.Attempt, kind orchestration.ActionKind, parameters map[string]any, controllerID string) orchestration.HostAction {
	if parameters == nil {
		parameters = map[string]any{}
	}
	if _, ok := parameters["task_canary"]; !ok {
		parameters["task_canary"] = attempt.TaskCanary
	}
	if _, ok := parameters["workspace"]; !ok {
		parameters["workspace"] = attempt.Binding.Workspace
	}
	if _, ok := parameters["mcp_isolation"]; !ok && attempt.MCPIsolation != "" {
		parameters["mcp_isolation"] = attempt.MCPIsolation
	}
	sequence := 0
	for _, existing := range state.Actions {
		if existing.AttemptID == attempt.ID && existing.Sequence > sequence {
			sequence = existing.Sequence
		}
	}
	now := time.Now().UTC()
	leaseDuration := controlActionLeaseDuration
	if kind == orchestration.ActionObserve {
		// Observe may synchronously wait for a real reviewer/provider turn. Keep
		// launch and cancellation leases short, but cover the public review
		// harness's ten-minute execution window without forcing a false unknown.
		leaseDuration = observeActionLeaseDuration
	}
	return orchestration.HostAction{ID: orchestration.NewID("action"), AttemptID: attempt.ID, Kind: kind, Sequence: sequence + 1, Lease: orchestration.NewID("lease"), ControllerInstanceID: controllerID, Parameters: parameters, ParametersHash: orchestration.HashParameters(parameters), Status: orchestration.ActionPending, IssuedAt: now, ExpiresAt: now.Add(leaseDuration)}
}

func (g *Gateway) saveState(state storage.State) error { return g.store.Save(state) }

func decodePayload(raw json.RawMessage, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fail("invalid_payload", err.Error(), nil)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return fail("invalid_payload", "payload contains trailing JSON", nil)
	}
	return nil
}

func hashBytes(raw []byte) string { sum := sha256.Sum256(raw); return hex.EncodeToString(sum[:]) }

func selectedMCPIsolation(value string) string {
	if strings.TrimSpace(value) == "" {
		return "strict"
	}
	return value
}

func sameBinding(left, right orchestration.Binding) bool {
	return left.Role == right.Role && left.Adapter == right.Adapter && left.Model == right.Model && left.Reasoning == right.Reasoning && left.Permission == right.Permission && canonicalPathForCompare(left.Workspace) == canonicalPathForCompare(right.Workspace) && strings.Join(left.MCPAllowlist, "\x00") == strings.Join(right.MCPAllowlist, "\x00")
}

func canonicalPathForCompare(path string) string {
	if canonical, err := orchestration.CanonicalDirectory(path); err == nil {
		return canonical
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return filepath.Clean(path)
	}
	return filepath.Clean(abs)
}

func copyStringMap(input map[string]string) map[string]string {
	if input == nil {
		return nil
	}
	output := make(map[string]string, len(input))
	for key, value := range input {
		output[key] = value
	}
	return output
}

func knownActionKind(kind orchestration.ActionKind) bool {
	switch kind {
	case orchestration.ActionLaunch, orchestration.ActionObserve, orchestration.ActionCancelHost:
		return true
	default:
		return false
	}
}

func validateAttemptStatusTransition(from, to orchestration.AttemptStatus) *Error {
	if err := orchestration.ValidateAttemptTransition(from, to); err != nil {
		return fail("invalid_attempt_transition", err.Error(), map[string]any{"from": from, "to": to})
	}
	return nil
}

func sameActionResult(left, right orchestration.HostActionResult) bool {
	return reflect.DeepEqual(left, right)
}

func identityHasSession(identity map[string]string) bool {
	return strings.TrimSpace(identity["session"]) != ""
}

func identityHasWorkspace(identity map[string]string) bool {
	return strings.TrimSpace(identity["workspace"]) != ""
}

func identityCompatible(attempt orchestration.Attempt, identity map[string]string) bool {
	if len(identity) == 0 || strings.TrimSpace(identity["provider"]) == "" || !identityHasSession(identity) || !identityHasWorkspace(identity) {
		return false
	}
	if normalizeIdentity(identity["provider"]) != normalizeIdentity(attempt.Binding.Adapter) {
		return false
	}
	if attempt.ProviderSessionID != "" && attempt.ProviderSessionID != identity["session"] {
		return false
	}
	if existingProvider := attempt.Identity["provider"]; existingProvider != "" && normalizeIdentity(existingProvider) != normalizeIdentity(identity["provider"]) {
		return false
	}
	if existingSession := attempt.Identity["session"]; existingSession != "" && existingSession != identity["session"] {
		return false
	}
	if workspace := strings.TrimSpace(identity["workspace"]); workspace != "" {
		canonicalIdentityWorkspace, err := orchestration.CanonicalDirectory(workspace)
		if err != nil || filepath.Clean(canonicalIdentityWorkspace) != filepath.Clean(attempt.Binding.Workspace) {
			return false
		}
	}
	return true
}

func normalizeIdentity(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var builder strings.Builder
	for _, character := range value {
		if (character >= 'a' && character <= 'z') || (character >= '0' && character <= '9') {
			builder.WriteRune(character)
		}
	}
	return builder.String()
}

func hasTerminalEvent(state storage.State, attempt orchestration.Attempt) bool {
	for _, event := range state.Events {
		if event.ID != attempt.TerminalEventID || event.AttemptID != attempt.ID || event.Type != "host_action.result" {
			continue
		}
		terminal, ok := event.Data["terminal"].(bool)
		return ok && terminal
	}
	return false
}

func latestAttemptOutcomeReason(state storage.State, attemptID string) string {
	for index := len(state.Events) - 1; index >= 0; index-- {
		event := state.Events[index]
		if event.AttemptID != attemptID || event.Type != "host_action.result" {
			continue
		}
		if reason, ok := event.Data["reason"].(string); ok && strings.TrimSpace(reason) != "" {
			return strings.TrimSpace(reason)
		}
	}
	return ""
}

func hasRetryApproval(state storage.State, nodeID string) bool {
	for index := len(state.HumanDecisions) - 1; index >= 0; index-- {
		decision := state.HumanDecisions[index]
		if decision.NodeID != nodeID {
			continue
		}
		return decision.Decision == "retry_node"
	}
	return false
}

func hasFallbackApproval(state storage.State, nodeID, attemptID string) bool {
	for index := len(state.HumanDecisions) - 1; index >= 0; index-- {
		decision := state.HumanDecisions[index]
		if decision.NodeID != nodeID || decision.AttemptID != attemptID {
			continue
		}
		return decision.Decision == "approve"
	}
	return false
}

func fallbackAttemptFor(state storage.State, nodeID, previousAttemptID string) (string, bool) {
	for _, attempt := range state.Attempts {
		if attempt.NodeID == nodeID && attempt.FallbackOf == previousAttemptID {
			return attempt.ID, true
		}
	}
	return "", false
}

func validFallbackApproval(state storage.State, node orchestration.WorkflowNode, attemptID string) bool {
	if strings.TrimSpace(attemptID) == "" || node.CheckpointAttemptID != attemptID {
		return false
	}
	attempt, ok := state.Attempts[attemptID]
	if !ok || attempt.NodeID != node.ID || attempt.RevisionID != node.RevisionID {
		return false
	}
	switch attempt.Status {
	case orchestration.AttemptFailed, orchestration.AttemptIncomplete, orchestration.AttemptTimedOut, orchestration.AttemptCancelUnconfirmed:
		return true
	default:
		return false
	}
}

func latestCompletionSucceeded(state storage.State, attemptID string) bool {
	latest, found := latestCompletionDecision(state, attemptID)
	return found && latest.Status == orchestration.AttemptCompleted
}

func latestCompletionDecision(state storage.State, attemptID string) (orchestration.CompletionDecision, bool) {
	var latest orchestration.CompletionDecision
	found := false
	for _, decision := range state.Completions {
		if decision.AttemptID != attemptID {
			continue
		}
		if !found || decision.CreatedAt.After(latest.CreatedAt) {
			latest = decision
			found = true
		}
	}
	return latest, found
}

// freezeContamination turns a provider/session/workspace/canary mismatch into
// a durable human checkpoint. The contaminated Attempt remains immutable and
// the owning task and node cannot dispatch more work until an explicit human
// decision releases the checkpoint.
func freezeContamination(state *storage.State, attempt orchestration.Attempt) {
	now := time.Now().UTC()
	state.Task.Status = orchestration.TaskAwaitingHumanDecision
	state.Task.UpdatedAt = now
	if node, ok := state.Nodes[attempt.NodeID]; ok {
		node.Status = orchestration.NodeAwaitingHumanDecision
		node.CheckpointAttemptID = attempt.ID
		node.UpdatedAt = now
		state.Nodes[node.ID] = node
	}
}

func retryCount(state storage.State, attemptID string) (int, error) {
	seen := make(map[string]struct{})
	count := 0
	for strings.TrimSpace(attemptID) != "" {
		if _, ok := seen[attemptID]; ok {
			return 0, errors.New("attempt retry lineage contains a cycle")
		}
		seen[attemptID] = struct{}{}
		attempt, ok := state.Attempts[attemptID]
		if !ok {
			return 0, fmt.Errorf("attempt retry predecessor %q was not found", attemptID)
		}
		if strings.TrimSpace(attempt.RetryOf) == "" {
			return count, nil
		}
		count++
		attemptID = attempt.RetryOf
	}
	return count, nil
}

func hasAnyAttemptForNode(state storage.State, nodeID, revisionID string) bool {
	for _, attempt := range state.Attempts {
		if attempt.NodeID == nodeID && attempt.RevisionID == revisionID {
			return true
		}
	}
	return false
}

func hasContaminationRelease(state storage.State, nodeID, attemptID string) bool {
	for index := len(state.HumanDecisions) - 1; index >= 0; index-- {
		decision := state.HumanDecisions[index]
		if decision.NodeID == nodeID && decision.AttemptID == attemptID && decision.Decision == "release_contamination" {
			return true
		}
	}
	return false
}

func hasUnreleasedContaminationForAttempt(state storage.State, nodeID, attemptID string) bool {
	attempt, ok := state.Attempts[attemptID]
	return ok && attempt.NodeID == nodeID && attempt.Status == orchestration.AttemptContaminated && !hasContaminationRelease(state, nodeID, attemptID)
}

func hasUnreleasedContamination(state storage.State, nodeID string) bool {
	for attemptID := range state.Attempts {
		if hasUnreleasedContaminationForAttempt(state, nodeID, attemptID) {
			return true
		}
	}
	return false
}

func terminalAttemptStatus(status orchestration.AttemptStatus) bool {
	switch status {
	case orchestration.AttemptCompleted, orchestration.AttemptFailed, orchestration.AttemptIncomplete, orchestration.AttemptTimedOut, orchestration.AttemptContaminated, orchestration.AttemptCancelled, orchestration.AttemptCancelUnconfirmed:
		return true
	default:
		return false
	}
}

func validateExpectedArtifact(expected orchestration.ExpectedOutput, workspace string) (orchestration.ArtifactRef, error) {
	if strings.TrimSpace(expected.ArtifactPath) == "" || !filepath.IsAbs(expected.ArtifactPath) {
		return orchestration.ArtifactRef{}, errors.New("required artifact path must be absolute")
	}
	canonicalWorkspace, err := orchestration.CanonicalDirectory(workspace)
	if err != nil {
		return orchestration.ArtifactRef{}, err
	}
	absArtifactPath, err := filepath.Abs(expected.ArtifactPath)
	if err != nil {
		return orchestration.ArtifactRef{}, err
	}
	artifactPath := filepath.Clean(absArtifactPath)
	relative, err := filepath.Rel(canonicalWorkspace, artifactPath)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return orchestration.ArtifactRef{}, errors.New("required artifact path is outside task workspace")
	}
	info, err := os.Stat(artifactPath)
	if err != nil || !info.Mode().IsRegular() {
		return orchestration.ArtifactRef{}, errors.New("required artifact does not exist as a regular file")
	}
	canonicalArtifactPath, err := filepath.EvalSymlinks(artifactPath)
	if err != nil {
		return orchestration.ArtifactRef{}, err
	}
	canonicalArtifactPath = filepath.Clean(canonicalArtifactPath)
	relative, err = filepath.Rel(canonicalWorkspace, canonicalArtifactPath)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return orchestration.ArtifactRef{}, errors.New("required artifact resolves outside task workspace")
	}
	artifactPath = canonicalArtifactPath
	minBytes := expected.MinBytes
	if minBytes < 1 {
		minBytes = 1
	}
	if info.Size() < int64(minBytes) {
		return orchestration.ArtifactRef{}, errors.New("required artifact is empty or shorter than min_bytes")
	}
	raw, err := os.ReadFile(artifactPath)
	if err != nil {
		return orchestration.ArtifactRef{}, err
	}
	if expected.Format == "markdown" {
		extension := strings.ToLower(filepath.Ext(artifactPath))
		if extension != ".md" && extension != ".markdown" {
			return orchestration.ArtifactRef{}, errors.New("markdown artifact must use .md or .markdown extension")
		}
	}
	for _, section := range expected.RequiredSections {
		if !strings.Contains(string(raw), section) {
			return orchestration.ArtifactRef{}, fmt.Errorf("required artifact section %q is missing", section)
		}
	}
	sum := sha256.Sum256(raw)
	return orchestration.ArtifactRef{Path: artifactPath, Format: expected.Format, Bytes: int64(len(raw)), SHA256: hex.EncodeToString(sum[:]), Validated: true}, nil
}

func appendOrReplaceArtifact(existing []orchestration.ArtifactRef, artifact orchestration.ArtifactRef) []orchestration.ArtifactRef {
	for index := range existing {
		if filepath.Clean(existing[index].Path) == filepath.Clean(artifact.Path) {
			existing[index] = artifact
			return existing
		}
	}
	return append(existing, artifact)
}

func sortActions(actions []orchestration.HostAction) []orchestration.HostAction {
	for i := 0; i < len(actions); i++ {
		for j := i + 1; j < len(actions); j++ {
			if actions[j].Sequence < actions[i].Sequence {
				actions[i], actions[j] = actions[j], actions[i]
			}
		}
	}
	return actions
}

func nodeID(node *orchestration.WorkflowNode) string {
	if node == nil {
		return ""
	}
	return node.ID
}
