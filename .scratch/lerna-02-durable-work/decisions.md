# 切片02：持久工作票据与最小接口决策

2026-10-03。依据 `.scratch/lerna-02-durable-work/spec.md`、runtime/data-model、ADR-0003/0004、现有contract.GetCommand及探索报告 `/tmp/lerna-durable-work-exploration.md`。本裁决最初写入临时目录；2026-10-03 切片01完整退出后按用户持续授权采用并发布。工具环境检查不作为02产品证据。

## 1. 演示边界与命令身份

采用无外部副作用的**内部Host演示工作**，不创建Task、Operation或假模型结果，不给已冻结1.0.0公共清单悄悄增加fixture.write。

Host演示命令使用独立准确内部版本 `host-durable-work-1`、profile `host`、method `durable_work.record`。可沿用共同信封结构及CommandDigest编码算法，但必须先经Host专属闭合Schema验证；专属Schema放演示/Host测试设施下，不进入 `contract/schema/1.0.0/methods.json`。CommandDigest允许通用合法格式不表示公共方法支持，这条既有边界保留。

受信租户/主体由Host注入；payload不得自报身份。记录最小演示业务事实，例如一段准确字符串、业务revision、已处理revision和处理结果；worker只处理同库记录，无模型、文件写入或网络目标。关联 `object_ref.kind="durable_work"`。公开command.get返回真实原CommandReceipt，progress取none，因为当前公共合同未定义durable_work进展；none不表示工作完成。完成与修订通过明确的内部Host观察接口验证，不读私有表冒充验收。

Host接纳必须复用准确原CommandRef与主体摘要，先查原键、再判断新接纳期限。主体/业务内容/期限/预期revision变化导致摘要变化；同键不同摘要返回稳定idempotency_conflict，原已固定receipt保持不变。冲突响应不能覆盖原键下accepted/applied为rejected。新过期命令在原键下固定rejected/expired；已经存在的原命令即使现在过期仍返回原决定。

原子性从第一张PG票据开始成立：事实、固定receipt和必要Job同事务；不能先交付只存receipt的“半接纳”，等后续才补Job。

## 2. 接口按眼前消费声明，不建立通用持久平台

职责可按以下最小端口分开，实际Go方法名由实现统一：

- **TxRunner.Within(ctx, ownerScope, fn(Tx))**：有限期限短事务；Tx带明确数据库实例及OwnerRef身份，禁止跨adapter/数据库/owner使用。事务成功与提交结果未知是可区分结果。
- **CommandStore**：在Tx中锁定/读取原命令键、保存不可变接纳记录；另实现现有contract.CommandFactReader，只读投影固定回执。
- **JobStore**：在同一Tx按对象+阶段增加work_revision并确保可唤醒；有界扫描/领取；按Claim续租、提交进展、保存等待或下一次due。
- **Claim**是值，不机械加接口：JobRef、worker_id、claimed_revision、lease_epoch、lease_until。提交时以库中记录核验全部必要绑定，不信任调用方把claimed_revision改大。
- **Clock**提供可信时间并可测试注入；期限/租约判断由owner选定的一套时间来源完成，不能让不同worker各自用任意本机时间裁决同一租约。PG默认使用数据库权威时间，SQLite由单写owner的可信时钟裁决。测试Clock和同步点要能被参与该竞争的所有进程一致使用。
- **业务repository**由Host演示消费方声明，仅含演示事实读写；可以绑定同一个Tx，不把业务含义塞进runtime，不向contract引入SQL。

Tx可以是携带scope的opaque token，具体adapter内部将token解析为自己的sql.Tx并验证scope。不要把SQL通用CRUD加到runtime接口，也不要让领域回调自己开启第二事务。每个adapter负责提供可绑定该Tx的CommandStore/JobStore及演示repository；持有错库/错owner token必须拒绝。

`accept`只组织原键裁决与同事务回执；`work`只组织领取、有限处理及条件提交。网络、模型、对象上传、用户等待必须在事务外；本演示处理是本地纯计算，也先取得阶段输入、离开领取事务再处理，最后短事务核验Claim并提交事实。不能拿测试处理很快当作将任意worker回调放入长事务的理由。

## 3. 数据库与迁移所有权

选用现有已验证工具的驱动家族：PG用pgx/v5的database/sql适配，SQLite用go-sqlite3。具体准确版本在项目锁定安装后复验，不依赖环境smoke go.mod；允许优先验证已安装的5.11.0/1.14.52候选。SQLite默认使用驱动自带构建能力并记录运行时sqlite_version，不依赖个人绝对路径中的头文件或CLI版本，也不把CLI3.46.1误写为驱动实际版本。

- PG默认READ COMMITTED、synchronous_commit=on、显式有限statement/lock deadline；原键竞争、对象/Job锁序及SKIP LOCKED（若采用）由真实并发验证。
- SQLite使用真实文件、WAL、**每个相关连接**synchronous=FULL；一个逻辑owner的写入由一个明确单写协调器串行处理。进程内队列只是写锁协调，不能保存唯一工作责任。第二个同时独立写Host必须被明确排除/受协调，或由真实跨进程锁设计证明安全；不能只设置MaxOpenConns(1)便宣称跨进程单写。
- 当前只创建runtime所拥有的命令/Job记录及演示owner业务表；迁移按实际owner放在各adapter的migrations目录。共享数据库不授予跨owner修改权限；所有唯一键、查询、更新包含tenant/owner。
- **稳定owner不迁移。** 同owner更换进程/worker/连接不改变命令和Job身份；本切片不做跨owner迁移，也不把“数据库schema migration”和“逻辑owner迁移”混用。

迁移必须从首次建表起版本化并有checksum/已应用版本记录。后续领取或调度功能确实需要新列/约束/索引时产生真实v2迁移；保留v1准确脚本与当时正常写入路径。升级验收以实际v1初始化并写入的原命令、Job和业务事实为输入，再运行向前迁移、重开、原引用查询/接替；不能手造一个所谓old-schema或只测空库。失败注入验证迁移事务/可恢复边界，不删库重建，不改写已应用迁移。

若首版设计一次已含全部列，也可以在后续实际需要的调度索引/约束上形成v2；不得为了勾“升级通过”制造无用途空迁移。没有真实old->new路径就如实保留升级验收未完成，不能把所有fresh库绿灯替代它。

## 4. Job与Claim规则

Job按固定业务对象+阶段唯一；新增触发增加原work_revision，不新建同义Job。领取事务固定claimed_revision和新epoch。续租及提交要求同Job、worker、epoch、claimed_revision且资格/期限仍有效；尚无新worker接替也不能让已过期旧Claim继续提交。

完成只将completed_revision推进到本次claimed_revision；若更大的work_revision已提交，Job仍ready可领。并发新工作与旧完成都应有受控同步点的两种顺序对照；不能last-write-wins把work_revision写回旧值。

租约过期允许新worker在原Job上接替，epoch单调递增；它不证明旧进程停机或任何外部动作未发生。本切片无外部效果，不能推广为外部恰好一次保证。

工作失败分类由处理者明确返回：完成、可检查等待、有限退避重试或持久记录失败后关闭本次责任。runtime不凭异常字符串判断业务，也不把永久Schema/权限/前态错误无限重试。记录失败事实不等于Task失败；这里没有Task。

## 5. 扫描、等待、容量与公平

基础领取从首次Claim实现即采用真实持久有界扫描；通知只加速，全部丢弃后仍可推进。后续票据扩展持久等待/退避，不能补救前票曾依赖内存通知保存唯一责任。

等待保存可检查条件、下一次due或有限重新检查时点；释放Claim/连接/事务和worker槽位。不能持有数据库事务等用户/信号，也不能以不断立即重领未来due任务冒充恢复。借可控clock、同步点和公开调度观察验证有界唤醒，不用长sleep或私有函数次数断言。

当前工作类别只需ordinary、control、reconciliation三个内部lane。各有显式并发容量，control/reconciliation保留大于0的独立槽；租户配额按lane实施，使单租户ordinary饱和不能顺便耗尽该租户全部控制槽。不要提前创建模型、工具、评测领域服务。

领取配额、活跃Claim统计与公平状态必须在数据库事务中约束，不能只靠单进程semaphore。公平至少综合租户已获分配和等待时间；可用持久的轮转/last-dispatch计数加最早等待，保证有限竞争租户下健康租户在有界领取轮次内推进。允许有界batch和游标扫描，不创建无限候选列表。调度算法不是公共线协议，但配置、竞争假设和可观察进展界必须记录。

队列容量限制作用于**新责任的接纳前**。已有持久责任不得因内存队列满或配额变动丢弃；同Job新修订不能被静默忽略。容量暂不可用可以在未形成接纳决定时返回Host可判断的backpressure/dependency_unavailable，回滚所有新事实，允许原身份稍后重试；这不是固定CommandReceipt拒绝，更不是财务budget_exhausted。若某业务明确选择固定拒绝，必须按接纳合同保存该决定，不混用临时不可用和固定拒绝。

## 6. 保留、正文清理和墓碑

最小去重依据必须长期保存：tenant/owner/command_id、准确方法/目标与必要主体绑定、原摘要、accept_before、固定决定及查询授权所需最小信息。原正文不是去重权威；可以从一开始只保留真正需要的内容，不为验收新增无用途敏感正文表。

清理正文或归档演示业务细节后，command.get可返回gone并保留原CommandRef；内部接纳重传仍能凭墓碑识别相同原命令、返回原固定决定或明确原决定已清理但不得再次接纳（本版优先保留固定回执使相同重传仍返回原决定）。异摘要原ID仍稳定冲突，不能因gone或期限久远重新执行。

随机ID、TTL、租约过期、完成状态或“很久没有重试”均不构成删除去重墓碑的证明。本切片不实现墓碑回收；将来只有能证明所有旧请求都会被拒绝的规则及其保留期后才允许回收。未结Job/失败核对责任不能随业务正文一起清理。

## 7. 已批准票据与唯一依赖图

八张票据、53 项验收条件经授权决策代理批准。依赖固定为：01→02/03；02+03→04；04→05/07/08；05→06。07、08均只依赖04；08交付本票故障验证，不承担全片隐藏关闭依赖。全片退出由主任务等待全部八票完成后统一核验。

01/02产生真实 v1 接纳迁移和 writer 数据；03/04因实际 Claim 需要形成 v2；07据此验证实际旧数据升级，不依赖06。详见 issues/ 各票、ticket-review.md 和 admission-decisions.md。

## 8. spec02七项验收的覆盖映射

1. 丢回执原键恢复、唯一业务工作、异内容冲突：01/02建立规则，08用进程故障证明，07保证清理后仍成立。
2. commit前/后杀进程：08两库各跑；01/02已有真实rollback/重开正常对照。
3. 旧修订完成保留新工作：03/04，以同一行为套件证明。
4. lease接替隔离旧worker：03/04，08追加跨进程终止/迟到返回证据。
5. 丢通知、未来due、无长事务等待：05，两库均运行。
6. 同一真实adapter套件与DB配置/并发记录：02/04/08；07加入真实迁移旧版本路径。
7. 控制核对容量、租户配额、有界队列与健康进展：06，配正常对照及多worker竞争。

## 9. 故障实验和证据要求

构建独立子进程Host harness，父进程通过管道/同步点知道已开始事务、尚未commit、commit已返回成功但业务答复尚未发出等准确阶段，再SIGKILL。不能凭定时sleep猜提交点。父进程以独立连接经Host/公开查询读取结果，再启动接替者；不读取私有表或凭调用次数判断业务成功。

“commit前杀”应没有成功receipt/部分业务+Job；“commit后答复前杀”应原键取回完全同一固定receipt和唯一业务责任；“提交结果无法知道”保留commit_unknown并沿原键恢复，不宣布失败后新建ID。真实数据库连接丢失导致COMMIT错误时由adapter区分可判断rollback与无法判定，不一概当未提交。

PG与SQLite都使用新建隔离测试数据；不得碰环境smoke数据库。配置显式注入，日志和失败信息不输出密码。test-integration缺服务或驱动必须失败且给出可操作原因，不能skip返回绿灯；基础make check的无外部凭据边界保持清晰。CI按真实服务运行相同入口。

记录代码/迁移/驱动/数据库版本、PG隔离及synchronous_commit、SQLite实际运行时版本和每连接WAL/FULL、有限时限、用例种子/同步点、清理范围与结果。进程SIGKILL只证明进程恢复，不等于断电、磁盘丢失、3AZ同步耐久或生产RPO0；17仍承担生产故障域验收。

