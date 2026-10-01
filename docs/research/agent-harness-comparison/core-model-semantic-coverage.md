# 六组核心对象与参考项目语义覆盖

研究日期：2026-10-01，Asia/Shanghai。本文按固定源码比较行为，服务于[核心模型收敛规格](../../../.scratch/harness-core-model-simplification/spec.md)。六组对象可以组织共同执行主线；完整承载参考语义还需要有身份的内部记录、准确配置及具有独立生命周期的扩展对象。下表不把同名类型、可序列化字段或设计映射计为运行能力。

本文是研究映射，规则仍由各[架构模块](../../architecture/README.md)负责。默认应用入口、对象归属和逐请求读写分别由本轮架构设计收敛；本报告不发布新协议，不改写[历史研究基线](architecture-baseline.md)或 [sources.json](sources.json)。运行内核、SDK、数据库和上游运行均未在本轮实现或运行，所有本项目语义的**运行验证状态均为未验证**。

## 1. 装配路径和判读口径

| 路径代号 | 固定项目与实际入口 | 存储、启用条件及不能混用的保证 |
| --- | --- | --- |
| C | [Codex](../codex/README.md)，`d4a475adda850d80b6149c76454de94e0cf4fd51`；CLI/TUI/app-server → `CodexThread` → `Session` | canonical rollout JSONL；本地默认 Paginated，SQLite history 是投影，Goal/queue/memory 库还有独立事实。feature gate、审批和 sandbox 配置影响可用能力；不是云端产品功能全集。[C-线程入口][C-本地存储边界][C-default] |
| P-C | [Pi 经典 CLI](../pi/README.md)，`8ce69e9d2b171d173fe4b6b2b6256f1f4411e69d`；SDK → `AgentSession` → classic `Agent` | Session v3 JSONL 树；同步 append 不等于 fsync。CLI extension 运行于 Node 宿主；subagent 是可选示例，不是默认持久委派。[P-sdk-agent][P-legacy-types][P-legacy-persist] |
| P-H | 同一 Pi 快照的公开 `AgentHarness` → Lane → operation driver | Session/Branch/Lane/Operation；Memory、v4 JSONL 或独立 SQLite backend。create/open 还原后由调用者驱动；backend 原子提交不代表跨进程执行权或外部效果原子性。[P-harness-types][P-harness-open][P-storage-port] |
| P-D | 同一 Pi 快照的独立 `pi-durable` → Conversation/Submission → TaskScheduler | 自有 Memory/JSONL/SQLite，不能与 P-C/P-H 的文件格式及屏障混用。这里的 Task 是持久阶段作业，Session 是容器；没有自动获得本项目目标条件与完成裁决语义。[P-durable-types][P-durable-storage][P-scheduler] |
| D | [DeepSeek Harness](../deepseek-harness/README.md)，`639ed015397290b3745d163aafe02ffee4aa3f84`；profile/bundle → AgentLoop | Session v4 事件与 JSONL 持久插件。base 挂载 checkpoint policy；sdk-minimal 是独立装配，不因此继承 base 的该策略。Goal、Schedule、terminal、subagent 等按实际插件可用性判断。[D-S08][D-profile-base][D-profile-minimal][D-S26] |
| R | [Prime Agent](../prime-agent/README.md)，`5784abc2aef523a78d5a8850a0c0be89883388b2`；daemon worker → SessionEngine → agent loop/Python kernel | Session JSONL、worker queue journal、RLM ledger、harness/cron/snapshot 文件分属不同提交。直接 core 调用不自动具备 daemon 恢复能力；命令 journal 有实现不能证明普通 prompt 全链已接入。[R-PA06][R-PA13][R-PA11][R-PA15] |
| K | [Crush](../crush/README.md)，`76cc5c574e15072b15aaed0f4f843a5711fae0d9`；本地 Workspace/Coordinator 或可选 HTTP Backend → SessionAgent | SQLite sessions/messages/files；active/queued run、确认 waiter、后台 shell 主要在内存。HTTP Accepted 和终结 SSE 不是持久任务接纳或可靠重放。[K-CR05][K-CR18][K-CR19][K-CR50] |

“未发现”限于所列入口、类型及已追踪调用链，不断言外部插件永远无法增加该能力。源码包含测试不等于本轮测试已执行。一个特性的设计覆盖按以下四类登记；混合项分别写明基础部分和扩展部分：

| 标记 | 含义 |
| --- | --- |
| 已有设计 | 本轮前已由架构明确承载，收敛只调整导航和组合方式；没有运行保证 |
| 本轮补齐 | 本轮须写清对象归属、生命周期或接口缺口；不代表新增机器合同或代码实现 |
| 按需扩展 | 有明确负责方与启用前提，普通请求不依赖它；完整功能留在独立能力切片 |
| 本阶段不交付 | 本轮不实现、测试或发布该运行能力/公共合同；须明确剩余差异 |

## 2. 本项目承载与设计覆盖矩阵

六组核心是聚合和接入组织，不是六张表。Session 由应用/交互组织；Task 由固定 Orchestrator 裁决；Decision 由 Brain 管理；Operation 的执行事实由 Executor 管理，原 OperationIntent 仍归 Orchestrator；Content 保存准确字节和版本；Grant 由权限模块管理。Confirmation 仍由实际业务负责方保存并一次消费，不能全部塞入 Grant。Capability、Binding、模型配置和 InstallLock 是必要配置；Memory、Schedule、执行环境和可复用 child 可以拥有独立生命周期。

| 语义族 | 六组对象及必要记录 | 设计覆盖和剩余差异 | 负责入口 / 运行验证 |
| --- | --- | --- | --- |
| [SM-01 会话历史](#sm-01) | Session + Content；消息身份/顺序/来源、原提交与 Task 关联 | 本轮补齐：线性对话、保存与接纳分开、归档不取消任务。完整分支另见 SM-09 | [Session](../../architecture/.draft/interaction/session-and-task.md)；未验证 |
| [SM-02 Turn / Run](#sm-02) | Session/Task 的查询投影；Command、InputSubmission、InputRequest 保留原身份 | 本轮补齐：回合结束、目标完成、工作进程退出分别表达，不建平行 Run 目标状态机 | [Session](../../architecture/.draft/interaction/session-and-task.md)、[交互](../../architecture/.draft/interaction/implementation.md)；未验证 |
| [SM-03 输入调度](#sm-03) | Task + 原提交子记录；排队/消费位置和控制范围 | 既有 InputRequest 回答的消费与 interaction.input_withdraw 已有设计；Session 自由输入的通用队列、steer/follow-up、多 Lane 按需扩展。全任务取消不能冒充单输入撤回 | [输入竞争](../../architecture/.draft/interaction/implementation.md#input-control-races)；公共差异 G-02，未验证 |
| [SM-04 压缩与模型视图](#sm-04) | Decision 输入投影 + Content；摘要来源、覆盖范围、额外 ModelCall | 已有设计；本轮补齐 Session 历史与模型输入区别。摘要不能删去权威目标、授权、未知效果 | [Brain 重建](../../architecture/.draft/brain/implementation.md#snapshot-reconstruction)；未验证 |
| [SM-05 模型步骤](#sm-05) | Decision；Snapshot/BrainContext、可选 ModelCall、Proposal、实际用量 | 已有设计；每 Decision 至多一次物理请求，辅助推理与透明重试须展开为独立责任 | [Brain](../../architecture/.draft/brain/README.md#model-recovery)；未验证 |
| [SM-06 工具与未知效果](#sm-06) | Operation；原意图、Attempt、Effect、结果 Content、结算关联 | 已有设计；本轮补齐统一查询视图，不合并跨负责方交接为假原子事务；unknown 不因生成回复而消失 | [Executor](../../architecture/.draft/execution/implementation.md)、[可靠工作](../../architecture/.draft/reliable-work.md)；未验证 |
| [SM-07 批准与隔离](#sm-07) | Grant + 使用/结算子记录；业务 Confirmation；准确 Binding/InstallLock | 已有设计；沙箱是执行机制，Skill/hook/目录声明不能授权，批准不证明物理启动或终止 | [权限](../../architecture/.draft/security/implementation.md#final-tool-use-check)；未验证 |
| [SM-08 持续目标](#sm-08) | Task；目标修订/Requirement/控制/预算/推进计数/ConditionResult/Result | 已有设计 + 本轮补齐继续权：目标 active、工作存在、当前准许新费用/效果分别检查 | [有界推进](../../architecture/.draft/orchestrator/implementation.md#bounded-progress)；未验证 |
| [SM-09 分支与回退](#sm-09) | Session 分支子记录 + Content；父链/分支头/来源截止/配置摘要范围 | 按需扩展；本轮补齐承载和禁止复制责任的规则。首版线性历史不具备完整分支功能，公共分支合同本阶段不交付 | 交互/应用 + 各原事实负责方；G-01，未验证 |
| [SM-10 子 Agent](#sm-10) | 子 Task/子 Session + Delegation + 精确启动配置 | 委派恢复已有设计；可复用 child 的持续身份与激活分开为按需扩展。历史 fork 不等于创建委派 | [协作](../../architecture/.draft/collaboration/implementation.md#cold-child-recovery)；G-03，未验证 |
| [SM-11 定时与周期触发](#sm-11) | 独立 Schedule + occurrence；产生原 Command/Task 或输入 | 按需扩展；本轮补齐归属/去重/停用边界。JobStore 只提供可靠执行机制，公共 Schedule 协议本阶段不交付 | 应用调度能力 + 固定 Task 负责方；G-04，未验证 |
| [SM-12 可复用执行环境](#sm-12) | Executor 环境资源记录；多个 Operation 引用同一环境，Content 保存受限检查点 | 按需扩展；本轮补齐占用、实际退出与恢复范围。cell 取消回执不释放仍忙的环境 | [程序化工具](../../architecture/.draft/execution/programmatic-tools.md)；G-05，未验证 |
| [SM-13 长期记忆](#sm-13) | 独立 Memory + Content + Grant；来源/范围/版本/冲突/关闭记录 | 已有设计，按需启用；本轮明确 Content 不替代记忆生命周期，普通记忆纠正不必走软件发布 | [Memory](../../architecture/.draft/memory/implementation.md)；G-06，未验证 |
| [SM-14 Skill / 插件](#sm-14) | 必要 Capability/Binding/InstallLock 配置；独立 Skill/Plugin/激活及发布记录 | 静态能力绑定已有设计；动态发现/安装按需扩展。正文和可执行代码、准备完成和已获准分别表达 | [扩展](../../architecture/.draft/extensions/implementation.md#staged-readiness)；G-07，未验证 |
| [SM-15 流呈现](#sm-15) | Decision/Operation 的 provisional 流 + Surface/Session 投影 + Content 正式结果 | 已有设计；短暂片段有界且可丢/合并，宣称事实已提交的提示须在提交后；不逐 token 建作业 | [呈现边界](../../architecture/.draft/interaction/implementation.md#surface-publication)；未验证 |
| [SM-16 重连与冷恢复](#sm-16) | 原 Command/Task/Decision/Operation/Grant、Surface 版本；Job/Claim/outbox 内部机制 | 已有设计；本轮补齐聚合后的原身份查询。重连不重放未知动作，快照不意味着外部世界恢复 | [恢复就绪](../../architecture/.draft/deployment.md#recovery-readiness)、[传输](../../architecture/.draft/contracts/transport.md)；未验证 |

## 3. 十六类语义的固定源码对照

每行同时说明实际对象、生命周期和恢复边界。P-C/P-H/P-D 各行独立成立，不能把 P-D 的持久提交或 P-H 的 safe replay 赋予默认 CLI。

<a id="sm-01"></a>
### SM-01 会话历史

| 路径 | 对象、生命周期与恢复含义 |
| --- | --- |
| C | ThreadId/SessionMeta 与 typed RolloutPayload；JSONL 原历史先写，分页 SQLite 后物化。ThreadId 与 revert 后的 RolloutId 可以不同；查询投影不是全部独立 SQLite 状态的替身。[C-会话元数据][C-历史载荷][C-历史写入顺序] |
| P-C | Session header 与 `entry.id/parentId` 形成树；user/assistant 首次出现才物化文件；打开文件重建 byId/leaf，历史与模型投影分开。[P-legacy-types][P-legacy-index][P-legacy-persist] |
| P-H | Session 是存储容器，Branch 是具名 tip，Lane 保存配置/inbox/当前 operation；Storage commit 原子处理记录，但采用 Memory backend 就只有内存保证。[P-harness-types][P-storage-port] |
| P-D | Session 容器中的 ConversationId/EntryId；Entry 的 model 与 data 分别承载模型和应用内容，Seq 标记提交。Conversation 的历史 parent 与执行 owner 是两种边。[P-durable-types][P-durable-records][P-durable-storage] |
| D | 不可变 Header + 连续 SessionSeq 事件；SessionLogOffset 是前缀长度而非事件位置。内存 append、storage accept、flush 各有成功点；恢复以已耐久前缀为限。[D-S09][D-S14][D-S18] |
| R | SessionHeader 与 FileEntry 父链；权威 JSONL 和可丢 window cache 分开。普通 append 失败回滚内存，retained 变体可保留 live index 并报告错误，界面可见不等于耐久。[R-PA07][R-PA13][R-PA43] |
| K | Session → Message → ContentPart，SQLite 保存正文和元数据；历史可以重读，内存 run/permission waiters 不因此可恢复。[K-CR10][K-CR11][K-CR14] |

<a id="sm-02"></a>
### SM-02 Turn / Run

| 路径 | 对象、生命周期与恢复含义 |
| --- | --- |
| C | TurnContext 下有多个 Step；TurnComplete 的状态映射是回合终结。暂停移交可恢复原 turn_id，但 pending 输入/waiter 不完整随迁，不能解释为 Task 成功或完整外部效果恢复。[C-回合运行类型][C-状态映射][C-暂停移交] |
| P-C | `turn_end` 是一次 assistant/tool 批次边界，`agent_end` 是 loop 停止；steer/follow-up 可继续循环，未发现经典 loop 独立持久 Run 聚合。[P-loop][P-agent-types] |
| P-H | OperationMeta 固定 operationId/lane/sourceTip/intent，活动阶段与最终 Result 分开；这里的 Operation 能包含整段模型/工具运行，不等于本项目有界工具 Operation。[P-harness-types][P-harness-api] |
| P-D | Submission 是一次输入责任，Task 是可检查点阶段作业；queued/placed/done/unanswered 与 Task 的 running/waiting/terminal 分开，任务完成不自动等于领域目标已核验。[P-durable-records][P-durable-task-state] |
| D | turn 从 claim 前打开，step 含模型及工具处理；turn 可零 step 地结束/阻塞，普通无工具回复也可 completed。[D-S10][D-S11] |
| R | queue 的 TurnSettle 区分 Completed/Aborted/Withdrawn/Failed；run_loop 结束与 GoalState 是独立状态，不构成 Requirement 验证。[R-PA11][R-PA20][R-PA08] |
| K | SessionAgentCall.RunID 由调用者提供，AcceptedRun 是内存预约；RunComplete 对准原提交，摘要续跑保留同 RunID。结束通知不证明效果或目标成功。[K-CR18][K-CR23][K-CR37] |

<a id="sm-03"></a>
### SM-03 输入排队、插入与撤回

| 路径 | 对象、生命周期与恢复含义 |
| --- | --- |
| C | 安装 queue 扩展后 QueuedItem 有独立 id、thread、顺序，支持更新/删除/重排；start-if-idle 成功后另删队列项。这是持久等待输入，不能从两次调用顺序推断与回合接纳原子。[C-队列表][C-queue-service] |
| P-C | Agent 内存 steering/follow-up 队列，支持 all/one-at-a-time；steer 在正常轮边界生效，已启动工具先完成。RPC success/disposition 不提供跨进程原命令去重。[P-agent-control][P-loop][P-rpc-ack] |
| P-H | Lane inbox 的 steer/followUp/nextRun/write 与 Entry 分开；持久 operation 身份和当前 lane 变更由 SessionMutation 组织，不能仅用最后一条消息判断是否消费。[P-harness-types][P-lane-command] |
| P-D | SubmissionId 与 conversation-scoped requestId，queued 未入历史、placed 已被运行持有、done 关联 answer；abortSubmission/withdraw 由 admission 与 inbox 处理，不是删除一段文本。[P-durable-records][P-durable-submission-api] |
| D | MessageId 在 pending 集合不可重复；`agent/inbox/spliced` 重建 next-turn/next-step，边界 claim 移除。消费需合看 splice/turn/step；splice 的内存发布仍须持久策略。[D-S36][D-S37][D-S14] |
| R | QueuedItem 的 steer/follow-up、来源优先级、终结类别与 worker recovery checkpoint 分开；busy 与 queue 同次读取后写 journal，写失败分支会跳过 checkpoint，不能无条件宣称已耐久接纳。[R-PA11][R-queue-checkpoint] |
| K | accept sequence/cancel 水位覆盖之前接纳，之后新输入不受旧取消影响；有 RunID 的队列项保留独立运行，旧 defer 用 compare-and-delete 避免清掉新运行。均是所读实例内控制。[K-CR22][K-CR23][K-CR53] |

<a id="sm-04"></a>
### SM-04 上下文压缩与消费视图

| 路径 | 对象、生命周期与恢复含义 |
| --- | --- |
| C | ContextManager 分模型窗口与 retained host facts；checkpoint 带窗口/配置信息，恢复只采用有效完整 checkpoint。补 `aborted` tool result 是请求配对修复，不是目标效果证据。[C-上下文宿主事实][C-压缩恢复][C-提示归一化] |
| P-C | compaction/branch_summary/context_edit 追加记录，按 leaf 路径投影；摘要有 firstKeptEntryId 与 usage。旧 JSONL 保留，摘要失败/retry 有额外模型成本。[P-projection][P-legacy-compaction][P-summary-call] |
| P-H | operation state 含 summary/navigation，Entry 树与当前 projection 分开；原 tip/config/state 恢复一致性由 restore 检查，不是从最后一段摘要猜测运行状态。[P-harness-types][P-restore] |
| P-D | CompactionTask 冻结摘要请求，summary entry 的 head 指向保留边界；generation-owned 与 conversation-owned 提交路径不同，后者经 write Submission，不偷改忙碌会话。[P-durable-compaction][P-durable-compaction-place] |
| D | summary 记录 model/usage/shadowed range，surface replacement 带 sourceEventSeqs；原 events 保留。未配对 compaction/start 可诊断，视图替换不是删除原事实。[D-S19][D-S33] |
| R | CompactionEntry 固定 first_kept_entry_id、usage；harness digest/fingerprint 在模型摘要后机械生成并随记录保存。摘要不是目标/授权完整性的证明。[R-PA26][R-PA54] |
| K | summary_message_id 控制后续上下文查询，保留旧消息；孤立结果过滤、缺失结果合成 interrupted 是格式修复，不能据此新开操作重放写效果。[K-CR27][K-CR28][K-CR29] |

<a id="sm-05"></a>
### SM-05 模型步骤、辅助调用与用量

| 路径 | 对象、生命周期与恢复含义 |
| --- | --- |
| C | capture_step 固定模型/工具/环境/权限；采样允许 provider 配置的 stream retry。一个 Turn/Step 不能直接视为本项目一次物理请求的 Decision。[C-步骤冻结][C-采样重试] |
| P-C | AgentMessage 转 TranscriptContext 后进入 Provider；AssistantMessage 有 provider/model/responseId/usage。经典 SDK 支持 retry，摘要也是模型调用，不能只按 UI 回合计费。[P-ai-message][P-ai-context][P-sdk-retry] |
| P-H | assistant.effect_pending 保留 responseEntryId 和已提交 frame prefix；恢复合成中断响应而不再发 provider 请求。此处 ZERO_USAGE 是合成表示，不证明未知供应商费用为零。[P-assistant-recovery] |
| P-D | generation/compaction 是独立持久任务，摘要请求和 attempt 在 checkpoint 中保留；scheduler 恢复任务不等于自动获准新推理费用，费用须看实际调用路径。[P-durable-compaction][P-durable-task-state] |
| D | prepareCall 绑定实际 adapter/config，buildRequest 保存 header/schema/surface；retry 插件可开启重试。官方 adapter 的 log 扩展还有模型可见 surface 之外的数据出口。[D-S39][D-S42][D-S75] |
| R | provider retry policy、compaction、refine/side question 可产生额外请求；Goal token 计数与请求响应 usage 有用，但不等于最终费用结算。[R-PA24][R-PA26][R-PA37] |
| K | Fantasy loop + PrepareStep/stream callbacks；标题、摘要和 OAuth 刷新重试是附加调用路径。Session cost 不具有本项目逐物理请求的会计责任。[K-CR18][K-CR24][K-CR27][K-CR48] |

<a id="sm-06"></a>
### SM-06 工具调用、取消与未知效果

| 路径 | 对象、生命周期与恢复含义 |
| --- | --- |
| C | ToolCallRuntime 持 StepContext，按目录及并行规则派发；missing result 的 `aborted` 配对仅改变模型输入。暂停/恢复历史不提供通用目标系统 Effect 对账。[C-工具并行][C-提示归一化][C-暂停移交] |
| P-C | ToolCall.id 配对 ToolResult，preflight 后可并行、按原源序写 transcript；AbortController 不证明已发出的工具或目标效果停止。经典 loop 无独立持久 Effect 账本。[P-ai-message][P-loop-tools][P-agent-abort] |
| P-H | 参数、effect_pending/replay 和稳定 invocationId 在执行前保存；saved 与 current 同为 safe 才重放，其余生成 unknown 合成结果。合成 error 不等于 unknown 已查明。[P-tool-intent][P-tool-recovery] |
| P-D | ToolTask 在 beforeTool 后重验最终参数，再持久 execute intent；恢复双 safe 检查，否则保守收尾。Task checkpoint 不是外部系统恰好一次证明。[P-durable-tool] |
| D | tool/call 与结果按 ToolCallId 配对，修复区分 NOT_STARTED/OUTCOME_UNKNOWN；checkpoint 插件在顶层 dispatch 前 flush。unknown 后的建议依赖模型/工具核对，未见独立持续 Effect job。[D-S12][D-S13][D-S26] |
| R | Python execute/host_request id 配对工具控制；force_abort 可先返回 aborted 而 execution 仍活着，后续复用可能 busy。kernel 局部终结不证明文件/网络效果撤销。[R-PA28][R-PA30] |
| K | ToolCall/ToolResult 保存在消息 parts；bash 转后台后通过 job_output/job_kill 操作内存 job。kill 和合成 interrupted 均不能裁决真实外部效果。[K-CR11][K-CR30][K-CR33][K-CR29] |

<a id="sm-07"></a>
### SM-07 人工批准、工具策略与实际隔离

| 路径 | 对象、生命周期与恢复含义 |
| --- | --- |
| C | Skip/NeedsApproval/Forbidden 与 platform sandbox 分开；ApprovedForSession 缓存复用审阅决定，升级只走特定拒绝分支。它没有直接等价于 Grant/UseSettlement 的完整合同。[C-审批编排][C-审批缓存][C-沙箱选择] |
| P-C | 项目信任、tool hooks 和进程内 extension 影响调用；扩展拥有宿主权限，已校验 input 可被 hook 修改后执行。Codemode 的脚本隔离不覆盖任意宿主工具。[P-permissions][P-tool-mutation][P-codemode-host] |
| P-H | tool 替换参数时重新 schema validate；工具由注入 ExecutionEnv 和调用者提供。当前已读接口没有把这一验证升级成资源/用途授权与 OS 强制隔离。[P-harness-validation][P-storage-port] |
| P-D | beforeTool 后统一重验并保存最终参数，ToolTask replay 由工具能力决定；类型、hook 和 safe 标记均不是可信批准或沙箱。[P-durable-tool] |
| D | ApprovalService ask/outcome 与 sandbox-policy 分离；late answer 不复活已取消请求，runner 缺失不退回裸执行。Windows partial 限制须按平台说明，不能从插件名字推定同等隔离。[D-S28][D-S44][D-S29] |
| R | 模型 Python/工程命令默认用当前用户权限；worker/kernel 进程分开不构成安全沙箱，MCP auth 不是资源用途许可。[R-PA02][R-PA32] |
| K | Permission 的 Take 保证内存 first-winner，获胜后才写会话复用许可；hook allow 可跳常规确认，blocker 不构成 OS 沙箱。进程重启不保留等待通道。[K-CR31][K-CR34][K-CR38] |

<a id="sm-08"></a>
### SM-08 持续目标、继续执行条件与完成

| 路径 | 对象、生命周期与恢复含义 |
| --- | --- |
| C | `thread_goals` 持久 objective/status/token budget/usage，Goal tool 可更新 complete/blocked/paused；所读更新路径未见本项目的 Requirement/独立成果核验。[C-目标表][C-目标更新] |
| P-C | 所读 Agent/loop 由消息、工具和队列驱动，agent_end 是运行停止；没有从该路径识别出独立持久 Goal/完成条件。扩展可增加目标，但本报告不借其可能性计覆盖。[P-agent-types][P-loop] |
| P-H | OperationResult 可 completed/declined/aborted/failed，描述某次运行；Lane 配置/检查点不是独立用户目标完成依据。[P-harness-types] |
| P-D | Task 的阶段/等待/终态和 owner completing 约束持久工作；用户可组合自己的目标任务，内建 Task 类型本身没有本项目 Requirement 语义。[P-durable-task-state][P-scheduler] |
| D | GoalId+revision 保存目标，armed/disarmed 是本地继续权，恢复初始 disarmed；round driver 保留目标/轮次/消息关联，flush 后再继续，不能由 durable active 无条件启动。[D-S45][D-S46][D-S47] |
| R | GoalState 保存 token/continuations/no_progress_streak，驱动更新持久记录；autonomous gates 有范围/预算/失败限制，shell gate 通过不等于全目标完成。[R-PA08][R-PA34][R-PA48][R-PA36] |
| K | Session 有 todos/usage，loop detection 通过重复调用启发式停止；所读 RunComplete/Session 结构不提供持续 Goal 与独立完成条件。[K-CR10][K-CR25][K-CR37] |

<a id="sm-09"></a>
### SM-09 分支、回退与来源位置

| 路径 | 对象、生命周期与恢复含义 |
| --- | --- |
| C | forked_from_id/ordinal 与 parent_thread_id 分开；HistoryPosition 有 rollout/ordinal/byte offset，revert 后逻辑线程可延续。rollback 重建模型历史，不证明撤销工作区或远端效果。[C-会话元数据][C-历史位置][C-回滚恢复] |
| P-C | branch 移内存 leaf，后续 append 接成新路径；branchWithSummary/forkFrom 保留不同来源形态。没有后续 append 的 leaf 移动不能仅凭旧文件恢复，也不回滚目标文件。[P-legacy-branch][P-legacy-fork][P-legacy-index] |
| P-H | Branch 保存具名 tip，Lane 绑定来源 tip/config，navigation 是运行意图之一；entry 树和 Branch 更新有存储合同，不能据分支移动复制外部执行权。[P-harness-types][P-restore] |
| P-D | Conversation.parent 固定源会话/Entry 截止，与 owner 执行边独立；Document 的 current/initial/asOf fork 策略须按 scope/历史模式选择，复制计算/文档不恢复外部效果。[P-durable-types][P-durable-records] |
| D | buildForkSeed 复制精确 inclusive 事件前缀，加 inherited marker 和开放尾部 synthetic closers；fork-tail unknown 提醒父可能在截止后执行，不能把闭合日志当回滚世界。[D-fork][D-S13] |
| R | fork_from 生成新 Session header、重连删除的 git_state 父链、采用目标 cwd 的 Git 信息后 flush；来源以 parentSession 保留。文件历史复制不代表重新领取原运行。[R-fork] |
| K | Session.parent_session_id 用于子会话，文件版本 history 用于编辑材料；所读会话/Workspace 未证明具名历史分支及精确截止 fork。父子字段不能自动算作该能力。[K-CR10][K-CR15][K-CR05] |

<a id="sm-10"></a>
### SM-10 子 Agent、持续身份与多次激活

| 路径 | 对象、生命周期与恢复含义 |
| --- | --- |
| C | parent-child graph 保存 Open/Closed 边，AgentControl 有 spawn reservation/配置继承/冷子线程恢复；完成通知 best effort，图/历史/registry 不组成分布式原子委派。[C-父子图][C-冷子线程恢复][C-子线程启动][C-子线程完成] |
| P-C | 可选 subagent 示例 spawn 新 `pi --no-session` 子进程，single/parallel/chain 聚合输出与 usage；进程结束后没有该示例的持久子会话恢复保证。[P-subagent][P-subagent-spawn] |
| P-H | Lane/Operation 可供宿主组合多个运行域；所读公开状态不提供等价 Delegation/父子效果及费用封闭。不能把 P-D Reporter 的保证算在此路径。[P-harness-types][P-harness-api] |
| P-D | 示例子 Conversation + background Reporter，以稳定 requestId/Submission/answer 去重交接；普通 owned work 阻止父完成，background 有不同规则。只是内部可恢复模式，不是跨负责方预算/授权协议。[P-durable-subagent][P-scheduler][P-durable-records] |
| D | 一次性 run、稳定 child SessionId、进程内 Activation 分开；descriptor 固定冷启动组合，不保存一次激活的全部预算/结果条件。continuable child 只有 Agent inbox 作队列。[D-S15][D-S16][D-S30] |
| R | RLM ledger 保存父子拓扑，spawn 返回 child admission handle；collect 有界等候/快照并保留结果。冷读可发现非驻留 child，但返回 handle 不等于答案或父目标通过。[R-PA10][R-PA29][R-PA45] |
| K | parent message/tool-call 派生子 Session identity，agent tool 同步等待子运行；child cost 加父失败只告警，返回文本不证明费用封闭或未知效果消除。[K-CR39][K-CR49] |

<a id="sm-11"></a>
### SM-11 Schedule 与周期 occurrence

| 路径 | 对象、生命周期与恢复含义 |
| --- | --- |
| C | 所读 queue 服务管理待输入、Goal 管目标；未在这两条本地入口发现用户可管理的时间规则/occurrence 合同。不能把桌面产品自动化或 idle queue 推定为本仓库等价 Schedule。[C-queue-service][C-目标表] |
| P-C | steering/follow-up 等待 loop 边界，不是按时间触发；已读 Agent 控制未提供持久用户 Schedule。宿主 extension 的潜在能力不计为默认功能。[P-agent-control][P-loop] |
| P-H | Lane inbox/operation driver 能等待或推进已接纳工作；其类型没有等价用户 Schedule/occurrence，不能将内部 retry_wait 当周期任务。[P-harness-types] |
| P-D | TaskScheduler 依据 owner、waiting/completing 调度持久任务；这是作业恢复调度，不等于可编辑的周期日历规则。独立 Schedule 须由应用组合并定义 occurrence 去重。[P-durable-task-state][P-scheduler] |
| D | 已装配 Schedule 时，规则/active/receipt 在 host storageDomain；到期先向 Session inbox 入消息并 flush，再写 receipt/next 时间。两个提交间 crash 可能重复交付，不能据此声称 occurrence 恰好一次。[D-S48][D-S49][D-S50] |
| R | cron 保存 job/claimed dispatch，含 heartbeat/独立 schedule；锁耗尽可无锁执行、写失败存在忽略分支，领取 API 返回不证明已耐久或跨进程唯一。[R-PA18][R-PA47][R-PA51][R-PA52][R-PA53] |
| K | 所读 Workspace/SessionAgent 的排队、后台 shell 和运行控制未提供用户 Schedule；后台进程脱离一次等待不等于未来按时间创建任务。[K-CR05][K-CR18][K-CR30] |

<a id="sm-12"></a>
### SM-12 可复用执行环境与检查点

| 路径 | 对象、生命周期与恢复含义 |
| --- | --- |
| C | unified_exec 持 process_id，后续 write_stdin 延续同一进程；产出/等待/进程数有边界。会话历史有进程引用不证明重启后可恢复 OS 进程。[C-unified-env] |
| P-C | bash/宿主工具与独立 Codemode worker 是不同环境；脚本取消不能撤销已派发工具。store/load custom entries 跟随历史分支，不能恢复进程或任意外部资源。[P-codemode][P-codemode-host][P-codemode-run] |
| P-H | ExecutionEnv/工具是宿主注入，Invocation memo/committed progress 提供局部恢复资料；所读合同不承诺通用长驻 kernel 或 OS 资源检查点。[P-invocation][P-bash-output] |
| P-D | Task/Document checkpoint 保存 JSON 状态和阶段；可恢复计算逻辑不等于恢复 socket/子进程/外部效果。环境复用需独立适配语义。[P-durable-types][P-durable-tool] |
| D | 安装 terminal 能力后 TerminalSessionId 归实际 Agent owner，一次 send 的 waitReason 与顶层 sessionStatus 分开；等待返回不意味任意子进程退出。后台 Jobs 及 ring/cursor 在内存。[D-terminal][D-S17] |
| R | 长驻 Python namespace，execute/interrupt/restore 各有 id；snapshot/manifest 尽力保存且标记 skipped。旧 dill 恢复并非安全交换格式，不能恢复全部外部资源，aborted 时环境可能仍 busy。[R-PA28][R-PA19][R-PA30][R-PA46] |
| K | 内存 BackgroundShellManager 使 shell 跨一次工具等待存活，read/kill 独立；数量/保留时间不证明输出内存有界，也没有通用持久环境恢复。[K-CR30][K-CR33] |

<a id="sm-13"></a>
### SM-13 跨任务长期记忆

| 路径 | 对象、生命周期与恢复含义 |
| --- | --- |
| C | feature gate 下 phase1 提取、phase2 合并，SQLite jobs/ownership/watermark 与文件产物分开；读扩展注入摘要/按范围检索。来源时间与 lease 不是本项目独立长期保存许可或关闭账本。[C-记忆启动][C-记忆任务表][C-记忆二阶段][C-记忆读取扩展] |
| P-C | Session 树、compaction/custom 可保存材料；所读路径未建立跨任务范围/许可/来源关闭的 Memory 生命周期，custom 不进入模型也不因此成为合法长期记忆。[P-legacy-types][P-projection] |
| P-H | Session entries/values/usage 与 Skill loader 提供存储和材料入口；没有从这些通用接口识别出等价的长期记忆许可、纠正与清理协议。[P-storage-port][P-skill-harness] |
| P-D | Document 提供 session/conversation/task scope 与版本回读，是状态基元；缺少领域来源/用途/关闭合同就不能仅把 Document 重命名为 Memory。[P-durable-types] |
| D | compaction/SessionQuery/FTS 服务历史和检索，memory MCP examples 是外部配置；所读第一方主线没有本项目完整 permissioned Memory 协议。[D-S21][D-S22][D-S33] |
| R | HarnessEntry 的 memory kind、local/global scope、CRUD/refine before-after 支持补充材料；状态文件并发 mtime 重读非事务 CAS，多文件不是统一提交。Content 之外仍需记忆来源/许可/纠正语义。[R-PA09][R-PA17][R-PA37][R-PA38] |
| K | summary/file history/Skill 是上下文或编辑材料；所读这些对象没有跨任务 Memory 的保存许可、来源关闭和当前用途规则。[K-CR15][K-CR27][K-CR41] |

<a id="sm-14"></a>
### SM-14 Skill、插件与工具绑定

| 路径 | 对象、生命周期与恢复含义 |
| --- | --- |
| C | Skill 有来源/scope/plugin 身份，Plugin manifest/版本目录独立；McpBinding/PreparedMcpCall 固定目录与 client，catalog lease 阻止陈旧准备调用。目录版本路径不等于本项目全依赖 InstallLock。[C-技能类型][C-插件清单][C-MCP绑定][C-MCP准备调用] |
| P-C | Extension load staging commit/discard；Skill metadata/body 分开，MCP direct/deferred/codemode/hidden 四种暴露；Node extension 生命周期不能赋予隔离或发布资格。[P-extension-load][P-skill-cli][P-mcp-entry] |
| P-H | sourced Skill loader 保留宿主定义的来源，ExecutionEnv/工具配置进入 Lane；加载成功和来源标签不自动等于准确制品锁定、当前许可或受信发布。[P-skill-harness][P-harness-types] |
| P-D | registry/task definition 版本用于阶段运行和迁移，tool hooks 参与调用；这是组合机制，不自动继承 P-C 插件管理或本项目 ReleaseApproval。[P-durable-task-definition][P-durable-tool] |
| D | Cordis/plugin/profile 组合、Skill catalog 按来源/调用可见性选项管理；MCP 先建下一 generation 成功再换。node:vm host runner 自认不是 containment，不能作为不可信代码隔离。[D-S72][D-S70][D-S51][D-S52] |
| R | Skill 可成为可导入 Python package，harness skill 条目和 MCP 配置另存；可导入、元数据版本及 refine expected outcome 均不证明受信发布或运行资格。[R-PA33][R-PA09][R-PA32][R-PA44] |
| K | Skill frontmatter/manager、MCP PendingConfig vs Config、LSP 懒启动；可用目录变更与实际绑定须分开，PrepareStep 当前 map 不能替代本项目原 Operation 的 Binding/InstallLock。[K-CR41][K-CR46][K-CR47][K-CR24] |

<a id="sm-15"></a>
### SM-15 流式呈现与正式结果

| 路径 | 对象、生命周期与恢复含义 |
| --- | --- |
| C | 事件可含 token/text delta，rollout 保留 typed response/events；原 writer 的接收和 flush 有区别。回合状态事件不能直接作为材料已耐久或 Goal 成功证明。[C-历史载荷][C-后台写入][C-状态映射] |
| P-C | stream partial 是可变 live helper，message_end 由应用写历史；delta 回调不构成独立不可变记录，也不是每 token 已提交。[P-ai-events][P-session-ctor] |
| P-H | live frames 与 committed prefix 分开，恢复只用已提交前缀，并显式说明新 live 输出可能丢失；合成 terminal response 不能宣称供应方原响应完整。[P-assistant-recovery] |
| P-D | live view/events 展示 run inputs、工具和 compaction 状态；持久 Task/Entry/Submission 是另一层事实。事件投影不得被解释成全部底层效果已结。[P-durable-events][P-durable-records] |
| D | assistant attempt 的实时帧与最后 settlement log 分开；浏览器基线累积在进程内，revision/index 不连续清空。UI 能接续前缀不证明前缀全部耐久。[D-S11][D-S55] |
| R | 流事件和最终 Session entry 区别；retained append 可在磁盘错误时保留 live 数据。attach snapshot 的 generation/event sequence 是呈现关联，不是目标效果事务。[R-PA43][R-PA39] |
| K | 默认 33ms debounce 合并文本，结构/终结 flush；FlushAll 错误记录后仍可发 RunComplete，MustDeliver 也有超时丢弃。正式结果须重读 DB，不能依赖通知名称。[K-CR16][K-CR21][K-CR40] |

<a id="sm-16"></a>
### SM-16 重连、冷恢复与原身份

| 路径 | 对象、生命周期与恢复含义 |
| --- | --- |
| C | app-server 可重发进程内 pending server requests；冷恢复取得 writer lock 并核验 history revision。网络重连、原回合恢复和外部效果核对是三条不同责任。[C-待审批重放][C-恢复取得写入权][C-暂停移交] |
| P-C | 打开 Session 重建历史；RPC id 是调用关联，未见长期去重账本。MCP HTTP 有 Last-Event-ID 重连属于工具传输，不能提升为整个应用 Session 的命令恢复。[P-legacy-load][P-rpc-types][P-mcp-http] |
| P-H | create/open 检查 lane/tip/config/state，返回 open 信息而不自动执行；orphaned assistant/tool 沿原 operation/invocation 保守恢复。任意 OS 环境与授权不由 session 文件重建。[P-harness-open][P-restore][P-tool-recovery] |
| P-D | storage 恢复 Conversation/Submission/Task/Document，scheduler 驱动作业阶段；工具重放受 saved/current 策略限制。它是独立恢复体系，不是经典 CLI 自动升级后端。[P-durable-storage][P-scheduler][P-durable-tool] |
| D | Session 持久前缀 + repair 和单 writer；SDK JSON-RPC pending map 在 close 后拒绝 promise，Web assistant baseline 在进程内。新 request id 不恢复原业务提交。[D-S14][D-S13][D-S55][D-S56] |
| R | daemon attach 按 snapshot/generation/sequence 分块，worker journal/lease 与 kernel snapshot 各自恢复；CommandRecoveryJournal 有 pending uncertain 实现，但不能仅凭定义假定普通 prompt 已调用它。[R-PA39][R-PA16][R-PA15][R-queue-checkpoint] |
| K | SSE 无 event id/Last-Event-ID 重放，ClientWorkspace 重连后重申当前 Session 并要求重读；DB 历史可恢复，active/queued run 与已丢终结通知不自动重现。[K-CR50][K-CR51][K-CR18] |

## 4. 需要独立交付的合同差异

以下是覆盖约束和后续切片边界，不是已经新增的方法、Schema 字段或状态枚举。现有 105 个领域方法保持，Session 仍是应用内部设计。完整能力需要开放公共合同的，须另行同步方法登记、Schema、示例及互操作检查；当前不得返回“已支持”来隐藏缺口。

| 差异 | 必须说明的合同和外部行为 | 本轮承载 / 不交付范围 |
| --- | --- | --- |
| G-01 历史分支 | 选择精确源 Session/消息或事件位置，保存父链、分支头及其并发修订；配置/摘要按来源范围选取；说明工作区沿用还是另建。fork 不复制活动 Operation、授权使用、未结费用或 Task 推进责任 | 交互/应用管理 Session 子记录，其他 owner 保留原事实；本轮写明边界，首版线性行为和原任务查询保持，完整公开 fork/branch/revert API 不交付 |
| G-02 可撤回输入与多 Lane | 对独立提交查询 queued/consumed/rejected/withdrawn 的准确含义；明确 steer/follow-up 插入位置、先后规则、撤回与 claim 竞争、旧完成事件关联、取消范围及不支持时的响应。旧 task.cancel 不能默默实现“只撤回 B，A 继续” | 现有 interaction.input_withdraw 已处理 InputSubmission：queued 撤回先胜出则不发送，sending 后只记 withdrawal_requested 并核对原业务。缺口限于 Session 自由输入的通用队列、steering/follow-up 与 Run 范围控制；不能把针对 InputRequest 回答的合同直接泛化到所有输入 |
| G-03 可复用 child | 固定父 Delegation、子 Task 与子 Session 的关系，声明后续激活的原输入、准确配置/当前许可、是否已有活动工作；wait timeout 不 cancel；一次激活结束、子会话保留、委派账务封闭分别可查 | Collaboration 负责委派，Session 由交互/应用组织；既有一次子任务和恢复合同不自动等于所有 reference child.send/reuse 行为 |
| G-04 Schedule | 独立规则/版本/启停，固定时区、到期/错过/重叠策略与 occurrence 身份；每次触发保存原命令/目标，不因答复丢失重建 Task；停用未来触发与取消已经生成的任务分开 | 应用调度能力持有 Schedule；可靠工作模板处理已接纳交付，不能靠 Job 的 due time 代替用户规则。公共调度协议和运行 scheduler 不交付 |
| G-05 执行环境 | 明确资源身份/负责 Executor/准确配置/占用/实际终止；多 Operation 共享时的并发、配额和失联处置；读取/取消/销毁权限；检查点仅声明可恢复的受限计算状态，不重放未知外部效果 | Executor 可选环境记录及有界程序 driver；默认安全基线不接收任意 dill/pickle，kernel 协议/平台实测另行交付 |
| G-06 Memory | 独立长期保存许可、作用域/来源/准确版本/冲突、当前检索许可、纠正 CAS 和关闭/副本清理；普通偏好纠正与软件改善发布分开 | 既有 Memory 合同保留，不因 Content 统一正文而删除；跨任务能力按启用阶段实现，历史检索不自动开放为长期记忆 |
| G-07 Skill / Plugin | 元数据可见、正文已加载、工具已选择、实例 ready、当前批准及原调用绑定分别成立；刷新不重绑旧操作，卸载/回退处理活动工作和当前批准 | Capability/Binding/InstallLock 是必要配置；动态发现和安装按需。不能用任意 metadata 或新造 CatalogVersion 代替现行绑定 |

Task 的继续权、原模型调用、效果 unknown、可信确认及费用收尾不是可省略的扩展差异：它们在普通工作流中就必须成立。六组对象收敛的是应用需要逐项操作的入口；内部记录保留独立身份/权限/保留期限或父对象终结后责任时，仍可独立存储和查询。

## 5. 验收和证据等级

主验收切面沿既有应用/SDK 规划入口观察提交、查询、控制、结果与恢复；内部故障注入只用于制造边界，不能以私有表行数代替用户行为。以下反例用于检查简化是否丢失语义：

1. Session 有 T1/T2，T1 等待澄清；回答原 InputRequest 只继续 T1。归档或界面回合结束不取消任务、不宣称完成。
2. A 在执行，B 排队；若 B 是既有 InputRequest 的回答，沿 interaction.input_withdraw 检验 queued 先撤回与 sending 后待核对；若 B 是普通 Session 自由输入，则须显式声明通用撤回能力是否开放。两者均不得以取消 A 代替撤回 B，旧 A 的完成通知不覆盖 C 的新视图。
3. Task active 但已暂停、授权不足或旧 writer 未隔离；重开会话仅恢复可读事实，不自动产生新模型费用或工具效果。
4. 在分支截止后，原路径已写文件；新分支保留来源并查询原效果，不复制原 Operation 或把历史回退当文件回滚。
5. 模型请求可能发出或工具写入已发生后断线；恢复原 Decision/Operation，unknown 保留且继续核对；不能新建同义对象透明重放。
6. child 激活结束但会话可复用；读/有界等待/取消相互独立。迟到费用仍归原 Delegation，父目标不因子最终文本直接完成。
7. schedule receipt 丢失后重试同一 occurrence，仍指向原命令；编辑未来规则不修改已产生 Task。未启用 Schedule 时不能声称通过此验收。
8. cell 已返回 aborted 而 kernel 仍 busy；保留占用和核对责任，禁止提前复用。检查点恢复不重建未知 socket/进程效果。
9. 记忆版本、来源许可或工具绑定已变；当前读取/发送重新核验，旧摘要、Skill 或缓存不绕过限制。
10. live delta 丢失、正式通知重复或订阅断档；通过原业务查询和新的界面快照恢复，不能只看终结流帧判断已提交。

这些是待运行场景，可与[既有 HAR 场景](../../architecture/.draft/validation/harness-scenarios.md)及 RT/BI/EX/权限/交互契约向量合并。结构、链接与固定源码路径/行号检查只说明材料可追溯；没有运行实现、数据库故障实验、真实平台隔离或性能测量，不能登记上述场景已通过。SQL、事务、flush/sync、字节和物理 IO 的口径继续按[同场景读写对照](data-flow-io-comparison.md)，不由对象数量推导下降比例。

## 6. 固定源码索引

下列引用均指向上述五个固定 commit。标签前缀只用于本文定位；链接含实际路径和行号。原项目报告继续提供更完整的代码上下文和局限说明。

[C-线程入口]: https://github.com/openai/codex/blob/d4a475adda850d80b6149c76454de94e0cf4fd51/codex-rs/core/src/codex_thread.rs#L201-L253
[C-本地存储边界]: https://github.com/openai/codex/blob/d4a475adda850d80b6149c76454de94e0cf4fd51/codex-rs/thread-store/README.md#L1-L34
[C-default]: https://github.com/openai/codex/blob/d4a475adda850d80b6149c76454de94e0cf4fd51/codex-rs/thread-store/src/local/mod.rs#L478-L481
[P-sdk-agent]: https://github.com/earendil-works/pi/blob/8ce69e9d2b171d173fe4b6b2b6256f1f4411e69d/packages/coding-agent/src/core/sdk.ts#L387-L448
[P-legacy-types]: https://github.com/earendil-works/pi/blob/8ce69e9d2b171d173fe4b6b2b6256f1f4411e69d/packages/coding-agent/src/core/session-manager.ts#L41-L194
[P-legacy-persist]: https://github.com/earendil-works/pi/blob/8ce69e9d2b171d173fe4b6b2b6256f1f4411e69d/packages/coding-agent/src/core/session-manager.ts#L1160-L1195
[P-harness-types]: https://github.com/earendil-works/pi/blob/8ce69e9d2b171d173fe4b6b2b6256f1f4411e69d/packages/agent/src/harness/session/types.ts#L16-L350
[P-harness-open]: https://github.com/earendil-works/pi/blob/8ce69e9d2b171d173fe4b6b2b6256f1f4411e69d/packages/agent/src/harness/runtime/harness.ts#L375-L408
[P-storage-port]: https://github.com/earendil-works/pi/blob/8ce69e9d2b171d173fe4b6b2b6256f1f4411e69d/packages/agent/src/harness/session/types.ts#L388-L555
[P-durable-types]: https://github.com/earendil-works/pi/blob/8ce69e9d2b171d173fe4b6b2b6256f1f4411e69d/packages/durable/src/types.ts#L17-L113
[P-durable-storage]: https://github.com/earendil-works/pi/blob/8ce69e9d2b171d173fe4b6b2b6256f1f4411e69d/packages/durable/src/types.ts#L986-L1082
[P-scheduler]: https://github.com/earendil-works/pi/blob/8ce69e9d2b171d173fe4b6b2b6256f1f4411e69d/packages/durable/src/harness/scheduler.ts#L159-L194
[D-S08]: https://github.com/deepseek-ai/deepseek-harness/blob/639ed015397290b3745d163aafe02ffee4aa3f84/apps/cli/src/profile-boot.ts#L1-L64
[D-profile-base]: https://github.com/deepseek-ai/deepseek-harness/blob/639ed015397290b3745d163aafe02ffee4aa3f84/packages/bundle/base/cordis.patch.yml#L313-L415
[D-profile-minimal]: https://github.com/deepseek-ai/deepseek-harness/blob/639ed015397290b3745d163aafe02ffee4aa3f84/packages/bundle/sdk-minimal/cordis.patch.yml#L1-L158
[D-S26]: https://github.com/deepseek-ai/deepseek-harness/blob/639ed015397290b3745d163aafe02ffee4aa3f84/packages/session/session-checkpoint-policy/src/index.ts#L20-L82
[R-PA06]: https://github.com/PrimeIntellect-ai/prime-agent/blob/5784abc2aef523a78d5a8850a0c0be89883388b2/crates/pa-core/src/session_engine/engine.rs#L104-L170
[R-PA13]: https://github.com/PrimeIntellect-ai/prime-agent/blob/5784abc2aef523a78d5a8850a0c0be89883388b2/crates/pa-core/src/session/manager/persist.rs#L52-L173
[R-PA11]: https://github.com/PrimeIntellect-ai/prime-agent/blob/5784abc2aef523a78d5a8850a0c0be89883388b2/crates/pa-daemon/src/worker/queue.rs#L1-L196
[R-PA15]: https://github.com/PrimeIntellect-ai/prime-agent/blob/5784abc2aef523a78d5a8850a0c0be89883388b2/crates/pa-daemon/src/journal.rs#L1-L240
[K-CR05]: https://github.com/charmbracelet/crush/blob/76cc5c574e15072b15aaed0f4f843a5711fae0d9/internal/workspace/workspace.go#L1-L170
[K-CR18]: https://github.com/charmbracelet/crush/blob/76cc5c574e15072b15aaed0f4f843a5711fae0d9/internal/agent/agent.go#L75-L144
[K-CR19]: https://github.com/charmbracelet/crush/blob/76cc5c574e15072b15aaed0f4f843a5711fae0d9/internal/backend/agent.go#L25-L157
[K-CR50]: https://github.com/charmbracelet/crush/blob/76cc5c574e15072b15aaed0f4f843a5711fae0d9/internal/server/proto.go#L177-L232
[C-会话元数据]: https://github.com/openai/codex/blob/d4a475adda850d80b6149c76454de94e0cf4fd51/codex-rs/protocol/src/protocol.rs#L3123-L3145
[C-历史载荷]: https://github.com/openai/codex/blob/d4a475adda850d80b6149c76454de94e0cf4fd51/codex-rs/history/src/rollout_payload.rs#L30-L72
[C-历史写入顺序]: https://github.com/openai/codex/blob/d4a475adda850d80b6149c76454de94e0cf4fd51/codex-rs/thread-store/src/local/live_writer.rs#L327-L382
[P-legacy-index]: https://github.com/earendil-works/pi/blob/8ce69e9d2b171d173fe4b6b2b6256f1f4411e69d/packages/coding-agent/src/core/session-manager.ts#L1057-L1133
[P-durable-records]: https://github.com/earendil-works/pi/blob/8ce69e9d2b171d173fe4b6b2b6256f1f4411e69d/packages/durable/src/types.ts#L277-L436
[D-S09]: https://github.com/deepseek-ai/deepseek-harness/blob/639ed015397290b3745d163aafe02ffee4aa3f84/packages/core/session/src/types.ts#L31-L130
[D-S14]: https://github.com/deepseek-ai/deepseek-harness/blob/639ed015397290b3745d163aafe02ffee4aa3f84/packages/session/session-persistence/src/handle.ts#L45-L116
[D-S18]: https://github.com/deepseek-ai/deepseek-harness/blob/639ed015397290b3745d163aafe02ffee4aa3f84/packages/core/session/src/index.ts#L711-L795
[R-PA07]: https://github.com/PrimeIntellect-ai/prime-agent/blob/5784abc2aef523a78d5a8850a0c0be89883388b2/crates/pa-types/src/session.rs#L1-L395
[R-PA43]: https://github.com/PrimeIntellect-ai/prime-agent/blob/5784abc2aef523a78d5a8850a0c0be89883388b2/crates/pa-core/src/session/manager/append.rs#L11-L100
[K-CR10]: https://github.com/charmbracelet/crush/blob/76cc5c574e15072b15aaed0f4f843a5711fae0d9/internal/session/session.go#L20-L153
[K-CR11]: https://github.com/charmbracelet/crush/blob/76cc5c574e15072b15aaed0f4f843a5711fae0d9/internal/message/content.go#L20-L195
[K-CR14]: https://github.com/charmbracelet/crush/blob/76cc5c574e15072b15aaed0f4f843a5711fae0d9/internal/db/connect.go#L18-L185
[C-回合运行类型]: https://github.com/openai/codex/blob/d4a475adda850d80b6149c76454de94e0cf4fd51/codex-rs/core/src/session/turn_context.rs#L303-L363
[C-状态映射]: https://github.com/openai/codex/blob/d4a475adda850d80b6149c76454de94e0cf4fd51/codex-rs/core/src/agent/status.rs#L6-L30
[C-暂停移交]: https://github.com/openai/codex/blob/d4a475adda850d80b6149c76454de94e0cf4fd51/codex-rs/core/src/session/turn_suspension.rs#L13-L118
[P-loop]: https://github.com/earendil-works/pi/blob/8ce69e9d2b171d173fe4b6b2b6256f1f4411e69d/packages/agent/src/agent-loop.ts#L163-L320
[P-agent-types]: https://github.com/earendil-works/pi/blob/8ce69e9d2b171d173fe4b6b2b6256f1f4411e69d/packages/agent/src/types.ts#L377-L510
[P-harness-api]: https://github.com/earendil-works/pi/blob/8ce69e9d2b171d173fe4b6b2b6256f1f4411e69d/packages/agent/src/harness/agent-harness.ts#L538-L622
[P-durable-task-state]: https://github.com/earendil-works/pi/blob/8ce69e9d2b171d173fe4b6b2b6256f1f4411e69d/packages/durable/src/types.ts#L485-L540
[D-S10]: https://github.com/deepseek-ai/deepseek-harness/blob/639ed015397290b3745d163aafe02ffee4aa3f84/packages/core/agent-loop/src/agent.ts#L296-L395
[D-S11]: https://github.com/deepseek-ai/deepseek-harness/blob/639ed015397290b3745d163aafe02ffee4aa3f84/packages/core/agent-loop/src/agent.ts#L398-L538
[R-PA20]: https://github.com/PrimeIntellect-ai/prime-agent/blob/5784abc2aef523a78d5a8850a0c0be89883388b2/crates/pa-agent/src/agent_loop/run.rs#L29-L248
[R-PA08]: https://github.com/PrimeIntellect-ai/prime-agent/blob/5784abc2aef523a78d5a8850a0c0be89883388b2/crates/pa-types/src/goal.rs#L12-L101
[K-CR23]: https://github.com/charmbracelet/crush/blob/76cc5c574e15072b15aaed0f4f843a5711fae0d9/internal/agent/agent.go#L1299-L1410
[K-CR37]: https://github.com/charmbracelet/crush/blob/76cc5c574e15072b15aaed0f4f843a5711fae0d9/internal/proto/proto.go#L62-L150
[C-队列表]: https://github.com/openai/codex/blob/d4a475adda850d80b6149c76454de94e0cf4fd51/codex-rs/state/queue_migrations/0001_queued_items.sql#L1-L11
[C-queue-service]: https://github.com/openai/codex/blob/d4a475adda850d80b6149c76454de94e0cf4fd51/codex-rs/ext/queue/src/service.rs#L265-L403
[P-agent-control]: https://github.com/earendil-works/pi/blob/8ce69e9d2b171d173fe4b6b2b6256f1f4411e69d/packages/agent/src/agent.ts#L278-L343
[P-rpc-ack]: https://github.com/earendil-works/pi/blob/8ce69e9d2b171d173fe4b6b2b6256f1f4411e69d/packages/coding-agent/src/modes/rpc/rpc-mode.ts#L394-L428
[P-lane-command]: https://github.com/earendil-works/pi/blob/8ce69e9d2b171d173fe4b6b2b6256f1f4411e69d/packages/agent/src/harness/runtime/lane.ts#L290-L357
[P-durable-submission-api]: https://github.com/earendil-works/pi/blob/8ce69e9d2b171d173fe4b6b2b6256f1f4411e69d/packages/durable/src/harness/harness.ts#L243-L274
[D-S36]: https://github.com/deepseek-ai/deepseek-harness/blob/639ed015397290b3745d163aafe02ffee4aa3f84/packages/core/agent-loop/src/inbox.ts#L20-L242
[D-S37]: https://github.com/deepseek-ai/deepseek-harness/blob/639ed015397290b3745d163aafe02ffee4aa3f84/packages/core/agent/src/consumed-work.ts#L1-L93
[R-queue-checkpoint]: https://github.com/PrimeIntellect-ai/prime-agent/blob/5784abc2aef523a78d5a8850a0c0be89883388b2/crates/pa-daemon/src/worker/queue.rs#L285-L358
[K-CR22]: https://github.com/charmbracelet/crush/blob/76cc5c574e15072b15aaed0f4f843a5711fae0d9/internal/agent/agent.go#L322-L368
[K-CR53]: https://github.com/charmbracelet/crush/blob/76cc5c574e15072b15aaed0f4f843a5711fae0d9/internal/agent/agent.go#L2096-L2151
[C-上下文宿主事实]: https://github.com/openai/codex/blob/d4a475adda850d80b6149c76454de94e0cf4fd51/codex-rs/core/src/context_manager/history.rs#L89-L123
[C-压缩恢复]: https://github.com/openai/codex/blob/d4a475adda850d80b6149c76454de94e0cf4fd51/codex-rs/core/src/session/rollout_reconstruction.rs#L57-L87
[C-提示归一化]: https://github.com/openai/codex/blob/d4a475adda850d80b6149c76454de94e0cf4fd51/codex-rs/core/src/context_manager/normalize.rs#L21-L67
[P-projection]: https://github.com/earendil-works/pi/blob/8ce69e9d2b171d173fe4b6b2b6256f1f4411e69d/packages/coding-agent/src/core/session-manager.ts#L434-L582
[P-legacy-compaction]: https://github.com/earendil-works/pi/blob/8ce69e9d2b171d173fe4b6b2b6256f1f4411e69d/packages/coding-agent/src/core/compaction/compaction.ts#L52-L135
[P-summary-call]: https://github.com/earendil-works/pi/blob/8ce69e9d2b171d173fe4b6b2b6256f1f4411e69d/packages/coding-agent/src/core/compaction/compaction.ts#L585-L639
[P-restore]: https://github.com/earendil-works/pi/blob/8ce69e9d2b171d173fe4b6b2b6256f1f4411e69d/packages/agent/src/harness/runtime/restore.ts#L91-L171
[P-durable-compaction]: https://github.com/earendil-works/pi/blob/8ce69e9d2b171d173fe4b6b2b6256f1f4411e69d/packages/durable/src/harness/compaction.ts#L98-L219
[P-durable-compaction-place]: https://github.com/earendil-works/pi/blob/8ce69e9d2b171d173fe4b6b2b6256f1f4411e69d/packages/durable/src/harness/compaction.ts#L399-L434
[D-S19]: https://github.com/deepseek-ai/deepseek-harness/blob/639ed015397290b3745d163aafe02ffee4aa3f84/packages/compaction/compaction-basic/src/region.ts#L135-L275
[D-S33]: https://github.com/deepseek-ai/deepseek-harness/blob/639ed015397290b3745d163aafe02ffee4aa3f84/packages/compaction/compaction-basic/src/region.ts#L470-L508
[R-PA26]: https://github.com/PrimeIntellect-ai/prime-agent/blob/5784abc2aef523a78d5a8850a0c0be89883388b2/crates/pa-core/src/session_engine/compact_session/prepare.rs#L38-L140
[R-PA54]: https://github.com/PrimeIntellect-ai/prime-agent/blob/5784abc2aef523a78d5a8850a0c0be89883388b2/crates/pa-core/src/session_engine/compact_session.rs#L425-L473
[K-CR27]: https://github.com/charmbracelet/crush/blob/76cc5c574e15072b15aaed0f4f843a5711fae0d9/internal/agent/agent.go#L1411-L1547
[K-CR28]: https://github.com/charmbracelet/crush/blob/76cc5c574e15072b15aaed0f4f843a5711fae0d9/internal/agent/agent.go#L1792-L1815
[K-CR29]: https://github.com/charmbracelet/crush/blob/76cc5c574e15072b15aaed0f4f843a5711fae0d9/internal/agent/agent.go#L1765-L1790
[C-步骤冻结]: https://github.com/openai/codex/blob/d4a475adda850d80b6149c76454de94e0cf4fd51/codex-rs/core/src/session/turn.rs#L426-L543
[C-采样重试]: https://github.com/openai/codex/blob/d4a475adda850d80b6149c76454de94e0cf4fd51/codex-rs/core/src/session/turn.rs#L1618-L1744
[P-ai-message]: https://github.com/earendil-works/pi/blob/8ce69e9d2b171d173fe4b6b2b6256f1f4411e69d/packages/ai/src/types.ts#L500-L607
[P-ai-context]: https://github.com/earendil-works/pi/blob/8ce69e9d2b171d173fe4b6b2b6256f1f4411e69d/packages/ai/src/types.ts#L732-L749
[P-sdk-retry]: https://github.com/earendil-works/pi/blob/8ce69e9d2b171d173fe4b6b2b6256f1f4411e69d/packages/coding-agent/src/core/sdk.ts#L316-L341
[P-assistant-recovery]: https://github.com/earendil-works/pi/blob/8ce69e9d2b171d173fe4b6b2b6256f1f4411e69d/packages/agent/src/harness/runtime/drive/recovery.ts#L22-L84
[D-S39]: https://github.com/deepseek-ai/deepseek-harness/blob/639ed015397290b3745d163aafe02ffee4aa3f84/packages/core/agent-loop/src/agent.ts#L546-L686
[D-S42]: https://github.com/deepseek-ai/deepseek-harness/blob/639ed015397290b3745d163aafe02ffee4aa3f84/packages/llm/llm-retry/src/index.ts#L164-L258
[D-S75]: https://github.com/deepseek-ai/deepseek-harness/blob/639ed015397290b3745d163aafe02ffee4aa3f84/packages/session/session-log-deepseek/src/index.ts#L38-L240
[R-PA24]: https://github.com/PrimeIntellect-ai/prime-agent/blob/5784abc2aef523a78d5a8850a0c0be89883388b2/crates/pa-core/src/session_engine/provider_retry.rs#L1-L102
[R-PA37]: https://github.com/PrimeIntellect-ai/prime-agent/blob/5784abc2aef523a78d5a8850a0c0be89883388b2/crates/pa-core/src/refinement/executor.rs#L191-L312
[K-CR24]: https://github.com/charmbracelet/crush/blob/76cc5c574e15072b15aaed0f4f843a5711fae0d9/internal/agent/agent.go#L844-L965
[K-CR48]: https://github.com/charmbracelet/crush/blob/76cc5c574e15072b15aaed0f4f843a5711fae0d9/internal/session/session.go#L93-L165
[C-工具并行]: https://github.com/openai/codex/blob/d4a475adda850d80b6149c76454de94e0cf4fd51/codex-rs/core/src/tools/parallel.rs#L44-L213
[P-loop-tools]: https://github.com/earendil-works/pi/blob/8ce69e9d2b171d173fe4b6b2b6256f1f4411e69d/packages/agent/src/agent-loop.ts#L508-L776
[P-agent-abort]: https://github.com/earendil-works/pi/blob/8ce69e9d2b171d173fe4b6b2b6256f1f4411e69d/packages/agent/src/agent.ts#L507-L529
[P-tool-intent]: https://github.com/earendil-works/pi/blob/8ce69e9d2b171d173fe4b6b2b6256f1f4411e69d/packages/agent/src/harness/runtime/drive/tools.ts#L44-L210
[P-tool-recovery]: https://github.com/earendil-works/pi/blob/8ce69e9d2b171d173fe4b6b2b6256f1f4411e69d/packages/agent/src/harness/runtime/drive/tools.ts#L478-L539
[P-durable-tool]: https://github.com/earendil-works/pi/blob/8ce69e9d2b171d173fe4b6b2b6256f1f4411e69d/packages/durable/src/harness/tool.ts#L33-L115
[D-S12]: https://github.com/deepseek-ai/deepseek-harness/blob/639ed015397290b3745d163aafe02ffee4aa3f84/packages/core/agent-loop/src/tool-calls.ts#L1-L225
[D-S13]: https://github.com/deepseek-ai/deepseek-harness/blob/639ed015397290b3745d163aafe02ffee4aa3f84/packages/core/session/src/repair.ts#L14-L97
[R-PA28]: https://github.com/PrimeIntellect-ai/prime-agent/blob/5784abc2aef523a78d5a8850a0c0be89883388b2/crates/pa-core/src/kernel/protocol.rs#L1-L245
[R-PA30]: https://github.com/PrimeIntellect-ai/prime-agent/blob/5784abc2aef523a78d5a8850a0c0be89883388b2/crates/pa-core/src/kernel/manager/execution.rs#L37-L108
[K-CR30]: https://github.com/charmbracelet/crush/blob/76cc5c574e15072b15aaed0f4f843a5711fae0d9/internal/shell/background.go#L16-L168
[K-CR33]: https://github.com/charmbracelet/crush/blob/76cc5c574e15072b15aaed0f4f843a5711fae0d9/internal/agent/tools/bash.go#L198-L347
[C-审批编排]: https://github.com/openai/codex/blob/d4a475adda850d80b6149c76454de94e0cf4fd51/codex-rs/core/src/tools/orchestrator.rs#L125-L223
[C-审批缓存]: https://github.com/openai/codex/blob/d4a475adda850d80b6149c76454de94e0cf4fd51/codex-rs/core/src/tools/sandboxing.rs#L65-L116
[C-沙箱选择]: https://github.com/openai/codex/blob/d4a475adda850d80b6149c76454de94e0cf4fd51/codex-rs/sandboxing/src/manager.rs#L49-L114
[P-permissions]: https://github.com/earendil-works/pi/blob/8ce69e9d2b171d173fe4b6b2b6256f1f4411e69d/README.md#L35-L47
[P-tool-mutation]: https://github.com/earendil-works/pi/blob/8ce69e9d2b171d173fe4b6b2b6256f1f4411e69d/packages/coding-agent/src/core/extensions/types.ts#L1198-L1212
[P-codemode-host]: https://github.com/earendil-works/pi/blob/8ce69e9d2b171d173fe4b6b2b6256f1f4411e69d/packages/codemode/src/runtime/host.ts#L276-L351
[P-harness-validation]: https://github.com/earendil-works/pi/blob/8ce69e9d2b171d173fe4b6b2b6256f1f4411e69d/packages/agent/src/harness/execution/tools.ts#L77-L122
[D-S28]: https://github.com/deepseek-ai/deepseek-harness/blob/639ed015397290b3745d163aafe02ffee4aa3f84/packages/interaction/user-approval/src/index.ts#L199-L306
[D-S44]: https://github.com/deepseek-ai/deepseek-harness/blob/639ed015397290b3745d163aafe02ffee4aa3f84/packages/sandbox/sandbox-policy/src/index.ts#L64-L179
[D-S29]: https://github.com/deepseek-ai/deepseek-harness/blob/639ed015397290b3745d163aafe02ffee4aa3f84/packages/sandbox/sandbox-local/src/index.ts#L1-L99
[R-PA02]: https://github.com/PrimeIntellect-ai/prime-agent/blob/5784abc2aef523a78d5a8850a0c0be89883388b2/README.md#L39-L120
[R-PA32]: https://github.com/PrimeIntellect-ai/prime-agent/blob/5784abc2aef523a78d5a8850a0c0be89883388b2/crates/pa-core/src/mcp/mod.rs#L1-L123
[K-CR31]: https://github.com/charmbracelet/crush/blob/76cc5c574e15072b15aaed0f4f843a5711fae0d9/internal/permission/permission.go#L94-L232
[K-CR34]: https://github.com/charmbracelet/crush/blob/76cc5c574e15072b15aaed0f4f843a5711fae0d9/internal/agent/tools/bash.go#L166-L197
[K-CR38]: https://github.com/charmbracelet/crush/blob/76cc5c574e15072b15aaed0f4f843a5711fae0d9/internal/agent/hooked_tool.go#L16-L94
[C-目标表]: https://github.com/openai/codex/blob/d4a475adda850d80b6149c76454de94e0cf4fd51/codex-rs/state/goals_migrations/0001_thread_goals.sql#L1-L18
[C-目标更新]: https://github.com/openai/codex/blob/d4a475adda850d80b6149c76454de94e0cf4fd51/codex-rs/ext/goal/src/tool.rs#L243-L313
[D-S45]: https://github.com/deepseek-ai/deepseek-harness/blob/639ed015397290b3745d163aafe02ffee4aa3f84/packages/goal/goal/src/types.ts#L19-L111
[D-S46]: https://github.com/deepseek-ai/deepseek-harness/blob/639ed015397290b3745d163aafe02ffee4aa3f84/packages/goal/goal/src/index.ts#L282-L492
[D-S47]: https://github.com/deepseek-ai/deepseek-harness/blob/639ed015397290b3745d163aafe02ffee4aa3f84/packages/goal/goal-round-driver/src/index.ts#L21-L159
[R-PA34]: https://github.com/PrimeIntellect-ai/prime-agent/blob/5784abc2aef523a78d5a8850a0c0be89883388b2/crates/pa-core/src/session_engine/goal_driver.rs#L380-L410
[R-PA48]: https://github.com/PrimeIntellect-ai/prime-agent/blob/5784abc2aef523a78d5a8850a0c0be89883388b2/crates/pa-core/src/session_engine/goal_driver.rs#L590-L688
[R-PA36]: https://github.com/PrimeIntellect-ai/prime-agent/blob/5784abc2aef523a78d5a8850a0c0be89883388b2/crates/pa-core/src/autonomous/gates.rs#L11-L187
[K-CR25]: https://github.com/charmbracelet/crush/blob/76cc5c574e15072b15aaed0f4f843a5711fae0d9/internal/agent/loop_detection.go#L10-L92
[C-历史位置]: https://github.com/openai/codex/blob/d4a475adda850d80b6149c76454de94e0cf4fd51/codex-rs/protocol/src/protocol.rs#L3090-L3115
[C-回滚恢复]: https://github.com/openai/codex/blob/d4a475adda850d80b6149c76454de94e0cf4fd51/codex-rs/core/src/session/rollout_reconstruction.rs#L116-L143
[P-legacy-branch]: https://github.com/earendil-works/pi/blob/8ce69e9d2b171d173fe4b6b2b6256f1f4411e69d/packages/coding-agent/src/core/session-manager.ts#L1573-L1726
[P-legacy-fork]: https://github.com/earendil-works/pi/blob/8ce69e9d2b171d173fe4b6b2b6256f1f4411e69d/packages/coding-agent/src/core/session-manager.ts#L1815-L1865
[D-fork]: https://github.com/deepseek-ai/deepseek-harness/blob/639ed015397290b3745d163aafe02ffee4aa3f84/packages/core/session/src/fork.ts#L1-L27
[R-fork]: https://github.com/PrimeIntellect-ai/prime-agent/blob/5784abc2aef523a78d5a8850a0c0be89883388b2/crates/pa-core/src/session/manager/lifecycle.rs#L195-L267
[K-CR15]: https://github.com/charmbracelet/crush/blob/76cc5c574e15072b15aaed0f4f843a5711fae0d9/internal/history/file.go#L14-L129
[C-父子图]: https://github.com/openai/codex/blob/d4a475adda850d80b6149c76454de94e0cf4fd51/codex-rs/agent-graph-store/src/store.rs#L13-L59
[C-冷子线程恢复]: https://github.com/openai/codex/blob/d4a475adda850d80b6149c76454de94e0cf4fd51/codex-rs/core/src/agent/control/spawn.rs#L467-L525
[C-子线程启动]: https://github.com/openai/codex/blob/d4a475adda850d80b6149c76454de94e0cf4fd51/codex-rs/core/src/agent/control/spawn.rs#L640-L870
[C-子线程完成]: https://github.com/openai/codex/blob/d4a475adda850d80b6149c76454de94e0cf4fd51/codex-rs/core/src/agent/control/completion.rs#L1-L129
[P-subagent]: https://github.com/earendil-works/pi/blob/8ce69e9d2b171d173fe4b6b2b6256f1f4411e69d/packages/coding-agent/examples/extensions/subagent/index.ts#L471-L545
[P-subagent-spawn]: https://github.com/earendil-works/pi/blob/8ce69e9d2b171d173fe4b6b2b6256f1f4411e69d/packages/coding-agent/examples/extensions/subagent/index.ts#L300-L375
[P-durable-subagent]: https://github.com/earendil-works/pi/blob/8ce69e9d2b171d173fe4b6b2b6256f1f4411e69d/packages/durable/test/examples/23-subagent-background.ts#L77-L119
[D-S15]: https://github.com/deepseek-ai/deepseek-harness/blob/639ed015397290b3745d163aafe02ffee4aa3f84/packages/subagent/subagent/src/types.ts#L19-L135
[D-S16]: https://github.com/deepseek-ai/deepseek-harness/blob/639ed015397290b3745d163aafe02ffee4aa3f84/packages/subagent/subagent/src/descriptor.ts#L1-L68
[D-S30]: https://github.com/deepseek-ai/deepseek-harness/blob/639ed015397290b3745d163aafe02ffee4aa3f84/packages/subagent/subagent/src/continuation.ts#L1-L101
[R-PA10]: https://github.com/PrimeIntellect-ai/prime-agent/blob/5784abc2aef523a78d5a8850a0c0be89883388b2/crates/pa-daemon/src/rlm_ledger.rs#L1-L167
[R-PA29]: https://github.com/PrimeIntellect-ai/prime-agent/blob/5784abc2aef523a78d5a8850a0c0be89883388b2/prime-agent-runtime/src/rlm/__init__.py#L161-L185
[R-PA45]: https://github.com/PrimeIntellect-ai/prime-agent/blob/5784abc2aef523a78d5a8850a0c0be89883388b2/prime-agent-runtime/src/rlm/__init__.py#L390-L425
[K-CR39]: https://github.com/charmbracelet/crush/blob/76cc5c574e15072b15aaed0f4f843a5711fae0d9/internal/agent/agent_tool.go#L14-L68
[K-CR49]: https://github.com/charmbracelet/crush/blob/76cc5c574e15072b15aaed0f4f843a5711fae0d9/internal/agent/coordinator.go#L1660-L1783
[D-S48]: https://github.com/deepseek-ai/deepseek-harness/blob/639ed015397290b3745d163aafe02ffee4aa3f84/packages/schedule/schedule/src/index.ts#L92-L114
[D-S49]: https://github.com/deepseek-ai/deepseek-harness/blob/639ed015397290b3745d163aafe02ffee4aa3f84/packages/schedule/schedule/src/storage.ts#L22-L60
[D-S50]: https://github.com/deepseek-ai/deepseek-harness/blob/639ed015397290b3745d163aafe02ffee4aa3f84/packages/schedule/schedule/src/runtime.ts#L86-L164
[R-PA18]: https://github.com/PrimeIntellect-ai/prime-agent/blob/5784abc2aef523a78d5a8850a0c0be89883388b2/crates/pa-core/src/cron/store/mod.rs#L1-L99
[R-PA47]: https://github.com/PrimeIntellect-ai/prime-agent/blob/5784abc2aef523a78d5a8850a0c0be89883388b2/crates/pa-core/src/cron/store/jobs.rs#L303-L414
[R-PA51]: https://github.com/PrimeIntellect-ai/prime-agent/blob/5784abc2aef523a78d5a8850a0c0be89883388b2/crates/pa-core/src/cron/store/state.rs#L261-L310
[R-PA52]: https://github.com/PrimeIntellect-ai/prime-agent/blob/5784abc2aef523a78d5a8850a0c0be89883388b2/crates/pa-core/src/cron/store/state.rs#L40-L67
[R-PA53]: https://github.com/PrimeIntellect-ai/prime-agent/blob/5784abc2aef523a78d5a8850a0c0be89883388b2/crates/pa-core/src/cron/store/state.rs#L361-L375
[C-unified-env]: https://github.com/openai/codex/blob/d4a475adda850d80b6149c76454de94e0cf4fd51/codex-rs/core/src/unified_exec/mod.rs#L72-L160
[P-codemode]: https://github.com/earendil-works/pi/blob/8ce69e9d2b171d173fe4b6b2b6256f1f4411e69d/packages/coding-agent/src/extensions/codemode/tool.ts#L1-L145
[P-codemode-run]: https://github.com/earendil-works/pi/blob/8ce69e9d2b171d173fe4b6b2b6256f1f4411e69d/packages/coding-agent/src/extensions/codemode/execute.ts#L245-L305
[P-invocation]: https://github.com/earendil-works/pi/blob/8ce69e9d2b171d173fe4b6b2b6256f1f4411e69d/packages/agent/src/harness/runtime/drive/tools.ts#L82-L130
[P-bash-output]: https://github.com/earendil-works/pi/blob/8ce69e9d2b171d173fe4b6b2b6256f1f4411e69d/packages/agent/src/harness/tools/bash.ts#L51-L145
[D-terminal]: https://github.com/deepseek-ai/deepseek-harness/blob/639ed015397290b3745d163aafe02ffee4aa3f84/packages/terminal/terminal/src/types.ts#L10-L145
[D-S17]: https://github.com/deepseek-ai/deepseek-harness/blob/639ed015397290b3745d163aafe02ffee4aa3f84/packages/jobs/jobs-local/src/index.ts#L1-L115
[R-PA19]: https://github.com/PrimeIntellect-ai/prime-agent/blob/5784abc2aef523a78d5a8850a0c0be89883388b2/crates/pa-core/src/kernel/manager/snapshot.rs#L25-L151
[R-PA46]: https://github.com/PrimeIntellect-ai/prime-agent/blob/5784abc2aef523a78d5a8850a0c0be89883388b2/prime-agent-runtime/src/rlm/repl.py#L1053-L1096
[C-记忆启动]: https://github.com/openai/codex/blob/d4a475adda850d80b6149c76454de94e0cf4fd51/codex-rs/memories/write/src/start.rs#L20-L92
[C-记忆任务表]: https://github.com/openai/codex/blob/d4a475adda850d80b6149c76454de94e0cf4fd51/codex-rs/state/memory_migrations/0001_memories.sql#L1-L35
[C-记忆二阶段]: https://github.com/openai/codex/blob/d4a475adda850d80b6149c76454de94e0cf4fd51/codex-rs/memories/write/src/phase2.rs#L49-L178
[C-记忆读取扩展]: https://github.com/openai/codex/blob/d4a475adda850d80b6149c76454de94e0cf4fd51/codex-rs/ext/memories/src/extension.rs#L25-L97
[P-skill-harness]: https://github.com/earendil-works/pi/blob/8ce69e9d2b171d173fe4b6b2b6256f1f4411e69d/packages/agent/src/harness/skills.ts#L38-L108
[D-S21]: https://github.com/deepseek-ai/deepseek-harness/blob/639ed015397290b3745d163aafe02ffee4aa3f84/packages/session-query/README.zh.md#L27-L43
[D-S22]: https://github.com/deepseek-ai/deepseek-harness/blob/639ed015397290b3745d163aafe02ffee4aa3f84/apps/cli/tests/memory-mcp-configs.spec.ts#L1-L55
[R-PA09]: https://github.com/PrimeIntellect-ai/prime-agent/blob/5784abc2aef523a78d5a8850a0c0be89883388b2/crates/pa-core/src/refinement/mod.rs#L11-L110
[R-PA17]: https://github.com/PrimeIntellect-ai/prime-agent/blob/5784abc2aef523a78d5a8850a0c0be89883388b2/prime-agent-runtime/src/rlm/harness.py#L345-L493
[R-PA38]: https://github.com/PrimeIntellect-ai/prime-agent/blob/5784abc2aef523a78d5a8850a0c0be89883388b2/crates/pa-core/src/refinement/planner.rs#L285-L358
[K-CR41]: https://github.com/charmbracelet/crush/blob/76cc5c574e15072b15aaed0f4f843a5711fae0d9/internal/skills/skills.go#L118-L180
[C-技能类型]: https://github.com/openai/codex/blob/d4a475adda850d80b6149c76454de94e0cf4fd51/codex-rs/skills/src/model.rs#L6-L94
[C-插件清单]: https://github.com/openai/codex/blob/d4a475adda850d80b6149c76454de94e0cf4fd51/codex-rs/plugin/src/manifest.rs#L3-L58
[C-MCP绑定]: https://github.com/openai/codex/blob/d4a475adda850d80b6149c76454de94e0cf4fd51/codex-rs/codex-mcp/src/binding.rs#L31-L98
[C-MCP准备调用]: https://github.com/openai/codex/blob/d4a475adda850d80b6149c76454de94e0cf4fd51/codex-rs/codex-mcp/src/binding.rs#L304-L362
[P-extension-load]: https://github.com/earendil-works/pi/blob/8ce69e9d2b171d173fe4b6b2b6256f1f4411e69d/packages/coding-agent/src/core/extensions/loader.ts#L523-L583
[P-skill-cli]: https://github.com/earendil-works/pi/blob/8ce69e9d2b171d173fe4b6b2b6256f1f4411e69d/packages/coding-agent/src/core/skills.ts#L67-L105
[P-mcp-entry]: https://github.com/earendil-works/pi/blob/8ce69e9d2b171d173fe4b6b2b6256f1f4411e69d/packages/coding-agent/src/extensions/mcp/index.ts#L1-L27
[P-durable-task-definition]: https://github.com/earendil-works/pi/blob/8ce69e9d2b171d173fe4b6b2b6256f1f4411e69d/packages/durable/src/types.ts#L142-L260
[D-S72]: https://github.com/deepseek-ai/deepseek-harness/blob/639ed015397290b3745d163aafe02ffee4aa3f84/packages/skill/skill/src/index.ts#L1-L68
[D-S70]: https://github.com/deepseek-ai/deepseek-harness/blob/639ed015397290b3745d163aafe02ffee4aa3f84/packages/mcp/mcp-client/src/tools.ts#L90-L265
[D-S51]: https://github.com/deepseek-ai/deepseek-harness/blob/639ed015397290b3745d163aafe02ffee4aa3f84/packages/extensions/cordis-host-runner/src/index.ts#L1-L38
[D-S52]: https://github.com/deepseek-ai/deepseek-harness/blob/639ed015397290b3745d163aafe02ffee4aa3f84/packages/extensions/cordis-host-runner/src/sandbox.ts#L1-L10
[R-PA33]: https://github.com/PrimeIntellect-ai/prime-agent/blob/5784abc2aef523a78d5a8850a0c0be89883388b2/crates/pa-core/src/skills/discovery.rs#L39-L150
[R-PA44]: https://github.com/PrimeIntellect-ai/prime-agent/blob/5784abc2aef523a78d5a8850a0c0be89883388b2/crates/pa-core/src/refinement/mod.rs#L244-L408
[K-CR46]: https://github.com/charmbracelet/crush/blob/76cc5c574e15072b15aaed0f4f843a5711fae0d9/internal/agent/tools/mcp/lifecycle.go#L16-L117
[K-CR47]: https://github.com/charmbracelet/crush/blob/76cc5c574e15072b15aaed0f4f843a5711fae0d9/internal/lsp/manager.go#L26-L122
[C-后台写入]: https://github.com/openai/codex/blob/d4a475adda850d80b6149c76454de94e0cf4fd51/codex-rs/rollout/src/recorder.rs#L998-L1088
[P-ai-events]: https://github.com/earendil-works/pi/blob/8ce69e9d2b171d173fe4b6b2b6256f1f4411e69d/packages/ai/src/types.ts#L751-L783
[P-session-ctor]: https://github.com/earendil-works/pi/blob/8ce69e9d2b171d173fe4b6b2b6256f1f4411e69d/packages/coding-agent/src/core/agent-session.ts#L362-L491
[P-durable-events]: https://github.com/earendil-works/pi/blob/8ce69e9d2b171d173fe4b6b2b6256f1f4411e69d/packages/durable/src/harness/events.ts#L25-L98
[D-S55]: https://github.com/deepseek-ai/deepseek-harness/blob/639ed015397290b3745d163aafe02ffee4aa3f84/packages/api/session-controller/src/assistant-stream.ts#L1-L101
[R-PA39]: https://github.com/PrimeIntellect-ai/prime-agent/blob/5784abc2aef523a78d5a8850a0c0be89883388b2/crates/pa-daemon/src/snapshot_stream.rs#L1-L198
[K-CR16]: https://github.com/charmbracelet/crush/blob/76cc5c574e15072b15aaed0f4f843a5711fae0d9/internal/message/message.go#L21-L135
[K-CR21]: https://github.com/charmbracelet/crush/blob/76cc5c574e15072b15aaed0f4f843a5711fae0d9/internal/agent/agent.go#L785-L838
[K-CR40]: https://github.com/charmbracelet/crush/blob/76cc5c574e15072b15aaed0f4f843a5711fae0d9/internal/pubsub/broker.go#L1-L196
[C-待审批重放]: https://github.com/openai/codex/blob/d4a475adda850d80b6149c76454de94e0cf4fd51/codex-rs/app-server/src/outgoing_message.rs#L446-L494
[C-恢复取得写入权]: https://github.com/openai/codex/blob/d4a475adda850d80b6149c76454de94e0cf4fd51/codex-rs/thread-store/src/local/live_writer.rs#L25-L97
[P-legacy-load]: https://github.com/earendil-works/pi/blob/8ce69e9d2b171d173fe4b6b2b6256f1f4411e69d/packages/coding-agent/src/core/session-manager.ts#L616-L669
[P-rpc-types]: https://github.com/earendil-works/pi/blob/8ce69e9d2b171d173fe4b6b2b6256f1f4411e69d/packages/coding-agent/src/modes/rpc/rpc-types.ts#L1-L74
[P-mcp-http]: https://github.com/earendil-works/pi/blob/8ce69e9d2b171d173fe4b6b2b6256f1f4411e69d/packages/mcp/src/transports/streamable-http.ts#L356-L432
[D-S56]: https://github.com/deepseek-ai/deepseek-harness/blob/639ed015397290b3745d163aafe02ffee4aa3f84/packages/sdk/protocol/src/transport.ts#L1-L77
[R-PA16]: https://github.com/PrimeIntellect-ai/prime-agent/blob/5784abc2aef523a78d5a8850a0c0be89883388b2/crates/pa-daemon/src/lease.rs#L1-L109
[K-CR51]: https://github.com/charmbracelet/crush/blob/76cc5c574e15072b15aaed0f4f843a5711fae0d9/internal/workspace/client_workspace.go#L869-L949
