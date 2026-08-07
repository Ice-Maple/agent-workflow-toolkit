from __future__ import annotations

import io
import json
import os
from pathlib import Path
import tempfile
import unittest
from contextlib import redirect_stderr, redirect_stdout
from unittest.mock import patch

from tools.context_continuity.probe_hooks import _default_log_dir, main, sanitize_event


class ProbeHookTests(unittest.TestCase):
    def test_sanitize_event_does_not_store_sensitive_text_or_paths(self) -> None:
        payload = {
            "hook_event_name": "UserPromptSubmit",
            "session_id": "session-123",
            "turn_id": "turn-456",
            "cwd": r"C:\Users\example\repo",
            "transcript_path": r"C:\Users\example\secret-rollout.jsonl",
            "prompt": "do not store this API_KEY=secret",
            "last_assistant_message": "private assistant text",
            "source": "startup",
        }

        record = sanitize_event(payload, received_at="2026-08-07T00:00:00+00:00")
        encoded = json.dumps(record, ensure_ascii=False)

        self.assertEqual(record["hook_event_name"], "UserPromptSubmit")
        self.assertEqual(record["prompt_chars"], len(payload["prompt"]))
        self.assertTrue(record["transcript_present"])
        self.assertNotIn("API_KEY", encoded)
        self.assertNotIn("secret-rollout.jsonl", encoded)
        self.assertNotIn("private assistant text", encoded)
        self.assertNotEqual(record["session_id_hash"], payload["session_id"])

    def test_main_appends_metadata_only_jsonl_and_emits_no_stdout(self) -> None:
        payload = {
            "hook_event_name": "PreCompact",
            "trigger": "auto",
            "session_id": "session-123",
            "transcript_path": r"C:\secret\rollout.jsonl",
        }
        with tempfile.TemporaryDirectory() as temp_dir:
            stdout = io.StringIO()
            stderr = io.StringIO()
            with patch("sys.stdin", io.StringIO(json.dumps(payload))), redirect_stdout(stdout), redirect_stderr(stderr):
                result = main(["--log-dir", temp_dir])

            self.assertEqual(result, 0)
            self.assertEqual(stdout.getvalue(), "")
            self.assertIn("PreCompact", stderr.getvalue())
            lines = (Path(temp_dir) / "events.jsonl").read_text(encoding="utf-8").splitlines()
            self.assertEqual(len(lines), 1)
            record = json.loads(lines[0])
            self.assertEqual(record["trigger"], "auto")
            self.assertNotIn("rollout.jsonl", lines[0])

    def test_malformed_input_is_non_blocking(self) -> None:
        with tempfile.TemporaryDirectory() as temp_dir:
            with patch("sys.stdin", io.StringIO("not-json")):
                result = main(["--log-dir", temp_dir])

            self.assertEqual(result, 0)
            record = json.loads((Path(temp_dir) / "events.jsonl").read_text(encoding="utf-8"))
            self.assertEqual(record["hook_event_name"], "invalid_json")

    def test_payload_file_accepts_utf8_bom_for_windows_smoke(self) -> None:
        payload = {"hook_event_name": "SessionStart", "source": "startup"}
        with tempfile.TemporaryDirectory() as temp_dir:
            payload_file = Path(temp_dir) / "payload.json"
            payload_file.write_text(json.dumps(payload), encoding="utf-8-sig")
            result = main(["--log-dir", temp_dir, "--payload-file", str(payload_file)])

            self.assertEqual(result, 0)
            record = json.loads((Path(temp_dir) / "events.jsonl").read_text(encoding="utf-8"))
            self.assertEqual(record["hook_event_name"], "SessionStart")

    def test_default_log_dir_is_system_temp_not_current_directory(self) -> None:
        with patch.dict(os.environ, {"CODEX_CONTEXT_CONTINUITY_PROBE_DIR": ""}):
            log_dir = _default_log_dir()

        self.assertEqual(log_dir.parent, Path(tempfile.gettempdir()))
        self.assertNotEqual(log_dir, Path("."))


if __name__ == "__main__":
    unittest.main()
