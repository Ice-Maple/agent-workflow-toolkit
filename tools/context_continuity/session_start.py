"""Wave 1 SessionStart hook: locate and inject a compact checkpoint."""

from __future__ import annotations

import json
import sys
from pathlib import Path
from typing import Any

if __package__:
    from .state import (
        append_event,
        checkpoint_content,
        checkpoint_path,
        emergency_manifest_content_from_state,
        ensure_session,
        estimate_tokens,
        hot_content,
        clear_compact_marker,
        read_compact_marker,
        session_directory,
        session_state_lock,
        write_state,
    )
else:  # Allow the exact file path used by hooks.example.json.
    sys.path.insert(0, str(Path(__file__).resolve().parents[2]))
    from tools.context_continuity.state import (  # type: ignore[no-redef]
        append_event,
        checkpoint_content,
        checkpoint_path,
        emergency_manifest_content_from_state,
        ensure_session,
        estimate_tokens,
        hot_content,
        clear_compact_marker,
        read_compact_marker,
        session_directory,
        session_state_lock,
        write_state,
    )


def _payload() -> dict[str, Any]:
    try:
        raw = sys.stdin.read() or "{}"
        value = json.loads(raw.lstrip("\ufeff"))
    except (json.JSONDecodeError, UnicodeError):
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
        ensure_ascii=True,
    )
    sys.stdout.write("\n")


def _emit_empty() -> None:
    json.dump({}, sys.stdout, separators=(",", ":"))
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


def _hot_or_bounded_warm(
    session_dir: Path,
    state: dict[str, Any],
    *,
    require_safe: bool,
) -> tuple[str | None, str | None, str | None]:
    """Prefer hot content and never inject an oversized warm checkpoint."""

    warm, warm_error = checkpoint_content(session_dir, state, require_safe=require_safe)
    # Hot and warm are a pair. Never inject a stale hot capsule when the
    # sealed warm checkpoint has been tampered with or cannot be read.
    if warm is None:
        return None, None, warm_error
    hot, hot_error = hot_content(session_dir, state, require_safe=require_safe)
    if hot is not None:
        return hot, "hot", None
    if warm is not None and estimate_tokens(warm) <= 600:
        return warm, "warm-fallback", hot_error or warm_error
    return None, None, hot_error or warm_error


def _status_after_safe_compact(
    session_dir: Path,
    state: dict[str, Any],
) -> tuple[str, str | None]:
    """Return the quiet post-compact status for a non-dirty origin."""

    if state.get("checkpoint_seq", 0) == 0 and state.get("checkpoint_hash") is None:
        return "clean", None
    sealed_content, sealed_error = checkpoint_content(session_dir, state, require_safe=False)
    if sealed_content is None:
        return "compact_failed", sealed_error or "unknown error"
    return "safe", None


def _main_locked(payload: dict[str, Any], *, quiet_initial: bool) -> int:
    source = payload.get("source")
    try:
        session_dir, state, error = ensure_session(
            payload.get("session_id"),
            payload.get("cwd"),
            source=payload.get("source"),
        )
    except (OSError, UnicodeError, RuntimeError, ValueError, TypeError) as exc:
        _emit_context(f"Context continuity is unavailable: {exc}.")
        return 0
    if error or session_dir is None or state is None:
        _emit_context(f"Context continuity is unavailable: {error or 'unknown error'}.")
        return 0

    transition_warning: str | None = None
    transition_persisted = True
    marker = read_compact_marker(session_dir)
    # A marker is the fallback when PreCompact wrote its manifest but could
    # not atomically update state.json. Consume it before treating a dirty
    # session as an ordinary startup.
    marker_matches_persisted_attempt = bool(
        marker
        and marker.get("started_at")
        and marker.get("started_at") == state.get("last_compact_started_at")
    )
    if marker and (
        state.get("status") not in ("compacting", "safe", "recovery_required")
        or not marker_matches_persisted_attempt
    ):
        marker_origin = marker.get("origin_status") if marker.get("origin_status") in ("safe", "dirty") else "dirty"
        if state.get("pending_turn_hash"):
            marker_origin = "dirty"
        marker_complete = marker.get("status") == "state_write_pending" and (
            marker_origin == "safe" or bool(marker.get("manifest_hash"))
        )
        if source == "compact" and marker_complete:
            if marker_origin == "dirty":
                state["status"] = "recovery_required"
                state["origin_status"] = "dirty"
            else:
                recovered_status, recovered_error = _status_after_safe_compact(session_dir, state)
                state["status"] = recovered_status
                state["origin_status"] = None
                if recovered_error:
                    transition_warning = f"safe checkpoint could not be verified after compact: {recovered_error}"
            state["emergency_manifest_hash"] = marker.get("manifest_hash")
            state["emergency_manifest_chars"] = marker.get("manifest_chars", 0)
            state["emergency_manifest_estimated_tokens"] = marker.get("manifest_estimated_tokens", 0)
            state["last_compact_trigger"] = marker.get("trigger")
            state["last_compact_turn_hash"] = marker.get("turn_hash")
            state["last_compact_started_at"] = marker.get("started_at")
            state["last_compact_completed_at"] = None
            state["last_compact_result"] = "recovered_from_marker"
        else:
            state["status"] = "compact_failed"
            state["last_compact_result"] = "compact_failed_from_marker"
            transition_warning = "compact recovery marker was found without a durable compacting state"
        try:
            write_state(session_dir / "state.json", state)
            clear_compact_marker(session_dir)
            marker_event_error = append_event(
                session_dir,
                {
                    "event": "SessionStartMarkerRecovery",
                    "source": source or "unknown",
                    "status": state.get("status"),
                    "origin_status": marker_origin,
                },
            )
            if marker_event_error:
                transition_warning = f"{transition_warning or 'compact marker recovery'} event was not recorded: {marker_event_error}"
        except OSError as exc:
            transition_persisted = False
            transition_warning = f"{transition_warning or 'compact marker recovery'} could not be persisted: {exc}"
    elif marker and marker_matches_persisted_attempt:
        # State already records this exact attempt; a previous cleanup may have
        # been interrupted, so remove only the now-redundant marker.
        clear_compact_marker(session_dir)

    # A compacting state is only considered successful when the host delivers
    # the compact SessionStart. Any other later entry marks the attempt failed
    # without discarding the last safe checkpoint.
    if state.get("status") == "compacting":
        if source == "compact":
            origin = (
                "dirty"
                if state.get("pending_turn_hash")
                or state.get("emergency_manifest_hash")
                or state.get("origin_status") == "dirty"
                else "safe"
            )
            if origin == "safe":
                recovered_status, recovered_error = _status_after_safe_compact(session_dir, state)
                state["status"] = recovered_status
                if recovered_status == "compact_failed":
                    state["last_compact_result"] = "recovered_safe_checkpoint_invalid"
                    transition_warning = f"safe checkpoint could not be verified after compact: {recovered_error or 'unknown error'}"
                elif recovered_status == "clean":
                    state["last_compact_result"] = "recovered_clean"
                else:
                    state["last_compact_result"] = "recovered_safe"
            else:
                state["status"] = "recovery_required"
                state["last_compact_result"] = "recovered_dirty"
            if origin == "safe" and state.get("status") in ("clean", "safe"):
                state["origin_status"] = None
            try:
                write_state(session_dir / "state.json", state)
                clear_compact_marker(session_dir)
                recovery_event_error = append_event(
                    session_dir,
                    {
                        "event": "SessionStart",
                        "source": "compact",
                        "status": state["status"],
                        "origin_status": origin,
                    },
                )
                if recovery_event_error:
                    transition_warning = f"compact recovery event was not recorded: {recovery_event_error}"
            except OSError as exc:
                transition_persisted = False
                transition_warning = f"could not record compact recovery: {exc}"
        else:
            state["status"] = "compact_failed"
            state["last_compact_result"] = "compact_failed"
            transition_warning = "compact started but no SessionStart(source=compact) was observed"
            try:
                write_state(session_dir / "state.json", state)
                clear_compact_marker(session_dir)
                failure_event_error = append_event(
                    session_dir,
                    {
                        "event": "SessionStart",
                        "source": source or "unknown",
                        "status": "compact_failed",
                        "origin_status": state.get("origin_status"),
                    },
                )
                if failure_event_error:
                    transition_warning = f"compact failure event was not recorded: {failure_event_error}"
            except OSError as exc:
                transition_persisted = False
                transition_warning = f"{transition_warning}; could not record failure: {exc}"

    checkpoint = checkpoint_path(session_dir)
    if not transition_persisted and state.get("status") in ("clean", "safe"):
        state["status"] = "compact_failed"
        state["last_compact_result"] = "compact_recovery_state_unpersisted"
    if quiet_initial and state.get("status") == "clean" and not transition_warning:
        _emit_empty()
        return 0
    lines = [
        "Context continuity is active.",
        f"Session state directory: {session_dir}",
        f"Hot state file: {session_dir / 'hot.md'}",
        f"Checkpoint file: {checkpoint}",
        f"Seal command: {_seal_command(session_dir, session_id=payload.get('session_id', ''), cwd=payload.get('cwd'))}",
    ]
    if transition_warning:
        lines.append(f"Warning: {transition_warning}.")

    recovery = state.get("status") == "recovery_required"
    if recovery:
        content, content_kind, checkpoint_error = _hot_or_bounded_warm(
            session_dir,
            state,
            require_safe=False,
        )
        manifest, manifest_error = emergency_manifest_content_from_state(session_dir, state)
        lines.append("Recovery is required: verify the workspace and artifacts before resuming work.")
        if content is not None:
            lines.extend(
                [
                    f"Last safe {content_kind} state (verify before relying on it):",
                    f"--- {('hot.md' if content_kind == 'hot' else 'checkpoint.md')} ---",
                    content.rstrip(),
                    f"--- end {('hot.md' if content_kind == 'hot' else 'checkpoint.md')} ---",
                ]
            )
        elif checkpoint_error:
            lines.append(f"Last safe checkpoint was not injected: {checkpoint_error}.")
        if manifest is not None:
            lines.extend(["Emergency manifest:", "--- emergency.md ---", manifest.rstrip(), "--- end emergency.md ---"])
        elif manifest_error:
            lines.append(f"Emergency manifest was not injected: {manifest_error}.")
        lines.append("Reconcile evidence, then write checkpoint.md and run seal before continuing substantive work.")
    elif state.get("status") == "compact_failed":
        content, content_kind, checkpoint_error = _hot_or_bounded_warm(
            session_dir,
            state,
            require_safe=False,
        )
        lines.append("Compact completion is unverified; preserve the last safe checkpoint and inspect evidence before continuing.")
        if content is not None:
            lines.extend(
                [
                    f"Last safe {content_kind} state (verify before relying on it):",
                    f"--- {('hot.md' if content_kind == 'hot' else 'checkpoint.md')} ---",
                    content.rstrip(),
                    f"--- end {('hot.md' if content_kind == 'hot' else 'checkpoint.md')} ---",
                ]
            )
        elif checkpoint_error:
            lines.append(f"Last safe checkpoint was not injected: {checkpoint_error}.")
    else:
        content, content_kind, checkpoint_error = _hot_or_bounded_warm(
            session_dir,
            state,
            require_safe=True,
        )
        if content is not None:
            if source == "compact":
                lines.extend(
                    [
                        f"Verify this sealed {content_kind} state against the current workspace before continuing:",
                        f"--- {('hot.md' if content_kind == 'hot' else 'checkpoint.md')} ---",
                        content.rstrip(),
                        f"--- end {('hot.md' if content_kind == 'hot' else 'checkpoint.md')} ---",
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
        elif state.get("status") == "dirty":
            lines.append(
                "Unsealed external mutations were recorded. Native compaction remains available; "
                "if recovery is needed, verify workspace evidence before creating a manual checkpoint."
            )
        else:
            lines.append("No sealed checkpoint exists yet. At the end of this turn, write a concise checkpoint and run seal.")

    _emit_context("\n".join(lines))
    return 0


def main(argv: list[str] | None = None) -> int:
    quiet_initial = "--quiet-initial" in (sys.argv[1:] if argv is None else argv)
    payload = _payload()
    try:
        session_id = payload.get("session_id")
        if not isinstance(session_id, str) or not session_id:
            raise ValueError("missing session_id")
        session_dir = session_directory(session_id)
        with session_state_lock(session_dir):
            return _main_locked(payload, quiet_initial=quiet_initial)
    except (OSError, UnicodeError, RuntimeError, ValueError, TypeError) as exc:
        _emit_context(f"Context continuity is unavailable: {exc}.")
        return 0


if __name__ == "__main__":
    raise SystemExit(main())
