package cursor

import (
	"os"
	"path/filepath"
	"testing"
)

// TestRealCursorClaudeAdapterReview is opt-in. It reuses the already verified
// Cursor read-only review harness to inspect the Claude Code implementation;
// the worker under review is not allowed to edit or invoke another Agent CLI.
func TestRealCursorClaudeAdapterReview(t *testing.T) {
	if os.Getenv("CURSOR_ADAPTER_CLAUDE_REVIEW") != "1" {
		t.Skip("set CURSOR_ADAPTER_CLAUDE_REVIEW=1 to run a real Cursor review of Claude")
	}
	config := cursorReviewConfig{
		Title:              "# Wave 3 Claude Code Adapter review",
		Prompt:             claudeAdapterReviewPrompt,
		DeliveryPrompt:     claudeAdapterReviewDeliveryPrompt,
		Canary:             "real-claude-adapter-review-canary",
		ActionID:           "claude-review-delivery-follow-up",
		RequiredSections:   []string{"# Wave 3 Claude Code Adapter review", "## Coverage ledger", "## Findings", "## Already addressed / not new", "## Deferred / out of scope", "## Verdict"},
		ReportRelativePath: filepath.Join("docs", "reviews", "2026-08-09-wave3-claude-adapter-review.md"),
	}
	run := runRealCursorReview(t, config)
	stageCursorReviewReport(t, run, config)
}

const claudeAdapterReviewPrompt = `Act as the independent reviewer for the current Wave 3 Claude Code Adapter implementation in this repository.

Read the actual current files, tests, evidence and worktree diff before judging them. At minimum read AGENTS.md, docs/reviews/2026-08-09-wave2-closure-wave3-handoff.md, docs/plans/2026-08-07-multi-agent-orchestration-mvp-waves.md, docs/reviews/2026-08-09-wave3-claude-adapter-progress.md, internal/adapters/claude/, internal/adapters/cli/, internal/adapters/artifact/, internal/gateway/, internal/orchestration/, and the related tests/evidence. Use only read-only inspection tools. Do not edit, create, delete, stage, commit, or reset any file. Do not start another Agent CLI or contact another agent.

Review this Claude Code implementation slice, not the future Codex CLI adapter and not a generic redesign. Check the actual code, tests, and current worktree diff for:
1. Windows argv-only launch through the Claude npm shim, hidden-window behavior, stdout/stderr and stream-json handling, timeout, process-tree cancellation, and orphan-process risk.
2. Explicit workspace trust, read-only plan permission, reasoning/effort handling, dangerous permission bypasses, model registration limitations, fallback behavior, and whether unsupported write paths fail closed.
3. strict/config-isolated/unsupported MCP behavior, task config path boundaries, exact allowlist matching, secret/config leakage and whether the invocation can see unrequested MCP servers.
4. provider/session/workspace identity, task canary binding, contaminated-session handling, resume/send authorization, action reuse, concurrent follow-up behavior, cleanup and race risks.
5. terminal versus final delivery semantics, assistant/result event normalization, tool-only output, exit 0 with empty final text, provider terminal errors, delivery_missing classification and same-session recovery.
6. transcript and artifact staging boundaries, canonical workspace checks, required-section validation, SHA-256/atomic replacement, and Gateway HostActionResult/CompletionDecision reason propagation.
7. whether the current tests, real smoke and documentation provide sufficient evidence for this Claude slice and whether the shared Cursor/CLI review invocation has concrete risks relevant to this change.

Produce the complete final review report in your final assistant message, not only a plan, tool payload, or turn-completed status. Use this exact report structure:

# Wave 3 Claude Code Adapter review
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
State clearly whether this Claude Code Adapter slice passes its independent review/checkpoint. If conditional or failing, list only the minimum required actions before the checkpoint.

Do not include credentials, authentication headers, environment-variable contents, private prompt text, or raw transcripts in the report. Keep the report complete and evidence-backed.`

const claudeAdapterReviewDeliveryPrompt = `Return the complete final Wave 3 Claude Code Adapter review report now. Do not reread files, call tools, edit files, or start another agent. Preserve the required headings, coverage ledger, exact file/line evidence, all findings with severity and status, and the final PASS/PASS WITH CONDITIONS/FAIL verdict from the review you already performed. Do not return a summary, plan, or tool payload; return the full Markdown report as assistant text. Do not include credentials, environment-variable contents, private prompt text, or raw transcripts.`
