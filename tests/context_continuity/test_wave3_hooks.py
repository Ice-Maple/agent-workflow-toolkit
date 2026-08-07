from __future__ import annotations

import io
import json
import unittest
from contextlib import redirect_stdout
from unittest.mock import Mock, patch

from tools.context_continuity.pre_tool_use import main as pre_tool_use_main
from tools.context_continuity.subagent_start import main as subagent_start_main
from tools.context_continuity.subagent_stop import main as subagent_stop_main
from tools.context_continuity.tool_policy import classify_tool


class Wave3ToolPolicyTests(unittest.TestCase):
    def test_explicit_tool_categories_and_dirty_policy(self) -> None:
        expected = {
            "Bash": ("write", True),
            "apply_patch": ("write", True),
            "Edit": ("write", True),
            "Write": ("write", True),
            "Read": ("read_evidence", True),
            "mcp__domp__get_page": ("read_evidence", True),
            "mcp__domp__update_page": ("write", True),
            "mcp__continuity__inspect": ("state", False),
            "inspect": ("state", False),
            "seal": ("state", True),
            "rotate": ("state", True),
            "new_future_tool": ("unknown", True),
        }
        for name, (category, dirty) in expected.items():
            result = classify_tool(name)
            self.assertEqual(result["category"], category, name)
            self.assertEqual(result["dirty"], dirty, name)


class Wave3HookTests(unittest.TestCase):
    @staticmethod
    def _run(main, payload: object) -> dict:
        output = io.StringIO()
        with patch("sys.stdin", io.StringIO(json.dumps(payload))), redirect_stdout(output):
            result = main()
        assert result == 0
        return json.loads(output.getvalue())

    def test_pre_tool_use_records_category_without_control_fields(self) -> None:
        recorder = Mock(return_value=(None, None, None))
        payload = {
            "session_id": "session",
            "cwd": ".",
            "turn_id": "turn",
            "tool_name": "Bash",
            "tool_use_id": "tool-use",
            "tool_input": {"command": "git status"},
        }
        with patch("tools.context_continuity.pre_tool_use.record_tool_use", recorder):
            output = self._run(pre_tool_use_main, payload)
        self.assertEqual(output, {})
        recorder.assert_called_once_with(
            "session",
            ".",
            "turn",
            "Bash",
            "write",
            tool_use_id="tool-use",
            tool_input={"command": "git status"},
        )
        self.assertNotIn("decision", output)
        self.assertNotIn("continue", output)

    def test_pre_tool_use_accepts_a_utf8_bom_from_windows_pipes(self) -> None:
        recorder = Mock(return_value=(None, None, None))
        payload = {
            "session_id": "session",
            "cwd": ".",
            "turn_id": "turn",
            "tool_name": "Read",
        }
        output = io.StringIO()
        with patch("tools.context_continuity.pre_tool_use.record_tool_use", recorder), patch(
            "sys.stdin", io.StringIO("\ufeff" + json.dumps(payload))
        ), redirect_stdout(output):
            self.assertEqual(pre_tool_use_main(), 0)
        self.assertEqual(json.loads(output.getvalue()), {})
        recorder.assert_called_once()

    def test_subagent_hooks_call_state_interfaces_and_fail_open_on_missing_fields(self) -> None:
        started = Mock(return_value=(None, None, None))
        stopped = Mock(return_value=(None, None, None))
        with patch("tools.context_continuity.subagent_start.mark_subagent_started", started):
            output = self._run(
                subagent_start_main,
                {
                    "session_id": "session",
                    "cwd": ".",
                    "turn_id": "turn",
                    "agent_id": "agent",
                    "agent_type": "researcher",
                },
            )
        self.assertEqual(output, {})
        started.assert_called_once_with("session", ".", "turn", "agent", "researcher")

        with patch("tools.context_continuity.subagent_stop.mark_subagent_stopped", stopped):
            output = self._run(
                subagent_stop_main,
                {
                    "session_id": "session",
                    "cwd": ".",
                    "turn_id": "turn",
                    "agent_id": "agent",
                    "agent_type": "researcher",
                    "last_assistant_message": "done",
                },
            )
        self.assertEqual(output, {})
        stopped.assert_called_once_with(
            "session", ".", "turn", "agent", "researcher", last_assistant_message="done"
        )

        # Missing payload fields are reported as a warning at most; no hook
        # control keys may accidentally turn this into a blocked stop.
        with patch("tools.context_continuity.subagent_stop.mark_subagent_stopped", stopped):
            output = self._run(subagent_stop_main, {})
        self.assertNotIn("decision", output)
        self.assertNotIn("continue", output)


if __name__ == "__main__":
    unittest.main()
