# 生产分布式方案 Mermaid 审阅

基准提交：`8bfc7af4f39fbc0a2f26b951becef051e5f08bb1`。仅提取相对 HEAD 新增或内容变化的 Mermaid；共 15 图，跳过 37 个未变块。15 个最终源块摘要均与已查看的渲染源一致。

本机 Mermaid CLI 渲染为 PNG 后逐图实际查看。源块 SHA-256、围栏行号、渲染路径及复用来源记录在 [manifest.json](manifest.json)。图编号为对应文件内从 1 开始的 Mermaid 顺序。

| 来源与图号 | 渲染 | 目视结论 |
| --- | --- | --- |
| `docs/architecture/contracts/grpc.md` 图 2（行 49） | [PNG](contracts-grpc-diagram-2.png) | 加入代次后已重新目视：绑定 A/1 与 B/2、内外 ready 分离、原 request_id 恢复及订阅缺口清楚，无裁切或重叠。 |
| `docs/architecture/deployment-production.md` 图 1（行 29） | [PNG](deployment-production-diagram-1.png) | 已目视：网关池和数据库连接池已明确区分；请求/存储访问与 WAL 复制边可辨，无裁切。 |
| `docs/architecture/deployment-production.md` 图 2（行 69） | [PNG](deployment-production-diagram-2.png) | 已目视：外 WSS 保持、内部 Ready 在查原命令前、回原请求和快照缺口方向清楚。 |
| `docs/architecture/deployment-production.md` 图 3（行 112） | [PNG](deployment-production-diagram-3.png) | 修订后已重新目视：保存 Reply 指向原库，提交后 ReplyAck 经网关返回设备；无反向 ACK 歧义。 |
| `docs/architecture/deployment.md` 图 1（行 29） | [PNG](deployment-diagram-1.png) | 标签缩短后已重新目视：生产与两种混合装配区分清楚，无标题换行碰撞或孤字。 |
| `docs/architecture/deployment.md` 图 2（行 170） | [PNG](deployment-diagram-2.png) | 已目视：写资格、恢复、自检、控制及新任务的开放顺序清楚。 |
| `docs/architecture/execution/implementation.md` 图 4（行 313） | [PNG](execution-implementation-diagram-4.png) | 已目视：OS 锁、原账本、未结动作与隔离分支有明确判定；图较长但文字与边可读。 |
| `docs/architecture/memory/implementation.md` 图 4（行 192） | [PNG](memory-implementation-diagram-4.png) | 控制查询修订后已重新目视：持久校准 job 调用 content.get mode=control，本地关闭先提交/发布先提交分支清楚；实际封闭核查后 release，未把查询当停止确认。 |
| `docs/architecture/memory/implementation.md` 图 5（行 256） | [PNG](memory-implementation-diagram-5.png) | 已目视：跨 owner 检查位于事务外，Memory 发布与派生索引推进分开，原命令查询方向清楚。 |
| `docs/architecture/memory/implementation.md` 图 7（行 448） | [PNG](memory-implementation-diagram-7.png) | 字节读取返回类型修订后已目视：ContentBytesGetOutput 两处完整可读；Reply 持久保存后 Ack、metadata 管理与 HTTPS 字节路径区分清楚，无裁切或重叠。 |
| `docs/architecture/storage-and-middleware.md` 图 1（行 10） | [PNG](storage-and-middleware-diagram-1.png) | 已目视：归属、短事务、身份权威与可重建派生数据可辨，无裁切或重叠。 |
| `docs/architecture/storage-and-middleware.md` 图 2（行 40） | [PNG](storage-and-middleware-diagram-2.png) | 分行后已重新目视：注释完整位于黄色框内；通知失败由批扫继续，通知与业务提交分离。 |
| `docs/architecture/storage-and-middleware.md` 图 3（行 71） | [PNG](storage-and-middleware-diagram-3.png) | 已目视：事务池、LISTEN 直连与管理保留连接的路径清楚。 |
| `docs/architecture/storage-and-middleware.md` 图 4（行 88） | [PNG](storage-and-middleware-diagram-4.png) | 分行后已重新目视：发布、孤儿与关闭清理路径可辨，末节点无断字。 |
| `docs/architecture/validation/README.md` 图 1（行 154） | [PNG](validation-README-diagram-1.png) | 主动分行后已重新目视：第一阶段生产闭环与多副本恢复明确，无孤字。 |

发现并修订：生产拓扑区分 WSS 网关池与数据库连接池；投递图拆开 Reply 持久保存和 ReplyAck 返回；装配图、建设阶段图及存储图缩短标签或主动分行。修订后的图分别重渲并复查。后续 gRPC 绑定代次、Memory 控制查询和字节读取返回类型三图也已重渲目视；Memory 图5内容未变，保留先前实际查看记录。

全景图只修改底部部署说明，原生导出及局部目视证据另见 [全景图标签检查](../panorama-label/review.md)。

这项检查验证语法、可读性及图与正文的局部语义一致性，不是服务运行、容量或故障恢复结果。
