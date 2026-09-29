# 传输与证明向量

本目录把消息结构、关联顺序和密码学证明分别表达，配合 [WSS](../../transport.md)与 [gRPC](../../grpc.md)查阅。向量中的认证、队列和业务事实均是显式前提；它们不替代真实握手、数据库、字节传输或进程恢复。

| 文件 | 检查目标 |
| --- | --- |
| [vectors.json](vectors.json) | Discovery/Ready、帧方向与类型、内部绑定、投递与回复、订阅缺口、集合请求、缓存披露、上传/镜像和关闭身份 |
| [invalid-mutations.json](invalid-mutations.json) | 在原向量的准确路径改变字段，必须命中指定拒绝规则 |
| [request-sequences.json](request-sequences.json) | 实际出队分配序号、控制插队、错误消费序号、乱序/迟到、内部恢复、跨连接隔离与序号耗尽 |
| [proofs.json](proofs.json) | 公开测试公钥、控制与 Delivery 的 ES256 签名、准确 payload 与用途绑定 |

ChannelBinding 与 ChannelFrameRecord 是 metadata/Protobuf 解码后的验证投影，不增加端云帧。向量检查外连接与限额不变、绑定代次比较、旧输出丢弃、唯一外 Ready、配额和序号不重置。Frame context 明确提供身份、连接、请求、订阅、排队字节和在途数，校验器只推演这些前提。

`paused_for_gap` 表示旧订阅已因缺口暂停，区别于新 Subscribed 需要快照但继续接提示。content.get bytes/control 输出分别与输入模式绑定，控制状态不能代替镜像读取资格。ClosedIdentityLookup 检查正文回收后的 gone/冲突与不可重开身份。

公开测试密钥只用于向量，私钥不作为运行凭据；JCS 向量覆盖嵌套、UTF-16 排序、数字编码和非法输入。签名匹配不能证明真实密钥托管、撤回传播或可信时钟，字段匹配不能证明磁盘同步和物理清理。运行入口、覆盖与验证状态见 [review.md](../../../review.md)，真实故障要求见 [validation](../../../validation/README.md)。
