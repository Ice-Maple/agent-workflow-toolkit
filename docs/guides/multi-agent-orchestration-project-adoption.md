# 多 Agent 协同框架项目接入与使用指南

- 目标读者：准备在具体项目中使用当前协同框架的技术负责人、Controller 开发者和项目维护者
- 当前推荐用途：只读探索、代码/文档评审、方案比较、证据收集
- 当前成熟度：`experimental`
- 架构背景：[`多 Agent 协同框架架构说明`](../architecture/multi-agent-orchestration-architecture.md)

## 1. 先确认任务是否适合

首轮项目接入应同时满足：

- Agent 只需要读取项目并输出报告或评审 artifact；
- 任务可以拆成一个或多个明确命名的 Workflow node；
- 业务 Skill 只声明 `role/task/count/after`；Adapter/model binding 由本机 route 配置；
- 普通节点完成后允许停下来等待人工批准；collaboration 内允许 validated completion 经 technical release 推进；
- Provider/CLI 已在当前机器完成登录或凭据配置，但凭据不会写入仓库；
- 团队接受当前没有 dashboard、daemon 和生产 SLA。

以下场景不要直接使用当前 `experimental` 路径：

- 让 Agent 自动修改业务代码并直接合并；
- 无人工检查点的长链自治；
- 依赖未验证的 MCP 全局配置；
- 需要跨机器队列、邮件箱、长期 daemon 或生产 SLA；
- 需要把领域业务规则写回共享核心。

## 2. 选择接入路径

| 路径 | 推荐度 | 适用条件 | 当前接入方式 |
| --- | --- | --- | --- |
| Codex App Native | 首轮推荐 | 已在 Codex App 中工作，希望用原生 subagent 做只读 explorer/reviewer | 将 pinned Skill 安装到消费仓库 `.agents/skills`，通过 Gateway 签发/回填 Native action |
| Cursor Agent | 条件可用 | 已安装并登录 Cursor Agent；任务不需要 MCP allowlist | `agent-workflow-bridge` 映射 core-signed HostAction |
| Claude Code | 条件可用 | 已安装并登录 Claude Code；MCP 配置可严格或配置隔离 | `agent-workflow-bridge` 映射 core-signed HostAction |
| Codex CLI | 条件可用 | 已安装并登录 Codex CLI；provider route、sandbox 和 approval 可显式配置 | `agent-workflow-bridge` 映射 core-signed HostAction |

三个 external CLI 共用公开 `agent-workflow-bridge`；Codex App Native 是另一条宿主路径，不经过该 CLI bridge。具体项目不能直接 import `internal/adapters/*`。

### 2.1 普通接入：Role-first

业务 Workflow/项目集中维护 `roles/roles.yaml` 和完整 Prompt；业务 Skill 的 `collaboration.yaml` 只写：

```yaml
schema_version: collaboration/v1
role_dependencies: [sample-workflow/reviewer]
steps:
  - id: review
    role: sample-workflow/reviewer
    task: 按当前 Skill 的业务规则评审实际修改
    count: 2
```

先从同一个 pinned clean checkout 把三个公开命令构建到 checkout 外，再安装带 Role 依赖的业务 Skill：

```powershell
$Dist = Join-Path $env:LOCALAPPDATA "agent-workflow-toolkit\dist\$PinnedCommit"
$Core = Join-Path $Dist 'agent-workflow-core.exe'
$Collaboration = Join-Path $Dist 'agent-workflow-collaboration.exe'
$Bridge = Join-Path $Dist 'agent-workflow-bridge.exe'
New-Item -ItemType Directory -Force -Path $Dist | Out-Null
go build -o $Core .\cmd\agent-workflow-core
go build -o $Collaboration .\cmd\agent-workflow-collaboration
go build -o $Bridge .\cmd\agent-workflow-bridge

.\tools\orchestration\install_role_first_skill.ps1 `
  -ToolkitRoot $ToolkitRoot -ConsumerRoot $ConsumerRoot `
  -SkillSource $BusinessSkill -RolePackage $RolePackage `
  -CorePath $Core -CollaborationPath $Collaboration -BridgePath $Bridge
```

该安装器会复制业务 Skill、其声明的 Role Package 和 framework Controller Skill，并写入来源/hash receipt 与 `.agents/agent-workflow-role-first-entry.json` managed trigger registry；相同 package/version/hash 的共享 Role Package 会复用，内容冲突则 fail closed。三个 `.exe` 固定在本机 versioned dist，不会复制进业务仓库。

安装后新建宿主任务。用户仍只调用业务 Skill；宿主在正常 Skill-dispatch 决策中发现已安装 Controller 后，根据 managed registry 进入 `invoke_business_skill.ps1`，从 schema-v2 receipt 读取并校验三个 binary 的 canonical absolute path，再调用 collaboration Router。业务 Skill 不需要显式调用 Controller 或下面的命令；下列命令仅用于理解和排障。这是 task-start Skill discovery 后的 managed host-policy 合同，不是能够拦截“未发现 Controller 的宿主”的 OS hook；fresh task discovery 是保留的人工边界。

```powershell
agent-workflow-collaboration.exe `
  --collaboration-file <skill>\collaboration.yaml `
  --roles-manifest <roles>\roles.yaml `
  --workspace <business-repository> `
  --objective <current-objective> `
  --state-dir <machine-local-new-run-state> `
  --coordination-dir <machine-local-coordination> `
  --policy-file <machine-local-routes-and-profiles>
```

返回 `direct` 时走宿主普通委派并如实报告不保证独立实例数；返回 `single` 时禁止 spawn/Gateway；只有 `framework` 才返回 Task、run、ready instances，并由 Controller 继续执行。安装不等于启用：machine gate 默认关闭，workspace 默认 `direct`。

Router 完全未安装时，业务 Skill 的完整正文照常执行；已安装但 gate 默认关闭时为 `direct`；`single` 由当前 Agent 串行执行且禁止 spawn/Gateway；`framework` 才进入 Controller/Gateway。一旦 framework admission 已选择，Role/route/preflight/permission/workspace/MCP 或运行证据失败必须 fail closed，不允许偷偷改走 `direct`。

### 2.2 启用、回退与一键关闭

Role-first 安装保持零行为变化。需要启用 framework 时，必须显式配置同一个 machine-local coordination root：

```powershell
$CoordinationDir = Join-Path $env:LOCALAPPDATA 'agent-workflow-toolkit\coordination'
& $Core --coordination-dir $CoordinationDir --enable
& $Core --coordination-dir $CoordinationDir --set-workspace-mode `
  --workspace $ConsumerRoot --workspace-mode framework
& $Core --coordination-dir $CoordinationDir --status --workspace $ConsumerRoot
```

只有 status 同时返回 `framework_enabled=true` 和 `effective_mode=framework` 才会创建 run。将 workspace mode 改为 `direct` 可保留业务 Skill、改走宿主普通委派；改为 `single` 则由当前 Agent 串行执行且禁止 spawn/Gateway。

```powershell
& $Core --coordination-dir $CoordinationDir --set-workspace-mode `
  --workspace $ConsumerRoot --workspace-mode direct
& $Core --coordination-dir $CoordinationDir --disable
```

`--disable` 先关闭新 admission、递增 gate epoch，再按 active-run 索引收敛已有 Attempt；返回的 run 状态可能是 `cancelled`、`cancel_pending`、`not_contacted`、`stale_owner` 或 `outcome_unknown`。它不伪造 Provider 已取消，也不替代业务审批；`cancel_pending`/`outcome_unknown` 必须保留 state/owner evidence 人工处理。

## 3. 推荐部署拓扑

不要把 toolkit 的 runtime state 复制进业务仓库。推荐保留两个独立 checkout/目录：

```text
E:\tools\agent-workflow-toolkit\          # 固定版本的通用框架
E:\projects\sample-project\              # 具体项目
  ├─ .agents\skills\<business-skill>\       # 业务 Skill + collaboration.yaml
  ├─ .agents\skills\multi-agent-orchestration\ # 已安装的 Controller Skill
  ├─ .agents\roles\<role-package>\           # roles.yaml + complete Prompt templates
  ├─ .agents\agent-workflow-toolkit.lock.json   # 三 binary/Skill 来源 receipt
  ├─ .agents\agent-workflow-role-first-<skill>.lock.json
  ├─ .agents\agent-workflow-role-first-entry.json # 统一 managed business-Skill trigger registry
  └─ reports\agent-review.md                    # 显式交付物，可按项目规则决定是否提交

%LOCALAPPDATA%\agent-workflow-toolkit\
  ├─ dist\<commit>\                        # 三个公开 binary
  ├─ coordination\                         # machine gate / workspace mode / active-run index
  └─ sample-project\core-0.2.0-p1-<commit>\
       ├─ policy.local.json                 # 本机 route/profile；不含凭据
       ├─ bridge\                           # owner receipt；不含 bearer capability
       └─ state\                            # snapshot / events / request receipts
```

职责分配：

| 内容 | 放置位置 |
| --- | --- |
| 通用核心、Adapter、Controller contract | `agent-workflow-toolkit` |
| `role/task/count/after`、Role Prompt、required sections、业务验收 | 具体项目或可移植 Workflow |
| Adapter/model/reasoning/permission/workspace route 与 external profile | machine-local policy/profile |
| provider token/登录态 | CLI/宿主自己的凭据存储，不进入任何项目文件 |
| Gateway runtime state | 用户级或项目外 `state-dir` |
| Controller Skill 与来源 receipt | 消费仓库 `.agents/`；是否提交由项目治理决定 |
| 最终报告/artifact | Task workspace 内的显式绝对路径 |

## 4. 固定、构建并安装同一版本

不要继续固定早于本指南修复的 Wave 4 基线 `130a901`。应选择包含 adoption hardening 的已提交 commit 或 release，并从干净 checkout 构建：

```powershell
$PinnedCommit = '<adoption-hardening-commit>'
$ToolkitRoot = 'E:\tools\agent-workflow-toolkit'
$ConsumerRoot = 'E:\projects\sample-project'
$CoreInstallRoot = Join-Path $env:LOCALAPPDATA "agent-workflow-toolkit\dist\$PinnedCommit"
$Core = Join-Path $CoreInstallRoot 'agent-workflow-core.exe'
$Collaboration = Join-Path $CoreInstallRoot 'agent-workflow-collaboration.exe'
$Bridge = Join-Path $CoreInstallRoot 'agent-workflow-bridge.exe'

Set-Location $ToolkitRoot
git checkout --detach $PinnedCommit
if (git status --porcelain=v1) { throw 'toolkit checkout must be clean' }
go test -count=1 ./...
New-Item -ItemType Directory -Force -Path $CoreInstallRoot | Out-Null
go build -o $Core .\cmd\agent-workflow-core
go build -o $Collaboration .\cmd\agent-workflow-collaboration
go build -o $Bridge .\cmd\agent-workflow-bridge
& $Core --version
& $Collaboration --version
& $Bridge --version
go version -m $Core
```

三个 binary 的 `--version` 都必须显示统一的 `version=0.2.0 protocol=1 maturity=experimental revision=<PinnedCommit> dirty=false`，仅命令名前缀不同。`revision=unknown`、commit 不同或 dirty 状态不符都不能安装为固定版本。

没有业务 Skill/Role Package 的 legacy 高级接入，可以单独安装 Controller Skill。`Copy` 是最低权限路径；确认本机支持 symlink 时也可改用 `SymbolicLink`：

```powershell
$Installer = Join-Path $ToolkitRoot 'tools\orchestration\install_controller_skill.ps1'
& $Installer `
  -ToolkitRoot $ToolkitRoot `
  -ConsumerRoot $ConsumerRoot `
  -CorePath $Core `
  -CollaborationPath $Collaboration `
  -BridgePath $Bridge `
  -Mode Copy

& $Installer `
  -ToolkitRoot $ToolkitRoot `
  -ConsumerRoot $ConsumerRoot `
  -CorePath $Core `
  -CollaborationPath $Collaboration `
  -BridgePath $Bridge `
  -Check
```

安装器生成：

- `$ConsumerRoot\.agents\skills\multi-agent-orchestration\`；
- `$ConsumerRoot\.agents\agent-workflow-toolkit.lock.json`；
- toolkit commit/dirty、三个 binary 各自的 canonical path/SHA-256/version/protocol/maturity/revision/dirty、Skill source/target/tree hash 和安装模式。

binary 必须构建到 checkout 外；如果把 `bin/` 之类未忽略目录写进 toolkit，安装器会按设计把 checkout 判为 dirty 并拒绝 receipt。安装器还会拒绝未知 VCS revision、core 与 checkout revision/dirty 不一致、已有目标和已有 receipt；`-AllowDirty` 仅供 toolkit 自身开发 smoke，不是消费项目接入方式。每次开始 Gateway Workflow 前先运行 `-Check`。安装或升级 Skill 后新建 Codex 任务，让宿主从消费仓库 `.agents/skills` 重新发现 Skill；不要静默使用全局旧副本。

项目升级时先归档旧 receipt 和 evidence，使用新的 version-specific state-dir，再构建、重装、`-Check` 和 smoke。当前没有 state migration：不要让新 binary 写旧 state-dir，也不要让多个 checkout 共享同一个 state-dir。

普通 Role-first 接入应使用第 2.1 节的 `install_role_first_skill.ps1`，并以相同参数执行 `-Check`；它会同时委托 canonical Controller installer 校验 `.agents\agent-workflow-toolkit.lock.json`，避免只安装业务 Skill 而漏掉 Controller receipt。

## 5. 准备本机 Role route 与 Adapter profile

Gateway 不提供“随便派发”的隐式默认值。Role-first 使用 `role_routes` 把业务 role 映射到本机 binding；legacy 低层 API 仍使用 `registered_bindings`。下面是 Codex App Native reviewer 模板：

```json
{
  "registered_bindings": [
    {
      "role": "sample-workflow/reviewer",
      "adapter": "codex-native",
      "model": "your-registered-model",
      "reasoning": "high",
      "permission": "read-only",
      "workspace": "E:\\projects\\sample-project",
      "mcp_allowlist": []
    }
  ],
  "role_routes": {
    "sample-workflow/reviewer": {
      "role": "sample-workflow/reviewer",
      "primary": {
        "role": "sample-workflow/reviewer",
        "adapter": "codex-native",
        "model": "your-registered-model",
        "reasoning": "high",
        "permission": "read-only",
        "workspace": "E:\\projects\\sample-project",
        "mcp_allowlist": []
      }
    }
  },
  "retry_mode": "limited_auto",
  "max_auto_retries": 1,
  "fallback_mode": "ask",
  "mcp_isolation": "strict",
  "goal_enabled": false
}
```

注意：

- `workspace` 是本机 absolute canonical path，因此这个 policy 通常不直接提交；
- Role-first role 使用 `<package>/<role>`，必须与 `roles.yaml`、`collaboration.yaml`、route key、route role 和 binding role 完全一致；
- `role/adapter/model/reasoning/permission/workspace/MCP allowlist` 必须和已批准 binding 完全匹配；route binding 的空 workspace 会在 run 创建时解析为 canonical task workspace，但首轮建议显式写明；
- 首轮保持 `read-only`、`goal_enabled=false` 和空 MCP allowlist；
- policy 只记录声明，不保存 API key、token、header 或 provider base URL 中的凭据；
- policy loader 对顶层和 nested binding 使用 strict JSON：未知字段、拼错字段和 trailing JSON 都会拒绝，不会静默落到默认值。

外部 Adapter 的标识分别是 `cursor`、`claude`、`codexcli`。它们还要求 route binding 的 `profile_id` 引用同一 policy 的 `adapter_profiles`；profile 至少包含匹配的 `id`、`adapter`、非敏感执行参数和 `profile_hash`。先把单个 profile 保存为 JSON（`profile_hash` 可为空），再执行 `& $Bridge --hash-profile --profile-file $ProfileDraft | Set-Content -Encoding utf8NoBOM $ProfileFile` 生成 canonical profile；把同一对象复制到 policy 的 `adapter_profiles`。不要手算或只 hash 文件文本。切换 Adapter 时必须新增完整 route/profile 并通过 bridge preflight，不要只改一个字符串后沿用旧批准。

例如 Cursor route/profile 的结构为：

```json
{
  "role_routes": {
    "sample-workflow/reviewer": {
      "role": "sample-workflow/reviewer",
      "primary": {
        "role": "sample-workflow/reviewer",
        "adapter": "cursor",
        "profile_id": "cursor-readonly",
        "model": "your-registered-cursor-model",
        "reasoning": "high",
        "permission": "read-only",
        "workspace": "E:\\projects\\sample-project",
        "mcp_allowlist": []
      }
    }
  },
  "adapter_profiles": {
    "cursor-readonly": {
      "id": "cursor-readonly",
      "adapter": "cursor",
      "executable": "cursor-agent.exe",
      "trust": "authorized",
      "mode": "plan",
      "profile_hash": "<agent-workflow-bridge --hash-profile 生成的 sha256>"
    }
  }
}
```

Claude 通常配置 `trust=authorized`，需要 MCP 时使用经过验证的 `mcp_config_path`；Codex CLI 使用 `sandbox=read-only`、`approval=untrusted`，custom Provider 的非敏感 route 可放在 `provider_ref/provider_base_url/provider_wire_api/provider_auth/environment_ref`，凭据本身仍留在宿主环境。profile JSON 可以从 policy 拆成独立文件传给 bridge，但内容与 hash 必须相同。

## 6. Role-first 运行闭环

### 6.1 创建冻结 run

先执行第 2.2 节的 enable/status，再调用：

```powershell
$Run = & $Collaboration `
  --collaboration-file $InstalledCollaboration `
  --roles-manifest $InstalledRoles `
  --workspace $ConsumerRoot `
  --objective '只读评审当前修改并生成规定 artifact' `
  --state-dir $StateDir `
  --coordination-dir $CoordinationDir `
  --policy-file $PolicyFile | ConvertFrom-Json
```

`$InstalledCollaboration` 和 `$InstalledRoles` 分别指向消费仓库 `.agents\skills\<business-skill>\collaboration.yaml` 与 `.agents\roles\<role-package>\roles.yaml`。只在 `$Run.effective_mode -eq 'framework'` 时读取 `$Run.run.run` 和 `$Run.run.instances`。`direct` 不会创建 Gateway run；`single` 只报告当前 Agent 执行。不要用请求的 `count` 伪造实际实例数。

> JSON 层级说明：外层 `$Run.run` 是 Router 的 `run` 字段，内层 `.run` 是 Gateway 返回的 `CollaborationRun`，因此 run ID 路径为 `$Run.run.run.run_id`；instances 路径为 `$Run.run.instances`。

### 6.2 准备 ready instance 与 Attempt

Role-first 后续 Gateway 调用必须同时携带 coordination root 和已安装 Role manifest：

```powershell
function Invoke-CollaborationGateway {
    param([hashtable]$Request)
    $raw = ($Request | ConvertTo-Json -Depth 20 -Compress) | & $Core --gateway `
      --state-dir $StateDir --policy-file $PolicyFile `
      --roles-manifest $InstalledRoles --coordination-dir $CoordinationDir
    if ($LASTEXITCODE -ne 0) { throw "core process failed: $LASTEXITCODE" }
    $response = $raw | ConvertFrom-Json
    if (-not $response.ok) { throw "$($response.error.code): $($response.error.message)" }
    return $response.payload
}
```

对返回状态为 `ready` 的每个 instance，调用 `prepareCollaborationInstance`，传入 exact `run_id`、`instance_id`、`gate_epoch` 和业务交付合同；它会从 machine-local route 解析 binding、验证 Role Prompt hash，并创建/复用对应 node：

```powershell
$Prepared = Invoke-CollaborationGateway @{
  protocol_version = 1; request_id = '<new-request-id>'
  operation = 'prepareCollaborationInstance'
  payload = @{
    run_id = $Run.run.run.run_id
    instance_id = '<ready-instance-id>'
    gate_epoch = $Run.run.run.gate_epoch
    kind = 'role-first-review'
    expected_output = @{
      format = 'markdown'; artifact_path = '<absolute-path-inside-workspace>'
      required = $true; min_bytes = 200
      required_sections = @('# Review', '## Findings', '## Verdict')
    }
  }
}
if ($Prepared.preflight.status -ne 'passed') { throw $Prepared.preflight.code }
```

若 `$Prepared.binding.adapter` 是 `cursor`、`claude` 或 `codexcli`，必须先把 `$Prepared.binding` 写成 `$BindingFile`，并使用与该 binding 的 `profile_id` 对应、已经 `--hash-profile` 固定的 `$ProfileFile` 做 Adapter capability preflight：

```powershell
$Prepared.binding | ConvertTo-Json -Depth 10 | Set-Content -Encoding utf8NoBOM $BindingFile
$AdapterCheck = & $Bridge --preflight `
  --binding-file $BindingFile --profile-file $ProfileFile | ConvertFrom-Json
if ($AdapterCheck.status -ne 'passed') { throw $AdapterCheck.code }
```

Codex App Native 不经过 CLI bridge，但 Controller 仍必须按宿主合同确认当前 Native capability、workspace 和只读权限。selected host/Adapter preflight 通过后，才能调用 `prepareAttempt` 签发第一个 HostAction：

```powershell
$Launch = Invoke-CollaborationGateway @{
  protocol_version = 1; request_id = '<new-request-id>'
  operation = 'prepareAttempt'
  payload = @{
    node_id = $Prepared.node.node_id
    controller_instance_id = $ControllerId
    collaboration_run_id = $Run.run.run.run_id
    instance_run_id = $Prepared.instance.instance_id
    gate_epoch = $Run.run.run.gate_epoch
    role_prompt_hash = $Prepared.role_definition.prompt_hash
  }
}
$AttemptId = $Launch.attempt.attempt_id
```

不要在此重新提供 Adapter/model 或手写 dispatch Prompt；Gateway 会由冻结 Role Prompt、step task、上游 output refs 和 output requirements 生成并签发。

### 6.3 执行 Native 或 external CLI action

Codex App Native 只执行 core-signed pending action，并用 App 原生 subagent 回填真实 handle/session/identity/canary。三个 external CLI 必须已经在第 6.2 节、`prepareAttempt` 之前完成 preflight；此处只执行 core-signed pending action：

```powershell
& $Bridge --execute `
  --core $Core --state-dir $StateDir --policy-file $PolicyFile `
  --roles-manifest $InstalledRoles --coordination-dir $CoordinationDir `
  --bridge-state-dir $BridgeStateDir --profile-file $ProfileFile `
  --attempt-id $AttemptId --controller-instance-id $ControllerId `
  --action-kind launch
```

若结果非 terminal，继续用新的 bridge 进程执行 `--action-kind observe`；需要取消时使用 `--cancel --reason <reason>`。bridge 会自行从 core 拉取唯一 pending action、提交结构化结果并复用 detached owner；只有存在多个匹配 pending action 时才显式传 `--action-id`。不要直接 shell 调 Provider CLI，也不要自行构造 `HostActionResult`。

### 6.4 Completion 与 technical release

terminal evidence 到齐后，`requestCompletion` 必须同时传 `attempt_id`、`collaboration_run_id`、`instance_run_id` 和 frozen `gate_epoch`：

```powershell
$Completion = Invoke-CollaborationGateway @{
  protocol_version = 1; request_id = '<new-request-id>'
  operation = 'requestCompletion'
  payload = @{
    attempt_id = $AttemptId
    collaboration_run_id = $Run.run.run.run_id
    instance_run_id = $Prepared.instance.instance_id
    gate_epoch = $Run.run.run.gate_epoch
  }
}
if ($Completion.completion.status -ne 'completed') { throw $Completion.completion.reason }

$Release = Invoke-CollaborationGateway @{
  protocol_version = 1; request_id = '<new-request-id>'
  operation = 'recordCollaborationTechnicalRelease'
  payload = @{
    run_id = $Run.run.run.run_id
    instance_id = $Prepared.instance.instance_id
    reason = 'validated framework completion'
  }
}
```

返回的 `next_ready` 是唯一可继续准备的下游 instances。technical release 不创建 `HumanDecision`、不伪造 `user-input:`，也不代表业务审批；failed completion、`delivery_missing`、contamination、cancel 风险或 outcome unknown 都必须停止自动推进。

## 7. Legacy Gateway 高级参考

以下完整 binding/Gateway loop 用于兼容没有 `collaboration.yaml` 的 legacy 接入、排障或开发底层 Controller；普通业务接入优先使用第 6 节 Role-first 路径。

### 7.1 调用特性

`agent-workflow-core --gateway` 每次只从 stdin 读取一个 protocol v1 request，输出一个 response 后退出。每个步骤都使用：

- 同一个 `--state-dir`；
- 同一个 `--policy-file`；
- 新的 `request_id`；
- 上一步 response 中返回的真实 task/node/attempt/action ID。

one-shot CLI 会对整个 state-dir 获取 OS file lock；同一 state-dir 的请求因此串行，不支持用多个并发 Controller 提升吞吐。锁在进程退出时由 OS 释放，等待超过 `--lock-timeout` 会返回结构化 `state_dir_busy`。

request journal 的重放语义是：`completed` 返回同一 durable payload，确定的 `failed` 返回同一业务错误，相同 ID 加不同内容返回 `request_id_reused`；如果进程在 durable outcome 前中断，`pending/outcome_unknown` 重放会返回 `request_outcome_unknown`，不会再次 dispatch。遇到未知结果时停止自动推进并保留 state/receipt 供人工检查，不要换新 request ID 重做同一意图。

`--validate-request` 只验证 protocol v1 envelope，包括 version、request ID、operation、payload object、unknown envelope field 和 trailing JSON；成功返回 `envelope_valid`。operation-specific payload 字段由 `--gateway` dispatch 严格校验，不能把 envelope valid 当成请求一定可执行。Gateway 的业务错误也会以 `ok=false` 的结构化 response 返回，所以不能只检查进程 exit code。

PowerShell 7 可以使用下面的薄调用函数：

```powershell
$Core = Join-Path $env:LOCALAPPDATA 'agent-workflow-toolkit\dist\<full-commit>\agent-workflow-core.exe'
$FrameworkId = 'core-0.2.0-p1-<commit12>'
$RuntimeRoot = Join-Path $env:LOCALAPPDATA "agent-workflow-toolkit\sample-project\$FrameworkId"
$StateDir = Join-Path $RuntimeRoot 'state'
$PolicyFile = Join-Path $RuntimeRoot 'policy.local.json'
New-Item -ItemType Directory -Force -Path $StateDir | Out-Null

function Invoke-TaskGateway {
    param([hashtable]$Request)

    $json = $Request | ConvertTo-Json -Depth 20 -Compress
    $raw = $json | & $Core --gateway --state-dir $StateDir --policy-file $PolicyFile --lock-timeout 5s
    if ($LASTEXITCODE -ne 0) {
        throw "agent-workflow-core process failed: $LASTEXITCODE"
    }
    $response = $raw | ConvertFrom-Json
    if (-not $response.ok) {
        $details = $response.error.details | ConvertTo-Json -Depth 10 -Compress
        throw "TaskGateway rejected request: $($response.error.code) - $($response.error.message) details=$details"
    }
    return $response.payload
}
```

如果必须使用 Windows PowerShell 5.1，优先把 request 保存为无 BOM UTF-8 文件再输入；不要依赖系统默认编码传递包含非 ASCII 字符的 JSON。

### 7.2 创建 Task

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

### 7.3 提议明确节点和交付物

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

### 7.4 运行 core policy preflight

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

### 7.5 准备 Attempt

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

### 7.6 Poll 并执行 core-signed action

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

Controller 必须从消费仓库加载 `.agents/skills/multi-agent-orchestration/SKILL.md`，由 managed entry 在路由前以 `-Check` 核验 receipt，并仅调用 receipt 中三个 absolute paths；PATH 或全局旧 binary 不是 fallback。该副本的上游合同见 [`skills/multi-agent-orchestration/SKILL.md`](../../skills/multi-agent-orchestration/SKILL.md)、[`invoke_business_skill.ps1`](../../skills/multi-agent-orchestration/invoke_business_skill.ps1) 和 [`native-action-contract.md`](../../skills/multi-agent-orchestration/references/native-action-contract.md)。没有 pending core action 时禁止启动 subagent。

Gateway v1 公开 HostAction 只有：

| Action | 签发时机 | Lease | 执行者 | 结果 |
| --- | --- | --- | --- | --- |
| `launch` | `prepareAttempt` 成功后 | 5 分钟；host 启动后应立即提交 running/terminal result | Native host / Adapter bridge | 绑定 handle、session、identity、canary；terminal=false 时产生 `observe` |
| `observe` | 已启动且未 terminal | 15 分钟，覆盖真实 reviewer/provider 的同步等待窗口 | 同一 host/session | 提交一次 terminal 或失败 evidence |
| `cancel_host` | `requestCancel` | 5 分钟 | host/Adapter runner | `cancelled` 或 `cancel_unconfirmed` |

`send` 和 `cleanup` 仅是 Adapter lifecycle primitive，不是 Gateway HostAction；Gateway 不签发也不接受它们。Provider 进程和临时资源由 Native host 或 Adapter runner 在 `finally` 中做幂等 cleanup，不能等待或提交一个 core-signed cleanup result。

#### 外部 CLI 路径

使用 `agent-workflow-bridge --preflight` 校验 frozen Binding/profile，再用 `--execute --attempt-id ... --controller-instance-id ...` 拉取并执行唯一 pending HostAction。首次 `launch` 创建临时 owner；后续 `observe`/`cancel_host` 必须命中同一 PID/start marker/loopback endpoint/capability/profile hash，缺失或 stale 时不会重新 launch。bridge 直接把结构化结果提交 Gateway；不要直接 shell 拼接 Provider CLI 后再伪造 core result。

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

### 7.7 请求 Completion

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

普通节点成功后仍进入 `awaiting_human_decision`。带 collaboration linkage 的 instance 可由 Controller 请求 `recordCollaborationTechnicalRelease`；只有 validated completed、交付完整、依赖满足且无 contamination/outcome unknown/cancel 风险时才会放行下游，不创建 `HumanDecision`，也不代表业务审批通过。

每个 Attempt 的 completion checkpoint 只记录一次。无论第一次结果是 completed 还是 `delivery_missing`，使用新 request ID 再次调用 `requestCompletion` 都会返回 `completion_already_recorded`；相同 request ID 的网络重放仍由 request journal 返回原 outcome。不得在 checkpoint 后本地补 artifact 并重算同一 Attempt。

### 7.8 记录人工决定

```powershell
$approved = Invoke-TaskGateway @{
    protocol_version = 1
    request_id = 'sample-approve-001'
    operation = 'recordHumanDecision'
    payload = @{
        node_id = $NodeId
        decision = 'approve'
        authority_ref = 'user-input:approve-sample-review'
    }
}
```

可用决定：

| Decision | `attempt_id` | 其他要求 | 语义 |
| --- | --- | --- | --- |
| 普通 `approve` | **禁止携带** | `authority_ref` | 只批准已成功完成且 artifact 合规的当前节点 |
| fallback `approve` | 必须是当前失败 checkpoint 的 Attempt | `authority_ref` | 只授权后续 `prepareFallbackAttempt`，不会批准节点 |
| `request_revision` | 不需要 | `instruction`、`authority_ref` | 创建新 revision 和新 node；`delivery_missing` 的支持恢复路径 |
| `retry_node` | 不需要 | `authority_ref`；后续 retry operation 引用旧的非 completed Attempt | 重新开放可重试的原 node；`delivery_missing` 的 completed Attempt 会返回 `retry_not_allowed` |
| `release_contamination` | 必须是 unreleased contaminated Attempt | 直接 `user-input:` authority | 确认污染并强制重新 preflight |
| `abort` | 不需要 | `authority_ref` | 停止 Task |

普通成功 `approve` 如果错误携带成功 Attempt ID，会按 fallback approval 解释并返回 `invalid_fallback_approval`；不得通过删改 core 校验来适配旧示例。

## 8. 项目侧应该落盘什么

建议具体项目至少增加一份项目内 Workflow 说明，内容包括：

```text
目标与适用任务
节点名称和顺序
每个步骤的 role / task / count / after
Role Package 与完整 Prompt 模板
本机 route/profile 的配置责任（不写入共享业务 Skill）
artifact 绝对路径生成规则
required sections / min bytes
人工检查点负责人
失败、污染和取消处理
哪些信息禁止进入 evidence
toolkit 固定版本
```

不要把 domain-specific prompt 或验收规则加入 `agent-workflow-toolkit`；它们应跟随具体项目版本化。

## 9. 失败处理速查

| 现象/错误 | 含义 | 正确处理 |
| --- | --- | --- |
| `binding_not_registered` | 请求 binding 与 policy 不完全一致 | 修正 policy/request；不要放宽为任意匹配 |
| `role_required` / `node_role_mismatch` | node role 为空或与 binding role 不同 | 新建/修订明确 role 的 node，使用完全一致的 binding |
| `workspace_mismatch` | Task 与 binding workspace 不同 | 使用 canonical absolute path，重新 preflight |
| `mcp_*_unsupported` | Adapter 无法证明 task MCP 隔离 | 清空非必要 allowlist 或更换已验证路径；不要继承全局 MCP |
| `delivery_missing` | terminal/exit 成功但缺合规正文或 artifact | 停在人工 checkpoint，选择 `request_revision` 或 `abort`；Gateway v1 不签发 `send`，也拒绝会形成死路的 `retry_node` |
| `delivery_revision_required` | 对 `delivery_missing` 误用了普通 `approve` | 保持 checkpoint，改用 `request_revision` 或 `abort`；不要添加 `attempt_id` 尝试 fallback |
| `completion_already_recorded` | 当前 Attempt 已形成 completion checkpoint | 不重算同一 Attempt；记录人工决定 |
| `attempt_in_flight` | 同一节点已有运行 Attempt | observe/cancel 现有 Attempt，不要重复 launch |
| `action_outcome_unknown` | lease/副作用状态不确定 | 停止自动 retry，进入人工判断 |
| `state_dir_busy` | 另一个进程持有同一 state-dir OS lock | 等待 owner 退出后重放原 request；不要并发 Controller |
| `request_outcome_unknown` | request 已开始但 durable outcome 不可安全重放 | 停止并保留 state/receipt 人工检查；不要换 ID 重做同一意图 |
| `unsupported storage schema` | binary 与 state-dir schema/protocol 不兼容 | 保持旧 evidence 不变，改用该版本的新 state-dir；当前不迁移 |
| `identity_mismatch` / contaminated | provider/session/workspace/canary 不一致 | 冻结；用户决定 release、revision 或 abort |
| `checkpoint_required` | 节点正在等待人工决定 | 先调用 `recordHumanDecision` |
| Codex CLI 401 | 隔离调用忽略用户 config，缺 custom provider route | 显式配置非敏感 route；凭据仍留在宿主环境 |

## 10. 验证与验收

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

1. clean pinned checkout 构建三个公开 binary，三者 `--version` 的 version/protocol/maturity/revision/dirty 一致，且 revision 与 commit 一致、`dirty=false`；
2. Role-first installer 安装业务 Skill、Role Package 和 Controller，canonical 与 role-first 两份 receipt 的 `-Check` 均通过，并在新宿主任务中确认发现；
3. machine gate/workspace mode 的 enable→status→direct/single→framework 与一键 disable 返回符合预期；
4. Role-first route 在 `direct` 不创建 run、`single` 不 spawn、`framework` 才按 `count` 创建 distinct instances；
5. Role Registry/Prompt hash、machine-local role route、binding 和 external profile hash 均通过 fail-closed 校验；
6. 所选 Native/Adapter 的真实只读 smoke；external CLI 还要覆盖 public bridge launch→跨进程 observe→completion→technical release→cleanup；
7. technical release 只推进满足依赖的 `next_ready`，且没有创建 `HumanDecision`、`user-input:` 或业务批准；
8. failed completion、`delivery_missing`、contamination、outcome unknown 和 cancel 风险拒绝 technical release；
9. disable 后无新 admission，已有 Attempt 的取消/未知状态与 active-run evidence 一致；仓库无 orphan、secret 或 runtime 文件；
10. 一次独立 reviewer 对实际 diff、公开入口、测试、receipts、指南与原始 Role-first 目标的检查。

toolkit 的普通测试已经把这条公开路径固化在 [`cmd/agent-workflow-core/adoption_test.go`](../../cmd/agent-workflow-core/adoption_test.go)：它覆盖 version/envelope、完整 create→propose→preflight→attempt→action→completion→approve、`delivery_missing` revision、8 进程同 request 串行、structured busy error 和 clean consumer Skill 安装/检查。真实 Provider smoke 仍保持 opt-in，不由 fixture 冒充。

legacy adoption hardening 见 [`AWT-adoption-hardening.md`](../../tests/orchestration/evidence/2026-08-11/AWT-adoption-hardening.md)；最新 Role-first clean-consumer、三 CLI public bridge handoff、Codex App Native smoke、technical-release 拒绝矩阵和独立 closure 见 [`role-first-implementation-smoke.md`](../../tests/orchestration/evidence/2026-08-13/role-first-implementation-smoke.md)。

### 项目上线边界

当前只应标记为“项目内受控试用”。如果真实项目实践产生通用缺陷，把最小复现和脱敏证据带回 toolkit；业务特有规则留在项目仓库。Wave 5、写入能力或生产化需要另行设计与批准，不因一次成功试用自动获得授权。

## 11. 当前最小落地路径

如果目标是尽快在一个具体项目试用，推荐顺序是：

```text
固定 toolkit 版本并在 checkout 外构建 core/collaboration/bridge
  → 安装并核验业务 Skill + Role Package + Controller receipts
  → version-specific state-dir + coordination root + strict 本机 role route/profile
  → 显式 enable machine gate，并把 workspace 设为 framework
  → 调用业务 Skill；agent-workflow-collaboration 创建冻结 run/ready instances
  → prepareCollaborationInstance + prepareAttempt
  → Codex App Native 或 external CLI public bridge
  → completion evidence + technical release + next_ready
  → DAG 完成后由业务 Workflow 执行真实人工验收
  → 保存项目侧试用记录
```

这条路径不要求继续开发 Wave 5。Codex App Native 使用宿主原生能力；Cursor/Claude/Codex CLI 的统一 public bridge 已实现并验收，不再由下游自行组合 Provider shell。
