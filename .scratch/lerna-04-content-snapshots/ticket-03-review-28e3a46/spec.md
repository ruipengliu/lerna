# Ticket03 独立 Spec review

固定基线：`1373112472761d32c22ed6d7603d806d8ef9a3c7`。固定候选：`28e3a468c86d4323d3b497836de23bb311a58952`。

审查命令：`git diff 1373112472761d32c22ed6d7603d806d8ef9a3c7...28e3a468c86d4323d3b497836de23bb311a58952`。
固定范围来自 `/tmp/lerna-03-28e-review-scope.json`：58 commits、244 changed paths；全文读取其 commit list，并机械处理全部 path inventory。所有源码引用均指候选 Git object，未使用 moving HEAD。

## 摘要（Spec 轴）

未发现需要修正的 (a) 缺失或部分实现、(b) 越界实现、(c) 错误实现：**a0 / b0 / c0**。

七项 AC 在固定源码中都有具体实现及对应验收路径。完整必要正文实际作为首份 Content 材料接冻结 rule/2；输入、回显、Proposal、外层负载及数量分别预检。来源包含实际处理但省略的材料；处理、保存、披露分别走当前权限。原 Snapshot 的轮次、读取预约及 unknown 耐久保存，Prepared 恢复沿原键与字节。新增 memo 只复用已成功的语法表示；每次仍读取完整新鲜行、核当前 Input/权限/时钟并完成原事务。

冻结 1.1、原规则与已发布迁移没有变化，未新增 Task、Grant 或真实 Provider，也未开放完整 1.2 广告。旧 writer 闭包已独立机械证明。

这是源码与验收设计的独立静态审查，**不是执行验收**。未运行 Go、native、DB 或新测试；未读取 Standards 报告。原 B 容量资格、本轮并发预算重验、完整 integration、资源资格及准确集成 CI 仍由执行方完成。编译包含某测试不证明该测试被选择或执行；历史失败、claimed 状态与 15/41 进度不被单独列为源码缺陷。

## 采用依据及边界

- 主规格：`.scratch/lerna-04-content-snapshots/issues/03-complete-context-dispatch.md:9–15`，七 AC。
- 整片规格：`.scratch/lerna-04-content-snapshots/spec.md:29–41`、`:45–51`，真实 Content、当前权限、完整 processed 来源及 Snapshot 边界。
- 接线：`decisions.md`、`final-api-handoff.md`、`contract-shape-decision.md`；准确已接受 `ticket-02-api-handoff.md` 与 `ticket-03-05-adopted-handoffs/lerna-04-after-02-final-port-delta.md`；具体票03条件 handoff 的 §1–7。
- 采用的局部决定：`ticket-03-no-dispatch-oracle/decision.md`、`ticket-03-original-budget/decision.md`、`ticket-03-selection-strategy/decision.md`、`ticket-03-overflow-diagnostic/decision.md`。无派发以原 Command not_found、可信 binding/dispatch 分别不存在、原实际 Component 入口零到达及有限恢复窗口共同证明；冻结 Decision.Get 的 result_unavailable 仅为辅助。
- 支持设计：根 `CONTEXT.md` 的职责与必须保留规则；`docs/adr/0006-authorization-budget-at-action-boundaries.md`、`0007-versioned-content-memory-snapshots.md`；`docs/architecture/capabilities.md` 上下文构造、`governance.md:87–93` 来源和生命周期。没有将完整设计中后续 Task/Grant/Provider 的要求提前变为本票隐藏依赖。

## 七 AC 静态 coverage ledger

下表的“验收路径”仅描述固定源码里的可执行设计，不声明任何本轮运行结果。

| AC / spec line | 固定候选中的实现证明 | 验收路径及限界 |
| --- | --- | --- |
| 1 / issue:9：领域职责与小端口 | `domain/task/context/types.go:117–133` 的 CurrentInput/ProcessingContent/Assembly；`compiler.go:11–75` 实际消费；无 components/adapter/conformance import。`context_dispatch.go:99–159`、`:193–258` 的受信持久 Input 与绑定/派发事务，仍在 fixture。 | 独立域边界和 fixture 输入 CAS；`context_world.go:100–151` 实际装配。输入在成功绑定后不能经 InstallInput 更新：`context_budget.go:110–115`；未建议或引入放宽该约束。 |
| 2 / issue:10：完整闭合 canonical M | `types.go:23–96` 完整 Input、预算、unknown、责任与 derivation；`canonical.go:26–136` 完整验证、`:154–206` processed 与 derivation 一致、严格解析；`:211–296` no-number canonical。 | `canonical_test.go:12–31` 独立 literal golden；两个 testdata JSON 保存 Unicode、控制字符、超 JS-safe decimal、非空 unknown。`content_context_test.go:42–115` 真实公开正文和 artifact 字节回显，`:277–318` 完整 canonical 集合投影。 |
| 3 / issue:11：真实首材料接冻结规则 | `assembly.go:80–110` M→shell→manifest→lock 无环规划，M 是 materials[0]；`source.go:52–91` 真 Content 读取及 M/完整输入/processed/首材料比对；`publisher.go:74–125` 原请求先持久登记、真实 Put/Command/published/准确回读。 | `content_context_test.go:58–115` 独立三份结构正文和 artifact 去前缀完整相等；`content_context_projection_test.go:34–124` 实际新对象构造一致但错误的省略映射，Source/Lock拒绝且重开保留原 queued 责任。不能把该直接 adapter 拒绝说成已运行 worker。 |
| 4 / issue:12：同时有限容量 | `assembly.go:113–180` 三份 metadata、全部实际材料、回显+真实 schema-shaped Proposal、完整请求和 Found response；`:191–229` 每版本完整祖先（包含中间节点）规划；`capacity.go:16–73` 63材料、64条件、64闭包、256KiB Content、1MiB wire、原 Decision I/O、总容量/预留同时检查。 | 精确局部边界 unit tests；真实 UTF8、metadata、Proposal、条件64/65、闭包64/65、Content层、62 static 材料 controls。62 是实际装配可达上限；局部63/64计数黄金不冒充端到端63正常。首次显式有限 tuple 与历史失败 scope 区分。 |
| 5 / issue:13：overflow 无成功 Snapshot/Decision | `compiler.go:29–35` 合法原身份先占轮再完整验证，`:67–72` Plan成功才出版；`assembly.go:165–181` 预检失败即退出；`context_world.go:142–149` 明确 overflow 记录、不 Bind。`context_entry.go:53–72` 真实边界机械见证并原样转发。 | `content_context_test.go:117–163` 原 Command not_found 与可信 binding/dispatch 独立缺席；`:216–274`、`:320–334` 实际正常对照及原装配重开观察窗口。Decision.Get result_unavailable 不等同不存在。每类可容纳正常对照由实际冻结规则完成。 |
| 6 / issue:14：原绑定与 Prepared 固定恢复 | `context_dispatch.go:207–258` 固定完整 binding、原请求、有限责任；`:261–353` 每次新鲜 row/input/权限与 trusted clock；`:357–403` 发送前 journal 原 bytes；`:515–643` 原 receipt 优先核对再有限原身份发送。`context_budget.go:97–131`、`:140–203`、`:244–319` 固定原 tuple/三轮/累计预约，unknown 不退。 | 原编译 rounds、累计验证读取、unknown、修订拒宽、3次overflow、真实锁等待及并发最后配额测试；`content_context_prepared_recovery_test.go:103–220` 真接受后响应期限故障、真实重开、无新编译/读取/计划/计费的原 Prepared 恢复。旧0003范围保留精确 BudgetHistoricalUnknown。 |
| 7 / issue:15：完整实际 processed 与真实边界 | `compiler.go:45–63` 每份实际读都入 processed/derivation；`selection.go:65–106` 实际 selector bytes 固定 include/omit。`assembly.go:74–86` M Sources包含全部读过来源，worker materials另按 selected；`:138–139` 保持旧 worker manifest+materials 含义。`processing.go:48–134` 两次完整 current read/process qualification，中间真实对象I/O。 | `content_context_selection_policy_test.go:23–243` 独立 process/save/disclose 拒绝与真实正常对照、include/omit 仍继承B限制；公开 Proposal 来源与 compiler 来源口径分开。models 未装配，无 Provider 计数或生产门禁声明。 |

## 新 memo 的准确源码证明

`context_dispatch.go:265–269` 先完成原 SELECT input_id/body/FOR UPDATE/Scan，然后才进入 memo；`:273–283` 每次重新严格解析当前 Input、完整 DeepEqual/running 与 trusted 当前时间；Binding/Current 的原权限比对和 Store.within 的 Commit 保持。

`context_binding_decode.go:15–43` key 包含 Store 指针、owner、schema、input_id、locator column/value 和整份新鲜 body 的 bytes.Equal。只有成功且 ≤1MiB metadata-only表示保存；错误、不符合缓存条件的表示仍走原解码结果。immutable entry 拥有 body 与 Binding 副本，atomic.Pointer 发布，最多一项；不保存 policy、time、许可或 Commit 判断。

`context_binding_decode.go:56–87` 深复制 Input 所有 slice/nested gaps/TraceContext 和 Bundle 的 Sources/Bytes/Materials/Processed，保持 nil 与 present-empty。逐项对照固定生成 `contract/gen/go/v1_1/values.go:588–621`；Request 的唯一额外 mutable 字段是 UseRefs 与 TraceContext，二者都已复制。

`context_budget_probe.go:99` 新 peer 显式复制五项既有装配字段，得到新空 memo；未复制 atomic state。该实际 source 变化需要重验三个 peer qualifier，历史资格不代替新执行。

`content_context_binding_decode_test.go:23–205` 新测试走合法当前 B process 撤权与既有真实 ControlAuthority；拒绝制造不可达的绑定后 Input 更新。process 子例仅实际 adapter 拒绝；worker-control 子例一次真实 Decide 到达并被 decision_cancelled 拒绝，零 RuleStarts/原 Applied receipt 与当前 get-only after-Reopen 权限分别观察。五项 memo unit 测试是机械表示/隔离资格，不作为业务数据库验收。

## 固定源码与归档机械证明

独立 Python/Git 只读检查（未启动 native/Go/DB）：

- 在候选 Git object 中读取 `testdata/legacy-context0003/provenance.json`，逐项读取其全部144 Artifact；核实 SHA256、Bytes，并与原 Git commit `33811016fe11defce61dcb21d71902f5d0d7c59e` 的 Destination **全字节比较**。
- 结果：**144 files、1,419,513 bytes、0 errors**。这是原 production/module/embed 闭包，并非新版产品。额外两份旧 writer 监督 driver 不冒充 archived production。
- 全部244 changed paths 机械检查：`contract/`、`components/decision_engine/`、已发布 Content0001/0002、fixture0001/0002 **0 changed paths**。0003与0004为该fixture新增链；旧0003实际运行后的追加0004没有改0003 checksum。
- `context_budget_historical_test.go:49–134` restore 固定闭包/manifest/完整hash；`:139–274` 原 producer gate、PID/PGID登记、有限实际 Wait/group absence/CloseACK；`:383–563` 真实旧 writer 独立scope追加0004、历史 Input/binding/queued责任保留、不填0预算、重开/新Snapshot graft拒绝，并有独立当前正常scope。源码中没有凭进程退出把原 unknown Close 洗成确认。
- 固定34个 component `TestContentContext*` 及5个 memo unit 函数存在；仅是静态 inventory。尚未进行本候选全部真实 integration/实际资源核验与集成 CI，报告不将存在、编译或历史测试当本次执行。

## Findings

(a) Missing/partial: 0。 (b) Scope creep: 0。 (c) Wrong implementation: 0。

未发现可定位到 spec 行及候选 hunk、具备可达原因的必要修正。该结论不接受或 resolve 七 AC，不提前宣称 whole04 完成。
