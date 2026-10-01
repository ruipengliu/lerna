# 协议与 SDK：连接可以重建，业务身份保持

[入口](README.md) · [持久工作](durable-work.md) · [应用交互](applications.md)

客户端提交保存报告，随后断线。它需要找回的是原服务对原命令的决定，以及原文件操作的效果；恢复整条旧 socket 的消息历史并不能回答这两个问题。

共同协议因此以命令、可查询回执和对象修订为基础。WSS 与 gRPC 承担交付，变化流加速读取，业务成功含义仍归实际 owner。

## 1. 只维护一份精确字段定义

当前资产是未发布草案：`harness/1`、领域 `full-harness-draft-2`、端云 `harness-wss-draft-3`、服务间 `harness-grpc-draft-1`。现登记 105 个领域方法。`frozen-draft` 表示这个修订具有精确字段和用例，不表示已发布或已有可运行实现。

| 要查什么 | 单一维护源 |
| --- | --- |
| 领域对象、输入输出和正文结构 | [protocol.schema.json](../architecture/.draft/contracts/schemas/protocol.schema.json) |
| 方法类型、目标、回执阶段、错误和恢复动作 | [methods.json](../architecture/.draft/contracts/schemas/methods.json)、[可读索引](../architecture/.draft/contracts/methods.md) |
| 发现、WSS 帧、内容传输和签名载荷 | [transport.schema.json](../architecture/.draft/contracts/schemas/transport.schema.json) |
| gRPC 消息外壳 | [harness.proto](../architecture/.draft/contracts/harness.proto) |
| Brain 内部生成与正文发布映射 | [brain-generation.schema.json](../architecture/.draft/contracts/schemas/brain-generation.schema.json) |
| 关联正反例及运行要求 | [协议序列](../architecture/.draft/contracts/examples/protocol/README.md)、[系统验收](../architecture/.draft/validation/README.md) |

本目录重写架构解释，不复制这些资产。字段业务含义归所属领域，类型与必填性归同版 Schema；出现冲突须一起修订后才能发布，不能让实现自行选择宽松解释。安装清单固定正文、Schema、方法登记和递归引用的摘要。

## 2. 三种回执不能合成一个成功位

| stage | 持久事实 | 调用方后续 |
| --- | --- | --- |
| accepted | 原请求和处理责任已保存，尚无最终决定 | 查同一 command_id |
| applied | 该方法的决定及下一责任已保存 | 按方法继续查效果、任务或逐目标结果 |
| rejected | 原命令确定拒绝，不再启动其目标行为 | 依据错误修复前提；改参数用新命令 |

并非每个方法允许全部阶段。内部准备不能增加未登记的 accepted，传输超时也不能伪造 rejected。task.submit 的 applied 是任务建立，execution.invoke 的 applied 是执行端接纳，task.cancel 的 applied 是取消决定保存。

幂等键为 `(tenant_id, logical_service_id, command_id)`。原请求的 method、target、expected_revision、expires_at 与 payload 摘要固定；同键异请求冲突。SDK 重投时不补默认值、不换预期修订，也不升级解释版本。

expires_at 限制首次接纳，已经接纳的责任继续按领域期限处理。完整回执清理后，长期最小去重记录仍使原键不可复用；同请求返回 `gone`，不同请求返回冲突。gone 是查询错误，不是将旧 applied 改成 rejected。

幂等键不跨逻辑服务。首次发送前必须保存原服务选择，失答复后只能查原服务；换一个 Orchestrator 即使保留 command_id，也可能建立第二个任务。本地记录全失且受信目录无法定位时，保留未知。

## 3. SDK 把恢复规则做成默认行为

Go／TypeScript SDK 保存完整原命令、接纳回执和业务对象映射，提供准确读取、有界等待与明确控制。只读 Query 没有 command_id，不伪装成持久命令；有副作用的“检查”，例如反馈开放或批准使用，仍按方法登记走 Command。

原提交本地保存失败时不发送。已发送后保存答复失败，仍查已保存的原身份，不能把客户端存储故障报告成业务拒绝。请求 context 或等待 deadline 只结束等待；用户取消必须提交领域控制命令。

错误同时带稳定 code 和可执行 retry 建议，但建议不能扩大领域的重试权。5xx、断连、not_found 均不证明没有外部效果；写结果可能未知时先查原命令和操作。

## 4. 三种绑定共享业务语义

| 位置 | 绑定 | 需要承担的成本 |
| --- | --- | --- |
| 同进程 | Go interface 与明确本地 Tx | 装配和类型边界，保留真实共同提交范围 |
| 浏览器、CLI、设备到云 | `WSS /v1/connect` | 双向交付、流控、身份复核和重连 |
| Harness 服务之间 | gRPC Call／EndpointChannel | 严格外壳校验、服务身份、deadline 和内部流重绑 |
| 发现、认证、大字节 | HTTPS | 与原身份、上传及内容许可关联 |

gRPC 的 Protobuf 外壳承载严格 JSON，领域字段继续只在同一 Schema 定义。这样避免两套字段漂移，代价是运行时 JSON 校验；不宣称逐方法 Protobuf 强类型或二进制压缩收益。

解码前检查大小、重复字段及互斥分支。JSON 拒绝重复键、非法 Unicode、非有限数值和安全整数越界，金额用十进制字符串。Protobuf 也须在普通解码覆盖重复字段之前按固定描述符检查。通过 Schema 后仍执行方法和对象关联校验。

## 5. WSS 与内部流各自恢复

WSS 首个 ready 固定 connection_id、logical_service_id 和限额。客户端实际发送循环分配递增 request_seq；网关按接收顺序维护高水位和有界在途关联，允许响应乱序。序号只关联本连接往返，不承担业务幂等。

每条 WSS 对应一个当前 EndpointChannel，可有一个候选重绑流。内部应用故障时，网关保留外连接、原序号与等待槽，以新 binding_id／更高 binding_revision 绑定同一逻辑服务的健康实例；旧输出不能覆盖新绑定。新内部 ready 不再向客户端发送第二个外 ready。

内部恢复期间普通新请求可返回 dependency_unavailable，已发命令先查原决定。初始请求总处理期限 5 秒不因重绑重置；连续后端不可用达到初始 60 秒上限后关闭外连接。网关自身退出则由客户端以新 connection_id 重连，恢复原命令、Reply 和订阅。

普通队列、在途请求、投递和连接数均有限，控制及收尾有保留容量，慢端无法容纳控制时断连。双方独立读写，不能等业务响应时停止收取取消。逐消息和每次重传都重新检查当前身份与披露资格。

## 6. 设备投递需要两端保存责任

Orchestrator 保存固定 Delivery，绑定原请求、目标端点实例及截止；设备保存原领域决定和待交回 Reply。Orchestrator 保存 Reply 及后续业务查询责任后才返回 ReplyAck。ACK 丢失时设备交回同一 Reply；它不因此重做动作。

Delivery 可以承载 command、query 或 receipt_lookup。原投递的回复一旦固定，需要新的查询就建立新投递，内含业务身份保持。投递截止、命令首次接纳截止和实际操作 deadline 分别检查。

缓存结果重传时若当前已不允许披露，保留原业务事实，关闭该投递的内容交付并返回严格 withheld 分支；恢复权限后需要新投递进行当前查询。不能把 withheld 当作原命令业务拒绝。

控制快照和 Delivery 使用预登记发送者密钥、精确接收方绑定及固定签名配置。签名证明来源与内容，没有授予新的 Grant，也没有替代当前 TaskGate 和时间检查。编码向量通过仍不证明真实密钥轮换或撤回传播已运行。

## 7. 大内容与集合恢复保留原 owner

大字节先预留上传空间，以原 upload_id 整份重传、核对长度与摘要、持久化后标 ready，再由 content.put 发布准确内容引用。ready 字节在生产不能只依赖接收进程临时磁盘。下载每次核验当前许可，客户端验证实际字节；默认不提供 Range 或分块恢复。

无入站设备可依原 copy 登记与 MirrorTicket 主动上传镜像，接收端不能将镜像发布成自己的新 Content。每次读取仍问原 owner 当前资格；来源关闭先阻断新读，再逐副本清理，已交付字节无法召回。

集合恢复先订阅并缓冲提示，再分页枚举，再核对提示中的已知与未知对象。Task、Operation、Memory、Surface、Activation、Grant 各有原 owner 的列表与读取合同；游标、快照边界和 Change 水位不是同一种值。

当前权限改变、分页截断、提示溢出或来源失联均留下缺口，恢复受轮次、页数与时间预算约束。有限集合遍历完不等于跨 owner 全局快照，也不授予以后继续披露旧内容的权利。

帧字段、确切限额和全部恢复分支继续查[WSS 传输合同](../architecture/.draft/contracts/transport.md)与[gRPC 合同](../architecture/.draft/contracts/grpc.md)。这些是实现要求；静态序列不能替代真实 SDK、认证、数据库和异构互操作证据。
