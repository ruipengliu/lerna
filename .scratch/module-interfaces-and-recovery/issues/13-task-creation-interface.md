# 13: 初始目标通过单个 Interface 创建任务

**What to build:** 会话用一次事务内调用创建完整初始任务，用户仍得到原有条件、输入、关联及回执阶段。

**Blocked by:** None (can start immediately).

**Status:** resolved

- [x] Task orchestration 内聚创建、显式条件验证接纳与初始输入登记；Sessions 只维护原顺序与关联。
- [x] 无显式条件保持 DRAFT、BoundInputVersion=0、ACCEPTED 初始输入和 REQUIREMENTS 等待；显式条件保留原接纳与 PROCESSED 语义。
- [x] 原 goal、content、source input、AcceptedBy、输入序号和初始版本绑定准确；依赖未满足不提前创建。
- [x] 旧 SubmitGoal 保持 SUBMITTED→DECIDE_GOAL→DECIDED，SubmitInput(GOAL) 保持同步语义；永久拒绝检查在旧路径写入之前完成。
- [x] 任务、条件、输入历史、会话关联、源记录及决定加入同一原事务；失败无部分记录，相同命令重放只生成一个任务。
- [x] 关闭后同会话新目标、显式条件、依赖路由与提交前/后/丢回执场景通过，更新相关设计。

## Comments

2026-10-07: Claimed for implementation on codex/interfaces-13.

## Answer

2026-10-07: Task orchestration 提供单一 `CreateFromGoalInTransaction`，在原事务内按原顺序创建任务、接纳适用显式条件并登记初始输入；三个原步骤成为私有实现。原命令身份只取自初始 `SessionInput`，避免来源参数分歧。同步 GOAL 和旧异步目标裁决均调用此端口，Sessions 保留会话序、投递状态与关联写入；Task/Session 设计已同步。

生产会话命令及任务/来源查询验证了原目标、内容、输入引用、`AcceptedBy`、`TaskInputSeq=1`、初始修订和绑定：草稿保持 `DRAFT`、`BoundInputVersion=0`、`ACCEPTED` 和 `REQUIREMENTS` 等待；显式条件保持 `USER_EXPLICIT`、绑定版本 1 和 `PROCESSED`，当前任务修订为 2 而原创建引用仍为 1。旧 `SubmitGoal` 保留 `SUBMITTED`、原 `DECIDE_GOAL` 责任及 `DECIDED`；同步输入保留 `TASK_ACCEPTED` 和原输入回执。到期、会话不存在、修订冲突及无会话却指定修订的永久拒绝均在任务写入之前完成，重放保持原拒绝且无部分会话或任务来源。重复显式条件的事务内失败同样不留下部分事实，随后有效目标只创建一个任务。既有 FAILED/CANCELLED 后同会话新目标及等待依赖重启路由测试通过。

精确测试源码为候选提交 `b4c3f20756900e92a1c2ffe2973e0d83ef9ece8e`（实现 `9f0d20aa475843ed90904b9a7f81d7f428633a9d`，已同步集成 `aec455f6ea19022125a0016b349f8fb36cbceb99`），树 `ab2fdc5d53a8206edb4502e86abc71e086c0868c`。本次无冲突合并的树与候选完全一致；后续只更新本工单，故复用同源验证：

- `make check-code CHECK_PACKAGES='./core/tasks ./core/sessions ./cmd/assembly ./conformance ./conformance/sessions'`：imports/fmt、普通和 fault lint（均 0 issues）、规则扫描及相关包 race 全部通过。日志：`/tmp/lerna-module-interfaces-implementation/ticket-13-final-check-code.log`。
- `go test -tags fault -race -count=1 -v ./conformance/fault -run 'Test(InitialGoalCreationCommitBoundaries|AbruptCrashAtEverySubmissionCommit|LostReceiptKeepsOriginalResponsibility|SessionInputAndProcessingCommitBoundaries)$'`：全部通过。草稿/显式目标分别覆盖原 `sessions.input` 窗口的提交前崩溃、提交后崩溃及丢回执，重启重放保留原决定和唯一任务；既有异步提交与输入处理窗口也通过。日志：`/tmp/lerna-module-interfaces-implementation/ticket-13-final-fault.log`。

Baseline、结构性 red（缺少创建端口的消费方编译失败）、green 和完整证据见 `/tmp/lerna-module-interfaces-implementation/ticket-13.md`。只解决本工单；父 spec 及三个既有用户文件保持原样。完整 `make check` 留给最终集成评审。
