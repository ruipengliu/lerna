# 切片 03 只读探索与 tracer ticket 建议

**正式采用：2026-10-03，前置02已按源码5548744/退出文档df2dbe5完整退出；授权决策代理完成最终接法复核，root据[最终交接](final-handoff.md)采用本记录。下文准备时的pin/pending状态保留为历史，当前接法及采用门槛以最终交接为准；没有宣称03已经实现。**


2026-10-03。扫描固定点：integration `8e7438e071727e25aa69e17fb81b2e53c416b78e`。03 尚未开始；02 的 01 PG 票为 claimed，其余票 ready-for-agent，runtime/adapter/Host 子进程恢复实现尚未合入。以下是准备事实和候选拆分，不是发布、批准或实现，也不是验收证据。03 必须等待 02 全部八票、审查、架构优化与退出证据完成。

依据：03 spec；CONTEXT.md 单一领域上下文；architecture capabilities/contracts/data-model/validation/verification；ADR-0002/0004/0005；02 decisions/admission-decisions/ticket-review 与进程恢复票；当前 contract、generator、TS facade、共同 harness。未修改仓库，未使用环境 smoke 或任何真实业务凭据。

## 1. 当前实际可复用的事实

1. 公共合同只支持准确 `1.0.0 / command / command.get`。生成值、JSON Schema、输入/输出 inventory、双方 codec、双向往返及 schema digest 黄金已存在。Go TS SDK 包是合同包，不是完整 Application SDK。
2. Go `ParseCommand` / TS `parseCommand` 验证通用信封，可为尚未登记方法计算准确 CommandDigest；成功不代表方法已支持或获准。公共 `DecodeCommand` / `decodeCommand` 具体返回 CommandGetRequest，不能直接承接 decide/get/cancel，也不能靠改通用 envelope 假称新方法完成。
3. ID/Revision/Time/OwnerRef/ObjectRef/ContentRef/Amount/SubjectBinding 可以复用其准确规则。Kind 是语法模式，新增 decision/snapshot 等 kind 不要求原 1.0.0 Kind 扩 enum；但新专用 ref 应在新版本固定 kind、必要 revision/准确绑定规则。
4. 共用 wire 仍是 UTF-8 1 MiB、容器深度 64、无 JSON number、重复 decoded key/非法 UTF-8/孤立 surrogate 拒绝。限额、byte_length、用量、修订、时间不得变为 JS number 或 Go 浮点；Scenario 的种子/计数若在线上设施传输也需明确闭合类型，不能绕过严格 wire。
5. 固定 CommandReceipt 与可变 Decision status/Proposal/usage 应分开。1.0.0 command.get progress 仅有 none/unavailable/task；它可以查询新写命令的原 CommandRef，却不能显示 Decision progress。不得往老 Progress union 添加 decision；decision_engine.get 是该事实的公开观察出口。
6. 原 CommandDigest 算法能绑定通用合法 envelope 与受信主体，包含新精确 version/profile/method、参数、owner、期限、expected_revision。Decision ID 和命令 ID 是不同身份：新的写命令键并不许可同一 decision_id 开第二份 Decision；需新方法自己的对象去重规则。
7. 02 决策已批准真实 PG/SQLite、短 Tx、固定 receipt 与必要 Job 同事务、Claim revision/epoch/lease、有界扫描等待、公平/配额、墓碑和旧版升级。接口由消费声明；当前尚无已合入的实际 Go 签名。不能引用 speculative Tx/Job 签名或把 Host durable_work.record 的 applied/text/project 业务当作 Decision 接纳实现。
8. 02 子进程票承诺 commit 前/后同步点、SIGKILL、独立连接观察、原键恢复与跨进程接替；仅证明进程崩溃，不是断电或生产故障域。03 可以借用这些基础设施，新增它自己的 Decision/目标业务阶段及观察，不复制一套泛型故障框架。
9. 共同 codec harness 已在 01 架构优化后采用两个 persistent runner，原 byte 通过有界设施 framing 传递，正反顺序均跑；并非旧版每例 452 次启动设计。03 扩展时保留真实 Go→TS/TS→Go typed roundtrip、逐例拒绝与诊断。

## 2. 需要处理的当前实现限制

| 位置 | 实际限制 | 03 所需的最小处理 |
|---|---|---|
| scripts/generate.mjs | source、inventory、输出均固定 1.0.0；生成一个 Values/Value surface | 在确有第二版本需求时做有限、明确的多来源生成；保留旧源/旧输出/旧黄金，不造通用版本插件平台。新准确版本命名、导出路径交 Astra。 |
| Go codec.go | Version、嵌入 SchemaJSON、资源 URL、缓存 key 为单一 named schema | 每个已支持版本需要真实 schema 选择/隔离缓存，避免同名 $defs 错编到旧 source。是现在第二版本的需求，不是预建所有未来版本。 |
| TS codec.ts | version、一个 Ajv schema、Values 映射固定单版 | 双版公开消费必须有明确 typed 导出/选择；不能 generic cast 成旧 CommandGetRequest，不能让旧 decode 行为扩大。 |
| commands.go / TS commands.ts | eligibility decoder 结果类型仅 command.get，另有 target/ref 相等规则 | 新专用 decoder 或真正命令联合只需覆盖目前四个实际方法；旧入口语义保留，不能把 get 的 ref 规则误用于 decide/cancel。 |
| generated inventory | 每方法输入要求顶层 const version/profile/method 和闭合 object payload；inventory 绑定 input/output | 三个方法每个都有闭合根，get 的正文也按统一 envelope 规则设计。metadata 只登记完整方法，不能因 Schema 存在广告半实现 profile。 |
| generator unionGo | oneOf 要求 named local refs、唯一必填 string discriminator；生成 private wrapper/New/As | Proposal 推进候选采用 named discriminated branches；不要用 optional 大结构或无 discriminator 的复杂嵌套联合。 |
| generator 审计 | 已支持 allOf/if/then/else；不支持任意关键词如 uniqueItems | 真需要新增 keyword 必须 fail-closed generator 支持及 refusal/golden 验证；去重 local_key、引用包含、绑定相等可做双方 codec 语义校验，不能默默忽略新 Schema。 |
| reachable digest | 严格 local $defs，完整递归输入/输出根摘要，安全整数 schema 元数据 | 新版本值定义应完整本地闭合或显式复制准确共同定义，不引用远端 URL。修改可达定义要改变摘要，旧黄金不变。 |
| TS package/Makefile/fixtures | package 1.0.0；fixture 固定目录/三份输入；Go runner 只生成现有 source | 明确 SDK 包版与支持 contract versions、各版目录与真实 roundtrip；不要只生成类型却遗忘 runner、脚本、fmt、CI 和拒绝未知版本的控制。 |

`ErrorCode` 1.0.0 是闭合枚举，未包含 docs 的 snapshot_unavailable/input_over_limit/provider_result_unknown/decision_mismatch/result_unavailable。新版本需定义这些是公共 error enum、Decision failure reason 或有类型结果差异；禁止给老 ErrorCode 添值或用 dependency_unavailable 抹平所有含义。规则基线不发真实模型，provider unknown 只作为明确设施模式/后续供应商的保留语义，不能伪造一次收费请求。

## 3. 04 前 Snapshot 与产物：必须明确的职责

文档要求 Snapshot 及 DecisionDispatch 归 Orchestrator，Proposal/Decision 归 decision_engine；04 才实现 Content 发布和 ContextCompiler，05 才有 Task owner。03 spec 直接要求 snapshot_ref 与可恢复必要内容，存在实施依赖空隙，不能靠先造 Task/Content 服务解决。

建议交 Astra 选取以下有界方案：

- **输入来自显式 fixture source**：在 test-only/Host 装配中准备不可变 Snapshot fixture 和准确材料字节，固定 ref/revision、goal/control 修订、组件/策略准确版本、processed_source_refs、限额与摘要。fixture source 是规则 module 当前真实消费者依赖，不是 Orchestrator 权威或 ContextCompiler；task_ref 仅是 fixture 身份，不能声称 Task 实际存在。
- 公开 decide 仍只消费契约要求的准确 snapshot_ref 等，不把 scenario/fault/rule选择字段塞入生产 payload。规则策略可作为已固定策略/配置版本，允许材料可推导分支；原 Decision 查询不再次读取“同名最新 fixture”。
- fixture source 验证准确 ref/hash/byte_length/当前测试允许用途，缺失/不匹配拒绝或保存明确失败；内容提供者不能拿 ContentRef 语法有效当作字节已存在或许可成立。
- Decision 接纳至少固定可重放所需的准确输入或耐久 fixture binding；父进程重新以同一 fixture 目录注入是有记录的测试前提，不能假装进程内 map 可恢复。推荐独立 durable fixture source 及字节摘要，以 restart 后真实读取证明来源保存。
- 必要 Proposal/产物字节/引用/完整 processed_source_refs 先耐久再 completed。小 fixture 可用同库准确字符串/字节及元数据共同提交、测试专用读回 seam，或明确 fixture publisher adapter；绝不把同库小字节方案写成实际 object-store Content.put 已完成。公共 DecisionGet 输出仅定义当前能准确读回的闭合产物，不制造缺字节的 published ContentRef。
- 04 以后替换 fixture source 的 adapter，而不是通过复制 decision_engine 的 Snapshot 表把它升格第二 Task 权威。04 必须通过公开输入确认规则组件收到真正 Snapshot；03 当前不验证目标提交前竞争/来源撤权全流程，那些属于04/05。

待 Astra 明确：新准确版本与 profile 名、ComponentRef/策略/InstallLock 的最小 fixture 表达、fixture source 的保存责任和读回 seam、cancel 控制依据（含 cancel 先于 decide 是否保存绑定关闭记录）、Decision/Command 双重幂等冲突、failure 码归属、Proposal bounds。以上是行为选择，不应提前由探索者定案。

## 4. Proposal 的确切闭合规则

- 文档并非“所有候选全互斥”：**requirement_delta 可伴随一种推进候选**；实质变化优先、其余不采纳由05的 Orchestrator 判断。03 可拒绝多个推进分支，并保留 delta 的准确来源；不能规则引擎擅自改变 goal_revision 或删除其他候选假称已采纳。
- actions/input_request/candidate_result/cannot_continue 至多一种；action 最多4项且互相独立。仅限制 maxItems=4 不能证明无依赖；用闭合候选结构禁止 arbitrary depends_on，同时核验本地键唯一、capability/binding 来自 Snapshot 给定集合、arguments_ref/来源不包含同提案尚未产生结果。依赖结果需后续 Decision。
- 不把任意合法 kind/ObjectRef 视作当前 Snapshot 提供的能力、参数或材料。仅依赖 schema 形状无法完成这些绑定，应经公开 decoder 与规则消费路径验证，双方同一夹具约束。
- Proposal 固定 decision_ref、snapshot_ref、goal/control revision、全部实际处理来源；对外展示集合不能替代 processed 集合。candidate_result 只是一份候选产物/证据/限制，不是 Task Result/Effect 或必要条件已通过。
- malformed proposal fixture 应通过公开 codec/Component 消费拒绝，不添加生产“注入错误提案”方法；规则组件从私有配置拿故障输出也必须将 Schema failure 记入原 Decision，不无限修复或创建新收费身份。

## 5. 独立模拟目标及故障机制的可验证出口

目标自身使用独立事实存储（独立 DB/schema/file identity，不能共享 Executor 表/Effect 值）。当前尚无 Executor；03 交付目标保证及独立 observer，不交付恢复 Executor，也不把 target 的 applied 观察当作系统 Effect 判断。

- 目标正常写入保存原请求 key、准确业务摘要、真实值/版本及接收次数；接收次数是目标合同明确定义的观察事实，不是被测内核私有调用次数。原键同内容/异内容、取消/查询的保证必须固定。
- 有查询模式能沿原 key 查询目标处理事实；`not_found` 与“可以断言永不会发生”严格分开。计划已耐久但尚未生效可以先查未找到，驱动受控 trigger 后独立观察实际写入；未来 Effect/may_apply_later 的裁决由07消费该证据。
- 幂等窗口内重传只产生原效果；窗口边界由统一可控 clock 持久配置，不因重启续期。过期后返回保证不足或实际允许重复这两种设施选择须记录，不把过期仍永远去重伪装成弱供应商；安全驱动必须不能据此宣称重传安全。
- 无查询模式的 **被测 target interface** 明确 unsupported；独立 privileged test observer 仍可检查真实目标状态，证明没有查询保证时普通消费者无法排除已发生。不能因测试 observer 存在就宣称供应商支持原查询。
- fault plan 不进入业务 decide/invoke payload；有种子、step/事件身份、有限次数、同步点、deadline 和耐久 cursor。提交前断开应无部分真实效果，提交后丢响应仍有实际值；重复/乱序/迟到的事件有明确原身份、可手动推进门闩、不会靠任意长 sleep。
- 子进程 SIGKILL 前后同步阶段需来自真正持久 commit/响应边界；重启恢复 plan 与原记录，不能只从 seed 重新安排一遍已执行 write。通道断开、响应丢失与目标未执行必须各自分开。
- 所有故障配正常对照和有限清理；记录种子/脚本/实际观察，测试数据只是假内容，不使用 `/workspace/.lerna-env` smoke 数据或真实账号。目标窗口/费用全部是测试保证，非供应商证明。

## 6. 候选 tracer tickets（仅准备，待02退出后复核/发布）

这里的编号不是已批准 issue，建议六张可独立观察行为的票；避免把 Schema、storage、worker 各拆成无出口水平层。new-version Schema/generator 支持应随首个真实 method tracer 交付，最终完整 metadata 仅在各路径实际完成后开放。

| 候选 | 可观察出口和主要验收 | 真正直接阻塞 |
|---|---|---|
| 01 · 原 Decision 的耐久规则提案查询 | decide 接纳→Job→确定性规则→Proposal/必要fixture产物耐久→get；正常 fixture、原命令重传、Decision ID 冲突、准确来源/修订/组件/限额；新增准确版本/双方类型/严格正反例随本条真实出口交付。无模型/Task成功。 | **02整片退出**；Astra 明确 fixture来源/新版本/错误码/身份决定。 |
| 02 · 有界候选与来源一致性 | 经公开 Component 执行 actions/input/candidate_result/cannot_continue 及 delta+单分支；互斥、max4、依赖/重复键、未提供能力/arguments_ref、processed来源反例；错误提案记原Decision失败，正常对照完成，无无限修复。 | 01（完整实际规则/查询及新版本路径）；不依赖 cancel。 |
| 03 · 取消、期限和不可复活的 Decision | cancel→固定停止事实→get 原结果/限制；控制依据错误、先后顺序、completed后取消不抹产物、重启后不复活；限额/截止拒绝有正常对照。不是Task控制。 | 01；cancel先于decide语义待Astra，不能默认需要02所有候选。 |
| 04 · 独立耐久目标与原键保证 | test target 正常 write/read/query，独立observer核实真实值/版本/接收事实；原键幂等窗口、同键异内容、重开记录、无query模式保证不足；明确目标与消费者存储隔离。 | **02整片退出**（当前依赖要求）；与01/02/03的Decision业务无数据依赖，可并行。 |
| 05 · 丢响应、迟到及有限故障计划 | 04正常对照→提交前后断开/丢响应、迟到生效、重复/乱序；已写独立可见、not_found后迟到、过期窗口/无query不假称未执行；计划种子和cursor可恢复。 | 04；不依赖Proposal候选或Decision cancel。 |
| 06 · 规则组件和模拟目标的子进程接替/重放 | 复用02进程设施，在Decision完成/产物发布和target提交/答复阶段SIGKILL；独立连接公开get/read恢复原身份、固定Proposal/真实效果，plan cursor不重复已完成步骤；正常脚本与相同种子有限重放。 | 02与03（此表候选票：完整规则候选+cancel状态）、05（目标故障机制）。只新增进程级业务恢复证据，不承担隐藏全片关闭。 |

精简依赖图：02整片→本表01/04；01→02/03；04→05；本表02+03+05→06。是否把候选02/03合并、是否两库都作为规则组件adapter，待02实现后的真实消费面积复核。默认云端规则组件PG足够，模拟目标独立耐久介质可选SQLite；03 spec不要求再把所有组件做双库。02已验证双库并不自动证明03的新领域表在两库均实现。

## 7. 退出证据与边界

根任务在所有03票真正完成后，核对六项原spec验收、公共完整方法 inventory、旧1.0.0拒绝/黄金保留、新精确版同版往返、持久 Proposal/产物、目标独立状态、明确query/idempotency保证、重启及可重演plan；另做代码/架构审查并记录commit、DB/驱动/生成器版本、命令、故障同步点、限制后才退出03。

当前没有03实现、没有新Schema版本、没有模拟target实验。verification.md 是历史文档交付的静态结果，它明说当时没有产品实现，不可拿它替代01/02/03 evidence。后续文档只需指明该记录日期范围与现有 progress，不能篡改旧检查结果。
