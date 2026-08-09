from __future__ import annotations

import io
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import threading
import time
import unittest
from contextlib import redirect_stdout
from unittest.mock import patch

from tools.context_continuity.pre_tool_use import main as pre_tool_use_main
from tools.context_continuity.session_start import main as session_start_main
from tools.context_continuity.state import (
    ATOMIC_REPLACE_ATTEMPTS,
    _atomic_write_text,
    _is_retryable_replace_error,
    read_state,
    session_directory,
    session_state_lock,
)


class StateLockingTests(unittest.TestCase):
    @staticmethod
    def _finish_process(
        process: subprocess.Popen[str],
        input_text: str | None = None,
        *,
        timeout: float = 5,
    ) -> tuple[str, str, bool]:
        streams = (process.stdin, process.stdout, process.stderr)
        if all(stream is None or stream.closed for stream in streams):
            return "", "", False
        timed_out = False
        try:
            stdout, stderr = process.communicate(input_text, timeout=timeout)
        except subprocess.TimeoutExpired:
            timed_out = True
            try:
                process.terminate()
            except OSError:
                pass
            try:
                stdout, stderr = process.communicate(timeout=3)
            except subprocess.TimeoutExpired:
                try:
                    process.kill()
                except OSError:
                    pass
                stdout, stderr = process.communicate(timeout=3)
        finally:
            for stream in streams:
                if stream is not None and not stream.closed:
                    stream.close()
        return stdout or "", stderr or "", timed_out

    def test_process_cleanup_consumes_and_closes_pipes_after_early_exit(self) -> None:
        process = subprocess.Popen(
            [sys.executable, "-c", "print('done')"],
            stdin=subprocess.PIPE,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            text=True,
        )
        process.wait(timeout=3)
        stdout, stderr, timed_out = self._finish_process(process)
        self.assertEqual(stdout.strip(), "done")
        self.assertEqual(stderr, "")
        self.assertFalse(timed_out)
        self.assertTrue(process.stdin is None or process.stdin.closed)
        self.assertTrue(process.stdout is None or process.stdout.closed)
        self.assertTrue(process.stderr is None or process.stderr.closed)

    def test_session_lock_is_cross_process_and_reentrant(self) -> None:
        child_code = """
import sys
from pathlib import Path
from tools.context_continuity.state import session_state_lock
with session_state_lock(Path(sys.argv[1]), timeout=2.0):
    Path(sys.argv[2]).write_text('locked', encoding='utf-8')
    sys.stdin.read(1)
"""
        with tempfile.TemporaryDirectory() as home:
            session_dir = Path(home) / "session-lock-test"
            ready = Path(home) / "child-ready"
            process = subprocess.Popen(
                [sys.executable, "-c", child_code, str(session_dir), str(ready)],
                stdin=subprocess.PIPE,
                stdout=subprocess.DEVNULL,
                stderr=subprocess.PIPE,
                text=True,
            )
            stderr = ""
            ready_error: str | None = None
            try:
                deadline = time.monotonic() + 5
                while not ready.exists() and process.poll() is None and time.monotonic() < deadline:
                    time.sleep(0.01)
                if not ready.exists():
                    ready_error = "lock holder did not become ready"
                else:
                    with self.assertRaisesRegex(TimeoutError, "waiting for session state lock"):
                        with session_state_lock(session_dir, timeout=0.1):
                            self.fail("a second process must not enter the locked transaction")
            finally:
                _, stderr, timed_out = self._finish_process(process, "x")
            self.assertFalse(timed_out, stderr)
            self.assertIsNone(ready_error, stderr)
            self.assertEqual(process.returncode, 0, stderr)

            with session_state_lock(session_dir):
                with session_state_lock(session_dir):
                    pass

    def test_session_lock_thread_contention_uses_the_same_deadline(self) -> None:
        with tempfile.TemporaryDirectory() as home:
            session_dir = Path(home) / "thread-lock-test"
            entered = threading.Event()
            release = threading.Event()
            failures: list[BaseException] = []

            def hold_lock() -> None:
                try:
                    with session_state_lock(session_dir):
                        entered.set()
                        release.wait(timeout=2)
                except BaseException as exc:  # pragma: no cover - asserted below
                    failures.append(exc)

            holder = threading.Thread(target=hold_lock)
            holder.start()
            self.assertTrue(entered.wait(timeout=2))
            started = time.monotonic()
            try:
                with self.assertRaisesRegex(TimeoutError, "in-process session state lock"):
                    with session_state_lock(session_dir, timeout=0.05):
                        self.fail("a competing thread must honor the lock deadline")
            finally:
                release.set()
                holder.join(timeout=3)
            self.assertLess(time.monotonic() - started, 0.3)
            self.assertFalse(holder.is_alive())
            self.assertEqual(failures, [])

    def test_atomic_write_retries_transient_replace_denial(self) -> None:
        real_replace = os.replace
        attempts = 0

        def flaky_replace(source: Path, destination: Path) -> None:
            nonlocal attempts
            attempts += 1
            if attempts == 1:
                raise PermissionError("transient destination lock")
            real_replace(source, destination)

        with tempfile.TemporaryDirectory() as home:
            destination = Path(home) / "state.json"
            with patch("tools.context_continuity.state.os.replace", side_effect=flaky_replace), patch(
                "tools.context_continuity.state._is_retryable_replace_error", return_value=True
            ), patch("tools.context_continuity.state.time.sleep") as sleep:
                _atomic_write_text(destination, "safe\n")
            self.assertEqual(destination.read_text(encoding="utf-8"), "safe\n")
            self.assertEqual(attempts, 2)
            sleep.assert_called_once()

    def test_atomic_write_does_not_retry_real_permission_denial(self) -> None:
        with tempfile.TemporaryDirectory() as home:
            destination = Path(home) / "state.json"
            with patch(
                "tools.context_continuity.state.os.replace",
                side_effect=PermissionError("persistent ACL denial"),
            ) as replace, patch(
                "tools.context_continuity.state._is_retryable_replace_error",
                return_value=False,
            ), patch("tools.context_continuity.state.time.sleep") as sleep:
                with self.assertRaisesRegex(PermissionError, "persistent ACL denial"):
                    _atomic_write_text(destination, "unsafe\n")
            replace.assert_called_once()
            sleep.assert_not_called()

    def test_atomic_write_surfaces_a_persistent_transient_lock_after_bounded_retries(self) -> None:
        with tempfile.TemporaryDirectory() as home:
            destination = Path(home) / "state.json"
            with patch(
                "tools.context_continuity.state.os.replace",
                side_effect=PermissionError("destination remains locked"),
            ) as replace, patch(
                "tools.context_continuity.state._is_retryable_replace_error",
                return_value=True,
            ), patch("tools.context_continuity.state.time.sleep") as sleep:
                with self.assertRaisesRegex(PermissionError, "destination remains locked"):
                    _atomic_write_text(destination, "unsafe\n")
            self.assertEqual(replace.call_count, ATOMIC_REPLACE_ATTEMPTS)
            self.assertEqual(sleep.call_count, ATOMIC_REPLACE_ATTEMPTS - 1)

    def test_replace_retry_codes_are_limited_to_windows_sharing_failures(self) -> None:
        def windows_error(code: int) -> PermissionError:
            error = PermissionError(f"winerror {code}")
            error.winerror = code  # type: ignore[attr-defined]
            return error

        with patch("tools.context_continuity.state.os.name", "nt"):
            for code in (5, 32, 33):
                self.assertTrue(_is_retryable_replace_error(windows_error(code)), code)
            self.assertFalse(_is_retryable_replace_error(windows_error(87)))
        with patch("tools.context_continuity.state.os.name", "posix"):
            self.assertFalse(_is_retryable_replace_error(windows_error(5)))

    def test_pre_tool_use_fails_open_when_the_session_lock_times_out(self) -> None:
        payload = {
            "hook_event_name": "PreToolUse",
            "session_id": "locked-session",
            "cwd": str(Path(__file__).resolve().parents[2]),
            "turn_id": "locked-turn",
            "tool_name": "Bash",
        }
        output = io.StringIO()
        with patch(
            "tools.context_continuity.state.session_state_lock",
            side_effect=TimeoutError("lock timeout"),
        ), patch("sys.stdin", io.StringIO(json.dumps(payload))), redirect_stdout(output):
            self.assertEqual(pre_tool_use_main(), 0)
        result = json.loads(output.getvalue())
        self.assertEqual(set(result), {"systemMessage"})
        self.assertIn("allowing the tool call", result["systemMessage"])
        self.assertIn("lock timeout", result["systemMessage"])

    def test_session_start_output_is_strict_utf8_with_a_surrogate_in_cwd(self) -> None:
        payload = {
            "hook_event_name": "SessionStart",
            "source": "startup",
            "session_id": "unicode-session-start",
            "cwd": f"workspace-\udcba",
        }
        raw_output = io.BytesIO()
        encoded_output = io.TextIOWrapper(raw_output, encoding="utf-8", errors="strict")
        with tempfile.TemporaryDirectory() as home, patch.dict(
            "os.environ", {"CODEX_CONTEXT_CONTINUITY_HOME": home}
        ), patch(
            "tools.context_continuity.session_start.ensure_session",
            return_value=(Path(home), {"status": "clean", "checkpoint_seq": 0, "checkpoint_hash": None}, None),
        ), patch("sys.stdin", io.StringIO(json.dumps(payload))), patch("sys.stdout", encoded_output):
            self.assertEqual(session_start_main(), 0)
            encoded_output.flush()
        result = json.loads(raw_output.getvalue().decode("utf-8"))
        context = result["hookSpecificOutput"]["additionalContext"]
        self.assertIn("\udcba", context)

    def test_concurrent_pre_tool_hooks_preserve_every_state_transition(self) -> None:
        script = Path(__file__).resolve().parents[2] / "tools" / "context_continuity" / "pre_tool_use.py"
        workspace = str(Path(__file__).resolve().parents[2])
        run_count = 16
        with tempfile.TemporaryDirectory() as home:
            environment = os.environ.copy()
            environment["CODEX_CONTEXT_CONTINUITY_HOME"] = home
            processes: list[subprocess.Popen[str]] = []
            payloads: list[str] = []
            try:
                for index in range(run_count):
                    payloads.append(
                        json.dumps(
                            {
                                "hook_event_name": "PreToolUse",
                                "session_id": "locking-race-session",
                                "cwd": workspace,
                                "turn_id": f"turn-{index}",
                                "tool_name": "Bash",
                                "tool_use_id": f"use-{index}",
                                "tool_input": {"command": f"command-{index}"},
                            }
                        )
                    )
                    processes.append(
                        subprocess.Popen(
                            [sys.executable, str(script)],
                            cwd=workspace,
                            env=environment,
                            stdin=subprocess.PIPE,
                            stdout=subprocess.PIPE,
                            stderr=subprocess.PIPE,
                            text=True,
                        )
                    )
                for process, payload in zip(processes, payloads, strict=True):
                    stdout, stderr, timed_out = self._finish_process(process, payload, timeout=20)
                    self.assertFalse(timed_out, stderr)
                    self.assertEqual(process.returncode, 0, stderr)
                    self.assertEqual(json.loads(stdout), {})
                    self.assertEqual(stderr, "")
            finally:
                for index, process in enumerate(processes):
                    payload = payloads[index] if index < len(payloads) else None
                    self._finish_process(process, payload, timeout=0.2)

            session_dir = session_directory("locking-race-session", home=Path(home))
            state = read_state(session_dir)
            self.assertIsNotNone(state)
            assert state is not None
            self.assertEqual(state["dirty_generation"], run_count)
            events = (session_dir / "events.jsonl").read_text(encoding="utf-8").splitlines()
            self.assertEqual(len(events), run_count)


if __name__ == "__main__":
    unittest.main()
