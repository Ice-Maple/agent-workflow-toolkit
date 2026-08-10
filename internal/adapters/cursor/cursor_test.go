package cursor

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

func TestInvocationUsesExplicitReadOnlyStreamContract(t *testing.T) {
	workspace := filepath.Clean(`E:\workspace\authorized`)
	args, err := (Invocation{
		Executable:      "agent",
		Workspace:       workspace,
		Model:           "cursor-grok-4.5-high",
		Mode:            "plan",
		Trust:           true,
		ResumeSessionID: "session-1",
	}).Args("return the smoke token")
	if err != nil {
		t.Fatalf("Args() error = %v", err)
	}
	joined := strings.Join(args, " ")
	for _, forbidden := range []string{"--force", "--yolo", "--approve-mcps", "--sandbox", "--add-dir"} {
		if strings.Contains(joined, forbidden) {
			t.Fatalf("unsafe or scope-expanding flag %q in args: %s", forbidden, joined)
		}
	}
	if !containsPair(args, "--output-format", "stream-json") || !containsPair(args, "--mode", "plan") || !containsPair(args, "--resume", "session-1") || !contains(args, "--trust") {
		t.Fatalf("unexpected Cursor args: %#v", args)
	}
	if args[len(args)-1] != "return the smoke token" {
		t.Fatalf("prompt was not kept positional and last: %#v", args)
	}
}

func TestPreflightSeparatesTrustSandboxAndMCPBranches(t *testing.T) {
	workspace := t.TempDir()
	probeCalls := 0
	adapter := New("agent")
	adapter.Probe = func(_ context.Context, _ string, args []string) (cli.Result, error) {
		probeCalls++
		if contains(args, "--version") {
			return cli.Result{ExitCode: 0, Stdout: []byte("2026.08.04-aaa8809\n")}, nil
		}
		return cli.Result{ExitCode: 0, Stdout: []byte("cursor-grok-4.5-high - Cursor Grok 4.5\n")}, nil
	}

	trust, err := adapter.Preflight(t.Context(), PreflightRequest{Workspace: workspace, Model: "cursor-grok-4.5-high", TrustAuthorized: false})
	if err != nil || trust.Status != "blocked" || trust.Code != "workspace_trust_required" || probeCalls != 0 {
		t.Fatalf("trust preflight = %#v, calls=%d, err=%v", trust, probeCalls, err)
	}

	sandbox, err := adapter.Preflight(t.Context(), PreflightRequest{Workspace: workspace, Model: "cursor-grok-4.5-high", TrustAuthorized: true, Sandbox: "enabled"})
	if err != nil || sandbox.Status != string(CapabilityUnsupported) || sandbox.Code != "windows_sandbox_unsupported" || probeCalls != 0 {
		t.Fatalf("sandbox preflight = %#v, calls=%d, err=%v", sandbox, probeCalls, err)
	}

	strictMCP, err := adapter.Preflight(t.Context(), PreflightRequest{Workspace: workspace, Model: "cursor-grok-4.5-high", TrustAuthorized: true, MCPIsolation: "strict", MCPAllowlist: []string{"evidence"}})
	if err != nil || strictMCP.Status != string(CapabilityUnsupported) || strictMCP.Code != "mcp_strict_unsupported" || probeCalls != 0 {
		t.Fatalf("strict MCP preflight = %#v, calls=%d, err=%v", strictMCP, probeCalls, err)
	}

	configMCP, err := adapter.Preflight(t.Context(), PreflightRequest{Workspace: workspace, Model: "cursor-grok-4.5-high", TrustAuthorized: true, MCPIsolation: "config-isolated", MCPAllowlist: []string{"evidence"}})
	if err != nil || configMCP.Status != string(CapabilityUnsupported) || configMCP.Code != "mcp_config_isolation_unsupported" || probeCalls != 0 {
		t.Fatalf("config MCP preflight = %#v, calls=%d, err=%v", configMCP, probeCalls, err)
	}
	if _, err := adapter.Launch(t.Context(), LaunchRequest{Workspace: workspace, Model: "cursor-grok-4.5-high", Mode: "plan", Permission: "read-only", TrustAuthorized: true, Prompt: "review", TaskCanary: "mcp-canary", MCPIsolation: "config-isolated", MCPAllowlist: []string{"evidence"}}); err == nil {
		t.Fatal("config-isolated MCP allowlist unexpectedly reached launch")
	}
}

func TestPreflightChecksVersionAndRegisteredModel(t *testing.T) {
	workspace := t.TempDir()
	adapter := New("agent")
	adapter.Probe = func(_ context.Context, _ string, args []string) (cli.Result, error) {
		if contains(args, "--version") {
			return cli.Result{ExitCode: 0, Stdout: []byte("2026.08.04-aaa8809\n")}, nil
		}
		return cli.Result{ExitCode: 0, Stdout: []byte("cursor-grok-4.5-high - Cursor Grok 4.5\n")}, nil
	}
	passed, err := adapter.Preflight(t.Context(), PreflightRequest{Workspace: workspace, Model: "cursor-grok-4.5-high", Mode: "plan", Permission: "read-only", TrustAuthorized: true})
	if err != nil || passed.Status != "passed" || !passed.ModelRegistered || passed.Version == "" {
		t.Fatalf("passed preflight = %#v, err=%v", passed, err)
	}

	adapter.Probe = func(_ context.Context, _ string, args []string) (cli.Result, error) {
		if contains(args, "--version") {
			return cli.Result{ExitCode: 0, Stdout: []byte("2026.08.04-aaa8809\n")}, nil
		}
		return cli.Result{ExitCode: 0, Stdout: []byte("cursor-grok-4.5-low - Cursor Grok 4.5\n")}, nil
	}
	missing, err := adapter.Preflight(t.Context(), PreflightRequest{Workspace: workspace, Model: "cursor-grok-4.5-high", TrustAuthorized: true})
	if err != nil || missing.Status != "blocked" || missing.Code != "model_not_registered" {
		t.Fatalf("missing model preflight = %#v, err=%v", missing, err)
	}
}

func TestLaunchObserveAndDeliveryMissingUseSeparateEvidence(t *testing.T) {
	workspace := t.TempDir()
	adapter := New(os.Args[0])
	adapter.Environment = []string{"CURSOR_ADAPTER_HELPER=1"}
	adapter.StartupTimeout = 2 * time.Second
	request := LaunchRequest{Workspace: workspace, Model: "fixture-model", Mode: "plan", Permission: "read-only", TrustAuthorized: true, Prompt: "return the smoke token", TaskCanary: "canary-1"}
	request.TranscriptPath = filepath.Join(workspace, "cursor-transcript.jsonl")

	running, err := adapter.Launch(t.Context(), request)
	if err != nil {
		t.Fatalf("Launch() error = %v", err)
	}
	if running.Status != "running" || running.ProviderSessionID != "cursor-session-1" || running.Identity["provider"] != Provider || running.TaskCanary != "canary-1" {
		t.Fatalf("launch result = %#v", running)
	}
	completed := waitForTerminal(t, adapter, running.ProviderHandle)
	if completed.Status != "completed" || completed.Reason != "terminal_and_final_delivery" || completed.FinalText != "ADAPTER_SMOKE_OK" || !completed.Terminal || !completed.TranscriptWritten {
		t.Fatalf("completed result = %#v", completed)
	}
	if transcript, err := os.ReadFile(request.TranscriptPath); err != nil || !strings.Contains(string(transcript), "cursor-session-1") {
		t.Fatalf("transcript = %q, err=%v", transcript, err)
	}

	toolOnly := New(os.Args[0])
	toolOnly.Environment = []string{"CURSOR_ADAPTER_HELPER=1", "CURSOR_ADAPTER_HELPER_MODE=tool-only"}
	toolOnly.StartupTimeout = 2 * time.Second
	toolOnlyResult, err := toolOnly.Launch(t.Context(), request)
	if err != nil {
		t.Fatalf("tool-only Launch() error = %v", err)
	}
	if toolOnlyResult.ProviderSessionID != "cursor-session-1" {
		t.Fatalf("tool-only launch identity = %#v", toolOnlyResult)
	}
	toolOnlyResult = waitForTerminalWithExpectedReason(t, toolOnly, toolOnlyResult.ProviderHandle, "delivery_missing")
	if toolOnlyResult.Status != "failed" || !toolOnlyResult.Terminal || toolOnlyResult.Reason != "delivery_missing" {
		t.Fatalf("tool-only result = %#v", toolOnlyResult)
	}
}

func TestLaunchStartupTimeoutReportsOnlyAfterProcessTermination(t *testing.T) {
	workspace := t.TempDir()
	adapter := New(os.Args[0])
	adapter.Environment = []string{"CURSOR_ADAPTER_HELPER=1", "CURSOR_ADAPTER_HELPER_MODE=no-session-sleep"}
	adapter.StartupTimeout = 25 * time.Millisecond
	request := LaunchRequest{Workspace: workspace, Model: "fixture-model", Mode: "plan", Permission: "read-only", TrustAuthorized: true, Prompt: "wait for a session", TaskCanary: "canary-startup-timeout"}

	result, err := adapter.Launch(t.Context(), request)
	if err != nil {
		t.Fatalf("Launch() error = %v", err)
	}
	defer func() { _ = adapter.Cleanup(CleanupRequest{HandleID: result.ProviderHandle}) }()
	if result.Status != "timed_out" || result.Reason != "process_timeout" || !result.Terminal || result.ProviderSessionID != "" {
		t.Fatalf("startup-timeout result = %#v", result)
	}
}

func TestSendRequiresCoreAuthorizationAndCancelStopsProcess(t *testing.T) {
	workspace := t.TempDir()
	adapter := New(os.Args[0])
	adapter.Environment = []string{"CURSOR_ADAPTER_HELPER=1", "CURSOR_ADAPTER_HELPER_MODE=sleep"}
	adapter.StartupTimeout = 2 * time.Second
	request := LaunchRequest{Workspace: workspace, Model: "fixture-model", Mode: "plan", Permission: "read-only", TrustAuthorized: true, Prompt: "sleep", TaskCanary: "canary-2"}
	running, err := adapter.Launch(t.Context(), request)
	if err != nil {
		t.Fatalf("Launch() error = %v", err)
	}
	if _, err := adapter.Send(t.Context(), SendRequest{HandleID: running.ProviderHandle, ActionID: "action-1", Prompt: "delivery only", TaskCanary: "canary-2"}); err == nil {
		t.Fatal("unauthorized follow-up was accepted")
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
}

func TestCoreAuthorizedSendResumesTheVerifiedSession(t *testing.T) {
	workspace := t.TempDir()
	adapter := New(os.Args[0])
	adapter.Environment = []string{"CURSOR_ADAPTER_HELPER=1"}
	adapter.StartupTimeout = 2 * time.Second
	running, err := adapter.Launch(t.Context(), LaunchRequest{Workspace: workspace, Model: "fixture-model", Mode: "plan", Permission: "read-only", TrustAuthorized: true, Prompt: "return the first token", TaskCanary: "canary-send"})
	if err != nil {
		t.Fatalf("Launch() error = %v", err)
	}
	_ = waitForTerminal(t, adapter, running.ProviderHandle)
	followUp, err := adapter.Send(t.Context(), SendRequest{HandleID: running.ProviderHandle, ActionID: "core-send-1", Prompt: "return the delivery token", CoreAuthorized: true, TaskCanary: "canary-send"})
	if err != nil {
		t.Fatalf("authorized Send() error = %v", err)
	}
	if followUp.ProviderSessionID != running.ProviderSessionID || followUp.Identity["session"] != running.ProviderSessionID {
		t.Fatalf("follow-up changed verified session identity: %#v", followUp)
	}
	terminal := waitForTerminal(t, adapter, running.ProviderHandle)
	if terminal.Status != "completed" || terminal.Reason != "terminal_and_final_delivery" {
		t.Fatalf("follow-up terminal = %#v", terminal)
	}
}

func TestSendRejectsFollowUpWhileAnotherSendIsInFlight(t *testing.T) {
	workspace := t.TempDir()
	adapter := New(os.Args[0])
	adapter.Environment = []string{"CURSOR_ADAPTER_HELPER=1"}
	adapter.StartupTimeout = 2 * time.Second
	running, err := adapter.Launch(t.Context(), LaunchRequest{Workspace: workspace, Model: "fixture-model", Mode: "plan", Permission: "read-only", TrustAuthorized: true, Prompt: "first", TaskCanary: "canary-concurrent-send"})
	if err != nil {
		t.Fatalf("Launch() error = %v", err)
	}
	_ = waitForTerminal(t, adapter, running.ProviderHandle)
	session, err := adapter.findSession(running.ProviderHandle)
	if err != nil {
		t.Fatal(err)
	}
	session.mu.Lock()
	session.sendInFlight = true
	session.mu.Unlock()
	if _, err := adapter.Send(t.Context(), SendRequest{HandleID: running.ProviderHandle, ActionID: "action-concurrent-send", Prompt: "must wait", CoreAuthorized: true, TaskCanary: "canary-concurrent-send"}); err == nil || !strings.Contains(err.Error(), "already has a follow-up in flight") {
		t.Fatalf("concurrent Send() error = %v", err)
	}
	session.mu.Lock()
	session.sendInFlight = false
	session.mu.Unlock()
	followUp, err := adapter.Send(t.Context(), SendRequest{HandleID: running.ProviderHandle, ActionID: "action-concurrent-send", Prompt: "resume after guard", CoreAuthorized: true, TaskCanary: "canary-concurrent-send"})
	if err != nil {
		t.Fatalf("guarded ActionID was not reusable: %v", err)
	}
	terminal := waitForTerminal(t, adapter, followUp.ProviderHandle)
	if terminal.Status != "completed" || terminal.Reason != "terminal_and_final_delivery" {
		t.Fatalf("follow-up terminal = %#v", terminal)
	}
}

func TestResumeWithoutSessionIdentityFails(t *testing.T) {
	workspace := t.TempDir()
	adapter := New(os.Args[0])
	adapter.Environment = []string{"CURSOR_ADAPTER_HELPER=1"}
	adapter.StartupTimeout = 2 * time.Second
	running, err := adapter.Launch(t.Context(), LaunchRequest{Workspace: workspace, Model: "fixture-model", Mode: "plan", Permission: "read-only", TrustAuthorized: true, Prompt: "first", TaskCanary: "canary-missing-session"})
	if err != nil {
		t.Fatalf("Launch() error = %v", err)
	}
	_ = waitForTerminal(t, adapter, running.ProviderHandle)
	adapter.Environment = []string{"CURSOR_ADAPTER_HELPER=1", "CURSOR_ADAPTER_HELPER_MODE=no-session-on-resume"}
	followUp, err := adapter.Send(t.Context(), SendRequest{HandleID: running.ProviderHandle, ActionID: "action-missing-session", Prompt: "resume without identity", CoreAuthorized: true, TaskCanary: "canary-missing-session"})
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	terminal := waitForTerminal(t, adapter, followUp.ProviderHandle)
	if terminal.Status != "failed" || terminal.Reason != "session_identity_missing" || terminal.ProviderSessionID != running.ProviderSessionID {
		t.Fatalf("missing-session result = %#v", terminal)
	}
}

func TestProviderTerminalErrorIsClassified(t *testing.T) {
	workspace := t.TempDir()
	adapter := New(os.Args[0])
	adapter.Environment = []string{"CURSOR_ADAPTER_HELPER=1", "CURSOR_ADAPTER_HELPER_MODE=provider-error"}
	adapter.StartupTimeout = 2 * time.Second
	running, err := adapter.Launch(t.Context(), LaunchRequest{Workspace: workspace, Model: "fixture-model", Mode: "plan", Permission: "read-only", TrustAuthorized: true, Prompt: "provider error", TaskCanary: "canary-provider-error"})
	if err != nil {
		t.Fatalf("Launch() error = %v", err)
	}
	terminal := waitForTerminal(t, adapter, running.ProviderHandle)
	if terminal.Status != "failed" || terminal.Reason != "provider_terminal_error" || terminal.FinalText != "" {
		t.Fatalf("provider-error result = %#v", terminal)
	}
}

func TestSendReleasesActionIDWhenProcessStartFails(t *testing.T) {
	workspace := t.TempDir()
	adapter := New(os.Args[0])
	adapter.Environment = []string{"CURSOR_ADAPTER_HELPER=1"}
	adapter.StartupTimeout = 2 * time.Second
	running, err := adapter.Launch(t.Context(), LaunchRequest{Workspace: workspace, Model: "fixture-model", Mode: "plan", Permission: "read-only", TrustAuthorized: true, Prompt: "return the first token", TaskCanary: "canary-send-retry"})
	if err != nil {
		t.Fatalf("Launch() error = %v", err)
	}
	_ = waitForTerminal(t, adapter, running.ProviderHandle)
	actionID := "core-send-retry"
	adapter.Executable = filepath.Join(t.TempDir(), "missing-agent.exe")
	if _, err := adapter.Send(t.Context(), SendRequest{HandleID: running.ProviderHandle, ActionID: actionID, Prompt: "return the delivery token", CoreAuthorized: true, TaskCanary: "canary-send-retry"}); err == nil {
		t.Fatal("Send() unexpectedly started a missing executable")
	}
	adapter.Executable = os.Args[0]
	followUp, err := adapter.Send(t.Context(), SendRequest{HandleID: running.ProviderHandle, ActionID: actionID, Prompt: "return the delivery token", CoreAuthorized: true, TaskCanary: "canary-send-retry"})
	if err != nil {
		t.Fatalf("same ActionID was not reusable after start failure: %v", err)
	}
	terminal := waitForTerminal(t, adapter, followUp.ProviderHandle)
	if terminal.Status != "completed" || terminal.Reason != "terminal_and_final_delivery" {
		t.Fatalf("retry terminal = %#v", terminal)
	}
}

func TestMismatchedResumeSessionIsContaminatedWithoutReplacingResumeAuthority(t *testing.T) {
	workspace := t.TempDir()
	adapter := New(os.Args[0])
	adapter.Environment = []string{"CURSOR_ADAPTER_HELPER=1"}
	adapter.StartupTimeout = 2 * time.Second
	running, err := adapter.Launch(t.Context(), LaunchRequest{Workspace: workspace, Model: "fixture-model", Mode: "plan", Permission: "read-only", TrustAuthorized: true, Prompt: "first", TaskCanary: "canary-contaminated"})
	if err != nil {
		t.Fatalf("Launch() error = %v", err)
	}
	_ = waitForTerminal(t, adapter, running.ProviderHandle)
	adapter.Environment = []string{"CURSOR_ADAPTER_HELPER=1", "CURSOR_ADAPTER_HELPER_MODE=mismatched-session"}
	followUp, err := adapter.Send(t.Context(), SendRequest{HandleID: running.ProviderHandle, ActionID: "action-contaminated", Prompt: "resume", CoreAuthorized: true, TaskCanary: "canary-contaminated"})
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	contaminated := waitForTerminal(t, adapter, followUp.ProviderHandle)
	if contaminated.Status != "contaminated" || contaminated.Reason != "session_identity_mismatch" || contaminated.ProviderSessionID != "cursor-foreign-session" || contaminated.Identity["session"] != "cursor-foreign-session" {
		t.Fatalf("contaminated result = %#v", contaminated)
	}
	session, err := adapter.findSession(running.ProviderHandle)
	if err != nil {
		t.Fatal(err)
	}
	session.mu.Lock()
	authoritySession := session.providerSessionID
	contaminatedLatch := session.contaminated
	session.mu.Unlock()
	if authoritySession != "cursor-session-1" || !contaminatedLatch {
		t.Fatalf("mismatched provider session changed local authority or latch: authority=%q contaminated=%t", authoritySession, contaminatedLatch)
	}
	if _, err := adapter.Send(t.Context(), SendRequest{HandleID: running.ProviderHandle, ActionID: "action-after-contamination", Prompt: "must be rejected", CoreAuthorized: true, TaskCanary: "canary-contaminated"}); err == nil {
		t.Fatal("Send() was accepted after session contamination")
	}
}

func TestTranscriptWriteFailureDoesNotEmitTranscriptReference(t *testing.T) {
	workspace := t.TempDir()
	adapter := New(os.Args[0])
	adapter.Environment = []string{"CURSOR_ADAPTER_HELPER=1"}
	adapter.StartupTimeout = 2 * time.Second
	adapter.TranscriptWriter = func(string, []byte) error { return errors.New("transcript write unavailable") }
	request := LaunchRequest{Workspace: workspace, Model: "fixture-model", Mode: "plan", Permission: "read-only", TrustAuthorized: true, Prompt: "return the smoke token", TaskCanary: "canary-transcript-failure", TranscriptPath: filepath.Join(workspace, "cursor-transcript.jsonl")}
	running, err := adapter.Launch(t.Context(), request)
	if err != nil {
		t.Fatalf("Launch() error = %v", err)
	}
	terminal := waitForTerminal(t, adapter, running.ProviderHandle)
	if terminal.Status != "completed" || terminal.TranscriptWritten {
		t.Fatalf("transcript failure result = %#v", terminal)
	}
	for _, ref := range terminal.RawEventRefs {
		if strings.HasPrefix(ref, "transcript:") {
			t.Fatalf("transcript failure emitted a false reference: %#v", terminal.RawEventRefs)
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
	t.Fatal("timed out waiting for Cursor terminal result")
	return ExecutionResult{}
}

func waitForTerminalWithExpectedReason(t *testing.T, adapter *Adapter, handle, reason string) ExecutionResult {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		result, err := adapter.Observe(t.Context(), ObserveRequest{HandleID: handle})
		if err != nil {
			t.Fatalf("Observe() error = %v", err)
		}
		if result.Terminal {
			if result.Reason != reason {
				t.Fatalf("terminal reason = %q, want %q; result=%#v", result.Reason, reason, result)
			}
			return result
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("timed out waiting for Cursor terminal result")
	return ExecutionResult{}
}

func TestMain(m *testing.M) {
	if os.Getenv("CURSOR_ADAPTER_HELPER") == "1" {
		if os.Getenv("CURSOR_ADAPTER_HELPER_MODE") == "no-session-sleep" {
			time.Sleep(30 * time.Second)
			os.Exit(0)
		}
		if os.Getenv("CURSOR_ADAPTER_HELPER_MODE") == "mismatched-session" && contains(os.Args[1:], "--resume") {
			fmt.Println(`{"type":"system","subtype":"init","session_id":"cursor-foreign-session"}`)
			fmt.Println(`{"type":"result","subtype":"success","is_error":false,"result":"FOREIGN_SESSION"}`)
			os.Exit(0)
		}
		if os.Getenv("CURSOR_ADAPTER_HELPER_MODE") == "no-session-on-resume" && contains(os.Args[1:], "--resume") {
			fmt.Println(`{"type":"result","subtype":"success","is_error":false,"result":"NO_SESSION"}`)
			os.Exit(0)
		}
		if os.Getenv("CURSOR_ADAPTER_HELPER_MODE") == "provider-error" {
			fmt.Println(`{"type":"system","subtype":"init","session_id":"cursor-session-1"}`)
			fmt.Println(`{"type":"result","subtype":"error","is_error":true,"error":"PROVIDER_ERROR","result":"SHOULD_NOT_DELIVER"}`)
			os.Exit(0)
		}
		fmt.Println(`{"type":"system","subtype":"init","session_id":"cursor-session-1"}`)
		if os.Getenv("CURSOR_ADAPTER_HELPER_MODE") == "sleep" {
			time.Sleep(30 * time.Second)
			os.Exit(0)
		}
		if os.Getenv("CURSOR_ADAPTER_HELPER_MODE") == "tool-only" {
			fmt.Println(`{"type":"tool_call","name":"CreatePlan","input":{"text":"report"}}`)
			fmt.Println(`{"type":"turn_ended","status":"success"}`)
			os.Exit(0)
		}
		fmt.Println(`{"type":"result","subtype":"success","is_error":false,"result":"ADAPTER_SMOKE_OK"}`)
		os.Exit(0)
	}
	os.Exit(m.Run())
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
