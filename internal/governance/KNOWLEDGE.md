# 准确操作知识与 Agent 配置

本切片保存普通 Skill 的准确正文、使用合同和来源，以及 AgentConfig 的固定 Brain、能力集合和控制上限。它不提供系统信任、Grant、任务成功证据或程序安装资格。

`skill.register/get/load/withdraw/reopen` 使用闭合 typed Schema；登记和重开保存原命令、准确版本与 validation Job，同事务提交。受信 Content 端口在事务外读原 bytes，先后以同库 KnowledgeGate 核当前主体、用途和完整来源；正文与合同均核原长度、摘要、UTF-8 和有界合同。永久版本绑定不允许相同 component/version 换摘要。撤回需要原作者或维护者与准确 CAS；重开重新核原 bytes，不覆盖原版本。

`agent_config.register/get` 同样保存原准确配置与 validation 责任。`knowledge.load` 固定本轮 Task/Snapshot、Brain 和父 InstallLock；Agent Brain 必须匹配实际宿主已选 Brain，不按客户端引用换实现。能力按准确版本取交集；每 Decision 的输入、输出、行动数、委派数、深度、时长及原单位费用上限逐项取最紧值。Task 累计预算与实际 Grant 仍由原 owner 校验。Skill 工具依赖必须在本轮有效候选中，选中冲突拒绝，证据依赖必须在已声明来源中。

正文每 Skill 最多 64 KiB、合同最多 16 KiB、来源最多 8、选中 Skill 最多 8、候选能力最多 32、普通 Packet 最多 128 KiB。Packet 的 `ordinary_knowledge/1` 类型只能进入普通 Material。数据 InstallLock 精确绑定正文、合同、来源和父锁，不声明 executable readiness、自检或隔离证据。当前未提交 Task Snapshot holder 的加载结果仅是候选数据，不是派发依据。

本片公共行为使用真实 SQLite 或显式配置的 PostgreSQL、实际文件 bytes、受信预置的当前来源权限 seam；测试不替代真实模型质量、外部 Agent 身份、生产规模或平台资格。
