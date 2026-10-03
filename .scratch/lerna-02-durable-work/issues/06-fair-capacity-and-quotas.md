# 06: 租户配额、有限队列及控制核对容量

**What to build:** 普通工作饱和时控制、核对和健康租户仍能推进，既有持久责任不会因容量不足被丢弃。

**Blocked by:** 05 — 持久等待与有界扫描

**Status:** resolved

- [x] 内部 ordinary／control／reconciliation 类别具有明确独立容量，控制与核对保留正容量；不提前实现未来模型或 Task 服务。
- [x] 领取并发、租户 lane 配额与公平状态由真实数据库事务保证；在领取和实际处理入口校验资格与额度。多 worker 竞争不超额，不能仅靠进程内 semaphore 声称保证，也不把租约过期当作旧进程已停止。
- [x] 公平兼顾租户已有分配与等待，有限竞争条件下健康租户在可解释的有界轮次取得进展；普通饱和与正常对照均完成。
- [x] 有限入队容量在新接纳前生效；临时背压不形成财务 budget_exhausted 或覆盖固定决定，原身份稍后重试仍保持正确裁决。
- [x] 已有持久责任和原 Job 新修订不因队列满或配额改变被静默删除／忽略；扫描与候选内存均有明确上界。
- [x] PG／SQLite 共享真实饱和、跨租户、类别竞争及取消／过期 Claim 对照，保留配置、假设、有限截止和实际结果。


## Comments

2026-10-03，独立 worktree `/tmp/lerna-worktrees/durable-work-06`、branch
`codex/durable-work-ticket-06`，clean integration `7fa7594` 起步；产品提交
`06ab246`，合入最新 adopted decisions `dc2653f` 的整合提交 `c43ef4d`
仅增加 adopted docs，受测产品字节相同。已读 implement-spec、tdd 与
其 tests/mocking 指导，沿批准的真实 Host/Tx/storage、Clock、pool 配置与
调度观察、公开 command.get、实际 Projection seams 做 vertical；不查私有
业务行、不用内部调用次数或 semaphore 替代持久事实。六项 AC 全满足；
切片02仍待整片两轴审查、架构优化和准确最终远端 CI，不把本票当全片退出。

**有限配置与统一入口。** 新真实迁移 `0005_pools.sql` 保存准确 pool
membership/config revision、Job固定 lane、原 Claim 的 reservation 来源、
持久 FIFO/page cursors 和 scope generation。每池最多64准确 OwnerRef，
每 owner 在 schema/file 内至多一个活动 pool；tenant×lane 跨该tenant多个
owner合计。ordinary/control/reconciliation 各有正有限 queue/concurrency，
准确tenant quota 可为0。DefaultPool仅提议 ordinary 64/4、control与
reconciliation 16/1，不隐式安装、不让nil代表无限。受信 PoolControl 明确
Install；配置使用 expectedRevision，实际有效Claim计数低于拟降额度前拒绝
缩容，queue降低保 overhang，未结成员不能移出。首次可信登记准确计入
既有积压/有效legacy Claim；以后配置更新不能把无来源 raw Claim 洗成
合法预约。正常旧suite明确安装单owner有限 fixture配置 ordinary
queue4096/concurrency64，控制/核对16/1；负例/竞争suite使用rawHost和自己
明确配置。当前cmd显式Install；真实历史 archive writer/输出未改。

所有真实 Service.Record / Worker Claim、Start、Finish、Complete、Renew、
Stop、Defer/Release 走同一协调范围；原runtime/storage端口仍是受信机制，
不授予消费者处理权限。锁序 `[command] -> registry coordination -> input ->
Job -> schedule`。当前 schema/file 各pool短事务共享一个registry coordinator
（PG验证schema限定advisory lock；SQLite BEGIN IMMEDIATE），不拿它等待
hash、timer或外部I/O；未声明生产吞吐或跨库全局容量。所有成员共享可信
Clock。reservation 与实际 Claim/epoch 同Tx；Start 再验配置、来源与当前
有效Claim、lane/tenant额度，原05资格/deadline/attempt门禁保留。严格
Finish/Complete/Renew不提供旁路；旧epoch不能释放新槽。Raw机制Claims
计入实际有效占位但没有消费者reservation时不能Start。

**接纳边界。** 当前鉴权和原键锁在配置前，同摘要固定receipt重传与异摘要
冲突不受新队列/config影响。不存在原键但缺pool时先临时dependency_unavailable，
没有固定receipt/input/policy/Job，连expired新键也不伪造固定裁决；准确配置
恢复后原身份能成功。已配置而队列满时原expiry/业务precondition裁决优先；
真正新未结责任在同事务背压回滚，非budget_exhausted。已未结Job新revision
复用位置；旧revision关闭保新责任，done->ready新增位置。Job初始 lane
固定，不能靠新policy或请求改lane逃逸额度。原查询/重传及gone最小事实继续
沿原权限可读。

**公平和独立服务。** 每 lane 保存最多64唯一tenant FIFO、Waiting锚点、
LastAllocated与成功Sequence。每次选择在pool锁内从实际 runtime 汇总刷新
资格；原合格等待者保序，新/重新合格者按最早due、稳定ID加入当前尾。
全lane满只暂停；tenant自己满/无due候选退出，恢复入尾。nextOwner是建议，
真实owner-local Claim事务重查同一FIFO/config/占位并原子记录成功序号/移尾。
候选按 due/Job identity keyset分页，有明确有限高水位轮次；跳input/Job锁
推进游标，未完成head不允许已经服务者越轮。direct Claim限1–64实际
候选尝试，每页最多64 metadata；Pool ClaimLane最多64建议/实际尝试，
没有加载无界候选/body。单tenant到达已扫轮末尾可有限回绕一次，仍受原候选
预算约束，不因释放后的原对象停在空尾页。N<=64固定持续合格tenant、
有限候选/无永久锁、pool与worker健康持续提供成功lane机会时，目标在N次
成功分配内取得；不是无条件墙钟SLA。Run拥有三条分别cancel/Wait的lane
循环，ordinary有效Claim满额期间control/reconciliation实际产生hello hash，
不是三个标签或仅配置数字。

**无quota维护。** 明确受信PoolControl装配提供Maintain；每次一个准确member、
最多64 Job metadata，用持久member/page/high-water cursors继续，不受执行quota
过滤。pool->input->Job锁后重读原schedule/可信now，准确expired/stopped或
已耗尽且无有效最后Start的责任只记其终结，不新Claim/attempt/hash/公平成功
Sequence。同revision有效旧Claim可被fence，higher revision仍保原Job/queue
责任。不同revision活Claim不被清除：可先固定最新expired，待其正常完成或
原lease到期才收尾。有效最后Start不能仅因attempt==max提前失败。缺配置
显式不可用并保责任；已验证drained移除成员的旧maintenance cursor被修剪，
不删除Command/Job历史。服务/Clock/有限集合/无永久锁假设与扫描机会界如
consumer README；zero quota不要求任何成功执行机会才能到期关闭。

**真实故事。** 同一两库suite覆盖 ordinary饱和+控制/核对实际Start/hash/Finish；
最后queue位两个并发新键仅一applied、落败原身份重试；tenant两个owner和8并发
worker共用一份quota；配置revision/缩容/overhang/未drain移除；Claim/config/FIFO
重开；N=2六次真实分配与双方全部known hello Projection；quota失去/恢复入尾；
A/C已等、B后来，C->重开->A->B，等待与次序持久；zero quota deadline前不加
attempt、到期无需新Claim关闭；真实Start后deadline fence、late Complete/Renew
拒绝而新epoch占位保留；latest短deadline不抹different-revision live Claim，旧
success保留；65-item zero-quota维护跨页/重开并配normal-policy不误关闭对照；
最后一次启动live对照与失效后的attempt耗尽关闭；无来源raw Claim即使reconfigure
仍拒绝。业务进展经Host/query/known literal SHA256观察。

PG另有同schema独立Store竞争共享实际capacity、另schema持pool registry锁
仍可正常Record/claim/finish，65个实际input锁前缀保head并跨页取得健康后缀，
以及quota0维护关闭同一实际锁前缀后的新revision。SQLite单BEGIN IMMEDIATE
writer不能制造同库独立input SKIP LOCKED前缀，未造假writer/锁替身；两库共同
证明其实际页/重开机制。scope binding 是非敏感 tuple：backend + PG configured
Host/Port/Database/validated schema 或已验证绝对SQLitefile path + durable nonce。
同scope多PG Store一致；foreign DB/schema/file错装拒绝，真实closed SQLite整file
复制（包含相同nonce）到另一owned path后错装也拒绝，原file正常成功。
Nonce会随backup复制，不是任意fork物理唯一证明；route别名不自动等价、
DNS/部署fork/旧连接fencing仍须可信部署drain，不增加身份探针/跨故障域承诺。

**TDD/失败记录。** 首小tracer缺pool API compile red，最小SQL/consumer门禁后
两库0.508s green；跨tenant调度入口和独立zero-quota维护各由缺API compile red
起步再green。新增真实raw Claim reconfigure测试曾在两库实际Start返回nil而
red，修正仅首次member登记adopt后green。due/Job keyset替换时SQLite DATETIME
扫描string得到可变RFC3339Nano使严格九位解析失配，实际fair/reopen red；改native
Time/NullTime后两库green。whole首次迁fixture缺 promoted pool port及busy fault已
持锁再setup、历史升级后未Install等实际red，先正常配置再fault/升级后明确Install
修复；后续whole09释放对象锁后单tenant停空轮red，用上述有限回绕解决。补充
防御/恢复用例有直接green者，未伪记red。首稿静态环不满足新/恢复者当前尾，经
采用fair-eligibility决定改持久FIFO；未降低N承诺或绕过门禁。

**版本与实际检查。** Go1.27.1、Node24.19.0、pnpm12.8.1、pgx/v5 5.11.0、
go-sqlite3 1.14.52/SQLite3.53.4、GCC14.2；PG18.6 READ COMMITTED /
synchronous_commit on，Tx3s/statement2s/lock1s，SQLite WAL/FULL/foreign_keys on、
busy100ms/Tx3s。完整mandatory native psql17.11实际版本被检查；CI受限container
psql lifecycle入口未替换/跳过。V5 PG checksum
`sha256:5f9356a7b4f58e908069e6038e599de0fd4d31c8f4f025124672eed8e72319e1`；
SQLite `sha256:3ae1c4c2b09e2ed9b9c43f507f9f2b73c4bf089ca124dedbe4ec719b21107b66`。
发布0001–0004、四套v1/v2来源/fixture、.gitattributes对7fa7594无diff；27 manifest
entries全OK，真实历史v1/v2完整restore/迁移失败rollback/retry/reopen经过current V5
配置与真实Start/Finish，不重写历史writer。生成器/公开1.0全量不变。

最终 `make bootstrap`、`make fmt`、`make check`（两向158fixture真实往返+build）、
`make test-race`、`go mod verify`通过；`make test-integration` mandatory count1全
recovery **60.305s**，whole `go test -race -count=1 -tags=integration -timeout=120s
./conformance/recovery/... ./internal/durableworkdemo/...` **91.786s / 1.415s**。
此前先行45.717s与89.560s不是最终新scope/copy/max-start出口。产品06ab246与docs-only
整合c43ef4d字节核对相同，merge无code冲突；全片 `8e7438e...HEAD --check` 通过，
不是只检查空worktree。工具缺配置/依赖硬失败，不skip必需数据库套件。

DSN仅读600保护文件进入环境，未打印/提交；每轮TMPDIR为/workspace登记owned
本地overlay目录，测试结束有限清理自己的文件及实际登记PG schemas，不drop caller
DB/历史smoke、不按prefix猜所有权。04历史未知schema和07未知CREATE未启动容器
限制保留，未声称已清。证据仅本机真实DB/进程故障与条件公平；不声称断电、任意
物理副本隔离、外部效果终止、生产吞吐/故障域耐久或远端CI成功。本分支仅commit，
未push/rootmerge/PR/清理worktree，whole02审查由root接续。
