# 04票03：已定位 Source-before-Prepared 后的一次最小成本细分

**采用一次有限、原条件不变的子阶段测量；暂不选择产品优化。** 这次需要分清已经测到的两项成本：Current 的 SQL/严格解码/比较，以及 Content 读取的数据库资格/实际对象 I/O。它们各自都有显著耗时，当前材料尚未读完，不能把“最后失败在Current”误当“只优化Current即可使完整计算和出版进入5秒”。

静态源：`d0ccc114f1255f6118fb94b794a1342c93050fc4`，实际产品 `5e8cad7e9d77162fda9773bde747235f8937c9ce`，基线 `137311247276`，WT `/tmp/lerna-worktrees/content-snapshots-03`。实际5e8→d0仅ticket-03-evidence.md；本次相关Source/Content/Dispatcher产品相对此前b717也无变化。旧决定原archive `.scratch/lerna-04-content-snapshots/ticket-03-large-graph-claim/decision.md`保持不改。本文只读，无native、DB、实现或清理动作；05仍持唯一LOCAL资格。

## 1. 此轮实际确认的因果

全文读取 `context-final-capacity-b-claim-diagnostic-race.log`（158行）、`large-graph-claim-diagnostic-results.md`、overlay manifest/map、五文件对应诊断实现与diff。overlay map SHA256 `14d11ea59db97548877ffee5e41a10cb3f1372d054ba9f4b9bf860daf6a770ad`；root确认该次独占 native3086554/start12876878、exit1/groupAbsent，包74.623s。原B四测试、caller30/Lease5/Go120/outer120和原业务tuple均保持。

| 事实 | closure64 | static62 |
|---|---|---|
| Step开始→返回 | 23.734→28.845s | 17.988→23.094s |
| Claim DB now→原LeaseUntil | 00:36:00.249249→00:36:05.249249Z | 00:36:35.158114→00:36:40.158114Z |
| 原running/startsequence1正向commit | 23.897s，Prepared=false | 18.143s，Prepared=false |
| 第一依赖错误 | Access.Current deadline，28.8386s | Access.Current deadline，23.0794s |
| 同时caller剩余 | 1.161s | 6.921s |
| Finish时ValidateClaim supplied DB now晚于原lease | 21.425ms | 24.545ms |
| Material调用（含最后失败） | 25次，成功24次 | 31次，成功30次 |

因此本轮可以准确说：**原Claim-bound calculate/ReadMaterial尚在执行，Access.Current先返回context deadline，随后Finish的真实ValidateClaim在原lease之后拒绝，最终暴露ErrClaim。** 原caller30未到期，原payload/Permission绝对期限更晚；running已提交，但没有Prepared保存/提交，也没有worker Plan/Publish/ReadPublished。原RuleStarts1已占用，不能盲重试原Decision补第二次计算或当作零费用重来。

这更新的是新独占运行的原因，不把此前74.336s失败重写为同一已证明阶段，不把原B末段05并发错误洗掉。closure65预期overflow及其他两个测试类通过仍按原范围记录，不能抵消两个正常控制失败。

## 2. 已测成本与仍未知部分

closure64：Snapshot1.241s、Lock1.387s、Material25累计2.310s；嵌套Current26累计1.254s，ContentRead28累计3.511s。static62：Snapshot1.121s、Lock1.237s、Material31累计2.576s；嵌套Current32累计1.755s，ContentRead34累计2.970s。末次失败调用的耗时属于未完成观察，不能作一个完整材料的均值。155/173条诊断事件均dropped0。

这些是包含关系，不能相加成更大的工作总量。现diagnostic Content wrapper只包worker adapter的processing读取，compiler实际读取走另一显式预算reader；日志Put4/Step4属于四份编译产物，不是worker出版已执行。Compile+Bind14.337/12.241s确实消耗caller剩余，却没有消耗尚未领取的Claim；移动setup计时不能修复本次已定位的5秒计算窗口。

源码确认每次Current读取完整Binding（含Bundle正文）、closedJSON两遍式严格解析，再锁/严格解码当前Input、完整DeepEqual及当前控制/permission/time检查。Content ReadForProcessing则在真实对象读取前后分别进行新的完整target/祖先资格Tx。**尚未知**：Current耗时主要在SQL等待/传输、JSON解码、DeepEqual还是事务固定开销；Content耗时主要在policy/LockVersion/Now、对象Read还是其他校验。不能由最后错误位于Current推断主要耗时；也不能从当前未完成前缀估算全部后续材料和artifact/Proposal的准确耗时。

## 3. 采用的单次测量设计

沿已核原overlay增量做**一个子阶段拆分实验**，仍一次原B完整selector race/p1/count1，caller30/Lease5/120不变，新独立own scopes，soleowner等待root明确LOCAL交槽后执行。原两个失败scope不得被复用为免费重算。无需新公共port、生产tracing框架、业务缓存或SQL迁移。

### A. Current：在实际fixture内部划分连续耗时

临时overlay仅给ContextDispatcher/其Store显式注入本scope诊断收集器。在原函数原位置包时钟，不复制算法、不发额外查询：

- binding QueryRow+Scan（含实际锁/传输）和closedJSON分别计时，后者只记录输入字节长度；
- 当前input QueryRow+Scan和closedJSON分别计时；
- 当前完整Input DeepEqual/control判定、PermissionFor/完整Permission比较分别计时；
- 原trusted的clock SQL累计；原within的BeginTx、会话设置/首次clock、callback、Commit/Rollback分层记录。BeginTx含连接池等待，Scan wall包含服务器/网络/解码等待，不能进一步冒称纯DB CPU。

所有原SQL、顺序、错误、锁、当前资格与fresh clock不变。明确归属当前Current/Binding/Authorize，不把别的编译事务累积到Current。不为了测量去掉严格重复键/未知字段/尾随输入检查。

### B. Content：只包现有实际Store和Objects端口

在ContextWorld原Content装配中显式加窄转发decorators：Within（callback与最终结果分开）、CheckPolicy、LockVersion、Now及Objects.Read。每项转发原ctx/参数恰一次，不改变原结果。已有adapter.ReadForProcessing wrapper为每次当前worker读建立有限诊断归属，嵌套store/objects事件按该归属汇总；compiler/setup/独立lock读取分别标识，不能混进worker。诊断标签只存在于本scope显式收集器，不借context.Value携带业务资格。

按一次ReadForProcessing的真实调用顺序可把两个Within标为读前/读后，前提是这个路径确实出现两次；首Tx/IO失败就保留缺失阶段，不补零。每个Within下的CheckPolicy/LockVersion/Now是包含项，Objects.Read在Tx外单列；剩余时间标作未分解本地/事务开销，不能冒称hash或FS耗时。若同scope出现并发且无法可靠归属，必须标ambiguous，不用全局可变phase猜归属；当前两大图路径顺序执行，可以保持scope隔离的最小收集器，无需通用trace系统。

只需各固定stage的count/total/max、成功/失败分母、返回字节量、第一错误和原Claim门；不逐node打印、不开网络追踪、不输出内容/SQL参数/凭据。使用有限累加器，保留原critical记录；dropped/无法归属必须显示。按原真实owner记录manifest、源码及overlay hash、binary、native退出/groupabsence和所有scope；测量自身开销不可声称为零。

## 4. 何时足以选择一个改动

这个实验的交付是有归属的成本表和最早失败子阶段，不要求诊断版本green。只测到“还是5秒超时”而没有分解数据不算完成，不继续同样盲复跑。根据结果选**单一**最占主导且可证明等价的窄改动，再进入其正常/故障验证：

- 若Current严格解码/比较占主导，才评估消费方所需准确projection的减少重复表示/解码；仍每次从真实当前Input验证完整控制、fullPermission和材料资格，不能以某个cachedRevision相等替代完整当前事实，也不能只删除ParseJSON。这里不预先批准cache、新projection表或全局registry。
- 若Current或Content SQL墙钟占主导，才评估同一个owner Tx内现有准确语句的机械合并/已锁事实复用；保持原锁序、锁后真实clock、所有动作、Ref/metadata错误优先级和最终post-IO资格。不预选索引，不盲改CTE或省Now，跨owner读取不合并成私表捷径。
- 若对象Read占主导，才核本地适配器真实IO路径的重复机械成本；仍逐原调用真实读完整字节、验证hash/length和实际关闭。不得让已读取缓存顶替本票承诺的物理读取或把后续unknown写成0。

这些分支是测量结果的决策标准，并非当前授权同时实施三项优化。不存在“只要节省Current的1.254/1.755s就一定完成”的保证：剩余36/32个Material调用、之后Plan/Prepared/全部publication/readback/Finish都尚未进入。已发生调用也不是每项同样大小/祖先数量，不能线性外推后声称充足。

## 5. 修复资格必须回到原完整链

后续单改动首先保同原inputs、完整graph、bytes、currentInput/currentPolicy和计量规则；旧engine/rule2、原SQL、RuleStarts1、费用/预算/绝对deadline/ctx均不变，不heartbeat、不在错误后renew、不中途重启30秒。若改善后推进到另一个真实门再失败，准确保留新失败，不把“已读完材料”冒称原正常完成。

真实恢复资格仍是原B两个正常分支一次rule start在原5秒Claim内读全材料、提交原Prepared、出版及独立回读全部产物、公开completed/完整M字节与来源用量正确；对应65overflow等拒绝保持无派发。相关当前权限/真实锁等待跨界/原重开恢复按实际改动范围回归，临时计数不替业务oracle。最终源去除临时探针后确认相同正常/拒绝结果；原normalA/B、raceA及两次raceB失败历史分别保留。当前未满足这项出口，七AC/whole04无完成声明。

补充静态核对：已全文读 `large-graph-subcost-static-pointers.md`，与上述实际边界一致；它不含新的子成本测量。现已运行的五文件overlay及manifest/hash保持不可变；下一实验另建新目录/新manifest，在副本增量插桩，不能覆盖旧诊断证据。若在closedJSON内拆ParseJSON/typed decode/EOF，仍仅计时原调用，不增减任何校验。
