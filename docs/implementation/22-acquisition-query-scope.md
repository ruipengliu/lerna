# 22 票获取驱动的请求级观察绑定

本增量补齐真实获取/搜索驱动内部的 Outcome 和证据观察，继续原查询上限，不代表预算组合或整票验收完成。

fetchexecution 与 searchexecution 的可选 ObservationScope 接收原 execution.Request，返回当前调用的 OutcomeStore 与 Evidence。Start/Inspect 在原能力身份检查之后绑定局部驱动副本，不修改共享驱动、原请求、Fetcher/Searcher、网络上限或恢复身份。未配置的基础装配保持旧路径；研究获取与搜索入口现明确配置。

fetchqueries 使用完整原 ActionBinding 为每次 Outcome 读取与证据 GET/READ/LOOKUP 在 I/O 前向 Core 计费，失败不退款。它不加载最新 Worker、不取得新租约、不提供新额度。Evidence 使用任务执行专属长度表，每次 READ 仍实际核验 Content 来源与当前授权，独立观察者不能预热该表。

AttemptStore.Inspect 的原尝试身份核对，以及 Begin/Complete 的网络预留和终结事实事务保留原行为；它们不因此免除后续 Outcome 或 Content 数据读取。取消后保存有限事实与任务查询资格的交互仍需专门验证，不能视为本增量已覆盖。

## 真实路径证据

真实 Core、SDK、SQLite、Content、HTTP 获取测试先观测原四次执行 Content 读取，预期七次而失败（0.753s）。接入后为原四次读取加恢复 LOOKUP、GET、READ，以及独立的一次 Outcome 查询，普通测试通过（0.905s）。旧行动结算后再调用驱动 Inspect 必须拒绝，不能将原资格升级为当前资格。

首次相关 race 组合失败（8.528s）：新 SDK 用例在同任务答案交接断言失败；未据此宣称通过。已补充状态/等待原因/查询数诊断，定向核验继续运行。

研究冻结组合接入后失败（31.484s）：冲突行动阶段已使用 60–62 条查询，两种部分失败为 57 条，答案阶段均耗尽 64。单页失败实际 Outcome 4、总查询 57：驱动与行动宿主各读两个终结事实。答案存储重开的账本断言因此更新为每个操作由驱动、行动宿主、新答案宿主分别读取。网络等待仍可能产生额外实际隐私读取，不能用固定耗时计数替代硬上界。

原 64 查询/模型/网络限额未增加；引用材料与质量评分未修改。前一批完整 race 会话 30063 在本增量之前编译，不能验证当前改动。当前尚待增量审查、回归及剩余预算问题解决，未提交或完成整票。

增加诊断后的新 SDK 定向 race 通过（2.935s），包括结算后旧资格驱动 Inspect 拒绝。首次交接失败未能据此定位，仍保留为未解释的回归风险；后续相关组合继续核验。

相关 SDK、原驱动身份/限制、单页账本计量及搜索真实进程恢复的 race 组合通过（11.480s）；首次失败仍未被定位。Standards 与 Spec 独立增量静态审查均无 actionable findings，未替代预算组合或整票验收。

真实 WAITING 关闭重开用例现同时重建执行 Content 与驱动 ObservationScope，使用显式恢复的原资格。原五条执行读取加驱动 LOOKUP/GET/READ 和 Outcome 共九条，原 Request/Action.Qualification 均不变；race 通过（2.300s）。这仍不等同子进程崩溃或任意检查点恢复。

## 使用任务证据 PUT 已返回的长度

恢复的 LOOKUP 仍保留，用于核对原证据操作；没有直接相信账本中的 Reference 跳过该关联。另经真实 Content 反例确认：成功证据 PUT 已返回有界长度，但随后仍重复 GET 元数据。fetchcontent.Save 现仅将经原检查的长度记入同一执行实例的有界表，后续每次 READ 仍实际检查完整元数据、当前权限、来源及摘要。

反例先因两次读取仍产生三次观察失败（0.173s）；改动后两次实际 READ 共两次观察，并发复用、撤权/过期、SDK 计量、WAITING 重开及单页账本的 race 组合通过（9.844s），vet/diff 通过。SDK 获取计量现为六次 Content 加一次 Outcome；WAITING 重开为七次 Content 加一次 Outcome，总八次。单页失败最低总查询56、Content46、Outcome4；网络等待可能增加实际检查。

这次仅复用任务自身 PUT 的元数据，不与独立观察者共享长度，也不缓存授权或正文。冻结组合重新运行，尚未据此宣称原64预算内通过；此前失败证据保留。

上述证据 PUT 长度增量的 Standards 与 Spec 静态审查均无 actionable findings。最新冻结组合仍失败（30.584s）：冲突行动阶段57–58条、部分失败55条，答案阶段仍耗尽64。单页和已通过的恢复计量证据不能外推为组合验收成功。

## 同一执行作用域复用成功提交的终结事实

fetchqueries 的 OutcomeStore 视图现最多保留十二个原 Request 对应的成功 Complete 结果。提交前核对 AttemptIntent 的 Task、OperationID 和原 Fingerprint；底层 Complete 失败或未知结果不进入表。缓存只有有限 Outcome，不含正文。命中后仍调用绑定原工作端口的 fetchtask.Guard 检查当前执行资格，不能以缓存恢复已结算或失效的行动。证据 LOOKUP/READ 与当前来源检查保持。

反例先观测到刚提交后仍发生一次 Outcome 查询而失败（0.810s）；同宿主改为复用已成功提交事实，定向 race 通过（2.810s），仍拒绝旧资格驱动 Inspect。新作用域没有旧事实或长度表，必须重新读取账本和证据元数据。真实 SDK 测试增加 Start 后重建实际作用域的分支；它只重新装配真实驱动，没有替换底层结果。

因此同作用域正常 SDK 为六次 Content、零次 Outcome 读取；新作用域为七次 Content、一次 Outcome。WAITING 存储重开后本次成功执行产生自己的终结事实，累计七条 Content，不能误称复用了关闭前尚未发生的结果。单页失败最低总查询54、Content46、Outcome2，答案阶段重开仍重新读取各操作。

上述当前/新作用域组合尚在核验；旧阶段计数是历史证据，不替代新结果。原预算失败、未定位的交接失败及整票剩余项保持。

当前/重建作用域、WAITING实际存储重开和单页账本的race组合通过（9.958s），vet/diff通过；新作用域明确丢弃旧长度和终结事实，再次产生真实计费读取。冻结组合正在按最新路径重跑，原上限保持。

成功Complete有限事实复用的独立Standards与Spec增量静态审查均无actionable findings；两者未重跑测试，也未将组合或整票标为通过。

最新冻结组合仍失败（21.230s）：冲突行动阶段54–55条，部分失败52条；答案阶段继续耗尽原64条预算。当前/重建作用域的计量测试通过不等于冻结组合或语义质量验收通过，下一步仍需解决完整链路成本及原票剩余要求。
