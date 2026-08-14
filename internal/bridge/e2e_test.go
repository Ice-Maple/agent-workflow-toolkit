package bridge

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"agent-workflow-toolkit/internal/coordination"
	"agent-workflow-toolkit/internal/gateway"
	"agent-workflow-toolkit/internal/orchestration"
	"agent-workflow-toolkit/internal/policy"
	"agent-workflow-toolkit/internal/storage"
)

func TestPublicBridgeRoleFirstHandoff(t *testing.T) {
	for _, adapter := range []string{ProviderCursor, ProviderClaude, ProviderCodexCLI} {
		t.Run(adapter, func(t *testing.T) {
			runPublicBridgeRoleFirstHandoff(t, adapter)
		})
	}
}

func TestPublicBridgePublishesOwnerReadyBeforeSlowCursorSession(t *testing.T) {
	runPublicBridgeRoleFirstHandoffWithOptions(t, ProviderCursor, buildBridgeExecutable(t), buildCoreExecutable(t), roleFirstE2EOptions{
		providerModel:                    "fixture-slow-model",
		expectReadyBeforeProviderSession: true,
	})
}

func TestDisableCancelsActiveExternalOwnerWithoutPanic(t *testing.T) {
	runPublicBridgeRoleFirstHandoffWithOptions(t, ProviderCursor, buildBridgeExecutable(t), buildCoreExecutable(t), roleFirstE2EOptions{
		disableWhileRunning: true,
	})
}

func TestFreshConsumerReceiptPinnedBridgeRoleFirstHandoff(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("the Role-first installer is currently a Windows adoption path")
	}
	powershell, err := exec.LookPath("powershell.exe")
	if err != nil {
		t.Skip("Windows PowerShell is unavailable")
	}
	repositoryRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	consumer := t.TempDir()
	coreBinary := buildCoreExecutable(t)
	bridgeBinary := buildBridgeExecutable(t)
	collaborationBinary := buildCollaborationExecutable(t)
	installer := filepath.Join(repositoryRoot, "tools", "orchestration", "install_role_first_skill.ps1")
	fixture := filepath.Join(repositoryRoot, "tests", "orchestration", "fixtures", "role-first")
	arguments := []string{
		"-NoProfile", "-ExecutionPolicy", "Bypass", "-File", installer,
		"-ToolkitRoot", repositoryRoot, "-ConsumerRoot", consumer,
		"-SkillSource", filepath.Join(fixture, "skill"), "-RolePackage", filepath.Join(fixture, "roles"),
		"-CorePath", coreBinary, "-CollaborationPath", collaborationBinary, "-BridgePath", bridgeBinary,
		"-AllowDirty",
	}
	if output, err := exec.Command(powershell, arguments...).CombinedOutput(); err != nil {
		t.Fatalf("install fresh consumer: %v\n%s", err, output)
	}
	receiptRaw, err := os.ReadFile(filepath.Join(consumer, ".agents", "agent-workflow-toolkit.lock.json"))
	if err != nil {
		t.Fatal(err)
	}
	var receipt struct {
		Binaries map[string]struct {
			Path string `json:"path"`
		} `json:"binaries"`
	}
	if err := json.Unmarshal(receiptRaw, &receipt); err != nil {
		t.Fatal(err)
	}
	receiptCore := receipt.Binaries["core"].Path
	receiptBridge := receipt.Binaries["bridge"].Path
	if receiptCore != coreBinary || receiptBridge != bridgeBinary {
		t.Fatalf("receipt paths core=%q bridge=%q", receiptCore, receiptBridge)
	}
	runPublicBridgeRoleFirstHandoffWithBinaries(t, ProviderCursor, receiptBridge, receiptCore)
}

func runPublicBridgeRoleFirstHandoff(t *testing.T, adapter string) {
	t.Helper()
	runPublicBridgeRoleFirstHandoffWithBinaries(t, adapter, buildBridgeExecutable(t), buildCoreExecutable(t))
}

func runPublicBridgeRoleFirstHandoffWithBinaries(t *testing.T, adapter, bridgeBinary, coreBinary string) {
	t.Helper()
	runPublicBridgeRoleFirstHandoffWithOptions(t, adapter, bridgeBinary, coreBinary, roleFirstE2EOptions{})
}

type roleFirstE2EOptions struct {
	workspace                        string
	providerModel                    string
	providerExecutable               string
	stepTask                         string
	expectedFinalText                string
	completionDeadline               time.Duration
	readyDeadline                    time.Duration
	runAdapterPreflight              bool
	expectReadyBeforeProviderSession bool
	disableWhileRunning              bool
}

func runPublicBridgeRoleFirstHandoffWithOptions(t *testing.T, adapter, bridgeBinary, coreBinary string, options roleFirstE2EOptions) {
	t.Helper()
	root := t.TempDir()
	workspace := options.workspace
	if workspace == "" {
		workspace = filepath.Join(root, "workspace")
		if err := os.Mkdir(workspace, 0o700); err != nil {
			t.Fatal(err)
		}
	} else {
		var err error
		workspace, err = filepath.Abs(filepath.Clean(workspace))
		if err != nil {
			t.Fatal(err)
		}
		if info, err := os.Stat(workspace); err != nil || !info.IsDir() {
			t.Fatalf("real Provider workspace is unavailable: %s err=%v", workspace, err)
		}
	}
	stateDir := filepath.Join(root, "state")
	coordinationDir := filepath.Join(root, "coordination")
	bridgeStateDir := filepath.Join(root, "bridge")
	providerBinary := options.providerExecutable
	if providerBinary == "" {
		providerBinary = buildFixtureProvider(t)
	}
	profile := fixtureProfile(t, adapter, providerBinary)
	role := "fixture/reviewer"
	promptPath := filepath.Join(root, "reviewer.md")
	prompt := "# Reviewer\n\nReturn the requested token.\n\nTask: {{ task }}\n\nOutput: {{ output_requirements }}\n"
	if err := os.WriteFile(promptPath, []byte(prompt), 0o600); err != nil {
		t.Fatal(err)
	}
	definition := orchestration.RoleDefinition{ID: role, Package: "fixture", Version: "1.0.0", PromptPath: promptPath, PromptHash: hashText(prompt)}
	providerModel := options.providerModel
	if providerModel == "" {
		providerModel = "fixture-model"
	}
	binding := orchestration.Binding{Role: role, Adapter: adapter, ProfileID: profile.ID, Model: providerModel, Reasoning: "none", Permission: "read-only", Workspace: workspace}
	if options.runAdapterPreflight {
		preflightContext, cancelPreflight := context.WithTimeout(t.Context(), 2*time.Minute)
		defer cancelPreflight()
		preflight, err := AdapterPreflight(preflightContext, binding, profile)
		if err != nil || preflight.Status != "passed" {
			t.Fatalf("real Adapter preflight err=%v result=%#v", err, preflight)
		}
	}
	config := policy.DefaultConfig()
	config.RegisteredBindings = []orchestration.Binding{binding}
	config.RoleRoutes = map[string]orchestration.RoleRoute{role: {Role: role, Primary: binding}}
	config.RoleDefinitions = map[string]orchestration.RoleDefinition{role: definition}
	config.AdapterProfiles = map[string]orchestration.AdapterProfile{profile.ID: profile.AdapterProfile}
	policyPath := filepath.Join(root, "policy.json")
	writeJSONFile(t, policyPath, config)
	profilePath := filepath.Join(root, "profile.json")
	writeJSONFile(t, profilePath, profile)
	rolesPath := filepath.Join(root, "roles.yaml")
	rolesYAML := "schema_version: collaboration-roles/v1\npackage: fixture\nversion: 1.0.0\nroles:\n  reviewer:\n    prompt: reviewer.md\n"
	if err := os.WriteFile(rolesPath, []byte(rolesYAML), 0o600); err != nil {
		t.Fatal(err)
	}

	store, err := storage.New(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	manager, err := coordination.New(coordinationDir, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.SetFrameworkEnabled(true); err != nil {
		t.Fatal(err)
	}
	status, err := manager.Status(workspace)
	if err != nil {
		t.Fatal(err)
	}
	core, err := gateway.New(store, config)
	if err != nil {
		t.Fatal(err)
	}
	task, err := core.CreateTask(gateway.CreateTaskInput{Objective: "public bridge role-first handoff", Workspace: workspace})
	if err != nil {
		t.Fatal(err)
	}
	locks, err := manager.AcquireRunLocks(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	core, err = gateway.NewWithCoordinationLocked(store, config, manager)
	if err != nil {
		_ = locks.Release()
		t.Fatal(err)
	}
	stepTask := options.stepTask
	if stepTask == "" {
		stepTask = "Return ADAPTER_SMOKE_OK"
	}
	plan := orchestration.CollaborationPlan{SchemaVersion: "collaboration/v1", RolePromptHashes: map[string]string{role: definition.PromptHash}, Steps: []orchestration.CollaborationStepSpec{{ID: "review", Role: role, Task: stepTask}}}
	run, err := core.CreateCollaborationRun(gateway.CreateCollaborationRunInput{TaskID: task.Task.ID, RevisionID: task.Revision.ID, Workspace: workspace, EffectiveMode: orchestration.CollaborationFramework, GateEpoch: status.GateEpoch, Plan: plan, StateDir: stateDir})
	if err != nil {
		_ = locks.Release()
		t.Fatal(err)
	}
	prepared, err := core.PrepareCollaborationInstance(gateway.PrepareCollaborationInstanceInput{RunID: run.Run.ID, InstanceID: run.Instances[0].ID, GateEpoch: status.GateEpoch, Kind: "bridge-e2e"})
	if err != nil {
		_ = locks.Release()
		t.Fatal(err)
	}
	launch, err := core.PrepareAttempt(gateway.PrepareAttemptInput{NodeID: prepared.Node.ID, ControllerInstanceID: "controller-restart-safe", CollaborationRunID: run.Run.ID, InstanceRunID: prepared.Instance.ID, GateEpoch: status.GateEpoch, RolePromptHash: definition.PromptHash})
	if err != nil {
		_ = locks.Release()
		t.Fatal(err)
	}
	if err := locks.Release(); err != nil {
		t.Fatal(err)
	}
	polled, err := (CoreClient{Executable: coreBinary, StateDir: stateDir, PolicyFile: policyPath, RolesManifest: rolesPath, CoordinationDir: coordinationDir}).PollHostActions(t.Context(), gateway.PollHostActionsInput{AttemptID: launch.Attempt.ID, ControllerInstanceID: "controller-restart-safe"})
	if err != nil {
		t.Fatal(err)
	}
	if len(polled.Actions) != 1 || polled.Actions[0].ParametersHash != orchestration.HashParameters(polled.Actions[0].Parameters) {
		t.Fatalf("public core action hash was not transport-stable: actions=%#v computed=%s", polled.Actions, func() string {
			if len(polled.Actions) == 0 {
				return ""
			}
			return orchestration.HashParameters(polled.Actions[0].Parameters)
		}())
	}

	launchStarted := time.Now()
	launchOutput := runBridgeCommand(t, bridgeBinary, coreBinary, stateDir, bridgeStateDir, policyPath, rolesPath, coordinationDir, profilePath, launch.Attempt.ID, "controller-restart-safe", "launch")
	if !strings.Contains(launchOutput, `"action_kind": "launch"`) {
		t.Fatalf("launch output = %s", launchOutput)
	}
	if options.expectReadyBeforeProviderSession {
		readyDeadline := options.readyDeadline
		if readyDeadline <= 0 {
			readyDeadline = time.Second
		}
		if elapsed := time.Since(launchStarted); elapsed >= readyDeadline {
			t.Fatalf("bridge launch waited for the slow Provider session instead of owner readiness: %s", elapsed)
		}
		ownerStore, err := NewOwnerStore(bridgeStateDir)
		if err != nil {
			t.Fatal(err)
		}
		owner, found, err := ownerStore.Load(launch.Attempt.ID)
		if err != nil || !found || owner.State != "ready" || owner.ProviderSessionID != "" {
			t.Fatalf("slow-start owner readiness found=%t err=%v receipt=%#v", found, err, owner)
		}
		repeatedStarted := time.Now()
		repeatedOutput := runBridgeCommand(t, bridgeBinary, coreBinary, stateDir, bridgeStateDir, policyPath, rolesPath, coordinationDir, profilePath, launch.Attempt.ID, "controller-restart-safe", "launch")
		if elapsed := time.Since(repeatedStarted); elapsed >= readyDeadline {
			t.Fatalf("repeated launch did not join the ready owner: %s", elapsed)
		}
		var repeated ExecutionOutput
		if err := json.Unmarshal([]byte(repeatedOutput), &repeated); err != nil {
			t.Fatalf("decode repeated launch output: %v\n%s", err, repeatedOutput)
		}
		if repeated.Owner.State != "ready" || repeated.Owner.OwnerPID != owner.OwnerPID || repeated.Result != nil {
			t.Fatalf("repeated launch owner = %#v result=%#v", repeated.Owner, repeated.Result)
		}
	}
	completionDeadline := options.completionDeadline
	if completionDeadline <= 0 {
		completionDeadline = 10 * time.Second
	}
	deadline := time.Now().Add(completionDeadline)
	lastObserveOutput := ""
	for {
		freshLocks, _, err := manager.AcquireRunLocksForConvergence(run.Run.ID)
		if err != nil {
			t.Fatalf("acquire collaboration convergence locks: %v", err)
		}
		freshStore, err := storage.New(stateDir)
		if err != nil {
			_ = freshLocks.Release()
			t.Fatal(err)
		}
		freshCore, err := gateway.NewWithCoordinationLocked(freshStore, config, manager)
		if err != nil {
			_ = freshLocks.Release()
			t.Fatal(err)
		}
		attempt, err := freshCore.GetAttempt(gateway.GetAttemptInput{AttemptID: launch.Attempt.ID})
		_ = freshLocks.Release()
		if err != nil {
			t.Fatal(err)
		}
		if attempt.Attempt.Status == orchestration.AttemptCompleted {
			break
		}
		if attempt.Attempt.Status != orchestration.AttemptRunning && attempt.Attempt.Status != orchestration.AttemptLaunching {
			t.Fatalf("bridge launch attempt = %#v", attempt.Attempt)
		}
		if options.disableWhileRunning && attempt.Attempt.Status == orchestration.AttemptRunning {
			disableCommand := exec.Command(coreBinary, "--disable", "--coordination-dir", coordinationDir)
			disableOutput, disableErr := disableCommand.CombinedOutput()
			if disableErr != nil {
				t.Fatalf("disable active external owner: %v\n%s", disableErr, disableOutput)
			}
			var disabled coordination.DisableResult
			if err := json.Unmarshal(disableOutput, &disabled); err != nil {
				t.Fatalf("decode disable output: %v\n%s", err, disableOutput)
			}
			if disabled.FrameworkEnabled || len(disabled.Runs) != 1 || disabled.Runs[0].RunID != run.Run.ID || disabled.Runs[0].Status != "cancelled" {
				t.Fatalf("disable result = %#v", disabled)
			}
			cancelLocks, _, err := manager.AcquireRunLocksForConvergence(run.Run.ID)
			if err != nil {
				t.Fatal(err)
			}
			cancelledCore, err := gateway.NewWithCoordinationLocked(store, config, manager)
			if err != nil {
				_ = cancelLocks.Release()
				t.Fatal(err)
			}
			cancelledAttempt, err := cancelledCore.GetAttempt(gateway.GetAttemptInput{AttemptID: launch.Attempt.ID})
			_ = cancelLocks.Release()
			if err != nil || cancelledAttempt.Attempt.Status != orchestration.AttemptCancelled {
				t.Fatalf("cancelled Attempt err=%v attempt=%#v", err, cancelledAttempt.Attempt)
			}
			ownerStore, err := NewOwnerStore(bridgeStateDir)
			if err != nil {
				t.Fatal(err)
			}
			owner, found, err := ownerStore.Load(launch.Attempt.ID)
			if err != nil || !found || owner.State != "terminal" {
				t.Fatalf("cancelled owner found=%t err=%v receipt=%#v", found, err, owner)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("bridge owner did not publish a terminal fixture result")
		}
		if attempt.Attempt.Status == orchestration.AttemptRunning {
			lastObserveOutput = runBridgeCommand(t, bridgeBinary, coreBinary, stateDir, bridgeStateDir, policyPath, rolesPath, coordinationDir, profilePath, launch.Attempt.ID, "controller-restart-safe", "observe")
			if !strings.Contains(lastObserveOutput, `"action_kind": "observe"`) {
				t.Fatalf("observe output = %s", lastObserveOutput)
			}
		}
		time.Sleep(25 * time.Millisecond)
	}
	if options.expectedFinalText != "" {
		var observed ExecutionOutput
		if err := json.Unmarshal([]byte(lastObserveOutput), &observed); err != nil {
			t.Fatalf("decode terminal observe output: %v\n%s", err, lastObserveOutput)
		}
		if observed.Result == nil || strings.TrimSpace(observed.Result.FinalText) != options.expectedFinalText {
			t.Fatalf("terminal Provider delivery = %#v", observed.Result)
		}
	}
	completionLocks, _, err := manager.AcquireRunLocksForConvergence(run.Run.ID)
	if err != nil {
		t.Fatalf("acquire completion locks: %v", err)
	}
	freshCore, err := gateway.NewWithCoordinationLocked(store, config, manager)
	if err != nil {
		_ = completionLocks.Release()
		t.Fatal(err)
	}
	completion, err := freshCore.RequestCompletion(gateway.CompletionInput{AttemptID: launch.Attempt.ID, CollaborationRunID: run.Run.ID, InstanceRunID: prepared.Instance.ID, GateEpoch: status.GateEpoch})
	if releaseErr := completionLocks.Release(); releaseErr != nil {
		t.Fatal(releaseErr)
	}
	if err != nil {
		t.Fatal(err)
	}
	if completion.Decision.Status != orchestration.AttemptCompleted {
		t.Fatalf("completion = %#v", completion.Decision)
	}
	releaseLocks, _, err := manager.AcquireRunLocksForRun(run.Run.ID, status.GateEpoch)
	if err != nil {
		t.Fatalf("acquire release locks: %v", err)
	}
	freshCore, err = gateway.NewWithCoordinationLocked(store, config, manager)
	if err != nil {
		_ = releaseLocks.Release()
		t.Fatal(err)
	}
	release, err := freshCore.RecordCollaborationTechnicalRelease(gateway.TechnicalReleaseInput{RunID: run.Run.ID, InstanceID: prepared.Instance.ID, Reason: "validated fixture handoff"})
	if releaseErr := releaseLocks.Release(); releaseErr != nil {
		t.Fatal(releaseErr)
	}
	if err != nil {
		t.Fatal(err)
	}
	if release.Instance.Status != orchestration.InstanceTechnicallyDone {
		t.Fatalf("release = %#v", release)
	}
	receipt, err := NewOwnerStore(bridgeStateDir)
	if err != nil {
		t.Fatal(err)
	}
	owner, found, err := receipt.Load(launch.Attempt.ID)
	if err != nil || !found || owner.State != "terminal" {
		t.Fatalf("owner cleanup receipt found=%t err=%v receipt=%#v", found, err, owner)
	}
}

func fixtureProfile(t *testing.T, adapter, executable string) Profile {
	t.Helper()
	profile := Profile{AdapterProfile: orchestration.AdapterProfile{ID: adapter + "-fixture", Adapter: adapter, Executable: executable, Trust: "authorized"}, TrustAuthorized: true}
	switch adapter {
	case ProviderCursor:
		profile.Mode = "plan"
	case ProviderClaude:
	case ProviderCodexCLI:
		profile.Sandbox = "read-only"
		profile.Approval = "untrusted"
	}
	profile.ProfileHash = orchestration.HashAdapterProfile(profile.AdapterProfile)
	return profile
}

func buildFixtureProvider(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(t.TempDir(), "bridge-fixture-provider")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	command := exec.Command("go", "build", "-o", binary, "./internal/bridge/testdata/provider")
	command.Dir = root
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("build fixture provider: %v\n%s", err, output)
	}
	return binary
}

func buildBridgeExecutable(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(t.TempDir(), "agent-workflow-bridge")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	command := exec.Command("go", "build", "-o", binary, "./cmd/agent-workflow-bridge")
	command.Dir = root
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("build bridge: %v\n%s", err, output)
	}
	return binary
}

func buildCoreExecutable(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(t.TempDir(), "agent-workflow-core")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	command := exec.Command("go", "build", "-o", binary, "./cmd/agent-workflow-core")
	command.Dir = root
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("build core: %v\n%s", err, output)
	}
	return binary
}

func buildCollaborationExecutable(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(t.TempDir(), "agent-workflow-collaboration")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	command := exec.Command("go", "build", "-o", binary, "./cmd/agent-workflow-collaboration")
	command.Dir = root
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("build collaboration: %v\n%s", err, output)
	}
	return binary
}

func runBridgeCommand(t *testing.T, binary, coreBinary, stateDir, bridgeStateDir, policyPath, rolesPath, coordinationDir, profilePath, attemptID, controllerID, kind string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, binary, "--execute", "--core", coreBinary, "--state-dir", stateDir, "--bridge-state-dir", bridgeStateDir, "--policy-file", policyPath, "--roles-manifest", rolesPath, "--coordination-dir", coordinationDir, "--profile-file", profilePath, "--attempt-id", attemptID, "--controller-instance-id", controllerID, "--action-kind", kind)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("bridge %s: %v\n%s", kind, err, output)
	}
	return string(output)
}

func writeJSONFile(t *testing.T, path string, value any) {
	t.Helper()
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
}
