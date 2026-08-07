"""Minimal Wave 1 session state and checkpoint storage."""

from __future__ import annotations

import hashlib
import json
import math
import os
from pathlib import Path
import re
import subprocess
import tempfile
from datetime import datetime, timezone
from typing import Any
import unicodedata


SCHEMA_VERSION = 1
MAX_CHECKPOINT_CHARS = 2400
MAX_EMERGENCY_CHARS = 1200
MAX_HOT_CHARS = 2400
MAX_HOT_ESTIMATED_TOKENS = 600
MAX_WARM_ESTIMATED_TOKENS = 1500
MAX_EMERGENCY_ESTIMATED_TOKENS = 250
MAX_ACTIVE_SUBAGENTS = 16
MAX_TOOL_NAME_CHARS = 256
TOOL_CATEGORIES = {"write", "read_evidence", "state", "unknown"}
VALID_STATUSES = {
    "dirty",
    "safe",
    "compacting",
    "recovery_required",
    "compact_failed",
}
_DIGEST_RE = re.compile(r"^[0-9a-f]{16}$")
SECRET_PATTERNS = (
    re.compile(r"-----BEGIN [A-Z ]+ PRIVATE KEY-----"),
    re.compile(r"\bBearer\s+[A-Za-z0-9._~+/=-]{12,}", re.IGNORECASE),
    re.compile(
        r"\b(?:api[_-]?key|password|passwd|secret|token|credential|client[_-]?secret)\s*[:=]\s*\S+",
        re.IGNORECASE,
    ),
    re.compile(r"\b(?:sk|ghp|github_pat|xox[baprs])-[-_A-Za-z0-9]{12,}"),
    re.compile(r"\bAKIA[0-9A-Z]{16}\b"),
)


def digest(value: Any) -> str | None:
    if value is None:
        return None
    text = str(value)
    if not text:
        return None
    return hashlib.sha256(text.encode("utf-8")).hexdigest()[:16]


def context_home() -> Path:
    configured = os.environ.get("CODEX_CONTEXT_CONTINUITY_HOME")
    if configured:
        return Path(configured).expanduser()
    return Path.home() / ".codex" / "context-continuity" / "sessions"


def workspace_root(cwd: str | Path | None) -> Path:
    candidate = Path(cwd or Path.cwd()).expanduser().resolve()
    try:
        result = subprocess.run(
            ["git", "-C", str(candidate), "rev-parse", "--show-toplevel"],
            capture_output=True,
            check=False,
            text=True,
            timeout=2,
        )
    except (OSError, subprocess.SubprocessError):
        result = None
    if result and result.returncode == 0 and result.stdout.strip():
        return Path(result.stdout.strip()).resolve()
    return candidate


def session_directory(session_id: str, *, home: Path | None = None) -> Path:
    session_hash = digest(session_id)
    if session_hash is None:
        raise ValueError("session_id is required")
    return (home or context_home()) / f"session-{session_hash}"


def state_path(session_dir: Path) -> Path:
    return session_dir / "state.json"


def checkpoint_path(session_dir: Path) -> Path:
    return session_dir / "checkpoint.md"


def hot_path(session_dir: Path) -> Path:
    """Return the optional hot-state file used for compact recovery."""

    return session_dir / "hot.md"


def _atomic_write_text(path: Path, content: str) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    # Best-effort private permissions. On Windows chmod is limited, but this
    # still prevents accidental group/other access on POSIX installations.
    try:
        os.chmod(path.parent, 0o700)
    except OSError:
        pass
    temporary: Path | None = None
    try:
        with tempfile.NamedTemporaryFile(
            mode="w",
            encoding="utf-8",
            newline="\n",
            dir=path.parent,
            prefix=f".{path.name}.",
            suffix=".tmp",
            delete=False,
        ) as stream:
            temporary = Path(stream.name)
            stream.write(content)
            stream.flush()
            os.fsync(stream.fileno())
        os.replace(temporary, path)
        try:
            os.chmod(path, 0o600)
        except OSError:
            pass
    finally:
        if temporary is not None:
            try:
                temporary.unlink(missing_ok=True)
            except OSError:
                pass


def write_state(path: Path, state: dict[str, Any]) -> None:
    _atomic_write_text(path, json.dumps(state, ensure_ascii=False, indent=2) + "\n")


def write_emergency_manifest(session_dir: Path, content: str) -> None:
    _atomic_write_text(session_dir / "emergency.md", content)


def compact_marker_path(session_dir: Path) -> Path:
    return session_dir / "compact-marker.json"


def write_compact_marker(session_dir: Path, marker: dict[str, Any]) -> None:
    _atomic_write_text(
        compact_marker_path(session_dir),
        json.dumps(marker, ensure_ascii=False, separators=(",", ":")) + "\n",
    )


def read_compact_marker(session_dir: Path) -> dict[str, Any] | None:
    path = compact_marker_path(session_dir)
    try:
        value = json.loads(path.read_text(encoding="utf-8"))
    except (OSError, UnicodeError, json.JSONDecodeError):
        try:
            if path.exists():
                return {"status": "invalid_marker"}
        except OSError:
            return {"status": "invalid_marker"}
        return None
    return value if isinstance(value, dict) else None


def clear_compact_marker(session_dir: Path) -> None:
    try:
        compact_marker_path(session_dir).unlink(missing_ok=True)
    except OSError:
        pass


def read_state(session_dir: Path) -> dict[str, Any] | None:
    path = state_path(session_dir)
    try:
        if not path.exists():
            return None
        value = json.loads(path.read_text(encoding="utf-8"))
    except (OSError, UnicodeError, json.JSONDecodeError):
        return None
    return value if isinstance(value, dict) else None


def _valid_digest(value: Any, *, allow_none: bool = True) -> bool:
    if value is None:
        return allow_none
    return isinstance(value, str) and _DIGEST_RE.fullmatch(value) is not None


def validate_state(state: Any) -> str | None:
    """Return a diagnostic when a state object violates its schema/invariants."""

    if not isinstance(state, dict):
        return "state.json must contain an object"
    if state.get("schema_version") != SCHEMA_VERSION:
        return "unsupported or missing schema_version"
    for field in ("session_id_hash", "workspace_hash"):
        if not isinstance(state.get(field), str) or not _DIGEST_RE.fullmatch(state[field]):
            return f"invalid {field}"
    if not isinstance(state.get("workspace_name"), str) or not state["workspace_name"]:
        return "invalid workspace_name"
    if state.get("status") not in VALID_STATUSES:
        return "invalid status"
    if (
        not isinstance(state.get("checkpoint_seq"), int)
        or isinstance(state.get("checkpoint_seq"), bool)
        or state["checkpoint_seq"] < 0
    ):
        return "invalid checkpoint_seq"
    if (
        not isinstance(state.get("checkpoint_chars"), int)
        or isinstance(state.get("checkpoint_chars"), bool)
        or not 0 <= state["checkpoint_chars"] <= MAX_CHECKPOINT_CHARS
    ):
        return "invalid checkpoint_chars"
    for field in ("checkpoint_hash", "last_sealed_turn_hash", "pending_turn_hash", "last_compact_turn_hash"):
        value = state.get(field)
        if value is not None and not _valid_digest(value, allow_none=False):
            return f"invalid {field}"
    if state.get("checkpoint_hash") is None and state["checkpoint_chars"] != 0:
        return "checkpoint_chars must be zero without checkpoint_hash"
    if state.get("status") == "safe":
        if (
            state.get("checkpoint_seq", 0) < 1
            or state.get("checkpoint_hash") is None
            or state.get("checkpoint_chars", 0) <= 0
            or state.get("last_sealed_turn_hash") is None
            or state.get("pending_turn_hash") is not None
        ):
            return "safe state is missing sealed checkpoint invariants"
    if state.get("last_source") is not None and not isinstance(state.get("last_source"), str):
        return "invalid last_source"
    if state.get("last_sealed_at") is not None and not isinstance(state.get("last_sealed_at"), str):
        return "invalid last_sealed_at"
    if (
        "dirty_generation" in state
        and (
            not isinstance(state.get("dirty_generation"), int)
            or isinstance(state.get("dirty_generation"), bool)
            or state["dirty_generation"] < 0
        )
    ):
        return "invalid dirty_generation"
    if state.get("origin_status") not in (None, "safe", "dirty"):
        return "invalid origin_status"
    if state.get("origin_status") == "safe" and state.get("pending_turn_hash") is not None:
        return "safe compact origin cannot have a pending turn"
    if state.get("status") == "recovery_required" and state.get("origin_status") not in (None, "dirty"):
        return "recovery_required must originate from dirty state"
    if state.get("last_compact_trigger") not in (None, "manual", "auto"):
        return "invalid last_compact_trigger"
    for field in (
        "last_tool_name",
        "last_tool_category",
        "last_tool_use_hash",
        "last_tool_input_hash",
        "last_dirty_reason",
        "transcript_path",
        "last_compact_started_at",
        "last_compact_completed_at",
        "last_compact_result",
        "last_subagent_id_hash",
        "last_subagent_type",
        "last_subagent_status",
        "last_subagent_message_hash",
    ):
        if state.get(field) is not None and not isinstance(state.get(field), str):
            return f"invalid {field}"
    if state.get("last_tool_name") is not None and len(state["last_tool_name"]) > MAX_TOOL_NAME_CHARS:
        return "last_tool_name is too long"
    if state.get("last_tool_category") not in (None, *TOOL_CATEGORIES):
        return "invalid last_tool_category"
    for field in ("last_tool_use_hash", "last_tool_input_hash", "last_subagent_id_hash", "last_subagent_message_hash"):
        if state.get(field) is not None and not _valid_digest(state.get(field), allow_none=False):
            return f"invalid {field}"
    if state.get("transcript_path") is not None and len(state["transcript_path"]) > 4096:
        return "transcript_path is too long"
    hot_chars = state.get("hot_chars", 0)
    if not isinstance(hot_chars, int) or isinstance(hot_chars, bool) or not 0 <= hot_chars <= MAX_HOT_CHARS:
        return "invalid hot_chars"
    hot_tokens = state.get("hot_estimated_tokens", 0)
    if (
        not isinstance(hot_tokens, int)
        or isinstance(hot_tokens, bool)
        or not 0 <= hot_tokens <= MAX_HOT_ESTIMATED_TOKENS
    ):
        return "invalid hot_estimated_tokens"
    warm_tokens = state.get("warm_estimated_tokens", 0)
    if (
        not isinstance(warm_tokens, int)
        or isinstance(warm_tokens, bool)
        or not 0 <= warm_tokens <= MAX_WARM_ESTIMATED_TOKENS
    ):
        return "invalid warm_estimated_tokens"
    if state.get("hot_hash") is not None and not _valid_digest(state.get("hot_hash"), allow_none=False):
        return "invalid hot_hash"
    if state.get("hot_hash") is None and (hot_chars != 0 or hot_tokens != 0):
        return "hot metadata is inconsistent"
    active_subagents = state.get("active_subagents", [])
    if not isinstance(active_subagents, list) or len(active_subagents) > MAX_ACTIVE_SUBAGENTS:
        return "invalid active_subagents"
    for item in active_subagents:
        if not isinstance(item, dict):
            return "invalid active_subagent entry"
        if not _valid_digest(item.get("agent_id_hash"), allow_none=False):
            return "invalid active_subagent agent_id_hash"
        if item.get("status") not in ("running", "unknown"):
            return "invalid active_subagent status"
        if not isinstance(item.get("agent_type", "unknown"), str) or len(item.get("agent_type", "unknown")) > 160:
            return "invalid active_subagent agent_type"
    if state.get("last_subagent_status") not in (None, "running", "completed", "unknown"):
        return "invalid last_subagent_status"
    emergency_chars = state.get("emergency_manifest_chars", 0)
    if (
        not isinstance(emergency_chars, int)
        or isinstance(emergency_chars, bool)
        or not 0 <= emergency_chars <= MAX_EMERGENCY_CHARS
    ):
        return "invalid emergency_manifest_chars"
    emergency_tokens = state.get("emergency_manifest_estimated_tokens", 0)
    if (
        not isinstance(emergency_tokens, int)
        or isinstance(emergency_tokens, bool)
        or not 0 <= emergency_tokens <= MAX_EMERGENCY_ESTIMATED_TOKENS
    ):
        return "invalid emergency_manifest_estimated_tokens"
    if state.get("emergency_manifest_hash") is not None and not _valid_digest(
        state.get("emergency_manifest_hash"), allow_none=False
    ):
        return "invalid emergency_manifest_hash"
    if state.get("emergency_manifest_hash") is None and (
        emergency_chars != 0 or emergency_tokens != 0
    ):
        return "emergency manifest metadata is inconsistent"
    return None


def checkpoint_content(
    session_dir: Path,
    state: dict[str, Any] | None = None,
    *,
    require_safe: bool = True,
) -> tuple[str | None, str | None]:
    """Read only a checkpoint that is cryptographically consistent with state."""

    return _checkpoint_content(session_dir, state, require_safe=require_safe)


def _checkpoint_content(
    session_dir: Path,
    state: dict[str, Any] | None = None,
    *,
    require_safe: bool,
) -> tuple[str | None, str | None]:
    """Internal checkpoint reader used by dirty recovery before re-sealing."""

    state = state or read_state(session_dir)
    if state is None:
        return None, "missing or invalid state.json"
    state_error = validate_state(state)
    if state_error:
        return None, state_error
    if require_safe and state.get("status") != "safe":
        return None, "checkpoint is not sealed"
    checkpoint = checkpoint_path(session_dir)
    try:
        content = checkpoint.read_text(encoding="utf-8-sig")
    except (OSError, UnicodeError) as exc:
        return None, f"cannot read checkpoint: {exc}"
    if not content.strip():
        return None, "checkpoint is empty"
    if len(content) > MAX_CHECKPOINT_CHARS:
        return None, f"checkpoint exceeds {MAX_CHECKPOINT_CHARS} characters"
    estimated_tokens = estimate_tokens(content)
    if estimated_tokens > MAX_WARM_ESTIMATED_TOKENS:
        return None, f"checkpoint exceeds {MAX_WARM_ESTIMATED_TOKENS} estimated tokens"
    recorded_tokens = state.get("warm_estimated_tokens", 0)
    if recorded_tokens not in (0, estimated_tokens):
        return None, "checkpoint token estimate does not match sealed state"
    if state.get("checkpoint_chars") != len(content):
        return None, "checkpoint length does not match sealed state"
    if state.get("checkpoint_hash") != digest(content):
        return None, "checkpoint hash does not match sealed state"
    if has_secret(content):
        return None, "checkpoint looks like it contains a secret"
    return content, None


def hot_content(
    session_dir: Path,
    state: dict[str, Any] | None = None,
    *,
    require_safe: bool = True,
) -> tuple[str | None, str | None]:
    """Read and verify the small hot capsule, if one was sealed."""

    state = state or read_state(session_dir)
    if state is None:
        return None, "missing or invalid state.json"
    state_error = validate_state(state)
    if state_error:
        return None, state_error
    if require_safe and state.get("status") != "safe":
        return None, "hot state is not sealed"
    path = hot_path(session_dir)
    try:
        content = path.read_text(encoding="utf-8-sig")
    except (OSError, UnicodeError) as exc:
        return None, f"cannot read hot state: {exc}"
    if not content.strip():
        return None, "hot state is empty"
    if len(content) > MAX_HOT_CHARS:
        return None, f"hot state exceeds {MAX_HOT_CHARS} characters"
    estimated_tokens = estimate_tokens(content)
    if estimated_tokens > MAX_HOT_ESTIMATED_TOKENS:
        return None, f"hot state exceeds {MAX_HOT_ESTIMATED_TOKENS} estimated tokens"
    if has_secret(content):
        return None, "hot state looks like it contains a secret"
    if state.get("hot_hash") != digest(content):
        return None, "hot state hash does not match sealed state"
    if state.get("hot_chars") != len(content):
        return None, "hot state length does not match sealed state"
    if state.get("hot_estimated_tokens") != estimated_tokens:
        return None, "hot state token estimate does not match sealed state"
    return content, None


def identity_for(cwd: str | Path | None) -> tuple[str, str]:
    root = workspace_root(cwd)
    return digest(str(root)) or "", root.name or str(root)


def _normalize_wave3_state(state: dict[str, Any]) -> bool:
    """Add Wave 3 fields to Wave 1/2 state without invalidating old sessions."""

    defaults: dict[str, Any] = {
        "last_dirty_reason": None,
        "last_tool_category": None,
        "last_tool_use_hash": None,
        "last_tool_input_hash": None,
        "hot_hash": None,
        "hot_chars": 0,
        "hot_estimated_tokens": 0,
        "warm_estimated_tokens": 0,
        "active_subagents": [],
        "last_subagent_id_hash": None,
        "last_subagent_type": None,
        "last_subagent_status": None,
        "last_subagent_message_hash": None,
    }
    changed = False
    for key, value in defaults.items():
        if key not in state:
            state[key] = list(value) if isinstance(value, list) else value
            changed = True
    return changed


def ensure_session(
    session_id: str | None,
    cwd: str | Path | None,
    *,
    source: str | None = None,
) -> tuple[Path | None, dict[str, Any] | None, str | None]:
    """Open or initialize a session, failing closed on identity mismatch."""

    if not isinstance(session_id, str) or not session_id:
        return None, None, "missing session_id"
    if cwd is not None and not isinstance(cwd, (str, Path)):
        return None, None, "invalid cwd"
    if source is not None and not isinstance(source, str):
        return None, None, "invalid source"
    try:
        session_dir = session_directory(session_id)
    except ValueError as exc:
        return None, None, str(exc)

    try:
        session_dir.mkdir(parents=True, exist_ok=True)
        os.chmod(session_dir, 0o700)
        session_hash = digest(session_id)
        workspace_hash, workspace_name = identity_for(cwd)
    except (OSError, RuntimeError, ValueError, TypeError) as exc:
        return session_dir, None, f"cannot open session state: {exc}"
    try:
        existing_state = state_path(session_dir).exists()
        state = read_state(session_dir)
    except OSError as exc:
        return session_dir, None, f"cannot read session state: {exc}"
    if state is None:
        if existing_state:
            return session_dir, None, "state.json is missing, invalid, or unreadable"
        state = {
            "schema_version": SCHEMA_VERSION,
            "session_id_hash": session_hash,
            "workspace_hash": workspace_hash,
            "workspace_name": workspace_name,
            "status": "dirty",
            "checkpoint_seq": 0,
            "checkpoint_hash": None,
            "checkpoint_chars": 0,
            "last_sealed_turn_hash": None,
            "pending_turn_hash": None,
            "last_source": source,
            "last_sealed_at": None,
            "dirty_generation": 0,
            "last_dirty_reason": None,
            "last_tool_category": None,
            "last_tool_use_hash": None,
            "last_tool_input_hash": None,
            "origin_status": None,
            "last_tool_name": None,
            "transcript_path": None,
            "hot_hash": None,
            "hot_chars": 0,
            "hot_estimated_tokens": 0,
            "warm_estimated_tokens": 0,
            "emergency_manifest_hash": None,
            "emergency_manifest_chars": 0,
            "emergency_manifest_estimated_tokens": 0,
            "last_compact_trigger": None,
            "last_compact_turn_hash": None,
            "last_compact_started_at": None,
            "last_compact_completed_at": None,
            "last_compact_result": None,
            "active_subagents": [],
            "last_subagent_id_hash": None,
            "last_subagent_type": None,
            "last_subagent_status": None,
            "last_subagent_message_hash": None,
        }
        try:
            write_state(state_path(session_dir), state)
        except OSError as exc:
            return session_dir, None, f"cannot initialize state.json: {exc}"
        return session_dir, state, None

    if state.get("session_id_hash") != session_hash:
        return session_dir, state, "session identity mismatch"
    if state.get("workspace_hash") != workspace_hash:
        return session_dir, state, "workspace identity mismatch"
    state_changed = _normalize_wave3_state(state)
    state_error = validate_state(state)
    if state_error:
        return session_dir, state, f"invalid state.json: {state_error}"

    if source is not None and state.get("last_source") != source:
        state["last_source"] = source
        state_changed = True
    if state_changed:
        try:
            write_state(state_path(session_dir), state)
        except OSError as exc:
            return session_dir, state, f"cannot update state.json: {exc}"
    return session_dir, state, None


def mark_turn_dirty(
    session_id: str | None,
    cwd: str | Path | None,
    turn_id: str | None,
) -> tuple[Path | None, dict[str, Any] | None, str | None]:
    session_dir, state, error = ensure_session(session_id, cwd)
    if error or session_dir is None or state is None:
        return session_dir, state, error
    if not isinstance(turn_id, str) or not turn_id:
        return session_dir, state, "missing turn_id"
    turn_hash = digest(turn_id)
    if state.get("status") == "compacting" and state.get("origin_status") == "safe":
        # A Stop callback can race the compact lifecycle. Safe-origin compact
        # must not acquire a synthetic pending dirty turn before its compact
        # SessionStart arrives.
        return session_dir, state, None
    if state.get("status") in ("compacting", "recovery_required", "compact_failed"):
        # Do not let a Stop/PreCompact callback erase the recovery obligation.
        # A pending turn may still be attached so the next seal has an exact
        # identity, but the state remains visibly non-safe until reconciliation.
        if state.get("pending_turn_hash") is None:
            state["pending_turn_hash"] = turn_hash
            try:
                write_state(state_path(session_dir), state)
            except OSError as exc:
                return session_dir, state, f"cannot update state.json: {exc}"
        return session_dir, state, None
    safe_checkpoint_missing = False
    if state.get("status") == "safe":
        safe_checkpoint_missing = checkpoint_content(session_dir, state)[0] is None
    if state.get("last_sealed_turn_hash") != turn_hash or safe_checkpoint_missing:
        was_same_dirty_turn = (
            state.get("status") == "dirty"
            and state.get("pending_turn_hash") == turn_hash
        )
        state["status"] = "dirty"
        state["pending_turn_hash"] = turn_hash
        state["last_dirty_reason"] = "turn"
        if not was_same_dirty_turn:
            state["dirty_generation"] = int(state.get("dirty_generation", 0)) + 1
        try:
            write_state(state_path(session_dir), state)
        except OSError as exc:
            return session_dir, state, f"cannot update state.json: {exc}"
    return session_dir, state, None


def _tool_input_digest(tool_input: Any) -> str | None:
    """Hash tool arguments without persisting their potentially sensitive body."""

    if tool_input is None:
        return None
    try:
        serialized = json.dumps(tool_input, ensure_ascii=False, sort_keys=True, separators=(",", ":"))
    except (TypeError, ValueError):
        serialized = str(tool_input)
    return digest(serialized)


def _safe_tool_name(tool_name: Any) -> str:
    value = sanitize_pointer(tool_name, MAX_TOOL_NAME_CHARS, always_hash=False)
    return value or "unknown"


def _persist_tool_event(
    session_dir: Path,
    *,
    tool_name: str,
    category: str,
    marks_dirty: bool,
    state: dict[str, Any],
    turn_id: str | None,
    tool_use_id: str | None,
) -> str | None:
    return append_event(
        session_dir,
        {
            "event": "PreToolUse",
            "tool_name": tool_name,
            "category": category,
            "marks_dirty": marks_dirty,
            "status": state.get("status"),
            "dirty_generation": state.get("dirty_generation", 0),
            "turn_hash": digest(turn_id),
            "tool_use_hash": digest(tool_use_id),
        },
    )


def record_tool_use(
    session_id: str | None,
    cwd: str | Path | None,
    turn_id: str | None,
    tool_name: str | None,
    category: str,
    *,
    tool_use_id: str | None = None,
    tool_input: Any = None,
) -> tuple[Path | None, dict[str, Any] | None, str | None]:
    """Record a PreToolUse observation and conservatively mark the root dirty."""

    session_dir, state, error = ensure_session(session_id, cwd, source="pretooluse")
    if error or session_dir is None or state is None:
        return session_dir, state, error
    if not isinstance(turn_id, str) or not turn_id:
        return session_dir, state, "missing turn_id"
    if category not in TOOL_CATEGORIES:
        return session_dir, state, "invalid tool category"

    normalized_name = _safe_tool_name(tool_name)
    turn_hash = digest(turn_id)
    operation_name = re.split(r"(?:__|[.:/\\])", normalized_name.lower())[-1]
    marks_dirty = not (category == "state" and operation_name == "inspect")
    state_changed = False

    # A PreToolUse callback can race a compact callback. Preserve the compact
    # recovery obligation, but do not manufacture a new dirty turn for a
    # safe-origin compact before SessionStart(source=compact) arrives.
    if marks_dirty and not (state.get("status") == "compacting" and state.get("origin_status") == "safe"):
        if state.get("status") not in ("compacting", "recovery_required", "compact_failed"):
            if state.get("status") != "dirty" or state.get("pending_turn_hash") != turn_hash:
                state["status"] = "dirty"
                state["pending_turn_hash"] = turn_hash
                state["dirty_generation"] = int(state.get("dirty_generation", 0)) + 1
                state_changed = True
        elif state.get("pending_turn_hash") is None:
            state["pending_turn_hash"] = turn_hash
            state_changed = True

        state["last_dirty_reason"] = f"tool:{normalized_name}"
        state_changed = True

    state["last_tool_name"] = normalized_name
    state["last_tool_category"] = category
    state["last_tool_use_hash"] = digest(tool_use_id)
    state["last_tool_input_hash"] = _tool_input_digest(tool_input)
    state_changed = True
    if state_changed:
        try:
            write_state(state_path(session_dir), state)
        except OSError as exc:
            return session_dir, state, f"cannot record tool state: {exc}"
    event_error = _persist_tool_event(
        session_dir,
        tool_name=normalized_name,
        category=category,
        marks_dirty=marks_dirty,
        state=state,
        turn_id=turn_id,
        tool_use_id=tool_use_id,
    )
    if event_error:
        return session_dir, state, f"tool state recorded but event evidence failed: {event_error}"
    return session_dir, state, None


def _safe_agent_type(agent_type: Any) -> str:
    value = sanitize_pointer(agent_type, 160, always_hash=False)
    return value or "unknown"


def _active_subagent_index(state: dict[str, Any], agent_hash: str) -> int | None:
    for index, item in enumerate(state.get("active_subagents", [])):
        if isinstance(item, dict) and item.get("agent_id_hash") == agent_hash:
            return index
    return None


def _record_subagent_event(
    session_dir: Path,
    *,
    event: str,
    state: dict[str, Any],
    turn_id: str | None,
    agent_id: str | None,
    agent_type: str,
    message: str | None = None,
) -> str | None:
    return append_event(
        session_dir,
        {
            "event": event,
            "status": state.get("status"),
            "dirty_generation": state.get("dirty_generation", 0),
            "turn_hash": digest(turn_id),
            "agent_id_hash": digest(agent_id),
            "agent_type": agent_type,
            "agent_message_hash": digest(message),
            "agent_message_chars": len(message) if isinstance(message, str) else 0,
            "active_subagent_count": len(state.get("active_subagents", [])),
        },
    )


def mark_subagent_started(
    session_id: str | None,
    cwd: str | Path | None,
    turn_id: str | None,
    agent_id: str | None,
    agent_type: str | None,
) -> tuple[Path | None, dict[str, Any] | None, str | None]:
    """Mark the root session dirty when a subagent starts."""

    session_dir, state, error = ensure_session(session_id, cwd, source="subagent_start")
    if error or session_dir is None or state is None:
        return session_dir, state, error
    if not isinstance(turn_id, str) or not turn_id:
        return session_dir, state, "missing turn_id"
    if not isinstance(agent_id, str) or not agent_id:
        return session_dir, state, "missing agent_id"
    agent_hash = digest(agent_id)
    if agent_hash is None:
        return session_dir, state, "missing agent_id"
    active = state.setdefault("active_subagents", [])
    existing_index = _active_subagent_index(state, agent_hash)
    if existing_index is None:
        if len(active) >= MAX_ACTIVE_SUBAGENTS:
            return session_dir, state, f"active subagent limit {MAX_ACTIVE_SUBAGENTS} exceeded"
        active.append(
            {
                "agent_id_hash": agent_hash,
                "agent_type": _safe_agent_type(agent_type),
                "status": "running",
                "turn_hash": digest(turn_id),
                "started_at": datetime.now(timezone.utc).isoformat(),
            }
        )
    elif active[existing_index].get("status") == "unknown":
        # A later start for the same id is the explicit lifecycle evidence
        # needed to reconcile an earlier orphan stop. A duplicate stop alone
        # must not clear the unknown entry.
        active[existing_index].update(
            {
                "agent_type": _safe_agent_type(agent_type),
                "status": "running",
                "turn_hash": digest(turn_id),
                "started_at": datetime.now(timezone.utc).isoformat(),
            }
        )
    if state.get("status") not in ("compacting", "recovery_required", "compact_failed"):
        if state.get("status") != "dirty" or state.get("pending_turn_hash") != digest(turn_id):
            state["dirty_generation"] = int(state.get("dirty_generation", 0)) + 1
        state["status"] = "dirty"
        state["pending_turn_hash"] = digest(turn_id)
    state["last_dirty_reason"] = "subagent:start"
    state["last_subagent_id_hash"] = agent_hash
    state["last_subagent_type"] = _safe_agent_type(agent_type)
    state["last_subagent_status"] = "running"
    try:
        write_state(state_path(session_dir), state)
    except OSError as exc:
        return session_dir, state, f"cannot record subagent start: {exc}"
    event_error = _record_subagent_event(
        session_dir,
        event="SubagentStart",
        state=state,
        turn_id=turn_id,
        agent_id=agent_id,
        agent_type=_safe_agent_type(agent_type),
    )
    return session_dir, state, f"subagent event evidence failed: {event_error}" if event_error else None


def mark_subagent_stopped(
    session_id: str | None,
    cwd: str | Path | None,
    turn_id: str | None,
    agent_id: str | None,
    agent_type: str | None,
    last_assistant_message: str | None = None,
) -> tuple[Path | None, dict[str, Any] | None, str | None]:
    """Record a subagent stop; only the root agent can later seal the state."""

    session_dir, state, error = ensure_session(session_id, cwd, source="subagent_stop")
    if error or session_dir is None or state is None:
        return session_dir, state, error
    if not isinstance(turn_id, str) or not turn_id:
        return session_dir, state, "missing turn_id"
    if not isinstance(agent_id, str) or not agent_id:
        return session_dir, state, "missing agent_id"
    agent_hash = digest(agent_id)
    if agent_hash is None:
        return session_dir, state, "missing agent_id"
    index = _active_subagent_index(state, agent_hash)
    if index is not None and state["active_subagents"][index].get("status") == "running":
        state["active_subagents"].pop(index)
        stop_status = "completed"
    elif index is not None:
        # Repeated orphan stops remain unknown until a matching start/stop
        # sequence explicitly re-establishes the child lifecycle.
        stop_status = "unknown"
    else:
        # An orphan/foreign stop is evidence of an unknown lifecycle; do not
        # turn it into a false safe state. Keep a bounded unknown entry so a
        # later, unrelated child completion cannot overwrite this warning.
        stop_status = "unknown"
        if len(state["active_subagents"]) >= MAX_ACTIVE_SUBAGENTS:
            return session_dir, state, f"active subagent limit {MAX_ACTIVE_SUBAGENTS} exceeded while recording unknown stop"
        state["active_subagents"].append(
            {
                "agent_id_hash": agent_hash,
                "agent_type": _safe_agent_type(agent_type),
                "status": "unknown",
                "turn_hash": digest(turn_id),
                "started_at": datetime.now(timezone.utc).isoformat(),
            }
        )
        state["last_dirty_reason"] = "subagent:unknown_stop"
    if state.get("status") not in ("compacting", "recovery_required", "compact_failed"):
        state["status"] = "dirty"
        state["pending_turn_hash"] = state.get("pending_turn_hash") or digest(turn_id)
    state["last_dirty_reason"] = state.get("last_dirty_reason") or "subagent:stop"
    state["last_subagent_id_hash"] = agent_hash
    state["last_subagent_type"] = _safe_agent_type(agent_type)
    state["last_subagent_status"] = stop_status
    state["last_subagent_message_hash"] = digest(last_assistant_message)
    try:
        write_state(state_path(session_dir), state)
    except OSError as exc:
        return session_dir, state, f"cannot record subagent stop: {exc}"
    event_error = _record_subagent_event(
        session_dir,
        event="SubagentStop",
        state=state,
        turn_id=turn_id,
        agent_id=agent_id,
        agent_type=_safe_agent_type(agent_type),
        message=last_assistant_message,
    )
    return session_dir, state, f"subagent event evidence failed: {event_error}" if event_error else None


def has_secret(text: str) -> bool:
    return any(pattern.search(text) for pattern in SECRET_PATTERNS)


def estimate_tokens(text: str) -> int:
    """Use the conservative, deterministic estimate from the continuity design."""

    cjk = 0
    punctuation = 0
    ascii_alnum = 0
    other_letters = 0
    for character in text:
        codepoint = ord(character)
        category = unicodedata.category(character)
        if (
            0x3040 <= codepoint <= 0x30FF
            or 0x3400 <= codepoint <= 0x4DBF
            or 0x4E00 <= codepoint <= 0x9FFF
            or 0xAC00 <= codepoint <= 0xD7AF
        ):
            cjk += 1
        elif category.startswith(("P", "S")):
            punctuation += 1
        elif character.isascii() and character.isalnum():
            ascii_alnum += 1
        elif category.startswith("L"):
            other_letters += 1
    nonempty_lines = sum(1 for line in text.splitlines() if line.strip())
    base = cjk + punctuation + math.ceil(ascii_alnum / 4) + math.ceil(other_letters / 2) + nonempty_lines
    return math.ceil(base * 1.20)


def append_event(session_dir: Path, event: dict[str, Any]) -> str | None:
    """Append metadata-only event evidence without copying prompts or transcripts."""

    path = session_dir / "events.jsonl"
    record = {"timestamp": datetime.now(timezone.utc).isoformat(), **event}
    try:
        session_dir.mkdir(parents=True, exist_ok=True)
        os.chmod(session_dir, 0o700)
        with path.open("a", encoding="utf-8", newline="\n") as stream:
            stream.write(json.dumps(record, ensure_ascii=False, separators=(",", ":")) + "\n")
            stream.flush()
            os.fsync(stream.fileno())
        try:
            os.chmod(path, 0o600)
        except OSError:
            pass
    except (OSError, TypeError, ValueError) as exc:
        return str(exc)
    return None


def sanitize_pointer(value: Any, limit: int, *, always_hash: bool = False) -> str | None:
    """Bound and redact hook-provided path/tool metadata before persistence."""

    if value is None:
        return None
    text = str(value).replace("\r", " ").replace("\n", " ").replace("\x00", " ")
    if always_hash or has_secret(text):
        return f"pointer:{digest(text)}"
    return text[:limit]


def emergency_manifest_content(
    *,
    trigger: str,
    origin_status: str,
    state: dict[str, Any],
    turn_id: str | None,
    transcript_path: str | None,
    workspace_name: str,
    tool_name: str | None = None,
) -> str:
    """Build a bounded mechanical capsule; semantic summaries belong in checkpoint.md."""

    def _pointer(value: Any, limit: int, *, force_digest: bool = False) -> str:
        text = str("unknown" if value is None else value).replace("\r", " ").replace("\n", " ").replace("\x00", " ")
        if force_digest:
            return f"redacted:{digest(text)}"
        return sanitize_pointer(text, limit) or "unknown"

    def _build(*, compact_pointers: bool) -> str:
        lines = [
            "# Emergency manifest",
            f"- Trigger: {_pointer(trigger, 16)}",
            f"- Origin: {_pointer(origin_status, 16)}",
            f"- Turn: {_pointer(digest(turn_id), 16)}",
            f"- Last safe checkpoint sequence: {_pointer(state.get('checkpoint_seq', 0), 16)}",
            f"- Dirty generation: {_pointer(state.get('dirty_generation', 0), 16)}",
            f"- Active subagents: {_pointer(len(state.get('active_subagents', [])), 16)}",
            f"- Last tool: {_pointer(tool_name or state.get('last_tool_name'), 160, force_digest=compact_pointers)}",
            f"- Transcript pointer: {_pointer(transcript_path or state.get('transcript_path'), 300, force_digest=compact_pointers)}",
            f"- Workspace: {_pointer(workspace_name, 160, force_digest=compact_pointers)}",
            "- Git/artifact evidence: verify from the workspace after compact.",
            "- Recovery: verify workspace and artifacts, then continue from the first incomplete step.",
        ]
        return "\n".join(lines) + "\n"

    content = _build(compact_pointers=False)
    if estimate_tokens(content) > MAX_EMERGENCY_ESTIMATED_TOKENS:
        content = _build(compact_pointers=True)
    # This is a final character guard; the normal path remains well below the
    # limit, while unusual Unicode workspace names cannot expand indefinitely.
    if len(content) > MAX_EMERGENCY_CHARS:
        content = content[: MAX_EMERGENCY_CHARS - 1].rstrip() + "\n"
    return content


def emergency_manifest_content_from_state(
    session_dir: Path,
    state: dict[str, Any],
) -> tuple[str | None, str | None]:
    """Read and verify the last mechanical capsule before injecting it."""

    manifest_path = session_dir / "emergency.md"
    try:
        content = manifest_path.read_text(encoding="utf-8")
    except (OSError, UnicodeError) as exc:
        return None, f"cannot read emergency manifest: {exc}"
    if not content.strip():
        return None, "emergency manifest is empty"
    if len(content) > MAX_EMERGENCY_CHARS:
        return None, "emergency manifest is oversized"
    if has_secret(content):
        return None, "emergency manifest looks like it contains a secret"
    if state.get("emergency_manifest_chars") != len(content):
        return None, "emergency manifest length does not match state"
    if state.get("emergency_manifest_hash") != digest(content):
        return None, "emergency manifest hash does not match state"
    if estimate_tokens(content) > MAX_EMERGENCY_ESTIMATED_TOKENS:
        return None, "emergency manifest exceeds its estimated-token budget"
    return content, None


def seal_checkpoint(
    session_dir: Path,
    *,
    cwd: str | Path | None = None,
    turn_id: str | None = None,
    session_id: str | None = None,
) -> tuple[int, dict[str, Any]]:
    """Validate and atomically seal the warm checkpoint and optional hot state."""

    state = read_state(session_dir)
    state_error = validate_state(state)
    if state is None or state_error:
        return 4, {"status": "error", "reason": state_error or "missing or invalid state.json"}

    try:
        resolved_session_dir = session_dir.resolve()
    except (OSError, RuntimeError, TypeError) as exc:
        return 4, {"status": "error", "reason": f"cannot resolve session directory: {exc}"}
    expected_dir_name = f"session-{state['session_id_hash']}"
    if resolved_session_dir.name != expected_dir_name:
        return 3, {"status": "identity_mismatch", "reason": "session directory identity mismatch"}
    if session_id is not None and not isinstance(session_id, str):
        return 3, {"status": "identity_mismatch", "reason": "session_id must be a string"}
    if session_id is not None and digest(session_id) != state.get("session_id_hash"):
        return 3, {"status": "identity_mismatch", "reason": "session identity mismatch"}
    if turn_id is not None and not isinstance(turn_id, str):
        return 2, {"status": "invalid", "reason": "turn_id must be a string"}

    try:
        workspace_hash, _ = identity_for(cwd)
    except (OSError, RuntimeError, ValueError, TypeError) as exc:
        return 4, {"status": "error", "reason": f"cannot resolve workspace identity: {exc}"}
    if state.get("workspace_hash") != workspace_hash:
        return 3, {"status": "identity_mismatch", "reason": "workspace identity mismatch"}
    if state.get("status") == "compacting":
        return 2, {"status": "invalid", "reason": "cannot seal while compact is in progress"}
    if state.get("active_subagents"):
        return 2, {
            "status": "invalid",
            "reason": "cannot seal while a subagent is still running or has unknown status",
            "active_subagents": len(state.get("active_subagents", [])),
        }
    if state.get("last_subagent_status") == "unknown":
        return 2, {
            "status": "invalid",
            "reason": "cannot seal while a subagent lifecycle is unknown; reconcile the agent first",
        }

    checkpoint = checkpoint_path(session_dir)
    try:
        content = checkpoint.read_text(encoding="utf-8-sig")
    except (OSError, UnicodeError) as exc:
        return 2, {"status": "invalid", "reason": f"cannot read checkpoint: {exc}"}

    if not content.strip():
        return 2, {"status": "invalid", "reason": "checkpoint is empty"}
    if len(content) > MAX_CHECKPOINT_CHARS:
        return 2, {
            "status": "invalid",
            "reason": f"checkpoint exceeds {MAX_CHECKPOINT_CHARS} characters",
            "checkpoint_chars": len(content),
        }
    warm_estimated_tokens = estimate_tokens(content)
    if warm_estimated_tokens > MAX_WARM_ESTIMATED_TOKENS:
        return 2, {
            "status": "invalid",
            "reason": f"checkpoint exceeds {MAX_WARM_ESTIMATED_TOKENS} estimated tokens",
            "warm_estimated_tokens": warm_estimated_tokens,
        }
    if has_secret(content):
        return 2, {"status": "invalid", "reason": "checkpoint looks like it contains a secret"}

    # A hot capsule is optional for Wave 1/2 compatibility. If the warm
    # checkpoint is already small enough, mirror it into hot.md so old users
    # automatically gain bounded hot recovery without a second model write.
    hot = hot_path(session_dir)
    hot_content_value: str | None = None
    hot_needs_write = False
    if hot.exists():
        try:
            hot_content_value = hot.read_text(encoding="utf-8-sig")
        except (OSError, UnicodeError) as exc:
            return 2, {"status": "invalid", "reason": f"cannot read hot state: {exc}"}
        if not hot_content_value.strip():
            return 2, {"status": "invalid", "reason": "hot state is empty"}
        if len(hot_content_value) > MAX_HOT_CHARS:
            return 2, {
                "status": "invalid",
                "reason": f"hot state exceeds {MAX_HOT_CHARS} characters",
                "hot_chars": len(hot_content_value),
            }
        hot_estimated_tokens = estimate_tokens(hot_content_value)
        if hot_estimated_tokens > MAX_HOT_ESTIMATED_TOKENS:
            return 2, {
                "status": "invalid",
                "reason": f"hot state exceeds {MAX_HOT_ESTIMATED_TOKENS} estimated tokens",
                "hot_estimated_tokens": hot_estimated_tokens,
            }
        if has_secret(hot_content_value):
            return 2, {"status": "invalid", "reason": "hot state looks like it contains a secret"}
    elif warm_estimated_tokens <= MAX_HOT_ESTIMATED_TOKENS:
        hot_content_value = content
        hot_needs_write = True

    hot_hash = digest(hot_content_value)
    hot_chars = len(hot_content_value) if hot_content_value is not None else 0
    hot_estimated_tokens = estimate_tokens(hot_content_value) if hot_content_value is not None else 0

    checkpoint_hash = digest(content)
    pending_turn_hash = state.get("pending_turn_hash")
    requested_turn_hash = digest(turn_id) if turn_id else pending_turn_hash
    if turn_id and pending_turn_hash is not None and requested_turn_hash != pending_turn_hash:
        return 2, {"status": "invalid", "reason": "turn_id does not match the pending turn"}
    if pending_turn_hash is None:
        if not (
            state.get("status") == "safe"
            and requested_turn_hash is not None
            and requested_turn_hash == state.get("last_sealed_turn_hash")
            and state.get("checkpoint_hash") == checkpoint_hash
            and state.get("checkpoint_chars") == len(content)
        ):
            return 2, {"status": "invalid", "reason": "no pending turn to seal"}
    sealed_turn_hash = pending_turn_hash or requested_turn_hash
    if sealed_turn_hash is None:
        return 2, {"status": "invalid", "reason": "no pending turn to seal"}

    if hot_needs_write:
        try:
            _atomic_write_text(hot, hot_content_value or "")
        except OSError as exc:
            return 4, {"status": "error", "reason": f"cannot write hot state: {exc}"}

    same_checkpoint = (
        state.get("status") == "safe"
        and state.get("checkpoint_hash") == checkpoint_hash
        and state.get("last_sealed_turn_hash") == sealed_turn_hash
        and state.get("warm_estimated_tokens", 0) == warm_estimated_tokens
        and state.get("hot_hash") == hot_hash
        and state.get("hot_chars", 0) == hot_chars
        and state.get("hot_estimated_tokens", 0) == hot_estimated_tokens
    )
    if same_checkpoint:
        return 0, {
            "status": "safe",
            "checkpoint_seq": state.get("checkpoint_seq", 0),
            "checkpoint_chars": len(content),
            "hot_estimated_tokens": hot_estimated_tokens,
            "warm_estimated_tokens": warm_estimated_tokens,
            "content_digest": digest(f"{hot_hash or ''}:{checkpoint_hash or ''}"),
            "idempotent": True,
        }

    was_recovery = state.get("status") == "recovery_required"
    state.update(
        {
            "status": "safe",
            "checkpoint_seq": int(state.get("checkpoint_seq", 0)) + 1,
            "checkpoint_hash": checkpoint_hash,
            "checkpoint_chars": len(content),
            "last_sealed_turn_hash": sealed_turn_hash,
            "pending_turn_hash": None,
            "last_sealed_at": datetime.now(timezone.utc).isoformat(),
            "origin_status": None,
            "last_compact_result": "sealed_after_recovery" if was_recovery else state.get("last_compact_result"),
            "warm_estimated_tokens": warm_estimated_tokens,
            "hot_hash": hot_hash,
            "hot_chars": hot_chars,
            "hot_estimated_tokens": hot_estimated_tokens,
        }
    )
    try:
        write_state(state_path(session_dir), state)
    except OSError as exc:
        return 4, {"status": "error", "reason": f"cannot write state.json: {exc}"}
    event_error = append_event(
        session_dir,
        {
            "event": "Seal",
            "status": "safe",
            "checkpoint_seq": state["checkpoint_seq"],
            "hot_estimated_tokens": hot_estimated_tokens,
            "warm_estimated_tokens": warm_estimated_tokens,
            "content_digest": digest(f"{hot_hash or ''}:{checkpoint_hash or ''}"),
        },
    )
    result: dict[str, Any] = {
        "status": "safe",
        "checkpoint_seq": state["checkpoint_seq"],
        "checkpoint_chars": len(content),
        "hot_estimated_tokens": hot_estimated_tokens,
        "warm_estimated_tokens": warm_estimated_tokens,
        "content_digest": digest(f"{hot_hash or ''}:{checkpoint_hash or ''}"),
        "idempotent": False,
    }
    if event_error:
        result["warning"] = f"seal state saved but event evidence failed: {event_error}"
    return 0, result
