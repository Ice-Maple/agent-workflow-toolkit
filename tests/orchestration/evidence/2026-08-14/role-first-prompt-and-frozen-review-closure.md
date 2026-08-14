# Role-first Prompt 与 frozen reviewer closure

- Date: 2026-08-14
- Scope: Cursor multiline Prompt exactness、project Role frozen Git input、实际 development reviewer、framework 生命周期
- Invalid candidates: `d68a143`、`7184bd5`、`c77d397` 不再可 pin
- Downstream write boundary: 未修改 `E:\projects\doc-ai-toolkit`
- Secret boundary: 不保存 Provider raw stream/stdout/stderr、URL、token、authorization header 或认证信息；仅记录 normalized verdict 和 hash/bytes/status

## 闭合的问题

1. Cursor 旧实现把 multiline Prompt 拆成多个 positional；Provider 会把它们合并成单行。Windows 现在解析 Cursor 安装目录中的 native `node.exe + index.js`，完整 Prompt 作为 `--` 后唯一 positional。
2. Adapter 从真实 stream-json `user` event 提取 Provider 实收 Prompt，逐字比较并回传 SHA-256；Gateway 对 Cursor launch/observe 校验 core `dispatch_prompt_hash`，缺失不算完成，失配进入 contamination。
3. 无 Shell reviewer 由 host 用 read-only Git 命令生成专用 frozen artifact；Router、Gateway、bridge、Adapter 校验 path/root/bytes/SHA-256 和 undeclared sibling，Cursor 只通过 `--add-dir` 读取该 root。
4. 真实 review 证明 Cursor `plan` 会把结论送入 CreatePlan 而不交付业务 verdict。frozen project Role 因此在 Gateway preflight 强制 machine-local Cursor `ask + read-only` profile；technical release 不能替代 reviewer PASS。
5. zero-install/direct/single 不再静默丢弃 frozen-input receipt；Cursor resume `Send` 在返回 running 前也等待并校验 Provider Prompt evidence。

## 真实证据

`TestRealCursorPromptTransportExactness` 使用包含空行、缩进、Markdown、code fence 和 leading `--` 的 206-byte Prompt：

```text
Provider-visible Prompt SHA-256: 7b340aa557aa59bfc075db3976f60e52e89aa420bec513bdb29efd0bd8aa17ef
stream-json exact comparison: PASS
terminal final: CURSOR_PROMPT_EXACT_OK
```

`TestRealCursorFrozenInputRead` 使用独立 100372-byte frozen artifact：

```text
artifact SHA-256: 4f9257b18ba9f797c4d3e75bbe206631041a7868f4230266ad617fe0f95a1889
artifact HEAD: c77d3972219308489f7c980df860c78f9ced56c9
Cursor plan read through explicit --add-dir: PASS
```

实际 `doc-ai-toolkit-development/reviewer` Role Prompt 通过 public core/bridge 路径读取重新生成的完整 frozen diff。该 run 覆盖 owner ready、同 owner repeated launch、observe、completion、technical release 与 disable；Provider Prompt hash 与 dispatch hash 相同。首轮 normalized verdict 为 `PASS WITH P2`，明确无开放 P0/P1；它提出的两个 P2（Gateway Prompt hash contamination 负例、`VerifyFrozenInputs` 定点测试）随后均已补齐。

补齐 P2 和文档后重新生成 138143-byte artifact（SHA-256 `7c60925bdaa0883d165a3405ca4b50be24a5507439ab6ff8cc17c1a5b69bee6e`），同一实际 Role 再次执行完整生命周期。Provider Prompt hash 为 `a1acc25c10cb5f2d081a0bcff9e4d150241b67b6b2fe63f602c261dff4456c97`，normalized final 明确“无开放 P0/P1”，最终 verdict `PASS`。

最终 P2 closure artifact 为 142441 bytes（SHA-256 `9b2d03037db8ba8189ca449fe37b7bc8275de671999a01213310bd3c0f9081d2`）。首次运行在 Provider session/user evidence 出现前命中旧的 60 秒 Cursor startup timeout，Attempt 正确落为 `timed_out`，未误报技术完成。bridge owner 已在启动窗口内独立发布 ready，因此只把 Provider cold-start 预算调整为 2 分钟，未延长 owner ready 窗口；对应定点测试同时更新。使用同一冻结 artifact 重跑后，实际 reviewer 完成约 213 秒的完整生命周期，Provider Prompt hash `6c0943e70f1d0048abfb890c39de87c00a2e768e74906a14c3203cb08e8b12ec`，normalized final 明确“无开放 P0/P1”“无开放 P2”，最终 verdict `PASS`。

生产 Adapter 与 opt-in real smoke/review 现统一使用该 2 分钟 Provider startup 合同。修正后再次运行 exact Prompt smoke，206-byte Prompt 仍以 SHA-256 `7b340aa557aa59bfc075db3976f60e52e89aa420bec513bdb29efd0bd8aa17ef` 逐字通过；再次运行 frozen read smoke，Cursor 从上述 142441-byte artifact 读取到 HEAD `c77d3972219308489f7c980df860c78f9ced56c9` 并通过。

随后对 144365-byte 完整冻结 diff（SHA-256 `a59ef126b4d512e0973e6d0569f0e968fab0a29c67180e14b92308eb6974ccd3`）执行最终 reviewer 时，一次已送达 owner 的 `observe` 请求超过旧的固定 10 秒 IPC response deadline，bridge 正确返回 `outcome_unknown`，但真实 E2E 因失败清理晚于 `TempDir` 清理而留下测试 owner。修复包括：owner action 覆盖 Adapter observe 与 core result submission 的独立 30 秒 deadline（若 caller 更早取消则服从 caller）、E2E bridge command 使用 45 秒调用预算、observe polling 与 owner 的 250 ms poll interval 对齐，并在 `TempDir` 前注册 disable + PID/start-marker 精确清理。定点 owner-ready、active disable、timeout contract 及 bridge/Adapter/Gateway/orchestration tests 均通过；该失败未被当作 reviewer PASS。

上述修复后的最终冻结 diff 为 150203 bytes，SHA-256 `486ef1fedc2ecc02c6875a9f7a1bf58d50bbe45cf20e5705ba396db38bfa5eb6`。实际 `doc-ai-toolkit-development/reviewer` 再次完成 owner ready → repeated launch same owner → observe → completion → technical release → disable，Provider Prompt hash 与 dispatch hash 同为 `d0681b21be9b32de83bbefefdc2aa34c290e495740295f8a7422feca86775647`；normalized final 包含冻结 HEAD，结论为“无开放 P0 / P1 / P2”，最终 verdict `PASS`。

## 发布边界

本文件先记录实现 worktree 证据，不预写 acceptance commit。最终 candidate 只能在 clean HEAD 上重新构建并完成 full test/vet/build、Windows/Linux build、PowerShell Parser、Markdown links、installer/receipt/version、真实 Prompt exactness 和 actual reviewer frozen-input E2E 后给出。
