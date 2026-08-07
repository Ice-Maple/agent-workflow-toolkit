已选定“按能力恢复”。结合仓库现有约束，可先固定三条规则：

- 恢复时无法证明结果的 attempt 进入 `unknown`，不直接改写为 `failed`。
- 自动重试必须创建新 attempt，保留原 attempt 的日志、部分产物与未知结果。
- provider、session、workspace 或 authority 不匹配时进入 `contaminated`；这不是可自动重试的普通失败。

下一个关键取舍是：是否把“续接原 session”和“新建 attempt 重试”视为同一种安全能力？

- A. 分开授权：分别声明 `safe_to_resume` 与 `safe_to_retry`。例如某 CLI 可以可靠续接原 session，但任务本身不允许从头重跑。语义最准确，策略字段稍多。
- B. 共用授权：一个 `recovery_safe` 同时允许续接和重试。协议更轻，但会把“继续原上下文”和“重新执行副作用”混为一谈。
- C. 身份匹配即可续接：通过 authority/session/workspace 校验后允许自动 resume；只有新建 attempt 才要求 `safe_to_retry`。自动化程度更高，但身份一致只能证明“这是原 session”，不能证明继续执行不会重复副作用。

我倾向 A：两种操作的风险不同，而任务、attempt、完成判断已经分离，多一个独立能力不会显著增加 v1 复杂度。

你希望选择 A、B 还是 C？