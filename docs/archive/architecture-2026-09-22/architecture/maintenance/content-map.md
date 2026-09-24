# 新旧文档内容归属与覆盖检查


## 当前正文组织与本轮改写

子系统后续已按“机制主线与最小局部示例”改写；逐章理解自检、故事信息去向及图文对应见[子系统示例复核](subsystem-example-review.md)。

现行正文按[系统整体 → 子系统 → 协作 → 工程实现与验证](../main/README.md)组织，示例另成一条正常过程和四组独立分支。下方保留最初 v1/v2 合并的来源记录；其旧章节号不表示当前阅读顺序。

- 原 overview 的五章进入系统整体，补充通用协作过程，让全景图与运行过程相接。
- 原 04–10 进入子系统设计，重写职责开篇、章内示例前提与交接说明；补充协作连接、观测改进两个全景职责分组。
- 原 11–12 进入协作部分，补充持久交接、控制与来源变化；云侧恢复移到工程部分，与生产部署相邻。
- 组装、部署、恢复、评测、验收和架构比较按工程依赖相接。原 16 的组件职责由子系统章解释，完整计分与发布仍只在工程章维护。
- 原完整任务保留正常时间线，回执丢失和交付缺口进入示例 3。原记忆章的具体 C1 输入表进入示例 2，正文改为通用输入要求；来源、偏好、v7/C1 等教学含义保留。
- 跨模块语义字段、接口目录、故障组合和验收参数继续在 reference 维护；内核本地实现规格已移交详细设计，其余组件仍为规划。

路径与源文件哈希见[本轮迁移清单](reorganization-manifest.json)。原 v1/v2 [迁移清单](migration-manifest.json)的目标已更新到当前位置，源路径与源哈希保留。旧节标题锚点作为兼容入口保留；图示资产的原编号继续用作稳定标识，阅读页以当前目录和标题生成。


本文供文档维护者核对旧材料的去向、机制的唯一维护位置和要求覆盖。第 01–17 篇正文与附录 01–05 均已成文；初读导航见 [README](../README.md)，各篇范围见[逐篇大纲](outline.md)，改写规则见《写作指南》（原路径 `docs/architecture-writing-guide.md`，当前仓库未提供）。这里记录文档归属，不证明实现或验收完成。

## 本次整合后的维护位置

当前入口为 [architecture/README](../README.md)。v2 最初的按用途分组后来调整为本页开头所列的 main 四部分，五篇综合附录与专题细则留在 reference。[组件详细设计](../design/README.md)已完成运行内核及配套规格，其余章节保留规划，不创建空白章节。

<a id="runtime-design-transfer"></a>
## 任务运行内核的详细设计移交

本次移交只细化内核实现，不改变 Go／SQLite 基线、跨模块权威、六态或既有恢复强度。确认的新增选择为集中纯规则、目标更新冻结并显式复核、单任务单有效决策、失联轮次退休保留未知用量、复用人工处置入口，以及单机有界写入口。

| 原维护位置／已有依据 | 现行维护位置 | 处理方式 |
| --- | --- | --- |
| runtime-details §A 提交伪代码 | [详细设计 §7](../design/01-runtime-kernel.md#commit) | 完整迁入并细化；原锚点保留解释及链接，不再复制算法。 |
| 内核正文的职责与运行过程 | [架构正文](../main/02-subsystems/01-runtime-kernel.md) | 保留概念解释；内部结构、决策恢复与更新复核转详细设计。 |
| 对象／接口附录中待定的本地类型、WorkStore 方法及布局 | [接口规格](../design/01-runtime-kernel-contracts.md)、[持久化规格](../design/01-runtime-kernel-storage.md) | 固定 Go 声明、局部枚举、DDL 与索引；附录保留跨模块语义和查阅索引。 |
| 交互细则“已准入工作仍适用” | [冻结与复核](../design/01-runtime-kernel.md#updates) | 固定可实施的 KEEP／DROP 与派发竞争；原位置链接，不另维护自动判定算法。 |
| 原 RUN 故障矩阵与阶段验收 | [内核验证矩阵](../design/01-runtime-kernel-validation.md#matrix) | 保留原编号，新增 RK 用例细化本次选择；静态检查不冒充运行验收。 |

其余来源索引和整合历史如下。

旧版 30 篇 Markdown 及图示已归档至 `docs/archive/architecture-v1/`，保留来源追溯；归档不承担现行规范维护。下面旧版到各专题的对应表继续作为来源索引。精确文件迁移、迁移前哈希和源提交见 [migration-manifest.json](migration-manifest.json)。

| 整合中确认的差异 | 现行归属 | 保留的状态与处理 |
| --- | --- | --- |
| 两个入口都宣称提供当前规范 | [统一入口](../README.md)、[归档说明](../../archive/architecture-v1/README.md) | 当前目录统一维护，旧稿明确归档；实质冲突不按版本自动裁定。 |
| 组件规则依靠示例跨篇拼接 | [状态归属](../main/01-system/05-domain-and-state.md)、[组件全景](../main/01-system/03-component-panorama.md)、[详细设计规划](../design/README.md) | 补足结构阅读路径；既有专题与细则继续有效。 |
| 旧评审总述的强对照、代价与可推翻条件 | [关键选择](../main/01-system/06-key-decisions.md)、[架构比较](../main/04-engineering/06-architecture-comparison.md) | 保留 H1–H4、自建／复用、分组与成本，仍为待验证主张。 |
| 旧实现参考 §5.5 的检索与停止策略 | [决策细则](../reference/decision-details.md) | 保留 8→16 候选、连续 3 次无进展、双语分词与 Recall 指标，均为建议。 |
| 目录缓存与分数、Skill 消歧、guest 时间随机数 | [执行细则](../reference/execution-details.md#catalog)、[扩展细则](../reference/extension-details.md) | 补回已有规则，不产生新协议或授权。 |
| 生产事务池、RLS、领取时钟及数据族布局 | [部署细则](../reference/deployment-details.md#postgresql) | 物理布局保持建议状态，隔离和资格要求保留。 |
| Cell 仲裁、共享控制库 HA 与主机隔离前提 | [恢复细则](../reference/recovery-details.md#ha) | 首个自托管验证建议，尚无生产证据。 |
| 评测 Store、观测范围及可靠通知的显式规则 | [评测细则](../reference/evaluation-details.md#persistence) | 各自持久化、引用与正文分开、动态 Schema 与快照补读。 |
| 已迁入参数仍指向旧稿作为现行权威 | [接口附录](../reference/02-api-and-message-catalog.md)、[验收配置](../reference/05-validation-profiles-and-traceability.md) | 改正当前入口，固定数值不变，旧链接仅留作来源。 |

各专题局部示例与决定当前判断的限制继续放在一起，不为拆目录而把正常过程、条件与例外分离。跨模块机制索引指向已有完整解释，避免另造一份重复规则。

## 迁移边界与保真

本次未改变已确认目标、状态语义、授权和预算强度，不更新外部协议或依赖选型。五篇综合附录中的样本、数值和故障编号保留；图源、图片、HTML 与正文链接一并迁移。独有信息以规则及其适用条件为单位记录去向，未将目录覆盖视为形式化等价证明。

归档源中已有缺失的 `.scratch` 和 `CONTEXT.md` 链接转换成不可点击的历史来源说明，原路径逐条记录在 [unavailable-history.json](unavailable-history.json)；没有补造其内容。旧源码与迁移前状态仍可按清单中的源提交追溯。

逐篇过程复述、条件变化与遗留问题见[编辑自检](editorial-review.md)，设计未定项见[设计缺口](open-questions.md)。

## 编排依据

| 事项 | 采用的方向 |
| --- | --- |
| 主要读者 | 技术负责人和核心开发者，先理解系统全貌与运行机制。 |
| 优化重点 | 符合人的阅读习惯，让概念和解释随阅读逐步展开。 |
| 阅读次序 | 目标 → 全貌 → 完整过程 → 模块设计 → 复杂运行环境 → 建设与验证。 |
| 单篇结构 | 每个主题有一篇完整讲解，长清单和精确配置进入查阅附录。 |
| 机制解释 | 首次介绍从简短的具体问题引出设计及其作用；后文回顾结论与适用条件，设计理由就近说明。 |
| 阅读衔接 | 按当前问题引入概念，允许补足背景的简短重述；影响正确理解的限制随机制说明。 |
| 图例与代码 | 延续手机与电脑协作的项目进展摘要任务，保持资料、操作及交付位置一致；专题正文可使用配有解释的短伪代码、状态或接口片段。 |
| 当前成文状态 | 第 01–17 篇正文及附录 01–05 均已成文，按用途维护于当前 architecture 目录。 |
| 跨篇全景入口 | [系统组件全景](../main/01-system/03-component-panorama.md)已完成。单页 draw.io／SVG 展示 9 个模块的组件、存储、能力与关键交互；读图说明承接详细条件。HTML／SVG 机制核查图保留 10 个职责分组、26 条交接及 60 项处理内容的归属。完整规则继续在所属正文维护。 |

现有内容是新稿的设计来源。改写时将讨论过程整理成完整叙事，保持已确认的目标、行为契约和验证条件；新设计建议及待验证主张继续保留其状态。

## 旧入口与总述的归属

| 旧文档 | 新版位置 | 处理方式 |
| --- | --- | --- |
| [原阅读入口](../../archive/architecture-v1/README.md) | [新版入口](../README.md) | 收敛为顺读导航；实现、研究和历史来源通过专题或迁移材料查找。 |
| [架构总述](../../archive/architecture-v1/review.md) | [01 定位目标](../main/01-system/01-purpose-and-scope.md)、[02 全貌](../main/01-system/02-architecture-overview.md)、[03 完整任务](../examples/01-task-lifecycle.md)、[04 运行内核](../main/02-subsystems/01-runtime-kernel.md)、[06 规划决策](../main/02-subsystems/03-planning-and-decision.md)、[12 端云协同](../main/03-coordination/04-edge-cloud-coordination.md)、[13 部署存储](../main/04-engineering/02-deployment-and-storage.md)、[14 故障恢复](../main/04-engineering/03-reliability-and-recovery.md)、[16 观测评测](../main/04-engineering/04-evaluation-and-release.md) | 01–03 已迁移用途边界、职责及任务过程，04 已解释持久运行的基础机制与取舍，06 已解释有限决策及选择成本，12 已解释位置、离线和移交边界，13 已解释 H4 的用户隔离及共享池对照，14 已解释 H1 的原任务续跑、恢复代价与强对照，16 已解释 H2 的同条件比较、失败成本与证据资格；实际收益仍需验证。 |
| [架构实现参考](../../archive/architecture-v1/review-reference.md) | [02 全貌](../main/01-system/02-architecture-overview.md)、[03 完整任务](../examples/01-task-lifecycle.md)、[04 运行内核](../main/02-subsystems/01-runtime-kernel.md)、[06 规划决策](../main/02-subsystems/03-planning-and-decision.md)、各专题及附录 | 02–03 已整理职责、概念、部署与正常过程，04 已解释版本化准入、原子提交及持久交接，06 已承接选择、补查与预算策略并保留示例阈值的建议状态，14 已承接 6.3 的跨层恢复、资源控制与业务连续性证据；完整组件由各专题解释，完整契约目录进入附录。 |

## 现有分册的迁移位置

| 旧文档 | 正文归属 | 查阅材料及迁移重点 |
| --- | --- | --- |
| [00 设计输入与硬约束](../../archive/architecture-v1/00-design-constraints.md) | [01 定位目标](../main/01-system/01-purpose-and-scope.md)、[15 开发组装](../main/04-engineering/01-implementation-and-technology.md)、[17 实施验收](../main/04-engineering/05-roadmap-and-validation.md) | 01 已连接多端场景、单机开发与生产配置，并集中交代量化目标和证据状态；15 已解释单机组装、依赖基线与模拟验证；17 已说明阶段放行，附录 05 已集中完整配置、成熟度和能力追溯。 |
| [01 系统结构](../../archive/architecture-v1/01-system-architecture.md) | [02 全貌](../main/01-system/02-architecture-overview.md)、[03 完整任务](../examples/01-task-lifecycle.md)、[15 开发组装](../main/04-engineering/01-implementation-and-technology.md) | 02 的逻辑分工与部署对照、03 的完整协作过程已成文；15 已解释消费方接口、包依赖与启动入口。 |
| [02 内核与持久化](../../archive/architecture-v1/02-runtime-and-state.md) | [04 运行内核](../main/02-subsystems/01-runtime-kernel.md)、[15 开发组装](../main/04-engineering/01-implementation-and-technology.md) | 04 已讲解当前状态、准入原子组、RunStore/WorkStore 与持久调度；15 已解释组装、生命周期、独立替换及持久后端迁移，附录 02 已整理组件接口，附录 03 已列工作与提交故障窗口。 |
| [03 契约与协议](../../archive/architecture-v1/03-contracts-and-protocols.md) | [04 运行内核](../main/02-subsystems/01-runtime-kernel.md)、[10 扩展与协议](../main/02-subsystems/08-extensions-and-runtime.md)、[12 端云协同](../main/03-coordination/04-edge-cloud-coordination.md) | 04 已区分持久接收、业务接纳与效果确认；10 已解释原生传输与编码、外部映射和兼容；12 已展开消息分级、连续前缀、补投、背压与过期恢复。 |
| [03 消息目录](../../archive/architecture-v1/03-contract-message-catalog.md) | [10 扩展与协议](../main/02-subsystems/08-extensions-and-runtime.md)及各接口所属专题 | 10 已解释行为分类、操作／消息／远端关联、未知结果及外部适配范围；附录 02 已逐项整理 11 个服务的 65 个方法、信封、错误、协商和一致性矩阵。 |
| [03 标识模型](../../archive/architecture-v1/03-identifier-model.md) | [02 全貌](../main/01-system/02-architecture-overview.md)、[04 运行内核](../main/02-subsystems/01-runtime-kernel.md)、[08 交互控制](../main/02-subsystems/05-interaction.md) | 02 已前置任务与操作概念，04 已区分操作、内部提交和消息身份；08 以连续聊天解释任务引用、等待项与正式操作的关系；完整对象关系、作用域和标识合并依据进入附录 01。 |
| [03 UI 交互](../../archive/architecture-v1/03-ui-interaction-contracts.md) | [08 交互与控制](../main/02-subsystems/05-interaction.md)、[配套细则](../reference/interaction-and-task-control-details.md) | 08 按输入生效过程解释组件分工、目标绑定、可靠转交、业务接纳、控制落实和显示恢复；细则承接完整恢复、类型登记、流控与验证，附录 01–03 维护字段、消息与故障矩阵。聊天归属为应用参考流程，不限定自然语言分类实现。 |
| [04 端云所有权与恢复](../../archive/architecture-v1/04-edge-cloud-and-recovery.md) | [04 运行内核](../main/02-subsystems/01-runtime-kernel.md)、[12 端云协同](../main/03-coordination/04-edge-cloud-coordination.md)、[14 恢复](../main/04-engineering/03-reliability-and-recovery.md) | 04 已讲任务主状态、等待与控制维度及局部恢复；12 已解释 Owner 世代、准备封存激活、离线条件及按对象重连；14 已组合这些规则解释原任务续跑和跨层故障边界。 |
| [04 恢复状态与故障表](../../archive/architecture-v1/04-recovery-state-and-failure-matrix.md) | [04 运行内核](../main/02-subsystems/01-runtime-kernel.md)、[08 交互控制](../main/02-subsystems/05-interaction.md)、[12 端云协同](../main/03-coordination/04-edge-cloud-coordination.md)、[14 恢复](../main/04-engineering/03-reliability-and-recovery.md) | 08 已承接 UI 重连和原操作核对；12 已解释移交、旧资格、Inbox 与窗口清理；14 已解释设施故障下的状态表达、原身份核对、旧备份隔离及连续性验收；附录 03 已列任务转换、八个移交窗口、可靠交付、关闭水位和注入断言。 |
| [05 上下文与记忆](../../archive/architecture-v1/05-memory-and-context.md) | [05 上下文记忆](../main/02-subsystems/02-context-and-memory.md)、[12 端云协同](../main/03-coordination/04-edge-cloud-coordination.md) | 05 已解释信息责任、证据获取、上下文组装、记忆类型及受控复用；12 已解释位置限制、非权威离线修改与正式修订的收敛。 |
| [05 记忆数据与生命周期](../../archive/architecture-v1/05-memory-data-and-lifecycle.md) | [05 上下文记忆](../main/02-subsystems/02-context-and-memory.md)、[12 端云协同](../main/03-coordination/04-edge-cloud-coordination.md) | 05 已解释集合权威、版本冲突、模型生成中删除、上下文失效和清理范围；12 已细化同步视图、快照边界、增量应用、权限变化与待提交意图；附录 01–02 已整理字段、服务和组件接口，附录 03 已列修订、来源失效、同步与清理矩阵。 |
| [05 端云记忆](../../archive/architecture-v1/05-edge-cloud-memory.md) | [12 端云协同](../main/03-coordination/04-edge-cloud-coordination.md) | 已解释数据驻留、原文留端、就地联合检索、覆盖缺口、端侧提取、派生限制及各出口的验证要求。 |
| [06 大脑与多 Agent](../../archive/architecture-v1/06-brain-and-agent-collaboration.md) | [02 概念辨析](../main/01-system/02-architecture-overview.md)、[06 规划决策](../main/02-subsystems/03-planning-and-decision.md)、[11 多 Agent](../main/03-coordination/03-agent-collaboration.md) | 02 已简述 Agent、大脑与模型的区别；06 已解释有限提案、模型调用、结果综合与独立替换；11 已解释委派分工、独立子任务、正式结果及父任务交付责任。 |
| [06 决策准入与协作恢复](../../archive/architecture-v1/06-decision-admission-and-collaboration.md) | [04 内核](../main/02-subsystems/01-runtime-kernel.md)、[06 决策](../main/02-subsystems/03-planning-and-decision.md)、[08 控制](../main/02-subsystems/05-interaction.md)、[11 协作](../main/03-coordination/03-agent-collaboration.md) | 04 已定义任务版本、更新条件、生成预算基线和原提交核对；06 已解释调用边界、有限修复、流式输出、未知用量与停止条件；08 已用追加要求的时间表连接版本检查与持续输入；11 已展开唯一接纳、权限与预算、父子传播、失联取消和收尾，附录 03 已列准入、协作控制、额度与收尾矩阵。 |
| [07 工具与模拟设备](../../archive/architecture-v1/07-tools-and-simulated-devices.md) | [06 决策](../main/02-subsystems/03-planning-and-decision.md)、[07 执行](../main/02-subsystems/04-tools-and-devices.md)、[16 评测](../main/04-engineering/04-evaluation-and-release.md) | 06 已解释候选选择与前提；07 已解释 API 执行、路径切换、GUI 观察闭环、资源接管及有状态模拟验证，保留千级目标和真实效果的证据边界；16 已解释各类计分分母、候选比较及专项判定，附录 05 已整理完整样本、预算与验收配置。 |
| [07 执行接口与验证](../../archive/architecture-v1/07-execution-contracts-and-validation.md) | [06 选择策略](../main/02-subsystems/03-planning-and-decision.md)、[07 执行](../main/02-subsystems/04-tools-and-devices.md) | 06 沿用检索语义；07 已解释目录准确声明、Driver/ExecutionStore、持久准备、关联丢失、取消、控制范围与恢复，以及两种 API Adapter 和两类设备驱动的验证要求；附录 02 已整理完整调用接口，附录 03 已列执行与资源故障矩阵，附录 05 已整理真实模型、独立替换与互操作的验收配置。 |
| [08 授权与用户控制](../../archive/architecture-v1/08-authorization-and-user-control.md) | [08 交互控制](../main/02-subsystems/05-interaction.md)、[09 身份授权](../main/02-subsystems/06-identity-and-authorization.md) | 08 已解释正式输入、授权回应入口及受理／落实区别；09 已解释主体、处理端授权、数据出口、凭证和扩展信任依据。 |
| [08 授权生命周期与接口](../../archive/architecture-v1/08-authorization-lifecycle-and-interfaces.md) | [09 身份授权](../main/02-subsystems/06-identity-and-authorization.md) | 09 已按申请、签发、验证和消费解释许可使用，单独说明撤销、离线及数据凭证边界；附录 01–02 已整理字段、管理与组件接口、身份认证、授权材料及凭证保存配置，附录 03 已列签发、消费、撤销、可信时间与凭证故障。 |
| [09 扩展生态](../../archive/architecture-v1/09-extension-ecosystem.md) | [10 扩展与协议](../main/02-subsystems/08-extensions-and-runtime.md) | 10 已沿保存能力解释发现、锁定、验证、激活、Skill 按需加载和版本升级，保留运行方式、数据与权限边界。 |
| [09 扩展契约与激活](../../archive/architecture-v1/09-extension-contracts-and-activation.md) | [10 扩展与协议](../main/02-subsystems/08-extensions-and-runtime.md)、[15 开发组装](../main/04-engineering/01-implementation-and-technology.md) | 10 已解释版本锁定、本地激活原子组、投影应用、旧绑定恢复与隔离限制，并以请求取消说明适配对业务行为的影响；15 已说明 SDK 锁定、兼容补齐和开发组装。附录 01 整理清单字段，附录 02 集中 SDK／运行器、严格握手、媒体类型等精确配置及管理接口。 |
| [10 观测评测与自进化](../../archive/architecture-v1/10-observation-evaluation-and-evolution.md) | [16 观测与改进](../main/04-engineering/04-evaluation-and-release.md) | 16 已解释权威事实、观测覆盖、隔离评测、版本化候选、阶段控制及数据生命周期。 |
| [10 评测与发布判定](../../archive/architecture-v1/10-evaluation-contracts-and-release-gates.md) | [16 观测与改进](../main/04-engineering/04-evaluation-and-release.md)、[17 实施验收](../main/04-engineering/05-roadmap-and-validation.md) | 16 已解释计分、配对比较、统计适用性、独立保留确认、发布守卫及保留规则；17 已说明这些证据如何进入阶段放行，附录 05 已承接完整样本、预算、统计、发布参数及报告。 |
| [11 里程碑与依赖](../../archive/architecture-v1/11-milestones-and-validation.md) | [17 实施验收](../main/04-engineering/05-roadmap-and-validation.md) | 17 已解释 M1–M4 与逐项 L1–L4 目标、独立替换、专项和同一候选发布条件；附录 05 已承接完整成熟度矩阵、配置、用例与证据追溯。 |
| [11 专项、持久化替换与交接](../../archive/architecture-v1/11-specialized-validation-and-handoff.md) | [15 开发组装](../main/04-engineering/01-implementation-and-technology.md)、[16 评测](../main/04-engineering/04-evaluation-and-release.md)、[17 实施验收](../main/04-engineering/05-roadmap-and-validation.md) | 15 已完整解释 bbolt、全范围停写、双向迁移及回切条件，16 已解释各专项独立判定与证据边界；17 已展开恢复工作包、统一报告与开源交接；附录 05 已整理完整样本、预算与验收配置。 |
| [12 数据链路与协议补充](../../archive/architecture-v1/12-data-path-and-protocol-semantics.md) | [02 状态归属](../main/01-system/02-architecture-overview.md)、[04 内核](../main/02-subsystems/01-runtime-kernel.md)、[07 执行](../main/02-subsystems/04-tools-and-devices.md)、[08 交互控制](../main/02-subsystems/05-interaction.md)、[11 协作](../main/03-coordination/03-agent-collaboration.md)、[13 部署存储](../main/04-engineering/02-deployment-and-storage.md)及对应专题 | 02 已区分主要状态归属，04 已解释运行原子组、操作身份与可靠交接，07 已解释执行原子组及执行事实维度；08 已解释 SurfaceStore、ActionRoute、控制来源合并与乱序边界；11 已解释父子独立提交、控制传播及逐层落实，13 已解释各原子组在用户分片和端侧存储中的放置，其他模块链路由所属专题展开。 |
| [13 分布式部署与存储](../../archive/architecture-v1/13-distributed-deployment-and-storage.md) | [13 部署存储](../main/04-engineering/02-deployment-and-storage.md) | 13 已完整解释用户隔离、Cell、受信网关、路由、配额、长连接、原子分区、热冷分层与用户迁移；[附录 04](../reference/04-capacity-model.md)已整理详细演算、端云分账、分片及增量预算，新增数值保留教学假设状态。 |
| [14 分布式自愈与运行](../../archive/architecture-v1/14-distributed-recovery-and-operations.md) | [13 部署存储](../main/04-engineering/02-deployment-and-storage.md)、[14 故障恢复](../main/04-engineering/03-reliability-and-recovery.md) | 13 已解释 HA 拓扑、故障域与耐久派发前提；14 已完整解释耐久屏障、切换、恢复协调、执行阶段、资源时限、备份恢复和演练证据；附录 03 已列恢复阶段、设施窗口和连续性断言，附录 04 已承接故障余量、重连及积压演算，附录 05 已整理正式验收的冻结项、演练范围与计量，恢复数值仍须基准试验确定。 |

## 附录 01 的字段覆盖

[对象、标识与字段](../reference/01-data-model.md)已按既定设计完成统一逻辑数据字典；下表承接上方旧来源中的数据部分。方法与消息已由附录 02 承接，状态转换与故障已由附录 03 承接；容量演算已由[附录 04](../reference/04-capacity-model.md)承接，完整验收配置已由[附录 05](../reference/05-validation-profiles-and-traceability.md)承接；文档成文不表示实现与验收完成。

| 数据范围 | 附录内定位 | 已整理的来源与精度 |
| --- | --- | --- |
| 对象、身份与版本 | [对象目录](../reference/01-data-model.md#objects)、[标识与版本](../reference/01-data-model.md#identity-version) | 03 标识、实现参考 2、12 数据链路；沿用身份合并，change_id 按后续各 Store 的内部提交语义解释。 |
| 任务、预算、控制与移交 | [任务运行](../reference/01-data-model.md#runtime) | 02 内核、04 端云／恢复、06 决策／协作、12 控制；区分提案局部键、持久工作、正式操作及拥有权。 |
| 执行与资源 | [操作和执行](../reference/01-data-model.md#execution) | 07 执行契约、12 数据链路；保留阶段／结果／效果、能力与 Driver 绑定、条件外部关联和控制版本。 |
| 记忆、来源与上下文 | [内容和记忆](../reference/01-data-model.md#content-memory) | 05 数据生命周期／端云记忆；记录语义字段组、来源依赖、同步视图及删除进展。 |
| 独立 UI | [交互与呈现](../reference/01-data-model.md#interaction) | 03 UI、12 SurfaceStore；保留 ActionRoute 固定意图、业务等待项权威及可选任务关联。 |
| 身份与授权 | [授权数据](../reference/01-data-model.md#authorization) | 08 授权生命周期；区分登记、策略、许可、验证材料、预留、实际消费与凭证。 |
| 扩展、观测与评测 | [扩展](../reference/01-data-model.md#extension)、[评测改进](../reference/01-data-model.md#evaluation) | 09 扩展契约、10 观测／评测；准确资产、不可变计划、判读、持久试运行分组及逐节点应用分别保存。 |
| 持久交付与保留 | [恢复记录](../reference/01-data-model.md#recovery-records)、[原子组与保留](../reference/01-data-model.md#atomic-retention) | 03 消息、04 恢复、12 原子组、13–14 生产设计；水位与各模块保留义务分开，RecoveryController 保持生产建议状态。 |

附录采用三张小型关系图及同一摘要任务的记录对照，不新增物理表或公共身份。最终类型、编码、字段编号与存储布局的未定项集中说明；字段成文不代表实现 Schema 已定稿或运行验收通过。

## 附录 02 的接口与消息覆盖

[接口、消息与兼容配置](../reference/02-api-and-message-catalog.md)已整理既定调用契约。原生服务逐项列方法，组件接口独立维护；未定方法名、最终签名和编码细节保留实现边界。

| 契约范围 | 附录内定位 | 已整理的来源与精度 |
| --- | --- | --- |
| 原生服务 | [方法目录](../reference/02-api-and-message-catalog.md#native-services) | 03 消息目录及各专题后续补充；11 个服务、65 个方法分别说明角色、输入输出和条件。 |
| 身份、结果与错误 | [共同规则](../reference/02-api-and-message-catalog.md#call-rules)、[返回确定性](../reference/02-api-and-message-catalog.md#results-errors) | 03 标识／消息、12 数据链路；原操作重试与新操作版本检查分开，内部 change_id 不进入公共信封。 |
| 替换与内部存储 | [组件接口](../reference/02-api-and-message-catalog.md#component-interfaces) | 02 内核、05 记忆、06 决策、07 执行、08 授权、09 扩展、10 评测、12 UI；不把所有 Store 改成同一方法组，不自动映射为 RPC。 |
| 编码、绑定与协商 | [绑定与信封](../reference/02-api-and-message-catalog.md#wire-contract) | 03 协议／消息；固定 Protobuf、动态 JSON Schema、bootstrap、能力和有界限制，保留实现未定项。 |
| 交付与恢复 | [交付规则](../reference/02-api-and-message-catalog.md#delivery-recovery) | 03 消息、04 恢复、12 数据链路；Inbox 扫描、原操作核对、可靠前缀、窗口与保留分别说明。 |
| 端侧 UI | [消息与类型](../reference/02-api-and-message-catalog.md#ui-messages) | 03 UI、12 独立状态；消息不擅自变为新方法，正式动作、临时事件和呈现回报分开。 |
| 外部与运行配置 | [固定兼容配置](../reference/02-api-and-message-catalog.md#compatibility)、[身份认证](../reference/02-api-and-message-catalog.md#identity-configuration)、[凭证保存](../reference/02-api-and-message-catalog.md#credential-configuration) | 03 协议、08 授权、09 扩展及固定研究快照；保留方向、SDK 差异、可选能力、认证与加密配置和未验证状态。 |
| 示例与一致性 | [两条消息示例](../reference/02-api-and-message-catalog.md#message-examples)、[验证索引](../reference/02-api-and-message-catalog.md#conformance) | S／J 已关联后的回包丢失与 U 回答 I 是独立分支；完整故障矩阵和验收参数仍归附录 03／05。 |

## 附录 03 的状态与故障覆盖

[状态转换与故障检查表](../reference/03-state-and-failure-matrices.md)已按模块整理状态／阶段表和四列故障矩阵。每行保留初始条件、注入位置、允许／禁止结果和核对证据；所属节给出正文与设计来源。编号只用于用例追溯，运行结果仍需实际验证。

| 范围 | 附录内定位 | 已整理的来源与精度 |
| --- | --- | --- |
| 共同条件与查表 | [读表方法](../reference/03-state-and-failure-matrices.md#reading)、[状态维度](../reference/03-state-and-failure-matrices.md#common-rules) | 03 消息、04 恢复、12 数据链路；原提交、原操作、当前权限、事实维度与正式枚举分别说明。 |
| 任务与工作 | [运行矩阵](../reference/03-state-and-failure-matrices.md#runtime) | 02 内核、04 恢复、06 决策；六态、控制优先级、准入、领取和预算故障，不新增 Work 状态枚举。 |
| 可靠交付 | [消息矩阵](../reference/03-state-and-failure-matrices.md#delivery) | 03 契约、04 恢复、12 原子组；Inbox 扫描、跨库转交、连续前缀、过期恢复和背压。 |
| 执行与交互 | [执行矩阵](../reference/03-state-and-failure-matrices.md#execution)、[UI 矩阵](../reference/03-state-and-failure-matrices.md#interaction) | 07 执行、03 UI、12 独立状态；三维执行事实、关联丢失、接管、固定路由、多端竞答及末次增量丢失。 |
| 记忆与数据 | [记忆矩阵](../reference/03-state-and-failure-matrices.md#memory) | 05 数据生命周期／端云记忆；修订、模型期间失效、删除、快照／游标、权限视图和位置限制。 |
| 授权 | [授权矩阵](../reference/03-state-and-failure-matrices.md#authorization) | 08 生命周期；签发、单次消费、撤销传播、可信时间及凭证恢复，管理决定与实际应用分别取证。 |
| 协作与额度 | [协作矩阵](../reference/03-state-and-failure-matrices.md#collaboration) | 06 决策／协作、12 控制；唯一子任务、根暂停来源、乱序解除、父子收尾及未结额度。 |
| 移交与离线 | [Owner 矩阵](../reference/03-state-and-failure-matrices.md#ownership) | 04 端云／恢复；三管理阶段、八个移交窗口、旧资格与迟到事实、有限离线及重连。 |
| 扩展 | [激活矩阵](../reference/03-state-and-failure-matrices.md#extensions) | 09 生态／激活；本地提交、投影与逐节点应用、旧绑定、回滚兼容和受限运行器中断。 |
| 观测与发布 | [评测矩阵](../reference/03-state-and-failure-matrices.md#evaluation) | 10 评测／发布；评测五态与 gate_result、缺证据及被测失败、阶段交接和实际应用。 |
| 生产与迁移 | [恢复矩阵](../reference/03-state-and-failure-matrices.md#operations)、[迁移矩阵](../reference/03-state-and-failure-matrices.md#migration) | 13–14 生产、11 专项迁移；耐久屏障、旧主隔离、恢复风暴、备份、分片与同机后端迁移。 |
| 清理与演练 | [水位矩阵](../reference/03-state-and-failure-matrices.md#retention)、[组合注入](../reference/03-state-and-failure-matrices.md#validation) | 各模块保留规则、14 连续性；先关闭并履行恢复义务再清明细，固定初始全集及独立证据。 |

本附录迁移已定行为与故障要求，不声明全部状态组合已形式化。实际 Schema、夹具、Adapter 支持范围和冻结 profile 仍需实施补齐；容量演算见[附录 04](../reference/04-capacity-model.md)，完整验收参数与追溯见[附录 05](../reference/05-validation-profiles-and-traceability.md)。

## 附录 04 的容量与存储覆盖

[容量演算与存储预算](../reference/04-capacity-model.md)已按顶层任务、独立增量和端云位置整理计算口径。沿用已有教学值，并补齐可复算的 token、提交、体量与资源能力假设；尚无实测容量或生产放行结论。

| 计算范围 | 附录内定位 | 来源与边界 |
| --- | --- | --- |
| 目标、任务和端云计数 | [输入](../reference/04-capacity-model.md#inputs)、[负载](../reference/04-capacity-model.md#rates) | 项目目标、旧 13 §1、v2 13；500 万项／日明确为根任务，内部子任务和后台工作另计。 |
| 模型、连接与读写 | [模型配额](../reference/04-capacity-model.md#rates)、[连接](../reference/04-capacity-model.md#connections)、[数据库](../reference/04-capacity-model.md#database) | 旧 13 §1、§3–5；请求／token／在途、消息／记录／事务、修改／净增／WAL 分别计量。 |
| 存储与端侧分账 | [存储主账](../reference/04-capacity-model.md#storage) | 旧 13 §4、12 原子组、附录 01；各数据族、副本、索引、备份和迁移余量分别展开，端侧执行事实不全部计入云侧。 |
| Cell、热点及独立增量 | [分片](../reference/04-capacity-model.md#placement)、[增量](../reference/04-capacity-model.md#increments) | 旧 13 §2–7、14 恢复；演算分片下界，子任务、GUI、后台、重连和积压分别回填受影响资源。 |
| 保留与实际证据 | [保留](../reference/04-capacity-model.md#retention)、[报告](../reference/04-capacity-model.md#report) | 03 消息、04 恢复、10 评测、13–14 生产设计及附录 01／03；示例窗口不改清理条件，正式验收的冻结项见[附录 05](../reference/05-validation-profiles-and-traceability.md#production)。 |

本附录用表格和短公式完成查阅，不新增部署承诺、统一 TTL 或公共接口。参数未知处保留待测，静态复算与实现、压测、故障验收分别记录。

## 附录 05 的配置与追溯覆盖

[验收配置与能力追溯](../reference/05-validation-profiles-and-traceability.md)已按配置冻结、成熟度、质量与专项、替换和环境、发布与报告整理既定要求。参数仍区分已确认工程配置、生产建议和实施前待冻结值；尚无运行达标证据。

| 覆盖范围 | 附录内定位 | 来源与边界 |
| --- | --- | --- |
| C／A／V 与成熟度 | [成熟度](../reference/05-validation-profiles-and-traceability.md#maturity)、[追溯](../reference/05-validation-profiles-and-traceability.md#traceability) | 项目目标、旧 00／11、v2 17；完整 C1–C9 × L1–L4、M1–M4 目标及 A1–A4／V1–V2 独立证据。 |
| 千级 API | [目录与样本](../reference/05-validation-profiles-and-traceability.md#api)、[预算](../reference/05-validation-profiles-and-traceability.md#budgets) | 旧 07／10；独立业务计数、三组分母、逐 API 覆盖、有限纠错及全路径用量。 |
| 联网、GUI、个性化 | [专项](../reference/05-validation-profiles-and-traceability.md#specialized) | 旧 11 专项与 v2 16–17；继承样本和预算，GUI 边界按两模式分别判定，五类明细及全部控制断言保留。 |
| 比较与改善 | [统计比较](../reference/05-validation-profiles-and-traceability.md#comparison) | 旧 10、统计研究；分层族配对、方法验证、保护条件及独立保留确认，绝对代价限额缺值须预先补齐。 |
| 替换、协议与持久化 | [替换](../reference/05-validation-profiles-and-traceability.md#replacement)、[互操作](../reference/05-validation-profiles-and-traceability.md#interop)、[迁移](../reference/05-validation-profiles-and-traceability.md#migration) | 旧 07／10／11、v2 15、附录 02；独立实现与对端、运行器上限、SQLite／bbolt 完整配置及停写双向迁移。 |
| 端云与生产 | [端云](../reference/05-validation-profiles-and-traceability.md#edge)、[生产](../reference/05-validation-profiles-and-traceability.md#production) | 旧 13–14、v2 12–14、附录 03–04；冻结负载与故障域、原任务全集、耐久范围、各数据出口及正式恢复门槛。 |
| 发布与统一报告 | [阶段发布](../reference/05-validation-profiles-and-traceability.md#release)、[报告](../reference/05-validation-profiles-and-traceability.md#report)、[保留](../reference/05-validation-profiles-and-traceability.md#retention) | 旧 10／11、v2 16–17；样本／时长、当前资格、停止／回滚、同一候选证据及来源失效。 |

追溯沿用能力编号和附录 03 故障编号，profile 名称只作查阅标签。目录、真值、注入夹具、实际命令、统计仿真与运行报告属于实施交付；本附录不新增公共 Schema，不将教学容量、模拟时钟或静态检查写成验收通过。

## 正文与专题细则的归属

正文优先支持理解与设计评审：职责、选择理由、正常过程和决定当前行为的条件就近解释。专题细则承接实现顺序和局部限制，附录 01–05 继续维护跨章节的完整字段、方法、状态、容量与验收配置；迁移不改变条款效力。

| 原正文内容 | 正文保留的判断依据 | 精确去向 |
| --- | --- | --- |
| 04 的存储提交与到期核对伪代码 | 原子组、资格检查、原提交核对及等待责任。 | [内核细则 A、B](../reference/runtime-details.md) |
| 05 的副本清理、接口与存储说明 | 来源失效、删除完成维度、恢复隔离与查询覆盖。 | [记忆细则 A、B](../reference/memory-details.md) |
| 07 的目录分页、Driver 方法、资源接口及验证矩阵 | 准确版本、启动与效果区分、未知结果、接管条件。 | [执行细则 A–D](../reference/execution-details.md) |
| 08 的并发输入、表单、控制传播、显示恢复及类型细目 | 目标绑定、可靠路由、业务消费、受理与落实。 | [原交互细则](../reference/interaction-and-task-control-details.md) |
| 09 的认证、签发异常、消费材料、时间与凭证细目 | 授权判定、许可消费、撤销和各数据出口条件。 | [原授权细则](../reference/identity-and-authorization-details.md) |
| 10 的清单、传输编码、外部适配、Wasm 限制及验证矩阵 | 激活与投影关系、Skill／脚本边界、旧绑定及隔离。 | [扩展细则 A–D](../reference/extension-details.md) |
| 11 的逐目标控制交接及验证矩阵 | 独立权威、唯一委派、预算守恒与全树收尾条件。 | [协作细则 A、B](../reference/collaboration-details.md) |
| 12 的可靠序号、保留与关闭水位细账及验证矩阵 | 重连先核资格、原操作恢复、窗口不重开。 | [端云细则 A、B](../reference/edge-cloud-details.md) |
| 13 的 Gateway 认证及逐层隔离说明 | 可信主体绑定、唯一可写分区、权限与路由分离。 | [部署细则 A、B](../reference/deployment-details.md) |
| 14 的恢复阶段、工作阶段矩阵及 HA 参数 | 恢复职责、三类不确定性、耐久屏障与旧主隔离。 | [恢复细则 A–C](../reference/recovery-details.md) |
| 15 的包路径、bbolt 实现限制与依赖版本表 | 消费方接口、唯一活动配置、停写及双向迁移。 | [组装细则 A–C](../reference/implementation-details.md) |
| 16 的记录责任、统计配置与保留期限 | 评分职责、方法适用性、发布守卫及来源失效。 | [评测细则 A–C](../reference/evaluation-details.md) |
| 17 的独立替换对象矩阵 | M3 范围、独立实现与同一候选的证据要求。 | [实施细则 A](../reference/roadmap-details.md) |

第 01–03 篇保留定位、全貌和任务叙事；第 06 篇将能力选择前置，再解释提案、反馈和模型调用，参考策略参数由决策细则承接。第 08、09 篇继续作为结构与解释效果的参照，现有细则保持原有归属。原逐章编辑自检（历史路径 `docs/drafts/architecture-v2-reader-review.md`，当前仓库未提供）记录具体障碍、过程复述、条件变化判断与保留的未定项，属于编辑材料。

## 容易重复的主题如何分工

| 主题 | 完整解释的正文位置 | 其他篇如何引用 |
| --- | --- | --- |
| 任务状态、版本、提案准入和持久工作 | 04 运行内核 | 03 用例展示；06 说明提交什么提案；08 将版本规则用于正式输入；14 使用既定恢复条件。 |
| 原子提交、稳定操作、Inbox/Outbox | 04 运行内核 | 各模块说明自己的原子组；10 做协议映射；14 组合为恢复过程。 |
| Driver、执行记录与外部效果 | [07 工具执行](../main/02-subsystems/04-tools-and-devices.md) | 03 展示调用；04 以原作业的分轮查询补足持久调度背景，并说明内核如何处理返回事实；14 组合为故障恢复过程。 |
| GUI 观察、资源控制与接管 | [07 工具执行](../main/02-subsystems/04-tools-and-devices.md) | 08 解释交互入口及呈现，09 定义权限依据，12–14 沿用控制资格与在途效果规则。 |
| 上下文、证据和记忆生命周期 | [05 上下文记忆](../main/02-subsystems/02-context-and-memory.md) | 06 消费上下文；09 定义权限依据；12 解释跨位置协作。 |
| 能力选择与目录 | [06 定义选择策略](../main/02-subsystems/03-planning-and-decision.md)；[07 定义目录和 Driver 能力](../main/02-subsystems/04-tools-and-devices.md) | 06 沿用目录契约解释准确声明与覆盖缺口；10 说明扩展如何登记；16 说明如何评测选择与执行质量。 |
| 独立 UI 与用户控制来源 | [08 交互控制](../main/02-subsystems/05-interaction.md) | 04 落实任务转换；09 消费授权输入；11 传播父子控制。 |
| 聊天输入的任务归属 | [08 交互控制](../main/02-subsystems/05-interaction.md) | 应用参考流程明确绑定与歧义澄清；新建、更新、回答及控制沿用既定任务契约，不新增会话身份或核心分类器要求。 |
| 身份、授权与离线有效期 | [09 身份授权](../main/02-subsystems/06-identity-and-authorization.md) | 各处理端说明自己的检查位置；12 按既定权限判断可否离线运行；13 解释生产 Gateway 的受信代理和用户作用域。 |
| 契约的传输、编码和外部适配 | [10 扩展与协议](../main/02-subsystems/08-extensions-and-runtime.md) | 12 解释断连后的通道行为；附录 02 提供完整方法和消息。 |
| 扩展激活、版本绑定与运行器 | [10 扩展与协议](../main/02-subsystems/08-extensions-and-runtime.md) | 06–07 沿用决策及能力的固定版本；08 沿用 Renderer 接入；15 说明 SDK、依赖和开发组装。 |
| 决策预算、委派及父子任务收尾 | [06 说明有限生成与停止](../main/02-subsystems/03-planning-and-decision.md)；[11 解释委派、额度及父子收尾](../main/03-coordination/03-agent-collaboration.md) | 04 持久化本任务预算；12 安排位置与离线运行；14 处理恢复调度。 |
| Owner 移交、离线运行与端云记忆 | [12 端云协同](../main/03-coordination/04-edge-cloud-coordination.md) | 02 介绍最少概念；05 定义记忆语义与修改权威；09 定义离线许可；13 说明生产放置；14 处理跨层故障时序。 |
| 数据分片、存储布局与用户迁移 | [13 部署存储](../main/04-engineering/02-deployment-and-storage.md) | 04 规定局部原子性；14 规定 HA 切换与恢复资格。 |
| 容量演算、端云分账与保留预算 | [附录 04](../reference/04-capacity-model.md) | 13 解释部署与主要换算，14 解释恢复资源控制；附录 05 维护冻结验收配置。 |
| 生产耐久屏障与恢复资源控制 | [14 故障恢复](../main/04-engineering/03-reliability-and-recovery.md) | 13 展示拓扑；17 指向实际演练证据。 |
| bbolt 独立后端与同机迁移 | [15 开发组装](../main/04-engineering/01-implementation-and-technology.md) | 17 固定 M3 依赖；附录 05 给配置与验收断言。 |
| 评测、比较与改进发布 | [16 观测评测](../main/04-engineering/04-evaluation-and-release.md) | 17 规定阶段完成条件；附录 05 保存完整样本和参数。 |
| 阶段、能力验收与实施交接 | [17 实施验收](../main/04-engineering/05-roadmap-and-validation.md) | 01 概述目标；15 说明工程组装；16 说明判分与发布机制；附录 05 保存完整配置和追溯矩阵。 |

每项规则在所属正文解释语义，在对应附录列出完整规格。其他篇用简短重述补足本节需要的结论、条件和局部示例，再链接深入解释。修改规则时检查这些重述的语义一致性，确保读者可以连续理解当前过程。

## 能力与约束覆盖

下表按[项目目标](../../harness-project-goals.md)逐项定位解释与验证要求。正文说明机制，附录保存数据、接口、故障、容量和验收配置；目录覆盖不表示实现或验收已经完成。

| 基线 | 正文位置 | 保留的验证入口 |
| --- | --- | --- |
| C1 任务理解、规划与决策 | 03、06、11 | 16 的决策与效果评测，17 和附录 05 的成熟度及预算。 |
| C2 上下文、记忆与个性化 | 05、09、12 | 删除、用途、适用性、成对个性化与跨端检索验证。 |
| C3 信息获取与证据处理 | 03、05、07、12 | 信息来源与时间、冲突、缺口、真实联网、跨位置检索和独立获取 Adapter。 |
| C4 工具调用与设备操作 | 07 | API、GUI、效果确认、资源接管和驱动替换。 |
| C5 任务运行与端云协同 | 04、11–14 | 持久运行、所有权、控制、恢复、用户隔离与生产负载。 |
| C6 扩展接入与协议互操作 | 10、15 | 原生/外部方向、独立对端、Skill、运行器、SDK 和 Renderer。 |
| C7 授权与用户控制 | 08–09、11–14 | 执行端检查、委派收缩、撤销、隐私出口、离线有效期与故障恢复中的控制保持。 |
| C8 运行观测与能力评测 | [16](../main/04-engineering/04-evaluation-and-release.md) | 可追溯记录、不可变计划、独立判分、统计适用性及版本比较。 |
| C9 受控自进化 | [16](../main/04-engineering/04-evaluation-and-release.md) | 候选、隔离评测、保留确认、实际改善、启用、监测与回滚。 |
| A1 逻辑职责 | 02、04–07 | 17 的依赖与运行事实检查，保持三系统和内核分工。 |
| A2 参考实现与独立替换 | 10、15 | M3 的独立实现、第二持久后端、迁移及共同契约验证。 |
| A3 端云位置可变 | 12–14 | 实际跨节点边界、位置组合、离线、移交及恢复资格；模拟端满足当前设备范围。 |
| A4 共同契约与统一授权 | 04、08–14 | 核心调用领域、管理接口、UI、跨端恢复和外部适配的授权及兼容验证。 |
| V1 联网问答 | 03、05、16–17 | 固定资料与实际联网分别取证，答案、引用、冲突和失败分别判定。 |
| V2 手机 GUI | 07、16–17 | 结构树/图像、点击、滑动、输入、返回、观察核验、中断与接管。 |

以下设计条件在改写中也必须保留：

| 条件 | 大纲中的明确位置 |
| --- | --- |
| Go 核心、SQLite 默认、无需独立基础设施服务的单机运行 | 01 的起点与边界、15 的默认组装。 |
| API 优先、GUI 适用兜底、超过 1000 个 API 与 90%/95% 目标 | 01 的执行范围与集中目标表、06 的选择、07 的执行、16 的评测、17 和附录 05 的独立判定。 |
| 多个有状态模拟设备，当前验收无需真机 | 07 的设备验证、15 的环境、17 的支持范围。 |
| 千万注册、百万日活、十万在线，按个人用户隔离 | 01 的目标、09 的身份权限、13 的设计、附录 04 的容量模型。 |
| 生产配置独立于单机开发配置 | 13 的两类部署、15 的本地组装与生产 Adapter。 |
| 独立 UI 的状态权威和持久转交 | 08 的 SurfaceStore、ActionRoute、业务等待项；附录 01–03。 |
| SQLite 与 bbolt 停写双向迁移，不扩展为在线多主能力 | 15 的替换迁移、17 的 M3 验收、附录 05。 |
| 生产持久化不把“本地可见”当作“已经具备派发耐久性” | 14 的生产耐久屏障、附录 03 的切换故障窗口。 |
| 配置改进与代码发布保留不同授权边界 | 16 的改进批准、附录 05 的发布判定。 |

## 研究依据与图示

[系统组件全景图](../diagrams/system-panorama.svg)及其[可编辑 draw.io 文档](../diagrams/system-panorama.drawio)以 02 的职责关系与各专题组件为依据，展示模块组成、关键能力和组件交接。组件名称对应旧版总体架构与实现参考中的责任主体，任务、执行、界面和记忆记录各有存储；运行持久化单元合并展示 RunStore 与 WorkStore。[读图说明](../main/01-system/03-component-panorama.md)沿 03 的摘要任务解释主线，补充等待、两种未知、控制、委派、端云和评测发布的条件。[机制核查图](../diagrams/system-flow-map.html)保留 60 项处理内容的归属、事务记录与精确规则，[正文及基线覆盖对照](../main/01-system/03-component-panorama.md#正文覆盖)用于检查解释入口。两图均不接管各机制的权威定义，也不证明字段、故障矩阵或运行验收已经完成。

研究报告保留在原研究目录，通过对应正文引用。目录重组不重新批准研究建议，不更新外部规范版本；后续若改变选型或版本，再在该变更范围内核查外部依据。

| 研究材料 | 对应正文 |
| --- | --- |
| [技术格局](../../research/agent-harness-landscape-2026-09.md) | 01、15 的定位与技术路线依据。 |
| [持久运行机制](../../research/harness-durable-runtime-options.md) | [04 运行内核](../main/02-subsystems/01-runtime-kernel.md)及 14–15 的状态、运行和复用设计。 |
| [协议与 Skill](../../research/harness-protocol-contracts.md)、[协议来源核对](../../research/harness-protocol-source-check.md) | [10 扩展与协议](../main/02-subsystems/08-extensions-and-runtime.md)已解释协议映射与适配边界，附录 02 承接完整版本与特性配置。 |
| [身份、凭据与离线时间](../../research/harness-identity-credentials-and-offline-time.md) | [09 身份授权](../main/02-subsystems/06-identity-and-authorization.md)已解释认证、凭证与有限离线许可；[12 端云协同](../main/03-coordination/04-edge-cloud-coordination.md)已按这些条件展开离线与恢复。 |
| [扩展运行器](../../research/harness-extension-runtime-boundaries.md)、[SDK 与 Skill 快照](../../research/harness-extension-sdk-and-skill-snapshots.md) | [10 扩展与协议](../main/02-subsystems/08-extensions-and-runtime.md)已解释运行方式、内容快照和 SDK 补齐边界，[15 开发组装](../main/04-engineering/01-implementation-and-technology.md)已承接依赖锁定、支持范围及兼容补齐。 |
| [评测统计与比较](../../research/harness-evaluation-statistics-and-comparison.md) | [16 观测评测](../main/04-engineering/04-evaluation-and-release.md)已解释既定配对推断方法及其验证前提，[附录 05](../reference/05-validation-profiles-and-traceability.md#comparison)已承接完整配置，统计适用性仍需实施验证。 |
| [第二持久后端](../../research/harness-second-persistent-backend.md) | [15 开发组装](../main/04-engineering/01-implementation-and-technology.md)已说明 bbolt 实现责任与双向迁移；[17 实施验收](../main/04-engineering/05-roadmap-and-validation.md)已承接 M3 依赖与后端证据范围。 |
| [真实设备执行路径](../../research/harness-device-execution-options.md) | 07、15 的后续 Adapter 背景，保持现有模拟验证范围。 |

01 已配有[多端任务插图](../diagrams/01-task-context.png)，并以表格比较职责、能力边界、运行环境和量化目标。02 已配有[职责总览](../diagrams/02-system-overview.html)和[单机与端云对照](../diagrams/02-deployment-comparison.html)，保留主要请求与反馈、按需记忆访问及可选部署位置。03 已配[正常任务时序图](../diagrams/03-task-sequence.html)，用资料摘录、摘要样稿及阶段、核验表连接输入、实际文件与手机交付。协作与端云图见下述 11–12；部署、存储与故障恢复图见下述 13–14，开发组装与评测改进图见下述 15–16，实施依赖图见下述 17。复用前检查图中是否引入尚未解释的概念，必要时简化或拆图。

04 已配[任务状态图](../diagrams/04-task-state.html)、[准入提交图](../diagrams/04-admission-commit.html)、[可靠交接时序图](../diagrams/04-reliable-handoff.html)及其 [Mermaid 图源](../diagrams/04-reliable-handoff.mmd)。记录、对象和阶段对照表配合正文决策伪代码及[内核细则](../reference/runtime-details.md)中的提交／扫描伪代码，解释决策基线、扫描领取、原作业查询与后续推进，区分正常结束一轮和租约过期后的接手；时序图展开持久接收、业务接纳和结果消费，各模块保留自己的原子组。

05 已配[信息组件图](../diagrams/05-information-components.html)、[上下文组装图](../diagrams/05-context-assembly.html)和[偏好删除时序图](../diagrams/05-memory-invalidation.html)。具体上下文表与短伪代码连接 E1、E2、P@1 和 C1；删除分支区分任务版本与来源修订，展示旧提案退出及 C2 重组，清理表保留副本、派生内容和备份的范围与完成度。

06 已配[有限决策循环图](../diagrams/06-decision-loop.html)和[能力选择图](../diagrams/06-capability-selection.html)。摘要 D 与 S、R 提案示意连接参数来源、依赖和实际反馈；近义候选对照及索引滞后分支保留前提与覆盖缺口，生成控制伪代码和预算、完成、验证表区分有限修复、原工作推进和新决策。

07 已补充[执行组件图](../diagrams/07-execution-components.html)，并保留[保存执行时序图](../diagrams/07-execution-sequence.html)和[资源接管图](../diagrams/07-resource-takeover.html)。S／R 与外部作业 J 沿用同一保存过程，启动及恢复伪代码保留持久准备、未知提交和效果核对边界；独立模拟待办例与步骤表说明观察、替换输入、返回核验及保存前接管，模拟能力与真实平台支持分别表达。

08 以问题和关键决策开篇，按输入生效过程组织正文。[组件关系图](../diagrams/08-interaction-components.svg)先建立状态归属，再用转交伪代码、版本时间表、[摘要确认时序图](../diagrams/08-confirmation-handoff.svg)、[暂停来源合并图](../diagrams/08-control-sources.svg)和[重连恢复图](../diagrams/08-surface-recovery.svg)说明交接与结果依据。版本表保留 T、A，确认图保留 D、I、U 与后续 S；控制意图、任务状态和落实进展分别解释。[配套细则](../reference/interaction-and-task-control-details.md)承接完整恢复、控制传播、类型登记、流控及原验证矩阵，无任务订阅操作 N 的[独立恢复时序图](../diagrams/08-action-recovery.svg)随示例迁入细则。原规则和事务边界不变，精确字段及接口继续归已有附录，旧标题锚点保留。

09 以问题说明和关键决策表开篇，正文由[组件依赖图](../diagrams/09-identity-authorization/01-components.svg)、[授权判定图](../diagrams/09-identity-authorization/02-decision.svg)、[签发时序图](../diagrams/09-identity-authorization/03-issuance.svg)、[撤销时序图](../diagrams/09-identity-authorization/04-revocation.svg)、[离线资格状态图](../diagrams/09-identity-authorization/05-offline.svg)、[数据出口图](../diagrams/09-identity-authorization/06-data.svg)和[凭证流向图](../diagrams/09-identity-authorization/07-credentials.svg)串起请求处理。行为伪代码与规则表保留两端各自提交、单次消费、未知占用、委派收缩和实际效果核对。归档许可与原保存撤销仍为独立分支，分别保留读回权限和有限收尾许可；图示不增加部署、持久状态枚举或隔离保证。[配套细则](../reference/identity-and-authorization-details.md)承接认证、材料、时间、密钥维护和完整验证要求，精确规格继续归已有附录；旧标题锚点继续承接既有跨篇链接。

10 已补充[扩展组件图](../diagrams/10-extension-components.html)，并保留[保存能力接入映射图](../diagrams/10-contract-mapping.html)和[在途操作升级时序图](../diagrams/10-upgrade-sequence.html)。原生／MCP 对照保留 S、J 与实际效果，Skill 只参与决策；MCP 的作业查询是工具前提。升级分支中 S、R 均保持 v1，尚未固定的工作可选 v2。激活伪代码保留本地提交及投影应用，协议、运行方式和管理动作对照表区分兼容、当前资格与运行证据。

11 已配[两项资料核对协作时序图](../diagrams/11-collaboration-sequence.html)和[失联后取消时序图](../diagrams/11-cancellation-recovery.html)。U1／U2 与 T1／T2 分别表示委派操作和子任务，结果限定为资料核对，父任务继续承担 D、S／R 与手机交付。K／K2 的取消分支保留原关联及未知占用，正常停止后按证据结算；预算演算不是默认额度或实测数据，控制和异常对照表承接父子收尾的完整规则。

12 已配[资料与运行位置图](../diagrams/12-data-placement.html)、[所有权移交时序图](../diagrams/12-owner-transfer.html)和[离线重连时序图](../diagrams/12-offline-reconnect.html)。T 在读取 E2 前从云移到电脑，E2 原文与相关计算留端；电脑完成本地 S／J、R，手机交付在重连核对后完成。移交图明确目标已激活但回执丢失时源端保持封存；移交与重连伪代码、检查点及同步规则表解释有限离线条件、连续消息前缀、保留窗口、就地检索、覆盖缺口和记忆修订收敛。

13 已配[单机与生产配置对照图](../diagrams/13-deployment-comparison.html)和[数据分层图](../diagrams/13-storage-layers.html)。独立生产分支由云侧持有 T、按许可处理 E1／E2，电脑保存原 S／J／R 与 D；用户 A 的正常请求和 B 的独立批量任务串起路由、原子分区、公平调度与迁移。隔离与迁移规则表、容量演算保留数据层次、全路径隔离、封存与激活、原操作与水位，以及生产组合和效果尚待验证的状态。

14 已配[原任务续跑图](../diagrams/14-task-resumption.html)和[生产耐久切换时序图](../diagrams/14-durable-failover.html)。主线保留已读取的资料、摘要 D 及已准入 S／R，云侧进程恢复后通过电脑执行模块核对原 S／J，确认保存后继续 R；独立数据库分支在 S 尚未派发时展开 C 的本地可见、同步确认未知及新权威核对。恢复规则表与派发伪代码连接恢复职责、阶段、限额、备份及证据，积压数字为演算，参数与恢复目标保留待验证状态。

15 已配[主要包依赖方向图](../diagrams/15-package-dependencies.html)和[本地组装图](../diagrams/15-local-assembly.html)。脚本 Brain 与有状态模拟设备解释可重复的开发组装，迁移主例从原 S／R 已准入且 S 尚未派发开始；替换与迁移对照表、启动伪代码补足记录保留、停写切点、唯一活动配置、双向迁移及不同验证配置的证据边界。

16 已配[独立评测流程图](../diagrams/16-evaluation-flow.html)和[阶段启用判定图](../diagrams/16-release-decision.html)。完整摘要 T 的错误保存提案被拒绝后，在原预算内纠正并完成；独立单步用例另设身份与真值，避免混用主指标分母。计分与放行对照表、配对演算和推进伪代码说明固定计划、首次正确与最终成功、统计适用性、专项判分及阶段控制；设施缺证据不等于任务失败或已证实退化，旧 S／R／J 及来源删除规则保留。

17 已配[实施依赖图](../diagrams/17-implementation-dependencies.html)，从教学验收报告进入阶段与证据判断。阶段与证据对照表、恢复验收伪代码保留 M1–M4、C1–C9 分组目标、A1–A4 与专项、第二后端和真实协议边界；逐项成熟度矩阵由[附录 05](../reference/05-validation-profiles-and-traceability.md#maturity)维护；原 S／J 已关联、电脑已确认保存但内核未收到结果的工作包，展示故障切点、独立动作历史核验、原 R 继续及手机交付。统一报告与交接表明确同一候选配置、支持范围及尚无运行验收的状态。

旧图集与 HTML 阅读页随旧稿归档，保留独立重建入口。01 的 PNG 和[生成提示词](../diagrams/01-task-context.prompt.md)、02–17 的独立图示 HTML/SVG 均在新版图示目录维护，[图示构建说明](../../../scripts/architecture/README.md#新版正文插图)记录各自的生成与核对方式，不纳入旧版 HTML 构建。整合后的 HTML 阅读页由统一构建器从当前 Markdown 生成，维护方法见构建说明。

## 编辑复核条件

以下用于检查文档改写是否完整，与正文中的系统验收要求分别执行。

- 01–03 能连续读完，读者无需预先熟悉内部术语或查阅历史讨论。
- 关键机制在首次出现时说明具体问题、设计和作用，读者能解释为什么需要它以及系统怎样推进。
- 每个专题按内容需要组织节数；必要限制就近说明，典型局部异常与跨模块故障按层次展开。
- 图、表和代码沿用具体示例的对象与结果，新增条件和分支起点已明确交代；代码片段有行为解释与关键判断说明。
- 本表列出的原分册内容均有归属，12–14 的追加内容已纳入相应主题。
- 同一机制的规则和规格有明确维护位置，跨篇使用处已补足当前理解所需的结论和条件，引用不形成理解上的循环依赖。
- 能力、约束、专项、配置和验证条件逐项保留，状态描述与实际证据相符。
- 图示、正文术语、相对链接和查阅附录相互一致。
