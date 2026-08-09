package cursor

import (
	"os"
	"path/filepath"
	"testing"
)

// TestRealCursorClaudeAdapterTargetedRecheck is opt-in and closes only the
// independent review's session-contamination finding plus the invocation gate.
func TestRealCursorClaudeAdapterTargetedRecheck(t *testing.T) {
	if os.Getenv("CURSOR_ADAPTER_CLAUDE_RECHECK") != "1" {
		t.Skip("set CURSOR_ADAPTER_CLAUDE_RECHECK=1 to run the Claude targeted re-check")
	}
	config := cursorReviewConfig{
		Title:              "# Wave 3 Claude Code Adapter targeted re-check",
		Prompt:             claudeAdapterTargetedRecheckPrompt,
		DeliveryPrompt:     claudeAdapterTargetedRecheckDeliveryPrompt,
		Canary:             "real-claude-adapter-recheck-canary",
		ActionID:           "claude-recheck-delivery-follow-up",
		RequiredSections:   []string{"# Wave 3 Claude Code Adapter targeted re-check", "## Evidence checklist", "## Finding status", "## Invocation process optimization", "## Gate decision"},
		ReportRelativePath: filepath.Join("docs", "reviews", "2026-08-09-wave3-claude-adapter-targeted-recheck.md"),
	}
	run := runRealCursorReview(t, config)
	stageCursorReviewReport(t, run, config)
}

const claudeAdapterTargetedRecheckPrompt = `Act as the independent reviewer performing a targeted re-check of the current Wave 3 Claude Code Adapter after fixing W3CL-F1 from docs/reviews/2026-08-09-wave3-claude-adapter-review.md.

Read the actual current files, current tests and current worktree diff before judging them. Read AGENTS.md, docs/reviews/2026-08-09-wave3-claude-adapter-review.md, docs/reviews/2026-08-09-wave3-claude-adapter-progress.md, internal/adapters/claude/, internal/adapters/cli/, internal/gateway/, and the relevant tests. Use only read-only inspection. Do not edit, create, delete, stage, commit, or reset files. Do not start another Agent CLI or contact another agent. Do not claim a command was run unless current evidence shows it.

Re-check only these closure items:
1. W3CL-F1: after a resumed Claude turn returns a different session_id, the Adapter must return contaminated evidence with the observed foreign session for Gateway comparison, retain the original verified session as local resume authority, mark the session contaminated, and reject every later Send on the same handle. Check the regression test, result identity, cleanup and concurrent lifecycle.
2. Confirm the normal same-session core-authorized Send path, ActionID rollback after process start failure, and task-canary/session binding remain intact.
3. Confirm the opt-in Cursor review invocation used explicit read-only plan mode and trust, sanitized the prompt/report, validated required sections, recorded initial/final delivery and any same-session follow-up, and cancels before cleanup on observation failure. Identify only concrete remaining optimizations.

Return the complete final report as assistant text, not a plan, tool payload or turn-completed status. Use exactly this structure:

# Wave 3 Claude Code Adapter targeted re-check
- Reviewer/model:
- Scope and worktree basis:
- Verdict: PASS | FAIL

## Executive summary

## Evidence checklist
Use a table covering W3CL-F1, normal Send/ActionID behavior, and the review invocation process, with PASS or FAIL and exact current file/line evidence.

## Finding status
State W3CL-F1 as CLOSED or OPEN with exact verification. Do not invent unrelated findings.

## Invocation process optimization
State whether the real review invocation path has concrete remaining risks or optimizations, separating them from the Adapter finding.

## Gate decision
State clearly whether the Claude Code Adapter review/checkpoint can be closed. If FAIL, list only the minimum remaining actions.

Do not include credentials, authentication headers, environment-variable contents, private prompt text or raw transcripts.`

const claudeAdapterTargetedRecheckDeliveryPrompt = `Return the complete final Wave 3 Claude Code Adapter targeted re-check report now. Do not reread files, call tools, edit files or start another agent. Preserve the exact headings, W3CL-F1 closure status, invocation process optimization section, evidence file/line references and final PASS/FAIL gate decision. Do not return a summary, plan or tool payload. Do not include credentials, environment-variable contents, private prompt text or raw transcripts.`
