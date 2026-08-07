from __future__ import annotations

import io
import json
from pathlib import Path
import tempfile
import unittest
from contextlib import redirect_stdout
from unittest.mock import patch

from tools.context_continuity.seal import main as seal_main
from tools.context_continuity.session_start import main as session_start_main
from tools.context_continuity.state import context_home, read_state, session_directory, state_path
from tools.context_continuity.stop import main as stop_main


class Wave1HookTests(unittest.TestCase):
    def _run_session_start(self, temp_home: str, workspace: str, *, source: str = "startup") -> dict:
        payload = {
            "hook_event_name": "SessionStart",
            "source": source,
            "session_id": "wave1-session",
            "cwd": workspace,
        }
        output = io.StringIO()
        with patch.dict("os.environ", {"CODEX_CONTEXT_CONTINUITY_HOME": temp_home}), patch(
            "sys.stdin", io.StringIO(json.dumps(payload))
        ), redirect_stdout(output):
            result = session_start_main()
        self.assertEqual(result, 0)
        return json.loads(output.getvalue())

    def _run_stop(self, temp_home: str, workspace: str, *, active: bool = False) -> dict:
        payload = {
            "hook_event_name": "Stop",
            "session_id": "wave1-session",
            "cwd": workspace,
            "turn_id": "wave1-turn",
            "stop_hook_active": active,
        }
        output = io.StringIO()
        with patch.dict("os.environ", {"CODEX_CONTEXT_CONTINUITY_HOME": temp_home}), patch(
            "sys.stdin", io.StringIO(json.dumps(payload))
        ), redirect_stdout(output):
            result = stop_main()
        self.assertEqual(result, 0)
        return json.loads(output.getvalue())

    def test_session_start_creates_private_state_and_injects_path(self) -> None:
        with tempfile.TemporaryDirectory() as temp_home, tempfile.TemporaryDirectory() as workspace:
            result = self._run_session_start(temp_home, workspace)
            context = result["hookSpecificOutput"]["additionalContext"]
            session_dir = session_directory("wave1-session", home=Path(temp_home))

            self.assertTrue(state_path(session_dir).exists())
            self.assertIn(str(session_dir), context)
            self.assertIn("No sealed checkpoint exists yet", context)
            self.assertNotIn("session_id\":", context)

    def test_stop_requests_one_seal_then_allows_same_turn(self) -> None:
        with tempfile.TemporaryDirectory() as temp_home, tempfile.TemporaryDirectory() as workspace:
            self._run_session_start(temp_home, workspace)
            session_dir = session_directory("wave1-session", home=Path(temp_home))

            first_stop = self._run_stop(temp_home, workspace)
            self.assertEqual(first_stop["decision"], "block")
            self.assertIn("checkpoint.md", first_stop["reason"])

            (session_dir / "checkpoint.md").write_text(
                "# Current task\n\n- Goal: verify Wave 1 continuity\n- Next: run the next test\n",
                encoding="utf-8",
            )
            seal_output = io.StringIO()
            with patch.dict("os.environ", {"CODEX_CONTEXT_CONTINUITY_HOME": temp_home}), redirect_stdout(seal_output):
                seal_code = seal_main(["--session-dir", str(session_dir), "--cwd", workspace])
            self.assertEqual(seal_code, 0)
            self.assertEqual(json.loads(seal_output.getvalue())["status"], "safe")

            self.assertEqual(self._run_stop(temp_home, workspace), {})

    def test_compact_session_start_reinjects_checkpoint(self) -> None:
        with tempfile.TemporaryDirectory() as temp_home, tempfile.TemporaryDirectory() as workspace:
            self._run_session_start(temp_home, workspace)
            session_dir = session_directory("wave1-session", home=Path(temp_home))
            self._run_stop(temp_home, workspace)
            (session_dir / "checkpoint.md").write_text(
                "# Current task\n\n- Next: continue from this exact step\n",
                encoding="utf-8",
            )
            with patch.dict("os.environ", {"CODEX_CONTEXT_CONTINUITY_HOME": temp_home}), redirect_stdout(io.StringIO()):
                self.assertEqual(seal_main(["--session-dir", str(session_dir), "--cwd", workspace]), 0)

            result = self._run_session_start(temp_home, workspace, source="compact")
            context = result["hookSpecificOutput"]["additionalContext"]
            self.assertIn("continue from this exact step", context)
            self.assertIn("do not repeat completed work without evidence", context)

    def test_seal_rejects_secret_and_oversized_checkpoint(self) -> None:
        with tempfile.TemporaryDirectory() as temp_home, tempfile.TemporaryDirectory() as workspace:
            self._run_session_start(temp_home, workspace)
            session_dir = session_directory("wave1-session", home=Path(temp_home))
            self._run_stop(temp_home, workspace)
            checkpoint = session_dir / "checkpoint.md"
            checkpoint.write_text("password=not-for-storage\n", encoding="utf-8")
            output = io.StringIO()
            with patch.dict("os.environ", {"CODEX_CONTEXT_CONTINUITY_HOME": temp_home}), redirect_stdout(output):
                code = seal_main(["--session-dir", str(session_dir), "--cwd", workspace])
            self.assertEqual(code, 2)
            self.assertEqual(json.loads(output.getvalue())["status"], "invalid")

            checkpoint.write_text("x" * 2401, encoding="utf-8")
            with patch.dict("os.environ", {"CODEX_CONTEXT_CONTINUITY_HOME": temp_home}), redirect_stdout(output := io.StringIO()):
                code = seal_main(["--session-dir", str(session_dir), "--cwd", workspace])
            self.assertEqual(code, 2)
            self.assertEqual(json.loads(output.getvalue())["status"], "invalid")

    def test_seal_rejects_turn_id_that_is_not_pending(self) -> None:
        with tempfile.TemporaryDirectory() as temp_home, tempfile.TemporaryDirectory() as workspace:
            self._run_session_start(temp_home, workspace)
            session_dir = session_directory("wave1-session", home=Path(temp_home))
            self._run_stop(temp_home, workspace)
            (session_dir / "checkpoint.md").write_text("# safe\n- Next: verify\n", encoding="utf-8")

            output = io.StringIO()
            with patch.dict("os.environ", {"CODEX_CONTEXT_CONTINUITY_HOME": temp_home}), redirect_stdout(output):
                code = seal_main(
                    [
                        "--session-dir",
                        str(session_dir),
                        "--cwd",
                        workspace,
                        "--turn-id",
                        "wrong-turn",
                    ]
                )
            self.assertEqual(code, 2)
            self.assertIn("pending turn", json.loads(output.getvalue())["reason"])
            self.assertEqual(read_state(session_dir)["status"], "dirty")

    def test_compact_does_not_inject_tampered_checkpoint(self) -> None:
        with tempfile.TemporaryDirectory() as temp_home, tempfile.TemporaryDirectory() as workspace:
            self._run_session_start(temp_home, workspace)
            session_dir = session_directory("wave1-session", home=Path(temp_home))
            self._run_stop(temp_home, workspace)
            (session_dir / "checkpoint.md").write_text("# sealed\n- Next: verify\n", encoding="utf-8")
            with patch.dict("os.environ", {"CODEX_CONTEXT_CONTINUITY_HOME": temp_home}), redirect_stdout(io.StringIO()):
                self.assertEqual(seal_main(["--session-dir", str(session_dir), "--cwd", workspace]), 0)

            (session_dir / "checkpoint.md").write_text("# tampered\n- Next: do not inject\n", encoding="utf-8")
            result = self._run_session_start(temp_home, workspace, source="compact")
            context = result["hookSpecificOutput"]["additionalContext"]
            self.assertNotIn("do not inject", context)
            self.assertIn("does not match sealed state", context)

    def test_active_stop_ends_without_a_second_continuation(self) -> None:
        with tempfile.TemporaryDirectory() as temp_home, tempfile.TemporaryDirectory() as workspace:
            self._run_session_start(temp_home, workspace)
            result = self._run_stop(temp_home, workspace, active=True)
            self.assertFalse(result.get("continue", True))
            self.assertNotIn("decision", result)
            self.assertIn("avoid a loop", result["systemMessage"])

    def test_non_utf8_checkpoint_is_not_injected(self) -> None:
        with tempfile.TemporaryDirectory() as temp_home, tempfile.TemporaryDirectory() as workspace:
            self._run_session_start(temp_home, workspace)
            session_dir = session_directory("wave1-session", home=Path(temp_home))
            self._run_stop(temp_home, workspace)
            (session_dir / "checkpoint.md").write_text("# valid\n- Next: verify\n", encoding="utf-8")
            with patch.dict("os.environ", {"CODEX_CONTEXT_CONTINUITY_HOME": temp_home}), redirect_stdout(io.StringIO()):
                self.assertEqual(seal_main(["--session-dir", str(session_dir), "--cwd", workspace]), 0)
            (session_dir / "checkpoint.md").write_bytes(b"\xff\xfe")
            result = self._run_session_start(temp_home, workspace, source="compact")
            context = result["hookSpecificOutput"]["additionalContext"]
            self.assertIn("cannot read checkpoint", context)

    def test_invalid_safe_state_is_rejected(self) -> None:
        with tempfile.TemporaryDirectory() as temp_home, tempfile.TemporaryDirectory() as workspace:
            self._run_session_start(temp_home, workspace)
            session_dir = session_directory("wave1-session", home=Path(temp_home))
            state = read_state(session_dir)
            state.update({"status": "safe", "checkpoint_seq": 1, "checkpoint_chars": 10})
            state_path(session_dir).write_text(json.dumps(state), encoding="utf-8")
            result = self._run_session_start(temp_home, workspace, source="resume")
            context = result["hookSpecificOutput"]["additionalContext"]
            self.assertIn("invalid state.json", context)

    def test_corrupt_state_is_not_silently_replaced(self) -> None:
        with tempfile.TemporaryDirectory() as temp_home, tempfile.TemporaryDirectory() as workspace:
            self._run_session_start(temp_home, workspace)
            session_dir = session_directory("wave1-session", home=Path(temp_home))
            (session_dir / "state.json").write_text("{not-json", encoding="utf-8")

            result = self._run_session_start(temp_home, workspace, source="resume")
            context = result["hookSpecificOutput"]["additionalContext"]
            self.assertIn("state.json is missing, invalid, or unreadable", context)
            self.assertEqual((session_dir / "state.json").read_text(encoding="utf-8"), "{not-json")


if __name__ == "__main__":
    unittest.main()
