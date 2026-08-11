# 多 Agent 协同框架项目接入问题清单与处理建议

- 日期：2026-08-11
- 状态：已关闭（public E2E、真实 Native smoke、Cursor broad review + targeted re-check）
- 审计基线：`9f048aae710fed3c5736281ae292bf626867d784`
- Wave 4 `experimental` 验收基线：`130a901`
- 项目接入指南提交：`e69860b`
- 适用范围：`agent-workflow-core --gateway`、protocol v1、`multi-agent-orchestration` Controller Skill 和项目接入指南

## 1. 结论

本文件第 2–8 节保留审计基线 `9f048aa` 上的原始问题与建议；当前实现已按第 9 节的 closure matrix 完成 adoption hardening，并通过全量验证、真实 Codex App Native smoke、真实 Cursor broad review 和独立 targeted re-check。最终 gate 为 PASS，不沿用早于本指南的 Wave 4 verdict。

本清单不要求提前实现写入型 Agent、Goal 自治、dashboard、daemon 或统一外部 Adapter executor。修复目标是让当前声明的“Codex App Native、只读 reviewer、Markdown artifact、人工检查点”最小路径真实、可重复、可验收。

### 本轮复核方式

- 在当前 HEAD 重新核对 guide、Controller Skill、public protocol、Gateway、policy、storage 和现有 tests，不继承旧 review 的 finding 状态。
- 定向运行 `go test -count=1 ./internal/gateway ./internal/policy ./internal/protocol/v1 ./cmd/agent-workflow-core`，现有测试通过；这说明下列问题主要位于未覆盖的 adoption/public-path 组合，而不是现有测试已经失败。
- 通过公共 CLI 独立复现：指南中的普通 approve 返回 `invalid_fallback_approval`；`delivery_missing` 后没有 pending `send`；相同 request ID 的 8 进程并发产生多个 Task/响应并发生 receipt 写竞争。
- 本轮没有重新运行真实 Provider smoke；这些问题在 fake/public CLI 层即可确认，不依赖领域业务或 Provider 登录态。

## 2. 优先级定义

| 优先级 | 定义 |
| --- | --- |
| P0 | 阻断指南所述最小接入路径，或使消费项目无法可靠固定和加载执行合同 |
| P1 | 不一定阻断首次 happy path，但会破坏恢复、安全边界、状态一致性或公开接口可信度 |
| P2 | 当前可通过明确限制规避，但需要在扩大试用或升级前收口 |

本轮未发现必须立即停止所有受控试验的 P0；现有问题可以通过预装 Skill、串行单 Controller、按 commit 固定版本以及对未知恢复结果 fail closed 暂时约束，但不能把这些约束误报为问题已经解决。

## 3. 问题总览

| ID | 优先级 | 类型 | 问题 | 建议归属 |
| --- | --- | --- | --- | --- |
| AWT-ADOPT-001 | P1 | 文档漂移 | 正常成功 `approve` 示例会被 Gateway 当作 fallback approval 拒绝 | Guide + Gateway conformance test |
| AWT-ADOPT-002 | P1 | 分发/发现合同缺口 | Controller Skill 缺少可复现、机器可核验的安装和来源记录 | Distribution/installation |
| AWT-ADOPT-003A | P1 | 实现缺陷 | delivery-only `send` 被声明为恢复路径，但 Gateway 没有公开签发和成功恢复路径 | Gateway + Controller contract |
| AWT-ADOPT-003B | P2 | 合同歧义 | `cleanup` 被列为 HostAction 和接入验收项，但责任方与公开触发方式未确定 | Gateway/Adapter/Guide |
| AWT-ADOPT-004 | P2 | 通用合同歧义 | Workflow node 的 `role` 与实际 binding 的 `role` 没有确定关系 | Gateway/policy/model |
| AWT-ADOPT-005 | P2 | 版本实现漂移 | `--version` 仍输出 `0.1.0-wave1`，不能证明 Wave 4 baseline 或 Skill 来源 | CLI/build metadata |
| AWT-ADOPT-006 | P2 | 已声明协议漂移 | `--validate-request` 按设计只验证 envelope，静态 schema 又缺少 runtime 已支持的 advanced operation | protocol v1 + CLI/docs |
| AWT-ADOPT-007 | P1 | 配置严格性 | policy JSON 静默接受未知字段，拼写错误可能落入默认行为 | policy loader |
| AWT-ADOPT-008 | P1 | 并发一致性 | one-shot CLI 只有进程内 mutex，同一 state-dir 的并发进程可能产生丢失更新 | storage/Gateway |
| AWT-ADOPT-009 | P1 | 幂等耐久性 | 状态变更与 request receipt 不原子，receipt 写失败后重放可能再次执行已生效请求 | Gateway/storage |
| AWT-ADOPT-010 | P2 | 升级合同 | state snapshot 缺少 schema/version 标识，跨版本续接边界未形成机器可判定合同 | storage/versioning |
| AWT-ADOPT-011 | P2 | 发布流程缺口 | adoption guide 在 Wave 4 验收后新增，未被可执行示例测试和独立接入复审覆盖 | CI/review process |

## 4. 详细问题与处理建议

### AWT-ADOPT-001：正常成功 `approve` 示例不可执行

**证据**

- [项目接入指南](../guides/multi-agent-orchestration-project-adoption.md) 第 321–331 行在正常完成后的 `approve` payload 中传入 `attempt_id`。
- [Gateway](../../internal/gateway/gateway.go) 第 1034–1044 行把所有携带 `attempt_id` 的 `approve` 解释为“针对失败 Attempt 的 fallback approval”；成功 Attempt 会返回 `invalid_fallback_approval`。
- [Gateway 测试](../../internal/gateway/gateway_test.go) 第 91–103 行的正常成功路径没有传 `AttemptID`，说明 core 行为与测试一致，漂移发生在后新增的指南示例。

**影响**

消费项目按指南完成第一个 reviewer node 后，会在最后一个人工检查点失败，导致整条最小接入路径无法照文档跑通。

**处理建议**

1. 从普通成功 `approve` 示例中删除 `attempt_id`。
2. 单独增加 fallback approval 示例，明确只有失败、`incomplete`、`timed_out` 或 `cancel_unconfirmed` checkpoint 才携带失败 Attempt ID。
3. 在 decision 表中区分普通 `approve`、fallback approval 和 `release_contamination` 所需字段。

**验收标准**

- 使用指南中的原始 PowerShell request 依次完成 create、node、preflight、attempt、completion 和普通 approve。
- 普通成功 approve 不含 `attempt_id` 且通过。
- 成功 Attempt 携带 `attempt_id` 时继续 fail closed。
- fallback approval 和 contamination release 各有独立正反测试。

### AWT-ADOPT-002：Controller Skill 缺少可复现的分发和来源记录

**证据**

- [项目接入指南](../guides/multi-agent-orchestration-project-adoption.md) 第 31、256–258 行要求 Controller 加载仓库内 `skills/multi-agent-orchestration/SKILL.md`。
- 指南同时推荐 framework checkout、消费项目和 runtime state 分离；当 Codex App 当前工作目录是消费项目时，文档没有说明如何发现另一个 checkout 中的 Skill。
- [OpenAI Docs：Build skills](https://learn.chatgpt.com/docs/build-skills#where-codex-loads-local-skills) 说明 Codex 会扫描消费仓库的 `.agents/skills`，并支持 symlinked Skill folder；当前指南没有把 upstream checkout 映射到这些发现位置。
- `skills/multi-agent-orchestration/` 当前只有 `SKILL.md` 和 Native action reference；指南没有给出安装命令、链接目标、来源记录或 core/Skill 版本核对步骤。

**影响**

消费项目即使固定了 core commit，仍缺少一条可重复、机器可核验的方式来说明实际加载的 Controller Skill 来自哪里；全局旧副本、手工复制或不同 checkout 可能形成 core/Skill contract drift。

**处理建议**

上游选择并正式支持一种最小分发方式，不要求同时实现多种：

1. 从固定 checkout 安装/链接到用户级 Skill 目录；或
2. 将固定 checkout 中的 Skill 链接到消费项目的 `.agents/skills`。

最低合同只需记录并检查：

- toolkit commit 或 release version；
- core binary canonical path 与 binary hash/版本；
- Controller Skill 链接目标或 source path；
- 安装模式和更新时间。

更强的 Skill 内容 hash、项目 lock 和 Controller/core runtime handshake 可以作为后续 hardening，不应被写成唯一可接受实现。无论采用哪种强度，都不能静默使用另一个旧的全局副本。

**验收标准**

- 从一个干净的 sample consumer 执行文档化安装/链接流程后，Codex App 能发现指定 Controller Skill。
- 安装记录能核对 checkout commit、binary 来源和 Skill 链接目标。
- 若实现了强 runtime handshake，替换成旧 Skill 或不同 core 后应在 preflight fail closed；若首版只做来源记录，则指南必须要求人工核对且不得宣称已有自动阻断。
- 升级不会复用旧版本正在运行的 state-dir。

### AWT-ADOPT-003A：delivery-only `send` 没有公开恢复路径

**证据**

- [Controller Skill](../../skills/multi-agent-orchestration/SKILL.md) 第 26、46 行描述 core-signed delivery-only `send`。
- [项目接入指南](../guides/multi-agent-orchestration-project-adoption.md) 第 371 行要求 `delivery_missing` 后等待 core 签发同 session `send`。
- [orchestration model](../../internal/orchestration/model.go) 定义了 `ActionSend`，Native fake executor 和 Gateway result handler 也认识该 kind。
- 当前 Gateway 的 `issueAction` 调用只签发 `launch`、`observe` 和 `cancel_host`，见 [gateway.go](../../internal/gateway/gateway.go) 当前第 569、763、1151 行；没有 `send` 的签发入口。
- 即使未来注入一个 `send` action，当前第 786–790 行只在 `send` 失败时把 Attempt 标为 `incomplete`，没有定义成功 delivery 如何恢复为可再次完成的状态。

**影响**

- `delivery_missing` 后 Controller 按指南等待会永久停住；局部 Adapter recovery 测试不能证明公共 Gateway 路径可用。

**处理建议**

1. 若保留当前恢复设计，应由 completion 的 `delivery_missing` 状态触发一次同 Attempt、同 verified session 的 core-signed action，保留第一次失败证据并禁止 relaunch。
2. 定义成功 `send` result 后 Attempt、terminal evidence 和 CompletionDecision 的合法状态转换。
3. 如果本 milestone 暂不实现，应删除“等待 core 签发 send”的承诺，并进入明确的人工 retry/revision/abort checkpoint，不能无限等待。

**验收标准**

- `delivery_missing → send → same-session delivery → completion` 可通过公共 protocol 完成，且最多签发一次 delivery-only send。
- 首次 delivery failure、session identity、canary、lease 和最终 artifact hash 均保留。

### AWT-ADOPT-003B：`cleanup` 的责任方和触发合同未闭合

**证据**

- [Controller Skill](../../skills/multi-agent-orchestration/SKILL.md) 第 26 行把 `cleanup` 列为可执行 action。
- [orchestration model](../../internal/orchestration/model.go) 定义了 `ActionCleanup`，各 Adapter/fake executor 存在相应处理，但 Gateway 没有签发路径。
- [项目接入指南](../guides/multi-agent-orchestration-project-adoption.md) 第 403 行要求首次接入检查 cleanup 后无孤儿进程，却没有说明这是 core action、Adapter runner 保证还是人工后置检查。

**影响**

项目可以验证“没有孤儿进程”，但无法知道是否还应等待一个 core-signed cleanup action；Action enum、Controller 和实际 lifecycle 的支持范围不一致。

**处理建议**

- 明确选择 core-signed cleanup 或 host/Adapter runner 后置责任。
- 如果暂不由 core 签发，从 Controller 可执行 action 列表中移除，并在指南写清 cleanup 证据由谁生成。
- 为每个公开 ActionKind 建立“声明、签发条件、执行者、结果状态转换、失败恢复、测试证据”矩阵。

**验收标准**

- cleanup 的责任方、触发时机、幂等语义和孤儿进程验收有唯一、无冲突的合同。
- 如果声明为 core-signed action，必须有从公共 Gateway lifecycle 触发的 E2E；如果声明为 runner 责任，Gateway/Controller 不得要求等待该 action。

### AWT-ADOPT-004：node role 与 binding role 的关系未定义

**证据**

- [Gateway](../../internal/gateway/gateway.go) 第 356–389 行保存 `WorkflowNode.Role`。
- 第 392–421 行的 `RunPreflight` 只将 request binding 交给 policy，没有校验 `node.Role == binding.Role`，也没有拒绝空 node role。
- 第 576 行派发提示展示的是 binding role，因此一个声明为 `reviewer` 的 node 可以实际按另一个已注册 role 派发。

**影响**

如果 `WorkflowNode.Role` 预期是准入字段，当前实现会削弱“明确 node + 明确 role + 完整 binding”的显式 admission；如果它只用于展示，则当前 model 和指南没有说明这一点，容易让消费项目误把它当作安全约束。

**处理建议**

先确定一个通用合同，再按选择实现：

1. 如果 role 属于 node admission：`proposeNode` 要求非空 role，`runPreflight` 校验 node role 与 binding role 完全一致；retry/fallback 改变角色时创建新 revision/node。
2. 如果 role 只属于 binding：删除或降级 `WorkflowNode.Role`，并让指南明确 node kind 承载语义、实际角色只以 approved binding 为准。

具体的 `reviewer`、`explorer` 等角色名称仍由消费项目定义，不应在共享核心形成闭合枚举。

**验收标准**

- 选定合同在 model、guide、Gateway 和测试中只有一种解释。
- 若 role 属于 admission，空 role、role mismatch 和 fallback role change 均 fail closed；若不属于，则 node 不再暴露一个看似权威但不生效的 role 字段。
- snapshot、dispatch prompt 和 completion evidence 不产生互相冲突的角色记录。

### AWT-ADOPT-005：`--version` 无法证明当前验收基线

**证据**

- [CLI main](../../cmd/agent-workflow-core/main.go) 第 16 行仍固定为 `0.1.0-wave1`。
- 项目接入指南要求固定 Wave 4 acceptance commit，并把 `--version` 作为首次接入检查项。

**影响**

操作者无法通过 binary 自证它是否对应 `130a901` 或后续修复版本，也无法核对 core 与 Controller Skill 是否同源。

**处理建议**

- 定义正式版本格式，至少包含 semantic/release version、protocol major、maturity 和 commit/build revision。
- 构建时通过 Go build info 或 `-ldflags` 注入 commit；dirty build 必须可识别。
- adoption lock 使用 commit/release 作为权威，`--version` 输出用于运行时核验，而不是继续依赖人工猜测 binary 来源。

**验收标准**

- `--version` 能区分 `130a901`、当前修复提交和 dirty build。
- 构建测试验证输出不再包含 `wave1` 旧标识。
- 安装/运行记录能核对 pinned commit 与 binary revision；若选择实现强 runtime handshake，再由 Controller preflight 自动比较。

### AWT-ADOPT-006：envelope validation 的命名与静态 schema 漂移

**证据**

- [CLI main](../../cmd/agent-workflow-core/main.go) 第 35–45 行的 `--validate-request` 按当前 protocol 包设计只调用 envelope decoder，成功时却返回容易被理解为完整校验的 `protocol_valid`；operation-specific payload 由 Gateway dispatch 校验。
- [request.schema.json](../../protocol/v1/request.schema.json) 的 operation enum 缺少 runtime 已支持的 `prepareRetryAttempt` 和 `prepareFallbackAttempt`。
- Gateway runtime 已在 [gateway.go](../../internal/gateway/gateway.go) 第 267–278 行接收这两个操作。
- [架构说明](../architecture/multi-agent-orchestration-architecture.md) 已公开承认静态 schema 缺少这两个 advanced operation，因此这是已知但尚未收口的协议漂移，不是新发现的隐藏 runtime bug。

**影响**

- 一个 envelope 合法但 payload 缺字段或字段错误的 request 会被报告为 `protocol_valid`，若指南不解释两层校验，使用者可能误以为整个 request 已可执行。
- 同一个 protocol v1 同时存在“runtime 支持”和“静态 schema 不支持”两种答案。

**处理建议**

1. 同步静态 schema 与 runtime operation registry，或者把当前 schema 明确重命名为只覆盖基础操作的子集；不要继续用同一个“protocol v1 request schema”名称表达不完整集合。
2. 明确 `--validate-request` 的 envelope-only 语义并返回 `envelope_valid`；如果未来承诺完整 request validation，再另行增加 operation-specific payload validation。
3. operation registry、schema enum、Gateway dispatch 和文档表应由单一源生成或通过测试强制一致。

**验收标准**

- envelope conformance 独立验证版本、request ID、operation、payload object、unknown envelope field 和 trailing JSON。
- Gateway payload conformance 独立验证各 operation 的必填字段、类型和 unknown payload field；只有选择实现深度 validate 时，才要求 `--validate-request` 与 dispatch 对 payload 得出相同结果。
- 所有 runtime operation 都出现在完整 schema/conformance matrix 中；若保留基础子集 schema，名称和文档必须明确其覆盖范围。
- CI 自动比较 runtime registry 与静态 schema，新增操作时不能只改一侧。

### AWT-ADOPT-007：policy JSON 未严格拒绝未知字段

**证据**

- [policy loader](../../internal/policy/policy.go) 第 309–318 行使用普通 `json.Unmarshal`。
- protocol request 和 operation payload 已使用 `DisallowUnknownFields`，policy 的严格性反而更弱。

**影响**

`retry_mode`、`goal_enabled`、`mcp_isolation` 或 binding 字段拼写错误时，配置可能被静默忽略并落到默认值；即使部分默认值 fail closed，也会造成操作者误判当前策略。

**处理建议**

- 使用 `json.Decoder` + `DisallowUnknownFields`，同时保持当前 `json.Unmarshal` 已具备的 trailing JSON 拒绝行为。
- 对顶层 policy 和 nested binding 都做严格解析。
- 错误信息包含 JSON path/字段名，便于本机 policy 修复。

**验收标准**

- 顶层未知键、binding 未知键和拼错的安全字段全部拒绝；trailing JSON 继续拒绝。
- 当前合法 policy fixtures 保持通过。
- 指南给出的 policy 模板经过同一 loader 测试。

### AWT-ADOPT-008：one-shot CLI 缺少跨进程并发保护

**证据**

- [Gateway](../../internal/gateway/gateway.go) 第 198–217 行和 [Store](../../internal/storage/store.go) 第 43–65 行使用的都是进程内 `sync.Mutex`。
- `agent-workflow-core --gateway` 每个 request 启动一个新进程；不同进程不会共享这些 mutex。
- 指南要求多个步骤复用同一个 state-dir，但没有强制 single Controller，也没有跨进程 task/state lock。
- 本轮独立公共 CLI 复现同时启动 8 个进程提交完全相同的 `createTask` request：最终落盘 8 个 Task，5 个进程成功并返回 5 个不同 `task_id`，3 个进程因 receipt atomic rename `Access is denied` 失败，最终只有 1 个 receipt。

**影响**

两个 Controller 或误触发的并发 request 可能同时读取同一 snapshot，再各自覆盖写入，造成 event、action、Attempt 或 request receipt 丢失。原子替换只能避免半文件，不能避免 lost update。

**处理建议**

当前 `experimental` 阶段至少选择一种明确策略：

1. 先用 state-dir 级 OS lock 全局串行，接受不同 Task 暂时不能并行；或
2. 使用 task/request 分粒度 OS lock，允许不同 Task 并行；或
3. 建立持久 Controller lease/epoch，非 owner request fail closed。

在实现前，指南必须明确“同一 state-dir 只允许一个串行 Controller”，并把并发调用列为 unsupported，而不是让使用者自行推断。

**验收标准**

- 两个进程并发操作同一 Task 时，要么严格串行，要么只有一个获得 authority；不得出现 lost update。
- 只有选择 task/request 分粒度方案时，才要求不同 Task 可并行；若首版选择 state-dir 粗锁，指南必须明确该限制。
- 进程崩溃后的 stale lock/lease 有安全恢复路径。

### AWT-ADOPT-009：状态变更与 request receipt 不具备原子幂等性

**证据**

- [Gateway.Handle](../../internal/gateway/gateway.go) 第 227–235 行先执行 dispatch，领域方法会保存 snapshot/event，随后才写 request receipt。
- 如果状态写入已成功而 receipt 写入失败，客户端会得到进程错误；相同 request ID 重试时找不到 receipt，会再次 dispatch。
- 指南把“相同 request ID 加相同内容可以安全重放”描述为无条件性质。
- AWT-ADOPT-008 的 8 进程复现同时证明相同 request ID 在跨进程竞争下可以生成多个 Task 和多个不同成功响应。

**影响**

在 receipt 路径故障或进程恰好中断时，`createTask`、`proposeNode`、`prepareAttempt` 等已生效操作可能重复执行，破坏 request-level exactly-once/at-most-once 预期。

**处理建议**

- 为 request 建立持久的 `pending → completed/failed/outcome_unknown` journal，并把 request ID 关联到产生的 event/state mutation。
- 如果无法在当前文件存储中事务化，重放遇到 pending/unknown 时必须 fail closed 并要求恢复，不能再次 dispatch。
- 明确业务错误是否保存 receipt；文档区分“成功结果重放”“确定失败重放”和“结果未知”。

**验收标准**

- fault injection 在 state save 成功、receipt save 前中断；重放不得创建第二个 Task/Node/Attempt/action。
- 相同 ID/相同 payload 返回同一 durable outcome；相同 ID/不同 payload 返回 `request_id_reused`。
- pending/unknown request 有可审计恢复状态。

### AWT-ADOPT-010：state 缺少版本化与升级合同

**证据**

- [storage State](../../internal/storage/store.go) 第 24–34 行没有 `schema_version`、writer version 或 migration metadata。
- 接入指南要求项目固定 toolkit 版本，但没有规定升级时旧 state-dir 的兼容、迁移、只读检查或隔离命名规则。

**影响**

后续 model/schema 演进时，新 binary 可能在没有显式判断的情况下读取并覆盖旧 snapshot；消费项目也无法机器判定某个 state-dir 属于哪个 core contract。

**处理建议**

- snapshot 增加 storage schema version 和 writer build/protocol 信息。
- 在 migration framework 出现前，采用简单的 fail-closed 策略：新版本检测到未知/不兼容 schema 时拒绝写入，并要求新 state-dir。
- 指南给出升级、回滚和保留旧 evidence 的标准步骤。

**验收标准**

- 当前 schema 能被明确识别。
- 未知新版本和不兼容旧版本均拒绝覆盖。
- 升级 smoke 使用新的隔离 state-dir，旧 evidence 保持可读且不被改写。

### AWT-ADOPT-011：adoption guide 缺少发布级 conformance 门禁

**证据**

- Wave 4 `experimental` 验收提交是 `130a901`。
- 当前 425 行项目接入指南由后续 docs-only commit `e69860b` 新增；之前的 Wave 4 独立 review 和测试没有覆盖该指南。
- AWT-ADOPT-001、003、005、006 都属于“已有局部实现或测试，但新指南对外组合后发生漂移”的问题。

**影响**

仅审查 core、Adapter 和局部合同，无法保证对外公布的项目接入流程可执行；后续文档更新仍可能再次引入无法运行的 request 或超出实现的能力声明。

**处理建议**

- 从指南示例生成或维护机器可执行 fixture，使用真实 binary、临时 state-dir 和 sample policy 跑公共 protocol E2E。
- 建立声明能力矩阵：每一项公开能力都链接 public-path test、真实 smoke 或明确的 `unsupported/experimental` 状态。
- adoption guide 发生实质修改时，必须重新执行独立 reviewer，而不是沿用早于该文档的 Wave 4 verdict。
- 真实 Provider smoke 保持 opt-in；公共 CLI/fake host conformance 必须进入普通 CI。

**验收标准**

- 指南 happy path 在 CI 中可执行。
- 指南中的 operation、错误码、ActionKind、policy 字段和 version 命令都由测试核对。
- 新 acceptance 记录包含指南 commit、测试输出和独立 review verdict。

## 5. 不应误判为本轮上游缺陷的事项

以下内容应保持为已声明边界，不能为了“接入完整”而混入本轮修复：

- 当前没有 dashboard、daemon、跨机器队列或生产 SLA。
- 当前没有统一外部 Cursor/Claude/Codex CLI executor command；消费项目不能 import `internal/adapters/*`。
- Codex App Native 仍由宿主 Controller 执行原生 subagent action，不能用 shell-launched Codex CLI 冒充。
- 当前成熟度只覆盖受控只读路径，不覆盖业务代码自动修改、写入型 Agent 或无人工检查点长链。
- 领域目标、Prompt、artifact 路径、required sections 和业务验收规则属于消费仓库。
- Gateway 的通用 `approve` 只接受本次 Agent artifact，不应自动改变消费项目的业务 review/status。
- 消费仓库根 `AGENTS.md` 的历史 CCB 配置清理属于消费仓库治理，不应写回共享核心。

## 6. 推荐处理顺序

### 阶段 U0：恢复最小接入合同

1. 修复 AWT-ADOPT-001 的 approve 示例。
2. 对 AWT-ADOPT-003A 的 `send` 和 AWT-ADOPT-003B 的 `cleanup` 分别作出“实现或明确移除”的合同决定。
3. 修复 AWT-ADOPT-005、006，使 version、runtime、schema 和指南一致。
4. 完成 AWT-ADOPT-002 的最小安装/链接与来源记录路径。
5. 建立 AWT-ADOPT-011 的可执行指南测试。

完成条件：指南描述的单 Controller、Codex App Native、只读 reviewer、Markdown artifact、人工 approve 路径可以从干净环境重复跑通。

### 阶段 U1：形成可消费的发行单元

1. 按需要加强 AWT-ADOPT-002 的自动 hash/lock/runtime handshake；最小来源记录已经在 U0 完成。
2. 完成 AWT-ADOPT-004 的 role/binding 合同决策及相应实现。
3. 完成 AWT-ADOPT-007 的 strict policy parsing。
4. 增加一个不包含领域业务规则的 sample consumer fixture。

完成条件：消费仓库只需提供领域 node/Prompt/artifact contract，不需要复制 Gateway 状态机，也不会意外加载旧 Controller Skill。

### 阶段 U2：补齐状态耐久性

1. 完成 AWT-ADOPT-008、009 的跨进程互斥和 request journal/fault recovery。
2. 完成 AWT-ADOPT-010 的 state schema/version 和升级隔离合同。

完成条件：并发、receipt 中断和版本升级测试均不会产生静默 lost update、重复派发或旧 state 覆盖。

U2 可以在首个受控试点后完成，但延期必须同时满足：同一 state-dir 仅有一个串行 Controller、每个 framework 版本使用独立 state-dir、request outcome 不确定时禁止自动重放，并在接入记录中明确这些限制。

## 7. 建议验证矩阵

| 验证项 | 最低要求 |
| --- | --- |
| Guide happy path | 从文档 fixture 通过公共 CLI 完成 create → propose → preflight → attempt → action → completion → approve |
| Action reachability | 每个公开 ActionKind 有 Gateway 签发 E2E，或明确标记 unsupported |
| Approval matrix | 普通 approve、fallback approval、retry、revision、contamination release、abort 各有正反测试 |
| Distribution | clean sample consumer 能发现 pinned Skill 并核对来源；若实现 runtime handshake，再自动阻断 core/Skill mismatch |
| Policy strictness | 顶层/nested unknown field 和拼错安全键均拒绝，trailing JSON 保持拒绝 |
| Protocol consistency | runtime operation registry、schema、validate 命令和文档一致 |
| Cross-process concurrency | 同 Task 并发进程无 lost update；不同 Task 是否并行取决于选定锁粒度并与文档一致 |
| Request durability | state/receipt fault injection 后不重复创建 Task/Node/Attempt/action |
| Upgrade | 未知 state schema fail closed，旧 evidence 不被新 binary 改写 |
| Independent review | reviewer 检查实际 diff、公共入口、测试和证据，不只读取实现者摘要 |

## 8. 上游修复后的交付条件

建议形成一个新的、可固定的 acceptance commit，并同时提供：

- 修正后的项目接入指南；
- core/Controller Skill 版本与来源核验方式；
- sample consumer 与可执行 guide fixture；
- `go test -count=1 ./...`、`go vet ./...`、`go build ./...`、`git diff --check` 结果；
- 一次真实 Codex App Native 只读 smoke；
- 一份独立 review 和 finding closure；
- 未完成项的明确 maturity、unsupported 行为和后续 milestone，不把 deferred 项误报为已实现。

领域仓库现在即可编写 `AGENTS.md`、sidecar managed policy 和 Workflow node contract 的设计或草案；在 U0/U1 完成并形成新 acceptance commit 前，不应正式启用 framework runtime、宣称接入完成或扩大到写入型授权。U2 若延期，必须采用上一节列出的受控限制。

## 9. Adoption hardening closure

以下结果以当前 worktree 的实际实现和测试为准，不回写原始 finding 的历史证据。最终 command output 与 Cursor verdict 记录在 [`AWT-adoption-hardening.md`](../../tests/orchestration/evidence/2026-08-11/AWT-adoption-hardening.md) 和后续独立评审报告。

| ID | 处理结果 | 实现/文档 | 自动化证据 | 状态 |
| --- | --- | --- | --- | --- |
| AWT-ADOPT-001 | 普通成功 `approve` 不再携带 `attempt_id`；成功 Attempt 的 attempt-bound approve 继续 fail closed；fallback approval 保持独立语义 | `docs/guides/multi-agent-orchestration-project-adoption.md`、`internal/gateway/gateway.go` | `TestFakeReadOnlyWorkflowReachesHumanCheckpoint`、`TestProjectAdoptionGuideHappyPathAndDeliveryFailureCheckpoint`、`TestFallbackApprovalIsAttemptBoundAndDoesNotReleaseCheckpoint` | 已关闭 |
| AWT-ADOPT-002 | 支持从 pinned checkout `Copy`/`SymbolicLink` 到消费仓库 `.agents/skills`，receipt 核验 checkout、core revision/dirty/hash 与 Skill source/target/tree hash | `tools/orchestration/install_controller_skill.ps1`、Controller Skill、接入指南 | `TestControllerSkillInstallerCreatesVerifiableReceipt`；宿主发现需安装后新建 Codex 任务，保留为明确人工激活检查 | 已关闭 |
| AWT-ADOPT-003A | Gateway v1 明确不实现 in-place delivery `send`；`delivery_missing` 进入人工 checkpoint，仅支持 `request_revision` 或 `abort` | `internal/gateway/gateway.go`、Controller Skill、Native contract、接入/架构文档 | `TestGatewayRejectsReservedAdapterLifecycleActions`、`TestRequiredArtifactControlsCompletion`、`TestProjectAdoptionGuideHappyPathAndDeliveryFailureCheckpoint` | 已关闭（明确 unsupported） |
| AWT-ADOPT-003B | `cleanup` 明确为 Native host/Adapter runner `finally` 的幂等责任，不是 Gateway HostActionResult | Controller Skill、Native contract、接入/架构文档 | reserved-action test；既有各 Adapter cleanup/process-tree tests | 已关闭（责任归一） |
| AWT-ADOPT-004 | node role 是 admission 数据：非空，且 preflight 时与 binding role 完全一致；role 值仍是开放字符串 | `internal/gateway/gateway.go`、接入/架构文档 | `TestWorkflowNodeRoleIsRequiredAndMustMatchBinding` | 已关闭 |
| AWT-ADOPT-005 | CLI 版本更新为 `0.2.0`，输出 protocol/maturity/VCS revision/dirty；installer 将其与 pinned checkout 机器核对 | `cmd/agent-workflow-core/main.go`、installer、接入指南 | `TestPublishedVersionAndEnvelopeValidationAreExplicit`、installer smoke | 已关闭 |
| AWT-ADOPT-006 | static schema 已包含 retry/fallback operation；`--validate-request` 改为明确的 envelope-only `envelope_valid`；runtime/schema 集合由测试锁定 | `protocol/v1/request.schema.json`、CLI、架构/指南 | `TestRuntimeOperationsMatchPublishedRequestSchema`、protocol envelope tests、`TestPublishedVersionAndEnvelopeValidationAreExplicit` | 已关闭 |
| AWT-ADOPT-007 | policy loader 使用 strict decoder，拒绝顶层/nested unknown field 与 trailing JSON | `internal/policy/policy.go`、接入/架构文档 | `TestLoadJSONRejectsUnknownAndTrailingConfiguration` | 已关闭 |
| AWT-ADOPT-008 | one-shot CLI 在 Store 前获取 state-dir 级 OS file lock；超时返回 `state_dir_busy`，异常退出由 OS 释放 | `internal/storage/root_lock*.go`、CLI、接入/架构文档 | `TestRootLockSerializesStateDirectoryAndReleasesCleanly`、`TestGatewayCLIConcurrentSameRequestIsSerialized`、`TestGatewayCLIReturnsStructuredBusyError` | 已关闭 |
| AWT-ADOPT-009 | request 在 dispatch 前写 `pending`，随后写 `completed/failed/outcome_unknown`；pending/unknown 重放返回 `request_outcome_unknown`，不再次 mutation | `internal/gateway/gateway.go`、`internal/storage/store.go`、接入/架构文档 | `TestPendingRequestJournalPreventsDuplicateMutationAfterOutcomeSaveFailure`、request ID idempotency test | 已关闭（fail-closed journal） |
| AWT-ADOPT-010 | snapshot 加 schema/protocol/writer metadata；未知或旧的无版本 state fail closed；升级必须使用新 state-dir | `internal/storage/store.go`、接入/架构文档 | `TestStateSchemaVersionIsRequiredAndUnknownVersionsFailClosed` | 已关闭（无 migration） |
| AWT-ADOPT-011 | 新增公共 binary adoption E2E、并发/锁、version/schema 和 clean consumer installer smoke；指南实质变更必须重跑真实独立评审 | `cmd/agent-workflow-core/adoption_test.go`、本 closure、acceptance evidence | public test suite + 真实 Native smoke + Cursor broad review/targeted re-check | 已关闭 |

### 保留边界

- 当前仍只覆盖受控只读 `experimental` 路径；不因本轮 closure 获得写入、Goal 自治、Wave 5 或生产 SLA 授权。
- 外部 Cursor/Claude/Codex CLI Adapter 仍没有统一 public executor command；消费项目首轮最小路径仍是 Codex App Native。
- state-dir OS lock 是粗粒度串行合同；不同 Task 并发需要隔离 state-dir，不宣称同目录并发吞吐。
- 当前没有 state migration，也没有 Gateway in-place delivery `send`；二者均是明确边界，不是隐藏能力。

### 真实 Native smoke 追加发现

公共测试通过后，core-signed Codex App Native reviewer 又发现并关闭了三项组合问题：

| Finding | 修复 | 证据 | 状态 |
| --- | --- | --- | --- |
| `delivery_missing` 的 completed Attempt 可接受 `retry_node`，随后无法真正 retry 且丢失 checkpoint | `recordHumanDecision` 对 completed Attempt 返回 `retry_not_allowed`，保留 revision/abort checkpoint | public adoption E2E + Native targeted re-review | 已关闭 |
| 指南把 binary 构建到 toolkit 内未忽略的 `bin/`，使 clean checkout 变 dirty 并被 installer 拒绝 | binary 改为构建到 `%LOCALAPPDATA%\agent-workflow-toolkit\dist\<commit>` | clean-checkout Native recheck | 已关闭 |
| 真实 reviewer 超过统一 5 分钟 action lease，observe 被误判 unknown | launch/cancel 保持 5 分钟，observe 改为 15 分钟并增加 lease contract test | `TestObserveActionLeaseCoversLongRunningProviderWait` + 真实 Native re-review | 已关闭 |

### 独立评审结论

- Broad review：[`2026-08-11-project-adoption-cursor-review.md`](./2026-08-11-project-adoption-cursor-review.md)，现行矩阵 AWT-ADOPT-001–010 PASS；唯一条件是避免报告覆盖自身造成的证据自引用。
- Targeted closure：[`2026-08-11-project-adoption-cursor-targeted-recheck.md`](./2026-08-11-project-adoption-cursor-targeted-recheck.md)，最终 gate `PASS`，明确 AWT-ADOPT-003A/011 可关闭，无开放 P0/P1。
- Targeted report 的 P3 仅指出 broad review 正文保留了评审启动时对旧报告的描述；保留 reviewer 内容，仅规范 Markdown trailing whitespace，以 targeted report 作为最终 closure authority。
