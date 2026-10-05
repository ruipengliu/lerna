# 04 票03：条件实施接线 handoff

2026-10-04；只读固定 root `70f69e524592491a5ad37754cf0793ee131acec6`，01交付 `1a7d910238eb74cddc712d92b0ba4014a72ff507`。依据已采用 `decisions.md`、`final-api-handoff.md`、`issues/03-complete-context-dispatch.md`，以及实际 Content service/ports、Decision service/ports/worker、decisionfixture source/publisher/store。02仍实施中；仅看其 management 声明作上下文，**没有采用WIP正确性或执行证据**。本稿不claim、不实现、不运行native；正式采用硬等02整合退出及实际端口/迁移/API delta复核。

## 1. 固定结论与当前缺口

继续完整 canonical `mandatory-context/1`（M）作为第一材料、Content-backed Source/Publisher、既有1.1 `fixture-rule/2` / `candidate_result`。不新增1.2 Decision、不改变旧规则收费/Prepared/schema/黄金，不创建Task服务、Grant或第二Orchestrator。原七AC一项不少。

实际 `components/decision_engine/worker.go:calculate`：读取 shell、lock、manifest、顺序材料；manifest.Snapshot须等于typed Snapshot；artifact为 `fixture result: ` 加第一材料；每个Requirement都会产生一项Evidence并重复完整artifact ref。输入量是真实三份Raw加材料长度，输出量是真实artifact加v1.1编码Proposal。processed公开集合是manifest+材料；不能把compiler已读上游伪称worker读了原文，也不能把shell/lock额外读取伪称已列入这个旧字段。

01 `domain/content/service.go:Get` 是read+disclose，不是read+process；`Objects.Read` 是介质端口，不是授权入口。当前实际没有可直接声称已有的“内部处理读取”端口。票03需要一个具体Content领域入口，复用02完整闭包门禁，不能在adapter直接读PG/对象绕过领域，也不能用公开Get通过就推断process获准。

## 2. 最小文件与消费关系

| 位置（新增文件名可按局部惯例细化） | 具体职责/消费端口 | 不承担 |
| --- | --- | --- |
| `domain/task/context/{compiler,types,canonical,capacity}.go`、同目录testdata | Compiler拥有闭合M、输入/处理集合/有限重建、容量判定；声明实际需要的CurrentInput、Content处理读取/发布、准确装配测量小端口；返回中立bundle/诊断 | 不import `components/decision_engine`、adapter或`conformance/internal`；不建Task数据库/调度服务 |
| `domain/content/processing.go`（或实际02领域门禁旁） | `ReadForProcessing`之类窄内部入口：准确ref、可信subject、purpose、remaining byte bound、有限ctx；返回准确完整字节及可用的来源/截止观察；前后current read+process、完整闭包/准确published/保留上限、全对象hash；超remaining在介质读取前拒绝 | 不放宽public Get的disclose，不新公共方法/profile；来源观察只给当前获准的内部调用者，不把观察凭据当未来发布权 |
| `adapters/content/decision/{source,publisher,assembly}.go` | 实现实际 `decision.Source/Publisher`；逐字段桥接1.1/1.2六字段ContentRef并闭合校验；消费自己声明的固定Mapping/当前fixture资格小端口和Content领域入口；装配/尺寸结果回给compiler | 不import conformance；无独立业务数据库、无StrategyRegistry；不访问Decision/fixture私表 |
| `conformance/internal/decisionfixture/context_*.go` | 新的显式Context dispatcher/持久输入与映射责任，装配上述端口；自身短Tx保存前态、原命令、绑定与待派发责任；提供受信有限Observe/Step/Reopen测试入口 | 不调用旧Seed复制正文，不让旧Store.Source/Publisher替换真实Content；不重解释旧fixture记录 |
| `conformance/component/content_context*_test.go`、实际recovery消费者 | 公开1.2 Content及1.1 Decision/Command观察、独立原字节、受信dispatcher公开进度；独立黄金及有限故障出口 | 不以私表SELECT、内部mock调用数作业务oracle |

fixture采用**新增独立context记录/命名空间和装配**，可复用已拥有的连接/短Tx/lifetime机制；若沿现decisionfixture owner迁移链落表，追加 `0003_context_dispatch.sql`（最终编号需复核），仅追加迁移注册，不改0001/0002原字节或旧Seed/Source/Publisher语义。这里是新fixture职责，不把旧对象库换名当Content。若实现改用独立owner schema，其新迁移链须明确命名和升级边界，不可修改旧0001来塞新表。

新表只存受信输入、准确Content绑定及有限派发/恢复事实；正文仍由Content发布。原fixture输入可保存完整有界目标等测试前态，这是其真实权威输入，不是第二份冒充Content的shell/material正文库。Content owner、fixture输入/派发owner、Decision owner各有自身提交边界；adapter不是第四个虚构owner。旧03独立scope继续原装配，不热混旧Prepared与新Publisher。

## 3. 输入、身份与事务接法

1. fixture自身短Tx固定完整输入revision（goal、conditions、control、budget、deadline、所有有限未结责任），预留原Snapshot/Task/component/install身份、原DecisionRef/CommandID、策略/容量/输出预留/limits/deadline。非空unknown责任必须真实存在于该fixture输入；未知额度/费用不写0。读取所有页/水位，不知道不能当空。
2. Compiler有限读取准确Content，read/process分别获准；包括只影响选择/最终丢弃的来源，实际processed累积不漏。最大3轮及原绝对deadline/累计读取上界，不因重开刷新。M闭合canonical/no-number格式保存完整采用字段；不含自身hash，也不含后来manifest hash，消除自引用。
3. 本地先规划全部准确字节和引用，形成M→shell/manifest→lock的无环图并测量；M直接来源为compiler实际处理集合，manifest/shell继承真实输入及中间材料。每个派生版本的完整已登记闭包都≤64（含该版本祖先中间节点）；不只检查直接sources，也不把互不为祖先的所有对象机械算成一个版本闭包。明确先后顺序，不能让lock与manifest互相作为来源造成环。
4. 只在预检通过后经真实Content Put/原Command查询及发布读取形成bundle；目标save与全部适用来源read/process/save在Content最终Tx重新核验。每份body/put的命令、ref、声明/来源/purpose、原保留及期限在发送前耐久登记；timeout继续原身份。预检失败无成功Snapshot/Decision；若读源或已启动的必要派生产生费用/暂存，仍保留其真实责任，不声称“完全没有任何工作”。
5. Content确认后fixture自身CAS原输入revision，原SnapshotRef→shell/M/manifest/lock准确ContentRefs、投影摘要、原完整Decide请求、稳定Permission与dispatch责任/必要Job同Tx。提交未知先读原记录核对，不凭重开最新输入重建原请求。CAS失败有限重建或明确缺口；已发布旧bundle保持准确来源/保留/清理责任。
6. dispatcher Step锁原责任并fresh DB time/current输入控制后取得有限发送资格，Tx外原bytes调用真实Decide；失响应用原Command.get或原bytes重传，不换键。原回执优先与current reader沿03实际语义；不把新准入AcceptBefore当旧receipt可读期限。过期/撤权后只核对及关闭/原控制责任，不续原执行资格。
7. Source/Authority后续逐次核对固定绑定与当前fixture资格；限额/deadline必须与保存的完整Decide输入一致，不能只比Permission已有字段（Permission没有完整limits）。M/shell条件投影与goal/control/task/component/use逐项校验；同ref不同mapping冲突，新版本不替旧版本。已派发后控制变化使用03既有控制/维护责任；不宣称跨owner瞬间撤销。

PlanPublication稳定规则需在装配固定：由原permission/Decision publicationKey和明确publisher配置派生固定长度不透明ContentID、version1、准确hash/length/media；原tuple含sources/purpose/原retention。worker已有key为tenant/owner/decision/InputDigest再加artifact或proposal后缀，不改它。Plan只验证/规划不出版；Publish真实accepted仅证明Content责任，只有published并准确回读才返回成功。preparing/unknown在有限ctx退出让原Prepared恢复；不新rule start收费。Content完成后Decision仍有自己的Finish Tx，任何一边unknown不得互相冒充提交成功。

## 4. 字节/权限与可执行输出上界

- `I = len(shell.Raw)+len(lock.Raw)+len(manifest.Raw)+Σlen(actual MaterialRefs)`；default `I+8192<=65536`，并满足原MaxInputBytes。metadata不能只测M；manifest又嵌入完整typed Snapshot，须实际编码后计数。
- `O = len("fixture result: ")+len(M)+len(v1.1 Proposal)`；满足8192预留、原MaxOutputBytes；1MiB wire/public Decision及256KiB单Content上限独立检查。材料1..63、条件1..64、各实际closure≤64，全部同时成立。
- **推荐具体预检**：adapter assembly内限定rule/2的私有尺寸器，使用既有v1.1类型/编码器编码一个明确仅用于计量的candidate_result形状。填入全部固定Decision/Snapshot/revisions/processed/RequirementRefs；artifact ref的owner/id/version/media/byte_length取已规划实际长度，hash用固定长度合法hex占位即可（转义长度相同）。每个条件的Evidence都带该ref，空delta/limitations与原规则一致。它不是实际Proposal、不得发布或作为业务证据；产物仍只由worker生成。真实返回O必须≤预检界且公开解码成功，独立黄金与真实worker对照防尺寸器漂移。不能复制完整worker/Prepared算法另跑一次假Decision。
- 完整请求/public Decision上界也用准确编码及有界修订/usage表示检查；只检查Proposal长度不足。无合法有限上界则编译失败，不派发“试试看”。所有长度计算用字节及溢出安全整数，不用字符数/float/tokens。
- Source额外读M验证投影、compiler处理、adapter读取shell/lock等分别保留真实I/O/处理观察；旧DecisionUsage只保留其既有计量口径，不伪加也不遗漏后声称总系统I/O。返回Snapshot.Raw必须是真实shell字节。
- 每阶段purpose在fixture配置/原声明中准确固定：compiler处理、rule.input、publication保存、验证主体披露分别核适用政策，不能把一个purpose获准自动翻译成另一个。固定选择能覆盖全部来源的用途，空交集拒绝。
- read/process内部入口与public read/disclose分开；内部处理不要求额外disclose作为默认政策，公开artifact验证主体则须全部适用read/disclose。保存另核save，sync未被使用不自动推得。已获准读过的内存不能保证瞬时撤回；最终保存/输出当前门禁仍必须执行。
- 原worker公开processed保持manifest+材料；shell/lock作为结构/绑定读取另准确记录，不改冻结1.1字段含义。锁不能夹带未进入M/manifest来源链的额外业务材料或独立选择事实。来源集合在每个实际Content派生上登记，不能只把上游refs写M正文而绕过02闭包。

## 5. 七AC与独立观察

| 票03 AC | 正常公开出口 | 拒绝/恢复的独立出口 |
| --- | --- | --- |
| 1 领域/端口 | fixture输入Observe→compiler→真实Content→真实Decision；静态import边界 | 当前输入修订竞争CAS拒绝旧bundle；重开原责任不成为Task服务 |
| 2 完整canonical M | 独立手工/审阅黄金含完整goal、多个必要条件、control、有限fixture预算/期限、非空unknown责任；公开Content全字节读回 | 未知字段/错投影/遗漏必要部分拒绝；Unicode/转义/十进制/排序黄金不从同一待测encoder生成预期 |
| 3 真Content接线 | Decision.get的Proposal/ref→content.get artifact，去固定前缀全字节等于M；shell/manifest/lock独立读回 | 源ref/hash/映射错拒绝；不能落旧Seed正文表使测试绿；正确fixture并列可完成 |
| 4 全容量 | 独立计算I/O数值与真正DecisionUsage/实际输出对应，另报告额外I/O | 下表每类边界独立，wire/Content/集合限制不被64KiB策略掩盖 |
| 5 overflow不派发 | 每类可容纳控制实际Component完成 | 返回context_overflow、fixture无成功Snapshot且dispatch封闭；Component授权查询原Decision/Command为not_found，原发送边界观察无接纳；只数内部mock或Step==0不够 |
| 6 固定恢复 | 三owner重开仍同Snapshot、manifest、Command receipt、ContentRefs、原输入digest/Prepared发布结果 | Content已接纳失响应及Prepared后Publish返回超时分别恢复原identity；对比usage rule_starts/fixture收费不增加（仅Prepared恢复适用）；pre-Prepared崩溃则保持03真实启动收费规则，不强求总费用1 |
| 7 全processed | 两真实源实际处理，其中一项仅影响选择；M记录两者、公开可披露的来源观察/受信Content来源观察完整；Proposal精确为worker实际材料集合 | 第二源撤权后新处理/保存/披露分别拒绝；规则正常有请求/overflow无请求；真实模型未装配，不制造Provider计数证据 |

Component not_found必须由当前获准reader观察，不能用forbidden/unavailable冒充不存在。为overflow场景设置独立可信有限查询授权，不为了得到not_found而预建Decision/成功Snapshot。来源元数据若02尚只有受信Observe接口，沿该实际入口验证；不新增公共任意查源或私表oracle。

## 6. 每类overflow独立黄金方案

每个case冻结原输入/策略/准确版本和预期边界数值，具有单独within/overflow配对；拒绝case保证其他限制留有余量，必要时在**编译前**显式选择较大但有限的策略配置，仅为隔离非default维度，不能失败后自动扩限。

| 主触发 | 构造与断言 |
| --- | --- |
| 必要正文输入 | 所有字段合法完整，只增长必要约束UTF-8正文；隔离输出用预先固定足够reserve，总I边界两侧；不以删字段恢复 |
| metadata/manifest输入 | M正文较小，增加合法ref/条件标识长度与JSON需转义文本；实际shell+manifest重复开销越线，M自身仍可容纳；配短标识正常 |
| 回显输出 | default8KiB下输入仍合格，增加M直到prefix+M+Proposal越reserve；相邻可容纳控制 |
| Proposal输出 | M回显仍合格，用更多必要条件及合法长ref增加重复Evidence开销；单独证明Proposal而非只回显导致越线 |
| 材料数量 | 固定小body/足够容量，63材料正常（含M）、64拒绝；同时安排closure≤64或较早就明确closure拒绝的单独fixture，不能一个case假证两个触发 |
| 条件数量 | 64必要条件在预先足够的输出reserve下正常，65在本地格式/编译界拒绝；不得靠v1.1晚解码失败代替无派发 |
| 来源闭包数量 | 64准确祖先（包括本轮实际中间节点）可容纳，65拒绝；用共享祖先去重/不同版本/错fullRef对照，不能截取前64 |
| 原Decision字节与外层限制 | 固定较低MaxInput/MaxOutput分别触发，旁边合法；单对象/wire限制若必先于本地策略触发，明确归因到该层input_over_limit/格式拒绝而非伪称compiler已context_overflow。合法但超过compiler策略的输入才要求context_overflow；不为测试扩冻结wire上限 |

63材料正常与64闭包正常不能盲目共用有大量祖先的bundle：manifest/shell/M也会占祖先位。按真实无环图分别构造可达上限；若某装配中材料63必先触发closure，诚实用无派发的局部容量黄金证明材料计数，并用该装配实际最大可达正常端到端作对照，不虚构不可达的端到端63成功。

## 7. 采用门槛与交付范围

02退出后先重核：完整closure读取/门禁可复用位置、Manager与来源Observe实际接口、policy传播/保留单调语义、Content迁移最终编号/Step调度、所有current fullRef与时钟门禁。新增内部processing入口只服务本票compiler/Source两个真实消费者；不把WIP API当固定可用，不抢02所有者做并发重构。

本票至少真实正常全链、上述独立拒绝、原发布未知/Prepared恢复及fixture CAS竞争自身闭合，保留原有限生命周期/unknown资源责任；不把票04/05/06或whole广告/最终CI当其隐藏前置。原whole04 spec AC4由本票核心覆盖、AC5/6提供实际编译/来源基础并保留后续完整场景，AC1/2/3/7仍按已发布六票原分工完成；本票七AC通过不等于whole04七验收完成。不存在需要新ADR的领域改变；只补当前真实消费者必要接线。
