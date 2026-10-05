# 04票03：大图正常对照返回 ErrClaim 的最小闭合路径

**默认采用：保持原业务输入、5秒 Decision Claim、30秒 testcase context 和120秒外层界限，先做一次真正独占、带窄阶段诊断的原条件复验。当前证据不足以选择产品性能补丁，更不足以延长 Claim 或重算原 Decision。** 若仍失败，依据同轮实际阶段数据只修一个可证实的重复成本，再验证同原完整链；不靠反复碰运气取得 green。

固定源 `b7171cebe825a982da8143a304e667e94106b63a`，基线短指针 `137311247276`；WT `/tmp/lerna-worktrees/content-snapshots-03`。已全文读取四份 context-final-capacity-{a,b}-{normal,race}.log、81行 native-identities.jsonl、worker Claim/Start/calculate/prepared/publish/Finish/Step、ContextWorld、两大图完整测试、Source/Assembly/Publisher 及相关 Content processing/closure/publication gates。本文只 STATIC 分析，不执行测试/DB，不改产品；05当前独占 LOCAL，03须待 root 明确交槽。

## 1. 已确认的执行事实与时间线

| 实际运行 | 结果与范围 |
|---|---|
| capacity A normal | 11.726s，exit0/groupAbsent，六组容量正常/拒绝子案例通过 |
| capacity B normal | 34.271s，exit0/groupAbsent，含闭包64正常/65拒绝、static62正常、单Content容量及重复selector拒绝 |
| capacity A race | 30.122s，exit0/groupAbsent，对应A完整通过 |
| capacity B race | 74.336s，exit1/groupAbsent；闭包64在29.46s、static62在22.10s返回 ErrClaim；其余三类仍通过 |

B race launcher PID/PGID3029880、starttime12658174、started_ns1791158343395047014；原命令明确 -race/-count=1/-p1/-timeout=120s。失败行均来自 completeContextDecision 的真实 `Decision.Step` 检查，而非编译、outer timeout或native unknown。它们不能作为正常容量对照已race通过。

父测试41.42s含64失败29.46s及65成功11.96s；static62随后22.10s，故第二失败位于**包累计约63.52s**，不是51s。root报告05 compile启动tick12664402，即B launcher后62.28s；static失败末段可能重叠，首64失败明显早于该启动。launcher到测试开始仍有实际启动开销，不能把包时长强当绝对时间戳。后续05 green启动+95.78s在B之后。并发违规须保留历史，但既不能解释首失败，也不能仅以重叠证明第二失败由共享负载造成；复验成功同样不能追认旧失败的原因。

## 2. ErrClaim 不能定位到“计算超过5秒”

ContextWorld.build 固定 engine.Config.Lease=5s；请求/Permission 的绝对Deadline初始约2min，RuleMaxSteps=1、fixture cost上限1。Claim实际在领取Tx取DB now后设 min(now+5s,原payloadDeadline)，**不是大图setup开始即占用Claim**。calculate 与 publishPrepared 的 ioContext 同时受原ctx、payloadDeadline、Permission.ValidUntil和原Claim.LeaseUntil约束，没有自动 heartbeat。

实际 ErrClaim 可来自 Claim/Start、lockedWork 的状态/stop/epoch/startsequence/原input不匹配、ValidateClaim或PoolClaim、原payloadDeadline、Prepared保存/完成/延期等。PG ValidateClaim查询同时要求完整job身份、state、epoch、worker、revision、准确lease和lease>传入now；sql.ErrNoRows不能单独分辨哪项失配。不能由错误文本或整个test耗时反推Claim开始时刻。

还存在**诊断原因覆盖**：calculate 的读失败转 failed completion，再 finish 时若claim无效会只返回ErrClaim；publishPrepared 原错误进入 deferPrepared，后者也可能返回ErrClaim。故日志未证明 Prepared 已提交、未证明哪次出版/回读已开始，甚至不能排除实际Content路径的底层Claim错误。静态仅能说“短Claim内有较多真实工作，过期是合理候选”。当前29.46/22.10也不是调用方30秒已过的直接证据。

原RuleStarts在Start耐久占用。未存Prepared时盲重领可能触发 rule_limit_exceeded；有Prepared也必须沿原key/原bytes/原费用恢复。不得通过循环Step、补RuleMaxStarts或重建Decision让原正常对照变绿。

## 3. 可见成本与诊断范围

实际完整链必须保留：真实Content建立所有源→compiler处理所有材料→四份M/shell/manifest/lock发布与真实读回→原耐久绑定/Component派发→规则 Source读取全部材料→原Prepared→真实artifact/Proposal出版/读回→完成及公开独立字节对照。闭包64的中间版本和static62的全部实际材料都不减少。

有源码依据的成本候选，而非已测结论：

- 每次 Source.ReadMaterial 都调用 Access.Current；fixture每次锁binding、closed-decode完整Binding（含四份正文）、再读/验证完整Input和当前authority。大图重复传输/解码结构可能昂贵，但当前许可不能缓存成跨调用许可。
- 每次 ReadForProcessing 都有独立读前/读后Tx，各自核target与全部已登记祖先、锁后clock，再Tx外真实Objects.Read和hash。M/shell/manifest/lock及产物拥有大闭包；多个大图读回的总成本很可能高于材料本身的几个字节，仍不能删后门或复用上一次Tx许可。
- PlanPublication 与 Publish 内多次 Current；publication还有StagePublication、真实InstallPolicy、Put、Content.Step的启动/完成来源门、GetCommand及processing readback。worker随后又ReadPublished。它们不是一次对象写的耗时；内部阶段有包含关系，诊断不能把总量与子量相加。
- 30秒 caller ctx 还包括两owner装配、约60份真实源建立、编译/发布、额外lock完整闭包观察及公开最终断言。因此需要记录Claim前setup/compile与Claim内work，不能把总体差额全归给rule计算。

## 4. 下一轮最小有限诊断（sole owner执行）

仅一次原B完整selector的独占race诊断；使用新准确own scopes，原测试输入/全部子案例/超时保持。可先独立有限构建准确测试binary，随后另受控运行，但必须记录两native资格、源/二进制hash和精确selector；这不延长原test/Claim/120s。不得触碰旧unknown资源。若root选择原go test入口也可，重点是完整独占与准确原条件。

诊断只加测试/fixture的显式窄decorators或临时有限overlay，转发**同参数/同ctx/同返回原因**，不重试、不改变成功判定。最低记录：

1. testcase开始、owner建立、源建立完成、Compile/Bind完成、独立lock观察、dispatch、Decision.Step开始/结束的单调耗时和原ctx剩余时间；输入digest及原各界限摘要，不输出正文/凭据。
2. Decision Store的真实Claim返回原LeaseUntil/epoch、后续ValidateClaim/PoolClaim的实际now/错误、SaveDecision的running/Prepared/terminal阶段及所属Within结果；Tx callback Save成功不能冒充commit成功。仅作为实现定位，不替代公开业务oracle。
3. Source四类调用、Access.Current、Publisher Plan/Publish/ReadPublished、Content Put/Step/ReadForProcessing的开始/结束、错误类别和有限字节/来源数量。尤其保留**最先出现的错误**以及随后Finish/defer返回错误，避免末尾ErrClaim覆盖原因。无需改冻结worker流程；实际依赖decorator可以记录其返回错误。
4. 若嵌套大图读取占大头，再在同诊断内或一次明确后续定点诊断分解实际policy/LockVersion/Now/对象IO时间；只用现消费方ports/适配器机械定位，不读另一owner私表，不以调用计数当业务验收。缓冲有限记录、结束汇总，避免每节点刷日志制造显著额外负载。

诊断不足以看到私有分支时明确“不知”，不新增生产通用tracing接口。即使测到某次ValidateClaim的now>=原lease，也只能确认该具体门的过期拒绝；仍要结合第一错误与Prepared的真实commit确定失效阶段，不能推称所有先前操作无效果。

## 5. 复验后的具体判据与最小默认

**原条件通过**：完整B exit0/groupAbsent，两个正常分支实际completed，artifact全正文独立相等，static62与closure64准确来源/用量仍匹配，65等拒绝仍无dispatch；无renew/newStart/预算改动。记录余量和诊断开销，撤掉临时探针后在最终受影响执行中保留原完整B覆盖。一次通过只说明该独占运行通过；旧失败保留，不宣布已证明负载原因或固定性能保证。

**原条件再失败**：停止盲复跑。若诊断确认当前binding读取/解码重复占主要成本，默认优先优化当前ContextDispatcher/Content-backed adapter的实际重复处理：保每次当前Input/subject/purpose/Permission和锁后fresh-time校验，不能以已编译bundle替代当前资格；选定具体重复操作后才改实现。若主要成本是Content SQL/门禁，则只考虑保持准确锁、全部动作、锁后clock/错误优先级的机械减少往返或同一Tx已锁事实复用，并要求原撤权/真实锁等待正常拒绝测试。无证据不指定新索引/批量授权port或跨Txcache。

任何优化必须用**同原bounds**两正常大图全链及对应overflow对照确认，保持current authority/完整真实I/O/原usage；受影响当前权限/期限拒绝也须通过。它不是修改冻结engine/rule2的许可。若5秒窗口确实容不下所需工作，记录未满足当前正常对照，另作明确装配设计决定，不能暗中把本次失败转为成功。

## 6. 初次装配与setup分期的裁决

engine.New允许有限Lease≤1min；因此未来**新独立装配**在初次Claim前固定另一个Lease，在结构上并非修改1.1 wire或旧规则。但它会改变实际worker调度/迟到窗口，是另一组证据，不能证明本次5秒正常条件已恢复。本次不采用改大lease；不在重开/失败后改变原work或原Config。现default保持5秒，不加heartbeat。

同理，预先明确的独立机械setup ctx可用于新fixture设计，但当前setup中已经发生真实Content接纳/出版且原Snapshot输入已安装，不可事后称这些都是“不算时间的机械准备”。把同一test重新给30秒，或移动/刷新原payloadDeadline、AcceptBefore、ReadBudget等，均会扩大本次失败scope，**不采用**。如果以后确需阶段化，必须预先冻结总上界、各阶段和原业务绝对deadline，原业务时间持续流逝、剩余额度不重置，并把新设计与旧失败证据分列。

当前无需新ADR、通用Job框架或修改已发布SQL。必要工作是准确诊断和满足原正常对照；全7AC与whole04仍未因本报告退出。
