# DeepSeek 暖会话的数据对象、逻辑追加与持久 I/O

固定源码：`deepseek-ai/deepseek-harness@639ed015397290b3745d163aafe02ffee4aa3f84`，manifest `0.2.0-rc.2`。本文件是静态源码取证，没有执行上游代码、安装依赖或测量系统调用／磁盘延迟。公式统计一条 Session 的本地持久路径；模型网络、SDK stdio、工具自身文件访问、启动装配和其他 Session 的工作另计。

## 1. 先给可用于比较的数值

**暖会话**在此有严格含义：同一 Host／Agent 仍驻留，已有已物化的当前 v4 日志，writer 已持锁且此前缓冲已排空，上一轮已结束；系统提示、工具目录、模型路由／默认参数及 request context 均未变化，已有 title，没有 runtime context 新快照。输入为一条普通文本。B 的普通工具同步返回一个短文本结果，不附加 `additionalContexts`、不提前结束 turn，也不调用其他工具／模型／后台工作。没有 compaction、Goal、Schedule、子任务、审批、retry、图片／附件或额外事件生产者。

| 路径 | A：用户文本 → 1 次模型 → 最终答复 | B：用户文本 → 模型工具调用 → 1 工具结果 → 模型最终答复 | 语义 checkpoint 调用 |
| --- | --- | --- | --- |
| AgentLoop 内核事件；无附加日志贡献 | 8 条事件 | 13 条事件 | 由装配决定；内核不自动把 append 变成 fsync |
| **出厂 `sdk-minimal` 原配置**；官方 DeepSeek adapter、日志扩展成功贡献并接受 | **9 条事件**：8 + 1 条日志接受水位 | **15 条事件**：13 + 2 条日志接受水位 | **A=0，B=0**；该完整 bundle 未挂 `session-checkpoint-policy` |
| 显式变体：`sdk-minimal` + checkpoint；关闭日志扩展 | 8 条事件 | 13 条事件 | **A=2，B=5**，即 `2M + T` |
| 同一 checkpoint 变体保留原日志扩展 | 9 条事件 | 15 条事件 | 仍是 A=2、B=5；水位事件不自带即时 checkpoint |

以上 9／15 还要求每个成功请求至少能把一个 pending event 装入默认 8MiB 日志字段。否则每请求接受水位可能没有追加，必须用下面的变量公式，不能把 9／15 写成所有 provider／任意历史长度的常数。接受水位是 adapter 在 HTTP 2xx 后调用本地扩展 accept 所记，不是独立远端 ACK。源码里的 `Session.append` 是同步内存接纳与发布；`flush` 是等待存储的耐久屏障；实际非空持久批次才调用文件写入和 `FileHandle.sync()`。**事件数、flush 数、文件写入批数和磁盘物理 I/O 数是四个不同量。**[S01][S02][S03][S04][S32][S33]

出厂 **`sdk`** 是另一条路径：它叠加 base + sdk-app，base 挂 checkpoint，JSONL 不指定 compression 时默认 `zstd`；sdk-app 禁用首 prompt 的标题模型。它不能与 `sdk-minimal` 的无 checkpoint、`compression:none` 拼成一个“DeepSeek 默认”。base 的 FTS 配置为 `openAt:never`，也不能默认给这两条简单会话加上 SQLite 搜索写入。[S05][S06][S07][S08]

## 2. 可运行组合及原配置差异

`sdk-minimal` 自身是完整 Cordis 树，JSON-RPC server、AgentLoop、官方 DeepSeek adapter、平台选定的一项 shell 工具及 JSONL provider 都在其中；它不是对 base 的小 patch。持久根为 `dshHomePath('sessions')`，显式 `compression:none`。原树挂了 `session-log-deepseek`、session-title fallback、llm-retry 和供 AgentLoop 唤醒的进程内 jobs service；它没有因此拥有模型可用的独立 jobs 工具或持久 jobs 表。A/B 没有 retry／后台工作／标题重写；已存在 title 时 fallback 不追加新 title。[S01][S08][S09]

可在具有该固定版本安装及相应凭据的环境使用以下组合；官方 minimal 文档说明 `DSH_HOME`、`DEEPSEEK_API_KEY` 和 SDK initialize 的模型选择要求。本文只列配置，不实际启动：[S37]

```sh
dsh --profile sdk-minimal --patch ./warm-io.patch.yml
```

`warm-io.patch.yml` 的内容：

```yaml
# 可选增强：在原 sdk-minimal 的纯文本 JSONL provider 上加语义 checkpoint。
- insert:
    - id: warm-io-checkpoint
      name: '@deepseek-ai/dsh-session-checkpoint-policy'

# 为比较纯内核路径，显式去掉默认官方日志请求贡献。
- id: session-log-deepseek
  config:
    enabled: false
```

CLI 按 bundle、profile、home、`--patch` 的顺序装配；用户层可以改变结果，所以实际运行须核对解析后的树，而不是只报 profile 名。此 overlay 不改存储格式、不加数据库、不改 AgentLoop，也不把其行为称为出厂配置。B 在 Linux/macOS 具体使用原 `bash` 工具，调用参数例如 `{"command":"cat ./fixture.txt"}`，fixture 为已有短文本，工具输出为 string 并渲染成 text block；工具自身文件读取及 PTY／进程 I/O 单列。它不是另装一个 read-file 插件；若实际具体工具另附 context／事件，则代入增量。[S10][S35]

## 3. 最小相关数据对象及热路径读入口

| 对象 | 实际保存及用途 | 暖 A/B 中的读取 |
| --- | --- | --- |
| SessionHeader | v4、SessionId、createdAt、cwd、lineage 等；文件首条 `type:session` 记录；不是普通 SessionEvent | 已在 live Session／handle 内存，不每轮读文件或重写 header |
| SessionEvent | `type + seq + time + data`，可带 `surfaceOp/sourceEventSeqs`；canonical append-only log | 追加前快照、验证并 deep-freeze，发布后 provider 将副本放入 live buffer |
| Inbox 投影 | `agent/inbox/spliced` 重建 next-turn／next-step 队列；用户输入先入队，再被 step claim | 从内存 projection 取队列；文本在入队事件和随后 user/message 中各出现一次，不是独立 Command／Task 表 |
| request/header、request/context | 模型 route、参数／工具声明及 request context 的日志折叠 | 从内存 log 折叠；同一暖 Agent 无变化时不每次重复追加。新 Agent／resume 或配置变化另计 |
| model surface／deriveMessages | 模型可见节点和由事件派生的 Message；与全部 canonical log 不同 | 从 Session log 内存及增量缓存取得；普通 append 不提升 contentGeneration，replacement／projection 才使缓存重建 |
| AssistantStreamAttempt | 进程内 attempt、chunk accumulator、assembler 和 live frames | 每个 chunk 进内存 accumulator／assembler并发送 live frame；完成时写一条 assistant/message，内含 canonical blocks、usage（若有）及 compact stream |
| tool/call、tool/result | 原 tool call 的 raw arguments／ID；结果 message、可选 meta／错误；result 引用 call 的 seq | 一次普通工具分别追加一条 call 和一条 result；结果进入下一个模型请求的 surface |
| JSONL write handle | materialized、cursor、单写者 lease、200ms live buffer、单飞 drain／mutation chain | 热路径不重扫日志正文；成功批写使用路径及 `handle.stat()` 取原长度，属于文件元数据读取 |

上述对象不会因名字类似而成为本项目的 Task／Operation／Effect／Grant 账本。暖路径主要是 Session 内存 log、投影与 buffer 的读写；同步验证、snapshot、deriveMessages 及 JSON 编码仍有 CPU／内存成本，不能因“不读盘”推断其为常数时间。[S02][S11][S12][S13][S14][S15][S34][S36][S38]

## 4. A/B 的逐条追加与变量公式

### A：一轮、一步、一次模型

在不计附加生产者时，顺序为：

1. `agent/inbox/spliced`：插入用户消息。
2. `turn/start`。
3. `agent/inbox/spliced`：claim 删除已取得的用户消息。空 next-step 不另造事件。
4. `step/start`。
5. `user/message`：将被 claim 的用户消息提交为模型可见输入。
6. `assistant/message`：成功 stream 的一次 settlement。
7. `step/end`。
8. `turn/end`。

系统提示、request header/context 若需更新在发送之前插入；官方日志接受事件通常在模型 HTTP 2xx、SSE chunk 消费之前插入。故它们不是上表遗漏的固定“每 token”事件。[S11][S12][S16][S17]

### B：一轮、两步、两次模型、一次普通工具

第一步与 A 的前五条相同；其 assistant/message 含 tool-call block。随后新增 `tool/call`、`tool/result`，结束 step 1；第二步新增 `step/start`、最终 `assistant/message`、`step/end`，最后只有一个 `turn/end`。空 next-step claim 不追加 splice，也不凭空追加第二条普通用户消息。相比 A 增加 **一次模型 settlement + 两个 step 边界 + tool call/result = 5 条**，因此 core 是 13。[S12][S16][S18]

令 `M` 为本场景模型请求数、`T` 为 top-level 普通工具数，`Qin` 为本轮非空 inbox 变更数、`Umain` 为原用户消息提交数。这里 `Qin=2`、`Umain=1`，每次模型对应一个 step，一轮有两个 turn 边界：

```text
E_core = Qin + 2(turn) + 2M(step) + Umain + M(assistant) + 2T(tool)
       = 5 + 3M + 2T
A: M=1,T=0 → 8
B: M=2,T=1 → 13

E_actual = E_core + H + C + S + Uextra + Iextra + P + L
```

- `H`：实际追加的 request/header；首次 Agent 请求为 initial／resume，后续 header 变化或新 series 才追加。
- `C`：request/context 的实际变化数。
- `S`：system/message 新增／replacement 数；准确系统文本不变且没有 series／surface 变化时为零。
- `Uextra/Iextra`：runtime context 或工具 additionalContexts 等引入的额外 user/message／inbox splice；本题设为零。
- `P`：标题、策略、插件等其他实际事件；已标题的暖 minimal A/B 为零，不能把未列明的插件写入猜成零。
- `L`：官方日志扩展成功接受次数，`0 ≤ L ≤ M`；原 `sdk-minimal` 小文本且 prefix 可装入时 `L=M`，关闭扩展／未采用该扩展的 provider 为零。

这些增量均通过实际 append 位点和日志类型计数，**不乘 token 数**。`AssistantStreamAccumulator` 将相邻 text／reasoning delta compact 成携带 `texts/dt` 的记录，仍能还原原 chunk 边界；一次最终事件同时带完整 message blocks 与 compact stream，因此日志字节和内存仍随文本／chunk 数增长，事件数固定不代表存储量固定。[S13][S14][S16][S17][S19]

## 5. checkpoint、flush、批写与 fsync 的公式

### 5.1 语义调用数

checkpoint 插件只在下列位点调用 `ctx.sessions.flush(session)`：每次 `agent/pre-step`，每次携 Session 的下游 `llm/stream`，每次有 Agent 且没有 parent 的 top-level `tools/execute`。准备的模型 adapter 最终也经过 llm/stream middleware。故在本题无 retry／nested／额外请求时：

```text
F_semantic = 2M + T
A = 2；B = 5
出厂 sdk-minimal 未装此插件：F_semantic = 0
```

没有“每轮最终 turn/end 后”的 checkpoint hook；故 A/B 最后 assistant/message、step/end、turn/end 的尾部仍可能处在 buffer。若调用方特意在收齐 turn 后 `await ctx.sessions.flush(session)`，另加 `F_end=1`：A=3、B=6 次显式 Session flush。它是这里的可选测量收尾，**不是 SDK 每次 prompt 或 settlement 的原默认保证**。SDK 的 prompt 把 followup 入队后就返回 messageId，未在该方法等待 Session flush。[S03][S20][S21][S31]

### 5.2 持久非空批数

每条 SessionEvent 经 `session/event` 路由进入 live buffer；首条未写事件启动最多 200ms 的故意等待计时。计时器、显式 session flush、service flush 或 close 均可以 drain。drain 在单飞链中一次取走当时全部 buffered events；写入期间新事件还会使同次 drain 循环产生下一批。空 buffer 或已经物化的 handle.flush **不再执行 file.sync**。[S04][S22][S23]

记 `K` 为本题事件最终全部耐久时成功执行的 **非空 persistBatch 次数**。正常暖当前格式、无 torn-tail／写失败，且没有其他事件进入同 writer：

```text
N_JSONL_event_rows = E_actual                  # header 已存在，本轮不重写
N_appendLines_calls = K
N_FileHandle_writeFile_calls = K
N_FileHandle_sync_calls = K                   # 文件同步调用；不是块设备 I/O 次数
N_body_readFile_calls_in_session_hot_path = 0 # 不含工具自己的读取／冷打开／显式历史读取
1 ≤ K ≤ E_actual                            # 计入最终 drain，已物化暖日志
```

该区间不预测具体批数：模型／工具延迟、200ms timer、flush 时点、并发追加及操作系统调度都会改变分批。出厂 minimal 没有出站前同步保证，在用户已收到最终结果的观察时点，可能尚有未耐久尾部；把后续 drain 算入后才适用“全部耐久”的 K。短时间内全部事件可合成一批，较长模型流可能让 prompt、日志接受水位和最终回复分成不同批。

checkpoint 变体若各语义屏障前的非空前缀均未被 timer 先写、期间没有额外分批，且最后显式收尾：A 可形成 **3 批**（输入／turn intake、模型 request prefix、最终尾部）；B 可形成 **6 批**（intake、request 1、call intent、result + step 1 end、request 2、最终尾部）。这是具体排程下的批次示例，不是 `flush=fsync` 的一般等式；timer 先写会让对应 flush 不新增同步，写入期间到达事件又可拆批。[S03][S04][S22]

### 5.3 文件同步的真实实现

暖 append 的具体路径是 `persistBatch → appendLines → open(path,'a') → handle.stat() → handle.writeFile(encoded batch) → handle.sync() → close()`。普通批次有明确的文件 sync。`writeFile` 可能内部使用多次系统写调用；OS cache、文件系统、设备缓存与文件同步语义不由这份源码决定，所以不报“物理磁盘写了 K 次”或“每批只一次 syscall”。[S24]

失败路径会增加 I/O：partial write／sync failure 后先关闭 append handle，再 `open('r+') + truncate(old size) + sync` 回滚；下次 drain 重试原缓冲。torn-tail 修复也有 truncate + sync。上述正常 K 公式不包含这些故障成本。[S24]

默认 zstd 变体将每个非空 batch 编码为一个独立压缩 frame；`compression:none` 则逐事件 JSON 行拼成一批。两种表示在正常 append 后都调用文件 sync，不能把压缩 frame 数当事件数，也不能把 zstd 误称另一套业务数据库。[S25]

## 6. 冷打开、初次物化与结束另外计数

| 路径 | 实际读取／写入 | 为何不算暖 A/B 固定热路径 |
| --- | --- | --- |
| Host/profile 启动 | 读取／装配 bundle、profile、home patch 等，并写 profile 的空根配置；加载模块、模型与工具 | 一次启动可以承载许多 prompt；此处不把每次启动配置 I/O 分摊成每轮固定数字 |
| 当前 v4 writer 冷 open | 根编码兼容性扫描（进程内 memo）、目录／generation 查找、writer lease、metadata stat、稳定整日志读取／解析、准备 primed events | file read 是历史长度相关的恢复成本，不是每一步把全日志重读一次 |
| handle 第一次 read | write-open 已持有 primed 解析结果，首次 read 可以直接切其 events；Session 由事件重建 surface／inbox等投影 | 后续显式存储 read 与历史查询另计，不能推广为所有读入口零磁盘读 |
| 冷 Agent 后第一模型 | request/header 追加 `resume`，request/context／system text 也可能变化 | 不能把“文件已存在但 Agent 新建”代入上面 H=C=S=0 的暖 Agent 常数 |
| 新 Session 首次物化 | header + 初批；POSIX mkdir／目录 sync、temp write+sync、link no-clobber 发布、父目录 sync；之后才成为 durable artifact | create 本身可只存在进程内；首次物化比已存在文件的 append 多出目录和发布成本 |
| 旧格式／torn tail | migration／generation 发布或首次变更前 truncate／rewrite recovered events；模型形状 repair 还可能追加修复事件 | 本题明确当前 v4、干净日志；不能给所有冷打开报同一个固定 I/O 数 |
| SDK shutdown／Session dispose | root lifecycle 的 handle.close drain，释放 writer lock；SDK transport.flush 是输出流冲刷 | shutdown 后尾部可被持久 drain；stdio flush 不等于 Session fsync，关闭不是每 prompt 的默认 checkpoint |

write-open 先 claim 单写者、取得 lease，再取 stored log；当前干净日志的 readStable 路径使用 stat／整文件读取／stat 检查前后身份。发现重叠变化时只重试一次，第二次仍变化则取该次读取前已提交长度的前缀，而非无限重读。目录查找也不止一次，因此此处列入口而不把 cold open 写成“一次物理读”。已经物化的 handle flush 是对既有 durable appends 的等待，空 session 的首次 flush 则会物化 header。[S22][S26][S27][S28][S29][S30]

## 7. 对本项目最小装配的含义

这条简单路径把会话内容、turn／step 边界、工具调用和消息结果放在一份日志中，另用内存投影与有界 buffer，不要求每个 chunk 一张表／一次提交。checkpoint 只在费用或副作用入口前同步可恢复前缀，这是有用的提交位置选择；其原配置是否挂载必须显式比较。

它没有在 A/B 中提供本项目独立 Task 完成条件、用途许可消费、Effect 持续核对或事务 jobs 的等价对象。需要这些行为时，不能仅因 8／13 或 9／15 较小就删掉其责任；反过来也不应为了记录 provenance 或呈现 stream，把本项目每个内部检查、token、来源位置都拆成服务、表和持久作业。可合并的诊断、准确正文单份保存和易失 live buffer 应与真正需要耐久的准入／外部发送责任分开。

## 固定源码入口

[S01]: https://github.com/deepseek-ai/deepseek-harness/blob/639ed015397290b3745d163aafe02ffee4aa3f84/packages/bundle/sdk-minimal/cordis.patch.yml#L1-L158
[S02]: https://github.com/deepseek-ai/deepseek-harness/blob/639ed015397290b3745d163aafe02ffee4aa3f84/packages/core/session/src/index.ts#L722-L766
[S03]: https://github.com/deepseek-ai/deepseek-harness/blob/639ed015397290b3745d163aafe02ffee4aa3f84/packages/session/session-checkpoint-policy/src/index.ts#L20-L82
[S04]: https://github.com/deepseek-ai/deepseek-harness/blob/639ed015397290b3745d163aafe02ffee4aa3f84/packages/session/session-persistence-jsonl/src/storage.ts#L274-L338
[S05]: https://github.com/deepseek-ai/deepseek-harness/blob/639ed015397290b3745d163aafe02ffee4aa3f84/packages/bundle/base/cordis.patch.yml#L130-L153
[S06]: https://github.com/deepseek-ai/deepseek-harness/blob/639ed015397290b3745d163aafe02ffee4aa3f84/packages/bundle/base/cordis.patch.yml#L412-L414
[S07]: https://github.com/deepseek-ai/deepseek-harness/blob/639ed015397290b3745d163aafe02ffee4aa3f84/packages/bundle/sdk-app/cordis.patch.yml#L1-L22
[S08]: https://github.com/deepseek-ai/deepseek-harness/blob/639ed015397290b3745d163aafe02ffee4aa3f84/packages/session/session-persistence-jsonl/src/index.ts#L68-L87
[S09]: https://github.com/deepseek-ai/deepseek-harness/blob/639ed015397290b3745d163aafe02ffee4aa3f84/packages/session/session-title/src/index.ts#L797-L830
[S10]: https://github.com/deepseek-ai/deepseek-harness/blob/639ed015397290b3745d163aafe02ffee4aa3f84/apps/cli/src/profile-boot.ts#L175-L205
[S11]: https://github.com/deepseek-ai/deepseek-harness/blob/639ed015397290b3745d163aafe02ffee4aa3f84/packages/core/agent-loop/src/inbox.ts#L109-L122
[S12]: https://github.com/deepseek-ai/deepseek-harness/blob/639ed015397290b3745d163aafe02ffee4aa3f84/packages/core/agent-loop/src/agent.ts#L296-L393
[S13]: https://github.com/deepseek-ai/deepseek-harness/blob/639ed015397290b3745d163aafe02ffee4aa3f84/packages/core/agent-loop/src/assistant-stream.ts#L48-L114
[S14]: https://github.com/deepseek-ai/deepseek-harness/blob/639ed015397290b3745d163aafe02ffee4aa3f84/packages/llm/llm/src/assistant-stream.ts#L99-L231
[S15]: https://github.com/deepseek-ai/deepseek-harness/blob/639ed015397290b3745d163aafe02ffee4aa3f84/packages/core/session/src/index.ts#L853-L880
[S16]: https://github.com/deepseek-ai/deepseek-harness/blob/639ed015397290b3745d163aafe02ffee4aa3f84/packages/core/agent-loop/src/agent.ts#L398-L538
[S17]: https://github.com/deepseek-ai/deepseek-harness/blob/639ed015397290b3745d163aafe02ffee4aa3f84/packages/session/session-log-deepseek/src/index.ts#L185-L240
[S18]: https://github.com/deepseek-ai/deepseek-harness/blob/639ed015397290b3745d163aafe02ffee4aa3f84/packages/core/agent-loop/src/tool-calls.ts#L147-L176
[S19]: https://github.com/deepseek-ai/deepseek-harness/blob/639ed015397290b3745d163aafe02ffee4aa3f84/packages/core/agent-loop/src/agent.ts#L598-L686
[S20]: https://github.com/deepseek-ai/deepseek-harness/blob/639ed015397290b3745d163aafe02ffee4aa3f84/packages/sdk/server/src/server.ts#L178-L194
[S21]: https://github.com/deepseek-ai/deepseek-harness/blob/639ed015397290b3745d163aafe02ffee4aa3f84/packages/llm/llm/src/index.ts#L964-L980
[S22]: https://github.com/deepseek-ai/deepseek-harness/blob/639ed015397290b3745d163aafe02ffee4aa3f84/packages/session/session-persistence-jsonl/src/storage.ts#L198-L259
[S23]: https://github.com/deepseek-ai/deepseek-harness/blob/639ed015397290b3745d163aafe02ffee4aa3f84/packages/session/session-persistence-jsonl/src/storage.ts#L508-L553
[S24]: https://github.com/deepseek-ai/deepseek-harness/blob/639ed015397290b3745d163aafe02ffee4aa3f84/packages/session/session-persistence-jsonl/src/index.ts#L1324-L1373
[S25]: https://github.com/deepseek-ai/deepseek-harness/blob/639ed015397290b3745d163aafe02ffee4aa3f84/packages/session/session-persistence-jsonl/src/index.ts#L1284-L1304
[S26]: https://github.com/deepseek-ai/deepseek-harness/blob/639ed015397290b3745d163aafe02ffee4aa3f84/packages/session/session-persistence-jsonl/src/index.ts#L342-L399
[S27]: https://github.com/deepseek-ai/deepseek-harness/blob/639ed015397290b3745d163aafe02ffee4aa3f84/packages/session/session-persistence-jsonl/src/index.ts#L1195-L1234
[S28]: https://github.com/deepseek-ai/deepseek-harness/blob/639ed015397290b3745d163aafe02ffee4aa3f84/packages/sdk/server/src/index.ts#L40-L90
[S29]: https://github.com/deepseek-ai/deepseek-harness/blob/639ed015397290b3745d163aafe02ffee4aa3f84/packages/session/session-persistence-jsonl/src/generation.ts#L247-L282
[S30]: https://github.com/deepseek-ai/deepseek-harness/blob/639ed015397290b3745d163aafe02ffee4aa3f84/packages/session/session-persistence-jsonl/src/storage.ts#L119-L175
[S31]: https://github.com/deepseek-ai/deepseek-harness/blob/639ed015397290b3745d163aafe02ffee4aa3f84/packages/llm/llm/src/index.ts#L1135-L1149
[S32]: https://github.com/deepseek-ai/deepseek-harness/blob/639ed015397290b3745d163aafe02ffee4aa3f84/packages/llm/llm-deepseek/src/adapter.ts#L105-L148
[S33]: https://github.com/deepseek-ai/deepseek-harness/blob/639ed015397290b3745d163aafe02ffee4aa3f84/packages/session/session-log-deepseek/src/index.ts#L38-L53
[S34]: https://github.com/deepseek-ai/deepseek-harness/blob/639ed015397290b3745d163aafe02ffee4aa3f84/packages/session/session-persistence-jsonl/src/format.ts#L79-L94
[S35]: https://github.com/deepseek-ai/deepseek-harness/blob/639ed015397290b3745d163aafe02ffee4aa3f84/packages/shell/tool-bash-persistent/src/index.ts#L413-L437
[S36]: https://github.com/deepseek-ai/deepseek-harness/blob/639ed015397290b3745d163aafe02ffee4aa3f84/packages/core/session/src/surface.ts#L568-L583
[S37]: https://github.com/deepseek-ai/deepseek-harness/blob/639ed015397290b3745d163aafe02ffee4aa3f84/packages/bundle/sdk-minimal/README.md#L25-L41
[S38]: https://github.com/deepseek-ai/deepseek-harness/blob/639ed015397290b3745d163aafe02ffee4aa3f84/packages/core/session/src/types.ts#L89-L130
