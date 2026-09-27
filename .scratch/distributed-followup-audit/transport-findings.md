# 分布式传输独立审查

审查日期：2026-09-27。只读核对当前工作区的 WSS、gRPC、身份路由、租约、恢复与发布规则；未改架构或契约，未运行服务。以下三项是能够构造故障轨迹的补充点。严重度表示应在生产实现前解决，不表示已观察到线上故障。

## T1：订阅要求的完整快照缺少部分类型的枚举入口

**定位：** [transport.md:203](/Volumes/Data/proj/lerna-docs/docs/architecture/contracts/transport.md:203) 接受 task／operation／memory／surface／activation／grant 六类集合订阅；[transport.md:205](/Volumes/Data/proj/lerna-docs/docs/architecture/contracts/transport.md:205) 要求首次或缺口后读完整快照；[transport.md:209](/Volumes/Data/proj/lerna-docs/docs/architecture/contracts/transport.md:209) 要求每次内部重绑重新快照。现有方法登记的 [execution.get](/Volumes/Data/proj/lerna-docs/docs/architecture/contracts/schemas/methods.json:4232)、[extensions.read](/Volumes/Data/proj/lerna-docs/docs/architecture/contracts/schemas/methods.json:4983)、[grant.read](/Volumes/Data/proj/lerna-docs/docs/architecture/contracts/schemas/methods.json:5519) 分别要求已经知道 operation_id、activation_id、grant_id，没有对应集合枚举方法。SubscribeInput 也没有限定已知对象 ID 的字段（[Schema:1071](/Volumes/Data/proj/lerna-docs/docs/architecture/contracts/schemas/transport.schema.json:1071)）。

**最短轨迹：** 客户端只订阅 grant 集合。另一个当前获准入口创建 Grant G；客户端恰在内部重绑，旧订阅进入 gap。新订阅返回新的水位并要求快照，G 的创建已在新水位之前且此后没有变化。客户端从未取得 G 的 ID，无法构造 grant.read，因而无法完成所要求的快照或补回 G。首次使用该浏览器会话也存在同样问题，无须真的丢通知。

**现有条款不足：** 保存有序提示和明确 gap 已解决“不谎称连续”，但没有解决缺口后的对象集合发现。已反查间接枚举：当前登记仅 task.list、memory.list、interaction.surface_list 三个通用列表；Task 返回字段中没有完整 OperationIntent／grant_refs 集合，open_effects 只覆盖仍未知或可能继续生效的操作（[任务规则:86](/Volumes/Data/proj/lerna-docs/docs/architecture/task-runtime/README.md:86)），不覆盖已结束操作，更不能枚举独立签发但尚未使用的 Grant。Extensions 的 active_binding 是宿主内部指针，公开 read 仍需先知道 activation_id／lock_id；Discovery 只提供服务、方法和配置，没有对象目录。原命令回执只覆盖本调用方知道的命令。周期单对象查询无法查询未知 ID。30 分钟内部流轮换使该缺口成为正常恢复路径。

**最小建议：** 保持已经定义的按类型订阅语义，补齐六类订阅的“集合来源／受权分页快照方法／快照截止／权限改变后的失效处理”映射。现有三个列表能复用的先复用；operation／activation／grant 须补实际可调用的有界枚举合同及 Schema／向量，不能只在正文增加一个未定义的 snapshot RPC。分页集合上限、过期重建以及当前权限检查沿现有列表模式，快照水位仍复用当前订阅机制，不引入全局业务消息日志。竞争方案是把缺少枚举的三类改为显式有限已知 ID 订阅，复用现有 read；这会缩小当前按类型发现的能力，不能悄然采用。

**关键决策：** 推荐方案是补齐现有能力，不需要改变业务主线；只有主动选择缩成已知 ID 订阅时，才需要用户确认能力边界的改变。优先级 P1。

## T2：本地租约截止尚缺跨暂停和主库换时钟的成立条件

**定位：** [production:100](/Volumes/Data/proj/lerna-docs/docs/architecture/deployment-production.md:100) 规定过期 boot 永不复活；[production:104](/Volumes/Data/proj/lerna-docs/docs/architecture/deployment-production.md:104) 用数据库时间续约、从请求发起的本地单调时间计算截止，并要求暂停醒来先查截止；[clock port](/Volumes/Data/proj/lerna-docs/docs/architecture/deployment.md:162) 仅写一般的 UTC／单调时间／可信界限；[PROD-13](/Volumes/Data/proj/lerna-docs/docs/architecture/deployment-production.md:346) 只规定暂停再恢复的刺激。

**最短轨迹：** 网关续到 15 秒租约后，所在主机或 VM 暂停 30 秒，且其单调时钟在这种暂停期间不推进。身份库正常推进并回收旧额度，其他网关占用新槽。旧进程恢复时本地截止仍显示有效，旧写循环在下一次续约失败之前发出队列中的消息。应用发送器也可在领取后、网络发送前发生同样暂停。此处不声称领域幂等会失效，而是“过期进程不能再转发／披露”和连接额度回收的共同前提失效。

**事实依据：** Go 官方明确提示某些系统休眠期间单调时钟停止；`Since`、`Until` 和带单调部分的时间比较因此可能不反映实际经过时间。[Go time 文档](https://go.dev/pkg/time/?m=old#hdr-Monotonic_Clocks) SIGSTOP 或普通调度停顿期间系统时钟继续推进的试验不能替代这种平台暂停试验。另一个应验证的推论是：提升后主库 UTC 明显快于旧主时，按旧数据库时间签发的 expires_at 可比持有者本地截止更早被新主判过期；当前“扣除安全余量”尚未绑定数据库间时差和时钟步进上限。

**最小建议：** 把 ClockAdapter 的生产前提写成可验收约束：支持的平台必须提供覆盖所允许休眠／VM 暂停的经过时间，或在恢复／时钟不连续时先封闭消息发送与新工作，重新核验原租约，不能先处理旧队列；数据库及进程的允许时差、速率误差、跳变检测和安全余量须同一处定义。主库提升后若时间界限不能证明，既有租约按保守规则停止使用并重建。分别注入 SIGSTOP、主机／VM 暂停、UTC 前后跳、主库切换时差，检查旧 boot 发出的最后一条消息和额度回收，而不只检查续约线程最终报错。

**严重度与已有机制的区别：** ClockPort 已要求可信时间界限，现有传输／许可规则也要求不可信时停止依赖远端窗口的新使用；本项不属于缺少租约或时间门禁机制，而是这些已有机制的生产成立条件尚需细化。睡眠／VM 暂停识别、主库换时钟的误差预算及相应故障实验仍须在生产上线前落实。

**关键决策：** 不需要新增业务或容灾决策；是已有 5／15 秒租约成立所需的平台验收边界。若希望允许无法检测且单调时钟停止的暂停环境，则必须另选不依赖该本地截止的门禁机制。优先级 P2。

## T3：健康副本发现与长 gRPC 流的负载分配尚未落到具体机制

**定位：** [grpc.md:20](/Volumes/Data/proj/lerna-docs/docs/architecture/contracts/grpc.md:20) 只规定解析器选择原服务的健康副本；[grpc.md:27](/Volumes/Data/proj/lerna-docs/docs/architecture/contracts/grpc.md:27) 复用有界 HTTP/2 池；[production:60](/Volumes/Data/proj/lerna-docs/docs/architecture/deployment-production.md:60) 描述稳定逻辑服务到健康进程映射；[production:144](/Volumes/Data/proj/lerna-docs/docs/architecture/deployment-production.md:144) 限每物理连接和应用实例流数。尚未规定 resolver 的地址集合／刷新机制、ClientConn 的分配单位、负载策略以及流满后的新选择。

**最短轨迹：** 网关最初只发现应用 A，已有 HTTP/2 连接向 A 建立大量 EndpointChannel。扩容 B／C 后，客户端仍使用原目标和已建立连接；采用默认 pick_first，或只发现一个 L4 VIP，不能保证新增候选绑定分散到 B／C。A 达到流数上限而 B／C 空闲，新外连接和重绑开始失败；继续增加应用副本并未增加网关实际可用容量。滚动排空 A 还会把这部分工作集中变成一次重绑峰值。

**事实依据：** gRPC 的默认策略是 pick_first；round_robin 等策略须通过 service config 明确选择。[gRPC Service Config](https://grpc.io/docs/guides/service-config/) 官方名称解析说明区分标准 DNS 与可随扩缩容更新地址的 watch 型 resolver。[gRPC Name Resolution](https://grpc.io/docs/guides/custom-name-resolution/) 由此可推断，仅说“健康副本”和“限制流数”不足以承诺新副本会承接新流；实际分布取决于尚未指定的解析与池策略。

**最小建议：** 固定一种生产起点：使用平台提供的可更新后端地址集合，统一 ClientConn／service config，短 Call 明确均衡策略；EndpointChannel 建流依据每后端活动流预算选目标，达到上限时选择其他健康后端或明确拒绝，不在单个连接中隐藏无界等待。已有流只在有界排空／正常轮换中移动，不假装 round_robin 能迁移正在运行的流。若组织已有可靠 L7 gRPC 代理，也可由它承接地址发现和按 RPC 分配，但必须明确这项职责并计入跨区故障和容量预算。验收增加“后端扩容且原 HTTP/2 不断开”的新流分布场景。

**关键决策：** 不涉及领域协议变更；在应用直连解析与组织已有 L7 代理之间选择一条默认部署路径即可。推荐复用已有平台能力，没有现成代理时优先明确应用端解析和负载配置，不为本项单独引入服务网格。优先级 P2。

## 已覆盖，未重复列作缺陷

| 主题 | 当前已有具体约束 | 审查结论 |
| --- | --- | --- |
| 旧绑定回写／旧流输出 | binding_revision 原子比较；同流同 boot 才幂等；最高代次存活至外槽结束；当前流对象＋binding_id 过滤 | 已定义关键跨副本裁决，见 grpc:45–70、production:106 |
| 提交未知与重试 | 原 command_id 查回；5 秒总请求期限不随重绑刷新；写 retry／hedging 关闭；ReplyAck 是持久点 | 已覆盖，未把传输成功或流断开解释为业务结果 |
| 身份和网关代理 | 原身份逐消息检查；sender_service_id 不取代理网关；凭据失效关闭，内部委托更新不延长原身份 | 已覆盖，见 grpc:109–122、transport:54–71 |
| 通知丢失与部分依赖失效 | 持久 outbox、批量索引补扫、可丢唤醒、gap、当前资格不足停披露 | 执行机制已定义；T1 仅指出恢复集合的方法缺口 |
| 慢端及并行连接 | 身份分区共享 2／16 连接额度；每连接和总体预算；控制预留；双连接同 Delivery 沿业务身份去重 | 已定义，不需再新增全局消息日志 |
| 滚动与格式回退 | 按角色排空；先兼容扩展、回填、切读写后收缩；不兼容变更维护切换；回退不删除业务历史 | 已覆盖原则与操作顺序，production:309–326；具体版本兼容矩阵属于实现发布证据，当前未据此再提泛化缺陷 |

以上是文档与契约语义审查；官方资料核查不等于本仓库已有 gRPC、时钟或暂停恢复实现。未进行网络、数据库或平台故障测试。
