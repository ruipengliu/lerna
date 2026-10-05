# Ticket03 独立 Standards 审阅

固定基点：`1373112472761d32c22ed6d7603d806d8ef9a3c7`。候选：`28e3a468c86d4323d3b497836de23bb311a58952`。

`git diff 1373112472761d32c22ed6d7603d806d8ef9a3c7...28e3a468c86d4323d3b497836de23bb311a58952`

## 审阅摘要（少于400词）

**1 项硬违反，最高 P2；0 项可选 smell 建议。**

- **[P2] StagePublication 丢失数据库读取失败原因。** 候选 `conformance/internal/decisionfixture/context_dispatch.go:372–374`：`QueryRowContext(...).Scan(&id)` 后，对所有 `err != nil` 都直接 `return engine.ErrForbidden`。若等待当前 input 行锁期间取消、超时，或连接／SQL 执行失败，该路径会把基础设施故障改成权限拒绝；原 `context.Canceled`、`context.DeadlineExceeded` 或驱动错误不再能通过 `errors.Is/As` 判断。`Store.within` 收到回调错误即返回，也不会补回被丢弃的原因。调用链 `Adapter.publishObject → Access.StagePublication` 将这个结果向上传递。此 hunk 违反 `AGENTS.md:105`：“错误必须保留可判断的原因，在边界映射为合同错误；不得吞掉错误”。建议仅将确切 `sql.ErrNoRows` 映射为权限拒绝，其余原因原样返回或保留 cause 的安全边界包装。不能靠放宽事务、调用或原业务截止来修复。

未发现其他需要报告的明确标准违反。单项语法表示复用仍先读取并锁定完整原行，命中后复制完整可变字段，当前输入、用途权限、时钟与提交检查仍独立执行。消费者声明的小端口、跨 owner 真实 Content I/O 在短事务外、原编译预算持久化与追加迁移符合已读取的文档边界。

完整12项 Fowler baseline 均作为启发式检查；本次没有足够收益明确的可选重构建议。fixture 受信同步点、边界观察器和显式逐字段版本转换服务当前资格验证，并未据名称或转发形状判为通用框架或无价值 Middle Man。工具检查范围不重复报风格问题。

本轴仅静态审阅，不执行测试、native 或数据库操作；不读取 Spec 轴或其他审阅／Astra 审阅结论，不接受 AC，也不把尚待最终验证的状态单独判为代码缺陷。

## Coverage ledger

- 固定范围实际核验：244 个 diff 路径、58 个 commits；与 `/tmp/lerna-03-28e-review-scope.json` 的完整 captured paths 和 commit list 逐项相同。该 JSON SHA256 为 `7cefcab217f7f6026bdd27ba8d42bb98857b536869e320903b48fe1870fe2a4e`。不是以 moving HEAD 替代候选。
- 标准实际读取：根 `AGENTS.md`、`CONTEXT.md`、`docs/agents/domain.md`；`docs/adr/README.md` 与 ADR0004/0006/0007/0009/0010；`docs/architecture/README.md`、`governance.md`、`data-model.md`、`contracts.md`；`domain/task/context/README.md`、`adapters/content/decision/README.md`、fixture owner README。就近 AGENTS 检索只有根 AGENTS 适用，humanizer skill 内部 AGENTS 不适用。
- 完整读取所有新增生产逻辑：`domain/task/context/{types,budget,compiler,canonical,capacity,selection,overflow}.go`，`domain/content/processing.go`，`adapters/content/decision/{ports,assembly,source,publisher}.go`。逐项检查职责与依赖、错误传播、完整身份、有限循环／上下文、来源集合、授权与预算入口、事务外 I/O。
- 完整读取新增 fixture 逻辑：`context_dispatch.go`（三块覆盖全部648行）、`context_budget.go`、`context_binding_decode.go`、`context_budget_probe.go`、`context_entry.go`、`context_world.go`、migration0003/0004。读取 `store.go` 修改 hunk、Open/Close/within 支撑路径；读取 `legacy_upgrade_test.go` 全部变更 hunks。原 helper 未变的全部1189行未逐行重审。
- 完整读取重点新测试：binding decode 单元与真实 policy/worker-control 资格；全部 compile-budget tests、实际 PG lock/peer 并发 tests、完整材料投影反例、原 Prepared 恢复；591行历史升级测试与两个独立新增 frozen producer supervision drivers；独立 canonical、capacity 和 overflow 主单元测试。其他 capacity/selection/processing/wire/主 integration tests 核验完整路径及 Test 函数 inventory，但未逐行读取全部测试正文。已读取的工作文件经静态 byte comparison 与固定候选一致（54个活跃代码／测试／README路径，0差异）。
- Frozen 资产机械核验：`testdata/legacy-context0003/provenance.json` 的144个 payload 对每项长度、SHA256及固定原始 commit `33811016fe11defce61dcb21d71902f5d0d7c59e` 的 `git show` 字节均一致；共1,419,513 bytes，0错误。读取 archived README 与恢复验证入口。没有逐行重审144个历史 payload，也没有把这些惰性 `.txt` 资产当作新生产代码。provenance／checksum 元数据与两项新 supervision driver 分开分类。
- 文档／证据：读取 ticket03 evidence 开头140行及完整 final verification plan，核对 partial / pending / frozen 的含义；其余历史 adoption、CI logs、progress 和 provenance 路径仅在 scope inventory 中纳入，没有声称逐行重审原始运行证据。发现基于候选实际 hunk 和 AGENTS，未从历史决策结论派生。
- 12项 baseline：Mysterious Name、Duplicated Code、Feature Envy、Data Clumps、Primitive Obsession、Repeated Switches、Shotgun Surgery、Divergent Change、Speculative Generality、Message Chains、Middle Man、Refused Bequest 均评估；repo职责与目前调用场景优先，未提升任何 heuristic 为硬违反。
