# 25：端云双向查询与进度

参考 profile 为 `ws-query-v1`。使用两个独立进程、独立节点私钥及 SQLite 状态，经实际 mTLS WebSocket 双向查询同一套能力目录与执行状态。宿主在本地预先受理模拟操作，WebSocket 只查询状态、订阅完整快照；后台工作仍由宿主推进。

## 接入

`wsbinding.Host` 接受 `nodetls.Endpoint`、有限 `Config`、本地主体声明、必需/可选能力、Schema 注册器和受信 `Resolve`。`NewServer(host).Serve(listener)` 提供 `/harness`，`Host.Dial(ctx, "wss://.../harness", expectedNode)` 建立连接。服务端通过 `Server.Accept(ctx)` 取得同样的 `Peer`；两端都可将它传给 `sdk.NewCapabilityClient(peer, namespace)`，调用 List、Search、Describe、GetInvocation。`Peer.Subscribe(ctx, operation)` / `Subscription.Receive(ctx)` 接收完整当前视图。显式 Close 释放连接或订阅，不取消业务任务。

Host 及其注册表在运行期间保持不可变；Resolve 必须使用当前权限和实际对端的完整 `GrantPresentation` 选择本地构造的 Binding，不能接收远端凭据或后端地址。Binding 内的目录服务保持自己的查询授权；执行服务核对固定对端并使用 ReadRemoteInvocation。必须提供 Disclose，检查具体请求的数据来源、目的位置、用途和当前披露许可。它在读取前后及实际发送前运行，大内容的每个分块也检查。回调和领域服务须遵守 context 的有限期限。

参考宿主仅公开合成的公共目录/计数器状态：目录与执行的 `local` 表示数据处理位置，另按对端 `edge` 或 `cloud` 检查 `content.disclose` 和来源策略。这不是把远端伪装成本地调用，也不宣称可直接承载任意个人资料；其他宿主须为其真实数据配置相应披露策略。

## 协商与线协议

HTTP Upgrade 仅接受 TLS、固定路径和 `harness.bootstrap.v1` 子协议；参考端口供节点进程使用，拒绝带 Origin 的浏览器请求。TLS 采用已有节点资格、证书信任及主体代理校验，并在业务消息收发中复核。

Hello/Welcome 在业务主版本之外固定。双方交换新 nonce、版本集合、必需/可选能力及有限限制，选择共同支持集和双方较小上限；缺必需能力则关闭连接。完整 Welcome 必须吻合，握手期间业务消息被拒绝。支持 catalog.read.v1、invocation.read.v1、progress.v1、chunks.v1；chunks 为该 profile 必需。配置只属于当前连接，达到 Lifetime 即关闭；重连从新 bootstrap 开始。

每条二进制 WebSocket 消息为共同 Envelope，现有样例字段编号保持。TaskRef 移到公共 proto 以避免 Envelope 与能力消息循环导入，类型全名及字段编号不变，contract.proto 通过 public import 保留旧消费者的导入方式，并由独立 proto 编译探针验证。五个固定逻辑流分别承载控制、查询、响应、临时快照和应用分块；两个方向各自从序号 1 递增。每流核对连续发送序号，不承诺跨流顺序。快照是当前累计视图；客户端可合并尚未消费的旧视图，不代表完整事件历史。

请求使用 message_id，响应和订阅视图用 reply_to 指回请求或订阅。查询的目标操作在原方法载荷中，Envelope.operation_id 留空。过期/无匹配的只读响应不能满足其他待处理请求；匹配中的错误方法/操作关联会关闭连接；已结束请求的响应按无匹配只读响应处理。动态 JSON 仅存在于 Protobuf DynamicPayload 内，精确校验类型与 Schema 的版本/摘要，禁止运行时远程 Schema 加载；此 profile 不声明扩展业务行为，合法扩展也返回不支持。

## 有限资源与交付边界

参考配置：4 个服务端连接、每连接 4 个待处理请求及每方向 4 个订阅，最大逻辑消息 65536 字节、应用分块 1024 字节，IO/握手/重组期限 2 秒、连接期限 1 分钟、进度轮询 50 毫秒。生产 Config 验证硬上限：32 个连接/窗口、1 MiB 消息，分块不超过消息的一半，IO 至多 5 秒、连接至多 10 分钟。

每连接控制、出站请求、入站待回复请求分别有 Window 容量；订阅发送端保留最新完整视图，消费端每订阅仅存一项。响应在发送循环中读取当前授权状态，不持久积压历史结果。逻辑消息超过分块限制时分为 Envelope 包装的应用块；分块队列是当前发送消息，控制消息可在块之间通过。最多 Window 个重组项、总声明重组容量至多 MaxMessage，单块及累计长度在追加前检查，整项有固定完成期限。RFC 帧重组由库按每条二进制消息的累计长度限制。压缩未启用。

队列满显式返回 UNAVAILABLE 或关闭连接；可靠类别不套用临时进度丢弃规则。各次业务读取、网络读写都有期限，控制优先级不承诺零延迟，正在进行的业务读取或已排入内核的字节仍可能延迟控制消息。心跳间隔为 IO 期限的三分之一。一次 Dial 创建一个 Peer，调用方负责其出站连接数量与生命周期。

未开放 Invoke、Reconcile、取消、任务提交或任意 Store 写入。未宣称持久回执、消息恢复或恰好一次消费；最终状态也作为可重新查询的临时完整快照，不发送 PERSISTED。26 号票继续负责可靠业务变更、持久 Inbox/Outbox、连续确认与补投。已发送的数据无法因后续撤销而收回；撤销阻止后续获准发送。关闭订阅后在途字节可到达，SDK 不再交付给该订阅。

## 验证和依赖

运行 `scripts/verify-ws.sh`。入口固定 Go 1.26.1，生成器要求 protoc 36.1，记录模块校验、生成差异、构建、静态检查和带竞态检测的真实链路/受影响回归。原始报告在 build/ws，持久证据与汇总保存在本目录的 evidence 下。

新依赖固定 github.com/gorilla/websocket v1.5.3。已检查其 ReadLimit、分片、并发读写及 WriteDeadline 实现；现有 x/net/websocket Codec 的逐帧语义不满足本票完整消息分片校验，未另写 RFC 帧实现。来源：[官方仓库](https://github.com/gorilla/websocket)、[固定版本发布记录](https://github.com/gorilla/websocket/releases/tag/v1.5.3)。

用例覆盖真实双进程双向查询/订阅、当前披露撤销、节点失效、版本/能力拒绝、消息关联、非法载荷、分块与帧累计上限、重组超时、慢消费者/队列压力和实际阻塞套接字超时。未把进程内测试冒充网络证据，也未执行完整系统质量或模型评测。

本轮结果：最终 WebSocket 专项与配置合计 13 项顶层测试通过，异步执行包完整竞态回归 36 项通过。扩展 catalogcheck 回归有 31 项基线失败，已在未修改的 4466a68 上逐项复现；验证入口本轮退出码为 1，不宣称全回归通过。完整计数、命令、源摘要及失败对照见 [验证报告](evidence/25-websocket-report.json)。
