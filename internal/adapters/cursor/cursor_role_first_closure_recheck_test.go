package cursor

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRealCursorRoleFirstClosureTargetedRecheck(t *testing.T) {
	if os.Getenv("CURSOR_ROLE_FIRST_CLOSURE_RECHECK") != "1" {
		t.Skip("set CURSOR_ROLE_FIRST_CLOSURE_RECHECK=1 to run the targeted Role-first closure re-check")
	}
	config := cursorReviewConfig{
		Title:              "# Role-first real-combination targeted re-check",
		Prompt:             roleFirstClosureRecheckPrompt,
		DeliveryPrompt:     roleFirstClosureRecheckDeliveryPrompt,
		Canary:             "role-first-real-combination-targeted-recheck",
		ActionID:           "role-first-closure-recheck-delivery",
		RequiredSections:   []string{"# Role-first real-combination targeted re-check", "## Rechecked evidence", "## Finding status", "## Verdict"},
		ReportRelativePath: filepath.Join("docs", "reviews", "2026-08-14-role-first-real-combination-targeted-recheck.md"),
	}
	run := runRealCursorReview(t, config)
	stageCursorReviewReport(t, run, config)
}

const roleFirstClosureRecheckPrompt = `You are the independent read-only reviewer performing a targeted re-check after docs/reviews/2026-08-14-role-first-real-combination-closure-review.md returned BLOCKED. Do not modify, stage, commit, reset, delete, or create files. Do not start another Agent. Inspect the actual current files and worktree snapshot, not an implementer summary.

Recheck only the two prior findings and their regression impact:

1. Prior P1 ready-owner follow-up launch. Inspect internal/bridge/executor.go, owner_process.go, e2e_test.go and relevant types/tests. Verify a repeated pending launch in starting/ready state validates owner identity/action identity and returns the same owner view without IPC, stale_owner, or Provider relaunch. Verify slow-start regression actually invokes launch twice before the Provider session and checks same PID. State whether cancellation during provider startup remains a truthful outcome_unknown boundary rather than being falsely reported cancelled.

2. Prior P2 legacy AttemptID-only nil map. Inspect cmd/agent-workflow-core/main.go and adoption_test.go. Verify the fallback map is assigned to entry.Attempts before per-attempt status writes, terminal Attempts are still not recreated in latest.Attempts, and the regression constructs a top-level AttemptID with nil Attempts and calls disableAndConverge.

Also check that these fixes do not weaken project Role receipt/fail-closed behavior, Cursor multiline prompt transport, or the distinction between fixture and real Cursor framework evidence. Report only current actionable findings with exact file/line references. If both prior findings are closed and no regression creates a new P0/P1, say so explicitly.

Return Markdown with exactly these top-level sections:
# Role-first real-combination targeted re-check
## Rechecked evidence
## Finding status
## Verdict

The verdict must be PASS, PASS WITH P2, or BLOCKED.`

const roleFirstClosureRecheckDeliveryPrompt = `Return only the complete final Markdown targeted re-check. It must begin with "# Role-first real-combination targeted re-check" and contain all required sections. Do not include preamble, tool narration, or a fenced wrapper.`
