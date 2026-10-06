# 14: 完成核验被拒绝、非成功关闭与关闭后补来结果

**What to build:** 完成核验不通过时，用户看到具体缺口，任务按缺口重新规划；任务可以非成功关闭并列出遗留的未知项；关闭后补来的账单或动作结果仍可查，而 Result 不变。

**Blocked by:** 07（完成门禁与 Result：主线打通）、12（安全重发与保持未知）

**Status:** resolved

- [x] 核验轮次持久；拒绝后说明缺口并重新请求提议
- [x] 三种关闭结果；非成功关闭可以遗留未知项，执行管理的核对责任和预算的结算责任继续（标注 G2）
- [x] 关闭后补来的结果和账单被接纳，Result 不变（标注 G10、G11）
- [x] "完成核验被拒绝后继续"作为 V4 用例之一，有故障测试

## Answer

拒绝的完成核验保存原轮次、具体缺口及唯一继续请求；原事实读取不可用时明确等待，取得可靠事实后按原身份恢复。公共流程用新的准入依据完成新动作并成功关闭，原拒绝轮次保持不变。

可信 `BeginTaskClose` 独立固定当前任务修订、输入、需求、控制代次及全部历史准入。FAILED 保留可靠未满足或未知结论；CANCELLED 引用原取消责任并等待原取消及当前关闭两套准确回执、端点封闭和实际原交接。旧关闭命令不能裁决新输入，新的可信当前关闭命令仍可使用原 CancellationRef；原 ACK 不自动关闭新依据。公开视图区分真实待交接准入、未发送 tombstone、原动作及读取不可用。MODEL、TARGET、CLOSURE、P4/P5、进行中的原出口、全部历史发送和逐发送费用保留原身份及后续责任；迟到效果、原账单和原 FILE 历史查询可完成负责方工作，固定 Result 字节不变。

实际公共用例见 [取消关闭](../../../conformance/admission/cancelled_closing_test.go)、[非成功关闭与迟到责任](../../../conformance/admission/task_closing_test.go)、[拒绝后继续](../../../conformance/admission/completion_continuation_test.go)、[三种提交故障模式](../../../conformance/fault/task_closing_test.go)和[真实 FILE 原历史查询](../../../conformance/fault/task_closing_file_test.go)。设计见 [ADR 0010](../../../docs/adr/0010-nonsuccess-task-closing.md)。

2026-10-06 的完整 `GOFLAGS=-v BUF_BASE=4664a0b9c8e1c0acd1b8007af3fd9fbf01027013 make check` 在冻结树 `fa369d4f5f84704f21b6285be9a6863861583eb4` 上实际退出 0，04:55:04.054814—06:18:01.043366 UTC，耗时 4976.959 秒；fault 包 4691.741 秒。FAILED 与 CANCELLED 各 42 个提交边界用例，每种结果各含提交前崩溃 14、提交后崩溃 14、回执丢失 14；原取消 27、拒绝继续 3、Trace 源恢复 12 个故障用例同时通过。SQL 实际 439 个 I/O 事件、2105 个镜像；启动 1504/7525；P4 51/260，实际 subjournal 打开 1、写入 18；native 14 个事件、225 个总镜像；联合 974 个 SQL 及 14 个 native 事件、989 个切点、4945 个成对镜像。全部原负对照、竞态、格式、lint、协议兼容与生成检查通过；28 个跳过项均为只能由父用例启动的 child 入口。333 个源码 SHA、全部受跟踪工作树和未跟踪清单与冻结版本一致。

永久原始日志：[ticket14-full-make-check.log](/Volumes/Data/proj/lerna-m1-context/ticket14-full-make-check.log)，SHA-256 `d89a09ede3ab0cfa55130ef2966a39c530fca428205e4fff20b70d51198576eb`；逐父用例、模式、负对照及冻结审计见 [ticket14-full-acceptance-audit.json](/Volumes/Data/proj/lerna-m1-context/ticket14-full-acceptance-audit.json)。平台为 macOS 27.0.1/26A434、Darwin 27.0.0 arm64/APFS、Go 1.27.1。当前结果基于正式 13/17/20，实际 API 18 与生产 driver 16 的后续组合由对应票及最终 23 接入验证。
