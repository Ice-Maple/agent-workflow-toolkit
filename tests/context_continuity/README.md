# Context continuity Wave 0

Wave 0 is an observational host probe. It records lifecycle metadata only; it
does not block hooks, inject context, modify the user configuration, or copy a
transcript.

## Local probe smoke

From the repository root:

```powershell
python -m unittest discover -s tests/context_continuity -p "test_*.py"

$probeLog = Join-Path ([IO.Path]::GetTempPath()) "codex-context-continuity-wave0"
$payload = @{
  hook_event_name = "PreCompact"
  trigger = "auto"
  session_id = "wave0-session"
  turn_id = "wave0-turn"
  cwd = (Get-Location).Path
  transcript_path = "C:\example\rollout.jsonl"
  prompt = "This must never be written to the probe log"
} | ConvertTo-Json

$payloadFile = Join-Path $probeLog "payload.json"
New-Item -ItemType Directory -Force -Path $probeLog | Out-Null
$payload | Set-Content -LiteralPath $payloadFile -Encoding utf8
py -3 tools/context_continuity/probe_hooks.py --payload-file $payloadFile --log-dir $probeLog
Get-Content (Join-Path $probeLog "events.jsonl")
```

The log must contain event metadata and lengths/hashes only. It must not contain
the prompt text or transcript path.

## Isolated Codex App probe

1. Copy `fixtures/wave0/hooks.example.json` to an isolated hook layer.
2. Replace `<REPO_ROOT>` with this repository's absolute path and
   `<PROBE_LOG_DIR>` with a user-private temporary evidence directory.
3. Do not overwrite `~/.codex/hooks.json` or the active user `config.toml`.
4. Start Codex with the isolated layer, review/trust the exact hook definitions,
   and run the smallest manual compact and low-threshold auto-compact cases.
5. Save the resulting `events.jsonl`, UI observations, and the active config
   summary under `tests/context-continuity/evidence/<date>/W0-*`.

The probe intentionally has no `continue:false` output. It cannot change the
compact result and therefore cannot create the `TurnAborted` failure being
verified.

## Wave 1 local continuity smoke

Wave 1 adds the first user-visible continuity path. Its isolated fixture is
`fixtures/wave1/hooks.example.json`; it invokes `session_start.py` and
`stop.py` directly, so both scripts support direct-file execution as well as
`python -m` execution.

Before starting an isolated Codex CLI/App process, set a private state root in
that process environment, for example:

```powershell
$env:CODEX_CONTEXT_CONTINUITY_HOME = Join-Path ([IO.Path]::GetTempPath()) "codex-context-continuity-wave1"
```

Then replace `<REPO_ROOT>` in the fixture, review/trust the exact hooks, and
run a two-turn task. At the first turn's stop, the Hook should request one
checkpoint seal. After writing the concise `checkpoint.md` at the injected
path and running the injected seal command, the same turn may end. A later
`SessionStart(source=compact)` should inject that checkpoint.

Wave 1 intentionally has no `PreToolUse` dirty tracking and no `PreCompact`
Hook. A mid-turn automatic compact before the first seal remains a known Wave 1
gap and is covered by Wave 2.

## Wave 2 non-blocking compact recovery smoke

Wave 2 adds `fixtures/wave2/hooks.example.json`. It keeps `PreCompact` non-
blocking: a dirty turn writes only `emergency.md` and compact is allowed to
finish. `PostCompact` records the host completion, and
`SessionStart(source=compact)` injects the last sealed checkpoint plus the
bounded emergency manifest, then marks the session `recovery_required` until a
new checkpoint is sealed. If a compacting session re-enters through startup or
resume without the compact source, it is marked `compact_failed` and the last
safe checkpoint is retained.

The manifest is mechanical only (turn/tool/transcript pointers, workspace,
sequence and trigger); it must not contain prompt text, transcript contents or
credentials. Neither `PreCompact` nor `PostCompact` returns `continue:false`.
Use the fixture only with a private `CODEX_CONTEXT_CONTINUITY_HOME` and an
isolated Codex App configuration; do not install it into the active user hook
layer before host smoke evidence is captured.

## Evidence requirements

At minimum capture:

- `SessionStart` sources: `startup`, `resume`, and `compact` when available;
- `PreCompact` and `PostCompact` trigger values: `manual` and `auto`;
- `Stop` payload presence and `stop_hook_active` behavior;
- whether compact completion is followed by `SessionStart(source=compact)`;
- whether a hook error or probe output changes the turn outcome.
