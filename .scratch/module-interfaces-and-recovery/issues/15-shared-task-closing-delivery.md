# 15: 非成功关闭接入共同封闭交付

**What to build:** 非成功任务关闭复用已有内部交付协议，保留原关闭证明、执行/结算后续责任和固定 Result。

**Blocked by:** 14（完成与取消共用原封闭回执交付）.

**Status:** resolved

- [x] TaskClosing 的原封闭意图、回执查询、交付与源确认迁移到 14 的私有机制，删除对应重复协议代码。
- [x] 保持关闭命令的独立范围、当前依据与准确原身份；封闭投递和固定 Result 仍由不同原阶段裁决。
- [x] 准确范围的原取消 ACK、关闭 ACK 与交接到齐之前不提前固定 CANCELLED Result。
- [x] FAILED/CANCELLED Result 固定后，迟到观察、账单、弱查询与原核对继续归原执行及预算责任，不改 Result 字节。
- [x] 普通控制不提前完成原工作，旧领取和丢回执恢复同一 intent、seal 与 followup。
- [x] 相关设计、非成功关闭公开场景与提交前/后/回执丢失故障场景通过。

## Comments

- 2026-10-07: Claimed on `codex/interfaces-15` after 14 was resolved in integration.

## Answer

2026-10-07: TaskClosing 已作为第三种固定 typed 工作复用票据 14 的私有交付、领取围栏和有界恢复机制，删除原重复协议。保留 `tasks-closing`、原命令身份、准确范围、TaskClosureSeal、源事件及原 ACK 事务；原回执查询与完整接纳验证通过后，源回执、必要事件和原 Job 同事务确认。`RecoverTaskClosures` 只交付封闭，`ProcessTaskClosings` 的最终裁决实现保持不变；准确原取消 ACK、当前关闭 ACK、原准入交接和原执行及结算后续责任仍阻止过早固定 CANCELLED Result。迟到事实、账单、弱查询及原核对继续由原负责方处理，固定 Result 字节不变。先更新了 Tasks 设计。

候选 `c34fb2557a79d78e3760d973de5d8bc68a9ec5eb` 同步了集成 `e02975d2f1b4e31122f0b94f28388d52dabb7811`，保留票据 08 的完整装配门禁及旧 setter 删除，无合并冲突。准确测试树为 `34d682a851773d5e020a8954fdd70f3aceec26e1`；集成合并后、写入本验收记录前的树与其完全相同。该组合源码全部通过：

- `make check-code CHECK_PACKAGES='./core/tasks ./cmd/assembly ./conformance/sessions'`：imports/fmt、普通及 fault lint（0 issues）、规则检查和 scoped race 通过。
- `go test -race -count=1 -v ./cmd/assembly ./conformance/sessions ./conformance/admission -run 'Test(Assembly|TasksConstructor|TasksCompleteAssembly|DeclaredTasksStore|DeclaredSession|Closure|Completion|Cancellation|FailedClosing|CancelledClosing|CLIFailedClosing|CLICancelledClosing|ProductionDriverCancellation|RestartResumesClaimedCompletionClosure|AcceptedInputAndControlSupersedeRound|LateSupersededClosure|RejectedContinuationUsesFresh)'`：装配 6.156s、Sessions 2.592s、admission 73.673s。完整构造与 nil/typed-nil 门禁、三种 typed 公共交付校验、准确范围与 ACK 等待、历史发送、原 followup、独立物理计数及固定 Result 检查通过。
- `go test -tags fault -race -count=1 -v ./conformance/fault -run 'Test(TaskClosingCommitBoundariesPreserveOriginalResponsibilities|CancelledClosingCommitBoundariesPreserveOriginalResponsibilities)/(before-p4|after-p4|after-p5)/(tasks[.]closing|ledger[.]task_closure_seal|tasks[.]task_closure_receipt)$'`：原源意图、接收方封闭、源 ACK 三窗口的提交前崩溃、提交后崩溃和丢回执共 54 个真实场景通过（97.743s）。
- `go test -tags fault -race -count=1 -v ./conformance/fault ./conformance/admission -run 'Test((Failed|Cancelled)ClosingKeepsOriginalFileHistoryQueryAndFixedResult|(Completion|Cancellation|Task)ClosurePreservesMissingExecutionResponsibility)$'`：两种真实 FILE 弱读取、历史查询、原计划与资源、独立计费及固定 Result 流程通过（19.349s）；三种封闭的 6 个缺失执行历史场景通过（7.050s）。

纯结构抽取采用原实现基线与 characterization，不制造 RED；扩展的 TaskClosing 公共校验先在原实现通过，再在共享机制通过。基线、一次测试分支 lint 修正及完整源码关系记录在 `/tmp/lerna-module-interfaces-implementation/ticket-15.md`；组合日志依次为同目录 `ticket-15-combined-check-code.log`、`ticket-15-combined-behavior-construction.log`、`ticket-15-combined-fault.log`、`ticket-15-combined-file-history.log`。仅解决本票，父 spec 和三处用户未提交修改保持不变；完整矩阵和最终项目检查由票据 18 执行。
