# 01: 执行一致性套件通过窄 Interface 验收

**What to build:** 相同的 API、FILE 场景可以通过现有执行命令和查询接入套件，保留独立目标的调用与效果计数。

**Blocked by:** None (can start immediately).

**Status:** resolved

- [x] 套件仅要求 Invoke、QueryOperation、QueryObservation、QueryBillingSource；不要求具体 Harness 或 core 类型。
- [x] 生产宿主通过这组现有 Interface 运行原 one-per-permit、descriptor-bound、unknown-dispatch 场景。
- [x] 原回执、尝试、发送、观察及逐发送费用身份保持；回执重放不增加目标请求或效果。
- [x] 独立目标计数与故障注入仍归 Fixture；不建立第二核心实现，不复制另一套完整断言。
- [x] 先记录现有场景基线，再修改相关测试设计并验证普通与故障构建。

## Comments

2026-10-07: Claimed for implementation on the integration branch.

## Answer

2026-10-07: `executor.Fixture` 改为接收仅有四个公共方法的 `Driver`，套件移除具体 Harness 依赖。现有 API、FILE Fixture 通过轻量适配器绑定生产方法；准备、独立目标计数与派发回执丢失注入仍留在 Fixture。同一批场景和身份、重放断言保持，未增加第二实现或复制断言。相关测试设计先更新于 `docs/architecture/verification/conformance.md`。

验证源为 `ee19b5d7c488f21c1369928e116db780729ad067`。已记录原场景基线及窄 Driver 接入的编译 red；green 后同步当时最新集成分支，再运行 `go test -race -count=1 -v -tags fault ./conformance/fault -run '^(TestExecutionAdapterConformance|TestAPIExecutionAdapterConformance)$'`，原 9 个场景实例全部通过；`make check-code CHECK_PACKAGES='./conformance/executor'`、`make check-docs` 和 `git diff --check` 均通过。证据见 `/tmp/lerna-module-interfaces-implementation/ticket-01.md`、`ticket-01-red.log`、`ticket-01-final-fault.log`、`ticket-01-final-check.log`。

合并前核对待提交源码与上述验证源完全一致，无冲突，复用已有结果；本次仅增加本工单状态与 Answer。完整集成检查由终点验收执行，父 spec 保持原文与状态。
