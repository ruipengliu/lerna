# 设计取舍与研究依据

本方案的技术方向是让智能策略可替换，同时让目标、效果、权限和费用可恢复、可追溯。前沿论文和开源实现用于发现机制与失败边界，不用排行榜或方法名字替代本项目证据。

来源核查截至2026-10-02。引用固定版本或官方文档；本轮对关键源码作只读核对，没有运行上游项目、论文实验或生产系统。文中选择是设计判断，不是收益已被验证。

## 1 现有ADR怎样落实

| 已定选择 | 本系列的具体落实 |
| --- | --- |
| [0001 最小关闭身份](../adr/0001-retain-closed-identities.md) | 命令墓碑、取消先到及终态不复活；正文清理独立 |
| [0002 Go/WSS/gRPC](../adr/0002-go-wss-grpc.md) | 独立服务JSON领域合同包在Protobuf外壳，同进程直接接口 |
| [0003 生产分布式](../adr/0003-production-distributed.md) | 三AZ、独立角色池、PG事实与Job同事务、内容介质单独验收 |
| [0004 固定Task路由](../adr/0004-discovery-fixed-task-routing.md) | 新Task先选owner，原命令首发前持久固定，扩容不改投 |
| [0005 证据分级](../adr/0005-evaluator-evidence-eligibility.md) | assessed保留限制；正常停用与判断缺陷分开 |
| [0006 条件优先](../adr/0006-adopt-requirements-before-actions.md) | 条件实际变化只提交新目标，同行动/完成建议失效 |
| [0007 独立旧版批准](../adr/0007-independent-rollback-approval.md) | 自动回退重新核准确旧批准、当前格式与新实例 |
| [0008 受信Renderer](../adr/0008-trusted-renderer-preview.md) | 正文实际取得/呈现由Renderer保证，引用不是阅读证明 |
| [0009 共用工作模板](../adr/0009-reliable-work-framework.md) | 接纳、Claim、work_revision和Finish统一，领域仍裁决成功 |
| [0010 单仓同版契约](../adr/0010-monorepo-shared-contract-release.md) | Go核心、SDK、Schema、接口和一致性证据同版维护 |

本系列未修改这些ADR。下列新增选择细化其实现合同，尚未作为新的已批准ADR记录；未来要推翻上述已定边界，必须明确重新决策，不能靠换文档标题覆盖。

## 2 本系列补齐的设计缺口

| 新选择 | 为什么选，代价是什么 |
| --- | --- |
| 可增长集合用完整关联索引、权威计数/修订和分页 | 解除有限数组与上千样本/环境的矛盾；完整性由owner事务保证，维护计数、索引和重建成本增加 |
| Task完成要求所有已准入目标Operation发送closed | 避免not_started操作在成功后迟到启动；需明确封闭未发意图并保存墓碑 |
| 同一事务域内的估算接受合同，跨事务域/分配/离线strict | 补本人接受入口且不伪造远端即时可见；部分无法给上界的能力会等待或不可用 |
| 退款单列净成本，不自动返可花预算 | 避免迟到贷记复活once或已封allocation；用户要扩大后续预算须显式调整 |
| 跨域EligibilityReceipt与缺陷交回 | 给证据当前资格明确窗口；窗口0只能同域事务，远程高保证部署更受限 |
| Session新目标队列与当前Task输入分别调度 | 避免后续目标挡住前项澄清；UI必须明确新目标、修改和回答 |
| Schedule固定时区/错过/重叠/原occurrence身份 | 补重启、编辑和取消竞态；默认跳过错过及重叠，牺牲自动补跑便利 |
| Environment与hostcall固定映射 | 复用计算状态而不重放外部世界；付出原调用账本、实例隔离与来源追踪成本 |
| 来源DAG、固定保留期限与授权前置检索 | 避免可滑动TTL、索引滞后和摘要掩盖限制；候选集合和召回可能更保守 |
| 正式评测永久尝试身份与独立曝光门禁 | 避免反复试到通过和选择偏差；失败/取消不能回收正式机会，成本透明 |

这些选择以对应章节的状态和失败路径为准。它们是明确的目标合同，仍需[验收](validation/README.md)建立运行保证。

## 3 为什么保留PG责任主线

Pi的恢复入口只附着原记录并返回开放工作，不自动发模型/工具；这支持区分“读懂原责任”与“有权继续执行”。[Pi harness，8ce69e9，L375–408](https://github.com/earendil-works/pi/blob/8ce69e9d2b171d173fe4b6b2b6256f1f4411e69d/packages/agent/src/harness/runtime/harness.ts#L375-L408)

DeepSeek的checkpoint在有关联Session的路径保护发送，但无Session和嵌套工具存在旁路。因此本项目验收必须计数真实出口，包含摘要、标题、直调adapter和hostcall。[DeepSeek checkpoint，639ed01，L20–82](https://github.com/deepseek-ai/deepseek-harness/blob/639ed015397290b3745d163aafe02ffee4aa3f84/packages/session/session-checkpoint-policy/src/index.ts#L20-L82)

Prime的cron锁失败仍运行且忽略状态写错误，Crush的MustDeliver可能在50ms后丢通知。它们提示代码名称不足以说明持久语义，本方案选择“事实/Job同事务，通知仅hint”。[Prime state，5784abc，L261–375](https://github.com/PrimeIntellect-ai/prime-agent/blob/5784abc2aef523a78d5a8850a0c0be89883388b2/crates/pa-core/src/cron/store/state.rs#L261-L375)；[Crush broker，76cc5c5，L1–48](https://github.com/charmbracelet/crush/blob/76cc5c574e15072b15aaed0f4f843a5711fae0d9/internal/pubsub/broker.go#L1-L48)

Temporal/DBOS可以运行Agent工作流，不是技术上不适合。当前不引入它们，是减少第二套责任、重放及版本治理的工程取舍。DBOS的恢复同样要求步骤安全重试、工作流确定性和版本兼容；它不消除任意外部副作用的未知。[DBOS恢复架构](https://docs.dbos.dev/architecture#how-workflow-recovery-works)

## 4 为什么未知效果要保留

Pi恢复effect_pending同时检查保存的与当前工具的safe replay声明，不能只信旧配置。[Pi replay，8ce69e9，L478–539](https://github.com/earendil-works/pi/blob/8ce69e9d2b171d173fe4b6b2b6256f1f4411e69d/packages/agent/src/harness/runtime/drive/tools.ts#L478-L539) DeepSeek区分未启动与结果未知，但修补对话不等于建立目标效果账本。[DeepSeek repair，639ed01，L14–97](https://github.com/deepseek-ai/deepseek-harness/blob/639ed015397290b3745d163aafe02ffee4aa3f84/packages/core/session/src/repair.ts#L14-L97)

近期exactly-once预印本说明：超时和空读回可能同时对应未执行与延迟提交。要安全重发须目标提供足够幂等/查询前提，不能只靠调用者观察。[Where Does Exactly-Once Live，v1](https://arxiv.org/html/2609.29095v1#S3) 本设计宁可保留unknown和预留，也不冒险创建第二次非幂等效果；代价是有些任务不得自动收束。

## 5 上下文和能力策略的借鉴

Codex分开历史版本、保留上下文和用户消息修订，且canonical写入先于SQLite投影；这启发权威事实与可重建投影分开，不能从该局部代码推断所有存储都已fsync。[ContextManager，d4a475a，L89–123](https://github.com/openai/codex/blob/d4a475adda850d80b6149c76454de94e0cf4fd51/codex-rs/core/src/context_manager/history.rs#L89-L123)；[live writer，L327–382](https://github.com/openai/codex/blob/d4a475adda850d80b6149c76454de94e0cf4fd51/codex-rs/thread-store/src/local/live_writer.rs#L327-L382)

Codex的准备调用绑定原catalog lease，Pi的渐进MCP exposure提供大工具集选材机制，均不证明本项目召回或总成本改善。[Codex binding，L304–362](https://github.com/openai/codex/blob/d4a475adda850d80b6149c76454de94e0cf4fd51/codex-rs/codex-mcp/src/binding.rs#L304-L362)；[Pi discovery，L441–477](https://github.com/earendil-works/pi/blob/8ce69e9d2b171d173fe4b6b2b6256f1f4411e69d/packages/coding-agent/src/extensions/mcp/index.ts#L441-L477)

DeepSeek的session日志还包含原event.data和头部信息，提醒我们核查messages之外的metadata/日志出口。[session log，639ed01，L38–110](https://github.com/deepseek-ai/deepseek-harness/blob/639ed015397290b3745d163aafe02ffee4aa3f84/packages/session/session-log-deepseek/src/index.ts#L38-L110) 压缩研究适合作为候选对照，不能支持删除控制和效果证据。[CliffCompaction，v1](https://arxiv.org/html/2609.26779v1)

每Decision最多一次物理请求是恢复和费用身份的明确选择，不是“统一授权必然要求”的定理。有限计划可减少额外交接；是否净收益由同任务实验决定。

## 6 记忆库不替代我们的权限权威

Graphiti区分事实有效、失效和参考时间，支持时间字段不混用。[Graphiti，6b4b56f，L263–282](https://github.com/getzep/graphiti/blob/6b4b56ff6f4b1e4e69c3c3c5487cf1b8762c483a/graphiti_core/edges.py#L263-L282)

LangGraph的TTL默认允许读取刷新，过期省略还依赖配置/后端。因此本设计的retention_until是固定上限，读取门禁不等垃圾回收。[TTLConfig，07b3318，L545–575](https://github.com/langchain-ai/langgraph/blob/07b33185eab893be2ed031eedae52f09314bf77c/libs/checkpoint/langgraph/store/base/__init__.py#L545-L575)

Mem0的BM25在语义候选上加分，并非两路候选并集；delete仍可能保留历史值。memU的存储批次可部分成功。不能直接把库API叫“混合召回”“delete”或“commit”就承诺相同语义。[Mem0搜索，94c3fe9，L1642–1701](https://github.com/mem0ai/mem0/blob/94c3fe9f238f3dbf29c9ce98643bd71eb13077cd/mem0/memory/main.py#L1642-L1701)；[Mem0删除，L2114–2142](https://github.com/mem0ai/mem0/blob/94c3fe9f238f3dbf29c9ce98643bd71eb13077cd/mem0/memory/main.py#L2114-L2142)；[memU，2c050bc，L343–391](https://github.com/NevaMind-AI/memU/blob/2c050bc9681a4c0aff1af211a000e73d14f33356/src/memu/app/agentic.py#L343-L391)

撤销记忆论文需连同配置和全文看：其中mem0(exp.)特意关闭默认过期过滤，不能概括为所有库默认都返回撤销项。[Revoked but Still Authoritative，v1](https://arxiv.org/html/2609.08258v1) 本项目分别测识别、状态暴露、每次使用门禁和派生清理。

## 7 前沿实验怎样转成我们的验收

Harness Value区分目标真值、完成声明和接纳判断；终态验证不能撤销已经发生的错误效果。全文明确披露端点版本与请求参数记录不全、缺失实验格及样本范围限制，本项目不沿用摘要效应量作收益预测。[Harness Value，v1](https://arxiv.org/html/2609.20474v1)

Coding harness消融同时改变工具、提示、文件状态和诊断，每格任务运行次数有限，不能推导“工具越少越好”。[Action-space消融，v1](https://arxiv.org/html/2609.20804v1) 可靠性研究把一致性、稳健性、可预测性和安全分开，也不构成最坏攻击保证。[Agent Reliability，v1](https://arxiv.org/html/2602.16666v1)

程序图研究在训练批次间演进，测试时冻结；它支持“生成候选、隔离评测、受控发布”，不支持在线随意改生产内核。[Procedural Graphs，v1](https://arxiv.org/html/2609.09153v1)

## 8 仓库研究的使用范围

继续查阅[五项目比较](../research/agent-harness-comparison/README.md)、[语义覆盖](../research/agent-harness-comparison/core-model-semantic-coverage.md)、[IO口径](../research/agent-harness-comparison/data-flow-io-comparison.md)、[记忆库](../research/agent-memory-libraries-2026-09-28.md)、[记忆论文](../research/agent-memory-papers-2026-09-28.md)、[项目启示](../research/ai-report-2026-09-project-implications.md)和[系统一模型](../research/system-one-models-2026-09-28.md)。它们提供候选与索引，新正文已直接给出必要合同。

历史静态引用检查和形式模型只适用于当时资产、假设与性质；尤其每用户固定权威与每Task固定owner不同，不能简单改路径沿用旧证明。这里没有运行TLC/Lean、参考程序、真实模型或数据库恢复实验。后续证据必须绑定本版内容与实际实现，不能以研究文件数量或链接通过率冒充设计正确性。
