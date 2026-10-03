# 准确操作知识与 Agent 配置

本切片保存普通 Skill 的准确正文、使用合同和来源，以及 AgentConfig 的固定 Brain、能力集合和控制上限。它不提供系统信任、Grant、任务成功证据或程序安装资格。

`skill.register/get/load/withdraw/reopen` 使用闭合 typed Schema；登记和重开保存原命令、准确版本与 validation Job，同事务提交。受信 Content 端口在事务外读原 bytes，先后以同库 KnowledgeGate 核当前主体、用途和完整来源；正文与合同均核原长度、摘要、UTF-8 和有界合同。永久版本绑定不允许相同 component/version 换摘要。撤回需要原作者或维护者与准确 CAS；重开重新核原 bytes，不覆盖原版本。

`agent_config.register/get/withdraw/reopen` 同样保存原准确配置与 validation 责任。`knowledge.load` 固定本轮 Task/Snapshot、Brain 和父 InstallLock；Agent Brain 必须匹配实际宿主已选 Brain，不按客户端引用换实现。能力按准确版本取交集；每 Decision 的输入、输出、行动数、委派数、深度、时长及原单位费用上限逐项取最紧值。Task 累计预算与实际 Grant 仍由原 owner 校验。Skill 工具依赖必须在本轮有效候选中，选中冲突拒绝，证据依赖必须在已声明来源中。

正文每 Skill 最多 64 KiB、合同最多 16 KiB、来源最多 8、选中 Skill 最多 8、候选能力最多 32、普通 Packet 最多 128 KiB。Packet 的 `ordinary_knowledge/1` 类型只能进入普通 Material。数据 InstallLock 精确绑定正文、合同、来源和父锁，不声明 executable readiness、自检或隔离证据。加载保存永久原候选，未提交 holder 的结果仅是候选数据，不是派发依据。

宿主以 `StageSelectionTx(ctx,tx,auth,KnowledgeAdmission)` 在原 Task/预算之后同库接纳：严格匹配已保存候选、实际 Packet 与 Snapshot 的原摘要/长度、全部来源与候选能力、固定 Brain/锁和原有限期限，随后核当前知识 head 并创建 Task 与原 Decision 的数据锁 holder。禁止 IO 和 Raise Job；最终 Job 仍归业务 owner。失败回滚所有 selection、语义键、数据锁与 holder；原重放复用准确集合，不刷新数据、版本或期限。`CheckSelectionTx`／公开 `knowledge.selection.get` 按原 SnapshotRef/DecisionID 同库再核当前来源、Skill/Agent head 和原 holder，返回原有效上限供真实 Brain/行动门禁收紧。普通主体与原 credential generation 绑定；受信 service 仍需当前完整 Content 门禁。

数据锁及最小 holder 身份永久保留作为原 Snapshot 依据；撤回关闭后续消费，不删除原记录或取消已发生效果、账单。它们与程序 `installations` 分离，不能用数据 holder 宣称实例就绪或处置已退出。实际开发宿主 Context/Brain 的装配由同版 helper 接入；单独调用元数据接口不证明 Task 已使用该 Skill。

本片公共行为使用真实 SQLite 或显式配置的 PostgreSQL、实际文件 bytes、受信预置的当前来源权限 seam；测试不替代真实模型质量、外部 Agent 身份、生产规模或平台资格。
