"""Install and restore the Wave 3 Codex context-continuity configuration.

The installer is deliberately conservative:

* it merges Wave 3 hook groups instead of replacing unrelated hooks;
* it refuses to replace different top-level compaction settings unless
  ``--force`` is supplied;
* every apply creates a timestamped backup and a manifest;
* restore refuses to overwrite files changed after the apply unless
  ``--force`` is supplied.

The script changes only the selected Codex home. It does not set environment
variables, change permissions, or modify the repository being protected.
"""

from __future__ import annotations

import argparse
import copy
import hashlib
import json
import os
import re
import shutil
import sys
import tempfile
from datetime import datetime, timezone
from pathlib import Path
from typing import Any


COMPACT_SCOPE_LINE = 'model_auto_compact_token_limit_scope = "total"'
COMPACT_PROMPT_LINE = (
    'compact_prompt = "Preserve user corrections, hard constraints, '
    'unfinished steps, evidence paths, and the precise next step. Remove '
    'logs, source code, and repeated explanations."'
)
CONFIG_KEYS = {
    "model_auto_compact_token_limit_scope": COMPACT_SCOPE_LINE,
    "compact_prompt": COMPACT_PROMPT_LINE,
}
HOOK_FILE_NAMES = ("config.toml", "hooks.json")
WAVE3_SCRIPT_MARKER = "tools/context_continuity/"


class ConfigurationError(RuntimeError):
    """Raised when a configuration cannot be safely applied or restored."""


def default_codex_home() -> Path:
    configured = os.environ.get("CODEX_HOME")
    if configured:
        return Path(configured).expanduser()
    return Path.home() / ".codex"


def resolve_repo_root(value: str | os.PathLike[str] | None) -> Path:
    if value is None:
        return Path(__file__).resolve().parents[2]
    return Path(value).expanduser().resolve()


def sha256_file(path: Path) -> str | None:
    if not path.exists():
        return None
    if not path.is_file():
        raise ConfigurationError(f"Expected a file, found: {path}")
    digest = hashlib.sha256()
    with path.open("rb") as handle:
        for chunk in iter(lambda: handle.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def ensure_safe_target(path: Path) -> None:
    if path.is_symlink():
        raise ConfigurationError(f"Refusing to replace a symbolic-link target: {path}")


def atomic_write_text(path: Path, content: str) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    temporary_path: str | None = None
    try:
        with tempfile.NamedTemporaryFile(
            mode="w",
            encoding="utf-8",
            newline="",
            dir=path.parent,
            prefix=f".{path.name}.",
            suffix=".tmp",
            delete=False,
        ) as handle:
            temporary_path = handle.name
            handle.write(content)
            handle.flush()
            os.fsync(handle.fileno())
        os.replace(temporary_path, path)
        temporary_path = None
    finally:
        if temporary_path:
            try:
                Path(temporary_path).unlink()
            except FileNotFoundError:
                pass


def atomic_copy_file(source: Path, destination: Path) -> None:
    destination.parent.mkdir(parents=True, exist_ok=True)
    temporary_path: str | None = None
    try:
        with tempfile.NamedTemporaryFile(
            mode="wb",
            dir=destination.parent,
            prefix=f".{destination.name}.",
            suffix=".tmp",
            delete=False,
        ) as handle:
            temporary_path = handle.name
            with source.open("rb") as source_handle:
                shutil.copyfileobj(source_handle, handle)
            handle.flush()
            os.fsync(handle.fileno())
        os.replace(temporary_path, destination)
        temporary_path = None
    finally:
        if temporary_path:
            try:
                Path(temporary_path).unlink()
            except FileNotFoundError:
                pass


def replace_repo_placeholder(value: Any, repo_root: str) -> Any:
    if isinstance(value, str):
        return value.replace("<REPO_ROOT>", repo_root)
    if isinstance(value, list):
        return [replace_repo_placeholder(item, repo_root) for item in value]
    if isinstance(value, dict):
        return {
            key: replace_repo_placeholder(item, repo_root)
            for key, item in value.items()
        }
    return value


def load_wave3_hooks(repo_root: Path) -> dict[str, Any]:
    fixture_path = repo_root / "tests" / "context_continuity" / "fixtures" / "wave3" / "hooks.example.json"
    if not fixture_path.is_file():
        raise ConfigurationError(f"Wave 3 fixture not found: {fixture_path}")
    try:
        fixture = json.loads(fixture_path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError) as exc:
        raise ConfigurationError(f"Cannot read Wave 3 fixture: {fixture_path}") from exc
    rendered = replace_repo_placeholder(fixture, repo_root.as_posix())
    if not isinstance(rendered, dict) or not isinstance(rendered.get("hooks"), dict):
        raise ConfigurationError("Wave 3 fixture does not contain a hooks object")
    return rendered


def is_wave3_hook_group(group: Any) -> bool:
    try:
        serialized = json.dumps(group, ensure_ascii=False).replace("\\", "/")
    except (TypeError, ValueError):
        return False
    serialized = re.sub(r"/+", "/", serialized)
    return WAVE3_SCRIPT_MARKER in serialized


def merge_hooks(existing: dict[str, Any] | None, wave3: dict[str, Any]) -> dict[str, Any]:
    merged: dict[str, Any] = copy.deepcopy(existing) if existing else {}
    existing_hooks = merged.setdefault("hooks", {})
    if not isinstance(existing_hooks, dict):
        raise ConfigurationError("Existing hooks.json has a non-object 'hooks' value")

    wave3_hooks = wave3["hooks"]
    for event_name, groups in wave3_hooks.items():
        if not isinstance(groups, list):
            raise ConfigurationError(f"Wave 3 event is not an array: {event_name}")
        current_groups = existing_hooks.get(event_name, [])
        if not isinstance(current_groups, list):
            raise ConfigurationError(f"Existing hook event is not an array: {event_name}")
        preserved = [group for group in current_groups if not is_wave3_hook_group(group)]
        existing_hooks[event_name] = preserved + copy.deepcopy(groups)

    merged.setdefault("description", wave3.get("description", "Wave 3 context continuity hooks"))
    return merged


def _top_level_prefix_lines(text: str) -> tuple[list[str], list[str]]:
    lines = text.splitlines(keepends=True)
    first_table = len(lines)
    for index, line in enumerate(lines):
        stripped = line.lstrip()
        if stripped.startswith("[") and not stripped.startswith("#"):
            first_table = index
            break
    return lines[:first_table], lines[first_table:]


def upsert_config_settings(text: str, *, force: bool) -> str:
    prefix, suffix = _top_level_prefix_lines(text)
    newline = "\r\n" if "\r\n" in text else "\n"
    prefix_text = "".join(prefix)

    for key, desired_line in CONFIG_KEYS.items():
        matches = [
            index
            for index, line in enumerate(prefix)
            if re.match(rf"^\s*{re.escape(key)}\s*=", line)
        ]
        if len(matches) > 1:
            raise ConfigurationError(f"Duplicate top-level setting in config.toml: {key}")
        if not matches:
            continue
        index = matches[0]
        current_line = prefix[index].strip()
        if '"""' in current_line or "'''" in current_line:
            raise ConfigurationError(
                f"Multiline setting requires manual review before updating: {key}"
            )
        if current_line != desired_line:
            if not force:
                raise ConfigurationError(
                    f"config.toml already defines a different {key}; "
                    "review it or rerun with --force"
                )
            prefix[index] = desired_line + newline

    missing = [
        desired_line
        for key, desired_line in CONFIG_KEYS.items()
        if not any(re.match(rf"^\s*{re.escape(key)}\s*=", line) for line in prefix)
    ]
    if missing:
        if prefix and prefix[-1].strip():
            prefix.append(newline)
        prefix.extend(line + newline for line in missing)
        prefix.append(newline)

    return "".join(prefix + suffix)


def create_backup(codex_home: Path, paths: dict[str, Path], repo_root: Path) -> tuple[Path, dict[str, Any]]:
    backup_root = codex_home / "backups"
    backup_root.mkdir(parents=True, exist_ok=True)
    stamp = datetime.now().strftime("%Y%m%d-%H%M%S")
    backup_dir = backup_root / f"context-continuity-{stamp}"
    suffix = 1
    while backup_dir.exists():
        backup_dir = backup_root / f"context-continuity-{stamp}-{suffix}"
        suffix += 1
    backup_dir.mkdir()

    files: dict[str, Any] = {}
    for name, path in paths.items():
        before_hash = sha256_file(path)
        existed = before_hash is not None
        backup_name = name if existed else None
        if existed:
            atomic_copy_file(path, backup_dir / name)
        files[name] = {
            "target_name": name,
            "existed_before": existed,
            "before_sha256": before_hash,
            "backup_name": backup_name,
        }

    manifest = {
        "version": 1,
        "created_at": datetime.now(timezone.utc).isoformat(),
        "repo_root": str(repo_root),
        "codex_home": str(codex_home),
        "files": files,
    }
    return backup_dir, manifest


def apply_global(
    repo_root: Path,
    codex_home: Path,
    *,
    force: bool = False,
    dry_run: bool = False,
) -> Path | None:
    repo_root = repo_root.resolve()
    codex_home = codex_home.expanduser().resolve()
    config_path = codex_home / "config.toml"
    hooks_path = codex_home / "hooks.json"
    paths = {"config.toml": config_path, "hooks.json": hooks_path}
    for path in paths.values():
        ensure_safe_target(path)

    config_text = config_path.read_text(encoding="utf-8") if config_path.exists() else ""
    updated_config = upsert_config_settings(config_text, force=force)

    existing_hooks: dict[str, Any] | None = None
    if hooks_path.exists():
        try:
            existing_hooks = json.loads(hooks_path.read_text(encoding="utf-8"))
        except (OSError, json.JSONDecodeError) as exc:
            raise ConfigurationError(f"Cannot parse existing hooks.json: {hooks_path}") from exc
        if not isinstance(existing_hooks, dict):
            raise ConfigurationError(f"Existing hooks.json must contain a JSON object: {hooks_path}")

    updated_hooks = merge_hooks(existing_hooks, load_wave3_hooks(repo_root))
    updated_hooks_text = json.dumps(updated_hooks, ensure_ascii=False, indent=2) + "\n"

    if dry_run:
        print(f"CODEX_HOME={codex_home}")
        print(f"CONFIG_TARGET={config_path}")
        print(f"HOOKS_TARGET={hooks_path}")
        print("DRY_RUN=True")
        return None

    codex_home.mkdir(parents=True, exist_ok=True)
    backup_dir, manifest = create_backup(codex_home, paths, repo_root)
    atomic_write_text(config_path, updated_config)
    atomic_write_text(hooks_path, updated_hooks_text)
    manifest["files"]["config.toml"]["after_sha256"] = sha256_file(config_path)
    manifest["files"]["hooks.json"]["after_sha256"] = sha256_file(hooks_path)
    atomic_write_text(
        backup_dir / "manifest.json",
        json.dumps(manifest, ensure_ascii=False, indent=2) + "\n",
    )
    print(f"BACKUP_DIR={backup_dir}")
    print(f"CONFIG_TARGET={config_path}")
    print(f"HOOKS_TARGET={hooks_path}")
    print("APPLIED=True")
    return backup_dir


def _load_manifest(backup_dir: Path) -> dict[str, Any]:
    manifest_path = backup_dir / "manifest.json"
    try:
        manifest = json.loads(manifest_path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError) as exc:
        raise ConfigurationError(f"Cannot read backup manifest: {manifest_path}") from exc
    if not isinstance(manifest, dict) or manifest.get("version") != 1:
        raise ConfigurationError(f"Unsupported backup manifest: {manifest_path}")
    if not isinstance(manifest.get("files"), dict):
        raise ConfigurationError(f"Backup manifest has no files map: {manifest_path}")
    return manifest


def restore_global(
    backup_dir: Path,
    codex_home: Path | None = None,
    *,
    force: bool = False,
    dry_run: bool = False,
) -> None:
    backup_dir = backup_dir.expanduser().resolve()
    manifest = _load_manifest(backup_dir)
    manifest_home = manifest.get("codex_home")
    if not isinstance(manifest_home, str) or not manifest_home:
        raise ConfigurationError("Backup manifest has no valid codex_home")
    target_home = (codex_home or Path(manifest_home)).expanduser().resolve()
    files = manifest["files"]

    actions: list[tuple[str, Path, dict[str, Any]]] = []
    for name in HOOK_FILE_NAMES:
        details = files.get(name)
        if not isinstance(details, dict):
            raise ConfigurationError(f"Backup manifest is missing {name}")
        target = target_home / name
        ensure_safe_target(target)
        expected_after = details.get("after_sha256")
        current_hash = sha256_file(target)
        if expected_after != current_hash and not force:
            raise ConfigurationError(
                f"Refusing to restore {target}: it changed after apply; "
                "rerun with --force only after reviewing it"
            )
        actions.append((name, target, details))

    if dry_run:
        print(f"BACKUP_DIR={backup_dir}")
        print(f"CODEX_HOME={target_home}")
        print("DRY_RUN=True")
        return

    for name, target, details in actions:
        if details.get("existed_before"):
            backup_name = details.get("backup_name")
            if not isinstance(backup_name, str):
                raise ConfigurationError(f"Backup entry has no source file: {name}")
            source = backup_dir / backup_name
            if not source.is_file():
                raise ConfigurationError(f"Backup file not found: {source}")
            atomic_copy_file(source, target)
        elif target.exists():
            if not target.is_file():
                raise ConfigurationError(f"Refusing to remove non-file target: {target}")
            target.unlink()

    print(f"BACKUP_DIR={backup_dir}")
    print(f"CODEX_HOME={target_home}")
    print("RESTORED=True")


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(
        description="Apply or restore Wave 3 Codex context-continuity global settings."
    )
    subparsers = parser.add_subparsers(dest="command", required=True)

    apply_parser = subparsers.add_parser("apply", help="merge and install the global settings")
    apply_parser.add_argument("--repo-root", help="repository root; defaults to this repository")
    apply_parser.add_argument("--codex-home", help="Codex home; defaults to CODEX_HOME or ~/.codex")
    apply_parser.add_argument("--force", action="store_true", help="replace conflicting compaction settings")
    apply_parser.add_argument("--dry-run", action="store_true", help="show targets without writing")

    restore_parser = subparsers.add_parser("restore", help="restore a backup created by apply")
    restore_parser.add_argument("--backup-dir", required=True, help="backup directory containing manifest.json")
    restore_parser.add_argument("--codex-home", help="restore into this Codex home instead of the manifest home")
    restore_parser.add_argument("--force", action="store_true", help="restore even if targets changed after apply")
    restore_parser.add_argument("--dry-run", action="store_true", help="show targets without writing")
    return parser


def main(argv: list[str] | None = None) -> int:
    args = build_parser().parse_args(argv)
    try:
        if args.command == "apply":
            repo_root = resolve_repo_root(args.repo_root)
            codex_home = (
                Path(args.codex_home).expanduser().resolve()
                if args.codex_home
                else default_codex_home().resolve()
            )
            apply_global(
                repo_root,
                codex_home,
                force=args.force,
                dry_run=args.dry_run,
            )
        else:
            codex_home = Path(args.codex_home).expanduser().resolve() if args.codex_home else None
            restore_global(
                Path(args.backup_dir),
                codex_home,
                force=args.force,
                dry_run=args.dry_run,
            )
        return 0
    except ConfigurationError as exc:
        print(f"ERROR: {exc}", file=sys.stderr)
        return 2
    except OSError as exc:
        print(f"ERROR: filesystem operation failed: {exc}", file=sys.stderr)
        return 2


if __name__ == "__main__":
    raise SystemExit(main())
