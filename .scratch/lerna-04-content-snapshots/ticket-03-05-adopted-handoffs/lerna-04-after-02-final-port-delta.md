# 04 票02之后的实际端口差量（条件资格）

固定 SOURCE：`5c02440800d87eba2af461c210e4333ff45c1633`；产品源码：`4c6219fb420cdd63ea67f6db4c0106779ea9ccfa`。两者只差两个 CI 工具文件；Go、SQL、归档字节相同。对照首01交付 `1a7d910238eb74cddc712d92b0ba4014a72ff507`，Decision domain/component、decisionfixture 与 local adapter 无改动。本次为只读端口核定，不执行测试、数据库或环境操作。

承接已完整读取的 `/tmp/lerna-04-ticket-03-conditional-implementation-handoff.md`、`/tmp/lerna-04-ticket-05-conditional-implementation-handoff.md`，不重写其方案。另读取 worker 当前 `.scratch/lerna-04-content-snapshots/ticket-02-api-handoff.md`：11602 bytes，SHA256 `8bb38ee15e82478df3b35882360c540e59a515cbd850341c74f27b7fc6e1d894`；这是本次读取的交接文本，不冒充固定 5c 的已交付证据。

**资格：以下可作为正式02接受后的装配差量。现在仍硬等02接受、整合后的准确源码/API及实际退出证据复核；不 claim 03/05，不认定02七AC或 whole04退出。**

## 1. 已存在的接口，不再按01旧形状装配

准确来源：`domain/content/ports.go`、`closure.go`、`management.go`；PG对应 `adapters/postgres/content`。

| 现在实际接口 | 后续消费者必须遵守的差量 |
|---|---|
| `Repository.CheckPolicy(ctx, tx, subject, ref, purpose, actions []string, now)` | 原单 action 已改为集合。必须1–5个不重复已知动作；空、重复、未知、超限 fail closed。完整subject/purpose和每个flag均实际核验，不把一次集合调用称为旧多次采样。所有显式wrapper/decorator须传原集合；nil结构遍历不调用它。 |
| `Repository.ScheduleRetention(ctx, tx, policy, record, sources []Record, budget)` | 实际新准入与同声明收紧cap两个Put分支均传本同Tx完整、确定排序、已锁的闭包Records；零源用完整非nil空集，nil不是延迟资格。不能把上一次读/另一Tx的观察传入。 |
| `SaveSources(...)`、`AdvancePolicyJob(...)` | 完整祖先登记与实际 policy work 推进已由Content owner承担；未来adapter不另写来源私表、不借query补Job。 |
| `Service.AuthorizeUse(ctx, subject, ref, purpose, action)` | 已有可信在线单动作、全闭包资格；没有返回字节或可转移许可。不能先调用它，再无门禁调用 `Objects.Read`，宣称完成受保护处理读取。 |

ScheduleRetention复用的是结构事实。管理仍按**目标Record原完整保存主体/用途**重新取得管理policy与原basis，包括alias调用者delegation chain不同的情况；既不能换成新调用者，也不能逐节点误换成每个源自己的保存主体。管理自己的legacy回填在修改源后仍须重读，不能复用失效观察。

实际可信管理入口是 `Manager.InstallPolicy` / `InstallFixturePolicy`、`ObserveChange`、`ObservePolicyChange`、`ObserveAdmissionChanges`、`RebuildSources`、`Step`。`ObservePolicyChange`只给原change首段，随后必须沿 `NextCursor` 调 `ObserveChange`；`ObserveAdmissionChanges`是目标版本的定点义务观察，不是通用来源读取接口。只读观察不创Job。`CleanupResponsibility`已保存准确ref、完整主体/用途、动作、原deadline、BodyCleanup、staging/object/attempt/publication、residual/reason；这仍不是物理擦除ACK或全部文件holder目录。

自然到期、历史主动变更与新准入定点义务已分阶段；原watermark、due/deadline及历史pending不能因新policy宽化或重开刷新。≤64约束是单版本完整祖先集合，不能误当全部后代/holder的分页总上限。

## 2. 03处理读取：复用点与真实时钟位置

固定源**尚无 `ReadForProcessing`**。原03建议保留为新增窄Content领域入口，实际消费者仍是compiler与Content-backed Decision Source。它们消费领域端口，`domain/task`不得导入 `components/decision_engine` 或 conformance 包。

可复用本包私有 `registeredClosure`：它保留全部中间版本，按版本身份去重并核完整ref一致、循环/自引用、同owner/tenant、准确published与cap；返回的 `closureObservation{refs,records,validBefore,retainBefore}`仅在本owner同Tx有效。不要为方便导出成可跨Tx传递的授权对象。

准确当前顺序是：PG `CheckPolicy` 的 MATERIALIZED 锁行 CTE之后，在外层依赖该行的投影采真实 `clock_timestamp()`；domain仍核policy完整ref。随后 `LockVersion` 成功后，再采 `Store.Now`，在缺失/错ref/未published元数据之前核policy ValidUntil/RetainUntil，之后核持久CurrentRetainUntil；调用者最后还有完整范围freshNow。SQL错误保留原错误。不得用调用前参数now、语句时间或最后一次clock代替这些实际门禁。

新增处理读取采用target及所有祖先的 **read+process**，不默认要求disclose，也不以read推导save。复用Get已有两短Tx结构：前门禁与介质读取前remaining-byte拒绝 → Tx外完整hash/length读取 → 新Tx重核当前target/完整闭包/当前上限 → 才交付字节。新Tx重建观察；不能把旧闭包Records跨过真实I/O继续当current。Get目前是read+disclose，target两次singleton及F1授权先于元数据的规则保持。

返回来源/截止观察只是本次真实读取的材料，不授权稍后保存或公开披露。Content-backed Publisher继续用真实Put、原Command与实际发布观察，不能借processing返回值跳过当前save/闭包检查。M第一材料、真实I/O/O上界、旧1.1 processed含义、63材料可达性限制和原身份恢复均沿原03 handoff，不因新集合policy端口改变。

## 3. 05必须接入的现有门，而非另造平行生命周期

现 `Objects` 仍只有Put/Read；Record仍只有publication历史、当前cap、最后AttemptKey与holder/pending标志，没有body seal、全部attempt集合、Delete、物理确认或第二holder协议。02的责任登记不能充当05这些能力。

| 固定源入口 | 05所需最小接入 |
|---|---|
| `Service.Put` 原Command重传、新Command关联/新准入、`finalAdmission` | 原key当前reader获准后仍返回固定原receipt。新保存准入须核准确版本seal；调度等阻塞工作后的最终锁定门也核seal。若此处拒绝，继续现有精确callback marker回滚首Tx、第二有限Tx原key winner优先的协议，不能提交部分version/change/Job。 |
| `registeredClosure` 与调用者target门 | 实际body使用不得越过已seal祖先/目标。不能把所有CleanupPending当seal；save-only政策pending而read/disclose/caps有效的正常读仍保留到真正封闭阶段。 |
| `Get` 初始/返回前observe、`AuthorizeUse`、03新增处理读取 | 在各自准确授权之后把body seal纳入真实body资格；前后观察一致。03若尚未合入，05先完成现有门，之后合入处理入口时复用实际门；不添加03→05隐藏依赖。 |
| `Service.Step` publication启动、attempt保存、Tx外Put、返回后锁版本/最终保存 | seal阻止新启动与迟到Finish重建published资格。已发Tx外写必须由准确attempt责任及文件跨进程fence处理，不能仅靠PG Claim或此门声称停止物理写。 |
| `Manager` policy注册/自然义务/传播/回填、`SaveVersion`相关路径 | 保留单调seal与清理历史；自然not_required或新宽policy不能洗掉已seal/erase事实。管理观察必须仍可读责任，不以seal禁止其核对，不借观察创建清理工作。 |

新增Lifecycle封闭Tx才登记原seal身份、全部已知holder/attempt和有限cleanup Job；actual file Put/Read须服从跨进程稳定锁/墓碑协议。扩展介质清理端口与独立holder观察沿原05手册；不把它扩成通用VFS，也不令对象adapter写Content权威表。

policy-only pending、不可逆body_sealed、独立物理擦除确认继续三分。Get的gone仍需原05规定的准确当前metadata-only披露资格和staging+primary真实确认；不能靠过期body policy或wrong-fullRef泄露元数据，也不把gone当ALLholders擦除。原Command receipt与published历史不可复活/改写；新query不repair或生成Job。

## 4. 冻结字节与下一迁移的真实owner

| 迁移链 | 固定字节与下一号 |
|---|---|
| Content `adapters/postgres/content/migrations/0001_content.sql` | 3682 bytes；SHA256 `00363b79dafb6eb1f373be9ef08e915fe23cc3db0fa55345e8ae6c051ce0c1ed`，保持冻结。 |
| Content `0002_source_policies.sql` | **2585 bytes；SHA256 `99519565ff1146d7cfc468413b449538c6ba71335e86edc8fd447a3bbff922a8`；blob `02a082a5a0edfa57d0e8d2a2680b0af5ad9a5a41`。** 已有来源代次/边、变更、责任及policy work；Job phase当前仅publish与policy_propagation。05所需seal/holders/attempts/cleanup phase追加Content **0003**，不得改0002。 |
| Decision fixture `conformance/internal/decisionfixture/migrations` | 0001_fixture.sql SHA256 `4aaf85dac7110ffcb4d07fe04dff5f38fbe55cd6d1e49eded4781a8a06bd709b`；0002_control_access.sql SHA256 `df15deae49a50a2a06b044c5019f7422bc533c95788ab58f4cc2c1f15099bdfd`。03新增fixture持久dispatch沿此独立链时下一号为 **0003_context_dispatch.sql**。不是Content 0003的竞争owner。 |

当前处理读取本身没有要求Content新表，不为03预留空迁移。若正式实现出现必要持久事实，再按实际owner链协调当时下一号；不更改已发布迁移、旧归档、Decision 1.1合同/Prepared或原收费身份。现有1.2 wire/profile不因这些内部ports新增方法；没有Task服务、Grant、Provider或whole04广告的隐含要求。

## 5. 正式采用时仅剩的delta核对

02真实退出后以最终合入对象复核上述ports、迁移原字节和当前current/auth/seal接点；若只是证据/交接文档变化，记录产品对象等价即可，不重起两套方案。03与05各自硬等02，彼此保持独立。全部实现、正常/恢复/故障验收、有限物理资源资格及whole退出证据仍由各正式票完成，本记录没有替它们提供测试或完成声明。
