import json
import tempfile
import unittest
from pathlib import Path

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
                            "Stop": [{"matcher": "existing", "hooks": []}],
                            "Custom": [{"matcher": ".*", "hooks": []}],
                        },
                    }
                ),
                encoding="utf-8",
            )

            first_backup = apply_global(REPO_ROOT, codex_home)
            self.assertIsNotNone(first_backup)
            first_hooks = json.loads((codex_home / "hooks.json").read_text(encoding="utf-8"))
            self.assertEqual(len(first_hooks["hooks"]["Stop"]), 2)
            self.assertIn("Custom", first_hooks["hooks"])
            config_text = (codex_home / "config.toml").read_text(encoding="utf-8")
            self.assertLess(
                config_text.index("model_auto_compact_token_limit_scope"),
                config_text.index("[model_providers.example]"),
            )

            second_backup = apply_global(REPO_ROOT, codex_home)
            self.assertIsNotNone(second_backup)
            second_hooks = json.loads((codex_home / "hooks.json").read_text(encoding="utf-8"))
            self.assertEqual(len(second_hooks["hooks"]["Stop"]), 2)
            self.assertEqual(second_hooks["hooks"]["Custom"], first_hooks["hooks"]["Custom"])

    def test_conflicting_compact_setting_requires_force(self) -> None:
        with tempfile.TemporaryDirectory() as temp_dir:
            codex_home = Path(temp_dir) / "codex"
            codex_home.mkdir()
            (codex_home / "config.toml").write_text(
                'compact_prompt = "my existing prompt"\n',
                encoding="utf-8",
            )
            with self.assertRaises(ConfigurationError):
                apply_global(REPO_ROOT, codex_home)

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


if __name__ == "__main__":
    unittest.main()
