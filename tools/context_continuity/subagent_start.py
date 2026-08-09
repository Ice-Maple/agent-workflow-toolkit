"""Wave 3 ``SubagentStart`` hook.

Starting a subagent makes the root session dirty and records the child
identity through :func:`state.mark_subagent_started`.  The hook does not
create a second checkpoint or seal either session.
"""

from __future__ import annotations

import json
import sys
from pathlib import Path
from typing import Any, Callable

if __package__:
    try:
        from .state import mark_subagent_started
    except (ImportError, AttributeError):  # pragma: no cover - compatibility during upgrades
        mark_subagent_started = None  # type: ignore[assignment]
else:  # Allow the exact file path used by hooks.example.json.
    sys.path.insert(0, str(Path(__file__).resolve().parents[2]))
    try:
        from tools.context_continuity.state import mark_subagent_started  # type: ignore[no-redef]
    except (ImportError, AttributeError):  # pragma: no cover - compatibility during upgrades
        mark_subagent_started = None  # type: ignore[assignment]


def _payload() -> dict[str, Any]:
    try:
        raw = sys.stdin.read() or "{}"
        value = json.loads(raw.lstrip("\ufeff"))
    except (json.JSONDecodeError, UnicodeError, TypeError, OSError):
        return {}
    return value if isinstance(value, dict) else {}


def _emit(value: dict[str, Any]) -> None:
    json.dump(value, sys.stdout, ensure_ascii=True, separators=(",", ":"))
    sys.stdout.write("\n")


def _warning(message: str) -> None:
    text = str(message).replace("\r", " ").replace("\n", " ").strip()[:240]
    _emit({"systemMessage": text or "Context continuity SubagentStart unavailable; allowing the subagent."})


def _first(payload: dict[str, Any], *names: str) -> Any:
    for name in names:
        if name in payload and payload[name] is not None:
            return payload[name]
    return None


def _record(payload: dict[str, Any]) -> str | None:
    recorder: Callable[..., Any] | None = mark_subagent_started
    if recorder is None:
        return "state.mark_subagent_started is unavailable"
    agent_id = _first(payload, "agent_id", "subagent_id", "id")
    agent_type = _first(payload, "agent_type", "subagent_type", "type")
    if agent_type is None:
        agent_type = "unknown"
    try:
        result = recorder(
            payload.get("session_id"),
            payload.get("cwd"),
            payload.get("turn_id"),
            agent_id,
            agent_type,
        )
    except Exception as exc:  # A continuity hook must not interrupt subagent startup.
        return f"{type(exc).__name__}: {exc}"
    if isinstance(result, tuple) and len(result) >= 3 and result[2]:
        return str(result[2])
    return None


def main() -> int:
    payload = _payload()
    error = _record(payload)
    if error:
        _warning(f"Context continuity SubagentStart unavailable; allowing the subagent: {error}")
    else:
        _emit({})
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
