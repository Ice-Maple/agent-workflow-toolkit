"""Wave 1 SessionStart hook: locate and inject a compact checkpoint."""

from __future__ import annotations

import json
import sys
from pathlib import Path
from typing import Any

if __package__:
    from .state import checkpoint_content, checkpoint_path, ensure_session
else:  # Allow the exact file path used by hooks.example.json.
    sys.path.insert(0, str(Path(__file__).resolve().parents[2]))
    from tools.context_continuity.state import (  # type: ignore[no-redef]
        checkpoint_content,
        checkpoint_path,
        ensure_session,
    )


def _payload() -> dict[str, Any]:
    try:
        value = json.loads(sys.stdin.read() or "{}")
    except json.JSONDecodeError:
        return {}
    return value if isinstance(value, dict) else {}


def _emit_context(text: str) -> None:
    json.dump(
        {
            "hookSpecificOutput": {
                "hookEventName": "SessionStart",
                "additionalContext": text,
            }
        },
        sys.stdout,
        ensure_ascii=False,
    )
    sys.stdout.write("\n")


def _seal_command(session_dir: Path, *, session_id: str, cwd: str | None) -> str:
    """Build a copy/pasteable command with the interpreter running this hook."""

    parts = [sys.executable, str(Path(__file__).with_name("seal.py")), "--session-dir", str(session_dir)]
    if cwd:
        parts.extend(("--cwd", cwd))
    parts.extend(("--session-id", session_id))
    # Hook commands are shell strings. The supported App environments are
    # Windows and POSIX; double-quoting is sufficient for generated absolute
    # paths and keeps the command readable in the model context.
    return " ".join(f'"{str(part).replace(chr(34), chr(34) * 2)}"' for part in parts)


def _startup_summary(content: str) -> str:
    """Keep startup/resume injection small while exposing useful continuation hints."""

    wanted = ("goal", "next", "first incomplete", "blocked", "constraint", "纠正", "下一")
    selected: list[str] = []
    for line in content.splitlines():
        normalized = line.strip().lower()
        if normalized.startswith(("-", "*")) and any(key in normalized for key in wanted):
            selected.append(line.strip())
        if len(selected) >= 3:
            break
    return " ".join(selected)[:500]


def main() -> int:
    payload = _payload()
    try:
        session_dir, state, error = ensure_session(
            payload.get("session_id"),
            payload.get("cwd"),
            source=payload.get("source"),
        )
    except (OSError, UnicodeError, RuntimeError, ValueError) as exc:
        _emit_context(f"Context continuity is unavailable: {exc}.")
        return 0
    if error or session_dir is None or state is None:
        _emit_context(f"Context continuity is unavailable: {error or 'unknown error'}.")
        return 0

    checkpoint = checkpoint_path(session_dir)
    lines = [
        "Context continuity is active.",
        f"Session state directory: {session_dir}",
        f"Checkpoint file: {checkpoint}",
        f"Seal command: {_seal_command(session_dir, session_id=payload.get('session_id', ''), cwd=payload.get('cwd'))}",
    ]
    content, checkpoint_error = checkpoint_content(session_dir, state)
    if content is not None:
        source = payload.get("source")
        if source == "compact":
            lines.extend(
                [
                    "Verify this sealed checkpoint against the current workspace before continuing:",
                    "--- checkpoint.md ---",
                    content.rstrip(),
                    "--- end checkpoint.md ---",
                    "Continue from the first incomplete step; do not repeat completed work without evidence.",
                ]
            )
        else:
            lines.append(
                f"A sealed checkpoint is available (sequence {state.get('checkpoint_seq')}); read it only if needed."
            )
            summary = _startup_summary(content)
            if summary:
                lines.append(f"Continuation hints: {summary}")
    elif checkpoint.exists() and checkpoint_error:
        lines.append(f"Checkpoint was not injected: {checkpoint_error}. Preserve the last safe state and inspect before continuing.")
    else:
        lines.append("No sealed checkpoint exists yet. At the end of this turn, write a concise checkpoint and run seal.")

    _emit_context("\n".join(lines))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
