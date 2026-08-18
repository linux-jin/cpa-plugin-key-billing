# Progress Log

## Session: 2026-08-18

### Phase 1: Requirements & Discovery
- **Status:** complete
- **Started:** 2026-08-18
- Actions taken:
  - 明确同一 API Key 多计划绑定、按模型隔离额度的需求。
  - 初始化持久化计划、发现和进度文件。
  - 定位单绑定领域字段、计划绑定事务、授权/计费 pending 链路。
  - 定位 SQLite `api_keys.plan_id + cycle_*` 持久化结构。
  - 定位 UI 单选计划控件、计划编辑器互斥逻辑、管理 API `scopes` 与模型授权调用顺序。
- Files created/modified:
  - `task_plan.md`
  - `findings.md`
  - `progress.md`

### Phase 2: Design
- **Status:** complete
- Actions taken:
  - 选定 `[]PlanBinding` + 独立 Cycle 数据模型。
  - 定义计划模型范围、首个匹配优先和 Key 权限交集规则。
  - 定义 schema v3 关系表与旧列迁移。
- Files created/modified:
  - `task_plan.md`
  - `findings.md`
  - `progress.md`

### Phase 3: Implementation
- **Status:** complete
- Actions taken:
  - 准备实施领域、持久化、API 与 UI 变更。
  - 新增 `PlanBinding`、计划模型选择与模型交集/首个匹配逻辑。
  - 将授权、pending 归属和用量扣费改为单请求命中的独立计划周期。
  - 将 Key 目录、绑定/解绑/重置改为多计划，并把计划 CRUD 从超长文件拆出。
  - 新增 SQLite 多对多关系、计划模型选择表和 schema v2→v3 数据迁移。
  - 管理 API 增加按计划解绑/重置参数，管理页展示每个 Key 的多计划额度并新增计划模型选择器。
  - 将计划 handler、ID helper、SQLite Key/Plan/模型组持久化拆分到独立文件，清理本次涉及的超长 Go 文件。
- Files created/modified:
  - `internal/billing/state.go`
  - `internal/billing/plan.go`
  - `internal/billing/plan_binding.go`（新增）
  - `internal/billing/plan_models.go`（新增）
  - `internal/billing/modelaccess.go`
  - `internal/billing/enforce.go`
  - `internal/billing/account.go`
  - `internal/plugin/intercept.go`
  - `internal/plugin/admin.go`
  - `internal/plugin/management.go`
  - `internal/plugin/ui.html`
  - `internal/plugin/plan_admin.go`（新增）
  - `internal/billing/ids.go`（新增）
  - `internal/sqlite/key_state.go`（新增）
  - `internal/sqlite/plan_state.go`（新增）
  - `internal/sqlite/model_group_state.go`（新增）

### Phase 4: Testing & Verification
- **Status:** complete
- Actions taken:
  - 补充 Grok/Gemini 按模型选择计划、独立扣费、模型并集授权测试。
  - 补充 SQLite v2 legacy 周期迁移到多绑定关系的测试。
  - Go 领域层测试通过；全仓在 `CGO_ENABLED=0` 下编译通过。
  - 管理页两个内联脚本均通过 Node 语法解析。
  - 使用 Zig 提供 CGO 编译器，完整运行 billing/plugin/sqlite 全部测试并执行 go vet，最终全部通过。
  - 删除工作区内本次下载的 Go/Zig 临时工具链与缓存。
  - `internal/billing/key_plans.go`（新增）
  - `internal/billing/plan_admin.go`（新增）
  - `internal/billing/keys.go`
  - `internal/billing/admin.go`
  - `internal/sqlite/schema.go`
  - `internal/sqlite/state.go`
  - `internal/sqlite/key_plan_state.go`（新增）
  - `internal/sqlite/plan_model_state.go`（新增）
  - `internal/sqlite/migrate.go`（新增）
  - `internal/sqlite/db.go`
  - `internal/sqlite/import.go`

## Test Results
| Test | Input | Expected | Actual | Status |
|------|-------|----------|--------|--------|
| Billing 自动化测试 | `go test -timeout 60s ./internal/billing` | 全部通过 | `ok`，5.8s | pass |
| 全仓编译 | `CGO_ENABLED=0 go test -run '^$' ./...` | 所有包编译 | billing/plugin/sqlite/cmd 全部通过 | pass |
| 管理页脚本语法 | Node `new Function` 解析两个 script | 无语法错误 | `ui script syntax ok: 2` | pass |
| 全仓 CGO 测试 | `go test -timeout 60s ./...` | 全部通过 | billing/plugin/sqlite 全部 `ok` | pass |
| 静态检查 | `go vet ./...` | 无问题 | exit 0 | pass |

## Error Log
| Timestamp | Error | Attempt | Resolution |
|-----------|-------|---------|------------|
| 2026-08-18 | 两个只读子代理均被分配到不存在的 `gpt-5.6-luna` | 1 | 均未启动；主代理直接完成检索 |
| 2026-08-18 | FastCtx grep 正则转义引发 SyntaxError | 1 | 改用无词边界的简单模式 |
| 2026-08-18 | 组合花括号/排除 glob 的 grep 再次 SyntaxError | 2 | 后续拆为固定字符串检索 |
| 2026-08-18 | 沙箱内 `Invoke-WebRequest` TLS 认证失败 | 1 | 提权下载 Go 1.24.12，SHA-256 校验通过 |
| 2026-08-18 | 首次 gofmt 发现测试复合字面量少一个右花括号；默认 Go build cache 无写权限 | 1 | 修正括号，并将 Go 缓存显式放入仓库临时工具目录 |
| 2026-08-18 | 多计划额度测试的缩小额度等于单次请求成本，第二次请求按预期在入场前被阻断 | 1 | 将测试额度调为 0.003，使两次请求入场后累计超额，再验证后续阻断 |
| 2026-08-18 | 首次 Node 检查正则转义错误，第二次只截取到第一个 script 的结束位置 | 2 | 改为顺序遍历全部 `<script>` 块并逐个解析，检查通过 |
| 2026-08-18 | Zig 默认全局缓存目录无写权限 | 1 | 将 Zig global/local cache 显式放入仓库临时工具目录 |
| 2026-08-18 | 完整测试发现空周期 SQLite 往返不等价、Windows YAML 路径未转义、POSIX 权限断言不适用 | 1 | 修复周期加载；测试 YAML 使用 Go 字符串转义；Windows 明确跳过 POSIX 位检查 |
| 2026-08-18 | Windows 无法在 SQLite 仍打开时清理 TempDir，插件测试业务完成后统一在 cleanup 阶段失败并触发超时 | 1 | 调整测试 cleanup 注册顺序，确保 App/DB 先关闭再删除临时目录 |

### Phase 5: Delivery
- **Status:** complete
- Actions taken:
  - 复核运行时代码中 legacy 单计划字段仅用于迁移。
  - 复核管理页计划模型选择、多计划 Key picker 和逐计划额度展示。
  - `git diff --check` 通过；临时测试工具已清理。

## 5-Question Reboot Check
| Question | Answer |
|----------|--------|
| Where am I? | Phase 3：实现多计划绑定 |
| Where am I going? | 设计、实现、测试、交付 |
| What's the goal? | 同一 API Key 多计划绑定并按模型独立限额 |
| What have I learned? | 见 findings.md |
| What have I done? | 已初始化工作记录 |
