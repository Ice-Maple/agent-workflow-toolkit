# W3 local CLI smoke

- Date: 2026-08-07
- Scope: isolated Python hook processes with temporary state/workspace directories
- Host Codex App lifecycle: **not tested**

## Flow

```text
SessionStart(startup)
→ PreToolUse(Bash)
→ seal(safe)
→ PreToolUse(mcp__domp__get_page)
→ SubagentStart
→ SubagentStop
→ PreCompact(auto)
→ PostCompact(auto)
→ SessionStart(compact)
```

Result: `CLI_W3_SMOKE=PASS`.

Observed output:

```text
SEAL={"status":"safe","checkpoint_seq":1,"checkpoint_chars":81,"hot_estimated_tokens":29,"warm_estimated_tokens":29,"content_digest":"72ad081e4e7f251f","idempotent":false}
PRECOMPACT={}
RECOVERY_REQUIRED=True
STATUS=recovery_required ORIGIN=dirty HOT_TOKENS=29 WARM_TOKENS=29 EMERGENCY_TOKENS=152
EVENTS=8
PreToolUse -> Seal -> PreToolUse -> SubagentStart -> SubagentStop -> PreCompact -> PostCompact -> SessionStart
RAW_SECRET_PERSISTED=False
```

The run confirms that a read/evidence MCP call and a completed subagent still
leave the root turn dirty until a new checkpoint is sealed. Dirty auto compact
is allowed, writes a bounded emergency manifest, and the compact SessionStart
injects the last safe hot state plus the mechanical recovery instructions.

This is a direct script/CLI contract test. The `auto` value is supplied in the
synthetic hook payload; it does not prove that the Codex App's native threshold
was reached or that the App loaded/trusted the Wave 3 fixture. A real App smoke
must separately capture host event ordering and UI continuation behavior.
