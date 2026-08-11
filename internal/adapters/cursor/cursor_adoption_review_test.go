package cursor

import (
	"os"
	"path/filepath"
	"testing"
)

// TestRealCursorProjectAdoptionReview is opt-in because it invokes a real
// Cursor Agent. The reviewer is constrained to read-only plan mode; the host
// stages the delivered report only after terminal delivery is validated.
func TestRealCursorProjectAdoptionReview(t *testing.T) {
	if os.Getenv("CURSOR_ADAPTER_ADOPTION_REVIEW") != "1" {
		t.Skip("set CURSOR_ADAPTER_ADOPTION_REVIEW=1 to run the project-adoption review")
	}
	config := cursorReviewConfig{
		Title:              "# Project adoption hardening review",
		Prompt:             projectAdoptionReviewPrompt,
		DeliveryPrompt:     projectAdoptionReviewDeliveryPrompt,
		Canary:             "real-project-adoption-review-canary",
		ActionID:           "project-adoption-review-delivery",
		RequiredSections:   []string{"# Project adoption hardening review", "## Scope and evidence", "## Finding closure matrix", "## Guide-to-implementation consistency", "## Findings", "## Verdict"},
		ReportRelativePath: filepath.Join("docs", "reviews", "2026-08-11-project-adoption-cursor-review.md"),
	}
	run := runRealCursorReview(t, config)
	stageCursorReviewReport(t, run, config)
}

func TestRealCursorProjectAdoptionTargetedRecheck(t *testing.T) {
	if os.Getenv("CURSOR_ADAPTER_ADOPTION_RECHECK") != "1" {
		t.Skip("set CURSOR_ADAPTER_ADOPTION_RECHECK=1 to close the project-adoption review")
	}
	config := cursorReviewConfig{
		Title:              "# Project adoption hardening targeted re-check",
		Prompt:             projectAdoptionTargetedRecheckPrompt,
		DeliveryPrompt:     projectAdoptionTargetedRecheckDeliveryPrompt,
		Canary:             "real-project-adoption-targeted-recheck-canary",
		ActionID:           "project-adoption-targeted-recheck-delivery",
		RequiredSections:   []string{"# Project adoption hardening targeted re-check", "## Scope", "## Re-check results", "## Findings", "## Gate decision"},
		ReportRelativePath: filepath.Join("docs", "reviews", "2026-08-11-project-adoption-cursor-targeted-recheck.md"),
	}
	run := runRealCursorReview(t, config)
	stageCursorReviewReport(t, run, config)
}

const projectAdoptionReviewPrompt = `Act as the independent reviewer for the current project-adoption hardening work in agent-workflow-toolkit. Work read-only in the trusted repository. Inspect actual current files, git status and the complete worktree diff; do not rely on the author's closure summary or any verdict that predates the adoption guide. Do not edit, create, delete, stage, commit or reset files. Do not invoke another Agent or contact another Provider.

At minimum inspect AGENTS.md, docs/reviews/2026-08-11-project-adoption-issues-and-recommendations.md, docs/guides/multi-agent-orchestration-project-adoption.md, docs/architecture/multi-agent-orchestration-architecture.md, tests/orchestration/evidence/2026-08-11/AWT-adoption-hardening.md, tests/orchestration/README.md, skills/multi-agent-orchestration/SKILL.md, skills/multi-agent-orchestration/references/native-action-contract.md, tools/orchestration/install_controller_skill.ps1, cmd/agent-workflow-core/main.go, cmd/agent-workflow-core/adoption_test.go, internal/gateway/gateway.go, internal/gateway/gateway_test.go, internal/storage/store.go, internal/storage/root_lock.go and platform lock files, internal/storage/store_test.go, internal/policy/policy.go, internal/policy/policy_test.go, internal/protocol/v1/envelope.go, internal/protocol/v1/envelope_test.go, internal/orchestration/model.go and protocol/v1/request.schema.json.

Re-evaluate every AWT-ADOPT-001 through AWT-ADOPT-011 item from current evidence. In particular verify:

1. ordinary successful approve omits attempt_id while attempt-bound approve remains only fallback authorization;
2. the consumer Skill installer/receipt checks checkout, binary version/revision/dirty/hash and Skill source/target/hash, and the guide states the fresh-task discovery boundary without claiming automation that does not exist;
3. Gateway v1 issues/accepts only launch, observe and cancel_host, while send/cleanup remain explicitly reserved Adapter lifecycle primitives; delivery_missing uses revision/abort and cleanup belongs to runner finally;
4. node role is non-empty admission data and exactly matches binding role without closing the role enum;
5. version, Go VCS metadata, installer checks and upgrade instructions agree;
6. --validate-request is accurately envelope-only, schema/runtime operation sets agree, and payload validation remains Gateway-specific;
7. policy top-level/nested unknown fields and trailing JSON fail closed;
8. the state-dir OS lock really protects separate one-shot processes and structured busy behavior matches the guide;
9. pending/completed/failed/outcome_unknown request journal semantics prevent automatic redispatch after unknown outcomes, without falsely claiming a transactional database or recoverable exactly-once result;
10. snapshot schema/protocol/writer metadata and new-state-dir upgrade instructions agree, with no false migration claim;
11. the public adoption tests exercise the published binary path and the evidence distinguishes deterministic conformance, real Provider smoke and the manual fresh-task Skill discovery check.

Also audit the PowerShell examples for actual parameter names, ordering, path/encoding behavior and whether a clean consumer can follow them. Treat the existing external Adapter executor gap, absent state migration, unsupported in-place send, coarse state-dir locking, optional Windows race tooling, Wave 5, write Agents, Goal autonomy, dashboards/daemons and production SLA as documented boundaries rather than defects unless the current docs falsely claim them.

Return a concise Markdown report with exactly these headings:
# Project adoption hardening review
## Scope and evidence
## Finding closure matrix
## Guide-to-implementation consistency
## Findings
## Verdict

The closure matrix must have one row for every AWT-ADOPT-001 through AWT-ADOPT-011 with PASS or FAIL and exact current file/line evidence. Findings must be evidence-based and prioritized P0/P1/P2; write "No findings" if none. The verdict must be PASS, PASS WITH CONDITIONS or FAIL and explicitly state whether any item remains open. Do not include credentials, URLs, auth headers, private prompts, raw Provider stdout/stderr or private absolute user paths.`

const projectAdoptionReviewDeliveryPrompt = `Return the complete final project adoption hardening review now. Use exactly these headings: # Project adoption hardening review, ## Scope and evidence, ## Finding closure matrix, ## Guide-to-implementation consistency, ## Findings, ## Verdict. Include all AWT-ADOPT-001 through AWT-ADOPT-011 statuses, exact current file/line evidence, prioritized findings and the final verdict. Do not edit repository files.`

const projectAdoptionTargetedRecheckPrompt = `Act as the independent Cursor reviewer performing a targeted closure check for the project-adoption hardening. Work read-only in the trusted repository. Do not edit, create, delete, stage, commit or reset files. Do not invoke another Agent or Provider.

Inspect the current docs/reviews/2026-08-11-project-adoption-cursor-review.md as the completed broad review artifact, but re-check its sole remaining P2 condition from current files rather than inheriting its verdict. Also inspect tests/orchestration/evidence/2026-08-11/AWT-adoption-hardening.md, tests/orchestration/README.md, docs/reviews/2026-08-11-project-adoption-issues-and-recommendations.md, internal/gateway/gateway.go, internal/gateway/gateway_test.go, cmd/agent-workflow-core/adoption_test.go, docs/guides/multi-agent-orchestration-project-adoption.md, skills/multi-agent-orchestration/SKILL.md and the current git status/diff available to you.

Verify only:
1. the broad review artifact now describes the current immutable delivery_missing checkpoint implementation instead of the stale pre-fix behavior;
2. evidence and tests/README link the current broad review and distinguish its actual verdict/limitations;
3. AWT-ADOPT-003A has no open contract drift: repeat completion, ordinary approve and retry_node all fail with the documented recovery direction while request_revision/abort remain available;
4. AWT-ADOPT-011 now has public-path automation, real Native evidence, the current broad Cursor review and this independent targeted closure path;
5. no new P0/P1 regression is visible in the targeted diff.

Return concise Markdown with exactly these headings:
# Project adoption hardening targeted re-check
## Scope
## Re-check results
## Findings
## Gate decision

Use PASS or FAIL for each of the five checks with exact file/line evidence. Write "No findings" if none. Gate decision must be PASS or FAIL and state whether AWT-ADOPT-003A and AWT-ADOPT-011 can close. Do not include secrets, URLs, raw Provider output, private prompts or private absolute user paths.`

const projectAdoptionTargetedRecheckDeliveryPrompt = `Return the complete final targeted re-check now with exactly these headings: # Project adoption hardening targeted re-check, ## Scope, ## Re-check results, ## Findings, ## Gate decision. Include PASS/FAIL for all five requested checks, exact current file/line evidence, and whether AWT-ADOPT-003A and AWT-ADOPT-011 can close. Do not edit repository files.`
