# 核心模型规格追踪

[规格](spec.md) · [实施图](README.md) · [行为验收](../../docs/architecture/.draft/validation/core-model-scenarios.md) · [票据 05](issues/05-acceptance-traceability.md)

本表按规格原编号映射 54 条用户故事和 24 项实施决策，不复写规格或另造每项独立测试。CM 是复用 HAR／FW／模块向量的行为场景组，CM-S 是本轮静态审查入口；预期行为是待实现断言。所有运行状态当前均为未验证，设计承载、静态检查和按需能力不得计为运行通过。

G-01～07 沿[参考语义矩阵的合同差异](../../docs/research/agent-harness-comparison/core-model-semantic-coverage.md#4-需要独立交付的合同差异)解释。G-01～05 中的完整公共能力尚待独立合同和实现；G-06／07 保留现有 Memory 与扩展合同并按需启用。设计中写明这些边界可以完成本轮映射，不表示已经交付对应运行能力。总览与工程导航的设计整合已由[票据 06](issues/06-document-integration.md)完成并合入，运行状态仍为未验证。

## 1. 用户故事

| 编号 | 设计承载／明确缺口 | 验收入口 | 预期外部行为 | 运行状态 |
| --- | --- | --- | --- | --- |
| US-01 | [六组核心对象](../../docs/architecture/.draft/core-data-model.md#core-objects) | [CM-01／S1](../../docs/architecture/.draft/validation/core-model-scenarios.md#cm-01) | 普通应用提交、观察和取结果，无需管理内部记录 | 未验证 |
| US-02 | [统一应用动作](../../docs/architecture/.draft/application-workflow.md#entry-points) | [CM-01～04](../../docs/architecture/.draft/validation/core-model-scenarios.md#baseline) | 同一工作流完成提交、查询、控制和恢复 | 未验证 |
| US-03 | [对象分类与保留标准](../../docs/architecture/.draft/core-data-model.md) | [CM-S1](../../docs/architecture/.draft/validation/core-model-scenarios.md#static-review) | 调用者分清核心、子记录、投影、配置与扩展依赖 | 未验证 |
| US-04 | [设计覆盖与 G-01～07](../../docs/research/agent-harness-comparison/core-model-semantic-coverage.md#4-需要独立交付的合同差异) | [CM-S2／S5](../../docs/architecture/.draft/validation/core-model-scenarios.md#static-review) | 未开放或未验证的能力有明确限制，不显示为已实现 | 未验证 |
| US-05 | [Session 与 Task](../../docs/architecture/.draft/interaction/session-and-task.md#2-本项目应补齐的分工) | [CM-05](../../docs/architecture/.draft/validation/core-model-scenarios.md#cm-05) | S1 的两个独立目标保留 T1／T2 各自责任 | 未验证 |
| US-06 | [原输入路径](../../docs/architecture/.draft/application-workflow.md#entry-points) | [CM-05／06](../../docs/architecture/.draft/validation/core-model-scenarios.md#cm-06) | 回答原 InputRequest 一次消费并继续原 Task | 未验证 |
| US-07 | [会话与控制](../../docs/architecture/.draft/application-workflow.md#observation-control) | [CM-05](../../docs/architecture/.draft/validation/core-model-scenarios.md#cm-05) | 归档或关窗不取消任务；重开不自动增权 | 未验证 |
| US-08 | [无 Session 的直接 API](../../docs/architecture/.draft/application-workflow.md#entry-points) | [CM-05](../../docs/architecture/.draft/validation/core-model-scenarios.md#cm-05) | 直接 task.submit 可创建和查询原 Task | 未验证 |
| US-09 | [原提交成功点](../../docs/architecture/.draft/application-workflow.md#original-submission) | [CM-05](../../docs/architecture/.draft/validation/core-model-scenarios.md#cm-05) | 消息保存、输入排队与业务接纳分别可见 | 未验证 |
| US-10 | [Session 关联恢复](../../docs/architecture/.draft/application-workflow.md#recovery) | [CM-03／05](../../docs/architecture/.draft/validation/core-model-scenarios.md#cm-03) | 丢失接纳答复或关联回写仍找回唯一原 Task | 未验证 |
| US-11 | [输入身份与范围](../../docs/architecture/.draft/application-workflow.md#input-scope)；自由聊天 G-02 | [CM-06](../../docs/architecture/.draft/validation/core-model-scenarios.md#cm-06) | 已支持的结构化提交可区分排队、消费、拒绝与撤回 | 未验证 |
| US-12 | [输入控制竞争](../../docs/architecture/.draft/interaction/implementation.md#input-control-races)；自由聊天 G-02 | [CM-06](../../docs/architecture/.draft/validation/core-model-scenarios.md#cm-06) | queued 撤回不取消其他工作，sending 保留消费未知 | 未验证 |
| US-13 | [自由输入合同缺口](../../docs/architecture/.draft/application-workflow.md#input-scope)；G-02 | [CM-06](../../docs/architecture/.draft/validation/core-model-scenarios.md#cm-06) | 未定义的 steering／follow-up 不冒充原问题回答；开放前明确投递规则 | 未验证；待合同 |
| US-14 | [旧输出关联](../../docs/architecture/.draft/application-workflow.md#observation-control) | [CM-05／17](../../docs/architecture/.draft/validation/core-model-scenarios.md#cm-17) | T1 迟到输出与控制不覆盖 T2 或新 Surface | 未验证 |
| US-15 | [历史分支边界](../../docs/architecture/.draft/interaction/session-and-task.md#history-branches)；G-01 | [CM-11](../../docs/architecture/.draft/validation/core-model-scenarios.md#cm-11) | 启用后保存来源位置、父链、分支头和配置／摘要范围 | 未验证；待合同 |
| US-16 | [fork 的责任边界](../../docs/architecture/.draft/interaction/session-and-task.md#history-branches)；G-01 | [CM-11](../../docs/architecture/.draft/validation/core-model-scenarios.md#cm-11) | 不复制活动操作、原使用、未结预算或推进权，不回滚真实文件 | 未验证；待合同 |
| US-17 | [模型输入重建](../../docs/architecture/.draft/brain/implementation.md#snapshot-reconstruction) | [CM-08／11](../../docs/architecture/.draft/validation/core-model-scenarios.md#cm-08) | 摘要保留原来源及范围，不改目标、效果或权限 | 未验证 |
| US-18 | [Task 状态与等待](../../docs/architecture/.draft/orchestrator/README.md#state) | [CM-05／07](../../docs/architecture/.draft/validation/core-model-scenarios.md#cm-07) | 澄清、等待与进程接替继续同一目标 | 未验证 |
| US-19 | [目标与继续条件](../../docs/architecture/.draft/orchestrator/README.md#state) | [CM-07](../../docs/architecture/.draft/validation/core-model-scenarios.md#cm-07) | active 不自动允许新模型费用或效果；当前门禁有效 | 未验证 |
| US-20 | [有界进展](../../docs/architecture/.draft/orchestrator/implementation.md#bounded-progress) | [CM-07](../../docs/architecture/.draft/validation/core-model-scenarios.md#cm-07) | 重启、重复反馈和只换控制不重置累计限制 | 未验证 |
| US-21 | [完成与证据](../../docs/architecture/.draft/orchestrator/verification.md) | [CM-04／07](../../docs/architecture/.draft/validation/core-model-scenarios.md#cm-07) | Result 含适用依据和限制，回复不能覆盖未知必要效果 | 未验证 |
| US-22 | [提案采纳](../../docs/architecture/.draft/orchestrator/implementation.md#proposal-consumption) | [CM-07](../../docs/architecture/.draft/validation/core-model-scenarios.md#cm-07) | 条件变化先提交新修订，旧提案余部失效 | 未验证 |
| US-23 | [Decision 子记录](../../docs/architecture/.draft/core-data-model.md#decision-operation-records) | [CM-08](../../docs/architecture/.draft/validation/core-model-scenarios.md#cm-08) | 原查询关联准确输入、提案和用量，无多套应用生命周期 | 未验证 |
| US-24 | [模型恢复](../../docs/architecture/.draft/brain/README.md#model-recovery) | [CM-08](../../docs/architecture/.draft/validation/core-model-scenarios.md#cm-08) | 每个实际模型请求可归属，辅助推理另准入且不透明重发 | 未验证 |
| US-25 | [Operation 原身份](../../docs/architecture/.draft/execution/README.md) | [CM-04／09](../../docs/architecture/.draft/validation/core-model-scenarios.md#cm-09) | 重连和失答复沿同一 Operation 核对，无重复外部写 | 未验证 |
| US-26 | [效果与恢复](../../docs/architecture/.draft/execution/README.md#效果和重复恢复) | [CM-09](../../docs/architecture/.draft/validation/core-model-scenarios.md#cm-09) | 仍未知的效果保留可见和核对责任 | 未验证 |
| US-27 | [执行控制](../../docs/architecture/.draft/execution/README.md#任务控制先于晚于行动到达时) | [CM-09／15](../../docs/architecture/.draft/validation/core-model-scenarios.md#cm-15) | 逻辑取消与物理退出分别可查，迟到结果仍归原操作 | 未验证 |
| US-28 | [原账务关联](../../docs/architecture/.draft/orchestrator/implementation.md#accounting-relations) | [CM-09／10](../../docs/architecture/.draft/validation/core-model-scenarios.md#cm-09) | 终态后追原累计费用差额，不双扣或重开目标 | 未验证 |
| US-29 | [正文发布与内联限制](../../docs/architecture/.draft/application-workflow.md#entry-points) | [CM-01／02／S1](../../docs/architecture/.draft/validation/core-model-scenarios.md#cm-01) | 准确引用与已有有界内联按字段合同使用，不重复拥有正文真值 | 未验证 |
| US-30 | [当前内容与来源](../../docs/architecture/.draft/memory/implementation.md#reference-gate) | [CM-10](../../docs/architecture/.draft/validation/core-model-scenarios.md#cm-10) | 旧历史、摘要、结果及缓存不能披露已关闭来源 | 未验证 |
| US-31 | [可信确认竞争](../../docs/architecture/.draft/interaction/implementation.md#confirmation-races) | [CM-10](../../docs/architecture/.draft/validation/core-model-scenarios.md#cm-10) | 普通对话不授权，原受信确认只供准确命令一次消费 | 未验证 |
| US-32 | [Grant 使用与结算](../../docs/architecture/.draft/security/implementation.md#5-一次使用与并发裁决) | [CM-10](../../docs/architecture/.draft/validation/core-model-scenarios.md#cm-10) | 原 owner 裁决额度、使用及结算，释放余额不返还 once | 未验证 |
| US-33 | [最后工具意图与绑定](../../docs/architecture/.draft/execution/README.md#final-tool-admission) | [CM-12](../../docs/architecture/.draft/validation/core-model-scenarios.md#cm-12) | 刷新同名工具不重绑原已准入操作 | 未验证 |
| US-34 | [实际隔离边界](../../docs/architecture/.draft/security/README.md#boundary-validation) | [CM-12／15](../../docs/architecture/.draft/validation/core-model-scenarios.md#cm-12) | 有批准仍受真实路径、网络、凭证及资源限制 | 未验证；需平台 |
| US-35 | [子任务冷恢复](../../docs/architecture/.draft/collaboration/implementation.md#cold-child-recovery) | [CM-13](../../docs/architecture/.draft/validation/core-model-scenarios.md#cm-13) | 原父子映射、结果和配置保持唯一，不建替身 child | 未验证 |
| US-36 | [可复用子会话](../../docs/architecture/.draft/collaboration/implementation.md#reusable-child)；G-03 | [CM-13](../../docs/architecture/.draft/validation/core-model-scenarios.md#cm-13) | 子 Session 可跨激活保留，新目标重新准入并保留旧账务 | 未验证；待合同 |
| US-37 | [读取与有界等待](../../docs/architecture/.draft/collaboration/implementation.md#async-child) | [CM-13](../../docs/architecture/.draft/validation/core-model-scenarios.md#cm-13) | wait timeout 只结束等待，不取消子工作 | 未验证 |
| US-38 | [Schedule 生命周期](../../docs/architecture/.draft/orchestrator/scheduled-triggers.md)；G-04 | [CM-14](../../docs/architecture/.draft/validation/core-model-scenarios.md#cm-14) | 编辑／停用未来规则不改写已生成 Task | 未验证；待合同 |
| US-39 | [occurrence 与原接纳](../../docs/architecture/.draft/orchestrator/scheduled-triggers.md#2-从到期到接纳)；G-04 | [CM-14](../../docs/architecture/.draft/validation/core-model-scenarios.md#cm-14) | 重复触发或丢答复恢复同一原命令与目标 | 未验证；待合同 |
| US-40 | [环境身份与停止](../../docs/architecture/.draft/execution/programmatic-tools.md#reusable-environment)；G-05 | [CM-15](../../docs/architecture/.draft/validation/core-model-scenarios.md#cm-15) | cell 已取消但仍忙时不释放占用，不接纳冲突复用 | 未验证；待合同／平台 |
| US-41 | [检查点恢复范围](../../docs/architecture/.draft/execution/programmatic-tools.md#3-取消恢复与结果)；G-05 | [CM-15](../../docs/architecture/.draft/validation/core-model-scenarios.md#cm-15) | 被动计算数据不等于 socket、进程或外部效果恢复 | 未验证；待合同／平台 |
| US-42 | [独立 Memory 生命周期](../../docs/architecture/.draft/memory/README.md#3-保存提取与修订)；G-06 | [CM-16](../../docs/architecture/.draft/validation/core-model-scenarios.md#cm-16) | Content 或聊天历史不自动成为获准长期记忆 | 未验证；按需 |
| US-43 | [经验候选与普通修订](../../docs/architecture/.draft/memory/implementation.md#experience-candidate) | [CM-16](../../docs/architecture/.draft/validation/core-model-scenarios.md#cm-16) | 普通偏好按原授权修订，旧候选不能覆盖新版本 | 未验证；按需 |
| US-44 | [Skill 与插件边界](../../docs/architecture/.draft/extensions/README.md#skill-materials)；G-07 | [CM-12／16](../../docs/architecture/.draft/validation/core-model-scenarios.md#cm-12) | 未启用扩展不进入普通请求主流程，安装也不授予权限 | 未验证；按需 |
| US-45 | [可靠接纳与工作模板](../../docs/architecture/.draft/reliable-work.md#scope) | [CM-17](../../docs/architecture/.draft/validation/core-model-scenarios.md#cm-17) | 应用无需管理 Job／Claim，原业务成功与恢复仍可查询 | 未验证 |
| US-46 | [可合并事务](../../docs/architecture/.draft/request-data-flows.md#transaction-boundaries) | [CM-17](../../docs/architecture/.draft/validation/core-model-scenarios.md#cm-17) | 合法本地裁决共同提交，跨 owner 保留原交接 | 未验证；需数据库 |
| US-47 | [事实与投影](../../docs/architecture/.draft/core-data-model.md#storage-boundaries) | [CM-17](../../docs/architecture/.draft/validation/core-model-scenarios.md#cm-17) | 重建缺口可见，投影不产生第二份任务真值或覆盖固定输入 | 未验证 |
| US-48 | [五项目十六类语义映射](../../docs/research/agent-harness-comparison/core-model-semantic-coverage.md) | [CM-S2](../../docs/architecture/.draft/validation/core-model-scenarios.md#static-review) | 每项有固定源码、实际行为及本项目承载／差异 | 未验证；静态项 |
| US-49 | [分装配路径判读](../../docs/research/agent-harness-comparison/core-model-semantic-coverage.md#1-装配路径和判读口径) | [CM-S2](../../docs/architecture/.draft/validation/core-model-scenarios.md#static-review) | Pi 三路径及其他 profile／宿主差异不拼成共同保证 | 未验证；静态项 |
| US-50 | [既有方法组合](../../docs/architecture/.draft/application-workflow.md)、[方法登记](../../docs/architecture/.draft/contracts/methods.md) | [CM-S3](../../docs/architecture/.draft/validation/core-model-scenarios.md#static-review) | 保留 105 方法；新增公共语义在独立契约变更前不宣称兼容覆盖 | 未验证；差异检查 |
| US-51 | [开发装配](../../docs/architecture/.draft/engineering.md#minimum-profile)、[生产部署](../../docs/architecture/.draft/deployment-production.md) | [生产验收](../../docs/architecture/.draft/deployment-production.md#6-发布观测与生产验收)、[CM-S4](../../docs/architecture/.draft/validation/core-model-scenarios.md#static-review) | 同进程便利不降低生产进程分工、固定路由和恢复要求 | 未验证；需平台 |
| US-52 | [四类请求计量](../../docs/architecture/.draft/request-data-flows.md#measurement) | [CM-01～04](../../docs/architecture/.draft/validation/core-model-scenarios.md#baseline)、[CM-S5](../../docs/architecture/.draft/validation/core-model-scenarios.md#static-review) | 分开调用、逻辑记录、SQL、提交、sync、字节及物理 IO，C 分段 | 未验证；未测量 |
| US-53 | [原工作流观察边界](../../docs/architecture/.draft/validation/core-model-scenarios.md#observation) | [CM-01～17](../../docs/architecture/.draft/validation/core-model-scenarios.md#baseline) | 从应用动作与独立真值验收，无测试专用业务入口 | 未验证 |
| US-54 | [证据分层](../../docs/architecture/.draft/validation/core-model-scenarios.md#evidence) | [CM-S5](../../docs/architecture/.draft/validation/core-model-scenarios.md#static-review) | 静态通过不填成运行、生产或性能通过 | 未验证；静态项 |

## 2. 实施决策

| 编号 | 设计承载／明确缺口 | 验收入口 | 预期行为或审查结论 | 运行状态 |
| --- | --- | --- | --- | --- |
| ID-01 | [设计交付边界](../../docs/architecture/.draft/core-data-model.md)、[证据状态](../../docs/architecture/.draft/validation/core-model-scenarios.md#evidence) | [CM-S5](../../docs/architecture/.draft/validation/core-model-scenarios.md#static-review) | 只交付架构与契约组织，运行能力明确待实施 | 未验证 |
| ID-02 | [六组与原负责方](../../docs/architecture/.draft/core-data-model.md#core-objects)、[应用动作](../../docs/architecture/.draft/application-workflow.md#entry-points) | [CM-01／17](../../docs/architecture/.draft/validation/core-model-scenarios.md#cm-17) | facade 组合原端口，不增加执行循环或完成裁决者 | 未验证 |
| ID-03 | [分类与保留理由](../../docs/architecture/.draft/core-data-model.md) | [CM-S1](../../docs/architecture/.draft/validation/core-model-scenarios.md#static-review) | 身份、负责方、生命周期、事务、查询与保留均有依据，不限定六表 | 未验证；静态项 |
| ID-04 | [Session 与原 Task](../../docs/architecture/.draft/interaction/session-and-task.md) | [CM-05](../../docs/architecture/.draft/validation/core-model-scenarios.md#cm-05) | 新目标、澄清、控制、批注分流，归档不取消 | 未验证 |
| ID-05 | [输入与回合范围](../../docs/architecture/.draft/application-workflow.md#input-scope)；G-02 | [CM-06／17](../../docs/architecture/.draft/validation/core-model-scenarios.md#cm-06) | 原输入独立可查，Turn 仅投影；自由聊天缺口不套用全任务取消 | 未验证；扩展待合同 |
| ID-06 | [上下文重建](../../docs/architecture/.draft/brain/implementation.md#snapshot-reconstruction)、[历史来源](../../docs/architecture/.draft/interaction/session-and-task.md#history-branches) | [CM-08／11](../../docs/architecture/.draft/validation/core-model-scenarios.md#cm-08) | 获准模型输入可少于完整历史，摘要不能改写权威事实 | 未验证 |
| ID-07 | [分支最小语义](../../docs/architecture/.draft/interaction/session-and-task.md#history-branches)；G-01 | [CM-11](../../docs/architecture/.draft/validation/core-model-scenarios.md#cm-11) | 线性首版与完整分支分开，fork 不复制执行义务或回滚世界 | 未验证；待合同 |
| ID-08 | [Task 记录](../../docs/architecture/.draft/core-data-model.md#task-records)、[核验生命周期](../../docs/architecture/.draft/orchestrator/verification.md) | [CM-07／09](../../docs/architecture/.draft/validation/core-model-scenarios.md#cm-07) | 完成依准确条件与证据，终态后仍解释费用、效果和证据缺陷 | 未验证 |
| ID-09 | [状态与继续权](../../docs/architecture/.draft/orchestrator/README.md#state)、[有界进展](../../docs/architecture/.draft/orchestrator/implementation.md#bounded-progress) | [CM-03／07](../../docs/architecture/.draft/validation/core-model-scenarios.md#cm-07) | 恢复不由 active 直接启动新消费，限制跨重启保留 | 未验证 |
| ID-10 | [Decision 内聚与恢复](../../docs/architecture/.draft/brain/README.md#model-recovery) | [CM-08](../../docs/architecture/.draft/validation/core-model-scenarios.md#cm-08) | 确定性可零模型，每 Decision 至多一请求，辅助请求独立归属 | 未验证 |
| ID-11 | [Operation 子结构](../../docs/architecture/.draft/core-data-model.md#decision-operation-records)、[执行](../../docs/architecture/.draft/execution/README.md) | [CM-09／17](../../docs/architecture/.draft/validation/core-model-scenarios.md#cm-09) | 原意图与执行接纳分属负责方，未知效果不随视图合并而丢失 | 未验证 |
| ID-12 | [内容规则](../../docs/architecture/.draft/core-data-model.md#supporting-objects)、[呈现发布](../../docs/architecture/.draft/interaction/implementation.md#surface-publication) | [CM-01／10／17](../../docs/architecture/.draft/validation/core-model-scenarios.md#cm-17) | 准确正文按引用与有界内联合同读取，流片段不冒充正式结果 | 未验证 |
| ID-13 | [Grant 使用](../../docs/architecture/.draft/security/implementation.md#5-一次使用与并发裁决)、[业务确认](../../docs/architecture/.draft/interaction/implementation.md#confirmation-races) | [CM-10／12](../../docs/architecture/.draft/validation/core-model-scenarios.md#cm-10) | 许可、一次消费、结算、受信预览与实际隔离各守成功点 | 未验证；平台另验 |
| ID-14 | [公共可靠工作](../../docs/architecture/.draft/reliable-work.md) | [CM-17](../../docs/architecture/.draft/validation/core-model-scenarios.md#cm-17) | 原命令、作业、领取和终态封装后仍可恢复，无通用 Run 取代业务裁决 | 未验证 |
| ID-15 | [配置与绑定](../../docs/architecture/.draft/core-data-model.md#configuration)、[扩展就绪](../../docs/architecture/.draft/extensions/implementation.md#staged-readiness) | [CM-12／S1](../../docs/architecture/.draft/validation/core-model-scenarios.md#cm-12) | 必要准确配置保留，动态发现可不启用；不新造公共目录版本 | 未验证；扩展按需 |
| ID-16 | [委派与可复用 child](../../docs/architecture/.draft/collaboration/implementation.md#reusable-child)；G-03 | [CM-13](../../docs/architecture/.draft/validation/core-model-scenarios.md#cm-13) | 原映射、读、等待、取消、激活与账务闭合分别成立 | 未验证；复用待合同 |
| ID-17 | [应用调度](../../docs/architecture/.draft/orchestrator/scheduled-triggers.md)；G-04 | [CM-14](../../docs/architecture/.draft/validation/core-model-scenarios.md#cm-14) | Schedule 管规则，occurrence 固定原交付；Job 不替代用户时间语义 | 未验证；待合同 |
| ID-18 | [环境与检查点](../../docs/architecture/.draft/execution/programmatic-tools.md#reusable-environment)；G-05 | [CM-15](../../docs/architecture/.draft/validation/core-model-scenarios.md#cm-15) | 环境跨 Operation，取消与真实停止分开，检查点不恢复外部效果 | 未验证；待合同／平台 |
| ID-19 | [Memory 修订](../../docs/architecture/.draft/memory/README.md)、[Skill 材料](../../docs/architecture/.draft/extensions/README.md#skill-materials)；G-06／07 | [CM-12／16](../../docs/architecture/.draft/validation/core-model-scenarios.md#cm-16) | 正文不取代许可／纠正／启用；普通偏好不走发布，软件改善仍需评测 | 未验证；按需 |
| ID-20 | [固定来源与 SM-01～16](../../docs/research/agent-harness-comparison/core-model-semantic-coverage.md) | [CM-S2](../../docs/architecture/.draft/validation/core-model-scenarios.md#static-review) | 五项目按各真实装配解释，不凭同名类型认定等价 | 未验证；静态项 |
| ID-21 | [默认接口](../../docs/architecture/.draft/application-workflow.md)、[105 方法登记](../../docs/architecture/.draft/contracts/methods.md) | [CM-S3](../../docs/architecture/.draft/validation/core-model-scenarios.md#static-review) | 不删改公共合同；完整扩展的公开差异另行同版交付 | 未验证；差异检查 |
| ID-22 | [四类流程与事务](../../docs/architecture/.draft/request-data-flows.md) | [CM-01～04／17](../../docs/architecture/.draft/validation/core-model-scenarios.md#baseline)、[CM-S5](../../docs/architecture/.draft/validation/core-model-scenarios.md#static-review) | 字节先耐久，必要发送屏障保留；计量单位及 C 两段不混合 | 未验证；未测量 |
| ID-23 | [最小装配](../../docs/architecture/.draft/engineering.md#minimum-profile)、[生产部署](../../docs/architecture/.draft/deployment-production.md) | [生产验收](../../docs/architecture/.draft/deployment-production.md#6-发布观测与生产验收)、[CM-S4](../../docs/architecture/.draft/validation/core-model-scenarios.md#static-review) | 单进程不替代生产分工、固定 Orchestrator 或本地事务边界 | 未验证；需平台 |
| ID-24 | [领域词汇](../../CONTEXT.md)、[架构总入口](../../docs/architecture/README.md)、[工程](../../docs/architecture/.draft/engineering.md)及原模块；[06](issues/06-document-integration.md)已完成设计整合 | [CM-S4](../../docs/architecture/.draft/validation/core-model-scenarios.md#static-review) | 映射只导航，规则留在原模块；ADR 无静默替代，总导航已对齐 | 未验证 |

## 3. 验证记录及更新规则

票据 05 的实际静态命令、检查范围与结果见[原 Comments](issues/05-acceptance-traceability.md#comments)，其中 06 尚待整合的说明保留当时状态。后续设计整合及静态证据见[交付核对](verification.md)；完整交付状态由实施图及最终整合记录维护，设计整合完成不改变本表的未验证运行状态。

规格故事、决策、公共行为或能力范围变化时同步对应行和 CM 场景，再检查原模块与契约差异。只改变名称或分组不能省略原身份、许可、来源、未知效果或迟到账务。运行实现、数据库／平台试验或性能测量真正执行后，应另附实现提交、冻结装配、刺激、实际结果及证据链接，再更新对应状态；不得从本次静态检查批量改为运行通过。
