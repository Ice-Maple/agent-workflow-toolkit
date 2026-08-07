# W2 local CLI smoke

- Date: 2026-08-07
- Scope: local Python subprocesses with temporary state/workspace directories
- Host Codex App lifecycle: **not tested**

## Flow

`SessionStart(startup)` → `Stop` (checkpoint request) → `seal.py` →
`PreCompact(auto)` → `PostCompact(auto)` → `SessionStart(compact)`.

Result: `CLI_W2_SMOKE=PASS`.

The compact hook output did not contain `continue:false`; the compact-start
hook reinjected the sealed checkpoint. This validates direct-file entry points
and the local state contract only. It does not establish the Codex App event
ordering, automatic threshold behavior, or UI continuation semantics.

## Automated coverage

```text
python -m unittest discover -s tests/context_continuity -p "test_*.py"
Ran 28 tests ... OK
```

The test suite covers safe/dirty recovery, duplicate compact callbacks,
marker fallback, stale PostCompact, malformed state/input, secret redaction,
budget bounds, and Stop loop protection.
