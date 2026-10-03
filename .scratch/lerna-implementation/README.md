# Lerna 实现切片与依赖索引

本轮将现有技术方案细化为 **22 份实现规格**，覆盖 A–F。每份规格均已发布到本仓库的本地 Markdown issue tracker，初始标记 `Status: ready-for-agent`，包含用户故事、实现决定、公开测试边界、验收条件及非目标。**规格发布不表示能力已经实现或验收通过；实际状态和证据见[实现进度](progress.md)及各规格。**

依据为 [架构入口](../../docs/architecture/README.md)、[实施与验证](../../docs/architecture/validation.md)、根 [CONTEXT](../../CONTEXT.md) 与 [ADR](../../docs/adr/README.md)。规格沿用既有架构选择；切片 01 的公共值合同和测试入口已建立，后续实现不得将占位协议示例当成已发布机器契约。

## 已确认的拆分决定

2026-10-03，用户确认以下三个建议：覆盖 A–F 全部阶段；按可独立实现和验收的能力细拆；以 Application / Component 公开合同为主，配合真实 PostgreSQL、SQLite 与可注入故障的目标。确认结束后按 `to-spec` 模板生成规格。

- **范围**：A–D 构成首阶段研究报告产品；E 增加记忆、委派与独立设备目标；F 完成受控改进和实际开放范围的规模、灾备、专项验收。
- **粒度**：每片有可观察出口及明确依赖。基础切片可交付合同或恢复演示；所有涉及真实动作的路径必须从首次开放起具备授权、预算与效果核对。
- **测试**：业务主要由 Application 驱动；替换能力通过 Component 同版套件验证；A 阶段在最高可用的 Host 边界验证真实存储适配器，不把 Host 变成第三方扩展 API。
- **领域记录**：没有新增领域概念或改变既有架构取舍；本轮不增加重复 ADR，不改写 CONTEXT。实施顺序与依赖记录在本索引。

## 切片目录

编号用于引用与建议排序，准确先决条件以“直接依赖”列为准；依赖的依赖同样必须满足。每片的 `Implementation: not-started` 表示尚未实施，`ready-for-agent` 仅表示规格无需再次 triage。实现者先核对前置切片的实际退出证据，再开始依赖工作。

| 编号 | 阶段 | 可交付能力 | 直接依赖 |
| --- | --- | --- | --- |
| [01](../lerna-01-command-contracts/spec.md) | A | 共同命令与机器契约 | 无 |
| [02](../lerna-02-durable-work/spec.md) | A | PG / SQLite 持久工作与接替 | [01](../lerna-01-command-contracts/spec.md) |
| [03](../lerna-03-deterministic-harness/spec.md) | A | 规则决策与可查询故障目标 | [01](../lerna-01-command-contracts/spec.md)、[02](../lerna-02-durable-work/spec.md) |
| [04](../lerna-04-content-snapshots/spec.md) | B | 准确内容发布与上下文快照 | [01](../lerna-01-command-contracts/spec.md)、[02](../lerna-02-durable-work/spec.md)、[03](../lerna-03-deterministic-harness/spec.md) |
| [05](../lerna-05-task-requirements/spec.md) | B | 目标接纳、条件与提案消费 | [03](../lerna-03-deterministic-harness/spec.md)、[04](../lerna-04-content-snapshots/spec.md) |
| [06](../lerna-06-authorized-admission/spec.md) | B | 行动准入、授权与预算账本 | [04](../lerna-04-content-snapshots/spec.md)、[05](../lerna-05-task-requirements/spec.md) |
| [07](../lerna-07-execution-reconciliation/spec.md) | B | 受管写入与未知效果核对 | [06](../lerna-06-authorized-admission/spec.md) |
| [08](../lerna-08-verified-results/spec.md) | B | 逐条件核验与正式结果 | [05](../lerna-05-task-requirements/spec.md)、[07](../lerna-07-execution-reconciliation/spec.md) |
| [09](../lerna-09-task-control/spec.md) | B | 目标修订、控制与终态收尾 | [08](../lerna-08-verified-results/spec.md) |
| [10](../lerna-10-model-search-adapters/spec.md) | C | 有界模型决策与真实搜索 | [09](../lerna-09-task-control/spec.md) |
| [11](../lerna-11-application-sdk/spec.md) | C | 应用 SDK、持久投递与受信交互 | [09](../lerna-09-task-control/spec.md) |
| [12](../lerna-12-research-report-app/spec.md) | C | 真实研究报告应用闭环 | [10](../lerna-10-model-search-adapters/spec.md)、[11](../lerna-11-application-sdk/spec.md) |
| [13](../lerna-13-distributed-runtime/spec.md) | D | 分布式传输、设备重连与租户隔离 | [09](../lerna-09-task-control/spec.md)、[11](../lerna-11-application-sdk/spec.md) |
| [14](../lerna-14-memory-core/spec.md) | D | 最小 Memory 合同与获准检索 | [06](../lerna-06-authorized-admission/spec.md) |
| [15](../lerna-15-component-interoperability/spec.md) | D | 三系统第二实现与异构互操作 | [10](../lerna-10-model-search-adapters/spec.md)、[12](../lerna-12-research-report-app/spec.md)、[13](../lerna-13-distributed-runtime/spec.md)、[14](../lerna-14-memory-core/spec.md) |
| [16](../lerna-16-activation-lifecycle/spec.md) | D | 版本激活、停用与在途恢复 | [13](../lerna-13-distributed-runtime/spec.md)、[15](../lerna-15-component-interoperability/spec.md) |
| [17](../lerna-17-production-readiness/spec.md) | D | 首次生产耐久、隔离与运维验收 | [12](../lerna-12-research-report-app/spec.md)、[15](../lerna-15-component-interoperability/spec.md)、[16](../lerna-16-activation-lifecycle/spec.md) |
| [18](../lerna-18-memory-lifecycle/spec.md) | E | 记忆提取、更正与来源生命周期 | [10](../lerna-10-model-search-adapters/spec.md)、[14](../lerna-14-memory-core/spec.md)、[16](../lerna-16-activation-lifecycle/spec.md) |
| [19](../lerna-19-bounded-delegation/spec.md) | E | 内部与外部 Agent 有界委派 | [09](../lerna-09-task-control/spec.md)、[13](../lerna-13-distributed-runtime/spec.md)、[16](../lerna-16-activation-lifecycle/spec.md) |
| [20](../lerna-20-device-autonomy/spec.md) | E | 独立设备目标与有限离线执行 | [13](../lerna-13-distributed-runtime/spec.md)、[16](../lerna-16-activation-lifecycle/spec.md)、[19](../lerna-19-bounded-delegation/spec.md) |
| [21](../lerna-21-evaluation-release/spec.md) | F | 冻结评测与受控改进发布 | [12](../lerna-12-research-report-app/spec.md)、[16](../lerna-16-activation-lifecycle/spec.md)、[18](../lerna-18-memory-lifecycle/spec.md)、[19](../lerna-19-bounded-delegation/spec.md) |
| [22](../lerna-22-capacity-recovery/spec.md) | F | 容量、异地灾备与专项验收 | [17](../lerna-17-production-readiness/spec.md)、[20](../lerna-20-device-autonomy/spec.md)、[21](../lerna-21-evaluation-release/spec.md) |

## 阶段出口与实施顺序

| 阶段 | 切片 | 退出证据 | 开放边界 |
| --- | --- | --- | --- |
| A | 01–03 | 同版合同、真实 PG / SQLite 去重与接替、可重复规则决策和故障目标 | 无真实业务副作用 |
| B | 04–09 | 原目标到准确写入、读回、逐条件 Result；控制与收尾恢复 | 确定性完整任务，不声称真实模型质量 |
| C | 10–12 | Go / TS SDK、真实模型和来源、至少一个内部应用接入 | 可接入的报告产品，仍需 D 生产门槛 |
| D | 13–17 | 真实 WSS / gRPC、三系统第二实现、版本生命周期、单区故障演练 | 首次生产出口，RPO=0 且控制与查询恢复≤60秒 |
| E | 18–20 | 记忆来源生命周期、根预算委派、独立设备任务可恢复 | 各能力逐项验收后显式启用 |
| F | 21–22 | 冻结评测、批准发布与回退、实测容量和异地灾备 | 只按实际证据声明质量与规模 |

当前第一个可开始的切片是 **01**；其余先等待各自依赖退出。依赖满足后，10 与 11 可分别推进，13 不必等待真实模型接入结束；14 可复用 B 的基础提前实现。这里的并行表示依赖允许，不假设已有团队、人力或工期承诺。

两处容易遗漏的依赖已明确处理：

1. **D 的 Memory 第二实现**：14 前置完整的最小 Memory 方法集合，15 验证互操作；18 再补自动提取、冲突和跨 holder 生命周期。D 不必等待全部 E，也不能以接口空壳通过 Memory 替换验收。
2. **首次生产恢复门槛**：17 已包含三可用区、同步耐久、旧主隔离与实际单区演练；22 扩展异地灾备及最终规模，不能替代或延后 17。

## 共同实施规则

- 规范强度沿架构约定：“必须／不得”为强制，“建议”为默认选择，“可选”为显式启用范围。不得用“当前未实现”绕过已开放路径的恢复、授权与效果核验。
- 每片增加公开方法时，一起实现闭合输入输出 Schema、状态前提、错误、准确版本、Go / TypeScript 类型与正反例；只广告完整支持的方法集合，不广告半实现 profile。
- 沿用 Go 单 module、PG 云端权威、SQLite 设备账本、对象存储、WSS / gRPC。实际依赖版本和供应商配置在实现时测试并锁定，规格不固定代码文件位置。
- 业务事实与必要 Job 同事务，跨 owner 分别接纳并保存交接责任。模型、网络、对象上传和用户等待不进入长持锁事务。
- 测试只断言公开行为、持久恢复结果与独立目标事实。故障案例必须有正常对照，不能靠全部拒绝取得表面正确。
- 切片中的英文 User Stories 沿 `to-spec` 的角色—能力—收益模板；规则、验收和范围以中文说明。尚无产品测试先例时明确写出，并引用前置切片待建立的复用设施。
- 本轮交付规格，不额外生成实现 tickets。需要更细的执行任务时按本地 tracker 约定拆入对应功能的独立 issue 文件，不把 22 份规格合并成一个巨型任务。

## 验收证据与完成含义

每片完成时，在其 Further Notes 下链接实际证据，至少包含准确代码与合同版本、依赖退出记录、数据库和目标环境、执行命令、正常与故障结果、限制与未关闭事项。已有 `Comments` 可追加讨论记录，不能修改历史结果来制造通过。

分别维护以下结论，不用一个绿色状态替代全部证据：

| 证据种类 | 说明 | 首要切片 |
| --- | --- | --- |
| 文档检查 | 链接、模板、依赖和覆盖关系可使用 | 本轮静态检查 |
| 合同与恢复 | 同版接口、实际存储、故障和原身份恢复符合约定 | 01–09、13–16、18–20 |
| 真实接入 | 真实来源、供应商、目标和实际读者试验 | 10–12、15 |
| 质量与成本 | 冻结样本、独立真值、失败分母、完整费用和区间 | 21–22 |
| 生产与灾备 | 实际故障域、容量、旧主隔离、RPO / RTO | 17、22 |

22 的验收工具实现可以形成实测报告，但最终覆盖、容量和质量目标只有实际达标才关闭；不足时明确列出缺口。模拟目标、同机多进程和小样本分别只证明自己的范围。

原设计的目标与全部故障反例映射见[验收矩阵](acceptance-matrix.md)。

## 执行时需要落实的外部条件

这些是后续真实验收的条件，不是当前规格仍在等待范围决策。缺少条件时可继续不依赖它的切片，但对应真实验收不得标记通过。

| 条件 | 负责角色 | 关闭方式 | 首次需要 |
| --- | --- | --- | --- |
| 获准模型、搜索、MCP 服务与实际计费保证 | 适配器维护者 | 固定配置，通过原查询、重试、费用上界和用途探针 | 10 |
| 内部接入应用、保存资源与实际读者 | 应用负责人 | 使用公开 SDK 完成报告及读者试验，冻结产品限制 | 12 |
| 独立组件与异构运行环境 | 组件维护者 | 三系统第二实现与真实组件开发者试验 | 15 |
| 身份、密钥、同步耐久与三可用区平台 | 平台负责人 | 原写端隔离和单区故障证据满足首次生产门槛 | 17 |
| 外部 A2A 服务及子任务预算保证 | 委派适配器维护者 | 真实协议探针和远端未知、取消、费用测试 | 19 |
| 正式样本、独立真值与用途许可 | 评测维护者 | 观察最终结果前冻结样本量、统计方法及验收计划 | 21 |
| 异地 RPO / RTO 与最终规模负载 | 平台及应用负责人 | 先定平台方案和指标，再完成实际演练与容量报告 | 22 |

费用、耗时和重试先采用应用硬限制；暂无硬限制时先有上限试运行，再冻结正式门槛。价格、时钟、隔离或查询保证无法证明时，限制对应能力，不能拿设计示例补证。

## 显式保留的可选范围

本轮完整覆盖 A–F 的工程主线，但没有将每个可选 profile 自动纳入开放范围。以下能力须另行补充实现规格和同版合同后才能启用；22 必须验证默认拒绝。

| 可选能力 | 当前范围 | 启用前必须覆盖 |
| --- | --- | --- |
| Schedule / Occurrence | 不实现调度产品，返回 unsupported | 时区版本、唯一发生时点、原创建身份、错过策略、暂停不取消已有 Task |
| 可复用 Environment / 程序化读取 | 不开放任意代码或运行栈恢复 | 资源隔离、hostcall 授权与账本、cell 崩溃后核对、变量与效果分离 |
| 通用 Surface | 11 只实现报告所需受信呈现与 InputRequest | 独立事件 Schema、版本、答案消费与恢复合同 |
| GUI 驱动 | 20 用受管本地资源验证接管，不声明手机支持 | 观察—输入—再观察、串行资源门禁、用户接管、迟到输入、真实平台专项验证 |
| 向量、混合、关系索引与递归策略 | 保留简单默认与替换合同 | 获准来源、可重建投影、累计成本及 21 的对照收益证据 |
