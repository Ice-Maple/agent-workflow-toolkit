# Wave 3 Codex App host smoke（已关闭）

- Date: 2026-08-11
- Platform: Windows, PowerShell, Store x64
- Host: Windows ChatGPT/Codex App `26.803.5235.0`
- Repository: `agent-workflow-toolkit`
- Scope: 当前 Windows Codex App 用户级 Wave 3 non-blocking Hooks
- Status: **CLOSED — supported host accepted; Wave 4 may start**

## 安全边界

原始用户级 `events.jsonl`、`state.json`、transcript 路径和 session/workspace
指纹不进入仓库。本记录只保留事件类型、时间、状态、计数和结论；工具参数、
Subagent 消息及用户私有路径均不落盘。

当前用户级配置注册以下六个事件，且不注册 `Stop`：

```text
SessionStart
PreToolUse
PreCompact
PostCompact
SubagentStart
SubagentStop
```

## 自动化回归

Command:

```powershell
python -m unittest discover -s tests/context_continuity -p 'test_*.py'
```

Result:

```text
Ran 70 tests in 15.787s
OK
```

这覆盖 Wave 3 工具分类、unknown fail-dirty、预算边界、hot/warm 注入、
Subagent 根 session 归属、completion fingerprint 重放/乱序/容量边界、并发
状态写入、无 `Stop` 默认安装和 fail-open 回归；它不替代下面的真实 App
观察。

## 采集方法与脱敏摘要

采集以 2026-08-11 11:07:26 +08 为固定 cutoff，从用户私有事件文件只选择
`timestamp`、`event`、`trigger`、`source`、`status`、`origin_status` 和
`checkpoint_seq`，并只保留 `PreCompact`、`PostCompact`、`SessionStart`、
`Seal`、`SubagentStart`、`SubagentStop` 六类事件。按时间顺序压成单行 JSON、
以 LF 连接并在末尾保留一个 LF 后，脱敏快照为：

```text
sanitized_event_lines = 29
PostCompact            = 5
PreCompact             = 5
Seal                   = 12
SessionStart            = 5
SubagentStart           = 1
SubagentStop            = 1
sanitized_sha256        = c65e4cd4d70233544a32dc8199d674443bcd79c6a0b39a2675baa6bdd56d6f20
```

原始文件在 cutoff 后继续增长，因此不记录原始文件 hash；上面的 digest 只用于
复核固定字段的脱敏快照，不承诺复现用户私有完整日志。

## 真实 dirty auto compact 链

截至 2026-08-11 11:07:26 +08，同一 App session 中观察到五次完整链路。
以下时间均为 Asia/Shanghai：

| Case | `PreCompact(auto, origin=dirty)` | `PostCompact(auto)` | `SessionStart(source=compact)` | 后续 `Seal(status=safe)` |
| --- | --- | --- | --- | --- |
| 1 | 2026-08-10 11:55:13 | 2026-08-10 11:56:23 | 2026-08-10 11:56:24 | 2026-08-10 11:57:06 |
| 2 | 2026-08-10 13:55:29 | 2026-08-10 13:56:13 | 2026-08-10 13:56:13 | 2026-08-10 13:57:00 |
| 3 | 2026-08-10 15:07:52 | 2026-08-10 15:09:27 | 2026-08-10 15:09:28 | 2026-08-10 15:11:34 |
| 4 | 2026-08-10 18:08:59 | 2026-08-10 18:10:10 | 2026-08-10 18:10:11 | 2026-08-10 18:11:10 |
| 5 | 2026-08-11 10:58:41 | 2026-08-11 10:59:32 | 2026-08-11 10:59:32 | 2026-08-11 11:00:42 |

每次链路均为：

```text
PreCompact(auto, origin_status=dirty, status=compacting)
→ PostCompact(auto, status=compacting)
→ SessionStart(source=compact, status=recovery_required, origin_status=dirty)
→ Seal(status=safe)
```

没有观察到 `PreCompact` 阻止、`TurnAborted` 或同一 compact 生命周期无限
重试。该证据证明当前 App 已加载 Hooks，并能在原生 auto compact 后进入
机械恢复和重新 seal；它不能单独证明恢复过程中没有语义重复执行。

固定 cutoff 后，当前根 task 又在 2026-08-11 12:03:16 +08 发生第六次 dirty
auto compact，并在 12:04:07 进入 `recovery_required`、12:09:51 重新 seal。
该次恢复同时暴露了下述 Subagent follow-up 归属缺陷；因为不属于上面的固定
29 行快照，不修改既有 digest。

## 真实工具与 Subagent 事件

同一 cutoff 原始源的独立 `PreToolUse.category` 聚合（不在上述 29 行 digest
范围内）包含：

```text
PreToolUse category counts:
  read_evidence = 108
  unknown       = 15
  write         = 632

SubagentStart = 1
SubagentStop  = 1
```

unknown/write 均保持 fail-dirty；read/evidence 事件被记录但不制造新的恢复
义务。Subagent 生命周期已回到 active count 0，随后 checkpoint 可以 seal。

## Wave 3 closure matrix

| Gate | Status | Evidence / required action |
| --- | --- | --- |
| 70 项机械回归 | PASS | 本记录的自动化回归 |
| 真实 `PreToolUse` 与 Subagent 事件 | PASS | 本记录的脱敏计数 |
| Subagent follow-up/replay 归属 | PASS | 修复后自动化与真实 App follow-up；见 `CC-09-subagent-followup/` |
| dirty native auto compact 与重新 seal | PASS | 五次完整链路 |
| 默认配置无 `Stop` | PASS | 当前用户级 Hook 事件集合 |
| clean 普通 turn 无 continuation 且保持 clean | PASS | 隔离 task 首轮无工具完成；最终消息唯一且 task 回到 idle；私有状态保持 clean |
| dirty 普通 turn 无 continuation 且不产生假 safe | PASS | 用户确认回复正常结束且无 checkpoint continuation；结束前后状态均为 dirty，未制造假 safe |
| clean compact 静默 | PASS | App 菜单真实 manual compact；见 `CC-10-manual-clean/` |
| manual compact safe/dirty | PASS | safe 恢复与 dirty 恢复后 seal；见 `CC-11-manual-safe/`、`CC-12-manual-dirty/` |
| Hook 错误不改变正常 turn 结果 | PASS | 用户级 `PreToolUse` 的 session-lock A-B-A fault injection；见 `CC-13-app-fail-open/` |
| 脱敏证据与计划/指南一致 | PASS | 逐 case 证据、计划和指南已同步；完整验证与独立 reviewer 均通过 |

## Closure verification

- 自动化回归：70/70 PASS（15.787s）。
- Python 编译检查：`compileall` PASS。
- 证据完整性：JSON/JSONL 解析、Markdown 相对链接、LF/末尾换行、
  trailing whitespace 和隐私扫描均 PASS。
- 差异检查：`git diff --check` PASS。
- 独立 reviewer：PASS；无 P0/P1/P2 finding。唯一 P3 措辞建议已修正，
  不改变任何 case 的 oracle 或结论。

## 用户侧 UI 观察

2026-08-11，用户明确确认用于 CC-03 的 dirty 普通 turn 正常结束，界面没有
出现 checkpoint continuation；同时确认当前 Windows Codex App 为 Wave 3
关闭范围，并授权后续使用隔离 task 完成剩余宿主 smoke。

用户提供的 PNG 截图显示该回复已正常完成，回复末尾仍是两项确认问题和
dirty-turn 观察说明，没有后续 checkpoint continuation UI。为避免提交用户
临时目录，仓库只记录附件元数据：

```text
format = PNG
width = 809
height = 689
bytes = 130904
sha256 = 68901125c5455a000f00f0a105fb93cb685395713b718d5d8b746011253820c9
```

截图本身证明 UI 观察，状态摘要证明该 turn 未被误标为 safe；两者共同构成
CC-03 dirty 变体证据。

## 隔离 clean 普通 turn

用户授权后创建 projectless 隔离 task `Wave 3 clean host smoke`，thread 标识只
保留 SHA-256 前 16 位：`2b5c36e65d02524f`。首轮明确禁止工具和文件操作，
最终消息为：

```text
W3_CLEAN_TURN_READY
```

App thread 观察为单一 completed turn、`latestToolMarker=null`、随后 task
回到 idle；用户私有状态摘要为：

```text
status = clean
checkpoint_seq = 0
active_subagents = 0
last_source = startup
events.jsonl = absent
```

这证明 clean 普通 turn 没有 checkpoint continuation，也没有被 Hook 误标为
dirty。当前 App 的正确 manual compact 入口是输入 `/` 后在菜单选择“压缩”；
直接发送文本 `/compact` 只会成为普通消息，不能计为 compact 证据。

## 隔离 manual compact 三变体

三个变体使用同一隔离 session；仓库只保留其 one-way pseudonym
`2b5c36e65d02524f`。下列时间均为 UTC：

| Case | 事件链 | 恢复结果 | Status |
| --- | --- | --- | --- |
| clean | `05:38:48.872300 PreCompact(manual)` → `05:39:05.626163 PostCompact` → `05:40:53.254299 SessionStart(compact, clean)` | 前后均为 clean、`checkpoint_seq=0`、无 emergency | PASS |
| safe | `05:45:24.325585 PreCompact(manual, safe)` → `05:45:46.557826 PostCompact` → `05:46:23.825523 SessionStart(compact, safe)` | `last_compact_result=recovered_safe` | PASS |
| dirty | `05:48:41.906722 PreCompact(manual, dirty)` → `05:48:58.378238 PostCompact` → `05:49:44.861936 SessionStart(recovery_required)` → `05:51:10.208883 Seal(safe)` | 159-token emergency manifest；marker 核验；`checkpoint_seq=2`、`sealed_after_recovery` | PASS |

逐 case 的 precondition、oracle、脱敏事件、状态和 observation 分别见
[`CC-10-manual-clean`](CC-10-manual-clean/observation.md)、
[`CC-11-manual-safe`](CC-11-manual-safe/observation.md) 和
[`CC-12-manual-dirty`](CC-12-manual-dirty/observation.md)。

## App fail-open A-B-A

正式 case 使用已加载的用户级 `PreToolUse`，不依赖项目 Hook trust。Baseline
在 `06:18:18.369187Z` 正常追加 `read_evidence`。随后以 `FileShare.None`
独占 session lock，并由独立 open 失败确认故障确实生效。

锁定期间，动态 probe 不在目标 prompt/context，却由实际只读工具返回；assistant
turn 在 18,646 ms 后 completed/idle，且无 error、continuation 或
`TurnAborted`。此时 `state`/`events` hash 与 events 长度 4,475 bytes 完全
不变，证明持久化失败没有伪造记录、同时没有阻断正常工具和 turn。

解锁后 control 在 `06:22:25.425005Z` 再次追加 `read_evidence`，events 增长至
4,715 bytes；用户级 `hooks.json` hash 前后不变，helper 与临时 probe 已清理。
当前 App chat 没有 `/hooks` 命令；App 的 Settings → Hooks UI 与 CLI slash
path 是不同入口，本 case 不依赖二者。项目 wrapper 未执行，不能算入证据。
完整脱敏材料见 [`CC-13-app-fail-open`](CC-13-app-fail-open/observation.md)。

## Subagent follow-up/replay 修复

真实 App 复用已完成 agent 执行 follow-up 时可能只发送新的 `SubagentStop`，
不再发送 `SubagentStart`。旧实现把这种合法 completion 当作 orphan unknown，
导致 seal 被错误阻塞。当前修复使用最多 64 条 completion fingerprint：exact
replay 幂等，同一已知 ID 的新 follow-up completion 继续保持 dirty 但归为
`completed`，陌生或淘汰后的 stop 仍 fail-dirty。

修复后同一 agent 通过真实 `followup_task` 返回
`W3_SUBAGENT_FOLLOWUP_OK`；事件终态 `active_subagent_count=0`、
`lifecycle_status=completed`，没有新 unknown。逐 case 脱敏证据见
[`CC-09-subagent-followup`](CC-09-subagent-followup/observation.md)。

## Conclusion

Wave 3 的实现、dirty-auto 关键链、manual clean/safe/dirty、App fail-open、
证据一致性和独立复核均已通过。Wave 3 对当前支持宿主 Windows ChatGPT/Codex
App `26.803.5235.0` Store x64 正式关闭，Wave 4 可以启动。其他机器或宿主版本
仍属于后续兼容性矩阵，不能由本次关闭结论自动外推。
