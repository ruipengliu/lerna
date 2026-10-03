# 10: 测试scope稳定归属与真实writer接替

**What to build:** 相同的PG/SQLite恢复行为测试通过稳定fixture handle关闭、重开或交接真实writer，原Command/Job/Projection事实保持；fixture在有限退出后准确清理自己创建的scope，普通caller不再维护creator保活、Store-pointer注册和后端关闭次序。

**Blocked by:** 02核心01–08、额外09与最终两轴review fixes已合入（当前实现基线`6783307ebe7d802f78f9aa7bdb1a1464ec5749e1`）。无03或后续切片依赖；root整片证据/最后CI不是隐藏业务验收。

**Status:** claimed

**Decision:** [采用的架构决定](../architecture-decision.md)。用户授权代理已完成5轮11项frontier选择并确认共享理解。唯一Worth exploring候选被选为本次可逆测试fixture deepening，无新增ADR/领域词汇。

## Acceptance criteria

- [ ] 通过一个当前recovery测试私有fixture module统一稳定owned scope与可替换writer generations；普通factory返回稳定handle。六类既有caller（admission、work、wait、retention、process、historical lifecycle）及新增pool正常路径移除三张Store-pointer maps、replacement/reopener手工登记、为保PG creator的backend分支。不以移动maps/helpers代替caller interface简化。
- [ ] PG私有admin在真实CREATE成功后登记归属，独立于所有业务writer；首次/连续重开及同scope peer不继承删除权，关闭writer不破坏最终Drop权限。SQLite仅管理本轮owned路径，旧writer成功Close后才能接替；失败排空不丢句柄、不删活跃文件、不强开替代。产品same-Store/owner/active Tx规则不变。
- [ ] 首个TDD tracer经新fixture interface，在两真实DB完成Record→Claim/Start→Close/reopen两次→公开command.get/Host Observe/正常Finish，保持原receipt、原Job和准确投影；显式fixture cleanup后自己的namespace/path确实消失，另一独立fixture经Host仍能读原事实。记录red→green，不以内部map数量/调用次数或私有业务行证明。
- [ ] 同scope PG peer、等待故事单连接及真实fault-holder由专有adapter明确配置，module内部登记其有限关闭；fault-holder Tx仍仅该writer为10秒，业务writer仍3秒，失败release及11秒join保持，原有限故事/whole timeout120不放宽。SQLite第二writer排他和真实闭库完整file-copy仍实际验证，复制到新owned path的scope拒绝与原scope正常对照均保留。
- [ ] 现有process module继续处理实际private配置管道、帧、Kill/Wait/reap；fixture负责先释放父writer再交接准确scope，关联真实child的有限退出协作，child无创建/删除权限。正常/失败/取消后只有确认child退出才重开排他writer或清理scope；普通caller不再读map或分支处理PG creator/SQLite Close。启动/Wait结果未知不得当作已经退出。
- [ ] historicalFixture继续验证/恢复真实v1/v2 artifact及原dump/COPY/版本/fault；它只改用稳定scope与writer生命周期。支持其实际“创建空owned scope→历史恢复→打开当前writer”次序；不得在Reopen隐式Migrate，不把完整历史loader/psql/docker变成fixture通用资源插件，不制造历史业务行。
- [ ] setup的Open/Create/Migrate/首writer任一步失败均有有限收尾：未确认Create不获owns，确认Create后的失败可清理其scope；replacement Open失败不丢stable scope。真实取消/Close排空及既有迁移fault提供失败对照；失败报告保留可判断原因而不打印DSN/密码。错误或Close未完成不得静默变nil。
- [ ] cleanup使用独立有限context，先封新writer、释放/join登记借用、Close所有writer/peer，确认后删除自有scope，最后Close admin；重复调用安全，部分失败保留真实归属/残留并报告。不得依赖普通caller安排creator cleanup次序，不增加无界后台goroutine、sleep、猜删scope或CID。基础设施观察只核验本轮确证scope，不读取私有业务表冒充业务断言。
- [ ] 旧Host/public command.get和明确storage机制测试surface、所有正常/故障断言保留。仅真正由新interface覆盖的浅helper镜像检查可以替换；不删唯一后端反例。当前产品代码、公开1.0、0001–0005及四组全部27项immutable来源校验均无改动；新module不抽取未来03的runtime/Decision实现。
- [ ] 相关targeted双库生命周期/failure/process/pool tests、`make check`及受影响完整顺序count1 integration和race按现有必需入口通过（normal/race各timeout120）；记录真实命令、pin、duration、red/失败历史、scope cleanup证据和限制。必须依赖缺失时硬失败，不skip。后续review与准确push CI由root核实，不以此前核心CI冒充本票的新验证。

## Implementation guidance

Interface用当前真实需要的动作描述：获得当前实际Store、关闭/替换writer、给既有process模块交接、最终cleanup；PG peer/SQLite copy/history初始化保持窄的backend专有能力。具体方法名和私有文件组织由实施者决定。测试module不暴露任意资源注册表、genericcleanup callback或产品CRUD代理；实际Store仍直接交给Host。

先完成一条完整双库生命周期tracer，再迁移其余caller和失败路径；不是先新增helper随后把正确退出推给另一票。内部可分reviewable commits，但本票必须实现实际caller责任收拢和真实关闭证据。

## Limits

本票只改善测试module的depth、leverage和locality，不宣称生产cleanup、未知资源回收、断电/跨区恢复或新的domain行为。历史04未知schema、07未知Docker CREATE/无完整CID的限制保留。HTML只读报告已生成但xdg-open exit3，不声称GUI验证。

## Comments

2026-10-03，root按已授权Astra选择/grill采用并发布；当前前置核心和review fixes均已完成，6783307准确远端CI success。该票已claimed，交独立worktree/branch实施；不在集成工作树修改产品，不提前关闭本票或整片。
