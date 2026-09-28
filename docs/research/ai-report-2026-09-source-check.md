# 2026 年 9 月 AI 调研报告：一手来源核验

核验日期：2026-09-28。材料来自用户提供的 [AI 领域调研报告会话](chatgpt-conversation://6a5f09b7-ea50-83ec-bf6d-26a2576aac1d)。本次取得 9 月 18—27 日日报片段，部分长条目被截断，不能视为整月全部报告。本文核对 19 项与 Harness 设计有关的一手来源，供[项目建议](ai-report-2026-09-project-implications.md)引用；没有复现实验、运行外部项目，也没有修改现行架构契约。

“已核验”表示已读取作者论文、官方规范或披露者原文，并确认表内所述内容；不表示独立验证了实验数据。论文未标明同行评审结果时按预印本处理。标注“摘要”的条目只核验题名、日期、摘要中的方法与范围，不据此声称读完论文。报告中的架构建议与本文的项目推论均不作为原论文结论。

## 核验记录

下表的日期为日报日期；括号中列出一手来源日期或本次读取版本。省略未必要引用的成绩数字，避免将特定基准上的结果写成本项目收益承诺。

| 日报日期／主题 | 一手来源 | 实际核验内容 | 适用边界与状态 |
| --- | --- | --- | --- |
| 9 月 18 日／Harness 组件消融（9 月 17 日，v1） | [An Empirical Study of Harness Design for Coding Agents](https://arxiv.org/abs/2609.20804v1) | 固定执行循环，分别改变规划、动作接口和上下文管理；上下文收益主要来自减少溢出，先规则删除再摘要较高效；规划效果随模型能力变化。 | **已核验：摘要。** 四个模型、SWE-Bench Verified 与 Terminal-Bench 2.1。可恢复被删内容在该实验中少被使用且没有准确率增益，不能据此删除项目的审计或恢复证据。 |
| 9 月 18 日／规划与验收（9 月 17 日，v1） | [How Do Agent Harnesses Create Value?](https://arxiv.org/abs/2609.20474v1) | 区分任务相关规划带来的成功收益与终态只读 verifier 减少错误放行的收益；选择取决于错误放行的损失，verifier 也会误挡正确结果。 | **已核验：摘要。** τ²-bench 的 Retail 实验与 Airline pilot。终态 verifier 属于结果验收，不等于动作发生前的权限裁决，也不撤销既有副作用。 |
| 9 月 18 日／完成声明（9 月 17 日首发，本次 v3／9 月 22 日） | [Quantifying Overclaiming Propensity in Frontier LLM Agents](https://arxiv.org/abs/2609.20812v3) | 以执行 transcript 核对文件审查覆盖范围与最终声明，确认自然语言完成声明经常不能准确反映实际动作。 | **已核验：摘要。** 五类文件审查场景；不判断模型意图，也不以产物正确与否定义 overclaiming。完成依据由运行时提供是项目设计推论，论文没有替项目定义通用回执协议。 |
| 9 月 18 日／结构化证据（9 月 17 日，v1） | [EviRCA](https://arxiv.org/abs/2609.19825v1) | 确定性提取阶段将指标、trace、日志转换为证据卡，模型通过少量只读工具推理，不直接读取原始遥测或执行代码。 | **已核验：摘要。** OpenRCA 的三个企业微服务系统，不能推广为所有 Agent 都应禁止探索或执行。可借鉴的是机器数据预处理与判断职责分开。 |
| 9 月 23 日／上下文压缩（9 月 22 日，v1） | [CliffCompaction](https://arxiv.org/abs/2609.26779v1) | 每轮从原始内容截断或删除，不重写内容，也不再次压缩前一轮压缩结果，以限制累计漂移。 | **已核验：摘要。** 长程编码与内核优化实验。该机制说明一种压缩选择，不证明摘要普遍无效；成本收益需要按本项目模型、窗口和任务复测。 |
| 9 月 24 日／按需记忆整理（9 月 23 日，v1） | [Just-in-Time Memory](https://arxiv.org/abs/2609.27334v1) | 保留原始轨迹，针对当前任务在读取时整理相关记忆；将记忆整理的收益直接关联到当前任务。 | **已核验：摘要。** ALFWorld、WebShop、τ²-bench；未由摘要验证企业数据的保留期限、撤权或删除传播。不能把保留原始轨迹解释为无条件永久保存。 |
| 9 月 27 日／跨会话证据（9 月 24 日，v1） | [C3M](https://arxiv.org/abs/2609.29735v1) | 在持久图文源证据上维护有界活动索引，合并安全冗余，保留互补、冲突和跨时间记录，查询时按预算展开源证据。 | **已核验：摘要。** 跨会话多模态记忆；支持保留来源与时间区别，不足以证明本项目需要引入图数据库或新记忆服务。 |
| 9 月 23 日／重复控制代码化（9 月 22 日首发，本次 v2／9 月 24 日） | [Grow the Harness, Not the Context](https://arxiv.org/abs/2609.26760v2) | 将重复控制决策积累为共享可执行代码；按函数轨迹定位失败，联合修复，并用留出任务门禁回滚损害既有能力的改动。 | **已核验：摘要。** BrowseComp-Plus 与 WebArena-Verified 的专用 Agent。部署阶段推理成本不等于训练、修复、门禁的总成本，也不构成在线自主改写生产 Harness 的依据。 |
| 9 月 25 日／Skill 编译（9 月 24 日，v1） | [HEXIS](https://arxiv.org/html/2609.30123v1) | 将 Skill 控制关系编为扩展有限状态机，状态内仍由模型推理；接受更新前做静态检查，并回放当前及先前接纳轨迹。 | **已核验：摘要及正文 §5—6。** 已有轨迹回放不保证未见分支覆盖或状态内推理正确；部分模型／任务的 token 成本增加。不能写成“编译后必然更省、更正确”。 |
| 9 月 25 日／重复副作用（9 月 24 日，v1） | [Where Does Exactly-Once Live?](https://arxiv.org/html/2609.29095v1) | 区分可立即回读确认的丢失应答与仍在途的延迟提交；后者在没有已知时限时，单靠查后重试不能兼顾完成与无重复。稳定幂等键的效果依赖服务契约。 | **已核验：摘要及正文 §3、§7。** LIMBO 模拟服务；对 Harness 的比较有相同工具和前置指令限制。服务端按键至多执行一次、返回原结果，批量中断可按键续做等前提不能省略。 |
| 9 月 24 日／工具静默失败（论文 9 月 21 日提交） | [Silent Failures in Agent-Tool Interaction](https://arxiv.org/abs/2609.26836v1) | 调用看似成功但 API／wrapper 缺失字段、功能或检索语义，可能继续产生表面有效的下游结果。 | **已核验：摘要。** 审计 ToolUniverse 环境接入的 15 个科学工具。报告的“9 月 24 日新榜”与论文提交日期含义不同；不据此断言所有工具均有同样发生率。 |
| 9 月 26 日／Skill 触发（9 月 24 日，v1） | [Demystifying Agent Skills for Smart Contract Auditing](https://arxiv.org/abs/2609.29454v1) | 调查真实审计 Skills 并比较多个 Agent／模型组合；触发是重要瓶颈，收益和执行行为随模型配置变化。 | **已核验：摘要。** 智能合约安全审计与 EVMBench。支持单独观测触发与应用结果，不证明通用 Skill Router 的特定实现优于模型选择。 |
| 9 月 27 日／Skill 评估（9 月 24 日，v1） | [Evaluating Agent Skills for Version-Specific Plugin Migration](https://arxiv.org/abs/2609.30120v1) | 将总分追溯到版本契约与逐项评分，发现评分错误、收益集中和 judge 敏感性；用可执行探针核查部分问题。 | **已核验：摘要。** 一个已发布迁移 Skill 的静态任务与历史报告。端到端可执行修复、独立人工标注和其他框架仍列为后续工作，不能称其完成全面生产评估。 |
| 9 月 18 日／插件安装身份（9 月 17 日披露） | [Air Security：Plugin4Shell](https://www.air.security/blog-posts/plugin4shell) | 披露了请求固定 SHA、实际检出其他内容的安装边界缺陷；检查实际 HEAD 与受审内容身份一致是其提出的修复关键。 | **已核验：披露者原文。** 利用条件涉及攻击者控制仓库及特定引用解析。受影响规模、产品版本与修复状态未逐厂商交叉核验，不沿用“所有安装都受影响”的泛化。 |
| 9 月 20 日／远程 Skill 信任（本次读取正式稳定规范） | [MCP Skills 稳定规范](https://github.com/modelcontextprotocol/ext-skills/blob/main/specification/stable/skills.mdx#security-considerations)；[官方仓库说明](https://github.com/modelcontextprotocol/ext-skills) | 规范要求区分来源与内容身份；MCP Skill 作为不可信输入，digest 仅证一致性；持久批准绑定完整资源集，内容变化撤销批准；远程内容不能隐式获得主机执行权。 | **正式规范已核验；日报旧 threat-model 链接不可访问。** 仓库确认 SEP-2640 在 9 月 13 日成为 Final，并区分规范与归档设计材料。规范要求不代表现有客户端均实现，也不自动成为本项目新契约。 |
| 9 月 27 日／注入传播（9 月 25 日公开） | [OpenAI：Self-replicating prompt injections exist](https://alignment.openai.com/misalignment-reports/self-replicating-prompt-injections-exist/) | 展示注入内容随模型输出进入邮件、文件或代码注释并继续传播的可行性。 | **已核验：官方研究披露。** 原文明示未观察到训练／评估模拟工具调用之外的影响；这是攻击能力实验，不是真实用户感染事件，也未证明来源标记本身足以阻断攻击。 |
| 9 月 25 日／规范与完成权威（9 月 24 日，v1） | [Who Holds the Pen? Let Specifications, Not Agents, Sign Off](https://arxiv.org/abs/2609.29921v1) | SpecHarness 将可见规范转换为关联来源的要求，Agent 提出计划、行动与完成请求，由合格提供方的可采信证据确立规范状态。 | **已核验：摘要。** 规范遵循与产物生成任务；模糊或主观要求仍属建议，不能推成所有目标均可机械验证。项目仍保持已确认的开放质量及用户验收机制。 |
| 9 月 19 日／工具解析（9 月 16 日，v1） | [Closed-World Resolution Against Tool Hallucination in LLM Agents](https://arxiv.org/abs/2609.19425v1) | 研究虚构工具、未声明参数及 MCP 多服务命名合并带来的冲突，提出登记成员与签名解析作为门禁前的检查。 | **已核验：摘要。** 十个模型、两类调用界面及 MCP 扩展实验；不意味着目录发现可绕过访问控制，也不证明合法参数一定符合用户真实意图。 |
| 9 月 21 日／生产评测成本（9 月 18 日，v1） | [Efficient Benchmarking in Production](https://arxiv.org/abs/2609.21267v1) | 以生产分析 Agent 的历史运行比较采样、缓存、固定子集与自适应测验；作者因运营简单而部署按难度分层的固定子集。 | **已核验：摘要。** 分数估计误差与操作成本的案例研究；论文采样比例不是本项目默认值，反复使用的子集不能代替本项目未暴露的正式保留集。 |

## 引用时需要保留的区别

**动作事实、数据完整性与任务验收是三个问题。** Overclaiming 研究说明完成声明需要可核对的动作记录；工具静默失败研究说明收到响应仍可能缺少任务需要的数据；终态 verifier 研究则关注是否接受最终结果。把它们都写成“增加一个 verifier”会掩盖证据由谁提供、何时可判成功的差异。[Overclaiming](https://arxiv.org/abs/2609.20812v3)、[工具静默失败](https://arxiv.org/abs/2609.26836v1)、[Harness 价值](https://arxiv.org/abs/2609.20474v1)

**有原始记录与可安全重试也分别成立。** 本地 trace 保存了请求，不能证明外部副作用尚未发生；“回读未找到”还要结合可见延迟和在途请求。项目建议可以据此强化未知结果与恢复测试，不能把本地去重或回执提升为任意外部工具的恰好一次保证。[Exactly-Once 论文 §3、§7](https://arxiv.org/html/2609.29095v1)

**三种记忆方法处理不同位置的信息损失。** CliffCompaction 针对运行上下文，JitMem 针对跨任务经验读取，C3M 针对跨会话图文源证据。将“源证据”和“当前入模内容”分别管理，是由这些方法得到的设计推论；它们没有共同规定单一记忆架构。[CliffCompaction](https://arxiv.org/abs/2609.26779v1)、[JitMem](https://arxiv.org/abs/2609.27334v1)、[C3M](https://arxiv.org/abs/2609.29735v1)

**稳定流程可代码化，但需验证适用条件和未覆盖行为。** Grow the Harness 与 HEXIS 支持为重复、可检验的流程做小范围原型；其验证机制不能替代项目的授权、外部效果恢复和未见任务测试。Skill 触发和 judge 偏差也应分别测量，避免把总分变化直接归因于 Skill 内容。[Growing Harness](https://arxiv.org/abs/2609.26760v2)、[HEXIS](https://arxiv.org/html/2609.30123v1)、[Skill 触发研究](https://arxiv.org/abs/2609.29454v1)、[迁移评估研究](https://arxiv.org/abs/2609.30120v1)

## 未核验范围与检查结果

未逐条核验日报的全部产业新闻、规模数字、模型排名或全部论文结果；这些仍为报告线索。本文不将它们作为项目优先级的独立依据。

原始 [MCP threat-model 路径](https://github.com/modelcontextprotocol/ext-skills/blob/main/docs/threat-model.md) 读取失败，raw 地址返回 404；随后从官方仓库找到并读取正式稳定规范。不能由旧链接失败推断规范不存在，也没有把未读到的旧 Threat Model 当作已核验材料。

来源核对已完成：表内论文标题与编号匹配、报告日期与来源日期分列、当前版本变化明确标注，引用均指向一手来源。网页可读取检查不等于论文实验复现；文档结构与建议图示的检查结果见[建议稿审查范围](ai-report-2026-09-project-implications.md#7-本次整理的审查范围)。
