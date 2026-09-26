# 传输、关闭身份与签名向量

[传输契约](../../transport.md) · [领域序列](../protocol/README.md) · [交付审查](../../../review.md)

本目录提供 92 个构造的有限数据向量，不启动网络或领域服务。配置为未发布的 `harness-wss-draft-3`，领域配置仍是 `full-harness-draft-2`。`vectors.json` 覆盖发现、Ready、内部重绑、双向帧关联、三类投递／回复、订阅与快照缺口、撤权后缓存披露、五类上传／镜像管理关联、上传发布、反向镜像及关闭记录；`invalid-mutations.json` 的 207 项变更必须命中各自指定的拒绝规则。内容引用为构造数据，上传关联通过不证明实际字节存在。

`proofs.json` 只保存公开测试公钥、两份签名及预期绑定：Home 控制与 Delivery。测试密钥临时生成后已丢弃私钥，与任何真实用户或服务无关。检查器验证 ES256、固定 kid／用途、完整载荷绑定与规范化；它不提供生产密钥托管、认证或轮换实现。

从仓库根目录运行：

```sh
python3 -B docs/architecture/validation/validate_transport.py
```

依赖 Python 的现行校验依赖和 Node.js；Node 内建密码学只用于测试向量。领域结构通过本地 Schema 解析，不从网络下载引用。严格 JSON 解码拒绝重复键；JCS向量覆盖嵌套对象、UTF-16排序、数值编码和无效输入。静态字段、字节／签名算法和实际服务互操作分别报告。

ChannelBinding 是内部 metadata 解码后的投影，ChannelFrameRecord 是 Protobuf ChannelFrame 解码后的投影；二者不是新的端云帧。其向量核对固定外连接、新候选的 binding_id 与递增 binding_revision、同候选幂等重试、迟到低代与同代冲突拒绝、原 limits 摘要、旧绑定输出隔离、内部 Ready 不二次外发，以及重绑不增外连接槽、不重复或清零请求数。

content.get 的 bytes／control 回复分别与原查询模式关联，WSS response 和 Delivery Reply 都不能交换两种结果；控制状态不能充当镜像读取许可。

Frame 用例的 context 显式提供当前认证身份、原连接与请求、订阅状态、排队字节和在途数量。检查器验证这些构造前提下的方向、关联、方法输入输出、限额及披露分支；它不从网络建立身份，也不自行观察数据库提交、实际队列或心跳。`paused_for_gap` 表示收到缺口帧后暂停的旧订阅，与首次 Subscribed 中要求客户端取快照的 `snapshot_required` 分开。

## 边界与运行续证

| 向量 | 检查内容 | 仍需实际运行 |
| --- | --- | --- |
| Discovery／Ready／Frame | 固定配置与子协议路径、封闭帧类型、当前连接及 request_id、互斥结果／错误、限额关系 | Cookie／Bearer 握手、Origin、双连接配额、真实帧拆装及跨服务互操作 |
| ChannelBinding／ChannelFrameRecord | 原连接／服务／limits 不变、绑定代次原子比较、binding_id 与当前流一致、单次外 Ready、重绑不重复占额 | 业务副本滚动保持真实 WSS、原命令查询、路由租约和迟到清理、委托轮换、5 秒请求期限与 60 秒恢复窗口 |
| 在途、队列及心跳 | 普通请求不能占满控制预留；累计 request_id 有界；每服务及身份总连接上限；ping／pong nonce 关联 | 慢端、双向读写、取消与撤权不饥饿、真实内存上限、集中重连与心跳失效 |
| 订阅与快照 | 准确过滤及 subscription_id、旧订阅暂停、明确缺口、当前披露资格 | 快照与提示并发、客户端刷新、游标过期、合并及缓冲溢出后的恢复 |
| Delivery／Reply | 原请求摘要、类型、命令ID和回复摘要一致 | 设备离线、回复提交后断网、原命令查询及限流 |
| upload_reserve／upload_lookup／Upload | 原 upload_id 与预留元数据不变、当前主体、已发布引用与已核验元数据一致 | 真实字节摘要、同步写入、磁盘满及引用与清理竞争 |
| mirror_reserve／mirror_lookup／mirror_control、MirrorTicket／MirrorRead | 原 ticket_id／control_id、接收方许可与配对端点、当前在线字节读取资格及单调关闭，控制查询不能代替下载定位；上传到期不代替副本保留期限 | 大截图交付、断线重传、原 owner 关闭与上传竞争、物理清理及残留 |
| ClosedIdentityLookup | 内容清理后仍返回gone／冲突，关闭操作不可重开 | 事务清理、永久索引、备份恢复及迟到请求 |
| 控制／投递证明 | 公开测试向量签名及精确绑定 | 真实凭据来源、密钥保护／撤销、时钟及当前领域权限 |
