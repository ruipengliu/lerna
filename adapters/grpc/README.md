# gRPC 参考 adapter

实现范围和准确帧见[参考传输合同](../../docs/architecture/protocol/grpc-reference.md)。`New(Config)` 不发网络请求；`Server.Serve(ctx, listener, tlsConfig)` 持有实际 listener/RPC 生命周期。Unary 可直接装配现有 `wss.LocalProcessor` 和平台 `IdentityProvider`。当前源码的 `HarnessService.Call` 与 `EndpointChannel` 均强制 `sdk/go/grpcwire.Codec`，不使用宽松默认 Protobuf 解码。

单次调用验证当前 bearer、实际 TLS1.3 peer、原 logical_service_id/profile 和结果类型。SDK `DialGRPC` 使用已认证 HTTPS Discovery 固定合同和 owner；关闭配置型 gRPC retry/service config，保留原 command 的 SDK journal 和原回执查询。gRPC 库仅在请求未进入应用时可能做透明连接恢复；同一原键仍由领域判重。

EndpointChannel 需要 `Store`、`MethodsDigest`、`ApplicationInstanceID`、固定 `GatewayIdentities` 及 `EndpointAuthority`。后者验证原 endpoint 配对和当前代次、源 owner 的有限 ES256 Delivery 证明，以及原 Reply 的领域输出 Schema和持久接收。只信经过 mTLS verified chain 的注册 SAN；gateway 证书不能代替原 endpoint bearer。未配置 port 时不开放双向流。

绑定、序号、Delivery、可能已发阶段和原 Reply 接收标记保存在本服务 owner 的 `grpc` namespace。网络和 authority I/O 在 Tx 外。更高代次保留原外 connection 的序号；同进程的重绑与实际发送入口共用门禁。发送 loop 是唯一出口，原生 Send 未结束前保留字节与槽。Relay 在原业务 owner 确认并且本机确认提交后才 Ack。

Go `OpenReplyJournal` 在私有 Root 目录以 file fsync、rename 和 directory sync 保存原 Delivery/Reply 与匹配 Ack。重启保留待交回责任；不匹配的 Ack 不清除旧 Reply。当前私有 journal 最多保留 128 个原身份，拒绝超额；需要扩大或退休身份的生产策略尚未验证。

`conformance/grpc` 通过真实 TCP/TLS/mTLS、SQLite、平台 credential ledger、固定注册 ES256 key 与实际文件 journal 验证正反例。包含严格外壳、源 JSON/JCS、原命令 SDK 重连回执、坏凭据/撤权/证书/配对、明文开发例外、普通32槽饱和时控制回执、绑定换代和丢 owner 确认后原 Reply 恢复。`go test -race` 与 `go vet` 通过。

以下不据本轮测试声明支持：WSS gateway 到该 channel 的真实生产接线；多应用实例同库绑定与 gateway 丢弃旧输出的整体验证；完整订阅/Change、动态发现 watch、真实证书发放、PG 传输重启故障和生产容量。跨进程旧物理帧即使与绑定提交交错也必须由 gateway 按外壳 binding 代次丢弃；单进程门禁测试不证明该生产环节。默认未配置内容传输 Processor，不开放 `content_transfer_json` 字节业务。
