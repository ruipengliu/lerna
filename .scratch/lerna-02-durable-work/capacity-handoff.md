# 票 06：容量与公平的实际实现交接

2026-10-03。授权决策代理分析后采用；06仍等待05完成，未开始实现。直接前置仍为05完整退出。root读取基线为 `d89e78900f1a46b4b3ba6cfdc1bc5795feb20839`；05独立worktree仅为当前实现参考，不当作退出证据。依据已采用scheduling-decisions、issues/06、实际command/input/Job锁序、05 Start/Finish/Stop/Release和runtime.ScheduleStore。

本文只补实现必须一致的scope、锁序、原子边界与公平扫描约束，不重开lane/预算/背压/旧身份等已定行为，也不预定义Go接口。

## 1. 池的实际存储范围

本票最小落地范围取**同一真实数据库、同一PG schema或同一SQLite文件**。现有Store限定一个schema/file和准确Tx token，这个范围足以验证多租户共享容量。不扩展跨schema聚合事务、跨数据库或跨设备全局配额。

- pool identity由受信Host装配指定，并在此存储范围内保存配置revision。PG任何pool advisory key也必须包含验证后的schema及pool identity，沿已修正的锁命名原则；不同测试schema不能又共享池锁。
- 成员是最多64个准确OwnerRef，而非仅owner_id字符串。业务访问仍限定当前Tx的tenant+owner；成员关系只允许协调这些范围的runtime Job/配额事实，不授权跨owner读写输入/Projection/命令内容。
- 配额key为 `pool + tenant_id + lane`，同一tenant若有多个owner，其活跃Claim和队列等待必须合并考虑，不能每个owner各拿一份tenant额度。公平轮转也按tenant，tenant内再选择已声明owner的工作；成员数与tenant数不是同一概念，公平界N取竞争tenant数，必有N≤64。
- 每个owner在本存储范围最多属于一个当前活动pool，Job不按请求临时换pool逃逸容量。pool成员/lane改变不能使未结Job脱离旧计数。最小做法是有未结责任/有效Claim时拒绝删除或迁移成员，待drain后用准确配置revision修改。
- Job lane沿已定首次对象阶段绑定固定。读取05已保存policy时验证其绑定一致；不能在新record换策略时把活跃Job从ordinary改control。遗留无lane元数据的真实Job按既定默认ordinary接入；存在显式准确lane的工作保留其绑定，不能批量覆盖为ordinary。
- 所有成员使用同一pool可信时间来判断全池lease有效性；正常PG用同库时间，SQLite用同文件单写Host时间。测试的所有成员及Record/worker/replacement注入同一共享Clock，不准各owner用不同now人为释放全局额度。

初始化容量配置不能忽略已有05责任：原未结Job计入积压，旧有效Claim计入占位，不能安装一个空counter便宣称池空闲。并发上限低于已存在有效Claim时按已定规则拒绝配置并要求drain；队列上限小于既有积压可明确保留overhang，只封新增占位。

## 2. 建议采用的最小统一锁序

当前Record是 command key→input→Job；05 Start/Finish是input→ValidateClaim锁Job→schedule事实；Stop/Release也先持input。不能只在末尾扣配额而局部追加一个池锁。

**建议本票使用一个pool协调行的短事务排他锁**，统一包围本池容量/fairness变更。它按lane分别计数和轮转，但不需要为每个tenant再造一套多层锁。相较复杂细锁，这是当前≤64成员演示最小可检查的实现；普通执行在事务外，普通槽饱和不等于持有池锁。它不证明生产吞吐，后续有实际证据再细分。

统一顺序为：

`[原command key（若有）] → pool coordination → 当前input → 当前Job → 当前revision schedule/结果`

pool配置修改只取pool协调锁并读必要runtime汇总，不倒过来等待业务input/command。Gate推进若不影响容量可维持现有独立短事务；它不得拿着gate锁反向遍历并锁所有Job。纯查询不需要为了只读而拿池排他锁。

具体要求：

1. Record原键查找/原决定返回仍在pool之前；只有确需执行新业务裁决的路径取得pool协调锁。取得pool锁本身不提前裁决“满额”，业务前态与新接纳deadline顺序仍按既有合同。
2. Claim、Start、Finish、Renew、Defer/Release、Stop及可能改变Job未结状态/有效Claim的路径都应遵守该顺序。清理07只处理done且不改变容量，不应为了标gone再在input之后拿pool锁。
3. 若保留机制级storage方法，不准它在调用者已持input/Job时偷偷首次获取pool锁。应由实际消费事务在入口按统一顺序建立协调范围，或等价地保证该前置锁已持有；具体小端口/Tx记录由实现者选择。
4. 不把一个跨owner batch放进某个owner token后循环操作其他owner业务。每个实际Claim仍在选中成员的准确owner事务里完成；pool协调可以读取声明范围内的runtime汇总，但不能借此伪造token owner。
5. 每次事务的扫描/领取数量有上限，不持池锁等待计算、timer、网络或用户。所有锁等待有限，等待后重新取得可信now再判断lease/额度。

明确要避免的死锁：A已经持pool，等待input X；B已持input X/Job X，完成时再等待pool。仅给Claim或Trigger加锁而不迁移Finish/Stop顺序就会形成这个环。不能把deadlock重试当作正确锁序的替代。

如实施者采用lane细锁，也必须先给出覆盖配置变更、批量领取和所有释放路径的统一顺序；不要以三种lane分别过了单线程测试就宣称没有交叉死锁。当前默认用一个pool协调行即可。

## 3. Queue占位与新Trigger的原子边界

计数依据是未关闭责任，不是函数调用次数、job表总行数或内存队列长度。ready/leased/waiting/backoff占未结位置，done不占。当前Job可以复用，但done→ready属于新增未结占位。

在同一个Record事务和池协调范围中：读取准确原Job状态；判定这次Trigger是否新增占位；若需要则按lane队列上限判断；保存输入、policy、原Job修订及固定receipt共同提交。可以先临时更新再因背压返回错误使整个事务回滚，但不能吞掉错误继续保存applied，也不能把new Input或Schedule留在另一个事务。

已未结Job的新revision占位delta为0，不因满额拒绝或丢失；完成旧revision时若仍有更高work_revision，位置仍为1，不先减计数再尝试重新入队。真正全部关闭才释放位置。done Job的新显式record若池满可以在接纳前临时背压，原command之后原样重试，不保存财务budget_exhausted或另一固定决定。

原command重传、已固定拒绝和同键摘要冲突不获取新队列位置。容量变化不得改原receipt，清理标gone也不删除其去重依据。

若用SELECT COUNT而非counter，必须在所有相关变更共享的协调锁下计数，再变更，否则READ COMMITTED下两个并发新对象可同时看见最后一个空位而超额。若用counter，必须覆盖上述所有delta和回滚；不能把缺额先扣在单独事务或进程内变量。建议先用真实状态的有界scope索引汇总，避免无需要的双份账本，但实际性能/扫描预算由真实测试确认。

## 4. Claim、Start与释放的配额原子性

有效占位按库中当前绑定且lease_until>可信now的Claim计；pool×lane与tenant×lane都检查，不允许只检查其中一个。一次新Claim/接替及其计数/公平分配序号同事务形成；响应未知时沿原Job/Claim事实核对，不先在内存再分配第二份。

在pool锁内确认全局及tenant额度，再对准确Job执行已有input→Job条件领取；失败/跳过候选不增加“成功分配序号”或活跃计数。候选扫描结果是建议，不是预先保留的许可，真正领取必须重查due/lease/work_revision及pool当前config。

Start保留05的真实门禁，在它的短事务中再次核验pool membership/lane、当前配置和原Claim有效占位，再记录attempt并开始处理。06不能新增一个“带quota的Claim但无quota Start”的半实现。等待gate、退避、取消、永久关闭、完成或Release都按原Job/epoch/revision释放对应占位；失败旧消息不能释放新epoch的槽。

过期Claim的处理有两种等价表示：从有效Claim状态按now计算占位，或在协调事务中回收其reservation。不得只给新worker腾内存槽却把库counter泄漏。Renew不再增加一个占位；租约过期之后不能通过Renew复活原Claim。下调并发上限须在同一pool协调事务核实有效占位并按已定规则拒绝不允许的下调。

工作过程仍不持pool/DB锁。池上限是有效Claim/启动资格保证，不是操作系统能强杀过期旧worker的证明。已有05真实Start/Finish/停止与deadline检查不得弱化为仅Claim时检查。

## 5. owner消费者与pool调度的连接

现有Worker绑定一个OwnerRef，仅反复调用某一owner的Claim并不能证明跨tenant公平。Host需有当前pool成员的有界装配与执行机会，或等价的可信调度入口，在公平选择后调用选中成员的实际worker。不得因“下一个tenant暂时没有进程来抢”就允许热点tenant无限消费下一轮；也不能选择了另一个tenant却在当前owner token下直接修改其Job。

调用方自行指定要处理的worker/owner不是公平分配依据。pool在数据库中校验当前分配机会，多个worker竞争同一个pool时遵守同一持久次序；不能各自在内存里维护一个看似公平的round-robin。

三个lane必须有实际可运行容量与有限事务机会。证明ordinary已占满的是其有效Claim，随后控制/核对对象经真实Start和Finish产生准确hash；仅观察三种标签或不同配置数字不算验收。单个pool短协调锁可以串行几次领取，但不得在ordinary计算/等待期间继续占有它。

## 6. 64上限、SKIP LOCKED与公平轮次

64个pool成员、每次最多64候选只限制单次内存/扫描范围，不能推出剩余队列不存在或已经公平。现有Scan按due/Job排序取第一页；遇到TryLockInput失败或Job SKIP LOCKED，06必须让有界游标前进，否则前64个被锁住时第65个健康对象永远不可见。

最小要求：

- 每lane持久保存公平轮转状态；tenant内对准确成员/候选使用稳定keyset和有界页。跳过input或Job锁也推进候选游标，失败页不能每次重回同一个前缀。
- 单页没有成功Claim不等于该tenant没有健康工作。完整一轮检查的边界/游标回绕须明确，重启不能总从阻塞第一页开始。队列容量有限，但扫描仍需page上限与有限事务，不把全部body/Job对象一次加载内存。
- 沿已定N次成功分配机会界：其他tenant在该轮已获得分配后，不能因为某个tenant还在遍历有界页面，就不断获得第二、第三轮机会。对于正在发现候选的公平轮次，持久保留其未解决位置，跨若干有限scan继续，直到找到候选或在该轮有限视图中确认不可领取；不要把“尚未扫描到”当作资格不满足。
- 在假设固定有限竞争集、目标持续合格且无长期锁、pool/worker健康下，一个tenant得到机会后移到尾部；批量请求必须重复逐次公平选择，不能一次limit=64让热点tenant独占整批。新加入/重新合格tenant放当前尾部，重启不清零既有分配。
- 游标/轮次必须应对due变化和新增Job：用明确的轮次边界或等价有界规则避免不断插入让一轮永不结束。候选变化可在下一有限轮发现；不能宣称仅一个LIMIT就证明并发任意积压的墙钟公平。

这里没有新增比原决定更强的无限竞争保证，而是要求实现别用第一页结果偷换其N轮前提。若算法实际上只能证明“经过若干分页轮才开始公平”，应先把候选发现界和成功分配界分开记录，不能未经说明写成N；优先实现上述不让已服务tenant越过尚未解决轮次的最小约束。控制/核对在各自lane推进，不应被ordinary的一轮分页当成长事务阻塞。

## 7. 06须观察的最小竞争反例

两真实DB复用同一行为suite；PG需要多连接/worker竞争，SQLite通过当前受控单writer调度多个worker，不绕过文件排他。

1. 剩一个queue slot时两个新对象并发record：至多一个新责任获applied，另一个临时背压且无部分input/schedule/receipt；释放后原身份重试能成功。
2. 满queue中同一未结Job新revision仍保存；旧完成遇新revision不减成空位、不丢新责任。done→ready与另一新Job竞争准确占一槽。
3. 剩一个lane/tenant Claim槽时多worker并发：有效Claim不超额，落败者不消耗公平序号；tenant有两个owner时总额仍为一份。
4. Claim/Start/Finish/Stop/Record互相交错，按真实同步点验证无反向等待锁环；取得Claim后撤权/取消/配置变化/到期，Start不能旁路。旧epoch迟到Release/Finish不释放新槽。
5. ordinary饱和时control/reconciliation实际完成；一个tenant有大量ordinary工作时另一个持续合格tenant在明确N机会内取得并完成处理，重开后轮转不归零。
6. 对一个scope制造满一页的可跳过竞争前缀，后面仍有正常候选；经有限分页/游标取得健康候选且有正常对照。不能靠无限重试等待前缀恰好解锁才通过。
7. 不同PG schema的相同pool/owner/job名字互不形成确定性共享锁；同schema同pool的独立Store实例仍共享真实容量。未知/错owner/错pool和跨DB token拒绝。
8. 配置revision竞争、成员删除/迁移、容量下调及初始化已有积压的行为准确；缺配置不自动默认为无限，不把临时容量错误记成财务拒绝。

观察通过Host调度事实、公开原receipt和实际Projection，允许明确池配置/有效Claim/分配序号观察；不直接查私有业务表或数内部调用次数。N机会界不是无条件墙钟SLA；假设、实际bounds、数据库/驱动版本与正常对照一并记录。

## 8. 交接边界

05结束后以实际代码复核ScheduleStore/consumer调用、lock ownership和migration编号，再实施06。本文没有要求新增通用pool framework、泛型Repository、全局跨库锁或新的公共协议。新增SQL/索引使用下一个未发布迁移，不能改v1/v2及已发布的05/07脚本。

root负责最终整合05/07/08/06的门禁与迁移一致性；06自身只按05退出启动，07/08无新隐含依赖。本准备报告不宣称上述容量、公平或死锁反例已经实现/通过。


## 9. 追加决定：所有实际 Host 消费入口统一经过容量门禁

2026-10-03 只读补充。核对05 draft 的 host/durablework.NewScheduledWorker、demo.Service.Record 和 Worker.Start：目前检查有限policy/worker permissions/Claim，但尚无pool配置。该差距属于06应交付行为，不能只新增一个 NewPoolWorker 而保留同等成功的无quota处理路径；本文不表示05未完成自己范围，也不提前启动06。

### 统一入口规则

**06集成后的每个实际演示消费者 owner，必须属于当前存储范围内一个准确、已耐久安装且有限的pool，才能产生新的业务接纳或开始处理。** owner registration、lane容量和tenant×lane配额来自受信Host配置，不来自record正文、调用者可选的临时pool或worker内存默认值。成员最多64个、owner至多一个活动pool、scope及配置revision沿本文前述规则。

这适用于 Service.Record、已有 NewScheduledWorker 产生的 Worker、Claim/Start/Process/Step/Run、Finish/Complete/Renew 等所有真实处理路径，不按新旧构造函数名字区分。允许移除不再适用的旧装配入口，或者使其装配相同强门禁；不得留 `quota == nil → 不检查`、宽松接口类型断言失败后继续、调用旧Complete直接提交Projection，或“仅供旧测试”的成功旁路。构造时尽早检查依赖，但不能仅在构造时验证一次配置后永久信任；事务内必须验证准确有效绑定。

raw runtime storage ports仍可作为受信低层机制存在，不能把所有纯函数都变成授权入口。纯 Project hash 可独立调用；它不授予保存Projection的资格。单独调用底层Claim获得一个看似有效的Claim，也不能绕过消费者Start对pool、配额占位、有限policy及worker资格的检查，更不能直接经Host Finish伪造处理成功。完整门禁必须在真实consumer内部，而非仅在方便构造函数外层。

### 缺少配置时的准确行为

1. **查询继续可用。** 已有原receipt、gone/固定去重事实及只读工作观察仍按原鉴权和准确owner读取；配置未安装/暂时不可读不能变成command not_found或业务已失败。读取本身不要求获得队列或Claim容量。
2. **原Command重传先裁决原键。** 完成当前受信主体与权限验证、锁原键后，同摘要返回原固定receipt，异摘要仍为idempotency_conflict。这两条不依赖pool注册、不再占位，不刷新policy/deadline，也不恢复清理正文。不是免鉴权重传。
3. **确实不存在原键的新Record缺少准确有限pool配置时，临时失败且不形成新固定receipt/业务revision/Job/policy。** 包括已有未结Job的新revision：虽然占位delta为0，仍必须有可核验的池归属。待受信配置恢复后以原Command身份重试；不得把配置故障固定成rejected、budget_exhausted，或声称commit_unknown。缺配置前态被确认时就是没有执行新接纳；若实际COMMIT确认丢失，仍沿既有真实unknown规则。
4. 配置前置判断应在原键去重之后、形成任何新的固定决定之前；原有业务deadline/expected_revision和已配置时的满额裁决顺序保持。实施者应按最终Admit接法保证该位置，不在入口加一个把历史重传也挡住的全局检查。缺配置公共边界可沿既有dependency_unavailable，内部保留可判断的缺配置原因，不添加公共1.0枚举。
5. **Claim/Start缺配置必须显式失败，不能把它表示为正常空队列/闲置。** 不领取、不递增attempt、不首次绑定legacy执行倒计时、不调用实际project处理。Start除pool/lane/tenant资格外须确认此原Claim拥有数据库可核验的有效占位；不能仅凭“总数似乎未超”接受没有登记来源的任意Claim。
6. **Finish/Complete/Renew也不得成为缺配置时的成功旁路。** 处理期间配置应由既定禁止带未结责任移除成员的规则保护。若配置异常丢失/不可读，则不得保存新的成功Projection或续租，不把未结责任丢弃/伪装成功；保留责任待配置修复/租约到期后恢复。安全释放、停止和期限关闭继续遵守原绑定及统一协调锁，不为了释放资源新造pool或将旧epoch释放算给新epoch。正常清理done正文仍无需新槽。

“pool已配置但满额”与“pool缺配置”不同：前者对已有未结Job的delta=0新revision仍按既定规则可接纳；后者没有可信scope就不能开始新的裁决。两者都不覆盖既有固定receipt。

### 升级、正常装配和现有suite迁移

06部署沿用已定有限排空/隔离旧binary，不能让未检查quota的旧worker和新worker在同一scope混跑。SQL迁移只建立表示，不从已有租户行猜测权限或静默创建无限池。受信Host显式安装有限配置并登记准确成员，安装时把已有未结Job、waiting/backoff及仍有效Claim纳入真实占位；已有quota规则不允许的配置必须拒绝，不能以空counter或删除旧责任获得通过。

新schema但尚未安装pool时可以读取历史事实，不能处理。安装后legacy责任沿原Job继续：保留ID/revisions/epoch/原lease；到期接替而非抢夺，已有05绝对deadline/attempts不刷新。尚未绑定05policy的真实旧revision，只有在已配置pool内第一次合法接管时才绑定既定T0+5分钟；单独安装pool、重启或失败Claim都不启动/重置其预算。有效旧Claim须被安装过程准确计入，升级后不会因为“之前已Claim”而免除新Start资格。

可信正常Host装配及测试fixture可以显式安装有限默认pool，用于迁移现有01–05/07/08正常路径。默认是配置便利，不是nil兜底：必须持久保存，可经Host观察到pool identity、准确成员、配置revision和上限，配置动作具有明确控制权限。若提供单owner演示默认，可采用队列ordinary/control/reconciliation分别64/16/16、并发4/1/1、该tenant配额4/1/1；仅登记调用方显式给出的准确owner。多owner/tenant或特定竞争suite传入其自己的明确有限配置，不能隐式为每个请求另建池来规避共享上限。

历史v1/v2 writer和artifact保持其真实历史行为，不往老writer回填06能力。恢复后的**当前**Host在处理前显式安装配置；新接纳的普通测试同样装配配置。配置安装失败须让测试失败，不能skip处理或使用私有dummy quota。07/08本身没有因此增加发布前依赖06；当06整合进入root时，由06/root迁移其当前运行入口并继续跑原真实套件。

### 必要可观察反例

两真实DB补充同一组入口级检查：

- 未安装pool，新Command不能留下固定receipt/业务责任；安装后原身份正常成功。当前配置失效时已存在同摘要重传仍返回原receipt、changed摘要仍冲突，合法只读查询仍准确。
- 只提供05 permissions/policy、使用旧构造路径或直接调用已有Host Claim/Start/Finish，不能获得无quota成功。允许正常完整配额路径先产生Projection，再对缺配置/无有效占位/错误pool逐一拒绝；不得拿未Start的work使所有负例假通过。
- pool重开后仍有原有限配置、成员和占位；同schema多个Store/worker共同受约束，nil配置不能各自形成独立容量。成员上限、重复owner、错tenant及配置revision冲突显式拒绝。
- legacy实际pending在未配置时仍可查且未被丢弃；安装后经真实Start/Finish完成原责任，已经绑定的期限/次数保持，历史receipt不变。已有有效Claim计入容量，并具有过期接替正常对照。

以上是06所有实际消费者的统一成功条件，不新增公共方法、不指定Go签名、不把mechanism端口误当生产入口。具体配置装配与最小内部端口由06在05退出后的实际代码上实现。
