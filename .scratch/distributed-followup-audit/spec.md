# 分布式方案补充审查

Status: resolved

后续：用户已接受本报告六项建议；方案与契约修订记录见[落实记录](implementation.md)。下文保留首次审查时的发现及证据，不将当时的行号作为修订后定位。

审查日期：2026-09-27。基线：`22be10c`（生产分布式架构）。本轮交付为文档与契约审查，未修订方案正文，未运行服务、压测或故障注入。下面的优先级表示设计补齐顺序，不表示已经观察到运行故障。

## 结论与范围

需要补充六项。其中三项关系到恢复正确性：同一工作槽合并新责任后的完成条件、Memory 变更的可靠提交水位、集合订阅缺口后的完整枚举。另三项需要把已有原则落到生产可验证的条件：租约时钟、gRPC 负载分配、可用性统计口径。

当前 Go、WSS、gRPC、单地域三可用区、稳定 Home／owner、托管 PostgreSQL 与对象存储的主线可以保留。这些发现均不要求增加 Redis、消息队列、协调集群或服务网格。实际吞吐和可用性仍需按已有故障模型验收，不能从设计审查推导已经达标。

| 编号 | 优先级／性质 | 当前未闭合处 | 最小补充及落点 |
| --- | --- | --- | --- |
| D1 | P1／并发恢复 | 旧工作者完成或退避可能覆盖同槽后来加入的工作 | 完成事务同时核对当前责任版本；任务运行时及共用工作领取规则 |
| D2 | P1／数据一致性 | Memory 水位没有定义可安全跳过的已提交前缀 | 明确 owner 内事务序列及水位取得条件；Memory 索引、视图 |
| D3 | P1／恢复契约 | operation／activation／grant 集合订阅缺少完整枚举入口 | 补齐类型到受权分页快照方法的映射及缺失契约；传输与领域查询 |
| D4 | P2／生产成立条件 | 租约截止对主机暂停、时钟跳变和数据库切换的要求不够具体 | 明确 ClockAdapter 平台边界、可信误差和停用条件；部署与租约 |
| D5 | P2／容量可用性 | 健康地址、HTTP/2 池和选副本策略未闭合 | 固定 resolver、ClientConn、建流均衡与容量拒绝机制；gRPC 与生产部署 |
| D6 | P2／验收定义 | command_id 去重不能覆盖 Query，迟到成功的计量不明确 | 分开统计调用可用性和业务接纳，固定截止；生产 SLO 与观测 |

P1 应在相应契约或存储实现冻结前解决；P2 应在生产部署和验收前落实。D4 不代表当前完全没有时钟保护，D5 也不代表完全没有流量限制。

## D1：工作槽的完成条件需要覆盖后来合并的责任

**依据。** [任务实现第 63 行](/Volumes/Data/proj/lerna-docs/docs/architecture/task-runtime/implementation.md:63)定义唯一活动 job 与领取代次；[第 72 行](/Volumes/Data/proj/lerna-docs/docs/architecture/task-runtime/implementation.md:72)允许新事实提前同一责任槽的 due_at；[第 272 行](/Volumes/Data/proj/lerna-docs/docs/architecture/task-runtime/implementation.md:272)要求写入时核对 lease_epoch。后者能拒绝已被新工作者接管的旧领取，尚未说明同一次领取期间新责任加入后，旧处理能否结束整个槽。

**故障轨迹。** 工作者 W 领取某执行端的控制槽，处理控制修订 r。新事务提交 r+1，将相同槽提前。没有发生重新领取，lease_epoch 未改变。W 的 r 结果随后到达，若只核对代次便写 done，或者用较晚退避时间覆盖 due_at，就会错误地结束或推迟 r+1 的责任。当前缺槽恢复扫描可以修复部分情况，但没有证明这条路径符合控制传播时限；仍处于 waiting 的槽也不一定属于“缺槽”。

**建议。** 结束或退避 job 的事务按既有锁顺序锁定原业务对象和工作槽，同时比较本次已处理版本与当前待处理版本。只在当前责任已覆盖时结束；否则保留 ready 或更早的 due_at。可以复用单调业务修订；没有单一业务修订可表达的槽再增加 desired／processed generation。领取代次与责任版本分开判断。固定 dispatch 槽仍只承载原命令，不能借合并更换操作身份。

新增工作与完成事务的两种先后都应成立：新工作先提交时，旧完成不能清除它；旧完成先提交时，新工作必须用相同责任键重新激活或创建唯一活动槽。不能只修其中一种顺序。

**代价与验证。** 工作者和存储适配层承担一次版本比较及必要的行锁／字段成本。至少覆盖“新责任→旧完成”“旧完成→新责任”“新责任→旧退避”、领取接管和通知全部丢失。这里不需要引入可靠消息队列。

## D2：Memory 的变化序列需要定义提交水位

**依据。** [Memory 表约束](/Volumes/Data/proj/lerna-docs/docs/architecture/memory/implementation.md:123)规定 index_checkpoint 单调、memory_changes 与业务修改同事务；[索引追赶](/Volumes/Data/proj/lerna-docs/docs/architecture/memory/implementation.md:323)按连续水位 I 与权威切点 R 补扫 `(I,R]`；[视图快照](/Volumes/Data/proj/lerna-docs/docs/architecture/memory/implementation.md:525)完成初始集合后沿 change_sequence 拉取。文档没有明确 sequence 如何分配、R 如何取得，以及事务回滚形成的空洞如何解释。每条 Memory 的 revision 与 owner 级 change_sequence 也需要明确区分。

**反例。** 若实现者把普通数据库序列的可见最大值当作 R：事务 A 分配 101 后暂停，B 分配 102 并提交；索引看到 B 后前进到 102；A 随后提交的 101 将落在补扫区间之外。若“连续”被实现为必须等齐每个整数，A 回滚后留下的空洞又可能让水位永久停住。这是尚未排除的错误实现路径，不是声称仓库已经有这样的数据库代码。PostgreSQL 的序列值分配不会随事务回滚撤销，不能直接提供上述水位保证。[PostgreSQL 事务隔离文档](https://www.postgresql.org/docs/18/transaction-iso.html)

**建议。** 优先使用 owner 内的事务 head：变更事务取得该 head 的锁，推进计数并与 Memory 变更、change 记录共同提交；回滚同时回滚 head。明确 head 与业务对象的统一锁顺序，读取切点与视图集合使用一致的数据观察规则。R 只代表该范围已提交且不会再补入更小编号的变化。索引的覆盖编号沿用相同范围，不能混用对象 revision。修改、删除及影响索引／视图可见性的本域变化须走同一路径。

若实测 owner 级写热点无法接受，可改为提交后串行发布，但必须同时解决尚未发布的业务变更如何参与权威补扫和快照，不能只复制一个异步发布器。当前 WSS 提示已在[生产文档第 134 行](/Volumes/Data/proj/lerna-docs/docs/architecture/deployment-production.md:134)定义提交后发布顺序；它不自动等于 Memory 的权威增量日志，也不能代替该条件。

**代价与验证。** Memory 写入承担 owner 内短暂串行的成本，热点 owner 需单独测量。至少验证先分配后提交的交错、回滚空洞、索引中途退出，以及快照期间新增／删除。无需跨 owner 全局排序或跨库原子快照。

## D3：集合订阅必须有能够完成的快照恢复入口

**依据。** [订阅输入](/Volumes/Data/proj/lerna-docs/docs/architecture/contracts/transport.md:203)按 task／operation／memory／surface／activation／grant 类型选择集合；[快照要求](/Volumes/Data/proj/lerna-docs/docs/architecture/contracts/transport.md:205)要求首次和缺口后重读完整快照；[内部重绑](/Volumes/Data/proj/lerna-docs/docs/architecture/contracts/transport.md:209)也要求重新快照。现有 `execution.get`、`extensions.read`、`grant.read` 均先要求已知对象 ID。方法登记中没有后三类的集合枚举入口。

**故障轨迹。** 客户端订阅 grant 集合，另一获准入口创建 G；客户端在内部重绑后重新订阅，新水位已晚于 G 的创建。G 此后不再变化，而客户端不知道 G 的 ID，无法构造 grant.read，也就无法完成承诺的快照。首次使用新客户端同样存在该问题。扩大日志窗口不能解决首次枚举。

已反查间接路径：Task 的 open_effects 只覆盖仍未知或可能生效的操作，不等于完整操作历史；它不能枚举独立未使用的 Grant。Extensions 内部 active_binding 和原命令回执也不构成当前调用方的完整对象目录。详见[传输独立审查](/Volumes/Data/proj/lerna-docs/.scratch/distributed-followup-audit/transport-findings.md)。

**建议。** 保持已定义的按类型订阅能力，集中定义六类对象的受权分页快照映射，复用现有 task、memory、surface 列表，并补齐缺失方法、Schema、方法登记和契约向量。规定集合边界、快照水位、分页截止、上限、权限缩减及部分结果的处理。分页／缓冲超限必须明确恢复未完成，不能把 partial 当作完整快照；权限扩大后的新可见集合也不能仅依赖对象自身恰好再次变化。

竞争选择是将后三类订阅限制为有限已知 ID；这会缩小现有能力，只有主动改变产品边界时才采用。推荐补齐枚举，不在此次审查中默改契约，也不增加独立全局对象目录。

**代价与验证。** 各领域查询接口、SDK 和授权分页承担实现及快照流量成本。至少验证另一客户端创建对象后首次订阅、窗口过期、内部重绑、分页中删除／撤权，以及超限明确缺口。各 owner 可独立取得快照，不新增跨 owner 一致快照承诺。

## D4：租约时钟需要可验证的平台前提

**依据与已有保护。** [Clock port](/Volumes/Data/proj/lerna-docs/docs/architecture/deployment.md:162)已要求 UTC、单调时间和可信界限，界限不足时停止依赖远端窗口的新启动。[生产租约](/Volumes/Data/proj/lerna-docs/docs/architecture/deployment-production.md:104)又规定数据库续约、本地保守截止、迟到答复不能续活、暂停恢复先检查截止。缺口是允许的暂停方式、时钟误差与安全余量尚未具体关联。

**风险轨迹。** 主机或 VM 暂停期间若单调时钟不推进，旧网关恢复时可能仍认为 15 秒租约有效，数据库却已经回收额度。仅“检查单调截止”不能证明其发送资格。Go 官方说明部分系统休眠期间单调时钟会停止，普通进程暂停试验不能代替主机休眠验收。[Go time 文档](https://pkg.go.dev/time#hdr-Monotonic_Clocks) 数据库提升前后 UTC 差异也可能改变 expires_at 的判断；扣除余量必须覆盖所允许的误差，不能只写一个无依据的常量。

**建议。** 明确支持平台的经过时间能力、主机恢复检测、UTC／数据库最大误差及跳变处理。恢复或界限不可证明时，先阻止发送和新工作，再核验原资格；不能先发送旧队列。安全余量从允许误差和暂停模型导出，不跨进程持久化本地单调读数。沿用现有 ClockAdapter 与平台能力，无须引入新的时钟服务。

**代价与验证。** 平台与时钟适配层承担支持范围、监测和恢复门禁，保守停止会损失短期可用性。分别覆盖进程暂停、主机／VM 暂停、UTC 前后跳和数据库提升时差，检查最后一次旧身份发送与额度回收，不只看续约线程最终是否报错。

## D5：gRPC 扩容需要明确新流怎样到达新增副本

**依据。** [gRPC 服务边界](/Volumes/Data/proj/lerna-docs/docs/architecture/contracts/grpc.md:20)要求解析健康副本，[第 27 行](/Volumes/Data/proj/lerna-docs/docs/architecture/contracts/grpc.md:27)要求有界 HTTP/2 池；[生产第 144 行](/Volumes/Data/proj/lerna-docs/docs/architecture/deployment-production.md:144)已有连接与实例流数预算，但未规定地址更新、ClientConn 的分配范围、负载策略及流满后的选择。

**风险轨迹。** 网关原先向 A 建立长期 HTTP/2 连接；扩容 B／C 后，新 EndpointChannel 仍向 A 建流，A 满载而 B／C 空闲。默认 pick_first 或仅解析到一个 L4 VIP 都不能单凭增加应用副本保证分担新 RPC。这是从尚未固定的发现与均衡策略推导的风险。gRPC 默认策略为 pick_first，其他策略须明确配置；round_robin 选择新 RPC 的目标，不会迁移已经建立的长流。[gRPC Service Config](https://grpc.io/docs/guides/service-config/) [gRPC 负载均衡机制](https://github.com/grpc/grpc/blob/master/doc/load-balancing.md)

**建议。** 默认复用平台可更新的实际后端地址集合，固定 ClientConn 生命周期与配置。短 Call 明确均衡策略；EndpointChannel 的新建／重绑使用后端容量约束，容量不足有界换候选或拒绝，已有流通过既有轮换／排空逐步移动。若组织已有可验证的 L7 gRPC 代理，可由它承担相同职责，但应明确责任与容量；没有现成能力时不为此单独增加服务网格。

**代价与验证。** 网关或已有代理承担发现、连接数、活动流预算及有界重分配成本。验收必须包括“旧 HTTP/2 保持时扩容”以及部分后端满载、失效、排空；观察新增容量承接的新流比例与重绑对 WSS 的影响。仅测试所有连接断开后的重建不充分。

## D6：调用可用性与业务接纳需要分开计量

**依据。** [生产 SLO](/Volumes/Data/proj/lerna-docs/docs/architecture/deployment-production.md:230)将逻辑请求统一按 command_id 去重，同时把权威查询列为独立可用性目标。普通 Query 没有 command_id；receipt_lookup 携带的是被查命令身份，不能代表每次查询。request_id 又只属于当前外连接。

**歧义轨迹。** 一次 task.read 超时，重连后另一次读取成功。按 request_id 是两次调用，按“最终成功”可能被算成一次成功，按 command_id 则无键可用。同一命令被多次查回执也不该合成一次可用性观察。命令落账但初次调用超时、稍后查到成功时，账本成功和接口及时可用是两件事。

**建议。** 接口可用性与延迟按可观察调用及原 5 秒期限统计；内部重绑不重置期限，客户端发起的重试另计尝试。业务接纳另按 command_id 去重。后来取得回执不能追改初次调用超时。普通 Query 和每次 receipt_lookup 按调用统计，不按被查对象合并；保留失败时段指标，避免成功重试稀释故障。若需统计包含自动重试的一次用户操作，使用观测关联及固定总期限，不能给领域查询新增虚构业务命令身份。

**代价与验证。** SDK、网关与观测适配层承担关联和聚合。用相同故障样本核对：成功响应、响应丢失、5 秒超时后原回执查回、查询重连、系统拒绝。请求可用性、账本接纳与最终任务成功分别报告。详见[运维独立审查](/Volumes/Data/proj/lerna-docs/.scratch/distributed-followup-audit/operations-findings.md)。

## 已覆盖的方面与剩余证据

| 已审查主题 | 当前已有具体规则 | 本轮结论 |
| --- | --- | --- |
| 提交未知、重复调用、重复外部效果 | 原 command_id／摘要、持久回执、固定操作、未知结果核对 | 保留现有方案，不再泛列“需要幂等” |
| 连接重建与旧绑定隔离 | connection／binding 分离、单调绑定代次、条件清理、旧输出丢弃 | 已覆盖；D3、D4、D5 分别补集合、时钟与负载条件 |
| 旧主隔离与数据恢复 | 数据库平台选主与 fencing、完整记录验证、按角色恢复、缺历史停写 | 不需默认跨地域双写；跨地域只读灾备是明确边界 |
| 内容跨库发布与清理 | reference_intents、登记 copy、持有者门禁、停止与物理清理分离 | 已定义具体竞争与恢复责任 |
| 过载、重连风暴与故障余量 | 分类／租户预算、控制保留、有限扫描、退避、积压排空条件 | 参数仍待实测，不把缺少实测重复描述为设计遗漏 |
| 配额换版、身份与密钥轮换 | 先降旧份额、当前身份核验、新旧 key 过渡和撤销 | 未发现需改变已确认语义的新问题 |
| 发布、数据库演进及回退 | 分角色排空、expand／backfill／switch／contract、旧格式兼容边界 | 已有方案；具体实现版本矩阵仍是发布验收输入 |
| 存储长期增长与备份 | 最小关闭索引、对象版本清单、WAL／维护预算及保留规则 | 已覆盖设计；增长、恢复与清理的运行证据尚未生成 |

这里的“已覆盖”只表示有可执行方向和异常规则，不能替代 SQL 并发试验、故障注入和容量测量。尤其要核对领取后崩溃是否消耗持久尝试预算、事务失败重试是否保留原 expected_revision，以及恢复期间当前授权是否持续检查；当前已有对应原则，本轮没有把它们升级为已证实缺陷。数据侧的独立推演与排除项见[数据审查](/Volumes/Data/proj/lerna-docs/.scratch/distributed-followup-audit/data-findings.md)。

## 决策依赖与后续落点

| 已确认前提 | 由此前提直接得到的补充 | 需要改变前提时才出现的新决策 |
| --- | --- | --- |
| PostgreSQL 保存事实及唯一工作责任 | D1 的版本比较、D2 的提交水位 | owner 串行热点实测不足后，才评估异步发布等更复杂路径 |
| WSS 按类型订阅，缺口重新快照 | D3 的集合枚举与恢复边界 | 是否削减为仅已知 ID 订阅；当前不推荐 |
| 租约失效不得继续发送，应用可独立扩容 | D4 的可信时间平台约束、D5 的发现与选副本 | 若必须支持不可检测暂停或特定平台代理，另定适配方案 |
| 已有请求期限与生产可用性目标 | D6 的统计对象和截止 | 若承诺重试后的最终成功率，另列指标，不替换接口可用性 |

推荐先修订 D1～D3 的完整规则、字段／方法及异常向量，再将 D4～D6 纳入同一次生产文档收敛。均可维持已有业务边界，不需要新增领域名词或 ADR。本轮没有把建议写成已接受决策。

验证范围：已做文档交叉核对、方法登记反查及官方资料核查；4 份审查记录的 60 个本地链接及定位行号检查通过。未改图示、Schema 或实现，因此没有新增图示渲染或重复运行原有契约套件；上述并发、暂停、扩容和指标场景均为待实现验收项。
