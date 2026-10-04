# 切片 03 实施前决定

**正式采用：2026-10-03，前置02已按源码5548744/退出文档df2dbe5完整退出；授权决策代理完成最终接法复核，root据[最终交接](final-handoff.md)采用本记录。下文准备时的pin/pending状态保留为历史，当前接法及采用门槛以最终交接为准；没有宣称03已经实现。**


2026-10-03。状态：仅准备，尚未发布票据、生成合同或执行 03 代码。03 必须等待 02 全部票据、审查、证据和切片退出完成；届时以实际 runtime 端口复核接入方式。本文不声称当前存在任何 03 runtime 签名或验收结果。

依据：`.scratch/lerna-03-deterministic-harness/spec.md`、`exploration.md`、根 AGENTS/CONTEXT、architecture contracts/capabilities/data-model、ADR-0002/0004/0005、实际 1.0.0 codec/query/generator，以及已批准 02 决定。以下是现有领域边界内的技术细化，不新增领域权威、不要求新 ADR。

## 1. 准确版本、类型与旧行为

决定采用准确合同 `1.1.0`，本片完成后支持 `command / command.get` 与 `decision_engine / decision_engine.decide|get|cancel`。这里只增加当前真实需要的第二版，不创建未来版本注册平台。SDK 包版本与线协议版本分开说明。

- 保留 `contract/schema/1.0.0`、旧机器清单、旧可达 Schema 摘要黄金和旧 codec 的接受/拒绝集合。不得在旧 ErrorCode、progress union、方法清单中追加新成员。旧共同 harness 继续原样运行。
- 新 Schema 放 `contract/schema/1.1.0`，共同值定义复制准确规则并在新版内闭合引用；新版本不依赖可变远程 `$ref`。输入/输出摘要仍覆盖每个根的全部可达 `$defs`，Schema 数字元数据与禁止 JSON number 的业务 wire 分别处理。
- 最小公开隔离采用 Go `contract/v1_1` 和 TS 包子路径 `./v1_1`；现有 Go `contract` 与 TS 主入口仍表示原 1.0.0。新生成物有独立目录/命名空间；Go 仍为根单 module。新 decoder 的返回类型只覆盖本版实际方法，不能将 decide 强转成旧 CommandGetRequest。
- 生成器明确遍历两个已知输入/输出配置即可。可以提取两版真正共用的私有严格 JSON / canonical 算法；每版 Schema 编译实例及缓存必须隔离，或以准确 Schema identity + root 为 key。不能按同名 `$defs` 共用错误缓存。TS 新 Schema 也递归冻结。
- 新版 CommandDigest 复用既有算法及完整绑定，不另造字段相似却不同的命令摘要。Decision 自身语义身份另见第 4 节。
- 共同 harness 增加新版真实 typed Go→TS / TS→Go 往返及拒绝例；保留原 byte、有限 runner 生命周期/请求期限及正反序运行，不改回每例开进程。新版字符串、整数、Unicode、重复键、未知字段、深度、大小同样严格。

**广告规则**：生成类型和开发中可直连的公开 Component 入口不代表该 profile 已可协商。前三条 Decision tracer 都验收后，由 root 在明确的整片集成步骤开放完整 decision_engine inventory；未完成 profile 不广告。完整 command profile 可在自身新版路径验收后开放。不要为广告时机给取消票或恢复票添加不存在的数据依赖；root 的全票退出检查承担完整清单、版本协商与全片证据。方法声明仍来自单一机器 inventory，不再维护一份硬编码运行时宣称清单。

**旧查询的实际兼容边界**：新版记录可能含 `decision_mismatch` 等旧 receipt 无法表示的原因。1.1.0 command.get 必须能准确查询这些固定原回执。1.0.0 reader 只提供能无损表示的原事实，progress 仍是 none；遇到不能表示的新记录，返回 reader error，沿现有 `ReadCommandFacts` 路径得到 `unavailable / dependency_unavailable`。不得改写原 reason、伪造旧 receipt、使用强转，或假设 typed error 会穿透当前 `GetCommand`。不为这一点新增旧版适配入口或修改旧 error 映射。这个 unavailable 表示旧视图无法提供原事实，绝不表示未接纳/未发生；调用方应使用准确 1.1.0 查询。此前讨论的“旧入口直接返回 version_unsupported”撤回，因为当前代码不具备该穿透语义。

## 2. 04 前的耐久输入和输出

Snapshot 的业务权威仍属于未来 Orchestrator；03 不实现第二个 Orchestrator、Content 服务、Task 或安装系统。正常规则组件实现放 `components/decision_engine`，依赖自己实际消费的小端口；host/cmd 只组合生命周期。fixture 实现与独立模拟目标放 conformance 测试设施，并遵守 Go internal 可见性；不要让顶层产品 adapter 反向导入 conformance/internal。

本片采用**明确命名的 durable fixture source / fixture publisher**：

1. 测试启动前由 fixture dispatcher 发布不可变 manifest、Snapshot 材料和假控制/使用依据，具有实际逻辑 owner、准确 ref/revision/hash/byte_length。task_ref 只指该 fixture 场景的逻辑身份，不能宣称真实 Task 已存在。组件配置固定实际规则版本和配置摘要；ComponentRef 的 InstallLock 引用指明是耐久 fixture lock，并能读回对应 manifest，不伪称生产安装已验证。
2. 默认规则组件用 PostgreSQL 实际存储。fixture source/publisher 可使用同一物理测试 PG 的独立 owner/schema 和独立事务，且只通过其读/发布 seam 访问。不得跨 owner 共事务或直接修改对方表。这样不必提前实现对象存储及其文件 fsync 协议，也不增加第二种 Decision adapter 的范围。模拟目标可独立使用 SQLite。
3. 当前最小消费需求为：按准确 ref 读不可变 Snapshot/材料、验证 fixture 的受信读取/用途依据、以原发布身份保存准确产物并读回核验。名称和 Go 签名由 02 退出后的实际消费者决定，不预建 Repository<T>、万能 Content API 或空端口。
4. Snapshot fixture 至少固定 task/snapshot identity、goal/control revision、准确材料、当前 requirement refs、明确能力与 binding 配对、可用参数/answer-schema refs、使用依据，以及能重放规则的输入。不得查询“同名最新 Snapshot”替换已接纳绑定。校验引用、字节和许可，不把 ContentRef 语法通过当作材料存在。
5. Decision 本地保存固定输入绑定和需要继续工作的责任；必要输出先通过 publisher 耐久保存并读回，然后在 Decision 自己的短事务中将 Proposal/引用/来源与 completed 共同保存。发布与 Decision 提交之间允许崩溃：使用稳定的原发布 key + 内容摘要恢复，同 key 异内容拒绝，重试同内容不能造第二身份。不声称跨 owner 原子提交。未引用的已发布 fixture 内容留给测试清理，不能据此报告 Decision 完成。
6. 重启必须从真正持久 fixture 来源再次读出字节，不由父进程重新构造内存 map 冒充恢复。04 用实际来源/publisher adapter 替换 fixture；05 再由真实 Task owner 提供 Snapshot/控制依据，组件不升级为第二份 Task 权威。

fixture 的读取许可/UseRef、能力费用、控制凭据均标明测试身份和有限范围。它们不能证明真实 Grant、供应商、Content 发布服务或计费账本已实现。

## 3. Proposal 的闭合形状和边界

Proposal 必须有准确 `decision_ref`、`snapshot_ref`、`goal_revision`、`control_revision`、`processed_source_refs`。DecisionRef 是稳定对象身份，查询 target 不携带猜测的当前 revision；SnapshotRef 必须固定准确 revision。Proposal 的顶层未知字段拒绝。

采用 `requirement_delta` 有界数组，加一个必填 `advance` 判别联合：`none | actions | input_request | candidate_result | cannot_continue`。每一分支有命名 Schema/类型，配合现有生成器的 oneOf/New/As 能力；不使用一个含五组 optional 字段的大对象。`none` 仅允许非空 delta，避免空提案被当作成功推进。delta 可以与**至多一种**推进分支并存；不要误读 spec 的“互斥”而拒绝合法组合。

本版具体 bounds：

| 字段/集合 | 本版上限与约束 |
| --- | --- |
| requirement_delta | 0–16 项；local_key 唯一；字段沿 contracts 表；replaces_ref 若存在必须匹配 Snapshot 当前条件的准确版本，同一原条件不可重复替换 |
| actions | 1–4 项；local_key 唯一；capability_ref + binding_ref 必须为 Snapshot 提供的准确配对；arguments_ref 与 source_refs 必须来自已存在的允许材料 |
| processed_source_refs / 每项 source_refs | 各最多 64，准确 ref 不重复；processed 集合完整覆盖实际处理来源，不得以较小披露集合代替 |
| input_request.preview_refs | 最多 16；question_ref、answer_schema_ref、purpose 必填；只请求澄清/材料，不提供授权确认分支 |
| candidate_result.artifact_refs | 1–16；逐条件 evidence 映射最多 64，条件 key 唯一，每项证据最多 16；必须引用准确已有条件与可读证据 |
| limitations / missing_requirements | 各最多 16 / 32；限制说明每项最多 1024 Unicode scalar；缺口引用来自准确输入范围 |
| cannot_continue 已完成成果 | 可选，最多 16 个准确引用；reason 必填且最多 1024 Unicode scalar |

local_key 沿用 ID 约束，purpose 和短文字至多 1024 Unicode scalar。总 wire 仍受 1 MiB 限制，这些局部上限不保证任意组合都能装入总上限。answer_schema_ref 指向 fixture 中可读的闭合 Schema，不接受内嵌任意 Schema/URL 执行。disclosed_source_refs 如有，作为独立有界集合，不能控制 processed 来源规则。

**依赖验证**不仅是 maxItems：闭合 actions 不接受 depends_on；arguments/source 不能引用同提案尚未产生的行动结果；能力、binding、目的及参数许可必须核对 Snapshot。即使伪造一个语法正确 ObjectRef，也不能绕过来源包含检查。有依赖的下一步必须由后续 Decision 决定。两语言 codec 检查可从正文判断的闭合形状、重复 local_key、互斥与引用结构；依赖真实 Snapshot 的包含/版本/用途核验由组件消费路径完成，不能让 codec 偷读数据库。

规则引擎只输出候选，不采纳 requirement_delta、不递增 Task goal_revision、不把 candidate_result 写成 Result。实质 delta 的采纳优先和其他候选不采纳由未来 Orchestrator 执行；03 对合法 delta+单分支保留原提案。错误提案以原 Decision 的明确失败结束，不做无限修复或创建新收费身份。故意错误输出仅来自私有 fixture 配置，经相同公开验证路径消费，不增加生产注入方法。

## 4. Command 与 Decision 两层身份

- Command 身份沿 02：准确 tenant/owner/command_id；摘要绑定完整受信主体、版本/profile/method/target/payload/expected_revision/accept_before，不含 trace/连接。先做当前认证授权，再查原命令；已有同摘要命令在截止过后仍返回固定原回执。同键异含义为 idempotency_conflict，不改原事实。
- Decision 身份为准确 tenant/Decision owner/decision_id。新增内部持久输入摘要 `SHA256("lerna-decision-input-1\n" + canonical(input))`；input 闭合包含 contract_version、profile、task_ref、snapshot_ref、component_ref、use_refs、limits、deadline、完整受信 SubjectBinding。canonical 复用既有无数字算法。它不包含 command_id、trace 或仅决定投递接纳时机的 accept_before。方法固定为本版 decide，无跨方法混用。
- 新 command_id + 同 Decision 身份 + 同输入摘要只为这次命令保存固定 accepted receipt，指向原 Decision，不创建第二 Job/Decision，不重跑已完成规则。新 command_id + 同 Decision 身份 + 不同输入摘要保存 rejected/decision_mismatch，原 Decision 不变。原 command_id 改参数则首先是 idempotency_conflict。
- 首次 decide 的成功接纳点是本 owner 短事务共同保存 Decision、固定 accepted receipt 与必要 Job。不能用 applied 暗示规则已完成。解析失败或无权请求不产生 Decision；可固定的已鉴权业务拒绝按 02 命令账本保存。
- completed 的成功点是可恢复 Proposal、必要产物与完整来源已经发布并绑定；get 返回该原事实。完成不改变原 accepted receipt，后续也不回 running。

所有新命令、原键重传、get/cancel 均以受信 principal 与 owner 路由鉴权；payload 不能覆盖主体，目录没有默认 owner fallback。组件可以允许多个主体访问同一 task fixture，但 Decision 输入包含实际主体，因此另一个主体不能借同 Decision ID 替换固定输入。

## 5. 状态、失败归属、有限资源

沿用 Decision 的 `accepted/running/waiting/completed/failed/cancelled`，以闭合状态变体规定必要/禁止字段。completed 必须有准确 Proposal/发布引用；failed 有有界失败事实；cancelled 有准确控制/停止依据；非 completed 不附上貌似可采纳的完整 Proposal。waiting 仅表示真实有限依赖等待，不虚构模型已发送。各终态不可复活，费用/清理等已有责任可以另行继续观察。

错误分层如下，不能把固定接纳回执随进展改写：

| 情形 | 归属 |
| --- | --- |
| 未知字段、非法整数、错误版本/方法、无权、接纳截止已过 | 通用 schema_invalid/version_unsupported/unsupported/forbidden/expired，沿准确方法的入口与命令接纳语义 |
| 命令同键异摘要 | 固定拒绝 idempotency_conflict |
| Decision 同身份异固定输入 | 新版固定拒绝 decision_mismatch |
| 原 Decision 已有准确取消墓碑，迟到匹配 decide | 新版固定拒绝 decision_cancelled；不创建工作 |
| 接纳后缺/坏 Snapshot、输入超限、提案非法、输出超限、规则步数耗尽、执行期限到达 | 原 Decision failure：snapshot_unavailable/input_over_limit/proposal_invalid/output_over_limit/rule_limit_exceeded/deadline_elapsed；原 accepted receipt 保留 |
| fixture 费用上限实际耗尽 | 原 Decision failure budget_exhausted；有可核对 fixture 用量，不能当作数据库容量拒绝 |
| get 查不到原 Decision | 闭合 result_unavailable 结果；不解释为从未发起/永不会出现 |
| 读取源当前不可用 | 独立 unavailable/dependency_unavailable；不伪造成 result_unavailable |

1.1.0 receipt ErrorCode 至少新增上述实际固定拒绝 decision_mismatch/decision_cancelled；DecisionFailure 是另一闭合枚举，不把全部异步失败塞进 receipt。`provider_result_unknown` 属于真实模型请求的合同语义，本规则基线物理模型请求数为零，不伪造收费请求/未知供应商结果，不提前宣称模型 adapter 支持。

最小 limits 包含十进制字符串 `max_input_bytes`、`max_output_bytes`、`max_rule_steps`、`max_actions` 和明确 fixture 单位的费用上限；前两项允许 0–1048576，steps 0–1024，actions 0–4。零上限可以形成可重复拒绝；不得用 0 隐含无限。金额沿准确 Amount/非浮点规则，单位必须标为 fixture，不冒充真实币种账单。计算用量记录实际读入/输出字节、规则步数与 fixture 计费依据；真实模型请求数为 0。输出还必须满足完整响应 1 MiB 上限。

payload.deadline 是原 Decision 的固定执行截止；envelope.accept_before 是本次命令接纳截止；context/timer 是一次调用资源期限，三者不得混同。准入和实际执行都核验使用资格、限额与截止；执行前已过期停止新增工作，执行中有限步数/时钟检查保证终止。到期不改原回执，也不能断言此前外部请求未发生（本规则基线没有外部模型）。fixture clock、费用、输入计数均有正常对照，不拿假值证明生产质量或容量。

## 6. 取消、先于 decide 的关闭记录

cancel payload 包含准确 decision_ref、task_ref、decision_input_digest，以及闭合 control_basis（issuer_owner、control_revision、valid_until、proof_ref）和有界 reason。控制由受信 fixture dispatcher 签发：核验 principal、issuer 与原 task_ref.owner 的关系、准确目标/输入摘要、期限和耐久 proof。输入自称 owner 或持有合法形状 ref 不构成授权。03 使用明确 fixture 控制核验 seam；未来 Task owner 替换签发方，不把夹具变成第二权威。

- 合法 cancel 可以先于 decide。短事务共同保存绑定原 Decision + task + input digest 的取消墓碑、固定 applied receipt 和必要停止工作；没有执行 Job 就不虚构需要运行的 Job。查询可以观察该 cancelled 关闭记录。
- 迟到 decide 若绑定相同，得到 decision_cancelled；若绑定不同，decision_mismatch；二者均不得启动或复活。不能以不存在 Decision 为理由省略未知目标取消的鉴权。
- 对活动 Decision，取消与 worker 完成竞争通过本 owner revision/Claim 资格及终态门禁裁决。已完成条件检查后仍必须在提交处核验；旧 worker 不得在取消后把它改 completed。已独立发布但未采纳的产物不自动成为 Proposal。
- 对 completed/failed/cancelled 的合法取消保留原终态与产物，可返回固定 applied 的无新增工作回执；不伪称撤销既有事实。更低控制修订拒绝 revision_changed；同控制修订同绑定幂等、同修订异绑定拒绝 decision_mismatch；新的停止控制须比 Snapshot/已保存控制更新。原命令重放优先按已保存原回执处理。
- cancel 是按单调 control_revision 应用控制的命令，不以通用 expected_revision 作为存在对象前提；否则无法支持 cancel-before-decide。该方法必须在新 Schema/前提说明中明确采用控制修订并禁止额外 expected_revision，不能让实现者猜测通用对象 CAS 的例外。decide 的创建/重传同样由双身份规则控制；get 只读。

这复用 ADR-0005 的“停止不抹事实、未知原身份仍须绑定关闭”原则，不交付 Executor TaskGate，也不改变 Task 控制权。

## 7. 独立目标与耐久故障设施

目标是测试设施，拥有独立 DB/schema/file identity，不能读取或复用被测 Decision/Executor 的效果表。普通 target seam 提供固定语义的 write/read/original-key query；独立 privileged observer 提供真实值、目标版本、接收计数和实际提交记录。接收计数是目标公开测试事实，不是内核私有函数调用次数。当前尚无 Executor，本片不得把目标观察值称为已由系统裁决的 Effect。

- write 原键绑定准确内容摘要。窗口内同键同内容只产生原效果，同键异内容明确 conflict；读回是目标自身提交事实。窗口起点/截止固定于首次耐久接收，不因重启或重传续期。
- 选择最小的过期语义：原键窗口过期后返回明确 guarantee_expired，不承诺重传安全，不返回假原成功。无需为了展示弱保证而故意制造第二次效果；但不能将永远去重伪装成已经验证短窗口供应商。已经耐久排队的迟到原请求仍可能生效，过期并不取消它。
- 有 query 模式只保证按原键查询当前已保存的处理事实。not_found 不等于未收到、永不会应用或可以安全换键。以耐久门闩暂停已接收但未生效的请求，先 query not_found，再释放门闩由独立 observer 证明实际迟到写入。
- 无 query 模式普通接口返回 unsupported；privileged observer 仍可证明真实写入，不能向普通消费者泄漏 observer 来补造供应商查询保证。目标读取当前值也不能替代原键查询。
- fault plan 是私有环境配置，不进入公开 decide 或未来 invoke 业务 payload。记录 seed、准确输入、step/event 身份、有限触发次数、阶段、截止、耐久 cursor；支持提交前断开、提交后响应丢失、延迟、重复、乱序和进程终止。每个 plan 有明确结束/清理条件，禁止无限等待与任意长 sleep。
- 可重复执行分为两种：同一持久 scenario 的重启继续原 cursor，不能从 seed 重发已完成步骤；新隔离 scenario 用同 seed/input 可复演相同逻辑。两者不能混淆。目标本地效果与属于该目标的 plan 阶段在可行时同事务记录；跨进程协调者用稳定 event key 和确认恢复，不声称跨 owner 全局原子事务。
- COMMIT 前杀进程应无部分效果，COMMIT 后/发响应前杀进程允许效果已存在且响应未知。独立连接或重启后的普通 query/read + observer 提供证据；通道断开不是目标没执行。子进程测试仅证明进程故障，不证明断电/异地灾备。
- 正常组件默认 PG、目标独立 SQLite 足够；不把 03 所有新表再次做双 adapter。仍真实测试所使用数据库的相关迁移、提交和恢复，不继承 02 的通过来冒充新领域表已经验证。

## 8. 六条 tracer 与直接依赖定案

| 票 | 独立可观察交付 | 直接阻塞 |
| --- | --- | --- |
| 01 原 Decision 耐久提案 | 新版 typed 合同随 decide→accepted+Job→确定性正常 Proposal/必要发布→get 交付；双身份冲突、准确固定输入与产物读回；真实 PG。先完成一个正常候选。 | 02 整片退出 |
| 02 有界候选与来源 | 全部推进分支、delta+单分支、max4、重复键/依赖/未提供能力及错误提案失败；有限正常对照。与本票新增语义有关的恢复由本票负责。 | 本片 01 |
| 03 取消与资源边界 | cancel-before-decide、错误控制绑定、完成/取消竞争、墓碑重启不复活、限额/截止和正常对照。 | 本片 01 |
| 04 独立目标原键保证 | 真实独立目标 write/read/query/observer；窗口、冲突、无 query、重新打开原记录。 | 02 整片退出 |
| 05 有限耐久故障计划 | 04 的目标加提交前后断开/响应丢失/迟到/重复乱序；原键证据、seed/cursor、有界结束。 | 本片 04 |
| 06 组件与目标进程接替 | 正常 Decision 发布/完成边界与目标提交/响应/plan 边界 SIGKILL，恢复原身份、产物与 cursor；相同种子的新 scenario 有限重演。 | 本片 01 + 05 |

从探索候选中**删除 06 对 02/03 的依赖**：06 验证正常 Decision 和 target 的进程恢复，不需要所有候选或取消业务。02/03 的特殊语义恢复归各自票，不把它们塞回 06 的最终关闭条件。root 等全部票 resolved，再完成准确 profile 广告、全片兼容/证据/代码与架构审查；这不是 06 的隐含依赖。

spec 六项验收映射：1→01+06；2→02；3→04+05；4→04+05；5→01+05+06（取消墓碑另由03证明）；6→每票自己的正常对照及有限结束。失败案例不得只有拒绝、缺允许完成的对照。旧1.0、新1.1共同合同、实际运行数据库、迁移、日志脱敏和可复演输入由 root 整合，不使用环境 smoke 代替产品验收。

## 9. 已解决的歧义与实施者仍需决定的局部细节

已明确处理的设计空隙：04/05 前无真实 Snapshot/Content/Task 权威→明确耐久 fixture source；“互斥候选”与 delta 共存→仅推进分支互斥；新 receipt 原因无法由旧版表达→旧视图沿既有 unavailable，新版完整读取；泛型 CAS 与先取消后创建→方法使用绑定输入的单调控制修订；未完整 profile→root 明确最后整合开放；进程恢复与全片关闭→分离责任，删除假依赖。这些不得在实现中静默回退成旧 enum 扩张、fixture 冒充真实服务或省略范围。

02 退出后，实施者依据实际代码决定短 Tx/Job/Claim 端口接法、SQL 字段和索引、适当 Go/TS 私有文件划分、source/publisher seam 的实际函数名、规则表实现及测试同步通道；这些不需要再问用户。先检查能否复用 02 的有限截止、持久重试、墓碑和进程设施，再加当前真正需要的最小函数。不得预先冻结不存在的接口、为全部后续版本/模型供应商造框架，或扩大为真实 Task/Content/Executor 功能。

## 10. 规则 Start 计量与耐久发布实施细化（2026-10-04）

root FULL read并采用授权Astra的窄决定
`/tmp/lerna-03-rule-start-accounting-decision.md`。这细化第5节既有有限规则用量，
不新增Task/计费领域或跨owner事务。首票的真实实现发现，Start提交后进程可能
尚未计算；不能将该窗口伪报为已完成规则步骤。

- 未发布1.1.0 Usage新增闭合必需字段`rule_starts`和
  `measurements_complete`。`rule_starts`记录耐久规则启动；步骤限额保守以启动数
  占用，费用依据准确fixture-rule/2组件、manifest、lock和Permission中固定的
  `durable_rule_start`及每Start一fixture单位。`rule_steps`只记录确认的实际规则
  执行。未确认窗口保留false；已确认字节/步骤是累计下界，不因接替归零。
  新计费基线费用准确；旧计费升级时已记录费用亦可能只为下界。模型请求为零。
- 同Claim/epoch无prepared只能授予一次计算资格。下一epoch可能补算时仍累计
  原starts/费用并保持先前未知观察；原限额耗尽则停止新增Start，不填假物理步骤。
- Publisher提供窄`PlanPublication`准确引用规划，授权检查和引用算法留在真实
  独立fixture owner；规划不发布正文。取得准确artifactRef后纯装配完整Proposal，
  先检查artifact+Proposal总输出及完整响应上限，同短Tx固定原keys、准确bytes/
  hashes/refs、来源和确认用量。然后Tx外按原键实际Publish及独立ReadPublished，
  最后短Finish再校验当前Claim、pool、trustedtime、权限期限和原deadline。
  prepared恢复只发布/读回/Finish，不重算或再收费。可恢复发布失败使用既有闭合
  waiting/dependency_unavailable，100ms后重试，最多8次且受原deadline限制。
- 不默改旧fixture-rule/1的fee-at-finish绑定，也不构造双计费执行器。
  owner0002在旧writer已退出/排空后分类升级：旧accepted无执行证据保零新starts/
  费用，固定failed/billing_basis_unsupported；旧running/waiting保留已记录用量
  下界并failed/usage_unavailable、不造start1/fee1。旧终态状态、Proposal和
  发布identity保留，不推断过去只启动一次。原input/accepted/manifest绑定不改。
  新调用旧binding先currentauth、再原key优先；新key固定unsupported无新Job。
  新正常执行必须新的准确fixture-rule/2 binding和新Decision。

这项细化随首票基本准入、真实旧writer升级、Start/prepared/限额恢复验收；特殊
候选、取消和完整SIGKILL矩阵仍分别由02/03/06交付，不能以它们尚未实施为漏记
首票启动或费用的理由。实际端口与恢复政策见components/decision_engine/README.md。
