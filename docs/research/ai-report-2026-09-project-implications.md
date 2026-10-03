# AI 研究对项目的关键启示

资料核对截至 2026-09-28。原研究从 9 月 18—27 日十期材料提取线索，并回到一手来源核查；其中七期原材料存在截断，未覆盖整月。下表保留影响设计的结果及核验深度，原作者结果未在本项目复现。现行采用范围见[设计依据](../architecture/decisions-and-evidence.md)。

| 主题与主要来源 | 对项目的启示 | 证据边界 |
| --- | --- | --- |
| [Harness Value](https://arxiv.org/abs/2609.20474v1)、[Overclaiming](https://arxiv.org/abs/2609.20812v3)、[SpecHarness](https://arxiv.org/abs/2609.29921v1) | 动作记录、成果正确性和任务完成分别检查；必要条件不能由完成声明替代 | 本组原资料核查为摘要；主观目标仍需质量判断或本人验收 |
| [Exactly-Once](https://arxiv.org/html/2609.29095v1)、[Silent Failures](https://arxiv.org/abs/2609.26836v1) | 回执丢失沿原身份核对，合法响应也要检查数据覆盖；未知副作用不能换键盲重发 | 前者核查 §3、§7，后者为摘要；幂等能力取决于真实提供方合同，不能承诺任意外部工具恰好一次 |
| [CliffCompaction](https://arxiv.org/abs/2609.26779v1)、[JitMem](https://arxiv.org/abs/2609.27334v1)、[C3M](https://arxiv.org/abs/2609.29735v1) | 原始证据、当前入模材料和跨任务经验分别管理；固定来源、时间与保留边界 | 原核查为摘要；分别处理运行上下文、读取时经验整理和跨会话图文证据，不共同规定单一记忆架构 |
| [Grow the Harness](https://arxiv.org/abs/2609.26760v2)、[HEXIS](https://arxiv.org/html/2609.30123v1) | 高频且可验证的控制流程可以代码化，改动经留出任务与旧能力回归后采用 | 前者为摘要，后者含正文 §5—6；旧轨迹回放不覆盖未见分支，总 token 或维护成本可能增加 |
| [Closed-World Resolution](https://arxiv.org/abs/2609.19425v1)、[Skill 触发](https://arxiv.org/abs/2609.29454v1)、[版本迁移 Skill 评估](https://arxiv.org/abs/2609.30120v1) | 工具目录和参数解析、Skill 是否触发、实际采用效果分别观测 | 原核查为摘要；目录成员合法不证明符合用户意图，总分变化不自动归因于 Skill 内容 |
| [Plugin4Shell](https://www.air.security/blog-posts/plugin4shell)、[MCP Skills 规范](https://github.com/modelcontextprotocol/ext-skills/blob/main/specification/stable/skills.mdx#security-considerations) | 检查实际安装字节和受审版本；远程 Skill 内容不自行获得主机执行权限 | 前者为披露者原文，未逐厂商核验规模和修复状态；后者为核查时的正式规范，滚动链接使用前应重新核对版本 |
| [Self-replicating prompt injections](https://alignment.openai.com/misalignment-reports/self-replicating-prompt-injections-exist/) | 来源和授权控制需贯穿输出、文件、消息及后续任务，不能只检查入模文本 | 官方研究披露；原文未观察到模拟工具调用之外的影响，不是真实用户感染统计，来源标记也非充分防御 |
| [EviRCA](https://arxiv.org/abs/2609.19825v1)、[Efficient Benchmarking](https://arxiv.org/abs/2609.21267v1) | 机器证据可先确定性提取；质量、完整成本和抽样方法一起记录 | 原核查为摘要；企业系统案例不要求所有任务限制为只读，生产固定子集也不能替代未暴露正式保留集 |

## 实施优先级

先实现目标条件、准确调用、未知效果恢复、当前授权和独立核验。随后按实际任务试验上下文压缩、检索、Skill 选择和子 Agent；程序性经验、自动精炼与代码化控制须有冻结评测和发布批准。每项优化同时记录收益、额外调用、维护成本和失败案例。

这些研究不能代替任务负责方的裁决，也没有证明本项目已达容量、质量或生产恢复目标。执行验收与剩余合同见[开发准备度](../architecture/engineering/implementation-readiness.md)和[验证设计](../architecture/validation/README.md)。
