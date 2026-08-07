#!/usr/bin/env python3
"""Validate the repository's brainstorming skill package."""

from __future__ import annotations

from pathlib import Path
import sys


REPO_ROOT = Path(__file__).resolve().parents[2]
SKILL_DIR = REPO_ROOT / "skills" / "brainstorming"
SKILL_FILE = SKILL_DIR / "SKILL.md"
MAX_LINES = 200
REQUIRED_LOCAL_FILES = ("visual-companion.md",)


def fail(message: str) -> None:
    print(f"FAIL: {message}", file=sys.stderr)
    raise SystemExit(1)


def parse_frontmatter(lines: list[str]) -> dict[str, str]:
    if not lines or lines[0] != "---":
        fail("SKILL.md must start with YAML frontmatter")

    try:
        end = lines.index("---", 1)
    except ValueError:
        fail("SKILL.md frontmatter is not closed")

    fields: dict[str, str] = {}
    for line_number, line in enumerate(lines[1:end], start=2):
        if not line.strip() or line.lstrip().startswith("#"):
            continue
        if ":" not in line:
            fail(f"invalid frontmatter entry on line {line_number}")
        key, value = line.split(":", 1)
        key = key.strip()
        value = value.strip()
        if not key or not value:
            fail(f"empty frontmatter key or value on line {line_number}")
        if key in fields:
            fail(f"duplicate frontmatter field: {key}")
        fields[key] = value
    return fields


def main() -> None:
    if not SKILL_FILE.is_file():
        fail(f"missing skill file: {SKILL_FILE}")

    text = SKILL_FILE.read_text(encoding="utf-8")
    lines = text.splitlines()
    if len(lines) > MAX_LINES:
        fail(f"SKILL.md has {len(lines)} lines; maximum is {MAX_LINES}")

    fields = parse_frontmatter(lines)
    expected_fields = {"name", "description"}
    if set(fields) != expected_fields:
        fail(
            "frontmatter fields must be exactly name and description; "
            f"found: {', '.join(sorted(fields))}"
        )
    if fields["name"] != "brainstorming":
        fail("frontmatter name must be brainstorming")

    for relative_path in REQUIRED_LOCAL_FILES:
        linked_file = SKILL_DIR / relative_path
        if not linked_file.is_file():
            fail(f"missing referenced local file: {linked_file}")
        if f"`{relative_path}`" not in text:
            fail(f"SKILL.md does not reference required local file: {relative_path}")

    print(
        "PASS: brainstorming skill structure is valid "
        f"({len(lines)} lines, frontmatter fields: name, description)"
    )


if __name__ == "__main__":
    main()
