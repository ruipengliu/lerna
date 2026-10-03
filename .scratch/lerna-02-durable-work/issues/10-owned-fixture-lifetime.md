# 10: 测试scope稳定归属与真实writer接替

**What to build:** 相同的PG/SQLite恢复行为测试通过稳定fixture handle关闭、重开或交接真实writer，原Command/Job/Projection事实保持；fixture在有限退出后准确清理自己创建的scope，普通caller不再维护creator保活、Store-pointer注册和后端关闭次序。

**Blocked by:** 02核心01–08、额外09与最终两轴review fixes已合入（当前实现基线`6783307ebe7d802f78f9aa7bdb1a1464ec5749e1`）。无03或后续切片依赖；root整片证据/最后CI不是隐藏业务验收。

**Status:** resolved

**Decision:** [采用的架构决定](../architecture-decision.md)。用户授权代理已完成5轮11项frontier选择并确认共享理解。唯一Worth exploring候选被选为本次可逆测试fixture deepening，无新增ADR/领域词汇。

## Acceptance criteria

- [x] 通过一个当前recovery测试私有fixture module统一稳定owned scope与可替换writer generations；普通factory返回稳定handle。六类既有caller（admission、work、wait、retention、process、historical lifecycle）及新增pool正常路径移除三张Store-pointer maps、replacement/reopener手工登记、为保PG creator的backend分支。不以移动maps/helpers代替caller interface简化。
- [x] PG私有admin在真实CREATE成功后登记归属，独立于所有业务writer；首次/连续重开及同scope peer不继承删除权，关闭writer不破坏最终Drop权限。SQLite仅管理本轮owned路径，旧writer成功Close后才能接替；失败排空不丢句柄、不删活跃文件、不强开替代。产品same-Store/owner/active Tx规则不变。
- [x] 首个TDD tracer经新fixture interface，在两真实DB完成Record→Claim/Start→Close/reopen两次→公开command.get/Host Observe/正常Finish，保持原receipt、原Job和准确投影；显式fixture cleanup后自己的namespace/path确实消失，另一独立fixture经Host仍能读原事实。记录red→green，不以内部map数量/调用次数或私有业务行证明。
- [x] 同scope PG peer、等待故事单连接及真实fault-holder由专有adapter明确配置，module内部登记其有限关闭；fault-holder Tx仍仅该writer为10秒，业务writer仍3秒，失败release及11秒join保持，原有限故事/whole timeout120不放宽。SQLite第二writer排他和真实闭库完整file-copy仍实际验证，复制到新owned path的scope拒绝与原scope正常对照均保留。
- [x] 现有process module继续处理实际private配置管道、帧、Kill/Wait/reap；fixture负责先释放父writer再交接准确scope，关联真实child的有限退出协作，child无创建/删除权限。正常/失败/取消后只有确认child退出才重开排他writer或清理scope；普通caller不再读map或分支处理PG creator/SQLite Close。启动/Wait结果未知不得当作已经退出。
- [x] historicalFixture继续验证/恢复真实v1/v2 artifact及原dump/COPY/版本/fault；它只改用稳定scope与writer生命周期。支持其实际“创建空owned scope→历史恢复→打开当前writer”次序；不得在Reopen隐式Migrate，不把完整历史loader/psql/docker变成fixture通用资源插件，不制造历史业务行。
- [x] setup的Open/Create/Migrate/首writer任一步失败均有有限收尾：未确认Create不获owns，确认Create后的失败可清理其scope；replacement Open失败不丢stable scope。真实取消/Close排空及既有迁移fault提供失败对照；失败报告保留可判断原因而不打印DSN/密码。错误或Close未完成不得静默变nil。
- [x] cleanup使用独立有限context，先封新writer、释放/join登记借用、Close所有writer/peer，确认后删除自有scope，最后Close admin；重复调用安全，部分失败保留真实归属/残留并报告。不得依赖普通caller安排creator cleanup次序，不增加无界后台goroutine、sleep、猜删scope或CID。基础设施观察只核验本轮确证scope，不读取私有业务表冒充业务断言。
- [x] 旧Host/public command.get和明确storage机制测试surface、所有正常/故障断言保留。仅真正由新interface覆盖的浅helper镜像检查可以替换；不删唯一后端反例。当前产品代码、公开1.0、0001–0005及四组全部27项immutable来源校验均无改动；新module不抽取未来03的runtime/Decision实现。
- [x] 相关targeted双库生命周期/failure/process/pool tests、`make check`及受影响完整顺序count1 integration和race按现有必需入口通过（normal/race各timeout120）；记录真实命令、pin、duration、red/失败历史、scope cleanup证据和限制。必须依赖缺失时硬失败，不skip。后续review与准确push CI由root核实，不以此前核心CI冒充本票的新验证。

## Implementation guidance

Interface用当前真实需要的动作描述：获得当前实际Store、关闭/替换writer、给既有process模块交接、最终cleanup；PG peer/SQLite copy/history初始化保持窄的backend专有能力。具体方法名和私有文件组织由实施者决定。测试module不暴露任意资源注册表、genericcleanup callback或产品CRUD代理；实际Store仍直接交给Host。

先完成一条完整双库生命周期tracer，再迁移其余caller和失败路径；不是先新增helper随后把正确退出推给另一票。内部可分reviewable commits，但本票必须实现实际caller责任收拢和真实关闭证据。

## Limits

本票只改善测试module的depth、leverage和locality，不宣称生产cleanup、未知资源回收、断电/跨区恢复或新的domain行为。历史04未知schema、07未知Docker CREATE/无完整CID的限制保留。HTML只读报告已生成但xdg-open exit3，不声称GUI验证。

## Comments

2026-10-03，root按已授权Astra选择/grill采用并发布；当前前置核心和review fixes均已完成，6783307准确远端CI success。该票已claimed，交独立worktree/branch实施；不在集成工作树修改产品，不提前关闭本票或整片。


2026-10-03，票10本地实施完成，代码 `1863fc49a0e57c087ee599a8296c2ccdfcf96a6e`，独立worktree `/tmp/lerna-worktrees/durable-work-10`、分支 `codex/durable-work-ticket-10`。在该代码提交后合并root提供的准确 `c6220fdf194e1f954f3b2c65d1cf31fc839c359c` 一次，结果Already up to date。随后只更新本票证据，没有产品或测试源码改动；root整合、独立两轴/架构收益复核及准确新远端CI仍待完成，whole02保持in-progress，03未开始。

**实际interface与caller迁移。** 当前recovery package私有 `ownedFixture` 保存稳定物理scope与当前实际writer；factory返回handle，Host继续直接消费真实Store。独立PG admin只在真实CREATE成功后获得删除权，并在故事开始前登记准确scope；所有业务writer与同scope peer都不继承删除权。SQLite管理本轮owned目录，Close成功后才能替换或读取完整闭库文件复制到新owned path。admission/work/wait/retention/process/history及pool caller的三张Store-pointer maps、手工replacement注册和PG creator保活分支已删除，没有把maps搬到新模块或引入CRUD proxy。历史module保留完整真实dump/COPY/版本/SQL迁移fault；Reopen不隐式Migrate。process module保留私有配置管道、帧、Kill/Wait/reap，仅与fixture做有限借用协作。实际Wait必须得到ProcessState才确认退出；未知spawn/Wait保留借用状态并拒绝重开/删除。

**实际TDD和失败记录。** 沿用已授权fixture、Host/public command.get及本轮namespace/path观察seams，读取implement-spec/TDD/tests/mocking/codebase-design/DEEPENING。首条真实双库tracer经Record→Claim/Start→连续两次实际writer替换→公开原receipt/Host原Job→正常完成hello准确投影→显式fixture cleanup→相邻独立fixture仍可读取原事实。首个可运行red `go test -count=1 -tags=integration -timeout=120s ./conformance/recovery -run '^TestOwnedFixtureTwoReplacementsAndIsolatedCleanup$'` 实际0.554s：两次接替与原事实已经成功，但PG报owned namespace remains，SQLite报owned directory remains；补齐admin Drop与owned-path删除后同命令green0.553s。这不是只因缺interface编译失败的red。

初版red Cleanup只关闭writer，主与neighbor的两次PG CREATE均成功却没有Drop，且当时未在进程外保存随机名称。因此本轮新增 **两个无法按准确名称确认归属的PG schema残留**；不得说只一个、不得按prefix/时间/业务行形状猜DROP，不声称它们已经清理。这与旧票04未知schema是独立限制。对应两个SQLite目录在实施者独自创建的overlay TMPDIR内，随后按准确路径登记并删除。正式实现从随后green起，在CREATE成功或owned目录创建后即时写入/fsync/关闭外部 `/tmp/lerna-02-arch-10-owned-scopes`；登记错误停止故事并经仍活的admin做独立有限精确收尾。

迁移后的两个新增测试作者错误如实保留：第一次focused失败1.985s，空历史scope正常恢复writer后遗漏显式Migrate，Host pool setup报missing durable_pools；修复的是测试显式初始化，未把Migrate藏进Reopen。第二次focused失败5.798s，重建原wire调用future()得到不同截止，两库正确返回idempotency_conflict；改为保存原wire。两个失败轮的登记scope与TMPDIR均完成精确收尾。之后focused正常10.254s、focused race44.132s通过；race过滤包含现有实际Process名称，最终完整套件包含全部process故事。

**失败生命周期和后端专有行为。** 真实取消的setup/首writer/replacement Open仍保留errors.Is(context.Canceled)，正常重开并重传原wire保持原决定。复用完整历史真实迁移拒绝fault验证有限cleanup。SQLite真实忽略取消的callback使Close超时，fixture保留原writer与活跃文件，排他peer实际被拒绝；callback确认退出后重复cleanup成功。本轮真实child仍持预提交事务时显式fixture cleanup实际Kill+Wait，再删除自有scope。PG专有single-connection wait、独立peer、锁范围和capacity正常/故障断言保留；真实PG transaction holder由窄的fault adapter登记到fixture，cleanup先release/join，再Close writer/peer、Drop scope、Close admin。业务writer3s、独立fault holder10s/失败join11s、whole正常/race120s未放宽。没有fake数据库、业务私表成功断言、调用次数或map长度断言，没有通用resource hooks或未来03模块。

**完整验证。** 锁定 `make bootstrap`、`make fmt`、`make check`、`make test-race`、`go mod verify`通过。四组v1/v2全部27项SHA256SUMS通过，`.gitattributes`、0001–0005、全部冻结来源与cleanbase零差异；完整baseline `8e7438e071727e25aa69e17fb81b2e53c416b78e` 至本票diff whitespace检查通过。完整 `make test-integration` 实际52.406s（wall54.014s），随后独自执行 `go test -race -count=1 -tags=integration -timeout=120s ./conformance/recovery/...` 实际93.619s（wall95.430s），两者顺序执行，各用fresh注册的 `/workspace` overlayfs TMPDIR，均exit0，没有skip或延长期限。实际PG18.6、pgx/v5 v5.11.0、go-sqlite3 v1.14.52；本机psql17.11完整恢复实际成功，CI固定18.6仍由root核实。基础make checks曾与focused race尾部短暂重叠，这是本轮调度错误；不把所有早期运行都说成顺序，最终完整正常/race没有与任何broad check或另一DB suite竞争。

**清理和证据范围。** 所有测试结束后，独立有限查询只观察外部登记的准确namespace，实际321个distinct已确认PG scopes全部不存在；319个已登记SQLite/red/temp目录全部不存在；同test database/current user的其他session为0，未留下recovery子进程。该查询不枚举未知schema，不将两初版red残留或旧未知资源计入清零结论。完整命令、日志、代码pin、故障历史及limits保存在 `/tmp/lerna-02-architecture-10-evidence.md` 与对应logs/registry；worktree与审核证据保留，没有push/PR或移除worktree。旧票04未知schema、票07CREATE无完整CID、此前118.936s全race竞争失败/7.282s真实holder期限失败与窄修复记录均保留。没有生产cleanup、原生SQLite Commit故障、断电、跨区耐久或全部资源为零的结论；whole02后续退出仍由root完成。


2026-10-03，root整合至 `c52e68b46c619df0c8e5df1b27a0b5dded3ef65f`，准确远端 CI37160694293 success。后置两轴独立审查各发现1项P2：已确认退出但失败的PG holder历史事务错误永久阻断后续Cleanup。票10重新claimed，AC8及修复后的AC10待验证；先前通过与失败历史保留。交原单一review fixer处理全部新增发现，不以绿色CI关闭未覆盖路径。结构收益独立复核已实现，cleanup正确性尚未退出；未知资源限制保留。


2026-10-03，新增两轴同一P2由原单一review fixer修复，实际代码 `f56d93095304f0956c23b5641d9b7b1e222c40c1`（仅三个recovery测试文件）。已确认退出的PG holder失败以errors.Is/As聚合报告，仍推进writer/peer、自有scope及admin收尾；未确认callback保留scope/句柄，确认后可重试。正常holder/两库接替对照、真实PgError42P01+取消聚合、真实短join未确认及重复cleanup、邻scope Host/public receipt均通过。核心runnable red0.328s→green0.302s；补测遗漏空schema显式Migrate的17.203s失败及修正保持记录。业务3s/holder10s/join11s/whole120s未放宽，没有泛用resource framework。

AC8/10本地复验完成：makefmt/check/base-race、27来源/modverify、完整顺序count1 integration50.431s/race93.834s（各timeout120）通过，无DB/broad竞争或skip。独立有限观察本fix即时fsync registry，实际285个PG namespace及265个SQLite fixture目录全absent；仅清理自己7个准确登记overlay TMPDIR。单初始red残留1个已登记PG namespace早已精确回收，不扫描未知scope。详细命令、actualcode、real失败与限度见[本次修复证据](../fixture-cleanup-fix-evidence.md)及`/tmp/lerna-02-fixture-cleanup-fix-evidence.md`。先前两prototype未知PG名、旧04未知schema/07不完整CID及全部历史结果保持；本票实现resolved不代替独立两轴/结构收益确认和准确新CI，whole02仍未退出。
