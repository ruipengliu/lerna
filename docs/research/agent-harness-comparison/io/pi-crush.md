# Pi 经典 CLI 与 Crush：同场景对象及持久存储调用

本稿为源码静态推导，未安装依赖、运行模型或测量设备 I/O。固定版本：Pi `8ce69e9d2b171d173fe4b6b2b6256f1f4411e69d`，Crush `76cc5c574e15072b15aaed0f4f843a5711fae0d9`。比较默认 Pi coding-agent 经典 CLI 与 Crush 本地暖会话，不能把 Pi 的三个独立存储主线合计，也不能把 Crush 可选 server 接纳路径计入默认本地入口。

## 1. 场景、口径与结论

共同条件：会话已打开、已有正常用户历史与标题；模型、system prompt、工具声明、channel 不变；无 reasoning、压缩、分支、重试、审批、子任务、goal、并发新输入、改变消息的扩展及额外 UI 刷新。使用正常成功路径，所有计数假设存储调用成功。A 为一次用户文本、一次模型最终答复；B 为一次用户文本、模型的一次工具调用、一个普通工具结果、第二次模型最终答复。B 的具体工具是读取工作目录内已有 UTF-8 普通文本文件，排除 skill、图片和需审批的路径。

Pi 的计数窗口内暂不触发 cache warming；若触发，另加变量 `w`。Crush 默认本地且持久 channel 已为空，TUI 提交前待登记的文件列表为空；列表非空的影响另列。既有标题本身不足以排除 Crush 自动标题模型：代码判断的是历史中有无正常用户文本。上述暖会话同时满足这个条件。[Pi 默认 Agent 装配](https://github.com/earendil-works/pi/blob/8ce69e9d2b171d173fe4b6b2b6256f1f4411e69d/packages/coding-agent/src/core/sdk.ts#L387-L438)、[Crush 本地入口](https://github.com/charmbracelet/crush/blob/76cc5c574e15072b15aaed0f4f843a5711fae0d9/internal/workspace/app_workspace.go#L111-L117)、[Crush 首次用户文本标题条件](https://github.com/charmbracelet/crush/blob/76cc5c574e15072b15aaed0f4f843a5711fae0d9/internal/agent/agent.go#L735-L759)。

| 指标 | Pi A | Pi B | Crush A | Crush B：一次普通 `view` |
|---|---:|---:|---:|---:|
| 模型调用 `m`、工具结果 `t` | 1、0 | 2、1 | 1、0 | 2、1 |
| 新增逻辑完整消息 | 2 | 4 | 2 | 4 |
| 经典会话 JSONL 新行／`appendFileSync` 调用 | 2／2 | 4／4 | 不适用 | 不适用 |
| 会话热路径内容读取 | JSONL 内容读取 0 | JSONL 内容读取 0 | 核心显式 SELECT 5 | 核心显式 SELECT 7 |
| 核心显式 DML | 不适用 | 不适用 | `4 + X` | `10 + X`，另加读取登记 1 |
| 流式片段是否可能产生中途持久写 | 否，结束消息追加 | 否，结束消息追加 | 是，合并后 UPDATE | 是，合并后 UPDATE |
| 显式 fsync 次数 | 经典追加路径未调用 | 经典追加路径未调用 | 不能从 SQL 次数推出 | 不能从 SQL 次数推出 |

表中 Crush B 假设 provider 正常发出一次 `OnToolInputStart`，对应 `S=1`；没有这一回调时核心 DML 减 1。这里 `X\geq0` 是**实际额外成功执行的消息 UPDATE 数**，含普通增量的定时刷新及刷新期间新状态导致的补写；不是 token/chunk 数，不是耗时除以 33ms。Crush A 的显式 SQL 总数为 `9+X`；B 核心为 `17+X`，计入这一次普通文件读取登记为 `18+X`。依据和变量在第 4 节展开。

严格区分：

- Pi 一条 JSONL entry 是逻辑日志行；一次 Node 追加 API 不是一个磁盘扇区写入或一次 fsync。
- Crush 一条顶层 SELECT／INSERT／UPDATE 是应用显式 SQL 调用；`RETURNING` 不再加一次 SELECT，trigger 内 SQL 不再算一次应用调用。
- SQLite 页更新、WAL 写、checkpoint、OS 缓存写回、设备落盘均不与顶层 SQL 一一对应。本稿没有这些物理次数，也不据此推断时延或吞吐。
- 工具读取目标文件、路径检查、鉴权/config 元数据检查及 LSP 的 I/O 与会话存储分开；“JSONL 热读 0”并不表示整个进程没有文件读取。

## 2. 参与对象与状态在哪里

| 作用 | Pi 经典 CLI | Crush 默认本地 |
|---|---|---|
| 会话身份与配置 | v3 `SessionHeader`，model/thinking 等 entry；默认位于 `~/.pi/agent/sessions/<encoded-cwd>/` | `sessions` 行：SessionID、title、channel、累计 usage/cost、summary 引用等 |
| 历史内容 | `SessionMessageEntry {type,id,parentId,timestamp,message}`；当前 leaf 与 byId/fileEntries 索引在内存 | `messages` 行：ID、SessionID、role、JSON parts、model/provider、完成时间等 |
| 单次模型请求 | Agent 内存 context 和流式 assistant Message，最终 `message_end` 后追加 | 每个模型 step 先 INSERT 空 assistant，再按 callback 更新其 JSON parts |
| 工具调用与结果 | ToolCall 为 assistant content part；toolResult 单独一条消息 | ToolCall 为 assistant part；ToolResult 单独 INSERT 一条 tool message |
| 正在流式呈现 | `message_update` / 部分 Message 在内存 | `pendingState` 内存缓冲最新 Message、dirty、timer、baseline；可刷新为 SQLite 状态 |
| 读文件辅助记录 | 该普通 `read` 实现未另写独立读取账本 | 成功文本 `view` 会 upsert `read_files`，与 tool message 分开 |
| 控制与完成通知 | Agent loop / 队列与事件，不因每个事件新写一行 | activeRequests、队列、RunID 接纳信息、`RunComplete` pubsub 不等于新增 SQLite 运行行 |

Pi 的核心索引先修改内存，再调用持久追加；这些对象不是多份独立数据库记录。[Pi 默认目录](https://github.com/earendil-works/pi/blob/8ce69e9d2b171d173fe4b6b2b6256f1f4411e69d/packages/coding-agent/src/core/session-manager.ts#L589-L604)、[entry 包装和索引](https://github.com/earendil-works/pi/blob/8ce69e9d2b171d173fe4b6b2b6256f1f4411e69d/packages/coding-agent/src/core/session-manager.ts#L1191-L1213)。Crush Session 字段见 [Session](https://github.com/charmbracelet/crush/blob/76cc5c574e15072b15aaed0f4f843a5711fae0d9/internal/session/session.go#L51-L67)，缓冲状态见 [pendingState 与 baseline](https://github.com/charmbracelet/crush/blob/76cc5c574e15072b15aaed0f4f843a5711fae0d9/internal/message/message.go#L75-L138)，创建消息见 [Create](https://github.com/charmbracelet/crush/blob/76cc5c574e15072b15aaed0f4f843a5711fae0d9/internal/message/message.go#L191-L224)。持久主库配置见 [SQLite 连接](https://github.com/charmbracelet/crush/blob/76cc5c574e15072b15aaed0f4f843a5711fae0d9/internal/db/connect.go#L18-L27)；运行完成事件见 [Run defer](https://github.com/charmbracelet/crush/blob/76cc5c574e15072b15aaed0f4f843a5711fae0d9/internal/agent/agent.go#L792-L823)。

## 3. Pi 经典 CLI 的热路径

### 3.1 实际存储入口与逻辑条数

默认 SDK 构造的是 `new Agent` 和 `AgentSession`。Agent loop 在开始时为用户消息发出 `message_end`；模型文本／thinking／工具参数增量发出 `message_update`，完成模型消息时发出 `message_end`。AgentSession 只在对应 `message_end` 分支为 user、assistant、toolResult 等调用 `SessionManager.appendMessage`。因此 A 的两行依次是 user、assistant；B 的四行依次是 user、含 ToolCall 的 assistant、toolResult、最终 assistant。工具参数增量不会各自变成一行。[初始消息事件](https://github.com/earendil-works/pi/blob/8ce69e9d2b171d173fe4b6b2b6256f1f4411e69d/packages/agent/src/agent-loop.ts#L103-L126)、[模型流事件](https://github.com/earendil-works/pi/blob/8ce69e9d2b171d173fe4b6b2b6256f1f4411e69d/packages/agent/src/agent-loop.ts#L381-L469)、[持久事件处理](https://github.com/earendil-works/pi/blob/8ce69e9d2b171d173fe4b6b2b6256f1f4411e69d/packages/coding-agent/src/core/agent-session.ts#L1104-L1130)。

定义 `s` 为 prompt/tool 声明变化等产生的额外 system message 数，`e` 为扩展自定义消息、配置变化、其他明确追加的 entries，`w` 为成功 cache warming usage entries。逻辑行数：

```text
J = 1 + m + t + s + e + w
A: m=1,t=0,s=e=w=0 => J=2
B: m=2,t=1,s=e=w=0 => J=4
```

`s=0` 需要 transcript 的 prompt sections 没有变化；变化时可插入 SystemMessage，不能把所有真实会话都固定为 2／4 行。[prompt sections 比较](https://github.com/earendil-works/pi/blob/8ce69e9d2b171d173fe4b6b2b6256f1f4411e69d/packages/coding-agent/src/core/agent-session.ts#L1670-L1683)、[prompt 前的变更消息](https://github.com/earendil-works/pi/blob/8ce69e9d2b171d173fe4b6b2b6256f1f4411e69d/packages/coding-agent/src/core/agent-session.ts#L2032-L2042)。cache warming 由 SDK 挂到 session 请求，成功后另 `appendUsage("cache_warm",…)`；它有额外模型请求，也有额外 JSONL 行，不能暗中纳入场景中的 `m=1/2`。[warming 装配](https://github.com/earendil-works/pi/blob/8ce69e9d2b171d173fe4b6b2b6256f1f4411e69d/packages/coding-agent/src/core/sdk.ts#L396-L406)、[warming usage 追加](https://github.com/earendil-works/pi/blob/8ce69e9d2b171d173fe4b6b2b6256f1f4411e69d/packages/coding-agent/src/core/cache-warmer.ts#L330-L350)。

### 3.2 追加、读取与恢复界限

暖会话已经 `flushed=true` 时，每个 entry 调用一次 `appendFileSync(sessionFile, JSON.stringify(entry)+"\\n")`，没有读取历史 JSONL；上下文来自已装载的内存索引。源码没有在这条路径显式调用 fsync/fdatasync 或目录 sync。API 为同步只说明调用完成的等待方式，不证明掉电后必然保留。第一次聊天则使用 `openSync("wx")` 和逐 entry `writeFileSync` 写出 header 与所有此前 setup entries，不能套用暖会话追加 API 的次数。现在建立文件的触发点已经是第一条 user 或 assistant，不能说必须等首条 assistant 完成。[首聊建立与暖追加](https://github.com/earendil-works/pi/blob/8ce69e9d2b171d173fe4b6b2b6256f1f4411e69d/packages/coding-agent/src/core/session-manager.ts#L1162-L1189)。

异常边界也不同于事务提交：`_appendEntry` 先加入 fileEntries/byId、推进 leaf，再尝试追加。AgentSession 又先通知 extension/public listener 后持久化，因此 UI 观察到消息事件不能单独证明文件追加成功；失败路径需回到实际文件状态，不可把显示结果当恢复证据。[索引修改顺序](https://github.com/earendil-works/pi/blob/8ce69e9d2b171d173fe4b6b2b6256f1f4411e69d/packages/coding-agent/src/core/session-manager.ts#L1191-L1195)、[通知先于持久处理](https://github.com/earendil-works/pi/blob/8ce69e9d2b171d173fe4b6b2b6256f1f4411e69d/packages/coding-agent/src/core/agent-session.ts#L1104-L1129)。

这里的“0 内容热读”仅针对 JSONL。鉴权数据可能比较文件 revision，revision 未变可使用内存快照；变化会重新读取，OAuth 或配置命令又有自己的开销，不应合入每条消息固定次数。[auth 最新数据与缓存](https://github.com/earendil-works/pi/blob/8ce69e9d2b171d173fe4b6b2b6256f1f4411e69d/packages/coding-agent/src/core/auth-storage.ts#L401-L447)。普通 `read` 还会解析路径、检查访问、检测图片类型，最后读取目标文件；文本切片不产生新的 session 记录，返回值随一个 toolResult 追加。不能把它表述为整个 B 只有一次文件 syscall。[read 默认 operations](https://github.com/earendil-works/pi/blob/8ce69e9d2b171d173fe4b6b2b6256f1f4411e69d/packages/coding-agent/src/core/tools/read.ts#L35-L49)、[read execute](https://github.com/earendil-works/pi/blob/8ce69e9d2b171d173fe4b6b2b6256f1f4411e69d/packages/coding-agent/src/core/tools/read.ts#L81-L154)。

## 4. Crush 的热路径与可计算 SQL

### 4.1 调用分解

默认本地 `AppWorkspace.AgentRun → Coordinator.Run → sessionAgent.Run`；可选 server 入口不会自动参与。默认 local 分支与显式开关见 [root 初始化](https://github.com/charmbracelet/crush/blob/76cc5c574e15072b15aaed0f4f843a5711fae0d9/internal/cmd/root.go#L233-L270)。service 接收同一正常 `db.Queries`，本场景没有包裹整个 turn 的显式 BeginTx／Commit。[app 存储服务装配](https://github.com/charmbracelet/crush/blob/76cc5c574e15072b15aaed0f4f843a5711fae0d9/internal/app/app.go#L93-L115)。

| 时点 | 顶层 SQL | 每 turn／step 次数 | 代码入口 |
|---|---|---:|---|
| Coordinator 同步 channel | GetSessionByID | turn 1 SELECT | [syncSessionChannel](https://github.com/charmbracelet/crush/blob/76cc5c574e15072b15aaed0f4f843a5711fae0d9/internal/agent/coordinator.go#L440-L457) |
| Agent 装载当前会话与历史 | GetSessionByID、ListMessagesBySession 或 from-summary | turn 2 SELECT | [Run](https://github.com/charmbracelet/crush/blob/76cc5c574e15072b15aaed0f4f843a5711fae0d9/internal/agent/agent.go#L735-L759)、[getSessionMessages](https://github.com/charmbracelet/crush/blob/76cc5c574e15072b15aaed0f4f843a5711fae0d9/internal/agent/agent.go#L1792-L1815) |
| 用户消息 | CreateMessage | turn 1 INSERT | [createUserMessage](https://github.com/charmbracelet/crush/blob/76cc5c574e15072b15aaed0f4f843a5711fae0d9/internal/agent/agent.go#L1591-L1606) |
| PrepareStep 过滤禁用 MCP server | ListMCPDisabledServers | step 1 SELECT，即使无 MCP 工具 | [PrepareStep](https://github.com/charmbracelet/crush/blob/76cc5c574e15072b15aaed0f4f843a5711fae0d9/internal/agent/agent.go#L856-L868)、[过滤函数](https://github.com/charmbracelet/crush/blob/76cc5c574e15072b15aaed0f4f843a5711fae0d9/internal/agent/agent.go#L1717-L1744) |
| PrepareStep 建立 assistant | CreateMessage | step 1 INSERT | [assistant 创建](https://github.com/charmbracelet/crush/blob/76cc5c574e15072b15aaed0f4f843a5711fae0d9/internal/agent/agent.go#L918-L932) |
| 文本或工具状态 callback | UpdateMessage | 实际成功刷新数 U | [callbacks](https://github.com/charmbracelet/crush/blob/76cc5c574e15072b15aaed0f4f843a5711fae0d9/internal/agent/agent.go#L965-L1039)、[write](https://github.com/charmbracelet/crush/blob/76cc5c574e15072b15aaed0f4f843a5711fae0d9/internal/message/message.go#L430-L451) |
| 普通 tool result | CreateMessage | tool result 1 INSERT | [OnToolResult](https://github.com/charmbracelet/crush/blob/76cc5c574e15072b15aaed0f4f843a5711fae0d9/internal/agent/agent.go#L1021-L1039) |
| OnStepFinish usage | GetSessionByID、UpdateSession | step 1 SELECT、1 UPDATE | [OnStepFinish](https://github.com/charmbracelet/crush/blob/76cc5c574e15072b15aaed0f4f843a5711fae0d9/internal/agent/agent.go#L1076-L1093) |
| 成功普通文本 view | RecordFileRead upsert | 本场景 1 DML | [view 完成](https://github.com/charmbracelet/crush/blob/76cc5c574e15072b15aaed0f4f843a5711fae0d9/internal/agent/tools/view.go#L230-L259)、[SQL](https://github.com/charmbracelet/crush/blob/76cc5c574e15072b15aaed0f4f843a5711fae0d9/internal/db/sql/read_files.sql#L1-L11) |

channel 已相同则没有 SetChannel；改变 channel 另加一次 DML，失败会日志后继续。第二次模型 step 使用 fantasy 的内存消息链，并不重新执行一次全量历史 List；但 PrepareStep 的禁用 MCP 查询和 OnStepFinish 的 usage Get 仍各做一次。Session.Save 是一条 UPDATE RETURNING，没有隐含的新 SELECT。[Save](https://github.com/charmbracelet/crush/blob/76cc5c574e15072b15aaed0f4f843a5711fae0d9/internal/session/session.go#L196-L231)、[SQL 定义](https://github.com/charmbracelet/crush/blob/76cc5c574e15072b15aaed0f4f843a5711fae0d9/internal/db/sql/sessions.sql#L26-L55)。Message.Create 也是 INSERT RETURNING，Message.Update 一次刷新覆写该消息 parts JSON 与完成信息。[message SQL](https://github.com/charmbracelet/crush/blob/76cc5c574e15072b15aaed0f4f843a5711fae0d9/internal/db/sql/messages.sql#L22-L52)。

### 4.2 公式与 B 的额外读取登记

无 reasoning，定义：

- `m`：模型 steps；`t`：正常 tool results；`S`：实际发生的 ToolInputStart 结构回调数，本例典型为 1；
- `N=1+m+t`：新 messages 行数；
- `U=m+t+S+X`：实际 messages UPDATE 数。每个模型 step 完成写一次、每个完整 ToolCall 写一次、每个 ToolInputStart 写一次，其余增量实际刷新计入 `X`；
- `R_core=3+2m`：上述核心显式 SELECT 数；
- `W_core=N+m+U=1+3m+2t+S+X`：核心顶层 INSERT／UPDATE 数；
- 普通成功文本 view 的 `read_files` 登记是 `F=1`；channel 变化另加 `h\in\{0,1\}`，提交前 TUI 待登记文件路径数为 `f`，必要时另加。

```text
A: m=1,t=0,S=0
   N=2, U=1+X, R_core=5, W_core=4+X

B: m=2,t=1,S=1（典型工具开始回调）
   N=4, U=4+X, R_core=7, W_core=10+X
   工作目录内成功普通文本 view: W=11+X（含 F=1）
   总显式 SQL=18+X

若 B 没有 OnToolInputStart: S=0，W 和总 SQL 各减1。
有 channel 变化或提交前待登记文件: W 再加 h+f。
```

`RecordRead` 是单条 INSERT…ON CONFLICT UPDATE，没有先查 `GetFileRead`；后者属于另一个 LastReadTime 方法，本次普通 view 不调用。登记失败仅记录日志，工具结果仍可能成功，因此“读成功”不能证明读取辅助账本已持久化。[RecordRead 与 LastReadTime](https://github.com/charmbracelet/crush/blob/76cc5c574e15072b15aaed0f4f843a5711fae0d9/internal/filetracker/service.go#L37-L58)。TUI 在提交时还会遍历 `sessionFileReads` 做登记，即纯文本提示也可能由于此前状态产生 `f` 次；该路径不应假装为所有 UI 下都固定为零。[提交前登记](https://github.com/charmbracelet/crush/blob/76cc5c574e15072b15aaed0f4f843a5711fae0d9/internal/ui/model/ui.go#L5108-L5115)。

view 自身还会 stat、探测格式、读文本、通知 LSP 并等待诊断。目标文件实际系统调用与 LSP 子系统开销不由上述 SQL 公式确定，也不等于一次 tool result。[view 文件读取及 LSP](https://github.com/charmbracelet/crush/blob/76cc5c574e15072b15aaed0f4f843a5711fae0d9/internal/agent/tools/view.go#L164-L259)。

### 4.3 stream、flush、commit、sync 是不同边界

Message.Service 默认 debounce 为 33ms；普通文本 callback 的 Update 先存内存状态，合并后才执行 SQLite UPDATE。增加工具调用、工具调用 Finished 变化、message Finish、reasoning 结束会绕过定时合并并同步 flush；`OnToolInputStart`、`OnToolCall` 和每次 `OnStepFinish` 是 B 的结构写。不是“所有 chunks 都落库”，也不是“只等最终答复才落库”。[默认 debounce 合同](https://github.com/charmbracelet/crush/blob/76cc5c574e15072b15aaed0f4f843a5711fae0d9/internal/message/message.go#L16-L44)、[Update 与 timer](https://github.com/charmbracelet/crush/blob/76cc5c574e15072b15aaed0f4f843a5711fae0d9/internal/message/message.go#L246-L301)、[结构判定](https://github.com/charmbracelet/crush/blob/76cc5c574e15072b15aaed0f4f843a5711fae0d9/internal/message/message.go#L469-L501)。

33ms 是调度窗口，写入被锁、正在刷新、错误和新状态竞争会改变实际数与延迟；源码未证明 33ms 耐久 SLA。sync flush 在写期间出现 dirty 会循环补写；失败把 dirty 恢复，后台 timer 忽略返回错误。故用实际刷新变量 `X`，不写成 `ceil(stream_duration/33ms)`。[flush 状态与补写](https://github.com/charmbracelet/crush/blob/76cc5c574e15072b15aaed0f4f843a5711fae0d9/internal/message/message.go#L349-L424)。

Run 的 defer 调用 FlushAll，用 detached context 最多等待 5 秒，但 flush 失败只是日志，后续仍发布 RunComplete。成功路径的终态 Update 正常同步刷新，不能扩大为“每个 RunComplete 都证明所有消息已持久化”。pubsub 消息也不是额外 SQL 行；通知丢失不改变已写数据库。[Run 退出刷新](https://github.com/charmbracelet/crush/blob/76cc5c574e15072b15aaed0f4f843a5711fae0d9/internal/agent/agent.go#L778-L823)。

默认连接采用 WAL 与 synchronous=NORMAL，最多一个打开连接。上述热路径直接执行 SQL，无整个 turn 的显式事务；通常每个独立 DML 在 SQLite 的隐式事务中完成，trigger 与该语句共享原子边界。不能把 Run 退出的 FlushAll 当作这批 DML 的单个 COMMIT 或 fsync。[pragmas 与单连接](https://github.com/charmbracelet/crush/blob/76cc5c574e15072b15aaed0f4f843a5711fae0d9/internal/db/connect.go#L18-L27)、[连接限制](https://github.com/charmbracelet/crush/blob/76cc5c574e15072b15aaed0f4f843a5711fae0d9/internal/db/connect.go#L137-L142)。SQLite 官方说明自动事务通常在最后一个活动 statement 结束时提交；游标 finalize/reset 等影响完成边界。[SQLite transaction](https://www.sqlite.org/lang_transaction.html).因此源码可计算顶层成功写语句，不能直接断言同样数量的 OS 持久刷新。

WAL+NORMAL 通常不会在每个事务提交时同步 WAL；checkpoint／WAL 复用等才决定同步时点，系统或电源故障后可能丢失先前已提交的事务。FULL 增加提交后的 WAL 同步；当前配置并非 FULL。[SQLite synchronous](https://www.sqlite.org/pragma.html#pragma_synchronous).实际 checkpoint、VFS sync 和设备 I/O 要另测。

messages INSERT 会触发 Session.message_count 更新，messages／sessions UPDATE 还有 updated_at triggers。这些会增加 SQLite 内部行、页或 WAL 工作，却不能在“应用显式 SQL 次数”表中再机械加一个 SELECT／UPDATE 调用；多行与 recursive trigger 行为也不等于多次 commit。[初始 schema 的 triggers](https://github.com/charmbracelet/crush/blob/76cc5c574e15072b15aaed0f4f843a5711fae0d9/internal/db/migrations/20250424200609_initial.sql#L16-L22)、[messages triggers](https://github.com/charmbracelet/crush/blob/76cc5c574e15072b15aaed0f4f843a5711fae0d9/internal/db/migrations/20250424200609_initial.sql#L61-L80)。

## 5. 冷打开必须另列

### Pi

正常显式 `SessionManager.open(path)` 且不传 cwdOverride 时，先 bounded header scan 取得 cwd，再构造 Manager 完整扫描该 JSONL。大 header 超过发现扫描上限时回退全量加载并复用结果；传 cwdOverride 可省 header scan。因此“打开一次会话只读一次文件”不是严格 API 次数。[open](https://github.com/earendil-works/pi/blob/8ce69e9d2b171d173fe4b6b2b6256f1f4411e69d/packages/coding-agent/src/core/session-manager.ts#L1766-L1785)。

完整扫描打开 fd，循环 `readSync`，buffer 为 1MiB，然后 close。若有 `k` 次返回正数的 readSync，还会有一次返回 0 的 EOF read；短读与 header scan 使系统调用次数不能直接写成文件大小的固定公式。load 解析行，验证 header 后，非空无换行尾部还可能补一条 newline。当前有效 v3 且正常换行结束的文件无需这条修复写；旧版本迁移、缺少 thinking metadata 会另有重写／追加。[buffer 与扫描](https://github.com/earendil-works/pi/blob/8ce69e9d2b171d173fe4b6b2b6256f1f4411e69d/packages/coding-agent/src/core/session-manager.ts#L604-L669)、[重写和索引](https://github.com/earendil-works/pi/blob/8ce69e9d2b171d173fe4b6b2b6256f1f4411e69d/packages/coding-agent/src/core/session-manager.ts#L1085-L1130)、[缺失配置 metadata](https://github.com/earendil-works/pi/blob/8ce69e9d2b171d173fe4b6b2b6256f1f4411e69d/packages/coding-agent/src/core/sdk.ts#L424-L435)。

`continueRecent` 另做目录发现及候选选择，再加载选中文件，候选数量影响读取／stat。模型、auth、settings、skills 等启动加载也另计，不拿 JSONL 扫描数代替整个冷启动的 I/O。[continueRecent](https://github.com/earendil-works/pi/blob/8ce69e9d2b171d173fe4b6b2b6256f1f4411e69d/packages/coding-agent/src/core/session-manager.ts#L1793-L1800)。

### Crush

仅计已知 SessionID 的本地 TUI 会话内容装载：GetSession、ListBySession 的文件历史、ListSessionReadFiles、ListMessages 四条 SELECT。ListMessages 先 FlushAll；真正冷闲置打开无 pending 则不产生 message UPDATE。[loadSession](https://github.com/charmbracelet/crush/blob/76cc5c574e15072b15aaed0f4f843a5711fae0d9/internal/ui/model/session.go#L74-L103)、[本地 ListMessages](https://github.com/charmbracelet/crush/blob/76cc5c574e15072b15aaed0f4f843a5711fae0d9/internal/workspace/app_workspace.go#L91-L98)。这四条是该装载函数的持久读取，不是全程序冷启动总数。

继续最近会话另先 ListSessions，因此这个装载路径为五条 SELECT。DB Connect 的 pragma、Ping、migration 以及配置、MCP/LSP 等取决于数据库版本与环境，应另外记录，不指定通用常数。[initial session](https://github.com/charmbracelet/crush/blob/76cc5c574e15072b15aaed0f4f843a5711fae0d9/internal/ui/model/ui.go#L650-L664)、[DB 初始化与 migrations](https://github.com/charmbracelet/crush/blob/76cc5c574e15072b15aaed0f4f843a5711fae0d9/internal/db/connect.go#L150-L168)。新会话行创建、首次正常用户文本触发的标题模型及 title 写入同样不属于暖会话 A/B。

## 6. Pi v4 与 durable 的独立变体

上述 J=2/4 的对象与追加次数属于默认经典 v3 SessionManager。公开 AgentHarness 的会话存储有独立 v4 类型和 storage adapter，JSONL 发布路径含临时文件、append 与 rename；SQLite adapter 有自己的事务管理。它不是默认 SDK 经典 AgentSession 被动追加的同一实现，不用其 commit/adapter 成本修饰默认 A/B。[v4 JSONL 类型](https://github.com/earendil-works/pi/blob/8ce69e9d2b171d173fe4b6b2b6256f1f4411e69d/packages/agent/src/harness/session/jsonl/types.ts#L4-L44)、[v4 JSONL 发布](https://github.com/earendil-works/pi/blob/8ce69e9d2b171d173fe4b6b2b6256f1f4411e69d/packages/agent/src/harness/session/jsonl/io.ts#L66-L118)、[SQLite adapter](https://github.com/earendil-works/pi/blob/8ce69e9d2b171d173fe4b6b2b6256f1f4411e69d/packages/session-backends/sqlite-node/src/index.ts#L78-L104)。

`packages/durable` 又使用独立 main marker／sidecar 的 storage.commit；fsync 选项默认 false，启用时 sidecar 追加后做 flush，marker 另追加。其 storage write 集与事务边界需要按该主线实际调用者推导，不能用默认经典的 2/4 行数作为 durable commit、sidecar API 或 sync 次数。[durable options 与 commit](https://github.com/earendil-works/pi/blob/8ce69e9d2b171d173fe4b6b2b6256f1f4411e69d/packages/durable/src/storage/jsonl/storage.ts#L241-L305)。本稿未执行该主线，也不声称这些 adapter 对默认 CLI 已提供同样恢复或掉电保证。

## 7. 对本项目 I/O 比较的直接边界

本稿支持“完整消息追加与合并后的增量更新有不同成本形状”这一源码判断，不支持把两者排序为固定性能优劣。统一比较应至少并列逻辑对象变化、应用存储调用、数据库原子边界、同步策略；每 token 必须 durable commit 与展示 token 需要合并是不同设计选择。

若后续测量，固定相同 prompt、同工具、provider 回调形状与 chunk 划分，再分别收集：Pi append API／新增行；Crush sqlc query 次数、message flush 次数、trigger／page 工作；两者的 read/write syscall、SQLite VFS xSync、checkpoint 和设备指标。仅在记录了这些层次后才给物理 I/O 和延迟实测结论。运行指标应保留 X、S、w、f 等环境条件，避免把 UI 刷新或缓存 warming 当成主 Agent loop 的固定成本。

