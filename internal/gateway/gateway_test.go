package gateway

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"agent-workflow-toolkit/internal/adapters/fake"
	"agent-workflow-toolkit/internal/orchestration"
	"agent-workflow-toolkit/internal/policy"
	protocol "agent-workflow-toolkit/internal/protocol/v1"
	"agent-workflow-toolkit/internal/storage"
)

func TestFakeReadOnlyWorkflowReachesHumanCheckpoint(t *testing.T) {
	root := t.TempDir()
	workspace := filepath.Join(root, "workspace")
	if err := os.Mkdir(workspace, 0o700); err != nil {
		t.Fatal(err)
	}
	store, err := storage.New(filepath.Join(root, "state"))
	if err != nil {
		t.Fatal(err)
	}
	binding := orchestration.Binding{Role: "reviewer", Adapter: "fake", Model: "fixture-model", Reasoning: "none", Permission: "read-only", Workspace: workspace}
	config := policy.DefaultConfig()
	config.RegisteredBindings = []orchestration.Binding{binding}
	core, err := New(store, config)
	if err != nil {
		t.Fatal(err)
	}

	taskResult, err := core.CreateTask(CreateTaskInput{Title: "fake e2e", Objective: "review the fixture", Workspace: workspace})
	if err != nil {
		t.Fatal(err)
	}
	nodeResult, err := core.ProposeNode(ProposeNodeInput{TaskID: taskResult.Task.ID, Kind: "review", Role: "reviewer"})
	if err != nil {
		t.Fatal(err)
	}
	preflight, err := core.RunPreflight(RunPreflightInput{NodeID: nodeResult.Node.ID, Binding: binding})
	if err != nil {
		t.Fatal(err)
	}
	if preflight.Preflight.Status != "passed" {
		t.Fatalf("preflight = %#v", preflight.Preflight)
	}
	launch, err := core.PrepareAttempt(PrepareAttemptInput{NodeID: nodeResult.Node.ID, ControllerInstanceID: "test-controller"})
	if err != nil {
		t.Fatal(err)
	}

	adapter := fake.NewAdapter()
	bridge := fake.NewBridge(adapter)
	action := launch.LaunchAction
	for {
		if err := bridge.Issue(toFakeAction(action)); err != nil {
			t.Fatal(err)
		}
		result, err := bridge.Execute(action.ID)
		if err != nil {
			t.Fatal(err)
		}
		started := action.Kind == orchestration.ActionLaunch || action.Kind == orchestration.ActionObserve
		receipt, err := core.SubmitHostActionResult(SubmitHostActionResultInput{
			AttemptID: action.AttemptID,
			ActionID:  action.ID,
			Lease:     action.Lease,
			Result: orchestration.HostActionResult{
				Status:            result.Status,
				ProviderHandle:    result.ProviderHandle,
				ProviderSessionID: result.Identity["session"],
				Terminal:          result.Terminal,
				ExecutionStarted:  &started,
				SideEffectState:   result.SideEffect,
				Identity:          result.Identity,
				TaskCanary:        result.TaskCanary,
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(receipt.Next) == 0 {
			break
		}
		action = receipt.Next[0]
	}

	completion, err := core.RequestCompletion(CompletionInput{AttemptID: launch.Attempt.ID})
	if err != nil {
		t.Fatal(err)
	}
	if completion.Decision.Status != orchestration.AttemptCompleted || completion.Node.Status != orchestration.NodeAwaitingHumanDecision {
		t.Fatalf("completion = %#v", completion)
	}
	approved, err := core.RecordHumanDecision(HumanDecisionInput{NodeID: nodeResult.Node.ID, Decision: "approve", AuthorityRef: "user-input:approve"})
	if err != nil {
		t.Fatal(err)
	}
	if approved.Node.Status != orchestration.NodeApproved {
		t.Fatalf("approved node = %#v", approved.Node)
	}

	state, err := store.Load(taskResult.Task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Events) < 6 || len(adapter.Calls()) != 2 {
		t.Fatalf("events=%d adapter_calls=%d, want at least 6 events and launch+observe", len(state.Events), len(adapter.Calls()))
	}
	if _, err := core.SubmitHostActionResult(SubmitHostActionResultInput{AttemptID: launch.Attempt.ID, ActionID: launch.LaunchAction.ID, Lease: launch.LaunchAction.Lease}); err != nil {
		t.Fatalf("duplicate completed action result should be idempotent: %v", err)
	}
}

func TestGatewayRequestIDIsIdempotentAndRejectsChangedPayload(t *testing.T) {
	root := t.TempDir()
	store, err := storage.New(filepath.Join(root, "state"))
	if err != nil {
		t.Fatal(err)
	}
	core, err := New(store, policy.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	first, err := protocol.NewRequest("same-request", "createTask", CreateTaskInput{Objective: "one", Workspace: root})
	if err != nil {
		t.Fatal(err)
	}
	firstResult, err := core.Handle(first)
	if err != nil {
		t.Fatal(err)
	}
	secondResult, err := core.Handle(first)
	if err != nil {
		t.Fatal(err)
	}
	if string(firstResult) != string(secondResult) {
		t.Fatalf("idempotent payload changed: %s != %s", firstResult, secondResult)
	}
	var decoded CreateTaskResult
	if err := json.Unmarshal(firstResult, &decoded); err != nil {
		t.Fatal(err)
	}
	changed, err := protocol.NewRequest("same-request", "createTask", CreateTaskInput{Objective: "two", Workspace: root})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := core.Handle(changed); err == nil {
		t.Fatal("changed request payload reused the same request id")
	}
}

func TestMismatchedTaskCanaryFreezesAttempt(t *testing.T) {
	core, store, launch := preparedAttemptForTest(t)
	started := true
	receipt, err := core.SubmitHostActionResult(SubmitHostActionResultInput{
		AttemptID: launch.Attempt.ID,
		ActionID:  launch.LaunchAction.ID,
		Lease:     launch.LaunchAction.Lease,
		Result: orchestration.HostActionResult{
			Status:            "running",
			ProviderHandle:    "foreign-handle",
			ProviderSessionID: "foreign-session",
			ExecutionStarted:  &started,
			SideEffectState:   "none",
			Identity:          map[string]string{"provider": "foreign"},
			TaskCanary:        "wrong-task-canary",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(receipt.Next) != 0 || receipt.Attempt.Status != orchestration.AttemptContaminated {
		t.Fatalf("contaminated receipt = %#v", receipt)
	}
	state, err := store.Load(launch.Attempt.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	if state.Attempts[launch.Attempt.ID].Status != orchestration.AttemptContaminated {
		t.Fatalf("persisted attempt was not contaminated: %#v", state.Attempts[launch.Attempt.ID])
	}
	if state.Task.Status != orchestration.TaskAwaitingHumanDecision || state.Nodes[launch.Attempt.NodeID].Status != orchestration.NodeAwaitingHumanDecision {
		t.Fatalf("contamination did not freeze task and node: task=%#v node=%#v", state.Task, state.Nodes[launch.Attempt.NodeID])
	}
	if _, err := core.PrepareAttempt(PrepareAttemptInput{NodeID: launch.Attempt.NodeID, ControllerInstanceID: "controller"}); err == nil {
		t.Fatal("contaminated node was prepared without human isolation release")
	} else if gatewayErr, ok := err.(*Error); !ok || gatewayErr.Code != "checkpoint_required" {
		t.Fatalf("contaminated same-node preparation error = %v", err)
	}
	if _, err := core.ProposeNode(ProposeNodeInput{TaskID: launch.Attempt.TaskID, Kind: "downstream"}); err == nil {
		t.Fatal("contaminated task proposed a downstream node without human isolation release")
	} else if gatewayErr, ok := err.(*Error); !ok || gatewayErr.Code != "checkpoint_required" {
		t.Fatalf("contaminated downstream proposal error = %v", err)
	}
	for _, decision := range []string{"approve", "retry_node"} {
		if _, err := core.RecordHumanDecision(HumanDecisionInput{NodeID: launch.Attempt.NodeID, Decision: decision, AuthorityRef: "user-input:" + decision}); err == nil {
			t.Fatalf("contaminated checkpoint accepted ordinary %s decision", decision)
		} else if gatewayErr, ok := err.(*Error); !ok || gatewayErr.Code != "contamination_release_required" {
			t.Fatalf("contaminated %s decision error = %v", decision, err)
		}
	}
	if _, err := core.RecordHumanDecision(HumanDecisionInput{NodeID: launch.Attempt.NodeID, AttemptID: launch.Attempt.ID, Decision: "release_contamination", AuthorityRef: "goal:contamination"}); err == nil {
		t.Fatal("goal authority released contamination without direct user input")
	} else if gatewayErr, ok := err.(*Error); !ok || gatewayErr.Code != "contamination_release_authority_required" {
		t.Fatalf("goal contamination release error = %v", err)
	}
	released, err := core.RecordHumanDecision(HumanDecisionInput{NodeID: launch.Attempt.NodeID, AttemptID: launch.Attempt.ID, Decision: "release_contamination", AuthorityRef: "user-input:release-contamination"})
	if err != nil {
		t.Fatal(err)
	}
	if released.Node.Status != orchestration.NodeProposed || released.Node.PreflightStatus != "" || released.Node.ApprovedBinding != nil {
		t.Fatalf("contamination release did not require fresh preflight: %#v", released.Node)
	}
	state, err = store.Load(launch.Attempt.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	if state.Task.Status != orchestration.TaskActive {
		t.Fatalf("contamination release did not reopen task: %#v", state.Task)
	}
	if _, err := core.RequestCompletion(CompletionInput{AttemptID: launch.Attempt.ID}); err != nil {
		t.Fatal(err)
	}
	state, err = store.Load(launch.Attempt.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	if state.Task.Status != orchestration.TaskActive || state.Nodes[launch.Attempt.NodeID].Status != orchestration.NodeProposed {
		t.Fatalf("completion of released contaminated Attempt re-froze workflow: task=%#v node=%#v", state.Task, state.Nodes[launch.Attempt.NodeID])
	}
	if _, err := core.BindProviderHandle(BindProviderHandleInput{AttemptID: launch.Attempt.ID, LaunchActionID: launch.LaunchAction.ID, ProviderHandle: "late-handle", ProviderSessionID: "late-session", Identity: map[string]string{"provider": "fake", "session": "late-session", "workspace": launch.Attempt.Binding.Workspace}}); err == nil {
		t.Fatal("released contaminated Attempt accepted a late provider handle")
	} else if gatewayErr, ok := err.(*Error); !ok || gatewayErr.Code != "attempt_contaminated" {
		t.Fatalf("late contaminated provider handle error = %v", err)
	}
	if _, err := core.RunPreflight(RunPreflightInput{NodeID: launch.Attempt.NodeID, Binding: launch.Attempt.Binding}); err != nil {
		t.Fatal(err)
	}
	fresh, err := core.PrepareAttempt(PrepareAttemptInput{NodeID: launch.Attempt.NodeID, PreviousAttemptID: launch.Attempt.ID, ControllerInstanceID: "controller"})
	if err != nil {
		t.Fatalf("same-node prepare after explicit release and fresh preflight failed: %v", err)
	}
	if fresh.Attempt.RetryOf != launch.Attempt.ID {
		t.Fatalf("released contamination lineage = %#v", fresh.Attempt)
	}
	started = true
	if _, err := core.SubmitHostActionResult(SubmitHostActionResultInput{
		AttemptID: fresh.Attempt.ID,
		ActionID:  fresh.LaunchAction.ID,
		Lease:     fresh.LaunchAction.Lease,
		Result: orchestration.HostActionResult{
			Status: "completed", Terminal: true, ProviderHandle: "fresh-handle", ProviderSessionID: "fresh-session", ExecutionStarted: &started, SideEffectState: "none",
			Identity: map[string]string{"provider": "fake", "session": "fresh-session", "workspace": fresh.Attempt.Binding.Workspace}, TaskCanary: fresh.Attempt.TaskCanary,
		},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := core.RequestCompletion(CompletionInput{AttemptID: fresh.Attempt.ID}); err != nil {
		t.Fatal(err)
	}
	if approved, err := core.RecordHumanDecision(HumanDecisionInput{NodeID: fresh.Attempt.NodeID, Decision: "approve", AuthorityRef: "user-input:approve-after-release"}); err != nil || approved.Node.Status != orchestration.NodeApproved {
		t.Fatalf("ordinary approval remained blocked after contamination was explicitly released: result=%#v err=%v", approved, err)
	}
	if _, err := core.ProposeNode(ProposeNodeInput{TaskID: launch.Attempt.TaskID, Kind: "downstream-after-release"}); err != nil {
		t.Fatalf("explicit contamination release did not permit downstream proposal: %v", err)
	}
}

func TestRequiredArtifactControlsCompletion(t *testing.T) {
	core, store, launch := preparedAttemptForTestWithOutput(t, orchestration.ExpectedOutput{Format: "markdown", ArtifactPath: "review.md", Required: true, MinBytes: 10, RequiredSections: []string{"## Findings"}})
	state, err := store.Load(launch.Attempt.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	artifactPath := state.Nodes[launch.Attempt.NodeID].ExpectedOutput.ArtifactPath
	started := true
	launchReceipt, err := core.SubmitHostActionResult(SubmitHostActionResultInput{AttemptID: launch.Attempt.ID, ActionID: launch.LaunchAction.ID, Lease: launch.LaunchAction.Lease, Result: orchestration.HostActionResult{Status: "running", ProviderHandle: "handle", ProviderSessionID: "session", ExecutionStarted: &started, SideEffectState: "none", Identity: map[string]string{"provider": "fake", "session": "session", "workspace": launch.Attempt.Binding.Workspace}, TaskCanary: launch.Attempt.TaskCanary}})
	if err != nil {
		t.Fatal(err)
	}
	if len(launchReceipt.Next) != 1 {
		t.Fatalf("launch next actions = %#v", launchReceipt.Next)
	}
	observe := launchReceipt.Next[0]
	observeReceipt, err := core.SubmitHostActionResult(SubmitHostActionResultInput{AttemptID: launch.Attempt.ID, ActionID: observe.ID, Lease: observe.Lease, Result: orchestration.HostActionResult{Status: "completed", Terminal: true, ProviderHandle: "handle", ProviderSessionID: "session", SideEffectState: "none", Identity: map[string]string{"provider": "fake", "session": "session", "workspace": launch.Attempt.Binding.Workspace}}})
	if err != nil {
		t.Fatal(err)
	}
	if observeReceipt.Attempt.Status != orchestration.AttemptCompleted {
		t.Fatalf("observe attempt = %#v", observeReceipt.Attempt)
	}
	missing, err := core.RequestCompletion(CompletionInput{AttemptID: launch.Attempt.ID})
	if err != nil {
		t.Fatal(err)
	}
	if missing.Decision.Status != orchestration.AttemptIncomplete || missing.Decision.Reason != "delivery_missing" {
		t.Fatalf("missing artifact completion = %#v", missing.Decision)
	}
	if err := os.WriteFile(artifactPath, []byte("# Review\n\n## Findings\nnone\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	complete, err := core.RequestCompletion(CompletionInput{AttemptID: launch.Attempt.ID})
	if err != nil {
		t.Fatal(err)
	}
	if complete.Decision.Status != orchestration.AttemptCompleted || len(complete.Decision.Artifacts) != 1 || !complete.Decision.Artifacts[0].Validated {
		t.Fatalf("validated artifact completion = %#v", complete.Decision)
	}
}

func TestAdapterDeliveryReasonSurvivesCoreCompletionDecision(t *testing.T) {
	core, _, launch := preparedAttemptForTest(t)
	started := true
	failed, err := core.SubmitHostActionResult(SubmitHostActionResultInput{
		AttemptID: launch.Attempt.ID,
		ActionID:  launch.LaunchAction.ID,
		Lease:     launch.LaunchAction.Lease,
		Result: orchestration.HostActionResult{
			Status:            "failed",
			Reason:            "delivery_missing",
			ProviderHandle:    "cursor-process",
			ProviderSessionID: "cursor-session",
			ExecutionStarted:  &started,
			SideEffectState:   "none",
			Identity:          map[string]string{"provider": "fake", "session": "cursor-session", "workspace": launch.Attempt.Binding.Workspace},
			TaskCanary:        launch.Attempt.TaskCanary,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if failed.Attempt.Status != orchestration.AttemptFailed {
		t.Fatalf("failed delivery attempt = %#v", failed.Attempt)
	}
	completion, err := core.RequestCompletion(CompletionInput{AttemptID: launch.Attempt.ID})
	if err != nil {
		t.Fatal(err)
	}
	if completion.Decision.Status != orchestration.AttemptIncomplete || completion.Decision.Reason != "delivery_missing" {
		t.Fatalf("delivery reason was not preserved: %#v", completion.Decision)
	}
}

func TestCancelAndTimeoutResultsMapToDistinctAttemptStates(t *testing.T) {
	core, _, launch := preparedAttemptForTest(t)
	cancel, err := core.RequestCancel(CancelInput{AttemptID: launch.Attempt.ID, Reason: "user requested stop"})
	if err != nil {
		t.Fatal(err)
	}
	confirmed, err := core.SubmitHostActionResult(SubmitHostActionResultInput{AttemptID: launch.Attempt.ID, ActionID: cancel.Action.ID, Lease: cancel.Action.Lease, Result: orchestration.HostActionResult{Status: "cancelled", Terminal: true}})
	if err != nil {
		t.Fatal(err)
	}
	if confirmed.Attempt.Status != orchestration.AttemptCancelled {
		t.Fatalf("confirmed cancel = %#v", confirmed.Attempt)
	}

	core, _, launch = preparedAttemptForTest(t)
	cancel, err = core.RequestCancel(CancelInput{AttemptID: launch.Attempt.ID, Reason: "provider does not confirm"})
	if err != nil {
		t.Fatal(err)
	}
	unconfirmed, err := core.SubmitHostActionResult(SubmitHostActionResultInput{AttemptID: launch.Attempt.ID, ActionID: cancel.Action.ID, Lease: cancel.Action.Lease, Result: orchestration.HostActionResult{Status: "unknown"}})
	if err != nil {
		t.Fatal(err)
	}
	if unconfirmed.Attempt.Status != orchestration.AttemptCancelUnconfirmed {
		t.Fatalf("unconfirmed cancel = %#v", unconfirmed.Attempt)
	}

	core, _, launch = preparedAttemptForTest(t)
	cancel, err = core.RequestCancel(CancelInput{AttemptID: launch.Attempt.ID, Reason: "provider identity is wrong"})
	if err != nil {
		t.Fatal(err)
	}
	contaminatedCancel, err := core.SubmitHostActionResult(SubmitHostActionResultInput{AttemptID: launch.Attempt.ID, ActionID: cancel.Action.ID, Lease: cancel.Action.Lease, Result: orchestration.HostActionResult{Status: "cancelled", Terminal: true, Identity: map[string]string{"provider": "foreign", "session": "foreign", "workspace": launch.Attempt.Binding.Workspace}, TaskCanary: "wrong-cancel-canary"}})
	if err != nil {
		t.Fatal(err)
	}
	if contaminatedCancel.Attempt.Status != orchestration.AttemptContaminated || len(contaminatedCancel.Next) != 0 {
		t.Fatalf("cancel identity mismatch was not preserved as contamination: %#v", contaminatedCancel)
	}

	core, _, launch = preparedAttemptForTest(t)
	started := true
	running, err := core.SubmitHostActionResult(SubmitHostActionResultInput{AttemptID: launch.Attempt.ID, ActionID: launch.LaunchAction.ID, Lease: launch.LaunchAction.Lease, Result: orchestration.HostActionResult{Status: "running", ProviderHandle: "handle", ProviderSessionID: "session", ExecutionStarted: &started, SideEffectState: "none", Identity: map[string]string{"provider": "fake", "session": "session", "workspace": launch.Attempt.Binding.Workspace}, TaskCanary: launch.Attempt.TaskCanary}})
	if err != nil {
		t.Fatal(err)
	}
	timedOut, err := core.SubmitHostActionResult(SubmitHostActionResultInput{AttemptID: launch.Attempt.ID, ActionID: running.Next[0].ID, Lease: running.Next[0].Lease, Result: orchestration.HostActionResult{Status: "timed_out", ProviderHandle: "handle", ProviderSessionID: "session", Identity: map[string]string{"provider": "fake", "session": "session", "workspace": launch.Attempt.Binding.Workspace}}})
	if err != nil {
		t.Fatal(err)
	}
	if timedOut.Attempt.Status != orchestration.AttemptTimedOut {
		t.Fatalf("timed out attempt = %#v", timedOut.Attempt)
	}
}

func TestConfirmedCancellationCanBeManuallyRetried(t *testing.T) {
	core, _, launch := preparedAttemptForTest(t)
	cancel, err := core.RequestCancel(CancelInput{AttemptID: launch.Attempt.ID, Reason: "user requested stop"})
	if err != nil {
		t.Fatal(err)
	}
	confirmed, err := core.SubmitHostActionResult(SubmitHostActionResultInput{
		AttemptID: launch.Attempt.ID,
		ActionID:  cancel.Action.ID,
		Lease:     cancel.Action.Lease,
		Result:    orchestration.HostActionResult{Status: "cancelled", Terminal: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if confirmed.Attempt.Status != orchestration.AttemptCancelled {
		t.Fatalf("confirmed cancellation = %#v", confirmed.Attempt)
	}
	completion, err := core.RequestCompletion(CompletionInput{AttemptID: confirmed.Attempt.ID})
	if err != nil {
		t.Fatal(err)
	}
	if completion.Node.Status != orchestration.NodeAwaitingHumanDecision || completion.Node.CheckpointAttemptID != confirmed.Attempt.ID {
		t.Fatalf("cancelled checkpoint = %#v", completion.Node)
	}
	if _, err := core.RecordHumanDecision(HumanDecisionInput{NodeID: confirmed.Attempt.NodeID, Decision: "retry_node", AuthorityRef: "user-input:retry-cancelled"}); err != nil {
		t.Fatal(err)
	}
	retry, err := core.PrepareAttempt(PrepareAttemptInput{
		NodeID: confirmed.Attempt.NodeID, PreviousAttemptID: confirmed.Attempt.ID, ControllerInstanceID: "controller",
	})
	if err != nil {
		t.Fatalf("confirmed cancellation could not be manually retried: %v", err)
	}
	if retry.Attempt.RetryOf != confirmed.Attempt.ID {
		t.Fatalf("cancelled retry lineage = %#v", retry.Attempt)
	}
}

func TestGoalAuthorityAndAuthorityReferenceAreValidated(t *testing.T) {
	root := t.TempDir()
	workspace := filepath.Join(root, "workspace")
	if err := os.Mkdir(workspace, 0o700); err != nil {
		t.Fatal(err)
	}
	store, err := storage.New(filepath.Join(root, "state"))
	if err != nil {
		t.Fatal(err)
	}
	config := policy.DefaultConfig()
	config.GoalEnabled = true
	core, err := New(store, config)
	if err != nil {
		t.Fatal(err)
	}
	task, err := core.CreateTask(CreateTaskInput{Objective: "goal authority", Workspace: workspace})
	if err != nil {
		t.Fatal(err)
	}
	node, err := core.ProposeNode(ProposeNodeInput{TaskID: task.Task.ID, Kind: "review"})
	if err != nil {
		t.Fatal(err)
	}
	goal, err := core.EnableGoalMode(GoalInput{TaskID: task.Task.ID, Scope: []string{"review"}, UserEvidence: "user-input:goal-enabled"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := core.EnableGoalMode(GoalInput{TaskID: task.Task.ID, Scope: []string{"not-in-workflow"}, UserEvidence: "user-input:invalid-scope"}); err == nil {
		t.Fatal("out-of-scope Goal authorization was accepted")
	}
	state, err := store.Load(task.Task.ID)
	if err != nil {
		t.Fatal(err)
	}
	awaiting := state.Nodes[node.Node.ID]
	awaiting.Status = orchestration.NodeAwaitingHumanDecision
	state.Nodes[node.Node.ID] = awaiting
	if err := store.Save(state); err != nil {
		t.Fatal(err)
	}
	if _, err := core.RecordHumanDecision(HumanDecisionInput{NodeID: node.Node.ID, Decision: "approve", AuthorityRef: "goal:forged"}); err == nil {
		t.Fatal("forged goal authority was accepted")
	} else if gatewayErr, ok := err.(*Error); !ok || gatewayErr.Code != "invalid_goal_authority" {
		t.Fatalf("forged goal authority error = %v", err)
	}
	if _, err := core.RecordHumanDecision(HumanDecisionInput{NodeID: node.Node.ID, Decision: "approve", AuthorityRef: "spoofed-ref"}); err == nil {
		t.Fatal("untyped authority reference was accepted")
	} else if gatewayErr, ok := err.(*Error); !ok || gatewayErr.Code != "invalid_authority_ref" {
		t.Fatalf("untyped authority error = %v", err)
	}
	approved, err := core.RecordHumanDecision(HumanDecisionInput{NodeID: node.Node.ID, Decision: "approve", AuthorityRef: "goal:" + goal.Authorization.ID})
	if err != nil || approved.Node.Status != orchestration.NodeApproved {
		t.Fatalf("valid goal authority was rejected: result=%#v err=%v", approved, err)
	}
	state, err = store.Load(task.Task.ID)
	if err != nil {
		t.Fatal(err)
	}
	awaiting = state.Nodes[node.Node.ID]
	awaiting.Status = orchestration.NodeAwaitingHumanDecision
	state.Nodes[node.Node.ID] = awaiting
	if err := store.Save(state); err != nil {
		t.Fatal(err)
	}
	if _, err := core.RevokeGoalMode(RevokeGoalInput{TaskID: task.Task.ID, GoalAuthorityID: goal.Authorization.ID, Reason: "test revoke"}); err != nil {
		t.Fatal(err)
	}
	if _, err := core.RecordHumanDecision(HumanDecisionInput{NodeID: node.Node.ID, Decision: "approve", AuthorityRef: "goal:" + goal.Authorization.ID}); err == nil {
		t.Fatal("revoked goal authority was accepted")
	} else if gatewayErr, ok := err.(*Error); !ok || gatewayErr.Code != "invalid_goal_authority" {
		t.Fatalf("revoked goal authority error = %v", err)
	}
}

func preparedAttemptForTest(t *testing.T) (*Gateway, *storage.Store, LaunchDecision) {
	return preparedAttemptForTestWithOutput(t, orchestration.ExpectedOutput{})
}

func preparedAttemptForTestWithOutput(t *testing.T, expected orchestration.ExpectedOutput) (*Gateway, *storage.Store, LaunchDecision) {
	t.Helper()
	root := t.TempDir()
	workspace := filepath.Join(root, "workspace")
	if err := os.Mkdir(workspace, 0o700); err != nil {
		t.Fatal(err)
	}
	if expected.ArtifactPath != "" && !filepath.IsAbs(expected.ArtifactPath) {
		expected.ArtifactPath = filepath.Join(workspace, expected.ArtifactPath)
	}
	store, err := storage.New(filepath.Join(root, "state"))
	if err != nil {
		t.Fatal(err)
	}
	binding := orchestration.Binding{Role: "reviewer", Adapter: "fake", Model: "fixture-model", Reasoning: "none", Permission: "read-only", Workspace: workspace}
	config := policy.DefaultConfig()
	config.RegisteredBindings = []orchestration.Binding{binding}
	core, err := New(store, config)
	if err != nil {
		t.Fatal(err)
	}
	task, err := core.CreateTask(CreateTaskInput{Objective: "canary test", Workspace: workspace})
	if err != nil {
		t.Fatal(err)
	}
	node, err := core.ProposeNode(ProposeNodeInput{TaskID: task.Task.ID, Kind: "review", ExpectedOutput: expected})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := core.RunPreflight(RunPreflightInput{NodeID: node.Node.ID, Binding: binding}); err != nil {
		t.Fatal(err)
	}
	launch, err := core.PrepareAttempt(PrepareAttemptInput{NodeID: node.Node.ID, ControllerInstanceID: "canary-test-controller"})
	if err != nil {
		t.Fatal(err)
	}
	return core, store, launch
}

func toFakeAction(action orchestration.HostAction) fake.HostAction {
	return fake.HostAction{ID: action.ID, AttemptID: action.AttemptID, Kind: fake.ActionKind(action.Kind), Sequence: action.Sequence, Lease: action.Lease, Parameters: action.Parameters, ParametersHash: action.ParametersHash}
}

func TestUnspecifiedNodeIsRecordedButCannotBecomeExecutable(t *testing.T) {
	root := t.TempDir()
	workspace := filepath.Join(root, "workspace")
	if err := os.Mkdir(workspace, 0o700); err != nil {
		t.Fatal(err)
	}
	store, err := storage.New(filepath.Join(root, "state"))
	if err != nil {
		t.Fatal(err)
	}
	core, err := New(store, policy.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	task, err := core.CreateTask(CreateTaskInput{Objective: "unnamed node", Workspace: workspace})
	if err != nil {
		t.Fatal(err)
	}
	nodeResult, err := core.ProposeNode(ProposeNodeInput{TaskID: task.Task.ID})
	if err != nil {
		t.Fatal(err)
	}
	if nodeResult.Node.Kind != "unspecified" || nodeResult.Node.Status != orchestration.NodePreflightBlocked {
		t.Fatalf("unnamed node = %#v", nodeResult.Node)
	}
	preflight, err := core.RunPreflight(RunPreflightInput{NodeID: nodeResult.Node.ID, Binding: orchestration.Binding{}})
	if err != nil {
		t.Fatal(err)
	}
	if preflight.Preflight.Code != "unspecified_node" || preflight.Node.Status != orchestration.NodePreflightBlocked {
		t.Fatalf("unnamed node preflight = %#v", preflight)
	}
	if _, err := core.PrepareAttempt(PrepareAttemptInput{NodeID: nodeResult.Node.ID, ControllerInstanceID: "controller"}); err == nil {
		t.Fatal("unspecified node produced a launch ticket")
	} else if gatewayErr, ok := err.(*Error); !ok || gatewayErr.Code != "unspecified_node" {
		t.Fatalf("unspecified node error = %v", err)
	}
}

func TestExpiredHostActionIsPersistedAndCannotAcceptLateResult(t *testing.T) {
	core, store, launch := preparedAttemptForTest(t)
	state, err := store.Load(launch.Attempt.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	action := state.Actions[launch.LaunchAction.ID]
	action.ExpiresAt = time.Now().UTC().Add(-time.Second)
	state.Actions[action.ID] = action
	if err := store.Save(state); err != nil {
		t.Fatal(err)
	}
	batch, err := core.PollHostActions(PollHostActionsInput{AttemptID: launch.Attempt.ID, ControllerInstanceID: "canary-test-controller"})
	if err != nil {
		t.Fatal(err)
	}
	if len(batch.Actions) != 0 {
		t.Fatalf("expired action was returned: %#v", batch.Actions)
	}
	recovered, err := store.Load(launch.Attempt.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	if recovered.Actions[action.ID].Status != orchestration.ActionExpired || recovered.Attempts[launch.Attempt.ID].Status != orchestration.AttemptIncomplete || recovered.Attempts[launch.Attempt.ID].SideEffectState != "unknown" {
		t.Fatalf("expired action was not durably closed: action=%#v attempt=%#v", recovered.Actions[action.ID], recovered.Attempts[launch.Attempt.ID])
	}
	if _, err := core.PrepareRetryAttempt(RetryAttemptInput{NodeID: launch.Attempt.NodeID, PreviousAttemptID: launch.Attempt.ID, ControllerInstanceID: "controller", ErrorCode: "transport_before_session"}); err == nil {
		t.Fatal("expired action entered automatic retry")
	}
	if _, err := core.PrepareFallbackAttempt(FallbackAttemptInput{NodeID: launch.Attempt.NodeID, PreviousAttemptID: launch.Attempt.ID, ControllerInstanceID: "controller", Binding: launch.Attempt.Binding, Approved: true}); err == nil {
		t.Fatal("expired action entered fallback")
	}
	if _, err := core.SubmitHostActionResult(SubmitHostActionResultInput{AttemptID: launch.Attempt.ID, ActionID: action.ID, Lease: action.Lease, Result: orchestration.HostActionResult{Status: "running"}}); err == nil {
		t.Fatal("late result for expired action was accepted")
	} else if gatewayErr, ok := err.(*Error); !ok || gatewayErr.Code != "action_outcome_unknown" {
		t.Fatalf("late result error = %v", err)
	}
}

func TestManualRetryRejectsUnknownSideEffectOutcome(t *testing.T) {
	core, _, launch := preparedAttemptForTest(t)
	started := true
	failed, err := core.SubmitHostActionResult(SubmitHostActionResultInput{
		AttemptID: launch.Attempt.ID,
		ActionID:  launch.LaunchAction.ID,
		Lease:     launch.LaunchAction.Lease,
		Result: orchestration.HostActionResult{
			Status: "failed", ExecutionStarted: &started, SideEffectState: "unknown",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if failed.Attempt.Status != orchestration.AttemptFailed || failed.Attempt.SideEffectState != "unknown" {
		t.Fatalf("failed attempt evidence = %#v", failed.Attempt)
	}
	if _, err := core.RequestCompletion(CompletionInput{AttemptID: failed.Attempt.ID}); err != nil {
		t.Fatal(err)
	}
	if _, err := core.RecordHumanDecision(HumanDecisionInput{NodeID: failed.Attempt.NodeID, Decision: "retry_node", AuthorityRef: "user-input:retry-unknown"}); err != nil {
		t.Fatal(err)
	}
	if _, err := core.PrepareAttempt(PrepareAttemptInput{NodeID: failed.Attempt.NodeID, PreviousAttemptID: failed.Attempt.ID, ControllerInstanceID: "controller"}); err == nil {
		t.Fatal("manual retry accepted an unknown side-effect outcome")
	} else if gatewayErr, ok := err.(*Error); !ok || gatewayErr.Code != "retry_not_allowed" {
		t.Fatalf("manual unknown-outcome retry error = %v", err)
	}
}

func TestFallbackRejectsWhitespacePaddedUnknownSideEffectOutcome(t *testing.T) {
	core, _, launch := preparedAttemptForTest(t)
	started := true
	failed, err := core.SubmitHostActionResult(SubmitHostActionResultInput{
		AttemptID: launch.Attempt.ID,
		ActionID:  launch.LaunchAction.ID,
		Lease:     launch.LaunchAction.Lease,
		Result: orchestration.HostActionResult{
			Status: "failed", ExecutionStarted: &started, SideEffectState: "   ",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if failed.Attempt.SideEffectState != "unknown" {
		t.Fatalf("side-effect state was not normalized: %#v", failed.Attempt)
	}
	if _, err := core.RequestCompletion(CompletionInput{AttemptID: failed.Attempt.ID}); err != nil {
		t.Fatal(err)
	}
	if _, err := core.RecordHumanDecision(HumanDecisionInput{NodeID: failed.Attempt.NodeID, AttemptID: failed.Attempt.ID, Decision: "approve", AuthorityRef: "user-input:fallback-approval"}); err != nil {
		t.Fatal(err)
	}
	if _, err := core.PrepareFallbackAttempt(FallbackAttemptInput{NodeID: failed.Attempt.NodeID, PreviousAttemptID: failed.Attempt.ID, ControllerInstanceID: "controller", Binding: launch.Attempt.Binding, Approved: true}); err == nil {
		t.Fatal("fallback accepted a whitespace-padded unknown side-effect outcome")
	} else if gatewayErr, ok := err.(*Error); !ok || gatewayErr.Code != "fallback_not_allowed" {
		t.Fatalf("whitespace unknown fallback error = %v", err)
	}
}

func TestFallbackAskRequiresGatewayHumanApproval(t *testing.T) {
	core, _, launch := preparedAttemptForTest(t)
	started := false
	failed, err := core.SubmitHostActionResult(SubmitHostActionResultInput{
		AttemptID: launch.Attempt.ID,
		ActionID:  launch.LaunchAction.ID,
		Lease:     launch.LaunchAction.Lease,
		Result: orchestration.HostActionResult{
			Status: "failed", ExecutionStarted: &started, SideEffectState: "none",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := core.RequestCompletion(CompletionInput{AttemptID: failed.Attempt.ID}); err != nil {
		t.Fatal(err)
	}
	if _, err := core.PrepareFallbackAttempt(FallbackAttemptInput{NodeID: failed.Attempt.NodeID, PreviousAttemptID: failed.Attempt.ID, ControllerInstanceID: "controller", Binding: launch.Attempt.Binding, Approved: true}); err == nil {
		t.Fatal("fallback ask mode accepted Approved=true without a core human decision")
	} else if gatewayErr, ok := err.(*Error); !ok || gatewayErr.Code != "fallback_approval_required" {
		t.Fatalf("fallback approval gate error = %v", err)
	}
}

func TestFallbackApprovalIsAttemptBoundAndDoesNotReleaseCheckpoint(t *testing.T) {
	core, _, launch := preparedAttemptForTest(t)
	started := false
	failed, err := core.SubmitHostActionResult(SubmitHostActionResultInput{
		AttemptID: launch.Attempt.ID,
		ActionID:  launch.LaunchAction.ID,
		Lease:     launch.LaunchAction.Lease,
		Result: orchestration.HostActionResult{
			Status: "failed", ExecutionStarted: &started, SideEffectState: "none",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	completion, err := core.RequestCompletion(CompletionInput{AttemptID: failed.Attempt.ID})
	if err != nil {
		t.Fatal(err)
	}
	if completion.Node.CheckpointAttemptID != failed.Attempt.ID {
		t.Fatalf("failed checkpoint attempt = %#v", completion.Node)
	}
	if _, err := core.RecordHumanDecision(HumanDecisionInput{NodeID: failed.Attempt.NodeID, Decision: "approve", AuthorityRef: "user-input:ordinary-approve"}); err == nil {
		t.Fatal("ordinary approval released a failed checkpoint")
	} else if gatewayErr, ok := err.(*Error); !ok || gatewayErr.Code != "fallback_attempt_required" {
		t.Fatalf("ordinary failed-checkpoint approval error = %v", err)
	}
	if _, err := core.ProposeNode(ProposeNodeInput{TaskID: failed.Attempt.TaskID, Kind: "downstream-before-fallback"}); err == nil {
		t.Fatal("failed checkpoint allowed a downstream node")
	}
	approved, err := core.RecordHumanDecision(HumanDecisionInput{NodeID: failed.Attempt.NodeID, AttemptID: failed.Attempt.ID, Decision: "approve", AuthorityRef: "user-input:fallback-approval"})
	if err != nil {
		t.Fatal(err)
	}
	if approved.Node.Status != orchestration.NodeAwaitingHumanDecision {
		t.Fatalf("fallback approval released node: %#v", approved.Node)
	}
	fallback, err := core.PrepareFallbackAttempt(FallbackAttemptInput{
		NodeID:               failed.Attempt.NodeID,
		PreviousAttemptID:    failed.Attempt.ID,
		ControllerInstanceID: "controller",
		Binding:              launch.Attempt.Binding,
		Approved:             true,
	})
	if err != nil {
		t.Fatal(err)
	}
	started = true
	if _, err := core.SubmitHostActionResult(SubmitHostActionResultInput{
		AttemptID: fallback.Attempt.ID,
		ActionID:  fallback.LaunchAction.ID,
		Lease:     fallback.LaunchAction.Lease,
		Result: orchestration.HostActionResult{
			Status: "completed", Terminal: true, ProviderHandle: "fallback-handle", ProviderSessionID: "fallback-session",
			ExecutionStarted: &started, SideEffectState: "none", Identity: map[string]string{
				"provider": "fake", "session": "fallback-session", "workspace": fallback.Attempt.Binding.Workspace,
			}, TaskCanary: fallback.Attempt.TaskCanary,
		},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := core.PrepareFallbackAttempt(FallbackAttemptInput{
		NodeID: failed.Attempt.NodeID, PreviousAttemptID: failed.Attempt.ID, ControllerInstanceID: "controller",
		Binding: launch.Attempt.Binding, Approved: true,
	}); err == nil {
		t.Fatal("fallback approval was reusable after the fallback Attempt became terminal")
	} else if gatewayErr, ok := err.(*Error); !ok || gatewayErr.Code != "fallback_already_prepared" {
		t.Fatalf("reused fallback approval error = %v", err)
	}
	completed, err := core.RequestCompletion(CompletionInput{AttemptID: fallback.Attempt.ID})
	if err != nil {
		t.Fatal(err)
	}
	if completed.Node.CheckpointAttemptID != fallback.Attempt.ID || completed.Node.Status != orchestration.NodeAwaitingHumanDecision {
		t.Fatalf("fallback completion checkpoint = %#v", completed.Node)
	}
	approved, err = core.RecordHumanDecision(HumanDecisionInput{NodeID: fallback.Attempt.NodeID, Decision: "approve", AuthorityRef: "user-input:approve-fallback-result"})
	if err != nil || approved.Node.Status != orchestration.NodeApproved {
		t.Fatalf("ordinary approval after fallback completion = result=%#v err=%v", approved, err)
	}
	if _, err := core.ProposeNode(ProposeNodeInput{TaskID: fallback.Attempt.TaskID, Kind: "downstream-after-fallback"}); err != nil {
		t.Fatalf("completed and approved fallback did not release downstream: %v", err)
	}
}

func TestAutomaticRetryComputesLineageCountInsteadOfTrustingCaller(t *testing.T) {
	core, store, launch := preparedAttemptForTest(t)
	started := false
	failed, err := core.SubmitHostActionResult(SubmitHostActionResultInput{
		AttemptID: launch.Attempt.ID,
		ActionID:  launch.LaunchAction.ID,
		Lease:     launch.LaunchAction.Lease,
		Result: orchestration.HostActionResult{
			Status: "failed", ExecutionStarted: &started, SideEffectState: "none",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	firstRetry, err := core.PrepareRetryAttempt(RetryAttemptInput{
		NodeID: failed.Attempt.NodeID, PreviousAttemptID: failed.Attempt.ID, ControllerInstanceID: "controller",
		ErrorCode: "transport_before_session", RetriesAlreadyUsed: 99,
	})
	if err != nil {
		t.Fatalf("caller-provided high retry count incorrectly blocked first retry: %v", err)
	}
	state, err := store.Load(launch.Attempt.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	retryAttempt := state.Attempts[firstRetry.Attempt.ID]
	retryAttempt.Status = orchestration.AttemptFailed
	retryAttempt.ExecutionStarted = false
	retryAttempt.SideEffectState = "none"
	state.Attempts[retryAttempt.ID] = retryAttempt
	if err := store.Save(state); err != nil {
		t.Fatal(err)
	}
	if _, err := core.PrepareRetryAttempt(RetryAttemptInput{
		NodeID: failed.Attempt.NodeID, PreviousAttemptID: retryAttempt.ID, ControllerInstanceID: "controller",
		ErrorCode: "transport_before_session", RetriesAlreadyUsed: 0,
	}); err == nil {
		t.Fatal("caller-provided zero retry count bypassed the configured retry limit")
	} else if gatewayErr, ok := err.(*Error); !ok || gatewayErr.Code != "retry_not_allowed" {
		t.Fatalf("lineage retry-limit error = %v", err)
	}
}

func TestPrepareAttemptRejectsDuplicateInFlightLaunch(t *testing.T) {
	core, _, launch := preparedAttemptForTest(t)
	started := false
	failed, err := core.SubmitHostActionResult(SubmitHostActionResultInput{AttemptID: launch.Attempt.ID, ActionID: launch.LaunchAction.ID, Lease: launch.LaunchAction.Lease, Result: orchestration.HostActionResult{Status: "failed", ExecutionStarted: &started, SideEffectState: "none"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := core.PrepareAttempt(PrepareAttemptInput{NodeID: launch.Attempt.NodeID, ControllerInstanceID: "second-controller"}); err == nil {
		t.Fatal("retry launch without lineage was accepted")
	} else if gatewayErr, ok := err.(*Error); !ok || gatewayErr.Code != "retry_lineage_required" {
		t.Fatalf("missing-lineage error = %v", err)
	}
	if _, err := core.PrepareRetryAttempt(RetryAttemptInput{NodeID: launch.Attempt.NodeID, PreviousAttemptID: failed.Attempt.ID, ControllerInstanceID: "controller", ErrorCode: "transport_before_session"}); err != nil {
		t.Fatal(err)
	}
	if _, err := core.PrepareRetryAttempt(RetryAttemptInput{NodeID: launch.Attempt.NodeID, PreviousAttemptID: failed.Attempt.ID, ControllerInstanceID: "controller", ErrorCode: "transport_before_session"}); err == nil {
		t.Fatal("duplicate in-flight retry was accepted")
	} else if gatewayErr, ok := err.(*Error); !ok || gatewayErr.Code != "attempt_in_flight" {
		t.Fatalf("duplicate retry error = %v", err)
	}
}

func TestPrepareAttemptRetryLineageRequiresHumanRetryDecision(t *testing.T) {
	core, store, launch := preparedAttemptForTest(t)
	started := false
	failed, err := core.SubmitHostActionResult(SubmitHostActionResultInput{
		AttemptID: launch.Attempt.ID,
		ActionID:  launch.LaunchAction.ID,
		Lease:     launch.LaunchAction.Lease,
		Result:    orchestration.HostActionResult{Status: "failed", ExecutionStarted: &started, SideEffectState: "none"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := core.PrepareAttempt(PrepareAttemptInput{NodeID: launch.Attempt.NodeID, ControllerInstanceID: "controller"}); err == nil {
		t.Fatal("failed Attempt was relaunched without a predecessor")
	} else if gatewayErr, ok := err.(*Error); !ok || gatewayErr.Code != "retry_lineage_required" {
		t.Fatalf("missing retry lineage error = %v", err)
	}
	if _, err := core.PrepareAttempt(PrepareAttemptInput{NodeID: launch.Attempt.NodeID, PreviousAttemptID: failed.Attempt.ID, ControllerInstanceID: "controller"}); err == nil {
		t.Fatal("generic PrepareAttempt bypassed the retry checkpoint")
	} else if gatewayErr, ok := err.(*Error); !ok || gatewayErr.Code != "manual_retry_approval_required" {
		t.Fatalf("manual retry bypass error = %v", err)
	}
	state, err := store.Load(launch.Attempt.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	node := state.Nodes[launch.Attempt.NodeID]
	node.Status = orchestration.NodeAwaitingHumanDecision
	state.Nodes[node.ID] = node
	if err := store.Save(state); err != nil {
		t.Fatal(err)
	}
	if _, err := core.RecordHumanDecision(HumanDecisionInput{NodeID: node.ID, Decision: "retry_node", AuthorityRef: "user-input:retry"}); err != nil {
		t.Fatal(err)
	}
	manualRetry, err := core.PrepareAttempt(PrepareAttemptInput{NodeID: node.ID, PreviousAttemptID: failed.Attempt.ID, ControllerInstanceID: "controller"})
	if err != nil {
		t.Fatal(err)
	}
	if manualRetry.Attempt.RetryOf != failed.Attempt.ID {
		t.Fatalf("manual retry lineage = %#v", manualRetry.Attempt)
	}
}

func TestIdentitySessionMismatchContaminatesAttempt(t *testing.T) {
	core, _, launch := preparedAttemptForTest(t)
	started := true
	running, err := core.SubmitHostActionResult(SubmitHostActionResultInput{
		AttemptID: launch.Attempt.ID,
		ActionID:  launch.LaunchAction.ID,
		Lease:     launch.LaunchAction.Lease,
		Result: orchestration.HostActionResult{
			Status: "running", ProviderHandle: "handle", ProviderSessionID: "session-a", ExecutionStarted: &started,
			SideEffectState: "none", Identity: map[string]string{"provider": "fake", "session": "session-a", "workspace": launch.Attempt.Binding.Workspace}, TaskCanary: launch.Attempt.TaskCanary,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	observe := running.Next[0]
	contaminated, err := core.SubmitHostActionResult(SubmitHostActionResultInput{
		AttemptID: launch.Attempt.ID,
		ActionID:  observe.ID,
		Lease:     observe.Lease,
		Result: orchestration.HostActionResult{
			Status: "completed", Terminal: true, ProviderHandle: "handle", ProviderSessionID: "session-b",
			SideEffectState: "none", Identity: map[string]string{"provider": "fake", "session": "session-b", "workspace": launch.Attempt.Binding.Workspace},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if contaminated.Attempt.Status != orchestration.AttemptContaminated || len(contaminated.Next) != 0 {
		t.Fatalf("session mismatch was not frozen: %#v", contaminated)
	}
}

func TestMissingExecutionEvidenceCannotEnterAutomaticRetry(t *testing.T) {
	core, store, launch := preparedAttemptForTest(t)
	failed, err := core.SubmitHostActionResult(SubmitHostActionResultInput{
		AttemptID: launch.Attempt.ID,
		ActionID:  launch.LaunchAction.ID,
		Lease:     launch.LaunchAction.Lease,
		Result:    orchestration.HostActionResult{Status: "failed"},
	})
	if err != nil {
		t.Fatal(err)
	}
	state, err := store.Load(launch.Attempt.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	if state.Attempts[failed.Attempt.ID].SideEffectState != "unknown" {
		t.Fatalf("missing execution evidence was not marked unknown: %#v", state.Attempts[failed.Attempt.ID])
	}
	if _, err := core.PrepareRetryAttempt(RetryAttemptInput{NodeID: launch.Attempt.NodeID, PreviousAttemptID: failed.Attempt.ID, ControllerInstanceID: "controller", ErrorCode: "transport_before_session"}); err == nil {
		t.Fatal("failed launch with missing execution evidence entered automatic retry")
	} else if gatewayErr, ok := err.(*Error); !ok || gatewayErr.Code != "retry_not_allowed" {
		t.Fatalf("missing evidence retry error = %v", err)
	}
}

func TestWorkspaceIdentityMismatchContaminatesAttempt(t *testing.T) {
	core, _, launch := preparedAttemptForTest(t)
	started := true
	outside := filepath.Join(t.TempDir(), "outside")
	if err := os.Mkdir(outside, 0o700); err != nil {
		t.Fatal(err)
	}
	receipt, err := core.SubmitHostActionResult(SubmitHostActionResultInput{
		AttemptID: launch.Attempt.ID,
		ActionID:  launch.LaunchAction.ID,
		Lease:     launch.LaunchAction.Lease,
		Result: orchestration.HostActionResult{
			Status: "running", ProviderHandle: "handle", ProviderSessionID: "session", ExecutionStarted: &started,
			SideEffectState: "none", Identity: map[string]string{"provider": "fake", "session": "session", "workspace": outside}, TaskCanary: launch.Attempt.TaskCanary,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if receipt.Attempt.Status != orchestration.AttemptContaminated || len(receipt.Next) != 0 {
		t.Fatalf("workspace mismatch was not frozen: %#v", receipt)
	}
}

func TestMCPIsolationIsRecordedAcrossPreflightLaunchAndObserveEvidence(t *testing.T) {
	core, store, launch := preparedAttemptForTest(t)
	state, err := store.Load(launch.Attempt.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	node := state.Nodes[launch.Attempt.NodeID]
	if node.PreflightMCPIsolation != "strict" {
		t.Fatalf("preflight MCP isolation = %q, want strict", node.PreflightMCPIsolation)
	}
	if launch.Attempt.MCPIsolation != "strict" || launch.LaunchAction.Parameters["mcp_isolation"] != "strict" {
		t.Fatalf("launch MCP evidence = attempt=%q parameters=%#v", launch.Attempt.MCPIsolation, launch.LaunchAction.Parameters)
	}
	started := true
	running, err := core.SubmitHostActionResult(SubmitHostActionResultInput{
		AttemptID: launch.Attempt.ID,
		ActionID:  launch.LaunchAction.ID,
		Lease:     launch.LaunchAction.Lease,
		Result: orchestration.HostActionResult{
			Status: "running", ProviderHandle: "handle", ProviderSessionID: "session", ExecutionStarted: &started,
			SideEffectState: "none", Identity: map[string]string{"provider": "fake", "session": "session", "workspace": launch.Attempt.Binding.Workspace}, TaskCanary: launch.Attempt.TaskCanary,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(running.Next) != 1 || running.Next[0].Parameters["mcp_isolation"] != "strict" {
		t.Fatalf("observe MCP evidence = %#v", running.Next)
	}
	if _, err := core.SubmitHostActionResult(SubmitHostActionResultInput{
		AttemptID: launch.Attempt.ID,
		ActionID:  running.Next[0].ID,
		Lease:     running.Next[0].Lease,
		Result: orchestration.HostActionResult{
			Status: "completed", Terminal: true, ProviderHandle: "handle", ProviderSessionID: "session", SideEffectState: "none",
			Identity: map[string]string{"provider": "fake", "session": "session", "workspace": launch.Attempt.Binding.Workspace},
		},
	}); err != nil {
		t.Fatal(err)
	}
	state, err = store.Load(launch.Attempt.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	preflightEvents, actionEvents := 0, 0
	for _, event := range state.Events {
		if event.Type == "node.preflight" {
			if event.Data["mcp_isolation"] != "strict" {
				t.Fatalf("preflight event MCP evidence = %#v", event.Data)
			}
			preflightEvents++
		}
		if event.Type == "host_action.result" {
			if event.Data["mcp_isolation"] != "strict" {
				t.Fatalf("host action event MCP evidence = %#v", event.Data)
			}
			actionEvents++
		}
	}
	if preflightEvents != 1 || actionEvents != 2 {
		t.Fatalf("MCP evidence event counts: preflight=%d actions=%d", preflightEvents, actionEvents)
	}
}

func TestBindProviderIdentityMismatchIsPersistedAsContamination(t *testing.T) {
	core, store, launch := preparedAttemptForTest(t)
	started := true
	if _, err := core.SubmitHostActionResult(SubmitHostActionResultInput{
		AttemptID: launch.Attempt.ID,
		ActionID:  launch.LaunchAction.ID,
		Lease:     launch.LaunchAction.Lease,
		Result: orchestration.HostActionResult{
			Status: "running", ProviderHandle: "handle", ProviderSessionID: "session", ExecutionStarted: &started,
			SideEffectState: "none", Identity: map[string]string{"provider": "fake", "session": "session", "workspace": launch.Attempt.Binding.Workspace}, TaskCanary: launch.Attempt.TaskCanary,
		},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := core.BindProviderHandle(BindProviderHandleInput{AttemptID: launch.Attempt.ID, LaunchActionID: launch.LaunchAction.ID, ProviderHandle: "handle", ProviderSessionID: "foreign-session", Identity: map[string]string{"provider": "fake", "session": "foreign-session"}}); err == nil {
		t.Fatal("provider identity mismatch was accepted")
	} else if gatewayErr, ok := err.(*Error); !ok || gatewayErr.Code != "identity_mismatch" {
		t.Fatalf("bind identity mismatch error = %v", err)
	}
	state, err := store.Load(launch.Attempt.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	if state.Attempts[launch.Attempt.ID].Status != orchestration.AttemptContaminated {
		t.Fatalf("bind identity mismatch was not persisted: %#v", state.Attempts[launch.Attempt.ID])
	}
}

func TestCompletionRequiresCoreTerminalEvent(t *testing.T) {
	core, store, launch := preparedAttemptForTest(t)
	started := true
	terminal, err := core.SubmitHostActionResult(SubmitHostActionResultInput{
		AttemptID: launch.Attempt.ID,
		ActionID:  launch.LaunchAction.ID,
		Lease:     launch.LaunchAction.Lease,
		Result: orchestration.HostActionResult{
			Status: "completed", Terminal: true, ProviderHandle: "handle", ProviderSessionID: "session", ExecutionStarted: &started,
			SideEffectState: "none", Identity: map[string]string{"provider": "fake", "session": "session", "workspace": launch.Attempt.Binding.Workspace}, TaskCanary: launch.Attempt.TaskCanary,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	state, err := store.Load(launch.Attempt.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	attempt := state.Attempts[launch.Attempt.ID]
	attempt.TerminalEventID = ""
	state.Attempts[attempt.ID] = attempt
	if err := store.Save(state); err != nil {
		t.Fatal(err)
	}
	completion, err := core.RequestCompletion(CompletionInput{AttemptID: terminal.Attempt.ID})
	if err != nil {
		t.Fatal(err)
	}
	if completion.Decision.Status != orchestration.AttemptIncomplete || completion.Decision.Reason != "terminal_event_missing" {
		t.Fatalf("completion without terminal event = %#v", completion.Decision)
	}
}

func TestHumanCheckpointBlocksSameNodeAndDownstreamPreparation(t *testing.T) {
	core, _, launch := preparedAttemptForTest(t)
	started := true
	if _, err := core.SubmitHostActionResult(SubmitHostActionResultInput{
		AttemptID: launch.Attempt.ID,
		ActionID:  launch.LaunchAction.ID,
		Lease:     launch.LaunchAction.Lease,
		Result: orchestration.HostActionResult{
			Status: "completed", Terminal: true, ProviderHandle: "handle", ProviderSessionID: "session", ExecutionStarted: &started,
			SideEffectState: "none", Identity: map[string]string{"provider": "fake", "session": "session", "workspace": launch.Attempt.Binding.Workspace}, TaskCanary: launch.Attempt.TaskCanary,
		},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := core.RequestCompletion(CompletionInput{AttemptID: launch.Attempt.ID}); err != nil {
		t.Fatal(err)
	}
	if _, err := core.PrepareAttempt(PrepareAttemptInput{NodeID: launch.Attempt.NodeID, ControllerInstanceID: "controller"}); err == nil {
		t.Fatal("same node was prepared while awaiting human decision")
	} else if gatewayErr, ok := err.(*Error); !ok || gatewayErr.Code != "checkpoint_required" {
		t.Fatalf("same-node checkpoint error = %v", err)
	}
	state, err := core.store.Load(launch.Attempt.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := core.ProposeNode(ProposeNodeInput{TaskID: state.Task.ID, Kind: "next"}); err == nil {
		t.Fatal("downstream node was proposed before checkpoint approval")
	} else if gatewayErr, ok := err.(*Error); !ok || gatewayErr.Code != "checkpoint_required" {
		t.Fatalf("downstream checkpoint error = %v", err)
	}
}

func TestRetryAndFallbackCreateImmutableAttemptLineage(t *testing.T) {
	root := t.TempDir()
	workspace := filepath.Join(root, "workspace")
	if err := os.Mkdir(workspace, 0o700); err != nil {
		t.Fatal(err)
	}
	store, err := storage.New(filepath.Join(root, "state"))
	if err != nil {
		t.Fatal(err)
	}
	originalBinding := orchestration.Binding{Role: "reviewer", Adapter: "fake", Model: "fixture", Reasoning: "none", Permission: "read-only", Workspace: workspace}
	fallbackBinding := originalBinding
	fallbackBinding.Adapter = "other-registered-adapter"
	config := policy.DefaultConfig()
	config.RegisteredBindings = []orchestration.Binding{originalBinding, fallbackBinding}
	core, err := New(store, config)
	if err != nil {
		t.Fatal(err)
	}
	task, err := core.CreateTask(CreateTaskInput{Objective: "retry lineage", Workspace: workspace})
	if err != nil {
		t.Fatal(err)
	}
	node, err := core.ProposeNode(ProposeNodeInput{TaskID: task.Task.ID, Kind: "review"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := core.RunPreflight(RunPreflightInput{NodeID: node.Node.ID, Binding: originalBinding}); err != nil {
		t.Fatal(err)
	}
	launch, err := core.PrepareAttempt(PrepareAttemptInput{NodeID: node.Node.ID, ControllerInstanceID: "controller"})
	if err != nil {
		t.Fatal(err)
	}
	started := false
	failed, err := core.SubmitHostActionResult(SubmitHostActionResultInput{AttemptID: launch.Attempt.ID, ActionID: launch.LaunchAction.ID, Lease: launch.LaunchAction.Lease, Result: orchestration.HostActionResult{Status: "failed", ExecutionStarted: &started, SideEffectState: "none"}})
	if err != nil {
		t.Fatal(err)
	}
	retry, err := core.PrepareRetryAttempt(RetryAttemptInput{NodeID: node.Node.ID, PreviousAttemptID: failed.Attempt.ID, ControllerInstanceID: "controller", ErrorCode: "transport_before_session"})
	if err != nil {
		t.Fatal(err)
	}
	if retry.Attempt.RetryOf != failed.Attempt.ID || retry.Attempt.ID == failed.Attempt.ID {
		t.Fatalf("retry lineage = %#v", retry.Attempt)
	}
	retryState, err := store.Load(task.Task.ID)
	if err != nil {
		t.Fatal(err)
	}
	retryAttempt := retryState.Attempts[retry.Attempt.ID]
	retryAttempt.Status = orchestration.AttemptFailed
	retryState.Attempts[retryAttempt.ID] = retryAttempt
	if err := store.Save(retryState); err != nil {
		t.Fatal(err)
	}
	if _, err := core.RequestCompletion(CompletionInput{AttemptID: failed.Attempt.ID}); err != nil {
		t.Fatal(err)
	}
	if _, err := core.RecordHumanDecision(HumanDecisionInput{NodeID: node.Node.ID, AttemptID: failed.Attempt.ID, Decision: "approve", AuthorityRef: "user-input:fallback-approval"}); err != nil {
		t.Fatal(err)
	}
	fallback, err := core.PrepareFallbackAttempt(FallbackAttemptInput{NodeID: node.Node.ID, PreviousAttemptID: failed.Attempt.ID, ControllerInstanceID: "controller", Binding: fallbackBinding, Approved: true})
	if err != nil {
		t.Fatal(err)
	}
	if fallback.Attempt.FallbackOf != failed.Attempt.ID || fallback.Attempt.Binding.Adapter != fallbackBinding.Adapter {
		t.Fatalf("fallback lineage = %#v", fallback.Attempt)
	}
	state, err := store.Load(task.Task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if state.Attempts[failed.Attempt.ID].RetryOf != "" || state.Attempts[failed.Attempt.ID].FallbackOf != "" {
		t.Fatal("original attempt was mutated by retry/fallback")
	}
}

func TestFallbackRunsCandidatePreflightBeforeIssuingLaunch(t *testing.T) {
	root := t.TempDir()
	workspace := filepath.Join(root, "workspace")
	if err := os.Mkdir(workspace, 0o700); err != nil {
		t.Fatal(err)
	}
	store, err := storage.New(filepath.Join(root, "state"))
	if err != nil {
		t.Fatal(err)
	}
	original := orchestration.Binding{Role: "reviewer", Adapter: "fake", Model: "fixture", Reasoning: "none", Permission: "read-only", Workspace: workspace}
	candidate := original
	candidate.Adapter = "mcp-adapter"
	candidate.MCPAllowlist = []string{"evidence"}
	config := policy.DefaultConfig()
	config.MCPIsolation = "unsupported"
	config.FallbackMode = "auto_registered"
	config.RegisteredBindings = []orchestration.Binding{original, candidate}
	core, err := New(store, config)
	if err != nil {
		t.Fatal(err)
	}
	task, err := core.CreateTask(CreateTaskInput{Objective: "fallback MCP gate", Workspace: workspace})
	if err != nil {
		t.Fatal(err)
	}
	node, err := core.ProposeNode(ProposeNodeInput{TaskID: task.Task.ID, Kind: "review"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := core.RunPreflight(RunPreflightInput{NodeID: node.Node.ID, Binding: original}); err != nil {
		t.Fatal(err)
	}
	launch, err := core.PrepareAttempt(PrepareAttemptInput{NodeID: node.Node.ID, ControllerInstanceID: "controller"})
	if err != nil {
		t.Fatal(err)
	}
	started := false
	failed, err := core.SubmitHostActionResult(SubmitHostActionResultInput{AttemptID: launch.Attempt.ID, ActionID: launch.LaunchAction.ID, Lease: launch.LaunchAction.Lease, Result: orchestration.HostActionResult{Status: "failed", ExecutionStarted: &started, SideEffectState: "none"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := core.RequestCompletion(CompletionInput{AttemptID: failed.Attempt.ID}); err != nil {
		t.Fatal(err)
	}
	if _, err := core.PrepareFallbackAttempt(FallbackAttemptInput{NodeID: node.Node.ID, PreviousAttemptID: failed.Attempt.ID, ControllerInstanceID: "controller", Binding: candidate, Approved: true}); err == nil {
		t.Fatal("fallback bypassed candidate MCP preflight")
	} else if gatewayErr, ok := err.(*Error); !ok || gatewayErr.Code != "fallback_preflight_blocked" {
		t.Fatalf("fallback MCP preflight error = %v", err)
	}
}

func TestRequestRevisionCreatesNewRevisionAndPreservesCheckpointHistory(t *testing.T) {
	root := t.TempDir()
	workspace := filepath.Join(root, "workspace")
	if err := os.Mkdir(workspace, 0o700); err != nil {
		t.Fatal(err)
	}
	store, err := storage.New(filepath.Join(root, "state"))
	if err != nil {
		t.Fatal(err)
	}
	core, err := New(store, policy.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	task, err := core.CreateTask(CreateTaskInput{Objective: "revision history", Workspace: workspace})
	if err != nil {
		t.Fatal(err)
	}
	node, err := core.ProposeNode(ProposeNodeInput{TaskID: task.Task.ID, Kind: "review"})
	if err != nil {
		t.Fatal(err)
	}
	state, err := store.Load(task.Task.ID)
	if err != nil {
		t.Fatal(err)
	}
	proposed := state.Nodes[node.Node.ID]
	proposed.Status = orchestration.NodeAwaitingHumanDecision
	state.Nodes[proposed.ID] = proposed
	if err := store.Save(state); err != nil {
		t.Fatal(err)
	}
	decision, err := core.RecordHumanDecision(HumanDecisionInput{NodeID: node.Node.ID, Decision: "request_revision", AuthorityRef: "user-input:revision", Instruction: "tighten the review scope"})
	if err != nil {
		t.Fatal(err)
	}
	if decision.NewNode == nil || decision.NewNode.RevisionID == task.Revision.ID {
		t.Fatalf("revision decision did not create a new node: %#v", decision)
	}
	recovered, err := store.Load(task.Task.ID)
	if err != nil {
		t.Fatal(err)
	}
	old := recovered.Revisions[task.Revision.ID]
	newRevision := recovered.Revisions[decision.NewNode.RevisionID]
	if old.Instruction != task.Revision.Instruction || newRevision.ParentRevisionID != old.ID || recovered.Task.CurrentRevisionID != newRevision.ID {
		t.Fatalf("revision history = old=%#v new=%#v task=%#v", old, newRevision, recovered.Task)
	}
}
