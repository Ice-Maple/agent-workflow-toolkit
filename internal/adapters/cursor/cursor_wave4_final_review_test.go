package cursor

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRealCursorWave4ExperimentalReadinessReview(t *testing.T) {
	if os.Getenv("CURSOR_ADAPTER_WAVE4_FINAL_REVIEW") != "1" {
		t.Skip("set CURSOR_ADAPTER_WAVE4_FINAL_REVIEW=1 to run the final Wave 4 readiness review")
	}
	config := cursorReviewConfig{
		Title:              "# Wave 4 experimental readiness review",
		Prompt:             wave4ExperimentalReadinessReviewPrompt,
		DeliveryPrompt:     wave4ExperimentalReadinessReviewDeliveryPrompt,
		Canary:             "real-wave4-experimental-readiness-review-canary",
		ActionID:           "wave4-experimental-readiness-review-delivery",
		RequiredSections:   []string{"# Wave 4 experimental readiness review", "## Scope and evidence", "## Benchmark audit", "## Findings", "## Experimental boundary", "## Verdict"},
		ReportRelativePath: filepath.Join("docs", "reviews", "2026-08-11-wave4-experimental-readiness-review.md"),
	}
	run := runRealCursorReview(t, config)
	stageCursorReviewReport(t, run, config)
}

const wave4ExperimentalReadinessReviewPrompt = `You are the independent reviewer for the final automated Wave 4 experimental-readiness slice in agent-workflow-toolkit. Work read-only in the trusted repository. Inspect actual files, current git status and current diff; do not rely on the author's summary alone. Do not edit, create, delete, stage, commit or reset files. Do not invoke another Agent or contact another Provider.

At minimum inspect AGENTS.md, docs/plans/2026-08-07-multi-agent-orchestration-mvp-waves.md, docs/reviews/2026-08-11-wave4-selected-follow-up.md, tests/orchestration/README.md, tests/orchestration/evidence/2026-08-11/W4-selected-follow-up.md, tests/orchestration/evidence/2026-08-11/W4-benchmark-complete.md, tests/orchestration/evidence/2026-08-10/W4-smoke-matrix.md, internal/adapters/cli/process.go, process_test.go, process_windows_test.go, process_benchmark_test.go, process_windows_benchmark_test.go, stream_benchmark_test.go, internal/adapters/artifact/artifact.go, artifact_test.go, artifact_benchmark_test.go, internal/storage/store.go, store_test.go, store_benchmark_test.go, internal/policy/policy_test.go, internal/gateway/gateway_test.go, and tests/orchestration/conformance_test.go. Compare the current diff with the historical 2026-08-10 Cursor review, but re-evaluate every relevant claim from current evidence.

Audit the complete benchmark against the seven dimensions explicitly listed in the Wave 4 plan: fresh-process startup, retained/peak memory, JSON/JSONL throughput, parallel different-actor isolation, cancellation response, event append and snapshot replace. Check timer boundaries, correctness assertions, process cleanup, Windows memory-counter layout and access, distinct actor workspace/environment values, event/snapshot measurement separation, and whether installed CLI --version probes are correctly described as startup-only rather than Provider smoke. Flag misleading performance claims or unreproducible evidence.

Re-audit the current automated hardening: real Windows parent/child process-tree cancellation, unexpected exit, atomic replacement interruption/recovery, per-Adapter and shared delivery recovery, contamination authority preservation and Send rejection, policy fail-closed combinations, Goal/revision authority and secret filtering. Keep deterministic fixture conformance separate from the existing real Cursor/Claude/Codex Provider smoke and historical Native App evidence.

The repository contains no import "C" and its normal tests/benchmarks run with CGO_ENABLED=0. The original Wave 4 work items do not name go test -race. On Windows, the race detector requires a separate CGO/C compiler toolchain. Treat current race-enabled verification as optional supplementary evidence, not a project runtime dependency or a P0/P1 blocker, unless actual current code evidence shows a concrete concurrency defect.

Return a concise Markdown report with exactly these headings:
# Wave 4 experimental readiness review
## Scope and evidence
## Benchmark audit
## Findings
## Experimental boundary
## Verdict

Findings must be evidence-based and prioritized P0/P1/P2. Do not invent future hardening or production requirements as current defects. State whether any P0/P1 remains. The verdict must be PASS, PASS WITH CONDITIONS or FAIL. Even on PASS, do not mark experimental accepted: say whether the automated evidence is ready for the user's explicit human approval and a later versioned acceptance record. Do not include credentials, URLs, auth headers, private prompts, raw Provider stdout/stderr or private absolute user paths.`

const wave4ExperimentalReadinessReviewDeliveryPrompt = `Return the final review report now. Use exactly these headings: # Wave 4 experimental readiness review, ## Scope and evidence, ## Benchmark audit, ## Findings, ## Experimental boundary, ## Verdict. Include the prioritized findings and verdict in the final message itself. Do not edit repository files.`
