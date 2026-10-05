# 05 实施前冲突与最小路线：条件性静态准备

本次只读研究，未运行 Go/Node、测试、数据库、服务或供应商；未修改仓库、发布票或创建 Task 夹具。**04尚未整片退出，05未开始。** 以下为到达05前沿时的推荐默认，不替代04最终源码、API、完整验收、CI与退出SHA复核；当前04内部票06不是后续切片06授权账本。

## 1. 来源与资格

- 根观察点：`b9db5cc67647a15d6556fe92941757034a1e80d1`。阅读根 `AGENTS.md`、`CONTEXT.md`、05规格、06实际规格 `lerna-06-authorized-admission/spec.md`、Task生命周期/合同/数据模型及相关ADR0001–0004、0006–0009。这些设计说明不等于能力已实现。
- 公开库存实际只有合同1.0.0/1.1.0/1.2.0；1.1为 Decision+Command，1.2为 Content+Command且方法广告仍false。根当前没有 `domain/task`；04票03工作树已有的 `domain/task/context` 是未整合的候选源码，不是已发布Task owner。
- 04候选读取自 `/tmp/lerna-worktrees/content-snapshots-03`：基线 `b28b2e3182d97156c5fe203baf5cf2183631b7f2` 加明确WIP；仅把其具体端口当未来接法输入。源码指纹见末尾。不能用候选容量正常样本替代04仍未关闭的真实race失败。
- 历史 `/tmp/lerna-05-conditional-decisions-final.md` 与 conditional-ticket-proposal 为准备资料，不是正式采用/实施证据。以下修正其实际接法不足；不重新起草全部切片。

## 2. 新Application机器合同与广告

**推荐：在正式前沿确认版本库存后分配1.3.0，新增严格封闭的Task方法类型、Go/TS同版生成物、正反金样与同版Command查询。** 不往冻结1.1/1.2塞Task字段，也不把ContentRef版本或ObjectRef.revision借作不同语义。以 `task.submit/get/list/answer` 和必要 `command.get` 组成准确方法清单；若1.3此前已被实际占用，重新分配唯一新版本，不能抢占已有绑定。

`submit` 的业务提交点是同Task owner短Tx内固定原Command、Task、原文引用、策略/fixture预算、初始目标与推进Job；`answer` 是同Tx一次消费准确InputRequest、记录答案和唤醒原Task。回执固定原提交点，不随TaskView后续revision变化。原key重放优先，但仍先验证当前查询主体有资格读取原命令；别直接裸用 `runtime.AdmitWithGate` 当完整授权入口——其original分支在新admission gate之前返回，调用方须承担current-reader检查。

TaskView需区分总revision/goal_revision/control_revision，保留原始与当前目标、完整Requirement/覆盖缺口、等待原因/期限、当前Snapshot/Decision、累计fixture费用及未知、未闭责任；`result` 在05无正式Result时明确为空。receipt不是Task完成。get/list不创建Job，分页有当前主体绑定与有限cursor；task.list列出现有同主体可读Task，不能靠省略未就绪Task伪造进度。

方法声明、schema支持、profile开放是三件事：开发中库存可列出准确版本但广告false；完整新Task profile只在全部其所承诺方法/失败/恢复同版真实闭合后开放。不得因为 `task.submit` 第一条green广告整片05，更不能把整套未来Application方法都列为支持。无需等待11 SDK或13网络传输才通过当前公开Application进程边界验收。

## 3. 第一真实Task owner与06尚未ready的预算

推荐真实 `domain/task` Orchestrator + 消费方仓储端口 + `adapters/postgres/task` 自有迁移/事实，不把04的 `ContextDispatcher` 测试owner改名成Task，也不在 `conformance/internal` 实现目标/条件权威。Task域不得导入 `components/decision_engine`、具体Content适配器或conformance；宿主装配公开Component客户端和消费方声明的小接口。

05的budget_ref准确指向**真实持久的Task fixture额度分配**，绑定原Task/主体/策略版本/单位/总上界/原期限。可信配置明确允许该确定性计费能力后才能接纳；不把用户目标、模型建议或普通数值当Grant。只支持已知有界 `fixture` 费用；生产Budget/Grant/Confirmation、USD账单与行动许可明确unsupported，留给06/10，无06隐藏前置。

同TaskTx先占用每个原Decision最坏fixture费用/调用额度、登记未被远端接纳也存在的派发责任和Job，再Tx外发送。绑定原Decision/Command/Snapshot/Component/上界；不因超时、worker替换、相同key重传或新Snapshot复活额度。根据公开Decision累计用量按准确来源修订结算；`durable_rule_start`收费和真实完成steps分开，未确认/legacy未知不记免费，未知责任未关闭不退占用。规则0模型调用是真值，但不表示整个Task无成本。

旧条件稿的8次/无进展3/15分钟等只可作为首次明确固定的fixture配置候选，到前沿按真实完整回路核对；不是现在扩大预算的许可证。无进展定义是持久业务差量（新增覆盖、有效答案等），等价改写、重放、换worker均非进展。05到限停止新推进并公开等待/耗尽原因，不能直接写failed/cancelled终态绕过08同Tx唯一Result。

## 4. 当前规则与Compiler确实不能直接完成05

实际 `components/decision_engine/worker.go:529` 对所有现有规则要求至少一个Requirement；`proposal_v3.go:25` 直接取首Requirement作替换。rule2只有candidate，rule3是固定delta/四行动/InputRequest/cannot分支，不能冒充由空条件完整提炼任意目标。**推荐新增准确的 `fixture-rule/4`（库存核对后）与有限 `task-clauses/1` 策略；保留2/3版本的全部原含义、配置摘要、计费、Prepared格式和恢复。** 不在原unknown label下增加合法业务，不预建StrategyRegistry。

冲突不仅在worker：04候选 `context/canonical.go:49` 要求 `Policy.Version == rule-fixture-utf8-bytes-v1` 且非空conditions；`adapters/content/decision/ports.go:53`、`assembly.go:97,106` 将Permission/manifest/lock固定为rule2。因此仅接入Task.CurrentInput或新增rule4计算函数不够。假造一个“占位已覆盖Requirement”通过旧Compiler会直接破坏AC2。

最小默认是05新增**准确版本的Task mandatory表示及对应编译/装配分支**，保留 `mandatory-context/1` +旧ByteStrategy解析/校验不变。建议Task使用 `mandatory-context/2` 与 `task-clauses/1`，显式容纳空但未ready的初始条件，以及完整Requirement来源/kind/required/rule_ref/成果范围、原文覆盖依据、三修订、预算引用与占用、真实未决Decision责任。旧Condition仅Ref/Text/Necessary/State/Gaps，旧Responsibility要求operation kind；不要省略新增必要事实，也不要把未派发Decision伪装成Operation来凑旧结构。该Content正文版本扩展不要求更改冻结1.1 Decision wire。

复用04已有真实Content读取、容量计算、耐久读预算和发布恢复机制；新版本只实现本次Task真正消费的表示/规则，直接版本分支即可，不造通用编译框架。新Assembly/Source要匹配真实rule4 manifest/lock/成本/完整I/O与Proposal上界；M第一材料、所有处理来源和真实readback保持。不能把旧candidate_rule的输出reserve直接当新delta/inputRequest最坏输出证明。这是05必要接法，不是04新增退出门槛。

## 5. Requirement覆盖、等价与提案消费

有限规则策略读取真实原目标Content及明确的条款语法/规则材料，给每条原要求稳定出处与适用成果；完整原文仍保留。覆盖的分母是所有原明确条款，包括effect/constraint，不是模型已提取的子集。无可机械判定的自由文本部分保持gap并请求澄清，公开这种能力范围；不能宣称自然语言语义覆盖已被证明。

等价默认基于版本化有限语法的规范结构：kind、required、scope、规则参数和源条款绑定，而非仅statement文本或模型声称同义。RuleSpec规则类别与Requirement的output/effect/constraint不是同一枚举。Proposal中1.1 Delta没有独立scope字段，准确规则文档应携带scope并由Task验证其来源/归属；不得忽略scope或通过未知wire字段传入。

当前1.1 `requirement_delta` **每Proposal最多16项**，conditions上限64不等于一次可合法提交64项。允许在原有限Task预算内分轮完整覆盖；保留所有未提及条件，准确替换旧ref，缺口未覆盖不得ready。多轮溢出/耗尽公开失败原因，不截断分母。来源真正撤销、BodySeal/Gone或当前资格不足时，旧Proposal不能更新当前业务。

Task短Tx以原Decision唯一消费键、当前Snapshot/goal/control/input修订及重新核验的来源资格落事实。实质Delta先提交新goal_revision，同Proposal剩余action/candidate全部失效；等价Delta不增加goal_revision。晚到、重复、乱序结果保留原费用/记录而不重复改变业务。05尚无06/07行动执行能力：无新Delta的行动分支也只能准确等待/unsupported，不能生成假的Operation或Result；“无行动外发”须由真实边界观察，非私有计数。

## 6. 04真实交接与恢复接缝

正式采用先核对04完整退出源码：`Compiler.Current` 的 `ObserveInput` 加 `BeginCompile/ReserveRead/ConfirmRead`；`ProcessingContent.ReadForProcessing`；`Assembly.Plan/PublishBundle` 的显式verification reader；Content-backed Access 的 `Binding/Current/StagePublication/PrepareContent`。这几个端口有不同owner责任，不能把Source适配器的Permission等同Task授权对象。

Task实现其自身输入/编译预算权威：原fullSnapshotRef唯一根、原Command/Decision/Component/limits/deadline/readbudget固定；BeginCompile原子扣次数，实际读取先预留、已发未知不退；同Snapshot修订/reopen不能重置最多3轮与累计真实字节。真正新Snapshot可有新编译身份，但Task总次数/费用/原absoluteDeadline继续累计。显式编译reader与worker DecisionUsage reader分开，不用context.Value隐式记账。

Task先耐久固定dispatch/Content原键与待办，Tx外编译/Content发布/Component发送，再以当前修订CAS推进；Content和Decision owner各自提交，绝不声称全局原子。跨owner当前资格的有限窗口与所有05BodySeal/ancestor/完整subject-purpose/read-process/save-disclose规则照常适用；真实拒绝保留已产生Content/Prepared/费用/清理责任。

原key恢复优先查询或重传，不能直接生成新Decision。“未派发”oracle修正旧草稿：1.1 `Decision.get` 无not_found，nil记录返回 `result_unavailable`，不能当不存在证据；应组合获准 `Command.get` 原key not_found、真实Task公开未派发状态/原身份责任以及真实Component有限出口观察。不得用测试模拟计数替代。

## 7. 最小可交付路线（非发布票）

| 纵向增量 | 本增量必须真实闭合的正常/拒绝或恢复 | 对应05 AC |
|---|---|---|
| 真实Task提交/查询与首次有界规则回路 | 原key唯一Task、真实owner PG、完整新版本上下文和规则Proposal；跨租户拒绝、提交未知原键恢复 | 1，2基础 |
| 完整条款覆盖/Delta/唯一消费 | 空与遗漏保持未ready；新Delta和同Proposal行动失效、等价不改goal；真实重复/乱序/stale | 2、3、4 |
| InputRequest/answer | 问题+有限封闭Schema+绑定修订/期限耐久；错答案/错版本/过期拒绝、合法once唤醒、reopen | 5 |
| 强制上下文容量与修正 | 真实M/I/O/reserve超限TaskView，无真实Decision发送；合法answer缩减非必要输入后正常推进，原要求/预算不删除 | 6 |
| 累计额度/无进展 | 真实多轮消耗停止，费用未知保责任；换worker/newSnapshot/replay不重置，允许范围内正常对照 | 7 |
| 原身份进程恢复汇合 | 接纳/发送/提案消费前后真实中断与原key恢复，等待及费用/历史持续；不依赖后续Result成功 | 1、4、5、7恢复 |

这些是可交付行为的次序建议，不是已发布的六票，也不要求首票等待全片收尾。答复依赖InputRequest真实存在，溢出纠正复用answer；无进展依赖真实Delta回路。整片合同广告、独立评审、准确CI和全部7AC证据在root整体闭合，不藏在最后一票。额外空间/权限边界测试服务实际变更，无未来16/17/19隐含前置。

## 8. 到前沿必须补齐什么

唯一当前硬门是whole04最终退出与候选API/source delta；然后确认新合同/规则/mandatory格式版本库存、真实Task命名空间、有限fixture额度和条款/答案策略固定输入、04生命周期/编译预算/容量实际边界。以上均能在已授权工程范围内形成默认，不需现在询问供应商/真实读者/云账户，也不能把尚不存在的生产Grant或Task服务写成已有依赖。没有发现必须推翻现有ADR的领域冲突；实际实现若改领域决定再按AGENTS同步，不为内部接法新建泛用ADR。

### 只读来源指纹

- 05 spec：`76cf3e58414b73d9a9c36e3f28b7a99eab710075bdc4f5b869860b2def8e4af0`。
- 根 CONTEXT：`cd395c9cbe84711cb5d30e90076a953d4d10b10dc8b1ae38339ba52e10dec583`。
- 候选 context/types.go：`afea8dc64d9754ae67e157c5393b9ebbe03c10035ebd264c5a1ef852246f8be2`；canonical.go：`1350ffdc90bafc5b07a38ac96c9cdbad4020974eac4946d6785bd53fb19f0c63`。
- 候选 compiler.go：`1833c2d3d8c989c01ad4c6c8dea447898303dddb5f71ebd9ba92e1e977c25dbe`；budget.go：`f19d5a58c0c9378ae5130990aa80fd416c964dbfba0e98d1a7d9c4e2792d3c46`。
- 候选 adapter ports.go：`edfc9faf253e3d16959ed37242b587ade9973a572d246d539c04e6094a75782c`；assembly.go：`81159617d649cead0c3ac2a6d7dd745753db3c07365d4eb8c99b3ee25ec62036`。
