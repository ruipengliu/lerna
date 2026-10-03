# 切片 02 两轴代码审查

基线：`8e7438e071727e25aa69e17fb81b2e53c416b78e`。准确审查 head：`26c91003e7cf09dcc42e8d8c90d0db7b65a915a5`。命令：`git diff 8e7438e071727e25aa69e17fb81b2e53c416b78e...26c91003e7cf09dcc42e8d8c90d0db7b65a915a5`；完整 commit list 已提供给两个独立只读审查者。

以下分别保留两轴原结论，不合并或重新排序。原发现已交单一独立修复分支；以下为准确26c9100的原报告。6783307修复后独立复核已全部关闭，分别见后文；审查前绿色CI不作为关闭依据。

## Standards

完整变更范围包括 product/runtime、双库 storage/migrations、admission、scheduling/pools、lifecycle、conformance、scripts/CI 和 docs。只读审查；未重跑测试、操作数据库或修改仓库。依据 AGENTS.md、CONTEXT.md、相关 ADR/架构和实际 README；已执行的 tooling 检查不重复报告。历史未知 schema/CREATE 资源、SQLite storage-port 确认丢失和本机故障域限制全部保留。

1. **[P2，硬规则] 保留 PostgreSQL 启动错误的可判断原因。** `/workspace/lerna/adapters/postgres/store.go:44`、`:54`：`return nil, errors.New("PostgreSQL connection configuration rejected")` / `errors.New("PostgreSQL connection unavailable")` 丢弃 sql.Open/PingContext 原因。违反 `/workspace/lerna/AGENTS.md:105`：“错误必须保留可判断的原因”。已取消或超时的 startup context 因此不能通过 errors.Is 判断；认证/连接故障也不能通过 errors.As 分类。最小修复：提供脱敏 Error()、保留 Unwrap()/Is/As 的错误包装；不要直接输出 DSN 或未经处理的 driver 文本。

2. **[P3，硬规则] 同步当前容量实现的 README。** `/workspace/lerna/runtime/README.md:25`（“Fairness and quotas remain ticket06”）、`/workspace/lerna/adapters/sqlite/README.md:7`（“quotas remain ticket06”）、`/workspace/lerna/host/durablework/README.md:87`–88 仍宣称未实现，和同一 Host README:111 的“Current admission and processing require a durable finite pool”、CONTEXT 当前状态矛盾。违反 `/workspace/lerna/AGENTS.md:139` 文档须与实际实现一致。读者可能遗漏必需 pool 装配。删除陈旧句，链接当前 pool 配置/保证边界；整片待审查/CI 的事实继续保留。

3. **[P3，判断性 smell：Duplicated Code] 收拢同事务的 Claim 资格准备。** `/workspace/lerna/internal/durableworkdemo/step.go:121`–142 和 `:228`–249 重复 `w.Clock.Now` → `poolRepo.ValidatePoolClaim` → `schedule.ValidateClaim` → 锁后再次取时及双复核，前后的 pool/input 锁定也同形。未来修改一处门禁容易遗漏另一个实际入口。最小修复：在同文件提取事务内锁定/复核 helper，两处调用；保留锁后第二次取时，不把两次取时误当冗余。该项不是当前正确性缺陷，也不要求通用框架。

未把 Host aliases、后端特有 SQL、已采用同库 pool 协调或低层存储测试误判为职责/私表违例。

## Spec

Review pin: `8e7438e...26c9100`（完整 three-dot 变更；01–08 共53 AC及09的5 AC）。只读审查产品路径、现有真实测试设计及 adopted decisions；未启动数据库或重复测试。

## Finding

**[P2] Pool Run 固定 fallback 会错过合法重试窗口** — `/workspace/lerna/internal/durableworkdemo/pool_worker.go:148`。

采用的规格 `/workspace/lerna/.scratch/lerna-02-durable-work/scheduling-decisions.md:41` 明确要求：**“取‘最早相关 due/lease/deadline’与有限 fallback 扫描周期的较早值。”** `PoolWorker.Run` 每个独立lane在 `StepLane` 后却直接等待整个fallback，没有读取持久下一边界。两adapter都受影响。例如显式有限pool内接纳默认ordinary策略，设置 `ExecutionLimit=500ms`、`TransientFailures=1`、`BaseBackoff=100ms`、`MaxAttempts=3`，可信Clock正常推进，lease一分钟，Run fallback一秒。第一次真实Start后保存retry/due约T+100ms；runner到T+1s才再次服务，Maintain先保存expired，无第二次Start或成功Projection。将fallback改为10ms或使用无临时失败策略可正常完成；原固定applied回执保持。现有Pool Run正常对照 `/workspace/lerna/conformance/recovery/pool_test.go:895` 只采用10ms和即时成功，未覆盖此合法窗口。

最小修复：每lane有界查询全部声明members的持久due/lease及current、claimed两修订的deadline，以可信now计算较早未来等待；不能只复用anchor单owner的NextWake或跨owner写业务Tx；保留到期/跳锁项有限fallback，保持独立lane和无quota维护。增加两库真实Run retry窗口、非anchor成员/较新修订deadline及正常成功对照，禁止用延长期限掩盖。

## Remaining review

(a) 上述下一唤醒要求部分缺失；(b) 未发现未批准scope creep；(c) 上述实现会造成错误的可观察处理结果。其余58 AC未确认新缺陷：核对原键先于pool/截止、same-Store owner-bound Tx、真实PG Commit unknown、revision/epoch/锁后时间、严格Start/Finish与新work保留、有限FIFO动态尾部/独立lane、缺pool failclosed、无quota维护、真实v1/v2完整来源升级与body-gone、进程提交及旧消息正常/故障对照。SQLite确认丢失仅证明真实提交后的存储端口；原生Commit异常、断电及历史未知schema/CID清理仍是已接受证据限制。whole02仍需修复后复核、architecture review及准确最终CI。

Standards：3 项，轴内最严重 P2（错误原因丢失）；Spec：1 项，轴内最严重 P2（错过合法重试窗口）。

## 修复记录

单一分支 `codex/durable-work-review-fixes` 的产品 `4311585`、clean tip `cc6a053` 经merger合入 `6783307ebe7d802f78f9aa7bdb1a1464ec5749e1`。完整baseline diff与原始001–005/四组来源冻结核验通过。真实PG/SQLite短期限Run、non-anchor、current/claimed两修订期限、零quota维护均有正常对照；原Start/Finish门禁保留锁后第二次可信Clock。完整count1集成50.359s、竞态103.345s，均timeout120；基础check/race、27源hash和module校验通过。

首轮竞态118.936s超时、孤立7.282s的实际持锁器提前退出及只调整测试独立holder10s窗口的修复如实保留；业务Tx仍3s。首次WallTimer red/green和最终可重演Clock/Timer测试分别记载，不将它们混为同一次测试。详细证据见[修复记录](code-review-fix-evidence.md)。

## Standards 复核

准确 head：6783307ebe7d802f78f9aa7bdb1a1464ec5749e1。重新取得完整 baseline..head commit list，核对完整 three-dot 范围，并逐项审阅 26c9100…6783307 增量。原 product/storage/admission/scheduling/lifecycle/tests/scripts/docs 审查继续有效。只读；未运行测试、访问数据库或读取凭据。

原发现结论：

1. **P2 硬规则，关闭。** `/workspace/lerna/adapters/postgres/store.go:40`–41 提供脱敏 Error() 与真实 Unwrap()，`:54`、`:64` 两个失败分支均保留 cause，满足 AGENTS.md:105“错误必须保留可判断的原因”。`conformance/recovery/startup_test.go:21`、`:63` 覆盖取消/截止、SQLite 对照、正常 PG、实际 pgconn.ParseConfigError 分类以及三种默认格式不泄露假配置。未用虚构 driver error 替代实际来源。

2. **P3 硬规则，关闭。** `/workspace/lerna/runtime/README.md:25`、`adapters/sqlite/README.md:7`、`host/durablework/README.md:87` 已改为当前必需有限 pool/配额/公平说明，链接真实装配和限制，满足 AGENTS.md:139 的实现一致性要求；整片退出仍待审查、架构及最终 CI。

3. **P3 Duplicated Code 判断建议，关闭。** `/workspace/lerna/internal/durableworkdemo/step.go:166` 的 prepareClaim 由 Start:110、Finish:227 实际共用。保留 pool→input→Job 锁序、Clock:178/188 两次采样、两轮完整 pool+Claim 资格复核和当前 worker 权限:198。未删除锁后复核或扩大为通用框架。

新增变更：双库 PoolNextWake（PG pool.go:490 / SQLite pool.go:488）只读已声明 members 的 due/lease 与 current、live-claimed deadline；外部 Timer 在事务外等待。真实 Host/revision/projection 对照保持。pool_test.go:968 的 10s 独立故障 holder 不改变业务 Store 的 3s 配置；:990 有有限失败 join，checkHeld 同时核验实际事务 context 和完成通道。这些符合 AGENTS.md 的有限生命周期及可重演故障要求。

**新增 Standards findings：0；新增需处理的 Fowler smell：0。** 所有十二项启发式结合仓库职责/后端差异判断，tooling 已检查项未重复报告。

证据限度：50.359s 正常/103.345s race 是已记录的 count1、timeout120 结果，本审查未复跑；118.936s 首轮 race 和7.282s隔离失败及窄修复记录仍保留。增量未改变0001–0005或四组冻结来源。未知历史 schema/CID、SQLite storage-port 确认丢失、本机进程故障域限制不变；whole02 仍待独立架构和准确新 CI。

## Spec 复核

Baseline `8e7438e071727e25aa69e17fb81b2e53c416b78e`；final pin `6783307ebe7d802f78f9aa7bdb1a1464ec5749e1`。重新生成63项完整commit list，核对完整three-dot范围及`26c9100...6783307`增量，复核全部58 AC、adopted decisions、原报告及tracked修复证据。仅只读；未运行测试或操作数据库。

**原P2已关闭：Pool Run不再固定等待fallback。** 原要求 `/workspace/lerna/.scratch/lerna-02-durable-work/scheduling-decisions.md:41`：“取‘最早相关 due/lease/deadline’与有限 fallback 扫描周期的较早值。” 当前 `/workspace/lerna/internal/durableworkdemo/pool_worker.go:181` 调用NextWake、独立lane Timer等待，并在取消后join三个loop。`/workspace/lerna/adapters/postgres/pool.go:502` 与 `/workspace/lerna/adapters/sqlite/pool.go:500` 按准确pool/member/lane聚合future scan_at及current/claimed两修订deadline，保留未结/epoch条件，采用可信now和正有限fallback；未只查询anchor或跨owner修改业务事实。

`/workspace/lerna/conformance/recovery/pool_wake_test.go:20` 使用真实双库、非anchor owner、实际Run/Start/Finish和共享Clock/外部Timer，保持500ms执行期限、100ms退避、一秒fallback，验证两次Start、准确hash、正常成功及原receipt。另覆盖live lease800ms与较新revision deadline300ms、claimed deadline400ms与current三秒/lease一分钟，以及零quota维护保留attempt/Sequence/Claim/queue关系。证据准确区分首次真实WallTimer red2.509s/green0.910s与最终可重演受控时钟测试。

维护仍每机会选择一个member/有界页；符合 `/workspace/lerna/.scratch/lerna-02-durable-work/capacity-expiry-decisions.md:46`：“不是无条件墙钟SLA”。测试故障holder改用有限10s独立PG连接并检查权威/失败join，业务Tx仍3s，未放宽产品期限或mandatory timeout。

**新增具体缺失/部分要求：0；未批准scope creep：0；已实现却错误的行为：0。** 原键裁决、same-Store Tx、严格Start/Finish锁后时间、revision/epoch与新work、持久FIFO/独立lane、missing-pool failclosed、无quota维护、body-gone、真实旧源升级和进程正常/故障对照未见新回归。prepareClaim保留两次Clock与双门禁。增量未改0001–0005、四组历史来源或公共1.0合同。

已读最终顺序count1正常50.359s/race103.345s及27hash记录，未冒充独立重跑。SQLite storage-port unknown、SIGKILL及历史未知schema/CID限制保持；不宣称原生SQLite Commit故障、断电或资源全零。whole02仍待architecture review与准确最终CI。

Standards：原3项关闭，新增0项；Spec：原1项关闭，新增0项。两轴结论分别保留，整片02仍待架构选择/实施与准确最终CI。

## 票10架构实施后的 Standards 复核

# Standards architecture followup — 8e7438e…c52e68b

准确 head：c52e68b46c619df0c8e5df1b27a0b5dded3ef65f，工作区 clean。重新取得完整 baseline..head commit list，核对完整 three-dot 范围；审阅 c6220fd…head 全部18个 recovery 文件及票10 Comments。6783307 两轴原四项关闭结论保持，产品/迁移/冻结来源无新变更；不重排其它 axis。

**[P2，新增硬规则偏离] 已退出但失败的 PG holder 永久阻断整个 fixture cleanup。** `/workspace/lerna/conformance/recovery/owned_fixture_test.go:153`–154 的 `if err := f.joinPGHolders(); err != nil { return err }`，结合 `owned_pg_fault_test.go:80`–81 的 `if h.joined { return h.result }`。

触发：真实 lock callback 失败或 holder TransactionTimeout 到期，Within 返回错误；ReleaseAndJoin 已收到 done 并将 joined=true/result=err。此时 holder 已确认退出，但 Cleanup 立即返回，未关闭 current/peers/admin，也未删除确证自有 schema。每次重试仍返回同一保存错误，清理永远无法推进；Check 因到期 Fatal 后的自动清理同样受影响。正常绿色套件未覆盖这个已确认失败退出路径。本结论由代码路径确认，未运行数据库实验。

违反采用的 `/workspace/lerna/.scratch/lerna-02-durable-work/architecture-decision.md:54` 的明确 lifecycle 规则：“join现有process/holder、逐一Close登记writer/peer，确认后Drop…最后Close admin。失败聚合报告…未完成步骤…可…重试”；该规则只在停止未确认时要求保留资源。AGENTS.md:104 的有限生命周期要求也应在故障收尾落实。最小修复：将 holder 的事务结果与退出确认状态分开；已 joined 的错误聚合保留，但继续关闭已知安全的句柄和清理自有 scope；只有未确认退出才阻断删除。补真实 holder 期限失败后 namespace 消失、邻近 scope 不受伤、错误原因仍可判断的有限对照。不要丢弃错误，也不要猜删未知资源。

**新增需处理的 Fowler smell：0。** 十二项启发式结合仓库 overrides 检查；实际 Store 仍直传 Host、scope/admin 与 writer 分离，三张 pointer maps 已删除，history restore 与 ProcessState/Wait 责任保留。后端专有能力没有扩大成通用 resource registry。

只读，未改repo、读凭据或复跑测试。52.406s正常/93.619s race为记录的count1/timeout120结果；321PG/319目录 absent仅覆盖准确登记项。首版red新增两个未知PG名称、旧04未知schema、07无CID与先前holder失败限制全部保留。whole02仍待本问题修复/复核、架构收益复核及准确新CI。

## 票10架构实施后的 Spec 复核

# Slice 02 — independent Spec architecture followup

Baseline `8e7438e071727e25aa69e17fb81b2e53c416b78e` → pin `c52e68b46c619df0c8e5df1b27a0b5dded3ef65f`。读取完整68项commit list及three-dot范围，逐项核对原58 AC、票10新增10 AC与全部采用决定；重点复核`c6220fd...c52`的18个recovery文件。只读审查，未运行测试或操作数据库。

**[P2] 已确认退出但失败的PG holder永久阻断scope清理。** `/workspace/lerna/conformance/recovery/owned_fixture_test.go:153`遇到任何join错误立即返回；`/workspace/lerna/conformance/recovery/owned_pg_fault_test.go:80`对已joined holder永久返回原result。真实lock callback返回SQL错误时，`:56`已经接收事务完成并记录错误；或者实际pool holder的10秒Tx到期，ReleaseAndJoin确认结束并保存deadline错误。随后每次Cleanup仍提前返回，无法Close业务writer/peer、Drop本轮确证namespace或Close admin；正常holder返回nil才能完整清理。

这违反票10 AC8 `/workspace/lerna/.scratch/lerna-02-durable-work/issues/10-owned-fixture-lifetime.md:20`的“重复调用安全”，及采用决定 `/workspace/lerna/.scratch/lerna-02-durable-work/architecture-decision.md:54`：“失败聚合报告而非吞掉…成功的步骤不重复制造故障，未完成步骤仍有归属并可在期限内重试。” 最小修复：区分退出未确认与已确认完成但事务失败；后者聚合诊断并继续安全的writer/peer/scope/admin收尾，已完成步骤不永久阻断重试。增加真实PG holder失败与正常对照，验证错误可判断、准确owned scope最终消失且邻scope不受损；无需放宽产品期限或引入通用资源框架。

**其余覆盖。** AC1–2稳定handle/实际Store、独立PG admin与SQLite成功Close后接替成立；AC3真实双库两次替换保留原receipt/Start/Job/hash且邻scope正常；AC4专有peer/单连接/完整copy与既有互斥断言保留；AC5实际ProcessState确认后重开/删除；AC6历史完整loader保留且Open不隐式Migrate；AC7取消/迁移拒绝/SQLite失败Close保留句柄；AC8存在上述缺口；AC9旧业务/故障断言与产品/公共1.0/0001–0005/四组27来源的增量未变；AC10读取真实normal52.406s/race93.619s、red与失败证据，未冒充独立重跑。

原Pool Run P2关闭仍有效；未发现新增scope creep或其它原58 AC回归。仅已登记321 PG/319目录的清理可确证；首次prototype两未知PG名、较早04未知schema及07无完整CID限制保留，不宣称资源全零。whole02仍待本项修复、architecture benefit复核及准确最终CI。

Standards：新增1项，轴内最严重P2；Spec：新增1项，轴内最严重P2。分别保留原结论，全部交原单一review fixer；whole02未退出。
