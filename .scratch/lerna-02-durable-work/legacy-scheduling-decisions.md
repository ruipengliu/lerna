# 票 05：legacy 未结工作的首次有限期限

2026-10-03。授权决策代理分析后采用。只读检查已集成 v2 Claim/Worker 与05当前草稿；不改被冻结0001/0002，不改record原命令含义或原接纳期限。此决定补充 scheduling-decisions 的legacy空隙。

## 决定

**v1/v2旧未结revision第一次由新调度器实际接管时，在同一个真实短事务中，以当前可信now一次性绑定有限执行策略。** 不用旧created_at、updated_at、原command.accept_before或迁移执行时刻作为执行期限anchor。

首次接管具体指：调度器选中已到due、可接替且scope/资格正确的原Job，取得需要的input/Job锁，准备领取该准确revision或为它保存新的有界等待阶段。此时以可信Clock读出的 `adopted_at` 保存稳定legacy policy identity、该revision和 `execution_deadline=adopted_at+5分钟`。策略绑定与该次Claim/阶段交接同事务成功；事务回滚没有生效绑定，下次才重新尝试。

NULL/无policy仅表示**尚未被新调度器接管**，绝不表示允许无限执行。新代码不允许未绑定有限policy就开始project、续跑无界旧处理或提交一个绕过新启动门禁的结果。第一次成功接管后，即使随后进程崩溃，重开也只能读取原adopted_at/deadline，不能刷新。

这是旧责任首次获得新调度规则的明确兼容处理；旧协议本来没有执行期限，不能追溯设一个早已过期的期限让真实oldpending升级后立即失败。migration只增加表示能力，不需要在整个积压队列上启动一场倒计时。调度器在健康有限扫描下才逐个接管；此兼容策略不承诺停机/从未启动scheduler时也有墙钟关闭SLA。

## 准确默认值

pure project演示的默认策略 identity 可固定为 `project-policy-1`，legacy首次绑定另外记录来源 `legacy-adoption`，新record为 `admission`。不由每次重启的“当前latest配置”覆写已绑定记录。

| 项目                   | 默认值                                                            |
| ---------------------- | ----------------------------------------------------------------- |
| 总执行时长             | 5分钟，绑定为绝对execution_deadline                               |
| 最大实际启动次数       | 3次，按原input revision持久计数                                   |
| 退避基础间隔           | 100毫秒                                                           |
| 最大退避               | 5秒；沿既定溢出安全指数退避规则                                   |
| 等待条件复查间隔       | 1秒                                                               |
| 无通知fallback扫描周期 | 最多1秒；更早due/lease/deadline优先                               |
| 默认scan batch         | 16；当前接口硬上限64保持                                          |
| legacy gate/失败注入   | 无gate、0次fixture临时失败、无永久失败注入；正常project可直接成功 |

单次调用和每次事务继续要求有限真实context；当前Claim租约范围沿已实现规则，租约/续租不能延长execution_deadline。上述5分钟是纯演示的默认执行预算，不是模型/生产超时保证。测试可由可信fixture配置采用更短、仍有限的策略验证边界，新record在其接纳事务当前可信now绑定该策略。

legacy默认规则固定为这组允许正常成功的值，不暗中对历史对象启用当前Host的故障/等待fixture。确有专门测试场景时应使用明确的新record及其已绑定策略，不能把升级兼容默认变成根据对象内容猜测模式。

## 旧v2 Claim如何过渡

新调度行为上线先停止新接纳/领取，排空旧事务，并让已运行旧worker完成或停止；无法有限退出时明确终止/隔离旧进程。新旧调度binary不混跑同一存储范围。这是本片兼容部署方式，不新增在线混版本调度协议。

- 已由旧worker在排空阶段真实完成的revision保留原完成/投影事实，不为done Job虚构新预算或再执行。
- 已持久v2 Claim尚未完成时，迁移原样保留Job ID、work_revision、completed_revision、claimed_revision、epoch及lease_until。不能把leased粗暴改ready、把completed推到work或以“升级了”声称旧执行已终止。
- 新调度器遵守原lease：未到期先等待；到期后按原Job产生更高epoch并领取**当前work_revision**，此时若该revision尚无policy才执行上述legacy首次绑定。原lease等待期间不启动新的5分钟预算，避免一个长原lease吞掉全部新处理时间。
- 若旧Claim是r1而当前输入/工作已到r2，维持二者关系直到正常接替；新epoch处理当前r2符合已批准最新投影合并规则。不伪造丢失的r1正文或声明它已成功投影。若升级前旧worker真实提交r1，新r2责任仍须保留。
- 旧完成消息在新epoch接替后必须被拒绝。升级后不以“旧Claim曾有效”为由绕过policy/启动资格检查接受未经新门禁处理的旧消息；需要保留成功的旧完成应在排空阶段先真实提交。

这里的进程排空不等于lease或epoch能停止外部效果；本演示只有纯本地hash，不扩展外部保证。

## 持久一次性与修订隔离

每个 `(owner,原Job,实际输入revision)` 的已生效policy/anchor/deadline只能绑定一次。用真实事务条件、唯一约束或等价原子前态防止两个worker分别写入不同期限；不要“读到空→脱离事务→无条件更新”。并发落败者读取胜者绑定，不能拿自己的较晚now延长期限。

以下情况均不得刷新已绑定deadline、policy或attempt预算：进程重启、重复通知、重新扫描、Claim续租、租约到期后新epoch、原command重传、migration再次执行。new epoch不是新业务revision。

新显式record的r+1可以拥有自己的有限策略/截止，已领取r的处理仍使用r的绑定；Trigger不能把r+1预算写进r的Claim，旧完成也不能把r策略覆盖回r+1。无须保存所有未领取中间正文；按既有最新投影语义保留当前/claimed实际所需绑定即可，具体表示由05实现者决定。

v2没有持久的实际启动次数，不能把lease_epoch当作此前物理尝试次数。legacy计数从此次策略接管后的实际启动记起；记录legacy-adoption来源，明确它只限制新调度规则下最多3次启动，不声称已重建历史执行次数或费用。一次开始事实已提交后崩溃可消耗该次尝试，重开不能重置。

## 最低真实两库验证

1. 真实v1/v2来源的pending Job，即使created/updated和原accept_before相对于fixture时钟已很久远，首次接管绑定T0+5分钟，并在正常对照中完成原Job；原固定applied/expired命令决定不改变。不能把原接纳期限当执行期限。
2. 首次绑定后重开，再以更晚可信时钟扫描/续租/接替，观察同一deadline和既有attempt_count；到原deadline明确关闭并保留处理依据，不能静默丢Job/改record回执。
3. 升级输入中有真实v2未结束Claim：未到lease不能领取；到期沿原Job更高epoch接替，首次policy在接替的可信now绑定；旧消息拒绝。不要手造旧表或伪造Claim数据代替真实v2领取。
4. 旧r1 Claim与新r2责任分别用受控两种提交顺序验证；不得丢新revision或重置旧预算。正常允许处理与期限/次数拒绝均有对照。
5. 并发首次接管只有一个policy绑定生效；迁移失败/重试不更新已生效deadline。通过受信Host工作/调度观察判断，不查私有表或统计内部调用次数。

schema可用nullable新增列或单独绑定记录表达legacy尚未接管；状态约束应避免新接纳遗漏policy，执行入口更不能将NULL解释为无上限。具体Go方法/SQL由05按实际04代码决定，不额外增加07/08业务依赖，也不改变旧migration源码。
