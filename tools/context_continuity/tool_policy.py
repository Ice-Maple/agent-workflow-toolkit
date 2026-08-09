"""Mechanical tool classification for the Wave 3 ``PreToolUse`` hook.

``dirty`` means that externally observable task state may have changed and
therefore needs reconciliation after an interrupted compact. Read/evidence
tools are still recorded, but they do not create that recovery obligation.
Unknown and shell-like tools remain dirty unless a narrow inspection of the
tool input can prove that a single shell command is read-only.

This module has no state or filesystem side effects.  The state module owns
the dirty-generation transition and event persistence; this module only
normalises a tool name into a small, open-ended policy value.
"""

from __future__ import annotations

import re
from typing import Any


_STATE_NAMES = frozenset({"inspect", "seal", "rotate"})
_SHELL_NAMES = frozenset(
    {
        "bash",
        "command",
        "exec",
        "exec_command",
        "execute",
        "run_command",
        "shell",
        "shell_command",
        "terminal",
    }
)
_SIMPLE_READ_ONLY_POWERSHELL_COMMANDS = frozenset(
    {
        "get-childitem",
        "get-content",
        "get-filehash",
        "get-item",
        "get-location",
        "resolve-path",
        "select-string",
        "test-path",
    }
)

# Names emitted by Codex itself, plus the conventional names used by the
# shell/file/image tools in adapters.  Matching is case-insensitive.
_WRITE_NAMES = frozenset(
    {
        "apply_patch",
        "applypatch",
        "bash",
        "computer",
        "computer_use",
        "edit",
        "exec",
        "execute",
        "command",
        "run_command",
        "exec_command",
        "shell",
        "shell_command",
        "terminal",
        "write_file",
        "file_write",
        "write",
    }
)
_READ_NAMES = frozenset(
    {
        "cat",
        "file_read",
        "find",
        "get",
        "git",
        "git_diff",
        "git_log",
        "git_status",
        "glob",
        "grep",
        "head",
        "image",
        "image_view",
        "list",
        "list_files",
        "ls",
        "open",
        "read",
        "read_file",
        "readfile",
        "read_files",
        "ripgrep",
        "search",
        "tail",
        "view_image",
        "view_text",
    }
)

# MCP tool names are namespaced (mcp__server__operation).  Keep these sets
# intentionally explicit: a new operation not covered by the read/write
# vocabulary remains ``unknown`` and therefore dirty.
_MCP_WRITE_WORDS = frozenset(
    {
        "append",
        "apply",
        "approve",
        "cancel",
        "click",
        "clear",
        "close",
        "comment",
        "connect",
        "create",
        "delete",
        "drag",
        "edit",
        "evaluate",
        "execute",
        "fill",
        "insert",
        "import",
        "export",
        "login",
        "logout",
        "launch",
        "mutate",
        "modify",
        "overwrite",
        "patch",
        "post",
        "press",
        "put",
        "reject",
        "record",
        "remove",
        "replace",
        "rename",
        "run",
        "save",
        "select",
        "send",
        "set",
        "start",
        "stop",
        "sync",
        "transition",
        "type",
        "update",
        "upload",
        "write",
        "disconnect",
        "download",
        "publish",
    }
)
_MCP_READ_WORDS = frozenset(
    {
        "browse",
        "capture",
        "check",
        "count",
        "describe",
        "diff",
        "fetch",
        "find",
        "get",
        "glob",
        "history",
        "inspect",
        "list",
        "lookup",
        "log",
        "navigate",
        "open",
        "query",
        "read",
        "resolve",
        "retrieve",
        "search",
        "screenshot",
        "snapshot",
        "status",
        "validate",
        "view",
        "wait",
    }
)


class ToolClassification(dict[str, Any]):
    """Mapping result with convenient attribute access for callers/tests."""

    @property
    def category(self) -> str:
        return str(self["category"])

    @property
    def dirty(self) -> bool:
        return bool(self["dirty"])


def _normalise_name(tool_name: Any) -> str:
    if not isinstance(tool_name, str):
        return ""
    # Host tool names should not contain newlines, but removing them keeps
    # diagnostics bounded if a malformed hook payload is sent.
    return tool_name.strip().replace("\r", " ").replace("\n", " ").lower()


def _last_name_part(name: str) -> str:
    """Return a possible operation name from adapter-qualified names."""

    if "__" in name:
        return name.rsplit("__", 1)[-1]
    return re.split(r"[.:/\\]", name)[-1]


def _qualified_operation_category(operation: str) -> str | None:
    """Classify descriptive local-tool operations such as ``read_file``."""

    words = set(re.findall(r"[a-z0-9]+", operation))
    if words & _MCP_WRITE_WORDS:
        return "write"
    first_word = re.split(r"[_-]", operation, maxsplit=1)[0]
    if first_word in _MCP_READ_WORDS:
        return "read_evidence"
    return None


def _mcp_category(name: str) -> str | None:
    if not name.startswith("mcp__"):
        return None
    pieces = name.split("__")
    if len(pieces) < 3:
        return None
    operation = "_".join(pieces[2:])
    words = set(re.findall(r"[a-z0-9]+", operation))
    # A mixed operation such as read_then_update must never be treated as
    # harmless merely because it contains a read word.
    if words & _MCP_WRITE_WORDS:
        return "write"
    if words & _MCP_READ_WORDS:
        return "read_evidence"
    return None


def _simple_read_only_shell_command(tool_input: Any) -> bool:
    """Recognize a deliberately small side-effect-free PowerShell subset.

    Shell syntax is too broad to classify generally.  This accepts only one
    direct cmdlet invocation with literal arguments.  Pipelines, chaining,
    redirection, interpolation, subexpressions, script blocks, and invocations
    through an operator remain conservatively dirty.
    """

    if not isinstance(tool_input, dict):
        return False
    command = tool_input.get("command")
    if not isinstance(command, str):
        return False
    command = command.strip()
    if not command or len(command) > 32768:
        return False
    if re.search(r"[\r\n;|&<>`$(){}@]", command):
        return False
    match = re.match(r"([A-Za-z][A-Za-z0-9-]*)(?:\s|$)", command)
    return bool(match and match.group(1).lower() in _SIMPLE_READ_ONLY_POWERSHELL_COMMANDS)


def classify_tool(tool_name: Any, tool_input: Any = None) -> ToolClassification:
    """Return ``category`` and whether this tool should make a turn dirty.

    Unknown names intentionally return ``{"category": "unknown", "dirty":
    True}``. Read/evidence tools return ``dirty: False``. ``inspect`` is the
    state-maintenance operation that has no dirty side effect. ``seal`` is
    observed *before* it runs, so it stays dirty until the seal command itself
    succeeds; ``rotate`` starts a new dirty task.
    """

    name = _normalise_name(tool_name)
    operation = _last_name_part(name)
    if name.startswith("mcp__") and "__" in name:
        mcp_operation = "_".join(name.split("__")[2:])
        if mcp_operation in _STATE_NAMES:
            operation = mcp_operation

    if operation in _STATE_NAMES:
        category = "state"
        dirty = operation != "inspect"
    elif (
        not name.startswith("mcp__")
        and (operation in _SHELL_NAMES or name in _SHELL_NAMES)
        and _simple_read_only_shell_command(tool_input)
    ):
        category = "read_evidence"
        dirty = False
    else:
        category = _mcp_category(name)
        if category is None:
            if operation in _WRITE_NAMES or name in _WRITE_NAMES:
                category = "write"
            elif operation in _READ_NAMES or name in _READ_NAMES:
                category = "read_evidence"
            elif name.startswith("git ") or name.startswith("git-") or name.startswith("git_"):
                category = "read_evidence"
            else:
                category = _qualified_operation_category(operation) or "unknown"
        dirty = category != "read_evidence"

    return ToolClassification(category=category, dirty=dirty)


def classify_tool_name(tool_name: Any, tool_input: Any = None) -> ToolClassification:
    """Compatibility alias with a descriptive name for adapter callers."""

    return classify_tool(tool_name, tool_input)


def tool_category(tool_name: Any, tool_input: Any = None) -> str:
    """Return only the category string used by ``state.record_tool_use``."""

    return classify_tool(tool_name, tool_input).category


def is_dirty_tool(tool_name: Any, tool_input: Any = None) -> bool:
    """Return the dirty policy for a tool name."""

    return classify_tool(tool_name, tool_input).dirty


__all__ = [
    "ToolClassification",
    "classify_tool",
    "classify_tool_name",
    "is_dirty_tool",
    "tool_category",
]
