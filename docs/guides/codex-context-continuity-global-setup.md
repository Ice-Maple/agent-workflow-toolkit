# Codex 上下文连续性 Wave 3：非阻塞全局配置与还原指南

## 适用范围

这份指南用于把本仓库的 Wave 3 上下文连续性 Hook 应用到另一台机器、另一个用户或另一个 Codex App/CLI 环境。

Wave 3 的全局默认配置是非阻塞的：它观察工具、压缩和子 Agent 生命周期，但不注册 `Stop` Hook，不会在正常回复结束时插入 checkpoint continuation。真实 Codex App 的事件顺序仍应在目标环境验证，因此推荐按“预览 → 备份后全局应用 → 重启并观察 → 必要时还原”的顺序操作。

如果环境中仍出现 `Write a concise checkpoint.md ...` 或 `<hook_prompt ...>`，说明还加载着旧版阻塞式 `Stop` 配置。ChatGPT/Codex App 使用 `Settings -> Hooks`，Codex CLI 才使用 `/hooks`；先在对应入口关闭该 Hook，再按本指南升级。仅把旧 Hook 从全局改成项目级不能解决问题。

本指南提供的安装器只修改指定的 Codex 用户目录，不修改业务仓库，不复制认证文件，不改变权限或审批策略。

## 当前实施状态（2026-08-11）

当前机器已经完成用户级全局应用，配置文件为 `%USERPROFILE%\.codex\hooks.json`，其命令直接指向 `E:\projects\agent-workflow-toolkit\tools\context_continuity` 下的脚本。当前版本的交付边界是：

- 已完成：Wave 0–3 连续性脚本、非阻塞全局 Hook、Windows 并发/文件占用修复、缺失 `session_id` 静默处理，以及只读 PowerShell 误标 `dirty` 修复。
- 已验证：70 项本地测试通过；本机全局 Hook 冒烟通过；当前 Windows App 已覆盖 dirty auto、clean/safe/dirty manual compact、clean/dirty 普通 turn 无 continuation、真实工具/Subagent 事件和 session-local lock fault injection 下的 fail-open。
- 已关闭：版本化证据与独立复核均已通过，Wave 3 已对当前支持的 Windows ChatGPT/Codex App `26.803.5235.0` Store x64 宿主关闭。
- 后续兼容性待办：其他机器、其他宿主版本和长时间多会话运行下的事件顺序与稳定性。
- 尚未实现：精确控制自动压缩只在 turn 边界发生的调度器。当前 Hook 不改变 Codex 原生自动压缩时机，只在中途压缩时提供恢复线索。

Wave 4 现在可以从 token 使用量、恢复质量和重复执行率评测开始。安全边界调度仍是条件触发的独立能力，不应把它误认为当前 Wave 3 全局安装的一部分。

## 1. 会安装什么

| 项目 | 默认位置 | 作用 |
| --- | --- | --- |
| 用户级配置 | Windows：`%USERPROFILE%\.codex\config.toml`；macOS/Linux：`~/.codex/config.toml` | 默认完全保留；只有显式升级参数才移除两条已人工确认的旧安装器压缩覆盖值 |
| 用户级 Hook | Windows：`%USERPROFILE%\.codex\hooks.json`；macOS/Linux：`~/.codex/hooks.json` | 注册非阻塞的 `SessionStart`、`PreCompact`、`PostCompact`、`PreToolUse`、`SubagentStart`、`SubagentStop`；不注册 `Stop` |
| 状态目录 | 未设置环境变量时：`~/.codex/context-continuity/sessions` | 保存 `state.json`、checkpoint、hot 状态和生命周期事件 |
| 备份 | `<CODEX_HOME>/backups/context-continuity-<时间>/` | 保存安装前文件和 `manifest.json`，供安全还原 |

如果设置了 `CODEX_HOME`，配置文件和 Hook 文件以该目录为准。状态目录由 `CODEX_CONTEXT_CONTINUITY_HOME` 单独控制；它没有设置时使用用户 Home 下的默认目录。

安装器不覆盖 `compact_prompt`、压缩 scope 或自动压缩 token 阈值。不同模型和宿主版本的上下文行为不同，应优先使用 Codex 原生默认值；自定义值只在隔离环境验证后手工设置。

官方 OpenAI Docs 说明了用户级 Hook 会跨项目加载、`Stop` 不支持 matcher，以及 `decision: "block"` 会产生 continuation prompt：[Hooks](https://developers.openai.com/codex/hooks)。配置层级见 [Advanced Configuration](https://developers.openai.com/codex/config-file/config-advanced)。

## 2. 安装前检查

在目标环境准备：

1. 把本仓库放到固定的绝对路径。全局 `hooks.json` 会写入这个路径，仓库移动后需要重新应用。
2. 准备 Python 3.10 或更高版本。Windows 优先使用 `py -3`，macOS/Linux 使用 `python3`。
3. 确认 Codex 已安装；如果当前版本支持严格配置校验，运行 `codex --strict-config --version`。
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
- 不增加任何全局压缩 prompt、scope 或阈值；只有指定 `--remove-legacy-compact-overrides` 才移除精确旧值；
- 应用前创建带时间戳的备份和 manifest；
- 如果多文件写入中途失败，自动回滚到该备份；还原前先验证所有备份源，避免部分还原；
- 升级时删除本仓库旧版 `Stop` 组，保留其他来源的 Hook；
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

命令输出中的 `BACKUP_DIR` 必须保存下来；还原时需要使用它。`apply --force` 仅为旧脚本兼容保留，不会覆盖用户自定义压缩设置。

从本仓库旧版全局安装器升级，并且已经确认 `config.toml` 中两条值确由旧安装器写入时，使用：

```powershell
py -3 $installer apply `
  --repo-root $repo `
  --remove-legacy-compact-overrides
```

macOS/Linux 使用相同参数。安装器无法从相同文本判断配置来源，因此不会默认删除；先检查、再显式确认可以避免误删用户恰好使用同样内容的自定义值。

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

1. 普通回复是否可以直接结束，不出现 checkpoint、seal 或 `<hook_prompt>`；
2. 新会话和无外部写入的 compact 是否保持安静；
3. `Read`、搜索、截图等读取型工具是否只记录事件而不把状态标成 `dirty`；
4. 修改文件、执行不透明 Shell 或调用写入型工具后，状态是否变成 `dirty`；
5. dirty 状态发生 compact 后，是否进入一次 `recovery_required` 核对，而不是在每轮结束时循环；
6. 状态目录是否只包含有限元数据，没有 prompt、完整 transcript、密码或 Token。

首次使用时如果宿主要求信任 Hook，应只信任已经检查过的本仓库路径。App 在
`Settings -> Hooks` 中区分用户级与 `From Projects` 来源，并可能分别要求目录信任、
具体 Hook 信任和 `Reload hooks`；App 对话中的 `/hooks` 不会打开审核器。Codex CLI
才在交互会话中使用 `/hooks`。不要用绕过 Hook 信任或绕过审批的危险选项来“验证成功”。

本机当前支持宿主的关闭证据见 [`W3-app-host-smoke.md`](../../tests/context_continuity/evidence/2026-08-11/W3-app-host-smoke.md)，脚本级证据见 [`W3-nonblocking-global-smoke.md`](../../tests/context_continuity/evidence/2026-08-08/W3-nonblocking-global-smoke.md)。这些证据不替代其他目标 App 版本或机器的宿主验证。

## 6. 安全还原

### 6.0 紧急停用

如果出现循环停止或可见 Hook 提示，App 通过 `Settings -> Hooks`、Codex CLI 通过
`/hooks` 关闭本功能的 Hook，并重启对应宿主。这一步只止血，不改变磁盘配置。
随后选择下面之一：

- 用安装器重新 `apply`：升级到当前非阻塞 Hook，并自动备份旧文件；若确认旧压缩覆盖来自本功能，再加 `--remove-legacy-compact-overrides`；
- 用已有 `BACKUP_DIR` 执行 `restore`：恢复到安装前状态。

不要删除整个 `~/.codex`，也不要覆盖其他用户 Hook。

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

此示例把仓库路径转换为 `/`，即使在 Windows 上也能避免 JSON 中未转义反斜杠的问题。当前非阻塞配置不要求修改 `config.toml`。如果从旧安装器升级，确认下面两行确由旧安装器写入后，可在备份后移除，或使用显式升级参数：

```toml
model_auto_compact_token_limit_scope = "total"
compact_prompt = "Preserve user corrections, hard constraints, unfinished steps, evidence paths, and the precise next step. Remove logs, source code, and repeated explanations."
```

不要删除内容不同的用户自定义值；它们不属于安装器可自动判定的旧配置。不要手工填写未经目标模型和宿主实测的 `model_auto_compact_token_limit`。

## 8. 跨环境注意事项

- 全局 Hook 使用仓库的绝对路径，不能直接把一台机器的 `hooks.json` 原样复制到另一台机器；在新机器重新 clone 后重新运行安装器。
- 如果仓库路径包含空格，保留安装器生成的引号，不要手工删除。
- 如果 Python 命令不是 `py -3` 或 `python3`，应在 Fixture 的 `command`/`commandWindows` 中改为目标环境的绝对 Python 解释器路径，并重新验证。
- `CODEX_HOME`、`CODEX_CONTEXT_CONTINUITY_HOME`、模型和 Provider 是环境级设置，不要把个人认证、代理凭据或完整用户配置提交到仓库。
- 这是通用能力，推荐修复验证后安装在用户级；项目级配置只改变生效范围，不会修复错误的 Hook 语义。
- 用户级 Hook 会影响该用户的多个项目，因此默认配置必须非阻塞、无 `Stop`，并且变更后需要重新检查 Hook 信任状态。
- 本能力不改变沙箱、审批、网络或 MCP allowlist。不要为了让 Hook 工作而扩大权限。
- 同一个状态目录不能被多个并发 session 共享；发生 session/workspace 不匹配时应保留告警，不要强行恢复旧状态。

## 9. 常见问题

| 现象 | 处理 |
| --- | --- |
| 仍出现 `Write a concise checkpoint.md` | 仍加载着旧版 `Stop`。先关闭 Hook，重新运行当前安装器并重启；App 在 `Settings -> Hooks` 检查来源，CLI 才使用 `/hooks`。 |
| Hook 没有输出 | 重启 App，检查目标 `hooks.json`、Python 路径和 Hook 信任；项目 Hook 还要在 `Settings -> Hooks -> From Projects` 完成目录/Hook 信任并 `Reload hooks`；先用 `--dry-run`。 |
| `codex --strict-config` 失败 | 检查现有 `config.toml`；当前安装器不会新增压缩字段。 |
| `restore` 拒绝覆盖 | 目标文件在安装后发生了变化。先保存或检查新改动，再决定是否 `--force`。 |
| `WinError 5` 指向 `.state.json.*.tmp -> state.json` | 临时文件已创建且重试后恢复时，通常是 Windows 瞬时文件占用，不是扩大权限的理由。更新到包含 session 事务锁和原子替换重试的版本；若持续失败，还需检查目录 ACL，以及杀毒、索引或同步软件是否长期占用状态目录。 |
| `PreToolUse unavailable ... missing session_id` | 更新到当前脚本。缺少会话身份的观测回调没有安全状态目标，当前版本会安静放行，不再显示警告。 |
| 只读 `Get-Content` 也把会话标成 `dirty` | 更新到当前脚本。简单且无管道、重定向、命令替换或链式执行的只读 PowerShell cmdlet 不再标脏；复合或未知 Shell 命令仍保持保守。 |
| 出现 `compact_failed` | 不要假设压缩恢复成功；保留最后安全 checkpoint，检查 `events.jsonl` 和工作区。 |
| 出现 `recovery_required` | 这是保护状态；先核对文件、测试和产物，再写短 checkpoint 并 seal。 |

## 相关入口

- [Wave 3 全局安装/还原脚本](../../tools/context_continuity/install_global.py)
- [Wave 3 Hook Fixture](../../tests/context_continuity/fixtures/wave3/hooks.example.json)
- [上下文连续性用户指南](codex-context-continuity.md)
- [上下文连续性测试说明](../../tests/context_continuity/README.md)
- [Wave 3 非阻塞全局 CLI smoke](../../tests/context_continuity/evidence/2026-08-08/W3-nonblocking-global-smoke.md)
- [上下文连续性 Wave 计划](../plans/2026-08-07-codex-context-continuity-waves.md)
