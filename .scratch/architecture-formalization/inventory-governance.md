# 正式架构整理库存：应用入口、交互、授权、扩展与评测

本清单供正式文档编写和交叉审阅使用。记录日为 2026-10-02；以下结论来自仓库现有文档的逐节阅读，不表示实现、数据库故障实验、平台隔离或模型质量实验已经完成。本次只增加此库存文件，不改写正式架构、.draft、ADR 或研究结论。

## 1. 规范层级与来源索引

标记含义：**ADR** 为已确认架构决定；**现行设计** 为 .draft 及较新正式正文中的业务语义；**研究参考** 为比较、方法或历史证据；**可选** 为普通请求不依赖的能力；**未冻结** 为没有公共机器合同的内部设计或后续能力。现行设计不等于已实现。内部对象、逻辑表和公共 Schema 不逐一对应，允许在同一事实负责方和事务范围内合并存储。

| 来源编号 | 文件与已读范围 | 正式文档应吸收的内容及主定义 |
| --- | --- | --- |
| APP | [application-workflow.md](../../docs/architecture/.draft/application-workflow.md)，全篇 1–93 | 应用行动到原方法、消息保存与接纳区别、原命令保存、结果/控制/恢复；是组合路径，不是新 SDK 或公开 Session 协议 |
| SES | [interaction/session-and-task.md](../../docs/architecture/.draft/interaction/session-and-task.md)，全篇 1–123 | Session、Message、Task、回合投影分工；创建来源、线性消息、归档、可靠关联、后续分支边界 |
| I-R | [interaction/README.md](../../docs/architecture/.draft/interaction/README.md)，全篇 1–222 | Surface/InputRequest/InputSubmission/Presentation 语义、方法和 UI-01–11 |
| I-I | [interaction/implementation.md](../../docs/architecture/.draft/interaction/implementation.md)，全篇 1–617 | 组件、记录、输入转交、原消费、目录、确认/撤回竞争、SDK 与 II-01–31 |
| S-R | [security/README.md](../../docs/architecture/.draft/security/README.md)，全篇 1–256 | Subject/Grant/Use/Settlement/Confirmation/Lease/Endpoint 领域合同、信任与隔离边界 |
| S-I | [security/implementation.md](../../docs/architecture/.draft/security/implementation.md)，全篇 1–527 | 授权账本、准确命令确认、父链/额度锁、配对、迟到账单、离线封账和 S-I01–31 |
| X-R | [extensions/README.md](../../docs/architecture/.draft/extensions/README.md)，全篇 1–172 | PackageManifest/InstallLock/Activation、prepare/activate/ready/停用/回退含义、EX-01–09 |
| X-I | [extensions/implementation.md](../../docs/architecture/.draft/extensions/implementation.md)，全篇 1–454 | 实际制品、引用登记、staging、活动代际与实例、迁移/排空、X-I01–16 |
| X-P | [extensions/progressive-discovery.md](../../docs/architecture/.draft/extensions/progressive-discovery.md)，全篇 1–25 | 同一准确目录上的 direct、检索、延迟加载对照；候选策略不另设目录权威 |
| E-R | [evaluation/README.md](../../docs/architecture/.draft/evaluation/README.md)，全篇 1–256 | 候选、冻结计划、全部尝试、暴露/正式证据资格、批准与发布、EV-01–20 |
| E-I | [evaluation/implementation.md](../../docs/architecture/.draft/evaluation/implementation.md)，全篇 1–471 | 评测账本、来源组门禁、不可返还占用、环境/样本启动、报告、逐目标发布与 V-I01–23 |
| FORMAL | [较新正式入口](../../docs/architecture/README.md)，重点 1–117、251–315、346–360、455–464 | 六组核心对象与主线；A/B/D 的确定性目标模板前提；同域事务可合并、外部等待不进短事务 |
| BASE | [CONTEXT.md](../../CONTEXT.md)、[领域文档规则](../../docs/agents/domain.md)、[ADR 目录](../../docs/adr/)，ADR-0001–0010 全读 | 领域术语与不可在改写中改变的决定；不把逻辑模块数当数据库/服务数 |

跨领域共有规范继续以 [contracts](../../docs/architecture/.draft/contracts/README.md) 和 [reliable-work](../../docs/architecture/.draft/reliable-work.md) 为主：Command/Receipt、原目标服务、修订、查询与写入、JobStore 领取与完成不是各章重新发明的机制。模块主定义业务成功、必要事实及恢复；精确类型/必填字段由对应 Schema 管理。语义与 Schema 矛盾阻止发布，不能选择较宽松的一份。

已确认 ADR 对本组的直接约束：ADR-0001 长期保留最小去重/终态身份；0002 Go/WSS/gRPC；0003 生产分布式与存储前提；0004 每 Task 固定逻辑 Orchestrator；0005 评估器证据资格；0006 先采用目标条件再行动；0007 回退使用独立且当前有效的旧批准；0008 准确预览由受信 Renderer 保证、引用不证明阅读；0009 共同可靠工作模板保留领域真值；0010 单仓共享合同发布。

## 2. 应用入口与 Session：保存对话关系，不接管目标推进

### 2.1 现行对象和调用库存

| 对象或记录 | 身份、字段与可变部分 | 创建/修改/读取负责方及持久位置 | 交接与恢复 | 来源/性质 |
| --- | --- | --- | --- | --- |
| Session | 连续对话标识、标题、线性消息顺序、获准材料及 Task 引用；每 Task 至多一个固定创建来源 Session | 应用/Interaction 应用层保存；列表与界面读取。没有独立 Session 服务、会话预算或执行器 | 重开只恢复原事实；归档只改列表组织，不取消/重启 Task | SES 25–37、70–90；现行设计、内部合同未冻结 |
| Message | 消息身份/顺序、用户或系统来源、准确 Content 或有界内联文本、原提交关联 | 应用创建并保存；进度、Effect、费用、Result 只存原引用和带修订缓存 | 仅消息提交可称“已保存”；正文读取继续查来源/用途，不因已入历史永久获准 | SES 28、43–48、70–74；现行设计 |
| Session→Task 创建来源关联 | 完整 (orchestrator_id, task_id)，固定原 task.submit Command 与目标逻辑服务 | 应用先保存待交付关系；Task 由原 Orchestrator 接纳后补关联 | 接纳后关联回写丢失：查原 Command 并补关联，不能再建 Task；其他界面引用不改创建来源 | APP 36–42；SES 43、72；现行设计 |
| 一次提交/界面回合 | 原 Command 或 InputSubmission；由原关联计算输入与输出分组 | 应用查询投影，不另有可写 Turn/Run 目标状态 | 回合结束、进程退出、Task 完成、效果已核清分别展示 | APP 47–61；SES 37；现行设计 |
| 目标与回答正文 | goal_ref、answer_ref 指向准确 Content；消息可有有界内联变体 | 应用先准备正文并持久保存原命令，按具体 Content owner 路径发布引用 | 首发前本地原命令保存失败则不发送；首发后失败按原身份查询 | APP 29、36–42、77–87；现行设计 |
| 后续分支关系 | 源 Session/准确截止、消息父链与分支头、配置/摘要覆盖范围、来源/工作区限制 | 后续应用能力；当前首版只有线性历史 | 分支不复制活动 Operation、Grant 使用、未结费用、待发命令、领取权；历史回退不撤销外部效果 | SES 107–123；可选/公共合同未冻结 |

普通新目标调用 task.submit；回答已有问题绑定原 InputRequest 并走 interaction.input；修改目标/暂停/恢复/取消走原 Task 方法；只存批注不产生模型请求。普通文字不成为可信批准或取消。Session 历史和模型输入分离：Decision 的获准输入从当前 Task 事实与本轮材料组装，完整历史不直接追加成提示词。没有跨任务保存意图时不调用 memory.create。

APP 14–27、66–87 的入口映射应进入正式应用章：提交后查原接纳；查询结果遵守各事实 owner 的修订；task.cancel 的持久决定不等于原 Operation 停止；读取 Content 和展示 Surface 不等于目标完成；关闭/归档也不改变 Task 状态。A/B/D 调用次数只适用于 FORMAL 74 的确定性目标模板与覆盖检查；任意自然语言质量判断若需要评估器，必须另计有独立身份和成本的 Operation（FORMAL 85），不能泛化“一/两/三次调用”。

### 2.2 Session 的具体存储映射建议

I-I 130–139 与 SES 已明确持久责任，但 I-I 205–218 的记录表没有 Session/Message/原提交 outbox。以下是**内部逻辑记录映射建议**，可吸收进正式实现章；表名/字段名没有被宣称为已发布 Schema。四种记录可以同库、同聚合或复用现有 outbox/命令表，不要求四张新物理表。

| 建议逻辑记录 | 最小数据及键 | 创建/变化和事务要求 | 读取/保留 |
| --- | --- | --- | --- |
| app_sessions | (tenant, app_owner_id, session_id) 唯一；title、revision、next_message_seq、archived_at? | 创建连续对话；追加消息时在同一事务分配唯一 seq 并递增相关修订；归档只改组织字段。archived_at 是建议内部表示，不新增公共枚举 | 只负责线性组织和列表；无 Task 执行状态、预算或新 owner 路由 |
| app_messages | (tenant, app_owner_id, session_id, message_id) 唯一；(session_id, seq) 唯一；source_kind、content_ref 或有界 inline_text、original_submission_ref、created_at；必要的源版本/用途引用 | 先使 Content 字节耐久可引用，再发布消息引用；选择有界内联时仍受同样来源/清理约束。新文本采用新消息/明确编辑修订规则，不能覆写已绑定原命令的准确意图 | 输出消息关联原 Task/Decision/Operation/Content，不复制独立可写的结果真值；被撤权的正文不能由列表缓存泄露 |
| app_task_links | (tenant, app_owner_id, session_id, orchestrator_id, task_id)；创建来源唯一约束及原 submit_command_ref；引用可标为创建来源或普通展示关联 | 原 Task 接纳已知后与对应 outbox 的交接完成共同更新；同库 Task 接纳若允许共用事务可一次完成。跨库不承诺原子性 | 完整复合 TaskRef，不按同名 task_id 合并；归档或删除正文不删除未结原 Task 责任 |
| 应用原命令/交付 outbox | (tenant, app_owner_id, target_logical_service, command_id)；不可变完整 Command、摘要、session_id/message_id、原结果/Receipt 引用、恢复责任与 JobStore 字段 | 消息、固定目标和原 Command 在首发前共同提交；worker 在短事务外发送/查询。接纳成功后保存 TaskRef 和原决定；未知时保留原 identity | 终结保留最小原身份/决定/关联，敏感正文可按期清理；可复用现有 outbox，不新增一套业务状态机 |

建议验收以用户行为而非表数判断：同 Session 有 T1/T2；T1 等澄清时回答不推进 T2；关闭页面后任务继续；Task 接纳后应用崩溃只恢复同一 Task；来源关闭后历史和摘要停止披露；取消 A 不影响 B；重开不产生新模型/工具调用。对应 SES 98–104 与语义研究 SM-01/02/03/16。

## 3. Interaction：谁保存请求、谁转交、谁消费

### 3.1 对象、版本、状态和记录映射

| 对象/记录 | 关键字段、状态和读写主责 | 存储/事务和清理边界 | 来源 |
| --- | --- | --- | --- |
| Surface / SurfaceSnapshot | surface_id、surface_owner_id、app_binding、task_ref?；revision、title/blocks/request_refs/expires_at，Task 投影另带 source_revision | surfaces + surface_revisions；创建后绑定固定，完整快照版本不可变。只有绑定处理器/Orchestrator 投影器能更新，Renderer 只呈现严格声明式组件 | I-R 158–159、171–174；I-I 143–174、205–207 |
| SurfaceProjection | last_source_revision、required_source_revision 单调；当前缺口、停止依据 | surface_projection 与周期核对 job；事实更新和新增投影责任同事务，丢 Change 不丢继续核对 | I-I 207、248–254、498–515 |
| InputRequest | request_id、owner_id、revision、kind、schema、question_ref、deadline、required_content_refs、allowed_actions、consumed_by?；open/consumed/expired/superseded | **实际业务 owner** 的请求/消费记录；Interaction 不集中持有全部请求真值。acceptance 另固定 task_ref、goal_revision、candidate_ref/hash | I-R 160–162；I-I 178–201 |
| InputSubmission | input_id/revision、surface/request及版本、answer_ref、preview_refs、target_command_id、state、withdrawal_requested、receipt_ref；queued/sending/applied/rejected/withdrawn | input_submissions 内部还保存固定目标服务与完整原 Command；首次接纳后回答不可换；状态变化增 revision。delivery_jobs 承担转交 | I-R 163–165、175–177；I-I 209、303–357 |
| ApplicationEvent | event_id、surface_revision、app_binding、event type、payload_ref、原目标/完整 Command | application_events + delivery_jobs；仅独立 Surface 的固定处理器。处理器须幂等且原命令可查，普通不可查回调不能承诺有副作用的可靠交接 | I-R 179–181；I-I 210、431–450 |
| Presentation | surface_id、endpoint_id、intent_revision、open、seen_revision | presentations；设备自身控制开/关，旧结果不得覆盖 close。seen_revision 仅显示遥测 | I-R 166、178；I-I 208、474–489 |
| 提示与有限目录 | surface_notifications；surface_queries；aggregate_task_queries 的查询身份、来源目录版本/授权范围版本、上界/游标/未输出候选/期限 | 提示在可读快照提交后发，可丢/合并/重复；目录是有限查询状态，当前页重查披露许可。跨来源 Task 列表不另存 Task 权威 | I-I 214–217、274–296、498–515 |
| submission_closures | 原 input/event 身份、target owner、command_id、原决定摘要 | 清理显示不清理未收束交付；长期保留最小防重放事实，回答/截图按许可清理 | I-I 218–220；I-R 181 |

业务 owner 还保留 Confirmation（安全通用组件可复用），不可把它误归为 Interaction 的 InputRequest 或普通按钮状态。请求 schema 由 request_ref.owner_id 对应的请求 owner 唯一定义；Surface 只引用请求及标签，避免两份可修改表单规范。

### 3.2 逐步交接与成功点

| 步骤 | 输入与事务内变化 | 事务外工作/下一责任 | 失败与恢复依据 |
| --- | --- | --- | --- |
| 创建/读取界面 | surface_create 保存 Surface、绑定和原回执；surface_read 按当前权限读取完整快照 | 所需 Content 另按引用取得；视图不改 Task 事实 | 旧修订、来源关闭、权限不足分别有缺口；无权读正文仍可保留合法取消/删除入口 |
| 读取请求并预览 | request_read 在实际业务 owner 校验版本/披露；Renderer 固定 request/candidate/refs | 完整取字节、核摘要、完成规定呈现后开放相关按钮 | 旧版、摘要/缩略图、呈现失败不够；任意认证客户端复制 refs 无法被服务端识别为“未看”，不新增取阅凭据（ADR-0008） |
| 接纳输入 | input 先核对请求绑定，再保存 InputSubmission、固定目标/Command、原 queued 决定与交付 job | worker 沿原目标发送；queued 仅为交互已接纳 | 先发后存不可用；网络失败不产生新 input_id/command_id |
| 领取/撤回竞争 | queued→sending 与 queued→withdrawn 在同事务竞争；sending 后仅记录 withdrawal_requested | worker 查询原业务是否消费；撤回请求不覆盖已经发生的消费 | 撤回先提交永不发送；领取先提交只能“待核对”，不可宣称阻止消费 |
| 原业务消费 | 业务事务锁当前 Task/Request/Confirmation，查预览资格，保存一次消费和业务决定 | 原回执可查询；Interaction 归并为 applied/rejected，结束对应交付责任 | 两端不同答案一胜一拒；目标/candidate/request 修订变化拒绝旧输入。跨 owner 资格不假装是全局原子快照 |
| 更新和呈现 | 完整快照与提示责任提交；设备独立保存 intent_revision | Change 唤醒重读；临时流片段有界，可合并/丢弃 | 旧连接、旧预览、旧运行回调不得覆盖新 generation；流结束不当持久完成 |

方法库存：interaction.request_read、surface_create/read/update/list、input/input_read/input_withdraw、present、application_event。它们的字段/类型仍对照共同接口登记，不新增 Session 通用输入方法。

### 3.3 验收库存

I-R UI-01–11（210–220）覆盖消费后失答复、多端竞争、预览到期/撤权、关窗竞态、无 Task 的独立 Surface、伪造批准、Change 乱序/断档、版本更换、跨来源同名 Task、翻页新条目/披露变更、首屏/来源读取缺口。

I-I II-01–31（582–612）补充实际实现边界：声明式脚本拒绝、Surface CAS、原输入与应用事件恢复、处理器版本绑定、presentation 竞争、严格预览和旧世代、queued 撤回/已发送核对、确认 first-winner、SDK 首发前/后存储失败、投影提交与提示、目录稳定分页和周期核对。正式验收章保留原用例编号与观察点，不能仅留下“UI 重连测试”。各浏览器/CLI/原生 Renderer 和慢连接配额分别取证，单适配器结果不外推。

## 4. Security：身份、准确确认、授权使用与费用封闭

### 4.1 对象与持久事实

| 对象/记录组 | 关键字段与状态 | 写入/读取负责方和持久映射 | 生命周期/恢复 |
| --- | --- | --- | --- |
| Subject / ResourceScope | tenant/actor/actor_kind、端点/Task/Delegation 引用；resource_owner/type/selector/normalizer_version | IdentityAdapter 从认证和原委派取得身份；ResourceNormalizer 解析实际资源；不是 payload 自报身份 | owner、endpoint、instance 三种身份不得混用；许可按规范资源/用途/接收方核验 |
| GrantRecord / 父链 | grant_id/owner/revision/policy/intent_hash/confirmation_ref/issued_at/propagation；active/revoked，到期另判 | GrantLedger：grants、grant_parents、grant_counters；父链当前撤销继续检查 | issue 与确认消费/回执共同提交；委派只能收缩；revoke 后不复活；原最小键长期保留 |
| ConfirmationRecord | 原 consumer_method/command_id/target、完整 consumer_command、规范 intent_hash、challenge/expiry、revision/state、本人决定及 consumed_by/at | **实际消费业务 owner** 的 confirmations，可使用共用 ConfirmationStore；grant.issue、lease.allocate、pair.approve、evaluation.approve、task.accept_result 在各自事务消费 | pending rev1→approved/denied rev2；approved→consumed rev3。准确整条命令按规范 JCS/SHA256 绑定；拒绝/旧命令不可消费；read 不消费 |
| UseRequest / UseReceipt | 固定 use_id、operation_id、usage_owner、grant/source refs、主体/资源/动作/用途/接收方/位置/max_units/max_cost/cost_bound；allowed/denied、准确 grant revisions、reserved、start_before | Grant owner 的 grant_uses、grant_use_items 与计数；回执不可变，查阅按当前披露权 | 同 owner 必要许可全成或全拒；跨 owner 部分占用仍按原 use 恢复，未齐不启动；grant.check 不占用、不构成启动依据 |
| UseSettlement / UsageRevision | 原 use/operation/usage_owner、reserved/spent/held/released、cost_bound、state、consumed_once、关闭依据 | use_settlements、use_usage_revisions；计量 owner 先存原累计用量再交回；GrantLedger 按累计差额转账 | unknown 继续 held；关闭证据齐才释放数值余量；once 已消费永不返还。可信迟到账单允许 final→final 单调上调，不改原 UseReceipt |
| 费用交回 outbox | (tenant, owner, use_id, usage_revision) 唯一；原 Task、首次/当前 billing_reconcile 命令、摘要、交付状态 | use_billing_outbox；可信费用上调与 outbox 同事务；直到原 Task 持久 JobAck | use 已 final 仍保留账务责任；旧命令确定到期且必要查询条件满足时可为同一 outbox 建后继命令，不能丢账或重复记账 |
| OfflineLease / LeaseUse | lease_id/owner/Grant refs、endpoint/instance、范围/allocated_units/cost、issued/expires、owner_revision；open/closed/reconciled | owner：offline_leases、lease_uses；端：单一原实例消费账本和 lease_report_outbox | 分配从当前可用额度转预留；端侧费用变化与待报同一本地事务。旧快照/时钟回退不恢复余额；重启不能继承旧实例续用资格 |
| PairingSession / Endpoint | pairing_id、两种 code hash、nonce、有限窗口/范围、pending/approved/denied/claimed/expired；Endpoint active/revoked、instance/credential_generation/ref/scope | pairing_sessions、endpoints；秘密留凭据库/有限恢复密文；begin 预认证域不允许设备自选 tenant | 批准由本人会话绑定 tenant；claim 原 nonce 恢复同一凭据；恢复包已清理则 gone，不新生 token/Endpoint；重配对新 instance，旧责任仍在旧身份 |
| 撤销/关闭 | revocation_targets 的要求/已落实修订、最后核对；closed_security_keys 的域化摘要/原 ID/终态 | 原 Grant/Endpoint owner 持久变更 + JobStore 传播；凭据与原正文各按期清理 | 本地 applied 不证明远端停止，离线端按有限窗口列未知；清理正文不允许原 use 重启 |

来源：S-R 202–230；S-I 129–159、185–189、195–301、353–437。金额为单位明确的非负十进制定点数，不同单位不能直接合并；同 owner 多许可按固定 grant_id 锁序，SQLite 写队列和 PG 显式短事务各自验证。strict/estimate 成本模式不是通用“可忽略超额”：估算仅在规定本地 Task+Grant 边界适用，硬 Grant/离线额度不能因此放宽。

### 4.2 接口、真正准入和跨模块顺序

| 方法/交接 | 持久成功点 | 必须保留的检查或后续责任 |
| --- | --- | --- |
| confirmation.request/read/decide → 原业务方法 | 固定准确原命令；本人决定已保存；随后业务消费另一次事务 | 不能只把 UI 按钮转成 Grant；同命令、受信主体、未过期/未消费及原意图共同核验 |
| grant.issue / grant.read / grant.list / grant.revoke | Grant 与原决定；撤销决定和传播责任；有限列表查询按当前披露 | 许可不是身份登录；list 可含终态但 partial/gaps 不等于完整；撤销传播保留未确认端 |
| grant.check / grant.use / grant.use.get | check 只读；use 才保存占用与固定 allowed/denied 回执 | 调用方先保存准确 Operation/ModelCall 身份；参数变换全部在准入前，实际资源入口重查当前控制、Grant、预算与资源 |
| grant.use.settle / grant.use.settlement | 原使用累计支出与关闭事实，不修改启动回执 | 实际计量 owner 身份验证；累计 revision 重放只算一次；任务账单交回不随 Task/Use 终态丢失 |
| grant.lease.allocate / grant.lease.settle | owner 预留与单实例租约；原使用明细累计归并/封账 | 每个 lease 只保留一份结果未知的报告；未知时不换命令跳过。仅旧命令过期且权威 not_found 等条件成立才可替换；gone/unknown 停止猜测 |
| endpoint.pair.begin/approve/claim / endpoint.revoke | 待批准、本人批准和领取分开提交；撤销代次与传播 | user_code 非秘密；device_code/nonce 不进入日志；旧连接不能绕过当前 instance 与 credential generation |

普通资源使用链：Orchestrator/Brain/Executor 保存原业务身份和最终语义 → GrantLedger 保留当前授权和费用额度 → 受控实际入口再次核验 → 原计量方保存累计费用/未知 → Grant owner 结算 → 原 Task billing_reconcile 接管。模型 use 的 operation_id 对应实际 ModelCall，离线/任务 billing_ref 可关联 Decision；这些引用用途不同，正式数据图须展示映射，不能把同名字段强改成一种身份。

hook、Skill、插件声明、模型输出和 prompt 不能授予权限。准入后 driver 编码/凭据注入不得扩大语义；重定向、分页、附件、诊断和 wrapper 请求都属于实际出口。资源检查必须落在文件句柄/受控根、逐跳网络目标、凭据接收方和当前平台隔离上；正文保存/披露另核用途，执行成功不自动授权永久保存结果。

### 4.3 验收与开放边界

S-I01–31（S-I 445–484）覆盖：确认/签发丢答复、两意图竞争、once 并发、use/revoke 与跨 owner 部分占用、配对恢复/猜码、旧长连接、离线旧快照、无证明封账、秘密清理后的重放、最小键、累计账单重放/冲突、准确最终工具意图、跨来源授权、时钟/实例边界和迟到账务。S-R 的安全场景还要求不可信内容、替换实现、合法授权正例、慢端控制容量。正式章不能把“认证成功”“用户 consent”“命令 applied”“宿主隔离通过”合成一个安全成功位。

## 5. Extensions：制品、活动绑定与本次实例就绪

### 5.1 对象、字段与保存关系

| 对象/记录 | 关键字段与负责方 | 创建、变更、读取与保留 |
| --- | --- | --- |
| PackageManifest / Artifact | package_id/version/digest/kind(plugin/skill/agent_config)、entrypoints/ports/contracts/dependencies、requested_permissions/trust/state_formats；ArtifactReader/PackageVerifier | 核对下载、展开以及实际装载字节；受管不可变目录与句柄防替换。字节先 sync，再短事务发布 artifacts 可用记录和清单；摘要相同不等于来源可信/获权 |
| InstallLock | lock_id、manifest_digest、完整依赖、config_digest/platform、trust_evidence/conformance_report/state_compatibility；LockStore | install_locks 固定准确集合，任一内容变化建立新 lock；prepare 同命令返回原清单，不重求最新依赖 |
| LockReference | (lock_id, owner_kind, owner_id)、reference_revision；ReferenceCollector 与业务 holder | lock_references 登记 Task/Operation/迁移/回退/管理持有者；同事务登记或跨域先预登记后发业务命令。原结果未知保留引用，明确释放后才减 |
| Activation | activation_id/target/old_lock/new_lock/approval_id、revision/phase/generation/ready_instance、历史 startup_evidence/current instance_readiness、新使用关闭/旧版ready/residual/error；LifecycleManager | activations 保存不可变意图及历史原依据；lifecycle_steps/migration_steps 保存原步骤、幂等键、已知效果与查询责任。phase 为工作流投影，不能代替各项真实结果 |
| ActiveBinding | (target_id, port) 唯一、generation、lock_id；BindingRouter | active_bindings 同一端口一个活动代际；新激活才推进 generation，重启不推进、不重迁移 |
| InstanceReadiness | (target, instance, generation)、实际装载/自检/当前启动依据 | instance_readiness 重启失效；历史 activation_use_id 不刷新成新实例依据。必须准确字节、当前批准、当前 instance 同时成立才开入口 |
| 可靠工作与最小键 | management_jobs 的原 activation/step 与公共 job 字段；closed_extension_keys | 原命令接纳与 job 同事务；业务事实的 revision 与 generation 不同。清理后保留防重做索引 |

来源：X-R 135–154；X-I 143–173、193–201、207–315。ReleaseApproval 真值归 Evaluation；Extensions 只保存准确引用和所得启动依据，不能自行把报告合格升级为批准。

### 5.2 正常、失联与退出链

| 方法/阶段 | 成功含义 | 后续、未知和竞争 |
| --- | --- | --- |
| extensions.prepare | 已核验字节和完整 InstallLock 耐久可用才 applied | 内部可以有耐久准备工作，但不增加公开 accepted 阶段；半包不可激活，失联查原命令 |
| extensions.activate | 保存准确原切换与 job；此时仅接管执行责任 | 锁目标/期望代际；竞争一胜；排空、迁移、装载、自检均由原步骤继续；命令到期不丢已接纳责任 |
| 排空 | 封闭旧绑定的新接纳；按每个 holder 的责任核对 | 超时为 blocked，不假定旧调用消失；旧 Operation 留原驱动。仅明确验收的隔离无状态能力可并存 |
| staging → ready | 暂存绑定原 activation+instance+generation+lock/config；事务重查后才发布 handler | 初始化不自动执行任务/模型/工具；真实自检有自己的授权与记录。配置 A 的回调不能写 B 的 readiness 或卸载 B；提交未知先查原事实 |
| 启动依据 | 本地同库：当前批准、动作登记、活动指针同事务；远端：原 ApprovalUse 固定 start_before | DB 提交与进程指针非原子，间隙只能暂不可用。未知是否激活先查原 activation，不换 ID 切第二次 |
| 实例重开 | 同 lock、同 generation，新 instance、固定新 reopen 动作与当前批准 | 不复用旧 ready、不重复 migration；全本地依据记 commit_id，不伪造 remote use；新实例不能继承旧 offline lease |
| extensions.deactivate/read/list | 停用关闭新入口并保存；查询按真实当前实例/残留；列表冻结有限集合 | 返回 new_use_disabled、previous_version_ready、residual_work 的独立事实；配置期望不代替实际装载 |
| 回退 | 按 ADR-0007 的独立且此刻有效旧批准产生新 activation | 新版 revoked 不成为回退权限；重查旧批准报告/来源/期限/目标、旧代码信任和当前格式可读；不能回滚 DB 覆盖已发生授权/账务/删除/任务事实 |
| extensions.dispose | 同引用权威事务先封闭新引用取得，再比较 expected/reference_revision 与所有 holder | 未决预登记或不可达 holder 使 blocked；不能遍历当前进程推断全部引用；删除后旧 prepare/dispose 不重新发布相同身份 |

X-R EX-01–09（162–170）、X-I X-I01–16（419–434）保留实际字节、半包、错误依赖/替换、活动指针提交后崩溃、两切换竞争、未知旧写排空、不可逆格式、隔离、三类可替换实现、跨域引用/dispose 竞争、当前实例失联、配置 A/B 迟回调等观察点。原制品/格式/回退保留和原外部效果不被“卸载完成”清零。

### 5.3 Skill 与渐进式发现的采纳边界

静态准确能力绑定与默认受信内置组件属于现行基线。Skill/Agent 材料合同须保留适用前提、正负例、版本/来源、所需能力和评测依据；材料文字与可执行插件分别隔离和授权。动态目录检索、metadata→正文延迟加载属于候选策略，X-P 只要求在同一 CatalogVersion、安装组合和任务上与 direct 基线对照，记录召回/选择/实际加载/成功/总成本。不新增全局可写目录版本、注册层或第二路由；原 Operation 始终保持准确绑定。X-03 的既有 Skill 对照和后续 X-09 的发现因素按原定义分别冻结，不能用更少目录 token 直接证明质量或费用改善。

## 6. Evaluation：实验事实、当前证据资格和发布批准

### 6.1 全部证据对象与存储位置

| 对象/逻辑记录 | 关键字段、状态与负责方 | 创建/修改/存储/保留 |
| --- | --- | --- |
| Observation | event_id/correlation/component_binding/time/kind/summary | 观测存储，只作诊断；丢失不影响原业务恢复，summary 最小化，不替代账本 |
| Candidate | candidate_id、release_kind、improvement_id?、parents、kind/artifact/source/baseline/digest | candidates；精确不可变制品与谱系；变更新候选。首装 compatibility 可无旧 baseline；improvement 必有过程身份 |
| ImprovementPolicy | improvement_id/policy_digest、候选范围/次数/stop_rule/inference/comparison/相关过程 | improvement_policies；受信维护者先绑定不可变过程规则；formal_attempts 的跨申请计数永久保留，换名称不重置 |
| DatasetPartition / SourceGroup | partition/dataset/split/sample_ids/source_group_map/content_digest/permissions | dataset_partitions、partition_sources、source_group_gates；development/selection/holdout 分开；登记不授正文权，相关样本按来源谱系而非仅字节摘要识别 |
| HoldoutReservation | reservation/release_request/candidate/plan/partition/time | holdout_reservations；partition、plan、release_request 各唯一，不可归还；正式计划同事务占用与计数 |
| EvaluationPlan | plan/digest/purpose；候选/基线、环境/判定器、样本/seed/预算/重试/停止/invalid规则；总体/抽样/相关结构/权重/推断；primary metric/最小实用增益/分类及成本时延退化限 | evaluation_plans；运行前冻结，purpose 为 development/selection/compatibility_check/release_confirmation；不能看结果后改分母/扩样到通过 |
| EvaluationRun / SampleRun / Attempt | run/plan/revision/state/count/environment/report；queued/running/scoring/finished/blocked；每 sample/arm 一逻辑位置，arm baseline/candidate；每物理 attempt 独立 outcome/usage/evidence/environment_key | evaluation_runs、sample_runs、sample_attempts；每 plan 唯一整体 run。总样本不因双臂/重试增长；完成样本要求所需臂均有最终结果；全部有限尝试保留 |
| Environment / SampleStartAdmission | 原 environment_key、instance/sealed/cleanup；run/sample/arm/stage/attempt、formal_gate_revision/start_before | environments、sample_start_admissions；创建前固定原环境键，不同样本/臂不共键；环境准备和样本尝试分别有限启动准入 |
| 取消/清理 | cancel_requested/reason/environment_sealed、cleanup_state pending/cleaned/residual | run、环境与相应 JobStore；停止新工作与实际 seal/物理清理分别记录；创建结果未知也在取消范围内；不能造新环境逃避原责任 |
| EvaluationReport | report/plan_digest/run_refs/metrics/coverage/gaps/digest/evidence_class；target_attainment/statistical_gate/improvement_gate 各有 applicable/result/reason，result pass/fail/inconclusive；paired_counts/category_changes | reports 不可变；conformance/formal/exploratory 分开；不适用不是 pass，失效报告仍可追溯；正文清理读 gone，不重跑旧计划构造报告 |
| FeedbackExposure / PlanEligibility | 暴露 ID/partition/source groups/report?、recipient/scope/occurred_at/recorded_at/evidence/reason；eligibility revision/eligible或ineligible/exposures/reason | feedback_exposures、exposure_sources 是原事实；plan_eligibility 可滞后投影且失效不可恢复。impact jobs 分页写影响，准入不能等扫描完成才查原暴露 |
| FormalQuarantine | owner、受影响分区、证据、封闭状态/恢复依据 | formal_quarantines；来源组超上限/关系不全先耐久封闭该 owner 全部正式改善准入，完整核验才解除；不能让分页遗漏放行 |
| ReleaseApproval | approval/revision/release_kind/candidate/report/targets/state active/revoked/expired；本人/confirmation/max_offline_window；batches/window/min samples/stop/expires/rollback_lock/ref | release_approvals；兼容与改善证据分别核验；批准+发布 job+确认消费共同提交。撤回/到期不可复活，旧版独立批准不能被新版代替 |
| ApprovalUse | use/approval/revision/target/lock/instance/action_kind/action_id/start_before；activation/reopen/work | approval_uses 保存固定在线动作回执；宿主保存原动作使用登记。同 use 查询不延长，已知撤回立即拒绝新启动 |
| ApprovalLease | lease/approval/revision/target/lock/instance/continue_until | 领域明确要求耐久分配；现表索引缺显式映射，见下一节建议。只对已活动实例、显式非零离线窗口；不激活/扩批、不跨实例继承 |
| Rollout / RollbackTarget | rollout/approval/逐目标activation/current_batch/state running/waiting/stopped/finished；回退 source_approval/target 唯一新 activation、独立旧批准引用 | rollout_targets、rollback_targets；逐目标以 Extensions 事实为准。finished 不消除当前批准核查、撤回或回退/残留责任；旧批准的 rollout 不被回退覆盖 |
| 关闭索引 | 域化 object_kind/original_id、关闭/次数/占用摘要 | closed_governance_keys；样本/报告正文可清理，尝试/占用/暴露最小事实继续阻止重获资格 |

来源：E-R 177–203；E-I 108–159。各唯一键都带 tenant 和权威 owner。SampleAttempt、环境和流水线 job 是内部记录，不把其字段当作对外 EvaluationRun 已冻结字段。普通任务的 ConditionResult 与这里的软件候选改善 EvaluationRun/ReleaseApproval 分工保留；不要求每次工具读取走完整发布评测。

### 6.2 ApprovalLease 的具体耐久放置建议

E-R 202、222 明确对象和“持久分配”，E-I 405–407 明确资格/实例/重启边界，但 E-I 129–154 的存储表只列 approval_uses。缺项是映射未写出，不能由此推断设计允许易失租约。

建议在批准 owner 的原事务数据库中增加**有类型的 ApprovalLease 逻辑记录映射**：可以复用 release_approvals 的子记录/通用原命令决定库，也可实现为 approval_leases 表；不要求新物理表、服务或独立状态机。唯一查找为 (tenant, approval_owner, lease_id)，原 Command 唯一绑定同一记录；保存完整原字段、原命令/摘要、签发时许可依据和固定 continue_until。分配事务同步锁/核验当前批准、准确目标/lock、已活动 instance、max_offline_window；improvement 还核验原暴露/来源门禁/formal_quarantine。记录和固定原回执共同提交，答复丢失沿同 Command 查询，不重新计算窗口。

宿主一侧在原 Activation/InstanceReadiness 的既有存储中保存 approval_lease_ref 及可核验的原租约依据，明确它与该 instance、lock、generation 的联系；窗口换算使用既有 ClockAdapter/可信时间代次，不另造公开时间字段。继续 work 前检查原 continue_until、已知撤回、来源/用途和本地实例；时间信任失效先封闭旧窗口。新进程不能继承旧租约；reopen 先取得本次实例新依据。租约清理不允许原命令重放分配新窗口，最小原决定与终态身份按 ADR-0001 保留。

这项建议只补“记录存在哪里、哪个事务保存、谁在重启时读取”，不改变 max_offline_window 默认零、有限离线风险、当前证据资格或不许离线激活/扩批的既有取舍。

### 6.3 评测到发布的事务与恢复链

| 方法/阶段 | 事务内固定事实 | 下一责任及故障边界 |
| --- | --- | --- |
| candidate_register / partition_register | 精确候选/谱系或数据/来源/用途/已知暴露 | 新 ID 不清除同源占用/暴露；正文许可单独检查 |
| plan_create | 冻结计划；正式计划的候选绑定、不可返还 HoldoutReservation、策略尝试号同事务 | plan applied 尚未运行。源门禁阻止与暴露并发错放；不能在结果不佳后改计划/换申请恢复次数 |
| evaluation.run | 一个 plan 唯一 run、全部样本位置与初始工作共同保存 | 重复同原 run 恢复；不同 run_id 引用同 plan 返回原 run 冲突；有限重试不新建分母 |
| 环境与样本启动 | 原环境映射/attempt、当前正式资格、有限 SampleStartAdmission | 外部创建/调用在事务外；每次环境准备/新样本/重试都查原暴露与 quarantine。暴露前已提交窗口列在途，不谎称跨域立即停止 |
| evaluation.cancel | 整组停止新工作与所有创建/未知环境封闭责任 | 沿原环境命令 seal，再清理；sealed 与 cleaned 分开；无法 seal 为 blocked 保留额度和原查询 |
| 报告封存 / evaluation.read | 按固定总样本、全部尝试/费用、独立真值封存报告 | 未开放 holdout 只返回进度/准备故障，不能逐例泄漏；报告本体不随资格失效改摘要 |
| feedback_open / exposure_record | 暴露、来源组索引、唯一影响 job、原回执共同提交；反馈必须先记暴露再返回 | 正常反馈也影响其他同源未封存计划；异常泄漏可无 report。分页扫描失联按游标继续；未扫描对象也不能绕过实时原事实核验 |
| evaluation.approve | 当前报告/源资格、精确候选/targets/期限/批次/回退、Confirmation 一次消费、批准和 rollout job | applied 不等于任一节点 active；兼容不能冒充 improvement，改善失败另走兼容也不恢复正式尝试额度 |
| approval_check / approval_lease | 原动作在线窗口或已活动实例有限离线窗口 | 原 use/lease 不延长；work/reopen/activation 不混用。本地同库当前核验+登记免远端回执，但仍检查绝对到期 |
| revoke / rollout_read / 回退 | 停止扩批、逐目标停用/恢复事实和 job | 查原 activation；部分离线/未知单列。自动回退重新检查独立旧批准及格式，不能复活失效批准或清零外部效果 |

来源：E-R 211–229；E-I 207–224、237–262、271–311、366–410。source_group_gates 在登记时建立，正式资格读共享锁；新增异常暴露取排他锁并推进修订。后台资格投影仅加速查询，不能成为唯一准入依据。

E-R EV-01–20（235–254）和 E-I V-I01–23（444–466）应按三组保留：①冻结计划/不可换样本/双臂与全部尝试/相关样本/invalid与失败/统计及实用改善/过程多重尝试；②反馈先记暴露/正常与异常同源污染/扫描中崩溃/门禁与 quarantine/在途窗口；③环境创建未知与 seal/取消/费用/逐目标激活丢答复/撤回离线/独立旧批准回退。观测后端丢失只影响诊断完整性，不改变业务真值。所有用例仍是待运行要求。

## 7. 本组跨模块交接索引

| 交接 | 发起方必须先保存 | 接收方持久接管/返回含义 | 发起方何时可结束交付；仍留什么 |
| --- | --- | --- | --- |
| 应用 → Task submit | Session 消息、固定目标和完整原 Command | Orchestrator 保存 Task/预算/原回执/首 job | 原接纳明确后补 TaskRef；原任务/账務在 Orchestrator，不复制到 Session |
| UI → InputRequest owner | InputSubmission、准确 answer/preview、原目标 Command 和 delivery job | 原请求 owner 同事务一次消费与业务决定 | 原业务 applied/rejected 或 queued先撤回；unknown/sending 保留查原命令 |
| 受信本人决定 → 实际业务 | 准确 Confirmation、本人决定/挑战 | 原 consumer 在业务事务消费确认并保存动作 | 本人批准不等于业务已完成；败方/普通文字不能产生许可 |
| 原 Operation/ModelCall → Grant owner | 固定原业务身份、最终语义与计量 owner | 原 use 全量占用/拒绝、不可变回执、独立结算 | 取得全部当前依据才启动；unknown 与后续账单仍按原对象 |
| Grant 结算 → Task 账务 | 每次可信上调与同修订 billing outbox | 原 Task 持久归并/JobAck | 最终账单可能迟到；Task/Use 终态不删除交回责任 |
| 业务持有者 → InstallLock 引用 | 固定 holder/原命令，跨域先登记引用 | 引用 owner 保存保留事实；业务 owner 另接纳 | 只按明确责任终结/独立驱动接管释放；不可达不等零引用 |
| Evaluation → Extensions | 精确批准、逐目标 activation_id 与 rollout/rollback责任 | Extensions 接管原激活，真实当前 ready 另查 | 管理 applied 不完成 rollout；每目标未知、停用、旧版恢复、残留分列 |
| Extensions instance → Approval owner | 固定 activation/reopen/work、instance/lock/target | 当前资格核验后固定原 use/lease | 有限窗口不刷新；本地共事务替代远端窗口；Grant 与批准仍分别成立 |
| Feedback → 全部受影响计划/批准 | 原暴露、source group 索引、唯一分页影响 job | 当前启动/批准直接检查原事实，后续扫描补资格/撤回 | 扫描可恢复；已发有限窗口/离线租约单列，不声称瞬时跨域屏障 |
| Source/Content 关闭 → UI/评测/插件持有者 | 来源关闭事实和各持有者责任 | 当前读取/新使用封闭，受管副本清理另回执 | 不再披露不等于物理副本已清；残留与正文 gone 可查，原最小身份保留 |

上述交接可同进程调用；只有同受信本地事务范围内才能共同提交。图的箭头不能自动算作 RPC/SQL/commit 次数；跨 owner 的请求、接管、答复与发起方归并须分别画出。所有等待用户、网络、外部资源和实际模型/工具调用位于短事务之外。

## 8. 研究输入：采纳什么、不能迁入什么

| 研究输入 | 已读内容与可吸收依据 | 采纳级别/明确限制 |
| --- | --- | --- |
| [AI 报告对项目的影响](../../docs/research/ai-report-2026-09-project-implications.md)与[源核对](../../docs/research/ai-report-2026-09-source-check.md) | 全篇；24 方向中本组 SEC-01/02、UI-01/02、EXT-01/02/03、EVA-01/02/03；不可信内容不扩权、真实隔离、实际完成依据、反馈/发布闭环、独立评估与全成本 | 用户确认方向已进入设计；具体算法/平台效果仍待实验。19 项来源主要为摘要，时间覆盖 9月18–27日局部；不能当本项目重复实验或全年趋势证明 |
| [Harness 综合](../../docs/research/agent-harness-comparison/README.md)、[历史基线](../../docs/research/agent-harness-comparison/architecture-baseline.md)、[优化](../../docs/research/agent-harness-comparison/architecture-optimization.md) | 九模块保持；O-03 准入后准确参数/绑定，O-07 多端输入原身份，O-08 staging/配置/ready，O-09 经验候选经评测发布，O-11 渐进发现，O-12 故障语料 | 落实/细化现有 owner；O-09/11 仍实验，O-08 按能力开放。参考项目实现不是本项目能力证明；不引入 RunID 权威/新调度服务/九库/任意动态代码默认权限 |
| [六组模型语义覆盖](../../docs/research/agent-harness-comparison/core-model-semantic-coverage.md) | 本文行为章节和全部 16 族；本组重点 SM-01/02/03/07/09/14/15/16。Session 与原提交、Render、批准/隔离、Skill/Plugin 各有独立生命周期 | 已有设计、本轮补齐、按需扩展和本阶段不交付分别登记。现有 105 方法不因研究映射自动新增；G-01 分支、G-02 自由输入、多 Lane，G-03 复用 child、G-04 Schedule、G-05 环境均不能标为运行支持；G-06/07 的既有 Memory/Extension 合同保留 |
| [读写频次对比](../../docs/research/agent-harness-comparison/data-flow-io-comparison.md) | 全篇；暖 A/B、冷 C 的固定负载、各来源真实装配不同、Session 轻量与事实/jobs 可共事务 | E/RW/Tx/flush/sync/字节不同单位；当前本项目无可运行存储 adapter。Brain 的4/8个阶段与 Executor3个阶段不是总事务/SQL/物理IO；新正式 A/B/D 限制应优先说明 |
| [静态研究验证](../../docs/research/agent-harness-comparison/verification.md) | 固定源码快照、引用/锚点与基线指纹、独立静态交叉审阅的范围 | 154 基线文件和原统计属于调研时快照；后续架构变化不重写旧哈希假装未变；未跑上游依赖/测试/模型/benchmark |
| [公共技术文档比较](../../docs/research/public-technical-docs-comparison-2026-09-25.md)、[Opus 样本](../../docs/research/opus-5-5-document-samples-2026-09-25.md)、[写作偏好](../../docs/research/model-taste-x-2026-09-25.md) | 全篇；先解释读者问题与因果，再下选择，比较真正有竞争力方案；正常成本与限制靠近论断；字段/规则成为查阅层 | 写作参考和有限样本，不迁入旧架构内容、不推断普遍模型排名或质量优势 |
| [module-design 局部试用](../../docs/research/module-design-workflow-check-2026-09-25.md) | 全篇；独立读者复述选择/次序/失败后的动作；允许已有解释足够而不改写 | 局部技能/样本审查，不证明完整文档或运行系统已通过；此次库存不自行改技能 |
| [形式方法建议](../../docs/research/formal-methods-for-architecture.md)与[单项交接](../../docs/research/handoff-verification-results.md)、[全机制结果](../../docs/research/all-mechanisms-verification-results.md)/[覆盖库存](../../docs/research/all-mechanisms-coverage.md) | 方法文全文；归档证据的适用范围/结果/未覆盖边界。可借用交接动作、错误变体、资格/预算/引用检查问题 | 归档 2026-09-26 的十模块模型与证明，不能搬成当前九模块运行证据；TLC有限配置、Lean规则、静态合同、真实系统故障分别登记。完整生产验收执行数为0，组合证明不可由多个局部通过推得 |

研究映射的实用取舍：保留原 Command/Task/Decision/Operation、准确 Content、当前 Grant、持久继续责任；收敛重复视图、同域提交、SDK 组合接口；动态分支/发现/程序环境/经验精炼分阶段。普通请求不为未使用 Memory、协作、动态安装或改善发布创建整套治理记录。

## 9. 真实缺项、编辑补齐及不得擅改的开放范围

| 编号 | 观察与依据 | 处理建议 | 是否需要新架构决定 |
| --- | --- | --- | --- |
| G-DOC-01 | Session/Message/原提交 outbox 的责任已明确（SES 25–37、70–74；I-I 130–139），但 I-I 205–218 的存储表未列 | 采用本清单2.2的内部逻辑映射，正式章把消息→原Command→TaskLink画到存储与恢复。保持字段为内部实现选择 | 否；无需公共 Session/Run 协议 |
| G-DOC-02 | ApprovalLease 对象/持久分配/窗口已明确（E-R 202、222；E-I 405–407），表129–154只列ApprovalUse | 在原owner事务库显式登记类型化lease记录，按6.2补恢复和保留；可复用账本，不强制新表 | 否；属于记录映射补齐 |
| G-DOC-03 | 应用 workflow、模块 README、implementation、Schema 分散说明同一交接 | 每个对象一个业务主定义，顶层轨迹只引用；存储表与方法/事务/案例相互索引。Confirmation归实际消费者，InputRequest归业务owner，不能集中“简化” | 否；规范导航与完整性补齐 |
| G-DOC-04 | 四条请求场景和完整 walkthrough 已有，模块失败场景充分，但同一业务步骤的对象/写入/交接/恢复横向分散 | 正式端到端章逐步列输入对象、创建/修改、owner、事务、跨域接管、成功点和case；普通主干与按需Memory/协作/评测分别展开 | 否；不能据分散断言没有场景或没有恢复设计 |
| G-DOC-05 | FORMAL74/85已限制确定性模板，旧研究计数只冻结外部工作量 | 正式改写使用较新明确限制，不把生成回答等同目标成功；开放质量评估的额外Operation/物理请求/成本显式计入 | 否；保留较新前提，防止整理造成退化 |
| G-OPEN-01 | Session自由输入队列/steer/follow-up、多Lane和分支未冻结；已有input_withdraw只覆盖结构化InputSubmission | 明确首版不支持范围；若正式方案以后要求交付，另同步方法/Schema/示例/互操作/恢复，不借 task.cancel 冒充单输入撤回 | 是，只有新增能力时才需独立合同决定 |
| G-OPEN-02 | 动态发现、经验精炼、程序化工具与广泛自动改善有研究候选，现运行证据没有 | 保留按需开关、基线、实验与退出条件；不在整理时选定未经比较的算法/供应商/平台或宣称收益 | 是，启用策略或扩大能力时需要证据；本轮不构成阻塞 |
| G-OPEN-03 | 当前九模块的运行内核、真实存储/平台隔离/质量/生产RPO/RTO证据未交付 | 正式验收标清已设计、静态检查、归档形式结论、待运行，不挪用研究数字 | 不是文档冲突；实现和取证待后续 |

本组逐节核对没有发现必须改变 ADR 才能解决的业务自相矛盾。尚未确定的物理 DDL/索引、内部 Session 字段名、ApprovalLease 行布局可以按现有边界实施；不能将这些实现选择写成已发布公共 API。若后续逐字段 Schema 对照出现实际冲突，应记录准确两处定义及受影响方法并阻止相应合同发布，不以本库存替代完整机器合同一致性检查。

## 10. 给正式编写者的可验收交付范围

应用章应能让读者完成提交/回答/控制/重开，不先读全部治理细则；Session 与 Task 的分工和存储补齐放在那里。Interaction 章主定义界面/请求/输入/呈现及其各自成功，SDK 原命令保存与预览/撤回竞争就近解释。Security 章主定义准确确认、Grant/use/settlement/lease/endpoint；Extensions 章主定义字节→lock→activation→binding→当前instance→引用释放；Evaluation 章主定义候选/冻结数据/计划/attempt/报告/当前证据资格/批准/逐目标发布。

每章至少有一条正常业务链和一条真实失联链，能沿步骤定位对象字段、记录位置、原命令、事务范围、负责继续的job及既有验收编号。跨章不复制可写真值，不把网络成功/回执/实例ready/目标完成/账务封闭合为一个“完成”。引用矩阵覆盖全部已读来源段落，正文可分层和压缩重复解释，但不能靠遗漏条件减少复杂度。
