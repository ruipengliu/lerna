# 20 endpoint-channel-rebinding

Status: claimed
Blocked by: 01, 02, 07, 08
Implementer: execution_impl (App/config/classified workers: storage_impl)

依据：A3／A4、协议 EndpointChannel 原连接／绑定合同，以及工程方案的分类 worker 和分布式装配。

装配真实 WSS gateway → gRPC EndpointChannel → 两个独立 application；采用明确开发身份、静态发现和受控端点签名密钥。原应用退出后保留外 connection_id、原请求期限及原回执，使用新 binding_id 和递增 revision，丢弃旧实例输出且不重复发送外 Ready。分类 worker 按显式 JobKinds 领取，不取得执行目标目录锁；测试原连接重绑、丢回执恢复和实际进程退出。生产证书、公司发现和跨 AZ 资格另列。

## 完成依据

实现、公开行为、真实负责方和必要故障正反例通过后记录准确提交与制品。尚未完成的本地路径保持 partial，不与外部资格混写。

## Comments

2026-10-03：固定源码复核 a5410e4 确认核心 Channel 已存在，但默认宿主未装配，补入原实施任务图。
