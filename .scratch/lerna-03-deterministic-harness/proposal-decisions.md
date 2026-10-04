**正式采用：2026-10-04，root按用户授权全文读并采用票02规则版本与prepared兼容。01九AC已resolved，最终源696ac49846105a16f33e5de86dc621a3858651b2、交付d806a92、整合5307702，已push检查点c9de1ba；下文候选/pending状态保留为历史，实际端口以[ticket01 API](ticket-01-api-handoff.md)为准。准确CI37174778053已按head c9de1baff7f481c2a9e4dde3c7873af43151819b核验success，见[CI记录](ci-verification.md)；本记录采用技术接法，不表示该后票已经实现或整片退出。**

# 03 ticket02：规则/配置与 prepared 的最小兼容决定

2026-10-04；只读条件准备。依据 root `issues/02-bounded-proposals.md`、`decisions.md`、`/tmp/lerna-03-ticket-02-proposal-exploration.md`，以及 main WT 固定候选 `41cbf7c9af2b9027ca6e81ddccf2ab06836e3810` 的 worker/ports/service、fixture Source/World 和 PG Record 存储（均以 git show 读取）。当前 01 仍未 resolved，机械 Open/Close 正在修改；本稿不采用 moving 工作区为业务退出证据。

**正式 ticket02 采用/实施硬门槛：01 真实 published/resolved 的最终 SHA、退出证据和实际 Source/Permission/Prepared/API handoff，再针对本文作 delta 复核。** 本轮仅写本文件，未改 repo/WT、运行数据库/测试/build/进程/服务或读取凭据；不宣布 02 claim/完成、03 whole exit 或完整 profile 广告。

## 1. 实际问题与推荐

候选源码 `Source.Seed` 固定 `ArtifactDigest=SHA256([]byte(RuleVersion))`、`ConfigDigest=SHA256([]byte(Snapshot.Rule))`；manifest 固定整个 Snapshot 和计费依据，lock 固定 ComponentRef 与 manifest。service Decide 的两次门禁与 worker Start 只认 `/2 + durable_rule_start`；calculate 对 `/2` 实际只产生 candidate_result，其他 Rule 字符串已具有明确 `proposal_invalid` 语义。Claim/Start 又要求记录的完整 ComponentRef 等于 cfg.Component。

**推荐新有限分支使用明确 `fixture-rule/3`，保留 `fixture-rule/2` 原实现语义。** 这是内部 fixture 规则版本，不是新增 wire 版本，也不是第三个 provider/策略注册系统。不得在同 `/2` ArtifactDigest 下把原未知 Rule 标签重新解释为 actions/none/input_request 等合法规则，不能把旧配置值换个名称就当原输入没有变化。

| 固定规则 | 执行与计费含义 |
| --- | --- |
| `/2` + `candidate_result` | 保留原有限 candidate 计算、原发布格式/键规则、原 durable-start fixture fee |
| `/2` + 其他原标签 | 保留原接纳/启动后 proposal_invalid 路径及真实已占用费用/测量；不升级成新增合法分支 |
| `/3` + 有文档的有限 case | 新五种 advance、delta 组合和故意错误候选；同一当前验证/预算/发布路径 |
| `/3` + 未定义 case | 来源/版本资格合格并接纳后，以原 Decision 的 proposal_invalid 有界结束；不能 fallback 到默认 candidate |
| 旧 `/1` / 旧 fee-at-finish | 保留 01 实际 legacy 退役/usage_unavailable 等处理，不因支持 `/3` 恢复旧计费执行器 |

新 `/3` 可继续明确采用每个 durable rule start 收取 1 `fixture` 的相同收费**规则**，但它是新 manifest 自己声明的依据，不改写 `/2` 的 fee/limits。一次有界 case evaluation 的逻辑 step 计量必须写清；durable start、已完成规则 step、实际已读字节和 CPU 运行不混同，物理模型请求仍 0。失败候选仍保留实际 start/读取/已生成输出观察，不伪造零费用。

## 2. 配置绑定和最小实际代码接法

首选继续使用有界 `Snapshot.Rule` 字符串作为准确 case 配置，`ConfigDigest` 仍 hash 其原 UTF-8 字节；`ArtifactDigest` 则 hash 准确 `/3` 字节。每个 case 的构造算法和故障变体由 `/3` 固定代码定义，输入引用/值来自已耐久 manifest 中的完整 Snapshot 与准确材料。case 名不同意味着不同配置摘要，不用 substring/prefix/别名猜版本。

新增 Snapshot/lock/manifest/Permission 必须有新的准确身份或合法新 revision；不得 reseed 原 immutable object/permission/Decision。旧候选重传查原固定回执，不能因新默认配置改成 rejected 或重新收费；当前认证仍先于读取。决定换组件/配置是新 Decision 输入，不能复用原 ID 绕过 input_digest。

只为实际消费增加小的明确 switch：准入与 Start 对支持的精确 RuleVersion/fee tuple 使用一致规则；Source.billingBasis、Seed、ReadFixtureLock、component/permission/manifest 校验同步支持 `/2` 和 `/3` 的准确组合。calculate 按已核对版本分支；不能只放开 service 的字符串检查而让 Source 或恢复路径继续暗拒 `/3`。没有 Version/StrategyRegistry、动态工厂、插件加载或任意规则解释器。

固定错误 case 必须构造错误 Proposal（必要时是有限 raw bytes）后进入与正常 `/3` 相同的 codec 和 Snapshot/来源/用途验证，不能直接返回预设 failure 冒充验证。typed 结构无法表达的未知字段可由准确私有 case 产生，仍过公共 raw decoder；不新增生产 decide 的 raw Proposal/故障注入参数。

若实际 case 需要额外配置参数，先用已绑定 Snapshot 的准确材料表达；若它们本质上决定规则配置，则用小的闭合不可变配置字节/ContentRef 并明确其新摘要规则。不能 hash 一个标签，却让未绑定的 parent callback、内存 map、可变环境或重开后重建数据决定其含义。本票不为尚未出现的参数预建配置平台。

## 3. prepared：原格式精确读，新格式明确写

候选 `Prepared` 是单 artifact 的具体结构；摘要为 `SHA256("lerna-decision-prepared-1\n" + json.Marshal(p with Digest=""))`，其中字段顺序、字段名、nil/空数组等均影响已有摘要。`Validate/publishPrepared/finish` 也假定总有一个 artifact。不能为 actions/delta-only 捏造无业务意义的空 artifact，更不能原地改此结构后重算旧 Digest。

**首选最小双读方案：原 Record.prepared / Prepared v1 原字段与摘要算法保留；增加一个明确独立的新内部 handoff，例如 `prepared_v2,omitempty`。** 新类型保存有界 `artifacts[0..16]`（各 key/ref/bytes）、必需的 Proposal key/ref/bytes/value、Sources、InputDigest、StartSequence 与新摘要域 `lerna-decision-prepared-2\n`。具体 Go 名称可按最终源码调整；只需这两个实际格式，不建格式注册框架。

每条记录最多一种 prepared；双格式并存、未知格式、摘要/原输入/StartSequence 不符均明确不可消费，不猜旧/新。新 `/3` 写新格式；旧 `/2` 原有和新产生的工作继续原格式。真正无需 artifact 的 advance 使用空列表；candidate_result 至少有实际成果，cannot_continue 的可选成果按真实生成情况填写。完成 ArtifactRefs 来源于实际 handoff，不固定塞单个值。

v1 恢复使用原 digest、准确 bytes/key/refs/Proposal/Sources 和原版本验证含义；不把旧 v1 normalize 后写成 v2、不套新 case 解释重算旧输出。v2 也先完成本票全部语义检查与有界计划，再同 owner Tx 保存 handoff，之后才跨 owner publication/readback。读取已存 prepared 恢复只发原发布身份和字节，不能新增 rule start/fee；真实必要读取另按既有测量含义处理，不伪称物理 I/O 为零。

两格式均保留当前身份/权限/原绝对截止、Claim/控制和完整 immutable 绑定检查；“兼容旧”不意味着永久许可。正文/Proposal 总生成字节按实际列表与 Proposal 有界累计，旧格式保持原口径；发布重传流量不冒称又生成了输出。部分 artifact 已发、Proposal 尚未完成时重开，继续原列表/key，不从第一步重算。

## 4. 迁移与同 scope 的实际界限

当前 PG 的 Record 保存在 body bytea JSON，columns/index/ledger 无需因多一个可选内部字段自动改变。上述精确双读且旧记录零重写的方案，**默认不要求 SQL migration**；必须用最终 published01 writer 留下的真实 accepted/running/prepared/completed/failed 数据验证，而非仅手写新 JSON。尤其 prepared 测试要保留原 writer 实际保存的 Digest、key/bytes 和已发布事实。

若最终 01 代码另有格式校验、约束或真实 backfill 需要，追加届时下一 migration（可能为 0003，以库存为准），不得修改已发布 0001/0002/checksum。迁移只作必要格式兼容，不能自动改业务终态、原 fee、RuleVersion、input_digest 或 prepared 身份；没有这种真实需要就不增加空迁移。

旧 binary 不理解新 prepared 字段，而且旧 Maintain 可能扫描同 pool 工作；不能因 Claim 的 cfg.Component 检查就声称允许新旧 writer 混滚。升级实际 scope 先停止并确认旧 writer 退出，再用新 binary + **原 cfg.Component/原 owner/scope/权限**恢复旧 `/2` 工作。原 scope 的当前查询/重传仍按原身份；不把旧 Decision 搬进 `/3` scope。

新 `/3` 不同 case 的 cfg.Component 含不同 ConfigDigest，现有单绑定 Service 是正确边界。首选各正常/错误 case 独立真实 Source+Decision fixture scope/pool/worker，重开保持同一准确配置。即使 pool 有不同 owner，当前 Claim 会先选 FIFO 再核对 ComponentRef，不能宣称两个单绑定 worker 会自动跳过不属于自己的 case 而公平服务。不要把多版本/多 case 混在同 pool 后靠重试掩盖饥饿。

只有真实票据需要同 scope 同时服务多个绑定时，才讨论一个窄的实际路由消费端口，并证明原 owner、固定绑定、队列选择和公平性；这不是本票先决条件。隔离的两个 case 已足以完成正常/错误与旧格式恢复，不能预建多版本 registry 或增加 06/process recovery 的隐藏前置。

## 5. 正式采用后的最小验证出口

1. 最终 01 writer 的 `/2 candidate_result` 正常和真实 prepared 中断记录，用新 binary 原 cfg 重开，公开查询/读回保持原 receipt、InputDigest、key/ref/bytes、Proposal 与累计 fee/start；原未知标签在 `/2` 仍 proposal_invalid，有同版本正常对照。
2. `/3` 各合法分支与 delta 组合通过真实 Source→Start→同一语义验证→prepared→publication/readback→completed；至少无 artifact 和有 artifact 两类重开恢复，不重复收费。cannot_continue 是 completed Proposal，不能偷换 failed Decision。
3. `/3` 有固定摘要的错误 case 已获 accepted 后在实际验证路径失败，重开与原 command.get 仍一致；四个独立行动有正常对照，stale condition/错误 tuple/purpose/来源遗漏有真实 Source 消费反例，codec 不读库。
4. 版本/配置/manifest/lock/permission 相互错配或 immutable reseed 真实拒绝；同原 Command 重放保持历史。新格式摘要/输入不符不发布，已有 v1 不受新 struct marshal 变化影响；Go/TS 全 Proposal raw 正反例仍按已冻结 1.1 隔离运行。

这些是 ticket02 自己的正常与恢复出口，不等整片最后票补齐；同时不替 01 的最终机械/DB 回归、不推进 03 whole 广告。当前没有需要新 ADR 的领域冲突。root 在 published01 最终 pin 的窄 delta 核对后可采用上述默认建议；若实际 API/格式不同，以读到的事实补最小差异，不把本候选 pin 当已发布能力。
