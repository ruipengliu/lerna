# 04票03：原Snapshot耐久编译预算

**采用fixture owner的窄耐久编译准入/读取预约端口；原完整SnapshotRef是预算身份。** 每次合法准入原子占用一轮，最多3；真实处理读取前占用准确字节上界，未确认不退款；输入revision变化、重开和失败均不得重置原累计或延长绝对期限。不建Task、Grant或通用计费框架。

只读固定partial **`1148a1fa7c13c006715c8f77fb964df0c3fb42c3`**，WT `/tmp/lerna-worktrees/content-snapshots-03`；root所述281fab为当前集成背景。读取compiler/types、fixture dispatcher/world/0003、Content-backed assembly/publisher接口、已采用decisions/finalAPI/票03和新queued budget测试。该budget产品尚未实现，本次无native；测试文件的存在不是red/green证据。

## 1. 当前缺口与原身份

当前Compile每次从Input.Policy.ReadBudget重新建立局部remaining，按本轮材料字节扣减；次数没有持久计数。fixture 0003已有inputs/bindings/dispatch/publications/failures，但没有原Snapshot预算。故仅修改局部循环无法约束重开、另一输入revision或并发Compile。

预算key为完整 `Request.Payload.SnapshotRef` 的严格稳定编码，包含tenant/owner/kind/id/revision；不能只用input_id、Snapshot.ID、最新input revision或一个进程counter。fixture owner仍是唯一该预算权威。

首次受信安装该原Snapshot输入时，固定不可重置tuple：原输入owner/完整可信principal、完整SnapshotRef、原TaskRef、Command owner+ID、Decision target/ID、component/install binding、原Decision limits/UseRefs、原AcceptBefore及Payload.Deadline、明确purpose、byte/selection策略、Capacity/OutputReserve/ReadBudget和原max rounds=3。全部使用现有准确值；不能由当前now、最后一次Observe或默认加倍生成预算。原RetainUntil/已发publication身份同样不能在恢复时放宽。

同Snapshot下允许原授权的目标/条件/材料等**未绑定前**input revision更新，但上述tuple必须相同；更新只改变该轮待编译内容，不生成新budget。tuple冲突明确拒绝，不能静默取较宽者。更新短ctx只可更早退出，不能成为下一次续期依据。已存在成功binding/dispatch时转原身份恢复/查询，不能继续用新输入重编译覆盖旧Snapshot。

仅可信owner明确批准的真正新业务Snapshot身份和相应新的合法Decision/Command绑定才有新预算；改个input_id或不断改revision不是授权扩额手段。同原业务失败的自动重试不得自行换Snapshot revision/new ID逃避3轮。

## 2. 最小consumer-owned端口

在 `domain/task/context`定义与编译职责相邻的小端口，由真实fixture Dispatcher实现，Compiler显式消费。可与CurrentInput由同一对象装配，但不通过conformance类型反向导入：

- `BeginCompile`：原input_id/预期完整输入revision与摘要/原Snapshot，返回准确attempt身份、原固定预算tuple/绝对deadline及此次准入结果。
- `ReserveRead`：attempt、稳定本次read identity、准确ContentRef及用途，返回**本次唯一新预约**的有限读取资格；重复已有预约不是再次物理读取许可。
- `ConfirmRead`：同原预约记录此次获准返回的准确ref/字节数或未确认结果；只补充观察，不释放额度。

名称可按当前代码风格落地；不需要泛用Budget/Use服务。各变更都是fixture自己的短Tx，实际Content读取在Tx外。提供受信有限 `ObserveCompileBudget`（或现Observe的准确附属观察）返回原tuple、rounds、reserved/confirmed/unknown与有限页，使测试通过实际消费端口观察，不读私表断言。

预算root行锁/原输入CAS和读取预约共用确定锁序；所有入口锁后及提交前采当前DB时间，检查原期限、当前完整可信主体、当前输入控制及attempt绑定。不要持fixture Tx等待Content owner I/O。相同root并发Begin/Reserve须由真实行锁/唯一约束限制，无跨owner共同事务保证。

## 3. 次数扣除与overflow

先验证有限ctx、可信主体、足以定位的合法固定tuple及当前输入；随后 **BeginCompile同Tx原子递增rounds并写attempt，再进入完整语义/数量/容量验证及真实处理**。输入Observe与Begin之间变化则CAS拒绝，不消耗未获准轮次；不能拿旧Observe覆盖新输入。

合法原身份的语义/数量/容量overflow必须消耗已获准这一轮，即使它在任何字节读取前拒绝。否则反复overflow可无限重做编译工作。完全无效/无权限/无法识别原tuple或原deadline已过的请求不准入，不制造新budget。现ValidateInput可在容量计数阶段返回ErrOverflow，因此要把原budget准入置于该guard之前，同时保持身份/安全结构校验先行，不能为计数接纳任意无界坏输入。

成功、overflow、selection mismatch、出版失败、CAS失败都不返还轮次。Begin提交后进程在计算前死亡，准确称“已耐久占用的编译轮次/未确认启动结果”，不能说已真实完成计算。第4次返回明确内部预算耗尽，空Bundle、无新读取/出版/派发；不把预算耗尽伪装成某个已测得的字节overflow诊断。

此处3按已采用handoff是总共最多3次获准编译（首次计入），不是首次+3次重建。正常重复Compile三次若都真正重做读取/规划，计3；仅核对原已持久publication/Prepared而不重新编译，不计一轮，也不得顺便重新读取材料逃过预约。

## 4. 读取占用、实际值与unknown

每次真正调用ProcessingRead之前，按经过准确ContentRef验证的ByteLength预占该次最大返回字节。原budget root中 `reserved + requested <= original ReadBudget`用溢出安全整数原子判断，完成原预约才允许一次调用。零长内容也有一次读取身份，不能把unknown读取计为“未发生”。

预约明确记录attempt/序号或本阶段固定identity、准确ref/用途/上界、状态。原预约key重放只用于核对，不能返回一个可无限重复发送的“还剩这些字节”额度。正常读取返回准确长度后记录confirmed；错误、进程死亡、响应或确认提交未知维持占用，不因当前ref可重读、not_found或ctx取消退款。最小默认所有已承诺read预约都不退款，包括实际read之前拒绝的保守占用；诊断区分reserved与confirmed，不把前者说成真实CPU/I/O字节。

预约提交未知时不先读；在同原identity核对。如果无法证明本次尚未消费的唯一发送资格，不复用该预约启动读取。需要重新物理读取时必须新预约并再占用，且仍在原attempt/原轮次及期限内；不能通过重建attempt无限重试。最小恢复可以放弃unknown本轮并占用下一轮，不需要建设通用分布式lease执行器。

总ReadBudget来自**首次显式受信输入的准确Policy.ReadBudget**。例如当前65536就是整个原Snapshot累计上界，不乘3、不从剩余Task/fixture费用推算、不在失败后扩大。后续输入同原Snapshot不能增加它。读取过程中input revision变化，已发生/可能发生的占用保留；下一轮读取新revision仍使用同root剩余额度。

## 5. 实际读取范围不能遗漏adapter回读

当前Compiler材料A+B合计22字节，但 `Assembly.PublishBundle → publishObject`在4份派生Content发布后还调用真实ReadForProcessing回读。若称“编译累计读取”，这些由该Compile触发的验证回读也必须纳入原读预算；不能只持久化材料22而声称全部编译处理读取受限。

最小装配复用现有中立 `ProcessingContent`：创建本attempt的预算化reader供材料循环，并把同一reader显式交给实际Assembly.PublishBundle用于bundle验证回读（只为这个实际消费者调整窄签名/局部参数）。它转发真实Content，不替代Content当前授权；每次Reserve/Confirm围绕实际ReadForProcessing。不要临时修改共享adapter配置、用全局cache或可伪造payload预算token。若adapter选择局部同效装配，也必须证明这两条实际读取路径均经过原budget。

`Bundle.ExtraReadBytes`现为compiler材料读取口径，首tracer22保持准确，不能突然改成另一个总数却不改其解释；预算Observe另报本编译材料+派生验证读取占用/确认。Content内部对象完整性I/O、之后Decision Source/Publisher读取和旧DecisionUsage各有自己的范围，不混称全系统物理I/O总量或重新计规则fixture费。Decision Prepared原字节恢复仍走原发布责任，不能被此预算逻辑强行重新Compile来得到许可。

## 6. fixture持久表与迁移

最小具体事实为三类有界记录：原Snapshot budget行（固定tuple、max3、rounds/reserved汇总），attempt行（序号、原input revision/摘要及状态），read预约行（原attempt+读identity唯一、ref/用途/字节、confirmed或unknown）。它们可按现有fixture JSON体加准确唯一键实现，不复制Content正文、不建Task表。原未确认派生Content publication journal继续承担自己的原Command/bytes责任。

**推荐追加fixture `0004_compile_budget.sql`。** 0003虽未作为整票发布，已经实际迁移/运行并有恢复对象；不修改已创建scope的0003 checksum或假装旧预算记录不存在。追加迁移代价很小，避免把先前真实scope变成不可重开。与05 Content owner的0003不是同迁移链。

对旧0003已有输入而无budget：不能假填rounds0/reads0为已知历史。首次原输入的明确新安装在同Tx建root；历史已有binding/publication/unknown处理迹象的scope只准原身份核对，不以0预算允许新Compile。必要旧scope升级可标历史计量unknown并拒绝新编译；新的独立正常scope作对照。不要为了本首测试drop旧schema或重写原迁移。若实施者另选改未发布0003，只可用于明确全新scope且须保留旧0003恢复路线，因此当前默认追加更窄、真实。

## 7. 首tracer能证明什么

queued `TestContentContextOriginalSnapshotCompilationLimitSurvivesReopen`直接调用真实Compiler/Publisher，在第三次前重开，前三次准确Content出版且原M/manifest身份不变、材料ExtraReadBytes22，第4次拒绝空Bundle并无dispatch。它能证明同输入、同原Snapshot的3轮上限跨重开及真实正常出版；仍须通过受信预算Observe确认累计状态，且第4次断言明确预算原因，不能“任意err都通过”。

它不能单独证明总字节耗尽、原deadline、并发或input revision变化。后续分别用：原累计read预算第二/三轮不足（含adapter回读）；三次overflow也占用；预约后未知不退款；锁等待跨deadline；同Snapshot换input revision仍原计数/tuple拒宽；并发两个Begin争最后一轮/Reserve争剩余字节；旧scope计量unknown；已有Prepared/原Content回执恢复不重新Compile。每项保留可获准正常对照、原身份及真实入口无派发观察。

不要求首tracer预先把所有后续case写成通用框架，但接口现在必须能表达原身份/次数/预约/未知，以免先用内存计数做green后返工。上述是静态默认决定，不是产品完成、原费用已核实或七AC退出声明。

## 8. 补充：明确分离compiler与worker的真实读取路径

采用显式参数，**不用context.Value保存预算资格**。具体最小签名是现 `Assembly.PublishBundle(ctx, input, bundle, verification ProcessingContent)`；Compiler把与材料循环相同的本attempt预算化ProcessingContent实例传入。这里复用既有接口，不再新建一层泛用CompilationRead注册器。

实际Adapter.PublishBundle把该参数逐次传给共享私有 `publishObject(ctx, in, object, verificationReader)`；4个object的成功验证都调用传入reader。必须显式非nil，nil配置失败，不能静默回退绕过预算。共享publisher的字节/当前Content门禁、原Command journal与原prepared身份不改。

实际worker `Publish`调用同private publishObject时明确传 `a.config.Content`（真实原Content reader），**不传编译预算reader**；worker `ReadPublished`同样保留原Content读取路径。它们由Decision/Publisher原有限deadline、Prepared/费用及实际读取范围约束，不被新Compile次数耗尽错误地阻塞。两条路径使用同一个介质验证机械实现，但预算资格的来源在各自真实调用点显式可见，不靠环境、goroutine或context猜测。

首budget tracer的原ReadBudget=65536保持。前三轮是否容得下实际“22材料字节 + M/shell/manifest/lock验证回读”的累计，必须由真实本场景长度与受信budget观察核实；本次没有测量或声称一定fit。如果原输入确不fit，准确暴露该事实并调整测试所要隔离的场景/原有合法小输入，不能自动抬高原获准budget，也不能删掉真实回读来迎合三次成功预期。原M强制字段不得为此省略。

后续执行资格（不倒写以上static cutoff）：root随后报告已独立核实 `context-compile-round-red.log`，真实首业务red **1.974s / native1 / groupAbsent**；前三轮真实出版、同M/manifest/原输入、各22材料读取及第三轮前3-owner重开已通过，失败准确为第四轮仍成功。budget尚未实现；该red仍不证明前三轮总编译读取占用在65536内，因为当时还没有累计预算消费。本文作者未执行该运行。
