# 03: 运行记录收集、查询与指标使用完整依赖

**What to build:** 运行记录 Adapter 按声明完成原事件收集、索引、查询和指标，准确报告截止水位与积压。

**Blocked by:** None (can start immediately).

**Status:** resolved

- [x] 来源读取、源方确认、事件、索引及指标能力显式声明并注入，取消隐藏必需断言。
- [x] SQLite 与运行记录域 Work 通过消费方编译验证；缺失必需配置在使用前明确拒绝。
- [x] 通过业务命令产生真实源事件，再公开查询原来源、序号、回执、索引、截止水位、积压与指标。
- [x] 按 ADR 0006 保留源事务、运行记录接纳、源方确认三次提交和独立索引；崩溃与回执丢失恢复原身份。
- [x] Trace 不改变任务、效果、费用或 Result；不回填历史，不扩大字段白名单，不加入性能改造。
- [x] 相关设计更新且现有运行记录普通与故障场景通过。

## Comments

2026-10-07: Claimed for implementation on codex/interfaces-03.

## Answer

2026-10-07: Trace Store 组合来源读取与确认、事件、独立索引和指标快照的全部现有能力，业务路径改为直接使用声明方法。`New` 返回 Service 与错误，并调用 `ValidateDependencies` 检查 Store、Work 和原回报 Source，拒绝 nil 与 typed nil。生产装配在保存版本检查和恢复前处理构造失败；三个依赖及 SQLite 运行记录域 Work 均有消费方编译验证。相关设计先更新于 `docs/architecture/core/trace/README.md`，静态依赖许可仅增加精确的 `core/durable$`。

声明 Store 包装器测试先复现缺少 `AcknowledgeTraceSource` 的隐藏接口 panic，再通过公开命令与查询证明原来源序号、事件和接纳回执、独立索引积压、原截止水位、指标、恢复重放及 Task 事实保持。旧的两方法 Store 编译探针现在明确失败；原未配置服务的扩展载荷拒绝测试迁到有效生产宿主。源方提交、接收方接纳、源方确认和索引仍各自提交，业务事实与字段白名单保持。

最终验证源为 `5f7c5a147fbc55bd821c7c49bc60d37d167089ac`（实现提交 `bd755f1`，已同步当时最新集成分支）。`make check-code CHECK_PACKAGES='./core/trace ./cmd/assembly ./conformance'`、`make check-docs` 和 `git diff --check` 均通过；`go test -race -count=1 -v ./conformance/admission -run 'Trace|MetricCLI'` 通过（32.111s）；来源／接纳／确认／索引四处的三种提交故障以及原生 FILE 来源切片通过（29.153s）。基线、red/green、完整故障命令与证据见 `/tmp/lerna-module-interfaces-implementation/ticket-03.md`、`ticket-03-final-check.log`、`ticket-03-final-admission.log`、`ticket-03-final-fault.log`，编译与 red 证据同目录保留。

合并前核对待提交源码与上述最终验证源一致，无冲突，复用已有检查。本次仅增加本工单验收状态与 Answer；完整集成检查由终点验收执行，父 spec 保持原文与状态。
