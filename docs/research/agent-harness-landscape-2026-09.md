# Agent 与 Harness 的技术格局、发展趋势与能力要求

## 1. 核心结论

**先进 Agent 的竞争单位已经是“模型、Harness、工具与环境、评测配置”的组合。** 模型决定理解与决策能力的上限，Harness 决定这些能力能否在长任务、故障、权限约束和真实外部系统中稳定兑现。Harness-Bench 将这一问题作为独立评测对象；云厂商和模型厂商也开始把会话、执行环境、身份、记忆与治理作为产品交付。[^7][^17][^38][^42]

最值得投入的方向有五项：

1. **可恢复的任务运行。** 把任务状态、事件、产物与工作进程分开，允许大脑或执行节点退出后恢复，并对外部动作的未知结果进行核对。
2. **有证据的执行闭环。** 计划、工具调用和文字汇报都不能替代结果验证；应同时评估任务完成、副作用、成本和重复运行可靠性。
3. **可替换、可组合的能力边界。** 模型、记忆、执行后端与外部 Agent 通过稳定契约协作，协议适配不应侵入内核状态语义。
4. **在行动边界落实治理。** 权限、凭证、预算、扩展安装和高影响动作由可信运行时控制，不能只依赖模型识别风险。
5. **经验证的个性化与自进化。** 记忆应改善实际行动；系统改进应经过隔离评测、版本发布和回滚，而不以生成新 Skill 或修改提示词为成功标准。

以上是研究建议，不是行业统一标准。尤其应避免把“更多 Agent、更长上下文、更多工具、能持续运行”直接当作更先进的证明。多 Agent 收益依赖任务可分解性；安全识别不一定转化为安全行为；自动改进在不同任务域上的表现也不一致。[^2][^3][^12]

## 2. 范围、时效与术语

### 2.1 范围与证据边界

本报告面向通用 Agent 与 coding agent 的平台架构设计，资料截止日为 **2026 年 9 月 9 日**。重点覆盖 2026 年成果，保留少量 2025 年底的重要基础工作；最新纳入论文发表于 2026 年 9 月 1 日。厂商部分覆盖 OpenAI、Anthropic、Google、Microsoft、AWS、NVIDIA、阿里云、字节跳动／火山引擎、腾讯云、Salesforce，并补充 Cursor 和 GitHub 的软件工程产品形态。

证据分为三类：

| 类型 | 能支持的判断 | 不能直接支持的判断 |
| --- | --- | --- |
| 论文与可复现评测 | 特定数据集、版本、预算下的结果与失败机制 | 所有生产任务的成功率；不同评测之间的统一排名 |
| 官方文档、代码仓库 | 功能契约、实现边界、公开产品状态 | 已通过独立安全审计；达到特定生产 SLA |
| 官方工程文章、发布公告 | 厂商采用的设计、产品方向、宣布的能力 | 普遍因果结论；未开放功能已经可用 |

预印本统一按研究证据处理，不因出现在 arXiv 就视为经过同行评审。论文日期以首次提交及引用版本为准；网页抓取日期不能替代发布日期。动态文档和仓库按访问日快照解释，不推定其所有 SDK、地区、套餐和部署方式具有相同能力。未对商业产品开展账号实测，也未在统一环境复跑开源项目，因此不提供伪精确的综合评分或市场份额排名。

### 2.2 Harness 的两种常见含义

狭义的 **Agent Harness** 是围绕模型的运行机制：装配上下文、执行工具循环、处理事件和失败、决定继续或停止。广义的 **Harness engineering** 还包括让 Agent 能有效工作的环境：仓库规范、可检索知识、测试、产物契约、权限与反馈设施。两者相关，但不能把优化提示词、搭建评测运行器和交付生产运行时混为一件事。[^7][^8][^15]

本报告沿用仓库领域词汇：**Harness** 是连接记忆、大脑和执行三大系统的通用运行与集成体系；**Harness 内核** 提供任务生命周期与权限等公共保障；**运行节点** 可部署在端或云；**插件** 提供实现，**Skill** 描述知识与流程，**Agent** 持有目标、上下文与决策循环。行业产品中的同名对象需经适配映射，不能直接替代本项目领域概念。[^54]

## 3. 论文与研究进展

### 3.1 2026 年近期重点成果

下表中的“启示”与“边界”是分析判断；数值仅描述论文原实验。

| 研究及日期 | 主要证据／方法 | 对 Harness 的启示 | 证据边界 |
| --- | --- | --- | --- |
| **HEART / Tool Primitives**，2026-09-01 | 用自然语言包装工具接口，内部处理 schema；ToolFace 收录 25,519 个函数，运行时检索工具；以 Planner、Router、Verifier 协调调用。[^1] | 工具目录需要动态发现、参数适配、验证与反馈，不能无限堆进上下文 | 很新的预印本；自然语言接口仍可能解析错误，不能取代最终执行前的类型与权限校验 |
| **HarnessRisk**，2026-08-18 | 128 个沙箱用例，覆盖配置、扩展、运行、持久化、动作控制、事件恢复；评估 14 个模型与 Harness 组合。[^2] | 安全测试必须覆盖完整生命周期，而不只测单次提示注入 | 使用模拟服务与受控环境，不能将结果解释为现实攻击概率 |
| **Evo-Bench**，2026-08-10，v2 为 08-11 | 固定任务执行模型，考察另一模型修改 Harness 的能力；覆盖 Search、Office、General；最佳配置报告最高 16.6 分的绝对提升。[^3] | 自进化应区分“执行者”和“改进者”，并检验跨任务迁移 | Office 任务仍困难；存在早期饱和，不能假定迭代越多越好 |
| **AI-to-AI Code Reviews**，2026-08-21 | 从公开事件关联出 248,641 个至少接受一次 AI 评审的 AI 归因 PR；分析同产品与跨产品评审。[^4] | 生成与评审的分工已进入真实开发流程，值得建立独立验收通道 | 观察性研究；归因、时间戳及 PR 构成限制比较，不能据评论数量证明代码更好 |
| **Agent Team Work Zone**，2026-07-24，v2 为 07-30 | 面向 Claude Code Agent Teams，提出基于文件系统的持久化团队工作空间。[^5] | 团队任务需要可交接的公共状态与产物，不能只依赖对话记录 | 特定宿主的方案，不能直接证明跨设备恢复或通用多 Agent 协议成立 |
| **HarnessX**，2026-06-12，v3 为 07-23 | 组合有类型的 Harness 原语，用轨迹驱动演进，并将轨迹反馈到 Harness 与模型训练。[^6] | 把可调整的机制模块化，为比较、消融和版本化改进创造条件 | 结果依赖原语、基线和五项基准；论文整体收益不等于每个模块都有独立收益 |
| **Harness-Bench**，2026-05-27 | 106 项离线沙箱任务、5,194 条轨迹，在共享环境和预算下比较模型与 Harness 配置。[^7] | 成功率必须附带完整运行配置，保留产物、轨迹和验证器输出 | 保留各 Harness 原生行为，比较的是整体配置；未隔离每一项机制的因果贡献 |
| **Code as Agent Harness**，2026-05-18 | 综述代码如何承载推理、行动、环境建模、反馈和多 Agent 协作。[^8] | 代码既可以是产物，也可以是组织工具调用和验证的运行媒介 | 综述提供分类框架，不是新系统超过所有基线的实验证明 |
| **Natural-Language Agent Harnesses**，2026-03-26，v2 为 05-18 | 用可编辑文档表达运行策略，由共享 IHR 解释成调用、交接、状态变更、验证门和产物契约。[^9] | 可声明的策略与可信执行机制可以分离，提高可读性和可消融性 | 自然语言可编辑不等于确定执行；解释器仍是必要运行基础 |

### 3.2 上下文、记忆、多 Agent 与真实环境

**上下文外置。** Recursive Language Models 将长输入视为外部环境，由模型通过程序读取片段、分解并递归调用。其 2026 年 5 月版本说明了“上下文作为可查询对象”的可行方向。对通用 Harness 的启示是保留原始材料与索引，按需形成工作上下文；论文的长输入处理结果不代表任意业务任务拥有无限可靠记忆。[^10]

**从记住事实到正确行动。** ACL 2026 的 Mem2ActBench 以 400 个工具使用任务检验记忆如何影响工具选择和参数。研究发现，被评估系统主动利用长期记忆仍存在不足。因此，个性化验收应检查是否在合适的动作中应用偏好，而不仅是能否回答“用户喜欢什么”。[^11]

**多 Agent 的收益有条件。** Towards a Science of Scaling Agent Systems 的 v3 对 260 个配置、六项基准进行受控比较，观察到工具密集任务的协调开销以及架构与任务不匹配时的退化。它支持根据可分解性选择协作方式，而非默认“复杂任务必须上团队”；其预测关系也不能当成普适扩展定律。[^12]

**coding agent 需要真实终端环境。** Terminal-Bench 2.0 包含 89 个难度较高的终端任务，覆盖真实工作流中的操作与执行问题。它适合补充“能生成正确代码”之外的环境操作评价，但不能代替长期维护、跨仓库变更和部署后的运行质量评价。[^13]

**手机操作需要用户与工具混合交互。** MobileWorld 包含 20 个应用中的 201 项任务，强调跨应用、长流程、用户交互与 MCP 增强；通过可观察后端验证实际状态。其意义是 GUI 行动可以与 API、询问用户协同，而不是始终只能模拟点击。模拟应用中的结果仍需在目标手机系统和真实应用上补测。[^14]

### 3.3 三组不能忽略的反证

1. **检测出风险，不代表动作被阻止。** HarnessRisk 中存在风险识别较高而攻击仍成功的配置。安全评价必须检查动作和持久化状态，不能只检查模型是否说出了警告。[^2]
2. **生成更复杂的 Harness，不代表改进可迁移。** Evo-Bench 中各领域收益不同；Harness-Bench 又表明模型与 Harness 的组合会改变行为。改进需要保留集和跨模型回归。[^3][^7]
3. **多 Agent 活跃，不代表产物更可靠。** 受控协作实验呈现正负两类结果，真实 PR 评审研究也不是缺陷下降的因果试验。应以经验证的结果和总成本决定是否增加 Agent。[^4][^12]

## 4. 开源项目的分层比较

### 4.1 运行内核、开发框架与集成 Harness

这里的“适用位置”是架构参考建议。公开仓库的存在不等于全部产品开源，框架自述的 production-ready 也不作为独立验证结论。选型时仍需固定依赖版本并检查该版本的许可证、维护和部署约束。

| 项目 | 公开能力与定位 | 适用位置／需要补齐的内容 |
| --- | --- | --- |
| **LangGraph** | 以 checkpointer 保存线程图状态，以 store 保存跨线程数据。[^55] | 可参考状态持久化与恢复机制；检查点本身不保证外部副作用只发生一次 |
| **Deep Agents** | 建立在 LangChain／LangGraph 上的集成 Harness，包含文件系统、上下文管理、子 Agent、记忆、Skill 和人工介入。[^19] | 通用长任务参考基线；工具和沙箱必须提供实际隔离，不能依赖模型自我约束 |
| **OpenHands Software Agent SDK** | 将 Agent、工具、会话、工作空间、事件及 Agent Server 分开；支持本地和临时远程工作空间。[^20] | coding agent 与远程执行边界的重点参考；定时、Webhook 和运行分派另由 automation 项目负责 |
| **Pi** | 当前仓库为 `earendil-works/pi`；拆分统一模型 API、Agent core、coding CLI 等包；旧 `badlogic/pi-mono` 地址重定向。[^21] | 轻量内核与可扩展宿主参考；README 明确没有内置文件、进程、网络、凭证权限限制，需要外部沙箱 |
| **OpenCode** | 开源 coding agent，强调可接不同模型提供方的开发体验。[^22] | 模型可替换及终端产品体验参考；不能直接视为跨节点任务平台 |
| **Google ADK** | 代码优先的 Agent 开发、评测和部署工具包。[^27] | Google 生态和 Agent 组合参考；框架接口与云托管服务应分别评价 |
| **Microsoft Agent Framework** | 支持 Python 与 .NET 的 Agent、多 Agent 工作流、编排和部署。[^28] | 企业代码集成与工作流参考；要逐包检查托管与自托管支持状态 |
| **Strands Harness SDK** | 原 `strands-agents/sdk-python` 已重定向至 `strands-agents/harness-sdk`，当前定位明确为可控制完整 Harness 的 SDK。[^29] | AWS 生态与自建运行机制的参考；不能将 SDK 与 AgentCore 服务视为同一个产品 |
| **AgentScope** | 面向 Agent 构建、运行与可观察性的开源框架。[^26] | 国产生态与 Agent 开发接口参考；跨节点断连恢复要由具体实现与契约证明 |

### 4.2 通用助手、工作流平台与执行基础设施

| 项目 | 公开能力与定位 | 研究价值及边界 |
| --- | --- | --- |
| **DeerFlow 2.0** | 由研究框架重写为通用 Harness，围绕沙箱、文件系统、记忆、Skill 与子 Agent 组织长任务。[^25] | 通用研究、编码和内容产物的整合参考；不要把 1.x 架构当作当前设计 |
| **Hermes Agent** | 强调持续记忆、用户画像、Skill、工具及消息入口。[^23] | 个人长期助手和经验沉淀的参考；“self-improving”定位需通过前后评测证明有效 |
| **OpenClaw** | 面向多平台使用的行动型个人 Agent 项目。[^24] | 端侧入口、持续服务和集成体验参考；个人助手的能力范围不自动等于企业隔离边界 |
| **NVIDIA OpenShell** | 面向自主 Agent 的运行时基础设施。[^30] | 执行隔离、受策略控制的运行环境参考；应与规划和任务决策分层 |
| **Dify** | 集成 Agent 工作流、RAG、模型和工具的应用平台，可用于云、VPC 或自托管部署。[^31] | 业务编排和运营界面参考；较完整的平台并不意味着适合作为小型跨设备内核 |
| **Volcengine AgentKit SDK** | 公开 Python SDK 和 CLI，面向 AgentKit Runtime 部署。[^46] | 国产托管环境接入参考；SDK 开源不代表托管控制平面可自行部署 |

### 4.3 优先阅读与比较顺序

**建议先做三个互补基线，而不是挑一个“大而全”的赢家：**

- 用 **Pi 或最小自建循环**建立可理解的决策与工具执行基线，测出最少机制能做到什么。
- 用 **Deep Agents／LangGraph**比较长任务状态、上下文管理和委派的增益与开销。
- 用 **OpenHands SDK 加隔离执行后端**比较 coding 任务的环境一致性、远程工作空间和产物验证。

随后按能力接入 Hermes／DeerFlow 的个性化与产物体验、ADK／Microsoft／Strands／AgentScope 的适配样本、OpenShell 的执行边界。框架融合应发生在任务或能力契约上，避免多个框架同时拥有同一任务的生命周期。

## 5. 主要厂商的最新产品形态

### 5.1 模型厂商与云平台

| 厂商 | 截止日公开产品／能力 | 已核实的状态与注意事项 | 对 Harness 的信号 |
| --- | --- | --- | --- |
| **OpenAI** | Codex、ChatGPT Work；开发侧 Agents SDK 与 Sandbox Agents 文档。[^32][^33][^34] | 官方文档已将 Agent Builder 标为弃用，计划 **2026-11-30** 关闭；ChatKit 仍可用，不能据此推断 Agents SDK 也弃用。[^35] | 通用工作与软件工程入口并进，SDK、执行环境和产品 UI 分层 |
| **Anthropic** | Claude Code、Cowork、Claude Managed Agents。[^36][^37][^56] | Managed Agents 当前仍为 **beta**；提供持久化事件、托管或自托管沙箱及运行中干预；MCP tunnels、dreaming 另属受限研究预览。[^36] | Harness 本身成为可消费服务，但自主性与数据状态需要显式控制 |
| **Google** | Gemini Enterprise app、Agent Platform、ADK；Antigravity 开发入口。[^38][^39] | 平台为 Vertex AI 的演进；**2026-07-29** 公告扩展 Runtime、Memory Bank、Identity 等可用能力，声明 Runtime 可连续运行最长七天。[^38] | 从模型平台走向 Agent 生命周期平台；运行上限并非任务成功率保证 |
| **Microsoft** | Copilot Studio／Microsoft 365 Copilot、Foundry、Agent Framework、Agent 365。[^40][^41] | Agent 365 商业版于 **2026-05-01 GA**；08-25 文档称 Foundry Hosted Agents 已 GA，同时当前 Python 自托管包仍为预发布。[^40][^41] | Agent 身份与治理成为独立控制平面，必须按组件区分成熟度 |
| **AWS** | Bedrock AgentCore 提供 Runtime、Memory、Gateway、Identity、工具、Observability、Policy 和 Evaluations 等运行能力。[^42][^43] | 官方更新记录显示 Policy 与 Evaluations 在 **2026 年 3 月**进入 GA，并继续扩大框架支持。[^42] | 托管价值覆盖运行、策略与评测；策略可在 Agent 代码之外执行 |
| **NVIDIA** | NemoClaw 参考栈与 OpenShell 运行基础设施，涉及推理接入、网络策略、集成与生命周期操作。[^30][^50] | 当前文档区分受支持运行时与仅处于适配验证阶段的候选 Agent，不能把所有候选项视为正式支持 | 本地／自有基础设施中的执行约束与模型接入获得独立产品位置 |

### 5.2 国内平台与企业应用厂商

| 厂商 | 截止日公开产品／能力 | 状态与证据边界 | 对 Harness 的信号 |
| --- | --- | --- | --- |
| **阿里云** | 百炼 Agent Studio 的 Managed Agents；事件流含工具调用、审批与结果事件。[^44] | 计费文档标明 **2026-08-17** 开始商业化计费；该日期来自官方索引正文，页面直接访问不稳定，需在采购时复核。[^45] | 托管运行时与可恢复的审批交互进入国内平台供给 |
| **字节跳动／火山引擎** | AgentKit Runtime 及公开 SDK；DeerFlow 2.0 为开源 Harness 路线。[^25][^46] | 官方 SDK 可核实部署入口；部分云文档获取不稳定，本报告不据此确认全部子功能的 GA 状态 | 开源开发体验与托管运行基础设施并行，二者交付边界不同 |
| **腾讯云** | ADP 4.0 国际版，官方介绍定位企业 AgentOps 平台。[^47] | 官方资料将其与 2026 WAIC 发布关联；国际版信息不能直接套用到国内版套餐和地区能力 | 从构建 Agent 扩展到企业运营与治理 |
| **Salesforce** | Agentforce 生态；与 Anthropic 合作的 Claudeforce／Salesforce in Claude。[^48] | 当前产品页仍写 **pilot customers**，计划 **2026 年 9 月 open beta**；不能因已进入九月就视为全量开放 | 企业数据、业务规则与动作可以通过插件进入通用 Agent，价值不只在聊天入口 |

国内产品比较没有将“模型可用”“SDK 可用”“运行时商用”和“全部治理功能正式开放”合并为一个标签。国产环境落地还应实测中文工具描述、网络访问、地域部署、端侧设备支持和既有系统权限映射。

### 5.3 coding agent 产品的共同变化

**Cursor** 的 2026 年 6 月工程文章将云 Agent 描述为独立虚拟机上的长期运行系统，重点讨论执行持久性、会话与机器解耦和环境自修复。启示是开发环境本身决定任务能否推进；其内部使用情况不是所有团队生产效率的代表样本。[^18]

**GitHub** 的当前文档提供以 Markdown 定义 Agentic Workflows 的路径，并允许选择执行用的 coding agent。这使 Agent 进入仓库事件、自动化和评审流程；但“编写工作流”“允许工具操作”“允许最终合并”仍是不同层次的授权与责任。[^49]

**Claude Code 与 Codex** 则应同时作为 Agent 宿主和开发产品观察。比较时应检查其上下文、扩展、任务交互、环境隔离和评审机制，而不是仅用默认模型名称替代系统比较。[^33][^56]

### 5.4 厂商竞争的三个层面

综合上述资料，可将竞争分成三层：

1. **用户入口：** 谁承接任务、持续展示进展、允许用户干预并交付可用产物。
2. **运行基础设施：** 谁管理状态、计算资源、凭证、工具、记忆、可观察性与恢复。
3. **业务执行与治理：** 谁拥有业务语义、可授权动作、系统记录与责任边界。

这一分层解释了为何基础模型厂商、云平台和企业软件公司能够同时扩张 Agent 产品，而不必提供完全相同的系统。对于开源 Harness，更有长期价值的差异化可能是跨实现的任务契约、端云能力协作和可验证替换性；“再包装一次模型 API”较容易被平台吸收。此处是战略判断，不是已被市场数据验证的预测。

## 6. 发展趋势与设计取舍

### 6.1 从会话循环到持久化任务系统

Anthropic 的 Managed Agents 工程设计将 session、Harness 和 sandbox 解耦；Cursor 也强调将会话状态与执行机器分开。两条工程路线支持相同判断：**任务不应依赖某一个进程或容器始终活着。**[^17][^18]

建议将任务状态、可重建的工作上下文、执行环境和用户界面分别管理。还需区分三种恢复：恢复模型上下文、恢复计算环境、核对外部动作结果。前两者恢复成功时，第三者仍可能不确定；这正是跨端任务不能简单“重发最后一次请求”的原因。

### 6.2 从长上下文到可查询的证据与状态

RLM 的外部上下文和 LangGraph 对线程状态／跨线程记忆的区分，说明“全部塞回提示词”不是唯一解法。[^10][^55] 建议至少区分：原始事件、当前任务状态、工作摘要、用户记忆、可复用经验、产物与证据。

摘要是加速读取的视图，不应成为唯一历史。记忆删除也不能只删一个向量索引项，应明确其在摘要、缓存、同步副本和后续任务中的使用边界。具体保留和删除策略需要由项目契约定义。

### 6.3 从工具堆叠到动态发现与可验证调用

HEART 探索大规模工具检索与接口适配；MCP 当前规范提供标准化工具与上下文接入。[^1][^51] 设计重点由“接入多少工具”转向“是否在正确时刻暴露正确能力，并得到可核对结果”。

建议为工具区分只读、可幂等写入、不可幂等写入和不可逆动作。模型侧可以采用自然语言或代码编排，但执行边界仍需核验目标资源、参数、授权和预期副作用。动态工具元数据与插件文档也不能自动获得可信指令地位。

### 6.4 从单一 Agent 到按任务结构选择协作

多 Agent 更适合边界清楚的并行调查、独立实现和独立验证；强串行依赖、频繁共享可变状态的任务可能因协调增加成本。[^12] 建议让委派显式带上目标、输入快照、输出契约、预算、截止条件和权限范围。

内部 Agent 可由内核管理生命周期；外部 Agent 应经任务协议交互。父任务取消不应只停止等待，还需要处理子任务、远程执行和已产生的外部副作用。

### 6.5 从自我声明完成到独立验收

Anthropic 的长任务实践先强调增量推进和明确特性列表，后引入生成与评估分工。[^15][^16] 其可迁移价值是“完成判定有独立依据”，不在于必须固定使用两个或三个 Agent。

coding 任务应交付 diff、测试和运行证据；研究任务应交付来源和引用对应关系；GUI 任务应验证动作后的界面或后端状态。LLM 评审可补充主观判断，但不能替代适用的确定性检查。

### 6.6 从附加 guardrail 到执行边界治理

AgentCore Policy 将工具调用策略放在 Gateway 层，HarnessRisk 则展示从配置到恢复的风险链。[^2][^43] 建议将政策执行与模型决策分开，确保换模型、装 Skill 或委派外部 Agent 后，权限保障仍成立。

授权应绑定具体主体、动作、资源、期限与参数范围。一次审批不能被复用于目标已经变化的另一次动作；凭证应尽可能通过代理使用，而不是成为模型可读文本。

### 6.7 从个人记忆到可治理的行动记忆

Hermes 展示个人记忆与 Skill 的产品方向，Mem2ActBench 将评价推进到工具参数和行为层。[^11][^23] 建议分别管理事实、偏好、推断和经验，记录来源、时间、置信度、适用任务及用户控制。

记忆质量不仅是检索命中率，还包括适用时采用、不适用时不采用、冲突时澄清，以及用户删除后不再影响行为。读取某项数据不应自动产生永久存储和跨端同步的许可。

### 6.8 从自动修改到受控自进化

HarnessX 与 Evo-Bench 为轨迹驱动改进提供近期研究依据，但没有证明任意在线系统可无监督持续改好自己。[^3][^6] 建议把自进化实现成有版本的改进流水线，并隔离运行者与评测控制面。

记忆、Skill、执行策略、插件代码和模型权重分别需要不同成本的验证。候选改进不能修改自己的验收标准；结果应同时检查能力收益、安全回归、成本与跨任务泛化。

### 6.9 协议分层趋同，但互操作仍需测试

截至访问日，MCP `latest` 指向 **2026-07-28**；A2A 文档已提供 **v1.0** 材料。MCP 的职责以工具和上下文为主，也存在长任务等可选扩展；A2A 面向独立 Agent 协作；Agent Skills 描述可分发的操作知识。不能简单断言“MCP 只能同步调用工具”，也不能因两端都写着支持 MCP 就推定扩展完全兼容。[^51][^52][^53]

建议使用能力协商和协议适配器：明确版本、扩展、错误、取消、恢复和授权传播。A2A 的 Agent Card 中用于声明能力的 skill，也不应与可安装的 `SKILL.md` 内容包混为一谈。

### 6.10 更强模型推动策略简化，运行保障仍需保留

模型升级后，某些为旧模型补短板的固定规划与提示策略可能变成额外成本；Anthropic 的新一轮 Harness 实践也讨论了随模型改进减少复杂度。[^16] 建议保持策略层可替换，并定期运行消融测试。

应优先简化的是未经验证的策略堆叠，而任务持久化、权限、执行记录、取消、预算、结果证据和故障恢复仍是系统责任。模型更聪明并不能让网络断连和外部写入的不确定性消失。

## 7. 先进 Harness 的参考架构

以下是研究提出的参考结构，不是已批准的实现方案。逻辑边界对齐仓库三系统，部署位置不固定。[^54]

```text
用户／定时器／外部事件
          │
          ▼
Harness 内核
  任务契约、状态与事件、调度、预算、取消、授权、恢复、版本记录
          │
          ├── 大脑系统：理解、规划、决策、上下文装配、模型路由
          │       └── 内部 Agent／经适配接入的外部 Agent
          ├── 记忆系统：用户事实与偏好、经验、来源、检索、纠正与删除
          └── 执行系统：能力发现、动作准入、工具／沙箱／浏览器／手机
                      └── 运行节点：端侧、云端、自有网络中的能力提供方

公共设施：产物与证据存储、可观察性、评测、扩展注册、版本与发布控制
```

### 7.1 三个关键区分

**任务事件与模型上下文。** 原始事件可按策略持久化；大脑接收的是与本次决策相关的上下文视图。切换大脑实现不应要求迁移到该提供方特有的唯一历史格式。

**行动请求与行动结果。** 大脑提出动作；执行系统校验授权与前置条件后实施；内核记录请求、执行回执、结果证据和未知状态。一次 HTTP 超时不能直接判成动作失败，也不能直接重试。

**业务完成与运行终止。** 模型停止输出、预算耗尽、用户取消、依赖失联和成果验收通过是不同事实。停止运行不能自动把任务标记为成功。

### 7.2 建议的最小契约

| 契约 | 建议字段／语义 | 关键保障 |
| --- | --- | --- |
| Task | ID、父任务、目标、输入／产物引用、状态、预算、期限、授权引用 | 状态可恢复；重复分派可识别；完成需证据 |
| Capability | 名称与版本、输入／输出 schema、所在节点、可用性、效果类型、成本信息 | 能力可发现、可替换；声明不等于实际获得授权 |
| Action | 操作 ID、目标、参数摘要、前置条件、授权、幂等键或核对方式 | 重试遵循动作语义；未知结果独立表示 |
| Observation | 对应动作／任务、时间、来源、结果、错误、可信度与产物引用 | 成功回执与结果确认可区分 |
| Memory | 类型、内容引用、来源、时间、置信度、用途、主体、保留策略 | 读取、保存、同步、删除分别受控 |
| Delegation | 目标、输入范围、输出契约、预算、权限上界、状态与取消通道 | 权限收缩；父子任务和外部 Agent 有关联记录 |
| Extension | 插件／Skill／Agent 类型、版本、依赖、来源、能力与所需权限 | 升级可审查；版本可固定；撤销后可失效 |

这些契约刻意不指定语言、数据库、消息中间件或设备系统。应先证明不同实现能遵守同一行为约定，再确定技术栈。

## 8. 功能要点与验收清单

优先级含义：**P0** 为可信执行与内核基线；**P1** 为通用性和长任务核心竞争力；**P2** 为在基线成立后的优化与生态扩展。以下是建议的建设顺序，不表示本项目已批准范围或降低某项既有目标的重要性。

### 8.1 任务运行与执行可靠性

| ID | 优先级 | 功能要点 | 可观察的验收证据 |
| --- | --- | --- | --- |
| F01 | P0 | 持久化任务生命周期 | 进程重启后能区分运行、等待输入、阻塞、取消、失败与成功 |
| F02 | P0 | 事件记录与产物关联 | 从任务定位关键工具调用、授权、错误与最终文件；记录有顺序与去重标识 |
| F03 | P0 | 外部动作结果核对 | 服务已写入但回执丢失时，不产生重复写入；无法核对时显式待处理 |
| F04 | P0 | 取消、暂停、干预 | 用户取消可传到执行端；已产生副作用有独立报告；新指令不被恢复过程覆盖 |
| F05 | P0 | 预算、超时和资源上限 | 达到任务／子任务限制时停止新增工作；不靠重复重试突破上限 |
| F06 | P0 | 独立完成验证 | 只有满足产物与结果条件才判完成；无证据时报告未确认 |
| F07 | P1 | 环境可重建 | 从已声明环境重新运行同一任务，依赖与工具版本可辨认 |
| F08 | P1 | 节点能力声明与路由 | 更换执行节点后同一能力契约仍成立；失联节点不继续接新任务 |
| F09 | P1 | 端云断连与恢复 | 重连后核对任务、授权和动作结果；本地续跑与依赖等待分别测试 |
| F10 | P1 | 定时与事件触发 | 重复投递不重复建立业务动作；时区、错过触发、重叠执行策略可测试 |

### 8.2 上下文、记忆与工具

| ID | 优先级 | 功能要点 | 可观察的验收证据 |
| --- | --- | --- | --- |
| F11 | P0 | 工作上下文与原始记录分离 | 摘要遗漏信息时仍能追溯原始证据；恢复后不丢失未完成目标 |
| F12 | P1 | 动态工具与 Skill 发现 | 大目录下能定位合适能力，且上下文开销可测；查不到时不伪造工具 |
| F13 | P0 | 工具输入、输出、错误契约 | schema 不合法被拒绝；空结果、拒绝和执行失败不被混成成功 |
| F14 | P1 | 联网问答证据链 | 关键结论对应实际取得的来源；能处理来源冲突、过期与不可访问 |
| F15 | P1 | 分类型、可追溯记忆 | 事实、偏好、推断与经验具有不同标记；能定位其来源及有效范围 |
| F16 | P1 | 记忆驱动行动 | 用户既有偏好在适用任务中正确影响工具参数；无关任务不误用 |
| F17 | P0 | 记忆与数据使用控制 | 读取许可不自动变成保存许可；纠正、删除、用途限制与同步策略有实测 |
| F18 | P1 | GUI 观察—动作—验证闭环 | 点击回执和任务成功分别判断；页面变动后重新观察，支持用户接管 |
| F19 | P1 | API、代码与 GUI 协同 | 同一目标可换执行路径；权限和证据规则不随工具类型弱化 |

### 8.3 治理与可观察性

| ID | 优先级 | 功能要点 | 可观察的验收证据 |
| --- | --- | --- | --- |
| F20 | P0 | 最小权限和执行端校验 | 换模型、装 Skill、委派 Agent 后仍不能扩大权限 |
| F21 | P0 | 隔离与凭证代理 | 沙箱访问范围、网络出口和密钥不可读性分别验证 |
| F22 | P0 | 参数绑定的审批 | 目标或关键参数变化后旧审批不再授权；单次许可不可重复使用 |
| F23 | P0 | 不可信内容隔离 | 网页、邮件、工具返回和外部 Skill 不能更改系统政策或默认权限 |
| F24 | P0 | 运行成本与质量追踪 | 可归因到任务、模型、工具、节点与版本；同时显示失败和重试成本 |
| F25 | P1 | 身份、租户与生命周期治理 | Agent 有所有者；撤销访问后正在等待的工作不能继续使用旧许可 |
| F26 | P1 | 事故恢复 | 能隔离有问题的扩展、保存必要证据、修复受污染状态并验证修复 |

### 8.4 协作、扩展与自进化

| ID | 优先级 | 功能要点 | 可观察的验收证据 |
| --- | --- | --- | --- |
| F27 | P1 | 内部／外部 Agent 任务契约 | 两类 Agent 都通过委派、进度、取消、结果与失败检查 |
| F28 | P1 | 有边界的并行协作 | 产物和修改所有权清晰；冲突、超预算、失联子任务能收敛 |
| F29 | P1 | 多模型与组件替换 | 至少两种实现通过同一契约集，替换后内核保障不变 |
| F30 | P1 | 协议协商与一致性测试 | 对 MCP／A2A 的版本、可选扩展与错误行为开展双端测试 |
| F31 | P1 | 扩展版本、依赖与撤销 | 插件、Skill、Agent 分别注册；升级失败可恢复旧版本 |
| F32 | P1 | 评测数据与运行版本绑定 | 每条结果可定位模型、提示、扩展、环境、预算和验证器版本 |
| F33 | P2 | 轨迹驱动改进提案 | 可解释改动来自哪些失败；改进只产生候选版本，不直接覆盖生产 |
| F34 | P2 | 隔离评测、灰度与回滚 | 保留任务集通过，成本与安全无不可接受回归；线上退化能撤回 |
| F35 | P2 | 自适应模型与协作策略 | 同预算比较证明路由／委派比简单基线更有收益，并可禁用策略 |

## 9. 评价先进性的指标体系

### 9.1 指标必须同时覆盖结果与代价

建议采用以下指标集，而不是单一“任务成功率”：

| 维度 | 建议指标 | 解释注意事项 |
| --- | --- | --- |
| 有效性 | 经独立验证的任务成功率；满足全部约束的产物比例 | 模型自评、PR 创建、文件生成不是最终成功 |
| 稳定性 | 相同条件多次执行的成功分布；连续成功比例 | 报告重复次数与区间，不只挑最好一条 |
| 恢复 | 各故障点的恢复成功率、恢复时间、未知结果比例 | 网络、进程、节点、提供方故障要分开统计 |
| 副作用 | 重复写入、越权动作、无关修改、信息泄露的发生数 | 安全与业务成功分别记分 |
| 效率 | 每个经验证成功任务的总成本、端到端时延与人工时间 | 包含失败尝试、环境、工具、评测和人工接管成本 |
| 记忆 | 行动参数正确率、冲突处理、删除后误用率 | 不用事实检索率替代个性化效果 |
| 通用性 | 模型／记忆／执行后端替换通过率，协议一致性结果 | 支持接入不等于行为等价 |
| 自进化 | 保留集收益、安全回归、成本变化、迁移效果 | 优化集提升不能代替未见任务提升 |

**推荐成本口径：** 某配置全部运行的模型、工具、环境及验收费用，加可量化的人工处理费用，再除以经验证成功的任务数。报告同时列出原始成本和失败率；若成功任务为零，不能用零成本或无穷小成本掩盖失败。

### 9.2 最小评测组合

推荐将以下测试分层开展，数量与阈值由目标设备、模型和成本预算确定，不在调研阶段虚构统一合格线：

1. **契约测试：** 状态、权限、错误、取消、产物 schema、插件版本兼容。
2. **受控任务测试：** 联网研究、代码修改、文档处理、偏好驱动行动、跨应用 GUI。
3. **故障注入：** 进程终止、回执丢失、节点失联、令牌过期、用户撤权、重复事件。
4. **对抗测试：** 配置诱导、恶意 Skill、网页指令、记忆污染、伪造审批、恢复过程污染。
5. **开放环境测试：** 在真实账号和设备的授权测试范围内，验证真实后端状态及人工接管。

每次比较固定任务输入、初始环境和预算；模型与 Harness 版本都进入实验配置。先建立单 Agent 基线，再比较记忆、检索、委派和改进策略；不要在换模型、换预算和换验证器的同时把收益全部归因于 Harness。

## 10. 对现有七项项目目标的映射

本节仅映射研究结论，不将目标草案中的待确认事项变为已确认要求。[^54]

| 项目目标 | 研究支持 | 最值得补充的设计问题 | 对应功能 |
| --- | --- | --- | --- |
| G1 端云协作 | 会话与执行资源解耦是明确工程方向 | 节点租约、断连期间可继续范围、动作未知结果、跨端撤权如何定义 | F01–F10、F20、F25 |
| G2 联网问答 | 工具检索、证据外置与产物验收可组合 | 信息获取失败、来源冲突、时效和引用错误是否有统一表达 | F11–F14、F06 |
| G3 深度个性化 | 研究已从记忆检索转向记忆驱动行动 | 推断与事实如何分开；纠正／删除如何传播到后续执行 | F15–F17、F32 |
| G4 手机 GUI | 混合 GUI、API 与用户交互有直接基准参考 | 观察是否过期、动作是否成功、用户接管后的状态如何恢复 | F18–F19、F03–F04 |
| G5 三系统与内核 | 可替换大脑、独立状态与执行环境符合工程演进 | 谁拥有权威任务状态；哪些信息是视图；替换是否保留运行保障 | F01–F02、F11、F29 |
| G6 标准协议与扩展 | MCP、A2A、Skills 分层生态已经形成 | 协议版本与扩展如何协商；插件、Skill、Agent 的生命周期如何区分 | F12、F27–F31 |
| G7 自进化 | HarnessX／Evo-Bench 提供改进与评价方向 | 训练／优化／保留任务如何隔离；谁能改变门槛；如何灰度与回滚 | F32–F35 |

**最有区分度的潜在定位：** 将个人端侧体验、企业式执行约束与跨实现任务契约统一到一个可替换内核中。现有产品分别在开发体验、云托管、长期助手或业务平台上较强，但公开资料尚不足以证明某一个项目完整满足本仓库七项目标，尤其是手机端云恢复、用途受控的记忆同步与跨组件自进化。

## 11. 建议建设顺序

### 第一阶段：建立可信运行基线

先定义 Task、Action、Observation 与授权契约，交付最小大脑、记忆和执行参考实现。重点证明任务持久化、取消、预算、权限、产物与未知动作结果的处理；同时保留一个简单单 Agent 基线用于后续比较。

### 第二阶段：证明可替换和跨节点成立

引入第二种模型和执行后端，验证端云断连、能力路由与状态核对。增加联网证据链与手机 GUI 参考任务，采用按能力验收，避免用一个成功演示替代恢复和权限测试。

### 第三阶段：完善记忆、协作与扩展

加入可追溯个性化、内部与外部 Agent 适配、Skill／插件生命周期及协议一致性测试。只有在同预算比较证明收益后，才扩大并行 Agent 数量与自动任务范围。

### 第四阶段：开放受控自进化

从离线轨迹分析和 Skill 改进提案开始，建立保留集、版本发布、效果监测与回滚。继续遵守项目已确认的规则：记忆在授权范围内更新；Skill／配置通过评测后逐步启用；插件与内核代码通过可审查补丁交由维护者确认发布。[^54]

## 12. 仍需实证的问题

- **长期可靠性：** 公开材料对数分钟、数小时任务的证据多于跨周真实业务维护；最长运行时间不能直接外推可靠自治时间。
- **端侧与手机：** 常见云 Harness 设计不能自动解决手机驱动限制、系统权限、前台状态、断连和用户抢占。
- **记忆治理：** “支持长期记忆”常未充分说明用途限制、删除传播和污染恢复的端到端语义。
- **自进化泛化：** 新预印本结果积极，但仍需跨任务、跨模型、固定成本的独立复现；不能默认开放生产自修改。
- **协议互操作：** 接入相同协议不代表权限、错误与恢复语义兼容，需要双端一致性测试。
- **开源交付：** 本报告不构成代码审计；采用任何项目之前应固定 commit／release 并检查许可证和依赖范围。
- **产品新鲜度：** 测试版、地区及套餐能力持续变化；表中明确的 beta／pilot 不因日期推进而自动升级为 GA。

## Sources

以下编号同时用作正文脚注。动态页面和仓库统一访问于 **2026-09-09**；未标发布日期者为持续维护资料。论文采用注明的版本，正式会议资料优先使用出版页面。少数访问受限资料的边界单独标注。

[^1]: Haibo Jin 等，[*Harness Engineering in LLM Tool Use via Agent-Native Reusable Tool Primitives*](https://arxiv.org/abs/2609.01736v1)，arXiv，2026-09-01，v1；[全文](https://arxiv.org/html/2609.01736v1)。用于工具原语、工具发现与 HEART 结构；预印本。
[^2]: Yajing Bai 等，[*HarnessRisk: A Lifecycle-Oriented Benchmark for Agent Harness Safety*](https://arxiv.org/abs/2608.17597v1)，arXiv，2026-08-18，v1；[全文及限制](https://arxiv.org/html/2608.17597v1)。用于生命周期安全与风险识别边界；预印本。
[^3]: Lisheng Huang 等，[*Evo-Bench: Can Language Models Improve Agent Harness?*](https://arxiv.org/abs/2608.09096v2)，arXiv，首发 2026-08-10，v2 2026-08-11。用于自进化评测与任务域差异；预印本。
[^4]: Niruthiha Selvanayagam、Taher A. Ghaleb，[*AI-to-AI Code Reviews of GitHub Pull Requests*](https://arxiv.org/abs/2608.21311v1)，arXiv，2026-08-21。用于真实 AI 评审活动及观察研究限制；预印本。
[^5]: Shouren Wang，[*Agent Team Work Zone: An Automated, Persistent Workspace for Long-Lived Claude Code Agent Teams*](https://arxiv.org/abs/2607.22917v2)，arXiv，首发 2026-07-24，v2 2026-07-30。用于持久化团队工作空间方向；预印本。
[^6]: Tingyang Chen 等，[*HarnessX: A Composable, Adaptive, and Evolvable Agent Harness Foundry*](https://arxiv.org/abs/2606.14249v3)，arXiv，首发 2026-06-12，v3 2026-07-23。用于可组合机制与轨迹驱动改进；预印本。
[^7]: Yilun Yao 等，[*Harness-Bench: Measuring Harness Effects across Models in Realistic Agent Workflows*](https://arxiv.org/abs/2605.27922v1)，arXiv，2026-05-27；[全文](https://arxiv.org/html/2605.27922v1)。用于配置级评测与实验限制；预印本。
[^8]: Xuying Ning 等，[*Code as Agent Harness*](https://arxiv.org/abs/2605.18747v1)，arXiv，2026-05-18。用于领域分类；综述预印本。
[^9]: Linyue Pan 等，[*Natural-Language Agent Harnesses*](https://arxiv.org/abs/2603.25723v2)，arXiv，首发 2026-03-26，v2 2026-05-18。用于声明式 Harness 与共享运行时；预印本。
[^10]: Alex L. Zhang、Tim Kraska、Omar Khattab，[*Recursive Language Models*](https://arxiv.org/abs/2512.24601v3)，arXiv，首发 2025-12-31，v3 2026-05-11。用于外部上下文和程序化访问。
[^11]: Yiting Shen、Kun Li、Wei Zhou、Songlin Hu，[*Mem2ActBench: A Benchmark for Evaluating Long-Term Memory Utilization in Task-Oriented Autonomous Agents*](https://aclanthology.org/2026.acl-long.370/)，ACL 2026，2026-07，8173–8190 页。用于记忆驱动工具行动的评价。
[^12]: Yubin Kim 等，[*Towards a Science of Scaling Agent Systems*](https://arxiv.org/abs/2512.08296v3)，arXiv，首发 2025-12-09，v3 2026-04-08。用于多 Agent 架构与任务匹配的受控实验。
[^13]: Mike A. Merrill 等，[*Terminal-Bench: Benchmarking Agents on Hard, Realistic Tasks in Command Line Interfaces*](https://arxiv.org/abs/2601.11868v1)，arXiv，2026-01-17；[ICLR 2026 论文](https://openreview.net/pdf?id=a7Qa4CcHak)。用于 Terminal-Bench 2.0 的任务设计，不引用动态排行榜名次。
[^14]: Quyu Kong 等，[*MobileWorld: Benchmarking Autonomous Mobile Agents in Agent-User Interactive and MCP-Augmented Environments*](https://arxiv.org/abs/2512.19432v3)，arXiv，首发 2025-12-22，v3 2025-12-30。用于移动跨应用、用户交互和混合工具评价。
[^15]: Anthropic，[*Effective harnesses for long-running agents*](https://www.anthropic.com/engineering/effective-harnesses-for-long-running-agents)，2025-11-26。用于初始化、增量推进、交接与验收实践。
[^16]: Prithvi Rajasekaran／Anthropic，[*Harness design for long-running application development*](https://www.anthropic.com/engineering/harness-design-long-running-apps)，2026-03-24。用于生成与评估分工、Harness 随模型变化调整。
[^17]: Anthropic，[*Scaling Managed Agents: Decoupling the brain from the hands*](https://www.anthropic.com/engineering/managed-agents)，2026-04-08。用于 session、Harness、sandbox 解耦及恢复设计。
[^18]: Josh Ma／Cursor，[*What we’ve learned building cloud agents*](https://cursor.com/blog/cloud-agent-lessons)，2026-06-02。用于云执行持久性、环境与会话解耦。
[^19]: LangChain，[*Deep Agents*](https://github.com/langchain-ai/deepagents)，官方 GitHub README。用于集成 Harness 功能及安全责任边界。
[^20]: OpenHands，[*Software Agent SDK*](https://github.com/OpenHands/software-agent-sdk)，官方 GitHub README。用于 SDK、工作空间、Agent Server 与 automation 边界。
[^21]: Earendil Works，[*Pi Agent Harness*](https://github.com/earendil-works/pi)，官方 GitHub README；旧地址 `badlogic/pi-mono` 重定向。用于组件结构及不内置权限限制的说明。
[^22]: Anomaly，[*OpenCode*](https://github.com/anomalyco/opencode)，官方 GitHub README。用于开源 coding agent 定位。
[^23]: Nous Research，[*Hermes Agent*](https://github.com/NousResearch/hermes-agent)，官方 GitHub README。用于记忆、用户画像、Skill 与消息入口。
[^24]: OpenClaw contributors，[*OpenClaw*](https://github.com/openclaw/openclaw)，官方 GitHub README。用于个人行动型 Agent 定位。
[^25]: ByteDance，[*DeerFlow 2.0*](https://github.com/bytedance/deer-flow)，官方 GitHub README。用于 2.0 重写、通用 Harness 与扩展能力。
[^26]: AgentScope，[*AgentScope*](https://github.com/agentscope-ai/agentscope)，官方 GitHub README。用于框架定位。
[^27]: Google，[*Agent Development Kit — Python*](https://github.com/google/adk-python)，官方 GitHub README。用于开发、评测和部署工具定位。
[^28]: Microsoft，[*Agent Framework*](https://github.com/microsoft/agent-framework)，官方 GitHub README。用于 Python／.NET 与多 Agent 工作流定位。
[^29]: Strands Agents，[*Harness SDK*](https://github.com/strands-agents/harness-sdk)，官方 GitHub README；旧 `sdk-python` 地址重定向。用于当前仓库和定位。
[^30]: NVIDIA，[*OpenShell*](https://github.com/NVIDIA/OpenShell)，官方 GitHub README。用于执行运行基础设施定位。
[^31]: Dify，[*Dify*](https://github.com/langgenius/dify)，官方 GitHub README。用于工作流、RAG 与部署形态。
[^32]: OpenAI，[*Agents SDK*](https://developers.openai.com/api/docs/guides/agents)；[*Sandbox Agents*](https://developers.openai.com/api/docs/guides/agents/sandboxes)，官方开发文档。用于开发侧能力分层。
[^33]: OpenAI，[*ChatGPT / Codex documentation*](https://learn.chatgpt.com/docs)，官方文档；原 Codex 文档入口重定向到此。用于当前产品与开发入口，不将导航存在解释为所有套餐开放。
[^34]: OpenAI，[*ChatGPT Work Overview*](https://learn.chatgpt.com/docs/enterprise/chatgpt-work-overview)，官方文档。用于多步骤工作、云端执行与工作区配置边界。
[^35]: OpenAI，[*Agent Builder*](https://developers.openai.com/api/docs/guides/agent-builder)，官方文档弃用提示。用于 2026-11-30 计划关闭及 ChatKit 保留状态。
[^36]: Anthropic，[*Claude Managed Agents overview*](https://platform.claude.com/docs/en/managed-agents/overview)，官方文档。用于 beta、会话、沙箱、干预与研究预览状态。
[^37]: Anthropic，[*Claude Cowork*](https://claude.com/product/cowork)，官方产品页。用于通用工作产品定位。
[^38]: Mike Clark／Google Cloud，[*What’s new in Gemini Enterprise Agent Platform*](https://cloud.google.com/blog/products/ai-machine-learning/whats-new-in-gemini-enterprise-agent-platform)，2026-07-29。用于 Runtime、Memory Bank、Identity 等更新；连续七天为厂商说明。
[^39]: Google Cloud，[*Gemini Enterprise Agent Platform*](https://cloud.google.com/products/gemini-enterprise-agent-platform)，官方产品页。用于当前平台名称与 Antigravity 入口。
[^40]: Microsoft，[*Overview of Microsoft Agent 365*](https://learn.microsoft.com/en-us/microsoft-agent-365/overview)，更新 2026-08-19。用于商业版 2026-05-01 GA 与治理定位。
[^41]: Microsoft，[*Hosting Agent Framework applications*](https://learn.microsoft.com/en-us/agent-framework/hosting/)，更新 2026-08-25。用于 Foundry Hosted Agents GA、自托管包预发布和宿主／协议分离。
[^42]: AWS，[*Release notes for Amazon Bedrock AgentCore*](https://docs.aws.amazon.com/bedrock-agentcore/latest/devguide/release-notes.html)，持续维护。用于组件及 Policy／Evaluations GA 更新。
[^43]: AWS，[*Core concepts — Policy in AgentCore*](https://docs.aws.amazon.com/bedrock-agentcore/latest/devguide/policy-core-concepts.html)，官方文档。用于 Gateway 工具策略、Cedar 与授权边界。
[^44]: 阿里云，[*会话事件流（SSE）*](https://docs.agent.bailian.aliyun.com/zh/managed-agents/sessions/event-stream)，百炼 Agent Studio 官方文档。用于工具、审批和结果事件。
[^45]: 阿里云，[*计费说明*](https://docs.agent.bailian.aliyun.com/zh/managed-agents/pricing/billing)，百炼 Agent Studio 官方文档，标明 2026-08-17 商业化。官方页面索引正文可读取，直接访问未稳定取得；仅据此记录状态，不据此提供价格建议。
[^46]: Volcengine，[*AgentKit SDK for Python*](https://github.com/volcengine/agentkit-sdk-python)，官方 GitHub README；[*Quick Start*](https://volcengine.github.io/agentkit-sdk-python/en/content/1.introduction/3.quickstart.html)，官方 SDK 文档。用于云 Runtime 部署入口。
[^47]: Tencent Cloud，[*Tencent Cloud ADP 4.0 International Edition Helps Enterprises Operationalize AI Transformation*](https://adp.intl.cloud.tencent.com/blog/adp-4-0-international-enterprise-ai-transformation)，官方介绍，2026 年 WAIC 后发布，页面索引显示 2026 年 8 月。用于国际版 AgentOps 定位；未据索引估算精确发布日期。
[^48]: Salesforce，[*Claudeforce: The #1 AI Meets the #1 CRM*](https://www.salesforce.com/claudeforce/)，官方产品页。用于 Salesforce in Claude、既有权限与业务规则、pilot 及计划九月 open beta 状态。
[^49]: GitHub，[*Develop agentic workflows in GitHub Actions*](https://docs.github.com/en/actions/tutorials/develop-agentic-workflows-in-github-actions)，官方文档。用于 Markdown 自动化与执行 Agent 选择。
[^50]: NVIDIA，[*NVIDIA NemoClaw*](https://docs.nvidia.com/nemoclaw/user-guide/openclaw/home/)，官方文档。用于 OpenShell 沙箱上的参考栈定位。
[^51]: Model Context Protocol，[*Specification — 2026-07-28*](https://modelcontextprotocol.io/specification/2026-07-28)，官方规范，访问时 `latest` 指向此版本。用于工具、上下文、可选扩展及实现方安全责任。
[^52]: A2A Project，[*A2A Protocol*](https://a2a-protocol.org/latest/)，官方文档，访问时含 v1.0 材料。用于独立 Agent 互操作与协议边界。
[^53]: Agent Skills，[*Specification*](https://agentskills.io/specification)，官方规范。用于 Skill 内容包与知识流程分发。
[^54]: 本仓库，[*Harness 领域词汇*](../../CONTEXT.md)、[*通用 Harness 项目目标*](../harness-project-goals.md)，目标草案更新 2026-09-09。用于术语和 G1–G7 映射；草案待确认事项保持待确认。
[^55]: LangChain，[*Persistence — LangGraph*](https://docs.langchain.com/oss/python/langgraph/persistence)，官方文档。用于 checkpointer 与 store 的职责区分。
[^56]: Anthropic，[*Overview — Claude Code*](https://code.claude.com/docs/en/overview)，官方文档。用于 coding agent 宿主和开发产品定位。
