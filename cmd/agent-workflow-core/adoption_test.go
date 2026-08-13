package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"agent-workflow-toolkit/internal/buildinfo"
	"agent-workflow-toolkit/internal/gateway"
	"agent-workflow-toolkit/internal/orchestration"
	"agent-workflow-toolkit/internal/policy"
	protocol "agent-workflow-toolkit/internal/protocol/v1"
	"agent-workflow-toolkit/internal/storage"
)

func TestPublishedVersionAndEnvelopeValidationAreExplicit(t *testing.T) {
	binary := buildAdoptionBinary(t)
	versionOutput, err := exec.Command(binary, "--version").CombinedOutput()
	if err != nil {
		t.Fatalf("--version: %v\n%s", err, versionOutput)
	}
	versionText := string(versionOutput)
	for _, expected := range []string{"version=0.2.0", "protocol=1", "maturity=experimental", "revision=", "dirty="} {
		if !strings.Contains(versionText, expected) {
			t.Fatalf("version output %q is missing %q", versionText, expected)
		}
	}
	if strings.Contains(versionText, "wave1") {
		t.Fatalf("version output retained stale milestone marker: %q", versionText)
	}

	request, err := protocol.NewRequest("validate-envelope", "prepareRetryAttempt", map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	input, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command(binary, "--validate-request")
	command.Stdin = bytes.NewReader(input)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("--validate-request: %v\n%s", err, output)
	}
	response, err := protocol.DecodeResponse(bytes.NewReader(output))
	if err != nil {
		t.Fatal(err)
	}
	var result struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(response.Payload, &result); err != nil {
		t.Fatal(err)
	}
	if result.Status != "envelope_valid" {
		t.Fatalf("validation status = %q", result.Status)
	}
}

func TestProjectAdoptionGuideHappyPathAndDeliveryFailureCheckpoint(t *testing.T) {
	binary := buildAdoptionBinary(t)
	root := t.TempDir()
	workspace := filepath.Join(root, "consumer")
	stateDir := filepath.Join(root, "state")
	if err := os.MkdirAll(filepath.Join(workspace, "reports"), 0o700); err != nil {
		t.Fatal(err)
	}
	binding := orchestration.Binding{Role: "reviewer", Adapter: "codex-native", Model: "fixture-model", Reasoning: "high", Permission: "read-only", Workspace: workspace, MCPAllowlist: []string{}}
	config := policy.DefaultConfig()
	config.RegisteredBindings = []orchestration.Binding{binding}
	policyRaw, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	policyPath := filepath.Join(root, "policy.local.json")
	if err := os.WriteFile(policyPath, policyRaw, 0o600); err != nil {
		t.Fatal(err)
	}

	requestIndex := 0
	call := func(operation string, payload any, target any) protocol.Response {
		t.Helper()
		requestIndex++
		request, requestErr := protocol.NewRequest(fmt.Sprintf("adoption-%02d-%s", requestIndex, operation), operation, payload)
		if requestErr != nil {
			t.Fatal(requestErr)
		}
		response, stderr, runErr := runAdoptionCLI(binary, stateDir, policyPath, request, 10*time.Second)
		if runErr != nil {
			t.Fatalf("%s process error: %v\nstderr=%s", operation, runErr, stderr)
		}
		if stderr != "" {
			t.Fatalf("%s wrote stderr: %s", operation, stderr)
		}
		if !response.OK {
			t.Fatalf("%s response error: %#v", operation, response.Error)
		}
		if target != nil {
			if err := json.Unmarshal(response.Payload, target); err != nil {
				t.Fatalf("decode %s payload: %v", operation, err)
			}
		}
		return response
	}

	var created gateway.CreateTaskResult
	call("createTask", gateway.CreateTaskInput{Title: "sample review", Objective: "只读评审并生成报告", Workspace: workspace}, &created)
	artifactPath := filepath.Join(workspace, "reports", "agent-review.md")
	var proposed gateway.ProposeNodeResult
	call("proposeNode", gateway.ProposeNodeInput{TaskID: created.Task.ID, RevisionID: created.Revision.ID, Kind: "architecture-review", Role: "reviewer", ExpectedOutput: orchestration.ExpectedOutput{Format: "markdown", ArtifactPath: artifactPath, Required: true, MinBytes: 30, RequiredSections: []string{"# Review", "## Findings", "## Verdict"}}}, &proposed)
	var preflight gateway.RunPreflightResult
	call("runPreflight", gateway.RunPreflightInput{NodeID: proposed.Node.ID, Binding: binding}, &preflight)
	if preflight.Preflight.Status != "passed" {
		t.Fatalf("preflight = %#v", preflight.Preflight)
	}
	var launch gateway.LaunchDecision
	call("prepareAttempt", gateway.PrepareAttemptInput{NodeID: proposed.Node.ID, ControllerInstanceID: "sample-controller", Binding: &binding}, &launch)
	var batch gateway.HostActionBatch
	call("pollHostActions", gateway.PollHostActionsInput{AttemptID: launch.Attempt.ID, ControllerInstanceID: "sample-controller"}, &batch)
	if len(batch.Actions) != 1 || batch.Actions[0].Kind != orchestration.ActionLaunch || batch.Actions[0].ID != launch.LaunchAction.ID {
		t.Fatalf("launch action batch = %#v", batch)
	}
	if err := os.WriteFile(artifactPath, []byte("# Review\n\n## Findings\n\nnone\n\n## Verdict\n\nPASS\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	started := true
	call("submitHostActionResult", gateway.SubmitHostActionResultInput{AttemptID: launch.Attempt.ID, ActionID: launch.LaunchAction.ID, Lease: launch.LaunchAction.Lease, Result: orchestration.HostActionResult{Status: "completed", Terminal: true, ProviderHandle: "native-task-1", ProviderSessionID: "native-session-1", ExecutionStarted: &started, SideEffectState: "none", Identity: map[string]string{"provider": binding.Adapter, "session": "native-session-1", "workspace": workspace}, TaskCanary: launch.Attempt.TaskCanary}}, &gateway.ActionReceipt{})
	var completion gateway.CompletionResult
	call("requestCompletion", gateway.CompletionInput{AttemptID: launch.Attempt.ID}, &completion)
	if completion.Decision.Status != orchestration.AttemptCompleted || completion.Node.Status != orchestration.NodeAwaitingHumanDecision {
		t.Fatalf("completion = %#v", completion)
	}

	requestIndex++
	invalidApproval, err := protocol.NewRequest(fmt.Sprintf("adoption-%02d-invalid-approve", requestIndex), "recordHumanDecision", gateway.HumanDecisionInput{NodeID: proposed.Node.ID, AttemptID: launch.Attempt.ID, Decision: "approve", AuthorityRef: "user-input:invalid-attempt-bound"})
	if err != nil {
		t.Fatal(err)
	}
	invalidResponse, stderr, runErr := runAdoptionCLI(binary, stateDir, policyPath, invalidApproval, 10*time.Second)
	if runErr != nil || stderr != "" {
		t.Fatalf("invalid approval process=%v stderr=%s", runErr, stderr)
	}
	if invalidResponse.OK || invalidResponse.Error == nil || invalidResponse.Error.Code != "invalid_fallback_approval" {
		t.Fatalf("successful attempt-bound approval = %#v", invalidResponse)
	}
	var approved gateway.HumanDecisionResult
	call("recordHumanDecision", gateway.HumanDecisionInput{NodeID: proposed.Node.ID, Decision: "approve", AuthorityRef: "user-input:approve-sample-review"}, &approved)
	if approved.Node.Status != orchestration.NodeApproved {
		t.Fatalf("approved node = %#v", approved.Node)
	}

	missingArtifact := filepath.Join(workspace, "reports", "missing-review.md")
	var missingNode gateway.ProposeNodeResult
	call("proposeNode", gateway.ProposeNodeInput{TaskID: created.Task.ID, Kind: "delivery-check", Role: "reviewer", ExpectedOutput: orchestration.ExpectedOutput{Format: "markdown", ArtifactPath: missingArtifact, Required: true, MinBytes: 10}}, &missingNode)
	call("runPreflight", gateway.RunPreflightInput{NodeID: missingNode.Node.ID, Binding: binding}, &gateway.RunPreflightResult{})
	var missingLaunch gateway.LaunchDecision
	call("prepareAttempt", gateway.PrepareAttemptInput{NodeID: missingNode.Node.ID, ControllerInstanceID: "sample-controller", Binding: &binding}, &missingLaunch)
	call("submitHostActionResult", gateway.SubmitHostActionResultInput{AttemptID: missingLaunch.Attempt.ID, ActionID: missingLaunch.LaunchAction.ID, Lease: missingLaunch.LaunchAction.Lease, Result: orchestration.HostActionResult{Status: "completed", Terminal: true, ProviderHandle: "native-task-2", ProviderSessionID: "native-session-2", ExecutionStarted: &started, SideEffectState: "none", Identity: map[string]string{"provider": binding.Adapter, "session": "native-session-2", "workspace": workspace}, TaskCanary: missingLaunch.Attempt.TaskCanary}}, &gateway.ActionReceipt{})
	var missingCompletion gateway.CompletionResult
	call("requestCompletion", gateway.CompletionInput{AttemptID: missingLaunch.Attempt.ID}, &missingCompletion)
	if missingCompletion.Decision.Reason != "delivery_missing" || missingCompletion.Node.Status != orchestration.NodeAwaitingHumanDecision {
		t.Fatalf("delivery missing completion = %#v", missingCompletion)
	}
	var afterMissing gateway.HostActionBatch
	call("pollHostActions", gateway.PollHostActionsInput{AttemptID: missingLaunch.Attempt.ID, AfterActionSequence: missingLaunch.LaunchAction.Sequence, ControllerInstanceID: "sample-controller"}, &afterMissing)
	if len(afterMissing.Actions) != 0 {
		t.Fatalf("delivery_missing unexpectedly issued follow-up actions: %#v", afterMissing.Actions)
	}
	requestIndex++
	invalidApprovalAfterMissing, err := protocol.NewRequest(fmt.Sprintf("adoption-%02d-invalid-delivery-approve", requestIndex), "recordHumanDecision", gateway.HumanDecisionInput{NodeID: missingNode.Node.ID, Decision: "approve", AuthorityRef: "user-input:approve-missing-delivery"})
	if err != nil {
		t.Fatal(err)
	}
	missingApprovalResponse, stderr, runErr := runAdoptionCLI(binary, stateDir, policyPath, invalidApprovalAfterMissing, 10*time.Second)
	if runErr != nil || stderr != "" {
		t.Fatalf("delivery approval process=%v stderr=%s", runErr, stderr)
	}
	if missingApprovalResponse.OK || missingApprovalResponse.Error == nil || missingApprovalResponse.Error.Code != "delivery_revision_required" {
		t.Fatalf("delivery_missing ordinary approve response = %#v", missingApprovalResponse)
	}
	requestIndex++
	invalidRetry, err := protocol.NewRequest(fmt.Sprintf("adoption-%02d-invalid-delivery-retry", requestIndex), "recordHumanDecision", gateway.HumanDecisionInput{NodeID: missingNode.Node.ID, Decision: "retry_node", AuthorityRef: "user-input:retry-missing-delivery"})
	if err != nil {
		t.Fatal(err)
	}
	retryResponse, stderr, runErr := runAdoptionCLI(binary, stateDir, policyPath, invalidRetry, 10*time.Second)
	if runErr != nil || stderr != "" {
		t.Fatalf("delivery retry process=%v stderr=%s", runErr, stderr)
	}
	if retryResponse.OK || retryResponse.Error == nil || retryResponse.Error.Code != "retry_not_allowed" {
		t.Fatalf("delivery_missing retry_node response = %#v", retryResponse)
	}
	var revised gateway.HumanDecisionResult
	call("recordHumanDecision", gateway.HumanDecisionInput{NodeID: missingNode.Node.ID, Decision: "request_revision", AuthorityRef: "user-input:revise-missing-delivery", Instruction: "重新执行并生成缺失的 artifact"}, &revised)
	if revised.NewNode == nil || revised.NewNode.Role != missingNode.Node.Role {
		t.Fatalf("delivery revision = %#v", revised)
	}
}

func TestGatewayCLIConcurrentSameRequestIsSerialized(t *testing.T) {
	binary := buildAdoptionBinary(t)
	root := t.TempDir()
	stateDir := filepath.Join(root, "state")
	request, err := protocol.NewRequest("concurrent-same-request", "createTask", gateway.CreateTaskInput{Objective: "concurrent idempotency", Workspace: root})
	if err != nil {
		t.Fatal(err)
	}

	const workers = 8
	start := make(chan struct{})
	type outcome struct {
		response protocol.Response
		stderr   string
		err      error
	}
	outcomes := make(chan outcome, workers)
	var group sync.WaitGroup
	for range workers {
		group.Add(1)
		go func() {
			defer group.Done()
			<-start
			response, stderr, runErr := runAdoptionCLI(binary, stateDir, "", request, 10*time.Second)
			outcomes <- outcome{response: response, stderr: stderr, err: runErr}
		}()
	}
	close(start)
	group.Wait()
	close(outcomes)
	ids := map[string]struct{}{}
	for outcome := range outcomes {
		if outcome.err != nil || outcome.stderr != "" || !outcome.response.OK {
			t.Fatalf("concurrent response process=%v stderr=%q response=%#v", outcome.err, outcome.stderr, outcome.response)
		}
		var created gateway.CreateTaskResult
		if err := json.Unmarshal(outcome.response.Payload, &created); err != nil {
			t.Fatal(err)
		}
		ids[created.Task.ID] = struct{}{}
	}
	if len(ids) != 1 {
		t.Fatalf("concurrent request created task IDs %v", ids)
	}
	tasks, err := os.ReadDir(filepath.Join(stateDir, "tasks"))
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 1 {
		t.Fatalf("concurrent request created %d task directories", len(tasks))
	}
}

func TestGatewayCLIReturnsStructuredBusyError(t *testing.T) {
	binary := buildAdoptionBinary(t)
	root := t.TempDir()
	stateDir := filepath.Join(root, "state")
	lock, err := storage.AcquireRootLock(stateDir, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Release()
	request, err := protocol.NewRequest("busy-state", "createTask", gateway.CreateTaskInput{Objective: "busy", Workspace: root})
	if err != nil {
		t.Fatal(err)
	}
	response, stderr, runErr := runAdoptionCLI(binary, stateDir, "", request, 50*time.Millisecond)
	if runErr != nil || stderr != "" {
		t.Fatalf("busy process=%v stderr=%s", runErr, stderr)
	}
	if response.OK || response.Error == nil || response.Error.Code != "state_dir_busy" {
		t.Fatalf("busy response = %#v", response)
	}
}

func TestControllerSkillInstallerCreatesVerifiableReceipt(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("the current Controller Skill installer is the supported Windows adoption path")
	}
	powershell, err := exec.LookPath("powershell.exe")
	if err != nil {
		t.Skip("Windows PowerShell is unavailable")
	}
	binaries := buildAdoptionBinaries(t)
	consumer := t.TempDir()
	repositoryRoot := filepath.Clean(filepath.Join(mustWorkingDirectory(t), "..", ".."))
	installer := filepath.Join(repositoryRoot, "tools", "orchestration", "install_controller_skill.ps1")
	baseArgs := []string{"-NoProfile", "-ExecutionPolicy", "Bypass", "-File", installer, "-ToolkitRoot", repositoryRoot, "-ConsumerRoot", consumer, "-CorePath", binaries.Core, "-CollaborationPath", binaries.Collaboration, "-BridgePath", binaries.Bridge, "-Mode", "Copy", "-AllowDirty"}
	install := exec.Command(powershell, baseArgs...)
	if output, err := install.CombinedOutput(); err != nil {
		t.Fatalf("install Controller Skill: %v\n%s", err, output)
	} else {
		var result struct {
			Status string `json:"status"`
		}
		if err := json.Unmarshal(output, &result); err != nil || result.Status != "installed" {
			t.Fatalf("install output = %s; decode error=%v", output, err)
		}
	}
	receiptPath := filepath.Join(consumer, ".agents", "agent-workflow-toolkit.lock.json")
	receiptRaw, err := os.ReadFile(receiptPath)
	if err != nil {
		t.Fatal(err)
	}
	var receipt struct {
		SchemaVersion int    `json:"schema_version"`
		ToolkitCommit string `json:"toolkit_commit"`
		Binaries      map[string]struct {
			Path          string `json:"path"`
			Revision      string `json:"revision"`
			VersionOutput string `json:"version_output"`
			Protocol      int    `json:"protocol"`
			Maturity      string `json:"maturity"`
		} `json:"binaries"`
	}
	if err := json.Unmarshal(receiptRaw, &receipt); err != nil {
		t.Fatal(err)
	}
	if receipt.SchemaVersion != 2 || receipt.ToolkitCommit == "" || len(receipt.Binaries) != 3 {
		t.Fatalf("installation receipt metadata = %#v", receipt)
	}
	for name, path := range map[string]string{"core": binaries.Core, "collaboration": binaries.Collaboration, "bridge": binaries.Bridge} {
		record := receipt.Binaries[name]
		if record.Path != path || record.Revision != receipt.ToolkitCommit || record.Protocol != protocol.Major || record.Maturity != buildinfo.Maturity || !strings.Contains(record.VersionOutput, "version=") {
			t.Fatalf("installation receipt %s metadata = %#v", name, record)
		}
	}
	checkArgs := append(append([]string(nil), baseArgs...), "-Check")
	check := exec.Command(powershell, checkArgs...)
	if output, err := check.CombinedOutput(); err != nil {
		t.Fatalf("check Controller Skill: %v\n%s", err, output)
	} else {
		var result struct {
			Status string `json:"status"`
		}
		if err := json.Unmarshal(output, &result); err != nil || result.Status != "valid" {
			t.Fatalf("check output = %s; decode error=%v", output, err)
		}
	}
	for _, expected := range []string{
		filepath.Join(consumer, ".agents", "skills", "multi-agent-orchestration", "SKILL.md"),
		receiptPath,
	} {
		if _, err := os.Stat(expected); err != nil {
			t.Fatalf("installed artifact %s: %v", expected, err)
		}
	}
}

func TestRoleFirstInstallerCreatesCanonicalControllerReceipt(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("the role-first installer is a Windows adoption path")
	}
	powershell, err := exec.LookPath("powershell.exe")
	if err != nil {
		t.Skip("Windows PowerShell is unavailable")
	}
	binaries := buildAdoptionBinaries(t)
	consumer := t.TempDir()
	repositoryRoot := filepath.Clean(filepath.Join(mustWorkingDirectory(t), "..", ".."))
	installer := filepath.Join(repositoryRoot, "tools", "orchestration", "install_role_first_skill.ps1")
	fixture := filepath.Join(repositoryRoot, "tests", "orchestration", "fixtures", "role-first")
	baseArgs := []string{"-NoProfile", "-ExecutionPolicy", "Bypass", "-File", installer, "-ToolkitRoot", repositoryRoot, "-ConsumerRoot", consumer, "-SkillSource", filepath.Join(fixture, "skill"), "-RolePackage", filepath.Join(fixture, "roles"), "-CorePath", binaries.Core, "-CollaborationPath", binaries.Collaboration, "-BridgePath", binaries.Bridge, "-AllowDirty"}
	if output, err := exec.Command(powershell, baseArgs...).CombinedOutput(); err != nil {
		t.Fatalf("install role-first Skill: %v\n%s", err, output)
	}
	for _, expected := range []string{
		filepath.Join(consumer, ".agents", "agent-workflow-toolkit.lock.json"),
		filepath.Join(consumer, ".agents", "agent-workflow-role-first-skill.lock.json"),
		filepath.Join(consumer, ".agents", "agent-workflow-role-first-entry.json"),
		filepath.Join(consumer, ".agents", "skills", "multi-agent-orchestration", "SKILL.md"),
	} {
		if _, err := os.Stat(expected); err != nil {
			t.Fatalf("installed artifact %s: %v", expected, err)
		}
	}
	checkArgs := append(append([]string(nil), baseArgs...), "-Check")
	if output, err := exec.Command(powershell, checkArgs...).CombinedOutput(); err != nil {
		t.Fatalf("check role-first Skill: %v\n%s", err, output)
	}

	controllerReceipt := filepath.Join(consumer, ".agents", "agent-workflow-toolkit.lock.json")
	assertInstallerCheckFails := func(name string, mutate func()) {
		t.Helper()
		mutate()
		if output, err := exec.Command(powershell, checkArgs...).CombinedOutput(); err == nil {
			t.Fatalf("%s unexpectedly passed -Check: %s", name, output)
		}
	}
	receiptRaw, err := os.ReadFile(controllerReceipt)
	if err != nil {
		t.Fatal(err)
	}
	assertInstallerCheckFails("tampered receipt path", func() {
		var receipt map[string]any
		if err := json.Unmarshal(receiptRaw, &receipt); err != nil {
			t.Fatal(err)
		}
		receipt["binaries"].(map[string]any)["bridge"].(map[string]any)["path"] = filepath.Join(t.TempDir(), "missing-bridge.exe")
		updated, _ := json.MarshalIndent(receipt, "", "  ")
		if err := os.WriteFile(controllerReceipt, append(updated, '\n'), 0o600); err != nil {
			t.Fatal(err)
		}
	})
	if err := os.WriteFile(controllerReceipt, receiptRaw, 0o600); err != nil {
		t.Fatal(err)
	}
	bridgeBackup, err := os.ReadFile(binaries.Bridge)
	if err != nil {
		t.Fatal(err)
	}
	assertInstallerCheckFails("tampered binary", func() {
		if err := os.WriteFile(binaries.Bridge, append(append([]byte(nil), bridgeBackup...), 0), 0o700); err != nil {
			t.Fatal(err)
		}
	})
	if err := os.WriteFile(binaries.Bridge, bridgeBackup, 0o700); err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command(powershell, checkArgs...).CombinedOutput(); err != nil {
		t.Fatalf("restored role-first installation failed -Check: %v\n%s", err, output)
	}
	wrongVersionArgs := append([]string(nil), checkArgs...)
	for index := range wrongVersionArgs {
		if wrongVersionArgs[index] == binaries.Bridge {
			wrongVersionArgs[index] = binaries.Collaboration
			break
		}
	}
	if output, err := exec.Command(powershell, wrongVersionArgs...).CombinedOutput(); err == nil {
		t.Fatalf("wrong bridge command/version unexpectedly passed -Check: %s", output)
	}
}

func TestFreshConsumerBusinessSkillOnlyEntrySmoke(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("the managed business-Skill entry is currently a Windows host path")
	}
	powershell, err := exec.LookPath("powershell.exe")
	if err != nil {
		t.Skip("Windows PowerShell is unavailable")
	}
	binaries := buildAdoptionBinaries(t)
	repositoryRoot := filepath.Clean(filepath.Join(mustWorkingDirectory(t), "..", ".."))
	smoke := filepath.Join(repositoryRoot, "tools", "orchestration", "smoke_business_skill_entry.ps1")
	arguments := []string{
		"-NoProfile", "-ExecutionPolicy", "Bypass", "-File", smoke,
		"-ToolkitRoot", repositoryRoot,
		"-CorePath", binaries.Core,
		"-CollaborationPath", binaries.Collaboration,
		"-BridgePath", binaries.Bridge,
		"-AllowDirty",
	}
	output, err := exec.Command(powershell, arguments...).CombinedOutput()
	if err != nil {
		t.Fatalf("business-Skill-only smoke: %v\n%s", err, output)
	}
	var result struct {
		Status                  string `json:"status"`
		ZeroInstall             string `json:"zero_install"`
		Direct                  string `json:"direct"`
		Single                  string `json:"single"`
		Framework               string `json:"framework"`
		Disable                 string `json:"disable"`
		RequestedInstances      int    `json:"requested_instances"`
		ActualInstances         int    `json:"actual_instances"`
		PinnedCollaborationPath string `json:"pinned_collaboration_path"`
		PinnedBridgePath        string `json:"pinned_bridge_path"`
		MissingPathFailClosed   bool   `json:"missing_path_fail_closed"`
	}
	if err := json.Unmarshal(output, &result); err != nil {
		t.Fatalf("decode business-Skill-only smoke: %v\n%s", err, output)
	}
	if result.Status != "passed" || result.ZeroInstall != "execute_business_skill" || result.Direct != "ordinary_delegation" || result.Single != "execute_sequentially" || result.Framework != "framework_controller" || result.Disable != "direct" || result.RequestedInstances != 2 || result.ActualInstances != 2 || !result.MissingPathFailClosed {
		t.Fatalf("business-Skill-only result = %#v", result)
	}
	if result.PinnedCollaborationPath != binaries.Collaboration || result.PinnedBridgePath != binaries.Bridge {
		t.Fatalf("business-Skill-only entry did not use receipt paths: %#v", result)
	}
}

func TestFreshConsumerBusinessSkillOnlyEntrySupportsHyphenatedSkillName(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("the managed business-Skill entry is currently a Windows host path")
	}
	powershell, err := exec.LookPath("powershell.exe")
	if err != nil {
		t.Skip("Windows PowerShell is unavailable")
	}
	binaries := buildAdoptionBinaries(t)
	repositoryRoot := filepath.Clean(filepath.Join(mustWorkingDirectory(t), "..", ".."))
	smoke := filepath.Join(repositoryRoot, "tools", "orchestration", "smoke_business_skill_entry.ps1")
	arguments := []string{
		"-NoProfile", "-ExecutionPolicy", "Bypass", "-File", smoke,
		"-ToolkitRoot", repositoryRoot,
		"-CorePath", binaries.Core,
		"-CollaborationPath", binaries.Collaboration,
		"-BridgePath", binaries.Bridge,
		"-BusinessSkill", "sample-role-first-review",
		"-AllowDirty",
	}
	if output, err := exec.Command(powershell, arguments...).CombinedOutput(); err != nil {
		t.Fatalf("hyphenated business-Skill entry smoke: %v\n%s", err, output)
	}
}

type adoptionBinaries struct {
	Core          string
	Collaboration string
	Bridge        string
}

func buildAdoptionBinaries(t *testing.T) adoptionBinaries {
	t.Helper()
	root := t.TempDir()
	repositoryRoot := filepath.Clean(filepath.Join(mustWorkingDirectory(t), "..", ".."))
	build := func(name, packagePath string) string {
		binary := filepath.Join(root, name)
		if runtime.GOOS == "windows" {
			binary += ".exe"
		}
		command := exec.Command("go", "build", "-o", binary, packagePath)
		command.Dir = repositoryRoot
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("build %s adoption CLI: %v\n%s", name, err, output)
		}
		return binary
	}
	return adoptionBinaries{
		Core:          build("agent-workflow-core", "./cmd/agent-workflow-core"),
		Collaboration: build("agent-workflow-collaboration", "./cmd/agent-workflow-collaboration"),
		Bridge:        build("agent-workflow-bridge", "./cmd/agent-workflow-bridge"),
	}
}

func buildAdoptionBinary(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	binary := filepath.Join(root, "agent-workflow-core")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	command := exec.Command("go", "build", "-o", binary, ".")
	command.Dir = mustWorkingDirectory(t)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("build adoption CLI: %v\n%s", err, output)
	}
	return binary
}

func runAdoptionCLI(binary, stateDir, policyPath string, request protocol.Request, lockTimeout time.Duration) (protocol.Response, string, error) {
	input, err := json.Marshal(request)
	if err != nil {
		return protocol.Response{}, "", err
	}
	args := []string{"--gateway", "--state-dir", stateDir, "--lock-timeout", lockTimeout.String()}
	if policyPath != "" {
		args = append(args, "--policy-file", policyPath)
	}
	command := exec.Command(binary, args...)
	command.Stdin = bytes.NewReader(input)
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	runErr := command.Run()
	if runErr != nil {
		return protocol.Response{}, stderr.String(), runErr
	}
	response, decodeErr := protocol.DecodeResponse(bytes.NewReader(stdout.Bytes()))
	if decodeErr != nil {
		return protocol.Response{}, stderr.String(), fmt.Errorf("decode response: %w; stdout=%s", decodeErr, stdout.String())
	}
	return response, stderr.String(), nil
}
