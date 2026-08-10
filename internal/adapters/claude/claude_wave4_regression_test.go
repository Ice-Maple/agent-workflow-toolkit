package claude

import (
	"strings"
	"testing"

	"agent-workflow-toolkit/internal/adapters/cli"
)

func TestWave4SendRejectsConcurrentFollowUp(t *testing.T) {
	workspace := t.TempDir()
	adapter := helperAdapter()
	running, err := adapter.Launch(t.Context(), LaunchRequest{Workspace: workspace, Model: "fixture-model", Permission: "read-only", TrustAuthorized: true, Prompt: "first", TaskCanary: "wave4-concurrent-send"})
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
	if _, err := adapter.Send(t.Context(), SendRequest{HandleID: running.ProviderHandle, ActionID: "wave4-concurrent-send", Prompt: "must wait", CoreAuthorized: true, TaskCanary: "wave4-concurrent-send"}); err == nil || !strings.Contains(err.Error(), "already has a follow-up in flight") {
		t.Fatalf("concurrent Send() error = %v", err)
	}
	session.mu.Lock()
	session.sendInFlight = false
	session.mu.Unlock()
	if err := adapter.Cleanup(CleanupRequest{HandleID: running.ProviderHandle}); err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
}

func TestWave4TerminalClassificationRequiresIdentityAndRejectsProviderError(t *testing.T) {
	missingIdentity := &session{
		providerHandle:    "claude-handle",
		providerSessionID: "claude-authoritative",
		request:           LaunchRequest{Workspace: t.TempDir(), Model: "fixture-model", TaskCanary: "wave4-terminal"},
	}
	missing := missingIdentity.terminalResult(&turn{
		expectedSession: "claude-authoritative",
		summary:         cli.StreamSummary{Terminal: true},
		result:          cli.Result{Started: true},
	})
	if missing.Status != "failed" || missing.Reason != "session_identity_missing" || missing.ProviderSessionID != "claude-authoritative" {
		t.Fatalf("missing identity result = %#v", missing)
	}

	providerError := &session{
		providerHandle:    "claude-handle",
		providerSessionID: "claude-authoritative",
		request:           LaunchRequest{Workspace: t.TempDir(), Model: "fixture-model", TaskCanary: "wave4-terminal"},
	}
	errorResult := providerError.terminalResult(&turn{
		expectedSession: "claude-authoritative",
		summary: cli.StreamSummary{
			SessionID:      "claude-authoritative",
			Terminal:       true,
			TerminalStatus: "error",
		},
		result: cli.Result{Started: true},
	})
	if errorResult.Status != "failed" || errorResult.Reason != "provider_terminal_error" || errorResult.FinalText != "" {
		t.Fatalf("provider error result = %#v", errorResult)
	}
}
