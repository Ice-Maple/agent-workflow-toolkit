# Role-first real-combination targeted re-check

- Reviewer/model: Cursor Agent / cursor-grok-4.6-high
- Adapter: Cursor (`cursor`)
- Execution: read-only `plan` mode with explicit workspace trust
- Initial delivery: status `completed`, reason `terminal_and_final_delivery`, final bytes `486`
- Final delivery: status `completed`, reason `terminal_and_final_delivery`, final source `result.result`, terminal `true`, exit code `0`, final bytes `9397`
- Delivery recovery: same-session core-authorized follow-up used: `true`
- Prompt: sanitized; private prompt text is intentionally not stored in this artifact

## Rechecked evidence

只读复核当前磁盘文件，未修改、暂存、提交、reset、删除或创建文件，也未另启 Agent。未执行测试；结论来自当前源码与测试实现，而非 implementer 摘要。

P1 相关：
- `internal/bridge/executor.go`：`Execute` 在已有 receipt 上对 `launch` 先 `validateOwnerForRequest`，再按 `ActionID` 校验后直接返回 owner view；`validateOwnerForRequest` 允许 `starting`/`ready`/`running`
- `internal/bridge/owner_process.go`：registration 后发布 `ready`，再同步 `execute(launch)`，最后 `serve()`
- `internal/bridge/e2e_test.go`：`TestPublicBridgePublishesOwnerReadyBeforeSlowCursorSession` 在 Provider session 前对 public `launch` 调用两次并比对同一 `OwnerPID`
- `internal/bridge/owner.go`：`executionMu` 仍串行化 `execute`；`serve()` 仍在 launch 之后
- `internal/bridge/runtime_test.go`：`validateOwnerForRequest` 拒绝 `terminal` 为 `stale_owner`
- `internal/bridge/testdata/provider/main.go`：`fixture-slow-model` 在 session 前 sleep 2s
- `internal/bridge/cursor_real_e2e_test.go`：真实路径同样设置 `expectReadyBeforeProviderSession`
- `cmd/agent-workflow-core/main.go`：`shutdownStatusForOwnerError` / `shutdownStatusForAttempt` 的取消与未知态映射

P2 相关：
- `cmd/agent-workflow-core/main.go`：`disableAndConverge` 回退 map 赋给 `entry.Attempts` 后再写 per-attempt status；`latest.Attempts` 缺 key 时 `continue`
- `cmd/agent-workflow-core/adoption_test.go`：`TestDisableLegacySingleAttemptIndexDoesNotPanic` 只设顶层 `AttemptID`、不设 `Attempts`，再调 `disableAndConverge`
- `internal/storage/store.go`：`Attempts` 为 `json:"attempts,omitempty"`，反序列化可为 nil
- `internal/coordination/coordination.go`：`RegisterRunUnderLock` 原样写入，不把 `AttemptID` 规范化进 `Attempts`

回归边界：
- Project Role fail-closed：`skills/multi-agent-orchestration/invoke_project_role.ps1`、`tools/orchestration/smoke_project_role_entry.ps1`、`cmd/agent-workflow-collaboration/main.go` `--role/--task` 仍生成 `Count: 1`
- Cursor multiline Prompt：`internal/adapters/cursor/cursor.go` 在 `--` 后按非空行传递；`internal/adapters/cursor/cursor_test.go` 仍覆盖 `--force is forbidden prompt text`
- Fixture vs 真实证据：fixture slow-start 与 opt-in `TestRealCursorFrameworkBridgeE2E` 分离；README/指南/2026-08-14 证据仍声明 fixture 不计真实 Provider，且 `d68a143` 已撤销

## Finding status

### Prior P1 ready-owner follow-up launch — CLOSED

先前缺陷是：owner 在 Provider session 前进入 `ready`，但再次 public `launch` 把 `ready` 当成 `stale_owner`，且会走 IPC。

当前 `Execute` 在已有 receipt 上对重复 `launch` 的路径是：校验 owner 身份（Attempt/profile/workspace/loopback/PID+start marker），再校验 launch `ActionID`，然后返回同一 owner view，不调用 `sendOwnerAction`，也不走 `startOwnerProcess`。

```117:127:internal/bridge/executor.go
	if err := validateOwnerForRequest(receipt, request); err != nil {
		return ExecutionOutput{}, err
	}
	if action.Kind == orchestration.ActionLaunch {
		if receipt.ActionID != action.ID {
			return ExecutionOutput{}, errors.New("owner_mismatch: launch action does not match the existing owner")
		}
		// The detached owner may have registered and returned ready before the
		// Provider publishes a session. A repeated public launch poll must join
		// that owner, not attempt IPC execution or manufacture a stale owner.
		return ExecutionOutput{SchemaVersion: BridgeSchema, AttemptID: attempt.ID, ActionID: action.ID, ActionKind: action.Kind, Owner: publicOwnerReceipt(receipt)}, nil
	}
```

```184:199:internal/bridge/executor.go
func validateOwnerForRequest(receipt OwnerReceipt, request Request) error {
	if receipt.AttemptID != request.AttemptID || receipt.Provider != normalizeProvider(request.Attempt.Binding.Adapter) || receipt.ProfileID != request.Profile.ID || !strings.EqualFold(receipt.ProfileHash, request.Profile.ProfileHash) || !samePath(receipt.Workspace, request.Attempt.Binding.Workspace) {
		return errors.New("owner_mismatch: receipt does not match the frozen Attempt/profile/workspace")
	}
	if receipt.State != "starting" && receipt.State != "ready" && receipt.State != "running" {
		return fmt.Errorf("stale_owner: owner receipt is in %s state", receipt.State)
	}
	if running, err := processIdentityMatches(receipt.OwnerPID, receipt.OwnerStartMarker); err != nil || !running {
		return errors.New("stale_owner: owner PID/start marker no longer identifies the original process")
	}
```

Slow-start 回归确实在 Provider session 前调用两次 public `launch`，并检查同一 owner PID、`ready`、且 `Result == nil`（无 IPC 执行结果）：

```29:33:internal/bridge/e2e_test.go
func TestPublicBridgePublishesOwnerReadyBeforeSlowCursorSession(t *testing.T) {
	runPublicBridgeRoleFirstHandoffWithOptions(t, ProviderCursor, buildBridgeExecutable(t), buildCoreExecutable(t), roleFirstE2EOptions{
		providerModel:                    "fixture-slow-model",
		expectReadyBeforeProviderSession: true,
	})
```

```243:275:internal/bridge/e2e_test.go
	launchOutput := runBridgeCommand(t, bridgeBinary, ..., "launch")
		owner, found, err := ownerStore.Load(launch.Attempt.ID)
		if err != nil || !found || owner.State != "ready" || owner.ProviderSessionID != "" {
			t.Fatalf("slow-start owner readiness found=%t err=%v receipt=%#v", found, err, owner)
		}
		repeatedStarted := time.Now()
		repeatedOutput := runBridgeCommand(t, bridgeBinary, ..., "launch")
		if repeated.Owner.State != "ready" || repeated.Owner.OwnerPID != owner.OwnerPID || repeated.Result != nil {
			t.Fatalf("repeated launch owner = %#v result=%#v", repeated.Owner, repeated.Result)
		}
```

`runBridgeCommand`（`e2e_test.go:501-509`）对非零退出会 `Fatalf`，因此重复 `launch` 若返回 `stale_owner` 会失败。第一次 `launch` 由 `startOwnerProcess` 在 `State != "starting"` 时返回（`owner_process.go:74-76`），通常即 `ready`；`starting` 与 `ready` 走同一 join 路径。

Cancellation during provider startup 仍是如实的 `outcome_unknown` 边界，不会被误报为 `cancelled`。`runOwnerProcess` 仍在 `execute(launch)` 之后才 `serve()`（`owner_process.go:174-198`）；`executionMu` 仍会挡住并发 `cancel_host`（`owner.go:286-288`）。disable 联系 owner 失败时，`stale_owner:` 前缀映射为 `stale_owner`，其余为 `outcome_unknown`（`main.go:401-428`）。`shutdownStatusForAttempt` 只把已终结 Attempt 标成 `cancelled`；`launching` 走 default `outcome_unknown`。这不是误报取消，也不构成新的 P0/P1。

### Prior P2 legacy AttemptID-only nil map — CLOSED

先前缺陷是：AttemptID-only 回退创建了局部 map，却向仍为 nil 的 `entry.Attempts` 赋值。

当前回退在 per-attempt 写入之前把同一个 map 赋给 `entry.Attempts`；terminal Attempt 仍不会写回 `latest.Attempts`：

```272:317:cmd/agent-workflow-core/main.go
		attempts := entry.Attempts
		if len(attempts) == 0 && entry.AttemptID != "" {
			attempts = map[string]storage.ActiveAttemptEntry{entry.AttemptID: {AttemptID: entry.AttemptID, InstanceID: entry.InstanceID, OwnerPID: entry.OwnerPID, OwnerStartMarker: entry.OwnerStartMarker, OwnerEndpoint: entry.OwnerEndpoint, OwnerCapabilityHash: entry.OwnerCapabilityHash, OwnerStateDir: entry.OwnerStateDir}}
			entry.Attempts = attempts
		}
			entry.Attempts[attemptID] = attemptEntry
		for attemptID, attempted := range entry.Attempts {
			current, stillActive := latest.Attempts[attemptID]
			if !stillActive {
				continue
			}
```

回归测试构造顶层 `AttemptID`、省略 `Attempts`（Go 零值为 nil），并调用 `disableAndConverge`；无 owner 时期望 `not_contacted` 且不 panic：

```609:625:cmd/agent-workflow-core/adoption_test.go
	registerErr := manager.RegisterRunUnderLock(storage.ActiveRunEntry{
		RunID: "legacy-single-attempt", Workspace: workspace, StateDir: stateDir, GateEpoch: status.GateEpoch,
		AttemptID: launch.Attempt.ID, InstanceID: "legacy-instance", ShutdownStatus: "active",
	})
	disabled, err := disableAndConverge(manager, 5*time.Second)
	if disabled.FrameworkEnabled || len(disabled.Runs) != 1 || disabled.Runs[0].Status != "not_contacted" {
		t.Fatalf("legacy disable result = %#v", disabled)
	}
```

`RegisterRunUnderLock`（`coordination.go:500-517`）不把 `AttemptID` 填进 `Attempts`，该形状可复现原 panic。

### Regression impact — no new P0/P1

- Project Role receipt/fail-closed：route 失败仍禁止 direct fallback（`invoke_project_role.ps1:141-142`）；Prompt tamper 仍 fail-closed（`smoke_project_role_entry.ps1:95-101`）；`--role/--task` 仍是 `Count: 1`（`cmd/agent-workflow-collaboration/main.go:165-167`）
- Cursor multiline Prompt：`--` 后按非空行传递（`cursor.go:267-277`）；`--force is forbidden prompt text` 测试仍在（`cursor_test.go:44-63`）
- Fixture vs 真实 Cursor：fixture slow-start 与 opt-in `TestRealCursorFrameworkBridgeE2E`（`CURSOR_FRAMEWORK_REAL_SMOKE=1`）分开；真实路径同样在 session 前做 repeated launch（`cursor_real_e2e_test.go:13-39`）

当前无新的可操作缺陷。两条先前 finding 均已闭合，且这次修复未引入新的 P0/P1。

## Verdict

PASS
