"""Wave 3 ``PreToolUse`` hook.

The hook is intentionally observational: it records the tool invocation in
the continuity state and never asks the host to block or continue a call.  A
missing field, malformed payload, state error, or persistence exception is a
warning at most (fail-open).
"""

from __future__ import annotations

import json
import sys
from pathlib import Path
from typing import Any, Callable

if __package__:
    try:
        from .state import record_tool_use
    except (ImportError, AttributeError):  # pragma: no cover - compatibility during upgrades
        record_tool_use = None  # type: ignore[assignment]
    from .tool_policy import classify_tool
else:  # Allow the exact file path used by hooks.example.json.
    sys.path.insert(0, str(Path(__file__).resolve().parents[2]))
    try:
        from tools.context_continuity.state import record_tool_use  # type: ignore[no-redef]
    except (ImportError, AttributeError):  # pragma: no cover - compatibility during upgrades
        record_tool_use = None  # type: ignore[assignment]
    from tools.context_continuity.tool_policy import classify_tool  # type: ignore[no-redef]


def _payload() -> dict[str, Any]:
    try:
        raw = sys.stdin.read() or "{}"
        value = json.loads(raw.lstrip("\ufeff"))
    except (json.JSONDecodeError, UnicodeError, TypeError, OSError):
        return {}
    return value if isinstance(value, dict) else {}


def _emit(value: dict[str, Any]) -> None:
    json.dump(value, sys.stdout, ensure_ascii=False, separators=(",", ":"))
    sys.stdout.write("\n")


def _warning(message: str) -> None:
    # Keep hook output bounded and free of newlines.  ``systemMessage`` is
    # informational and does not carry an unsupported decision/continue key.
    text = str(message).replace("\r", " ").replace("\n", " ").strip()[:240]
    _emit({"systemMessage": text or "Context continuity PreToolUse unavailable; allowing the tool call."})


def _record(payload: dict[str, Any]) -> str | None:
    tool_name = payload.get("tool_name", payload.get("name"))
    try:
        classification = classify_tool(tool_name)
        category = (
            classification.get("category", "unknown")
            if isinstance(classification, dict)
            else getattr(classification, "category", "unknown")
        )
    except Exception as exc:  # Classification is diagnostic and must also fail open.
        return f"{type(exc).__name__}: {exc}"
    recorder: Callable[..., Any] | None = record_tool_use
    if recorder is None:
        return "state.record_tool_use is unavailable"
    try:
        result = recorder(
            payload.get("session_id"),
            payload.get("cwd"),
            payload.get("turn_id"),
            tool_name,
            category,
            tool_use_id=payload.get("tool_use_id"),
            tool_input=payload.get("tool_input", payload.get("input")),
        )
    except Exception as exc:  # Hooks must never turn a diagnostic failure into a blocked tool call.
        return f"{type(exc).__name__}: {exc}"
    if isinstance(result, tuple) and len(result) >= 3 and result[2]:
        return str(result[2])
    return None


def main() -> int:
    payload = _payload()
    error = _record(payload)
    if error:
        _warning(f"Context continuity PreToolUse unavailable; allowing the tool call: {error}")
    else:
        _emit({})
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
