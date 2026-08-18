# Findings & Decisions

## Requirements
- 一个 API Key 可同时绑定多个订阅计划。
- 示例：同一 Key 同时绑定 Grok 计划和 Gemini 计划。
- Grok 计划只允许 Grok 模型并拥有独立 100 USD 限额。
- Gemini 计划只允许 Gemini 模型并拥有独立 1000 USD 限额。
- 不能把一次请求重复计入多个计划。
- 现有单计划绑定升级后必须继续有效。

## Research Findings
- 当前单绑定由 `billing.KeyState.PlanID string` 和单个 `Cycle` 表示；视图也只返回 `PlanID/PlanName/LimitUSD/SpentUSD`。
- `Authorize` 只查 `key.PlanID`，单个请求的 pending/usage 只携带一个 `CyclePlanID` 与 `CycleStartAt`，天然按一个额度桶计费。
- 创建/编辑计划会拒绝已绑定其他计划的 Key；计划编辑提交的是该计划完整 Key 集合。
- SQLite 把 `plan_id` 与唯一一组 cycle 字段直接存放在 `api_keys`，需要新增关系/周期表才能表达多绑定和每计划独立周期。
- `key_models` 是按 Key+模型累计的终身数据，不是计划周期数据；不能直接替代每计划的周期额度。
- 已有 `key_model_groups`/`key_allowed_models` 描述的是 Key 全局模型权限；用户需求更适合让模型范围属于计划，否则同一 Key 无法区分 Grok/Gemini 额度桶。
- 现有旧 JSON/SQLite 数据需要把 `api_keys.plan_id + cycle_*` 迁入新绑定记录，保留已消费金额与周期时间。
- 重新核对计划后，核心约束仍是“每请求只选一个计划额度桶”与“旧单绑定无损迁移”。
- 管理 UI 的 Key 列表使用单选 `<select name="plan_id">`；计划编辑器还会禁用“已被其他计划拥有”的 Key，必须改为复选绑定且不再互斥。
- 管理 API 已有计划创建/更新的 `scopes` 完整集合，继续沿用可自然表达多对多；Key 快捷绑定接口可扩展为增删单个关系或提交 `plan_ids`。
- 当前请求顺序是先 `AuthorizeModel`（Key 全局模型权限），再 `Authorize`（单计划额度）；多计划需要把“按请求模型选择计划”纳入同一次授权，或者先返回匹配计划 ID 再检查该计划额度。
- 计划目前没有模型选择字段；需要给 `Plan` 增加 `ModelGroupIDs/Models`，并复用现有模型组解析和模型身份匹配逻辑。
- `Authorize(scope, at)` 当前不知道请求模型；多计划选择必须把计费模型身份传入授权函数，才能确定 Grok/Gemini 对应的计划。
- `KeyState` 最小新形态可用 `Bindings map[string]Cycle`（JSON 友好但顺序不稳定）或 `[]PlanBinding{PlanID,Cycle}`（顺序确定）；计划优先级应沿 `State.Plans` 的顺序而非绑定 map 顺序。
- Pending/UsageEvent 已携带 `CyclePlanID`，无需扩展为多值：授权阶段只要为请求确定一个计划，就能继续保证只扣一个额度桶。
- `RecordUsage` 当前完成后只 settle `key.PlanID` 的周期；多绑定后应定位事件计划对应的 binding，并只更新/结算该周期。
- 现有 `settleExpiredCycle/activateCycle` 直接操作 `key.Cycle`；应改为操作 `*Cycle`，由 binding 提供对应周期，避免复制逻辑。
- 模型身份匹配的关键是 `ResolveBillingModel(upstreamModel, routeModel)` 后做大小写无关精确比较；计划模型匹配必须复用同一语义。
- `modelaccess.go` 已超过 300 行，新增计划模型逻辑应放入独立文件，避免继续扩大。
- `KeyView` 当前把一个计划汇总成单组限额字段；多计划 UI 需要 `plan_bindings[]`，每项包含计划身份、额度、周期消费、阻断状态。可暂留旧字段只做迁移兼容，但新 UI 不应依赖它们。
- `BindKey/UnbindKey/ResetCycle/ResetAllCycles/SyncKeys` 都直接操作唯一 `Cycle`，应迁移为按 plan ID 操作绑定；Key 退役/恢复时所有绑定周期都要重置但绑定关系保留。
- `keys.go` 已 456 行、`admin.go` 也超过 300 行；实现时应把多计划绑定与视图 helper 拆到新文件，避免继续增长超限文件。
- 当前 Key 表把单计划下拉、本期费用、到期时间压成一行；多计划更合适显示计划复选/摘要，并在额度列逐计划展示 `spent/limit`，重置操作需要指定计划或提供“全部重置”。
- 计划表单目前只有 Key picker，没有模型范围 picker；需新增计划模型组/单模型选择区，并让空选择语义明确（建议“匹配全部模型”，但多计划场景存在歧义，需禁止同一 Key 绑定多个全模型计划或按计划顺序首个匹配）。
- 采用确定性规则：按 `State.Plans` 列表顺序选择第一个绑定且匹配请求模型的计划；空模型选择表示匹配全部模型，用于旧计划兼容。UI 应提示重叠时靠前计划优先。
- 计划编辑器可直接复用现有 group/model picker 的数据源和 CSS，但需要独立状态集合，避免与模型分组编辑状态互相覆盖。
- 删除计划提示应改为“从这些 Key 移除此计划”，不能再说 Key 全部变为不限制，因为它可能仍绑定其他计划。
- API 路由无需新增：`/keys/bind` 可保持“增加一个绑定”，`/keys/unbind` 和 `/keys/reset` 请求体增加可选/必填 `plan_id`；计划编辑 `scopes` 仍替换该计划的完整 Key 集合。
- `admin.go` 336 行超限；与计划绑定相关的 handler 应抽到新文件，当前任务修改时顺便把相关函数迁出，降低文件体积。
- SQLite 新增三类表最清晰：`key_plan_bindings`（关系+独立周期）、`plan_model_groups`、`plan_allowed_models`；都按 position 保存稳定顺序。
- `replaceKeys` 先删除 `api_keys`，现有 Key 从表通过 ON DELETE CASCADE 清理；新 `key_plan_bindings` 应只外键到 api_keys，不外键到会整表重写的 plans。
- `Load` 当前先 keys 后 plans；可在 `loadKeys` 末尾读取新 binding 表并把旧 `plan_id/cycle_*` 迁移到内存，再由 schema 版本迁移负责一次性落盘。
- 数据库版本需从 2 升到 3；`init()` 在建表后、stamp 前执行 `INSERT ... SELECT`，把非空旧 `api_keys.plan_id/cycle_*` 拷贝到 `key_plan_bindings`。
- `plans` 整表重写时还需同步重写计划模型选择子表；因不设到 plans 的 FK，不会在 DELETE plans 时误删后续要重建的数据。
- 新安装（version 0）的 JSON seed 也要先把旧 `KeyState.PlanID/Cycle` 规范化成 bindings，否则旧 JSON 导入会丢关系。
- 请求拦截可以保持两阶段：扩展 `AuthorizeModel` 让它按“Key 全局权限 ∩ 已绑定计划模型并集”判断并返回 billing model；随后 `Authorize(scope, billingModel, now)` 选择首个匹配计划并检查其 binding cycle。
- 计划模型不匹配时仍走现有 403 `model_not_allowed`；计划额度耗尽继续走 429，并能显示命中的具体计划。
- UI 已有通用 `pickerOption`、模型目录和下线模型辅助函数；计划表单可复用这些构造器，实现“全部模型/模型分组/单模型”选择，无需新增弹窗框架。
- 授权函数签名变化后，现有 billing 测试仍调用旧的 `Authorize(scope, at)`，需要批量更新并把测试夹具迁到 `PlanBindings`。
- 非测试运行时代码仅在 SQLite 旧列读取中保留 `key.PlanID/key.Cycle`，符合迁移用途；剩余直接访问都集中在测试，需要更新断言。
- UI 初始化在脚本末尾调用 `resetPlanForm()`，因此新增计划模型 picker 必须在初始 DOM 中存在并由 reset 初始化；新增搜索监听可与现有 plan 事件放在一起。
- 运行时代码的旧 `PlanID/Cycle` 直接访问已基本只剩 SQLite v2 迁移读取；当前主要编译清理量来自测试夹具和旧 `Authorize/ResetCycle` 调用。
- 管理 API 的解绑与重置已接受可选 `plan_id`；前端计划编辑器负责维护每个计划的 Key 集合，不再禁止同一 Key 出现在多个计划中。
- `keys.go` 已降到 236 行，但仍残留未使用的 `slices` 导入；`billing/admin.go` 为 310 行，可把通用 ID helper 单独拆出。
- `plugin/admin.go` 337 行，应将计划及绑定 handler 抽到独立文件；`sqlite/state.go` 519 行，应按 Key、Plan、模型组持久化职责拆分，避免本次改动继续堆叠超长文件。
- 拆分后核心已修改文件均回到 300 行以内；SQLite 主状态文件降到 188 行，Key 持久化独立成 209 行文件。
- 旧单计划内存夹具可能先经过 `AuthorizeModel` 再进入会写迁移的 `Authorize`，因此绑定查询/管理事务仍应主动规范化 legacy 字段，不能只依赖 SQLite load/import。
- `NormalizePlanBindings` 需要比较规范化前后内容而不只是长度，否则空格修剪等同长度变化不会被标记持久化。
- 旧授权签名只存在于 17 处 billing 测试调用，旧重置签名只存在于 2 处测试调用；运行时代码签名已全部接通。
- 现有测试大量直接断言 legacy `key.Cycle/PlanID`，需要改为从 `FindPlanBinding(planID)` 读取关系周期；SQLite JSON 导入测试则应刻意保留 legacy 输入，用来证明迁移无损。
- 原有 `TestPlanBindingTransactions` 期待“不能抢占其他计划”的旧行为，必须反转为验证同一 Key 同时保留两个计划绑定。
- 插件层仍有 4 处 legacy 测试夹具/断言；管理 API 测试应改查 `KeyView.PlanBindings`，拦截测试则改写指定 binding 的周期。
- 最终运行时代码复查确认：legacy `KeyState.PlanID/Cycle` 只在内存规范化和 SQLite v2 旧列读取中保留；授权、扣费、管理事务均只操作 `PlanBindings`。
- 本机没有 GCC/Clang/MSVC/WSL，因此可完成 `CGO_ENABLED=0` 全仓编译，但 SQLite 与插件层运行测试需要另找带 C 编译器的环境执行。
- 使用官方 Zig 补齐 CGO 后，完整测试发现空周期从 SQLite 读回时被错误写入 `Cycle.PlanID`；根因是 load 构造周期时无条件赋值，现改为只装载时间/消费并由 binding 规范化决定活动周期身份。
- 插件测试的 Windows 临时路径放在 YAML 双引号内但未转义反斜杠，导致全部注册测试在进入业务逻辑前失败；改用 `strconv.Quote` 生成跨平台合法字符串。
- 数据库 `0600` 权限位断言只适用于 POSIX，Windows 明确跳过这两项；生产代码的权限收紧逻辑未弱化。
- 第二轮插件测试的业务断言未失败，但 Windows 不能删除仍打开的 SQLite 文件；测试 helper 先注册了 App shutdown、后注册 TempDir cleanup，LIFO 导致删除先于关闭。调整注册顺序，让数据库先关闭再清理目录。
- 最终 UI 复核确认计划表单同时提交 `model_groups`、`models` 和完整 `scopes`；Key picker 只提示“另有 N 个计划”而不禁用，满足多绑定编辑语义。

## Technical Decisions
| Decision | Rationale |
|----------|-----------|
| 为 Key-Plan 关系保存独立 Cycle | Grok 和 Gemini 必须拥有独立额度与重置周期 |
| 把模型组/模型选择放到 Plan 上 | 请求模型需要先确定匹配计划，再使用该计划的额度桶 |
| 保留 Key 现有全局模型权限作为附加过滤 | 避免升级时扩大既有 Key 的可用模型范围 |

## Issues Encountered
| Issue | Resolution |
|-------|------------|
| FastCtx grep 的词边界转义引发脚本语法错误 | 改用简单正则并记录到计划 |
| 第二个复杂 grep 模式再次触发语法错误 | 改为固定字符串分拆检索 |
| 子代理运行时不可用 | 主代理完成两条检索 |

## Resources
- `internal/billing`
- `internal/sqlite`
- `internal/plugin`

## Visual/Browser Findings
- 本任务暂无视觉或浏览器材料。
