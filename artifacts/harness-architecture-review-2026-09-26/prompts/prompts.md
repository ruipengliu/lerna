# Harness 架构评审：逐页 imagegen 提示词集

生成方式：内置 image_gen。以下共同要求与每页专属内容合并为一条提示词，15 页分别生成完整 16:9 位图，再嵌入 PPTX。所引事实以 docs/architecture/ 的 2026-09-27 现行设计为准；详细来源保存在各页讲稿中。

## 共同视觉提示

Use case: productivity-visual. Asset type: one complete, flat, full-bleed 16:9 landscape architecture review slide image for senior engineers. Warm ivory #F5F3EC background, dark navy #102A3A typography, deep teal #087F83 emphasis, sparse ochre #D79C42 for uncertainty. Large crisp Simplified Chinese sans-serif typography, editorial engineering style, fine rules, generous whitespace. Preserve every quoted label verbatim and once. No photo, 3D, gradients, shadows, dashboard-like card grids, watermark, device mockup, or extra text. Put only the two-digit page number at lower right.

## 01 封面

左侧标题“Harness 架构评审”，副标题“生产分布式设计基线”；页脚“九模块协作 · 任务归属 · 故障恢复”和“2026.09.27”。右侧逻辑示意以“Orchestrator”为中心，以无部署含义的细线连接“Brain”“Memory”“Executor”。

## 02 设计目标与约束

标题“设计目标与约束”；将“跨任务记忆、工具与设备操作、跨端持续执行”与“模型提案可能错误、外部效果可能未知、端点可能失联”分成两条横向带。主结论“接纳、许可、效果与完成分别裁决”。底部明确标作“待运行验收的生产目标”：10 万在线用户、500 万任务／日、单区 RPO=0 且控制与查询 RTO≤60 秒。

## 03 九模块与事实归属

标题“九模块与事实归属”，副标题“逻辑职责不等于独立服务”。中央“Orchestrator：目标 · 控制 · 预算 · 完成”，周围分别为交互、Brain 快照提案、Memory 内容与来源、Executor 启动与效果、Agent 协作委派与封账。用无箭头直线表达协作。底部三项支撑职责：权限与隔离、宿主与扩展、观测评测与改进。

## 04 关键决策与承担的代价

三列表格“选择／解决的问题／主要代价”。四行：固定 Orchestrator／同一任务一个裁决者／失联等待；同域短事务／状态与 jobs 同提交／热键和数据库写入压力；跨域原命令恢复／答复丢失可查／关闭索引持续增长；有限离线窗口／界定远端撤权边界／已发动作仍须核对。结论：进程可拆分，业务事实仍按原 owner 裁决。

## 05 生产拓扑与写权威

浏览器、CLI、设备通过 WSS 到网关池；网关通过 gRPC 到 Orchestrator 应用池与工作池；业务工作再交接隔离 Executor。仅在 Orchestrator 分区下方连接“PostgreSQL：分区事实与持久 jobs”；另示“对象存储：准确版本字节”。不得画 Executor 到 Orchestrator PostgreSQL 的连线。主结论“每个稳定分区只有一个数据库写权威”；旧主隔离或记录完整性不足时暂停新接纳与新发送。

## 06 任务推进的六个步骤

六项编号列表，无跨行箭头：接纳目标（Task、回执、首项 job 同提交）；取得上下文（Memory 返回获准版本）；取得提案（Brain 按固定快照提议）；准入行动（意图、预留、派发 job 同提交）；执行取证（Executor 保存 Attempt 与效果）；核验完成（Orchestrator 提交 Result）。强调第一和第四项；结论“决定与继续责任同事务保存”。

## 07 完成判断与独立事实

三列分开表示“接纳回执：业务决定持久保存”“执行效果：目标效果已有证据”“任务完成：必要条件全部满足”，列间不得画因果箭头。下方列出 Orchestrator 完成条件：成果版本准确、任务及委派效果核清、当前目标条件满足。完成依据标为 verified / assessed / user_accepted；暂停可凭既有证据完成，费用未结继续预留。

## 08 写入生效后，答复丢失

原 Operation、Executor 发出写入、目标可能已生效、答复丢失的时间线。故障比较：Executor 已保存效果时，Orchestrator 查询原 command／operation；Executor 未保存目标答复时，Executor 保留 unknown 并按原关联核对。结论“沿原身份恢复，不创建第二次写入”。

## 09 取消与迟到效果

四条相互独立的状态轨道：Task.status 从 active 到 cancelled；执行入口从开放到封闭新发送；原操作效果从 unknown 到 applied 或继续未知；费用从预留到结清或继续预留。结论“取消终态不因迟到结果重开”；已跨发送门禁的动作仍可能生效。

## 10 授权与内容当前资格

行动许可：Orchestrator 准入、Grant owner 消费许可、实际资源入口再核验。信息用途：读取、处理、长期保存、同步与披露分别授权。ContentRef 固定 version + hash，每次使用检查当前资格。删除先禁止新使用，物理清理另行追踪；远端离线许可默认关闭。

## 11 Agent 协作与预算封账

左侧：同 Orchestrator 内父子任务，子任务、预算与首项 job 同事务创建。右侧：跨 Orchestrator 委派，用无箭头编号文字表达父方固定身份与额度、接收方返回接纳及原任务引用、接收方回传关闭证明和最终用量。父方归并效果并结算。未封账额度继续预留；父恢复不解除子自身暂停。

## 12 扩展发布与实例开放

两条发布路径：兼容发布需精确安装锁、共同契约验证、受信批准；改善发布需固定单候选、隔离保留评测、统计与实用改善门禁、受信批准。当前实例另须重新核对批准与依赖，ready 后开放。报告分数不自行授权发布；历史激活不代替本次就绪。不可信插件需要平台隔离证据。

## 13 当前静态证据与运行验收

左侧“现行静态校验已通过”：104 个领域方法、52 组正常序列与 366 个反例、100 个传输向量与 228 个反例、8 组请求序列与 12 个反例。右侧“尚缺：真实服务证据”，突出“Harness 运行验收未执行”；列出原命令恢复、取消与撤权、单区故障、质量容量与过载隔离。结论“静态通过不证明真实事务、网络、鉴权或目标效果”。

## 14 建议评审结论

突出“建议确认设计基线，进入参考实现与故障取证”，明确“设计者建议，待评审确认”。三项需接受的代价：固定 Orchestrator 带来失联等待；未知效果与长期关闭索引带来核对、预留和存储成本；生产分布式首阶段需平台隔离、跨进程恢复与容量证据。生产准入仍以真实运行实验和质量测量为据。

## 15 附录：四项设计收敛

标题“附录：四项设计收敛”，副标题“消除重复定义与派生状态”。四行：输入结构的 Schema 由业务 InputRequest 唯一提供；连接请求在实际发送时分配递增 request_seq；委派 phase 由同修订事实只读投影；OfflineLease 仅有 open／closed／reconciled，用量读原账本。原身份、恢复责任与当前资格保持；协议未发布，不保留旧字段共存路径。
