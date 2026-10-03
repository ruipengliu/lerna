# 03：06合入后的容量端口与独立Decision存储接法

**正式采用：2026-10-03，前置02已按源码5548744/退出文档df2dbe5完整退出；授权决策代理完成最终接法复核，root据[最终交接](final-handoff.md)采用本记录。下文准备时的pin/pending状态保留为历史，当前接法及采用门槛以最终交接为准；没有宣称03已经实现。**


日期：2026-10-03。**准确只读pin：`6211bff647c61d9c6a4994cfbf254bc26d9fba7a`**（docs: record durable work review findings and verified core CI）。本次产品文件均用`git show 6211bff:path`读取，不把singlefixer当前工作区变更当作已合入事实。未改repo、未运行数据库/产品测试、未启动03、未发布票。02仍待修复、完整审查和退出；本报告不是API冻结或验收。

本记录更新`/tmp/lerna-03-port-recheck-current.md`的093e6e4容量空白，补充`storage-handoff.md`已选独立PG Decision owner方案。保留03仅PG Decision实现、独立SQLite测试目标、旧1.0冻结、强类型1.1账本及全部既定业务身份。下面先列真实现状，再给有顺序的实际接法。

## 1. 这个pin实际具有什么

| 当前文件/符号 | 实际耦合或保证 | 对03的直接影响 |
| --- | --- | --- |
| runtime/admission.go：Tx/TxRunner/Clock | opaque Tx只公开Owner；短事务由adapter运行；没有名叫runtime.Store的通用实现 | 可以复用机制接口，不能假设存在通用Store可new |
| runtime/admission.go：AdmitWithGate | 在原Command查找/摘要比较后、新固定决定前调用gate；旧Admit仅传nil gate | gate位置已可参考，但Record/Receipt/TransportOutcome仍具体旧1.0，不能直接接新版拒绝 |
| runtime/work.go、schedule.go | Claim完整worker/epoch/revision/lease绑定，有限lease/scan；ScheduleStore含Validate/Release/Defer等 | 行为适用；当前具体SQL是否可用要逐项检查，不是声明接口就自动支持Decision |
| internal/durableworkdemo/pool.go | LaneLimit/TenantQuota/PoolConfig/PoolCursor/PoolState/PoolBinding/PoolObservation/PoolRepository全在demo包 | 目前没有独立可import的通用容量包，Decision直接引用它会形成业务方向错误 |
| 同文件的refreshPoolCursor/rotatePoolCursor | 已有耐久Order FIFO、Waiting、LastAllocated、Sequence和After/Through；scope最多64members，queue≤4096/lane，concurrent≤64，tenant quota允许0 | 当前真正可复用的是有限容量值和FIFO运算，不是demo.Service/Worker |
| PG/SQLite store.go：transaction | 含`*demo.PoolState`、poolLocked以及exact `*Store`、sql.Tx、owner、active | 连事务实现也不能原样放到新adapter后声称已去demo依赖 |
| PG pool.go：poolCoordination | 实际用schema范围`durable-pool-registry` advisory lock，class 3；一个schema内池协调串行，不是此前建议的每pool行锁 | 接入必须沿最终真实锁协议；不要凭旧设计文档假称当前已有细粒度pool行锁 |
| 两库pool.go | PoolCounts/ReadyTenants/Page/Claim登记基本操作runtime Job；PoolQueue和SetJobLane却写死`phase='project'` | 即使改Go类型归属，两个方法仍不能拿Decision ID直接调用 |
| 两库0005_pools.sql | 给jobs加lane/pool_claim_epoch；配置/member/FIFO/scope表；历史lane从durable_schedules.Policy提取 | 不可把整份旧host迁移复用为新owner初始化 |
| 两库jobs FK | PG0001、SQLite0002实际jobs外键均指向durable_inputs | Decision独立jobs与自己的FK仍是必要选择，不插假Input、不删旧FK |
| demo service.go/step.go/work.go | Record gate拿pool后input→Job；Claim登记pool_claim_epoch，Start/Finish/Renew重验pool占位和权限/期限，StartEpoch持久 | 新consumer必须覆盖这些成功门禁，但不得用demo hash/Policy作为Decision规则 |
| demo maintenance.go | 受信PoolControl驱动有限成员/Job分页，不分配执行Claim便关闭准确expired/stopped/permanent责任 | 思路可复用；Decision终态/取消及其deadline仍由Decision自己解释 |
| host/durablework | Host/Worker/PoolWorker均为demo别名/装配；Storage含demo.Repository；NewScheduledWorker要求demo PoolRepository/ScheduleRepository | 不是通用Decision宿主，不能包装Proposal为Text或将其Worker搬入组件 |

PG token仍拒绝foreign实例、wrong owner、expired token。PG Within是真实READ COMMITTED、synchronous_commit on、有限transaction/statement/lock timeout，Clock用数据库clock_timestamp；COMMIT不确定分类保留。SQLite为单writer/文件排他，其token也持demo pool类型。本报告不要求为Decision创建SQLite adapter；如果共享值类型被提取，现有SQLite demo只需机械更新引用并通过原行为回归。

PoolScope当前由持久scope_id和实际数据库连接范围构成：PG包括host/port/database/schema，SQLite包括文件路径；PoolBinding同时绑定Scope与pool ID。独立scope即使同pool名字也不是同一额度，不能在路由建议与真实Claim之间丢掉这层绑定。该pin仍在review fix阶段，最终应采用02退出修复后的准确表示/校验。

## 2. 已定存储布局保持，有限共享范围说清

Decision使用独立PG schema、独立owner migration序列、自己的强类型1.1命令账本、Decision事实及FK到Decision的jobs；同一个具体Decision Store实例创建并消费全部相关Tx token。准备/接纳、账本、必要Job和本scope容量事实同Tx提交；fixture source/publisher保持其他owner的Tx外交接。

**“共享容量”在03指这个Decision存储scope内，显式登记的tenant/Decision OwnerRefs、多个真实worker/Store实例共享同一耐久pool。** 不是每worker内存semaphore，也不是与独立demo schema或SQLite目标跨库合算。最多64members、同tenant多owner合算tenant×lane、准确pool归属、FIFO/配额和缺配置failclosed沿02最终规则。

不要求也不声称把不同schema的demo+Decision池合成全局池。要跨schema共享总额会新增跨owner事务协调，不是03当前需求，不以同一PG进程地址偷换为同一scope。初版Decision新工作的lane可以固定ordinary；control/reconciliation有限保留配置可沿机制存在，但不能为了填lane演示制造假Task/假工作。取消/到期的安全收尾不因ordinary满额而受阻，也不新增Job换lane执行本来的计算。

## 3. 最小实际改动顺序（只能在03正式开始后做）

### 第一步：只提升两个真实consumer共用的容量值与FIFO计算

选一个职责明确的机制包，例如`runtime/workpool`（这是拟议位置，当前不存在），放有限Config/Limits/Quota/Binding/Cursor值、校验及FIFO保序/旋转运算。03组件与02演示是真实两个消费者，所以现在已有提取依据；不是为未来所有owner预造插件框架。

输入身份沿共同OwnerRef/ID，runtime Job/Claim及Tx机制仍沿现有值。1.1生成身份如果不是相同Go类型，做准确字段与校验转换；这不授权转换新版receipt成旧receipt。

demo可用内部类型别名/薄包装保持已有装配和测试调用，PG/SQLite token中pool缓存引用机制类型。**不把整份demo.PoolRepository、Policy、WorkerPermissions、ScheduleRepository、Input、Projection、Start/Finish搬入runtime。** FIFO纯运算只接受有限ready观察和当前持久状态；需要哪些DB读取/修改的小接口，继续由实际消费方/调用该机制的调度器声明。保留consumer-owned接口，不引入一个全仓万能Repository。

FIFO和配置格式改变必须保持已发布demo持久字段可读；不能趁提取重置Sequence、丢Waiting、改lane或覆写旧迁移。02最后的修复若改了深拷贝/游标校验，提取其最终测试过的版本，不复制6211bff的已知待修bug。

### 第二步：提取PG私有事务/Job/容量机械实现，避免复制整个Store

选择适配层私有公共位置，例如`adapters/postgres/internal/...`，仅供当前postgres demo adapter与新`adapters/postgres/decision_engine`引用。实际内容是：有限连接与Tx运行、Clock/commit分类、活跃token与存储scope校验，以及两个owner确实共享的jobs/claim/pool显式SQL。

每个外层Store各自拥有一个唯一底层实例；token绑定该实例、owner、active和scope，只有该Store组合的Repo/Job/容量实现能消费。共享的是代码，不是让另一Store得到借用token的许可。不要在runtime/组件端口公开`*sql.Tx`或可任意执行SQL的回调；SQL访问留在adapter私有包。

机械jobs/pool表可在两个owner schema内采用相同必要列/索引协议，显式固定schema/table绑定；FK由各owner迁移独立声明到自己的业务事实。没有字符串模板语言、动态owner插件注册、自动生成领域表。必要时将PoolQueue/SetJobLane从裸object_id+硬编码project改为准确Job身份/对象kind+phase参数，或拆成consumer确定占位delta后调用通用容量检查；**必须去掉当前硬编码，而不是把Decision phase改名project迁就SQL。** 具体局部签名由实施者以最终代码定。

可以共享的实际机械范围包括：pool注册/配置校验及scope、计数、ready发现、有界分页、原Claim占位登记/验证、准确释放、FIFO持久化，以及不解释业务原因的条件关闭Job。demo Input锁、Text读取、Projection、policy/gate、正文清理、旧receipt codec留原adapter；Decision事实/1.1ledger SQL属于新adapter。StopRevision/NextWake对durable_schedules的demo JOIN不能照搬；新consumer提供其真实deadline/终态查询，机械等待仍有限。

迁移不抽成通用自动执行器。旧host0001–0005保持；新Decision initial migration一次建立它真实需要的最终机械列和自身FK/账本，不运行包含demo迁移数据回填的旧hostMigrate。03自身后续迁移再用实际上一版数据验证。

这一步是03首票实际接入所需的小重构，必须由旧02 PG和SQLite回归证明行为未退化。不要求把SQLite机械SQL也改成通用跨数据库框架：它只更新共用类型/必要接口适配，Decision没有SQLite实现义务。

### 第三步：新组件自己的接纳/领取/Start/完成

所有真实Decision消费路径，包括旧构造方式、重开worker和直接处理seam，都须经过同一有限pool门禁；没有nil无限/测试专用成功旁路。Host只组合准确owner、Store、可信Clock、有限pool配置和worker资格。

接纳顺序为：受信鉴权及准确版本→锁原Command→原键返回/冲突→新命令检查/锁准确pool→锁Decision→新身份裁决/必要Job容量→同Tx Decision+固定receipt+Job。clock在等锁后重新读，按既定截止裁决。实际锁顺序是`Command（如有）→pool coordination→Decision→Job→处理状态`，不能先锁Decision后临时取得pool锁。

- 原Command同摘要重传/原查询无需新槽，缺pool时也不丢旧事实；异摘要仍冲突。
- 新Command复用同Decision同输入只关联原事实，不再Trigger/占第二槽；不同输入固定decision_mismatch。新Command仍要可验证的受信配置，不能以delta=0跳过所有门禁。
- 真正新增未结Job才检查有限队列；容量暂不足回滚所有新事实，暴露临时不可用，原身份可重试，不固定财务budget_exhausted或假accepted。永久前态拒绝和临时容量拒绝分开。
- Claim在选中owner的准确Tx中执行，按同一持久FIFO/配额校验，取得真实Decision输入，再登记准确Claim占位与成功Sequence。路由建议须绑定真实storage scope+pool identity，不能错owner fallback。
- 实际Start在短Tx再次检查当前worker资格、pool membership/lane/有效占位、原Claim全绑定、当前Decision未终结、固定execution deadline、真实limits/累计使用，保存准确本次start事实后才在Tx外执行规则/source/publisher I/O。
- Finish/续租/等待/释放也按统一锁序检查原资格；不能用只支持裸Claim的Complete当业务成功入口。必要发布已真实完成并读回后，再同Tx保存Proposal/来源/状态和关闭原Job。

**Decision采用原请求中固定limits/deadline，不继承demo默认5分钟、3次project尝试或legacy-adoption规则。** 进程重启、新epoch、另一个Command关联原Decision均不重置这些预算。规则不调用模型；不能复用demo的fixture transient计数当真实供应商费用/请求次数。

### 第四步：收尾无需挤进新的执行槽

Decision取消/到期按准确控制绑定和当前状态裁决，可在受信短Tx关闭新增处理资格并条件fence原Claim、保留真实已发布产物/必要核对责任，不需要新执行Claim。不能因tenant quota=0而永远留着已到原deadline的Decision；维护有限扫描与机会界沿02真实机制，关闭含义由组件决定。

cancel-before-decide只存已定准确关闭墓碑和固定applied，无execution Job，不受ordinary queue已满阻挡；仍需当前有效scope/配置和控制授权，不伪造完整Snapshot。没有实际未结外部发布责任时不要为了“reconciliation lane”制造空Job；有责任时登记真实原身份，不能把取消当删除所有恢复依据。具体取消行为仍由03票03交付，首票不提前宣称已完成。

## 4. 03首票的具体证据和真实依赖

票01仍是一个tracer：typed1.1公开decide→真实PG有限接纳→原accepted+Decision+Job原子保存→有真实配额的Claim/Start→规则Proposal/耐久fixture发布→新get/command.get恢复读取。上述提取不是另开只改runtime/只建表的横向产品票；可分内部review commits，但必须服务并完成这条行为。

最低补充反例：

1. 同Decision schema多worker/多个Store竞争最后一个queue或lane/tenant槽，不超额；同tenant两个owner合算。不同schema同pool名不互相借额度，scope错配拒绝。
2. 缺pool新请求不留新receipt/Decision/Job；安装显式有限配置后原Command可成功。原receipt query/replay与changed-key冲突仍准确。
3. 同Command并发、不同Command同Decision并发各正确去重，queue满时已有原Decision关联不造第二工作；回滚/commit_unknown沿原身份核对。
4. 真实Start正常成功后，再测无pool登记的裸Claim、错owner/旧epoch/过期/无Start/当前取消或限额耗尽拒绝；不能所有负例只因遗漏Start而假通过。
5. 配额0/饱和时到期责任经维护明确关闭，未到期工作保留等待；旧完成不越过关闭、必要新责任不丢。对03实际需要的FIFO/公平做多tenant正常对照，不宣称demo suite自动覆盖新SQL。
6. 原02真实PG/SQLite接纳、领取、调度、容量、清理/升级和故障suite在提取后继续通过；新Decision仅PG测试，不额外承诺SQLite Decision。

fixture source/publisher自身仍是独立owner并有真实耐久存储；它不能在Decision持锁Tx内网络调用，也不能用Decision pool token写其事实。原03六票依赖保持：02whole exit→01/04；01→02/03；04→05；01+05→06。容量接入不产生取消/所有候选与恢复票的隐藏依赖，root负责全片汇总/广告。

## 5. 必须保留的最终02唤醒复核标记

**STATUS：PREPARATION ONLY / WAIT FOR FINAL 02 EXIT SHA。** 收到root明确02whole-exit和最终SHA之前，不采用/发布03票据、不提取包、不改migration、不实现新owner。

最终唤醒时必须用准确Git对象重新核对：

- singlefixer/架构决定最终是否改变PoolRepository、PoolState/FIFO拷贝与校验、PoolScope和两个Host入口、维护关闭/计数；不假设6211bff待修字段就是最终API。
- 最终pool协调锁协议与所有新接纳/Start/Finish/配置路径；Tx token、有限Clock/timeout、Claim登记的准确字段、错误映射；当前建议的私有提取是否仍最小。
- jobs最终列/约束/索引与host迁移checksum、旧fixture不变；新ownerFK仍必须独立，旧receipt仍不得扩大。
- 1.1届时实际生成/codec接入起点（03此前未实施则确认仍不存在），consumer所有新成功入口的门禁和强类型receipt计划。
- 02真实两库/fault/race/CI及审查退出证据，之后才发布01的完整tracer及其内部提取任务。

本pin的结论是可执行的接法选择，不把端口差距留给实施者猜：**有限pool机制提升到真实共用机制位置，PG只提取实际共享的事务/Job/容量核心，新Decision独立业务与新版账本；两个scope共享实现代码而非混合事实/事务。** 局部Go方法名、具体文件拆分由最终代码决定，不开放未来框架。
