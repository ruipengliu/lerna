# 12: 三种封闭共享发送关闭算法

**What to build:** 完成、取消与非成功任务关闭共用同一发送封闭算法，每条流程仍返回原类型证明并保留未完成责任。

**Blocked by:** None (can start immediately).

**Status:** resolved

- [x] 在 Execution Manager 内共享全部发送历史检查、REGISTERED 关闭、修订、派发封闭和执行工作更新，三条流程同时接入。
- [x] 各自命令身份、授权来源、清单、typed seal、事件及非成功关闭 followup 分别保留。
- [x] 三种流程各验收 P4 前、P4 后/P5 前、P5 后、迟到原交接、旧 worker、旧可能发送加最新未发送及缺失执行历史。
- [x] 封闭与 P4/P5/实际 I/O 共用原临界区；SEALED 不等于 NoSendProven；先到封闭标记永久有效。
- [x] 不释放预算、不返还授权使用、不清除旧未知、迟到观察或逐发送费用；后续执行和结算责任及固定 Result 字节保持。
- [x] 原事实、seal、Job、followup 与源事件原子提交；提交前/后故障及丢回执恢复同一身份，相关设计与测试通过。

## Comments

- 2026-10-07: Claimed for implementation on `codex/interfaces-12`, based on the integration branch.
- 2026-10-07: Resolved after merging the implementation and reusing checks against the identical integrated source tree.

## Answer

完成、取消和非成功关闭已接入 `core/ledger/send_closure.go` 的私有发送封闭算法，共享当前及全部历史发送检查、REGISTERED 关闭、原修订规则、派发封闭和 EXECUTE_OPERATION 更新。各自命令身份、来源授权、清单、typed seal、永久先到标记及来源事件继续由原流程保存；非成功关闭在原事务、原位置保留 ExecutionFollowup。三条出口入口和 P4/P5/实际 I/O 继续共用原跨进程临界区。

已有 AttemptRefs 而完整 Execution 缺失时继续保守保存可能发送和 UNKNOWN；部分执行历史缺少必需尝试、发送或引用时，在原事务返回普通内部错误 `incomplete execution history` 并回滚，不固定业务拒绝回执。真实故障用例验证原意图、源工作、执行工作、效果和费用不变，恢复精确原记录后原命令身份重试、回执重放及原类型证明通过。该处理不增加读取预检、历史重建或公共错误类别。

封闭算法没有费用或授权裁决。Completion 的新增旧 P5 发送加最新 REGISTERED 发送公共用例验证准确关闭新发送、保留旧 UNKNOWN/迟到可能性和 30 的费用责任，保留两条每发送预留各一次已消费使用及原 P4 回执；迟到原观察按原身份结算为 25，独立目标调用、效果和账单各一。预算仍只按其原不可变证据规则处理未发送预留和最终计费；共享算法不改变该规则、原后续责任或固定 Result。

受测候选 `bf15365ec9a6dd8d66b8870c59ab8e39178c7e41`（实现 `f719c30b5f863515d99f2b71f37f612fc3b3a347`，已同步集成 `60531f9020dd8db17570e4c58da2464f949da484`），树 `20a02274b54783c3cd1897fdb5909c351d36c6ba`。合并索引树与该候选完全一致，后续仅本票验收元数据变化，复用以下同源结果：

- `go test -race -count=1 ./conformance/admission -run '^Test(Completion|Cancellation|LateSupersededClosure|RestartResumesClaimedCompletionClosure|FailedClosing|CancelledClosing)'`：通过，47.232s。覆盖三类封闭窗口、实际 I/O 围栏、迟到交接、旧 worker、完整发送历史、迟到费用及固定 Result。
- `go test -race -count=1 -tags fault ./conformance/admission -run '^Test(Completion|Cancellation|Task)ClosurePreservesMissingExecutionResponsibility$'`：六个完整/部分历史缺失子场景通过，5.997s。三条路径均先真实 panic RED，再接入算法 GREEN。
- 完成的 `ledger.completion_seal` 三种提交故障切片通过，8.939s；取消及 FAILED/CANCELLED 的 `ledger.cancellation_seal`/`ledger.task_closure_seal` 三窗口、三模式切片通过，51.010s。共 30 个接收方提交边界子场景核对原证明、源回执/事件、followup、物理次数和固定 Result。
- `make check-code CHECK_PACKAGES='./core/ledger ./core/egress ./cmd/assembly'` 和 `make check-docs`：通过，普通及 fault lint 均 0 issues，格式、规则和相关 race 检查通过。合并后补做文档差异检查。

基线、各轮 RED/GREEN、全部精确故障命令及日志位于 `/tmp/lerna-module-interfaces-implementation/ticket-12.md` 和同目录 `ticket-12-*.log`。上述是开发切片；本票未运行完整存储或 intent/source-ACK 矩阵，最终完整 `make check` 由票 18 在评审修复后运行。用户三文件及父规格保持不变。
