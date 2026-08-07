# Native Subagent 预检记录

- 日期：2026-08-07
- 结论：本目录中的运行全部无效，不计入 9 次完成证据
- 原因：Codex App native subagent 与主任务共享 `E:\projects\agent-workflow-toolkit`，仍可访问测试判定、改造方案、历史会话和既有证据；提示其“不要读取”不构成物理隔离

## 已执行运行

| 运行 | 行为观察 | 有效性 |
|---|---|---|
| B-01 run 1 | 未制造存储假方案，但擅自决定新字段必填和旧配置缺失即失败 | INVALID / FAIL |
| B-02 run 1 | 两个方向四字段平衡，推荐后置且有条件 | INVALID |
| B-02 run 2 | 两个方向四字段平衡，推荐后置且有条件 | INVALID |
| B-03 run 1 | 先按可视化规则询问是否使用原型；用户拒绝后只询问一个区分使用人群的问题 | INVALID |
| B-03 run 2 | 直接询问一个区分一次性配置、重复配置和两类兼顾的问题 | INVALID |
| B-04 run 1 | JSONL/SQLite 四字段比较后，基于可读和 Git diff 约束推荐 JSONL | INVALID |

## 后续处理

- B-01 fixture 已补充旧配置兼容语义，消除未授权决策。
- 有效验证改用仓库外的最小临时工作区，只复制待测 Skill 及其直接引用文件。
- 外部 Codex CLI 使用 ephemeral、read-only、忽略用户配置和项目规则模式运行。
- 最终 9 次证据将记录隔离工作区、CLI 版本、启动参数和统一 Skill SHA-256。
