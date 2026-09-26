# 线字段、方法登记与恢复一致性

[共同调用语义](README.md) · [传输配置](transport.md) · [方法查阅](methods.md) · [序列用例](examples/protocol/README.md)

本页与机器资产定义未发布的 `harness/1`、`full-harness-draft-2` 配置。原有 40 个严格方法、本轮补齐的 53 个保留方法，以及为预算关闭、输入请求读取、在线费用结算与可信确认补充的 8 个交接方法，共构成 101 个领域方法；当前登记无 reserved 方法。发现、WSS 双向交接、认证证明与内容字节另由传输配置规定，服务间 RPC 另由[gRPC 绑定](grpc.md)规定，不能把领域方法数量当作完整服务互操作证据。

`frozen-draft` 表示当前修订具有精确输入、输出和关联用例，仍可随未发布设计统一修订。发布时须共同冻结正文、Schema、登记及用例摘要，不能以另一份变化中的正文解释已发布消息。进程内实现使用相同对象与业务语义，不要求先编码网络报文。

## 1. 资产及实现边界

| 资产 | 权威内容 | 实际检查 |
| --- | --- | --- |
| [protocol.schema.json](schemas/protocol.schema.json) | 共同对象、全部领域输入输出、正文类型与序列容器 | JSON Schema 2020-12、日期格式及封闭业务对象 |
| [methods.json](schemas/methods.json) | 方法种类、输入输出映射、目标、条件修订、回执阶段、错误与恢复动作 | 按具体方法分派，不接受同名异义或自由字段 |
| [transport.schema.json](schemas/transport.schema.json) | 发现、WSS 帧、投递、回复、上传、关闭索引查询与证明载荷 | 独立结构与跨字段向量；共享对象引用领域Schema |
| [领域校验](../validation/validate_protocol.py) | 有限记录序列的身份、版本、状态及恢复关系 | 每个方法至少一项有效调用，新增方法有结构与关联反例 |
| [传输校验](../validation/validate_transport.py) | 原请求／回复摘要、三类交接、内容发布及关闭记录 | 结构与关联校验，另运行公开密码学向量 |
| [harness.proto](harness.proto) | 服务间 Call 与 EndpointChannel 的 Protobuf 外壳；JSON 内层复用以上 Schema | 描述符编译与消息映射检查；不等同于 gRPC 服务互操作 |
| [完成判断投影](schemas/task-outcome.schema.json) | 独立结果与效果关系 | 保持原5正例／9反例，不作为完整协议 |

这些资产可以指导两个实现交换同义数据，但没有服务、数据库或驱动。身份、许可、实际效果和来源关系的夹具是构造前提；字段合法不能证明这些前提在运行环境真实成立。安装、签发、创建界面及内容交接已经有精确方法，不再以“预先装配”替代其协议定义；部署缺少相应适配器时按所属模块拒绝或等待。

## 2. 编码与共同决定

| 对象／字段 | 精确选择 |
| --- | --- |
| 标识 | 小写类型前缀、下划线和32位十六进制随机部分；不透明、不可复用，不是凭据 |
| ComponentRef | id、三段version及digest；安装后绑定精确不可变制品 |
| ContentRef | tenant_id、owner_id、content_id、version、hash、media_type、byte_length；引用准确字节版本 |
| ObjectRef | owner_id、id、revision；定位可变对象的准确修订 |
| AuthorizationRef | kind=grant/use/offline_lease、owner_id、id、revision |
| 数值与时间 | 计数／修订／字节长度为安全整数，最大9007199254740991；金额为带单位的非负十进制字符串；时间使用UTC Z |
| Command | command_id、method、target_id、expires_at、payload；只有登记的条件更新方法携带必需expected_revision |
| Query | method、target_id、payload；没有command_id、首次接纳截止或业务持久接纳含义 |
| Receipt | command_id、stage及对应字段；业务决定固定，可按当前披露权限隐藏完整output |
| QueryResult | output、observed_at，可带resource_revision、cursor、gaps；元数据不能与输出修订矛盾 |
| Error | code、message、retry及有限辅助字段；按准确方法登记解释恢复动作 |

端云 WSS 每帧携带严格 JSON；服务间 gRPC 在 Protobuf bytes 中携带同一严格对象。Protobuf oneof、内层 Schema 与方法关联分别校验，不用二进制序列化字节重定义领域请求摘要。完整映射见[gRPC 编码约束](grpc.md#2-protobuf-与领域字段的权威)。

金额不使用浮点计算。签名与传输请求摘要使用[传输配置](transport.md)定义的JCS；拒绝重复键、非法Unicode和整数舍入。原命令请求结构在重试时保持不变，SDK不得补上新默认值、替换预期修订或升级解释版本。

每个方法的target固定为准确对象或负责服务，payload中有关联目标时两者必须一致。所有租户身份来自认证上下文；引用中的tenant只能用于核对，不能授予选择租户的能力。配对的有限预认证例外另按传输配置处理，不把夹具AuthContext当作匿名端点已经认证的证明。

### 回执与恢复

accepted必须有accepted_at，不能带decided_at或业务错误；applied必须有decided_at和该方法合法output；rejected必须有decided_at和已登记Error，不能带output。允许的阶段逐方法登记。execution.invoke的applied确认Operation及执行责任已提交，Operation.execution_state=accepted仍表示尚未跨越发送边界；Operation.effect独立描述目标效果。

当前披露允许固定回执设置redacted=true并省略整个output，不允许任意部分裁剪后逃过结构或身份检查。redacted不改变stage、原时间或原请求，也不使调用方可以重新执行原动作。

完整回执清理后，长期关闭索引按[共同保留规则](README.md)返回gone或输入冲突，不构造新的业务rejected覆盖原applied。原方法回执查询和传输错误分别表达；取消和终态身份保持禁止重新启动。未结责任不受普通查询保留期清理。

## 3. 领域关联及正文类型

| 领域 | 必须保持的关系 | 完整机制 |
| --- | --- | --- |
| 任务与预算 | 固定Home、原提交及操作身份；修订递增；余额与分配不重复消费，封账后才归还；列表切点有界 | [运行时](../task-runtime/implementation.md) |
| Brain与计划 | 原决策固定快照及至多一次物理生成；上下文正文及有界计划都有准确结构，确定性物化仍逐步准入 | [大脑](../brain/implementation.md) |
| 能力与资源 | 精确版本和实例绑定；resource.observe仍产生原Operation；资源代次、GUI观察和TaskGate同时有效 | [执行](../execution/implementation.md) |
| 许可与配对 | Subject来自受信身份，确认一次消费；单次使用与离线分配不扩大来源用途；配对回复丢失不重复发凭据 | [安全](../security/implementation.md) |
| 记忆与内容 | 实际处理输入完整继承来源；上传字节与引用一致；稳定分页、派生候选、视图确认及清理分别保存 | [记忆](../memory/implementation.md) |
| Surface与输入 | 声明式块及固定处理器；呈现意图、实际预览、排队转交与一次业务消费分别判断 | [交互](../interaction/implementation.md) |
| Agent协作 | preparing可无映射；active只有一份子映射；closed要求效果与费用结清；不换远端任务掩盖失联 | [协作](../collaboration/implementation.md) |
| 安装与批准 | 首装／兼容证据与正式改善用途区分；历史激活不变，当前实例开放重新核验；本地事务与远端回执均可表达 | [扩展](../extensions/implementation.md)、[评测](../evaluation/implementation.md) |

Invoke.arguments和行动模板最终参数仍按准确Capability版本、摘要及Binding对应的能力Schema检查。开放的是工具声明的参数结构，不是任意领域消息；能力Schema必须是有界、闭合且引用已固定的声明。夹具仅允许本地引用，运行安装同样须固定所有依赖，不能为校验访问任意网络。

字段之外的不可机械证明条件仍需真实实现裁决，例如来源限制的子集关系、可信用户确认、Grant是否有效、独立真值是否存在。序列容器里的已知许可、请求、批准或环境是显式测试前提；报告必须说明它们未由该静态工具自行建立。

`content.get` 保持同一查询方法，按输入分成互斥的 bytes／control 模式。省略 mode 或 mode=bytes 沿原输入返回 ContentBytesGetOutput 下载定位；mode=control 只携带准确 content_ref 与 copy_id，向当前认证 holder 返回 `{mode:control, control:ContentControl, copy:ContentCopyControl}`。后者仅供自身持有者停止／清理恢复，正文关闭后仍可查询，不含 download_id，不授予读取或保存资格。输出必须与请求模式关联，镜像读取只能采用 bytes 分支；完整字段和恢复机制见[内容接口](../memory/implementation.md#51-小元数据与大字节分开)，正反关联见[持有者控制序列](examples/protocol/55-content-holder-control.json)。

## 4. 方法覆盖

完整签名索引见[methods.md](methods.md)，逐方法错误码及可行恢复动作直接查登记表。本配置不再允许以reserved作为已列领域接口的替代；某个具体服务可以只声明自己实现的子集，但必须一并实现相应查询及恢复义务。

| 所属模块 | 严格方法数 | 查询与恢复重点 |
| --- | --- | --- |
| [brain](../brain/README.md) | 3 | 请求、输出、阶段、错误与原身份关联见方法登记 |
| [collaboration](../collaboration/README.md) | 5 | 请求、输出、阶段、错误与原身份关联见方法登记 |
| [evaluation](../evaluation/README.md) | 13 | 请求、输出、阶段、错误与原身份关联见方法登记 |
| [execution](../execution/README.md) | 14 | 请求、输出、阶段、错误与原身份关联见方法登记 |
| [extensions](../extensions/README.md) | 5 | 请求、输出、阶段、错误与原身份关联见方法登记 |
| [interaction](../interaction/README.md) | 9 | 请求、输出、阶段、错误与原身份关联见方法登记 |
| [memory](../memory/README.md) | 18 | 请求、输出、阶段、错误与原身份关联见方法登记 |
| [security](../security/README.md) | 12 | 请求、输出、阶段、错误与原身份关联见方法登记 |
| [task-runtime](../task-runtime/README.md) | 14 | 请求、输出、阶段、错误与原身份关联见方法登记 |

## 5. 从静态资产取得什么证据

正常序列保存调用方可观察的原回执、查询结果和权威前提。反例通过具体JSON路径改变身份、版本、数量、资格或阶段，每项必须命中它声称破坏的规则；仅因无关格式错误拒绝不算该语义被验证。校验器保留原40方法的回归，不以新增覆盖掩盖已有行为退化。

运行符合还须启动两个独立实现，从接纳入口产生调用，在提交前、提交后回复前、对方确认后本方记账前三处注入故障，并查验双方业务记录及目标真值。签名向量通过不证明认证系统可用；全部 101 个方法的构造序列通过也不证明事务、权限或容量成立。可复现命令与最新计数见[交付审查](../review.md)。
