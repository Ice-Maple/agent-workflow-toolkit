package claude

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestRealClaudeAdapterSmoke is opt-in because it performs a real provider
// call. It uses the known Wave 0 model and disables tools so the smoke proves
// the Go process/session/delivery boundary without repository side effects.
func TestRealClaudeAdapterSmoke(t *testing.T) {
	if os.Getenv("CLAUDE_ADAPTER_REAL_SMOKE") != "1" {
		t.Skip("set CLAUDE_ADAPTER_REAL_SMOKE=1 to run a real Claude Code call")
	}
	workspace := os.Getenv("CLAUDE_ADAPTER_SMOKE_WORKSPACE")
	if workspace == "" {
		t.Fatal("CLAUDE_ADAPTER_SMOKE_WORKSPACE is required for the real smoke")
	}
	model := os.Getenv("CLAUDE_ADAPTER_SMOKE_MODEL")
	if model == "" {
		model = "deepseek-v4-pro"
	}
	executable := os.Getenv("CLAUDE_ADAPTER_EXECUTABLE")
	if executable == "" {
		executable = DefaultExecutable
	}

	adapter := New(executable)
	adapter.StartupTimeout = 60 * time.Second
	preflightContext, cancelPreflight := context.WithTimeout(t.Context(), 3*time.Minute)
	defer cancelPreflight()
	preflight, err := adapter.Preflight(preflightContext, PreflightRequest{
		Workspace: workspace, Model: model, Permission: "read-only", TrustAuthorized: true,
	})
	if err != nil {
		t.Fatalf("Claude preflight failed to start: %v", err)
	}
	if preflight.Status != "passed" {
		t.Fatalf("Claude preflight was not admitted: %#v", preflight)
	}

	requestContext, cancelRequest := context.WithTimeout(t.Context(), 10*time.Minute)
	defer cancelRequest()
	transcriptPath := filepath.Join(t.TempDir(), "claude-smoke.jsonl")
	running, err := adapter.Launch(requestContext, LaunchRequest{
		Workspace: workspace, Model: model, Permission: "read-only", TrustAuthorized: true,
		DisableTools: true, Prompt: "Do not call tools, read files, or edit anything. Return exactly ADAPTER_SMOKE_OK and nothing else.",
		TaskCanary: "real-claude-smoke-canary", TranscriptPath: transcriptPath,
	})
	if err != nil {
		t.Fatalf("Claude launch failed to start: %v", err)
	}
	defer cleanupRealSmoke(t, adapter, running.ProviderHandle)
	terminal := waitForRealTerminal(t, adapter, requestContext, running.ProviderHandle)
	t.Logf("version=%s launch_status=%s launch_session_present=%t terminal_status=%s terminal_reason=%s final_source=%s terminal=%t exit_code=%d final_bytes=%d transcript=%t", preflight.Version, running.Status, running.ProviderSessionID != "", terminal.Status, terminal.Reason, terminal.FinalSource, terminal.Terminal, terminal.ExitCode, len(terminal.FinalText), terminal.TranscriptWritten)
	if terminal.Status != "completed" || terminal.FinalText != "ADAPTER_SMOKE_OK" || !terminal.Terminal || !terminal.TranscriptWritten {
		t.Fatalf("Claude delivery was not completed: status=%s reason=%s source=%s session_present=%t terminal=%t exit_code=%d final_bytes=%d transcript=%t", terminal.Status, terminal.Reason, terminal.FinalSource, terminal.ProviderSessionID != "", terminal.Terminal, terminal.ExitCode, len(terminal.FinalText), terminal.TranscriptWritten)
	}
}

func waitForRealTerminal(t *testing.T, adapter *Adapter, ctx context.Context, handle string) ExecutionResult {
	t.Helper()
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	var latest ExecutionResult
	for {
		observed, err := adapter.Observe(ctx, ObserveRequest{HandleID: handle})
		if err != nil {
			t.Fatalf("Claude observe failed: %v", err)
		}
		latest = observed
		if latest.Terminal {
			return latest
		}
		select {
		case <-ctx.Done():
			t.Fatalf("Claude observe timed out: status=%s reason=%s session_present=%t terminal=%t", latest.Status, latest.Reason, latest.ProviderSessionID != "", latest.Terminal)
		case <-ticker.C:
		}
	}
}

func cleanupRealSmoke(t *testing.T, adapter *Adapter, handle string) {
	t.Helper()
	probeContext, cancelProbe := context.WithTimeout(context.Background(), time.Second)
	observed, observeErr := adapter.Observe(probeContext, ObserveRequest{HandleID: handle})
	cancelProbe()
	if observeErr != nil || !observed.Terminal {
		cancelContext, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		if _, err := adapter.Cancel(cancelContext, CancelRequest{HandleID: handle}); err != nil {
			t.Logf("Claude smoke cancellation: %v", err)
		}
		cancel()
	}
	if err := adapter.Cleanup(CleanupRequest{HandleID: handle}); err != nil {
		t.Logf("Claude smoke cleanup: %v", err)
	}
}
