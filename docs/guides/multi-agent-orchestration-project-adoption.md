# 多 Agent 协同框架项目接入与使用指南

- 目标读者：准备在具体项目中使用当前协同框架的技术负责人、Controller 开发者和项目维护者
- 当前推荐用途：只读探索、代码/文档评审、方案比较、证据收集
- 当前成熟度：`experimental`
- 架构背景：[`多 Agent 协同框架架构说明`](../architecture/multi-agent-orchestration-architecture.md)

## 1. 先确认任务是否适合

首轮项目接入应同时满足：

- Agent 只需要读取项目并输出报告或评审 artifact；
- 任务可以拆成一个或多个明确命名的 Workflow node；
- 每个节点有明确角色、Adapter/model binding 和交付路径；
- 节点完成后允许停下来等待人工批准；
- Provider/CLI 已在当前机器完成登录或凭据配置，但凭据不会写入仓库；
- 团队接受当前没有 dashboard、daemon 和一键外部 Adapter executor。

以下场景不要直接使用当前 `experimental` 路径：

- 让 Agent 自动修改业务代码并直接合并；
- 无人工检查点的长链自治；
- 依赖未验证的 MCP 全局配置；
- 需要跨机器队列、邮件箱、长期 daemon 或生产 SLA；
- 需要把领域业务规则写回共享核心。

## 2. 选择接入路径

| 路径 | 推荐度 | 适用条件 | 当前接入方式 |
| --- | --- | --- | --- |
| Codex App Native | 首轮推荐 | 已在 Codex App 中工作，希望用原生 subagent 做只读 explorer/reviewer | Controller 加载仓库 Skill，通过 Gateway 签发/回填 Native action |
| Cursor Agent | 条件可用 | 已安装并登录 Cursor Agent；任务不需要 MCP allowlist | 需要 toolkit-side host-action bridge；Adapter 本身已通过真实 smoke |
| Claude Code | 条件可用 | 已安装并登录 Claude Code；MCP 配置可严格或配置隔离 | 需要 toolkit-side host-action bridge |
| Codex CLI | 条件可用 | 已安装并登录 Codex CLI；provider route、sandbox 和 approval 可显式配置 | 需要 toolkit-side host-action bridge |

当前没有把三个外部 CLI Adapter 包装成统一命令的正式 executor。具体项目不能直接 import `internal/adapters/*`；如果没有准备实现 toolkit-side bridge，请先采用 Codex App Native 路径。

## 3. 推荐部署拓扑

不要把 toolkit 的 runtime state 复制进业务仓库。推荐保留两个独立 checkout/目录：

```text
E:\tools\agent-workflow-toolkit\          # 固定版本的通用框架
E:\projects\sample-project\              # 具体项目
  └─ reports\agent-review.md              # 显式交付物，可按项目规则决定是否提交

%LOCALAPPDATA%\agent-workflow-toolkit\sample-project\
  ├─ policy.local.json                     # 本机 binding；不含凭据
  └─ state\                                # snapshot / events / receipts
```

职责分配：

| 内容 | 放置位置 |
| --- | --- |
| 通用核心、Adapter、Controller contract | `agent-workflow-toolkit` |
| 项目目标、角色语义、Prompt、required sections、业务验收 | 具体项目 |
| provider token/登录态 | CLI/宿主自己的凭据存储，不进入任何项目文件 |
| Gateway runtime state | 用户级或项目外 `state-dir` |
| 最终报告/artifact | Task workspace 内的显式绝对路径 |

## 4. 固定版本并构建核心

在 toolkit checkout 中固定一个已经验收的 commit 或 release。当前 Wave 4 acceptance 基线是 `130a901`：

```powershell
Set-Location E:\tools\agent-workflow-toolkit
git checkout 130a901
go build -o .\bin\agent-workflow-core.exe .\cmd\agent-workflow-core
.\bin\agent-workflow-core.exe --version
```

项目升级 toolkit 版本时，重新运行该版本的测试/真实 smoke，并把升级决定记录在项目侧；不要让多个未固定 checkout 共享同一个 state-dir。

## 5. 准备本机 policy

Gateway 默认没有 registered binding，不提供“随便派发”的隐式默认值。为目标项目创建 `policy.local.json`。下面是 Native reviewer 模板：

```json
{
  "registered_bindings": [
    {
      "role": "reviewer",
      "adapter": "codex-native",
      "model": "your-registered-model",
      "reasoning": "high",
      "permission": "read-only",
      "workspace": "E:\\projects\\sample-project",
      "mcp_allowlist": []
    }
  ],
  "retry_mode": "limited_auto",
  "max_auto_retries": 1,
  "fallback_mode": "ask",
  "mcp_isolation": "strict",
  "goal_enabled": false
}
```

注意：

- `workspace` 是本机 absolute canonical path，因此这个 policy 通常不直接提交；
- `role/adapter/model/reasoning/permission/workspace/MCP allowlist` 必须和请求完全匹配；
- 首轮保持 `read-only`、`goal_enabled=false` 和空 MCP allowlist；
- policy 只记录声明，不保存 API key、token、header 或 provider base URL 中的凭据。

外部 Adapter 的标识分别是 `cursor`、`claude`、`codexcli`。切换 Adapter 时必须新增完整 binding，并先单独通过对应 Adapter 的 capability preflight；不要只改一个字符串后沿用旧批准。

## 6. 调用 Gateway

### 6.1 调用特性

`agent-workflow-core --gateway` 每次只从 stdin 读取一个 protocol v1 request，输出一个 response 后退出。每个步骤都使用：

- 同一个 `--state-dir`；
- 同一个 `--policy-file`；
- 新的 `request_id`；
- 上一步 response 中返回的真实 task/node/attempt/action ID。

相同 request ID 加相同内容可以安全重放；相同 ID 加不同内容会返回 `request_id_reused`。Gateway 的业务错误也会以 `ok=false` 的结构化 response 返回，所以不能只检查进程 exit code。

PowerShell 7 可以使用下面的薄调用函数：

```powershell
$Core = 'E:\tools\agent-workflow-toolkit\bin\agent-workflow-core.exe'
$StateDir = Join-Path $env:LOCALAPPDATA 'agent-workflow-toolkit\sample-project\state'
$PolicyFile = Join-Path $env:LOCALAPPDATA 'agent-workflow-toolkit\sample-project\policy.local.json'
New-Item -ItemType Directory -Force -Path $StateDir | Out-Null

function Invoke-TaskGateway {
    param([hashtable]$Request)

    $json = $Request | ConvertTo-Json -Depth 20 -Compress
    $raw = $json | & $Core --gateway --state-dir $StateDir --policy-file $PolicyFile
    if ($LASTEXITCODE -ne 0) {
        throw "agent-workflow-core process failed: $LASTEXITCODE"
    }
    $response = $raw | ConvertFrom-Json
    if (-not $response.ok) {
        throw "TaskGateway rejected request: $($response.error.code) - $($response.error.message)"
    }
    return $response.payload
}
```

如果必须使用 Windows PowerShell 5.1，优先把 request 保存为无 BOM UTF-8 文件再输入；不要依赖系统默认编码传递包含非 ASCII 字符的 JSON。

### 6.2 创建 Task

```powershell
$created = Invoke-TaskGateway @{
    protocol_version = 1
    request_id = 'sample-create-001'
    operation = 'createTask'
    payload = @{
        title = 'sample project architecture review'
        objective = '只读检查当前架构边界并生成评审报告'
        workspace = 'E:\projects\sample-project'
    }
}

$TaskId = $created.task.task_id
$RevisionId = $created.revision.revision_id
```

### 6.3 提议明确节点和交付物

required artifact 必须是 Task workspace 内的绝对路径：

```powershell
$proposed = Invoke-TaskGateway @{
    protocol_version = 1
    request_id = 'sample-node-001'
    operation = 'proposeNode'
    payload = @{
        task_id = $TaskId
        revision_id = $RevisionId
        kind = 'architecture-review'
        role = 'reviewer'
        expected_output = @{
            format = 'markdown'
            artifact_path = 'E:\projects\sample-project\reports\agent-review.md'
            required = $true
            min_bytes = 200
            required_sections = @('# Review', '## Findings', '## Verdict')
        }
    }
}

$NodeId = $proposed.node.node_id
```

不要使用空 `kind`；空节点会被永久标为 `unspecified_node` 并 fail closed。

### 6.4 运行 core policy preflight

```powershell
$binding = @{
    role = 'reviewer'
    adapter = 'codex-native'
    model = 'your-registered-model'
    reasoning = 'high'
    permission = 'read-only'
    workspace = 'E:\projects\sample-project'
    mcp_allowlist = @()
}

$preflight = Invoke-TaskGateway @{
    protocol_version = 1
    request_id = 'sample-preflight-001'
    operation = 'runPreflight'
    payload = @{
        node_id = $NodeId
        binding = $binding
    }
}
```

只有 `preflight.status=passed` 才继续。外部 CLI 路径还必须运行 Adapter 自己的 version/model/trust/MCP capability preflight；core policy pass 不代表 Provider CLI 一定可启动。

### 6.5 准备 Attempt

```powershell
$ControllerId = 'sample-controller-001'
$launch = Invoke-TaskGateway @{
    protocol_version = 1
    request_id = 'sample-attempt-001'
    operation = 'prepareAttempt'
    payload = @{
        node_id = $NodeId
        controller_instance_id = $ControllerId
        binding = $binding
    }
}

$AttemptId = $launch.attempt.attempt_id
```

把 response 中的 `dispatch_prompt` 原样展示给用户。不要在 Attempt 创建后替换 role、Adapter、model 或 Reasoning。

### 6.6 Poll 并执行 core-signed action

```powershell
$batch = Invoke-TaskGateway @{
    protocol_version = 1
    request_id = 'sample-poll-001'
    operation = 'pollHostActions'
    payload = @{
        attempt_id = $AttemptId
        after_action_sequence = 0
        controller_instance_id = $ControllerId
    }
}
```

#### Native 路径

Controller 加载 [`skills/multi-agent-orchestration/SKILL.md`](../../skills/multi-agent-orchestration/SKILL.md)，按 [`native-action-contract.md`](../../skills/multi-agent-orchestration/references/native-action-contract.md) 执行 App 原生 subagent action。没有 pending core action 时禁止启动 subagent。

#### 外部 CLI 路径

需要 toolkit-side bridge 把 HostAction 映射到 `internal/adapters/cursor`、`claude` 或 `codexcli`。当前没有可以从业务仓库直接调用的统一 executor command；不要直接 shell 拼接 Provider CLI 后再伪造 core result。

无论哪条路径，提交结果时都必须使用 action 返回的 exact `action_id` 和 `lease`：

```json
{
  "protocol_version": 1,
  "request_id": "sample-result-001",
  "operation": "submitHostActionResult",
  "payload": {
    "attempt_id": "<attempt-id>",
    "action_id": "<core-issued-action-id>",
    "lease": "<core-issued-lease>",
    "result": {
      "status": "running",
      "provider_handle": "<real-host-handle>",
      "provider_session_id": "<real-session-id>",
      "execution_started": true,
      "side_effect_state": "none",
      "identity": {
        "provider": "codex-native",
        "session": "<real-session-id>",
        "workspace": "E:\\projects\\sample-project"
      },
      "task_canary": "<exact-attempt-canary>",
      "terminal": false
    }
  }
}
```

Gateway 可能返回后续 `observe` action；继续 poll/execute/submit，直到 terminal。不要自行循环调用 Provider。

### 6.7 请求 Completion

Agent 已把 artifact 写到 canonical path 且 terminal result 已提交后：

```powershell
$completion = Invoke-TaskGateway @{
    protocol_version = 1
    request_id = 'sample-completion-001'
    operation = 'requestCompletion'
    payload = @{ attempt_id = $AttemptId }
}
```

只有以下条件同时满足才会得到 completed decision：

- provider handle/identity 存在且匹配；
- task canary 已验证；
- terminal event 已保存；
- required artifact 位于 workspace 内；
- format、bytes 和 required sections 通过；
- artifact SHA-256 已记录。

成功后节点仍会进入 `awaiting_human_decision`，不会自动继续。

### 6.8 记录人工决定

```powershell
$approved = Invoke-TaskGateway @{
    protocol_version = 1
    request_id = 'sample-approve-001'
    operation = 'recordHumanDecision'
    payload = @{
        node_id = $NodeId
        attempt_id = $AttemptId
        decision = 'approve'
        authority_ref = 'user-input:approve-sample-review'
    }
}
```

可用决定：

| Decision | 使用时机 |
| --- | --- |
| `approve` | 当前完成证据和 artifact 可接受 |
| `request_revision` | 需求/指令需要修订；必须提供 `instruction` |
| `retry_node` | 当前失败可安全重试；新 Attempt 必须引用旧 Attempt |
| `release_contamination` | 用户明确确认污染并允许重新 preflight；必须引用 contaminated Attempt |
| `abort` | 停止 Task |

## 7. 项目侧应该落盘什么

建议具体项目至少增加一份项目内 Workflow 说明，内容包括：

```text
目标与适用任务
节点名称和顺序
每个节点的 role / Adapter / model / Reasoning / permission
Prompt 模板或输入来源
artifact 绝对路径生成规则
required sections / min bytes
人工检查点负责人
失败、污染和取消处理
哪些信息禁止进入 evidence
toolkit 固定版本
```

不要把 domain-specific prompt 或验收规则加入 `agent-workflow-toolkit`；它们应跟随具体项目版本化。

## 8. 失败处理速查

| 现象/错误 | 含义 | 正确处理 |
| --- | --- | --- |
| `binding_not_registered` | 请求 binding 与 policy 不完全一致 | 修正 policy/request；不要放宽为任意匹配 |
| `workspace_mismatch` | Task 与 binding workspace 不同 | 使用 canonical absolute path，重新 preflight |
| `mcp_*_unsupported` | Adapter 无法证明 task MCP 隔离 | 清空非必要 allowlist 或更换已验证路径；不要继承全局 MCP |
| `delivery_missing` | terminal/exit 成功但缺合规正文或 artifact | 等核心签发同 session delivery-only `send`，不要自行续写 |
| `attempt_in_flight` | 同一节点已有运行 Attempt | observe/cancel 现有 Attempt，不要重复 launch |
| `action_outcome_unknown` | lease/副作用状态不确定 | 停止自动 retry，进入人工判断 |
| `identity_mismatch` / contaminated | provider/session/workspace/canary 不一致 | 冻结；用户决定 release、revision 或 abort |
| `checkpoint_required` | 节点正在等待人工决定 | 先调用 `recordHumanDecision` |
| Codex CLI 401 | 隔离调用忽略用户 config，缺 custom provider route | 显式配置非敏感 route；凭据仍留在宿主环境 |

## 9. 验证与验收

### toolkit 升级前

在 toolkit checkout 运行：

```powershell
go test -count=1 ./...
go vet ./...
go build ./...
git diff --check
```

Windows `go test -race` 需要额外 CGO/C 编译器工具链；当前项目不依赖 CGO，它是可选补充验证，不是现有 `experimental` 阻塞项。

### 项目首次接入

至少完成：

1. `--version` 和 `--validate-request`；
2. 临时 state-dir 的 `createTask` CLI smoke；
3. 目标 workspace 的 core preflight；
4. 所选 Native/Adapter 的真实只读 smoke；
5. artifact hash 和 required sections 校验；
6. 人工 `approve` 检查点；
7. cleanup 后无孤儿进程和未提交 secret/runtime 文件；
8. 一次独立 reviewer 对实际 diff、证据和边界的检查。

### 项目上线边界

当前只应标记为“项目内受控试用”。如果真实项目实践产生通用缺陷，把最小复现和脱敏证据带回 toolkit；业务特有规则留在项目仓库。Wave 5、写入能力或生产化需要另行设计与批准，不因一次成功试用自动获得授权。

## 10. 当前最小落地路径

如果目标是尽快在一个具体项目试用，推荐顺序是：

```text
固定 toolkit 版本
  → 构建 agent-workflow-core
  → 项目外 state-dir + 本机 policy
  → 只读 reviewer Node + Markdown artifact
  → Codex App Native Controller
  → completion evidence
  → 人工 approve
  → 保存项目侧试用记录
```

这条路径不要求继续开发 Wave 5，也不要求先实现统一外部 Adapter executor。需要 Cursor/Claude/Codex CLI 自动桥接时，再把 executor command 作为独立通用能力推进。
