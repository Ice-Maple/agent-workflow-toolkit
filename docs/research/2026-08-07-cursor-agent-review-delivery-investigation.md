# Cursor Agent 评审派发与结果交付调查

- 日期：2026-08-07
- Cursor Agent CLI：`2026.08.04-aaa8809`
- 模型：`cursor-grok-4.5-high`（非 Fast）
- 平台：Windows / PowerShell
- 范围：workspace trust、sandbox capability、退出码 0 但 text stdout 为空，以及评审产物持久化

## 目的与证据边界

本调查来自对 `docs/brainstorms/2026-08-07-multi-agent-orchestration-mvp.md` 的真实独立评审调用。目标是解释本次 Adapter 阻碍并将结论反馈到正式设计，不把单次现象直接泛化为所有 Cursor 版本。

证据包括：

- Cursor CLI help、version 和 models 输出；
- 三次 preflight/执行返回；
- Cursor 用户级 chat transcript 和 meta；
- 同一 session 的 delivery-only follow-up；
- Git 工作树检查。

未读取或保存认证文件、token、请求头或秘密配置。

## 调用绑定

用户可见派发为：

> reviewer（Cursor Agent / Grok 4.5 High，非 Fast）

已通过 `agent models` 确认同时存在：

- `cursor-grok-4.5-high`
- `cursor-grok-4.5-high-fast`

实际调用明确指定无 `-fast` 后缀的模型。评审任务使用 `--mode plan`，要求只读目标设计和研究证据。

## 阻碍 1：workspace trust

首次调用在模型启动前失败：

```text
Workspace Trust Required
To proceed ... pass --trust, --yolo, or -f
```

这不是模型失败，也没有创建 chat/provider session。当前仓库已经由用户明确放入任务 scope，因此重试时只增加 `--trust`；没有使用 `--force`/`--yolo`。

结论：

- workspace trust 是 Adapter preflight，不应创建 Attempt 或消耗模型 retry；
- Adapter 只能在任务 workspace 已获用户授权时使用 `--trust`；
- 未获授权时必须进入人工决定，不能为了 headless 运行自动信任任意路径；
- workspace trust 与 Agent 执行权限是不同维度，不能互相替代。

## 阻碍 2：Windows sandbox 不可用

带 `--sandbox enabled` 的调用在模型启动前失败：

```text
Sandbox mode is enabled but not available on this system.
Sandbox requires macOS or Linux.
Run 'agent sandbox disable' to switch to allowlist mode.
```

`agent sandbox --help` 同时确认 CLI 提供 enable/disable/reset/run 子命令，disable 使用 allowlist mode。调查没有修改用户全局 sandbox 配置。移除不支持的 flag、保留 `--mode plan` 后才进入首次真实模型执行。

结论：

- sandbox 支持必须按 Adapter version + platform 预探测；
- Windows 不能传递已知不支持的 `--sandbox enabled`；
- 只读探索/评审可使用 Cursor 的 `plan`/`ask` mode，但仍需真实验证其工具边界；
- Windows 可写任务只有在 allowlist、workspace 边界和无隐藏全局批准经过 smoke test 后才能声明支持；
- 不允许以 `--force`/`--yolo` 代替 capability 缺口。

## 现象：退出码 0 但 stdout 为空

首次真实执行使用：

```text
agent -p <review-prompt>
  --model cursor-grok-4.5-high
  --output-format text
  --mode plan
  --trust
  --workspace E:\projects\agent-workflow-toolkit
```

进程运行约 170 秒后 exit code 为 0，但 text stdout 为空。工作树没有被 reviewer 修改。

Cursor 为该次调用创建了 chat session `452a1cd0-842b-436e-a218-0a1ad9d28276`。用户级 transcript 位于：

```text
%USERPROFILE%\.cursor\projects\e-projects-agent-workflow-toolkit\
  agent-transcripts\452a1cd0-842b-436e-a218-0a1ad9d28276\
  452a1cd0-842b-436e-a218-0a1ad9d28276.jsonl
```

transcript 证实首轮实际完成了以下动作：

1. 读取目标设计、AGENTS、README 和三份研究材料；
2. Grep 核对 canary、TaskGateway、idempotency、cancel 等合同；
3. 调用 `CreatePlan`，将完整评审报告放入该 tool payload；
4. 随后记录 `turn_ended`，status 为 `success`；
5. 没有产生最终 assistant text message。

因此报告没有丢失，而是停留在 Cursor session transcript 的 tool payload 中。`--output-format text` 没有把 `CreatePlan` payload 当作最终正文输出；CLI 的 exit code 0 只表示该 turn 正常结束，不代表 expected output 已交付。

## 同 session 恢复

使用 `--continue`、相同 workspace、Adapter、模型和只读模式发送 delivery-only follow-up，明确要求不要重新读文件或调用工具，只返回已完成报告。该 follow-up 产生最终 assistant text，控制器成功取得完整评审报告。

这属于同一已核验 session 的结果交付恢复，不是换模型、fallback 或重新评审。首次 `delivery_missing` 事实仍应保留，不能被 follow-up 覆盖。

## 根因判断

本次空 stdout 的直接原因是：

> Reviewer 将结果提交给 `CreatePlan` 工具后结束 turn，没有再生成最终 assistant text；text renderer 没有输出 tool payload，但 CLI 仍以成功退出。

这不是 workspace trust 或 sandbox 失败的延续，也不能仅凭当前证据认定为 Cursor CLI 丢失模型输出。它是“turn 成功”和“结果完成交付”没有被调用方区分的问题。

## 设计修正

### Preflight 与 Attempt 分离

- version/model、workspace trust、sandbox/permission、MCP 和输出能力在 Attempt 之前探测；
- preflight blocked 不创建 provider session，也不消耗 retry；
- preflight 通过后才创建 Attempt 和不可复用 launch ticket。

### 使用结构化事件

- 外部 CLI 支持时默认使用 stream-json；
- 保存 session ID、tool events、assistant final、terminal event 和原始 transcript 引用；
- exit code 0、`turn_ended: success` 或 tool result 都不是单独完成证据。

### 持久 artifact 为完成条件

评审、设计、计划和测试报告通过 `expected_output.artifact_path` 指定落盘路径：

- Prompt 优先要求 Agent 直接写入已授权 artifact staging/path；
- read-only mode 不允许 Agent 写文件时，由 Go 核心从结构化最终正文原子写入目标路径；
- 核心验证 canonical path、非空、格式、必要章节和 hash；
- 没有最终正文和合规 artifact 时标记 `incomplete: delivery_missing`；
- transcript 中的 `CreatePlan`/tool payload 保存为 recovery evidence，但不能自动冒充最终 artifact；
- identity/authority 不变时允许同 session delivery-only follow-up，仍保留首次失败事件。

## 后续验证

实施 Cursor Adapter 时至少覆盖：

1. 未 trust workspace 在模型启动前被阻止；
2. 已授权 workspace 可显式 trust，且不扩大任务权限；
3. Windows sandbox probe 返回 unsupported，不启动错误 Attempt；
4. plan/ask 只读边界；
5. Windows allowlist 可写能力未证实时 fail closed；
6. stream-json 中 session/tool/final/terminal 的 fixture；
7. exit 0 + empty final → `delivery_missing`；
8. CreatePlan/tool-only turn 不判 completed；
9. 同 session delivery-only follow-up；
10. required artifact 原子落盘并通过结构校验后才 completed。
