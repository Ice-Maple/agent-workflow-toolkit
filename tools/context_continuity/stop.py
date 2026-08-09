"""Wave 1 Stop hook: request one checkpoint continuation per turn."""

from __future__ import annotations

import json
import sys
from pathlib import Path
from typing import Any

if __package__:
    from .state import checkpoint_content, mark_turn_dirty
else:  # Allow the exact file path used by hooks.example.json.
    sys.path.insert(0, str(Path(__file__).resolve().parents[2]))
    from tools.context_continuity.state import checkpoint_content, mark_turn_dirty  # type: ignore[no-redef]


def _payload() -> dict[str, Any]:
    try:
        raw = sys.stdin.read() or "{}"
        value = json.loads(raw.lstrip("\ufeff"))
    except (json.JSONDecodeError, UnicodeError):
        return {}
    return value if isinstance(value, dict) else {}


def _emit(value: dict[str, Any]) -> None:
    json.dump(value, sys.stdout, ensure_ascii=True)
    sys.stdout.write("\n")


def _seal_command(session_dir: Path, *, session_id: str, cwd: str | None) -> str:
    parts = [sys.executable, str(Path(__file__).with_name("seal.py")), "--session-dir", str(session_dir)]
    if cwd:
        parts.extend(("--cwd", cwd))
    parts.extend(("--session-id", session_id))
    return " ".join(f'"{str(part).replace(chr(34), chr(34) * 2)}"' for part in parts)


def main() -> int:
    payload = _payload()
    try:
        session_dir, state, error = mark_turn_dirty(
            payload.get("session_id"),
            payload.get("cwd"),
            payload.get("turn_id"),
        )
    except (OSError, UnicodeError, RuntimeError, ValueError, TypeError) as exc:
        _emit({"systemMessage": f"Context continuity check unavailable; allowing turn to end: {exc}"})
        return 0
    # A diagnostic/continuity hook must fail open if the host omits fields or
    # the state directory cannot be opened; it must not break a user turn.
    if error or session_dir is None or state is None or not payload.get("turn_id"):
        _emit({})
        return 0

    safe_content, _ = checkpoint_content(session_dir, state)
    if state.get("status") == "compacting":
        _emit(
            {
                "systemMessage": (
                    "Context compact is in progress; allowing the host to finish its recovery lifecycle "
                    "before requesting another checkpoint continuation."
                )
            }
        )
        return 0
    if state.get("status") == "safe" and state.get("last_sealed_turn_hash") and safe_content is not None:
        _emit({})
        return 0

    seal_command = _seal_command(
        session_dir,
        session_id=payload.get("session_id", ""),
        cwd=payload.get("cwd"),
    )
    if payload.get("stop_hook_active") is True:
        _emit(
            {
                "continue": False,
                "stopReason": "checkpoint remains unsealed after one continuation",
                "systemMessage": (
                    "Context checkpoint is still not sealed. Ending this turn to avoid a loop; "
                    f"write checkpoint.md and run {seal_command} before the next substantive turn."
                ),
            }
        )
        return 0

    _emit(
        {
            "decision": "block",
            "reason": (
                "Write a concise checkpoint.md (and, for Wave 3, a hot.md capsule of at most 600 estimated tokens) "
                "and seal it before ending this turn. "
                "Do not repeat completed work. Run "
                f"{seal_command}"
            ),
        }
    )
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
