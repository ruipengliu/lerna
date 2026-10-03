# 20 endpoint-channel-rebinding

Status: in-progress
Blocked by: 01, 02, 07, 08
Implementer: execution_impl (adapter/router); storage_impl (development装配/分类worker)

依据：A3／A4、协议 EndpointChannel 原连接／绑定合同，以及工程方案的分类 worker 和分布式装配。

装配真实 WSS gateway → gRPC EndpointChannel → 两个独立 application；采用明确开发身份、静态发现和受控端点签名密钥。原应用退出后保留外 connection_id、原请求期限及原回执，使用新 binding_id 和递增 revision，丢弃旧实例输出且不重复发送外 Ready。分类 worker 按显式 JobKinds 领取，不取得执行目标目录锁；测试原连接重绑、丢回执恢复和实际进程退出。生产证书、公司发现和跨 AZ 资格另列。

## 完成依据

实现、公开行为、真实负责方和必要故障正反例通过后记录准确提交与制品。尚未完成的本地路径保持 partial，不与外部资格混写。

## Comments

2026-10-03：固定源码复核 a5410e4 确认核心 Channel 已存在，但默认宿主未装配，补入原实施任务图。

2026-10-03：Root 显式委派 execution_impl 实现静态有界 Router／EndpointAuthority、WSS 可选原连接生命周期及真实 TLS 两应用重绑验证；storage_impl 保留 development App/config 与分类 worker 接线。原 connection/endpoint/instance/generation/methods digest 固定，内部 Ready 后切新候选，高水位不重置；已发 command 只在原等待窗口内查询回执，不盲重发。

2026-10-03：第一可编译行为片提供 bounded Router、静态 mTLS／原 bearer 配对、WSS optional Open／ordered Begin／actual disclosure gate。实际 WSS/TLS＋两个 gRPC/mTLS Server 共享 SQLite 的原连接／新 binding／原 receipt／序号不重置／单域事实／实际退出 race 通过 7.253 秒；默认 WSS 实际丢回复／logout race 回归通过 1.750 秒，vet/diff 通过。此片明确是两个独立 Server 实例，分进程／PG／旧输出及 Reply/Ack 故障、development 公开装配未据此宣称完成。

2026-10-03：第二行为片在真实 SQLite／PG 上启动两个独立应用参考子进程，使用同版测试二进制、真实 Dispatcher／Channel 和 TLS／mTLS。原提交后、回复前 SIGKILL，原五秒等待内取得同一 receipt；实际原 command 入口一次、替代进程零次，原 ID／TTL／连接／seq 固定，替代进程 SIGTERM 实际退出。加 PG 两 Server 原连接／seq 恢复回归，三例 race 实际通过 13.609 秒，过程记录 endpoint-channel-process-db-race.log；此前编译失败日志保留，不计通过。公开 cmd/application 装配、旧输出和双向 Reply/Ack 尚待后续片。
