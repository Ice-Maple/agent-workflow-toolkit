package cursor

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRealCursorRoleFirstClosureReview(t *testing.T) {
	if os.Getenv("CURSOR_ROLE_FIRST_CLOSURE_REVIEW") != "1" {
		t.Skip("set CURSOR_ROLE_FIRST_CLOSURE_REVIEW=1 to run the independent Role-first closure review")
	}
	config := cursorReviewConfig{
		Title:              "# Role-first real-combination closure review",
		Prompt:             roleFirstClosureReviewPrompt,
		DeliveryPrompt:     roleFirstClosureReviewDeliveryPrompt,
		Canary:             "role-first-real-combination-review",
		ActionID:           "role-first-closure-review-delivery",
		RequiredSections:   []string{"# Role-first real-combination closure review", "## Scope and evidence", "## Findings", "## Regression matrix", "## Complexity check", "## Verdict"},
		ReportRelativePath: filepath.Join("docs", "reviews", "2026-08-14-role-first-real-combination-closure-review.md"),
	}
	run := runRealCursorReview(t, config)
	stageCursorReviewReport(t, run, config)
}

const roleFirstClosureReviewPrompt = `You are an independent read-only reviewer. Do not modify, stage, commit, reset, delete, or create files. Do not start another Agent. Inspect the actual current worktree and git diff in this repository rather than relying on summaries.

Review the Role-first real-combination closure. At minimum inspect AGENTS.md, README.md, cmd/agent-workflow-core/main.go and its tests, internal/bridge/owner_process.go, internal/bridge/owner.go, internal/bridge/e2e_test.go, internal/bridge/cursor_real_e2e_test.go, internal/adapters/cursor/cursor.go and cursor_test.go, cmd/agent-workflow-collaboration/main.go and tests, skills/multi-agent-orchestration/SKILL.md, invoke_project_role.ps1, tools/orchestration/install_project_role_package.ps1, smoke_project_role_entry.ps1, the adoption/architecture guides, and tests/orchestration/evidence/2026-08-14/role-first-real-combination-closure.md. Inspect current git status and git diff.

Verify these exact risks:
1. active external owner disable cannot panic, does not recreate a terminal Attempt, and returns truthful cancellation/uncertainty;
2. owner readiness is published only after listener and collaboration owner registration, independently of Provider session startup;
3. Cursor receives the entire multiline dispatch Prompt after an option terminator, including text that resembles CLI options;
4. a project Role Package has one generic upstream installer/receipt/registry/managed entry and a one-step Router path, without requiring a downstream development Skill or embedding Adapter/model fields;
5. zero-install/direct/single/framework/disable and post-admission fail-closed boundaries remain accurate;
6. deterministic fixture tests are not represented as real Provider evidence, while the opt-in real Cursor test actually covers public bridge launch, observe, completion and technical release;
7. the change does not import doc-ai-toolkit or add domain semantics, credentials, raw Provider output, or PATH fallback.

Report only current actionable findings with exact file and line references, severity P0/P1/P2, impact, and minimum fix. Do not repeat historical findings that current code closes. If there are no findings, explicitly say there are no open P0/P1/P2. Include a concise complexity check that distinguishes the minimal request/response path from safety/durability layers and flags avoidable new abstraction if present.

Return Markdown with exactly these top-level sections:
# Role-first real-combination closure review
## Scope and evidence
## Findings
## Regression matrix
## Complexity check
## Verdict

The verdict must be PASS, PASS WITH P2, or BLOCKED.`

const roleFirstClosureReviewDeliveryPrompt = `Return only the complete final Markdown review. It must begin with "# Role-first real-combination closure review" and contain all required sections. Do not include preamble, tool narration, or a fenced wrapper.`
