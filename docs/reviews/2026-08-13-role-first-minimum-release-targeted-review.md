# Role-first 最小发布 Targeted Re-check

- 日期：2026-08-13
- 范围：统一 CLI version、三 binary receipt、business-Skill-only managed entry 与上一轮三个 P1 修复
- 方式：独立只读复核
- 结论：**PASS**
- 开放 P0：**0**
- 开放 P1：**0**

## 复核结果

1. `invoke_business_skill.ps1` 已在路由前校验 business Skill 与 Role Package tree hash、registry/Role-first receipt 的 Skill/Role/Controller targets、Role-first 与 Controller receipt 的 toolkit root/commit、schema-v2 三 binary hash，并执行 canonical Controller `-Check`。校验失败不会进入 Router 或降级到 direct。
2. `TestFreshConsumerReceiptPinnedBridgeRoleFirstHandoff` 在 fresh consumer 中执行 Role-first installer，从 schema-v2 receipt 读取 core/bridge 路径，使用 receipt-pinned 路径完成 Cursor fixture launch、跨进程 observe、completion 和 technical release，不依赖 PATH 或全局 binary。
3. [接入指南](../guides/multi-agent-orchestration-project-adoption.md)与[架构文档](../architecture/multi-agent-orchestration-architecture.md)已准确限定透明入口：安装或升级后需要新建宿主任务；Controller 由 task-start Skill discovery 发现；managed entry 在宿主正常 Skill-dispatch 决策中运行；它不是 OS hook，无法拦截未发现 Controller 的宿主。
4. 三 CLI version 合同和 schema-v2 installer/receipt 的 canonical path、SHA-256、version/protocol/maturity/revision/dirty 与 fail-closed 测试继续成立。

## 验证证据

以下定向测试通过：

```text
TestRoleFirstInstallerCreatesCanonicalControllerReceipt
TestFreshConsumerBusinessSkillOnlyEntrySmoke
TestFreshConsumerBusinessSkillOnlyEntrySupportsHyphenatedSkillName
TestFreshConsumerReceiptPinnedBridgeRoleFirstHandoff
```

`git diff --check` 通过。

## 最终判定

上一轮三个 P1 均已闭合，未发现新的 P0/P1。Role-first 最小发布收口可以进入最终全量回归、acceptance commit 与 clean pinned build 验证。
