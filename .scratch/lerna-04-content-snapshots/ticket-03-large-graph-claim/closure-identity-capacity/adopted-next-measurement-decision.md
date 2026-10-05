# 04-03：局部身份复用后，发布回读未闭合的下一步

## 1. 唯一下一步决定

**现有证据不足以选另一处等价产品优化。采用一次有限、非 profiling 的发布阶段细分测量；本轮不再改产品。** 它只回答新出现的 Prepared 后 publication/readback 成本与首失败子门，保原四项 B NORMAL、原 caller30 / Claim5 / Go120 / outer120、count1、原输入和 scope 注册责任。不盲重跑原粗粒度记录，也不执行已构建但业务未运行的 race binary。

这是对真实新失败的必要观测，不是新测试矩阵：在当前五份低开销诊断 overlay 上，仅补 **worker publication 阶段的 Content.ReadForProcessing 前Tx / 实体读取 / 后Tx 和其现有直接调用阶段**。不重新运行旧9份子成本overlay、不再CPU profile，不以旁路权限/SQL合并/省略回读来获得通过。测量完成后 root 根据具体结果选唯一修正；本决定不授权实验失败后自动再调参或执行第二轮。

## 2. 精确源码与本次证据

只读 WT `/tmp/lerna-worktrees/content-snapshots-03`。HEAD `28e3a468c86d4323d3b497836de23bb311a58952`，实际 binary 是 HEAD 加明确四项 WIP，不能称 pristine28e：

| 当前读到的对象 | SHA256 |
|---|---|
| domain/content/closure.go | `8542696150191da3fafd6b0df90e38f272fd934de807317442933c536fb1331c` |
| conformance/internal/decisionfixture/context_dispatch.go | `5c2b8d472fdbe458b9affb9a9543db3359c85c0dfa90d5357bea843995e624fb` |
| conformance/component/content_context_publication_cause_test.go | `e944e3439eabe84936eb166e1966b178166fd7e1f7b8a1972139b96b9b2768d0` |
| conformance/internal/decisionfixture/context_publication_probe.go | `39051536517c23e30d1c80e919824d9c509e2d773a8e0568fe786fe82f9d5517` |

另实际读取 `domain/content/processing.go` SHA `32f14cd27d48a55069eed5a89aab39a3465f03d6b05189875be9d8382d9a6f32`、`adapters/content/decision/publisher.go` SHA `aaee89f0653a4172062f32a0305a59867f35a77938125147dda73ca9e54d55d0`，以及 frozen worker 的 calculate/ioContext/publishPrepared/finish/deferPrepared。

全文读162行 `context-final-capacity-b-closure-identity-normal.log`（26803B，SHA `4d40b79faf796680faa5eabb5c4832980b08b2b734feefc36e7abce201d4645c`）、`large-graph-closure-identity-overlay/normal-outcome.json`、`large-graph-closure-identity-results.md` 与此前本人 CPU 决定。manifest `57a357247462e2d5650a55922c020fc1b63541f6f5634d0e1231b49856022e86`；normal binary `9d26cd68407c8a438b7e8ef56a6c38d7a00e5fe2f55e8acb9e68547968e98891`，22447782B，dev27/inode560496。native3420931/start14323934/session74589，actual exit1 / groupAbsent / noTimeout，已 STOP/RELEASE。本人没有运行这些命令或任何 native。

- closure64 FAIL12.64s，Step5.024561962s。running/start1 COMMIT7.606859325，Prepared COMMIT10.733288045；61次 Material 与两Plan均无记录错误，两Publish均返回无错误。两ReadPublished中第二次失败：Content.ReadForProcessing 在12.589605205返回pgconn context deadline，调用持续111.574925ms，caller仍余17.410365587s。最后 fresh Claim DB gate 比原lease晚3.586ms。无Completed COMMIT，成功Step后receipt/完整最终正文/RuleStarts/reopen尾部未执行。
- static62 PASS9.77s，Step2.497347856s，原62材料/两Plan/两Publish及两回读完成；Prepared COMMIT6.869493042，Completed COMMIT8.185956122，原公开尾部通过。**raw中的 access.Current 是 count71、inclusive258.600351ms**；不是71.2586ms，也不能把count与时长连读。
- overflow65 PASS4.52s，Content单体normal/refusal PASS，duplicate selector拒绝PASS。不能将局部PASS覆盖closure64失败。
- 独立 StagePublication cause normal1.801/race5.731是另一项原因保持资格；不能作为容量green。race binary `0514d380b88d3bbc3c2985afbe946e9d5bc23be19a11c0826251b84421725d02` 已构建，但本轮业务未运行。

## 3. 因果已知与未知

这次明确不是caller30先到；原Claim5的有限I/O上下文先到，后续DB gate确认已过lease。它已从旧 Source-beforePrepared、旧首次PrepareContent失败推进到 **Prepared后第二ReadPublished**，必须保留为新的真实阶段，不覆盖旧失败。

但失败回读是累计5秒最后的受阻操作，不等于它是主要成本原因。它只实际运行约111.6ms；不能由“在回读超时”推出“优化这111ms就能完成全部Finish”。当前粗记录不能区分该回读在前Tx、实际Objects、后Tx的哪个位置停下，更没有新SQL等待/解码/GC原因证明。

本次closure64的Prepared到Step结束约1.8603s；两Publish inclusive1.576565721s、两ReadPublished inclusive240.608127ms提供了可量的阶段，但这些及Content内部总计有嵌套关系，不相加。`content.Put count6`、`ReadForProcessing count69`等覆盖编译与worker多个调用，不能全部归两次worker publication。新观测必须解决这个归属问题。

局部 map 当前只复用同一次registeredClosure的成功 fullRef→ID，最多65项，满后原计算；未缓存Record/Policy/error，原访问、锁、clock和逐action不删。静态没有发现能把本次失败归为 map65预分配或语义回归的依据。一次normal比旧normal慢/快不能定因。原CPU样本仍是未含这次map的旧28e，标签cum与off-CPU限制不变；不能把已改路径的旧CPU数字再当下一优化收益，也不再凭推测缩map/删排序/改validator。

## 4. 真实路径与不能删的工作

`components/decision_engine/worker.go:760 publishPrepared` 对每个原prepared publication：新lockedWork/Prepared digest gate → Publisher.Publish →准确ref比较→ Publisher.ReadPublished →原bytes相等；最后finish另有fresh lockedWork/claim/save/complete。两个publication共用原lease，不可为第二项重开5秒。

`adapters/content/decision/publisher.go` 的 Publish 内又执行原Plan/current binding、StagePublication原请求耐久登记、PrepareContent、Content.Put、有限原GetCommand/Step进度，然后 **publishObject自己的真实 ReadForProcessing 验证**。外层 ReadPublished 又先独立Current，再真实ReadForProcessing。前者负责Content出版实际正文，后者是冻结worker的独立读取验证；二者不是可凭字节相同删除的“重复调用”。不让缓存的Publish返回值假代替worker回读。

`domain/content/processing.go:30` 每次真实读取有两次独立owner短Tx；每次target政策完整read/process、target真实LockVersion与锁后Now、fullRef优先、完整registeredClosure当前所有来源、末freshNow；中间有实际Objects.Read/hash/length与bounded context。第二Tx不使用第一Tx的Record/Policy资格。读后Tx不因只剩短lease而省略或异步补做。

ContextDispatcher.Current仍重新SELECT binding与full Input，准确Permission/fullInput/control/trusted clock；Binding memo仅语法值复用。以上门禁、错误顺序、完整来源、读取字节计量、rule starts/费用、原Prepared及publication身份全保留。

## 5. 一次有限测量的明确实现范围

只由soleowner/root另行授LOCAL后执行；此报告没有准备或执行overlay。

1. 用当前四源SHA生成**新**overlay/provenance；继承当前五份原phase诊断的未变部分。不要覆盖新memo/map/cause代码，不复用旧9份替换源码。新增诊断替换只限当前 publisher.go 与 processing.go 的实际调用位置及现诊断记录器所需连线；先静态diff证明只有取时/计数/原因记录，无SQL/授权/返回值/调用顺序改变。冻结worker不改。
2. 原Publisher装饰器区分两次worker Publish、两次ReadPublished，使用bounded ordinal/阶段标签；compiler PublishBundle独立标为compiler或不纳入细分。明确区分 `Publish内verification` 与 `ReadPublished外层read`，不能靠六个Content调用的总数猜来源。诊断标签只能影响记录选择，不能承载权限、预算、deadline或改变上下文取消。没有归属时记录unattributed，不能强制分配。
3. 每个worker Publish保留现总时长，并各计实际 Plan/Current、StagePublication、PrepareContent、Put、GetCommand、Step、verification read 的调用次数/耗时/首错。多次有限轮询按原16上界汇总，不扩大循环。不要打印请求/正文/凭据，不记录SQL原文或新增查询。
4. 对这四次worker范围内的实际 processing read（两Publish内验证、两ReadPublished），分别记录：pre Within入口/退出、其callback入口/退出、Objects.Read入口/退出、现有hash/length段、post Within及其callback入口/退出。沿原observe中现成位置，仅累积target gate、registeredClosure、末Now的时间与首错位置；若需定位闭包失败，只记录当前动作类别（Now/CheckPolicy/LockVersion/postNow）、有限ref摘要与已访问数，不收全部逐节点成功事件。全部真实Store/Objects调用不替换、不增加clock/SQL取样。
5. 时间以Go单调elapsed作耗时，原已有DBNow/lease事实仍作资格；diagnostic Now不能伪装可信DB钟。失败段返回的原cause不包成新业务错误。固定小型计数器和有界事件数组，最多保留首错/每段首末事件；溢出计数要报告，不能增加容量制造更重测量。打印在测试结束后，不在每个SQL边界同步写log。父子inclusive分开，callback residual只标混合成本，不能直接叫CPU或SQL等待。
6. **一次原exact4B NORMAL**，原count1/30/5/120/120/初次输入与所有原oracle保持。已有fresh scope登记/FD、Wait、group/Close责任照旧。原biggraph setup也计入同caller30，不另造机械setupcontext。测量本身有开销；若改变失败位置，报告本样本，不将旧失败改归因。若出现无错通过，只表明本次通过及阶段成本，不能当无产品修复的确定性能green。

这一次不需要新的CPU采样或调用树，也不需要新DB-plan/索引实验。测量目的仅是把当前1.86s publication尾段与最后read的子门解开，为下一处有证据的等价改动定位置；不能因目标未达就自动连续诊断。若耗时仍主要在测量不到的callback残差，输出未知与原证据，由root重新裁决，不能擅造跨Tx缓存。

## 6. 后续修正与验收边界

下一项产品修正必须以这份实际阶段结果和源码中的具体重复为依据；目前不采用SQL合并、permission/wholeclosure缓存、并行材料或publication、弱化post-I/O门、heartbeat、Claim续期/重试、RuleStarts/费用归零、减少正文读取或扩大budget。

一旦选定并实施某一等价修正，仍须新source受影响真实current-authority/锁等待/撤权/wholeclosure/metadata错误顺序控制，以及原完整4B normal和独立race，观察两个真实Publish与回读、Completed COMMIT及原公共尾部。原65overflow/Content正常拒绝/duplicate正常不能删。不能用本次已有static62 PASS或旧原因保持测试代替这些出口。

本轮真实closure64失败、Prepared/已发生publication和全部scope责任保留；不把groupAbsent当所有per-resource CloseACK，不清理旧unknown/profile FD。未接受票03七AC、whole04、CI或交付。本人仅静态分析与写此/tmp报告。
