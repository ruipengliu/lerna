# 原外连接的静态 Channel 路由

`NewRouter` 只接收受信配置，不出站。配置固定 logical owner、gateway instance、准确 methods digest、最多两台 `grpcs` application，以及原主体／凭据代次对应的 endpoint／instance／generation。每台应用必须显式 TLS 根与 gateway 客户端证书，不开放明文或跳过服务端验证。`Credentials` 取得原已认证主体的受控凭据，mTLS 身份不能替换业务主体。

Router 实现 WSS 的可选 `ConnectionProcessor`。`Open` 使用原外 connection_id 完成内部 Bind／Ready 后才返回；WSS 随后只发送一次外 Ready。每次候选使用新的 binding_id 和更高 revision，核完整内部 Ready 后切流。`Begin` 在外读循环内按原 seq 入队，`Wait` 可并发；一个发送循环按序发送请求，控制有独立预留，重绑不重置高水位。

流失效时关闭该实际流并回收队列，新普通请求返回 `query_original`，不无限积压。已可能发出的 command 只在原等待窗口内向新 application 查询原 receipt；不盲重发 command、另造 ID 或延长原 TTL。Query／receipt lookup 保留原字节和等待截止。连续 60 秒不能重绑则关闭实际外连接。`Close` 取消并等待原网络读写与恢复任务退出；旧流退出后退休，历史候选集合不无限增长。

原绑定在队列接纳和实际外部 socket 写入前核验。外发送门禁与 gateway 切流共用锁，直到实际 Write 结束才释放；旧输出不能在切到新 Ready 后披露。当前凭据仍由平台每次请求／披露核验。内部 binding 的持久 CAS 与序号由原 application 的 `grpc` namespace 保持。

`StaticEndpointAuthority` 接受显式配对、预登记 ES256 key、原准确 ProofReader 和 ReplyReceiver；签名固定原 owner、endpoint／instance／generation、Delivery 全摘要与有限窗口，Reply 再按原 recipient 方法输出合同核验。没有来源证明或原 owner 接收端口时 Delivery 保持关闭。Router 的 `EmitChecked`／`Receive` 可传闭合 Delivery／Reply／Ack；端点仍须持久保存原 Reply 到匹配 Ack，网关内存不代替端点账本。

当前行为证据为真实 WSS／TLS＋gRPC／mTLS、两个独立 Server 实例共享原 SQLite／PostgreSQL，应用退出后原外连接／seq／receipt／领域事实保持。另以同版测试二进制启动两个独立应用参考进程，SIGKILL 原应用发生在原命令提交后、回复前；替代进程在原五秒等待内只查询同一回执，实际 command 入口次数为原进程一次、替代进程零次。记录原数据库／owner／连接与 binding／命令及 TTL／二进制摘要，SIGTERM 后观察替代进程实际退出。测试预置原开发身份、静态 endpoint 配对和 mTLS 信任；SQLite 临时库结束后删除，PG 使用独立原 owner。这些进程调用实际 Dispatcher 与 Channel adapter，但不是 `cmd/application` 公共装配。

默认 Processor 的实际 TLS 丢回复／logout 回归通过。多实例迟到输出、双向 Reply/Ack 故障和 `development` 公共入口接线仍待后续片；不据接口存在声明这些验收已完成。
