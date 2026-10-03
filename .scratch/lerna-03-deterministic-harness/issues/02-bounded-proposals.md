# 02: 有界候选与完整来源

**What to build:** 组件开发者得到闭合、有界并且绑定准确输入的候选；含混行动组合、伪造来源和错误提案不能进入可消费的完成事实。

**Blocked by:** 01 原 Decision 的耐久规则提案

**Status:** ready-for-agent

- [ ] Proposal保留准确decision／snapshot／目标与控制修订、完整processed来源；结构采用有界requirement_delta加至多一种advance判别分支，非空delta才允许none，不把合法delta加单候选误拒绝。
- [ ] actions、input_request、candidate_result、cannot_continue各有正常可恢复对照；闭合类型、bounds及必要字段符合已批准决定，候选Result不裁决Task终态，输入请求不暗含授权确认。
- [ ] 最多4个互相独立行动；local_key唯一，能力与binding配对、arguments和来源均在准确Snapshot允许范围；依赖同提案未发生结果、伪造合法形状ref或depends_on均拒绝。
- [ ] 条件替换绑定当前准确条件版本，重复替换拒绝；processed集合覆盖全部实际处理来源，披露集合不能替代，候选证据与参数用途经真实消费路径核验。
- [ ] Go／TS共同codec拒绝可从正文判断的非法组合、重复键、未知字段与越界；涉及来源、版本和用途的包含判断在组件入口执行，codec不读取私有数据库。
- [ ] 故意错误输出来自已固定配置摘要／fixture lock绑定的私有fixture配置并经同一公开验证路径，不能偷偷变更同一固定输入的规则含义；原Decision记录proposal_invalid等明确失败而保留accepted，无无限修复／新收费身份；错误状态与本票新分支重开后仍一致。
- [ ] 完整新增候选的正常和拒绝路径都有有限结束、准确期望及耐久产物观察；规则只提出delta并保留其他候选，不自行采纳或递增Task修订。


## Comments

2026-10-03，root依据授权Astra批准的粒度/真实edges及最终df2dbe5前置退出发布；本票验收尚未实现。等待上述直接前置resolved，不能以spec ready替代实现依赖。
