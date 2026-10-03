# Agent Harness 参考项目调研

研究日期：2026-10-01，时区 Asia/Shanghai。各项目报告分别保存在 `docs/research/<project>/`；共同基线与架构建议保存在本目录。源码按固定版本引用，当前不要求本地保留参考仓库。

建议先读[架构优化分析](architecture-optimization.md)，再按模块打开对应项目报告。综合判断是保留本项目既有九模块和可靠任务基线，吸收上游的内部组织、恢复检查点与故障语料；程序化工具、渐进式发现和经验精炼以候选实验验证。

<a id="reports"></a>
## 1. 项目报告与源码快照

| 项目报告 | 分支 / 固定 commit | 报告重点及局部建议 |
| --- | --- | --- |
| [OpenAI Codex](../codex/README.md) | `main` / `d4a475adda850d80b6149c76454de94e0cf4fd51` | Rust Session/Turn/Step、目录绑定、rollout/SQLite 各类状态、平台执行、冷恢复、Agent/Memory/扩展；C-01～14 |
| [Pi](../pi/README.md) | `main` / `8ce69e9d2b171d173fe4b6b2b6256f1f4411e69d` | 经典 CLI、公开 AgentHarness、独立 durable 三线，投影/存储/重放、hooks、MCP/Codemode、服务协议及评测；P-01～10 |
| [DeepSeek Harness](../deepseek-harness/README.md) | `master` / `639ed015397290b3745d163aafe02ffee4aa3f84` | 能力 seam、SessionEvent、semantic checkpoint、unknown、Goal/Schedule、子恢复、native/VM/Python 及实际数据出口；D-01～13 |
| [Prime Agent](../prime-agent/README.md) | `main` / `5784abc2aef523a78d5a8850a0c0be89883388b2` | Rust daemon/worker + Python RLM、持续目标、spawn/collect、kernel、harness/refine、文件提交与定时作业降级；R-01～07 |
| [Crush](../crush/README.md) | `main` / `76cc5c574e15072b15aaed0f4f843a5711fae0d9` | Go App/Backend/Workspace、SQLite、终端/可选 HTTP-SSE、接纳取消、多端确认、tools/MCP/LSP/Skill；K-01～07 |

每份报告覆盖架构与模块依赖、核心数据结构、存储/提交/恢复、核心流程与时序、模型/工具/安全/协作/记忆/交互/扩展/评测、优势及代价、九模块与共同工程基线对照。51 项局部建议在综合分析中归并为 12 项，保留出处和既有优化关联；数量不代表已采用或测得收益。

原仓库分别为 [openai/codex](https://github.com/openai/codex)、[earendil-works/pi](https://github.com/earendil-works/pi)、[deepseek-ai/deepseek-harness](https://github.com/deepseek-ai/deepseek-harness)、[PrimeIntellect-ai/prime-agent](https://github.com/PrimeIntellect-ai/prime-agent)、[charmbracelet/crush](https://github.com/charmbracelet/crush)。研究当时使用固定提交快照，不声称覆盖完整提交历史。后续复核按[来源清单](sources.json)的仓库地址和准确 commit 下载到独立缓存，不把默认分支最新内容当成报告依据。

## 2. 共同分析与来源

- [六组核心对象与参考语义覆盖](core-model-semantic-coverage.md)：16 类语义逐项对照五个项目的七条实际路径，区分设计覆盖、内部记录、可选能力和未公开合同；运行能力仍待实现与验证。
- [数据对象与同场景读写对比](data-flow-io-comparison.md)：补充连续对话、模型—工具循环的对象组织、逻辑记录与实际持久化边界，以及本项目的复杂度收敛。
- [本项目架构比较基线](architecture-baseline.md)：九模块事实归属、ADR、工程选型与 24 项已采用方向。
- [架构优化分析](architecture-optimization.md)：逐模块对照、12 项建议、工程影响、实施依赖、验收/实验及局部编号追踪。
- [来源与基线清单](sources.json)：保留项目 remote、准确 commit、许可来源及核对边界，以及本项目当时的基线 commit。
- [日常检查说明](../../architecture/validation/README.md#6-全仓链接与研究来源维护)：核对保留文档、链接与来源元数据；参考源码只在需要时另行获取和验证。

## 3. 如何使用这些结论

**首批可靠闭环。** 综合 O-01～05、O-07、O-12 细化上下文、准确绑定、出站前耐久资格、结果范围和输入竞争；O-08 中精确安装及失败关闭是相应能力的前提。各项复用原 Task、Operation、Grant、Content、Surface 和 JobStore，没有另建事实负责方。

**后续能力。** O-06/08 按子 Agent 和扩展的真实能力开放；O-09～11 涉及经验候选、程序化工具和渐进式发现，只在独立权限/隔离/评测条件成立后实验。X-01～06 的既有统计含义保持，交互 code cell 等新因素另冻结对照。

本次采用源码静态追踪及多智能体分项目研究、根侧综合与交叉复核。没有安装上游依赖、启动模型/外部服务或执行上游测试/benchmark；不提供跨项目质量、延迟、费用或容灾排名。研究时的架构草案和 ADR 是比较依据，此次交付是研究报告及建议，运行实现和真实收益仍需后续取证。

用户随后确认据此刷新架构；原设计采用映射随架构草案移除，可通过 Git 历史查看。模型收敛的研究目标、约束及语义覆盖保留在[语义覆盖研究](core-model-semantic-coverage.md#core-model-scope)。来源清单与 architecture-baseline 保留调研时的准确版本；完整旧哈希及原验证输出可从[历史来源清单](https://github.com/ruipengliu/lerna/blob/f6b8f300dc034817cfcdac9c95ce6cfa3ee6a986/docs/research/agent-harness-comparison/sources.json)与[历史验证记录](https://github.com/ruipengliu/lerna/blob/f6b8f300dc034817cfcdac9c95ce6cfa3ee6a986/docs/research/agent-harness-comparison/verification.md)恢复。历史记录不能代替当前源码重验。新增读写对比区分参考源码行为与更新后本项目的设计阶段，不表示已运行性能测试。

源码复用还需按固定版本的实际许可判断。尤其 Crush 当前根许可为 FSL-1.1-MIT，含未来 MIT 条款，不能按“当前全部 MIT”复制；具体定位见[Crush 报告](../crush/README.md#1-定位版本与复用范围)。其他项目许可也在对应报告说明。
