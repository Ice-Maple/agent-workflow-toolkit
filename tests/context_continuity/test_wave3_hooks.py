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
            "Read": ("read_evidence", False),
            "mcp__domp__get_page": ("read_evidence", False),
            "mcp__domp__update_page": ("write", True),
            "mcp__filesystem__download_file": ("write", True),
            "download_file": ("write", True),
            "read_mcp_resource": ("read_evidence", False),
            "list_mcp_resources": ("read_evidence", False),
            "codex_app__read_thread_terminal": ("read_evidence", False),
            "codex_app__send_message_to_thread": ("write", True),
            "update_plan": ("write", True),
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

    def test_only_simple_read_only_powershell_commands_override_opaque_bash_policy(self) -> None:
        read_only = classify_tool(
            "Bash",
            {"command": "Get-Content -LiteralPath 'C:\\workspace\\AGENTS.md' -Raw"},
        )
        self.assertEqual(read_only, {"category": "read_evidence", "dirty": False})

        unsafe_commands = (
            "Get-Content input.txt | Set-Content output.txt",
            "Get-Content input.txt; Remove-Item input.txt",
            "Get-Content input.txt > output.txt",
            'Get-Content "$(Remove-Item input.txt)"',
        )
        for command in unsafe_commands:
            with self.subTest(command=command):
                result = classify_tool("Bash", {"command": command})
                self.assertEqual(result, {"category": "write", "dirty": True})


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
            marks_dirty=True,
            tool_use_id="tool-use",
            tool_input={"command": "git status"},
        )
        self.assertNotIn("decision", output)
        self.assertNotIn("continue", output)

    def test_pre_tool_use_quietly_ignores_payload_without_session_id(self) -> None:
        for session_id in (None, "", 123):
            with self.subTest(session_id=session_id):
                recorder = Mock(return_value=(None, None, "missing session_id"))
                payload = {
                    "cwd": ".",
                    "turn_id": "turn",
                    "tool_name": "Bash",
                    "tool_input": {"command": "Get-Content -Raw README.md"},
                }
                if session_id is not None:
                    payload["session_id"] = session_id
                with patch("tools.context_continuity.pre_tool_use.record_tool_use", recorder):
                    output = self._run(pre_tool_use_main, payload)
                self.assertEqual(output, {})
                recorder.assert_not_called()

    def test_pre_tool_use_records_simple_get_content_as_read_only(self) -> None:
        recorder = Mock(return_value=(None, None, None))
        payload = {
            "session_id": "session",
            "cwd": ".",
            "turn_id": "turn",
            "tool_name": "Bash",
            "tool_use_id": "tool-use",
            "tool_input": {"command": "Get-Content -LiteralPath 'README.md' -Raw"},
        }
        with patch("tools.context_continuity.pre_tool_use.record_tool_use", recorder):
            output = self._run(pre_tool_use_main, payload)
        self.assertEqual(output, {})
        recorder.assert_called_once_with(
            "session",
            ".",
            "turn",
            "Bash",
            "read_evidence",
            marks_dirty=False,
            tool_use_id="tool-use",
            tool_input={"command": "Get-Content -LiteralPath 'README.md' -Raw"},
        )

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

    def test_pre_tool_use_warning_is_utf8_safe_for_unpaired_surrogates(self) -> None:
        recorder = Mock(side_effect=RuntimeError("bad host value \udcba"))
        payload = {
            "session_id": "session",
            "cwd": ".",
            "turn_id": "turn",
            "tool_name": "Read",
        }
        raw_output = io.BytesIO()
        encoded_output = io.TextIOWrapper(raw_output, encoding="utf-8", errors="strict")
        with patch("tools.context_continuity.pre_tool_use.record_tool_use", recorder), patch(
            "sys.stdin", io.StringIO(json.dumps(payload))
        ), patch("sys.stdout", encoded_output):
            self.assertEqual(pre_tool_use_main(), 0)
            encoded_output.flush()
        output = json.loads(raw_output.getvalue().decode("utf-8"))
        self.assertEqual(set(output), {"systemMessage"})
        self.assertIn("\udcba", output["systemMessage"])

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
