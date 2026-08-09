package cursor

import (
	"os"
	"path/filepath"
	"testing"
)

// TestRealCursorCodexAdapterTargetedRecheck closes only the findings from the
// initial Codex review and the review invocation gate.
func TestRealCursorCodexAdapterTargetedRecheck(t *testing.T) {
	if os.Getenv("CURSOR_ADAPTER_CODEX_RECHECK") != "1" {
		t.Skip("set CURSOR_ADAPTER_CODEX_RECHECK=1 to run the Codex targeted re-check")
	}
	config := cursorReviewConfig{
		Title:              "# Wave 3 Codex CLI Adapter targeted re-check",
		Prompt:             codexAdapterTargetedRecheckPrompt,
		DeliveryPrompt:     codexAdapterTargetedRecheckDeliveryPrompt,
		Canary:             "real-codex-adapter-recheck-canary",
		ActionID:           "codex-recheck-delivery-follow-up",
		RequiredSections:   []string{"# Wave 3 Codex CLI Adapter targeted re-check", "## Evidence checklist", "## Finding status", "## Invocation process optimization", "## Gate decision"},
		ReportRelativePath: filepath.Join("docs", "reviews", "2026-08-09-wave3-codex-adapter-targeted-recheck.md"),
	}
	run := runRealCursorReview(t, config)
	stageCursorReviewReport(t, run, config)
}

const codexAdapterTargetedRecheckPrompt = `Act as an independent reviewer performing a targeted re-check of the current Wave 3 Codex CLI Adapter after fixing W3CX-F1 and W3CX-F2 from docs/reviews/2026-08-09-wave3-codex-adapter-review.md.

Read the actual current files, current tests, current evidence, current progress note and current worktree diff before judging them. Read AGENTS.md, docs/reviews/2026-08-09-wave3-codex-adapter-review.md, docs/reviews/2026-08-09-wave3-codex-adapter-progress.md, tests/orchestration/evidence/2026-08-09/W3-codex-adapter-smoke.md, internal/adapters/codexcli/, internal/adapters/cli/, internal/adapters/cursor/cursor_codex_review_test.go, internal/adapters/cursor/cursor_codex_targeted_recheck_test.go, and the relevant Gateway tests. Use only read-only inspection. Do not edit, create, delete, stage, commit, or reset files. Do not start another Agent CLI or contact another agent. Do not claim a command was run unless current evidence shows it.

Re-check only these closure items:
1. W3CX-F1: when no task MCP allowlist is requested, Codex Invocation and Adapter Launch/Send must use the verified user-config isolation flag, while explicit custom provider route configuration remains argv-only and cannot contain embedded credentials. Explicit task MCP allowlists/config-isolated requests must still fail closed consistently in Preflight and Launch. Check exact argv tests and the real evidence without exposing secrets.
2. W3CX-F2: the opt-in real smoke must cover the non-ephemeral Adapter Preflight, Launch, thread identity, Observe, terminal/durable delivery and cleanup path, in addition to the direct ephemeral durable-delivery regression. Check current evidence and the test's timeout/cancellation/cleanup behavior. Do not require a follow-up real Send unless the current evidence claims it.
3. Confirm previously addressed identity contamination, ActionID rollback, terminal/final delivery, transcript reference and Gateway mapping remain intact at the current lines.
4. Review invocation process: explicit read-only plan mode and trust, sanitized prompt/report, required-section validation, same-session delivery follow-up, cancellation before cleanup on observation failure, and whether using a separate provider route for the real Codex smoke creates a concrete evidence or security risk. Separate invocation optimizations from Adapter findings.

Return the complete final report as assistant text, not a plan, tool payload or turn-completed status. Use exactly this structure:

# Wave 3 Codex CLI Adapter targeted re-check
- Reviewer/model:
- Scope and worktree basis:
- Verdict: PASS | FAIL

## Executive summary

## Evidence checklist
Use a table covering W3CX-F1, W3CX-F2, previous regression safeguards, and the invocation process, with PASS or FAIL and exact current file/line evidence.

## Finding status
State W3CX-F1 and W3CX-F2 as CLOSED or OPEN with exact verification. Do not invent unrelated findings.

## Invocation process optimization
State whether the real review invocation path and the Codex smoke invocation have concrete remaining risks or optimizations, separating them from Adapter findings.

## Gate decision
State clearly whether the Codex CLI Adapter review/checkpoint can be closed. If FAIL, list only the minimum remaining actions.

Do not include credentials, authentication headers, environment-variable contents, private prompt text or raw transcripts.`

const codexAdapterTargetedRecheckDeliveryPrompt = `Return the complete final Wave 3 Codex CLI Adapter targeted re-check report now. Do not reread files, call tools, edit files or start another agent. Preserve the exact headings, W3CX-F1 and W3CX-F2 closure statuses, invocation process optimization section, evidence file/line references and final PASS/FAIL gate decision. Do not return a summary, plan or tool payload. Do not include credentials, environment-variable contents, private prompt text or raw transcripts.`
