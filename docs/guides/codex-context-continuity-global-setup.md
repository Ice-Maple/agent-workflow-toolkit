# Codex 上下文连续性 Wave 3：跨环境配置与还原指南

## 适用范围

这份指南用于把本仓库的 Wave 3 上下文连续性 Hook 应用到另一台机器、另一个用户或另一个 Codex App/CLI 环境。

Wave 3 当前的状态是：脚本实现、本地回归和 CLI smoke 已完成；真实 Codex App 宿主的完整事件顺序仍需要在目标环境单独验证。因此推荐按“隔离验证 → 全局应用 → 观察 → 必要时还原”的顺序操作。

本指南提供的安装器只修改指定的 Codex 用户目录，不修改业务仓库，不复制认证文件，不改变权限或审批策略。

## 1. 会安装什么

| 项目 | 默认位置 | 作用 |
| --- | --- | --- |
| 用户级配置 | Windows：`%USERPROFILE%\.codex\config.toml`；macOS/Linux：`~/.codex/config.toml` | 增加 `model_auto_compact_token_limit_scope = "total"` 和 `compact_prompt` |
| 用户级 Hook | Windows：`%USERPROFILE%\.codex\hooks.json`；macOS/Linux：`~/.codex/hooks.json` | 注册 `SessionStart`、`Stop`、`PreCompact`、`PostCompact`、`PreToolUse`、`SubagentStart`、`SubagentStop` |
| 状态目录 | 未设置环境变量时：`~/.codex/context-continuity/sessions` | 保存 `state.json`、checkpoint、hot 状态和生命周期事件 |
| 备份 | `<CODEX_HOME>/backups/context-continuity-<时间>/` | 保存安装前文件和 `manifest.json`，供安全还原 |

如果设置了 `CODEX_HOME`，配置文件和 Hook 文件以该目录为准。状态目录由 `CODEX_CONTEXT_CONTINUITY_HOME` 单独控制；它没有设置时使用用户 Home 下的默认目录。

自动压缩 token 阈值没有被安装器写死。不同模型和任务的上下文窗口不同，目标环境应先观察实际行为，再决定是否在隔离配置中临时设置 `model_auto_compact_token_limit`。

官方 OpenAI Docs 的配置参考说明了用户级 `~/.codex/config.toml`、项目级 `.codex/config.toml`、`hooks.json`/`[hooks]` 和自动压缩字段：[Configuration Reference](https://learn.chatgpt.com/docs/config-file/config-reference#configtoml)。

## 2. 安装前检查

在目标环境准备：

1. 把本仓库放到固定的绝对路径。全局 `hooks.json` 会写入这个路径，仓库移动后需要重新应用。
2. 准备 Python 3.10 或更高版本。Windows 优先使用 `py -3`，macOS/Linux 使用 `python3`。
3. 确认 Codex 已安装，并运行 `codex --strict-config --version` 检查当前配置能被解析。
4. 检查目标用户目录中是否已有 `config.toml` 或 `hooks.json`。不要直接复制另一台机器的完整 `config.toml`，其中可能包含代理、Provider 或其他机器专属设置。
5. 先运行仓库回归：

```powershell
cd E:\projects\agent-workflow-toolkit
python -m unittest discover -s tests/context_continuity -p "test_*.py"
```

macOS/Linux：

```bash
cd /path/to/agent-workflow-toolkit
python3 -m unittest discover -s tests/context_continuity -p 'test_*.py'
```

## 3. 推荐方式：使用跨平台安装器

安装器入口是 [`tools/context_continuity/install_global.py`](../../tools/context_continuity/install_global.py)。它会：

- 从 Wave 3 Fixture 渲染当前仓库的绝对路径；
- 合并现有 `hooks.json`，保留不相关的用户 Hook；
- 重复执行时替换本仓库之前安装的 Wave 3 Hook，不产生重复组；
- 在 `config.toml` 的顶层增加或检查两个压缩配置；
- 应用前创建带时间戳的备份和 manifest；
- 发现用户已有不同的同名压缩配置时停止，除非显式传入 `--force`；
- 不设置 `CODEX_CONTEXT_CONTINUITY_HOME`，也不修改认证、权限和审批配置。

### Windows

先做不写盘的预览：

```powershell
$repo = (Resolve-Path "E:\projects\agent-workflow-toolkit").Path
$installer = Join-Path $repo "tools\context_continuity\install_global.py"

py -3 $installer apply `
  --repo-root $repo `
  --dry-run
```

确认目标目录和仓库路径正确后应用：

```powershell
$repo = (Resolve-Path "E:\projects\agent-workflow-toolkit").Path
$installer = Join-Path $repo "tools\context_continuity\install_global.py"

py -3 $installer apply --repo-root $repo
```

如果目标环境使用了自定义 `CODEX_HOME`，显式传入它：

```powershell
py -3 $installer apply `
  --repo-root $repo `
  --codex-home "D:\CodexHome"
```

命令输出中的 `BACKUP_DIR` 必须保存下来；还原时需要使用它。只有确认现有配置中的同名字段确实应被替换时，才使用 `--force`。

### macOS/Linux

```bash
repo="$(cd /path/to/agent-workflow-toolkit && pwd -P)"
installer="$repo/tools/context_continuity/install_global.py"

python3 "$installer" apply --repo-root "$repo" --dry-run
python3 "$installer" apply --repo-root "$repo"
```

自定义 Codex Home：

```bash
python3 "$installer" apply \
  --repo-root "$repo" \
  --codex-home "$HOME/.codex-work"
```

## 4. 状态目录和环境变量

不设置环境变量时，当前脚本默认把状态写入用户私有目录：

```text
~/.codex/context-continuity/sessions/
```

隔离 smoke 或需要明确控制状态位置时，给启动 Codex App/CLI 的同一个进程设置 `CODEX_CONTEXT_CONTINUITY_HOME`。

Windows 当前 PowerShell 会话：

```powershell
$env:CODEX_CONTEXT_CONTINUITY_HOME = `
  Join-Path $env:USERPROFILE ".codex\context-continuity\sessions"
```

Windows 持久化到当前用户（设置后重启 App）：

```powershell
$stateRoot = Join-Path $env:USERPROFILE ".codex\context-continuity\sessions"
[Environment]::SetEnvironmentVariable(
  "CODEX_CONTEXT_CONTINUITY_HOME", $stateRoot, "User"
)
```

macOS/Linux 当前 Shell：

```bash
export CODEX_CONTEXT_CONTINUITY_HOME="$HOME/.codex/context-continuity/sessions"
```

桌面 App 从图形界面启动时，Shell 中的 `export` 不一定会传给 App；应使用目标平台的用户级启动环境，或者不设置该变量而使用默认目录。不要把状态目录指向业务仓库，也不要在多台机器之间共享同一个状态目录。

## 5. 应用后验证

配置或 Hook 变更后，重启 Codex App/CLI 进程。旧进程可能已经缓存了配置和环境变量。

先做静态验证：

```powershell
codex --strict-config --version
py -3 "$repo\tools\context_continuity\install_global.py" `
  apply --repo-root $repo --dry-run
```

macOS/Linux 将 `py -3` 换成 `python3`。然后做一个很小的真实任务，观察：

1. `Stop` 是否在没有 checkpoint 时要求一次整理和 seal；
2. 手动 compact 后，下一轮是否出现 `SessionStart(source=compact)` 恢复入口；
3. 修改文件或调用工具后，状态是否保持 `dirty`，而不是错误地变成 `safe`；
4. dirty 状态发生 compact 后，是否提示 `recovery_required` 并要求核对工作区；
5. 状态目录中是否出现 `state.json`、`events.jsonl`，必要时出现 `emergency.md`；
6. Hook 是否只记录有限的元数据，没有把 prompt、完整 transcript、密码或 Token 写进状态文件。

首次使用时如果宿主要求信任 Hook，应只信任已经检查过的本仓库路径。不要用绕过 Hook 信任或绕过审批的危险选项来“验证成功”。

本仓库已有脚本级证据，但它不替代目标 App 宿主验证：[`W3-local-cli-smoke.md`](../../tests/context_continuity/evidence/2026-08-07/W3-local-cli-smoke.md)。

## 6. 安全还原

### 6.1 还原安装前的配置

使用安装器输出的 `BACKUP_DIR`。先预览：

Windows：

```powershell
py -3 "$repo\tools\context_continuity\install_global.py" restore `
  --backup-dir "C:\Users\<USER>\.codex\backups\context-continuity-<TIMESTAMP>" `
  --dry-run
```

macOS/Linux：

```bash
python3 "$repo/tools/context_continuity/install_global.py" restore \
  --backup-dir "$HOME/.codex/backups/context-continuity-<TIMESTAMP>" \
  --dry-run
```

确认后去掉 `--dry-run` 执行。还原器会检查目标文件的 SHA-256：如果 `config.toml` 或 `hooks.json` 在安装后被其他操作改过，它会拒绝覆盖并提示人工检查。

只有在确认当前改动可以丢弃后，才使用：

```powershell
py -3 "$repo\tools\context_continuity\install_global.py" restore `
  --backup-dir "<BACKUP_DIR>" `
  --force
```

macOS/Linux 将 `py -3` 换成 `python3`。如果安装前没有 `hooks.json`，还原会删除安装器新建的这个文件；如果安装前没有 `config.toml`，还原也会删除安装器新建的配置文件。删除目标仅限 manifest 中明确记录的文件，不会递归删除 Codex Home。

还原配置后重启 Codex App。还原器默认保留状态目录，因为其中可能有仍需核对的 checkpoint 和事件；确认不再需要后，再手动处理那个明确的状态目录。

### 6.2 清除环境变量

Windows 当前会话：

```powershell
Remove-Item Env:CODEX_CONTEXT_CONTINUITY_HOME -ErrorAction SilentlyContinue
```

Windows 用户级持久变量：

```powershell
[Environment]::SetEnvironmentVariable(
  "CODEX_CONTEXT_CONTINUITY_HOME", $null, "User"
)
```

macOS/Linux：

```bash
unset CODEX_CONTEXT_CONTINUITY_HOME
```

清除变量不会删除已有状态文件。删除状态前应先检查路径，确保它是本功能的用户私有目录，而不是仓库或宽泛的系统目录。

## 7. 没有安装器时的手工方式

如果目标环境不能运行安装器，可以只渲染 Wave 3 Fixture。仅当用户级 `hooks.json` 不存在时使用直接写入方式；如果文件已经存在，必须先备份并合并 `hooks` 下的事件数组，不要覆盖其他 Hook。

Windows 示例：

```powershell
$repo = ((Resolve-Path "E:\projects\agent-workflow-toolkit").Path -replace '\\', '/')
$codexHome = if ($env:CODEX_HOME) {
  $env:CODEX_HOME
} else {
  Join-Path $env:USERPROFILE ".codex"
}
$hookPath = Join-Path $codexHome "hooks.json"

if (Test-Path -LiteralPath $hookPath) {
  throw "hooks.json already exists; back up and merge it instead of overwriting it."
}

New-Item -ItemType Directory -Force -Path $codexHome | Out-Null
$fixture = Get-Content -Raw -Encoding utf8 `
  (Join-Path $repo "tests/context_continuity/fixtures/wave3/hooks.example.json")
$fixture.Replace("<REPO_ROOT>", $repo) |
  Set-Content -LiteralPath $hookPath -Encoding utf8
```

此示例把仓库路径转换为 `/`，即使在 Windows 上也能避免 JSON 中未转义反斜杠的问题。手工修改 `config.toml` 时，把以下两个顶层设置放在第一个 `[section]` 之前：

```toml
model_auto_compact_token_limit_scope = "total"
compact_prompt = "Preserve user corrections, hard constraints, unfinished steps, evidence paths, and the precise next step. Remove logs, source code, and repeated explanations."
```

不要把这两行放进 `[desktop]`、`[features]` 或其他表中；不要手工填写未经目标模型实测的 `model_auto_compact_token_limit`。

## 8. 跨环境注意事项

- 全局 Hook 使用仓库的绝对路径，不能直接把一台机器的 `hooks.json` 原样复制到另一台机器；在新机器重新 clone 后重新运行安装器。
- 如果仓库路径包含空格，保留安装器生成的引号，不要手工删除。
- 如果 Python 命令不是 `py -3` 或 `python3`，应在 Fixture 的 `command`/`commandWindows` 中改为目标环境的绝对 Python 解释器路径，并重新验证。
- `CODEX_HOME`、`CODEX_CONTEXT_CONTINUITY_HOME`、模型和 Provider 是环境级设置，不要把个人认证、代理凭据或完整用户配置提交到仓库。
- 项目级 `.codex/hooks.json` 需要项目被宿主信任；用户级 Hook 会影响该用户的多个项目，必须先做隔离 smoke。
- 本能力不改变沙箱、审批、网络或 MCP allowlist。不要为了让 Hook 工作而扩大权限。
- 同一个状态目录不能被多个并发 session 共享；发生 session/workspace 不匹配时应保留告警，不要强行恢复旧状态。

## 9. 常见问题

| 现象 | 处理 |
| --- | --- |
| `apply` 报已有不同的 `compact_prompt` | 先检查现有提示是否有意保留；确认后使用 `--force`，或手工合并。 |
| Hook 没有输出 | 重启 App，检查目标 `hooks.json`、Python 路径和 Hook 信任；先用 `--dry-run`。 |
| `codex --strict-config` 失败 | 检查 `config.toml` 的表边界和手工插入位置；不要把顶层压缩字段放进其他表。 |
| `restore` 拒绝覆盖 | 目标文件在安装后发生了变化。先保存或检查新改动，再决定是否 `--force`。 |
| 出现 `compact_failed` | 不要假设压缩恢复成功；保留最后安全 checkpoint，检查 `events.jsonl` 和工作区。 |
| 出现 `recovery_required` | 这是保护状态；先核对文件、测试和产物，再写短 checkpoint 并 seal。 |

## 相关入口

- [Wave 3 全局安装/还原脚本](../../tools/context_continuity/install_global.py)
- [Wave 3 Hook Fixture](../../tests/context_continuity/fixtures/wave3/hooks.example.json)
- [上下文连续性用户指南](codex-context-continuity.md)
- [上下文连续性测试说明](../../tests/context_continuity/README.md)
- [Wave 3 本地 CLI smoke](../../tests/context_continuity/evidence/2026-08-07/W3-local-cli-smoke.md)
- [上下文连续性 Wave 计划](../plans/2026-08-07-codex-context-continuity-waves.md)
