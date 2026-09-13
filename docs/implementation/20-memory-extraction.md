# 20 票实施记录

## 本地显式偏好提取

19 已完成验收，20 最终审查基点固定 `8687e5a`。使用 Brief 中已约定的本地提取器接口开展第一个 TDD 切片；先落实材料到候选，再连接真实受控任务宿主。全部原票验收范围保留。

新增消费方定义的 Extractor 接口及 localextraction Rules Adapter。来源身份由可信读取器提供；正文不能自授主体。当前支持质量基线 v1 的四个显式偏好文本，完整语句匹配，候选复制精确来源修订/定位，保留 preference 类型、本人陈述依据及 answer 条件。输入材料数量、大小有界；无网络、文件保存或 Memory 写入。规则输出本身不构成保留、保存或披露授权。

首个简洁偏好测试在缺失接口时失败，实际实现后 race 通过；详细/中文/英文样本起初全部 unsupported，补齐规则后完整包 race 通过。见 evidence/20-rule-preference-{red,green}.log 与 evidence/20-rule-variants-{red,green}.log。目标包 vet 和 diff 检查通过。

尚未完成推断提取、完整14例质量报告、受控任务/授权宿主、约束交集、候选持久化、持续扫描、未知保存恢复、生命周期清理、跨任务使用、CLI/SDK、整票验证及审查。此切片不代表20交付，也不替代18真实模型效果。

## 基于独立选择事件的推断

输入增加由可信来源读取器提供的 Choice（事件身份、任务类型、格式）；不从自然语言身份或事件声明取得真实性。三份独立、同主体、同报告格式的选择生成 tentative inference，挂齐三份来源及定位，条件仅为 report。重复事件或同一来源的新修订不算独立证据；证据不足、不一致、主体不匹配及混合文本指令均无候选。

接口缺失红灯后，简洁/详细两种真实规则推断通过。临时移除独立性检查时，重复事件和同源新修订的反例实际产生错误候选，测试失败；恢复检查后完整本地提取器 race 通过，vet/diff 通过。证据为20-rule-inference-{red,green,duplicate-red,boundaries-green}.log。临时探针已清除。

这只验证固定事件模型的推断规则；当前还没有可信来源读取宿主和来源约束交集，不能声称拥有提取/保留/披露权限或已自动入库。其余整票要求继续待办，下一步补齐固定质量集完整报告并连接受控任务宿主。

## 固定质量集实际报告

将实施前质量计划 v1 的14个原输入和标签原样编码为 profiles/extractioncheck/testdata/quality-v1.json；文本不按案例编号进入提取器，事件作为可信观测结构输入。Quality 在执行前构造独立预期，逐项核对候选主体、类型、值、条件、置信依据及全部精确来源，报告数据集摘要、实际候选、缺失、误提取和分母。候选与执行效果分开计量。

实际 `go test -race -v ./profiles/extractioncheck`：正确候选6/输出6，召回6/6，误提取0/8，完整符合14/14。报告见 [20-quality-v1-report.json](evidence/20-quality-v1-report.json)。临时让真实规则全部返回unsupported时，报告失败、缺失6、precision=null，保留20-quality-no-output-red.log；恢复实现后相关完整包race和vet通过（20-quality-regression.log）。临时变更已清除。

该报告仅覆盖固定公开合成语料，不能证明任意中文表达、模型效果或真实权限治理。受控任务、来源读取、保存/扫描、生命周期及跨任务使用继续实施；尚无20完整CLI/profile验收。

## 独立当前授权检查

新增 extractionauth，将可信宿主固定的来源资源、用途及本地位置映射到当前 Harness ActionView。read、extract、候选retain、save、scan和候选disclose分别检查对应权限；extract同时需要source.read，scan同时需要source.read/extract/scan。正文及候选不能自选资源或扩大用途。调用一秒内有界，只返回当前观察，不产生可缓存许可。

真实 SQLite 授权测试证明单独读取不能提取/扫描，只有策略没有Grant仍拒绝，有提取Grant后成功，撤销策略后拒绝；错误命名空间、主体、处理位置或接收位置均拒绝。接口缺失红灯、真实链绿灯及边界回归见20-auth-{phases-red,phases-green,boundaries-green}.log；race/vet/diff通过。

这是本地宿主授权适配器，尚未接入实际任务执行及来源读取，不以独立Check代替效果前复验。来源当前修订/驻留交集、候选使用登记和原Memory保存准入仍须连接；其他原票行为继续待办。

## 来源限制交集及执行接入决策

新增可信策略输出 Restrictions 的有界交集：逐项相交存储、计算、用途和接收方范围，保留期限取最早值；任何一项无交集或已到期就拒绝，不丢弃该来源。可把更严格候选策略作为额外操作数；结果排序并独立复制，不修改来源策略。该计算不授予权限，不能接受正文声明作为可信输入；实际效果前仍须重新解析来源策略。

接口缺失红灯后，双来源交集、所有维度不兼容、到期和独立复制测试通过；四个相关包race、vet及diff通过。证据20-restrictions-{red,green,regression}.log。尚未连接实际来源解析和出口，因此不能据此声称完整驻留验收。

任务接口核对发现 WorkPort.GuardExecution 会将一个已有预算的工作世代绑定到原执行操作，ConsumeExecution接收原效果和受控输出引用；普通Brain完成回调不是候选保存的替代事务。后续采用现有Execution Driver的Start/Observe路径：来源引用作为输入，候选持久化作为可核对效果，结果只指向受控候选产物，并沿原操作恢复。不得直接把候选正文写入Core结果或另建提取循环。实际Driver/宿主尚待实现；本次没有声称已接通任务。

## 候选与原操作的原子持久化

新增受信 CandidateStore 和 SQLite Adapter；候选身份沿用原提取操作，在命名空间内保存主体、候选和继承限制。Commit同事务写入原身份及完整记录；重放必须保持原语义，改变候选返回IDENTITY_CONFLICT，未知提交调用方应Lookup原操作。候选仍不是已入库长期记忆。

SQLite采用私有0600普通单链接文件、WAL/FULL、两秒调用期限和事务内写锁；总记录512、单记录16KiB，容量不自动驱逐身份。实际保存/关闭/重开/原操作查询、重放、语义冲突及命名空间隔离通过，race1.064s；接口缺失红灯保留20-candidate-store-red.log，绿灯见20-candidate-store-green.log；目标vet/diff通过。

该存储不自行授予当前权限或判断当前来源，尚无用户读取入口、真实进程退出证据、候选失效/清理及使用登记。生产效果接入前必须补齐这些治理条件；执行Driver与来源读取仍待接入，不把本切片当作20完成。

## 候选退役与原效果事实

CandidateLifecycle新增Retire和只返回最小身份/效果事实的Inspect。Retire在同一写事务中保存永久原操作屏障并DELETE候选正文；已保存候选标记Committed=true，保存前已退役标记false。两者均拒绝迟到Commit；不同主体不能替换原退役身份。退役记录与可用候选共用512身份容量，不按时间自动驱逐。

实际保存后退役、关闭/重开后正文不可读取、原效果事实仍可核对、重复退役及保存前屏障均通过；接口缺失红灯和race1.105s绿灯见20-candidate-retirement-{red,green}.log，目标vet/diff通过。只证明受控SQLite逻辑正文清理，不声称WAL/介质即时物理擦除。

当前退役需受信调用，尚未连接来源事件、授权使用通知及实际消费完成度，也未提供真实进程退出证据。执行与用户披露仍不得绕过这些待接入治理；第20票保持in-progress。

## 候选真实进程退出恢复

新测试以实际本地规则产生候选，子进程在提交前、提交后、已提交候选退役后及保存前退役后直接os.Exit(73)，不调用Close。父进程打开同一SQLite文件并通过公开存储接口核对：未提交状态不伪造效果，已提交重放保留原候选，退役保留原Committed事实且不可复活，保存前屏障不冒称已保存。

临时将退役事务Commit改为Rollback，两个退役场景均实际失败；恢复实现后完整SQLiteExtraction包race1.341s通过，vet/diff通过。证据20-candidate-process-{red,green}.log；临时修改已移除。该证据是本地存储崩溃恢复，不替代受控执行、保存回包未知或完整任务重启验收，后续仍需接入宿主。

## 提交与退役竞争

两个真实SQLite连接同时Commit/Retire，同一原操作最终只能是retired；Committed事实必须与提交先发生或被屏障阻止的实际返回一致，正文不可读取，迟到重试仍拒绝。12轮不同原身份竞争通过。临时跳过Commit内退役屏障时，首轮实际出现退役后提交，测试失败；恢复后完整SQLiteExtraction race通过（20-candidate-concurrency-green.log），vet/diff通过。无生产代码变更，临时修改已移除。

驱动接入仍需把执行Request的原语义关联到候选效果核对，不能把任意同操作ID请求当作相同调用；该关联与来源读取是下一步，当前未宣称完成执行接入。

## 原执行关联的持久保存

CandidateRecord及最小CandidateState新增InvocationSHA256，供驱动固定原执行请求元数据的摘要，不允许用原始来源正文摘要替代。Commit同原候选保存，改变摘要的同操作重放拒绝；Retire在清除正文的同一事务复制该关联，Inspect无需候选正文即可取得。空旧关联不作为执行证明。

旧退役表使用事务内迁移新增空默认列，两个Open通过原写锁串行化迁移；独立旧schema/已有提交与保存前屏障两类记录迁移并再次重开，保留原事实且摘要仍为空，不补造历史关联。新调用保存/变更拒绝/退役/重开以及相关完整race通过，见20-candidate-invocation-{red,green,migration}.log；目标vet/diff通过。

当前仍是受信存储字段，实际Driver必须比较execution.Request.Fingerprint，不得把字段存在当成已做校验。来源读取及Execution宿主仍待连接，第20票未完成。

## Execution 驱动首条实际链路

新增extractionexecution.Driver，满足既有Start/Inspect接口。宿主固定身份、位置及用途，Execution负责调用前的Core资格；Driver读取有界来源引用输入，检查独立提取权限，要求可信Sources先验证读取权限并提供精确来源和限制，完成交集、本地实际提取和当前保留检查后沿原操作Commit。来源引用及预期元数据独立复制，提取实现不能通过修改共享指针改变来源证明。最终保存前检查保留期限。

Inspect比较持久InvocationSHA256与原Request.Fingerprint；候选未出现仍UNKNOWN，不把未知判为没发生。已提交后退役仍报告CONFIRMED/FAILURE且无输出；可用候选经当前来源及披露检查后仅输出候选身份引用，不输出正文。来源/规则/权限/持久化均通过接口依赖，无额外运行循环。

实际集成使用本地文件、真实SQLite授权及候选数据库、本地Rules：提取保存、原操作重复Start、替换InputRef拒绝、退役后仅核对原效果通过。接口缺失红灯后，驱动及相关完整race、vet/diff通过，证据20-extraction-driver-{red,green,regression}.log。来源读取器目前是固定公开文件的测试边界实现，不能作为生产实时来源认证或变化检测证据。

尚未组装真实Core/SDK宿主，未支持无候选结果的持久等待、保存到Memory、持续触发、原使用登记及来源清理通知。预提交退役没有执行关联时驱动拒绝核对，不伪造原调用证明。现有驱动只是第一条链路，以上原票要求必须继续完成，不能将本测试作为20整票验收。

## 真实本地来源适配器

localextractionsource实现受信文件登记和当前检查，注册表由宿主固定精确来源修订、私有文件路径/摘要、主体、定位及限制；提取请求不能自选文件。Read在允许计算位置、用途和来源未过期时读取至多4096字节，拒绝特殊文件、符号链接、额外硬链接及非私有文件，验证完整文件摘要；Validate重新检查当前登记、所有限制和实际文件。配置及返回值深复制，Replace为受信配置端口，撤销后旧引用立即拒绝。

驱动集成已使用此真实Adapter替换先前固定文件测试实现；源文件改变后保留CONFIRMED事实但拒绝输出候选引用，另验云位置拒绝、保留期限收紧及来源移除。相关完整race为LocalExtractionSource1.017s、ExtractionExecution1.175s，vet/diff通过；证据20-local-source-{red,green,driver}.log。

目前来源支持已注册文本文件；选择事件读取、来源修改事件与候选持久失效通知尚未连接。仅靠恢复文件或恢复策略可能再次满足当前校验，原使用的永久失效仍须接入19通知机制，不能把动态检查当作生命周期治理完成。Core/SDK宿主及其余原票验收继续待办。

## 真实选择事件来源

可信登记项增加Encoding，默认text，choice仅解析精确event/task/format三个字符串字段。来源正文不能自选编码、主体或来源方法。三个实际私有文件经摘要和限制检查后交给真实Rules，产生report范围的tentative inference并保留三份来源；指令字段拒绝。

反例发现encoding/json默认允许重复字段，可能覆盖事件身份；已改用项目jsonvalue严格解码并检查精确字段名，重复字段、大小写别名和额外指令字段均拒绝。红灯20-choice-source-ambiguity-red.log保留实际失败；完整本地来源/执行驱动race及vet/diff通过，见20-choice-source-{red,green,regression}.log。此为真实文件到推断链路，尚未完成Core/SDK任务与Memory自动保存验收。

## Core 与 Capability SDK 正式执行链路

新增单机参考宿主，使用真实Core任务、Worker资格、签名一次调用授权、受控输入/输出产物、Capability SDK和实际提取Driver；来源文件与候选分别独立保存。宿主只准备一项有界工作并沿既有Execution运行，不另建任务循环。完成后SDK原调用重放及再次Run保持任务版本和原回执，候选匹配原Request.Fingerprint。

首轮真实链路因Driver没有Evidence而WAITING/UNKNOWN，已补上仅含原操作、调用摘要及Committed事实的证据；正常链路COMPLETED/SUCCESS/CONFIRMED且有受控结果引用，候选为实际简洁偏好。红灯20-core-sdk-integration-red.log及绿灯20-core-sdk-green.log保留。

进一步在实际Driver提交后、Execution核对前退役候选，暴露执行层只接受FINISHED/SUCCESS/CONFIRMED，错误丢掉已发生事实。现在接受有证据、空Output的FINISHED/FAILURE/CONFIRMED，且不保存不可用结果或生成引用；原Core消费保留效果事实，任务不冒称完成。红灯20-retired-core-red.log保留实际UNKNOWN，修复后真实链路通过。原有executioncheck完整race18.767s、新宿主2.046s通过（20-core-sdk-regression-final.log）；目标vet/diff通过。

此阶段是正式能力执行链，仍未覆盖Memory自动保存、持续触发、持久来源/授权失效通知、全出口治理及任务退出恢复。宿主暂无20具名CLI，整票verify与双轴审查留待全部需求实现后；第20票保持in-progress。

### 正式任务的独立权限撤销验证

`extractioncheck.CheckTaskPermission` 在实际 SDK 接纳之后、Execution 开始之前，通过真实授权服务仅撤销读取、提取、候选保留或披露中的一项权限。其余任务和能力权限保留，使用实际来源文件、规则与 SQLite 候选库。

读取、提取和候选保留撤权均未产生候选；披露撤权保留已提交事实，返回 FAILURE/CONFIRMED，无结果引用，Core 不进入 COMPLETED。编译红灯、定向通过及整个 extractioncheck 的 race 结果分别保存在 `evidence/20-task-permissions-{red,green,race}.log`。此项不代表长期保存、扫描或来源永久失效已完成。

### Memory 来源授权适配

`localextractionsource.Source.Check` 实现 Memory 消费方的来源检查接口，按动作分别检查存储、计算或披露位置，并检查用途、保留期限及实际文件修订。未知动作拒绝，不把提取许可升级为持续扫描许可。测试采用不同的存储、计算、披露位置，避免单一位置掩盖维度混用；同时验证来源文件变更、越界期限、用途和云侧请求拒绝。

`evidence/20-memory-source-red.log` 记录缺失接口的红灯；`evidence/20-memory-source-green.log` 记录来源、执行适配及 Core/SDK profile 的 race 回归。该适配是当前来源校验，尚不提供持久消费者登记或删除清理确认；自动保存和永久失效验收仍未完成。

### 受控提取任务中的自动 Memory 保存

`extraction.SaveJournal` 将候选身份关联到完整且不可替换的原 `MemoryWrite`。SQLite 在候选仍保留时绑定保存请求，每个候选最多一份，每个 Memory 操作最多关联一个候选；共享候选写锁与退休事务。退休原子清除保存请求正文，仅留下原操作与 Memory 引用用于核对和后续清理。该关联不代表 Memory 已提交。

`extraction.Saver` 按当前保存权限、来源修订及约束构造固定 schema 的 attribute/value 投影，先持久化原请求，再调用真实 Memory。原请求包含固定 RecordedAt、来源及保存身份，重复恢复不改写。提交回包未知后通过 `InspectOperation` 核对；committed 可确认，unknown 保持未知，不能另建操作或宣称成功。Saver 没有后台循环，由 `extractionexecution.WithMemory` 放入现有 Core 资格约束的执行步骤。

`extractioncheck.CheckSave` 已通过实际 SDK/Core、签名调用许可、本地文件提取、Memory 授权与 SQLite 写入完成偏好保存；再次核对得到原回执，集合仅一项变更。`CheckSaveFault` 覆盖真实提交后丢回包、授权接纳后提交前未知，以及撤销 memory.put。丢回包恢复任务成功；未知保持未完成且无结果引用；保存撤权保留另行获准的候选，但不产生保存请求或 Memory 写入。

证据：`20-save-intent-{red,green}.log`、`20-save-intent-process.log`、`20-auto-save-{red,green,core-green,fault-red,fault-green,regression}.log`。进程验证目前只覆盖保存意图落盘及退休后的突然退出；不能据此声称整个 Memory 提交跨进程恢复已验收。相关包 race 与 go vet 已通过，未运行整票完整 verify。

尚待完成：保存后的独立任务实际使用；完整 Memory 提交与确认前后进程退出；候选/保存意图/派生记忆的永久来源失效、清理与消费者确认；敏感附加约束及所有出口治理；持续触发与取消；本地规则不支持结果的持久等待；正式 CLI/profile 报告及最终双轴审查。18 的额外付费模型预算仍未使用。

### 保存恢复请求的独立保留权限

新增真实撤权竞争验证：候选已成功提交后，通过授权服务撤销 `memory.candidate.retain`，保留 Memory 保存许可。修复前任务仍成功，证明保存请求副本绕过了当前候选保留权限（`evidence/20-save-retention-red.log`）。Saver 现于持久化保存请求前独立检查 retain，拒绝新增正文副本；原候选提交事实保留，没有 Memory 保存效果，也不完成任务。

`evidence/20-save-retention-green.log` 记录提取、执行适配及真实 Core/SDK profile 的 race 回归通过；相关 go vet 和 diff 检查通过。本修复不替代后续的保留到期清理或永久来源失效。

### Memory 保存任务的真实进程恢复

新增 `TestMemorySaveTaskRecoversAfterAbruptProcessExit`：子进程使用真实 Core/SDK 接纳、授权、来源文件、候选库与 Memory 数据库，在 Memory 接纳后提交前、实际提交后回包前、Core 确认后分别 `os.Exit(92)`，不执行正常 Close。父进程重新打开同一数据目录，以原任务资格、原候选和保存意图执行 Reconcile/Drain。

提交前退出：Memory 状态保持 unknown，任务不完成，无新操作或 Memory 记录。提交后退出：恢复原 committed 回执并完成原任务，集合只有一项变更。确认后退出：核对不改变已确认任务版本，也不重复写入。所有用例均使用有限超时及私有临时目录，凭证不进入日志。

`evidence/20-memory-save-process-first.log` 记录首次运行；临时跳过实际提交后，after-commit 用例明确失败为 unknown 而非 committed（`20-memory-save-process-mutation.log`），变异随后恢复。`20-memory-save-process-race.log` 为整个 extractioncheck race 通过，`20-memory-save-process-final.log` 覆盖新增确认版本不变断言；go vet/diff 检查通过。这补齐保存提交相关进程故障证据，尚不代表持续扫描、独立任务使用、永久失效/清理或整票验收完成。

### 候选来源的持久修订屏障

候选库新增可信 `CandidateInvalidator` 接口，采用第 19 票受控产物相同的“来源截至某修订永久失效”语义。SQLite 持久保存单调屏障，并与候选提交共用事务写锁；一次事务内退休所有相关候选、清除保存意图正文、保留原提交事实和保存操作身份。混合来源整体退休，不能只删标签。

已验证关闭重开、重复事件、较旧事件不回退屏障、新操作不能重建旧修订、独立命名空间以及新修订可接纳；两个实际 SQLite 连接的 12 轮提交/失效竞争也通过。每次扫描受现有 512 候选上限、每记录 16 KiB 和事务超时约束；来源屏障另有 512 身份上限，不静默淘汰。修订以十进制文本保存并按 uint64 解析，避免 SQLite 整数范围或浮点转换影响比较。

证据为 `evidence/20-source-fence-{red,green,race}.log`，相关 go vet/diff 检查通过。返回数量仅表示本库新退休候选，不能当作 Memory 或其他消费者的清理确认。来源事件的宿主接线、已保存派生 Memory 清理及持久消费者确认仍待实现，本票保持进行中。

### 提取与 Memory 共用持久来源屏障

`extractionsourceguard.Guard` 在来源读取、候选释放检查及 Memory 来源检查前后复核 `SourceFence`，底层来源仍负责当前文件、用途和位置限制。参考宿主统一接入该适配，因此恢复相同来源配置不会清除候选库保存的永久失效修订。

真实文件/SQLite 用例验证：来源屏障写入后关闭重开数据库、重新构造原来源配置，提取读取和 Memory 的 store/process/discover/disclose 均拒绝。临时移除屏障拒绝逻辑后用例明确失败，随后恢复（`evidence/20-source-guard-mutation.log`）。`20-source-guard-green.log`、`20-source-guard-core.log` 记录相关 race 回归。

Core/SDK 新增实际提交后来源失效故障：Memory 原操作仍为 committed，但 ContentAvailability 为 unavailable；候选和保存意图退休，任务返回 FAILURE/CONFIRMED 且无结果引用。整个 extractioncheck race 通过，见 `20-source-guard-memory.log`。相关 vet/diff 检查通过。

此处仅完成新使用阻止与参考宿主接线。Memory 原记录正文的实际删除、外部来源事件持久订阅和各消费者清理确认尚未完成，不能将不可访问报告为已清理。

### 退休候选对应 Memory 正文的实际清理

`extraction.Cleaner` 对已退休且原保存已确认的候选运行单次有界清理。删除前用 `CleanupJournal` 持久关联原 Memory DeleteRequest；原请求包含固定引用、期望修订、用途和操作身份，重开或重试不能替换。当前 Memory 删除授权与原删除状态通过既有服务核对，不从来源正文获取权限。

真实 Core/SDK 提取保存后使来源失效，再由清理器调用实际 Memory Delete。验收先检查数据库确有正文，删除后 Read 返回 Missing；重复清理核对同一事件，Memory 变更仅保存和删除两项。DeletionReporter 保留 replicas/derived 的 not_covered 状态，未声称所有消费者已清理。证据：`evidence/20-memory-cleanup-{red,green,race}.log`。

清理请求重开测试验证：活跃候选不能登记删除、原请求持久不变、替代操作/期望修订拒绝、跨主体拒绝。首次使用 reflect 比较包含 Protobuf 内部状态导致测试断言失败；改为公开字段与 proto.Equal 后通过，分别记录在 `20-cleanup-intent-test-comparison-failure.log` 和 `20-cleanup-intent-reopen.log`。相关 vet/diff 检查通过。

仍待清理链补项：实际删除回包丢失/退出恢复、持久待办枚举和消费者确认、来源事件调度。若原 Memory 后来被纠正，删除期望修订冲突会保持未完成，不自动删除新修订；完整来源生命周期仍须处理该情况。本票整体验收尚未完成。

### 删除清理的回包丢失与未知提交

`CheckSaveFault` 新增 `cleanup-lost-reply` 与 `cleanup-before-delete`，均先通过实际 Core/SDK 完成来源提取和保存，再使来源失效。故障只发生在真实 Memory 删除的提交边界：丢回包先执行 SQLite Delete；提交前未知保留已发生的真实授权接纳。

丢回包用例恢复原删除事件，正文实际消失，重复清理保持原操作且只有一次删除。提交前未知用例保持 unknown、无完成报告，正文仍在，重复调用既不生成替代删除操作也不声称已清理。报告对未接入消费者仍为 not_covered。

证据：`evidence/20-cleanup-fault-{red,green,regression}.log`；整个 extractioncheck race 和相关 vet/diff 检查通过。当前为明确提交边界故障；删除过程的真实进程退出、持久待办调度、消费者确认和整票验收仍未完成。

### Memory 清理的真实进程退出恢复

`TestCleanupRecoversOriginalDeletionAfterProcessExit` 先通过实际 Core/SDK 提取保存，再使来源失效；子进程分别在删除接纳后提交前、实际 SQLite 删除后回包前、删除确认后以 `os.Exit(93)` 退出。父进程重开同一授权、候选和 Memory 数据库，使用已持久化的原清理请求核对。

删除前退出保持 unknown、正文仍在，不能声称已清理；删除后或确认后退出恢复同一删除事件，正文确实缺失。重复调用始终使用原删除操作，变更数量分别为一项（保存）或两项（保存及删除）。

首次运行见 `evidence/20-cleanup-process-first.log`。临时跳过实际 SQLite Delete 后 after-delete 用例失败为 unknown 而非 committed，见 `20-cleanup-process-mutation.log`，变异已恢复。整个 extractioncheck race 通过，见 `20-cleanup-process-race.log`；相关 vet/diff 检查通过。持久待办调度、消费者确认、来源纠正后的清理范围、独立任务使用及整票验收仍待完成。

### 重启后的退休保存记录发现

`CleanupDiscovery.ListRetiredSaves` 按命名空间和主体返回稳定排序的退休保存身份，每页 1–16 项，无候选正文。实际 SQLite 重开测试覆盖分页、活跃记录排除、主体/命名空间隔离及超限拒绝。真实清理 profile 已通过发现结果选取记录，再用原删除状态执行核对。

此列表包括已删除和尚未删除记录，不把出现/消失当成消费者完成证据。宿主完成一轮后必须从头开始下一轮，以覆盖并发退休且排序早于旧游标的记录；接口本身不启动扫描循环，也尚未提供持久调度游标。

证据：`evidence/20-cleanup-discovery-{red,green,core}.log`；相关 race、vet/diff 检查通过。持续触发、持久清理调度和消费者确认仍待完成。

### 持久清理游标与单轮工作者

`CleanupProgressStore` 按命名空间、主体、消费者及配置摘要绑定调度游标，SQLite 使用版本比较后推进，拒绝过期写入及配置变化后冒用旧进度；最多保留 64 个绑定，不静默淘汰。关闭重开、版本冲突和配置冲突测试通过，见 `evidence/20-cleanup-progress-{red,green}.log`。

`CleanupWorker.Run` 仅运行显式调用的一轮，整体 5 秒、每轮最多 16 个身份。逐项沿原删除操作核对后才推进游标；崩溃前已发生的效果仍由原删除请求恢复。空页将下一轮游标归零，以重新覆盖排序较早的新退休记录。逐项保留 Memory 删除报告和受限错误码，不把游标当作消费者清理完成证明。

实际 Core/SDK 提取保存、失效及 Memory 删除链已通过工作者执行；替换工作者对象从持久游标读到页尾，下一轮再从头核对原删除，数据库仍只有保存/删除两次效果。证据：`20-cleanup-worker-{red,green}.log`；相关 race、vet/diff 通过。工作者进程中途退出、错误项公平推进及完整消费者确认仍需后续验收；未引入隐含后台循环或持续来源扫描。

### 清理失败项的公平推进与逐项超时

新增真实双任务提取保存用例，随后共同来源失效。第一项清理意图存储不可用时，工作者仍推进到第二项并实际删除其 Memory；故障解除、页尾复位后，第一项在下一轮完成。临时改为失败项不推进游标后用例明确失败，见 `evidence/20-cleanup-fairness-mutation.log`，变异已恢复。初始接口红灯、首次通过和整组 race 分别见 `20-cleanup-fairness-{red,first,green}.log`。

进一步让第一项等待上下文截止，复现整轮 5 秒耗尽后无法提交游标的问题（`20-cleanup-item-timeout-red.log`）。工作者现为每项设置 1 秒上限，开始新项前预留检查点提交时间；不足时间时返回已处理的有界部分，不将失败视为已清理。故障项、后续项及下一轮恢复均通过，整个 extractioncheck race 见 `20-cleanup-item-timeout-green.log`；相关 vet/diff 通过。

调度进度仍不等于消费者清理确认。来源持续触发、独立任务实际使用、更多出口治理及整票验收仍待完成。

### 显式持续触发登记

新增 `TriggerRegistry` 与 SQLite 触发登记：固定来源范围、`source.changed` 条件、有效期、总轮次上限和每轮步骤上限。登记调用独立检查当前 source.read/extract/scan 权限，只有读取或提取权不能启用持续扫描。登记不执行任务、不读取来源正文，也不创建监听循环。

真实授权服务用例先证明默认权限登记被拒且不落盘，再通过显式策略与 continuous grant 开通扫描后成功。第二个 SQLite 连接读取到原配置；同一 ID 重复登记不重置预算，修改预算拒绝，已取消登记不能重新激活。取消接口目前是可信宿主存储控制口，尚非完成的 SDK 取消入口。

登记最多 64 项、来源最多 16 项、配置最多 16 KiB、有效期最长 24 小时；轮次/步骤目前为不可变的登记约束，实际触发预算扣减及任务提交仍待接入。证据为 `evidence/20-trigger-registration-{red,green,regression}.log`，相关 race、vet/diff 通过。事件去重、原任务关联、逐次权限检查、撤权/过期停止及取消竞争尚需后续实施，本票保持进行中。

### 来源修订事件去重与持久轮次扣减

`TriggerRounds` 以固定来源 kind/key/revision 作为事件身份，在一个 SQLite 写事务内绑定完整原 Core Submission 并扣减一次轮次。重复事件保留原提交操作、输入引用和任务约束，替换请求拒绝；不同事件不能复用同一提交操作。新轮次检查登记状态、来源范围、期限、步骤预算及永久来源屏障，参考路径不允许模型请求预算。

登记最多 64 轮、全库最多 512 个轮次，每个请求最多 16 KiB；轮次保留，不靠记录淘汰重置预算。真实重开测试证明去重和预算持续，取消后不能登记新轮次；两个实际 SQLite 连接同时争用最后一轮时仅一个成功。

证据：`evidence/20-trigger-round-{red,green,concurrency}.log`；相关 race、vet/diff 通过。现有轮次读取仅用于原事实恢复，不代表当前执行许可；实际 dispatcher 仍需逐次复核扫描授权、取消与有效期，再提交原任务。真实事件到 Core 提交、任务执行前取消/撤权防护及触发进程恢复尚未接入，本票保持进行中。

### 事件到 Core 的原任务提交与当前扫描许可

`TriggerDispatcher.Dispatch` 在有限宿主事件调用内读取登记、检查当前 scan 权限和来源屏障，准备受控元数据输入，持久绑定原提交请求，再复查当前状态后调用真实 Core Submit。重试先读取已有轮次，提交相同请求；输入准备失败或竞争失败不得执行来源读取或业务动作，未采用的受控元数据输入按原保留期限处理。

新增 `TriggerGuard` 可装配到提取执行和保存阶段，将登记有效/未取消及当前持续扫描许可与普通阶段许可共同检查。真实 Core 用例证明重复事件返回同一任务与版本，并在 SDK 已接纳、Execution 尚未开始时取消登记，实际执行不产生候选或成功结果。临时移除执行阶段登记检查后该用例失败，变异已恢复。

证据：`evidence/20-trigger-dispatch-{red,green,regression}.log`、`20-trigger-execution-cancel.log`、`20-trigger-execution-cancel-mutation.log`；相关 race、vet/diff 通过。

仍待完成：触发任务与事件的执行侧强绑定及完整输入范围检查、触发的实际提取/保存成功链、撤权/过期/取消竞争、提交未知及真实进程恢复、调度入口和消费者确认。登记/轮次原事实读取不等于当前执行许可；本票尚未整体验收。

### 触发执行绑定原任务、原输入与登记范围

`extractionexecution.BindTrigger` 仅接受已装配匹配 TriggerGuard 的提取驱动。开始前从持久轮次取得原提交操作，通过真实 Core LookupOperation 确认任务身份及原输入引用，并拒绝扩大原任务预算。输入按严格 JSON 解析，每个来源必须在登记范围内、不得重复，并包含原来源事件修订；来源屏障与普通执行许可仍分别检查。原效果核对不以取消为由伪称未发生。

真实用例验证另一个已存在任务不能借用触发登记、替换输入引用拒绝，大小写别名/重复字段/重复来源拒绝；有效触发则通过真实 Core/SDK/Execution 完成本地偏好提取并生成候选，取消前执行仍被阻止。证据：`evidence/20-trigger-binding-{red,green,regression}.log` 与 `20-trigger-bound-execution.log`；相关 race、vet/diff 通过。

此处成功链止于候选；触发后的自动 Memory 保存、跨任务使用、触发提交回包未知/进程恢复、完整撤权/过期竞争、持续调度和消费者确认尚需补齐。本票仍未整体验收。

### 触发任务自动保存与保存前取消

参考集成将已绑定原任务的触发驱动经 `WithMemory` 接入实际 Memory Service，Saver 使用同一个 TriggerGuard，保存阶段同时受当前扫描许可与普通保存许可约束。真实 Core/SDK/Execution 用例完成来源读取、偏好提取和自动保存后，核验 Memory 正文恰为 format/concise、类型为 preference、来源与原事件修订一致；重复保存核对同一回执，实际 Memory 变更仅一次。

在真实候选提交后、自动保存前取消持久触发登记，候选已提交事实仍可核对，但没有创建保存意图、没有 Memory 写入，也没有成功结果引用。本测试在实际驱动和服务之间注入取消，不预设候选或 Memory 成功响应。宿主仍须将 TriggerGuard 明确装配到 Saver；本次不宣称所有可能装配均自动获得该约束。

接口红灯与初次通过见 `evidence/20-trigger-save-{red,green}.log`；强化正文/来源断言后的整组 race 见 `20-trigger-save-regression.log`（extractioncheck 24.337 秒，驱动与 extraction 包缓存通过），相关 vet 与 diff 检查通过。

跨任务实际使用、触发提交未知和进程恢复、完整撤权/过期竞争、持续调度及消费者确认仍待完成；此次为触发保存集成验证，不是整票验收。

### 触发提交的真实进程恢复

`TestTriggerSubmissionRecoversAfterProcessExit` 在真实子进程中登记仅允许一轮的触发，准备受控输入并持久化原轮次，在调用 Core 前或 Core 实际提交后立即 `os.Exit(94)`，不执行正常关闭。父进程重新打开授权、Core 和候选 SQLite 存储后，重发相同来源修订事件。

提交前退出时先确认原提交操作在 Core 中为 NotFound，再沿已保存的原请求提交；提交后退出时确认原任务已存在，恢复返回相同任务身份及版本。两条路径均核验完整 Submission 未变、原操作仍映射该任务、重复事件不推进任务；新修订事件因一轮预算已占用而返回 Capacity，重启不能重置轮次。提交后退出覆盖成功回包尚未交付调用方的边界，未将其误报为未提交。

证据：`evidence/20-trigger-recovery-red.log`（缺少测试装配函数的编译红灯）、`20-trigger-recovery-green.log`（实际进程 race 验证，1.829 秒）；相关 vet 和 diff 检查通过。复用既有持久轮次与 Core 原操作恢复实现，本次未修改生产协议。来源事件调度、撤权竞争、跨任务使用及完整消费者确认仍未整体验收，本票保持进行中。

### 候选提交后扫描撤权与触发过期

沿真实触发任务自动保存路径，在候选已提交、Memory 保存尚未开始的边界分别移除当前策略的 `memory.scan`，或将参考时钟推进到登记有效期边界。两种情况下均确认普通读取、提取、候选保留、保存和披露权限仍获准；候选提交事实仍存在，但不生成 Memory 保存意图、不写入 Memory、不返回成功结果引用。

临时移除 TriggerGuard 的阶段性当前状态复查后，撤权用例错误保存成功，过期用例生成保存意图，两个用例均明确失败。变异已恢复，生产代码与该变异前完全一致。证据为 `evidence/20-trigger-revoke-save-{red,green}.log`、`20-trigger-revoke-expire-green.log`（race，2.687 秒）及 `20-trigger-revoke-expire-mutation.log`；相关 vet/diff 通过。该验证覆盖已提交候选到保存的边界，不能替代调度停止、所有并发释放点及整票验收。

### 普通提取输入的无歧义来源身份

真实来源、授权与候选存储测试复现普通驱动使用标准结构体 JSON 解码时接受重复字段和大小写别名，四类输入实际返回成功；重复来源则进入提取阶段后返回 Unavailable。现普通入口使用有界严格 JSON 解析：仅接受精确 `sources` 与 `kind/key/revision` 字段、正整数修订，每个 kind/key 只能出现一次，输入仍限制 8192 字节和 16 个来源。遵守共享 jsonvalue 的数值精度约束；该局部元数据格式不声明支持任意 uint64 的 JSON 数字表示。

上述歧义输入现在在来源读取前返回 Invalid，候选状态仍为 Missing；已提交原调用的事实核对顺序不变。证据：`evidence/20-input-strict-red.log` 与 `20-input-strict-green.log`，后者覆盖 extractioncheck 和提取驱动的 race 回归；相关 vet/diff 通过。修复普通入口不能替代尚缺的跨任务使用、等待状态、持续调度和整票验收。

### 行为记录到暂定推断的自动保存路径

`TestObservedChoicesAutomaticallySaveTentativeInference` 使用三个独立、私有本地文件记录详细报告格式选择，由可信宿主登记精确摘要、主体、来源修订及位置策略。真实来源读取器解析 observed choice，实际规则提取器生成候选，经 Core/SDK/Execution 和 Memory 保存后完成任务；没有直接写入预制推断或使用固定成功驱动。

核验实际 Memory 正文为 format/detailed，类型为 inference，主体为 operator，适用条件为 report，置信为 tentative，依据为三个独立且一致的报告格式选择、方法为 local-rules-v1；三条来源修订、提取方式和定位完整保留。重复保存返回同一回执且只有一次 Memory 变更。参考任务装配现允许传入来源元数据，以保留原普通偏好测试并覆盖多来源推断。

初始装配缺失红灯及目标用例 race 通过见 `evidence/20-inference-save-{red,green}.log`；extractioncheck 整组 race 通过见 `20-inference-save-regression.log`，相关 vet/diff 通过。此处证明受控提取和自动保存成功路径，未证明后续独立任务使用，也不代表模型提取质量或整票完成。

### 独立任务的受权读取与上下文绑定

接入上下文时发现本地来源适配器未支持 Memory 的 `store_reference` 检查，合法本地引用也会被拒绝。现引用保留使用来源的 Storage 限制，仍检查用途、期限及文件当前摘要；Memory 另行检查当前引用保留授权和 discover。该来源实现没有更宽松的“仅引用可上云”策略，不因正文已经保存而扩大引用位置。真实文件驻留检查红灯/绿灯见 `evidence/20-reference-storage-{red,green}.log`。

推断保存成功后，参考宿主创建独立 Core 任务，显式配置 Memory 读取与引用保留许可，签发绑定原修订和完整读取语义的单次凭据。实际 Memory Reader 读回完整原推断，再由 ContextMemory 绑定新任务/决策/事实版本，将获准 report 推断投影为 `tentative-report-format=detailed` 数据块。原提取任务不能借用该上下文授权；相同读取在上下文加载时复用原分配，不增加消耗。

随后使三个来源中的一个修订永久失效，同一上下文加载被拒且不返回正文；不是仅检查候选标签。证据：`evidence/20-later-read-red.log`、`20-later-read-green.log`、`20-later-context-green.log`（目标 race，3.401 秒）；来源及来源屏障包 race、相关 vet/diff 通过。

当前跨任务验证到达受权上下文投影，后续报告决策和受控发布尚未接入，不能据此勾选“实际使用”完整验收。测试宿主固定首个决策和新签名凭据，跨进程保存/恢复该上下文装配也尚待整合；消费者清理、等待状态、持续调度和整票验收仍未完成。

### 推断影响独立报告决策与受控发布

独立报告任务现使用自己的受控文本输入及有限生成预算，经真实 GenerationPort 预留决策，TaskContext 校验当前 Core 事实，ContextAssembler/SQLite 保存受权快照，Brain 读取实际上下文后生成报告。ContextPolicy 登记原使用，Session Lineage 将原 Memory 修订接入输出，Answers Port 在完成任务前复核上下文和产物。

本地确定性契约模型根据实际 `tentative-report-format` 记忆块选取详细报告表述，输出明确称其依据为暂定推断。测试通过 Content READ 读取实际发布正文并比较预期文本及引用，检查输出带有 Memory 来源；随后将行为来源中的一个修订置为永久失效，原上下文加载被拒，已发布报告也不再通过 ValidateResult。Core 历史完成事实不因此撤销；该检查不宣称产物字节已清理。

首次装配红灯见 `evidence/20-report-decision-red.log`。集成时修正了将提取 JSON 输入用于文本报告及 READ 超出实际正文尺寸的问题，定位记录见 `20-report-decision-debug.log`；最终目标 race 通过见 `20-report-decision-green.log`（13.662 秒）。本地契约模型的固定用量仅验证预算核算路径，不是模型效果或真实供应商 token 计费证据，未调用外部模型。

整组 extractioncheck race 通过见 `evidence/20-report-decision-regression.log`，相关 vet/diff 通过。这是推断一类记录的跨任务决策/发布证明；显式偏好的同等路径、更多条件反例、跨进程上下文恢复和消费者实际清理仍需后续验收，不据此完成整票。

### 显式偏好影响独立任务输出

`TestExtractedPreferenceControlsIndependentTaskOutput` 从真实本地明确陈述提取 concise 偏好并自动保存，再创建独立任务，经单次受权 Memory 读取、持久任务上下文、Brain 和受控发布形成简短报告。保存记录仍为 preference/explicit，条件为 answer；可信宿主为该任务配置相应条件与 `explicit-reply-format` 投影，不将显式偏好改写为暂定推断。

发布正文及 Memory 引用均由模型外的测试通过实际 Content READ 检查。契约模型不再因“未读到期待记忆”主动返回测试式失败；无适用记忆时生成独立的默认说明，有 concise 记忆时才生成简短报告。推断 detailed 的原路径同时保留其暂定措辞并回归。两个路径均在来源失效后验证上下文及已发布输出不可继续使用，尚不表示字节清理完成。

初始真实失败是旧投影只支持 inference，证据见 `evidence/20-preference-use-red.log`；第一次两路径 race 通过见 `20-preference-use-green.log`；使默认输出与简洁偏好输出区分后的最终两路径 race 通过见 `20-preference-use-final.log`，相关 vet/diff 通过。本扩展不计入模型质量指标；条件不匹配反例、上下文进程恢复、实际消费者清理及整票验收仍待完成。

### 可读取但不适用的记忆不影响报告格式

新增已保存 answer 条件偏好与宿主 report 条件不匹配的反例。沿原 Memory 读取授权可取得记录，但 TaskContext 实际传给 Brain 的输入没有 Memory 正文块，只有一个 inapplicable 状态；对应正例仍要求恰好一个 Memory 块且无不适用状态。验证通过 Session 的公开 Assemble 接口观察真实输入，不以模型自述作为依据。

模型外读取实际发布结果，条件不匹配时须为独立默认正文，引用普通输入而非 Memory；正例须为记忆决定的正文并引用 Memory。参与适用性评估的记忆仍作为派生依赖保留，原来源失效后输出停止可用，不能通过丢掉标签绕开来源治理。此反例不声称默认输出依赖记忆正文，也不声称已清理产物字节。

原测试只接受适用结果的真实红灯见 `evidence/20-condition-use-red.log`；条件反例、偏好正例和推断正例三路径 race 通过见 `20-condition-use-green.log`，相关 vet/diff 通过。上下文进程恢复、实际消费者清理、等待状态、持续调度及整票验收仍未完成。

### 报告上下文与产物的实际清理及消费者确认

来源永久失效后，参考宿主通过退休保存记录发现原 Memory 操作，沿原清理请求实际删除 Memory，再由两个具名 SourceConsumer 消费真实 Memory 删除事件：report-contexts 退休 SQLite 上下文，report-artifacts 使派生产物失效并运行有界文件清理。消费者配置摘要绑定数据根、消费者角色和实现版本，确认位置写入实际 Memory 消费进度存储。

测试先读取真实上下文及报告文件证明内容存在；执行后检查 Memory 正文为 Missing、上下文为 Invalidated，实际文件列表中不再有原报告 blob。删除报告在消费前对两个目标返回 pending，消费完成后才返回 applied；重建消费者对象读取原持久位置，没有重复应用事件。再次沿原 Cleaner 请求核对相同删除事件，未接入副本继续为 not_covered。

此范围是已接入的报告上下文/产物及 Memory 活跃正文。上下文退休不宣称擦除 SQLite 空闲页、WAL 或历史备份；授权比较记录、其他派生出口、上下文进程恢复及完整消费者库存仍需整体验收。现有历史任务完成事实保留，不把删除解释为任务从未发生。

初始装配缺失红灯及偏好路径 race 通过见 `evidence/20-report-cleanup-{red,green}.log`；补充 Memory 正文删除断言后的偏好、推断及条件反例三路径 race 通过见 `20-report-cleanup-regression.log`，相关 vet/diff 通过。本票仍保持进行中。

### 本地不支持结果的原操作持久事实

新增可信宿主 DeclineStore 接口，沿原提取操作与调用摘要记录 unsupported 状态，Committed 固定为 false，原因仅允许 unsupported、insufficient_evidence、inconsistent_evidence。状态使用既有操作屏障存储，不创建新身份或候选正文；与候选及退休屏障共享 512 条容量，不通过淘汰重置原身份。

SQLite 迁移为既有退休事实添加空原因，不把旧数据推断为不支持结果。重复写入同一事实幂等，改变原因或调用摘要拒绝；不支持后提交候选被屏障拒绝，已提交候选也不能被改成不支持。两个实际 SQLite 连接竞争八轮，候选和不支持事实始终只有一个成立；重开及后续退休保留原事实。

证据：`evidence/20-unsupported-store-{red,green,regression}.log`，存储和驱动包 race、相关 vet/diff 通过。此处仅完成状态持久接口，尚未将非产出结果接入提取驱动、SDK 查询及任务状态，不宣称“能力不足”端到端验收已完成。

### 不支持结果接入受控执行与查询

提取驱动现在要求持久 Outcomes 接口。真实规则返回零候选时，重新检查来源与提取权限，再沿原调用保存受限原因码；已经记录的不支持操作只返回原事实，不重新读取或提取。Inspect 以 NOT_OCCURRED/FAILURE 表达未产生候选，在当前披露获准时通过受控产物提供 unsupported 状态和原因，内容仅含原操作身份及受限元数据。

自动保存包装器在确认原候选未产生且没有保存意图时保留该事实，不再将缺少 Memory 保存误判为未知。真实两条行为记录产生 insufficient_evidence：SDK 可查询原调用，受控 Content READ 可读取原因，没有候选或 Memory 写入；原任务不完成，重试原调用保留结果身份。这里 NOT_OCCURRED 指候选/Memory 效果未发生，不否认曾进行获准来源读取。

披露撤权反例复现执行层对无正文失败结果仍创建空正文产物的问题。finish 现对携带内部事实但没有正文的失败结果不调用产物保存；原不支持事实和未发生效果仍可核对，但不发布原因引用。此修改也适用于同类执行失败，未引入远程模型或替代执行路径。

证据：`evidence/20-unsupported-task-{red,green,final}.log`、`20-unsupported-disclose-red.log`、`20-unsupported-execution-regression.log`；最后一项中 execution 包无独立测试，驱动和存储包 race 通过，实际执行行为由 profile 测试覆盖。`20-unsupported-profile-regression.log` 记录执行与提取整组 race 通过（20.377 秒、68.344 秒），相关 vet/diff 通过。多候选、任意第三方提取器或存储故障不因此被擅自归类为不支持；更多恢复、调度及整票验收仍待完成。

### 不支持事实与确认边界的真实进程恢复

`TestUnsupportedAttemptRecoversAfterProcessExit` 在已获 SDK 接纳的真实任务中，于进入提取驱动前、Decline 事实实际落盘后但尚未回报 Core、以及回报并 Drain 后分别 `os.Exit(95)`。父进程重新打开实际授权/Core/候选/Memory 存储，恢复原本地来源提供者但不重建来源文件，再沿原调用 Reconcile。

事实未落盘时维持 UNKNOWN，任务保持 WAITING，后续 Run 不再执行该调用，也不写出新的不支持事实。事实已落盘时恢复为 NOT_OCCURRED/FAILURE，保留原原因及调用摘要；已确认任务重启后不推进版本。所有路径均核对原输出操作和结果引用，不产生候选或 Memory 保存意图/变更。恢复了可执行的真实来源，因此“没有新事实”的断言不依赖来源不可用而碰巧成立。

证据：`evidence/20-unsupported-process-{red,green,final}.log`；最终覆盖全部不支持任务及三个进程退出路径的 race 测试通过（4.334 秒），相关 vet/diff 通过。上下文装配恢复、持续调度和完整出口/消费者库存等其余整票要求仍需完成。

### 统一实现证据入口与固定质量 CLI

新增 `cmd/extractioncheck` 的 local-rules-quality-v1 CLI，输出固定数据集摘要、逐样本结果、质量指标、实现/位置及 Go 版本，明确外部模型请求为零。未知 profile 或多余参数返回退出码 2；质量失败和运行错误返回非零。运行说明见 [验证入口](20-verification-guide.md)。

`scripts/verify-extraction.sh` 构建 CLI、运行相关 vet、串行包级 race/真实进程测试，再生成固定质量报告。阶段失败中断后续工作，通过 EXIT trap 保存实际状态；每次开始清除旧报告，避免沿用上次成功结果。报告固定标明 ticket_acceptance=not_evaluated，不替代原票完整验收、仓库完整 verify 或双轴审查。

实际运行四个阶段均通过：8 个包、110 个测试及子用例通过，0 个失败；6 个仅供子进程调用的探针在普通枚举中跳过，其父测试已实际运行子进程。质量为 14/14 精确匹配，6/6 正例输出正确，缺失与误报均为零，范围仍仅限预先固定语料。原始证据归档为 `evidence/20-unified-tests.jsonl`、`20-unified-stages.json`、`20-unified-quality.json`；CLI 单独运行及非法 profile 验证见 `20-quality-cli.json`、`20-quality-cli-invalid.log`。启动时清理旧阶段文件的补充通过 shell 语法检查；本次证据不是整票完成证明。

### 候选保留期限收缩与任务输入边界核对

任务更新 `UpdateService` 的 append/reply/revise 都只向 InputRefs 追加引用；revise 不接受替换 InputRefs。因此“任务移除原输入后，触发驱动仍读取它”的假设目前不可经公开更新接口触发，未据此修改触发绑定。若将来增加输入删除/替换，须同时评估新执行的当前输入资格与历史效果核对，不能通过禁止 Inspect 丢失已发生效果。

新增可信宿主配置 `WithRetentionDeadline`：以绝对截止时间收缩提取候选期限，多次配置取更早值，再与来源期限取交集。过期配置在新操作读取来源前拒绝；已产生的操作仍沿原事实核对。实际三条独立选择提取出推断，真实 SDK 执行后候选及 Memory 的期限均从来源一小时收缩到五分钟；再次配置两小时不能延长原期限。五分钟到期后，原候选效果仍 CONFIRMED，但不再披露结果。另一真实任务验证过期配置无候选、保存意图及 Memory 变更。

此配置证明更严格保留期限可以贯穿当前保存路径，不代表已完成候选专属计算/披露策略、到期物理清理或宿主配置的进程恢复。候选、来源与 Memory 当前权限仍需分别检查。定向测试先因缺少接口失败，实现后两项 race 测试通过；回归证据见 `evidence/20-retention-regression.log`。之前统一验证证据保持原状，不将其冒充包含此次变更的结果。

### 当前来源快照的有界轮询入口

`TriggerPoller` 将一次明确宿主唤醒连接至当前修订查询和既有 Dispatcher。单次最多一个登记来源、一个原任务分派、五秒超时。读取元数据前检查当前扫描授权及来源登记，返回后由 Dispatcher 再检查授权、取消、期限与永久来源屏障；仍只由受控任务读取正文。`CurrentSources` 是宿主元数据接口，本地实现从可信清单选择最高修订后检查位置、用途、披露及期限，禁止以旧修订规避最新限制。

真实授权/Core/SQLite 测试证明：重建轮询对象复用原任务；新修订创建独立任务；两轮用尽后重建对象不能重置预算；取消后不再分派。精确竞争测试在真实清单读取后修改实际授权，普通读取/提取权仍有效，但扫描权撤销导致拒绝，且没有触发轮次或任务提交记录。来源接口测试以不存在的正文文件证明轮询只读取清单元数据，同时真实正文读取仍失败，最高修订受限时不会回退，返回值不能修改清单。

原 `TestTriggerSubmissionRecoversAfterProcessExit` 现在通过轮询入口进入提交前/实际提交后的 `os.Exit(94)`，重开真实持久存储后再次轮询，核对原提交、任务身份、版本和预算。定向 race 测试均已通过；整组结果单独保存在 `evidence/20-trigger-poll-regression.log`。本实现采用当前快照语义，不捕获两次唤醒之间全部中间修订；不是内置后台调度器或文件监听器，宿主仍负责可信观察与唤醒。该接口补齐有界扫描分派入口，不单独证明第 20 票其余出口、清理库存及整体验收完成。

### 到期候选进入实际清理队列

此前仅在读取/披露时拒绝过期候选，活动候选和保存意图正文可能继续存在。新增 `ExpiryWorker` 从可信宿主时钟取得截止时间，调用 `CandidateExpiry.RetireExpired`，每轮至多退休 16 项。SQLite 使用与提交、退休及来源失效相同的写锁，按期限和原操作顺序选择本命名空间/主体的到期候选，在同一事务内清除候选与保存意图正文并保留原调用摘要及已提交事实。没有独立分页游标，后续轮次从尚未退休的最早到期项继续。

真实任务先自动保存一分钟期限的记忆：到期前退休数量为零；到期后候选 Lookup 不再返回正文，保存意图仅保留原身份，Inspect 仍证明原效果；重提原候选被永久操作屏障拒绝。现有 CleanupWorker 能发现该退休保存并执行原 Memory 删除，实际 Memory 修订正文不再可读，重复清理保留原删除事件。到期计数与 Memory/消费者清理结果分别判断，不将队列接纳当作清理完成。

另用两个真实 SQLite 连接验证每轮上限、最早到期顺序、命名空间/主体隔离、恰好到期边界、并发只退休一次及重开后状态；未来候选和其他主体/命名空间不受影响。定向测试先因缺少接口失败，实现后 race 通过；相关回归原始结果见 `evidence/20-expiry-regression.log`。该路径清理活动记录，不证明 SQLite 空闲页或备份擦除。Memory 已被纠正到更高修订时，原删除的版本冲突仍需后续按来源依赖解决，不能擅自删除不相关的新内容。

本轮首次并行包回归中，既有 `TestExtractedPreferenceControlsIndependentTaskOutput` 在上下文装配处返回 PERMISSION_DENIED，保留原始失败记录 `evidence/20-expiry-regression-initial-failure.log`。同一用例随后单独运行三次均通过（`20-expiry-preference-recheck.log`），因此暂未得到稳定复现，也没有据此修改权限或放宽超时；后续按统一验证脚本的 `-p 1` 串行包设置重跑。单独复查通过不能证明该偶发失败的根因已修复。

串行包重跑最终全部通过，原始结果为 `20-expiry-regression.log`；相关 vet 与 diff 检查通过。首轮失败记录继续保留，偶发上下文装配拒绝仍待定位，不将此次重跑解释为根因修复或整票验收。

### 上下文装配偶发拒绝的后续定位

现已通过单 CPU 多进程稳定复现，定位为装配自身预算耗尽后，下游失败关闭结果被误报为权限拒绝。Assemble/Validate 在自身上下文结束时统一返回不可用，禁止正文释放并保留原快照核对路径；五秒预算和授权要求不变。真实保留复查中的定点取消先红后绿，压力复测不再出现权限拒绝，但超预算运行仍失败为不可用。完整命令、原始日志及解释见 [诊断记录](20-context-timeout-diagnosis.md)。因此此前“拒绝原因未定位”的状态由本节更新，而不是删除原失败证据。

### 触发登记的受权取消入口

补充 `TriggerRegistry.Cancel`，将原先仅可信存储层可调用的取消操作接到当前身份与权限检查。`extractionauth` 的 `cancel-scan` 阶段只检查独立动作 `memory.scan.cancel`；注册所需的读取、提取、扫描权限不授予取消，也不是取消的前置条件。方法检查原登记所有者、位置、用途并在写入前复查控制权限，不要求登记仍活跃或未过期。原登记永久取消、重复请求沿原身份幂等，已有候选及保存事实不因此被改写。

真实授权/SQLite profile 先验证只有扫描权时取消失败且登记未变，再发放独立控制权，撤销全部读取/提取/扫描权限并推进时钟至登记过期。所有者仍能取消，伪造主体被拒绝，重复取消成功；撤销取消权后连重复请求也被拒绝。测试先因缺少接口失败，实现后 race 通过。相关整组结果见 `evidence/20-trigger-control-regression.log`。该入口是宿主 Go API，不声称已经发布跨端取消协议，也不替代现有任务取消及实际效果核对。

### 触发输入的真实来源标签与可复用准备器

整体验收核对发现，原 profile 的输入准备器虽然在 JSON 中写入触发事件，Content 来源标签却沿用了固定 note/one@1，无法证明后续修订的元数据遵守自己的来源策略。先将本地 Content 策略推进到第二修订，轮询测试立即因旧标签被拒绝，确认该缺口。

新增 `adapters/extractioninputs.Preparer` 作为 `TriggerInputs` 的可复用实现，参考宿主改为调用它。实际事件必须属于登记范围；正文、来源标签使用同一个精确修订，输入期限与单轮任务期限取登记期限和五分钟上限的较早值。Content 独立验证当前权限及来源策略，返回的受控记录需匹配准备的 Spec。输入存储操作与原任务提交操作分开，不读取源正文或授予提取权。

第二修订的实际 Content GET 证明来源标签已更新且输入期限未超出任务期限。已有恢复测试也补充了第二修订的真实元数据授权，之后仍保留“重启不能重置一轮预算”的原断言；首次失败记录见 `evidence/20-trigger-input-initial.log`，修正后全部触发相关 race 测试通过，见 `20-trigger-input-green.log`。未借放宽策略绕过来源标签检查。统一验证脚本增加该适配器，并继续将固定语料质量与契约/故障结果分别报告；整票的来源清理完整性和最终门禁仍需完成。

更新后的专项验证入口实际四阶段均通过：118 个测试及子用例通过、0 失败；6 个子进程探针仅在普通枚举中跳过，其父测试仍实际执行进程退出路径。8 个含测试的包通过；新输入适配器无独立测试，其真实行为由 extractioncheck profile 覆盖。固定数据集仍为 14/14 精确匹配、6/6 正例正确、零缺失/误报，没有调用外部模型。原始事件、阶段和质量报告分别保存在 `20-trigger-input-tests.jsonl`、`20-trigger-input-stages.json`、`20-trigger-input-quality.json`，报告继续标记整票验收未评定。

### 来源变更先失效、后启用

新增本地来源 `Change` 入口，将权威旧修订失效事件与替代清单项串联。替代项在任何变更前完整验证，必须属于同一来源且使用更高修订；已有更高配置与请求不一致时拒绝，避免旧变更覆盖新配置。持有提供者配置锁期间，先调用持久失效接口退休候选/保存意图，再检查新修订未被其他失效事件覆盖，最后启用新项。删除事件只移除截至该修订的登记，保留之后的修订。原始源文件的所有权与删除仍归宿主管理。

真实受控任务先自动保存记忆，再以新修订收紧来源保留期：旧候选和保存意图被退休，新修订可经 Guard 读取；重复变更不重复退休。恢复旧原始清单后，旧来源仍被持久屏障拒绝，原 Memory 可由现有清理器实际删除。精确故障注入覆盖失效提交前失败与实际提交后丢回包，两者都不启用新配置；后者保留已提交屏障，恢复时核对原事件后继续。来源删除也通过同一入口移除登记。轮询参考宿主已改用 Change 推进修订。

定向测试先因缺少接口失败，实现后通过，证据 `evidence/20-source-change-targeted.log`；相关整组结果见 `20-source-change-regression.log`。此入口不替代宿主清单持久化，不接受来源正文或模型生成的失效指令。仍须继续核对原始来源派生的 Content 元数据是否统一接入永久屏障，以及已纠正 Memory 的清理边界；不能仅凭候选退休推断全部出口和消费者已完成治理。

### 原始来源的 Content 出口接入永久屏障

真实任务输入验证发现：即使候选存储已持久化来源失效，Content 自身尚未收到清理事件且旧来源策略仍允许时，GET 仍返回旧输入元数据。测试以零候选的来源失效事件重现，说明不能只根据“有候选被退休”来决定是否联动其他出口。

新增 `extractionsourceguard.ContentPolicy`，在 Content 当前来源策略检查前后复查同一候选来源屏障，映射拒绝/不可用错误，并转发已有同步授权视图。无需读取原始源文件。参考宿主先打开候选存储再组装 Content，执行输入/产物及报告回退来源统一使用包装后的策略。当前策略允许旧来源也不足以越过已持久化的失效状态。

测试先观察到旧 GET 错误成功，接入后 GET/READ 都在 Content 自身清理之前拒绝且不返回正文或元数据。随后交付真实 Content 来源失效事件并执行清理，检查待清理数量归零、实际 blob 列表不含原输入。相关整组证据见 `evidence/20-content-fence-regression.log`；拒绝释放与实际清理分别留证。事件交付/消费清单仍需完整核对，这一包装器本身不是后台清理调度器，也不表示已完成全部跨存储恢复与消费者确认。

### 原始来源清理通知的持久恢复

来源失效与 Content 通知之间的退出窗口通过持久屏障发现关闭：新增 SourceFenceDiscovery、SourceCleanupWorker 及 Content 清理适配器。SQLite 从已有 source fence 表恢复最新截止值，来源身份摘要仅用作稳定分页游标；无须新增业务操作身份或复制候选正文。工作器复用现有配置绑定和 CAS 进度，有界交付后继续其他来源，扫描末尾回绕，失败可重试。原始来源屏障继续在释放路径即时拒绝访问，清理完成则由具体接收器执行并核对。

测试使用实际 Core/SDK 输入、SQLite 和文件 Content：在没有候选时先提交来源屏障，再以进程退出码 96 中断通知；重启后发现来源并删除原文件。另验证截止值提高后的分页/重开恢复、单来源通知失败后的公平性和恢复重试。核查时发现默认输入小于内联阈值，因此强化旧 Content fence 用例和新恢复用例，使用实际落盘输入并验证清理前文件存在；此前只检查文件不存在的断言不足以单独证明文件删除。

此增量仍不表示整票验收或全部消费者清理完成。调用方需为来源清理配置独立 Consumer/配置摘要并定期唤醒；完整消费者覆盖与最终全量验收另行记录。

本次 `scripts/verify-extraction.sh` 四阶段全部通过：126 条测试/子用例通过、7 条仅供子进程入口在父进程跳过、8 个测试包通过、2 个适配器包无独立测试；固定集 14/14 精确匹配，外部模型请求为零。原始事件及阶段/质量报告见 `evidence/20-source-cleanup-{tests.jsonl,stages.json,quality.json}`，初次内联输入断言失败及修正结果见 `evidence/20-source-cleanup-initial.log`。

### 纠正后的 Memory 逐修订擦除（本地路径）

新增可信宿主 `memory.SourceErasureStore` 和 SQLite 实现，按原始来源身份与截止修订擦除整条匹配 Memory 修订，不改写其中的标签或字段。来源屏障、正文移除、原写入比较摘要清除与精确 `SourceErased` 事件同事务提交，并与 Put/Correct 共享写锁。匹配历史修订与不匹配的新修订可共存；独立来源修订不被擦除。写入提交检查同一永久来源屏障，零匹配擦除也保留屏障。

Memory 头部、写入预期修订和 Query 的最新选择改以原操作历史判断，避免正文移除后退回较旧修订。精确 Delete 同样使用原操作头部，防止擦除修订 2 后误把尚存修订 1 当作当前版本。原删除语义与原操作不变；逐修订擦除不伪造 Delete 提交。

`extractioncleanup.NewMemory(store)` 可接既有 SourceCleanupWorker，其 Complete 只表示本地正文擦除且事件持久化。真实 Core/SDK 自动保存后，独立授权 Memory.Correct 到第二版，再持久来源失效并运行该工作器，两个派生正文均已实际擦除，原 Put 仍可核对 committed。原复现测试已迁移到新入口，没有把原冲突错误改名为成功。

当前精确事件下游仍待接入：Content、Context 和授权比较材料消费者遇到 SourceErased 返回未完成，不误当作“纠正到下一版”并推进水位。现有备份恢复协议仍按连续操作序列和整记录删除验证，尚不能解释交错的精确擦除事件；含此类事件的恢复保持拒绝/隔离，不报告已恢复。后续需补精确消费者投递/确认及恢复清单，P2 与整票验收尚未关闭。

本增量验证：SQLite/Memory/既有清理适配器 race 回归通过（18.359s/2.047s/1.527s），竞争写入独立测试通过；补充旧恢复入口拒绝后，SQLite 全套 race 再通过（18.920s）。提取完整 profile race 通过（78.478s），相关 vet 与 diff 检查通过。证据为 `evidence/20-source-erasure-{regression,concurrency,storage-final,profile-regression}.log`。历史头部断言曾确实失败，见 `20-source-erasure-head-red.log`；修复后验证使用原操作头部。未运行最终全仓验收，无外部模型请求。

### 精确修订事件的授权比较材料清理

`memorycleanup.Admissions` 现支持 SourceErased。接收器先通过 `ErasedRevisionStore.ErasedRevisionOperation` 核对 SQLite 中该命名空间、Memory 引用、精确修订和事件位置的持久擦除记录，再清理对应原操作的授权比较材料；不会把事件中的声明直接当作擦除证明。旧整记录删除路径保持原约束。未实现证明接口的其他后端明确返回未完成。

真实自动保存/Correct 测试先验证两个原操作均保留授权比较摘要，再单独交付一个精确事件，证明仅目标摘要被清除。伪造事件位置被拒绝且不改变两个摘要；随后真实 SourceConsumer 消费持久序列并确认位置 4，重放不重复推进，两个已擦除修订均不再保留比较摘要。原授权 reserved 与 Memory committed 事实仍保留，不互相冒充。

该消费者已接入；Content、Context 的精确事件清理及备份恢复扩展仍待完成，本地正文清理结果不等于它们已经确认。

本增量的 SQLite、清理适配器、Memory 授权适配器与提取 profile 全套 race 均通过，相关 vet 和 diff 检查通过；原始输出见 `evidence/20-exact-admissions-regression.log`。定向 red/green 证据保留为 `20-exact-admissions-{red,green}.log`。没有外部模型请求，也未将该回归作为整票最终全仓验收。

### 精确修订事件的 Context 清理

`memorycleanup.Contexts` 已通过 `RevisionInvalidationStore.InvalidateRevision` 接入 SourceErased。SQLite 为精确引用持久化单独的屏障，与原范围水位共享 512 项上限及写锁；依赖该修订的完整快照、原语义比较材料和检查点一起退役。范围失效与精确失效复用同一事务内快照扫描/退役实现，保持原范围语义不变。

装配器的快照依赖检查区分精确修订与范围水位，覆盖 selected、trimmed、inapplicable 状态。精确擦除不抹掉其他修订，也不把缺失事实解释成该正文修订。提交快照时同时检查两种持久屏障，换一个任务/决定身份也不能重新暂存已经失效的修订。

新增测试实际装配并保存修订 1、2、3 的独立快照，只擦除修订 2，验证前后修订的正文及检查点保留；重开后仍拒绝目标修订的新快照。真实进程退出测试增加 after-exact-cleanup：直接退出跳过 Close，恢复后先验证精确屏障仍生效且其他修订可装配，再继续原范围清理回归。定向 red/green 证据见 `evidence/20-exact-context-{red,green}.log`。Content 精确清理与备份恢复仍未完成，P2 保持开放。

本增量的 contextassembly、SQLite Context、清理适配器、Memory 授权适配器和提取完整 profile race 回归全部通过，相关 vet 与 diff 检查通过；原始输出为 `evidence/20-exact-context-regression.log`。未运行最终全仓验收，未调用外部模型。

### 精确修订事件的 Content 清理

Content 新增可信 `InvalidateRevision` 端口和持久精确屏障。它与范围水位共享 512 项上限，复用既有失效/状态转换，按精确来源身份和修订将整个匹配对象置为 cleaning。混合来源对象整体失效，其他修订不受影响。所有 Content journal 提交及旧对象迁移均复查精确屏障，来源适配器即使仍允许旧修订，也不能重新发布它。

`memorycleanup.Artifacts` 对 SourceErased 使用该端口，执行一次已有有界文件清理，再核对剩余 cleaning 数量；只有实际清理完成才返回完成。未实现精确端口的后端返回未完成，不将事件降为范围清理。

新增实际文件用例分别保存修订 1、2、3 及混合 2/3 产物，先确认独立文件存在，再擦除修订 2：目标和混合文件删除，修订 1/3 仍能读取。重开后重复清理成功，禁止新操作重新发布修订 2，允许修订 3。原 17 文件崩溃恢复测试扩展到精确模式，在真实 Remove 之后、cleaned 状态提交之前直接退出，重启保持未确认并分批完成清理。定向证据见 `evidence/20-exact-content-{red,green,process}.log`。

三个精确消费者均已接入；备份恢复仍不能承载来源屏障/精确事件清单，尚待完成后才能关闭 P2 与整票验收。

本增量 Content、Memory 清理适配器及提取完整 profile race 回归全部通过，相关 vet 与 diff 检查通过，原始输出见 `evidence/20-exact-content-regression.log`。未运行最终全仓验收，无外部模型请求。

### 精确擦除的恢复清单与证明（执行端待接入）

RecoverySnapshot 新增完整精确擦除事件清单 Erased 和命名空间来源截止清单 SourceFences，继续不携带正文。集合 Position 现在计入精确事件；Operations 只包含当前最多 512 个位置窗口内的原操作，擦除位置由完整事件清单解释。校验联合核对正文/删除/擦除历史、修订连续性、位置覆盖与原比较摘要，拒绝遗漏事件、重复/跨命名空间屏障、位置碰撞和已擦除正文重新出现。先精确擦除再整记录删除的历史也纳入验证。

SQLite 在同一只读事务导出上述清单。恢复签名及验证器配置域升级为 v3，新增字段参与签名且空集合规范化；旧 v2 证明不能用于 v3 验证，验证器配置变化会让原恢复进度重新核对。真实 Ed25519 用例验证来源截止值即使结构仍合法，也不能在未重新签名时修改。

恢复执行事务尚未导入这些状态，因此 recoveryProof 在验签后仍对带新清单的备份返回未完成，不允许旧执行路径忽略字段后激活。当前增量完成的是状态导出、结构校验和证明，尚未完成备份恢复；P2 保持开放。

Memory、恢复证明与 SQLite Memory 全套 race 回归通过，相关 vet 与 diff 检查通过。证据见 `evidence/20-exact-recovery-snapshot-{red,green,regression}.log`；未执行最终全仓验收，无外部模型请求。

### 精确擦除的隔离恢复执行

恢复执行端现已接入 v3 清单：在原位置分页核对中同时验证原操作和擦除事件，在同一 SQLite 事务导入单调来源屏障、移除精确正文及其原比较摘要、保存独立恢复擦除标记，再推进 sanitized 状态。原操作历史不被新恢复标记改写；同一集合位置下来源截止值回退也拒绝。ReadView 依据权威清单的当前头部读取，当前修订已擦除时不回退到旧正文，独立历史修订仍可通过精确受验证读取访问。

此处修正前文“激活”的措辞：恢复入口始终保持普通运行时隔离；sanitized 只表示本文件完成本次维护核对，不会解除 quarantine。读取仍需实时签名证明和宿主权限控制。

四组真实文件/Ed25519 测试覆盖擦除历史或当前修订、备份位于擦除前或后，以及重开后的结果保持；另有同位置来源截止回退拒绝测试。原分页恢复进程测试现覆盖整记录删除和精确擦除两种模式，在第 512 个位置以及清理提交后真实退出子进程，重启验证 515 个原位置核对完成、权威 516 位置应用、目标移除且独立正文保留。证据见 evidence/20-exact-restore-{red,green,regression,cutoff,process}.log。尚待全仓最终验证和双轴复核，无外部模型请求。

### 最终验收

完整验证实际退出 0：26 阶段、1531 个 CLI 必验项通过；本地固定规则集 14/14、外部模型请求 0；全仓 Go/race 包通过。双轴审查无未解决发现。原始输出、构建身份、报告摘要、范围及历史未运行项见 [最终验收](20-final-acceptance.md)。第 20 票按原范围完成，不替代第 18 票模型效果或完整系统验收。
