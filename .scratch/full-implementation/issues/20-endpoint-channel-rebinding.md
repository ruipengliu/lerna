# 20 endpoint-channel-rebinding

Status: in-progress
Blocked by: 01, 02, 07, 08
Implementer: execution_impl

依据：A3／A4、协议 EndpointChannel 原连接／绑定合同，以及工程方案的分类 worker 和分布式装配。

装配真实 WSS gateway → gRPC EndpointChannel → 两个独立 application；采用明确开发身份、静态发现和受控端点签名密钥。原应用退出后保留外 connection_id、原请求期限及原回执，使用新 binding_id 和递增 revision，丢弃旧实例输出且不重复发送外 Ready。分类 worker 按显式 JobKinds 领取，不取得执行目标目录锁；测试原连接重绑、丢回执恢复和实际进程退出。生产证书、公司发现和跨 AZ 资格另列。

## 完成依据

实现、公开行为、真实负责方和必要故障正反例通过后记录准确提交与制品。尚未完成的本地路径保持 partial，不与外部资格混写。

## Comments

2026-10-03：Root将public App/config/分类worker及实际cmd进程验收完整交给execution_impl；storage_impl确认20没有WIP，继续保留16远端Executor桥和solemerger。Config/App/Run仅20字段、初始化、角色与TLS/Channel精确hunks由execution_impl维护；RemoteExecutors/RuntimeFactory/Foreign/WASI不改。public路径仍待实际正反例，不能据adapter/SDK证据宣称公开装配完成。双向Delivery在public App缺原业务Reply接收者和proof端口时保持关闭，不制造泛用Reply账本。

2026-10-03：固定源码复核 a5410e4 确认核心 Channel 已存在，但默认宿主未装配，补入原实施任务图。

2026-10-03：Root 显式委派 execution_impl 实现静态有界 Router／EndpointAuthority、WSS 可选原连接生命周期及真实 TLS 两应用重绑验证；storage_impl 保留 development App/config 与分类 worker 接线。原 connection/endpoint/instance/generation/methods digest 固定，内部 Ready 后切新候选，高水位不重置；已发 command 只在原等待窗口内查询回执，不盲重发。

2026-10-03：第一可编译行为片提供 bounded Router、静态 mTLS／原 bearer 配对、WSS optional Open／ordered Begin／actual disclosure gate。实际 WSS/TLS＋两个 gRPC/mTLS Server 共享 SQLite 的原连接／新 binding／原 receipt／序号不重置／单域事实／实际退出 race 通过 7.253 秒；默认 WSS 实际丢回复／logout race 回归通过 1.750 秒，vet/diff 通过。此片明确是两个独立 Server 实例，分进程／PG／旧输出及 Reply/Ack 故障、development 公开装配未据此宣称完成。

2026-10-03：第二行为片在真实 SQLite／PG 上启动两个独立应用参考子进程，使用同版测试二进制、真实 Dispatcher／Channel 和 TLS／mTLS。原提交后、回复前 SIGKILL，原五秒等待内取得同一 receipt；实际原 command 入口一次、替代进程零次，原 ID／TTL／连接／seq 固定，替代进程 SIGTERM 实际退出。加 PG 两 Server 原连接／seq 恢复回归，三例 race 实际通过 13.609 秒，过程记录 endpoint-channel-process-db-race.log；此前编译失败日志保留，不计通过。公开 cmd/application 装配、旧输出和双向 Reply/Ack 尚待后续片。

2026-10-03：第三行为片取得真实旧完成输出丢弃与 SQLite／PG 原生 WSS signed Delivery／fsync ReplyJournal reopen／lost Ack／same Reply／伪改 Reply 拒绝证据。Ack 丢失真实 RED 暴露 gateway 删除缓存后拒绝原 Reply；修为只交当前静态配对 application 的原 ledger 复核，gateway 最多 32 项，不造权限或 Ack 墓碑。原始 RED 1.292 秒保留 endpoint-channel-reply-red.log，三例最终 race 通过 11.102 秒，vet/diff 通过，过程 endpoint-channel-disclosure-reply-db-race.log。旧输出测试用公共 PendingResponse 延迟＋实际进程 SIGKILL＋WSS 实际写 gate；外 Ack 故障在 EmitChecked 写前返回失败。SDK 可选端点 consumer／development 公开装配未据此闭合。

2026-10-03：第四可编译行为片新增 Go SDK 显式 EndpointConfig／Receiver／P-256 来源验签和 current／profile／recipient Schema 门禁，扩展原 ReplyJournal 的 prepare／started＋单调 sequence；handler 前与 Reply 出站前分别 fsync，started未知只Lookup。实际 WSS／TLS／mTLS 的首正例 race 通过 2.564 秒，原匹配 Ack 重开保留。默认请求 client 无端点配置仍关闭。SDK 旧 unknown 复开／scope/key/window 反例／SDK 丢 Ack、公开 App 装配仍待后续片。
