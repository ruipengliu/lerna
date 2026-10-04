# 02: 有界候选与完整来源

**What to build:** 组件开发者得到闭合、有界并且绑定准确输入的候选；含混行动组合、伪造来源和错误提案不能进入可消费的完成事实。

**Blocked by:** 01 原 Decision 的耐久规则提案

**Status:** resolved

- [x] Proposal保留准确decision／snapshot／目标与控制修订、完整processed来源；结构采用有界requirement_delta加至多一种advance判别分支，非空delta才允许none，不把合法delta加单候选误拒绝。
- [x] actions、input_request、candidate_result、cannot_continue各有正常可恢复对照；闭合类型、bounds及必要字段符合已批准决定，候选Result不裁决Task终态，输入请求不暗含授权确认。
- [x] 最多4个互相独立行动；local_key唯一，能力与binding配对、arguments和来源均在准确Snapshot允许范围；依赖同提案未发生结果、伪造合法形状ref或depends_on均拒绝。
- [x] 条件替换绑定当前准确条件版本，重复替换拒绝；processed集合覆盖全部实际处理来源，披露集合不能替代，候选证据与参数用途经真实消费路径核验。
- [x] Go／TS共同codec拒绝可从正文判断的非法组合、重复键、未知字段与越界；涉及来源、版本和用途的包含判断在组件入口执行，codec不读取私有数据库。
- [x] 故意错误输出来自已固定配置摘要／fixture lock绑定的私有fixture配置并经同一公开验证路径，不能偷偷变更同一固定输入的规则含义；原Decision记录proposal_invalid等明确失败而保留accepted，无无限修复／新收费身份；错误状态与本票新分支重开后仍一致。
- [x] 完整新增候选的正常和拒绝路径都有有限结束、准确期望及耐久产物观察；规则只提出delta并保留其他候选，不自行采纳或递增Task修订。


## Comments

2026-10-03，root依据授权Astra批准的粒度/真实edges及最终df2dbe5前置退出发布；本票验收尚未实现。等待上述直接前置resolved，不能以spec ready替代实现依赖。

2026-10-04，root正式claim本票。直接前置01已resolved并merge5307702（最终源696ac49、交付d806a92）；05已resolved并merge077f616。准确push c9de1ba的CI37174778053全部success，见[CI记录](../ci-verification.md)。按用户已授权的全部实现任务及已采用技术决定，在新的独立worktree/branch实施；原01工作树保留，不能修改归档或用候选pin冒充前置。验收seams已在整片授权固定为公开Component／真实fixture Source与target普通接口及独立observer，物理生命周期仅使用明确标注的机械driver观察。所有build/DB/全套测试须先获root独占槽；本票仅关闭自己的AC，不承担whole03或后续依赖。
本票实际接法见[正式采用记录](../proposal-decisions.md)与[ticket01 API](../ticket-01-api-handoff.md)。


2026-10-04，本票七AC已由唯一implementer实现并全部实际验证。产品3d60b6a，
机械恢复测试25287d5，最终shared helper/独立消费者检查与两轴完整复核8f94f26。
完整新PG及原双DB正常/race、83＋158跨语言真实往返、原FINAL01与970公开producer升级、
严格ErrClaim限定、native Source实际关闭后的读取故障均分层记录，未借旧结果追认新资格。
all sessions/children/native holders实际退出；1628独立PG、64targetFS、244recoveryFS、
15archive目录、27确认进程组均absent，晚登记的7个Node toolcache文件及owned root已安全移除。
仅本票resolved；03／whole profile／root最终架构与远端CI未据此关闭。

## Answer

五种闭合Proposal与Source证据变体、四独立行动、准确条件与完整来源／用途验证均耐久完成。
新fixture-rule/3和prepared_v2单格式保留原/2未知标签及Prepared v1字节／digest含义；
错误私有固定配置通过同一codec及真实消费路径拒绝，原accepted、费用和失败事实重开保持。
限额、真实发布读回、五种prepared恢复与两套原public producer升级具备正常/race证据。
两个独立轴最终Standards0hard／0smells，Speca0／b0／c0。
逐AC／pin／命令／失败历史／资源范围见[退出证据](../ticket-02-exit-evidence.md)，
可消费接法见[API交接](../ticket-02-api-handoff.md)。Root负责实际merge、push及整片后续退出。
