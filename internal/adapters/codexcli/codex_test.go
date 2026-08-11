package codexcli

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"agent-workflow-toolkit/internal/adapters/cli"
)

func TestInvocationBuildsSafeDurableOutputCommand(t *testing.T) {
	i := Invocation{
		Executable:            "codex.exe",
		Workspace:             `E:\projects\agent-workflow-toolkit`,
		Model:                 "gpt-5.6-luna",
		OutputLastMessagePath: `E:\temp\last-message.txt`,
		Ephemeral:             true,
	}
	args, err := i.Args("return the smoke token")
	if err != nil {
		t.Fatalf("Args() error = %v", err)
	}
	want := []string{
		"--ask-for-approval", "untrusted",
		"--sandbox", "read-only",
		"--cd", `E:\projects\agent-workflow-toolkit`,
		"--model", "gpt-5.6-luna",
		"exec", "--ignore-user-config", "--json",
		"--output-last-message", `E:\temp\last-message.txt`,
		"--ephemeral", "return the smoke token",
	}
	if !reflect.DeepEqual(args, want) {
		t.Fatalf("Args() = %#v, want %#v", args, want)
	}
	joined := strings.Join(args, " ")
	if strings.Contains(joined, "--force") || strings.Contains(joined, "--dangerously-bypass") {
		t.Fatalf("unsafe bypass flag found in args: %s", joined)
	}
}

func TestNormalizeRecoversDurableMessageWhenStdoutIsEmpty(t *testing.T) {
	result := (RawResult{ExitCode: 0, LastMessage: []byte("\nADAPTER_SMOKE_OK\n")}).Normalize()
	if result.Status != StatusCompleted || result.Reason != "durable_last_message" || result.FinalText != "ADAPTER_SMOKE_OK" {
		t.Fatalf("unexpected durable result: %#v", result)
	}
}

func TestNormalizeMarksEmptySuccessfulTurnAsDeliveryMissing(t *testing.T) {
	result := (RawResult{ExitCode: 0, Stdout: []byte{}}).Normalize()
	if result.Status != StatusDeliveryMissing || result.Reason != "delivery_missing" {
		t.Fatalf("unexpected empty result: %#v", result)
	}
}

func TestNormalizeDoesNotTreatToolOnlyJSONLAsFinal(t *testing.T) {
	stdout := []byte(`{"type":"item.completed","item":{"type":"tool_call","name":"CreatePlan","input":{"text":"report"}}}` + "\n" + `{"type":"turn.completed","status":"success"}`)
	result := (RawResult{ExitCode: 0, Stdout: stdout}).Normalize()
	if result.Status != StatusDeliveryMissing || result.Reason != "delivery_missing" {
		t.Fatalf("tool-only JSONL was treated as final: %#v", result)
	}
}

func TestNormalizeExtractsStructuredAgentMessage(t *testing.T) {
	stdout := []byte(`{"type":"item.completed","item":{"type":"agent_message","text":"ADAPTER_SMOKE_OK"}}`)
	result := (RawResult{ExitCode: 0, Stdout: stdout}).Normalize()
	if result.Status != StatusCompleted || result.Source != "item.completed" || result.FinalText != "ADAPTER_SMOKE_OK" {
		t.Fatalf("unexpected structured result: %#v", result)
	}
}

func TestRunUsesDurableOutputFileWithLocalHelperProcess(t *testing.T) {
	lastMessagePath := filepath.Join(t.TempDir(), "last-message.txt")
	i := Invocation{
		Executable:            os.Args[0],
		Workspace:             `E:\projects\agent-workflow-toolkit`,
		Model:                 "fixture-model",
		OutputLastMessagePath: lastMessagePath,
		Environment: []string{
			"CODEX_ADAPTER_HELPER=1",
			"CODEX_ADAPTER_LAST_MESSAGE=" + lastMessagePath,
		},
	}
	result, err := i.Run(t.Context(), "return the smoke token")
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.Status != StatusCompleted || result.Reason != "durable_last_message" || result.FinalText != "ADAPTER_SMOKE_OK" {
		t.Fatalf("unexpected helper result: %#v", result)
	}
}

func TestRunReturnsStartErrorForMissingExecutable(t *testing.T) {
	lastMessagePath := filepath.Join(t.TempDir(), "last-message.txt")
	_, err := (Invocation{
		Executable:            filepath.Join(t.TempDir(), "missing-codex.exe"),
		Workspace:             `E:\projects\agent-workflow-toolkit`,
		Model:                 "fixture-model",
		OutputLastMessagePath: lastMessagePath,
	}).Run(t.Context(), "return the smoke token")
	if err == nil {
		t.Fatal("Run() accepted a missing executable as a provider result")
	}
}

func TestInvocationBuildsReasoningAndResumeCommand(t *testing.T) {
	i := Invocation{
		Executable:            "codex.exe",
		Workspace:             `E:\projects\agent-workflow-toolkit`,
		Model:                 "gpt-5.6-luna",
		Reasoning:             "max",
		OutputLastMessagePath: `E:\temp\resume-message.txt`,
		ResumeSessionID:       "thread-1",
	}
	args, err := i.Args("return the follow-up token")
	if err != nil {
		t.Fatalf("Args() error = %v", err)
	}
	want := []string{
		"--ask-for-approval", "untrusted",
		"--sandbox", "read-only",
		"--cd", `E:\projects\agent-workflow-toolkit`,
		"--model", "gpt-5.6-luna",
		"--config", `model_reasoning_effort="max"`,
		"exec", "resume", "--ignore-user-config", "--json",
		"--output-last-message", `E:\temp\resume-message.txt`,
		"thread-1", "return the follow-up token",
	}
	if !reflect.DeepEqual(args, want) {
		t.Fatalf("Args() = %#v, want %#v", args, want)
	}
}

func TestInvocationBuildsExplicitProviderConfigWithoutEmbeddedCredentials(t *testing.T) {
	i := Invocation{
		Executable:            "codex.exe",
		Workspace:             `E:\projects\agent-workflow-toolkit`,
		Model:                 "fixture-model",
		ModelProvider:         "custom",
		ProviderBaseURL:       "https://provider.example/v1",
		ProviderWireAPI:       "responses",
		ProviderAuth:          true,
		OutputLastMessagePath: `E:\temp\provider-message.txt`,
	}
	args, err := i.Args("return the token")
	if err != nil {
		t.Fatalf("Args() error = %v", err)
	}
	joined := strings.Join(args, " ")
	for _, expected := range []string{
		`model_provider="custom"`,
		`model_providers.custom.name="custom"`,
		`model_providers.custom.base_url="https://provider.example/v1"`,
		`model_providers.custom.wire_api="responses"`,
		`model_providers.custom.requires_openai_auth=true`,
		"--ignore-user-config",
	} {
		if !strings.Contains(joined, expected) {
			t.Fatalf("Args() = %q, missing %q", joined, expected)
		}
	}
	if _, err := (Invocation{
		Executable:            "codex.exe",
		Workspace:             `E:\projects\agent-workflow-toolkit`,
		Model:                 "fixture-model",
		ModelProvider:         "custom",
		ProviderBaseURL:       "https://user:secret@provider.example/v1",
		OutputLastMessagePath: `E:\temp\provider-message.txt`,
	}).Args("return the token"); err == nil {
		t.Fatal("Args() accepted provider credentials embedded in a URL")
	}
}

func TestPreflightFailsClosedForWritesAndMCP(t *testing.T) {
	workspace := t.TempDir()
	adapter := New("codex")
	probeCalls := 0
	adapter.Probe = func(context.Context, string, []string) (cli.Result, error) {
		probeCalls++
		return cli.Result{ExitCode: 0, Stdout: []byte("codex 0.147.0")}, nil
	}
	write, err := adapter.Preflight(t.Context(), PreflightRequest{Workspace: workspace, Model: "fixture-model", Permission: "workspace-write", TrustAuthorized: true})
	if err != nil || write.Status != string(CapabilityUnsupported) || write.Code != "write_capability_unproven" || probeCalls != 0 {
		t.Fatalf("write preflight = %#v, calls=%d, err=%v", write, probeCalls, err)
	}
	mcp, err := adapter.Preflight(t.Context(), PreflightRequest{Workspace: workspace, Model: "fixture-model", Permission: "read-only", TrustAuthorized: true, MCPAllowlist: []string{"evidence"}})
	if err != nil || mcp.Status != string(CapabilityUnsupported) || mcp.Code != "mcp_unsupported" || probeCalls != 0 {
		t.Fatalf("MCP preflight = %#v, calls=%d, err=%v", mcp, probeCalls, err)
	}
	configMCP, err := adapter.Preflight(t.Context(), PreflightRequest{Workspace: workspace, Model: "fixture-model", Permission: "read-only", TrustAuthorized: true, MCPIsolation: "config-isolated"})
	if err != nil || configMCP.Status != string(CapabilityUnsupported) || configMCP.Code != "mcp_config_isolation_unsupported" || probeCalls != 0 {
		t.Fatalf("config-isolated MCP preflight = %#v, calls=%d, err=%v", configMCP, probeCalls, err)
	}
	if _, err := adapter.Launch(t.Context(), LaunchRequest{Workspace: workspace, Model: "fixture-model", Permission: "read-only", TrustAuthorized: true, MCPIsolation: "config-isolated", Prompt: "must be rejected", TaskCanary: "mcp-config-canary"}); err == nil {
		t.Fatal("config-isolated MCP request unexpectedly reached Launch")
	}
	passed, err := adapter.Preflight(t.Context(), PreflightRequest{Workspace: workspace, Model: "fixture-model", Permission: "read-only", TrustAuthorized: true})
	if err != nil || passed.Status != "passed" || !passed.ModelRegistered || probeCalls != 1 {
		t.Fatalf("passed preflight = %#v, calls=%d, err=%v", passed, probeCalls, err)
	}
}

func TestLaunchObserveSendAndCleanupLifecycle(t *testing.T) {
	workspace := t.TempDir()
	adapter := New(os.Args[0])
	adapter.Environment = []string{"CODEX_ADAPTER_HELPER=1"}
	adapter.StartupTimeout = 2 * time.Second
	transcript := filepath.Join(workspace, "codex-transcript.jsonl")
	running, err := adapter.Launch(t.Context(), LaunchRequest{
		Workspace: workspace, Model: "fixture-model", Permission: "read-only", TrustAuthorized: true,
		Prompt: "return the smoke token", TaskCanary: "canary-lifecycle", TranscriptPath: transcript,
	})
	if err != nil {
		t.Fatalf("Launch() error = %v", err)
	}
	if running.Status != "running" || running.ProviderSessionID != "codex-thread-1" || running.Identity["provider"] != Provider || running.TaskCanary != "canary-lifecycle" {
		t.Fatalf("launch result = %#v", running)
	}
	completed := waitForCodexTerminal(t, adapter, running.ProviderHandle)
	if completed.Status != "completed" || completed.Reason != "terminal_and_final_delivery" || completed.FinalText != "ADAPTER_SMOKE_OK" || completed.FinalSource != "durable_last_message" || !completed.LastMessageAvailable || !completed.TranscriptWritten {
		t.Fatalf("completed result = %#v", completed)
	}
	if transcriptBytes, err := os.ReadFile(transcript); err != nil || !strings.Contains(string(transcriptBytes), "codex-thread-1") || !strings.Contains(string(transcriptBytes), "durable_last_message") {
		t.Fatalf("transcript = %q, err=%v", transcriptBytes, err)
	}
	followUp, err := adapter.Send(t.Context(), SendRequest{HandleID: running.ProviderHandle, ActionID: "core-send-1", Prompt: "return the delivery token", CoreAuthorized: true, TaskCanary: "canary-lifecycle"})
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	if followUp.ProviderSessionID != running.ProviderSessionID || followUp.Identity["session"] != running.ProviderSessionID {
		t.Fatalf("follow-up changed verified thread identity: %#v", followUp)
	}
	terminal := waitForCodexTerminal(t, adapter, running.ProviderHandle)
	if terminal.Status != "completed" || terminal.Reason != "terminal_and_final_delivery" {
		t.Fatalf("follow-up terminal = %#v", terminal)
	}
	if _, err := adapter.Send(t.Context(), SendRequest{HandleID: running.ProviderHandle, ActionID: "core-send-1", Prompt: "duplicate", CoreAuthorized: true, TaskCanary: "canary-lifecycle"}); err == nil {
		t.Fatal("duplicate core send action was accepted")
	}
	if err := adapter.Cleanup(CleanupRequest{HandleID: running.ProviderHandle}); err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
}

func TestLaunchDeliveryMissingAndStartupTimeout(t *testing.T) {
	workspace := t.TempDir()
	toolOnly := New(os.Args[0])
	toolOnly.Environment = []string{"CODEX_ADAPTER_HELPER=1", "CODEX_ADAPTER_HELPER_MODE=tool-only"}
	toolOnly.StartupTimeout = 2 * time.Second
	running, err := toolOnly.Launch(t.Context(), LaunchRequest{Workspace: workspace, Model: "fixture-model", Permission: "read-only", TrustAuthorized: true, Prompt: "tool only", TaskCanary: "canary-tool"})
	if err != nil {
		t.Fatalf("tool-only Launch() error = %v", err)
	}
	result := waitForCodexTerminal(t, toolOnly, running.ProviderHandle)
	if result.Status != "failed" || result.Reason != "delivery_missing" || !result.Terminal {
		t.Fatalf("tool-only result = %#v", result)
	}

	timeoutAdapter := New(os.Args[0])
	timeoutAdapter.Environment = []string{"CODEX_ADAPTER_HELPER=1", "CODEX_ADAPTER_HELPER_MODE=no-session-sleep"}
	timeoutAdapter.StartupTimeout = 25 * time.Millisecond
	timeoutResult, err := timeoutAdapter.Launch(t.Context(), LaunchRequest{Workspace: t.TempDir(), Model: "fixture-model", Permission: "read-only", TrustAuthorized: true, Prompt: "wait", TaskCanary: "canary-timeout"})
	if err != nil {
		t.Fatalf("timeout Launch() error = %v", err)
	}
	if timeoutResult.Status != "timed_out" || timeoutResult.Reason != "process_timeout" || !timeoutResult.Terminal || timeoutResult.ProviderSessionID != "" {
		t.Fatalf("startup timeout result = %#v", timeoutResult)
	}
}

func TestDeliveryMissingRecoversWithSameThreadDeliveryFollowUp(t *testing.T) {
	workspace := t.TempDir()
	adapter := New(os.Args[0])
	adapter.Environment = []string{"CODEX_ADAPTER_HELPER=1", "CODEX_ADAPTER_HELPER_MODE=tool-only"}
	adapter.StartupTimeout = 2 * time.Second
	request := LaunchRequest{Workspace: workspace, Model: "fixture-model", Permission: "read-only", TrustAuthorized: true, OutputLastMessagePath: filepath.Join(workspace, "first-last-message.txt"), Prompt: "return the first delivery", TaskCanary: "canary-delivery-recovery"}
	running, err := adapter.Launch(t.Context(), request)
	if err != nil {
		t.Fatalf("Launch() error = %v", err)
	}
	first := waitForCodexTerminal(t, adapter, running.ProviderHandle)
	if first.Status != "failed" || first.Reason != "delivery_missing" || first.ProviderSessionID == "" {
		t.Fatalf("initial delivery-missing result = %#v", first)
	}

	adapter.Environment = []string{"CODEX_ADAPTER_HELPER=1"}
	followUp, err := adapter.Send(t.Context(), SendRequest{HandleID: running.ProviderHandle, ActionID: "delivery-recovery-send", Prompt: "return the missing final delivery", CoreAuthorized: true, TaskCanary: request.TaskCanary})
	if err != nil {
		t.Fatalf("delivery recovery Send() error = %v", err)
	}
	terminal := waitForCodexTerminal(t, adapter, followUp.ProviderHandle)
	if terminal.Status != "completed" || terminal.Reason != "terminal_and_final_delivery" || terminal.ProviderSessionID != first.ProviderSessionID || terminal.FinalText != "ADAPTER_SMOKE_OK" || terminal.FinalSource != "durable_last_message" {
		t.Fatalf("delivery recovery terminal = %#v", terminal)
	}
	if err := adapter.Cleanup(CleanupRequest{HandleID: running.ProviderHandle}); err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
}

func TestSendReleasesActionIDWhenProcessStartFails(t *testing.T) {
	workspace := t.TempDir()
	adapter := New(os.Args[0])
	adapter.Environment = []string{"CODEX_ADAPTER_HELPER=1"}
	adapter.StartupTimeout = 2 * time.Second
	running, err := adapter.Launch(t.Context(), LaunchRequest{Workspace: workspace, Model: "fixture-model", Permission: "read-only", TrustAuthorized: true, Prompt: "first", TaskCanary: "canary-retry"})
	if err != nil {
		t.Fatalf("Launch() error = %v", err)
	}
	_ = waitForCodexTerminal(t, adapter, running.ProviderHandle)
	actionID := "core-send-retry"
	adapter.Executable = filepath.Join(t.TempDir(), "missing-codex.exe")
	if _, err := adapter.Send(t.Context(), SendRequest{HandleID: running.ProviderHandle, ActionID: actionID, Prompt: "retry", CoreAuthorized: true, TaskCanary: "canary-retry"}); err == nil {
		t.Fatal("Send() unexpectedly started a missing executable")
	}
	adapter.Executable = os.Args[0]
	followUp, err := adapter.Send(t.Context(), SendRequest{HandleID: running.ProviderHandle, ActionID: actionID, Prompt: "retry", CoreAuthorized: true, TaskCanary: "canary-retry"})
	if err != nil {
		t.Fatalf("same ActionID was not reusable after start failure: %v", err)
	}
	if result := waitForCodexTerminal(t, adapter, followUp.ProviderHandle); result.Status != "completed" {
		t.Fatalf("retry terminal = %#v", result)
	}
}

func TestMismatchedResumeSessionIsContaminatedWithoutReplacingAuthority(t *testing.T) {
	workspace := t.TempDir()
	adapter := New(os.Args[0])
	adapter.Environment = []string{"CODEX_ADAPTER_HELPER=1"}
	adapter.StartupTimeout = 2 * time.Second
	running, err := adapter.Launch(t.Context(), LaunchRequest{Workspace: workspace, Model: "fixture-model", Permission: "read-only", TrustAuthorized: true, Prompt: "first", TaskCanary: "canary-contaminated"})
	if err != nil {
		t.Fatalf("Launch() error = %v", err)
	}
	_ = waitForCodexTerminal(t, adapter, running.ProviderHandle)
	adapter.Environment = []string{"CODEX_ADAPTER_HELPER=1", "CODEX_ADAPTER_HELPER_MODE=mismatched-session"}
	followUp, err := adapter.Send(t.Context(), SendRequest{HandleID: running.ProviderHandle, ActionID: "core-send-contaminated", Prompt: "resume", CoreAuthorized: true, TaskCanary: "canary-contaminated"})
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	contaminated := waitForCodexTerminal(t, adapter, followUp.ProviderHandle)
	if contaminated.Status != "contaminated" || contaminated.Reason != "session_identity_mismatch" || contaminated.ProviderSessionID != "codex-foreign-thread" || contaminated.Identity["session"] != "codex-foreign-thread" {
		t.Fatalf("contaminated result = %#v", contaminated)
	}
	session, err := adapter.findSession(running.ProviderHandle)
	if err != nil {
		t.Fatal(err)
	}
	session.mu.Lock()
	authoritySession := session.providerSessionID
	session.mu.Unlock()
	if authoritySession != "codex-thread-1" {
		t.Fatalf("mismatched provider session replaced local resume authority: %q", authoritySession)
	}
	if _, err := adapter.Send(t.Context(), SendRequest{HandleID: running.ProviderHandle, ActionID: "core-send-after-contamination", Prompt: "must be rejected", CoreAuthorized: true, TaskCanary: "canary-contaminated"}); err == nil {
		t.Fatal("Send() was accepted after session contamination")
	}
	byProviderID, err := adapter.Observe(t.Context(), ObserveRequest{HandleID: "codex-thread-1"})
	if err != nil {
		t.Fatalf("Observe() by original thread = %v", err)
	}
	if byProviderID.ProviderSessionID != "codex-foreign-thread" {
		t.Fatalf("observed contamination did not remain visible = %#v", byProviderID)
	}
}

func waitForCodexTerminal(t *testing.T, adapter *Adapter, handle string) ExecutionResult {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		result, err := adapter.Observe(t.Context(), ObserveRequest{HandleID: handle})
		if err != nil {
			t.Fatal(err)
		}
		if result.Terminal {
			return result
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("timed out waiting for Codex terminal result")
	return ExecutionResult{}
}
