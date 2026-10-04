**正式采用：2026-10-04。** 前置01–03完整退出，准确03退出提交5fbb1a020658093266b4346370571370435f9630、受CI验证47ebce1/run37194868564 success。用户已授权按建议决定粒度/依赖并实施，不重复确认。root全文读取条件草稿和Astra high的具体接法，并实际核对最终exit SHA：与1aa条件pin相比仅12metadata路径改变，全部真实产品/工具/Schema/测试对象相等。下列原准备时pending描述保留为历史，当前接法由本采用及final-api-handoff确定；此时04尚无实现/验收证据。

# 04 准确内容与 Snapshot：实施前决定草稿

2026-10-03；仅 /tmp 准备，03尚未发布/实施，04不得启动。依据04 spec、CONTEXT、ADR0006/0007及 capabilities/data-model/contracts/governance/deployment，另对照 `/tmp/lerna-03-decisions.md`、`/tmp/lerna-03-storage-handoff.md`。当前实际仓库仍为02实施阶段，没有 Content/ContextCompiler/03组件可供宣称已复用。下列选择须在**03整片退出的最终SHA**上复核实际合同、源/发布端口、Tx/Job/容量及故障设施后采用；不是新ADR或已实现承诺。

## 1. 版本及真正公开边界

草稿选下一准确合同 **1.2.0**，只新增当前实际的 `content` profile（content.put/content.get）及能读取其固定回执的command profile。沿用既定ContentRef全部字段和准确整数/UTC规则，旧1.0与将冻结的1.1源、行为、广告和黄金均不改。Go/TS新版独立入口、生成物和Schema缓存沿03最终方案扩展一个明确配置，不造任意版本注册平台。完整能力才广告；包版本不冒充合同版本。

ContentRef 的 `(owner,content_id,version)` 是不可覆写的准确版本键；hash/media_type/byte_length全部参与核验，不是随意附加提示。首版Content version使用正整数十进制字符串，不要求客户端版本连续，也不提供“latest”含糊读取。版本不同是新内容事实，不能覆盖原版本；同版本同字节但不同来源/用途声明也不能偷偷更换来源政策。Command键与Content版本键分别去重：原Command异摘要冲突；新Command同版本同固定声明关联原发布；新Command同版本异声明固定version_conflict。版本不是当前状态revision。

`content.put` 采用有限内联字节的首版，避免另造上传会话协议：payload包含准确Content声明、sources、purpose，以及严格规范RFC4648 padded base64正文；解码字节最多256 KiB，线上总包仍≤1 MiB。含空白、非规范padding、错误hash/byte_length拒绝；byte_length是解码原字节长度，摘要是原字节SHA-256，不能散列base64。每次最多64准确直接sources，重复拒绝，继承来源闭包也须有限（首版64，超限拒绝不截断）。普通ID/字段闭合约束沿旧版。大于此上限显式input_over_limit；当前不提供流式、大对象分片或任意外部URL抓取，后续有实际需求再扩准确profile。

这是针对本片的小材料传输上限，不把PG变成最终对象正文库：接纳事务保存**有界临时staging字节**、准确声明、准备事实、固定accepted及必要Job。staging是恢复责任所需的真实暂存holder；其来源/保存授权、清理与保留也要登记。不能accepted后仅把正文留在请求goroutine或内存队列。发布确认后通过耐久清理责任删除PG staging；不得为新一次尝试重新持久复制无数正文。

put fixedaccepted只代表原准备责任已提交。hash/长度在接纳前可验证时直接拒绝；对象介质回读核验失败是随后发布失败，不倒改accepted。失败种类在1.2区分固定拒绝与异步发布状态；不把新错误强转旧ErrorCode。初始闭合发布观察为preparing/published/failed；对当前被政策关闭或正文清理另用可读性/清理状态，不能因撤权把历史“曾published”抹去。

`content.get` 按准确ContentRef和purpose只读；准备中返回preparing、失败返回已记录原因、published且当前获准时返回准确字节、正常清理后返回gone及可披露最小元数据；不可用/完整性损坏独立于not_found/gone。首版选择直接返回有限base64字节，不发可绕过即时撤权的长期对象URL。可选范围使用准确offset+length十进制字符串，先验证整个有界对象hash和length后切片；若首版不实现range，必须在机器方法形状显式不接受它，不广告支持。这里默认实现有限range，空内容仅允许无range或明确零长合法边界，拒绝越界。

查询授权先于披露存在性/字节。给定ContentRef字段与保存声明不一致不能返回另一版本。查询失败不创建Job/修补字节；恢复由原发布责任做。旧command.get只在原回执无损可表示时桥接，否则沿既定unavailable；准确新版能读取新版固定拒绝。

## 2. 实際对象存储选择与证据界

本片默认选择 **文件系统支撑的不可变对象适配器**，独立于PG元数据，位于adapters/objectstore的实际实现。它以不透明key的Put/Read/Delete承担对象字节，不是内存fake，也不要求部署S3协议服务器。使用Go已有能力，根路径显式受信配置；Linux本地持久卷是当前验证范围，不声称云S3、多机共享卷、断电或跨区保证。

选择理由有实际证据：环境README记录S3rver 3.7.1、file-backed以及PUT/服务重启/GET smoke。但只读其filesystem.js putObject可见createWriteStream和元数据写入，未见fsync/fdatasync承诺；其成功响应/GET本身不足以成为本片同步耐久确认。不要把已安装S3rver当作天然更强的发布保证，也不临时改它或加一套复杂VFS。需要S3协议兼容时可另测该adapter，但不是04必需第二实现；若改选S3为唯一介质，必须先给出真实durability条件及其证据，不能只换产品名称。

本地adapter必须真正执行：有界写入本轮临时对象→核验原字节hash/length→文件Sync→原子、不覆盖的final安装→包含新目录项的目录Sync→独立打开回读确认。中途错误不报告成功；EEXIST只在全部固定声明/字节一致时视为原对象已经存在。具体系统调用和安全文件打开由实施者确定，不自制通用文件系统。root必须固定为受信路径，key来自编码/散列身份而非用户路径，拒绝路径穿越/symlink逃逸；临时/最终对象可被有界准确列举并关联原发布责任。

删除也必须有确认边界：在准确原key上阻止迟到旧写重新安装，再unlink并同步目录。推荐在同一对象锁协议中保存耐久删除墓碑（原key不再可写），避免仅用DB Claim误称文件写已被隔离；可采用等价真实无重建保证。原版本gone后不能以原Command重传重新上传。进程内mutex不足以证明跨进程删除/迟到安装安全；测试须覆盖实际并发/接替边界。墓碑和最小身份不计作残留正文。

本片验证同步调用、真实文件、SIGKILL和独立重开后的字节，**不把SIGKILL当作掉电试验**。fsync到本机文件系统的保证及底层卷限制必须记录。此次准备没有运行这些实验，也没有启动/修改环境S3服务。

## 3. 发布、原身份恢复、孤儿与清理

Content属于自己的PG owner/迁移/版本账本/Job，不能把03 Decision表、02 demo Input或03 fixture publisher改名当Content。内部机制沿03最终真实端口复用；全部元数据、来源/holder、固定receipt和本owner必要Job同一短Tx。跨owner上传、读取、发布均不在持锁Tx中等待。

准备提交后，以固定发布身份与对象key保存字节；字节durable并独立核验后，新的短事务再次检查来源资格、当前发布状态与Claim，把published、准确storage binding、来源/holder及后续staging清理责任共同保存。尚未published的storage key不经content.get返回；原Command fixedaccepted不随发布推进改变。

对象写后、元数据发布前崩溃：新worker读原staging/声明及对象观察；同key正确字节且当前资格允许则继续原发布，不造新version；资格失效、声明不一致或放弃发布则记录失败与准确对象清理Job/负责方。不能看到“某个hash文件存在”就认定属于本次版本。

孤儿发现先从已提交的准备/attempt记录和本owner登记的有限对象prefix恢复；不要扫描/删除其他租户对象。临时key必须在写前可关联登记的发布attempt或可验证私有临时命名范围，迟到worker产物也需有可追踪清理路径。清理前条件锁定原版本进入不可再发布状态，再执行对象删除，最后确认结果；不能与另一worker发布竞争而删掉已引用的正确对象。已published引用由正文清理流程处理，不冒称孤儿。

清理是受信管理/生命周期seam，不新开公共content.delete方法。先收紧新读取/处理/保存/同步/披露资格并保存传播/清理责任；物理删除后再确认body-gone。保留最小版本/摘要/原Command去重及可披露来源缺口，不能泄露无权查看的来源ID。受信holder观察可报告cleanup_pending/residual/erased；离线holder不回执就保留负责方/残留与期限，不能报告全局擦除。真实正常删除必须移除PG staging及最终对象正文；不能只标gone。磁盘WAL/备份的法证擦除不在本片证据内，保留限制明确。

不扩大为通用传播系统：本片一个实际本地对象holder，加一个受控第二holder（确实写过独立字节，可模拟离线/删除失败）足以验证残留；仅虚构一行“某副本离线”不证明真实副本清理。重开后用其独立观察确认字节/删除状态。holder登记和恢复责任有界分页，不能只第一页判断零引用。

## 4. 最小授权夹具与完整来源

Content/编译器实际消费小授权端口，区分read/process/save/sync/disclose及准确subject、resource、purpose、当前policy revision/期限。nil/不可核验failclosed。受信fixture配置须显式、耐久、可收紧、与正文分离；payload的sources/purpose是待裁决请求，不是授权。

最小实现使用Content owner同库的**明确测试政策记录**及受信安装/撤销seam，能在最终发布事务内重读/锁定适用政策revision。它不是domain/authorization的Grant服务，不签假的生产GrantUse，不声称许可消费/真实预算/离线窗口已实现。生产装配不默认为允许所有；06切片以后替换/接入真实Grant，仍要复验真实发送门禁。

04首版支持同tenant、同Content owner管理的准确来源及已登记本owner派生闭包；跨tenant拒绝，跨Content owner派生明确unsupported，不偷偷访问对方私有表或声称分布式原子授权。当前范围内可对实际所有来源及测试政策版本做同Tx最终复核，足以验证撤权与来源交集；跨owner使用凭据由真正需要时的后续切片接入。

用途取全部实际处理来源允许用途交集；read获准不推出process/save/disclose获准。输出保留截止取适用来源最早期限，并受请求更短期限约束，不因每次派生/重启重新计时。空交集拒绝；缺少处理/保存权限就不能先生成/存正文再只封披露。不同来源政策会收紧派生结果，新请求逐次核验原来源当前资格；不能仅复制发布时的宽许可便永远放行。撤权后先封新使用，再持久传播清理，不承诺已披露字节撤回。

外部content.put的sources是受信主体提交的**来源声明**，接纳不能证明外部制品的真实完整生成过程。内部ContextCompiler/规则摘要的processed集合由实际有界读取/转换过程累积：包括读取后丢弃、仅影响选择、不出现在最终披露列表的材料；继承这些材料的已登记来源约束。disclosed refs可小于processed，不能反过来缩小限制。禁止把字符串搜索到的引用或摘要最终引用集合当实际处理集合。

不造模型摘要来“验证来源”：用固定版本的确定性摘要策略（例如有界提取）实际读取两份真实Content并生成派生字节，完整保留两者来源，即使只展示一项引用。准确策略、输入/输出hash与处理集合形成可回读派生关系；它证明自身过程，不能证明原材料事实真伪。

## 5. ContextCompiler、Snapshot以及未实现的Task边界

编译器放domain/task下的实际context子职责，消费明确小端口；不提前创建Task服务/Task表/Task终态。04的输入提供者与派发仍是03明确的durable fixture dispatcher，保存测试场景目标/控制修订、有限budget/deadline、策略和component lock；该fixture的当前状态不是生产Task权威。

实际材料读取/必要产物发布切换到本片真实Content adapter，不再用03 fixture publisher作为Content实现。Snapshot body是编译器形成的不可变文档，可作为准确Content版本发布；Snapshot的语义身份、目标/控制绑定及派发权仍在fixture dispatcher（05接入真实Task owner），不是Content owner决定哪个Task当前应该运行。

必须保留当前完整目标、所有必要条件、控制、预算、期限、相关未决效果；额外材料可以按固定顺序/优先级有界选取，保留省略清单和可见缺口，不能把必要事实标optional绕过上限。准确input refs、组件/策略版本、processed refs、派生关系和限制随Snapshot保存。保存Snapshot不能替代下一次处理的当前权限检查。

04先用明确的 **rule-fixture-utf8-bytes-v1** 容量计量：输入规范编码的UTF-8字节 + 明确预留输出字节必须≤该策略总容量，且受wire/Content上限；所有线上限额整数字符串。默认总容量64 KiB、输出预留8 KiB，测试可显式更小；这不是某供应商token数，也不冒称对应生产模型上下文窗口。强制部分已超限返回context_overflow、无Snapshot成功/无Decision派发；不能自动删必要条件或换更大未批准模型。有限编译最多3次重建且不超过原绝对deadline，总读取/处理字节有限；每轮真实处理来源仍计入该轮结果/废弃清理，不按每次失败重置总预算。

2026-10-04 条件复核已按固定 `1aa21fcf47352c4460877bb51fc04dc302cdbae9` 解决本项接法，详见 `final-api-handoff.md`；whole03最终CI/退出仍待，不据此启动04。**确定采用完整 canonical mandatory-context/1 Content 文档作为 MaterialRefs[0]，真实 Content-backed Source/Publisher 与原1.1 Decision接合；04不新增1.2 Decision profile。** 既有rule/2 candidate_result会实际读取并逐字节回显第一材料，可由公开Proposal+Content读回核对完整目标、条件、控制、预算、期限、未决责任和compiler全部processed来源。闭合Snapshot外壳/manifest保持原字段，准确Content字节及其不可变映射、完整派生闭包和当前资格由各自owner负责；不能只塞一个未回读的引用或把完整文档藏进未知Raw字段。

旧fixture Seed复制同owner/version1正文的具体实现不充当新Content；04新增实际adapter并用独立scope，PlanPublication按原key确定引用、Publish只有真实published并回读才成功，超时继续原Prepared/原身份，不重算收费。compiler processed与worker实际processed分别准确记录并通过真实Content闭包关联，不把摘要接收冒称原文读取。规则不解释强制约束或裁决Task成功；04证明完整传递及夹具当前门禁，05/06仍要接真实Task/Grant。

容量必须计入Snapshot.Raw、Lock.Raw、ManifestRaw、全部材料及完整回显artifact+Proposal。64KiB总容量/8KiB默认输出预留不承诺能容纳近56KiB的第一材料；已知回显输出不容纳也在派发前context_overflow，不裁强制字段、不让accepted后失败代替编译验收。更大输出预留只能在编译前显式选择获准固定配置并保持总上限。正式采用仍核whole03最终exit SHA及实际对象相等性；若实际端口变化再对真实差异决定，不开放静默扩旧1.1的任选方案。

### 提交时来源变化与原子边界

04的可强保证点是：**同Content owner的Snapshot字节最终发布事务**重核全部确切sources及政策revision；来源在此之前被撤权、到期、正文清理或标记不可用于本用途，则不发布未经核验的Snapshot内容，按有限预算重建或返回准确缺口。新版本出现本身不使已固定旧版本变成混合输入；只有“要求当前head”的选择前提、适用政策或材料可用性变化才需要重建。不能读完旧正文却把新版本ref/hash填入Snapshot。

fixture dispatcher随后绑定已发布Snapshot和自己的目标/控制修订时用自身持久比较前态；目标改变即不派发旧Snapshot。来源在Content发布后到dispatch之间再次改变，派发前及实际Decision源读取重新查当前资格，拒绝使用并保留明确缺口。各owner各自提交；不把这些有序检查宣传为跨owner同一瞬间原子Snapshot+Task准入。存在最终检查后撤销的普通竞态时，证明边界是各自检查/消费点，不是全球瞬时撤销。

**05必须再次通过真实Application验收Task修订竞争、Snapshot绑定/派发、迟到Proposal消费；06必须接入真正Grant及同库准入/使用规则。** 若04验收将“Snapshot提交”解释为跨owner Task+Content强原子当前视图，现规格与实际owner约束不匹配；不得用fixture共享内存锁掩盖。上述单owner源发布门禁+fixture自身CAS是本片准确、可实现的范围。

## 6. 零模型出口的诚实证据

03规则引擎从未调用真实模型；04不能凭两条路径都为零就声称验证了真实模型发送/计费门禁，也不能临时添加假模型调用以做漂亮计数。

AC4的强行为证据是独立Component接收边界：可容纳输入实际送达规则Decision并完成；强制溢出没有Decision接纳/派发，原必要信息未被裁剪。观察必须来自真实边界记录/原Decision查询，不能数编译器内部mock调用。真实模型出口未装配且不获凭据：记录事实为“本测试配置无真实模型发送；实际规则Decision边界正常/溢出分别有/无请求”。如已有03真实受控出口观察设施可验证零发送则复用；不得声称未知的未来Provider接口已经有计数器。将这一限制连同05/10后续真实模型门禁复验写进04退出证据，不静默把零数字当生产结论。

## 7. 实施前复核和范围限制

03最终SHA退出后重读：1.1 Snapshot/Decision准确Schema及错误，组件实际source/publisher/授权需求，原固定回执账本与reader桥接，Tx/Claim/配额/维护关闭端口，有限进程故障设施可见性。再锁定1.2机器合同和局部方法签名、对象适配器支持的文件系统原语、owner迁移编号与最小配置。

04需要真实PG+实际对象字节及重开/杀进程测试，缺配置/服务硬失败。用新单独scope且登记清理归属；不读取环境密钥文件、不触碰已有smoke bucket/volume。本文只读过环境README及无凭据的S3rver代码，没有连接/写入服务，也没有运行04。

未验证的包括生产S3、断电/跨区耐久、远端来源原子许可、真实Task并发、生产Grant/预算、真实模型token计量与调用。它们不能由本片本机真实存储结果替代。
