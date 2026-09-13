# 核实第二持久化后端的事务与恢复边界

Type: research
Interaction: AFK
Labels: wayfinder:research
Status: resolved
Assignee: persistent_backend_research
Blocked by:
Parent: [Harness 技术实现方案决策地图](../map.md)

## Question

是否可采用嵌入式 Go KV 后端 bbolt 作为 SQLite 之外的独立持久化验证实现，在不增加独立服务的前提下落实局部原子提交、版本比较、操作回执、待处理工作、快照与重启恢复？

## Scope

核对官方固定稳定版本、Go/平台要求、读写事务与同步语义、并发/文件锁、备份与迁移及有界使用限制。明确存储原语能提供什么、哪些必须由 Harness Adapter 构建；不替用户承诺生产容量、不实现 Adapter、不改默认 SQLite。

## Output

`docs/research/harness-second-persistent-backend.md`。

## Comments

由能力里程碑对第二个持久化后端实际替换证据的要求产生。独立 research 分支保存报告；根会话继续制定不依赖具体库的能力阶段及专项验收草案。

## Answer

已完成官方资料核对，报告：[第二持久化后端的契约适配边界](../../../docs/research/harness-second-persistent-backend.md)。研究分支 `research/harness-second-persistent-backend`，worktree `/tmp/lerna-wayfinder-persistent-backend`，报告提交 `d230a764716ee1b315422a1e072437d67d9cb079`；已整合至主工作区。

- 固定候选 go.etcd.io/bbolt v1.5.0，上游提交 e7a8b2dd498494a3766ba24dd94d3509e5588485；最低 Go 1.25.0，模块 toolchain 为建议 go1.25.11，不构成调用项目必须固定该补丁。
- 单文件原子写事务和读快照可支持 Store 原语；CAS、回执、工作/Outbox、索引与授权均由 Harness Adapter 落实。单文件仅一个读写持有进程，多进程通过已有接口访问。
- 同步错误可能使提交结果不明，故障实例不能靠缓存回执报告耐久成功；原生事务无 context 强制取消，需有界准入和后续核对。
- 跨请求快照需固定视图，备份 CopyFile 不显式同步目标；迁移、文件/目录耐久化、旧备份隔离及空间限制需另行实现与验证。

报告未安装依赖、运行故障实验或宣称生产性能；具体选型及迁移路线由 [确定能力里程碑与实施依赖](14-implementation-milestones.md) 采纳。
