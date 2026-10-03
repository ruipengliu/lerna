# gRPC 参考 adapter

实现范围和准确帧见[参考传输合同](../../docs/architecture/protocol/grpc-reference.md)。`New(Config)` 不发网络请求；`Server.Serve(ctx, listener, tlsConfig)` 持有实际 listener/RPC 生命周期。Unary 可直接装配现有 `wss.LocalProcessor` 和平台 `IdentityProvider`。当前源码的 `HarnessService.Call` 与 `EndpointChannel` 均强制 `sdk/go/grpcwire.Codec`，不使用宽松默认 Protobuf 解码。

单次调用验证当前 bearer、实际 TLS1.3 peer、原 logical_service_id/profile 和结果类型。SDK `DialGRPC` 使用已认证 HTTPS Discovery 固定合同和 owner；关闭配置型 gRPC retry/service config，保留原 command 的 SDK journal 和原回执查询。gRPC 库仅在请求未进入应用时可能做透明连接恢复；同一原键仍由领域判重。

EndpointChannel 需要 `Store`、`MethodsDigest`、`ApplicationInstanceID`、固定 `GatewayIdentities` 及 `EndpointAuthority`。后者验证原 endpoint 配对和当前代次、源 owner 的有限 ES256 Delivery 证明，以及原 Reply 的领域输出 Schema和持久接收。只信经过 mTLS verified chain 的注册 SAN；gateway 证书不能代替原 endpoint bearer。未配置 port 时不开放双向流。

`StaticEndpointAuthority` 同时实现 `EndpointReplyValidator`：首次 Reply 在 transport ledger 冻结前按原 recipient 输出合同验证，且不写业务账本；SQL Tx 再核准确原 Delivery／registration 与当前 binding。错误首 Reply 不占住原身份。排队恢复的 Delivery 若已取得原 Reply 则跳过发送，合法原 Reply 的先到不会触发盲重发或使 Ack 丢失。

绑定、序号、Delivery、可能已发阶段和原 Reply 接收标记保存在本服务 owner 的 `grpc` namespace。网络和 authority I/O 在 Tx 外。更高代次保留原外 connection 的序号；同进程的重绑与实际发送入口共用门禁。发送 loop 是唯一出口，原生 Send 未结束前保留字节与槽。Relay 在原业务 owner 确认并且本机确认提交后才 Ack。

Go `OpenReplyJournal` 在私有 Root 目录以 file fsync、rename 和 directory sync 保存原 Delivery/Reply 与匹配 Ack。重启保留待交回责任；不匹配的 Ack 不清除旧 Reply。当前私有 journal 最多保留 128 个原身份，拒绝超额；需要扩大或退休身份的生产策略尚未验证。

`conformance/grpc` 通过真实 TCP/TLS/mTLS、SQLite、平台 credential ledger、固定注册 ES256 key 与实际文件 journal 验证正反例。包含严格外壳、源 JSON/JCS、原命令 SDK 重连回执、坏凭据/撤权/证书/配对、明文开发例外、普通32槽饱和时控制回执、绑定换代和丢 owner 确认后原 Reply 恢复。`go test -race` 与 `go vet` 通过。

静态 WSS gateway→Channel、两个应用实例／独立应用参考进程、SQLite／PG、SIGKILL 后原 receipt 恢复、外连接／序号固定、旧输出实际写 gate 丢弃与 signed Delivery／Reply/Ack 故障证据见 `adapters/endpointchannel/README.md`。Go SDK 可选接收端合同见 `sdk/go/ENDPOINT.md`。公开 `cmd/application` 的静态 mTLS 配置现沿 `development/ENDPOINT_CHANNELS.md` 装配；真实 PG 公开 Gateway／两 Application／两分类 worker／独立 SQLite Executor 的原连接、回执与实际退出验证另有记录。公开 App 的 Delivery 仍需原业务 receiver/proof，未配置时关闭。

以下不据参考测试声明支持：生产接线资格、完整订阅/Change、动态发现 watch、真实证书发放、跨 AZ 与生产容量。默认未配置内容传输 Processor，不开放 `content_transfer_json` 字节业务。
