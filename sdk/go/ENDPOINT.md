# Go SDK 显式 WSS 端点

`DialWebSocketEndpointWithHTTP` 是可选双向 consumer。原 `DialWebSocket*` 仍只接收请求响应；没有接收配置时 Delivery 关闭。端点配置固定来源 tenant／issuer、接收 logical owner、endpoint／instance／generation、当前 identity scope／credential revision、profile／core digest 与准确 recipient 方法合同；还需受信 Current、预登记 P-256 公钥、准确 ProofReader、业务 Receiver 和原 ReplyJournal。公钥不从帧或 URL 取得，显式 HTTP TLS 信任不能跳过验证。

收到准确 Delivery 后先核严格帧、recipient 原输入 Schema、ProofRef 所有权／原字节摘要、ES256 原 audience／对象／摘要／generation／窗口及当前身份。`ReplyJournal.PrepareInvocation` 以原 delivery_id 固定输入、合同与本机单调 sequence；`StartInvocation` fsync 后才能调用 Receiver.Invoke。业务 Receiver 仍负责原命令准入、Grant／资源／效果和当前输出披露，SDK 的签名验证不代替这些裁决。

Invoke 的错误表示结果不明，SDK 保留 `started`，不猜成功／失败、不再调用 Invoke。重开或 `RecoverEndpoint` 只对原 `started` 调用 Receiver.Lookup；即使 Lookup 确认没找到，也保留责任，不盲重执行。已确认原 Reply 通过准确结果身份和输出 Schema 后先 fsync 再回交；只有匹配的 `stored=true` Ack 才持久标记结束。旧输入／期限／sequence／receiver／方法摘要不刷新。旧责任缺准确 decoder 时关闭，新 schema 不能替旧责任改写。

既有 ReplyJournal 的已保存 Reply 可收尾，原档案仍保留；新增未完成调用必须具有明确 prepare／started 元数据。独立 `receipt_lookup` Delivery 必须显式配置原接收 owner 的纯 ReceiptDecoder，否则该 kind 关闭。Lookup 自身须沿业务 owner 的原授权读取结果；不能靠控制收尾资格重新披露已撤权正文。

接收普通 28 项、控制 4 项，3 MiB／1 MiB 预留、总 4 MiB；持有到实际 handler 返回。两普通 worker 和一控制 worker 独立推进，读取循环持续接收。网络和 callback 等待各限五秒，Close 取消实际读写并观察 worker 退出；callback 不退出时返回 `endpoint_handlers_not_exited`，不宣告业务已停止。Journal 最多 128 个最小身份，恢复页 32；超过上限明确关闭，不静默丢弃责任。

第一行为片以实际 WSS／TLS→Channel／mTLS、固定 ES256 和原 recipient Dispatcher 证明 handler 前 prepare／started fsync、Reply 在 owner I/O 前 fsync，以及 matching Ack 重开保留。SDK 的旧 unknown 复开／错误 key／scope／窗口与外 Ack 故障验证仍待后续片；平台生产资格不据此宣称完成。
