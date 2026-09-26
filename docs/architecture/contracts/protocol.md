# 线字段、方法登记与恢复一致性

[共同调用语义](README.md) · [运行时](../task-runtime/README.md) · [示例与检查](examples/protocol/README.md)

本页与机器资产定义未发布的 `harness/1`、`core-recovery-draft-1` 配置。它固定一组方法的请求、答复及关联规则，使实现者能交换同义数据，避免各自从段落推测字段。配置中有 40 个方法完成严格字段定义，另有 53 个方法明确登记为 `reserved`。**本配置只覆盖预先装配依赖后的核心调用与恢复；完整 Harness 的线接口尚未冻结。**

共同调用语义集中在[共同契约](README.md)，领域职责、前提和异常仍以所属专题为准。本页只增加编码选择、阶段可选性与资产覆盖范围。`frozen-draft` 指当前修订已有精确资产，方便共同审查；它不表示已经发布，也不禁止本轮在全套正文、资产和样例中统一修订。

## 1. 配置边界与接入前提

本配置支持直接 HTTPS Command／Query，以及按原 `command_id` 查询回执；进程内实现可以复用相同对象。本配置没有冻结能力发现响应、SSE、设备取件／回复、内容字节传输和身份握手的线字段。对应实现不能仅凭本配置声称这些入口互操作通过。

运行所需的内容、精确能力与绑定、许可、用户输入请求和软件安装锁，必须由受信装配或同宿主模块预先提供。`content.get`、`capability.describe`、`grant.issue`、界面创建及安装准备仍在保留清单；本配置因此不能单独启动一套任意远端 Harness。这样限定的代价是初期跨模块合同测试需要固定依赖夹具；收益是先冻结会影响重复副作用、控制和成功判断的链路，避免用任意 JSON 载荷掩盖字段缺口。扩展覆盖时必须同时添加输入、输出、关联约束和有效／无效用例，再修改方法登记，不能只移除 `reserved` 标记。

| 资产 | 权威内容 | 检查方式 |
| --- | --- | --- |
| [protocol.schema.json](schemas/protocol.schema.json) | 146 个可组合定义：共同对象、领域输入输出及夹具容器；所有业务对象拒绝未知字段 | JSON Schema 2020-12，启用日期格式检查 |
| [methods.json](schemas/methods.json) | 方法种类、输入输出定义、目标映射、条件修订、允许回执阶段、错误码及恢复动作 | 按准确方法分派验证，保留方法不可调用 |
| [校验入口](../validation/validate_protocol.py) | 字段合法性、跨字段关联及有限记录序列的保证检查 | 每项反例必须命中它声称破坏的规则 |
| [完成判断投影](schemas/task-outcome.schema.json) | 独立的成功条件、未知效果及完成依据投影 | [原校验器](../validation/validate.py)独立执行，不作为完整协议 |

请求信封中的 `payload` 与答复的 `output` 必须继续按登记方法对应的定义验证。只验证共同信封不算配置符合。`Invoke.arguments` 及 Brain 行动参数还必须按**准确能力版本、摘要与绑定修订**对应的已冻结能力 Schema 验证；资产不可能预先列出所有外部工具参数。夹具要求参数根对象关闭未知字段，只允许本地可解析的 Schema 引用，校验过程不访问网络。实际能力安装须完成同等的固定引用解析。

方法登记中的 `source` 是相对于 `architecture/` 根目录的领域文档路径，例如 `brain/README.md`；它用于定位业务语义，不随 Schema 的相对引用基准解释。

## 2. 共同编码与固定决定

| 对象／字段 | 本配置的精确选择 |
| --- | --- |
| 标识 | 小写类型前缀、下划线和 32 位十六进制随机部分；例子用确定性占位值满足格式，不作为生产 ID 生成算法 |
| ComponentRef | `id, version, digest`；version 使用三段版本，可带预发布后缀；digest 为 `sha256:` 加 64 位十六进制 |
| ContentRef | `tenant_id, owner_id, content_id, version, hash, media_type, byte_length`；精确正文引用；认证租户必须与引用一致 |
| ObjectRef | `owner_id, id, revision`，用于可变业务对象准确修订；不替代 ContentRef 或 ComponentRef |
| AuthorizationRef | `kind=grant/use/offline_lease, owner_id, id, revision`；含义由[权限专题](../security/README.md)裁决 |
| 时间、费用 | 时间采用 UTC `Z`；费用／用量用 `{unit, amount}`，amount 为非负十进制字符串；不使用浮点比较 |
| Command | `command_id, method, target_id, expires_at, expected_revision?, payload`；tenant 仅来自认证上下文 |
| Query | `method, target_id, payload`；查询方法即使无参数也传 `{}`，不带 command_id 或接纳截止 |
| Receipt | `command_id, stage` 及该阶段规定字段；可带资源 ID／修订；决定时间固定 |
| QueryResult | `output, observed_at`，可带 `resource_revision, cursor, gaps`；若输出有 revision，元数据不得与其冲突 |
| Error | `code, message, retry`，及可选关联身份、当前修订和退避毫秒；code 和允许 retry 组合从方法登记取得 |

每个方法的 `target` 固定对象或负责服务，不能同时在信封与载荷里选两个目标。`expected_revision` 只允许出现在登记为条件更新的方法中，且这些方法必填。写入失败后改参数、目标或预期修订需新命令；原命令重新查询不改变 `expires_at`。

`accepted` 必须有 `accepted_at`，不能带 `decided_at` 或业务错误。只有 Brain 决策和执行门禁传播允许这一中间回执：前者还未取得终态决策；后者尚有入口未落实门禁。可选 output 仍须满足该方法的准确输出结构。`applied` 必须有 `decided_at` 和完整合法 output；`rejected` 必须有 `decided_at` 和合法 Error，不能带 output。**本配置中的 execution.invoke 只在接纳操作及执行责任已提交时答 applied；Operation.effect 与 Receipt.stage 是两个字段。**

权限变化时允许在同一固定回执元数据上设置 `redacted=true` 并省略整个 output。本配置不开放任意部分字段裁剪；需要更细粒度披露时另定义明确投影。查询隐藏 output 不改变 stage、决定时间或原身份，也不使一个未知 output 对象免于验证。请求侧的租户、参数、控制及引用关联始终检查，不能因答复裁剪而跳过。

传输中断、认证失败或尚未接纳的不可用响应不伪造成 durable Receipt。记录序列夹具只表达已取得的持久回执、查询结果和已知负责端前提，不模拟 HTTP 连接；断线通过“业务回执已保存、调用方改走原查询”的有限序列表达。

## 3. 必须保持的领域关联

| 领域 | 本配置冻结的关联与编码选择 | 完整机制 |
| --- | --- | --- |
| 任务 | Task 固定 tenant、Home 和原提交；目标修订递增，改变目标及进入终态须推进 control_revision；Requirement.kind 为 effect／quality，验收方式由规则及 basis 表达 | [运行时](../task-runtime/README.md#records) |
| 等待 | `WaitReason={kind, object_ref?, resume_condition, deadline?}`；kind 为 input／authorization／dependency／effect／budget／capacity；没有特定对象时也必须说明恢复条件 | [状态](../task-runtime/README.md) |
| 任务结果 | 精确 goal_revision 和成果版本；只有 required 条件决定完成依据，可选条件失败不自动否决成功；主体任务的未知效果仍阻断成功，其他独立任务不能被混入 | [结果判断](../task-runtime/README.md) |
| 用户输入 | `task.input` 只消费 clarification；`task.accept_result` 只消费 acceptance，并固定候选摘要、目标修订及受信 Confirmation 引用；请求所属任务／owner 和请求修订均须匹配 | [交互](../interaction/README.md) |
| Brain | 一个 decision_id 固定 snapshot_revision；最多一项原物理 model_call；完成提案只能使用本轮可见的准确能力且不超数量限制；applied 的 decide 输出必须终态 | [Brain](../brain/README.md) |
| 执行 | Invoke 中 Home、task、goal 与认证控制快照相同；新的门禁不能倒退目标修订；按最高 control_revision 处理乱序，取消墓碑阻断迟到 Invoke；所有入口的最低已执行修订决定整端 enforced | [执行](../execution/README.md) |
| 用途使用 | UseReceipt 固定 use_id、owner、意图、许可准确修订、占用量及原期限；后续查询不能续期；denied 不占用额度，allowed 不保证目标已启动 | [权限](../security/README.md) |
| 资源选择器 | 本配置仅支持准确 `object_ids[]`；可选 `versions[]` 是这些对象共同允许的版本集合，组合范围为对象与版本的笛卡尔积。不同对象有不同版本限制时拆为多个 ResourceScope；需要其他规范化选择器时本配置返回 unsupported | [资源范围](../security/README.md) |
| 记忆 | 固定 read 修订不可静默换新；replace／restrict／delete 比较当前修订；删除输出 MemoryControl 墓碑，不要求继续返回旧正文。Scope 仅冻结 task_types／resource_ids／purpose_tags 三个集合 | [记忆](../memory/README.md) |
| 交互转交 | InputSubmission 的 request、answer、preview_refs 和 target_command_id 首次接纳后固定；sending 后撤回只记 withdrawal_requested，不能报已阻止消费 | [交互](../interaction/README.md) |
| 协作 | preparing 可尚无子映射；active 必须有一个内部或外部映射，之后不可替换；closed 需无未决效果并有最终结算依据 | [协作](../collaboration/README.md) |
| 激活 | activate 输出 `{activation_id}` 只接纳切换责任。Activation 在 prepared 等阶段可以尚无 approval_revision／activation_use_id；active 必须绑定已取得的在线 ApprovalUse、准确目标／锁／当前实例及新代际 | [扩展](../extensions/README.md) |
| 批准 | 在线 use 固定 action_kind／action_id，重放不能续期；离线 lease 只用于已有活动实例且需非零明确上限。max_offline_window=0 仍允许有限在线启动 | [评测与批准](../evaluation/README.md) |

`memory.restrict` 的 `policy_ref` 指向已保存的严格 ContentPolicy；接纳方必须核实新策略是原策略的收紧。Schema 能检查引用，不能证明策略子集。`home_proof`、Confirmation 引用和认证上下文也不能靠字符串格式证明可信，必须由真实认证及持久负责方核验。夹具把它们明确当作已验证前提，不提供签名实现。

## 4. 准入方法清单

下表中“条件”表示必须提供 expected_revision。“读”使用 Query；“写”使用 Command。输入和输出定义的准确名称与完整字段以 [methods.json](schemas/methods.json) 及其引用的 Schema 为准。所有方法都可按声明错误拒绝，表中成功阶段不扩展到领域效果。

| 方法 | 种类／条件 | 成功阶段 | 方法成功含义 |
| --- | --- | --- | --- |
| `brain.cancel` | 写 | applied | decide 的 applied 仅在 DecisionRecord 终态；cancel 保存取消决定；get 返回当前记录。 |
| `brain.decide` | 写 | accepted／applied | decide 的 applied 仅在 DecisionRecord 终态；cancel 保存取消决定；get 返回当前记录。 |
| `brain.get` | 读 | QueryResult | decide 的 applied 仅在 DecisionRecord 终态；cancel 保存取消决定；get 返回当前记录。 |
| `collaboration.control` | 写；条件 | applied | 本地控制及原映射传播责任提交。 |
| `collaboration.delegate` | 写 | applied | 内部子创建或外部委派与发送责任提交；尚无远端映射时继续 preparing。 |
| `collaboration.read` | 读 | QueryResult | 原委派和缺口。 |
| `collaboration.reconcile` | 写 | applied | 原映射核对 job；不新建远端任务。 |
| `collaboration.submit_input` | 写 | applied | 原远端输入的固定转交已保存。 |
| `evaluation.approval_check` | 写 | applied | 固定动作的有限在线窗口持久化；重放不续期。 |
| `evaluation.approval_lease` | 写 | applied | 已激活实例的显式有限离线续用，不能用于激活。 |
| `execution.cancel` | 写 | applied | 禁止新发送责任或未知操作墓碑持久化；在途效果继续核对。 |
| `execution.control` | 写 | accepted／applied | accepted 保存 gate 与传播责任；applied 仅全部受控入口落实当前 gate。 |
| `execution.control.get` | 读 | QueryResult | 返回门禁及逐入口已执行修订。 |
| `execution.get` | 读 | QueryResult | 读取原操作当前事实。 |
| `execution.invoke` | 写 | applied | 原操作接纳与执行责任持久化；不确认效果。 |
| `execution.reconcile` | 写 | applied | 有限核对责任保存；返回当前事实。 |
| `extensions.activate` | 写 | applied | 切换请求和责任保存；actual active/ready 另查。 |
| `extensions.read` | 读 | QueryResult | 本 profile 只开放 Activation 查询；InstallLock 查询仍 reserved。 |
| `grant.check` | 读 | QueryResult | 当前可用性检查，无许可占用。 |
| `grant.use` | 写 | applied | 固定用途的占用决定提交；allowed 不证明业务启动。 |
| `grant.use.get` | 读 | QueryResult | 返回原固定使用回执，不能续期。 |
| `interaction.input` | 写 | applied | 输入和固定转交命令持久化；queued 不证明消费。 |
| `interaction.input_read` | 读 | QueryResult | 读取原转交与业务消费状态。 |
| `interaction.input_withdraw` | 写；条件 | applied | queued 先胜则 withdrawn；否则仅登记 withdrawal_requested。 |
| `memory.create` | 写 | applied | 权威记录、原答复与索引责任提交。 |
| `memory.delete` | 写；条件 | applied | 关闭修订与清理责任提交；不证明所有副本已删除。 |
| `memory.inspect` | 读 | QueryResult | 可管理元数据，不要求正文可读。 |
| `memory.read` | 读 | QueryResult | 当前仍获准的精确修订。 |
| `memory.replace` | 写；条件 | applied | 修订与后续工作提交。 |
| `memory.restrict` | 写；条件 | applied | 限制收紧及清理责任提交；不能放宽原策略。 |
| `task.accept_result` | 写 | applied | 准确请求与候选的验收消费及核验 job 保存；并非 Task 成功。 |
| `task.attach_evidence` | 写 | applied | 核验工作已保存；不直接修改效果。 |
| `task.cancel` | 写；条件 | applied | Home 本地控制决定与传播责任提交；pending 不证明远端生效。 |
| `task.input` | 写 | applied | 澄清请求一次消费及后续 job 保存。 |
| `task.pause` | 写；条件 | applied | Home 本地控制决定与传播责任提交；pending 不证明远端生效。 |
| `task.read` | 读 | QueryResult | 本 Home 当前获准任务快照。 |
| `task.result` | 读 | QueryResult | 固定 Result 或当前非成功状态。 |
| `task.resume` | 写；条件 | applied | Home 本地控制决定与传播责任提交；pending 不证明远端生效。 |
| `task.revise` | 写；条件 | applied | 新目标修订及旧工作失效责任提交。 |
| `task.submit` | 写 | applied | Task 与首项工作持久创建。 |

`extensions.read` 在本配置只允许 `kind=activation`；安装锁读取分支尚未冻结。

### 保留方法及当前缺口

以下方法在领域设计中有职责，但完整线字段及答复尚未冻结，不得在能力发现中宣称本配置支持。当前不得用空对象、任意载荷或同名但异义接口代替。

| 所属专题 | reserved 方法 |
| --- | --- |
| [evaluation/README.md](../evaluation/README.md) | `evaluation.approve`、`evaluation.cancel`、`evaluation.candidate_register`、`evaluation.exposure_record`、`evaluation.feedback_open`、`evaluation.partition_register`、`evaluation.plan_create`、`evaluation.read`、`evaluation.revoke`、`evaluation.rollout_read`、`evaluation.run` |
| [execution/README.md](../execution/README.md) | `capability.describe`、`capability.search`、`resource.acquire`、`resource.get`、`resource.observe`、`resource.release`、`resource.renew`、`resource.takeover` |
| [extensions/README.md](../extensions/README.md) | `extensions.deactivate`、`extensions.dispose`、`extensions.prepare` |
| [interaction/README.md](../interaction/README.md) | `interaction.application_event`、`interaction.present`、`interaction.surface_create`、`interaction.surface_list`、`interaction.surface_read`、`interaction.surface_update` |
| [memory/README.md](../memory/README.md) | `content.close`、`content.get`、`content.put`、`content.register_copy`、`content.release_copy`、`memory.cleanup.get`、`memory.extract`、`memory.list`、`memory.query`、`memory.view.ack`、`memory.view.open`、`memory.view.pull` |
| [security/README.md](../security/README.md) | `endpoint.pair.approve`、`endpoint.pair.begin`、`endpoint.pair.claim`、`endpoint.revoke`、`grant.issue`、`grant.lease.allocate`、`grant.lease.settle`、`grant.read`、`grant.revoke` |
| [task-runtime/README.md](../task-runtime/README.md) | `budget.allocate`、`budget.settle`、`task.adjust_budget`、`task.list` |

## 5. 从保证到正反例

有效例子是有限的**记录序列**。它们展示负责端必须记录和返回什么；校验器核对已给出的关联，不执行任何目标动作，也不证明数据曾耐久提交。反例通过可查 JSON 路径改变这些序列，每项都要求命中明确规则，不能因无关字段格式错误而冒充语义检查成功。

| 保证 | 所属规则 | 正例与有针对性的反例 |
| --- | --- | --- |
| 原命令恢复及固定决定 | [共同契约](README.md)的幂等、过期与查询披露 | 02-original-command：原回执查询、重复、冲突及合法裁剪；改变请求／回执、换 owner、错误恢复动作被拒 |
| 任务控制与准确完成 | [运行时](../task-runtime/README.md)的控制修订及成功门槛 | 01、05、06：目标修订与当前结果；终态不推进控制、旧成果、必需条件失败、未知效果仍报成功被拒 |
| 准确能力与单轮边界 | [Brain](../brain/README.md)的快照与一次生成 | 01、09：接纳后终态、查询；换快照／物理调用、不可见版本、越界行动数量、未声明工具参数被拒 |
| 控制乱序及取消先到 | [执行](../execution/README.md)的 gate／墓碑 | 03、04、06：9 先于 8、取消先于 Invoke；旧 gate 覆写、未落实全入口却报 applied、已取消或旧目标仍接纳被拒 |
| 用途单次占用 | [权限](../security/README.md)的 grant.use | 10：check、use、原 use 查询；错 owner／许可修订、超占用、查询延长期限被拒 |
| 输入消费及验收绑定 | [交互](../interaction/README.md)和[运行时](../task-runtime/README.md) | 07、08：固定转交、撤回竞争、受信验收；跨任务、缺预览、旧修订、两次消费、换候选、伪造确认关联被拒 |
| 精确记忆修订与删除 | [记忆](../memory/README.md)的修订／控制元数据 | 11：读、纠正、收紧和删除；静默换版、未递增修订、删除仍报 active 被拒 |
| 原委派映射 | [协作](../collaboration/README.md)的创建关联 | 12：接纳到远端映射、输入、取消和核对；active 无映射、查询或控制时换远端任务被拒 |
| 准确激活和有界批准 | [扩展](../extensions/README.md)、[评测](../evaluation/README.md) | 13、14、15：在线零离线窗口、显式离线租约、已撤回拒绝；错锁／实例、未 ready、原 use 续期、零离线资格发租约被拒 |
| 严格方法边界 | 本页与登记表 | 40 个冻结方法各有未知 payload 字段反例；未知／reserved 方法、租户注入、错误信封及裁剪绕过验证被拒 |

完整文件与复现命令见[示例说明](examples/protocol/README.md)。真实互操作验收仍须运行两个独立实现，注入提交前后崩溃、连接中断、并发竞争和权限变化，并查验两端业务记录及实际目标状态；静态合同检查不能代替这些证据。
