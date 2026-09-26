# Go／WSS／gRPC 本轮图示检查

本目录记录 7 张本轮新增或修改的图，图编号按每个源文件中 Mermaid 块出现顺序从 1 计数。模块文档此前已检查的另两张图未重跑；记忆图 6 因元数据调用改动重新检查。

使用本机 Mermaid CLI 与 Chrome 渲染为 PNG，并逐张实际查看；七张图渲染均成功。检查了文字裁切、节点重叠、箭头方向、协议与事实归属。原 Mermaid、PNG 和源码摘要保存在本目录，摘要见 `manifest.json`。

| 源文件 | 图编号 | 当前代码围栏行 | 渲染图 | 源码一致性 |
| --- | --- | --- | --- | --- |
| `docs/architecture/contracts/grpc.md` | 1 | 9 | [contracts-grpc-diagram-1.png](contracts-grpc-diagram-1.png) | 通过 |
| `docs/architecture/deployment-production.md` | 1 | 29 | [deployment-production-diagram-1.png](deployment-production-diagram-1.png) | 通过 |
| `docs/architecture/deployment-production.md` | 2 | 77 | [deployment-production-diagram-2.png](deployment-production-diagram-2.png) | 通过 |
| `docs/architecture/deployment-production.md` | 3 | 118 | [deployment-production-diagram-3.png](deployment-production-diagram-3.png) | 通过 |
| `docs/architecture/deployment.md` | 1 | 29 | [deployment-diagram-1.png](deployment-diagram-1.png) | 通过 |
| `docs/architecture/contracts/transport.md` | 1 | 105 | [contracts-transport-diagram-1.png](contracts-transport-diagram-1.png) | 通过 |
| `docs/architecture/memory/implementation.md` | 6 | 393 | [memory-implementation-diagram-6.png](memory-implementation-diagram-6.png) | 通过 |

七张图布局均可读，未发现文字裁切或节点重叠。部署拓扑的分区边框到外部依赖连线由正文明确解释，三个可用区仍只有一个当前数据库主；gRPC 图区分网络适配与 Go 用例接口；传输图在持久保存 Reply 后才确认 ReplyAck。

首次检查发现 `deployment-production.md` 图 2 在重建通道后漏画新 ready。主线已补充“经接入层返回新 ready”，本次已重渲并实际查看，确认新 ready 位于查询原命令之前，问题关闭。

本次记忆图 6 将原 HTTPS 镜像预留改为 gRPC mirror_reserve，返回原 Mirror 后沿 WSS 主动发送票据；HTTPS 箭头仅剩原始字节 PUT／GET。字段管理没有变成内容所有权或读取资格，图文一致。上传、镜像管理的五类 kind 在正文集中指向传输契约，不增加领域方法。

此记录属于图示渲染和局部语义审查，不是 WSS、gRPC 或故障恢复的运行验收结果。
