# 03: 取消墓碑与有限资源

**What to build:** 调用方能按受信控制关闭原 Decision，先到取消与迟到工作不会复活；限额和执行期限都有可查询的明确结果。

**Blocked by:** 01 原 Decision 的耐久规则提案

**Status:** claimed

- [ ] cancel绑定原decision／task／Decision输入摘要和闭合控制依据，核验受信principal、明确标注的fixture task owner签发关系、耐久proof及期限；payload自报身份、错误目标、摘要或过期证明不能授权停止。
- [ ] 合法cancel先于decide持久保存绑定关闭墓碑和固定applied回执，get准确返回关闭绑定且不伪造尚未收到的Snapshot或原decide请求；迟到相同绑定固定decision_cancelled，不同绑定decision_mismatch，均无新工作，重开后不复活。
- [ ] 控制修订单调，更低修订拒绝，同修订同绑定幂等、异绑定冲突；本方法明确禁止通用expected_revision，不借不存在对象跳过鉴权。
- [ ] 取消与规则完成有受控真实并发两种顺序；提交处再次核验终态和Claim资格，旧worker不能在取消后写completed；已发布未采纳产物不冒充完成。
- [ ] completed／failed／cancelled上的合法取消不抹原终态、Proposal或产物，原命令固定回执不改变；停止范围及可能残留责任在公开get中准确表达。
- [ ] 十进制字节／步骤／行动／fixture费用限额在准入及实际执行核验；零不等于无限，输入超限、坏来源、输出超限、步数与费用耗尽均记录原Decision failure并配允许完成对照。
- [ ] 执行deadline、命令accept_before、单次context资源期限分开；截止前正常、截止后及执行中有限终止都有可复演观察，不伪造真实模型请求或生产费用证明。
- [ ] 本票控制墓碑、资源拒绝及竞争的特殊状态经重开／接替保持，所有等待、回收与I/O有界，不让后续进程票承担隐藏取消关闭依赖。

## 最终接法注记（whole02已退出）

最终依据 `../final-handoff.md`（代码554874470d5abeb71fa743708580f3121b8944f1，退出文档df2dbe5624120bc258dc7419ea022a08ebc0d6b0）。原Decision limits/deadline在接纳与实际Start当前核验，不继承demo的5分钟/3次政策，epoch不重置预算；基础门禁已属01，本票提供完整极值/取消反例。ordinary quota0/饱和不能永久阻断准确原责任本地到期关闭及旧Claim fencing；外部已发布仍保留原身份事实。取消绑定、先于decide墓碑、expected_revision例外、直接依赖01均不变；不新增AC或把特殊恢复推给06。


## Comments

2026-10-03，root依据授权Astra批准的粒度/真实edges及最终df2dbe5前置退出发布；本票验收尚未实现。等待上述直接前置resolved，不能以spec ready替代实现依赖。

2026-10-04，root正式claim本票。直接前置01已resolved并merge5307702（最终源696ac49、交付d806a92）；05已resolved并merge077f616。准确push c9de1ba的CI37174778053全部success，见[CI记录](../ci-verification.md)。按用户已授权的全部实现任务及已采用技术决定，在新的独立worktree/branch实施；原01工作树保留，不能修改归档或用候选pin冒充前置。验收seams已在整片授权固定为公开Component／真实fixture Source与target普通接口及独立observer，物理生命周期仅使用明确标注的机械driver观察。所有build/DB/全套测试须先获root独占槽；本票仅关闭自己的AC，不承担whole03或后续依赖。
本票实际接法见[正式采用记录](../control-decisions.md)与[ticket01 API](../ticket-01-api-handoff.md)。
