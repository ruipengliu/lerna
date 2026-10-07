# 05: 任务裁决与持久工作声明完整依赖

**What to build:** 任务创建、规划、准入、模型、完成、取消与关闭的支持路径使用明确 Store 和命令能力，原决定与来源仍原子保存。

**Blocked by:** None (can start immediately).

**Status:** resolved

- [x] 审计 Tasks 与其使用的 Durable、Decisions、Work 全部支持路径，显式声明存储、历史、恢复、指标和 observed-decisions 等必需能力。
- [x] SQLite、裁决域 Work 和现有窄测试 Adapter 通过消费方 Interface 编译验证；构造与循环连接缺项可明确报告。
- [x] 通过既有公开任务、准入、模型、driver、完成、取消和关闭场景验证正常路径与原命令重放。
- [x] 确定拒绝、原决定、Job 与必需源记录保留原事务和保存点语义；暂时失败不变为永久拒绝。
- [x] CLAIM 围栏、固定 Job 类型及 owner 完成门禁保持；普通工作控制不绕过尚无回执的责任。
- [x] 不修改共同消息、持久格式或业务状态语义；更新设计并通过相关普通与故障场景。
- [x] 本票只迁移依赖声明、注入、编译检查和配置验证；规则提取、共同封闭算法、目标创建内聚及恢复编排由各对应票据交付。

## Comments

2026-10-07: Claimed for implementation on codex/interfaces-05.

## Answer

2026-10-07: Tasks Store 完整组合原来通过断言发现的 14 个存储切面与能力历史查询，覆盖任务、输入、规划、提议请求、模型位置与结果、driver 历史、准入、开始、交接、进展、三类封闭、核验、Result 和恢复扫描。Decisions 声明实际消费的 `ExecuteObserved` 与原 `QueryReceipt`；Start 三项接口声明文件使用时的逐发送、已消费凭据及执行事实能力；Confirmations 声明准入确认预检查。只把依赖断言改为声明调用，保留指标观察和原源事务逻辑。Durable Store 原已完整，未扩大其业务职责；现有 Work 接口保持固定 Job 类型与负责方领取／完成方法。

真实 SQLite 与裁决域、ContentWork、LedgerWork、TraceWork 通过消费方编译验证；Tasks Store、Decisions、Start 和 Durable Store 的声明接口包装器不提升底层隐藏方法。声明 Tasks Store 的公开 RequestProposal 先复现缺少 AllTaskClosings 的 panic，声明 Decisions 的实际准入指标场景先复现 DEPENDENCY_UNAVAILABLE，声明 Start 的真实 FILE 发布先复现同类隐藏依赖失败。修复后原提议请求、输入历史、原回执、指标重放与实际 FILE 证据保持。

Tasks 与 Durable 的 `New` 返回 Service 与错误，立即拒绝 Store 的 nil／typed nil。Tasks 的 `ValidateDependencies` 检查 Store 及 35 个必需链接，覆盖 Decisions、原工作端口、逐发送与 FILE 门禁、事实查询、确认、推理工厂和三类封闭依赖；每项 nil／typed nil 均明确报告名字。生产宿主在历史兼容性核验前完成验证，已有恢复顺序不变。共用 RequireDependencies，不增加分散反射 helper；仅精确放开 Tasks 对 core/durable 公共配置能力的导入。Tasks／Durable 设计先更新，未改变共同消息、持久格式、事实负责方和业务状态语义。

候选 `4f905600e26b8d4e35d27a4213f809f77a99558f` 已合入最新集成 `2661ea1f266bfb872d5c202aaba571b9f0c6f6c7`，保留 13 的 CreateFromGoalInTransaction 与三个私有创建 helper，以及既有 Fixed 规则。组合后的配置及公开 race 切片通过：core durable 1.813s、durable conformance 3.917s、content 2.812s、总 conformance 2.812s、sessions 3.493s、admission 19.396s。覆盖原目标／任务、声明 Adapter、构造及循环缺项、原 claim、模型重启不重发或重计费、driver 原位置、实际 FILE、单次完成、取消、关闭后迟到费用、原命令重放、通用 Job 不得擦除尚未接纳的 handoff 及主观确认的一次消费。Legacy 永久目标拒绝不产生 Task 源记录单独通过（2.203s）。

精确故障切片通过（27.571s）：旧工作者失去 claim 后不能写入、丢失领取回执恢复原 claim、准入／接收／源 ACK 提交边界、暂时内容不可用回滚后重试原身份、真实保存点溢出的接受／拒绝／暂时写失败、历史或当前未知嵌套工作和整批拒绝、原 driver 配置恢复。tasks.planning 的原事实与必需源记录在提交前／提交后／回执丢失三个窗口通过（3.865s）。因此确定拒绝与原决定、Job、来源仍按原事务和保存点处理，暂时失败未转为永久拒绝。`make check-code CHECK_PACKAGES='./core/tasks ./core/durable ./cmd/assembly ./conformance/durable'` 通过格式、普通／fault 双 lint、规则与 scoped race；文档和差异检查通过。广范围 admission 检查已停止且未计为通过，遗留子进程已清理；最终完整矩阵留给终点验收。

主集成合并无冲突。追加本 Answer 前的树为 `cc7480de0abb979be810a98afc891e83b209b95c`，与上述已测候选完全一致，逐文件差异核对也为空，故复用这些组合源的精确证据。基线、red／green 与完整命令范围见 `/tmp/lerna-module-interfaces-implementation/ticket-05.md`；组合日志为同目录 `05-merged-public-race.log`、`05-merged-fault.log`、`05-merged-source-fault.log`、`05-merged-legacy-rejection.log`、`05-merged-check-code.log`。本次只更新 05 状态、验收项和 Answer；父 spec 与三个用户原编辑不变，08 未开始。
