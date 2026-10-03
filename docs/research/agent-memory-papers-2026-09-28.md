# Agent Memory 论文：关键结论

来源核对日期：2026-09-28。纳入的新论文以 v1 日期落在 2026-03-28 至 2026-09-28 为准；读取修订版不改变首次发表日期。以下来自原论文研究，未复现实验，不能作为本项目收益承诺。

## 结论与来源

| 论文 | 阅读版本与首次日期 | 对本项目有用的结论及限制 |
| --- | --- | --- |
| P01 / [TRUSTMEM: Learning Trustworthy Memory Consolidation for LLM Agents with Long-Term Memory · 2606.25161](https://arxiv.org/abs/2606.25161v1) | v1；v1 2026-06-23 | 写入前审查状态变化，拒绝无来源的合并和删除；不能把一个 verifier 当作授权权威。 |
| P02 / [Grounding Agent Memory: Environment-Probing Curation for Enterprise Agents · 2609.11060](https://arxiv.org/abs/2609.11060v1) | v1；v1 2026-09-10 | 保存前可用受限环境探针补查事实；探针仍需独立授权、预算和原调用身份。 |
| P03 / [Infini Memory: Maintainable Topic Documents for Long-Term LLM Agent Memory · 2606.10677](https://arxiv.org/abs/2606.10677v1) | v1；v1 2026-06-09 | 主题文档能聚合证据；主题文本覆盖不能替代不可变事实修订与来源。 |
| P04 / [Can Agent Memory Systems Track Evolving State? · 2608.19652](https://arxiv.org/abs/2608.19652v1) | v1；v1 2026-08-20 | 显式维护替代、时序和依赖关系；更正后同时检查依赖结论是否仍适用。 |
| P05 / [MemoryCPT: An End-to-End Agent Memory Framework for Cost-Performance Trade-off · 2608.04843](https://arxiv.org/abs/2608.04843v1) | v1；v1 2026-08-05 | 分别计量构建、检索、整理和生成；检索 token 下降不等于生命周期总成本下降。 |
| P06 / [Agent Memory Distillation: Empowering Small LLM Agents with Hierarchical Teacher Memory · 2608.07169](https://arxiv.org/abs/2608.07169v1) | v1；v1 2026-08-07 | 任务策略、子任务示例与函数经验分层；复用需保存适用前提和工具/环境版本。 |
| P07 / [Procedural Graphs: Self-Evolving Execution Structures for LLM Agents · 2609.09153](https://arxiv.org/abs/2609.09153v1) | v1；v1 2026-09-08 | 程序改动先独立验证，再冻结评测与发布；成功轨迹不赋予当前行动权限。 |
| P08 / [Agent Memory: Characterization and System Implications of Stateful Long-Horizon Workloads · 2606.06448](https://arxiv.org/abs/2606.06448v2) | v2；v1 2026-06-04 | 完整生命周期和长时程工作负载会改变成本，需纳入增长、恢复与后台处理。 |
| P09 / [DynamicMem: A Long-Horizon Memory Benchmark in Real-World Settings · 2606.22877](https://arxiv.org/abs/2606.22877v1) | v1；v1 2026-06-22 | 稳定保留与动态更新分开测试，不能只用静态问答命中率判断记忆质量。 |
| P10 / [Deployment-Time Memorization in Foundation-Model Agents · 2606.10062](https://arxiv.org/abs/2606.10062v2) | v2；v1 2026-06-08 | 删除测试覆盖正文、摘要、索引和代理再次写回；逻辑不可见与物理清除分别取证。 |
| P11 / [Revoked but Still Authoritative: An Empirical Study of Revocation Enforcement in Agent-Memory Systems · 2609.08258](https://arxiv.org/abs/2609.08258v1) | v1；v1 2026-09-08 | 区分识别撤销、暴露旧内容和实际采用；文中 mem0(exp.) 关闭了默认过期过滤，不能泛化到默认部署。 |
| P12 / [MemSecBench: Tracking Agent Memory Poisoning from Persistence to Consequence and Repair · 2607.27080](https://arxiv.org/abs/2607.27080v1) | v1；v1 2026-07-29 | 从恶意写入追到采用、后果和修复，测试跨任务传播及重启后的残留。 |

## 进入实现时的选择

优先把当前事实、时间、来源、纠正和关闭变成可核验记录。图索引、读时整理及程序性经验作为独立候选实验，按同模型、同输入、同预算与独立真值比较。完整聊天、任务摘要和经验候选都不自动取得长期保存许可。

评测至少覆盖冲突与过期、硬约束丢失、来源撤销、派生残留和费用增长，并记录未知/失败。原论文的模型、数据集、配置和统计范围不能直接移植成本项目阈值。开源接入边界见[记忆库结论](agent-memory-libraries-2026-09-28.md)，正式合同见[Memory](../architecture/memory/README.md)及[Evaluation](../architecture/evaluation/README.md)。
