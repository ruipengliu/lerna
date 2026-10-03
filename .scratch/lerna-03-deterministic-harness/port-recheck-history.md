# 03 最终接法预备复核（仍非正式采用）

**正式采用：2026-10-03，前置02已按源码5548744/退出文档df2dbe5完整退出；授权决策代理完成最终接法复核，root据[最终交接](final-handoff.md)采用本记录。下文准备时的pin/pending状态保留为历史，当前接法及采用门槛以最终交接为准；没有宣称03已经实现。**


2026-10-03。文档/审查基线：`56f9d7b0d4c544ad34f54549de0c303b65cea32d`；其实际产品与测试源码基线：`c52e68b46c619df0c8e5df1b27a0b5dded3ef65f`。两者diff仅4份02审查/票10/progress文档，源码一致。关键代码以`git show c52e68b:path`固定读取；没有运行测试/数据库、读取凭据、修改repo或参与唯一fixer正在处理的Cleanup修复。

结论：独立PG Decision owner、新版强类型账本、1.1.0隔离、双身份、原取消binding及六票42AC依赖图继续成立。**无新领域选择，无须ADR。** 相比旧6211复核，需补全all-members唤醒的实际义务，并避免把票10现有demo-typed夹具误当通用owner装配。这是正式发布前准备，不是whole02退出证据。56f9记录的holder历史错误阻断Cleanup P2仍由唯一fixer修复；先前绿色CI不能消除该待办。

依据：03 decisions/storage handoff/capacity recheck/ticket-review、六份/tmp草稿、02已采用architecture-decision及票10、当前runtime/consumer/PG/Host/fixture/codec/generator。以下补充覆盖旧报告6211的相应技术接法；没有重开已批准Proposal、限额、target/fault-plan领域语义。

## 1. 已读实际端口与新增事实

| 位置 | c52实际事实 | 03接法 |
| --- | --- | --- |
| runtime/admission.go | TxRunner/Clock/Command/Job机制；账本及AdmitWithGate仍具体旧1.0；没有通用runtime.Store | 新1.1强类型接纳/账本仍必要，不给旧receipt扩大enum、不用any兼容 |
| runtime/work.go / schedule.go | Claim含原对象、phase、worker、claimed revision、epoch、lease；ScheduleStore提供Validate/Release/Defer/NextWake/StopRevision等 | 可复用机制合同，不等于其demo具体SQL或worker自动可用 |
| demo/pool.go | PoolState/Config/Binding/FIFO及PoolRepository仍在demo；MaintenanceJob等还含消费者事实 | 只提升实际两个消费者共有的容量值/纯FIFO，组件按实际需求声明自己的小接口；不把整包/整份大Repository搬到runtime |
| PG/SQLite Store.transaction | 缓存`*demo.PoolState`，同时保持same-Store/owner/active token校验 | 共享容量值提取需机械更新两adapter，不可借旧Store Tx token写新Decision库 |
| PG pool.go | schema范围class3协调锁；PoolQueue/SetJobLane及deadline查询写死project/durable_work/durable_schedules | 新Decision必须实现自身真实对象/phase查询，不把新ID当demo Input或复用假schedule |
| demo/pool_worker.go:NextWake/Run | 在pool锁后取可信now，调用PoolNextWake；Run按3lane独立循环，等待在Tx外；fallback限1ms–1s | 新消费者也须等待全pool成员的真实最早边界，有限fallback防饱和忙转，不能只查anchor owner |
| PG pool.go:PoolNextWake | 合并所有登记members的job.scan_at，以及current work revision和live claimed revision对应尚未关闭的execution deadline；只取未来边界与fallback较早者 | 新版SQL按Decision真实deadline/phase表达同样语义；没有demo式可变输入就不伪造两个InputRevision |
| demo/step.go:prepareClaim | pool→input→Job顺序；Clock/PoolClaim/Claim门禁在可能等待取得Job锁后再次执行；Start/Finish共用；Complete仍要求已Start | 提取时保留锁后可信时间及完整资格复核；新Decision actualStart和Finish不得因复用helper变成裸Claim成功旁路 |
| PG store.go:connectionError | 脱敏Error文案保留cause及Unwrap | 新PG入口保持errors.Is/As/context分类，不为脱敏丢因果，亦不把原敏感文本直接输出 |
| recovery/owned_fixture_test.go | 稳定owned scope、独立admin、可替换writer、peer/holder/child；current类型为waitStore | 提升了02当前测试深度，但不是可直接new Decision Store的泛型fixture |
| recovery/wait/admission/work类型 | waitStore最终嵌入demo repository、旧CommandFactReader及机制接口 | Decision/source/publisher/target的事实不同，禁止补假demo接口来满足此fixture |
| host/durablework | Host=demo.Service；Storage包含demo.Repository和旧reader | 只能证明内部project演示，03需要真实Decision组件装配，不能包装Proposal成text或另称Task |

源码所述scope身份仍为耐久scope_id加实际PG host/port/database/schema（SQLite为文件身份）；配置变更或scope错配不能靠同pool名称误共享槽位。不同schema锁隔离已保留。03有限pool共享范围继续是**同Decision存储scope内显式登记的tenant/Decision owner/worker**，不与demo、fixture publisher或SQLite target跨库假合算。

## 2. 实施时的最小有序接法

1. 03首票引入真实第二消费者时，提升有限pool配置、lane/tenant额度、scope binding、FIFO值与纯运算到职责明确的机制包（位置由实施者据最终代码定）。接口由实际consumer需求声明；可有一个真正共同的窄机制端口，但不强迫Decision实现demo maintenance/projection/history职责，也不为每个表造interface。原demo与两个adapter只作必要机械迁移并跑受影响原suite；不改变0001–0005。
2. PG适配层只抽取两实际owner共用的有限Tx/token/Clock/连接因果/机械pool-Job操作，不复制完整Store，也不构造动态SQL owner注册框架。固定合法表绑定及具体领域SQL仍在各owner adapter。Decision owner有独立schema/迁移ledger、Decision/取消墓碑、新版固定账本、FK到Decision的Jobs；source/publisher另有自己的owner/事务。
3. 新decide事务顺序原Command→准确scope的pool→Decision→Job。原Command同摘要重放先于新接纳限额/前态，仍先鉴权；新同Decision同输入仅关联原记录，不新增Job/队列占用；异输入固定decision_mismatch。配置缺失/临时容量不足不伪造accepted，不把库容量写成fixture费用耗尽。原query/replay不依赖取得新的执行槽。
4. 首票就落实原limits/deadline的当前准入和真实Start检查，保存启动资格后再Tx外读源/计算/发布。Claim/Start/Finish/Renew保留准确pool占位、worker、epoch、claimed revision、lease、原owner及当前状态；经过锁等待后重取可信时间。首票不是“先无界运行，等取消票补限制”；票03再提供完整极值/取消/到期业务反例及其特殊状态恢复。
5. 必要输出仍先原publication key+digest耐久发布/独立读回，再Decision短Tx保存completed。取消/终态变更可能发生于发布期间，Finish必须重新核验；未采纳发布不能变成完成。相同库地址不合并fixture与Decision的事务。

这些是已有选择在实际代码上的落点，不预定尚不存在的Go签名、SQL列名或未来供应商接口。

## 3. 必须补入首票的all-members wake义务

新Decision pool worker的等待决策必须在准确scope/pool内覆盖**所有显式成员**，包括anchor以外owner/tenant的可执行due、可接替lease/scan时刻、仍承担责任的当前/在途阶段执行截止，以及实际存在的有限等待边界。不能只遍历本worker最近领取过的owner，不能只看已取得quota的候选；未到期但当前quota为0的责任到点仍需本地维护关闭。

按实际Decision状态/Job阶段查询即可，不要求引入demo的可变text revision或其durable_schedules表。若新域确有旧Claim与更新work revision并存，两者尚未关闭责任的截止均不能漏掉；若Decision输入不可变，只维护真实存在的固定deadline和phase。已到期/已due但因quota或锁暂不可执行，不返回零/负wait导致busy loop，先有限维护或等待正fallback。Timer等待不得持Tx；control/reconciliation不等ordinary计算返回才获服务机会。

首票正常及反例至少观察：anchor空而另一合法member更早到期时实际提前服务/关闭；非anchor live Claim接替边界不丢；普通quota0/饱和时到期责任关闭但未到期保留；过去due且当前不可领时有限正等待；正常有容量能完成原Proposal。采用共享受信Clock和有限可控Timer/实际Component get事实，不用私有SQL计数或“调用了NextWake一次”代替行为。仅在有服务机会和固定装配范围内承诺有界机会，不声称数据库断网/进程不运行时墙钟准时执行。

## 4. 票10夹具能够复用的边界

稳定scope生命周期原则已真实落地：PG admin独立于业务writers，只有CREATE确认成功才登记owns；replacement不继承删除权；SQLite只管理自己创建的准确路径；child须确认退出、holder须释放/join，才能进行冲突重开/删除；清理失败保留残留和因果。这个收益适用于03，但**现有ownedFixture本体不是通用端口**：它包含`waitStore`、`*postgres.Store` peers、demo子进程和具体历史loader契合关系。

03实际出现第二消费者时选择最小接法：创建Decision自身强类型测试装配并持有真正的Decision Store；只在两个实际fixture需要共同物理scope ownership时提取小的PG scope/admin生命周期部分到conformance可见的内部测试设施。原demo ownedFixture继续拥有自己writer/peer/child的生命周期。Decision fixture同样收拢自己的这些责任，不要求普通caller重新登记Store pointer map，也不把shared helper扩成任意resource callback registry/ORM或产品cleanup服务。若没有值得共享的稳定机械部分，保留两个明确typed wrapper，不能为避免数行重复逼Decision实现demo repository。

source/publisher的独立owner和SQLite target要有各自实际初始化/迁移、持久数据与资源归属；不能直接把02测试文件换名字当新领域数据。旧history恢复、PG holder故障配置、原dump/COPY、SQLite copy和process帧处理仍是专有故事，不因“通用fixture”自动携带到Decision。复用子进程设施指有限private配置传递、真实阶段同步、Kill/Wait等机制；不要求新binary实现旧demo协议或继承其测试业务权限。

当前唯一待修P2是已确认退出但失败的holder历史错误永久阻断Cleanup；本稿不指定并行修复、更不先采用未验证新实现。02退出后须确认修复区分“已结束的历史操作失败”与“仍活跃/退出未知的资源”：保留历史失败报告，但前者不应永久阻止安全的后续自有scope清理；后者继续阻止不安全删除。未知旧schema/CID不能靠prefix猜删，也不能把此次新scope清零推成历史所有资源清零。

## 5. 机器合同复核未出现新分支

当前generator仍硬编码1.0源/输出，Go schema compiler URL和TS version也固定1.0；所以03准确两个源的显式生成配置仍是真实必要变更，不能称新版入口已存在。当前TS生成Schema递归freeze、schema缓存和typed真实往返设施可沿用原则；新同名$defs必须隔离identity/cache。strict JSON、紧凑canonical writer、Unicode/重复键/数字/深度/大小、可达Schema摘要及batch runner有限生命周期的旧行为均保留；Schema元数据数字不使用业务wire禁数字校验。

`ReadCommandFacts`实际reader错误/编码错误仍统一返回unavailable/dependency_unavailable；只有无损旧事实可桥接，1.1完整查询新固定拒绝。不扩旧ErrorCode/方法/广告，不能强cast或把typed error想象成version_unsupported穿透。新cancel-before-decide状态只要求真实已知的Decision/task/input digest/control binding，无原Snapshot时不填假数据。

cancel仍由耐久fixture task owner凭受信principal/proof签发，准确原输入摘要+单调control_revision；禁止通用expected_revision妨碍先取消。旧completed/failed/cancelled及费用/已发布产物不被取消抹去；这些由票03自身验收，不新增票06对03的依赖。

## 6. 六票42AC和最终醒来检查

保持9+7+8+6+6+6=42条及真实图：**whole02→01/04；01→02/03；04→05；01+05→06。** 本轮仅在/tmp票01、03、06追加不增加AC的接法说明：首票实际门禁/全成员唤醒；取消原limits与维护边界；typed fixture复用和完整退出门槛。各新增特殊状态由所属票自证恢复，root全片广告、全部审查/证据/CI仍不藏入06关闭条件。

不可变项：旧公开1.0全部源/广告/黄金；已发布host0001–0005；四组27项历史source/artifact。新Decision初始owner迁移不需要伪造先前不存在的Decision库升级；片内后来新增migration使用真实前版数据验证。机械共享代码更新必须保持原PG/SQLite正常/故障路径，不凭02绿色继承新Decision SQL的正确性。

**FINAL-WAKE：收到唯一fixer完成、whole02正式退出的准确SHA之后，发布/采用03之前，做一次小复核：**

1. 比较本稿c52源码与退出SHA的实际diff，确认已报告P2修复及其有限失败释放证据，未改变产品端口/锁序；若有产品变更更新本稿。
2. 固定最终Tx/Claim/pool作用域、all-members wake和Start/Finish门禁签名；核对新owner最小抽取范围与demo-typed fixture边界。
3. 确认0001–0005、27源、旧1.0仍冻结；准确新CI和完整数据库/故障证据由root核实，不引用旧绿色冒充最终退出。
4. 再将这份技术细化与既定decisions/storage handoff一起用于正式六票；此时才可能发布/启动03。当前不重新grill、不向用户询问例行内部实现选择。
