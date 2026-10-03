# Agent Harness 比较：关键结论

研究日期：2026-10-01。结论来自固定源码的静态追踪，没有运行上游测试、模型或跨项目性能实验。主要价值是借鉴内部组织与恢复机制；本项目的权威归属、授权、效果和费用规则由[现行架构](../../architecture/README.md)定义。

## 1 五个项目值得借鉴什么

| 项目 | 关键结果 | 本项目采用时的边界 |
| --- | --- | --- |
| Codex | Session/Turn/Step 组织模型与工具；canonical rollout 与 SQLite 历史投影分开，工具调用可预先固定绑定。[持久写入][E01]、[准备调用][E02] | 另有独立元数据与后台责任；回合结束、取消回执和补出的 aborted 消息均不证明外部效果撤销或目标完成 |
| Pi | 经典 CLI、公开 AgentHarness、独立 pi-durable 是不同运行与存储合同；open 恢复与再次 drive 分开。[恢复入口][E26]、[工具发送与重放][E03] | v3/v4/独立 durable 格式与保证不能混用；后端原子提交不提供外部副作用原子性 |
| DeepSeek Harness | 原事件、模型视图、存储接纳与 flush 分开；checkpoint 保护已接入的实际出站路径。[接纳][E05]、[checkpoint][E06]、[恢复分类][E07] | base 与 sdk-minimal 装配不同；无 Session 和嵌套路径须另查，不因安装插件就推断所有出口均受保护 |
| Prime Agent | 持续目标保存无进展依据；上下文使用机械摘要；RLM kernel 区分中断和可复用状态。[目标推进][E08]、[压缩][E09]、[kernel][E10] | 会话、队列、子任务和 cron 是不同文件提交；直接 core 调用不自动具有 daemon 的恢复保证 |
| Crush | Go 分层、输入接纳/取消水位、流合并与终结 flush、多界面先到确认可作工程参考。[Agent][E11]、[发布通道][E12]、[确认][E13] | SQLite 消息与内存 queue/waiter 的耐久性不同；Accepted、SSE 或 RunComplete 不代表持久任务完成 |

## 2 保留的工程建议

编号用于追溯原研究建议，采用状态以对应模块为准。

| 编号 | 建议与判断依据 | 实施入口 |
| --- | --- | --- |
| O-01 | 用有来源的投影组装上下文；目标、授权和未知效果从原事实重建，摘要不能覆盖它们 | [Brain](../../architecture/brain/README.md) |
| O-02 | 持久记录推进次数、无进展原因和下一次可检查条件，重启不重置限额 | [任务编排](../../architecture/orchestrator/README.md) |
| O-03 | 固定最终参数、能力版本和准确绑定后再准入；hook 改参须重新核验 | [执行](../../architecture/execution/README.md) |
| O-04 | 先恢复原责任，再判断当前是否允许新调用；效果未知沿原 Operation 核对 | [持久工作](../../architecture/runtime/README.md) |
| O-05 | 适配器分别提供原输出、覆盖/缺口和实际物理调用，不把合法 JSON 当成数据完整 | [执行](../../architecture/execution/README.md) |
| O-06 | 子 Agent 辅助接口复用原 Delegation；取消、结果范围、未知效果和费用各自核清 | [协作](../../architecture/collaboration/README.md) |
| O-07 | 写明多端输入、确认、旧流和分支的竞争规则，断线后读取原权威状态 | [交互](../../architecture/interaction/README.md) |
| O-08 | 安装制品、依赖、配置代次、批准与当前实例就绪一起核对 | [扩展](../../architecture/extensions/README.md) |
| O-09 | 经验精炼只形成准确候选，经独立保存许可、冻结评测与批准后采用 | [记忆](../../architecture/memory/README.md)、[评测](../../architecture/evaluation/README.md) |
| O-10 | 程序化工具作为有界适配器实验；保留 hostcall 身份、隔离和实际退出证据 | [执行环境](../../architecture/execution/README.md#7-程序化工具与可复用环境) |
| O-11 | 渐进目录发现与 Skill 加载独立对照，测召回、误选、额外调用和完整成本 | [渐进发现](../../architecture/brain/README.md#5-能力和-skill-的渐进发现) |
| O-12 | 共享重复、乱序、丢回执、旧实例等故障语料；机制测试与质量实验分别取证 | [验收](../../architecture/validation/README.md) |

首先完成可恢复主线，再按实际需要启用子任务、经验、程序工具与动态发现。六组对象和范围见[语义结论](core-model-semantic-coverage.md)，存储计量见[读写结论](data-flow-io-comparison.md)。

<a id="reports"></a>
## 3 源码版本与后续使用

| 项目 | 固定源码版本 | 建议本地目录 |
| --- | --- | --- |
| codex | [d4a475ad](https://github.com/openai/codex/tree/d4a475adda850d80b6149c76454de94e0cf4fd51) | `.reference/codex` |
| pi | [8ce69e9d](https://github.com/earendil-works/pi/tree/8ce69e9d2b171d173fe4b6b2b6256f1f4411e69d) | `.reference/pi` |
| deepseek-harness | [639ed015](https://github.com/deepseek-ai/deepseek-harness/tree/639ed015397290b3745d163aafe02ffee4aa3f84) | `.reference/deepseek-harness` |
| prime-agent | [5784abc2](https://github.com/PrimeIntellect-ai/prime-agent/tree/5784abc2aef523a78d5a8850a0c0be89883388b2) | `.reference/prime-agent` |
| crush | [76cc5c57](https://github.com/charmbracelet/crush/tree/76cc5c574e15072b15aaed0f4f843a5711fae0d9) | `.reference/crush` |

机器可读版本见 [sources.json](sources.json)。后续复核或移植时再按[下载说明](../README.md#source-download)取得所需项目；当前文档不依赖本地源码副本。Crush 的研究版本为 FSL-1.1-MIT，复制代码前核对许可证及适用条件。

[E01]: https://github.com/openai/codex/blob/d4a475adda850d80b6149c76454de94e0cf4fd51/codex-rs/thread-store/src/local/live_writer.rs#L327-L382
[E02]: https://github.com/openai/codex/blob/d4a475adda850d80b6149c76454de94e0cf4fd51/codex-rs/core/src/mcp_tool_call.rs#L436-L480
[E03]: https://github.com/earendil-works/pi/blob/8ce69e9d2b171d173fe4b6b2b6256f1f4411e69d/packages/agent/src/harness/runtime/drive/tools.ts#L478-L539
[E05]: https://github.com/deepseek-ai/deepseek-harness/blob/639ed015397290b3745d163aafe02ffee4aa3f84/packages/core/session/src/index.ts#L711-L795
[E06]: https://github.com/deepseek-ai/deepseek-harness/blob/639ed015397290b3745d163aafe02ffee4aa3f84/packages/session/session-checkpoint-policy/src/index.ts#L20-L82
[E07]: https://github.com/deepseek-ai/deepseek-harness/blob/639ed015397290b3745d163aafe02ffee4aa3f84/packages/core/session/src/repair.ts#L14-L97
[E08]: https://github.com/PrimeIntellect-ai/prime-agent/blob/5784abc2aef523a78d5a8850a0c0be89883388b2/crates/pa-core/src/session_engine/goal_driver.rs#L590-L688
[E09]: https://github.com/PrimeIntellect-ai/prime-agent/blob/5784abc2aef523a78d5a8850a0c0be89883388b2/crates/pa-core/src/session_engine/compact_session/prepare.rs#L87-L149
[E10]: https://github.com/PrimeIntellect-ai/prime-agent/blob/5784abc2aef523a78d5a8850a0c0be89883388b2/prime-agent-runtime/src/rlm/__init__.py#L390-L425
[E11]: https://github.com/charmbracelet/crush/blob/76cc5c574e15072b15aaed0f4f843a5711fae0d9/internal/agent/agent.go#L1299-L1410
[E12]: https://github.com/charmbracelet/crush/blob/76cc5c574e15072b15aaed0f4f843a5711fae0d9/internal/pubsub/broker.go#L1-L48
[E13]: https://github.com/charmbracelet/crush/blob/76cc5c574e15072b15aaed0f4f843a5711fae0d9/internal/permission/permission.go#L111-L174
[E26]: https://github.com/earendil-works/pi/blob/8ce69e9d2b171d173fe4b6b2b6256f1f4411e69d/packages/agent/src/harness/runtime/harness.ts#L375-L408
