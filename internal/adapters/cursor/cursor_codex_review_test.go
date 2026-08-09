package cursor

import (
	"os"
	"path/filepath"
	"testing"
)

// TestRealCursorCodexAdapterReview is opt-in. It uses the verified Cursor
// read-only review harness to inspect the Codex CLI Adapter without allowing
// the reviewer to edit the repository or invoke another Agent CLI.
func TestRealCursorCodexAdapterReview(t *testing.T) {
	if os.Getenv("CURSOR_ADAPTER_CODEX_REVIEW") != "1" {
		t.Skip("set CURSOR_ADAPTER_CODEX_REVIEW=1 to run a real Cursor review of Codex CLI")
	}
	config := cursorReviewConfig{
		Title:              "# Wave 3 Codex CLI Adapter review",
		Prompt:             codexAdapterReviewPrompt,
		DeliveryPrompt:     codexAdapterReviewDeliveryPrompt,
		Canary:             "real-codex-adapter-review-canary",
		ActionID:           "codex-review-delivery-follow-up",
		RequiredSections:   []string{"# Wave 3 Codex CLI Adapter review", "## Coverage ledger", "## Findings", "## Already addressed / not new", "## Deferred / out of scope", "## Verdict"},
		ReportRelativePath: filepath.Join("docs", "reviews", "2026-08-09-wave3-codex-adapter-review.md"),
	}
	run := runRealCursorReview(t, config)
	stageCursorReviewReport(t, run, config)
}

const codexAdapterReviewPrompt = `Act as the independent reviewer for the current Wave 3 Codex CLI Adapter implementation in this repository.

Read the actual current files, tests, evidence, documentation and worktree diff before judging them. At minimum read AGENTS.md, docs/reviews/2026-08-09-wave2-closure-wave3-handoff.md, docs/plans/2026-08-07-multi-agent-orchestration-mvp-waves.md, docs/reviews/2026-08-09-wave3-codex-adapter-progress.md, internal/adapters/codexcli/, internal/adapters/cli/, internal/adapters/artifact/, internal/gateway/, internal/orchestration/, and the related tests/evidence. Use only read-only inspection tools. Do not edit, create, delete, stage, commit, or reset any file. Do not start another Agent CLI or contact another agent.

Review this Codex CLI implementation slice, not Codex App Native, the Cursor/Claude adapters, or a generic redesign. Check the actual code, tests, and current worktree diff for:
1. Windows argv-only launch through the Codex npm shim, hidden-window behavior, stdout/stderr and JSONL handling, 'thread.started' identity, 'exec resume', timeout, process-tree cancellation, cancel_unconfirmed and orphan-process risk.
2. Explicit workspace authorization, read-only permission and '--sandbox read-only', '--ask-for-approval untrusted', model/reasoning binding, dangerous bypasses, ephemeral versus resumable sessions, and whether unsupported write/approval paths fail closed.
3. MCP strict/config-isolated/unsupported behavior, task allowlist validation, whether unsupported Codex MCP paths fail closed consistently in Preflight and Launch, and whether unrequested MCP servers can become visible.
4. provider/thread/workspace identity, task canary binding, contaminated-session handling, local resume authority after a foreign resumed thread, core-authorized send, ActionID rollback/idempotency, concurrent follow-up behavior, cleanup and race risks.
5. terminal versus final delivery semantics, durable '--output-last-message', structured JSONL fallback, tool-only output, exit 0 with empty final text, provider terminal errors, delivery_missing classification and same-thread recovery.
6. durable last-message path freshness and boundaries, transcript and artifact staging, canonical workspace checks, required-section validation, SHA-256/atomic replacement, and Gateway HostActionResult/CompletionDecision reason propagation.
7. whether the current unit/lifecycle/Gateway tests, opt-in real smoke and documentation provide sufficient evidence for this Codex slice.
8. whether the Cursor review invocation path itself has concrete security, delivery, sanitization, cancellation or cleanup optimizations relevant to this review; separate those from Adapter findings.

Produce the complete final review report in your final assistant message, not only a plan, tool payload, or turn-completed status. Use this exact report structure:

# Wave 3 Codex CLI Adapter review
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
State clearly whether this Codex CLI Adapter slice passes its independent review/checkpoint. If conditional or failing, list only the minimum required actions before the checkpoint.

Do not include credentials, authentication headers, environment-variable contents, private prompt text, or raw transcripts in the report. Keep the report complete and evidence-backed.`

const codexAdapterReviewDeliveryPrompt = `Return the complete final Wave 3 Codex CLI Adapter review report now. Do not reread files, call tools, edit files, or start another agent. Preserve the required headings, coverage ledger, exact file/line evidence, all findings with severity and status, the invocation-process assessment, and the final PASS/PASS WITH CONDITIONS/FAIL verdict from the review you already performed. Do not return a summary, plan, or tool payload; return the full Markdown report as assistant text. Do not include credentials, environment-variable contents, private prompt text, or raw transcripts.`
