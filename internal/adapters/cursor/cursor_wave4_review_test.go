package cursor

import (
	"os"
	"path/filepath"
	"testing"
)

// TestRealCursorWave4Review is opt-in. It uses the existing read-only Cursor
// review harness to inspect the current Wave 4 worktree and stages the report
// only after terminal delivery and required-section validation succeed.
func TestRealCursorWave4Review(t *testing.T) {
	if os.Getenv("CURSOR_ADAPTER_WAVE4_REVIEW") != "1" {
		t.Skip("set CURSOR_ADAPTER_WAVE4_REVIEW=1 to run a real Cursor review of Wave 4")
	}
	config := cursorReviewConfig{
		Title:              "# Wave 4 cross-Adapter hardening review",
		Prompt:             wave4CursorReviewPrompt,
		DeliveryPrompt:     wave4CursorReviewDeliveryPrompt,
		Canary:             "real-wave4-cursor-review-canary",
		ActionID:           "wave4-review-delivery-follow-up",
		RequiredSections:   []string{"# Wave 4 cross-Adapter hardening review", "## Scope and evidence", "## Coverage ledger", "## Findings", "## Wave 4 exit criteria", "## Verdict"},
		ReportRelativePath: filepath.Join("docs", "reviews", "2026-08-10-wave4-cursor-review.md"),
	}
	run := runRealCursorReview(t, config)
	stageCursorReviewReport(t, run, config)
}

const wave4CursorReviewPrompt = `Act as the independent reviewer for the current Wave 4 cross-Adapter hardening work in this repository.

Read the actual current files, tests, evidence and worktree diff before judging them. At minimum read AGENTS.md, README.md, docs/plans/2026-08-07-multi-agent-orchestration-mvp-waves.md, docs/reviews/2026-08-10-wave4-baseline.md, tests/orchestration/evidence/2026-08-10/W4-baseline.md, tests/orchestration/evidence/2026-08-10/W4-smoke-matrix.md, tests/orchestration/evidence/2026-08-10/W4-benchmark-baseline.md, tests/orchestration/README.md, internal/adapters/cli/process.go, internal/adapters/cli/process_test.go, internal/adapters/cli/replay_test.go, internal/adapters/cli/stream_benchmark_test.go, internal/adapters/artifact/artifact_benchmark_test.go, internal/adapters/claude/claude_real_smoke_test.go, internal/adapters/codexcli/codex_real_smoke_test.go, internal/adapters/cursor/cursor.go, internal/adapters/cursor/cursor_test.go, internal/adapters/cursor/cursor_real_smoke_test.go, internal/adapters/cursor/cursor_real_review_test.go, tests/orchestration/conformance_test.go, all Wave 4 replay fixtures, and the existing Cursor/Claude/Codex CLI smoke evidence. Inspect the current git status and git diff as read-only evidence. Treat any existing docs/reviews/2026-08-10-wave4-cursor-review.md as a prior-run artifact, not as current implementation evidence; never copy its findings or verdict without rechecking the current files and diff. The existing review file may itself contain stale W4-IR1 text and old metadata from before the current staging fix; do not use that text or its old hash as evidence. Verify the current 'normalizeReviewBody' implementation and 'TestNormalizeReviewBodyRemovesDuplicateHarness', and judge W4-IR1 from the current staging code plus the current file/hash: if the current artifact has one harness and the baseline hash/bytes match it, mark W4-IR1 ALREADY ADDRESSED rather than NEW. In particular, re-audit the current Cursor contamination fix: verify that Send rejects a contaminated session, terminalResult preserves the local resume authority on a mismatching resume response while exposing the observed foreign session, and cursor_test.go deterministically verifies all three behaviors; mark the old W4-R7 as ALREADY ADDRESSED if the current code proves them, or report it as NEW only with current evidence. Check that real smoke failure diagnostics do not dump raw Provider stdout/stderr or credentials. Do not edit, create, delete, stage, commit, or reset any file. Do not start another Agent CLI or contact another agent. Do not claim a command was run unless current evidence shows it.

Review this implementation slice and its evidence:
1. Shared Windows CLI runner hardening: argv-only launch, stdout/stderr collector behavior, final-line flushing, ordering and loss risks around cmd.Wait, long output, cancellation and process-tree lifecycle. Check whether the regression test actually protects the reported race.
2. JSONL replay and delivery semantics: Cursor, Claude and Codex event normalization, provider/session identity, terminal evidence, final-text sources, tool-only output, malformed diagnostics, provider errors, and whether exit 0 can be mistaken for completion.
3. Cross-Adapter lifecycle conformance: preflight, launch, observe, terminal/final delivery, identity consistency and cleanup. Separate deterministic fixture-process evidence from real Provider smoke evidence and identify any misleading coverage claim.
4. Evidence and documentation: compare the Wave 4 baseline, evidence record, README and current diff with actual implementation maturity. Check sanitized paths, reproducibility, and the pending race/build/environment caveat.
5. Wave 4 exit criteria: identify the minimum concrete remaining work for Windows process-tree/atomic-replace/recovery stability, policy/contamination coverage, full smoke matrix, benchmark evidence and independent review. Do not turn future hardening into a current defect unless the current change makes a false claim.

Produce only the complete final review report in your final assistant message, not a plan, tool payload, prior report copy, or turn-completed status. Start exactly with the required title below and do not include duplicate harness metadata or any text before the title. Use exactly this structure:

# Wave 4 cross-Adapter hardening review
- Reviewer/model:
- Scope and worktree basis:
- Verdict: PASS | PASS WITH CONDITIONS | FAIL

## Scope and evidence

## Coverage ledger
Use a table with one row for each review area above and status PASS, FAIL, DEFERRED, or NOT APPLICABLE. Cite exact current file paths and line numbers.

## Findings
For every finding include an ID, severity P0/P1/P2/P3, status NEW / ALREADY ADDRESSED / OUT OF SCOPE, exact file and line, evidence, impact or reproduction, minimal fix, and verification. Do not invent findings merely to be cautious; distinguish defects in this Wave 4 slice from already addressed behavior and future exit criteria.

## Wave 4 exit criteria
List each baseline exit criterion as CLOSED, OPEN, or DEFERRED, with exact evidence and the minimum next action where needed. Keep real Provider smoke, deterministic replay, and fixture conformance clearly separated.

## Verdict
State clearly whether this Wave 4 slice passes its independent review/checkpoint. If conditional or failing, list only the minimum required actions before the checkpoint. Do not mark experimental accepted unless the current evidence actually proves every required criterion.

Do not include credentials, authentication headers, environment-variable contents, private prompt text, or raw transcripts in the report. Keep the report complete and evidence-backed.`

const wave4CursorReviewDeliveryPrompt = `Return the complete final Wave 4 cross-Adapter hardening review report now. Do not reread files, call tools, edit files, or start another agent. Preserve the exact headings, coverage ledger, findings with severity and status, Wave 4 exit-criteria statuses, exact file/line evidence, and final PASS/PASS WITH CONDITIONS/FAIL verdict from the review you already performed. Do not return a summary, plan, or tool payload. Do not include credentials, environment-variable contents, private prompt text, or raw transcripts.`
