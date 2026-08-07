## `request_timeout` 简洁设计草案

### 背景与目标

为已有 CLI 增加可选的请求超时配置，同时完全兼容旧版 `settings.yaml`：旧配置不修改即可继续使用 30 秒超时。

### 核心设计

在现有 `settings.yaml` 中增加可选字段：

```yaml
request_timeout: 60
```

沿用现有 YAML 解析与校验入口，在配置解析阶段生成有效值：

- 字段缺失：有效值为 `30` 秒。
- 字段存在：必须是大于 `0` 的 YAML 整数。
- CLI 发起请求时，使用该有效值替代当前固定的 30 秒。

### 校验规则

接受：

```yaml
request_timeout: 1
request_timeout: 60
```

拒绝：

```yaml
request_timeout: 0
request_timeout: -1
request_timeout: 1.5
request_timeout: "60"
request_timeout: true
request_timeout: null
```

校验失败时沿用现有配置错误处理方式，并明确指出 `request_timeout` 必须为正整数秒数。不额外设置上限。

### 方案决策

只有一个符合全部硬约束的方向：把默认值解析、类型校验和有效值生成集中在现有配置入口，业务请求代码只消费已校验的超时值。

不采用命令行参数、环境变量、第二配置文件或独立存储；这些都违反已确认约束。

### 验收标准

- 旧版 `settings.yaml` 未包含该字段时，请求超时仍为 30 秒。
- 配置正整数 `N` 时，请求超时为 `N` 秒。
- 非正整数值在现有配置校验阶段被拒绝。
- 不增加依赖、配置文件或存储机制。
- 除请求超时可配置外，CLI 其他行为不变。

开放问题：无。请确认这个概要；确认后再按技能要求形成最终设计文档，不进入实现。