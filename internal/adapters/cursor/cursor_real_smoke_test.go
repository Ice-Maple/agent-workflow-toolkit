package cursor

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestRealCursorAdapterSmoke is opt-in because it performs a real Cursor
// Agent request. It intentionally uses plan mode, explicit trust and a
// no-tool prompt; it never enables force/yolo, sandbox enabled mode or MCP
// auto-approval.
func TestRealCursorAdapterSmoke(t *testing.T) {
	if os.Getenv("CURSOR_ADAPTER_REAL_SMOKE") != "1" {
		t.Skip("set CURSOR_ADAPTER_REAL_SMOKE=1 to run a real Cursor Adapter call")
	}
	workspace := os.Getenv("CURSOR_ADAPTER_SMOKE_WORKSPACE")
	if workspace == "" {
		t.Fatal("CURSOR_ADAPTER_SMOKE_WORKSPACE is required")
	}
	model := os.Getenv("CURSOR_ADAPTER_SMOKE_MODEL")
	if model == "" {
		model = "cursor-grok-4.5-high"
	}
	executable := os.Getenv("CURSOR_ADAPTER_EXECUTABLE")
	if executable == "" {
		executable = DefaultExecutable
	}
	adapter := New(executable)
	adapter.StartupTimeout = 45 * time.Second
	probeContext, cancelProbe := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancelProbe()
	preflight, err := adapter.Preflight(probeContext, PreflightRequest{Workspace: workspace, Model: model, Mode: "plan", Permission: "read-only", TrustAuthorized: true})
	if err != nil {
		t.Fatalf("Cursor preflight failed to start: %v", err)
	}
	if preflight.Status != "passed" {
		t.Fatalf("Cursor preflight was not admitted: %#v", preflight)
	}
	canonicalWorkspace, err := filepath.Abs(workspace)
	if err != nil {
		t.Fatal(err)
	}
	requestContext, cancelRequest := context.WithTimeout(t.Context(), 4*time.Minute)
	defer cancelRequest()
	running, err := adapter.Launch(requestContext, LaunchRequest{Workspace: canonicalWorkspace, Model: model, Mode: "plan", Permission: "read-only", TrustAuthorized: true, Prompt: "Do not read files or call tools. Return exactly ADAPTER_SMOKE_OK and nothing else.", TaskCanary: "real-cursor-smoke-canary"})
	if err != nil {
		t.Fatalf("Cursor launch failed to start: %v", err)
	}
	deadline := time.Now().Add(4 * time.Minute)
	var terminal ExecutionResult
	for time.Now().Before(deadline) {
		terminal, err = adapter.Observe(requestContext, ObserveRequest{HandleID: running.ProviderHandle})
		if err != nil {
			t.Fatalf("Cursor observe failed: %v", err)
		}
		if terminal.Terminal {
			break
		}
		time.Sleep(250 * time.Millisecond)
	}
	t.Logf("status=%s reason=%s source=%s session_present=%t terminal=%t exit_code=%d final_bytes=%d", terminal.Status, terminal.Reason, terminal.FinalSource, terminal.ProviderSessionID != "", terminal.Terminal, terminal.ExitCode, len(terminal.FinalText))
	if terminal.Status != "completed" || terminal.FinalText != "ADAPTER_SMOKE_OK" {
		t.Fatalf("Cursor delivery was not completed: %#v", terminal)
	}
}
