"""Wave 1 checkpoint seal CLI."""

from __future__ import annotations

import argparse
import json
from pathlib import Path
import sys

if __package__:
    from .state import seal_checkpoint
else:  # Allow the exact file path printed by SessionStart.
    sys.path.insert(0, str(Path(__file__).resolve().parents[2]))
    from tools.context_continuity.state import seal_checkpoint  # type: ignore[no-redef]


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--session-dir", required=True, type=Path)
    parser.add_argument("--cwd", type=Path, default=None, help="Workspace root/cwd for identity validation.")
    parser.add_argument("--turn-id", default=None, help="Optional current turn id; pending turn is used by default.")
    parser.add_argument("--session-id", default=None, help="Session id from the hook payload for identity validation.")
    return parser


def main(argv: list[str] | None = None) -> int:
    args = build_parser().parse_args(argv)
    code, result = seal_checkpoint(
        args.session_dir,
        cwd=args.cwd or Path.cwd(),
        turn_id=args.turn_id,
        session_id=args.session_id,
    )
    json.dump(result, sys.stdout, ensure_ascii=True, separators=(",", ":"))
    sys.stdout.write("\n")
    return code


if __name__ == "__main__":
    raise SystemExit(main())
