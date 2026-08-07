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
        "transcript_path",
        "last_compact_started_at",
        "last_compact_completed_at",
        "last_compact_result",
    ):
        if state.get(field) is not None and not isinstance(state.get(field), str):
            return f"invalid {field}"
    if state.get("last_tool_name") is not None and len(state["last_tool_name"]) > 256:
        return "last_tool_name is too long"
    if state.get("transcript_path") is not None and len(state["transcript_path"]) > 4096:
        return "transcript_path is too long"
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
        or not 0 <= emergency_tokens <= 250
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
        content = checkpoint.read_text(encoding="utf-8")
    except (OSError, UnicodeError) as exc:
        return None, f"cannot read checkpoint: {exc}"
    if not content.strip():
        return None, "checkpoint is empty"
    if len(content) > MAX_CHECKPOINT_CHARS:
        return None, f"checkpoint exceeds {MAX_CHECKPOINT_CHARS} characters"
    if state.get("checkpoint_chars") != len(content):
        return None, "checkpoint length does not match sealed state"
    if state.get("checkpoint_hash") != digest(content):
        return None, "checkpoint hash does not match sealed state"
    if has_secret(content):
        return None, "checkpoint looks like it contains a secret"
    return content, None


def identity_for(cwd: str | Path | None) -> tuple[str, str]:
    root = workspace_root(cwd)
    return digest(str(root)) or "", root.name or str(root)


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
            "origin_status": None,
            "last_tool_name": None,
            "transcript_path": None,
            "emergency_manifest_hash": None,
            "emergency_manifest_chars": 0,
            "emergency_manifest_estimated_tokens": 0,
            "last_compact_trigger": None,
            "last_compact_turn_hash": None,
            "last_compact_started_at": None,
            "last_compact_completed_at": None,
            "last_compact_result": None,
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
    state_error = validate_state(state)
    if state_error:
        return session_dir, state, f"invalid state.json: {state_error}"

    if source is not None and state.get("last_source") != source:
        state["last_source"] = source
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
        state["status"] = "dirty"
        state["pending_turn_hash"] = turn_hash
        try:
            write_state(state_path(session_dir), state)
        except OSError as exc:
            return session_dir, state, f"cannot update state.json: {exc}"
    return session_dir, state, None


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
        text = str(value or "unknown").replace("\r", " ").replace("\n", " ").replace("\x00", " ")
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
            f"- Last tool: {_pointer(tool_name or state.get('last_tool_name'), 160, force_digest=compact_pointers)}",
            f"- Transcript pointer: {_pointer(transcript_path or state.get('transcript_path'), 300, force_digest=compact_pointers)}",
            f"- Workspace: {_pointer(workspace_name, 160, force_digest=compact_pointers)}",
            "- Git/artifact evidence: verify from the workspace after compact.",
            "- Recovery: verify workspace and artifacts, then continue from the first incomplete step.",
        ]
        return "\n".join(lines) + "\n"

    content = _build(compact_pointers=False)
    if estimate_tokens(content) > 250:
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
    if estimate_tokens(content) > 250:
        return None, "emergency manifest exceeds its estimated-token budget"
    return content, None


def seal_checkpoint(
    session_dir: Path,
    *,
    cwd: str | Path | None = None,
    turn_id: str | None = None,
    session_id: str | None = None,
) -> tuple[int, dict[str, Any]]:
    """Validate and atomically seal a Wave 1 checkpoint."""

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

    checkpoint = checkpoint_path(session_dir)
    try:
        content = checkpoint.read_text(encoding="utf-8")
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
    if has_secret(content):
        return 2, {"status": "invalid", "reason": "checkpoint looks like it contains a secret"}

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

    same_checkpoint = (
        state.get("status") == "safe"
        and state.get("checkpoint_hash") == checkpoint_hash
        and state.get("last_sealed_turn_hash") == sealed_turn_hash
    )
    if same_checkpoint:
        return 0, {
            "status": "safe",
            "checkpoint_seq": state.get("checkpoint_seq", 0),
            "checkpoint_chars": len(content),
            "idempotent": True,
        }

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
            "last_compact_result": "sealed_after_recovery"
            if state.get("status") == "recovery_required"
            else state.get("last_compact_result"),
        }
    )
    try:
        write_state(state_path(session_dir), state)
    except OSError as exc:
        return 4, {"status": "error", "reason": f"cannot write state.json: {exc}"}
    return 0, {
        "status": "safe",
        "checkpoint_seq": state["checkpoint_seq"],
        "checkpoint_chars": len(content),
        "idempotent": False,
    }
