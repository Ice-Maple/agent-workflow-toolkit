# CCG Workflow 多 Agent 机制只读研究

## 研究对象、日期、方法与限制

- **研究对象**：`E:\projects\others\ccg-workflow-main`（CCG Workflow 源码快照）。
- **研究日期**：2026-08-06。
- **方法**：先确认仓库含 `.codegraph/`，再使用 `codegraph explore` / `codegraph node` 定位 provider、执行器、解析器与安装器；随后阅读相关源码、模板和 Go/TypeScript 测试。全程只读，未运行外部模型或修改研究对象。
- **限制**：该目录没有 `.git`，`git status` 与 `git log` 均返回 *not a git repository*，因此不能用历史验证发布过程或变更原因。本报告以当前磁盘源码和测试为准。
- **版本漂移**：运行时源码 `codeagent-wrapper/main.go:17` 和安装器 `src/utils/installer.ts:66` 都是 `5.12.0`；但 `codeagent-wrapper/CLAUDE.md:275-276` 与 `src/CLAUDE.md:129` 仍写 `5.10.0`。因此本文把源码/测试、模板/文档声明和研究判断明确区分。

证据强度：**高**表示源码或测试直接证实；**中**表示安装模板或命令协议；**低**表示仅文档声明，且可能受上述漂移影响。

## 架构和关键调用链

| 层次 | 已证实职责与调用链 | 证据 |
| --- | --- | --- |
| TypeScript 安装器 | CLI 注册后调用 `installWorkflows()`，将命令、hooks、agents、prompts、skills、规则和 wrapper 二进制安装到宿主目录；它不是运行时多模型调度器。 | 源码：`src/utils/installer.ts:1010-1093`、`src/utils/installer.ts:222-317`；高 |
| Go `codeagent-wrapper` | 解析单任务或并行输入 → 选 backend → 构造 argv / stdin → 执行进程并解析流 → 返回消息、session、日志和错误。 | 源码：`codeagent-wrapper/main.go:345-528`、`codeagent-wrapper/executor.go:811-1374`；高 |
| Provider 抽象 | `Backend` 只定义 `Name()`、`Command()`、`BuildArgs()`；registry 当前注册 Codex、Claude、Gemini、Antigravity/agy、Grok。 | 源码：`codeagent-wrapper/backend.go:11-38`、`codeagent-wrapper/config.go:69-87`；高 |
| 角色与任务协议 | 模板通过 `ROLE_FILE:` 指向角色 prompt；wrapper 读取并注入实际内容。外部模型任务使用 `<TASK>` 与 `OUTPUT` 约束。 | 源码：`codeagent-wrapper/main.go:287-293,416-420`；模板：`templates/codex/AGENTS.md:124-176`；中 |
| 项目状态 | 模板规定 `.ccg/tasks/<id>/task.json`、`context.jsonl`、`plan.md`、`review.md`；但 wrapper 只把 session 置于 `TaskResult`/stdout，不写回这些状态文件。 | 模板：`templates/codex/AGENTS.md:51-87`；源码：`codeagent-wrapper/config.go:34-66`、`codeagent-wrapper/main.go:517-520`；高 |
| 并行调度 | 自定义 `---TASK---` / `---CONTENT---` 文本被解析为 `TaskSpec`；任务按拓扑层次执行，同层 goroutine 并发；失败依赖被跳过，并可限制 worker 数。 | 源码：`codeagent-wrapper/config.go:115-200`、`codeagent-wrapper/executor.go:288-545`；测试：`codeagent-wrapper/executor_concurrent_test.go:222-239`；高 |

## 可直接借鉴

- **最小而清晰的 provider 边界**：将命令发现与 argv 构造放到 adapter 内，使用一个显式 registry；不同 CLI 差异不要散落在 workflow 中。
  证据：`codeagent-wrapper/backend.go:11-38,123-250`、`codeagent-wrapper/config.go:69-87`；源码事实，高。

- **统一任务结果契约**：`TaskResult` 集中携带退出码、消息、session ID、错误、日志位置，以及覆盖率/改动文件/测试数等可选摘要。
  证据：`codeagent-wrapper/config.go:50-66`；源码事实，高。

- **尽早捕获可恢复 session**：解析器一遇到 snake_case 或 camelCase 的 session ID 即触发回调；单任务立即写 stderr，使超时或失败也能得到真实 ID。
  证据：`codeagent-wrapper/parser.go:102-109,196-202`、`codeagent-wrapper/executor.go:1082-1097`；源码事实，高。

- **超时、取消与 Windows 进程树处理**：统一 context timeout，超时时终止进程；Windows 使用 `taskkill /T /F` 防止 Node 子进程继承 stdout 后悬挂。
  证据：`codeagent-wrapper/executor.go:979-982,1212-1257,1322-1330,1464-1521`；源码事实，高。

- **依赖分层和失败隔离**：依赖显式、同层并发、上游失败导致下游明确跳过。
  证据：`codeagent-wrapper/executor.go:288-351,354-545`；`codeagent-wrapper/executor_concurrent_test.go:222-239`；源码与测试，高。

- **流协议的容错解析**：统一处理 Codex/Claude/Gemini/Grok 流，支持 Gemini 在 JSON 前插入文本，限制单行最大 10MB，未知事件安全忽略。
  证据：`codeagent-wrapper/parser.go:57-60,180-224,334-428`；源码事实，高。

- **每任务日志与降级**：并行任务优先各自临时日志，无法创建时退回共享日志；失败时输出近期错误。
  证据：`codeagent-wrapper/executor.go:224-254,453-500`、`codeagent-wrapper/logger.go:66-109,267-336`；源码事实，高。

- **安装期模板渲染**：模型、MCP、轻量模式等占位符在安装时渲染；Gemini/Grok model flag 按所在行的 backend 判断，避免无效参数。
  证据：`src/utils/installer-template.ts:101-194`；源码事实，高。

## 简化后借鉴

- **任务协议**：保留 `id`、`workdir`、`dependencies`、`provider`、`session_id`、prompt、预期输出等字段，但应使用真正的 JSON/YAML schema。当前 `ParallelConfig` 名称看似 JSON，实际使用自定义分隔文本。
  证据：`codeagent-wrapper/config.go:28-48,115-200`；源码事实，高。研究判断：不要复用该分隔符格式。

- **角色 prompt**：可保留职责、输入、输出 schema、权限/文件边界、完成条件；可借鉴 `<TASK>`/`OUTPUT`、审查分级及文件所有权要求。不要把“所有任务一律建档并 Git 提交归档”硬编码为通用规则。
  证据：`templates/codex/AGENTS.md:51-87,214-255,286-307`；模板声明，中。后半句为研究判断。

- **worker → reviewer → fix loop**：可借鉴 Critical 阻断、Warning 可选、修复后再审的门禁语义；循环次数、自动修复授权和写权限须由主控状态机显式管理。此仓库主要靠 prompt 规定，而非引擎强制。
  证据：`templates/codex/AGENTS.md:249-255,306-307`、`templates/commands/spec-review.md:67-116`；模板声明，中。后半句为研究判断。

- **Doctor**：适合借鉴“声明式检查项 + 可执行修复建议”，但不能把 Claude 专有 hook 数量、全局路径结构当成所有 workflow 的健康定义。
  证据：`src/commands/doctor.ts:29-188`；源码事实，高。后半句为研究判断。

- **MCP**：可借鉴 registry、配置合并和不覆盖用户配置；但不应以 `~/.claude.json` 为跨宿主 workflow 的中心配置。
  证据：`src/utils/installer-template.ts:50-55,174-192`、`src/utils/installer.ts:647-679`；源码事实，高。后半句为研究判断。

## 不建议复制

- **默认全权限执行**：Claude 始终附加 `--dangerously-skip-permissions`；Codex 默认 bypass approval/sandbox 并跳过 Git repo 检查；Grok 也恒定 `--always-approve`。权限应成为 task/provider policy 的显式输入。
  证据：Claude/Grok 见 `codeagent-wrapper/backend.go:85-112,177-202`，Codex 见 `codeagent-wrapper/executor.go:758-783`；测试：`codeagent-wrapper/backend_test.go:12-58,115-137`；源码与测试，高。最后一句为研究判断。

- **Claude Code 专用的分发包袱**：`~/.claude` commands、agents、rules、hooks、skills、settings.json 注册，以及 Codex 侧全局 AGENTS.md，是安装/宿主适配层，不是通用调度内核。
  证据：`src/utils/installer.ts:222-317,647-679,1063-1079`；源码事实，高。

- **默认 SSE Web UI**：wrapper 每次任务默认启动短生命周期 WebServer；已有主控 UI/事件流时会重复管理浏览器与服务生命周期。
  证据：`codeagent-wrapper/executor.go:887-903,1065-1073`；源码事实，高。后半句为研究判断。

- **过度的任务仪式化**：对所有小任务创建目录、状态文件、归档并提交 Git，会让维护成本超过小任务本身的价值。
  证据：`templates/codex/AGENTS.md:48-90,312-320`；模板声明，中。结论为研究判断。

- **半安装继续运行**：binary 下载或 Codex files 安装有非致命路径，可能得到“命令已安装而实际不可运行”的状态。
  证据：`src/utils/installer.ts:647-679,1081-1093`；源码事实，高。

## 为未来非 Codex 主控预留接口

以下是研究判断，不构成当前 workflow 的最终方案。

1. **`ProviderAdapter`**：`id`、可执行文件发现、`buildInvocation()`、`parseStream()`、`resume()` 与 capabilities。最小参考为 `Backend`。
   证据：`codeagent-wrapper/backend.go:11-18`；源码事实，高。

2. **`Invocation`**：argv、stdin、cwd、环境白名单、超时、取消 token、输出格式、权限模式；不要让 prompt 模板直接拼 shell heredoc。
   证据：现有逻辑分散于 `codeagent-wrapper/main.go:440-454`、`codeagent-wrapper/executor.go:859-885,989-1014`；源码事实，高。

3. **`StreamEvent`**：至少覆盖 `session_started`、content、tool/progress、completed、error，并统一 session ID 命名。
   证据：`codeagent-wrapper/parser.go:71-109,196-202,226-428`；源码事实，高。

4. **`TaskSpec` / `TaskResult`**：除现有 dependency/workdir/session/provider 外，应预留 stable run ID、attempt、artifact reference、结构化 reviewer findings。
   证据：`codeagent-wrapper/config.go:34-66`；当前字段为源码事实，高；扩展字段为研究判断。

5. **`StateStore`**：由主控独立持久化 phase、provider sessions、review-loop iteration、取消/恢复点；不能依赖 provider session 或 prompt 自觉落盘。
   证据：模板要求状态文件（`templates/codex/AGENTS.md:51-87`），而 wrapper 只返回结果（`codeagent-wrapper/executor.go:1367-1374`）；源码/模板对照，高。

6. **`ProviderCapabilities`**：声明 stdin 支持、多行 argv 安全、stream 格式、cwd、resume flag 和 approval semantics。当前条件分支已经证明新增 provider 成本主要在这些差异。
   证据：`codeagent-wrapper/executor.go:862-878`、`codeagent-wrapper/backend.go:85-112,123-145,177-202,215-250`；源码事实，高。

## 已验证风险/坑

- **版本说明不可信于源码**：5.12.0 源码与 5.10.0 子模块文档并存，设计或兼容性调查不能只读 README/CLAUDE。
  证据：`codeagent-wrapper/main.go:17`、`src/utils/installer.ts:66`，对比 `codeagent-wrapper/CLAUDE.md:275-276`、`src/CLAUDE.md:129`；高。

- **并行模型参数不闭环**：`--parallel` 会消费 `--gemini-model`/`--grok-model` 后只发 warning，虽 `TaskSpec` 有模型字段，入口并未写入它们。
  证据：`codeagent-wrapper/main.go:231-250,281-294` 对比 `codeagent-wrapper/config.go:45-46`；高。

- **prompt 传输不可完全统一**：Gemini 在 Windows 需 stdin pipe；Antigravity 不读 stdin、只能 `-p`；Grok 直接 `-p`；Codex 可用 `-`。registry 加一行并不等于新增 provider 完成。
  证据：`codeagent-wrapper/executor.go:862-878`、`codeagent-wrapper/backend.go:123-145,177-202,215-250`；高。

- **工作目录与环境隔离仍是高风险边界**：当前 Gemini 使用项目 cwd，同时从 `~/.claude/settings.json` 提取 env 注入；这与历史文档描述存在演变痕迹。密钥来源和环境白名单必须明确。
  证据：`codeagent-wrapper/backend.go:42-83,236-242`、`codeagent-wrapper/executor.go:991-1014`；高。

- **成功判定依赖流解析**：若没有解析到 agent message，即便进程结束也被判失败；CLI 流格式漂移会直接影响完成判定。
  证据：`codeagent-wrapper/executor.go:1351-1357`、`codeagent-wrapper/parser.go:180-224,412-428`；高。

- **研究对象无 Git 元数据**：不能从当前目录验证提交、发布二进制或文档版本同步流程。
  证据：本次只读命令 `git -C E:\projects\others\ccg-workflow-main status` 和 `log` 均返回 *not a git repository*；高。

## 结论

最有价值的不是其 Claude Code 安装模板，而是 Go wrapper 中已被测试覆盖的 provider adapter、session 早捕获、跨 CLI 流解析、进程生命周期控制、依赖并发和每任务日志。后续研究若参考这些机制，应将宿主专属安装器、全权限默认、浏览器 SSE、模板强制归档与真实调度核心分离；本报告只提供证据和边界，不提出或落地最终 workflow 方案。
