# 应用、界面与用户输入

[模块与数据 UML](../uml-models.md#interaction) · [可编辑 UML 图册](../diagrams/uml-models.drawio)

[总览](../README.md) · [任务运行](../orchestrator/README.md) · [授权](../security/README.md) · [公共调用契约](../contracts/README.md)

应用层让用户创建和查看任务、处理输入与授权请求、管理个人信息并接管执行。它覆盖 C5、C6、C7 及 A4。Surface 是具有固定负责端的界面会话，可关联一个任务，也可独立承载记忆管理或应用内容；Surface 与任务生命周期分别保存。

默认提供 CLI 和轻量 Web 适配器，Web 支持生产入口和本地调试装配。交互服务保存 Surface 与输入转交责任，Orchestrator 保存任务输入消费与任务控制，授权服务保存许可决定。界面不能通过渲染一个“同意”按钮取得授权裁决权。

本页的输入消费、版本复核和恢复语义属于适配器共同契约。参考实现核心使用 Go；浏览器、CLI 和设备统一经 `/v1/connect` 的 WSS 双向长连接调用，同进程使用 Go 接口，服务跨进程使用 gRPC。HTTPS 保留发现、认证与大文件传输。界面样式可以替换，替换后仍须区分“本端已保存”“业务已消费”和“目标效果已证实”。

## 实现阅读路径

先阅读本页的职责与行为，再读[实现设计](implementation.md)：声明式快照、可靠输入与受信确认。实现设计规定内部记录、事务、算法与故障实验；[机器契约](../contracts/schemas/protocol.schema.json)和[方法登记](../contracts/schemas/methods.json)提供精确线字段。

实现阅读顺序为[软件形状与依赖](implementation.md#module-shape) → [核心对象流转](implementation.md#data-flow) → [输入跨提交域时序](implementation.md#key-sequence) → [生产可用性与性能](implementation.md#production)。服务、连接层与宿主渲染器可分开扩容，实际请求和确认的消费仍归业务 owner。

本页与实现设计均为待实现规格，静态序列通过不代表服务、隐私隔离或恢复机制已经运行。

## 1. 用快照恢复界面，用业务记录确认输入

默认界面读取可版本化的快照，变化通知只负责唤醒。相较于把所有渲染增量变成必须连续接收的指令，这使新设备和断线设备可以直接恢复当前状态；代价是应用维护快照版本与分页查询。只有实际带宽或交互频率证明有必要时，才增加局部补丁，并继续保留完整快照入口。

同一条 WSS 连接承载 Command、Query、原回执查询与 Change 提示。Change 可以合并、重复或丢失；重连、提示缺口或版本不一致时，宿主重新读取当前获准快照，不依赖通知连续回放。连接建立、帧收到和 ReplyAck 都不证明用户已看到页面或业务输入已消费；精确帧及恢复规则见[公共传输契约](../contracts/transport.md)。

浏览器握手使用同源 Secure、HttpOnly 会话 Cookie，并由入口校验 Origin；CLI／设备使用受限 bearer 凭据。握手只建立连接主体，后续每条业务消息和状态披露仍检查当前身份、凭据代次与权限，连接存活不延长会话或内容资格。设备断线后沿原输入和命令身份恢复，UI 在取得原业务回执前保持待发送、处理中或离线状态。

下图以“用户为任务选择保存目录”为例。箭头表示命令或查询，两个写入点分别确认本地转交和业务消费。

```mermaid
sequenceDiagram
    participant U as 用户界面
    participant I as 交互服务
    participant H as Orchestrator
    U->>I: 原 input_id、请求修订、目录回答
    I->>I: 保存输入及唯一目标 command_id
    I-->>U: 已保存，等待任务处理
    I->>H: 用固定 command_id 提交回答
    H->>H: 原子消费输入请求并保存后续工作
    H-->>I: 原消费结果及任务修订
    I->>I: 保存回执及呈现修订
    U->>I: 查询原 input_id 与当前快照
    I-->>U: 已生效或明确拒绝原因
```

| 发起方 → 处理方 | 交接输入 | 持久成功点 | 失败后由谁继续 |
| --- | --- | --- | --- |
| 用户宿主 → 交互服务 | 原输入、请求修订、回答及精确预览引用 | 交互服务保存 InputSubmission、唯一目标命令和转交责任 | 宿主查询原 input_id；交互服务恢复转交，不生成第二个目标命令 |
| 交互服务 → Orchestrator／已绑定处理器 | 固定目标命令、业务请求与用户回答 | 业务负责端原子消费请求并保存决定及后续工作 | 交互服务查原命令；业务负责端继续已接纳责任 |
| 受信确认宿主 → Grant owner | 本人认证和准确授权请求 | Grant owner 保存许可决定，界面取得固定回执 | 宿主查原授权；普通 Surface 不补造许可 |
| Orchestrator／应用投影器 → Surface owner | 已绑定来源修订与完整声明式快照 | Surface owner 提交新快照和变化提示责任 | 投影器重读源状态；宿主读取当前快照补齐提示遗漏 |

表格只汇总责任，输入消费和撤回规则分别见[输入](#input-consumption)与[故障恢复](#interaction-recovery)，对象字段集中在[契约查阅](#interaction-contracts)。

同宿主共库时，输入登记和 Orchestrator 消费可以合并为一个事务，避免额外队列。跨端时交互服务必须先持久化唯一目标命令，再向 Orchestrator 发送；重启沿原命令查询或原样重投。没有耐久宿主支持的浏览器只能显示“本端待发送”，不能承诺清理浏览器数据后仍能恢复未发出的回答。

## 2. 会话与呈现有独立生命周期

Surface 的业务快照由固定 surface_owner_id 管理。任务 Surface 指向 orchestrator_id/task_id，独立 Surface 指向一个已注册应用处理器。处理器声明允许的事件种类、输入格式和幂等查询；不能把任意界面事件路由到任意管理接口。

任务投影更新绑定 task_revision，交互服务只应用较新的源修订；同修订不同内容视为冲突并回查 Orchestrator。独立 Surface 的应用处理器通过预期 Surface 修订更新快照。两类更新均同事务保存新快照和变化提示责任，提示丢失时完整读取仍能恢复。

每台设备独立保存打开、关闭及最后查看修订。关闭页面停止本端主动呈现，不取消任务，也不撤回已消费输入；稍后结果到达只更新可查询记录，不自行重新打开页面。用户明确点击取消时，才发送任务控制命令。设备接管入口直接到[执行资源控制](../execution/README.md)，不等待模型解释或普通聊天输入。

跨端发现按用户已登记 Orchestrator 和 Surface 负责端分别查询目录。目录可能因部分端点失联而不完整，返回 unreachable_endpoints；不创建一个看似全局权威的聊天索引。任务标题、预览及目录条目本身也需读取权限，任务 ID 不能绕过检查。

界面呈现只使用本次读取获准的内容引用。正文、截图及派生内容保留精确版本、摘要与来源；读取和披露分别检查[内容与记忆规则](../memory/README.md)。缓存到期、来源失效或权限改变后，既有卡片显示不可用，不继续用缓存提供新的阅读或回答依据。

```mermaid
flowchart TB
    S[Surface 快照与请求修订] --> G{当前内容与披露资格}
    G -->|允许且必需材料已取得| V[显示内容并开放相关输入]
    G -->|缺权限、到期或缺字节| B[显示缺口并禁用依赖输入]
    V --> A[提交精确请求和预览版本]
    A --> C{业务负责端复核}
    C -->|当前请求仍有效| D[消费一次并保存回执]
    C -->|已消费或版本改变| R[返回原结果或要求刷新]
```

<a id="input-consumption"></a>
## 3. 输入、主观验收与真实授权

InputRequest 由实际业务负责端创建，固定 request_id、revision、schema、期限及必需预览。Orchestrator 对同一请求只消费一个有效回答；两个设备同时作答时，先成功提交的回答获胜，另一端收到已消费的请求及可披露的回执，不悄悄覆盖。

普通澄清只改变其允许的目标参数。对开放式成果的用户验收，必须绑定精确成果版本及未满足条件；验收结果交 Orchestrator 决定 completion_basis，不能覆盖未知副作用。任务目标整体改变是否创建关联任务，遵循[任务运行](../orchestrator/README.md)的修订规则。

目标命令由已绑定处理器决定：clarification 调用 task.input；acceptance 调用 task.accept_result，传 request_id、request_revision、候选摘要、goal_revision 和受信用户决定；application 调用已登记处理器。Orchestrator 原子消费原请求并保存验收记录，界面不能通过更换命令类型消费同一请求两次。

授权确认走受信入口：宿主显示请求主体、动作、准确资源、用途、上限、期限和拟使用的数据位置；宿主先固定原业务命令及规范意图，在实际业务负责端登记确认；用户认证并提交决定后，业务负责端在原命令的事务中消费确认并产生决定。请求内容、插件自绘 UI、远端 Agent 的“已获批准”字段都不能替代此过程。界面只传递授权 request_id 与原决定回执，完整 Grant 字段和消费规则集中在[授权](../security/README.md)。

需要“先看截图再确认”的输入，交互服务在返回快照时给出 required_content_refs。宿主先向实际请求负责端调用 `interaction.request_read` 取得准确请求、Schema 和预览要求，再取得并校验这些内容版本后才启用按钮，提交时将实际预览的引用放入 preview_refs。Surface 输入块只保存 request_ref 和呈现标签，字段与约束以请求 owner 的 schema 为唯一依据；旧请求修订或 owner 不可达时禁用依赖输入并刷新或等待。业务负责端复核请求修订、期限、所需引用覆盖关系和当前来源资格；按钮曾经可点击不是消费依据。若只是缺图但普通目录输入仍完整可回答，应用按具体请求的依赖开放输入，不封闭整个界面。

<a id="interaction-contracts"></a>
## 4. 权威对象与业务接口

共同标识、认证、命令回执和幂等规则见[公共契约](../contracts/README.md)。下列对象的 revision 在各自负责端单调增加，不比较不同对象的修订大小。

| 对象／字段 | 约束 |
| --- | --- |
| Surface：surface_id、surface_owner_id、app_binding、task_ref? | app_binding 固定处理器版本；task_ref 是 orchestrator_id/task_id，可为空 |
| Surface：revision、snapshot | snapshot 集中保存 title、blocks、request_refs、expires_at；任务投影另含 source_revision。blocks 只含[严格声明式组件](implementation.md#2-严格声明式组件)，不运行模型脚本 |
| InputRequest：request_id、owner_id、revision、kind、schema、question_ref、deadline、state | kind 为 clarification、acceptance 或 application；权限请求引用安全模块对象，不复制为普通请求 |
| InputRequest：required_content_refs、allowed_actions、consumed_by? | 必需预览绑定精确版本；consumed_by 由业务负责端原子保存 |
| 验收请求绑定：goal_revision、candidate_ref、candidate_hash | 仅 acceptance 必填；改目标或候选后创建新请求修订，旧确认不得套用 |
| InputSubmission：input_id、revision、surface_id、request_id、request_revision、answer_ref | 首次接纳后不可换回答；状态变化递增 revision；新回答用新 input_id，仍受一次消费约束 |
| InputSubmission：preview_refs | 提交时实际取得的精确 ContentRef 列表；无预览要求时为空。必须覆盖原请求 required_content_refs，不用新版本替代旧版本；消费时还须通过当前资格复核 |
| InputSubmission：target_command_id、state、withdrawal_requested、receipt_ref | state 为 queued、sending、applied、rejected、withdrawn；sending 表示已领取发送，可能已消费；queued 只证明交互服务已耐久保存 |
| Presentation：surface_id、endpoint_id、intent_revision、open、seen_revision | 本端负责；seen_revision 仅是显示遥测，不证明用户理解或同意 |

| 方法 | 业务输入／输出 | 成功与失联后的责任 |
| --- | --- | --- |
| interaction.request_read | request_ref；返回当前准确 InputRequest 与材料缺口 | 实际请求 owner 认证并复核披露；旧修订返回 revision_conflict，不拿旧按钮消费新请求 |
| interaction.surface_create | 应用绑定、可选 task_ref、初始快照；返回 surface_id | 保存 Surface、应用绑定及原命令回执后 applied |
| interaction.surface_read | surface_id、可选已知修订；返回获准快照或未变化 | 每次复核当前权限；内容不可取返回具体缺口 |
| interaction.surface_update | surface_id、预期修订、完整声明式快照及源修订；返回新修订 | 只允许已绑定处理器或 Orchestrator 投影器调用；保存快照及提示责任后 applied |
| interaction.surface_list | 负责端、有限筛选及游标；返回可披露条目、下一游标及完整性 | 固定查询切点和有限候选，每页复核权限；失联端单列 |
| interaction.input | 原输入、请求修订、回答与预览引用；返回 input_id、state | queued 后由交互服务 job 负责转交；重复读原输入得到固定决定 |
| interaction.input_read | input_id；返回原转交和业务消费回执 | 不能凭当前表单消失推断历史回答成功 |
| interaction.input_withdraw | input_id、预期修订；返回 withdrawn 或 withdrawal_requested | queued 时原子撤回；已发送则仅保存核对责任，不能保证阻止业务消费 |
| interaction.present | surface_id、预期 intent_revision、open/close；返回新修订 | 负责端保存本设备意图后 applied；迟到结果不覆盖 close |
| interaction.application_event | 独立 Surface、准确 surface_revision/app_binding、event_id、事件类型及负载引用；返回固定目标 service/command_id 与转交状态 | 仅调用固定应用处理器，按输入相同的持久转交规则恢复 |

独立应用处理器必须实现幂等消费及原命令查询。只能接收普通回调且无法查询的处理器，不可用于承诺可恢复的有副作用事件。Surface 可以清理，但未收束输入及其目标映射按命令保留策略继续保存；清理历史显示不删除业务消费事实。

<a id="interaction-recovery"></a>
## 5. 故障、拒绝与用户后续动作

| 场景 | 对外行为与恢复 |
| --- | --- |
| 用户回答已被 Orchestrator 消费，UI 回执丢失 | 显示处理中并查原 target_command_id；返回原应用结果，不再创建回答 |
| WSS 网关断开，原输入是否提交不明 | 保留原 input_id 与 command_id；重连后查原回执和当前快照，不把连接失败当作业务失败，也不更换身份重试 |
| 两台设备提交不同答案 | 一份被消费，另一份明确 request_already_consumed；刷新到新快照，必要的新澄清必须由负责端发起 |
| 用户提交时请求或候选已更换 | 拒绝旧修订，保留拒绝回执并刷新原请求；旧答案不能自动套用新目标或新候选 |
| 预览已看过，但提交前到期、撤权或来源关闭 | 禁止消费依赖该预览的输入，给出具体材料缺口；由请求负责端重建有效预览或等待资格恢复，用户重新查看后再提交新输入 |
| 预览引用版本不符或正文未取得 | 不启用依赖按钮；若绕过宿主直接提交，业务负责端仍拒绝，不能以 seen_revision 或“用户已读”字段放行 |
| 关窗后重启，正式成果到达 | 保持本端关闭意图；任务目录可查成果。主动打开后才重新读取正文 |
| Surface 所在端在线，请求 owner 失联 | 禁用新的依赖输入；失联前已保存的 queued／sending 输入保留原目标和恢复责任，仍显示尚未确认生效。尚未发送的输入可撤回，发送结果不明时查原命令 |
| 模型生成伪造权限按钮或管理链接 | 作为不可信文本呈现；实际批准只能进入宿主认证的权限页面 |
| 读取权限撤销，用户仍有删除或取消权 | 关闭正文展示，保留最小可授权管理元数据与控制入口；不能要求读正文才能管理 |

排队输入撤回与发送领取在交互服务内原子竞争：撤回先提交则永不发送；领取先提交则显示“撤回待核对”，不能声称已阻止消费。没有原请求撤销能力时，应明确用户需继续用业务取消或纠正入口处理。

输入超出 schema、请求过期和版本冲突返回明确拒绝，保留原拒绝回执；读获准新请求后才能提交新的回答。来源不可用与当前无权限分别表达，前者可等待原材料恢复，后者须从本人管理入口取得相应用途资格，不能靠刷新反复尝试读取。

## 6. 交互验收

| 用例 | 刺激 | 预期可观察结果与目标 |
| --- | --- | --- |
| UI-01 | 消费后丢答复、重启界面并重投 | 一次消费，一个目标命令；C5、A4 |
| UI-02 | 两设备并发回答不同目录 | 一胜一拒绝，保存目标不漂移；C7 |
| UI-03 | 必需截图过期或被撤权 | 缺口可见，相关回答禁用，普通无依赖输入仍可用；C2、C7 |
| UI-04 | 关窗与结果到达竞争 | 本端关闭意图不被结果覆盖，任务事实不被关闭修改；C6 |
| UI-05 | 独立记忆管理 Surface 无 task_ref | 可分页查看获准元数据并发起受控修改，接口不强制建任务；C2、C6 |
| UI-06 | 将伪造批准放入 Skill 或外部输出 | 无 Grant 产生，真实受信确认仍需用户认证；C7 |
| UI-07 | WSS Change 丢失、合并、乱序或重连后提示不连续 | 完整快照恢复，输入和效果判断不依赖通知连续性；A4 |
| UI-08 | 预览后更换候选或关闭来源，再提交已启用按钮的回答 | 原输入拒绝并有固定回执；刷新显示准确缺口，不自动把旧确认用到新版本；C5、C7 |

Surface、排队输入、快照字节、WSS 连接、连接发送队列和每用户订阅数均设置有限配额；满额拒绝新 Surface/输入，保留取消、撤回、回执查询和已接纳转交的容量。慢客户端的 Change 可合并或丢弃，持续阻塞时断开并要求从快照恢复；业务回执与已接纳责任留在原 owner，不随连接队列丢弃。浏览器储存、CLI 终端显示和原生端各自验证实际能力，不从其中一个适配器的通过结果外推全部 UI。
