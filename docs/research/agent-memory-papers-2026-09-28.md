# Agent Memory 近半年 arXiv 论文调研

[优化方案](../architecture/memory/optimization-plan.md) · [开源库调研](agent-memory-libraries-2026-09-28.md)

调研日期：2026-09-28。新论文窗口按 arXiv **首次提交日期**确定，为 **2026-03-28—2026-09-28**；修订日期只用于确定阅读版本。本文精读 12 篇直接影响本项目设计选择的论文，另记录被排除的候选和窗口外基础工作。这里的实验结果均为作者报告，本轮未复现实验。

建议保持现有 Memory owner、不可变修订、来源依赖、授权和清理契约，优先把已有时间冲突、读时整理、经验复用规则落实为可检验的数据与处理步骤。论文支持加强写入质量、当前状态判定和全生命周期评测；尚不足以支持首版全面改用图数据库、逐轮调用模型整理，或允许模型直接决定长期写入与删除。

## 1. 对本项目选择的影响

仓库已经规定：复合正文各断言保留独立时间与范围；明确 replace 后旧修订退出当前查询；派生物进入 `needs_review`；查询先限定获准处理范围；读时整理复用现有索引和 ContentRef；关闭沿 `source_edges` 传播。因此，下表是落实和验证这些规则的建议，不把既有保证列为新增能力。依据见[模块契约](../architecture/memory/README.md#temporal-conflicts)与[实现设计](../architecture/memory/implementation.md#read-time-curation)。

| 关键选择 | 论文提供的证据 | 推荐的实现方向与边界 |
| --- | --- | --- |
| 写入前校验哪些内容 | P01 对新事实遗漏、旧事实损坏、无依据新增分别评估；P02 用只读环境证据补齐轨迹缺口 | 在候选中保存本次输入、拟修改对象、支持证据与不确定项；把局部语义审核作为质量信号。许可、期望修订和发布竞争仍由 owner 确定裁决 |
| 如何回答“现在是什么” | P04 显式维护替代关系及依赖；P09 把保持稳定事实和替换变化事实分开测量 | 对断言明确身份、观察时间、适用时间、范围与替代依据；不同来源的矛盾保留并提示，不能以相似度、最近写入或置信度直接覆盖 |
| 是否加入主题层或图结构 | P03 主题文档改善证据聚合，但跨主题更新仍困难；P07 的程序图解决动作顺序 | 首先以准确 ContentRef 和普通关系表表达必要关联。主题摘要是可重建派生物；程序关系、语义关系与治理用 `source_edges` 分别解释，不混为一种“记忆图” |
| 何时付出额外检索与整理成本 | P05 将检索与摘要分开，P08 发现构建、检索和生成成本需要分项 | 用同模型、同预算比较字面基线、BM25、获准语义混合检索、按需整理；固定输入及调用上限，累计构建、清理和在线使用成本 |
| 经验如何复用 | P06 区分任务策略、子任务示例与函数使用；P07 用独立验证门禁接纳改动 | 经验保存适用前提、工具／环境版本、执行事实、效果与反例；先用于建议和上下文，再评估可复用程序。论文中的成功轨迹不能替代当前执行授权 |
| 如何证明关闭与修复有效 | P10 检查派生层残留；P11 区分识别、暴露和执行撤销；P12 贯通写入、采用和修复 | 扩展既有关闭验收：同时测直接引用、摘要／索引派生、代理再次写回及重启恢复；行为不再泄露与物理清理证据分别报告 |

以上是面向本仓库的推论。下文保留每项推论所依赖的实验设置和不能外推的条件。

## 2. 纳入论文与日期核对

下表日期来自各论文 arXiv `Submission history`。除明确列出 v2 外，检索时页面仅列 v1；“最新版本”指截至调研日核对到的版本。

| 编号 | 论文与 arXiv 原始记录 | v1 日期 | 阅读版本 |
| --- | --- | --- | --- |
| P01 | [TRUSTMEM: Learning Trustworthy Memory Consolidation for LLM Agents with Long-Term Memory · 2606.25161](https://arxiv.org/abs/2606.25161) | 2026-06-23 | v1 |
| P02 | [Grounding Agent Memory: Environment-Probing Curation for Enterprise Agents · 2609.11060](https://arxiv.org/abs/2609.11060) | 2026-09-10 | v1 |
| P03 | [Infini Memory: Maintainable Topic Documents for Long-Term LLM Agent Memory · 2606.10677](https://arxiv.org/abs/2606.10677) | 2026-06-09 | v1 |
| P04 | [Can Agent Memory Systems Track Evolving State? · 2608.19652](https://arxiv.org/abs/2608.19652) | 2026-08-20 | v1 |
| P05 | [MemoryCPT: An End-to-End Agent Memory Framework for Cost-Performance Trade-off · 2608.04843](https://arxiv.org/abs/2608.04843) | 2026-08-05 | v1 |
| P06 | [Agent Memory Distillation: Empowering Small LLM Agents with Hierarchical Teacher Memory · 2608.07169](https://arxiv.org/abs/2608.07169) | 2026-08-07 | v1 |
| P07 | [Procedural Graphs: Self-Evolving Execution Structures for LLM Agents · 2609.09153](https://arxiv.org/abs/2609.09153) | 2026-09-08 | v1 |
| P08 | [Agent Memory: Characterization and System Implications of Stateful Long-Horizon Workloads · 2606.06448](https://arxiv.org/abs/2606.06448) | 2026-06-04 | v2，2026-09-22 |
| P09 | [DynamicMem: A Long-Horizon Memory Benchmark in Real-World Settings · 2606.22877](https://arxiv.org/abs/2606.22877) | 2026-06-22 | v1 |
| P10 | [Deployment-Time Memorization in Foundation-Model Agents · 2606.10062](https://arxiv.org/abs/2606.10062) | 2026-06-08 | v2，2026-07-09 |
| P11 | [Revoked but Still Authoritative: An Empirical Study of Revocation Enforcement in Agent-Memory Systems · 2609.08258](https://arxiv.org/abs/2609.08258) | 2026-09-08 | v1 |
| P12 | [MemSecBench: Tracking Agent Memory Poisoning from Persistence to Consequence and Repair · 2607.27080](https://arxiv.org/abs/2607.27080) | 2026-07-29 | v1 |

## 3. 写入、状态与检索

### P01：TRUSTMEM——审核每次状态变化

论文把更新表示为“输入片段、原记忆、Write／Revise／Prune 操作、新记忆”，用冻结 LLM 检查 coverage、preservation、faithfulness，再以局部偏好及任务奖励训练更新策略。它主要提供训练监督，不能视作确定性的事实验证器。实验覆盖 MemoryAgentBench、HaluMem 和 Mem-alpha；训练使用 Mem-alpha 池中的 562 个实例，验证使用 463 个留出实例，基线模型配置并非全部一致。[全文 §3、§4、附录 A](https://arxiv.org/html/2606.25161v1)

**项目借鉴：**先为候选更新建立可审计的差异和三类错误标签，作为提取质量门禁及回归样例。模型审核通过仍须执行原有来源、权限、修订和候选发布检查；本轮证据不要求立即训练专用模型。

### P02：Grounding Agent Memory——保存前补查环境

异步 curator 在 propose–probe–commit 流程中取得最小只读工具，用环境证据核实或缩小经验适用范围。CLBench 包含 40 问、途中 schema 迁移的设置；另测 90 个改编 APEX 任务。CLBench 表 1 中，无记忆通过率为 39%，轨迹记忆为 70%，增加探测为 73%；**39%→73% 不能全部归因于探测**。主实验有五次配对运行，且报告的 task-agent 成本排除了 distillation／curation 成本。[全文 §3—5、附录 B](https://arxiv.org/html/2609.11060v1)

**项目借鉴：**对高复用、易过时的工具经验添加有预算的只读复核任务；探测失败保留不确定性。复核由 Orchestrator 承担执行和权限责任，候选保存仍由 Memory owner 裁决；将探测费用纳入本项目总成本。

### P03：Infini Memory——以主题文档聚合证据

新观察先进入缓冲区，定期整合为带来源和时间信息的主题文档；查询时按目录、检索、正则和行读取逐步寻找证据。MemoryAgentBench 实验统一使用 gpt-5-mini、4096-token 输入块，agentic 读取最多七轮，证据不足时补充 BM25。作者指出单跳更新较好，跨多个主题的多跳更新仍显著困难，不能据主题化本身推断全局状态一致。[全文 §3—4](https://arxiv.org/html/2606.10677v1)

**项目借鉴：**主题摘要可作为额外候选入口，但应链接准确断言与来源修订，读时继续复核当前资格。目录和摘要也继承实际输入的限制；不要把摘要生成时间当成全部事实的有效时间，也不要照搬整文档“最新覆盖”。

### P04：StateMem——显式表示替代和依赖

每轮输入被编码为状态单元，包含来源、优先级及 `derived_from`／`coupled_with` 依赖；更新先记录 supersession，再沿依赖检查。StateMemBench 有 234 个合成多会话场景，评分区分当前答案、过时答案及其他错误。作者的限制声明十分关键：场景约 3k 或 7–15k tokens、单次运行，每场景约 165–600 次编码调用；基准中的陷阱也贴近其方法针对的失误。[全文 §5、附录 H](https://arxiv.org/html/2608.19652v1)

**项目借鉴：**将已规定的断言时间／范围和替代依据落实为结构化字段，评测“旧值仍被采用”。先实现有限依赖与受影响对象复核，再测是否值得逐轮抽取。语义 supersession 不能替代来源关闭，模型预测的替代关系不能直接扩大自动 replace 权限。

### P05：MemoryCPT——将构建与查询成本分开

QAD 用教师轨迹训练离线构建模型；QAR 以 BM25、稠密检索和 RRF 合并候选，再用 GRPO 训练摘要策略，平衡回答质量和 token 成本。LoCoMo 仅测试 conv-49／50 的 314 问；LongMemEval 被作者重新划为 150／98／105 问的训练／验证／测试集。因此不能把其分数与原公开测试设置直接横比。成本模型显式摊销离线构建，但训练奖励使用的在线成本代理不包含数据库检索等固定费用。[全文方法、实验设置、附录 C](https://arxiv.org/html/2608.04843v1)

**项目借鉴：**先测试获准范围内的混合召回与有界摘要；分别记录构建、模型整理、最终推理及基础设施成本。现有字面基线仍用于确定收益，专用模型训练留到任务分布和样本量稳定以后。

## 4. 经验与程序性记忆

### P06：Agent Memory Distillation——按复用粒度组织经验

AMD 从教师成功轨迹提取 Workflow、Subtask、Function 三类记忆：前两类在任务开始时提供，Function 在工具调用错误时检索。实验使用 GPT-5-mini 教师、四个 4B–8B 学生，覆盖 AppWorld、BFCL V3、ToolSandbox。作者明确限定为文本及结构化工具任务；记忆离线构建并在推理时冻结，不能据此声称支持在线持续改进或视觉操作迁移。[全文 §3—5、Limitations](https://arxiv.org/html/2608.07169v1)

**项目借鉴：**在现有 `experience` 内标注策略、子步骤示例、工具调用注意项，按失败位置选择最小材料。经验记录仍区分行动与核实效果，并保存环境／工具版本；小模型能否使用某份经验要独立验证。

### P07：Procedural Graphs——对程序改动设置验证门禁

程序图表示 procedure 间的关系；指导模型根据当前节点和邻近子图给下一步建议。refiner 对照成功和失败轨迹生成图改动，只保留不降低留出集表现的版本，并记住被拒绝的改动。实验覆盖七种基准，包括 HotpotQA、ALFWorld、tau-bench、BFCL 和 EnterpriseArena。所谓 online evolution 指训练批次之间更新，**每个 episode 内及测试阶段图保持固定**。[全文 §3—4、附录 D.2](https://arxiv.org/html/2609.09153v1)

**项目借鉴：**先给可复用经验定义前置条件、步骤关系、停止条件和验证样例，保留原任务执行器的裁决。只有步骤关系确实改善相同预算下的成功率时，再引入程序图；验证通过不代表任意新环境可安全自动执行。

## 5. 成本、长时程与安全评测

### P08：Agent Memory Characterization——测完整生命周期

论文以分阶段 profiling 比较十个系统的构建、检索、生成成本。主要系统刻画使用 MemoryAgentBench 的 LongMemEval_S_*：五份约 360k-token 历史、每份 60 问；另以 MemoryArena 观察会话间写入与新查询竞争。作者保留系统原生缓冲等差异，并做必要 prompt 适配；例如 Letta 另用 512-token 输入块，不能把差异全部归因于单个存储算法。[全文 §3—4、附录 A](https://arxiv.org/html/2606.06448v2)

**项目借鉴：**在既有 p95/p99、索引滞后和清理指标外，按构建／召回／整理／生成记录模型调用、输入输出、占用和成本，并按每份历史实际查询次数摊销。比较高质量但昂贵的写入方案时，必须展示低查询量和高查询量两种负载。

### P09：DynamicMem——稳定保留与动态更新分开测

基准合成十个用户、15 个月、16 个应用的轨迹，每用户约 2.2M tokens；在五个季度检查点分别测状态补全和个性化服务，区分属性、习惯与偏好。它检查稳定事实保留和变化事实更新，并分析输出证据缺失、实体身份和过时状态等错误。虽然题名包含 Real-World Settings，数据仍是合成的；论文中的长期服务表现不能视为真实用户试验。[全文 §3—5、附录 I](https://arxiv.org/html/2606.22877v1)

**项目借鉴：**为现有时间冲突验收加入多检查点、同名实体、短期例外恢复和跨应用证据分散。将推断偏好与用户明确陈述分开评分，避免为了 benchmark 的隐式画像目标而扩大实际采集和保存许可。

### P10：Deployment-Time Memorization——测派生层删除残留

论文在 raw／summary 两层注入高熵 canary，测个性化回忆、对抗提取和删除后残留；比较仅删原文、重摘要、全清理和 redaction。v2 正文的主实验为 LongMemEval oracle split 500 例，两种主模型；删除实验仍为 50 例。只删 raw 后 summary 仍可能恢复 canary，但零文本恢复率**不证明**磁盘、向量或缓存已物理删除，也不能覆盖语义改写后的个人事实。[v2 全文 §2—4](https://arxiv.org/html/2606.10062v2)

**项目借鉴：**沿现有 `source_edges` 和 holder 清理责任做逐层残留测试，同时检查使用已停止和物理清理报告。优先测摘要及重新写回的派生结论；压缩减少敏感词出现不能替代授权隔离。

### P11：Revoked but Still Authoritative——分开验证撤销的三个环节

研究用九类策略场景、九个模型，检查五种 memory 系统的撤销识别、状态可见性和读时执行。**全文表 3 明确 mem0 默认过滤过期记录；文中的 mem0(exp.) 特意关闭该过滤。**其他系统可能覆盖旧记录、未识别矛盾，或不向调用方暴露撤销标志，不能概括为“五个系统默认都返回已撤销事实”。语义 guard 还会误挡当前事实，部分条件下效果变差。[全文 §3—5、附录 B.3](https://arxiv.org/html/2609.08258v1)

**项目借鉴：**坚持 owner 权威状态与索引候选分离，并测试撤销后“命中但不能交付”的路径。来源关闭、同一对象明确替换、尚未裁决的跨来源矛盾要分别处理；普通检索结果不是独立的环境复核证据。

### P12：MemSecBench——贯通写入、采用与修复

基准包含 310 个案例、48 个上下文，按 Write–Execute–Forget 和七个检查点追踪恶意语义从持久化到动作后果，再到选择性修复。实验是两个 agent harness、四个 backend、三个模型的 24 种配置，固定版本及适配器；作者明确这些是完整组合对比，并非只替换存储介质的消融。结果随 harness 和模型变化，不能据聚合攻击率给库排序。[全文实验设置、附录 C—E](https://arxiv.org/html/2607.27080v1)

**项目借鉴：**在既有隔离／来源关闭用例中加入“低信任内容被提取成经验—被后续任务采用—删除后再次召回”链路。分别记录入库、召回、采用、实际动作和修复，避免只验证数据库删行；外部材料不得因存入 memory 升级为执行指令。

## 6. 窗口外基础工作与未纳入候选

下列工作仅用于识别方案来源或实验基线，**不计入近半年新论文**。本轮只核对其 arXiv 元数据和摘要，不据此重述性能结论。

| 基础工作 | v1／核对到的最新修订 | 与本轮的关系 |
| --- | --- | --- |
| [A-MEM · 2502.12110](https://arxiv.org/abs/2502.12110) | 2025-02-17／v11 2025-10-08 | 关联笔记与记忆演化基线，窗口外 |
| [Mem0 · 2504.19413](https://arxiv.org/abs/2504.19413) | 2025-04-28／v1 | 事实提取及更新基线，窗口外 |
| [SimpleMem · 2601.02553](https://arxiv.org/abs/2601.02553) | 2026-01-05／v3 2026-01-29 | 结构压缩、合成与按意图检索，窗口外 |
| [Learning to Remember / UMA · 2602.18493](https://arxiv.org/abs/2602.18493) | 2026-02-13／**v2 2026-09-01** | 近期修订的旧论文；不因 9 月修订成为近半年新作 |

为保留有竞争力的替代方向，另外核对了以下窗口内候选。未纳入 12 篇精读不等于否定其价值，表示它们未改变本项目这一轮的推荐。

| 候选 | 日期／版本 | 暂不作为主要依据的原因 |
| --- | --- | --- |
| [Memory Intelligence Agent · 2604.04503](https://arxiv.org/abs/2604.04503) | v1 2026-04-06；v4 2026-04-19 | 以 deep research 的参数／非参数记忆互换和持续训练为主；本模块首先需要可纠正、可删除的外部记忆实现 |
| [Self-Aware Vector Embeddings / SmartVector · 2604.20598](https://arxiv.org/abs/2604.20598) | v1 2026-04-22 | 合成版本策略实验规模为 258 个向量、138 问；不足以支持用置信衰减及访问强化替代确定的授权与时间语义 |
| [From Storage to Experience · 2605.06716](https://arxiv.org/abs/2605.06716) | v1 2026-05-07 | 综述提供 storage／reflection／experience 分类，本文优先以原始方法和实验作为实现依据 |
| [Harness the Memory · 2608.15008](https://arxiv.org/abs/2608.15008) | v1 2026-08-15 | 统一 harness 的结果支持按任务比较多种表示；摘要说明代码待接收后发布，本轮用 P08 支撑具体生命周期计量 |

## 7. 检索、证据与验证边界

检索使用英文题名和主题词，以 arXiv 摘要页发现论文、以 `Submission history` 核对日期，再读版本化 HTML 的方法、实验和限制。代表性检索式如下；月度查询使用 April、May、June、July、August、September 2026，避免只检索“2026”而混入窗口外工作。

- `site:arxiv.org "agent memory" "2026" "Submitted on"`
- `agent memory arxiv <month> 2026`
- `site:arxiv.org "memory consolidation" "Apr" "2026"`
- `site:arxiv.org "TRUSTMEM"`、`"Infini Memory"`、`"DynamicMem"`
- `site:arxiv.org "Revoked but Still Authoritative"`、`"MemSecBench"`、`"Procedural Graphs"`

本次是面向设计决策的代表性取样，不是 arXiv 全量系统综述；没有声称穷尽窗口内论文或证明检索召回率。筛选后核心论文集中在 6—9 月，4—5 月候选及窗口外近期修订已单列。参考清单、二次解读及搜索摘要只用于发现题名，结论链接均回到 arXiv 一手记录或全文。

两处需要保留的核对结果：P10 摘要页与 v2 HTML 的规模和摘要数字存在差异，本文采用版本化正文的实验设置，不合并数字；P11 摘要中的概括比全文逐配置结果更强，本文按全文记录 mem0 默认过滤与实验覆盖差别。论文分数只用于解释本篇设置，未跨论文汇总为排行榜。

静态证据：已读取上述 12 篇版本化全文的相关方法、实验及边界，并对照本仓库 Memory 两篇设计文档；本文件新增的建议仍需进入完整方案评审。运行证据：**未安装、运行或复现这些论文实现，未测本仓库服务性能，未证明模型质量或物理清理保证。**后续复现实验需固定论文版本、代码提交、模型、数据拆分、授权过滤、候选与 token 预算，并保留失败分类和分阶段费用。
