# 准确操作知识与 Agent 配置

本切片保存普通 Skill 的准确正文、使用合同和来源，以及 AgentConfig 的固定 Brain、能力集合和控制上限。它不提供系统信任、Grant、任务成功证据或程序安装资格。

`skill.register/get/load/withdraw/reopen` 使用闭合 typed Schema；登记和重开保存原命令、准确版本与 validation Job，同事务提交。受信 Content 端口在事务外读原 bytes，先后以同库 KnowledgeGate 核当前主体、用途和完整来源；正文与合同均核原长度、摘要、UTF-8 和有界合同。永久版本绑定不允许相同 component/version 换摘要。撤回需要原作者或维护者与准确 CAS；重开重新核原 bytes，不覆盖原版本。

`agent_config.register/get/withdraw/reopen` 同样保存原准确配置与 validation 责任。`knowledge.load` 固定本轮 Task/Snapshot、Brain 和父 InstallLock；Agent Brain 必须匹配实际宿主已选 Brain，不按客户端引用换实现。Agent 能力集合要求唯一，父候选按原 Capability/Binding 索引取交集，保留同能力的不同准确绑定和原顺序。每 Decision 的输入、输出、行动数、委派数、深度、时长及原单位费用上限逐项取最紧值。Task 累计预算与实际 Grant 仍由原 owner 校验。Skill 工具依赖必须在本轮有效候选中，选中冲突拒绝，证据依赖必须在已声明来源中。

正文每 Skill 最多 64 KiB、合同最多 16 KiB、来源最多 8、选中 Skill 最多 8、候选能力最多 32、普通 Packet 最多 128 KiB。Packet 的 `ordinary_knowledge/1` 类型只能进入普通 Material。数据 InstallLock 精确绑定正文、合同、来源和父锁，不声明 executable readiness、自检或隔离证据。加载保存永久原候选，未提交 holder 的结果仅是候选数据，不是派发依据。

宿主以 `StageSelectionTx(ctx,tx,auth,KnowledgeAdmission)` 在原 Task/预算之后同库接纳：严格匹配已保存候选、实际 Packet 与 Snapshot 的原摘要/长度、全部来源与候选能力、固定 Brain/锁和原有限期限，随后核当前知识 head 并创建 Task 与原 Decision 的数据锁 holder。禁止 IO 和 Raise Job；最终 Job 仍归业务 owner。失败回滚所有 selection、语义键、数据锁与 holder；原重放复用准确集合，不刷新数据、版本或期限。`CheckSelectionTx`／公开 `knowledge.selection.get` 按原 SnapshotRef/DecisionID 同库再核当前来源、Skill/Agent head 和原 holder，返回原有效上限供真实 Brain/行动门禁收紧。普通主体与原 credential generation 绑定；受信 service 仍需当前完整 Content 门禁。

数据锁及最小 holder 身份永久保留作为原 Snapshot 依据；撤回关闭后续消费，不删除原记录或取消已发生效果、账单。它们与程序 `installations` 分离，不能用数据 holder 宣称实例就绪或处置已退出。

开发宿主的可选 `Config.Knowledge` 已接入真实 Task Context：普通 Packet、完整来源、有效 Capability/Binding、固定 Brain 与复合数据锁进入实际 Snapshot。Context 按完整编码 bytes、输出 reserve 和真实模型 cost bound 拒绝超限输入，再由 Task 的 `ContextCommitter` 在原预算和 Decision 事务中建立 holder；不合格编码不创建 reservation 或物理请求。Brain 在开始与返回后核当前原 selection；退出后沿原 CallID 核已知费用，不因 Skill 撤回重发。

提案读取在缓存回放和工具准备前核当前 selection 与行动数；真实行动准入继续核能力、费用和最紧时长，由 Task 冻结有限 deadline。程序仍使用原可执行 leaf InstallLock，数据锁不能替换其资格。配置关闭后，原已接纳 Snapshot 仍核其持久 selection；只有准确 `not_found` 兼容旧 Snapshot，数据库或当前权限错误保留原 cause。

公开验证使用真实 SQLite／PostgreSQL、实际 HTTP 合同模型、准确普通 Packet、真实目标文件读回和持久重开：输入／费用上限阻止出站；输出 reserve 为 128；两行动超过一行动上限时未建立工具 Use；未选 Write 在冻结模型合同处被拒；30 秒行动时限由原五分钟 Task 收紧；同一 GUI 能力的两不同绑定保留；原 Skill 撤回后保持唯一 POST 和已知 0.00024 USD 账单。合同模型的费用真值来自测试明确锁定的 tariff 和 final usage，不是 live 供应商结算。

本片按 R4 选择固定小目录直接装载、有界 Skill 和单宿主同库装配。大目录渐进检索是未开放优化，不是该本地切片的前提；跨 owner 知识 holder 交接另列资格。内部/外部委派须消费准确父 selection 的 Agent 能力和控制上限，再与父 Grant 取交集，实际接线由工单17验收，不能由本片的上限元数据宣称通过。实际模型质量、生产规模和平台资格也须独立取证。
