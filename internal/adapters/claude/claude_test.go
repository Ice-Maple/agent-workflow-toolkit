package claude

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"agent-workflow-toolkit/internal/adapters/cli"
)

func TestInvocationUsesReadOnlyStreamAndStrictMCPContract(t *testing.T) {
	workspace := filepath.Clean(`E:\workspace\authorized`)
	args, err := (Invocation{
		Executable:      "claude",
		Workspace:       workspace,
		Model:           "deepseek-v4-pro",
		Reasoning:       "high",
		Permission:      "read-only",
		Trust:           true,
		MCPIsolation:    "strict",
		DisableTools:    true,
		ResumeSessionID: "session-1",
	}).Args("return the smoke token")
	if err != nil {
		t.Fatalf("Args() error = %v", err)
	}
	joined := strings.Join(args, " ")
	for _, forbidden := range []string{
		"--dangerously-skip-permissions", "--allow-dangerously-skip-permissions", "--fallback-model", "--add-dir", "--no-session-persistence",
	} {
		if strings.Contains(joined, forbidden) {
			t.Fatalf("unsafe, scope-expanding or non-resumable flag %q in args: %s", forbidden, joined)
		}
	}
	for _, pair := range [][2]string{{"--model", "deepseek-v4-pro"}, {"--output-format", "stream-json"}, {"--permission-mode", "plan"}, {"--effort", "high"}, {"--resume", "session-1"}} {
		if !containsPair(args, pair[0], pair[1]) {
			t.Fatalf("missing argument pair %q=%q: %#v", pair[0], pair[1], args)
		}
	}
	if !contains(args, "--strict-mcp-config") || !contains(args, "--tools=") || args[len(args)-2] != "-p" || args[len(args)-1] != "return the smoke token" {
		t.Fatalf("unexpected Claude args: %#v", args)
	}
}

func TestPreflightSeparatesTrustPermissionReasoningAndMCPBranches(t *testing.T) {
	workspace := t.TempDir()
	adapter := New("claude")
	probeCalls := 0
	adapter.Probe = func(_ context.Context, _ string, args []string) (cli.Result, error) {
		probeCalls++
		if !contains(args, "--version") {
			t.Fatalf("unexpected probe args: %#v", args)
		}
		return cli.Result{ExitCode: 0, Stdout: []byte("2.1.226 (Claude Code)\n")}, nil
	}

	trust, err := adapter.Preflight(t.Context(), PreflightRequest{Workspace: workspace, Model: "deepseek-v4-pro", TrustAuthorized: false})
	if err != nil || trust.Status != "blocked" || trust.Code != "workspace_trust_required" || probeCalls != 0 {
		t.Fatalf("trust preflight = %#v, calls=%d, err=%v", trust, probeCalls, err)
	}

	write, err := adapter.Preflight(t.Context(), PreflightRequest{Workspace: workspace, Model: "deepseek-v4-pro", Permission: "workspace-write", TrustAuthorized: true})
	if err != nil || write.Status != string(CapabilityUnsupported) || write.Code != "write_capability_unproven" || probeCalls != 0 {
		t.Fatalf("write preflight = %#v, calls=%d, err=%v", write, probeCalls, err)
	}

	reasoning, err := adapter.Preflight(t.Context(), PreflightRequest{Workspace: workspace, Model: "deepseek-v4-pro", Reasoning: "ultra", TrustAuthorized: true})
	if err != nil || reasoning.Status != string(CapabilityUnsupported) || reasoning.Code != "reasoning_unsupported" || probeCalls != 0 {
		t.Fatalf("reasoning preflight = %#v, calls=%d, err=%v", reasoning, probeCalls, err)
	}

	missingMCP, err := adapter.Preflight(t.Context(), PreflightRequest{Workspace: workspace, Model: "deepseek-v4-pro", TrustAuthorized: true, MCPIsolation: "strict", MCPAllowlist: []string{"evidence"}})
	if err != nil || missingMCP.Status != "blocked" || missingMCP.Code != "mcp_config_required" || probeCalls != 0 {
		t.Fatalf("missing MCP preflight = %#v, calls=%d, err=%v", missingMCP, probeCalls, err)
	}

	unsupportedMCP, err := adapter.Preflight(t.Context(), PreflightRequest{Workspace: workspace, Model: "deepseek-v4-pro", TrustAuthorized: true, MCPIsolation: "unsupported", MCPAllowlist: []string{"evidence"}})
	if err != nil || unsupportedMCP.Status != string(CapabilityUnsupported) || unsupportedMCP.Code != "mcp_unsupported" || probeCalls != 0 {
		t.Fatalf("unsupported MCP preflight = %#v, calls=%d, err=%v", unsupportedMCP, probeCalls, err)
	}

	configPath := filepath.Join(workspace, "task-mcp.json")
	if err := os.WriteFile(configPath, []byte(`{"mcpServers":{"evidence":{}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	passed, err := adapter.Preflight(t.Context(), PreflightRequest{
		Workspace: workspace, Model: "deepseek-v4-pro", Reasoning: "high", TrustAuthorized: true,
		MCPIsolation: "config-isolated", MCPAllowlist: []string{"evidence"}, MCPConfigPath: configPath,
	})
	if err != nil || passed.Status != "passed" || !passed.ModelRegistered || passed.MCPIsolation != "config-isolated" || probeCalls != 1 {
		t.Fatalf("passed preflight = %#v, calls=%d, err=%v", passed, probeCalls, err)
	}
}

func TestLaunchObserveDeliveryMissingAndTranscriptUseSeparateEvidence(t *testing.T) {
	workspace := t.TempDir()
	adapter := helperAdapter()
	request := LaunchRequest{
		Workspace: workspace, Model: "fixture-model", Permission: "read-only", TrustAuthorized: true,
		DisableTools: true, Prompt: "return the smoke token", TaskCanary: "canary-1",
		TranscriptPath: filepath.Join(workspace, "claude-transcript.jsonl"),
	}
	running, err := adapter.Launch(t.Context(), request)
	if err != nil {
		t.Fatalf("Launch() error = %v", err)
	}
	if running.Status != "running" || running.ProviderSessionID != "claude-session-1" || running.Identity["provider"] != Provider || running.TaskCanary != "canary-1" {
		t.Fatalf("launch result = %#v", running)
	}
	completed := waitForTerminal(t, adapter, running.ProviderHandle)
	if completed.Status != "completed" || completed.Reason != "terminal_and_final_delivery" || completed.FinalText != "ADAPTER_SMOKE_OK" || !completed.Terminal || !completed.TranscriptWritten {
		t.Fatalf("completed result = %#v", completed)
	}
	if transcript, err := os.ReadFile(request.TranscriptPath); err != nil || !strings.Contains(string(transcript), "claude-session-1") {
		t.Fatalf("transcript = %q, err=%v", transcript, err)
	}

	toolOnly := helperAdapter()
	toolOnly.Environment = []string{"CLAUDE_ADAPTER_HELPER=1", "CLAUDE_ADAPTER_HELPER_MODE=tool-only"}
	toolOnlyResult, err := toolOnly.Launch(t.Context(), LaunchRequest{Workspace: workspace, Model: "fixture-model", Permission: "read-only", TrustAuthorized: true, Prompt: "tool only", TaskCanary: "canary-tool"})
	if err != nil {
		t.Fatalf("tool-only Launch() error = %v", err)
	}
	toolOnlyResult = waitForTerminalWithExpectedReason(t, toolOnly, toolOnlyResult.ProviderHandle, "delivery_missing")
	if toolOnlyResult.Status != "failed" || !toolOnlyResult.Terminal || toolOnlyResult.Reason != "delivery_missing" {
		t.Fatalf("tool-only result = %#v", toolOnlyResult)
	}
}

func TestLaunchStartupTimeoutReportsOnlyAfterProcessTermination(t *testing.T) {
	workspace := t.TempDir()
	adapter := helperAdapter()
	adapter.Environment = []string{"CLAUDE_ADAPTER_HELPER=1", "CLAUDE_ADAPTER_HELPER_MODE=no-session-sleep"}
	adapter.StartupTimeout = 25 * time.Millisecond
	result, err := adapter.Launch(t.Context(), LaunchRequest{Workspace: workspace, Model: "fixture-model", Permission: "read-only", TrustAuthorized: true, Prompt: "wait", TaskCanary: "canary-timeout"})
	if err != nil {
		t.Fatalf("Launch() error = %v", err)
	}
	defer func() { _ = adapter.Cleanup(CleanupRequest{HandleID: result.ProviderHandle}) }()
	if result.Status != "timed_out" || result.Reason != "process_timeout" || !result.Terminal || result.ProviderSessionID != "" {
		t.Fatalf("startup-timeout result = %#v", result)
	}
}

func TestSendRequiresCoreAuthorizationAndResumesOnlyVerifiedSession(t *testing.T) {
	workspace := t.TempDir()
	adapter := helperAdapter()
	running, err := adapter.Launch(t.Context(), LaunchRequest{Workspace: workspace, Model: "fixture-model", Permission: "read-only", TrustAuthorized: true, Prompt: "first", TaskCanary: "canary-send"})
	if err != nil {
		t.Fatalf("Launch() error = %v", err)
	}
	_ = waitForTerminal(t, adapter, running.ProviderHandle)
	if _, err := adapter.Send(t.Context(), SendRequest{HandleID: running.ProviderHandle, ActionID: "action-unauthorized", Prompt: "follow-up", TaskCanary: "canary-send"}); err == nil {
		t.Fatal("unauthorized follow-up was accepted")
	}
	followUp, err := adapter.Send(t.Context(), SendRequest{HandleID: running.ProviderHandle, ActionID: "action-1", Prompt: "delivery", CoreAuthorized: true, TaskCanary: "canary-send"})
	if err != nil {
		t.Fatalf("authorized Send() error = %v", err)
	}
	if followUp.ProviderSessionID != running.ProviderSessionID || followUp.Identity["session"] != running.ProviderSessionID {
		t.Fatalf("follow-up changed verified session identity: %#v", followUp)
	}
	terminal := waitForTerminal(t, adapter, running.ProviderHandle)
	if terminal.Status != "completed" || terminal.FinalText != "ADAPTER_SEND_OK" {
		t.Fatalf("follow-up terminal = %#v", terminal)
	}
	if _, err := adapter.Send(t.Context(), SendRequest{HandleID: running.ProviderHandle, ActionID: "action-1", Prompt: "duplicate", CoreAuthorized: true, TaskCanary: "canary-send"}); err == nil {
		t.Fatal("a completed core send action was reused")
	}
}

func TestSendReleasesActionIDWhenProcessStartFails(t *testing.T) {
	workspace := t.TempDir()
	adapter := helperAdapter()
	running, err := adapter.Launch(t.Context(), LaunchRequest{Workspace: workspace, Model: "fixture-model", Permission: "read-only", TrustAuthorized: true, Prompt: "first", TaskCanary: "canary-retry"})
	if err != nil {
		t.Fatalf("Launch() error = %v", err)
	}
	_ = waitForTerminal(t, adapter, running.ProviderHandle)
	actionID := "action-retry"
	adapter.Executable = filepath.Join(t.TempDir(), "missing-claude.exe")
	if _, err := adapter.Send(t.Context(), SendRequest{HandleID: running.ProviderHandle, ActionID: actionID, Prompt: "delivery", CoreAuthorized: true, TaskCanary: "canary-retry"}); err == nil {
		t.Fatal("Send() unexpectedly started a missing executable")
	}
	adapter.Executable = os.Args[0]
	followUp, err := adapter.Send(t.Context(), SendRequest{HandleID: running.ProviderHandle, ActionID: actionID, Prompt: "delivery", CoreAuthorized: true, TaskCanary: "canary-retry"})
	if err != nil {
		t.Fatalf("same ActionID was not reusable after start failure: %v", err)
	}
	terminal := waitForTerminal(t, adapter, followUp.ProviderHandle)
	if terminal.Status != "completed" || terminal.FinalText != "ADAPTER_SEND_OK" {
		t.Fatalf("retry terminal = %#v", terminal)
	}
}

func TestMismatchedResumeSessionIsContaminatedWithoutReplacingResumeAuthority(t *testing.T) {
	workspace := t.TempDir()
	adapter := helperAdapter()
	running, err := adapter.Launch(t.Context(), LaunchRequest{Workspace: workspace, Model: "fixture-model", Permission: "read-only", TrustAuthorized: true, Prompt: "first", TaskCanary: "canary-contaminated"})
	if err != nil {
		t.Fatalf("Launch() error = %v", err)
	}
	_ = waitForTerminal(t, adapter, running.ProviderHandle)
	adapter.Environment = []string{"CLAUDE_ADAPTER_HELPER=1", "CLAUDE_ADAPTER_HELPER_MODE=mismatched-session"}
	followUp, err := adapter.Send(t.Context(), SendRequest{HandleID: running.ProviderHandle, ActionID: "action-contaminated", Prompt: "resume", CoreAuthorized: true, TaskCanary: "canary-contaminated"})
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	contaminated := waitForTerminal(t, adapter, followUp.ProviderHandle)
	if contaminated.Status != "contaminated" || contaminated.Reason != "session_identity_mismatch" || contaminated.ProviderSessionID != "claude-foreign-session" || contaminated.Identity["session"] != "claude-foreign-session" {
		t.Fatalf("contaminated result = %#v", contaminated)
	}
	session, err := adapter.findSession(running.ProviderHandle)
	if err != nil {
		t.Fatal(err)
	}
	session.mu.Lock()
	authoritySession := session.providerSessionID
	session.mu.Unlock()
	if authoritySession != "claude-session-1" {
		t.Fatalf("mismatched provider session replaced local resume authority: %q", authoritySession)
	}
	if _, err := adapter.Send(t.Context(), SendRequest{HandleID: running.ProviderHandle, ActionID: "action-after-contamination", Prompt: "must be rejected", CoreAuthorized: true, TaskCanary: "canary-contaminated"}); err == nil {
		t.Fatal("Send() was accepted after session contamination")
	}
}

func TestCancelStopsProcessAndTranscriptFailureDoesNotEmitFalseReference(t *testing.T) {
	workspace := t.TempDir()
	adapter := helperAdapter()
	adapter.Environment = []string{"CLAUDE_ADAPTER_HELPER=1", "CLAUDE_ADAPTER_HELPER_MODE=sleep"}
	running, err := adapter.Launch(t.Context(), LaunchRequest{Workspace: workspace, Model: "fixture-model", Permission: "read-only", TrustAuthorized: true, Prompt: "sleep", TaskCanary: "canary-cancel"})
	if err != nil {
		t.Fatalf("Launch() error = %v", err)
	}
	cancelContext, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	cancelled, err := adapter.Cancel(cancelContext, CancelRequest{HandleID: running.ProviderHandle})
	if err != nil {
		t.Fatalf("Cancel() error = %v", err)
	}
	if cancelled.Status != "cancelled" || !cancelled.Terminal || cancelled.SideEffectState != "none" {
		t.Fatalf("cancel result = %#v", cancelled)
	}

	transcriptAdapter := helperAdapter()
	transcriptAdapter.TranscriptWriter = func(string, []byte) error { return errors.New("transcript write unavailable") }
	transcriptRunning, err := transcriptAdapter.Launch(t.Context(), LaunchRequest{Workspace: workspace, Model: "fixture-model", Permission: "read-only", TrustAuthorized: true, Prompt: "first", TaskCanary: "canary-transcript", TranscriptPath: filepath.Join(workspace, "transcript.jsonl")})
	if err != nil {
		t.Fatalf("transcript Launch() error = %v", err)
	}
	transcriptResult := waitForTerminal(t, transcriptAdapter, transcriptRunning.ProviderHandle)
	if transcriptResult.Status != "completed" || transcriptResult.TranscriptWritten {
		t.Fatalf("transcript failure result = %#v", transcriptResult)
	}
	for _, ref := range transcriptResult.RawEventRefs {
		if strings.HasPrefix(ref, "transcript:") {
			t.Fatalf("transcript failure emitted a false reference: %#v", transcriptResult.RawEventRefs)
		}
	}
}

func TestExtractIdentityRejectsMismatchedSessionAdapterAndCanary(t *testing.T) {
	workspace := t.TempDir()
	if _, err := ExtractIdentity("session-1", workspace, Provider, "session-2", "canary", "canary"); err == nil {
		t.Fatal("session mismatch was accepted")
	}
	if _, err := ExtractIdentity("session-1", workspace, "foreign", "", "canary", "canary"); err == nil {
		t.Fatal("adapter mismatch was accepted")
	}
	if _, err := ExtractIdentity("session-1", workspace, Provider, "", "canary", "wrong"); err == nil {
		t.Fatal("canary mismatch was accepted")
	}
	identity, err := ExtractIdentity("session-1", workspace, Provider, "", "canary", "canary")
	if err != nil || identity["provider"] != Provider || identity["session"] != "session-1" {
		t.Fatalf("valid identity = %#v, err=%v", identity, err)
	}
}

func helperAdapter() *Adapter {
	adapter := New(os.Args[0])
	adapter.Environment = []string{"CLAUDE_ADAPTER_HELPER=1"}
	adapter.StartupTimeout = 2 * time.Second
	return adapter
}

func waitForTerminal(t *testing.T, adapter *Adapter, handle string) ExecutionResult {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		result, err := adapter.Observe(t.Context(), ObserveRequest{HandleID: handle})
		if err != nil {
			t.Fatalf("Observe() error = %v", err)
		}
		if result.Terminal {
			return result
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("timed out waiting for Claude terminal result")
	return ExecutionResult{}
}

func waitForTerminalWithExpectedReason(t *testing.T, adapter *Adapter, handle, reason string) ExecutionResult {
	t.Helper()
	result := waitForTerminal(t, adapter, handle)
	if result.Reason != reason {
		t.Fatalf("terminal reason = %q, want %q; result=%#v", result.Reason, reason, result)
	}
	return result
}

func TestMain(m *testing.M) {
	if os.Getenv("CLAUDE_ADAPTER_HELPER") == "1" {
		if os.Getenv("CLAUDE_ADAPTER_HELPER_MODE") == "no-session-sleep" {
			time.Sleep(30 * time.Second)
			os.Exit(0)
		}
		if os.Getenv("CLAUDE_ADAPTER_HELPER_MODE") == "mismatched-session" && hasArg("--resume") {
			fmt.Println(`{"type":"system","subtype":"init","session_id":"claude-foreign-session"}`)
			fmt.Println(`{"type":"assistant","session_id":"claude-foreign-session","message":{"content":[{"type":"text","text":"FOREIGN_SESSION"}]}}`)
			fmt.Println(`{"type":"result","subtype":"success","is_error":false,"result":"FOREIGN_SESSION","session_id":"claude-foreign-session"}`)
			os.Exit(0)
		}
		fmt.Println(`{"type":"system","subtype":"init","session_id":"claude-session-1"}`)
		if os.Getenv("CLAUDE_ADAPTER_HELPER_MODE") == "sleep" {
			time.Sleep(30 * time.Second)
			os.Exit(0)
		}
		if os.Getenv("CLAUDE_ADAPTER_HELPER_MODE") == "tool-only" {
			fmt.Println(`{"type":"assistant","session_id":"claude-session-1","message":{"content":[{"type":"tool_use","name":"Read","input":{}}]}}`)
			fmt.Println(`{"type":"result","subtype":"success","is_error":false,"session_id":"claude-session-1"}`)
			os.Exit(0)
		}
		if hasArg("--resume") {
			fmt.Println(`{"type":"assistant","session_id":"claude-session-1","message":{"content":[{"type":"text","text":"ADAPTER_SEND_OK"}]}}`)
			fmt.Println(`{"type":"result","subtype":"success","is_error":false,"result":"ADAPTER_SEND_OK","session_id":"claude-session-1"}`)
			os.Exit(0)
		}
		fmt.Println(`{"type":"assistant","session_id":"claude-session-1","message":{"content":[{"type":"text","text":"ADAPTER_SMOKE_OK"}]}}`)
		fmt.Println(`{"type":"result","subtype":"success","is_error":false,"result":"ADAPTER_SMOKE_OK","session_id":"claude-session-1"}`)
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func hasArg(wanted string) bool {
	for _, arg := range os.Args {
		if arg == wanted {
			return true
		}
	}
	return false
}

func contains(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func containsPair(values []string, key, value string) bool {
	for index := 0; index+1 < len(values); index++ {
		if values[index] == key && values[index+1] == value {
			return true
		}
	}
	return false
}
