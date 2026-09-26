# 身份、许可与接口

[总览](README.md) · [处理机制](mechanisms.md) · [跨端合同](cross-endpoint.md) · [验证与交付](validation.md)

本页是本模块字段和内部接口的权威定义。以下表格定义逻辑对象与内部接口；其可发送字段与 v1 领域类型沿[跨端合同](cross-endpoint.md)及对应 Schema。协议已有 UUID、UTC 时间、十进制 uint64 及错误编码沿用[公共字段](../endpoint-communication/wire-format.md)；内部修订同样不得绕回。

<a id="identity"></a>
## 1. 从登录身份到受信调用上下文

| 对象 | 最小内容与绑定 | 建立与撤销责任 |
| --- | --- | --- |
| 用户 | 内部 user_id；云端外部身份唯一键 `(issuer, subject)`；账户状态与身份修订 | 认证适配器验证外部身份，授权存储映射到内部用户。邮箱、显示名及请求载荷不能作用户主键；账户禁用使新使用拒绝 |
| 主体 | actor_id、所属用户、kind=user／agent／service、状态、受信宿主绑定 | 用户主体由登录建立；Agent／服务由受信组装或登记建立，插件安装不自动取得用户权限 |
| 逻辑端点 | endpoint_id、用户、登记状态、ledger_id、活动 owner_instance_id／owner_generation | 登记入口建立，不以客户端自报 endpoint_id 认证。账本丢失按新端点或恢复缺口处理 |
| 活动所有者 | 端点与持续账本、实例、代次、隔离适配器引用、资格状态 | 同账本独占运行锁与受控执行入口联合落实；新实例须证明旧实例无法写入及启动新行动，网络超时不构成证明 |
| 登录会话 | session_ref、用户、actor、已认证端点／实例、认证时间与强度、有效期、撤销状态 | 不透明随机引用；服务端保存摘要及绑定。受信宿主代 Agent 调用时从隔离实例映射 actor，不接受任意 actor 参数 |
| 连接票据 | 票据摘要、用户、端点、owner 代次、gateway 受众、绝对到期、消费结果 | 身份接入签发，目标网关单次原子消费；具体外部字段沿现有 HTTP 契约 |
| RequestContext | 受信 user_id、endpoint_id、owner 资格、原行动 actor_id、认证依据及已验证消息关联 | 入口先独立确定调用身份，再解析 grant_ref 核对叶子主体；资源服务代原请求调用时另认证当前处理服务，不能把两种主体混成一个 actor |

云端默认用标准 OIDC 登录适配器，再建立 Harness 自己的会话。适配器验证签名、预配置 issuer、audience、有效期及适用的 nonce，将 `(iss, sub)` 映射为用户；不把上游 ID Token 当作业务授权。原生客户端用外部浏览器及授权码 + PKCE，浏览器由受信后端管理会话并落实 Origin、CSRF 与 Cookie 限制。依据为 [OIDC Core](https://openid.net/specs/openid-connect-core-1_0.html#IDTokenValidation)、[RFC 8252](https://www.rfc-editor.org/rfc/rfc8252.html)及 [RFC 9700](https://www.rfc-editor.org/rfc/rfc9700.html)。这三个标准不定义本项目的许可。

本地独立部署可由 OS 用户与受限本地 IPC 建立身份，但必须显式登记为独立用户／端点权威；它不能在断网后冒充原云端用户的签发方。账号合并和本地权威迁移未定义，首期拒绝。

原行动主体必须与叶子许可的 subject.actor_id 相等。对同端点多个 Agent，受信宿主从隔离运行实例取得 actor，在进入共享 SDK／发送队列前检查相等及允许的 source；插件不能直接写入已认证连接。对远端独立 Agent，默认每个逻辑端点绑定一个可代表的 actor；宿主复用多 actor 仅在接收端信任其上述隔离检查时开放。协议中“由 grant_ref 解析委派主体”据此解析待验证的主体，而非仅凭引用就选定调用身份。无法证明来源代表关系时拒绝。

处理端调用 BeginUse 时同时携带受信入口封装的原行动上下文和自己的服务认证；原上下文通过本地受控调用或已验证的网关／操作关联取得，不能让外部请求任填。前者匹配许可 subject，后者匹配 processor／audience。Delegate 同样要求原调用方匹配父许可主体且父许可允许委派，或由真实用户管理入口作明确批准；仅能查看父许可不等于能委派它。

登记和确认默认由受信应用后端或本地宿主调用下文接口。远程设备凭据由该接入适配器负责安全登记与校验；只有验证出的会话已绑定正确端点／活动实例，才能申请连接票据。首期不定义通用设备配对协议；没有这种适配器的部署不开放远程自助登记。

票据使用至少 256 位随机熵，数据库只保存摘要。申请仍为 `POST /harness/v1/connect-tickets`，请求体只有 `endpoint_id`，owner 等依据从已认证 HTTPS 上下文及身份存储取得，不能新增未定义字段。消费事务同时校验未消费、未过期、会话和账户仍有效、端点及 owner 当前资格，并保存新连接绑定。并发消费只有一个成功；消费成功但 welcome 丢失时重新申请票据，旧票据不得再消费。票据不进入 URL、日志或持久业务消息。

同一 owner 的多条连接共享配额与账本，不创建第二个所有者。实例接替首期只开放具有受信本地监督器、独占账本锁及执行入口隔离的部署；跨主机接替缺少等价证明时等待。云端废止凭据能阻止新的在线连接，不能证明离线进程已停止。

<a id="grant"></a>
## 2. 许可与具体使用意图

许可范围不可原地扩大或重写。改变范围签发新许可，旧许可按需撤销；撤销状态和消费状态另存，不能把被消费许可改回未消费。单次和持续许可使用同一范围模型。

| Grant 字段组 | 含义与约束 |
| --- | --- |
| `grant_ref`、`user_id`、`issuer_id` | 不透明引用及唯一授权权威；用户从上下文核对，不靠引用随机性隔离 |
| `subject` | 精确 actor，以及允许代表该 actor 的源端点／受信宿主集合；子许可另绑定被委派 actor |
| `audiences` | 可落实许可的处理端点和资源服务集合；中继不能因转发而成为受众 |
| `scope` | 有限条目的集合，每项为资源选择器、动作、用途、数据使用约束；不同条目不能拆开拼出未批准组合 |
| `constraints` | 任务或操作绑定、允许接收方、处理位置、保留上限、参数限制与执行前提；未声明为可选的约束必须全部落实 |
| `not_before`、`expires_at` | 有效区间 `[not_before, expires_at)`；签发时固定，重投或续连不延长 |
| `mode`、`use_authority` | `once` 或 `continuous`；once 由唯一消费权威裁决，默认在线授权权威，离线时只允许指定端点账本 |
| `parent_ref`、`delegation_depth` | 根许可无父；子许可只有一个父，整链均有效，深度受限；用户确认根许可或父许可显式允许委派 |
| `confirmation_ref`、`policy_version` | 根许可的受信批准事实、适用硬限制版本；模型或 Skill 产物不能充当批准 |
| `offline` | 缺省禁止；启用时固定端点、owner 代次、账本、时间锚、离线截止及可验证证明，见[离线机制](mechanisms.md#offline) |

资源选择器首期只支持精确规范资源 ID，或同用户、固定资源类型和容器的成员集合。容器成员关系由资源权威验证，不能按不规范字符串前缀或模型推断判定；集合包含关系不可证明时拒绝委派。动作与用途取已安装目录中的有限标识，未知标识拒绝。

读取、提取、短期上下文使用、长期保存、同步、向指定接收方披露及行动分别检查。`purpose` 是调用方声明并由受信处理链绑定的用途，授权服务不能证明人或任意插件的真实意图；约束执行依赖受控数据出口与隔离。无法阻止插件任意联网的部署不能声称实现了禁止外发。

| UseIntent 字段组 | 提供与核对责任 |
| --- | --- |
| `resource`、`action`、`purpose` | 资源适配器解析真实对象，固定动作和用途；调用方自报路径不是最终对象身份 |
| `processor`、`recipient`、`location` | 真实处理服务、数据接收方及处理位置；在数据交给模型前同样检查 |
| `task_binding`、`operation_binding` | 适用 task_id、owner_epoch 及原 operation_id；准入前 Evaluate 可无 operation_id，按固定意图检查；BeginUse 必须绑定已分配的原操作。无任务的独立使用可省略任务绑定 |
| `intent_binding` | 覆盖固定资源、动作、参数、用途、接收方、处理端及来源集合的规范化内容／摘要；授权与处理端必须使用同一版本的规范化器 |
| `source_bindings` | 完整来源、版本、用途及许可依据，由受信组装器建立，不能让模型删项；与[内核来源集合](../task-kernel/storage-and-interfaces.md)一致 |
| `required_obligations` | 此次使用必须落实的约束及资源处理端能力；不能执行某约束时拒绝，不能只记录后继续 |

一项使用及其全部来源检查必须同时成立：每个来源都获准用于本用途／接收方才可放行。多项独立操作分别判定。对文件，处理端要解析符号链接、稳定绑定文件／父目录并在实际打开时防止对象替换；对 GUI，限制到已验证设备、应用及动作；缺少足够约束能力时只开放能落实的资源范围。

UseIntent 由受信调用上下文建立；本地值及跨端编码见[授权领域合同](cross-endpoint.md#use)。主体字段即使出现在领域载荷中也只能校验受信绑定，不能建立身份。入站 request 仍只携带 `authorization={grant_ref}`；处理端依据真实参数、受信操作关联及所属领域提供的完整来源解析结果建立 UseIntent。远端无法取得这份来源和意图绑定时返回依据缺口，不假定省略即无来源。response／event／control 仍不带 authorization：按受信来源、原操作或订阅关联及当前内容使用权核验，不能因为它是“回复”而绕过披露检查。跨端完整来源通过 [source.prove 与精确来源绑定](../content-and-provenance.md#remote)取得，授权服务核对该证明与本次意图一致。

<a id="interfaces"></a>
## 3. 内部服务接口

所有接口带由入口构造的 RequestContext，服务身份调用也按目标用户受限。变更接口带固定 `command_id`、内容绑定及 deadline；命令 ID 在用户内唯一。下表命名为逻辑契约，SDK 可映射为类型化方法，不允许客户端直接写授权表。

| 接口 | 关键输入 | 输出与成功依据 |
| --- | --- | --- |
| `Authenticate` | 接入适配器验证后的身份依据 | 受信上下文／认证失败；不授予资源行动权限 |
| `RegisterEndpoint` | 受信用户管理操作、账本与实例登记依据 | 固定端点登记回执；ledger 冲突或隔离证据不足拒绝 |
| `ActivateOwner` | 原端点、预期 owner 代次、同一账本恢复证明、旧实例隔离证据、新实例 | 条件提交新 owner 及回执；隔离证据必须覆盖全部写入和执行入口，不能只证明旧连接关闭 |
| `IssueConnectTicket`／`ConsumeConnectTicket` | 已认证端点；或票据、网关与连接关联 | 票据／一次消费后的连接绑定；外部映射沿现有协议 |
| `CreateConfirmation` | 用户、显示内容的固定摘要、完整 Grant 范围、期限、受信呈现入口 | 确认请求 ID；服务保存完整范围。确认窗口内任何扩大、换对象或用途都须新建请求 |
| `ConfirmAndIssue` | 原确认请求、经受信入口认证的用户批准、固定 command_id | 同一事务消费确认并签发根许可、保存回执；拒绝、过期或重复回应不能签发第二份 |
| `ReadConfirmation` | 原确认请求 ID、创建方的受信关联或本人管理身份 | 待确认／已拒绝／已过期／已签发及获准披露的 grant_ref；只说明确认事实，使用前仍 Evaluate |
| `Delegate` | 父许可、目标主体与宿主、收缩范围、确认或父许可允许的委派依据 | 子许可及回执；与撤销共用串行裁决，跨用户、不可判定包含或深度超限拒绝 |
| `Evaluate` | grant_ref、UseIntent、所需新鲜度 | allow／deny／indeterminate、依据修订与全部约束；只供规划、准入与预检，不消费也不充当启动回执 |
| `BeginUse` | grant_ref、UseIntent、处理端身份、原操作及固定使用键 | 当前可用的使用回执或拒绝／提交未知；单次占用及回执一起保存；具体语义见[消费](mechanisms.md#use) |
| `BeginUseSet` | 同一用户／消费权威、原 command_id／operation_id、固定集合摘要及 1～64 个 grant_ref／UseIntent | 同一有限使用单元的全部成员一次校验、原子消费；返回整体历史回执及本次集合判定。任一成员不满足则不新增任何消费，提交未知核对原集合；见[集合消费](mechanisms.md#use-set) |
| `ReadUseSet` | 原消费权威、command_id、operation_id，当前查询权限 | 原完整集合回执或明确缺口；当前未见不证明未提交，历史回执不授予新启动资格 |
| `ReadUse`／`ReadCommand` | 原使用键／原 command_id，当前查询权限 | 原提交事实或可核对缺口；查到历史成功不代表当前可启动，不返回今天无权披露的历史正文 |
| `Revoke` | 精确许可、主体或端点，以及当前管理权限与预期修订 | 撤销提交修订、传播跟踪引用；不报告物理停止。许可撤销不删除原使用记录 |
| `ReadAuthorityState` | 当前用户管理身份、过滤条件及分页游标 | 本用户主体、端点、许可范围、消费及撤销状态的受限视图；敏感历史仍检查当前披露许可 |
| `ReadRevocation` | 原撤销命令与当前管理权限 | 已提交修订、各相关端点已应用位置／未确认、未到期离线许可边界；操作效果需另查执行系统 |
| `OpenRecovery`／`ReadChanges`／`ApplyRecovery` | 恢复轮次、端点／owner、所需集合；游标；完整快照或连续页 | 固定切点和接续游标；持久应用后的授权恢复依据或缺口，见[同步](mechanisms.md#sync) |

受信确认入口由应用宿主维护，只允许真实用户会话执行 `ConfirmAndIssue`；Agent／插件只能请求确认，不能提交用户批准。本地管理直接调用身份接口；跨端确认沿[受信确认绑定](cross-endpoint.md#confirmation)，把原确认、完整范围摘要、呈现入口、一次性挑战与真实用户认证绑定。`task.input` 已接纳不等于许可已签发。

用户管理接口以当前未禁用用户的受信登录／本地 OS 认证为根依据，限本人，签发及敏感变更要求部署配置的近期认证；不要求用户先拥有可由自己签发的行动许可。Agent 和后台服务没有这种隐含管理资格。用户可分页查看范围和状态、批准新许可、撤销或以新许可替换旧范围；账户恢复及运营人员代用户操作不在首期接口内。

`BeginUse` 使用键固定为 `(user, grant_ref, operation_id, use_slot)`；use_slot 由已安装动作模式固定为 `(unit_no, source_role)`，区分有限使用单元及其来源。处理端在本地账本先持久分配 unit_no，重投和恢复复用原号；同一单元不得因超时换号，下一单元须由已安装动作模式依据进度有界产生。GUI 独立动作仍各自使用内核分配的 operation_id，不靠 unit_no 绕过准入。

once 许可对整个 grant 只有一个操作／意图占用，仅允许一个使用单元；全部来源 slot 继承首次消费时固定的启动截止，不能换 slot 或 command_id 刷新。continuous 对每个固定使用键保存回执，新单元要重新检查当前权限。同键异意图返回冲突。

集合按 `(grant_ref, unit_no, source_role)` 固定排序，成员键不重复；完整规范值和 `set_sha256` 都参与原意图核对。所有成员具有相同的操作、actor、processor、处理位置、接收方和 unit_no，资源及来源角色可以不同。请求中的权威须与受信路由、各 grant 的实际消费权威一致。集合回执绑定原 command_id、权威、操作、完整成员、共同 start_before 及各成员原回执。已登记集合不能靠单项 BeginUse 增补成员或刷新截止；单项查询／复核可返回其中原回执。不能取得完整集合回执及当前集合 allow 时，不启动依赖该集合的行动。

| Decision / UseReceipt 字段 | 语义 |
| --- | --- |
| `decision`、`reason` | Evaluate 的 allow／deny／indeterminate 及受限原因；indeterminate 绝不按 allow 处理 |
| `receipt`、`current_decision` | BeginUse 分别返回不可变历史使用回执和本次串行复核的 Decision；仅 current_decision=allow 且回执仍有效可用于启动。ReadUse 只返回历史事实及查询状态，不返回启动资格 |
| `basis_revision`、`policy_version` | Decision 记录本次读取版本，receipt 保留原消费版本；仅用于解释与恢复，不代替当前检查 |
| `grant_chain`、`intent_binding`、`processor_binding` | 权限链、固定意图、处理端及 owner；回执不能换目标或转交别的处理端启动 |
| `obligations` | 处理端必须执行的全部约束 |
| `use_receipt_id`、`start_before` | BeginUse 才有；启动必须早于此时间，同时满足 grant、调用和本地控制期限。Evaluate 无此资格 |
| `commit_state` | 变更接口独立返回 Committed／Rejected／Unknown；不与 allow 等权限结论混用 |

<a id="errors"></a>
## 4. 错误、超时与版本

先验证身份、用户与对象可访问性，再查询原请求、比较意图及判断新操作前提。无权限时不透露资源或许可是否存在。用于历史回执的披露权限与用于新增行动的权限分别检查。

| 内部原因 | 已有线错误或对外表现 | 调用方继续行为 |
| --- | --- | --- |
| 身份失效／不可验证 | `core.unauthenticated` | 重新认证；不能更换业务操作身份以绕过 |
| 跨用户、无许可、已撤销、用途或受众不符 | `core.forbidden` | 停止该使用，用户改变授权后重新判定 |
| 同键异意图 | `core.identity_conflict` | 保留原记录，拒绝改写；不能自动换键重试有副作用操作 |
| 确认过期、修订冲突、单次已绑定其他操作 | `core.precondition_failed` | 按原记录核对；需要新意图时重新确认 |
| 不支持的类型／版本／必要扩展 | `core.unsupported_type`／`core.unsupported_extension` | 补齐实现／协商前拒绝此能力；已知类型中无法落实的资源约束用 `core.forbidden` |
| 权威暂不可达、时间或同步依据不足 | indeterminate；未接纳请求可用 `core.unavailable`，具体原因保留内部 | 核心保存 blocker 并有限重试；原操作有可能启动时先查原记录 |
| 过载且确定未接纳 | `core.overloaded` | 遵守等待指示和原期限；控制／撤销使用保留容量 |
| 提交是否成功未知 | Unknown 与原键；不伪装已拒绝 | ReadCommand／ReadUse 核对或原样重试；当前未见不等于未提交 |

线错误名称以现行注册为准，内部原因不增添协议错误枚举；适配器须选用确已注册的公共码。同步只读超时可原样重试；变更调用超时不撤回事务。取消等待一个授权请求只停止调用方等待，若需撤回已签发权限，必须查清原提交后发起 Revoke。版本不支持时拒绝，不忽略安全约束。接口及规范化器版本随部署锁定，跨端证明固定采用[授权 profile 1](cross-endpoint.md)。
