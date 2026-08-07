package codexcli

import (
	"os"
	"path/filepath"
	"testing"
)

// TestRealCodexDeliverySmoke is intentionally opt-in because it performs a
// real provider call. It is the regression test for the observed exit-0 /
// empty-stdout condition and must run with the same registered model binding
// used by the target environment.
func TestRealCodexDeliverySmoke(t *testing.T) {
	if os.Getenv("CODEX_ADAPTER_REAL_SMOKE") != "1" {
		t.Skip("set CODEX_ADAPTER_REAL_SMOKE=1 to run a real Codex CLI call")
	}
	workspace := os.Getenv("CODEX_ADAPTER_SMOKE_WORKSPACE")
	if workspace == "" {
		t.Fatal("CODEX_ADAPTER_SMOKE_WORKSPACE is required for the real smoke")
	}
	model := os.Getenv("CODEX_ADAPTER_SMOKE_MODEL")
	if model == "" {
		model = "gpt-5.6-luna"
	}
	executable := os.Getenv("CODEX_ADAPTER_EXECUTABLE")
	if executable == "" {
		executable = "codex"
	}
	lastMessagePath := filepath.Join(t.TempDir(), "last-message.txt")
	result, err := (Invocation{
		Executable:            executable,
		Workspace:             workspace,
		Model:                 model,
		OutputLastMessagePath: lastMessagePath,
		Ephemeral:             true,
	}).Run(t.Context(), "Adapter smoke test. Do not call tools, read files, or edit anything. Return exactly the token ADAPTER_SMOKE_OK and nothing else.")
	if err != nil {
		t.Fatalf("real Codex Adapter call failed to start: %v", err)
	}
	t.Logf("status=%s reason=%s source=%s exit_code=%d final_bytes=%d stdout_bytes=%d", result.Status, result.Reason, result.Source, result.ExitCode, len(result.FinalText), len(result.Stdout))
	if result.Status != StatusCompleted || result.FinalText != "ADAPTER_SMOKE_OK" {
		t.Fatalf("Codex delivery was not completed: %#v", result)
	}
}
