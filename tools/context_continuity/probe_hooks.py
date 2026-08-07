"""Record safe lifecycle-hook metadata for Wave 0 host probing.

The probe is deliberately observational. It never returns a blocking decision,
never emits prompt/transcript contents, and exits successfully even when the
input cannot be parsed so a diagnostic hook cannot interrupt a Codex session.
"""

from __future__ import annotations

import argparse
import hashlib
import json
import os
from pathlib import Path
import sys
import tempfile
from datetime import datetime, timezone
from typing import Any, Mapping


SCHEMA_VERSION = 1


def _hash(value: Any) -> str | None:
    if value is None:
        return None
    text = str(value)
    if not text:
        return None
    return hashlib.sha256(text.encode("utf-8")).hexdigest()[:16]


def _non_negative_length(value: Any) -> int | None:
    if isinstance(value, str):
        return len(value)
    return None


def sanitize_event(payload: Mapping[str, Any], *, received_at: str | None = None) -> dict[str, Any]:
    """Return only low-sensitivity metadata from a hook payload.

    The full hook input may contain prompts, assistant messages and transcript
    paths. Those fields are intentionally reduced to presence/length/hash
    metadata so the probe can be used against real sessions without creating a
    second transcript store.
    """

    event_name = payload.get("hook_event_name")
    event = str(event_name) if event_name is not None else "unknown"
    record: dict[str, Any] = {
        "schema_version": SCHEMA_VERSION,
        "received_at": received_at or datetime.now(timezone.utc).isoformat(),
        "hook_event_name": event,
    }

    for key in ("trigger", "source", "model", "permission_mode"):
        value = payload.get(key)
        if isinstance(value, (str, int, float, bool)):
            record[key] = value

    for key in ("session_id", "turn_id", "agent_transcript_path"):
        value = payload.get(key)
        digest = _hash(value)
        if digest is not None:
            record[f"{key}_hash"] = digest

    cwd = payload.get("cwd")
    cwd_hash = _hash(cwd)
    if cwd_hash is not None:
        record["cwd_hash"] = cwd_hash
        if isinstance(cwd, str) and cwd:
            record["cwd_name"] = Path(cwd).name or Path(cwd).drive

    transcript = payload.get("transcript_path")
    record["transcript_present"] = isinstance(transcript, str) and bool(transcript)
    transcript_hash = _hash(transcript)
    if transcript_hash is not None:
        record["transcript_path_hash"] = transcript_hash

    prompt = payload.get("prompt")
    prompt_length = _non_negative_length(prompt)
    record["prompt_present"] = prompt_length is not None
    if prompt_length is not None:
        record["prompt_chars"] = prompt_length

    assistant_message = payload.get("last_assistant_message")
    assistant_length = _non_negative_length(assistant_message)
    record["last_assistant_message_present"] = assistant_length is not None
    if assistant_length is not None:
        record["last_assistant_message_chars"] = assistant_length

    stop_hook_active = payload.get("stop_hook_active")
    if isinstance(stop_hook_active, bool):
        record["stop_hook_active"] = stop_hook_active

    return record


def _default_log_dir() -> Path:
    configured = os.environ.get("CODEX_CONTEXT_CONTINUITY_PROBE_DIR")
    if configured:
        return Path(configured)
    return Path(tempfile.gettempdir()) / "codex-context-continuity-wave0"


def append_event(log_dir: Path, record: Mapping[str, Any]) -> Path:
    """Append one compact JSON record and return the log path."""

    log_dir.mkdir(parents=True, exist_ok=True)
    log_path = log_dir / "events.jsonl"
    line = json.dumps(record, ensure_ascii=False, sort_keys=True, separators=(",", ":"))
    with log_path.open("a", encoding="utf-8", newline="\n") as stream:
        stream.write(line)
        stream.write("\n")
    return log_path


def _read_payload(payload_file: Path | None = None) -> dict[str, Any]:
    if payload_file is not None:
        raw = payload_file.read_text(encoding="utf-8-sig")
    else:
        raw = sys.stdin.read()
    if not raw.strip():
        return {}
    try:
        value = json.loads(raw)
    except json.JSONDecodeError:
        return {"hook_event_name": "invalid_json", "raw_chars": len(raw)}
    return value if isinstance(value, dict) else {"hook_event_name": "invalid_payload"}


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument(
        "--log-dir",
        type=Path,
        default=None,
        help="Directory for the metadata-only events.jsonl log.",
    )
    parser.add_argument(
        "--payload-file",
        type=Path,
        default=None,
        help="Read a UTF-8 (with or without BOM) test payload instead of stdin.",
    )
    return parser


def main(argv: list[str] | None = None) -> int:
    args = build_parser().parse_args(argv)
    payload = _read_payload(args.payload_file)
    record = sanitize_event(payload)
    log_path = append_event(args.log_dir or _default_log_dir(), record)
    # Keep stdout empty: plain stdout has event-specific meaning for some
    # Codex hooks. The probe is observational and must not inject context.
    print(f"wave0 probe recorded {record['hook_event_name']} in {log_path}", file=sys.stderr)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
