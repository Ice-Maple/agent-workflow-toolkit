# Wave 3 non-blocking global installer smoke

- Date: 2026-08-08
- Platform: Windows, PowerShell
- Repository: `E:/projects/agent-workflow-toolkit`
- Scope: local script/CLI evidence; this does not replace a restarted Codex App host smoke.

## Regression suite

Command:

```powershell
python -m unittest discover -s tests/context_continuity -p 'test_*.py'
```

Result:

```text
Ran 50 tests in 7.188s
OK
```

The suite covers quiet clean startup/compact, mutation-only dirty recovery,
read-only tool classification, handler-level stale blocking-Stop removal,
preservation of unrelated hooks and user compaction settings, apply rollback,
restore hash prevalidation, transactional restore rollback, and manifest-based
restore.

## Installer apply/restore smoke

The installer was applied to a new temporary `CODEX_HOME`, its generated JSON
was parsed, and the emitted `BACKUP_DIR` was then restored.

Observed after apply:

```text
APPLIED=True
events=SessionStart,PreToolUse,PreCompact,PostCompact,SubagentStart,SubagentStop
has_stop=False
has_status_message=False
config_created=False
```

Observed after restore:

```text
RESTORED=True
hooks_remain=False
config_remain=False
```

Conclusion: the current global profile installs no `Stop`, produces no optional
hook status text, does not create a compaction config for a fresh user, and can
restore the pre-install file state from its manifest.

## Required App follow-up

After applying to a real user profile, restart Codex App, review/re-enable the
changed non-managed hooks, and verify that a normal response ends without a
checkpoint continuation. Then verify one clean compact and one dirty compact.
