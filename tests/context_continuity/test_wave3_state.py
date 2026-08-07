from __future__ import annotations

import io
import json
from pathlib import Path
import tempfile
import unittest
from contextlib import redirect_stdout
from unittest.mock import patch

from tools.context_continuity.pre_tool_use import main as pre_tool_use_main
from tools.context_continuity.seal import main as seal_main
from tools.context_continuity.session_start import main as session_start_main
from tools.context_continuity.state import (
    MAX_HOT_ESTIMATED_TOKENS,
    MAX_WARM_ESTIMATED_TOKENS,
    estimate_tokens,
    mark_subagent_started,
    mark_subagent_stopped,
    mark_turn_dirty,
    read_state,
    session_directory,
)


class Wave3StateTests(unittest.TestCase):
    def _session_start(self, home: str, workspace: str, *, source: str = "startup") -> dict:
        payload = {
            "hook_event_name": "SessionStart",
            "source": source,
            "session_id": "wave3-session",
            "cwd": workspace,
        }
        output = io.StringIO()
        with patch.dict("os.environ", {"CODEX_CONTEXT_CONTINUITY_HOME": home}), patch(
            "sys.stdin", io.StringIO(json.dumps(payload))
        ), redirect_stdout(output):
            self.assertEqual(session_start_main(), 0)
        return json.loads(output.getvalue())

    def _pre_tool(self, home: str, workspace: str, *, name: str, turn: str = "wave3-turn", tool_input=None) -> dict:
        payload = {
            "hook_event_name": "PreToolUse",
            "session_id": "wave3-session",
            "cwd": workspace,
            "turn_id": turn,
            "tool_name": name,
            "tool_use_id": f"use-{name}",
            "tool_input": tool_input,
        }
        output = io.StringIO()
        with patch.dict("os.environ", {"CODEX_CONTEXT_CONTINUITY_HOME": home}), patch(
            "sys.stdin", io.StringIO(json.dumps(payload))
        ), redirect_stdout(output):
            self.assertEqual(pre_tool_use_main(), 0)
        return json.loads(output.getvalue())

    def _seal(self, home: str, workspace: str, session_dir: Path, content: str, *, hot: str | None = None) -> tuple[int, dict]:
        (session_dir / "checkpoint.md").write_text(content, encoding="utf-8")
        if hot is not None:
            (session_dir / "hot.md").write_text(hot, encoding="utf-8")
        output = io.StringIO()
        with patch.dict("os.environ", {"CODEX_CONTEXT_CONTINUITY_HOME": home}), redirect_stdout(output):
            code = seal_main(
                [
                    "--session-dir",
                    str(session_dir),
                    "--cwd",
                    workspace,
                    "--session-id",
                    "wave3-session",
                ]
            )
        return code, json.loads(output.getvalue())

    def _prepare(self, home: str, workspace: str, content: str = "# Goal\n- Next: verify\n") -> tuple[Path, dict]:
        self._session_start(home, workspace)
        session_dir = session_directory("wave3-session", home=Path(home))
        # Stop's first continuation is represented by the pending turn in the
        # existing state contract; avoid coupling this test to its UI wording.
        self._pre_tool(home, workspace, name="Bash")
        code, result = self._seal(home, workspace, session_dir, content)
        self.assertEqual(code, 0, result)
        return session_dir, result

    def test_pre_tool_use_marks_write_read_and_unknown_dirty_without_raw_input(self) -> None:
        with tempfile.TemporaryDirectory() as home, tempfile.TemporaryDirectory() as workspace:
            self._session_start(home, workspace)
            self._pre_tool(home, workspace, name="mcp__domp__get_page", tool_input={"password": "DO_NOT_STORE"})
            session_dir = session_directory("wave3-session", home=Path(home))
            state = read_state(session_dir)
            self.assertEqual(state["status"], "dirty")
            self.assertEqual(state["last_tool_category"], "read_evidence")
            self.assertEqual(state["last_tool_name"], "mcp__domp__get_page")
            self.assertNotIn("DO_NOT_STORE", json.dumps(state))
            events = (session_dir / "events.jsonl").read_text(encoding="utf-8")
            self.assertIn('"event":"PreToolUse"', events)
            self.assertIn('"category":"read_evidence"', events)

            self._pre_tool(home, workspace, name="new_future_mcp_tool")
            state = read_state(session_dir)
            self.assertEqual(state["last_tool_category"], "unknown")
            self.assertGreaterEqual(state["dirty_generation"], 1)

    def test_inspect_is_the_only_non_dirty_state_operation(self) -> None:
        with tempfile.TemporaryDirectory() as home, tempfile.TemporaryDirectory() as workspace:
            session_dir, _ = self._prepare(home, workspace)
            self.assertEqual(read_state(session_dir)["status"], "safe")
            self._pre_tool(home, workspace, name="inspect")
            self.assertEqual(read_state(session_dir)["status"], "safe")
            self._pre_tool(home, workspace, name="mcp__continuity__inspect")
            self.assertEqual(read_state(session_dir)["status"], "safe")
            self._pre_tool(home, workspace, name="rotate")
            self.assertEqual(read_state(session_dir)["status"], "dirty")

    def test_repeated_stop_observation_of_same_dirty_turn_does_not_advance_generation(self) -> None:
        with tempfile.TemporaryDirectory() as home, tempfile.TemporaryDirectory() as workspace:
            self._session_start(home, workspace)
            with patch.dict("os.environ", {"CODEX_CONTEXT_CONTINUITY_HOME": home}):
                _, state, error = mark_turn_dirty("wave3-session", workspace, "wave3-turn")
            self.assertIsNone(error)
            first_generation = state["dirty_generation"]
            with patch.dict("os.environ", {"CODEX_CONTEXT_CONTINUITY_HOME": home}):
                _, state, error = mark_turn_dirty("wave3-session", workspace, "wave3-turn")
            self.assertIsNone(error)
            self.assertEqual(state["dirty_generation"], first_generation)

    def test_warm_and_hot_budgets_are_enforced_without_truncation(self) -> None:
        with tempfile.TemporaryDirectory() as home, tempfile.TemporaryDirectory() as workspace:
            self._session_start(home, workspace)
            session_dir = session_directory("wave3-session", home=Path(home))
            self._pre_tool(home, workspace, name="Bash")

            warm_too_large = "# Goal\n" + ("界" * 1300)
            self.assertGreater(estimate_tokens(warm_too_large), MAX_WARM_ESTIMATED_TOKENS)
            code, result = self._seal(home, workspace, session_dir, warm_too_large)
            self.assertEqual(code, 2)
            self.assertIn("estimated tokens", result["reason"])
            self.assertEqual(read_state(session_dir)["status"], "dirty")

            hot_too_large = "# Hot\n" + ("界" * 600)
            self.assertGreater(estimate_tokens(hot_too_large), MAX_HOT_ESTIMATED_TOKENS)
            code, result = self._seal(home, workspace, session_dir, "# Goal\n- Next: verify\n", hot=hot_too_large)
            self.assertEqual(code, 2)
            self.assertIn("hot state", result["reason"])

    def test_large_warm_state_is_not_spilled_into_compact_context(self) -> None:
        with tempfile.TemporaryDirectory() as home, tempfile.TemporaryDirectory() as workspace:
            warm = "# Goal\n" + ("界" * 900)
            self.assertLessEqual(estimate_tokens(warm), MAX_WARM_ESTIMATED_TOKENS)
            session_dir, result = self._prepare(home, workspace, warm)
            self.assertEqual(result["hot_estimated_tokens"], 0)
            context = self._session_start(home, workspace, source="compact")["hookSpecificOutput"]["additionalContext"]
            self.assertNotIn("界" * 100, context)
            self.assertIn("Hot state", context)
            self.assertIn("Checkpoint file", context)
            self.assertEqual(read_state(session_dir)["status"], "safe")

    def test_subagent_lifecycle_keeps_root_dirty_until_reconciled(self) -> None:
        with tempfile.TemporaryDirectory() as home, tempfile.TemporaryDirectory() as workspace:
            self._session_start(home, workspace)
            session_dir = session_directory("wave3-session", home=Path(home))
            with patch.dict("os.environ", {"CODEX_CONTEXT_CONTINUITY_HOME": home}):
                _, state, error = mark_subagent_started(
                    "wave3-session", workspace, "wave3-turn", "agent-1", "researcher"
                )
            self.assertIsNone(error)
            self.assertEqual(state["status"], "dirty")
            self.assertEqual(len(state["active_subagents"]), 1)
            code, result = self._seal(home, workspace, session_dir, "# Goal\n- Next: reconcile\n")
            self.assertEqual(code, 2)
            self.assertIn("subagent", result["reason"])

            with patch.dict("os.environ", {"CODEX_CONTEXT_CONTINUITY_HOME": home}):
                _, state, error = mark_subagent_stopped(
                    "wave3-session", workspace, "wave3-turn", "agent-1", "researcher", "done"
                )
            self.assertIsNone(error)
            self.assertEqual(state["last_subagent_status"], "completed")
            self.assertEqual(state["active_subagents"], [])
            code, result = self._seal(home, workspace, session_dir, "# Goal\n- Next: continue after agent\n")
            self.assertEqual(code, 0, result)
            self.assertEqual(read_state(session_dir)["status"], "safe")

    def test_orphan_subagent_stop_cannot_be_sealed_as_safe(self) -> None:
        with tempfile.TemporaryDirectory() as home, tempfile.TemporaryDirectory() as workspace:
            self._session_start(home, workspace)
            session_dir = session_directory("wave3-session", home=Path(home))
            with patch.dict("os.environ", {"CODEX_CONTEXT_CONTINUITY_HOME": home}):
                _, state, error = mark_subagent_stopped(
                    "wave3-session", workspace, "wave3-turn", "unknown-agent", "unknown", "lost"
                )
            self.assertIsNone(error)
            self.assertEqual(state["last_subagent_status"], "unknown")
            self.assertEqual(len(state["active_subagents"]), 1)
            with patch.dict("os.environ", {"CODEX_CONTEXT_CONTINUITY_HOME": home}):
                _, state, error = mark_subagent_started(
                    "wave3-session", workspace, "wave3-turn", "agent-2", "researcher"
                )
            self.assertIsNone(error)
            with patch.dict("os.environ", {"CODEX_CONTEXT_CONTINUITY_HOME": home}):
                _, state, error = mark_subagent_stopped(
                    "wave3-session", workspace, "wave3-turn", "agent-2", "researcher", "done"
                )
            self.assertIsNone(error)
            self.assertEqual(len(state["active_subagents"]), 1)
            code, result = self._seal(home, workspace, session_dir, "# Goal\n- Next: investigate\n")
            self.assertEqual(code, 2)
            self.assertIn("unknown", result["reason"])


if __name__ == "__main__":
    unittest.main()
