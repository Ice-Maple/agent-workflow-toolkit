from __future__ import annotations

import io
import hashlib
import json
from pathlib import Path
import tempfile
import unittest
from contextlib import redirect_stdout
from unittest.mock import patch

from tools.context_continuity.post_compact import main as post_compact_main
from tools.context_continuity.pre_compact import main as pre_compact_main
from tools.context_continuity.pre_tool_use import main as pre_tool_use_main
from tools.context_continuity.seal import main as seal_main
from tools.context_continuity.session_start import main as session_start_main
from tools.context_continuity.state import (
    MAX_HOT_ESTIMATED_TOKENS,
    MAX_WARM_ESTIMATED_TOKENS,
    _tool_input_digest,
    append_event,
    digest,
    estimate_tokens,
    mark_subagent_started,
    mark_subagent_stopped,
    mark_turn_dirty,
    read_state,
    sanitize_pointer,
    session_directory,
)


class Wave3StateTests(unittest.TestCase):
    def _session_start(
        self,
        home: str,
        workspace: str,
        *,
        source: str = "startup",
        quiet: bool = False,
    ) -> dict:
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
            self.assertEqual(session_start_main(["--quiet-initial"] if quiet else []), 0)
        return json.loads(output.getvalue())

    def _compact(self, home: str, workspace: str, *, phase: str, turn: str = "wave3-turn") -> dict:
        payload = {
            "hook_event_name": "PreCompact" if phase == "pre" else "PostCompact",
            "trigger": "auto",
            "session_id": "wave3-session",
            "cwd": workspace,
            "turn_id": turn,
        }
        output = io.StringIO()
        main = pre_compact_main if phase == "pre" else post_compact_main
        with patch.dict("os.environ", {"CODEX_CONTEXT_CONTINUITY_HOME": home}), patch(
            "sys.stdin", io.StringIO(json.dumps(payload))
        ), redirect_stdout(output):
            code = main(["--mutation-only"]) if phase == "pre" else main()
            self.assertEqual(code, 0)
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

    def test_non_blocking_profile_is_quiet_until_real_recovery_is_needed(self) -> None:
        with tempfile.TemporaryDirectory() as home, tempfile.TemporaryDirectory() as workspace:
            self.assertEqual(self._session_start(home, workspace, quiet=True), {})
            session_dir = session_directory("wave3-session", home=Path(home))
            self.assertEqual(read_state(session_dir)["status"], "clean")

            self.assertEqual(self._compact(home, workspace, phase="pre"), {})
            self.assertEqual(read_state(session_dir)["origin_status"], "safe")
            self.assertFalse((session_dir / "emergency.md").exists())
            self.assertEqual(self._compact(home, workspace, phase="post"), {})
            self.assertEqual(self._session_start(home, workspace, source="compact", quiet=True), {})
            self.assertEqual(read_state(session_dir)["status"], "clean")

            self._pre_tool(home, workspace, name="Bash", turn="write-turn")
            self.assertEqual(read_state(session_dir)["status"], "dirty")
            self._compact(home, workspace, phase="pre", turn="write-turn")
            self._compact(home, workspace, phase="post", turn="write-turn")
            recovery = self._session_start(home, workspace, source="compact", quiet=True)
            self.assertIn("Recovery is required", recovery["hookSpecificOutput"]["additionalContext"])
            self.assertEqual(read_state(session_dir)["status"], "recovery_required")

    def test_pre_tool_use_records_reads_without_marking_dirty_and_unknowns_remain_dirty(self) -> None:
        with tempfile.TemporaryDirectory() as home, tempfile.TemporaryDirectory() as workspace:
            self._session_start(home, workspace)
            self._pre_tool(home, workspace, name="mcp__domp__get_page", tool_input={"password": "DO_NOT_STORE"})
            session_dir = session_directory("wave3-session", home=Path(home))
            state = read_state(session_dir)
            self.assertEqual(state["status"], "clean")
            self.assertEqual(state["last_tool_category"], "read_evidence")
            self.assertEqual(state["last_tool_name"], "mcp__domp__get_page")
            self.assertNotIn("DO_NOT_STORE", json.dumps(state))
            events = (session_dir / "events.jsonl").read_text(encoding="utf-8")
            self.assertIn('"event":"PreToolUse"', events)
            self.assertIn('"category":"read_evidence"', events)
            self.assertIn('"marks_dirty":false', events)

            self._pre_tool(
                home,
                workspace,
                name="Bash",
                tool_input={"command": "Get-Content -LiteralPath 'README.md' -Raw"},
            )
            state = read_state(session_dir)
            self.assertEqual(state["status"], "clean")
            self.assertEqual(state["last_tool_category"], "read_evidence")
            self.assertEqual(state["last_tool_name"], "Bash")

            self._pre_tool(home, workspace, name="new_future_mcp_tool")
            state = read_state(session_dir)
            self.assertEqual(state["status"], "dirty")
            self.assertEqual(state["last_tool_category"], "unknown")
            self.assertGreaterEqual(state["dirty_generation"], 1)

    def test_unicode_surrogates_are_safe_across_tool_state_and_event_boundaries(self) -> None:
        low_surrogate = "\udcba"
        high_surrogate = "\ud83d"
        with tempfile.TemporaryDirectory() as home, tempfile.TemporaryDirectory() as workspace:
            self._session_start(home, workspace)
            output = self._pre_tool(
                home,
                workspace,
                name=f"Read/{low_surrogate}",
                tool_input={
                    "nested": [low_surrogate, {"high": high_surrogate}],
                    "emoji": "😀",
                    "chinese": "中文",
                },
            )
            self.assertEqual(output, {})

            session_dir = session_directory("wave3-session", home=Path(home))
            state = read_state(session_dir)
            self.assertEqual(state["last_tool_name"], f"Read/{low_surrogate}")
            self.assertIsNotNone(state["last_tool_input_hash"])
            events = [
                json.loads(line)
                for line in (session_dir / "events.jsonl").read_text(encoding="utf-8").splitlines()
            ]
            self.assertEqual(events[-1]["tool_name"], f"Read/{low_surrogate}")

    def test_unicode_helpers_preserve_valid_text_and_distinguish_surrogates(self) -> None:
        low_surrogate = "\udcba"
        high_surrogate = "\ud83d"
        self.assertNotEqual(digest(low_surrogate), digest(high_surrogate))
        self.assertNotEqual(digest(low_surrogate), digest(r"\udcba"))
        self.assertEqual(sanitize_pointer(f"path/{low_surrogate}", 100), f"path/{low_surrogate}")
        self.assertNotEqual(sanitize_pointer(low_surrogate, 100), sanitize_pointer(r"\udcba", 100))
        self.assertEqual(sanitize_pointer(f"xxxx{low_surrogate}", 5), f"xxxx{low_surrogate}")

        for valid_text in ("中文", "😀", "中文😀"):
            expected = hashlib.sha256(valid_text.encode("utf-8")).hexdigest()[:16]
            self.assertEqual(digest(valid_text), expected)

        nested = {"value": [low_surrogate, high_surrogate, "中文", "😀"]}
        self.assertEqual(_tool_input_digest(nested), _tool_input_digest(nested))
        self.assertNotEqual(_tool_input_digest(nested), _tool_input_digest({"value": [high_surrogate]}))

        with tempfile.TemporaryDirectory() as home:
            session_dir = Path(home) / "session"
            self.assertIsNone(
                append_event(
                    session_dir,
                    {
                        "event": "Unicode",
                        "path": f"x/{low_surrogate}",
                        "high": high_surrogate,
                        "literal": r"\udcba",
                        "chinese": "中文",
                        "emoji": "😀",
                    },
                )
            )
            event_text = (session_dir / "events.jsonl").read_text(encoding="utf-8")
            self.assertIn(r"\udcba", event_text)
            event = json.loads(event_text)
            self.assertEqual(event["path"], f"x/{low_surrogate}")
            self.assertEqual(event["high"], high_surrogate)
            self.assertEqual(event["literal"], r"\udcba")
            self.assertNotEqual(event["path"].removeprefix("x/"), event["literal"])
            self.assertEqual(event["chinese"], "中文")
            self.assertEqual(event["emoji"], "😀")

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
