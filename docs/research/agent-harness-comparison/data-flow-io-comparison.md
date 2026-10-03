# 数据与读写：关键结论

研究日期：2026-10-01。以下是固定源码的静态路径计数，未测量 IOPS、端到端时延或跨项目性能。源码版本见[清单](sources.json)；需要复核时再[下载对应源码](../README.md#source-download)。

## 1 可比较的场景与条件

A 是暖会话直接回答：一条输入、一次模型调用、一个完整答复。B 是读取后回答：第一次模型调用提出一次文本文件读取，第二次模型调用给最终答复。C 是关闭进程后重开，仅恢复原状态和未结责任，尚不自动调用模型。

A/B 固定短输入与短结果，默认已有合法权限、历史和标题；不触发 reasoning、压缩、记忆、子 Agent、确认或重试。模型计费正常返回。工具自身读取、初始化、可选能力、后台工作与独立质量核验另计。

| 指定路径 | A / B 的核心写入结论 | 持久确认与计数边界 |
| --- | --- | --- |
| Codex，本地 Paginated | 条件示例为 9 / 14 条 rollout 记录，另加投影写 | [live_writer](https://github.com/openai/codex/blob/d4a475adda850d80b6149c76454de94e0cf4fd51/codex-rs/thread-store/src/local/live_writer.rs#L316-L383) 追加并 flush 后更新 SQLite 投影；普通追加无显式 fsync。记录数不等于数据库事务数 |
| Pi，经典 v3 CLI | 2 / 4 条完整消息，分别为 2 / 4 次 appendFileSync | [SessionManager](https://github.com/earendil-works/pi/blob/8ce69e9d2b171d173fe4b6b2b6256f1f4411e69d/packages/coding-agent/src/core/session-manager.ts#L589-L604) 不逐流式片段保存；同步 append 不等于 fsync。不能套用到 AgentHarness 或独立 durable |
| DeepSeek，sdk-minimal 加小文本日志扩展 | 内核为 8 / 13 条事件；每次成功接受日志前缀再加一次，为 9 / 15 | 200ms 缓冲后非空批次 writeFile + sync；[存储](https://github.com/deepseek-ai/deepseek-harness/blob/639ed015397290b3745d163aafe02ffee4aa3f84/packages/session/session-persistence-jsonl/src/storage.ts#L274-L338)。该装配没有 base 的 checkpoint 策略，回复时尾部可能尚未落盘 |
| Prime，基础消息路径 | 2 / 4 条消息 | 每次 [append_cached](https://github.com/PrimeIntellect-ai/prime-agent/blob/5784abc2aef523a78d5a8850a0c0be89883388b2/crates/pa-core/src/session/manager/append.rs#L55-L95) 调用 write_all、flush、sync_data；其他状态文件与条件工作另计 |
| Crush，Coordinator→Run | A 为 4+X 次核心 DML；B 为 10+X，普通 view 另加一次 read_files upsert | [调用路径](https://github.com/charmbracelet/crush/blob/76cc5c574e15072b15aaed0f4f843a5711fae0d9/internal/agent/agent.go#L735-L759) 下 A/B 分别有 5/7 次显式 SELECT；X 是实际额外流式 UPDATE 数。触发器和 WAL 另计，客户端查询另计 |
| 本项目，设计阶段 | Brain 为 4 / 8 个基本持久阶段；B 的 Executor 另有 3 个基本阶段及适用资源入口 | 还包括 Task、内容发布、权限、领取、核验与结算；这些是逻辑提交阶段，尚无真实总 Tx/SQL/fsync 数据 |

DeepSeek 的计数条件为内核事件 `5 + 3M + 2T`，其中 M 为模型次数，T 为工具次数；日志扩展可能再加 M。启用 checkpoint 的装配另按语义 flush 与实际非空存储批次计数。Crush 的 X 不是 token 数；没有 ToolInputStart 回调时，B 的写入基数减少一次。不同确认点不能放在同一排名里。

## 2 对本项目的结论

本项目的额外持久事实来自目标、授权、效果恢复和费用承诺。优化重点是同域事实与 Job 共事务、原字节以 Content 复用、有界批读和按需启用扩展，而非按对象数量拆服务。

- 默认云端 PG 保存 Task 等权威事实，设备 SQLite 保存本机执行、门禁及补传账本；见[存储设计](../../architecture/data/storage.md)。
- Session 管理输入与任务关联；Task 的预算、推进和完成仍由 Orchestrator 裁决。流式呈现有界合并，避免逐 token 持久化。
- 同库准入和事实归并可以合并事务；跨 owner 交接仍分别提交，不能将两个库称为同一原子事务。
- 单进程用于开发调试；首次生产按独立网关、应用、工作池和执行宿主装配。见[工程切片](../../architecture/engineering/README.md#3-四条实施切片)。

## 3 实现后怎样验证

固定 A/B/C 以及文件保存后独立读回四种请求，分别测暖态、冷恢复和故障。记录逻辑事件、实际文件/SQL 调用、事务、flush/sync、WAL、正文与上下文字节、模型物理请求和完整费用。要保留失败与 unknown，且分别报告前台完成和后台收尾。

本次没有实测性能瓶颈或优化收益。只有在同样输入、耐久确认点、缓存条件和故障域下测得的数据才可比较；执行方案见[性能实验](../../architecture/validation/README.md#4-性能实验)。
