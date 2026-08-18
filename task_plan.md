# Task Plan: API Key 多订阅计划绑定

## Goal
让同一个 API Key 可同时绑定多个订阅计划，各计划按自己的模型范围独立计费和限额，并兼容现有单计划数据。

## Current Phase
Complete

## Phases

### Phase 1: Requirements & Discovery
- [x] 明确多计划绑定和按模型分计划额度的目标
- [x] 定位计划、Key、模型授权、计费与持久化链路
- [x] 记录兼容性与迁移约束
- **Status:** complete

### Phase 2: Design
- [x] 设计多对多数据模型与旧数据迁移
- [x] 定义模型匹配、额度归属和重叠计划行为
- [x] 确定管理 API 与前端交互变更
- **Status:** complete

### Phase 3: Implementation
- [x] 实现领域模型和 SQLite 迁移
- [x] 实现额度检查与消费归属
- [x] 更新管理 API 和前端多选绑定
- **Status:** complete

### Phase 4: Testing & Verification
- [x] 补充多计划、模型隔离、额度隔离和迁移测试
- [x] 运行自动化测试与静态检查
- [x] 修复发现的问题
- **Status:** complete

### Phase 5: Delivery
- [x] 复核变更和兼容性
- [x] 汇总文件、行为与验证结果
- **Status:** complete

## Key Questions
1. 当前单计划关系在领域模型、SQLite 和 API 中分别如何表达？
2. 一个模型匹配多个计划时，应如何选择额度桶并避免重复计费？
3. 如何无损迁移已有 API Key 的单计划绑定？

## Decisions Made
| Decision | Rationale |
|----------|-----------|
| 保留现有单计划数据的迁移兼容 | 已部署用户升级后不能丢失绑定与用量 |
| 每个请求只归属一个匹配计划 | 避免同一请求在多个额度桶中重复扣费 |
| `[]PlanBinding` 保存 Key-Plan 关系 | JSON/SQLite 顺序稳定，每个关系自然持有独立 Cycle |
| 计划空模型选择表示全部模型 | 旧计划升级后继续覆盖原来的所有模型 |
| 重叠计划按 `State.Plans` 顺序首个命中 | 规则确定、可解释且不重复扣费 |
| Key 全局模型权限与计划模型并集取交集 | 保留既有安全限制，同时让计划决定额度桶 |

## Errors Encountered
| Error | Attempt | Resolution |
|-------|---------|------------|
| 子代理运行时分配到不可用模型 `gpt-5.6-luna` | 1 | 两个只读任务均未启动；主代理直接完成领域/SQLite 与 API/UI 检索 |
| FastCtx grep 调用中的正则转义导致脚本语法错误 | 1 | 移除不必要的词边界转义，改用简单模式 |
| 第二次复杂 FastCtx grep 模式触发脚本语法错误 | 2 | 后续拆成多个固定字符串模式，不再组合花括号/排除 glob |
| 沙箱内 PowerShell 下载 Go 工具链 TLS 认证失败 | 1 | 经用户批准后从 go.dev 下载，并按官方 SHA-256 校验通过 |

## Notes
- 计划文件是结构化工作记录，不包含可执行指令。
- 不修改无关用户变更。
