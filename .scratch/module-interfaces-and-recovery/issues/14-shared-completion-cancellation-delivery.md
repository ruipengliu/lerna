# 14: 完成与取消共用原封闭回执交付

**What to build:** 完成和取消的封闭意图用同一私有交付协议恢复，原接纳回执持久保存后才确认源工作。

**Blocked by:** None (can start immediately).

**Status:** resolved

- [x] 共享 CLAIM 围栏、读取原意图、查询原回执、按原身份投递、接纳验证及源事务确认；两条真实 typed workflow 同时接入。
- [x] 只有 NOT_FOUND 才交付；UNAVAILABLE 不解释为未提交；验证完整命令身份、摘要、负责域和原结果类型。
- [x] 完成仍重读核验、补建封闭并继续 REJECTED 的唯一请求；取消 ACK 只更新等待，不固定 Result。
- [x] 旧领取、伪造 CLAIM、自然租约到期与 context 取消保持原行为；等待不占用事务锁。
- [x] 源意图、接纳、源确认三个提交窗口的崩溃和丢回执均继续同一责任；源回执、必要源事件与原 Job 完成在同一原源事务提交，源事件失败整体回滚；无新增物理业务发送。
- [x] 私有机制只处理核心固定的 typed 工作，无注册式 handler；不修改普通准入 handoff，相关设计和故障场景通过。

## Comments

- 2026-10-07: Claimed for implementation on `codex/interfaces-14`, based on the integration branch.

## Answer

2026-10-07: 完成与取消已接入 Tasks 私有的固定类型交付、CLAIM 领取及有界恢复算法。两条流程保留各自的原命令、负责身份、封闭类型、事件和源事务；只有 `NOT_FOUND` 补交原身份，原回执通过完整接纳验证后才在同一源事务保存回执、必要事件并完成原 Job。完成核验重读、补建封闭与 `REJECTED` 唯一继续请求仍由原流程负责，取消 ACK 仍只更新等待。没有公开通用 handler 或修改普通准入交接；先更新了 Tasks 设计。

合并候选 `9fec56d6ab7a4f87439e94084c02eda43e1ca893` 已同步集成 `23d68a86882769519f1381e35d2b7766fc3df985`，包含票据 12 的 Ledger 共享算法。检查使用的完整树为 `c50f8218ac0bf945f9dff0794ae277a978a7baa8`；集成合并后、记录本 Answer 前的树与其完全一致，没有合并冲突或新源码变更。通过的准确检查为：

- `make check-code CHECK_PACKAGES='./core/tasks ./core/ledger ./core/egress ./cmd/assembly'`：imports/fmt、普通及 fault lint（0 issues）、规则检查及所选包 race 均通过。
- `go test -race -count=1 -v ./conformance/admission -run 'Test(Closure|Completion|Cancellation|RestartResumesClaimedCompletionClosure|AcceptedInputAndControlSupersedeRound|LateSupersededClosure|RejectedContinuationUsesFresh|FailedClosing|CancelledClosing)'`：通过（57.855s）。真实 typed 入口覆盖完整回执变化、不可用查询、伪造领取、源事件失败整体回滚及原工作重试、context 取消保留未过期领取；同时覆盖自然租约恢复、历史发送、迟到效果与费用、固定 Result、物理调用计数和两层共享机制的业务门禁。
- `go test -tags fault -race -count=1 -v ./conformance/fault ./conformance/admission -run 'Test(CompletionCommitBoundariesRecoverOneResultAndOriginalSeal|CancellationCommitBoundariesKeepOriginalWindowAndResponsibility|RejectedContinuationCommitRecoversOneActualRequest|(Completion|Cancellation|Task)ClosurePreservesMissingExecutionResponsibility)$'`：原 42 个提交与丢回执组合通过（67.477s），三种封闭的 6 个缺失执行历史场景通过（3.953s）。

这是私有实现抽取，原有行为基线及补充公共入口 characterization 在原实现与共享实现上均通过，不制造虚假的 RED。完整执行、基线和一次 timeout 断言修正记录见 `/tmp/lerna-module-interfaces-implementation/ticket-14.md`；上述组合日志依次为同目录的 `ticket-14-combined-check-code.log`、`ticket-14-combined-behavior.log`、`ticket-14-combined-fault.log`。保留父 spec 及三处用户未提交修改；最终完整检查由票据 18 执行。
