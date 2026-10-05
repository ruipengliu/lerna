# 04-03：实际 CPU 样本后的单次闭包身份计算复用

## 1. 本次采用的唯一产品改动

固定源码 `28e3a468c86d4323d3b497836de23bb311a58952`，WT `/tmp/lerna-worktrees/content-snapshots-03`。

**采用：仅在每一次 `registeredClosure` 调用内部，对成功的 `VersionIdentity(full ContentRef) → ID` 建立有界局部值复用，供其原排序和原 visit 同位置调用。** 相同完整 ref 已在该调用中通过原 schema/identity 计算时，不再重复同一纯计算。`VersionIdentity` 算法、公共函数、PG Store 的独立验证与全部业务/时间/授权门不改。

这已由当前 CPU 栈与准确代码重复两方面支持；本轮不需要再申请 profile 或诊断矩阵。它是最小等价优化，不是完整5秒容量已可保证的结论。禁止顺手加入第二优化（SQL合并、Input memo、跨事务 closure/政策缓存等）。用户授权的必要建议由 root 采用后交 solefixer；本报告没有改产品或运行任何 native/Go/PG。

## 2. 实际证据资格与精确差异

已读：`large-graph-worker-cpu-overlay/{profile-outcome.json,analysis-outcomes.json,commands-static.json}`；七份 `context-final-capacity-b-worker-cpu-analysis-01..07.log` 全文；159行 `context-final-capacity-b-worker-cpu-race.log` 全文；本代理此前全文读的非profile160行race及 `large-graph-binding-reuse-results.md`、原 `e344...` 下一测量决定。command文档仅作为记录阅读，未执行其env/build/pprof命令。

唯一 profile SHA256 `427bbc672bad5cf520934ed2e31116f75eca4991572fd5ecd0c3f7c3a4ff31e6`，144298 B，dev27/inode560161。原binary native3299533/start13781792/session30305实际exit1/groupAbsent；七次pprof分析各actual0/absence，root已STOP/RELEASE。原 `profile-outcome` 写的 `pprof_parsing_analysis_unrun=true` 是生成时cutoff，后续analysis-outcomes/七log才提供已分析事实，不把早期字段当最新状态或改旧文件。

**standard testing原profile FD logical Close仍UNKNOWN。** 后续独立descriptor fsync/Close、native Wait/groupAbsent、pprof成功解析都不能补作原Close ACK；原文件/责任继续保留，不授权清理。

完整profile：78.40s wall /62.18s sampled CPU。标签总6.80s，其中closure64 2.83、static62 3.97；55.38s未归入这两个标签，不能强归worker/某个DB调用。七输出百分比分母仍为完整62.18s，即便tagfocus也不能读成各case内部百分比。主要race/runtime/GC栈和off-CPU成本仍须诚实区分。

| 有标签累计栈（s，包含子栈） | closure64 | static62 |
|---|---:|---:|
| registeredClosure | .98 | .97 |
| VersionIdentity | .54 | .45 |
| ContentRef Encode | .51 | .48 |
| PG LockVersion | .28 | .42 |

这些累计栈嵌套、调用来源部分交叉，不能相加，也不能全算成本次可省时间。closure64 sortRefs累计.22s提供排序身份转换的直接栈证据；static62未列top30中的sortRefs不等于其成本0。样本支持“值得减少现存纯重复身份转换”，不支持“省去全部VersionIdentity”或预言确定毫秒收益。

本次profile改变了可观察失败边界：

- closure64已在装配/编译后只剩caller约3.38s，Step实际3.385588s，首Content.ReadForProcessing在30.001427s caller过期。最后成功Claim gate还显示原lease余4.95321s（那是早先gate），末次事务未取得新的成功DB gate。本轮应称 **caller30耗尽**，不能复制旧非profileStep5.073903/lease超16.702ms的结论。仅一次Material调用失败，无Prepared/Publisher。
- static62保留全部62材料、两Plan和原Prepared正向COMMIT（24.839706s），第一Publish在PrepareContent超时，Step5.091103s，末Claim晚29.191ms，caller仍余约4.844s；没有Content.Put接纳/ReadPublished/completed正事实。与此前非profilePrepared后失败是各自独立样本，不能混用时刻。
- 原非profilenormal完整4B allPASS与nonprofile race两个失败照原范围保留，normal Step3.708556/3.044059不能线性推算race。profile其余overflow/Content正常和拒绝/duplicate拒绝仍PASS。本次不是outer watchdog或DATA RACE green。

## 3. 精确代码重复与最小实现

固定源 `domain/content/identity.go:VersionIdentity` 首先执行原 `v.Encode(ref)` 完整验证，然后对owner/contentID/version闭合位置数组按原 `lerna-content-version-1` 算法hash，返回ID/key。输入只含值型字段，结果无时间、DB、权限或可变外部事实。

固定 `domain/content/closure.go`：

- :37 target的VersionIdentity；:49每次visit再次调用。
- :115 children和:127 roots先 `sortRefs`；:157 sort对每个ref调用VersionIdentity，随后遍历在:49对同ref重新调用。
- 已通过不同边到达的同fullRef，:49身份转换发生在:56现有visited map查重之前。因此已有节点去重不消除这些纯身份重复。
- sortRefs只有这两个实际调用点；不需要导出port、共享identity服务或generic memo框架。

具体默认：在registeredClosure栈内新建最多65条的 `map[v.ContentRef]string`（target+原64来源容量），局部 `identityID(ref)`：命中完整ref值则返回原ID；miss仍调用**原**VersionIdentity，err原样返回；仅成功且map未满时保存ID。map满时继续原函数、只不保存，不能新增input_over_limit或缩短原遍历/错误路径。target仍在原首次位置经该helper验证，可自然进入同一map。

让私有排序辅助接收该局部identity函数（或等价同文件局部sort闭包），仍先原顺序逐ref计算ID，再原sort.Slice比较相同ID字符串；visit同位置使用该函数。可以调整现私有sortRefs签名，它没有外部consumer；不要保留无人调用旧helper或增加interface/strategy。局部函数不是可注入业务身份规则，生产路径始终闭合到原VersionIdentity。

**只保存full ref →纯ID成功值**：键必须包括Owner两项、ContentID、Version、Hash、MediaType、ByteLength，不能以tuple ID、contentID/version、hash、指针或短摘要代替。相同tuple但不同fullmetadata会分别经原校验，之后原visited/ref mismatch仍返回integrity。map不存Record、Policy、clock、error、authority、whole closure或正文，也不把验证结果传给PG Store。

生命周期严格是一趟registeredClosure，结束即丢弃。即使同一Tx后来再次调用、另一action调用、同一material读后Tx、下一材料、下一Step、reopen，都新建空map；不挂Service/config/ctx.Value，不复用前一Tx或Binding memo，也不引入锁/全局map/LRU。

## 4. 原顺序与当前事实的硬保持

这里减少的是重复成功纯计算，不是调换失败门：

- target校验仍最先；sortRefs对len<2仍直接返回，不能提前校验单元素导致foreign owner与invalid metadata错误先后改变。
- 多元素排序仍按原输入次序做身份校验，不先owner筛选/预去重/截断/排序error；visit仍先tenant/owner拒绝，再identity、active/cycle、旧fullRef相等、64上限。已经成功的同值重用不会吞新错误；失败不缓存。
- 不移动任何身份校验到原DB授权门之外的新位置；不改变refs、records、active原标记时机、完整children、确定性输出排序与全部中间版本。
- 每个原本会访问的唯一来源仍在原位置执行Now→全actions CheckPolicy（PG锁后clock）→LockVersion→新Now→policy与retention/fullRef/publication/cap门；source metadata错误与current authority优先级原样。
- PG `LockVersion`仍独立VersionIdentity/原advisory/真实SELECT FOR UPDATE/返回列与JSON核对；不通过新domain helper把PG验证当已完成。CheckPolicy保持原fullsubject/actions/精确policy绑定、原MATERIALIZED时钟。所有Store decorators仍实际调用，无新增“命中即省端口”。
- Content外部真实Objects.Read/hash/length与读后独立完整Tx照旧；Current新SELECT/Input/control/Permission/原时钟、Binding隔离、原publication保存/PrepareContent/put/readback和Prepared/Finish门照旧。

此优化会影响registeredClosure所有实际消费者的CPU，不仅03测试；这正是必须保留其nil-actions结构路径及read/process/save/disclose各既有反例的原因。不能为03单case加fixture绕路或只关闭race instrumentation。

## 5. TDD与闭合方向

已有原非profile4B race是在原30/5/120边界的真实容量red；本CPU样本提供热点选择证据，不需要为了同一成本再造一次人工business red或新profile。源码未改，不能提前宣称该优化已让red转green。

由solefixer先固定最小等价反例/正常资格，再只实施上述局部复用：

1. 同fullRef从多根/diamond/重复边到达，完整输出refs/records/所有中间版本与原顺序一致；相同tuple不同hash/media/length仍拒绝，target/self/循环仍拒绝。nil actions不能突然调用政策；actions路径当前政策/clock和Store调用不得减少。已有实际测试覆盖就复用，缺少关键组合才补小测试，不重写通用图库。
2. 明确错误优先级的小型机械域测试：单根foreign owner且metadata非法；多根排序中非法ref；超过64、恰64、成功身份memo超过65容量时原错误/结果不变。memo容量只是内存界，不是新的业务限额。若验证纯helper命中次数，应标纯计算机械证据；无需新增公开计数/port或业务fake-privateSQL。
3. 受影响真实Content/Source current-authority、错fullRef、锁等待到期、post-I/O撤权与完整闭包边界测试；原Binding/current-input/control反例仍有效。测试不能用减少SQL/clock调用作“提速成功”oracle。
4. 在新准确source上以**非profiling、保原低开销phase记录**的原exact4B normal与独立race各执行，原30caller/5lease/120、原initial input/read/capacity/fee/RuleStarts1全部保持。不预热、不失败后renew/retry/换原scope预算；观察真实running/Prepared/两个Publish/readback/completed以及原拒绝尾部。测的是全链出口，不能只看单helperbenchmark。
5. 如果仍失败，保新firstgate/完整日志和原scope责任，按真实未闭合阶段再决定；本优化的等价资格与整体容量未达可同时存在。不得把本次累计.54/.45秒全部扣掉预报PASS，不能虚称未运行的publication为0成本。

Standards现有cause P2是独立必要错误原因保持工作，不把它当性能修复；若solefixer同delivery包含它，必须标清变更和各自反例，不能将合并后收益全归本memo。Root最终新pin复核/实际测试/CI仍独立。原profile FD UNKNOWN、所有旧失败/Prepared责任及unknown scope不清洗；不接受七AC或whole04退出。
