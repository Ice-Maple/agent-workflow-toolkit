import json
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

import tools.context_continuity.install_global as installer_module
from tools.context_continuity.install_global import (
    ConfigurationError,
    apply_global,
    load_wave3_hooks,
    restore_global,
)


REPO_ROOT = Path(__file__).resolve().parents[2]


class GlobalInstallTests(unittest.TestCase):
    def test_rendered_hooks_use_absolute_repo_paths(self) -> None:
        hooks = load_wave3_hooks(REPO_ROOT)
        serialized = json.dumps(hooks)
        self.assertNotIn("<REPO_ROOT>", serialized)
        self.assertIn(REPO_ROOT.as_posix(), serialized)
        self.assertIn("PreCompact", hooks["hooks"])
        self.assertNotIn("Stop", hooks["hooks"])
        self.assertNotIn("statusMessage", serialized)

    def test_apply_merges_unrelated_hooks_and_is_idempotent(self) -> None:
        with tempfile.TemporaryDirectory() as temp_dir:
            codex_home = Path(temp_dir) / "codex"
            codex_home.mkdir()
            (codex_home / "config.toml").write_text(
                "# keep this\n[model_providers.example]\nname = \"keep\"\n",
                encoding="utf-8",
            )
            (codex_home / "hooks.json").write_text(
                json.dumps(
                    {
                        "description": "existing hooks",
                        "hooks": {
                            "Stop": [
                                {
                                    "matcher": "existing",
                                    "hooks": [
                                        {
                                            "type": "command",
                                            "command": "python3 /old/tools/context_continuity/stop.py",
                                        },
                                        {
                                            "type": "command",
                                            "command": "python3 /keep/notify.py",
                                        },
                                    ],
                                }
                            ],
                            "Custom": [{"matcher": ".*", "hooks": []}],
                        },
                    }
                ),
                encoding="utf-8",
            )

            first_backup = apply_global(REPO_ROOT, codex_home)
            self.assertIsNotNone(first_backup)
            first_hooks = json.loads((codex_home / "hooks.json").read_text(encoding="utf-8"))
            expected_stop = [
                {
                    "matcher": "existing",
                    "hooks": [{"type": "command", "command": "python3 /keep/notify.py"}],
                }
            ]
            self.assertEqual(first_hooks["hooks"]["Stop"], expected_stop)
            self.assertIn("Custom", first_hooks["hooks"])
            config_text = (codex_home / "config.toml").read_text(encoding="utf-8")
            self.assertNotIn("model_auto_compact_token_limit_scope", config_text)
            self.assertNotIn("compact_prompt", config_text)

            second_backup = apply_global(REPO_ROOT, codex_home)
            self.assertIsNotNone(second_backup)
            second_hooks = json.loads((codex_home / "hooks.json").read_text(encoding="utf-8"))
            self.assertEqual(second_hooks["hooks"]["Stop"], expected_stop)
            self.assertEqual(second_hooks["hooks"]["Custom"], first_hooks["hooks"]["Custom"])

    def test_user_compact_setting_is_preserved(self) -> None:
        with tempfile.TemporaryDirectory() as temp_dir:
            codex_home = Path(temp_dir) / "codex"
            codex_home.mkdir()
            (codex_home / "config.toml").write_text(
                'compact_prompt = "my existing prompt"\n',
                encoding="utf-8",
            )
            apply_global(REPO_ROOT, codex_home)
            self.assertIn(
                'compact_prompt = "my existing prompt"',
                (codex_home / "config.toml").read_text(encoding="utf-8"),
            )

    def test_legacy_managed_compact_settings_are_removed(self) -> None:
        with tempfile.TemporaryDirectory() as temp_dir:
            codex_home = Path(temp_dir) / "codex"
            codex_home.mkdir()
            (codex_home / "config.toml").write_text(
                'model_auto_compact_token_limit_scope = "total"\n'
                'compact_prompt = "Preserve user corrections, hard constraints, unfinished steps, evidence paths, and the precise next step. Remove logs, source code, and repeated explanations."\n'
                'model = "keep"\n',
                encoding="utf-8",
            )
            apply_global(
                REPO_ROOT,
                codex_home,
                remove_legacy_compact_overrides=True,
            )
            config = (codex_home / "config.toml").read_text(encoding="utf-8")
            self.assertEqual(config, 'model = "keep"\n')

    def test_legacy_exact_values_require_explicit_removal_flag(self) -> None:
        with tempfile.TemporaryDirectory() as temp_dir:
            codex_home = Path(temp_dir) / "codex"
            codex_home.mkdir()
            legacy = 'model_auto_compact_token_limit_scope = "total"\n'
            (codex_home / "config.toml").write_text(legacy, encoding="utf-8")
            apply_global(REPO_ROOT, codex_home)
            self.assertEqual((codex_home / "config.toml").read_text(encoding="utf-8"), legacy)

    def test_restore_returns_original_files_and_removes_new_hooks(self) -> None:
        with tempfile.TemporaryDirectory() as temp_dir:
            codex_home = Path(temp_dir) / "codex"
            codex_home.mkdir()
            original_config = "model = \"example\"\n"
            (codex_home / "config.toml").write_text(original_config, encoding="utf-8")

            backup_dir = apply_global(REPO_ROOT, codex_home)
            self.assertIsNotNone(backup_dir)
            restore_global(backup_dir)

            self.assertEqual((codex_home / "config.toml").read_text(encoding="utf-8"), original_config)
            self.assertFalse((codex_home / "hooks.json").exists())

    def test_restore_refuses_changed_target_without_force(self) -> None:
        with tempfile.TemporaryDirectory() as temp_dir:
            codex_home = Path(temp_dir) / "codex"
            backup_dir = apply_global(REPO_ROOT, codex_home)
            (codex_home / "hooks.json").write_text("{}\n", encoding="utf-8")
            with self.assertRaises(ConfigurationError):
                restore_global(backup_dir)

    def test_apply_failure_rolls_back_both_targets(self) -> None:
        with tempfile.TemporaryDirectory() as temp_dir:
            codex_home = Path(temp_dir) / "codex"
            codex_home.mkdir()
            original_config = 'model_auto_compact_token_limit_scope = "total"\nmodel = "keep"\n'
            original_hooks = '{"hooks":{"Custom":[]}}\n'
            (codex_home / "config.toml").write_text(original_config, encoding="utf-8")
            (codex_home / "hooks.json").write_text(original_hooks, encoding="utf-8")

            real_write = installer_module.atomic_write_text
            failed = False

            def fail_once_for_hooks(path: Path, content: str) -> None:
                nonlocal failed
                if Path(path) == codex_home / "hooks.json" and not failed:
                    failed = True
                    raise OSError("simulated hooks write failure")
                real_write(path, content)

            with patch.object(installer_module, "atomic_write_text", side_effect=fail_once_for_hooks):
                with self.assertRaises(ConfigurationError):
                    apply_global(
                        REPO_ROOT,
                        codex_home,
                        remove_legacy_compact_overrides=True,
                    )

            self.assertEqual((codex_home / "config.toml").read_text(encoding="utf-8"), original_config)
            self.assertEqual((codex_home / "hooks.json").read_text(encoding="utf-8"), original_hooks)
            manifests = list((codex_home / "backups").glob("context-continuity-*/manifest.json"))
            self.assertEqual(len(manifests), 1)
            self.assertEqual(json.loads(manifests[0].read_text(encoding="utf-8"))["status"], "rolled_back")

    def test_restore_validates_all_backup_hashes_before_mutating(self) -> None:
        with tempfile.TemporaryDirectory() as temp_dir:
            codex_home = Path(temp_dir) / "codex"
            codex_home.mkdir()
            (codex_home / "config.toml").write_text(
                'model_auto_compact_token_limit_scope = "total"\nmodel = "keep"\n',
                encoding="utf-8",
            )
            (codex_home / "hooks.json").write_text('{"hooks":{"Custom":[]}}\n', encoding="utf-8")
            backup_dir = apply_global(
                REPO_ROOT,
                codex_home,
                remove_legacy_compact_overrides=True,
            )
            config_after_apply = (codex_home / "config.toml").read_text(encoding="utf-8")
            (backup_dir / "hooks.json").write_text("corrupted\n", encoding="utf-8")

            with self.assertRaises(ConfigurationError):
                restore_global(backup_dir)
            self.assertEqual(
                (codex_home / "config.toml").read_text(encoding="utf-8"),
                config_after_apply,
            )

    def test_restore_copy_failure_rolls_back_already_restored_target(self) -> None:
        with tempfile.TemporaryDirectory() as temp_dir:
            codex_home = Path(temp_dir) / "codex"
            codex_home.mkdir()
            (codex_home / "config.toml").write_text(
                'model_auto_compact_token_limit_scope = "total"\nmodel = "keep"\n',
                encoding="utf-8",
            )
            (codex_home / "hooks.json").write_text('{"hooks":{"Custom":[]}}\n', encoding="utf-8")
            backup_dir = apply_global(
                REPO_ROOT,
                codex_home,
                remove_legacy_compact_overrides=True,
            )
            applied_config = (codex_home / "config.toml").read_text(encoding="utf-8")
            applied_hooks = (codex_home / "hooks.json").read_text(encoding="utf-8")
            real_copy = installer_module.atomic_copy_file
            failed = False

            def fail_once_on_hooks_restore(source: Path, destination: Path) -> None:
                nonlocal failed
                if (
                    Path(source) == backup_dir / "hooks.json"
                    and Path(destination) == codex_home / "hooks.json"
                    and not failed
                ):
                    failed = True
                    raise OSError("simulated restore copy failure")
                real_copy(source, destination)

            with patch.object(installer_module, "atomic_copy_file", side_effect=fail_once_on_hooks_restore):
                with self.assertRaises(ConfigurationError):
                    restore_global(backup_dir)

            self.assertEqual((codex_home / "config.toml").read_text(encoding="utf-8"), applied_config)
            self.assertEqual((codex_home / "hooks.json").read_text(encoding="utf-8"), applied_hooks)


if __name__ == "__main__":
    unittest.main()
