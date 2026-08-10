package codexcli

import (
	"os"
	"strings"
	"testing"
	"time"

	"agent-workflow-toolkit/internal/adapters/cli"
)

func TestWave4SendRejectsConcurrentFollowUp(t *testing.T) {
	workspace := t.TempDir()
	adapter := New(os.Args[0])
	adapter.Environment = []string{"CODEX_ADAPTER_HELPER=1"}
	adapter.StartupTimeout = 2 * time.Second
	running, err := adapter.Launch(t.Context(), LaunchRequest{Workspace: workspace, Model: "fixture-model", Permission: "read-only", TrustAuthorized: true, Prompt: "first", TaskCanary: "wave4-concurrent-send"})
	if err != nil {
		t.Fatalf("Launch() error = %v", err)
	}
	_ = waitForCodexTerminal(t, adapter, running.ProviderHandle)
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
		providerHandle:    "codex-handle",
		providerSessionID: "codex-authoritative",
		request:           LaunchRequest{Workspace: t.TempDir(), Model: "fixture-model", TaskCanary: "wave4-terminal"},
	}
	missing := missingIdentity.terminalResult(&turn{
		expectedSession: "codex-authoritative",
		summary:         cli.StreamSummary{Terminal: true},
		result:          cli.Result{Started: true},
	})
	if missing.Status != "failed" || missing.Reason != "thread_identity_missing" || missing.ProviderSessionID != "codex-authoritative" {
		t.Fatalf("missing identity result = %#v", missing)
	}

	providerError := &session{
		providerHandle:    "codex-handle",
		providerSessionID: "codex-authoritative",
		request:           LaunchRequest{Workspace: t.TempDir(), Model: "fixture-model", TaskCanary: "wave4-terminal"},
	}
	errorResult := providerError.terminalResult(&turn{
		expectedSession: "codex-authoritative",
		summary: cli.StreamSummary{
			SessionID:      "codex-authoritative",
			Terminal:       true,
			TerminalStatus: "error",
		},
		result: cli.Result{Started: true},
	})
	if errorResult.Status != "failed" || errorResult.Reason != "provider_terminal_error" || errorResult.FinalText != "" {
		t.Fatalf("provider error result = %#v", errorResult)
	}
}
