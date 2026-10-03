# 06：新加入与重新合格租户的公平等待次序

2026-10-03。用户授权的决策代理在/tmp分析，主任务核对后采用。已读root scheduling-decisions第7节、capacity-handoff第5/6节，以及06当前draft的demo work.go/pool_worker.go/pool.go、PG PoolPage/SavePoolCursor等。06仍实施中；N=2既有green不代表以下动态资格边界已通过。本决定不阻止实施者继续独立分页/Run测试。

## 决定

**保留已批准的N机会界与“新/重新合格者加入当前尾部”，将每lane的固定tenant索引环改为耐久、有界、去重的tenant等待FIFO。** 当前最多64准确成员，tenant数也≤64；直接在现有pool协调行的lane状态中保存至多64个准确tenant ID及必要观察事实即可，不需通用消息队列、priority engine或新服务。PG/SQLite仍用现有pool锁和短事务。

当前PoolConfig.Tenants按ID排序，PoolCursor.Tenant只索引该固定数组。它能实现稳定成员的一轮轮转，但无法记录某tenant曾无工作/无quota、何时重新进入当前等待集合。例：A已服务，C已在等，B从无工作变为有工作；固定索引若恰在B，会给B插到C之前。简单复位Tenant、重排config列表或只看lastAllocated=0都不能保证尾部加入。

## 最小状态与更新规则

lane保留已存在的成功分配Sequence；额外的有界FIFO保存当前等待tenant身份。若需要对应既定等待观察，保存首次**观察到合格并入队**的可信时间/序号，不能把它伪称数据库未观察期间的精确资格变化时刻。正在解析head的After/Through继续使用现有keyset/highwater，并明确绑定head身份；队列刷新不能把某tenant的游标转交给另一个tenant。

每次实际选择在持有pool锁后，用锁后的可信now刷新资格；成员≤64，可以一次有界分组查询或≤64个索引EXISTS/额度检查。查询只返回runtime候选存在性/最早due等有限摘要，不加载所有Job正文、不锁其他owner业务输入。

1. 保留原队列中仍具候选资格的tenant和原有相对顺序，不因其他tenant新到达重置它们的等待。
2. 已移除成员、tenant quota=0、当前tenant有效Claim已占满自身quota、或完整候选谓词确认当前没有到due工作的tenant退出等待队列。不存在原政策不能自动当作无限执行；legacy接管规则不变。
3. 从未入队/已退出而本次重新具资格的tenant统一追加当前尾。单次刷新同时发现多个新候选，先按可观察的最早就绪due、再按稳定tenant ID确定tie，不让字典序覆盖已经在队的等待者。该tie只在新入队组内排序。
4. 全lane容量满只是暂停执行分配，**不能清空仍合格tenant的队列**；否则槽释放后重新按ID排序会抹掉真实等待。tenant自身满额则按上条退出，恢复空额后尾部重新加入。
5. FIFO head最多获得一次实际成功Claim；Claim、原pool登记、配额变更、Sequence递增与头部移除/尾部重新加入同一个事务。成功后若仍有候选且仍有tenant空额则移尾，否则退出，待下一次重新合格再尾部加入。失败/跳锁不能增加成功Sequence。
6. 每个批量Claim机会重复此规则；不能保存一次资格快照就让原head吃完整批。若发生时间/其他事务变化，下次选择再次核验。

“候选资格”是可以用runtime事实发现的注册成员、tenant空额及准确lane下有due且未关闭/可接替的候选；真正资格仍在实际Claim/Start按原policy、授权和锁后时间复核。它不是无限提前证明每个候选能够成功执行。已到期/已停止责任由既定维护关闭路径处理，不靠其反复夺取执行配额。

## 分页和双入口保持同一事实

未完成head有界页扫描时保留head及After/Through，不把“本页锁冲突/尚未看到后缀”当作失去资格从FIFO删除。不允许其他tenant趁该head还在有界发现阶段反复拿下一轮成功分配。

一轮固定highwater全部扫描完而没有可领取候选，可以退出该轮等待；后续在新扫描机会重新发现资格则入当前尾。若跳锁使物理候选仍存在，重新发现不表示能够瞬间执行；它最多再次占一个有限发现轮，不能永久持锁等待。稳定候选中无长期锁的健康tenant仍会在有限分页后取到Claim。持续新增Job不会延长既有highwater轮次，旧游标消失、空页及回绕必须准确处理。

发现存在性可以独立索引EXISTS，不能调用PoolPage然后意外改写一个尚未轮到tenant的After/Through。单页非空仅说明有候选；空页也要区分当前keyset后没有和整个tenant无候选。数据库查不到与缺少Host worker装配是不同情况：已登记且有工作的owner缺worker是配置错误，不得把tenant视为不合格偷跳过。

PoolWorker.nextOwner只给建议，Worker.claimLanes的真实owner事务仍须刷新/验证同一持久FIFO。另一worker抢先改变队列时，以当前队头为准重新选择，不在旧建议下给错误tenant发Claim。单owner旧入口也不能另维护一套数组环；head属于另一owner时交由pool driver，不消耗该head机会。重开、多个Store/worker及不同lane共享各自相同的耐久顺序。

配置revision更新应按准确tenant身份保留仍有效的队列次序，而不是保留数组下标然后mod新长度。新增成员只在具候选资格时入尾；移除按已有未结责任/drain限制执行。不得因更新无关配置而重置Sequence/等待。相关06迁移仍未发布时可按本分支规则调整；不改已发布02迁移。

## 有界性与范围

对入队后持续具备资格的目标，前方最多N−1个tenant。每个成功分配者移到其后；新来者也只能在其后，故目标在至多N次该lane成功分配机会内取得Claim。N为该有限竞争范围tenant数（≤64），不是单次candidate页长度。

新/重新合格者的发现必须有界，不能靠“下一次恰好轮到字典序索引”才发现。上述每次选择刷新≤64个tenant提供最小明确发现机会；暂停期间的有限fallback也继续检查资格/维护。发现机会与成功分配机会分开观察，不能保证停机、数据库不可用、无限锁占用或没有空槽时的墙钟上界。公平次序证明不能替代实际tenant空额、准确Claim登记和原Start门禁。

同一次刷新观察到的未知历史先后，用明确due/ID tie裁决；不声称恢复未记录的纳秒级事件次序。已经耐久入队者先于后来首次观察到合格者，这一尾部约束不降低。

## 必需真实两库反例

1. A/B/C（稳定ID顺序故意会让B插队）：A、C已有候选且A先成功，B此前无工作；在C已等待时为B真实Record，随后必须先C再B，而非固定索引B先。各对象经实际Start/Finish有正常hash。
2. B已等待后因quota=0/自身满槽退出；C等待期间恢复B的quota或真实释放其Claim，B从尾部重新加入。全lane满与tenant满分别测试：全lane满后释放不能把整个已有等待队列清零。
3. 在上述等待中重开Store/进程，或由多个worker并发Claim，次序/Sequence一致，只有真正成功Claim递增；同tenant多owner仍一份队列位置。
4. head有超过64个候选、前页受控跳锁，健康后缀最终被发现；期间新tenant到达不能替换head游标，旧hottenant不能越过未解决轮反复分配。
5. 配置添加新member（其ID会改变旧排序下标），既有等待者次序仍不变；同lane配置revision变化不会重置已服务事实。失配/缺worker仍明确错误。

观察经已有Host PoolObservation扩展出的有限tenant等待/Sequence事实和真实Projection；不以私有表或调用次数冒充公平。现有N=2和Run/分页测试继续保留，新测试覆盖原静态环未证明的动态尾部加入行为。
