# gRPC 参考传输合同

本补充固定 [gRPC 外壳](harness.proto)参考实现的明确方法子集和内部帧；领域 JSON、原命令与 owner 仍遵循[协议正文](README.md)和[共同方法合同](method-contract.md)。传输名字不代表整个 architecture profile 已通过。

## 单次调用

`Call` 接受原 `command/query/receipt_lookup` 严格 JSON；原 authenticated HTTPS Discovery 固定 logical_service_id、profile、core Schema 与准确 methods_digest。`content_transfer_json` 外壳可解码，只有实际装配对应 Processor 才能开放，不冒充原始字节上传支持。

服务器和 Go SDK 都强制严格 codec：未知字段、重复 singular 或 oneof、错误 wire type、非 UTF-8 字符串、非法内层 JSON 与超界 revision 拒绝。传输保留原 JSON 字节，JCS 摘要在领域层按同版规则计算。

真实网络要求 TLS 1.3 与当前 opaque bearer；重复 Authorization 拒绝。凭据通过平台 IdentityProvider 认证，每次入口和披露再查当前代次。mTLS 的 gateway 身份不替代 endpoint 的原业务身份。只有显式开发选项和实际 loopback listener/peer 同时成立，才允许明文。SDK 禁止跳过服务器证书验证，关闭配置型 RPC retry/service config；库透明恢复限于请求未进入应用时，领域仍按原键判重。RPC 等待结束提示沿原责任查询。

单次调用普通槽 32、控制槽 4，RPC 本地等待最多五秒，JSON 256 KiB、外壳 1 MiB。关闭最多等待五秒，然后中断实际连接；不据此宣布领域停止。

## EndpointChannel 闭合帧

参考内部 transport_profile 为 `harness-grpc-endpoint/1`。所有帧均携原 proto 的 `binding_id/binding_revision`；frame_json 只接受下列闭合结构，不能提供 AuthContext。实现必须取得 Gateway 验证过的 mTLS SAN 和独立原 endpoint bearer，再由受信 EndpointAuthority 验原配对/实例/代次。没有该 port 时方法保持 `unsupported`。

| 帧 type | 固定字段 |
| --- | --- |
| bind | logical_service_id, connection_id, gateway_instance_id, endpoint_id, instance_id, endpoint_generation, protocol, profile, transport_profile, methods_digest |
| ready | bind 中的身份及合同、limits；只确认当前内部绑定，不重新发送外 socket Ready |
| request | request_seq, kind, payload；kind 为 command/query/receipt_lookup，payload 是原领域外壳 |
| response | request_seq, result_kind, payload；与本 binding 的在途请求准确关联 |
| delivery | delivery_id, sender_service_id, recipient_endpoint_id, recipient_instance_id, request_digest, kind, request, deliver_before, proof_ref |
| reply | delivery_id, request_digest, result_kind, payload |
| reply_ack | delivery_id, request_digest, stored |
| ping/pong | nonce |

binding 持久保留原外 connection 身份、候选 binding、endpoint 实例/配对代次、gateway 实例和原主体/凭据代次。同代次只接受同候选及实例；更高代次原子换绑定，旧流不能更新新绑定或发出新输出。request_seq 是原外 connection 的高水位，重绑不重置；接纳旧命令后的业务责任不因换流撤销。

Delivery 在传输 ledger 保存原准确身份、摘要与可能已发送阶段。Reply 先固定原 delivery 的准确输入，再交原业务 owner 幂等持久接收；只有 owner 确认与本机完成标记均确认提交，才发送匹配 `stored=true` Ack。提交未知或依赖失联不 Ack，重连仍交原 Reply。Receipt 类型和原 command/query 摘要必须匹配；端点待交回责任保留到 Ack。签名 proof 由预登记 owner key 验有限窗口和准确受众/对象/摘要，不从 frame 下载公钥或 URL。

每流普通发送 32 项/3 MiB，控制预留 4 项/1 MiB，总计 4 MiB；一个实际发送循环分配网络出站。每主体每服务最多两流，单服务最多 16；租户总待发上限 32 MiB，全进程上限 64 MiB。所有在途字节直到实际发送结束才释放；控制无法入队或发送超过五秒即关闭流，责任留在账本。双方持续独立读取，30 秒 ping、90 秒无读超时。新普通请求不在失效流上无限积压。

## 验收与缺口

验收使用 `conformance/grpc` 的真实 TCP、TLS/mTLS、SQLite 和实际平台身份/固定 ES256 key。准确方法集、认证、否定用例、重连原回执、绑定更替和丢 Ack 的责任分别取证。WSS 网关与该流的生产接线、跨进程/多实例原绑定 CAS、后端发现 watch、完整订阅/Change、真实证书部署及规模压力须由各自实际装配验证；参考流不能替代这些证据。
