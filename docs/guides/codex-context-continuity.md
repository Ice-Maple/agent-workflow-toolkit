# Codex 上下文连续性：用户使用指南

## 这是什么

这个功能用于降低 Codex 在工作过程中自动压缩上下文后“忘记做到哪一步、重复已经完成的工作、丢失用户纠正”的风险。

它会在用户私有目录保存一份很短的工作备忘录。压缩前如果本轮还有没有整理好的工作，会再保存一张更小的“紧急便签”；压缩完成后，Codex 会先看到这些信息，再继续工作。

它不是一个精确控制 token 的调度器，也不能保证压缩只发生在任务结束时。当前版本的目标是：即使压缩发生在中途，也尽量让任务从正确的位置继续。

## 先用一句话理解它

可以把它想成 Agent 的“压缩恢复记录器”：

- 正常回复直接结束，不插入额外提示；
- 如果工作做到一半突然被压缩，先写一张更小的临时便签；
- 只有压缩时存在外部写入或异常状态，才要求核对磁盘结果并恢复。

## Wave 分别做什么

Wave 是开发阶段，不是需要用户切换的四种模式。

| 阶段 | 通俗解释 | 当前状态 |
| --- | --- | --- |
| Wave 0 | 只观察 Codex App 什么时候发送生命周期事件，不改变任务行为。 | 已完成，主要用于宿主验证 |
| Wave 1 | 每轮结束时保存一份 checkpoint，下一轮或压缩后恢复。 | 已完成 |
| Wave 2 | 处理中途压缩：保存紧急便签，压缩后要求核对并恢复。 | 已完成，当前初步可用版本 |
| Wave 3 | 让工具调用和子 Agent 自动标记“工作是否变脏”，限制恢复信息大小，并提供非阻塞全局配置。 | 已完成脚本级实现与本地回归，真实 App smoke 待验证 |
| Wave 4 | 用真实任务统计效果；只有确实需要时，才考虑精确安排压缩时机。 | 计划中 |

日常使用至少需要 Wave 2；如果希望工具调用和子 Agent 也自动参与状态判断，可以改用 Wave 3 Fixture。Wave 0 不需要一直开启，Wave 4 仍是计划中的评测与调度阶段。

## 当前版本能做什么

### 正常的一轮工作（Wave 3 默认配置）

1. 不注册 `Stop` Hook，普通回复直接结束。
2. `PreToolUse` 只记录有限元数据；明确的读取型工具不会标脏。
3. 写入型、未知或不透明 Shell 工具会把状态标成 `dirty`；只有单条、无管道/重定向/命令替换且位于窄白名单内的只读 PowerShell 命令（例如简单 `Get-Content`）不会标脏。
4. checkpoint/seal 保留为恢复和显式保存机制，不再是每轮结束的强制步骤。

### 压缩发生在中途

1. `PreCompact` Hook 不阻止压缩。
2. 如果本轮有未整理的工作，Hook 写入一个很小的 `emergency.md`。
3. `PostCompact` 记录宿主报告的压缩完成事件。
4. `SessionStart(source=compact)` 注入上一次安全 checkpoint 和紧急便签。
5. 模型先核对工作区、测试和产物，再从首个未完成步骤继续。

这样做的原因是：阻止 context-limit 压缩可能直接中止当前 turn，所以本版本选择“允许压缩，但保留恢复线索”。

### Wave 3 会多做什么

Wave 3 不改变上面的“允许压缩”策略，只把几个容易依赖模型自觉的地方变成机械记录：

- `PreToolUse` 在工具真正执行前记录工具名和类别；写入、未知和复合 Shell 工具会标记 `dirty`，明确的读取/证据工具和可机械证明的简单只读 PowerShell 命令只记录事件，不制造恢复义务；缺少 `session_id` 的宿主回调没有安全的状态目标，因此会安静放行；
- `SubagentStart`/`SubagentStop` 记录子 Agent 是否仍在运行。根 Agent 不能因为子 Agent 返回了结果就自动变成 `safe`，必须先核对结果、更新 checkpoint 并 seal；
- seal 时分别检查小的 `hot.md` 和较完整的 `checkpoint.md`，超出预算就报可修复错误，不静默截断；
- 压缩后优先注入 hot 状态；较大的 warm checkpoint 只保留磁盘路径，避免恢复内容本身再次挤满上下文。

## 如何初步使用

修正版 Wave 3 是通用能力，验证后可安装到用户级 `~/.codex/hooks.json`；项目级与用户级只影响生效范围，不改变功能正确性。

如果验证通过后要在其他机器或用户环境中重复安装、备份和还原，请使用[跨环境配置与还原指南](codex-context-continuity-global-setup.md)及其安装器；全局安装是明确的用户操作，不会由本功能自动执行。

### 1. 先运行本地测试

在仓库根目录执行：

```powershell
cd E:\projects\agent-workflow-toolkit
python -m unittest discover -s tests/context_continuity -p "test_*.py"
```

当前仓库还提供了本地 CLI smoke 证据：[W2-local-cli-smoke.md](../../tests/context_continuity/evidence/2026-08-07/W2-local-cli-smoke.md) 和 [W3-nonblocking-global-smoke.md](../../tests/context_continuity/evidence/2026-08-08/W3-nonblocking-global-smoke.md)。两者都是脚本级证据，不等同于真实 Codex App 宿主验证。

### 2. 创建隔离的 Hook 配置

以下示例把 Hook 放在临时 workspace 的 `.codex/hooks.json`，不会修改用户级配置：

```powershell
$repo = "E:\projects\agent-workflow-toolkit"
$smokeRoot = Join-Path ([IO.Path]::GetTempPath()) "codex-context-continuity-app-smoke"
$codexDir = Join-Path $smokeRoot ".codex"

New-Item -ItemType Directory -Force -Path $codexDir | Out-Null

$template = Get-Content -Raw `
  "$repo\tests\context_continuity\fixtures\wave3\hooks.example.json"
$template.Replace("<REPO_ROOT>", $repo) |
  Set-Content -LiteralPath (Join-Path $codexDir "hooks.json") -Encoding utf8

# 运行状态放在临时目录，不进入业务仓库
$env:CODEX_CONTEXT_CONTINUITY_HOME = Join-Path $smokeRoot "state"
```

然后用这个临时 workspace 启动或重启 Codex App，并确认它加载了该 workspace 的 `.codex/hooks.json`。如果 App 已经在运行，旧进程可能没有新的环境变量，需要重启后再测试。

### 3. 做一个很小的验证任务

可以让 Codex 完成一个包含“读取、修改、测试”的小任务，然后观察：

1. 普通回复是否直接结束，不出现 checkpoint/seal continuation；
2. 新会话和只读操作后的 compact 是否保持安静；
3. 写入操作后状态是否变成 `dirty`；
4. dirty 状态下 compact 后，是否只进入一次 `recovery_required` 核对；
5. `%TEMP%\codex-context-continuity-app-smoke\state` 下是否出现有限的 `state.json`、`events.jsonl` 和必要时的 `emergency.md`。

如果只想先验证 Wave 2，可以把上面的 `wave3` 改成 `wave2`。Wave 3 还应额外观察 `PreToolUse`、`SubagentStart` 和 `SubagentStop` 事件，以及 `hot.md` 是否保持很小。

不需要故意把大量 token 填满上下文。先用手动 compact 验证生命周期；需要测试 auto compact 时，再在隔离配置中设置一个临时、偏保守的自动压缩阈值。

## checkpoint 应该写什么

checkpoint 的目的不是保存聊天记录，而是让下一次 Agent 能准确接着做。建议只保留：

- 当前目标；
- 用户的明确约束和最近纠正；
- 已完成工作的结论和证据路径；
- 当前正在做什么；
- 首个未完成步骤；
- 阻塞项或需要用户决定的问题。

不要写入：

- 完整聊天复述；
- 大段命令输出或源码；
- 已经可以从仓库重新读取的日志；
- 密码、Token、认证头或其他凭据。

当前 checkpoint 有字符和估算 token 上限。seal 失败时会保留 dirty 状态，模型需要把内容改得更短后再 seal。

当前实现的上限是：warm `checkpoint.md` 不超过 2400 个字符、约 1500 个估算 token；hot `hot.md` 不超过 2400 个字符、600 个估算 token；紧急便签不超过 1200 个字符、约 250 个估算 token。小 checkpoint 在 seal 时会自动生成 hot 状态；较大的 warm 状态不会被硬截断。

## 这些状态是什么意思

| 状态 | 通俗解释 | 该怎么做 |
| --- | --- | --- |
| `clean` | 尚无需要额外恢复的外部写入，也没有强制 checkpoint。 | 正常工作；原生压缩可以直接进行。 |
| `safe` | 重要进度已经保存并验证过。 | 可以继续正常工作或压缩。 |
| `dirty` | 最近做过工作，但 checkpoint 可能还没包含最新结果。 | 先整理 checkpoint，避免把新结果漏掉。 |
| `compacting` | 压缩已经开始，正在等待压缩后的入口。 | 不要开始新的正式业务工作，等待恢复事件。 |
| `recovery_required` | 压缩发生时还有未整理工作。 | 先核对文件、测试和产物，再重新 seal。 |
| `compact_failed` | 没观察到正常的压缩恢复入口。 | 保留最后安全 checkpoint，检查事件和工作区，不要假设压缩成功。 |

## 会影响哪些配置

### `hooks.json`

Wave 2 Fixture 配置了四类 Hook：

- `SessionStart`：加载或恢复 checkpoint；
- `Stop`：检查本轮是否已经 seal；
- `PreCompact`：保存紧急便签，但不阻止压缩；
- `PostCompact`：记录压缩完成。

配置模板是 [wave2/hooks.example.json](../../tests/context_continuity/fixtures/wave2/hooks.example.json)。模板中的 `<REPO_ROOT>` 必须替换为仓库绝对路径。

Wave 3 Fixture 使用非阻塞全局语义：

- 不包含 `Stop`；升级安装会删除本仓库旧版 `Stop` 组；
- `PreToolUse`：记录工具类别和 dirty 状态；
- `SubagentStart`/`SubagentStop`：记录根 session 下的子 Agent 生命周期；
- `SessionStart` 的 `additionalContextLimit=800`：给 hot 状态和固定恢复提示留出空间。

配置模板是 [wave3/hooks.example.json](../../tests/context_continuity/fixtures/wave3/hooks.example.json)。Hook 只记录机械状态，不返回正常轮次的阻止决定，也不会自动替根 Agent seal。

### `CODEX_CONTEXT_CONTINUITY_HOME`

这个环境变量指定状态目录。建议始终设置到用户私有目录或临时目录，例如：

```text
%TEMP%\codex-context-continuity-app-smoke\state
```

状态不会写入业务仓库。目录中可能包含：

```text
state.json                 机械状态和校验信息
hot.md                    压缩后优先注入的小状态
checkpoint.md              精简的语义进度
emergency.md               中途压缩时的机械便签
compact-marker.json        状态写入失败时的恢复标记
events.jsonl               不含提示词正文的生命周期记录
```

### `additionalContextLimit`

Wave 2 的 SessionStart Fixture 使用 `1200`；Wave 3 使用 `800`，因为 Wave 3 只允许把不超过 600 个估算 token 的 hot 状态直接注入，再加上固定恢复提示。这个值只在 Hook 配置中生效，不会修改模型的上下文窗口。

### 自动压缩阈值

当前 Fixture 不替用户写死 `model_auto_compact_token_limit`。原因是不同模型和不同任务的上下文窗口不同，不能凭经验填一个永久值。

如果要测试 auto compact：

1. 只在隔离配置中临时设置阈值；
2. 先保留足够空间给当前 turn 和 checkpoint；
3. 记录实际事件顺序和恢复效果；
4. 不要把一次测试得到的数字直接推广到所有模型。

### `compact_prompt`

当前安装器不再写入全局 `compact_prompt`。旧版安装器写入的精确默认值只有在显式传入 `--remove-legacy-compact-overrides` 后才会移除；默认保留所有用户配置。原因是宿主可能把自定义压缩提示显示到会话中，而且安装器无法仅凭相同文本证明配置来源。

## 安全和隐私

- 状态默认放在用户私有目录，不写进业务仓库；
- 不复制完整 transcript；
- checkpoint 和 emergency manifest 都会做长度与 secret 检查；
- transcript/tool pointer 会在持久化前做有界处理，避免把敏感原文写入状态；
- Hook 发现 session 或 workspace 不匹配时拒绝恢复旧状态；
- 当前 Hook 不修改系统或用户级权限、不绕过审批，也不阻止 context-limit 压缩；状态文件会尽量使用私有文件权限。

## 常见情况

### 出现 `Write a concise checkpoint.md` 或 `<hook_prompt>`

说明仍加载着旧版阻塞式 `Stop`。先在设置或 `/hooks` 中关闭它，运行当前安装器升级 `hooks.json`，重启 App 后再核对 Hook 来源。把同一 Hook 移到项目级不能解决循环。

### 出现 `recovery_required`

这是保护行为，不是报错。先查看工作区真实文件、Git diff、测试结果和产物，再把“已确认的结果 + 首个未完成步骤”写入新的 checkpoint 并 seal。

### 出现 `compact_failed`

说明系统没有看到预期的 `SessionStart(source=compact)`。不要假设上下文已经正确压缩；先保留最后安全 checkpoint，查看 `events.jsonl`，必要时重新启动隔离任务。

### 出现 `PreToolUse unavailable ... missing session_id`

这是旧版脚本把宿主的不完整观测回调显示成了警告。当前版本会安静放行并跳过记录；如果仍看到该消息，检查 Hook 实际引用的脚本路径并更新到当前版本。

### 只读 `Get-Content` 后仍进入 `recovery_required`

旧版策略把所有 `Bash`/PowerShell 调用都保守标脏。当前版本只对语法简单、可机械证明为只读的 PowerShell cmdlet 降级为读取；带管道、重定向、命令替换、链式执行或未知语法的 Shell 命令仍保持 `dirty`。

### Hook 没有输出任何内容

检查：

1. App 是否重新启动过；
2. workspace 是否真的加载了 `.codex/hooks.json`；
3. `<REPO_ROOT>` 是否已替换；
4. `CODEX_CONTEXT_CONTINUITY_HOME` 是否在 App 进程中可见；
5. Windows 是否使用了 `commandWindows` 对应的 Python 命令。

## 如何停用和回滚

在隔离 workspace 中，移除或改名该 workspace 的 `.codex/hooks.json`，然后重启 Codex App；同时在当前 shell 中清除：

```powershell
Remove-Item Env:CODEX_CONTEXT_CONTINUITY_HOME -ErrorAction SilentlyContinue
```

这不会修改业务仓库文件。仓库默认不会自动修改用户级 `~/.codex/hooks.json` 或全局 `config.toml`；如果明确运行全局安装器，请按[跨环境配置与还原指南](codex-context-continuity-global-setup.md)保留备份并使用 manifest 还原。

## 当前成熟度

当前版本适合：

- 隔离 workspace 的初步使用；
- 手动 compact 和小规模 auto compact 验证；
- 观察 checkpoint 是否减少重复工作。

当前版本还不能承诺：

- 压缩一定只发生在任务边界；
- 精确 token 调度；
- 自动捕获所有工具调用产生的新事实；
- 真实 Codex App 在所有版本中的事件顺序完全一致。
- 只在任务结束时自动压缩，或精确安排压缩时机。

Wave 3 的本地脚本 smoke 已通过，但真实宿主 smoke 仍需单独验证；在此之前建议只在隔离 workspace 中启用。

## 相关实现和验证材料

- [Wave 计划](../plans/2026-08-07-codex-context-continuity-waves.md)
- [设计说明](../brainstorms/2026-08-07-codex-context-continuity.md)
- [Wave 2 Hook Fixture](../../tests/context_continuity/fixtures/wave2/hooks.example.json)
- [Wave 3 Hook Fixture](../../tests/context_continuity/fixtures/wave3/hooks.example.json)
- [跨环境配置与还原指南](codex-context-continuity-global-setup.md)
- [全局安装/还原脚本](../../tools/context_continuity/install_global.py)
- [本地 CLI smoke 证据](../../tests/context_continuity/evidence/2026-08-07/W2-local-cli-smoke.md)
- [Wave 3 本地 CLI smoke 证据](../../tests/context_continuity/evidence/2026-08-07/W3-local-cli-smoke.md)
