package bridge

import (
	"os"
	"testing"
	"time"
)

// TestRealCursorFrameworkBridgeE2E is opt-in because it performs a real
// read-only Cursor Agent request through the public core/bridge process path.
// It proves that owner readiness is independent of Cursor session startup and
// then completes launch -> observe -> completion -> technical release.
func TestRealCursorFrameworkBridgeE2E(t *testing.T) {
	if os.Getenv("CURSOR_FRAMEWORK_REAL_SMOKE") != "1" {
		t.Skip("set CURSOR_FRAMEWORK_REAL_SMOKE=1 to run the real Cursor framework bridge E2E")
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
		executable = "agent"
	}
	runPublicBridgeRoleFirstHandoffWithOptions(t, ProviderCursor, buildBridgeExecutable(t), buildCoreExecutable(t), roleFirstE2EOptions{
		workspace:                        workspace,
		providerExecutable:               executable,
		providerModel:                    model,
		stepTask:                         "Do not modify files. Return exactly CURSOR_FRAMEWORK_OK and nothing else.",
		expectedFinalText:                "CURSOR_FRAMEWORK_OK",
		completionDeadline:               4 * time.Minute,
		readyDeadline:                    10 * time.Second,
		runAdapterPreflight:              true,
		expectReadyBeforeProviderSession: true,
	})
}
