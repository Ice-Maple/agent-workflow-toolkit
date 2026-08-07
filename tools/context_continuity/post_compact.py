"""Wave 2 PostCompact hook: record that the host reported compaction complete."""

from __future__ import annotations

import json
import sys
from datetime import datetime, timezone
from pathlib import Path
from typing import Any

if __package__:
    from .state import append_event, digest, ensure_session, write_state
else:  # Allow the exact file path used by hooks.example.json.
    sys.path.insert(0, str(Path(__file__).resolve().parents[2]))
    from tools.context_continuity.state import append_event, digest, ensure_session, write_state  # type: ignore[no-redef]


def _payload() -> dict[str, Any]:
    try:
        value = json.loads(sys.stdin.read() or "{}")
    except (json.JSONDecodeError, UnicodeError):
        return {}
    return value if isinstance(value, dict) else {}


def _emit(value: dict[str, Any]) -> None:
    json.dump(value, sys.stdout, ensure_ascii=False, separators=(",", ":"))
    sys.stdout.write("\n")


def main() -> int:
    payload = _payload()
    trigger = payload.get("trigger")
    if trigger not in ("manual", "auto"):
        _emit({})
        return 0
    try:
        session_dir, state, error = ensure_session(
            payload.get("session_id"),
            payload.get("cwd"),
            source="postcompact",
        )
    except (OSError, UnicodeError, RuntimeError, ValueError, TypeError) as exc:
        _emit({"systemMessage": f"Context continuity PostCompact unavailable: {exc}"})
        return 0
    if error or session_dir is None or state is None:
        _emit({"systemMessage": f"Context continuity PostCompact unavailable: {error or 'unknown error'}"})
        return 0

    if state.get("status") != "compacting":
        _emit({"systemMessage": "Context continuity ignored a stale PostCompact event because no compact is in progress."})
        return 0
    expected_trigger = state.get("last_compact_trigger")
    if expected_trigger in ("manual", "auto") and expected_trigger != trigger:
        _emit({"systemMessage": "Context continuity ignored a PostCompact event whose trigger did not match PreCompact."})
        return 0
    expected_turn_hash = state.get("last_compact_turn_hash")
    actual_turn_hash = digest(payload.get("turn_id"))
    if expected_turn_hash and actual_turn_hash and expected_turn_hash != actual_turn_hash:
        _emit({"systemMessage": "Context continuity ignored a PostCompact event from a different turn."})
        return 0

    completed_at = datetime.now(timezone.utc).isoformat()
    state.update(
        {
            "last_compact_completed_at": completed_at,
            "last_compact_result": "postcompact_observed",
            "last_compact_trigger": trigger,
        }
    )
    try:
        write_state(session_dir / "state.json", state)
    except OSError as exc:
        _emit({"systemMessage": f"Context continuity could not record PostCompact: {exc}"})
        return 0
    event_error = append_event(
        session_dir,
        {
            "event": "PostCompact",
            "trigger": trigger,
            "status": state.get("status"),
            "origin_status": state.get("origin_status"),
            "turn_hash": digest(payload.get("turn_id")),
        },
    )
    if event_error:
        _emit({"systemMessage": f"Context continuity recorded PostCompact state but not its event evidence: {event_error}"})
    else:
        _emit({})
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
