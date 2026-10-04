**正式采用：2026-10-04。** 前置01–03完整退出，准确03退出提交5fbb1a020658093266b4346370571370435f9630、受CI验证47ebce1/run37194868564 success。用户已授权按建议决定粒度/依赖并实施，不重复确认。root全文读取条件草稿和Astra high的具体接法，并实际核对最终exit SHA：与1aa条件pin相比仅12metadata路径改变，全部真实产品/工具/Schema/测试对象相等。下列原准备时pending描述保留为历史，当前接法由本采用及final-api-handoff确定；此时04尚无实现/验收证据。

# 04 完整上下文的最终 API 接法（1aa 条件准备）

2026-10-04。固定源码 `1aa21fcf47352c4460877bb51fc04dc302cdbae9`，读取 `/tmp/lerna-worktrees/deterministic-harness-whole` 的 git objects。**决定：采用完整 canonical mandatory-context Content 材料 + Content-backed Source/Publisher；04 不增加 1.2 Decision profile。** 1.2 仍只承担本片新的 Content/相应 Command 读取合同。此结论是具体接法，不留“任选扩版”岔路。

whole03 最终 CI/正式退出在本次委派时尚 pending；本稿不发布04、不实现、不证明任何04验收。正式采用须对 whole03 最终 exit SHA 的下列对象再做 equality/delta 复核。已读04两稿、882 preliminary recheck、实际04 spec、CONTEXT、ADR0006/0007及上下文架构。未运行 build/test/DB/服务，未读取环境/凭据，未改仓库。

## 1. 实际兼容依据与不可改处

- `components/decision_engine/ports.go` 的 Snapshot 明确是 fixture manifest，不是完整 Orchestrator Snapshot。其闭合字段只有准确 ref、Task/goal/control/component、materials、requirements、capability bindings、answer schemas、use refs、rule；Raw 是实际读取字节，不是任意新增字段容器。
- 1.1 `DecisionDecidePayload` 只传 SnapshotRef、TaskRef、ComponentRef、UseRefs、limits、deadline。SnapshotRef 是准确 owner/id/revision 身份，不含正文 hash；必须由 Source 的耐久不可变绑定补足，不可查“当前最新”。
- `worker.go:calculate` 真正读取 Snapshot.Raw、Lock.Raw、ManifestRaw，比较 manifest.Snapshot 和返回的 typed Snapshot；随后按顺序读取每份材料，核 hash/byte_length，累计所有实际字节。rule/2 的 candidate_result 把**第一份完整材料**前缀加上 `fixture result: ` 作为 artifact，Proposal 保留 manifest + 所有 material 的 processed refs。
- worker 接受1..63份材料、1..64条件；rule/3 扩展实际参数/schema材料 union 后也≤63；Proposal processed 总数≤64。这不是 Source.validateSnapshot 允许64材料就等于 worker 可接纳64。
- `source.go` 当前 Seed 把材料复制进 fixture_objects，并只允许同 Source owner/version1；这是旧夹具具体实现的限制，不能靠在04继续 Seed 新Content字节证明已接入Content。Source/Publisher 小接口本身没有该 owner/version1 限制。
- 旧 manifest/lock 的 kind、完整 Snapshot、RuleVersion、收费 basis 与 charge 都需准确校验。现 fixture-rule/2、candidate_result 的 ArtifactDigest=SHA256(rule版本字节)、ConfigDigest=SHA256(rule标签字节) 保持；Start 的 durable_rule_start、每start1 fixture费用也保持。不改 rule/2 含义、不编造新 rule、不改 Prepared 原key/原bytes/计量。
- ContentRef 既有六字段可无损承载新Content准确引用；media_type不支持参数，选 application/json。跨版本值桥接需逐字段、闭合校验，不能将1.2 Envelope假cast为1.1，ComponentRef.contract_version仍准确1.1.0。

**为何足够：** 本片要验证必要信息完整保存并实际送达规则决策，不要求 fixture 解释自然语言条件或裁决预算。完整正文在第一材料里会实际被读取、散列且逐字节进入可回读候选；这是既有 rule 的真实语义。1.2 Decision 不会增加本片所需表达力，反而复制旧账本和恢复兼容负担。完整读取/回显不证明理解、条件达成、真实模型或Task成功。

## 2. 唯一映射：完整文档 + 不变外壳

ContextCompiler 保持 domain/task 的上下文职责。它输出自己拥有的、闭合且准确版本化的 **mandatory-context/1 内容格式**（内部内容格式，不是对1.1方法加字段），再由实际装配 adapter 转成 decision.Source 所需 fixture 外壳。domain/task 不导入默认 decision_engine 或其私有表。

文档至少完整包含：

| 部分 | 必须保存的内容 |
| --- | --- |
| 格式/来源身份 | 内容格式版本、fixture输入owner及准确输入revision、TaskRef、goal_revision、control_revision、observed_at；明确 fixture 来源 |
| goal | 完整当前目标、原要求/约束正文；不是标题、摘要或一个裸ref |
| conditions | 全部当前条件的准确ref/revision、完整可检查内容、必要性与现有状态/证据缺口；所有必要条件禁止截断 |
| control | 当前准入/暂停/取消等实际fixture状态、控制修订、当前限制；未知显式未知，不默认running |
| budget | 原有限fixture预算/已记录占用与剩余额度、单位、观察修订、未知项及本轮准确DecisionLimits；不把root真实预算/Grant填成已实现 |
| deadlines | 原绝对期限及适用更早截止，不因编译/重开重置 |
| unresolved | 全部相关未结责任，含尚未远端接纳、效果unknown/may_apply_later、费用或清理未闭合及准确原身份；真实fixture未建此对象则只声明其有界场景确知的空集合，不宣称生产Task无责任 |
| selection/provenance | 精确输入refs、编译器实际全部processed refs、每个派生的策略版本/输入输出hash/来源、保留的进展、optional省略清单与缺口、用途及保留约束 |
| 固定策略 | 编译策略/字节计量版本、实际component/lock引用、本轮容量和输出预留 |

全字段有闭合类型、准确十进制字符串/规范UTC/有效Unicode；集合规范排序去重，语义顺序数组保持明确顺序。定义固定 no-number canonical JSON 编码（沿既有 RFC8785 子集规则），给独立黄金；现 `writeCanonical` 是旧包私有函数，不能假称已有公开通用API，也不改冻结旧摘要。新格式编码可用所需最小局部实现。Content hash 是最终canonical原字节SHA256，不是CommandDigest。

生成顺序消除自引用：先固定 SnapshotRef/TaskRef/ComponentRef/InstallLockRef 等非正文hash身份 → 编码文档 M → 发布其ContentRef → 形成既有 typed Snapshot（MaterialRefs[0]=M，其后为实际额外材料）→ 编码完整 manifest/lock及外壳。M 不含自身ContentRef/hash，也不含稍后 manifest 的hash；完整“本轮输入集合”由外壳绑定M及其材料，M记录其上游处理集合，不伪造自处理来源。

外壳 RequirementRefs 与 M 的条件准确投影一一一致，goal/control/task/component/use绑定逐项一致。在Source入口重新验证，而不是只信编译时检查。额外材料不能挤走 M；超出材料数、条件数、来源闭包数或字节上限都明确溢出/不支持，不删强制内容。

Snapshot是“准确外壳 + M + 已固定材料/派生来源”的不可变集合。外壳和 manifest 字节也经本片Content发布；fixture仅保存 SnapshotRef/InstallLockRef 到这些准确ContentRef的不可变映射及自己的前态/派发责任。原 ref 异映射拒绝，不能每次read动态重造 manifest 或换最新版。锁正文可保存为Content，但其绑定仍由fixture准确lock身份负责。

## 3. 来源完整性的两种集合必须分清

**Compiler processed** 是编译器所有实际读取/转换/参与选择的准确Content，含被丢弃或只影响选择的材料；完整写入M及Content派生元数据，承继来源闭包。**Decision processed** 是既有worker实际读取的 manifest + M + MaterialRefs（rule/3另含实际参数/schema union）。二者不要求冒充相同，更不能把compiler读过但worker只收到摘要的原文说成worker读过。

M、manifest、Snapshot外壳与规则输出的Content派生记录均关联真实直接来源及完整闭包；从 Proposal.ProcessedSourceRefs 回读Content来源可追到Compiler全部来源。不能只把隐藏来源ID写进M文本却不进入真正用途/保存/披露门禁。04同Content owner的64来源闭包上限继续执行，**包含新增中间Content所实际占用的来源位**；不是原63材料的每个来源再额外不计数。

M及其回显artifact包含全部必要上下文，保存/读取/披露必须分别获准。第一材料回显不能作为公开绕过控制/预算/未决效果私密性的通道；默认仅给获准验证主体读回，UI/日志不默认输出完整M。独立的disclosed refs可少于processed，不缩小原政策。

## 4. Content-backed ports 的最小可实施职责

新增的是04实际adapter装配，不改旧03 fixture Store，不让产品依赖 conformance/internal。ContextCompiler消费自己的小内容/当前输入端口；conformance下仅承担受信当前输入fixture与派发装配。生产Content端口和服务不得反读fixture/Decision私表。

- **ReadSnapshot**：用原SnapshotRef查fixture的固定映射，经Content受控读取外壳准确版本及必要M验证；返回闭合 typed Snapshot和其真实Raw。校验M与外壳绑定、完整条件/限制与原派发输入相符。不得把所有上下文字节藏在Raw的额外未知字段，也不得让worker收到伪造长度Raw。
- **ReadFixtureLock**：读准确锁/manifest Content，校验原kind、hash/length、manifest完整Snapshot、component/config、RuleVersion、basis/charge；返回实际Raw/ManifestRaw，复用既有 worker 检查。外部读取均有限且在fixture/Decision持锁Tx之外。
- **ReadMaterial**：对准确ContentRef做当前read/process及用途检查，受传入remaining byte bound限制读取原字节；不能先把任意大对象整体读进进程再报超限。Content本片256KiB对象上限与worker剩余额度都必须满足。为ReadSnapshot完整性额外读取M时也要独立有界、记录真实I/O；不得冒称该重复校验流量计入旧DecisionUsage的单次输入语义。
- **Authority/ControlAuthority**：仍为显式受信fixture权限，不是Grant。原Decision绑定当前主体/输入/固定component、准确limits/deadline、fixture控制与用途；原Command replay/currentauth和控制独立权限保持03顺序。返回的Permission须稳定匹配OriginalPermission，撤权用当前拒绝而不是悄悄换许可。Content自己的源政策也每次重核，旧fixture Permission不绕过它。
- **PlanPublication**：由原权限/Decision原publicationKey稳定派生Content owner/id/version及准确hash/length/media；校验当前处理/保存条件。只规划，不保存published内容/回执，不给后续授权。固定tuple含原字节、sources及purpose，异tuple拒绝。
- **Publish**：同一原tuple发真实Content put，accepted后只核对同一版本/原命令，直到本次有限期限内确认为published并准确回读才返回成功；准备中/未知返回可重试dependency状态，保持Decision原Prepared，不新建key/版本、不重新计算收费。Content receipt+Job在Content own Tx；Decision Prepared/Finish在Decision own Tx，不共享事务。
- **ReadPublished**：经当前权限读取原published版本并核验原字节；不能把preparing当可见或只凭file存在返回成功。原身份即使已撤权也不再新写；清理/核对责任不消失。

publication media选择保持确定性；rule/2前缀artifact为text/plain，Proposal为application/json。新Publisher有自己的Content owner身份，因此不假装与旧fixture的publication ContentRef相同；同一04装配中 Plan/Publish/重开必须精确一致。混装旧Prepared和新Publisher不支持：04用新独立fixture/Decision scope及准确绑定，旧03恢复继续原装配，不广告热切换。

## 5. 容量与费用：不能只量M或只量输入

固定 rule/2 candidate_result 为04正常验收默认。使用现有 `fixture-rule/2` / `candidate_result`，不需要rule/3扩展输出。M放第一位的原因是能通过公开Proposal和Content读回独立证明**完整强制字段进入真正worker**，无需内部mock观察。

编译前精确计算或给有证明的保守上界：
`I = len(Snapshot.Raw) + len(Lock.Raw) + len(ManifestRaw) + Σ len(actualMaterial)`；
`O = len("fixture result: " + M) + len(encoded Proposal)`。
完整Decision公共状态及请求/回执也要能通过原1MiB闭合codec。不得遗漏manifest中外壳的重复字节、ref清单/JSON转义、来源与条件证据造成的Proposal开销。无需复制worker业务来生成“真Proposal”；可用明确schema上界/规范编码测量工具预留，真实输出仍由worker形成并核验。

04默认总容量64KiB、固定输出预留8KiB仍有效：要求 `I + reserve <= capacity`、`O <= reserve`、输入/输出各≤Decision原限额及1MiB；Content单对象≤256KiB，材料/条件/来源数量同时有界。**8KiB不是保证可接近56KiB的M**，因为该rule完整回显M；已知输出容不下也在派发前返回context_overflow，不能用accepted→output_over_limit冒充成功编译。若需要更大回显，调用方在编译前明确选择更大且已批准的固定输出预留/策略配置，仍受同一总容量；不得溢出后自动扩限、裁M或换规则。

编译器额外读取、派生和最多3次重建有自己的原有限读取/期限累计记录；DecisionUsage仍严格是已有规则输入/输出、rule_starts/fixture费用，不把前期编译费用藏进或覆盖旧计量。1 fixture/durable start 是夹具收费，不是USD/CPU/模型费用。真实模型仍未装配，本片只证规则Decision边界有/无派发。

## 6. 当前性、CAS与恢复边界

完整M来自受信fixture当前输入快照，不来源于任意客户端声称current的JSON。fixture需在自身短Tx固定goal/condition/control/budget/deadline/责任集合的输入revision；完整读取必须覆盖全部分页/水位，不知道不能写空/0。当前04只为有限真实测试场景建该fixture，不预建Task服务、Grant、Provider或根计费系统。

Content发布M/外壳/manifest时，在Content自己的最终Tx重核全部来源当前许可与原处理记录。fixture随后用自己原输入revision和goal/control等前态CAS，将原Snapshot映射、原Command派发责任及必要Job同Tx提交；改前态则不派发旧Snapshot。跨owner IO在Tx外，提交未知恢复原责任。不同owner不存在共同瞬间原子保证。

CAS后排队期间的新fixture控制/源撤权还必须在实际派发与Source/Authority当前门禁拒绝旧输入；有已发Decision时走原控制/维护责任，不能只修改M或重新Seed。由控制与当前来源阻止新处理，不假称已读取内存或Tx外在途效果立即消失。05再用实际Task/Application复验修订/派发/消费；06再接真实Grant/预算。此接法不改变这些硬边界，也无新ADR冲突。

## 7. 票03/04的具体公开验收补足（此刻均未执行）

1. 正常：真实Content存储M，包含非空goal、多个完整必要条件、控制、fixture预算/期限和一个真实fixture未结unknown效果记录；公开1.1 decide完成。经公开get取得Proposal/ref，经真实content.get读artifact，去固定前缀后与M**全字节相等**，再独立按闭合格式检查所有必要字段，不只检查refs存在。
2. 同源：原Snapshot/manifest/lock重开仍绑定准确M与材料；修改同身份正文/投影、错误hash/版本、控制或limit绑定均拒绝。新的同名Content版本不能替旧ref。
3. 完整来源：确定性编译实际读取两源，仅披露一个；M中compiler处理集合与Content闭包有两者，Proposal精确声明worker实际集合。第二源撤销使新处理/保存/披露拒绝，不能靠摘要漏ref通过。
4. 边界：分别强制正文、metadata+manifest开销、输出回显/Proposal、材料/条件/来源数量超限；编译返回准确overflow，无Decision派发；每类配可容纳正常对照。InputLimit/OutputLimit事后worker拒绝仍保持防线，但不能代替编译前容量验收。
5. 竞争：读取后来源变化、Content发布后fixture CAS失败、CAS后控制收紧、派发前撤权，分别观察有限拒绝/重建及原责任。fixtureCAS证明仅其owner，不宣传全局原子或生产Task控制。
6. 恢复：Content put accepted后超时/回执丢失、字节写后发布前SIGKILL、Decision Prepared后publication重传；原key/bytes/source/receipt/fixture计费保持，未确认的发布/holder继续负责。正常与故障都独立读Content/Component公开事实，不靠私表或内部调用次数。

正式04 frontier：核对whole03真实退出、最终CI和上述对象相等性；再定1.2 Content机器闭合合同、mandatory-context局部schema/黄金、04自己的迁移编号和实际接口接法。不改旧1.1 schema、广告、错误、账本或规则版本；不扩其他未来票。若最终真实对象改变到上述接法不成立，针对实际差异重做本项决定，而不是给实施者留下静默扩版许可。

