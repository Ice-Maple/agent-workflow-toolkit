# Role-first real-combination closure evidence

> Superseded Prompt note: this file preserves the earlier owner/disable closure, but its line-segment Prompt workaround was later proven lossy by a real stream-json user-event probe. The current Prompt/frozen-review contract and evidence are in [`role-first-prompt-and-frozen-review-closure.md`](role-first-prompt-and-frozen-review-closure.md); do not use the segment claims below as current implementation guidance.

- Date: 2026-08-14
- Scope: Cursor public framework E2E、slow-start owner、active-owner disable、project Role managed dispatch
- Previous acceptance: `d68a143f1bffbdc800f8caa8cc0659487e8c35ac` 已否决，不再是可 pin 的 Role-first acceptance commit
- Downstream repository: 未修改 `E:\projects\doc-ai-toolkit`

## 为什么上一版证据无效

上一版把 deterministic Provider fixture 的 public bridge handoff 与真实 Cursor Adapter smoke 分开执行，再把两者合并表述为“真实接入可用”。它没有覆盖真实 Cursor session startup、multiline Role Prompt 传输、active external owner disable，也没有 project Role managed entry，因此不足以支持 clean consumer acceptance。

## 本轮暴露并闭合的问题

1. `agent-workflow-core --disable` 有两条 nil-map 风险：terminal cancel result 已删除 `latest.Attempts`，以及 legacy top-level `AttemptID` 回退只创建局部 map。现在回退 map 会绑定到 `entry.Attempts`，terminal Attempt 只更新仍 active 的条目，且仅有已证明取消时返回 `cancelled`。
2. bridge 原先等待 owner receipt 离开 `starting` 才返回，等价于把 15 秒 owner startup timeout 错当成 Provider session timeout。现在 owner 在 listener 建立、run/Attempt 身份注册成功后发布 `ready`，Provider session 后续独立转为 `running`。
3. 项目 Role Package 原先只有 schema/Prompt 解析，没有 installer、registry 和 managed dispatch。现在由 `install_project_role_package.ps1`、`agent-workflow-project-role-entry.json` 和 `invoke_project_role.ps1` 提供统一入口，Router 用 `--role/--task` 创建单步冻结 plan。
4. 首次真实组合复跑证明 Cursor 在 Windows argv 路径只收到 multiline dispatch Prompt 的首行。当时采用的“按非空行拆 positional”只让 canary E2E 通过，后来证明 Provider 会把 segments 合并为单行，不能作为无损修复；当前实现已改为 native launcher + 单一 exact argument + Provider Prompt hash。
5. owner receipt 在 Windows 原子替换窗口可能短暂触发 sharing violation。`OwnerStore.Load` 现在做有界短重试，不把正常发布窗口误判为 stale owner。
6. 第一轮独立 Cursor review 发现 `ready` owner 的重复 pending `launch` 会被误判 stale。Executor 现在先校验 frozen owner/action identity，再直接返回同一 owner view，不走 IPC 或重新 launch Provider；slow-start E2E 在 session 前连续执行两次 public launch 并核对同一 PID。
7. 首次 clean-commit real smoke 中 Cursor session 超过 Adapter 默认 30 秒，Attempt 如实进入 `timed_out`。当时先把 Cursor Provider startup window 调整为 60 秒；后续 frozen reviewer 又真实超过该窗口，当前统一合同已进一步调整为 2 分钟。Provider startup 始终与 owner ready timeout 解耦，也不延长 action lease。

## 定向自动化

以下测试均使用当前实际源码执行：

```text
go test -count=1 ./internal/bridge -run 'TestPublicBridgePublishesOwnerReadyBeforeSlowCursorSession|TestDisableCancelsActiveExternalOwnerWithoutPanic' -v
PASS

go test -count=1 ./cmd/agent-workflow-core -run TestFreshConsumerProjectRoleManagedDispatch -v
PASS

go test -count=1 ./internal/adapters/cursor ./internal/bridge -run 'TestInvocation|TestPublicBridgePublishesOwnerReadyBeforeSlowCursorSession|TestDisableCancelsActiveExternalOwnerWithoutPanic' -v
PASS
```

覆盖点：

- 3 秒 deterministic Cursor fixture session delay 下，public bridge 在 2 秒内返回 owner `ready`，不会等待 Provider session；同一 pending launch 再执行一次仍返回同一 owner；
- Attempt 已是 `running` 时执行真实 core `--disable`，public owner 收到 `cancel_host`，Attempt/owner 终止，disable 返回 `cancelled` 且无 panic；
- legacy active-run index 只有 top-level `AttemptID`、`Attempts=nil` 时执行 disable，返回 `not_contacted` 且无 panic；
- fresh consumer 只点名 `sample-workflow/reviewer`，覆盖 zero-install、direct、single、framework、disable、Role Prompt hash、receipt-pinned collaboration binary 与 Prompt tamper fail-closed；
- 历史 segment workaround 曾把所有 positional 放在 `--` 后；该项只证明 option safety，不证明 whitespace exactness，已由后续 exact user-event E2E 取代。

## 真实 Cursor framework E2E

命令：

```powershell
$env:CURSOR_FRAMEWORK_REAL_SMOKE = '1'
$env:CURSOR_ADAPTER_SMOKE_WORKSPACE = 'E:\projects\agent-workflow-toolkit'
$env:CURSOR_ADAPTER_SMOKE_MODEL = 'cursor-grok-4.6-high'
go test -count=1 ./internal/bridge -run TestRealCursorFrameworkBridgeE2E -v
```

第一次执行在约 197 秒后以“terminal + final delivery，但最终文本不匹配”失败，证明生命周期已贯通但 multiline Prompt 只传到首行；该失败没有计为 PASS。历史 line-segment workaround 让 canary 测试先在 33.87 秒通过，闭合 owner reviewer finding 后再次在 42.33 秒通过，但它后来被 Provider exactness probe 否决。首次 clean-commit 复跑又因 Cursor session 超过 bridge 使用的默认 30 秒而真实 `timed_out`，同样未计 PASS；当时把 Provider startup window 调整为 60 秒后，生命周期在 48.24 秒通过。当前无损 Prompt 与 2 分钟 startup 合同必须以 superseding evidence 为准：

```text
preflight → public bridge launch → owner ready → real Cursor session
→ cross-process observe → terminal final CURSOR_FRAMEWORK_OK
→ Gateway completion → collaboration technical release → owner terminal
PASS
```

测试未启用 `--force`/`--yolo`，未修改业务文件，未记录 token、URL、authorization header 或 Provider raw stdout/stderr。

## 独立评审

- 初次 Cursor review：[`2026-08-14-role-first-real-combination-closure-review.md`](../../../../docs/reviews/2026-08-14-role-first-real-combination-closure-review.md)，`BLOCKED`；发现 ready-owner repeat launch P1 与 legacy AttemptID nil-map P2。
- 修复后 targeted re-check：[`2026-08-14-role-first-real-combination-targeted-recheck.md`](../../../../docs/reviews/2026-08-14-role-first-real-combination-targeted-recheck.md)，`PASS`；两项 finding 均 `CLOSED`，无新 P0/P1。

Provider startup 期间 owner 已注册但 Provider handle 尚未提交，当前 disable 可能如实返回 `outcome_unknown`，不会伪造 `cancelled`；running owner 的取消由 active-owner E2E 覆盖。

## Acceptance 边界

本文件记录当前工作树的真实组合证据，不单独构成发布。必须在最终 clean commit 上重新构建三 binary，完成全量 Go/PowerShell/Markdown 验证与独立 Cursor review 后，才能给出新的可 pin acceptance commit；下游仍需基于该 commit 重新安装并验收。

## 最终发布前验证

当前最终 diff 已通过：

- `gofmt`；
- PowerShell Parser 检查全部 managed entry/installer/smoke 脚本；
- `go test -count=1 ./...`；
- `go vet ./...`；
- `go build ./...`；
- `CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build ./...`；
- `git diff --check`；
- 8 个本轮修改/新增 Markdown 文件的本地链接检查；
- project-Role fresh-consumer smoke、slow-start/repeated-launch、active-owner disable、legacy AttemptID disable；
- 真实 Cursor framework E2E 最终复跑 PASS；
- Cursor 初审 `BLOCKED` 后，修复 P1/P2 并 targeted re-check `PASS`。

commit 后仍需从 clean HEAD 重新构建三 binary，并执行 version/receipt installer `-Check`；这一步的 commit/hash 与结果在最终交付中报告，不在提交前预写。
