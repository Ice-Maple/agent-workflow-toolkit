"""Wave 2 PreCompact hook: save a bounded mechanical recovery manifest."""

from __future__ import annotations

import json
import sys
from datetime import datetime, timezone
from pathlib import Path
from typing import Any

if __package__:
    from .state import (
        append_event,
        checkpoint_content,
        digest,
        emergency_manifest_content,
        estimate_tokens,
        ensure_session,
        MAX_EMERGENCY_ESTIMATED_TOKENS,
        mark_turn_dirty,
        write_compact_marker,
        sanitize_pointer,
        write_emergency_manifest,
        write_state,
    )
else:  # Allow the exact file path used by hooks.example.json.
    sys.path.insert(0, str(Path(__file__).resolve().parents[2]))
    from tools.context_continuity.state import (  # type: ignore[no-redef]
        append_event,
        checkpoint_content,
        digest,
        emergency_manifest_content,
        estimate_tokens,
        ensure_session,
        MAX_EMERGENCY_ESTIMATED_TOKENS,
        mark_turn_dirty,
        write_compact_marker,
        sanitize_pointer,
        write_emergency_manifest,
        write_state,
    )


def _payload() -> dict[str, Any]:
    try:
        raw = sys.stdin.read() or "{}"
        value = json.loads(raw.lstrip("\ufeff"))
    except (json.JSONDecodeError, UnicodeError):
        return {}
    return value if isinstance(value, dict) else {}


def _emit(value: dict[str, Any]) -> None:
    json.dump(value, sys.stdout, ensure_ascii=False, separators=(",", ":"))
    sys.stdout.write("\n")


def main() -> int:
    payload = _payload()
    session_id = payload.get("session_id")
    cwd = payload.get("cwd")
    turn_id = payload.get("turn_id")
    trigger = payload.get("trigger")
    if trigger not in ("manual", "auto"):
        _emit({"systemMessage": "Context continuity PreCompact received no valid trigger; allowing compact to continue."})
        return 0

    try:
        session_dir, state, error = ensure_session(session_id, cwd, source="precompact")
    except (OSError, UnicodeError, RuntimeError, ValueError, TypeError) as exc:
        _emit({"systemMessage": f"Context continuity PreCompact unavailable; allowing compact to continue: {exc}"})
        return 0
    if error or session_dir is None or state is None:
        _emit({"systemMessage": f"Context continuity PreCompact unavailable; allowing compact to continue: {error or 'unknown error'}"})
        return 0

    # Codex may deliver more than one lifecycle observation for the same
    # compact attempt. Preserve the first origin/manifest instead of turning a
    # safe attempt into a synthetic dirty one on the duplicate callback.
    if state.get("status") == "compacting":
        event_error = append_event(
            session_dir,
            {
                "event": "PreCompactDuplicate",
                "trigger": trigger,
                "status": "compacting",
                "origin_status": state.get("origin_status"),
                "turn_hash": digest(turn_id),
            },
        )
        if event_error:
            _emit({"systemMessage": f"Context continuity could not record duplicate compact observation: {event_error}"})
        else:
            _emit({})
        return 0

    # A new turn becomes dirty before compact only when it is not the last
    # successfully sealed turn. This keeps the safe boundary path manifest-free.
    if turn_id:
        session_dir, marked_state, mark_error = mark_turn_dirty(session_id, cwd, turn_id)
        if mark_error or session_dir is None or marked_state is None:
            _emit({"systemMessage": f"Context continuity could not mark the turn dirty; allowing compact to continue: {mark_error or 'unknown error'}"})
            return 0
        state = marked_state

    safe_checkpoint, safe_error = checkpoint_content(session_dir, state)
    origin_status = "safe" if state.get("status") == "safe" and safe_checkpoint is not None else "dirty"
    now = datetime.now(timezone.utc).isoformat()
    transcript_path = sanitize_pointer(payload.get("transcript_path"), 4096, always_hash=True)
    tool_name = sanitize_pointer(payload.get("tool_name") or payload.get("last_tool_name"), 256, always_hash=True)

    if origin_status == "dirty":
        generation = int(state.get("dirty_generation", 0)) + 1
        state["dirty_generation"] = generation
        if turn_id and state.get("pending_turn_hash") is None:
            state["pending_turn_hash"] = digest(turn_id)
        manifest = emergency_manifest_content(
            trigger=trigger,
            origin_status=origin_status,
            state=state,
            turn_id=turn_id,
            transcript_path=transcript_path,
            workspace_name=str(state.get("workspace_name", "unknown")),
            tool_name=tool_name,
        )
        if estimate_tokens(manifest) > MAX_EMERGENCY_ESTIMATED_TOKENS:
            # The builder normally hashes long/untrusted pointers until this
            # cannot happen. Keep a final tiny capsule rather than silently
            # abandoning the dirty recovery path.
            manifest = (
                "# Emergency manifest\n"
                f"- Trigger: {trigger}\n"
                f"- Origin: {origin_status}\n"
                f"- Turn: {digest(turn_id) or 'unknown'}\n"
                "- Recovery: verify workspace and artifacts before continuing.\n"
            )
        try:
            write_emergency_manifest(session_dir, manifest)
        except OSError as exc:
            try:
                write_compact_marker(
                    session_dir,
                    {
                        "status": "manifest_write_failed",
                        "trigger": trigger,
                        "origin_status": origin_status,
                        "turn_hash": digest(turn_id),
                        "started_at": now,
                        "error": str(exc)[:240],
                    },
                )
            except OSError:
                pass
            _emit({"systemMessage": f"Context continuity could not write the emergency manifest; allowing compact to continue: {exc}"})
            return 0
        marker = {
            "status": "state_write_pending",
            "trigger": trigger,
            "origin_status": origin_status,
            "turn_hash": digest(turn_id),
            "started_at": now,
            "manifest_hash": digest(manifest),
            "manifest_chars": len(manifest),
            "manifest_estimated_tokens": estimate_tokens(manifest),
        }
        state.update(
            {
                "status": "compacting",
                "origin_status": "dirty",
                "emergency_manifest_hash": digest(manifest),
                "emergency_manifest_chars": len(manifest),
                "emergency_manifest_estimated_tokens": estimate_tokens(manifest),
            }
        )
    else:
        marker = {
            "status": "state_write_pending",
            "trigger": trigger,
            "origin_status": "safe",
            "turn_hash": digest(turn_id),
            "started_at": now,
            "manifest_hash": None,
            "manifest_chars": 0,
            "manifest_estimated_tokens": 0,
        }
        state.update(
            {
                "status": "compacting",
                "origin_status": "safe",
                "emergency_manifest_hash": None,
                "emergency_manifest_chars": 0,
                "emergency_manifest_estimated_tokens": 0,
            }
        )
    state.update(
        {
            "last_compact_trigger": trigger,
            "last_compact_turn_hash": digest(turn_id),
            "last_compact_started_at": now,
            "last_compact_completed_at": None,
            "last_compact_result": "started",
            "transcript_path": transcript_path,
            "last_tool_name": tool_name,
        }
    )
    try:
        write_compact_marker(session_dir, marker)
    except OSError as exc:
        # The state path remains the primary recovery record; surface the loss
        # of this fallback marker without blocking host compaction.
        marker_error = str(exc)
    else:
        marker_error = None
    try:
        write_state(session_dir / "state.json", state)
    except OSError as exc:
        _emit({"systemMessage": f"Context continuity could not record compact state; allowing compact to continue: {exc}"})
        return 0
    event_error = append_event(
        session_dir,
        {
            "event": "PreCompact",
            "trigger": trigger,
            "origin_status": origin_status,
            "status": "compacting",
            "turn_hash": digest(turn_id),
            "manifest_hash": state.get("emergency_manifest_hash"),
            "manifest_estimated_tokens": state.get("emergency_manifest_estimated_tokens", 0),
        },
    )

    # Deliberately omit continue:false: context-limit compaction must be allowed
    # to finish so SessionStart(source=compact) can restore the state.
    if marker_error or event_error:
        _emit(
            {
                "systemMessage": (
                    "Context continuity recorded compact state but some diagnostic evidence failed "
                    f"(marker={marker_error or 'ok'}, event={event_error or 'ok'}); compact remains allowed."
                )
            }
        )
    elif origin_status == "dirty" and trigger == "manual":
        _emit({"systemMessage": "Context continuity saved a small dirty recovery manifest; compact is allowed, and recovery will be required afterward."})
    else:
        _emit({})
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
