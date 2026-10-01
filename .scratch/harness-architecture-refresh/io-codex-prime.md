# Codex / Prime：同场景数据对象与持久读写

静态核查于 2026-10-01。Codex 固定提交 `d4a475adda850d80b6149c76454de94e0cf4fd51`；Prime 固定提交 `5784abc2aef523a78d5a8850a0c0be89883388b2`。以下计数指源码上的**逻辑记录追加和 API 调用**，不指块设备 I/O、系统调用、网络包或吞吐实测。

范围：已加载、已物化、至少已有一个助手回复的暖会话，历史仍在内存；一次新用户文本，无并发输入、审批、retry、compaction、子任务、goal、refine。A = 1 次模型请求、0 次工具；B = 2 次模型请求、1 次工具且 1 条结果，再由模型最终答复。配置/工具目录/上下文不变、无额外通知时可用下方条件示例；未固定的伴随路径另列。若工具自行读写文件/网络，这些业务效果另计。**Codex 当前 LocalThreadStore 的默认模式是 Paginated**；trait 的 Legacy 默认仅是接口兼容值，恢复已有历史或明确指定的模式另论。[C21][C29][C30]

## 1. 数据对象

| 项目 | 对象与关联 | 权威载体 / 热路径角色 |
| --- | --- | --- |
| Codex | Thread / Session → TurnContext → StepContext；历史是 ResponseItemEnvelope；展示为 TurnItem / EventMsg | Session 内存持有活动回合；每次请求捕获 StepContext 并从内存历史生成模型输入。[C1][C2] |
| Codex | RolloutItem：SessionMeta、ResponseItem、TurnContext、WorldState、RetainedContext、TokenUsageRecord、EventMsg 等 | 会话 JSONL 是 canonical history；一个请求能产生多类行，展示事件与原始 item 并非二选一。[C3][C4][C5] |
| Codex | SQLite threads 元数据；thread_history turns/items/projection cursor；goals、memory/jobs、queue、logs 等数据库 | history projection 可重建；独立 goals/jobs 等具有自己的持久责任，不能统称为会话缓存。A/B 不使用 goal/job 路径。[C6][C7] |
| Prime | SessionHeader + FileEntry 树；EntryBase id/parentId/timestamp 指向当前 leaf；message 的 User / Assistant / ToolResult | 会话 JSONL；助手 content 内含文本、thinking、toolCall，usage 与 stopReason 随整条助手消息保存；工具结果带 toolCallId。[P1][P2][P24] |
| Prime | AgentContext / AgentSession / SessionManager；daemon 模式由 SessionFile 写权威文件 | 模型循环读内存消息；MessageEnd 才追加完整消息。daemon 的引擎内存与 SessionFile 不等于两个权威文件写者。[P3][P4][P5][P25] |
| Prime | window-cache、info-cache；kernel namespace snapshot + manifest；harness state；thread_goal_state；RLM/cron/lease/command recovery records | 前两者为可丢侧车；kernel 为最佳努力恢复；harness/goal/cron 各自有保存语义，完整对象资料见[原项目报告](../../docs/research/prime-agent/README.md)。当前 A/B 只把 kernel/lease 等相关伴随路径列出，不能把整个对象集合都记为每提示一次写入。[P6][P7][P8] |

## 2. 逻辑追加数：核心记录与附加项分开

| 项目 / 路径 | A 的核心会话对象 | B 的核心会话对象 | 完整会话行计数 |
| --- | --- | --- | --- |
| Codex | 用户 ResponseItem 1 + 最终助手 ResponseItem 1 = **2** | 上述 2 + tool call 1 + tool output 1 = **4** | 还须加生命周期、用量、呈现及上下文记录；见公式 |
| Prime | 用户 message 1 + 助手 message 1 = **2** | 用户 1 + 带 toolCall 的助手 1 + ToolResult 1 + 最终助手 1 = **4** | 基础公式 `N_message = 1 + M + T`；伴随 custom/git 等行另计 |

Codex 的新增 JSONL 行可以写成：

`N_C = (2 + 2T + R) + 2 + K + W + C + M + U + D + H`

- `M` 是实际完成的模型请求数：A = 1，B = 2；`T` 是工具调用数：A = 0，B = 1。`R` 是额外完成的原始模型项，如 reasoning、额外 commentary 或多个最终 item；`2` 生命周期行是 TurnStarted / TurnComplete。[C8][C9][C10]
- `K` 是 TurnContext 行；当前正常真实用户回合建立 1 条基线，item 带新 turn_id；`W` 为 WorldState 全量/patch，`C` 为额外模型可见上下文 ResponseItem（设置差分、hook、skill、提醒等）；稳定暖会话可令 `W=C=0`，不能对所有配置如此假定。[C11][C12]
- 每完成一次正常 sampling 会发 TokenCount，即 `M`；`U` 为实际有 usage 的 TokenUsageRecord 数，`0 ≤ U ≤ M`。RawResponseCompleted 本身被持久策略排除。使用量完整时 `U=M`。[C13][C25][C27][C4]
- `D` 为该 history_mode 下通过过滤的呈现事件。Legacy 保存 UserMessage / AgentMessage / 可见 reasoning 与部分工具终态；Paginated 保存 ItemCompleted，过滤相应 legacy 呈现。一个 assistant 的多段文本可能派生多条 AgentMessage。工具类型也会改变 `D`：普通命令的 ExecCommandEnd 被过滤，MCP 的 McpToolCallEnd 在 Legacy 保留。[C4][C14]
- `H` 为未落入上述分类的其他通过过滤行；本范围禁用的 goal/compaction/子任务应为 0，但不能把开启扩展的其他路径默认为 0。Token、metadata 的字段变大不自动增加行数。[C3][C4]

**已限定示例**：最终助手只有一个纯文本段，模型无 reasoning / commentary 等额外 item，usage 完整，只有常规上下文基线，无 WorldState/注入/扩展增量（`R=W=C=H=0,K=1,U=M`）。当前本地默认 **Paginated**：A 的 User / Agent ItemCompleted 共 2 条，`D=2`，合计 **9 行**；B 若使用普通命令工具，其 CommandExecution ItemCompleted 多 1 条，`D=3`，合计 **14 行**。明确选择 **Legacy** 的相同例子：A **9 行**，B 的 ItemCompleted / ExecCommandEnd 被过滤，`D=2`，合计 **13 行**。工具类型或实际呈现项数不同时，仍用公式；SQLite projection 与 JSONL 行分开计数。[C4][C14][C15][C28][C29]

Prime 的 `N_message=1+M+T` 来自：用户 prompt 只通过 loop 的 MessageEnd 追加一次；每模型请求结束追加一个 AssistantMessage；每工具结果再发一个 MessageEnd。AssistantMessage 已含 toolCall 和 usage，因此不能额外加“call 行”“usage 行”。在本范围 A=2、B=4；若第一条助手在同一 content 中附带说明/thinking，不额外拆 message 行。[P2][P3][P9][P10][P26][P27][P29]

Prime 的总会话行是 `N_P = N_message + J + G + S + O`：`J` 为实际送入 loop 的 custom/digest/恢复通知；`G` 为实际新增 git_state；`S` 为创建/恢复或明确状态操作产生的 session_state；`O` 为其他明确追加。暖会话没有 pending digest 时会直接返回，不注入第一回合 digest。standalone 在 AgentStart/End 探测 Git，只有变化才追加；daemon 的常规持久事件分支只对 User/Assistant/ToolResult 等行追加。当前未证实每提示都追加 busy/idle session_state，因此不给它固定 `+2`。[P3][P5][P11][P12]

## 3. 热路径读、写与提交边界

| 路径 | 读入口 | 写 / 提交边界 | 能界定什么 |
| --- | --- | --- | --- |
| Codex 模型输入 | `clone_history().for_prompt()`：每 sampling 从 Session 内存历史取值 | 完成的 response item → record_conversation_items；用户输入及工具结果有各自调用入口 | A/B 的核心历史读无需每次重新加载整份 JSONL；不能据此称全请求“0 文件读”，动态资源/配置/工具可读文件。[C2][C16][C17] |
| Codex JSONL | live recorder 从内存 map 取；普通 append 不为重建输入扫描历史 | 非空过滤批次：AddItems + flush ack；每条序列化为 JSON+newline，然后 write_all、file.flush；writer 批次结束还 flush | 成功路径每新增行 1 次 write_line，即 `N_C` 次 write_all 和至少 `N_C` 次 file.flush。另有 writer 批次 flush；**普通追加无 sync_data / sync_all**。不能把方法名 durable_write 或 committed 注释解释为 fsync。[C18][C19][C20] |
| Codex Legacy | 热历史不投影 thread_history | 明确指定 / 保留原 Legacy 历史的分支只 durable_write | 这里的 thread_history projection commit = **0**；threads 元数据、usage/memory/logs 等伴随 SQLite 操作不包含在这个 0 中。[C18][C21] |
| Codex Paginated（本地默认） | 每进入带 state DB 的 materialization 查询 projection cursor，读 session_meta 并读 JSONL 新增完整后缀；有新增数据的事务内再读 cursor 核对 | JSONL flush 成功后才能投影；有新数据时 BEGIN IMMEDIATE，改 turns/items 和 byte/ordinal cursor，commit | 设 `Q` 为非空 canonical append 批次数、`B` 为显式 persist/flush 次数，则 materialize 调用 `Q+B`；成功且有新完整字节的事务数 `K_SQL` 满足 `0 ≤ K_SQL ≤ Q+B`，非按 message 数或模型数固定。SQL row 数受 change-set 影响；WAL / synchronous Normal 不能直接换算 fsync 次数。[C18][C22][C23][C29][C31][C32] |
| Prime standalone | AgentContext 消息复制/转换；SessionManager 内存 leaf/index；persist_entry 检查文件存在 | MessageEnd → append_message_retained → persist_entry → append_cached | 已有助手、文件在场且 flushed 的成功路径，每 message 行 **1 次 append_cached，1 次 write_all、flush、sync_data**，所以核心 A=2、B=4 次 sync_data。失败会保留 live row 并记录错误，不能称 UI 可见就已耐久。[P3][P13][P14] |
| Prime daemon | 引擎读内存；worker SessionFile 用内存索引；每 lease.append 读 owner 并核对 token/path | EngineEvent 完整消息 → SessionFile.persist_entry → lease.append → 同一个 append_cached | 核心仍 2 / 4 次 sync_data；租约 owner 文件读取、锁文件/目录检查另有 filesystem 操作，不能给全链路固定 read syscall 数。[P5][P15][P16] |

Codex 若要精确统计高层 flush 调用，可记 `F_file = N_C + F_writer`；`F_writer` 是实际进入 write_pending_once 的次数。已物化、无错误路径中，AddItems 的自动 flush 与随后 ack flush 都可能调用 writer flush，显式回合结束 flush 另计；空追加被过滤后不发 AddItems。`Q` 可以是多行批次，`F_file` 也不等于磁盘写次数。[C18][C19][C20][C24]

Prime 刚建但未首次助手/文件尚未物化的分支不是这里的暖条件：部分行留在内存；首次助手可能把 header+已有 entries 全量 rewrite，一次临时文件 sync_all 后 rename。文件丢失也会走重写分支；daemon rewrite 同样有 bootstrap。不能把首次请求的 rewrite 成本套到稳态追加，也不能把 sync_all+rename 扩展为跨文件原子事务或断电实测结论。[P14][P15]

## 4. 流式 chunk 与伴随写

- **Codex**：AgentMessageContentDelta、ReasoningContentDelta、ExecCommandOutputDelta、ItemStarted、RawResponseItem 等在持久策略中为 false；完整 ResponseItem / 允许的终态 EventMsg 才形成 canonical 行。因此 token/chunk 数不放大上述 canonical 记录数。trace/log export 可另有行为，未计入。[C4][C16]
- **Prime**：delta 是 MessageUpdate，工具 progress 是 ToolExecutionUpdate；persist_event 只处理 MessageEnd 和 Git 边界，daemon 持久 switch 也不为 AssistantUpdate / ToolExecutionUpdate 写 message。TCP/attach frame chunk 数不等于会话追加数。[P3][P5][P9]
- **Prime B 的默认可比模式**：生产引擎确保存在 `ipython`，模型发一个 Python program 调用，执行一个只产生普通文本结果的 cell，再最终回答；这最接近 B，但不能把 program 内任意多个 bash/MCP 动作算成 1 个普通业务工具效果。可注册普通工具也走同一 loop 公式。默认持久 namespace 在成功 cell 后安排 debounced snapshot，因此它的成本必须单列。[P17][P18]
- kernel snapshot 可能在响应后写 `.dill` 数据+manifest、临时文件和 replace；受 debounce、freshness skip、当前配置与运行期变量大小影响，不能每个 toolResult 固定 `+1 文件写`，也不与会话行形成事务。warm A 若 kernel 已稳定且无待 flush，不由模型最终文本触发成功-cell snapshot。[P7][P18][P19]
- Prime window-cache 的 append 时只更新认证的进程内 snapshot；持有租约释放时才 best effort flush window-cache / info-cache。缓存 save 用临时文件、flush、rename，没有该路径的 sync_data；会话长期保持打开时，不把释放写计入每提示。[P6][P20]
- `CommandRecoveryJournal` 能保存 received/result，但本提交静态检索未找到从正常暖 prompt 调用它的生产入口（定义/单测可见），**不给 A/B 固定加 2 行**；如果将来有入口，应以实际调用计数。trace、telemetry、诊断、auth/settings 更新、模型目录刷新、目标工具业务副作用均未纳入上述核心公式；全进程物理 I/O 上界未确定。[P21][C25]

## 5. 冷打开另算

| 项目 | 冷打开的数据读写 | 和暖请求的区别 |
| --- | --- | --- |
| Codex Legacy resume | open_rollout_line_reader 逐行扫描 JSONL（或解压），解析 SessionMeta/items/usage/context 后重建；另有 SQLite 初始化/元数据、配置/资源装配 | 不是每 sampling 重扫；格式错误行可能被警告并跳过。Paginated 还可按投影游标 catch up，故依赖缓存现状和落后范围，不能固定“1 次读”。[C26][C22] |
| Prime 常规 reader | repair_jsonl_damage 后 read_to_string 整文件；必要 migration/repair 可重写 | repair/rewrite 的 sync_all 属冷打开或异常恢复成本。[P22][P14] |
| Prime windowed reader | 检查文件 generation；有效内存/sidecar cache 可恢复窗口；cache miss 反向扫描，损坏/旧 schema/非 Unix 可 fallback 完整 reader | 范围读与 cache read 数由文件/窗口/命中情况决定；kernel snapshot 存在则可能 prewarm restore，并在下一入场回合注入恢复说明，不能混入无恢复通知的暖 A/B。[P6][P23][P17] |

比较时最小可信指标是：模型/工具调用数、canonical 逻辑行数与字节数、API 级 append/flush/sync 次数、SQL statement / transaction 次数、日志/缓存/快照伴随成本各自报告。实际 OS page cache、Tokio 文件后端、SQLite WAL checkpoint、文件系统与工具业务读写需要运行期 tracing 才能比较物理 I/O；静态计数不证明性能高低。

[C1]: https://github.com/openai/codex/blob/d4a475adda850d80b6149c76454de94e0cf4fd51/codex-rs/core/src/session/session.rs#L57-L160
[C2]: https://github.com/openai/codex/blob/d4a475adda850d80b6149c76454de94e0cf4fd51/codex-rs/core/src/session/turn.rs#L461-L543
[C3]: https://github.com/openai/codex/blob/d4a475adda850d80b6149c76454de94e0cf4fd51/codex-rs/history/src/rollout_payload.rs#L30-L72
[C4]: https://github.com/openai/codex/blob/d4a475adda850d80b6149c76454de94e0cf4fd51/codex-rs/rollout/src/policy.rs#L9-L206
[C5]: https://github.com/openai/codex/blob/d4a475adda850d80b6149c76454de94e0cf4fd51/codex-rs/thread-store/README.md#L1-L34
[C6]: https://github.com/openai/codex/blob/d4a475adda850d80b6149c76454de94e0cf4fd51/codex-rs/state/src/sqlite.rs#L34-L111
[C7]: https://github.com/openai/codex/blob/d4a475adda850d80b6149c76454de94e0cf4fd51/codex-rs/state/goals_migrations/0001_thread_goals.sql#L1-L18
[C8]: https://github.com/openai/codex/blob/d4a475adda850d80b6149c76454de94e0cf4fd51/codex-rs/core/src/session/mod.rs#L2220-L2235
[C9]: https://github.com/openai/codex/blob/d4a475adda850d80b6149c76454de94e0cf4fd51/codex-rs/core/src/tasks/mod.rs#L826-L873
[C10]: https://github.com/openai/codex/blob/d4a475adda850d80b6149c76454de94e0cf4fd51/codex-rs/core/src/stream_events_utils.rs#L93-L156
[C11]: https://github.com/openai/codex/blob/d4a475adda850d80b6149c76454de94e0cf4fd51/codex-rs/core/src/session/mod.rs#L4621-L4698
[C12]: https://github.com/openai/codex/blob/d4a475adda850d80b6149c76454de94e0cf4fd51/codex-rs/core/src/session/turn_context.rs#L749-L788
[C13]: https://github.com/openai/codex/blob/d4a475adda850d80b6149c76454de94e0cf4fd51/codex-rs/core/src/session/mod.rs#L4713-L4744
[C14]: https://github.com/openai/codex/blob/d4a475adda850d80b6149c76454de94e0cf4fd51/codex-rs/protocol/src/legacy_events.rs#L125-L179
[C15]: https://github.com/openai/codex/blob/d4a475adda850d80b6149c76454de94e0cf4fd51/codex-rs/protocol/src/legacy_events.rs#L549-L647
[C16]: https://github.com/openai/codex/blob/d4a475adda850d80b6149c76454de94e0cf4fd51/codex-rs/core/src/session/mod.rs#L3458-L3605
[C17]: https://github.com/openai/codex/blob/d4a475adda850d80b6149c76454de94e0cf4fd51/codex-rs/core/src/session/turn.rs#L2472-L2499
[C18]: https://github.com/openai/codex/blob/d4a475adda850d80b6149c76454de94e0cf4fd51/codex-rs/thread-store/src/local/live_writer.rs#L316-L383
[C19]: https://github.com/openai/codex/blob/d4a475adda850d80b6149c76454de94e0cf4fd51/codex-rs/rollout/src/recorder.rs#L1892-L1969
[C20]: https://github.com/openai/codex/blob/d4a475adda850d80b6149c76454de94e0cf4fd51/codex-rs/rollout/src/recorder.rs#L2071-L2097
[C21]: https://github.com/openai/codex/blob/d4a475adda850d80b6149c76454de94e0cf4fd51/codex-rs/thread-store/src/store.rs#L94-L105
[C22]: https://github.com/openai/codex/blob/d4a475adda850d80b6149c76454de94e0cf4fd51/codex-rs/thread-store/src/local/thread_history_materialization.rs#L22-L83
[C23]: https://github.com/openai/codex/blob/d4a475adda850d80b6149c76454de94e0cf4fd51/codex-rs/thread-store/src/local/thread_history.rs#L103-L241
[C24]: https://github.com/openai/codex/blob/d4a475adda850d80b6149c76454de94e0cf4fd51/codex-rs/rollout/src/recorder.rs#L1035-L1088
[C25]: https://github.com/openai/codex/blob/d4a475adda850d80b6149c76454de94e0cf4fd51/codex-rs/core/src/session/turn.rs#L2939-L3000
[C26]: https://github.com/openai/codex/blob/d4a475adda850d80b6149c76454de94e0cf4fd51/codex-rs/rollout/src/recorder.rs#L1091-L1147
[C27]: https://github.com/openai/codex/blob/d4a475adda850d80b6149c76454de94e0cf4fd51/codex-rs/core/src/session/turn.rs#L3173-L3179
[C28]: https://github.com/openai/codex/blob/d4a475adda850d80b6149c76454de94e0cf4fd51/codex-rs/core/src/tools/events.rs#L581-L609
[C29]: https://github.com/openai/codex/blob/d4a475adda850d80b6149c76454de94e0cf4fd51/codex-rs/thread-store/src/local/mod.rs#L478-L481
[C30]: https://github.com/openai/codex/blob/d4a475adda850d80b6149c76454de94e0cf4fd51/codex-rs/core/src/session/mod.rs#L724-L744
[C31]: https://github.com/openai/codex/blob/d4a475adda850d80b6149c76454de94e0cf4fd51/codex-rs/state/src/sqlite.rs#L349-L394
[C32]: https://github.com/openai/codex/blob/d4a475adda850d80b6149c76454de94e0cf4fd51/codex-rs/thread-store/src/local/thread_history.rs#L63-L101
[P1]: https://github.com/PrimeIntellect-ai/prime-agent/blob/5784abc2aef523a78d5a8850a0c0be89883388b2/crates/pa-types/src/session.rs#L40-L74
[P2]: https://github.com/PrimeIntellect-ai/prime-agent/blob/5784abc2aef523a78d5a8850a0c0be89883388b2/crates/pa-types/src/ai/mod.rs#L568-L615
[P3]: https://github.com/PrimeIntellect-ai/prime-agent/blob/5784abc2aef523a78d5a8850a0c0be89883388b2/crates/pa-core/src/session_engine/mod.rs#L400-L445
[P4]: https://github.com/PrimeIntellect-ai/prime-agent/blob/5784abc2aef523a78d5a8850a0c0be89883388b2/crates/pa-agent/src/agent_loop/response.rs#L94-L160
[P5]: https://github.com/PrimeIntellect-ai/prime-agent/blob/5784abc2aef523a78d5a8850a0c0be89883388b2/crates/pa-daemon/src/worker/turn.rs#L682-L705
[P6]: https://github.com/PrimeIntellect-ai/prime-agent/blob/5784abc2aef523a78d5a8850a0c0be89883388b2/crates/pa-core/src/session/window_cache.rs#L105-L239
[P7]: https://github.com/PrimeIntellect-ai/prime-agent/blob/5784abc2aef523a78d5a8850a0c0be89883388b2/crates/pa-core/src/kernel/manager/snapshot.rs#L411-L458
[P8]: https://github.com/PrimeIntellect-ai/prime-agent/blob/5784abc2aef523a78d5a8850a0c0be89883388b2/crates/pa-core/src/session_engine/runtime_wiring.rs#L230-L266
[P9]: https://github.com/PrimeIntellect-ai/prime-agent/blob/5784abc2aef523a78d5a8850a0c0be89883388b2/crates/pa-agent/src/agent_loop/response.rs#L179-L254
[P10]: https://github.com/PrimeIntellect-ai/prime-agent/blob/5784abc2aef523a78d5a8850a0c0be89883388b2/crates/pa-agent/src/agent_loop/tool_call.rs#L302-L344
[P11]: https://github.com/PrimeIntellect-ai/prime-agent/blob/5784abc2aef523a78d5a8850a0c0be89883388b2/crates/pa-core/src/session_engine/harness_digest.rs#L509-L533
[P12]: https://github.com/PrimeIntellect-ai/prime-agent/blob/5784abc2aef523a78d5a8850a0c0be89883388b2/crates/pa-core/src/session/manager_ext.rs#L55-L92
[P13]: https://github.com/PrimeIntellect-ai/prime-agent/blob/5784abc2aef523a78d5a8850a0c0be89883388b2/crates/pa-core/src/session/manager/append.rs#L55-L95
[P14]: https://github.com/PrimeIntellect-ai/prime-agent/blob/5784abc2aef523a78d5a8850a0c0be89883388b2/crates/pa-core/src/session/manager/persist.rs#L99-L173
[P15]: https://github.com/PrimeIntellect-ai/prime-agent/blob/5784abc2aef523a78d5a8850a0c0be89883388b2/crates/pa-daemon/src/session_store/write.rs#L154-L224
[P16]: https://github.com/PrimeIntellect-ai/prime-agent/blob/5784abc2aef523a78d5a8850a0c0be89883388b2/crates/pa-daemon/src/lease.rs#L392-L413
[P17]: https://github.com/PrimeIntellect-ai/prime-agent/blob/5784abc2aef523a78d5a8850a0c0be89883388b2/crates/pa-core/src/session_engine/engine.rs#L410-L499
[P18]: https://github.com/PrimeIntellect-ai/prime-agent/blob/5784abc2aef523a78d5a8850a0c0be89883388b2/crates/pa-core/src/kernel/manager/mod.rs#L620-L635
[P19]: https://github.com/PrimeIntellect-ai/prime-agent/blob/5784abc2aef523a78d5a8850a0c0be89883388b2/prime-agent-runtime/src/rlm/repl.py#L754-L920
[P20]: https://github.com/PrimeIntellect-ai/prime-agent/blob/5784abc2aef523a78d5a8850a0c0be89883388b2/crates/pa-daemon/src/lease.rs#L351-L372
[P21]: https://github.com/PrimeIntellect-ai/prime-agent/blob/5784abc2aef523a78d5a8850a0c0be89883388b2/crates/pa-daemon/src/journal.rs#L158-L235
[P22]: https://github.com/PrimeIntellect-ai/prime-agent/blob/5784abc2aef523a78d5a8850a0c0be89883388b2/crates/pa-core/src/session/manager/repair.rs#L134-L147
[P23]: https://github.com/PrimeIntellect-ai/prime-agent/blob/5784abc2aef523a78d5a8850a0c0be89883388b2/crates/pa-core/src/session/window.rs#L204-L267
[P24]: https://github.com/PrimeIntellect-ai/prime-agent/blob/5784abc2aef523a78d5a8850a0c0be89883388b2/crates/pa-types/src/session.rs#L657-L667
[P25]: https://github.com/PrimeIntellect-ai/prime-agent/blob/5784abc2aef523a78d5a8850a0c0be89883388b2/crates/pa-core/src/session_engine/runtime_wiring.rs#L113-L120
[P26]: https://github.com/PrimeIntellect-ai/prime-agent/blob/5784abc2aef523a78d5a8850a0c0be89883388b2/crates/pa-agent/src/agent_loop/entry.rs#L25-L64
[P27]: https://github.com/PrimeIntellect-ai/prime-agent/blob/5784abc2aef523a78d5a8850a0c0be89883388b2/crates/pa-core/src/session_engine/admission.rs#L144-L173
[P29]: https://github.com/PrimeIntellect-ai/prime-agent/blob/5784abc2aef523a78d5a8850a0c0be89883388b2/crates/pa-agent/src/agent_loop/run.rs#L78-L123
