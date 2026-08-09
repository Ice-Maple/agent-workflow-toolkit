package cursor

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestRealCursorAdapterReview is opt-in because it performs a real external
// review. The review is constrained to Cursor's read-only plan mode and the
// final report is staged only after the Adapter has verified terminal delivery.
func TestRealCursorAdapterReview(t *testing.T) {
	if os.Getenv("CURSOR_ADAPTER_REAL_REVIEW") != "1" {
		t.Skip("set CURSOR_ADAPTER_REAL_REVIEW=1 to run a real Cursor review")
	}
	config := cursorReviewConfig{
		Title:              "# Wave 3 Cursor Adapter review",
		Prompt:             cursorWave3ReviewPrompt,
		DeliveryPrompt:     cursorWave3ReviewDeliveryPrompt,
		Canary:             "real-cursor-review-canary",
		ActionID:           "review-delivery-follow-up",
		RequiredSections:   []string{"# Wave 3 Cursor Adapter review", "## Coverage ledger", "## Findings", "## Verdict"},
		ReportRelativePath: filepath.Join("docs", "reviews", "2026-08-09-wave3-cursor-adapter-review.md"),
	}
	run := runRealCursorReview(t, config)
	stageCursorReviewReport(t, run, config)
}

// TestRealCursorAdapterTargetedRecheck is opt-in and is intended to close a
// previously recorded review. It does not repeat the broad review matrix.
func TestRealCursorAdapterTargetedRecheck(t *testing.T) {
	if os.Getenv("CURSOR_ADAPTER_REAL_RECHECK") != "1" {
		t.Skip("set CURSOR_ADAPTER_REAL_RECHECK=1 to run a targeted Cursor re-check")
	}
	config := cursorReviewConfig{
		Title:              "# Wave 3 Cursor Adapter targeted re-check",
		Prompt:             cursorWave3TargetedRecheckPrompt,
		DeliveryPrompt:     cursorWave3TargetedRecheckDeliveryPrompt,
		Canary:             "real-cursor-recheck-canary",
		ActionID:           "targeted-recheck-delivery-follow-up",
		RequiredSections:   []string{"# Wave 3 Cursor Adapter targeted re-check", "## Evidence checklist", "## Finding status", "## Invocation process optimization", "## Gate decision"},
		ReportRelativePath: filepath.Join("docs", "reviews", "2026-08-09-wave3-cursor-adapter-targeted-recheck.md"),
	}
	run := runRealCursorReview(t, config)
	stageCursorReviewReport(t, run, config)
}

type cursorReviewConfig struct {
	Title              string
	Prompt             string
	DeliveryPrompt     string
	Canary             string
	ActionID           string
	RequiredSections   []string
	ReportRelativePath string
}

type cursorReviewRun struct {
	Adapter      *Adapter
	Workspace    string
	Model        string
	Initial      ExecutionResult
	Terminal     ExecutionResult
	FollowUpUsed bool
}

func runRealCursorReview(t *testing.T, config cursorReviewConfig) cursorReviewRun {
	t.Helper()
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
	canonicalWorkspace, err := filepath.Abs(workspace)
	if err != nil {
		t.Fatal(err)
	}

	adapter := New(executable)
	adapter.StartupTimeout = 60 * time.Second
	probeContext, cancelProbe := context.WithTimeout(t.Context(), 3*time.Minute)
	defer cancelProbe()
	preflight, err := adapter.Preflight(probeContext, PreflightRequest{
		Workspace:       canonicalWorkspace,
		Model:           model,
		Mode:            "plan",
		Permission:      "read-only",
		TrustAuthorized: true,
	})
	if err != nil {
		t.Fatalf("Cursor review preflight failed to start: %v", err)
	}
	if preflight.Status != "passed" {
		t.Fatalf("Cursor review preflight was not admitted: %#v", preflight)
	}

	requestContext, cancelRequest := context.WithTimeout(t.Context(), 10*time.Minute)
	defer cancelRequest()
	transcriptPath := filepath.Join(t.TempDir(), "cursor-review.jsonl")
	running, err := adapter.Launch(requestContext, LaunchRequest{
		Workspace:       canonicalWorkspace,
		Model:           model,
		Mode:            "plan",
		Permission:      "read-only",
		TrustAuthorized: true,
		Prompt:          config.Prompt,
		TaskCanary:      config.Canary,
		TranscriptPath:  transcriptPath,
	})
	if err != nil {
		t.Fatalf("Cursor review launch failed to start: %v", err)
	}
	defer cleanupCursorReview(t, adapter, running.ProviderHandle)

	initial := observeCursorReview(t, adapter, requestContext, running.ProviderHandle)
	terminal := initial
	followUpUsed := false
	if terminal.Reason == "delivery_missing" || !hasReviewSections(terminal.FinalText, config.RequiredSections) {
		followUpUsed = true
		followUp, sendErr := adapter.Send(requestContext, SendRequest{
			HandleID:       running.ProviderHandle,
			ActionID:       config.ActionID,
			Prompt:         config.DeliveryPrompt,
			CoreAuthorized: true,
			TaskCanary:     config.Canary,
		})
		if sendErr != nil {
			t.Fatalf("Cursor review delivery follow-up failed to start: %v", sendErr)
		}
		terminal = observeCursorReview(t, adapter, requestContext, followUp.ProviderHandle)
	}
	t.Logf("initial_status=%s initial_reason=%s initial_final_bytes=%d final_status=%s final_reason=%s final_source=%s session_present=%t terminal=%t exit_code=%d final_bytes=%d follow_up=%t", initial.Status, initial.Reason, len(initial.FinalText), terminal.Status, terminal.Reason, terminal.FinalSource, terminal.ProviderSessionID != "", terminal.Terminal, terminal.ExitCode, len(terminal.FinalText), followUpUsed)
	if terminal.Status != "completed" || !terminal.Terminal || strings.TrimSpace(terminal.FinalText) == "" {
		t.Fatalf("Cursor review delivery was not completed: %#v", terminal)
	}
	return cursorReviewRun{Adapter: adapter, Workspace: canonicalWorkspace, Model: model, Initial: initial, Terminal: terminal, FollowUpUsed: followUpUsed}
}

func stageCursorReviewReport(t *testing.T, run cursorReviewRun, config cursorReviewConfig) {
	t.Helper()
	body := strings.TrimSpace(run.Terminal.FinalText)
	body = strings.TrimPrefix(body, "\ufeff")
	body = strings.TrimSpace(strings.TrimPrefix(body, config.Title))
	report := fmt.Sprintf("%s\n\n- Reviewer/model: Cursor Agent / %s\n- Adapter: Cursor (`cursor`)\n- Execution: read-only `plan` mode with explicit workspace trust\n- Initial delivery: status `%s`, reason `%s`, final bytes `%d`\n- Final delivery: status `%s`, reason `%s`, final source `%s`, terminal `%t`, exit code `%d`, final bytes `%d`\n- Delivery recovery: same-session core-authorized follow-up used: `%t`\n- Prompt: sanitized; private prompt text is intentionally not stored in this artifact\n\n%s\n", config.Title, run.Model, run.Initial.Status, run.Initial.Reason, len(run.Initial.FinalText), run.Terminal.Status, run.Terminal.Reason, run.Terminal.FinalSource, run.Terminal.Terminal, run.Terminal.ExitCode, len(run.Terminal.FinalText), run.FollowUpUsed, strings.TrimSpace(body))
	reportPath := filepath.Join(run.Workspace, config.ReportRelativePath)
	artifact, err := run.Adapter.StageArtifact(run.Workspace, reportPath, "markdown", report, config.RequiredSections)
	if err != nil {
		t.Fatalf("Cursor review report failed artifact validation: %v", err)
	}
	t.Logf("review artifact path=%s bytes=%d sha256=%s validated=%t", artifact.Path, artifact.Bytes, artifact.SHA256, artifact.Validated)
}

func hasReviewSections(content string, requiredSections []string) bool {
	for _, section := range requiredSections {
		if !strings.Contains(content, section) {
			return false
		}
	}
	return true
}

func observeCursorReview(t *testing.T, adapter *Adapter, ctx context.Context, handle string) ExecutionResult {
	t.Helper()
	var latest ExecutionResult
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		observed, err := adapter.Observe(ctx, ObserveRequest{HandleID: handle})
		if err != nil {
			cancelCursorReview(t, adapter, handle)
			t.Fatalf("Cursor review observe failed: %v", err)
		}
		latest = observed
		if latest.Terminal {
			return latest
		}
		select {
		case <-ctx.Done():
			cancelCursorReview(t, adapter, handle)
			t.Fatalf("Cursor review observe timed out: %#v", latest)
		case <-ticker.C:
		}
	}
}

func cancelCursorReview(t *testing.T, adapter *Adapter, handle string) {
	t.Helper()
	cancelContext, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := adapter.Cancel(cancelContext, CancelRequest{HandleID: handle}); err != nil {
		t.Logf("Cursor review cancellation after observation failure: %v", err)
	}
}

func cleanupCursorReview(t *testing.T, adapter *Adapter, handle string) {
	t.Helper()
	probeContext, cancel := context.WithTimeout(context.Background(), time.Second)
	observed, observeErr := adapter.Observe(probeContext, ObserveRequest{HandleID: handle})
	cancel()
	if observeErr != nil || !observed.Terminal {
		cancelCursorReview(t, adapter, handle)
	}
	if err := adapter.Cleanup(CleanupRequest{HandleID: handle}); err != nil {
		t.Logf("Cursor review cleanup: %v", err)
	}
}

const cursorWave3ReviewPrompt = `Act as the independent reviewer for the current Wave 3 Cursor Agent Adapter implementation in this repository.

Read the actual current files and diff before judging them. At minimum read AGENTS.md, docs/reviews/2026-08-09-wave2-closure-wave3-handoff.md, docs/plans/2026-08-07-multi-agent-orchestration-mvp-waves.md, docs/reviews/2026-08-09-wave3-cursor-adapter-progress.md, internal/adapters/cursor/, internal/adapters/cli/, internal/adapters/artifact/, internal/gateway/, internal/orchestration/, and the related tests/evidence. Use only read-only inspection tools. Do not edit, create, delete, stage, commit, or reset any file. Do not start another Agent CLI or contact another agent.

Review this implementation slice, not a future Claude Code or Codex CLI adapter. Check the actual code, tests, and current worktree diff for:
1. Windows argv-only process launch, hidden-window behavior, stdout/stderr and JSONL stream handling, timeout, process-tree cancellation, and orphan-process risk.
2. workspace trust, read-only plan/ask enforcement, sandbox capability reporting, MCP allowlist/isolation behavior, and whether unsupported write/permission paths fail closed.
3. session/provider/workspace identity, task canary binding, contaminated-session handling, resume/send authorization, action reuse, cleanup, and race/concurrency risks.
4. terminal versus final delivery semantics, tool-only/CreatePlan output, exit 0 with empty final text, delivery_missing classification, and same-session delivery-only recovery.
5. transcript and artifact staging boundaries, canonical workspace checks, required-section validation, SHA-256/atomic replacement, and Gateway HostActionResult/CompletionDecision reason propagation.
6. whether the current tests and real smoke provide sufficient evidence for this Cursor slice and whether the documentation claims match the implementation.

Produce the complete final review report in your final assistant message, not only a plan, tool payload, or turn-completed status. Use this exact report structure:

# Wave 3 Cursor Adapter review
- Reviewer/model:
- Scope and worktree basis:
- Verdict: PASS | PASS WITH CONDITIONS | FAIL

## Executive summary

## Coverage ledger
Use a table with one row for each review area above and status PASS, FAIL, DEFERRED, or NOT APPLICABLE. Cite exact current file paths and line numbers.

## Findings
For every finding include an ID, severity P0/P1/P2/P3, status NEW / ALREADY ADDRESSED / OUT OF SCOPE, exact file and line, evidence, impact or reproduction, minimal fix, and verification. Do not invent findings merely to be cautious; distinguish current Wave 3 blockers from future hardening.

## Already addressed / not new

## Deferred / out of scope

## Verdict
State clearly whether this Cursor Adapter slice passes its independent review/checkpoint. If conditional or failing, list only the minimum required actions before the checkpoint.

Do not include credentials, authentication headers, environment-variable contents, private prompt text, or raw transcripts in the report. Keep the report complete and evidence-backed.`

const cursorWave3ReviewDeliveryPrompt = `Return the complete final Wave 3 Cursor Adapter review report now. Do not reread files, call tools, edit files, or start another agent. Preserve the required headings, coverage ledger, exact file/line evidence, all findings with severity and status, and the final PASS/PASS WITH CONDITIONS/FAIL verdict from the review you already performed. Do not return a summary, plan, or tool payload; return the full Markdown report as assistant text. Do not include credentials, environment-variable contents, private prompt text, or raw transcripts.`

const cursorWave3TargetedRecheckPrompt = `Act as an independent reviewer performing a targeted re-check of the current Wave 3 Cursor Agent Adapter after fixes for W3C-F1 through W3C-F5.

Read the actual current files, current tests, and current worktree diff before judging them. Read AGENTS.md, docs/reviews/2026-08-09-wave2-closure-wave3-handoff.md, docs/reviews/2026-08-09-wave3-cursor-adapter-review.md, docs/reviews/2026-08-09-wave3-cursor-adapter-progress.md, internal/adapters/cursor/, internal/adapters/cli/, and the relevant tests. Use only read-only inspection. Do not edit, create, delete, stage, commit, or reset files. Do not start another Agent CLI or contact another agent. Do not claim a command was run unless the current evidence shows it.

Re-check only these closure items and the review invocation process:
1. W3C-F1: startup timeout must use timeout cancellation evidence, return a terminal result only after the process turn is actually done, and preserve a non-terminal cancel_unconfirmed result when termination cannot be confirmed. Check the regression test and the process lifecycle for orphan risk.
2. W3C-F2: a core Send ActionID must not be permanently consumed when Invocation construction or process start fails; check retry/idempotency and concurrent-session identity binding.
3. W3C-F3: a generic event-level name must not turn a valid assistant/result text event into a tool event; check JSONL fixtures and delivery classification.
4. W3C-F4: an unverified task MCP config-isolation request must have one fail-closed decision in Preflight and Launch; do not call it degraded if Launch cannot admit it.
5. W3C-F5: a transcript write failure must not emit a transcript raw event reference or otherwise claim durable evidence; check the regression test and returned evidence.
6. Review invocation process: verify the opt-in harness uses explicit read-only plan mode and trust, keeps the review prompt/report sanitized, validates required sections before staging, records the initial delivery and any same-session core-authorized follow-up, and attempts cancellation before cleanup if observation fails. Identify only concrete optimizations or risks in this invocation path, separating them from Adapter contract findings.

Return the complete final report as assistant text, not only a plan, tool payload, or turn-completed status. Use exactly this structure:

# Wave 3 Cursor Adapter targeted re-check
- Reviewer/model:
- Scope and worktree basis:
- Verdict: PASS | FAIL

## Executive summary

## Evidence checklist
Use a table covering W3C-F1 through W3C-F5 and the invocation process, with PASS or FAIL and exact current file/line evidence.

## Finding status
For each previous finding state CLOSED or OPEN, with exact verification. Do not invent unrelated findings.

## Invocation process optimization
State whether the real review invocation path has concrete remaining risks or optimizations. Include only evidence-backed items.

## Gate decision
State clearly whether the Cursor Adapter review/checkpoint can be closed. If FAIL, list only the minimum remaining actions.

Do not include credentials, authentication headers, environment-variable contents, private prompt text, or raw transcripts.`

const cursorWave3TargetedRecheckDeliveryPrompt = `Return the complete final Wave 3 Cursor Adapter targeted re-check report now. Do not reread files, call tools, edit files, or start another agent. Preserve the exact headings, W3C-F1 through W3C-F5 closure statuses, invocation process optimization section, evidence file/line references, and final PASS/FAIL gate decision. Do not return a summary, plan, or tool payload. Do not include credentials, environment-variable contents, private prompt text, or raw transcripts.`
