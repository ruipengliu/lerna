# 本项目架构比较基线

本页固定五个参考项目的比较口径。基线是研究当时的 `docs/architecture/.draft`、根 `CONTEXT.md` 与 `docs/adr/`，不是已经运行的 Harness 实现。快照时本仓库 HEAD 为 `e493ad266d110097aeeb69e10abdabfa771967ab`；[sources.json](sources.json) 保留准确基线 commit；原 154 个文件的哈希清单保留在[历史版本](https://github.com/ruipengliu/lerna/blob/f6b8f300dc034817cfcdac9c95ce6cfa3ee6a986/docs/research/agent-harness-comparison/sources.json)。清理前已按该 commit 的 Git 对象核对全部摘要吻合，本文仍只描述当时基线。

当前交付已包括九模块、共同契约、生产部署、工程布局和 24 项优化方向。上游项目中的同类机制可用于落实、细化或验证这些要求，不能据“上游有代码，本项目有设计”判断本项目遗漏该行为。现行方案明确运行实现、真实故障实验与收益测量尚未完成。[方案入口](https://github.com/ruipengliu/lerna/blob/e493ad266d110097aeeb69e10abdabfa771967ab/docs/architecture/.draft/README.md)、[工程状态](https://github.com/ruipengliu/lerna/blob/e493ad266d110097aeeb69e10abdabfa771967ab/docs/architecture/.draft/engineering.md)、[优化采用范围](https://github.com/ruipengliu/lerna/blob/e493ad266d110097aeeb69e10abdabfa771967ab/docs/architecture/.draft/validation/optimization-evidence.md)

## 1. 九个领域及其事实归属

| 领域 | 负责的判断与事实 | 比较中必须保持的边界 |
| --- | --- | --- |
| [Orchestrator](https://github.com/ruipengliu/lerna/blob/e493ad266d110097aeeb69e10abdabfa771967ab/docs/architecture/.draft/orchestrator/README.md) | Task、Goal 修订、Requirement 覆盖、提案消费、任务控制、预算、完成依据及继续责任 | 每 Task 固定一个逻辑负责方；工作进程可更换。会话结束、模型 stop 和 UI 断开不是任务完成 |
| [Brain](https://github.com/ruipengliu/lerna/blob/e493ad266d110097aeeb69e10abdabfa771967ab/docs/architecture/.draft/brain/README.md) | 按快照选择路径、一次决策 Proposal、输入编码及处理来源 | 本版每 Decision 零次或一次物理模型请求，关闭 SDK/代理/网关透明重试；重试由 Orchestrator 以新 Decision/ModelCall 接纳。辅助摘要及其他请求另行准入计量，逐次记账不放开单轮限制；长行动循环由 Orchestrator 掌握 |
| [Execution](https://github.com/ruipengliu/lerna/blob/e493ad266d110097aeeb69e10abdabfa771967ab/docs/architecture/.draft/execution/README.md) | 能力目录、Operation/Attempt、执行结果、原效果证据、控制及继续核对 | 提案不等于发送，运行关闭不等于外部未发生；unknown 和可能迟到效果沿原标识继续，不能改工具/渠道同义重做 |
| [Security](https://github.com/ruipengliu/lerna/blob/e493ad266d110097aeeb69e10abdabfa771967ab/docs/architecture/.draft/security/README.md) | 主体、用途、资源与范围授权，Grant 使用、撤回、收缩及累计结算 | 认证、用户工具确认、插件声明和 prompt 不能代替 Grant；一次消费与数值费用结算分开 |
| [Memory](https://github.com/ruipengliu/lerna/blob/e493ad266d110097aeeb69e10abdabfa771967ab/docs/architecture/.draft/memory/README.md) | 获得独立保存许可的跨任务材料，来源、时间、推断性质、当前使用及派生关闭 | 任务上下文、完整历史、摘要和缓存不自动成为长期记忆；local_only 限制不因派生或精炼解除 |
| [Collaboration](https://github.com/ruipengliu/lerna/blob/e493ad266d110097aeeb69e10abdabfa771967ab/docs/architecture/.draft/collaboration/README.md) | Delegation、唯一子映射、父子报告与结果范围、控制、未决效果及费用封账 | phase 是只读投影；拿到 child handle 或子报成功不使父成功；父结束后旧子责任仍可继续 |
| [Interaction](https://github.com/ruipengliu/lerna/blob/e493ad266d110097aeeb69e10abdabfa771967ab/docs/architecture/.draft/interaction/README.md) | Surface、版本快照、InputSubmission/InputRequest、准确预览与可信输入转交 | 呈现、转交、原业务消费分别成立；普通输入和模型生成内容不冒充本人确认；通知是读取加速器 |
| [Extensions](https://github.com/ruipengliu/lerna/blob/e493ad266d110097aeeb69e10abdabfa771967ab/docs/architecture/.draft/extensions/README.md) | 组件合同、InstallLock、安装/健康/迁移/激活、排空和回退 | 精确制品字节及依赖可核对；批准有效与实例 ready 分开；回退旧版需当前独立批准 |
| [Evaluation](https://github.com/ruipengliu/lerna/blob/e493ad266d110097aeeb69e10abdabfa771967ab/docs/architecture/.draft/evaluation/README.md) | 冻结总体/样本/各臂/计划、全部物理尝试、独立判断、暴露记录及证据资格 | 自评或 CI 通过不构成改善证据；恢复和取消不重置分母，质量与完整成本用同一实验口径 |

领域术语按 [GLOSSARY](../../../GLOSSARY.md) 使用。参考项目的 session、thread、lane、task、operation、goal 等原名只解释该项目自己的对象；名称相同不意味着本项目身份、状态或成功点相同。

## 2. 已确定的共同选型

| 范围 | 现行选择与证据 | 上游比较可改变的部分 |
| --- | --- | --- |
| 工程 | 单仓、初期一个 Go module；公开接口/嵌入 runtime/Go 与 TS SDK 分开；领域实现内部化 [ADR-0010](../../adr/0010-monorepo-shared-contract-release.md) | 包内模块深度、组合根、adapter 组织和共享验收套件；crate/package 数量不要求新增 Go module |
| 通信 | 同进程直接调用；端云 WSS、服务间 gRPC；Proto 外壳载严格 JSON；HTTPS 做发现、认证及大字节 [ADR-0002](../../adr/0002-go-wss-grpc.md) | 编码实现、缓冲/背压、适配、恢复快照和诊断；不另立一套协议字段权威 |
| 生产装配 | 从首次生产起分 gateway/application/worker/execution；公司平台托管 PG/对象字节及发现 [ADR-0003](../../adr/0003-production-distributed.md) | 生命周期、排空和 readiness 细化；本地 daemon、SQLite 与 CLI 结果不证明单区 RPO=0 或 RTO≤60 秒目标 |
| 任务路由 | 新任务可信发现；原命令、对象及旧工作固定原逻辑 owner [ADR-0004](../../adr/0004-discovery-fixed-task-routing.md) | host 内工作进程接替；客户端 presence、session 最近活跃或连接恢复不改变 Task 归属 |
| 存储 | 事实与 jobs 在适用本地 Tx 同提交；生产 PG、开发独立 SQLite；不可变字节先耐久、引用后提交 [存储方案](https://github.com/ruipengliu/lerna/blob/e493ad266d110097aeeb69e10abdabfa771967ab/docs/architecture/.draft/storage-and-middleware.md) | SQL/索引/有界投影/备份适配及本机实现；不采用聊天 JSONL 作为多域生产权威 |
| 持久工作 | 共用接纳与有界工作模板、逻辑 JobStore、有限扫描、可丢通知 [ADR-0009](../../adr/0009-reliable-work-framework.md) | 领取/旧完成/新增责任竞争的内部实现；不增加统一业务状态机或调度服务 |
| 去重与关闭 | 最小原命令和终态身份长期保留，敏感字节另行清理 [ADR-0001](../../adr/0001-retain-closed-identities.md) | 缓存和投影可重建；日志截断或正文清理不能抹掉拒绝重复启动的依据 |

三类材料应分开：业务权威事实、可丢/可重建投影、准确版本的正文。一个上游 SQLite 数据库可能同时有投影表和独立工作事实；一个 JSONL 文件也可能同时记录原始事件与模型可读材料，必须逐表、逐写路径判断，不能按存储格式推断全部可靠性。[技术建模](https://github.com/ruipengliu/lerna/blob/e493ad266d110097aeeb69e10abdabfa771967ab/docs/architecture/.draft/technical-overview.md#4-从目标到对象和提交边界的建模方法)

## 3. 关键行为决策

| 决策 | 在此次比较中的含义 |
| --- | --- |
| [目标条件先采用](../../adr/0006-adopt-requirements-before-actions.md) | 带目标修订的 proposal 先提交条件变化，原 proposal 的行动不同时生效；上游 hook/loop 不能绕过重新决策 |
| [评估证据资格](../../adr/0005-evaluator-evidence-eligibility.md) | 评估器退休与已知缺陷使证据失效不同；多个模型答复相同不能自动制造独立真值 |
| [旧版独立回退批准](../../adr/0007-independent-rollback-approval.md) | restore/rollback 能力只证明可恢复内容；旧版当前资格和准确依赖仍要核查 |
| [受信渲染器准确预览](../../adr/0008-trusted-renderer-preview.md) | preview ref、按钮出现和普通 answer 不证明准确内容已由受信界面呈现并消费 |

Task 可取消但原 Operation 的效果、费用与清理责任未封闭；暂停可阻止新工作，已足够且有效的完成证据仍可按既定规则提交。运行进程、模型 loop、操作发送、目标效果、业务完成、授权结算和副本清理是独立维度。[技术总览](https://github.com/ruipengliu/lerna/blob/e493ad266d110097aeeb69e10abdabfa771967ab/docs/architecture/.draft/technical-overview.md)、[任务状态](https://github.com/ruipengliu/lerna/blob/e493ad266d110097aeeb69e10abdabfa771967ab/docs/architecture/.draft/orchestrator/README.md#state)

## 4. 已采用的 24 项优化与此次分析的增量

| 已采用优化组 | 数量 | 此次源码研究应补充的证据或细化 |
| --- | --- | --- |
| ORC-01～03 | 3 | 目标语义覆盖、多次压缩保留硬约束、有限计划退出；上游停止/goal 工具不能取代条件核验 |
| BRN-01～03 | 3 | 最后编码及来源、工具/Skill 选择阶段、规划对照；逐次请求归属与缓存 prefix 实现 |
| EXE-01～03 | 3 | 准确目录绑定、输出覆盖、原效果恢复；hook 变换后检查、并发结果关联及恢复准入 |
| MEM-01～03 | 3 | 来源/时间/冲突、读时整理实验、派生关闭；摘要/经验候选与权威事实分开 |
| SEC-01～02 | 2 | 不可信内容不扩权、实际平台隔离；确认与执行检查顺序及程序化工具边界 |
| COL-01～02 | 2 | 子结果/效果/费用范围、有限独立分支对照；async handle、report 去重和晚到责任 |
| UI-01～02 | 2 | 真实完成依据与具体缺口；多端确认、队列取消、流缓冲和断线读取 |
| EXT-01～03 | 3 | 实际制品、Skill/Agent 合同、评测/发布/回退；装载 staging、配置世代与 ready 细化 |
| EVA-01～03 | 3 | 数据暴露/尝试分母、失败归因、配对质量/时延/全成本；跨 backend 故障语料和固定 stream |

全部方向与 OPT-01～12、X-01～06 的现行定义见[优化证据](https://github.com/ruipengliu/lerna/blob/e493ad266d110097aeeb69e10abdabfa771967ab/docs/architecture/.draft/validation/optimization-evidence.md)。本次综合报告使用三种增量标记：**落实**表示已有要求的工程做法；**细化**表示尚可写得更明确的内部接口、次序或验收反例；**实验**表示需通过冻结对照后才可启用的策略。标记均不代表本次已实施、验证或发布。

## 5. 本次比较的证据强度

- **源码事实**：固定 commit 的类型、实际入口、持久写路径、状态机、SQL 和测试定义；报告提供路径和行号。
- **机制推断**：例如较小接口有利于替换、持久无进展计数便于重启延续；陈述适用条件，不给实测收益数字。
- **本项目建议**：在既有 owner/接口/流程内提出实现细化或候选，列出成本与退出条件。
- **待运行证据**：真实数据库提交、进程/存储故障、平台隔离、模型质量、端到端费用与生产指标，均不能由此次静态研究替代。

参考仓库的 CI、benchmark 和评测包是可借鉴资产。本次没有执行它们，也不把 README 的质量、速度或自动改善主张当跨项目可比结论。
