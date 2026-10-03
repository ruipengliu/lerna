# 03 task-accounting-collaboration

Status: partial
Blocked by: 跨 owner 权威与交接、外部 Agent、独立设备和生产部署的后续装配及验收

依据 [实施规格](../spec.md) 与根 AGENTS.md，保留准确身份、负责方、事务、门禁和恢复。实际编译、公开接口行为、正反例及所需平台证据均通过后才关闭。

## Comments

本方 Orchestrator、Accounting、内部协作和 ChildHandle 参考合同已实现，完整架构仍为 partial。实现范围与端口合同见 [Task 实现说明](../../../docs/architecture/orchestrator/implementation-notes.md) 和 [本地协作 adapter](../../../adapters/collaboration/README.md)；总体能力、故障及外部前提以[实施覆盖](../../../docs/architecture/engineering/implementation-coverage.md)为准。

| 已实现责任 | 实际行为与门禁 |
| --- | --- |
| 目标、条件、输入、完成 | 冻结准确目标原文；条件候选按语义 upsert，保留未提及的硬条件；输入按原请求、Schema、goal/control 一次消费。完整当前覆盖、必要检查、证据 gate 和完整关系共同裁决；空条件、旧版本、负检查和未知效果不能产生成功 Result |
| Snapshot、Decision、行动 | 固定 Snapshot、派发意图、原命令和预留；至多四个独立行动整批准入。GrantUse 与原 IntentHash、ActionConsumption、预算和 Job 同事务；冻结提交者代次与角色，当前门禁核撤权。无进展保存有限等待，确认回滚的 stale Context 沿原 Job 重调度，提交未知保留原身份 |
| Accounting、Allocation | 按准确源修订/摘要归并累计差额、原预留、迟到费用和独立退款。内部分配唯一接收、关闭先到永久门禁、签名原 closure 和迟到父预算差额；纯账务未结不等于目标或效果未关闭 |
| 控制、Result、恢复 | 当前 Task/祖先门禁、根到叶及源记录锁序、最多五秒签名控制窗口。输入先准备，再核当前门禁签首窗；原 invoke 重放不刷新窗口、命令或预留。终态 Result 不可变，Content 导出有独立 Job，治理缺陷沿原 Result 保存 notice |
| 内部协作、ChildHandle、受信装配 | 显式同库创建子 Task/额度并核完整有界子树效果；ChildHandle 先准备原 Session 命令，新目标 CAS 需旧目标和效果关闭。实际本地 Session/steer/answer 转交仅据原消费方回执归并。批请求视图先锁全部 Task 再锁准确请求；ContextFacts、冻结 Decision/Snapshot/上界及原意图等 typed 用例供宿主装配，不是新增公开线方法 |

公开方法以闭合 `api.Contract[I,O]` 登记；16 类 Task Job 均有实际 handler。跨模块仅使用消费方小端口；同库参与者由宿主显式声明，外部准备、字节读取、发送和取证在 Tx 外。缺少实际端口、签名或当前资格时关闭相应入口或保持准确等待，不以目标正文冒充证明。

## 已运行证据

- `3704590` 锁序修复合入后，完整 `go test ./internal/task ./adapters/collaboration -count=1` PASS 85.517s。Task 默认套件使用持久 SQLite；配置真实 PostgreSQL 后，指定的生命周期/证据/累计账务、反序批请求、账务与决策竞争合同也实际运行，不代表每个用例都运行了两个数据库。受影响 PG、ChildHandle、本地协作与检查范围 race PASS 46.369s。两条真实 PG 反序锁曾 RED SQLSTATE 40P01，修复后按原身份正确提交或明确拒绝 stale Snapshot。
- `3a2405f` 执行准备切片：真实 SQLite 的 5.2 秒准备先 RED（耗尽原五秒窗口），移动准备阶段后 GREEN；准备期间取消、实际 DevIdentity 撤权阻止派发；真实 Memory/ObjectStore 出版后丢准备回执，恢复同一 ContentRef 和预留；原 invoke 丢回执并跨窗口期限后保持准确旧命令/窗口。该范围及 legacy 端口、原凭据/旧 Claim 共八项 race PASS 84.568s；Task/Collaboration/development vet、宿主装配编译与文档检查通过。测试入口见 [执行准备](../../../internal/task/dispatch_preparation_test.go)、[持久恢复](../../../internal/task/persistence_test.go)、[检查流水](../../../internal/task/check_pipeline_test.go) 和 [本地协作](../../../internal/task/collaboration_adapter_test.go)。
- 宿主集成另取得真实文件写入、独立读回、检查、不可变 Result、Content 导出及 DB 重开的 [报告闭环](../../../adapters/development/app_test.go)：SQLite PASS 20.63s、PG PASS 48.05s，总包 68.697s。制品 `/workspace/harness-dev-environment/report-assembly-verification.json` 固定实现 `7232c316`、Task 准备 `3a2405f`、准确 profile/Schema 和运行前提。宿主随后补充 [独立 Application/Gateway/Worker](../../../cmd/internal/runner/services_test.go) PG PASS 75.821s，以及 fresh Web 原 Result 与准确全文呈现通过；这两项是后续集成证据，旧报告制品的 `not_verified` 仍把三角色列为失败/诊断中，尚待宿主同步后续结果，不能当作已更新。

有界条件检查使用准确、预批准规则；默认报告闭环是明确正文/保存路径/读回要求的受信结构化目标，执行及许可费用明确为零。上述行为证明本方 Task 消费、账务和恢复，不证明自然语言规划质量、真实付费供应商账单或所有外部工具。可配置模型的实际 HTTP 协议/非零费用测试属于宿主与供应商合同的另项证据。

## 剩余边界

跨 owner Delegation/Allocation、远端 Evidence/Grant authority、外部 Agent、独立设备有限授权和断网端云闭环尚未完成装配验收；当前本地协作 adapter 对未配置对端在新增责任前返回 unsupported。云端 Task 权威没有移交设备缓存，本机同 owner/同库与三角色 PG 进程的通过不能证明跨主机或独立设备部署。

真实公司身份/密钥基础设施、任意目标的语义条件与检查质量、真实供应商迟到账单/退款、三 AZ、时间异常、生产容量/历史增长及灾备目标继续按实施覆盖保留 blocked/partial。完整浏览器故障矩阵由集成工单继续记录，原 Result/全文单项通过不关闭其全部范围。本工单没有将完整架构标记 resolved。
