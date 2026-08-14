package bridge

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"agent-workflow-toolkit/internal/orchestration"
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

// TestRealCursorProjectReviewerFrozenInputE2E runs the actual downstream
// development reviewer Role Prompt against a host-frozen Git status/diff. The
// Provider may read the artifact through --add-dir but cannot invoke Shell.
func TestRealCursorProjectReviewerFrozenInputE2E(t *testing.T) {
	if os.Getenv("CURSOR_PROJECT_ROLE_REAL_REVIEW") != "1" {
		t.Skip("set CURSOR_PROJECT_ROLE_REAL_REVIEW=1 to run the real project Role frozen-input review")
	}
	workspace := os.Getenv("CURSOR_ADAPTER_SMOKE_WORKSPACE")
	rolePromptPath := os.Getenv("CURSOR_PROJECT_ROLE_PROMPT")
	artifactPath := os.Getenv("CURSOR_PROJECT_ROLE_FROZEN_INPUT")
	if workspace == "" || rolePromptPath == "" || artifactPath == "" {
		t.Fatal("CURSOR_ADAPTER_SMOKE_WORKSPACE, CURSOR_PROJECT_ROLE_PROMPT and CURSOR_PROJECT_ROLE_FROZEN_INPUT are required")
	}
	rolePrompt, err := os.ReadFile(rolePromptPath)
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := os.ReadFile(artifactPath)
	if err != nil {
		t.Fatal(err)
	}
	artifactPath, err = filepath.Abs(filepath.Clean(artifactPath))
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(artifact)
	frozenInput := orchestration.FrozenInputRef{Name: "repository-review", Path: artifactPath, Root: filepath.Dir(artifactPath), Format: "markdown", Bytes: int64(len(artifact)), SHA256: hex.EncodeToString(sum[:])}
	if _, err := orchestration.VerifyFrozenInputs([]orchestration.FrozenInputRef{frozenInput}); err != nil {
		t.Fatalf("frozen reviewer input: %v", err)
	}
	head := frozenGitHead(string(artifact))
	if head == "" {
		t.Fatal("frozen reviewer input has no HEAD identity")
	}
	model := os.Getenv("CURSOR_ADAPTER_SMOKE_MODEL")
	if model == "" {
		model = "cursor-grok-4.5-high"
	}
	executable := os.Getenv("CURSOR_ADAPTER_EXECUTABLE")
	if executable == "" {
		executable = "agent"
	}
	result := runPublicBridgeRoleFirstHandoffWithOptions(t, ProviderCursor, buildBridgeExecutable(t), buildCoreExecutable(t), roleFirstE2EOptions{
		workspace:                        workspace,
		providerExecutable:               executable,
		providerModel:                    model,
		providerMode:                     "ask",
		role:                             "doc-ai-toolkit-development/reviewer",
		rolePrompt:                       string(rolePrompt),
		stepTask:                         "The framework-frozen Git status/diff is the authoritative current-delta input. Open this exact file with read-only file tools: " + artifactPath + ". Do not attempt git or Shell. Report FROZEN_HEAD=<value copied from its Repository identity>, then independently review that frozen diff and relevant workspace files for the Cursor exact Prompt transport and project Role frozen-input changes. End with exactly one verdict line: PASS, PASS WITH P2, or BLOCKED.",
		frozenInputs:                     []orchestration.FrozenInputRef{frozenInput},
		completionDeadline:               8 * time.Minute,
		readyDeadline:                    10 * time.Second,
		runAdapterPreflight:              true,
		expectReadyBeforeProviderSession: true,
		disableAfterRelease:              true,
	})
	t.Logf("normalized reviewer final delivery:\n%s", result.FinalText)
	if !strings.Contains(strings.ToLower(result.FinalText), strings.ToLower(head)) {
		t.Fatalf("reviewer did not prove frozen artifact reading; expected HEAD %s final_bytes=%d verdict=%q", head, len(result.FinalText), reviewerVerdict(result.FinalText))
	}
	verdict := reviewerVerdict(result.FinalText)
	if verdict != "PASS" && verdict != "PASS WITH P2" {
		t.Fatalf("project reviewer verdict=%q final_bytes=%d", verdict, len(result.FinalText))
	}
	if !result.TechnicalReleaseDone || !result.Disabled || !strings.EqualFold(result.DispatchPromptHash, result.ProviderPromptHash) {
		t.Fatalf("project reviewer lifecycle result=%#v", result)
	}
	t.Logf("project Role reviewer PASS verdict=%s final_bytes=%d prompt_hash=%s run=%s attempt=%s", verdict, len(result.FinalText), result.ProviderPromptHash, result.RunID, result.AttemptID)
}

func frozenGitHead(content string) string {
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "HEAD: `") || !strings.HasSuffix(line, "`") {
			continue
		}
		value := strings.TrimSuffix(strings.TrimPrefix(line, "HEAD: `"), "`")
		if len(value) == 40 {
			return value
		}
	}
	return ""
}

func reviewerVerdict(content string) string {
	verdict := ""
	for _, line := range strings.Split(content, "\n") {
		line = strings.ToUpper(strings.Trim(strings.TrimSpace(line), "*`# "))
		switch {
		case strings.HasSuffix(line, "PASS WITH P2"):
			verdict = "PASS WITH P2"
		case strings.HasSuffix(line, "BLOCKED"):
			verdict = "BLOCKED"
		case strings.HasSuffix(line, "PASS"):
			verdict = "PASS"
		}
	}
	return verdict
}
