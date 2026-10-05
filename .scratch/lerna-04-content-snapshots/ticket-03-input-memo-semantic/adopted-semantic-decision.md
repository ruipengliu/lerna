# 03 Input memo：下一条语义资格决定（STATIC）

采用一个有限的“真实 PG 热输入仍服从当前资格”vertical；不增加产品能力，不直接重跑容量。机械 memo 测试不替代此资格。本次没有运行 Go、PG、测试或任何业务调用，也不授予 native 执行槽。

## 1. 精确源码与证据界限

根为 `b28b2e3182d97156c5fe203baf5cf2183631b7f2`，WT `/tmp/lerna-worktrees/content-snapshots-03` 是明确 WIP，不把 HEAD 当全部源码。全文读 2165-byte `minimal-input-memo.diff`，SHA256 `27206dba0ab2a6fa753c6efb93ea3b6ff2e7e291eeadff1d3efeca00f67d24ea`；目录为 `/workspace/lerna-content-03-137311247276/input-decode-memo-test-first-static/input-memo-candidate-static/`。机器读取 formatted manifest 的对应字段并核实际文件（未声称人工全文审计其 332 项）：

| 相对 WT 文件 | SHA256 |
|---|---|
| conformance/internal/decisionfixture/context_dispatch.go | c1042da40d5ab316d4bb788d7b866c4f1085ac595508a7751524d3bcb5c0161b |
| conformance/internal/decisionfixture/context_input_decode.go | 0fa3cc7acc76bc3d16c5fde7e6b5a45371f1aca2508bc35c781ae8287b3f256f |
| conformance/internal/decisionfixture/context_binding_decode.go | fe6e723b65cf1864e8e5a1071f38c6b6752394c4e5a91a5d7e87a6bb9ea45fc3 |
| conformance/internal/decisionfixture/context_budget.go | b3ad33bb1ed9c89dc079afedb81dc9e43420a0609cb2b87422d1bb009ec9078d |
| domain/content/processing.go | c01386301858c71432457591b8e122ab03b765d1cb796a5c91c58445f7195c9e |
| domain/content/closure.go | 2dc73e9e85b9efab3760218da62a56ed90eb8eda2f7d671468cf83e80bf6d1c2 |
| adapters/postgres/content/policy.go | 650d0325388217df4c93362da7876f7f62b9789baa7053d594f6a33225596182 |

`ContextDispatcher.input` 仍先完成原 SQL/FOR UPDATE 全行 Scan，再查一项成功语法 memo。Store/owner/schema/inputID/full bytes 全相等才命中；返回独立 clone；错误不缓存，冷路径保留 typed partial output。`BeginCompile/currentAttempt` 的完整 tuple、当前 revision、重新编码后 digest、control、保存的原 attempt、最后 trusted/budget clocks 均在 memo 之外。`Current/binding` 的完整当前输入比较和前后 trusted 查询不被 memo 代替。clone 两个 Revision 指针的真实 RED/green 是独立历史资格，不冒充 Input memo 的行为 RED。

## 2. 唯一下一 vertical：补真实 warm revision，再跑受影响语义控制

最小 test-only 改动落在现有 `conformance/component/content_context_compile_budget_test.go: TestContentContextInputRevisionKeepsOriginalRoundsAndRejectsWiderTuple`，不新建私有事实或测试业务端口：

1. 原第一次真实 Compile/六次已确认读取完成后，保留同一个 dispatcher；可用两次公开 `ObserveInput` 验证相同完整输入及返回副本不污染原输入。缓存命中次数的证明属于已有机械测试，PG 故事不新增计数 oracle。
2. 原受信 `InstallInput(expected=1)` 合法更新未绑定的 revision/GoalRevision/Goal。**第二次 Compile 在原 dispatcher 上执行，取消此处提前 Reopen**；后面的既有 Reopen→第三轮、第四轮拒绝与最终重开观察全部保留。因此同时覆盖热行改变与冷恢复，原三轮、65536 累计读取预算、原 Request/Deadline 不变。
3. 更新后、第二轮前，使用第一轮公开预算观察返回的准确 CompileAttempt，调用一次合法形状、此前未使用序号（原六读后为 `7`）的 `ReserveRead`。必须是 `compiler.ErrChanged`、空 reservation、公开预算全文不变；不执行对应实际读取。此处直接覆盖 `currentAttempt` 每次重新核 revision/digest/control，而不是只靠 ObserveInput 的显示。用原真实 ContentRef/purpose/subject，不猜私有行。
4. 保留既有新 M 的公开 bytes→DecodeMandatory→完整 Input 相等、第二轮预算 12 读、拒绝扩大 tuple、第三轮 18 读及 exact totals、第四轮 BudgetRounds、公开无派发与重开不重置尾部。不能为多一次观察增大原 30 秒 caller；若该准确 scope 未完成，应报告实际失败。

这是新增资格断言，正确候选可能直接 green；不制造假旧实现或宣称必有新的业务 RED。此前真实容量失败和 missing-helper compile RED 保留各自含义。

已存在且应在同一最终源码语义选择集中复用的控制，不再复制一套矩阵：

- `content_context_binding_decode_test.go` 的 `TestContentContextDecodedBindingPreservesCurrentPolicyAndWorkerControl`：完整正常完成、热绑定后真实 Process 撤权、真实 worker Cancel；绑定之后不得为了测试改写已冻结 Input。
- `content_context_budget_concurrency_test.go` 的原期限真实锁等待，以及最后一轮/六字节竞争；前者可在原 peer、同一原 caller 中先两次 `ObserveInput` 再进入原 budget 锁等待，以覆盖热表示不绕过最后 DB 时钟。原 deadline 2 秒、lock 等待窗口及原注册/退出责任不变。
- `content_processing_body_seal_test.go`、`content_processing_ancestor_seal_test.go`、`content_processing_ancestor_read_gate_test.go` 的现有正常/封存/实际 I/O 中撤权控制，以及 `content_policy_actions_test.go` 的实际政策锁后时钟与 record 等待控制。它们证明 Content 两事务实际 gates，不证明 memo 提速或每种网络错误。

TrustedUntil 仍是每次当前受信查询的原固定上界；不得在 warm/reopen 时续期。上述 budget deadline 故事不冒称单独测过 TrustedUntil 自然到期。此语法改动没有删除 trusted 查询，现阶段不另造 trusted-expiry 矩阵；如新执行在此门产生失败，应精确记录，不把它归为缓存性能。

## 3. 旧 clock9 的错误覆盖：必要最小补口，准确命名

现有八项成功/撤权/到期控制不能证明查询错误传播。删除的是未消费的前置 Now；当前 `CheckPolicy` 仍真实执行 MATERIALIZED 锁行+锁后时钟，Scan 错误返回，processing/closure 立即传播；后续 LockVersion/Now/final gates 仍在。不存在要求故意重建已删除 Now 调用的理由，也无需为本票新增 TCP 故障设施。

本 vertical 只补一组现有 `contentfixture.World.HoldPolicy` 可实现的**真实 PG 锁超时**正常/故障对照：先完整发布可读取 Content，正常释放锁→真实 `ReadForProcessing` 完整正文；故障分支确认原查询确实被该自有 policy 锁阻塞，保持到原 `lock_timeout` 自然返回（不增加配置或睡到 caller 超时），检查原生 PG 错误因果、空 `ProcessingRead`，原 caller 仍有效。释放/Join/首 Close 确认后独立公开正文及固定回执仍准确；正常后续读取应可恢复。可用含一个祖先的现有材料把被锁对象放在祖先，经过真实 closure `CheckPolicy` 错误分支；不扩为逐 action/逐门故障矩阵。保留原 fixture Tx/statement/lock 上限与有限父 caller，所有 actor 注册、Join，Close unknown 不清 scope。

这是真实 PostgreSQL 查询拒绝/错误传播资格，**不是 native 网络运输断连、不是假注入诊断的物理故障证明**。运输断连这一特定类别仍未执行，不写“已覆盖所有 transport faults”；目前没有源证据要求另加连接破坏框架。错误若被吞、变成 nil policy 成功或返回正文，才有真实新的修复触发。

## 4. 有限出口

先完成并归档已有机械 N/条件 R，再准备上述 test-only delta 与准确有限 selector；由 root 单独授 native，N 成功才 R。既有控制按其原 caller/bounds 执行，不增加 Claim5、30 秒大图 caller、120 Go/外层、原费用/次数/读取预算；不使用容量重跑替代语义资格。该 vertical 完成后只证明候选的受影响语义，不证明四 B 容量已通过、不保证 5 秒充分、不接受票03/whole04。下一容量资格仍需另有固定源码、原输入与原边界的明确批准计划，所有先前失败、Prepared/效果未知、FD UNKNOWN 原样保留。
