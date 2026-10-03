# 06 补充：无执行额度时的准确到期关闭

2026-10-03。只读核对实际root `7fa7594ff213a23c1cf156187cf70295c9270db4` 的 runtime.ScheduleStore、demo Worker Claim/Start/Finish、Service.Stop，以及PG/SQLite StopRevision和NextWake；06工作树仍为实施中。授权决策代理分析后采用，不预定Go函数名；06仍在实施，决定不作为通过证据。

## 决定

采用**受信的短事务维护关闭**，无需新执行Claim或执行额度。已到原execution deadline或已被准确授权停止的责任，不能因为tenant×lane quota=0、lane槽饱和或普通公平轮尚未轮到而永远无法关闭。关闭只撤销本库处理资格、保存准确终结依据并释放原占位，不做project、不保存成功Projection，不新增Job、不换lane、不借控制槽执行原ordinary工作。

这不是第9节缺配置时允许无限处理的例外：维护仍要求已安装且可核验的准确pool/member/config、可信维护执行身份和有限context，锁序仍为pool→input→Job→schedule。普通worker的执行权限不是自动的管理权；Host必须明确装配允许此维护责任的受信执行路径。可以与现有pool调度循环共用生命周期，不需额外守护服务/工作流。

## 实际接口差距与最小行为

当前05 Start/Finish到期分支通过closeState调用Claims.Complete，后者要求有效执行Claim。若06先因quota拒绝领取，这条分支不可达，不能只把quota检查放到Claim之前便认为到期行为完整。

当前Service.Stop已经示范无新Claim的可信原revision关闭，ScheduleStore.StopRevision两库SQL能在无Claim或claimed_revision匹配时清除绑定、推进completed_revision，并在work_revision更大时保留ready。但它无到期时间参数，不能自己决定expired；且当前接口只返回error，SQL零行也可能nil。因此可复用其条件关闭机制，但consumer必须锁后裁决真实状态并核验所需结果；不能把一次nil返回当成原Job已被关闭。具体补充锁/观察/条件返回的小端口由06按实际实现选择，不要求强塞一个假Claim给Complete。

维护路径必须：

1. 从真实持久policy/Job发现准确候选，扫描不受执行quota=0或lane满额过滤；due、deadline和停止事实是不同依据。已到deadline的leased工作也要被发现，不能只扫`scan_at <= now`从而等一个更晚lease才知道deadline。只读候选不是关闭授权。
2. 在准确owner的同一Store短Tx中取得pool→input→Job必要锁，锁后重取可信now，读取原revision policy、状态和当前Claim。仅`now >= 原持久deadline`可记expired；普通due到时不能记expired。已经success/permanent/expired/stopped的原revision保持终态和原因，不反复覆盖。
3. 关闭准确revision与保存`expired/deadline`或已有可信stop依据同Tx。attempts、原policy/deadline、Command固定receipt保持；没有发生新的Start，不能加一次attempt。关闭操作不算成功的执行容量分配，不推进执行公平序号。
4. 对应同revision Claim即使lease尚有效，也可由此受信控制在本库撤销：清除原worker/claimed_revision/lease绑定，或其他等价条件fence；后续旧Finish/Renew/Release不得成功或释放新Claim槽。无需为了使旧消息失败而虚增一次“领取”；若改epoch，必须准确解释其为fencing且处理准确整数上限。此撤销只保护本库提交，不证明旧进程/外部效果停止。
5. 只有实际关闭的Job责任释放队列占位；仅原Claim被撤销时释放原Claim占位。work_revision仍有更高未结责任时保留原Job、其ready责任和队列位置，不把它变成done或重新找槽入队。所有计数变化在同一个协调事务中完成，重复维护必须幂等。

### 并发新修订及不同Claim

关闭r1时如果当前work_revision=r2，r2未到期且未停止，不能写r2的schedule、deadline、attempt或Projection，也不能将completed_revision推进到r2。r1对应Claim可被fence，原Job继续ready供r2在正常quota下执行。

如果要关闭的r与当前有效Claim属于**不同revision**，不得借关闭r清除另一个Claim。例如r2采用更短期限已到期，而r1的有效Claim仍存在：可先固定r2确实expired的事实，但不能宣称整Job已done/空出队列；保留r1原绑定，待它正常结束或原有限lease到期后，再在维护事务中核验并收尾最新已关闭责任。过期租约可以按原条件解除失效占位，不能刷新lease等待。当前最新投影合并规则允许在原Claim已失效时关闭最新到期责任；不得凭r2的期限把r1也标成expired或声称它成功执行过。

反向竞争也须保护：维护扫描后有新Record提交r+1，关闭时重读真实work_revision；新的未结责任仍在。先完成成功后维护到达，保留success；先到期关闭后旧完成到达，旧完成不能保存Projection。任何无Claim关闭都不能是无条件UPDATE完成所有修订。

## 哪些可以等待，哪些必须收尾

- **普通ready/due或gate/retry到期、尚未到execution deadline：** quota=0/饱和时可保留原未结状态等待额度，不能运行hash，也不能把容量不足当永久失败。若已有绝对deadline，此等待不延长期限；到deadline转入上述维护关闭。
- **已到持久execution deadline、已持久停止：** 在配置有效且维护有服务机会时必须有限发现并准确关闭，不能等执行quota恢复。已耗尽持久attempt预算且没有有效在跑尝试时，同样可以按原规则明确关闭；不能把一个仍允许完成的最后一次已Start工作仅凭attempts==Max提前失败。
- **legacy尚未绑定policy：** 不从旧created/accept_before推断已过期。quota阻止实际接管时可以保留等待；只有合法首次接管才一次性绑定既定5分钟预算。此未接管兼容状态没有凭空产生的新墙钟关闭承诺。
- **缺失/不可读配置、owner未注册、服务停机或数据库不可用：** 沿第9节failclosed并保留事实，明确报告无法维护，等待受信配置/服务恢复。不能新造默认pool、改owner或假称已关闭。正常变更不得删除含未结责任的member，因此不应把这一故障分支当作日常停止开关。

## 有界服务机会与扫描

最小装配在每次有界pool调度服务机会给维护一个有限扫描预算，然后再做执行配额选择；maintenance空闲/额度不足不能让整个循环停止提供维护。每批至多64，按稳定游标/有限轮次继续，跨tenant/member及跳锁页面不能每次从同一热点前缀重来。独立lane执行保留容量不因此被维护长事务占满，纯扫描和每次修改均有context/锁期限。

界限以“持久候选有限、pool最多64准确members、worker持续运行、数据库可用、没有永久锁占用、可信Clock推进”为条件。声明实际page数和每次服务预算：稳定有限候选集合一轮有限page机会可发现到期责任；锁竞争释放后后续有限轮可关闭。正在等不同revision有效Claim的收尾还受其原有限lease约束。无限新入队不能无限延长已开始轮次，使用已有有限轮次边界/游标原则。

这不是执行N次成功分配公平界，也不是无条件墙钟SLA。quota=0下可能没有一次执行分配，维护仍须继续。fallback最多1秒和适当deadline wake保持有限，不以quota恢复事件作为唯一唤醒；已到期但暂时跳锁的项不能造成无限busy-loop。

## 两真实DB最低验收

1. 新record已绑定有限deadline，tenant ordinary quota=0：deadline前不执行、不加attempt；到期后无需任何新执行Claim便观察expired/准确关闭，原receipt保持，Projection不存在。另一个有正quota且未到期的正常对象经真实Start/Finish成功。
2. ordinary全lane槽被另一个真实有效Claim占满，目标在waiting/retry/ready到deadline仍关闭；不把目标移入control、不占用新Claim槽、不改变执行公平序号。关闭后队列/有效Claim计数与实际责任一致。
3. 目标已经真实Start且lease尚有效，在deadline维护关闭，再投递旧Finish/Renew/Release：均不得保存成功或伤害新epoch占位；正常同Claim deadline前Finish对照成功。
4. 维护与新revision接纳两种提交顺序；旧r关闭不丢新r+1，已关闭最新r遇不同revision活跃Claim时不误清；lease结束后原Job可以有界收尾。每个场景观察准确schedule/Job责任和Projection，不用私有表/内部调用次数证明业务效果。
5. 超过一页的到期候选及受控可跳锁前缀，quota始终为0，有限分页仍关闭健康后缀；重开继续原游标/事实，原deadline和attempt不刷新。
6. 缺配置时既有receipt可查、维护显式不可用且责任保留；恢复原有限配置后到期责任关闭，不能临时接受无pool成功处理。所有故障配正常对照和有限测试截止。

这是06现有额度/到期规则的交接细化，不添加公共1.0方法、Task状态或跨owner业务，也不把05或06草稿冒充已经验证。06据最终实现记录真实扫描界和端口变化，root复核原02套件仍经过同一成功门禁。
