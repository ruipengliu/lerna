# 18 票受控个性化上下文：实施中

固定审查基点 304aeb3。Agent Brief 已接受；本票尚未完成。实际 Memory 已接通回答/API 行动、动态来源治理和部分进程恢复，确定性验证入口为 personalized-context-v1。已接通行动跨进程恢复与多决策绑定；真实 API 对照通过，真实回答对照未通过。行动有界重组已接通；剩余恢复边界、CLI 补齐及最终审查仍待完成。下文按实施时间记录，早期章节的待办以末尾进展为准。

## 持久快照边界

contextassembly.Store 是消费方定义的内部受信接口；Key 由任务 namespace、task_id 和任务内决策序号组成，不新增全局操作身份。决策身份必须由宿主沿 Core 事实固定，网络重试不能另分配身份。

Bind 原子保存首次结果，空结果也占据该身份；同主体和语义摘要的重放返回原文档，不接受新候选覆盖。变更语义或主体拒绝。Read 只取原快照；失败未知不能被当作未写入而换身份重做。

SQLite Adapter 使用私有普通 0600 文件、WAL/FULL、事务写锁、每次 2 秒期限；最多 512 条快照，每份文档最多 64KiB。达到容量明确拒绝，没有驱逐旧身份。Store 本身不执行外部授权，只能在 Assembler 治理边界后使用，目前还未开放到客户端。

首个纵向测试先失败后实现，验证双连接原绑定重放、语义冲突及关闭重开。另检查并发首次绑定只有一个耐久结果，以及已取消调用不提交。日志位于 build/18-snapshot-red.log、18-snapshot-green.log、18-snapshot-concurrency.log。真实进程退出及当前来源权限验证尚未完成，不以这些测试替代。

## 当前来源修订校验

Memory.ValidateCurrent 与精确历史读取分离。它先读取依赖修订并检查当前用途、来源/处理/披露策略、Schema 和有效时间，再在内部 HeadStore 查询当前修订，返回前重查权限。相关记录被纠正返回 CONTEXT_INVALIDATED；无关记录变化不使该依赖失效。没有返回正文，也不分配/替代单次读取许可。

HeadStore 是消费方定义的内部 metadata 接口，SQLite 以按记录排序的有界查询实现，不把整集合变更位置当作上下文版本。其他 Store 未实现该能力时明确不可用，不假装历史读成功等于当前有效。

定向 race 测试通过：创建依赖、无关新增、相关纠正、使用新修订及撤权后的旧/新修订校验；未授权先返回拒绝，不通过 stale 状态暴露隐藏信息。源码及全包编译检查通过。日志为 build/18-current-source-red.log、18-current-source-green.log、18-current-source-vet.log、18-current-source-build.log。

尚待实现 ContextAssembler 组装策略及来源 Adapter、受控正文/引用快照、Brain/发布/行动接口桥接、成对真实模型效果、具名 profile、进程恢复及最终双轴审查。当前测试不代表这些要求完成。

## ContextAssembler 语义切片

Assembler.Assemble/Validate 已实现，消费方 Facts 与 Memories 接口分别负责受信事实和记忆来源。Request 固定任务内决策、事实版本、组装策略、主体、用途、模型处理位置、快照存储位置、候选修订及预算；这些值由可信宿主构造，不能代替 Provider 认证。每次调用最多 5 秒。

首轮组装先保留完整必需事实，再优先装入必需记忆，最后按候选顺序选择可选项。完整输入最多 32768 字节、16 条基础 Blocks 和 16 条记忆候选；必需部分超限返回 INPUT_BUDGET_EXCEEDED 且不创建快照，可选不足留下裁剪状态。模型 token 包络仍由模型 Adapter 单独校验。

获准记录的缺失、不适用、裁剪与冲突通过有界结构报告；权限拒绝不创建快照。冲突依据受信 Schema 投影的 Claim/Value 比较，保留两条来源引用，不以置信数覆盖一方；Claim/Value 本身不持久化。适用性判断和 Schema 投影是消费方策略接口，当前尚未实现真实 Memory Adapter。

快照保存事实摘要和每个候选的原处理状态。只有 CanStore 且再次通过保留许可校验的正文才写入 SQLite；其他所选来源仅保存受控引用和指纹，恢复时重新获准 Load。原绑定的缺失结果不因后来出现来源而扩大；未留存正文的同一修订返回不同内容则失效。返回前再次验证当前事实、依赖修订和权限；不适用或裁剪的来源也校验，避免旧覆盖状态泄露已撤权来源。存储许可撤销后不释放先前保存的正文；其物理清理仍需接入后续生命周期。

定向 race 测试通过，包含不留存正文、变更内容拒绝、撤权、必需优先于可选、缺失及冲突固定、不适用来源撤权、存储许可撤销，以及必需事实不足时不落快照。相关 vet 和全包 build 通过。日志为 build/18-assembler-red.log、18-assembler-green.log、18-required-budget-red.log、18-required-budget-green.log、18-assembler-status-tests.log、18-assembler-budget-tests.log、18-assembler-vet.log、18-assembler-build.log。

这些测试使用真实 SQLite 和受控接口替身验证组装逻辑，没有证明真实 Harness policy/Grant 与 Memory Adapter 的集成，也未证明模型或实际 API 闭环。下一步实现这些 Adapter 与任务宿主桥接，原票验收继续保持未完成。


## 真实 Memory/Grant Adapter

contextmemory.Adapter 已实现消费方 Memory、Reader、Grants 和 Projector 接口。宿主为每个决策固定主体、事实/策略版本、用途、处理和存储位置，以及至多 16 个精确来源的原读取 ID/签名材料；适配器复制配置，不自行签发或替换读取身份。只有真实 Memory.Get 才完成单次许可预留及正文读取，重放沿原绑定，原许可的 Allocated 保持 1。

ValidateProcessing 校验实际模型处理位置，不能只靠向该位置披露的权限；ValidateRetention 独立检查正文副本的存储、处理与披露。新增 ReferenceStorage 配置及 memory.store_reference/source.store_reference 动作分别控制引用元数据的存储；未声明 ReferenceStorage 默认不允许创建上下文引用副本。发现权限不能隐含授予存储权。

即使正文已经合法留存，Adapter.Validate 也通过 ReadPermits.Authorize 检验原签名、主体/代理、语义、用途及撤销状态；Authorize 不分配新用量。真实集成反例曾复现“签名许可撤销后仍可重放留存正文”，修复后拒绝。原始失败日志保留在 build/18-context-grant-red.log。

另一个反例证明可选来源被拒绝后仍保存 missing 引用会绕过元数据限制。Assembler 现在收到 Denied 就拒绝绑定；Memories 的 Missing 只适用于元数据存储本身已获准的缺失来源。build/18-denied-reference-red.log 保存修复前失败。

真实 Harness policy/local Grant、P-256 签名许可、Memory SQLite 与 Snapshot SQLite 集成通过：读取结果实际进入上下文，重复组装仍为原正文，分配量为 1，签名撤销后留存快照被拒；读取仍获准但正文存储权限撤销时，留存检查拒绝，单独获准的引用仍可校验。禁止位置的正文/引用存储均拒绝。

相关 race 测试、vet、全包 build 通过，见 build/18-retention-{red,green}.log、18-reference-storage-{red,green}.log、18-context-grant-{red,green}.log 和 18-adapter-checkpoint-{tests,vet,build}.log。集成测试的任务事实及文本投影仍为有界测试策略，尚未接入实际 Brain/发布/行动宿主或真实模型；不能据此勾选整票完成。

## 任务事实与 Brain.Context 绑定

TaskContext.Facts 消费 Core.Current/CheckDecision、既有受控 Content.Assemble/Validate 和可信时钟。Current 只读快照，不能单独作为执行授权；CheckDecision 复核当前任务执行许可及工作资格。ActionPort 已增加与 GenerationPort 同名的无状态推进校验入口，复用内核既有 current 检查。

Facts 固定目标、约束、输入引用/补充事实、主体和资源，以 UpdateVersion+1 作为本次输入事实版本，并单独校验 owner/epoch、工作身份/代次、RUN 控制、租约与任务截止时间。预算记账和 Task.Version 的独立变化不会错误地使相同事实失效。构造时复制基线，调用方后续修改不能改变绑定。

TaskContext.Session 将同一组装 Request 接入 Brain.Context；模型位置、任务身份或事实改变时拒绝，调用不能默默放宽原输入预算。缺失、裁剪、不适用和冲突作为 context-status 数据 Block 传入模型；状态序列化也纳入模型输入字节限额，不静默丢失状态。Session 不自行分配新决策或替换原签名许可。

定向测试通过：记账版本变化保留事实，输入改变、PAUSE、owner 变化及任务执行权限撤销均失效；Session 实际调用 Assembler/SQLite 输出记忆 Block。另通过真实 AnswerBrain.Decide 代码路径，在模型返回期间撤销来源，验证答案不会调用 Output.Save。此测试使用可控模型、Core 和输出端口，尚不是实际模型或最终 Core 发布验收。

日志为 build/18-task-facts-{red,green}.log、18-task-authorization-{red,green}.log、18-brain-session-{red,green}.log、18-brain-context-revocation.log、18-task-context-vet.log 和 18-task-context-build.log。相关 race、vet、全包 build 通过。下一步最终发布/新行动启动校验、受控输出来源关联、真实任务/profile 和模型成对回归；当前仍不宣称完整 18 票完成。

## 发布与行动派发前校验

answers.BindContextPort 为个性化流程显式绑定与 Brain 相同的 Context。正式发布保持原产物核对和基础内容策略校验，并在 PreparePublication 前复核该上下文。已提交的原 change_id 先核对历史回执，不因后续失效把已经发生的发布改写为未发生。孤立输出恢复时也使用原上下文组装接口验证答案来源，不退回只包含基础输入的上下文。

实际答案 profile 反例覆盖模型已调用、答案产物已存为 available，但最终上下文校验失败：任务不完成，未记录 Publication。该反例使用真实任务/授权/产物宿主和受控失效 Context，尚不是长期记忆与真实模型的全链路验收。

ActionBrain 在 READY 操作进入 Core.Next 前校验上下文，拒绝时以 input_invalidated 等待并保留 READY。反例在真实 Core.Admit 之后撤销目录来源策略；旧行为曾留下 DISPATCHED 并进入 reconciliation，修复后仍有一个 READY、零个 DISPATCHED，独立 API 目标仍为 draft、账本仍为 1000。报告增加 ReadyOperations/DispatchedOperations 区分已准入与已派发操作数量。已 DISPATCHED 的核对路径保持，不能因来源变化换新身份重试。

失败证据保留于 build/18-publication-red.log 和 18-action-dispatch-red.log；通过日志以 build/18-publication-dispatch-tests.log 为准。端到端个性化宿主、实际 Invocation 启动时的上下文依赖、输出派生来源、投影参考实现及真实模型效果仍待完成，不把本检查点视作整票验收。

本检查点定向 race 回归已通过：答案发布正反例、准入后失效、单步/多步、错误写入及未知动作恢复；对应 package 日志分别为 answer 1.860s、catalogcheck 73.111s。vet、全包 build 和 git diff --check 通过。尚未运行 18 票最终完整 make verify 或代码审查，待完整切片就绪后统一执行。

## 输出来源清单与保留期限

ContentAccess.WithLineage 接入受信 LineageProvider。保存完整答案时合并基础输入与附加依赖，按完整类型化身份去重，采用最短保留期限；合并后超过受控产物的 16 项来源上限明确失败，不能裁掉约束。附加来源不局限于模型主动引用的片段，还必须包含影响风格、选择和状态判断的依赖。

真实答案/授权/产物 profile 验证：附加来源进入 ContentSpec，输出保留期限不超过来源期限；该来源未获准时不发布，发布后撤销来源权限则产物读取拒绝。测试使用明确标注的受信本地来源规则，尚不是 Memory 动态策略 Resolver 的完整实现。

Assembler.Dependencies 导出经原快照当前权限复核的 Reference/State 清单，包含 selected、missing、inapplicable 和 trimmed；它不授予新产物的存储或披露权限。新产物 Resolver 必须分别落实当前位置、用途与保留政策。缺失来源曾在重放时跳过元数据权限校验，新增反例已修复；已获准缺失可继续保持原缺失状态，但元数据权限撤销后不能重放。

日志为 build/18-output-lineage-red.log、18-output-lineage-green.log、18-missing-lineage-red.log、18-missing-lineage-green.log、18-dependencies-red.log，以及 18-lineage-checkpoint-{tests,vet,build}.log。一次构造函数字段调整的编译失败保留在 18-output-lineage-compile-failure.log。ContextAssembler 与完整答案 profile 的 race 测试、相关 vet、全包 build 和 diff 检查通过。

下一步需把原上下文依赖映射为实际 Memory 产物来源并接入动态 Resolver。注意产物 Sources.Check 当前在授权事务内执行，不能直接重入同一个 SQLite 授权服务；需通过明确的事务接口传递当前授权校验，不能用静态规则冒充完整 Memory 策略。该问题尚待实施，不把本切片当作完整输出治理验收。


## Memory 动态派生产物授权

产物服务新增只读 SourceAuthority 回调视图，共享当前授权事务，回调结束即失效；不能修改操作记录，也不能在回调内重入授权库。Memory Authority.ReadPolicy 复用此视图校验当前策略；Memory.ValidateDerived 在独立记忆库检查精确修订、用途、有效期、实际位置与保留期限。该边界是当前授权快照加独立 Memory 读取，不承诺跨库原子撤权。

contextmemory.Sources 将受信精确引用映射为不含原始键的 SHA-256 来源身份，限制最多 512 个映射；宿主需恢复原映射，缺失时拒绝。真实 Memory、授权 SQLite 与文件产物集成已证明：同一授权服务内写入不会重入死锁；仅撤销 memory.disclose、保留 content 权限时，派生产物读取被拒。派生保留期限不能超过原记忆期限，更正来源后旧修订不能用于新派生产物。

删除派生产物不删除 Memory，也不要求读取 Memory 正文；仍要求产物删除权限和独立来源发现检查。撤销披露但保留上述权限后，删除可以完成。来源发现权限也撤销、来源过期或更正时的生命周期清理仍需后续治理路径，不能据此声称完整清理已交付。

定向 race 测试、相关 vet 和全包 build 通过。日志为 build/18-source-view-{red,green}.log、18-derived-memory-{red,green}.log、18-derived-content-{red,green}.log、18-derived-delete-{red,green}.log、18-derived-correction-green.log 和 18-derived-checkpoint-{vet,build}.log；修正测试调用签名的编译失败另存 18-derived-correction-compile-failure.log。

本检查点尚未把 ContextAssembler.Dependencies 自动接到答案 LineageProvider，也未实现映射持久化恢复、完整个性化任务宿主和真实模型成对验收；18 票仍在实施，最终全量检查与双轴审查待完整切片完成。


## 原上下文绑定答案来源

TaskContext.Session.Lineage 从同一不可变决策的 Dependencies 生成答案 LineageProvider；检查任务事实、原快照和每项依赖，再次验证后才返回。来源不限于模型引用的片段，inapplicable/trimmed 状态也参与派生治理；超过产物 16 项容量明确失败。

ContextMemory.Adapter.Derive 重用原签名读取身份获取授权保留期限，单独验证实际输出存储位置，返回精确 Memory 来源身份和最短期限，不返回正文。答案宿主需同时安装对应动态 Sources Resolver。当前 Memory 的 missing 状态尚无元数据专用派生策略，因此明确拒绝派生，不能省略该依赖继续发布；这是仍需完整宿主覆盖的边界。

真实 Memory/签名许可/SQLite 集成已验证：原依赖进入清单；禁止输出存储位置、任务目标变化或原读取许可撤销均拒绝；关闭并重开快照库、重建 Session 后保留原来源身份及期限，读取许可 Allocated 仍为 1。本次重开是进程内数据库重开，不替代尚待执行的实际进程恢复验收。

定向 race、相关 vet、全包 build 与 diff 检查通过，证据为 build/18-bound-lineage-{red,green,reopen,vet,build}.log。尚待完整答案/API 宿主接线、来源映射的进程恢复、投影参考实现及真实模型成对验收，18 票保持实施中。


## 已登记 Schema 的参考投影器

ContextMemory.RegisteredProjector 实现 Projector 接口，按受信配置固定 Schema ID/版本、Memory Kind、适用条件、主张字段和允许值；最多 32 条规则，每条最多 64 个、每值最多 256 字节，输入 JSON 最多 32768 字节。构造时复制规则，策略版本绑定到原组装请求。主张和值以 JSON 数据进入 memory Block，不传播任意额外字段、角色或指令。

参考策略要求 About 匹配当前主体、Conditions 匹配配置条件；不匹配返回不适用，保留受控依赖供状态与后续校验。未知 Schema、未登记值、重复 JSON 键、非法输入或策略版本变化均失效。该参考实现覆盖明确枚举偏好；自由文本及其他适用性策略通过 Projector 接口扩展，不能将任意内容当作授权规则。

真实 Memory/许可/SQLite 集成已替换原 textProjection 测试投影，验证结构化偏好实际进入 ContextAssembler，保留前述撤权、来源清单及重开约束。定向 race、vet、全包 build 和 diff 检查通过，日志 build/18-projector-{red,green,integration,vet,build}.log。尚未完成实际回答/API 宿主与真实模型成对验收，18 票仍在实施。


## 首次 Invocation 启动检查

Execution.Service.BindStartGuard 提供受信宿主绑定，在输入读取/Schema 校验后、首次 Started 标记事务前调用 ValidateStart；已 Started 操作先返回原状态，核对路径不调用此检查。检查位于授权事务外，允许查询原上下文的授权服务；该检查与独立 Memory 库、外部效果仍不是全局原子操作。

TaskContext.Session.StartGuard 固定原操作 ID、完整请求指纹和任务事实副本，调用原 Session.Validate。不同操作或输入不能复用该绑定；原上下文失效拒绝新启动。宿主必须在服务暴露及重启恢复前重新绑定原 Guard，目前不会将 Guard 写入 SDK 请求，也不能据此声称宿主自动恢复已经完成。

真实执行 profile 的 SQLite/有状态目标验证：调用已准入后上下文失效，Run 拒绝，Invocation.Started 为 false，目标 Changes 为 0；重新获准后原操作执行一次，再次失效不阻止返回已启动状态，也不增加效果。Session/Assembler 测试独立验证操作身份、输入变化及 Memory 撤权检查。相关 race 回归通过，profiles/executioncheck 15.819s；vet、全包 build 和 diff 检查通过。日志 build/18-start-context-{red,green}.log、18-bound-start-{red,green}.log、18-start-{vet,build}.log。

完整 Memory→回答/API 宿主接线和真实进程恢复仍待实现；此处执行 profile 使用受控失效 Guard，不能冒充完整 Memory 行动或真实模型验收。


## 实际个性化回答发布

profiles/answer.RunPersonalizedAnswer 已接通真实授权 SQLite、Memory SQLite、P-256 单次读取许可、已登记 Schema 投影、ContextAssembler、TaskContext、AnswerBrain、Core 代次记账及受控产物发布/查询。动态 Memory 来源 Resolver 与 Session.Lineage 在同一宿主中配置；任务事实来自实际 GenerationPort，输出发布使用与模型相同的 Session。

确定性成对验收已通过完整发布流程：简洁偏好得到“Memory, Brain, Execution”；详细偏好得到“The three systems are Memory, Brain, and Execution.”；不适用于回答的详细偏好保持默认简洁结果。三者均完成任务，发布产物包含一项 Memory 来源，原读取许可 Allocated 为 1。不适用来源仍进入输出治理清单，未以“没有被模型引用”为由丢弃。

入口默认使用明确标注的确定性模型。显式传入模型时按模型处理位置配置公开合成数据权限，并限制一次请求、最多 1024 输出 token；本检查点没有调用真实模型，不能据此声称真实效果或 90%/95% 质量指标。当前宿主为临时单机演示，尚未提供该个性化流程的进程恢复和 CLI 具名 profile。

定向失败/通过证据为 build/18-personalized-answer-{red,green}.log；一次 BindContextPort 参数顺序编译错误保存在 18-personalized-answer-compile-failure.log。实际三组通过日志包含独立答案、状态、产物来源与许可用量断言。相关 vet、全包 build 已通过；答案包 race 回归记录在 18-personalized-answer-regression.log。API 个性化行动、真实模型、故障恢复与整票最终审查尚待完成。


## 实际个性化 API 行动

catalogcheck 的 personalized-item / personalized-alternative / personalized-inapplicable 模式已接通真实 Memory、原签名读取、ContextAssembler、ActionBrain、Core 准入、SDK Invoke 和独立 SQLite 业务目标。参考任务要求只授权一条记录，默认 item；适用 preferred-record 偏好可选择 alternative。两条记录预先为 submitted，数量分别为 3、5，独立账本结果用于效果验收。

三组实际行动对照通过：默认/不适用偏好授权 item 后账本为 997，适用 alternative 偏好授权替代记录后账本为 995；每组只准入并执行一项操作。执行前绑定原 Session.StartGuard，保存到当前宿主服务映射以供同进程核对使用。模型仍是明确标注的确定性模型，选择读取实际记忆 Block，不接收验收预期记录；完成判断由宿主读取业务目标与原操作输入独立完成。

初次接线失败揭示同一授权数据库不能使用不同 GrantConfig 重新初始化签名日志。现已复用宿主既有签名授权器；保留失败证据于 build/18-personalized-action-locations-failure.log、18-personalized-action-green.log、18-personalized-action-green-2.log 和 18-personalized-action-diagnostic.log。成功三组日志为 18-personalized-action-green-3.log。增加未选记录保持 submitted、Memory 原读取用量为 1 的断言，并纳入原多步/等待/恢复定向回归，见 18-personalized-action-regression.log；最终 race 回归通过，121.575s。vet、全包 build 及 diff 检查通过。

这是单决策选择一项有状态 API 的参考集成。行动参数及决策产物的完整 Memory 派生来源治理、多决策上下文重绑定、实际进程恢复、CLI profile 和真实模型对照仍需完成；不能将当前同进程恢复绑定当成耐久恢复验收，也不能据确定性模型宣称质量百分比。


## 行动派生产物与独立启动预算

个性化行动宿主现在将原决策的受控来源和最短保留期限用于模型决策记录、API 参数、执行结果及最终任务证据。执行结果沿既有 executioncontent.Adapter 继承参数来源。来源清单在原决策仍有效时获取，之后每次产物读写都由动态 Memory Resolver 重新检查当前权限、精确修订和期限；记录已发生效果不要求业务状态仍与行动前一致。

引用路由在受信宿主初始化阶段绑定，现有 Content/Execution Adapter 继续访问同一个产物服务；SDK 请求无法更改路由。实际产物验收读取四类产物，逐一断言 Memory 来源与期限，然后保留 content 权限、仅撤销 memory.disclose，逐一检查读取拒绝。不以模型引用清单代替完整派生依赖。

新增治理后的 race 测试暴露首次启动检查共用 1 秒 I/O 预算的限制。独立延迟反例复现上下文检查耗尽后续事务预算；执行层现为 StartGuard 提供最多 5 秒上下文预算，并为随后 Started 提交重新建立原有 I/O 预算，调用方总截止时间仍有效。反例日志为 build/18-start-budget-{red,green}.log。初始行动失败及非 race 诊断日志为 18-action-lineage-green.log、18-action-lineage-diagnostic.log；最终个性化 race 回归记录在 18-action-lineage-regression.log。

本切片继续保留单决策参考范围；多决策绑定、实际进程恢复、missing 元数据派生策略、CLI profile、真实模型及最终双轴审查仍待完成。

本轮最终验证：三组个性化产物 race 回归通过（63.820s），原两步 API 用例通过（6.889s），启动预算反例及相关 vet、全包 build、diff 检查通过。补充日志为 build/18-action-lineage-existing.log、18-action-lineage-final-vet.log、18-action-lineage-final-build.log。


## 真实回答链路的当前记忆失效验收

profiles/answer.RunPersonalizedInvalidation 在真实 Memory、签名许可、ContextAssembler、GenerationPort 和受控答案发布链路中注入变更。两个时点分别是模型处理期间、答案保存后发布前；每个时点分别更正原记忆、创建并更正无关记录、撤销原签名读取许可，共六组确定性契约用例。

六组 race 用例通过（32.458s）：原记忆更正或原许可撤销均阻止旧答案完成发布，任务未暴露 Result；无关更正仍完成原答案发布。每组实际模型调用数为 1、原 Memory 读取分配量为 1，未通过换新许可或重新生成来掩盖失效。发布边界另以受控 LOOKUP 确认原输出操作已经对应 available 产物，且引用匹配原提案，再实施变更。

这是实际宿主的确定性故障验收；模型是受控替身，未调用真实供应商。不把它作为真实模型效果或多进程恢复证据。测试入口 `go test -race ./profiles/answer -run TestCurrentMemoryControlsActualAnswerPublication`；日志 build/18-answer-current-{red,green,durable,vet,build}.log。完整六组与补充耐久输出查询分开记录，最终整票检查仍待完成。


## 真实行动链路的当前记忆失效验收

个性化行动新增两个确定性变更时点：Core.Admit 成功后、SDK Invoke 已建立持久 Invocation 但 Run 尚未启动时。每个时点分别更正相关 Memory、更正无关记录、撤销原签名读取许可。变更经真实 Memory/Grant 端口发生一次，后续恢复不能重新实施故障或更换原动作身份。

报告分别检查 READY/DISPATCHED、受权查询的 Invocation、Started 标记、独立目标状态和账本，以及原决策/操作数量与读取用量。四组相关更正/撤权用例通过：准入后保留 READY 且不派发；Invoke 后保留一条原 Invocation 且 Started=false；目标仍 submitted、账本 1000。无关更正用例实际完成原授权，账本为 997。

第一轮报告漏计了 DONE 操作对应的 Invocation，导致两个无关更正用例断言失败；业务结果已正确，但不能把该轮整体记为通过。已将已完成的真实 Invocation 纳入查询，分别复验。保留证据为 build/18-action-current-red.log、18-action-current-report-failure.log、18-action-current-unrelated-green.log，相关 vet/build 为 18-action-current-{vet,build}.log。首轮通过的四个子用例和修复后两个子用例需分开读取。

这些是受控模型与真实授权/Memory/Core/执行/目标的契约验收，不能替代真实模型效果和实际进程恢复。多决策绑定、Guard/映射恢复、missing 元数据派生策略、CLI profile 及最终审查仍待完成。

修复后两个无关更正子用例 race 复验通过（44.887s），均完成原动作、Started=1、读取用量=1；相关 vet、全包 build 和 diff 检查通过。本轮没有重新运行整票完整 make verify。


## ContextAssembler 真实进程退出恢复

新增父子进程验收：子进程组装并提交真实 SQLite 快照后直接 os.Exit(73)，不执行 Close/清理；父进程要求该退出码，再打开同一数据库恢复。分别覆盖获准正文留存与仅保存引用。恢复后正文和原快照身份保持；引用路径拒绝相同修订下被替换的正文；两条路径当前权限撤销后均拒绝释放内容，原 Document 和 SemanticSHA256 不被改写。

`go test -race ./contextassembly -run 'TestProcessExit|TestContextCrashProbe' -v` 通过（1.198s），相关 vet 通过。父进程测试实际执行两个子进程；顶层直接运行的 Probe 会跳过，仅在显式子进程环境中执行。build/18-context-process-red.log 验证缺少 Probe 时普通成功退出不能冒充指定故障；green.log 记录两个进程恢复用例通过，vet.log 记录静态检查。

证据范围是实际 SQLite/WAL 和进程退出下的 ContextAssembler 恢复。事实、来源及权限端口为受控测试实现，尚未证明真实签名密钥/原读取材料、任务资格、动态来源映射及启动 Guard 的整套宿主跨进程恢复。该剩余项继续保留，18 票不据此完成。


## 完整回答宿主的跨进程恢复

参考回答宿主新增私有恢复配置，持久保存原任务/代次资格、签名密钥、原 read_id/签名材料/Grant ID、来源规则和原快照摘要；不额外复制 Memory 正文。配置限制 1MiB、仅普通私有文件，创建使用 O_EXCL/0600，文件与目录同步后子进程直接退出 73，跳过数据库和文件的延迟清理。

恢复重新打开原授权 SQLite、Memory SQLite、Context SQLite 与产物文件，使用原签名密钥和读取材料，重建动态 Memory 来源 Resolver、事实绑定、Session、LineageProvider 及发布端口。恢复分支跳过授权策略/LocalGrant 初始化、Memory 写入和新签名读取许可签发。原工作资格仍有效才续租；不会静默创建新工作代次或上下文身份。

实际子进程恢复验收检查原快照摘要不变、原读取分配量为 1。正常恢复只调用一次确定性模型，并发布原简洁答案；恢复后撤销原读取许可必须返回 PERMISSION_DENIED、模型调用为 0 且不发布。恢复验收与原个性化/失效用例回归记录在 build/18-answer-process-regression.log；初始接口失败、早期恢复和静态检查分别见 18-answer-process-{red,green,vet,build}.log。早期 green 在正常关闭后退出，最终 regression 已改为同步配置后、清理前直接退出，不能混淆这两种退出边界。

当前完整宿主覆盖获准正文快照、模型请求尚未派发时的恢复。尚未覆盖资格过期后重新接管、模型已派发/输出已保存等恢复点、完整引用模式、行动 Guard 的跨进程恢复及多决策绑定。私有测试签名密钥属于参考宿主配置，不代表生产密钥托管方案。18 票继续实施。

本轮最终恢复、个性化及失效 race 回归通过（60.795s），相关 vet、全包 build 和 diff 检查通过；仍未执行 18 票最终完整 make verify 与双轴审查。


## 已派发请求与孤立答案的跨进程核对

回答恢复新增 dispatched 与 output 两个子进程退出点。dispatched 在原代次的 BeginRequest 标记持久化后退出，不伪造结果或释放预算；output 使用实际 AnswerBrain 保存产物并结算用量后、正式发布前退出。子进程直接退出，测试不允许 Probe 正常返回后再补一个退出码来冒充故障边界。

恢复依据当前 Core 代次的 Started/Settled 状态分流，不相信恢复文件中的阶段猜测。已派发请求和已结算代次只调用原 Port.Recover，不再调用模型；输出身份必须仍匹配原代次。未知请求保持未发布、ReservedRequests=1、UsedRequests=0；孤立答案沿原输出恢复发布，UsedRequests=1、ReservedRequests=0。恢复后撤销原 Memory 读取许可时，孤立答案拒绝发布。四个新增 race 子进程用例通过（23.106s），父进程模型调用数均为 0、原 Memory 读取分配量均为 1。

日志为 build/18-answer-dispatch-recovery-{red,green,ready,exit,vet,build}.log，其中 ready 复验原尚未派发路径，exit 复验 Probe 必须直接退出的约束。引用模式、过期工作资格接管、其他输出/结算间故障点、行动 Guard 跨进程恢复及最终整票验收仍待完成。这里的派发标记表示可能已发送；测试没有调用真实外部模型服务。

本轮补充复验通过：原 ready 恢复路径 15.346s，直接退出约束 3.787s；相关 vet、全包 build 与 diff 检查通过。18 票最终完整 make verify 及双轴审查尚未执行。


## 完整宿主的仅引用恢复

ContextMemory.Config.ReferenceOnly 新增显式宿主存储限制，只允许 ContextAssembler 保存受控引用和指纹，不留存 Memory Block。该选项不能授予读取、引用存储或派生产物权限，这些仍由真实 Memory/签名许可及动态产物 Resolver 校验。宿主收紧为仅引用模式后，原快照中已留存的正文不可继续释放；原绑定不会被原地改写成另一个快照。

参考回答宿主将该模式及独立组装策略版本保存到恢复配置。真实子进程 reference 模式写入“详细”偏好，断言快照 Document 不包含偏好正文后直接退出；恢复使用原签名材料重新读取精确 Memory 修订，发布详细答案。恢复后撤销原许可时，模型调用为 0、未发布。两条完整进程用例通过（16.064s），原读取分配量为 1。

这个参考模式是宿主主动选择不留存正文，不声称底层 Memory 已拒绝本地正文存储。它补充实际 Memory/许可/任务/发布跨进程的引用恢复证据；已有拒绝正文存储的 Memory 契约测试负责验证权限边界。来源拒绝后不使用旧摘要。

日志为 build/18-reference-host-{red,green,retained-regression,vet,build}.log 与 18-reference-policy-tests.log。Memory/Adapter 定向 race 与相关 vet、全包 build、diff 检查通过；原正文留存恢复另行复验。过期资格接管、行动 Guard 跨进程恢复、多决策绑定、missing 元数据派生策略、CLI profile、真实模型与最终审查仍待完成。

原正文留存恢复 race 复验通过（11.565s），本轮定向验证结束；最终整票 make verify 和双轴审查仍未执行。


## 统一 CLI 契约入口

`personalized-context-v1` 汇集 26 项必需契约：3 项回答、3 项 API 行动、6 项回答失效、6 项行动失效，以及 8 项真实子进程恢复。每项限定 45 秒，使用真实本地 SQLite、签名读取许可和受控产物，模型为确定性实现，不产生外部模型请求。

在仓库根目录执行：

```sh
go build -o build/contractcheck ./cmd/contractcheck
./build/contractcheck -profile personalized-context-v1 > build/personalized-context-report.json
```

报告列出每项状态、构建版本、配置及尚未覆盖的限制。恢复用例通过 CLI 私有探针在检查点直接退出 73，再由父进程恢复原身份。`make verify` 已加入对应阶段及报告清理；未运行、失败和通过分别记录，不能沿用旧报告作为当前证据。

CLI 集成先因未知 profile 失败，注册后定向测试通过（113.921s）；相关 vet、CLI 编译、脚本语法与 diff 检查通过。26 项不等于整票验收，也不证明 90%/95% 模型质量；完整 make verify 和双轴审查尚未运行。

独立 CLI 报告已生成并核对：26 个唯一必需用例全部 passed，整体 passed；构建来源为 86b045d 上的未提交候选（dirty=true）。报告为 build/personalized-context-report.json。新增用例数量断言将随最终 CLI 回归运行，本轮另以实际 JSON 核对数量；不将此前测试运行声称为已执行该后加断言。


## 过期资格的恢复拒绝

回答恢复在原 Worker 续租失败时，返回带有当前持久预算的拒绝报告，不初始化 Memory 绑定、不签发读取许可、不调用模型，也不自动领取新资格或代次。当前实现保留原任务，供既有受控恢复流程处理；不把拒绝描述成已完成接管。

新增真实子进程 dispatched 检查点退出后的租约过期用例。测试等待实际耐久租约到期，不改写数据库或时钟；连续两次恢复均报告 VERSION_CONFLICT、零模型调用、零发布、ReservedRequests=1、UsedRequests=0。随后通过 Core.Load 核对原 Qualification、唯一 Generation 与 OutputOperation 保持，任务仍为 RUNNING 且无结果。

反例先因仅返回错误而失败，报告实现后 race 验证通过（14.129s）。日志为 build/18-expired-recovery-{red,green,normal,vet,build}.log；该用例尚未加入 26 项 CLI profile。行动 Guard 跨进程恢复、多决策绑定、missing 派生策略、真实模型和最终整票审查仍待完成。

正常原资格恢复 race 复验通过；相关 vet、全包 build 和 diff 检查通过。本轮没有运行最终整票 make verify 或双轴审查。


## 行动 Memory 原绑定重建

行动宿主将首次初始化与原绑定恢复分开。恢复材料保存决策 Key、原 ReadID、签名材料、GrantID 和来源规则，不复制 Memory 正文。恢复跳过策略发布、Grant 签发和 Memory.Put，以原授权重建 Reader、Session、动态产物 Resolver 和派生来源清单。

新增参考行动模式 personalized-reopen-alternative：序列化恢复材料，关闭 Memory 与 Context SQLite，再重开并绑定原决策；逐字比较快照 Document 并核对原读取身份及授权记录。恢复后仍从真实偏好选择 alternative，完成实际 API，账本 995，另一个记录保持 submitted，四类产物继续受 Memory 撤权控制。personalized-reopen-revoked 在关闭前撤销原单次许可，重建必须拒绝，不能重新签发许可绕过。

先建立失败反例再实现。序列化测试另外发现普通 JSON 无法还原 Protobuf AuthorizationScope 的 oneof 字段，已改为 Protobuf 字节保存并设置恢复大小上限。日志为 build/18-action-binding-{red,green,identity,revoke-red,final,vet,build}.log。

这一步完成行动跨进程恢复的原绑定前置工作；当前关闭重开仍在同一进程，未声称已证明完整行动宿主/Invocation/StartGuard 的跨进程恢复。完整进程检查点、其余整票验收继续待办。

本轮修复过程还捕获到报告整体重建覆盖 MemoryBindingRestored 标记，以及撤权断言误用授权层错误类型的问题；恢复拒绝实际来自 ContextAssembler。失败日志保留在 identity/final，修复后结果单独记录在 fixed，不以早期 green 替代最终版本证据。

修复后的原绑定恢复与撤权两项 race 用例、原 personalized-item 初始化路径 race 回归均通过；相关 vet、全包 build、diff 检查通过。本轮未运行最终整票 make verify 和双轴审查。


## Invocation 接收后的行动跨进程恢复

新增完整子进程检查点：实际 ActionBrain 经 SDK 将个性化动作提交为 Invocation 后、首次 Started 前，写入并同步私有检查点和目录，然后直接退出 73，不执行 deferred cleanup。检查点固定原任务事实、Memory 读取绑定、Invocation 请求、来源规则与快照摘要；不重复保存 Memory 正文。

父进程重开原授权、签名密钥、业务目标、Memory 和 Context 库，恢复原 Session 与动态产物 Resolver，绑定原 Core 资格和 Session.StartGuard 后才调用原操作 Run。恢复不重新调用模型、签发 Memory 许可或分配能力操作。两次 Run 使用同一原操作；第一个已启动后沿现有结果核对语义处理。

重开最初因通用宿主再次推进资源控制而返回 PERMISSION_DENIED。修复将原宿主初始化动作与恢复读出分开，恢复跳过初始化推进，不扩大个性化策略。错误报告记录阶段名称，不输出私有检查点。

本参考宿主沿既有 catalog fixture 使用确定性时钟，并恢复检查点时间；因此只覆盖原资格有效的恢复，不能据此证明真实墙钟过期接管。当前检查点也尚未覆盖已启动但效果未知、其他行动阶段或完整任务终态恢复；这些边界仍须后续验证。

最终版本的两条进程 race 用例通过（50.58s）：正常恢复账本 997、目标 authorized；恢复后撤销原许可则账本 1000、目标 submitted 且 Started=false。两者均保持原请求与唯一 Core 操作、原快照摘要、另一记录 submitted 和原读取分配量 1。日志为 build/18-action-process-{red,green,diagnosis,restored,final,vet,build}.log，早期失败保留，不作为通过证据。

原 personalized-item 初始化与执行 race 回归通过；相关 vet、全包 build 和 diff 检查通过。该检查点尚未加入 26 项 CLI profile，最终整票 make verify 与双轴审查仍未运行。


## 效果已发生、执行结果尚未知的进程恢复

新增 effect 子进程检查点。包装实际业务驱动的 Start，只有真实目标已经提交效果后才同步私有检查点并退出 73；执行层已持久化 Started，尚未调用 Inspect 或保存正式结果。测试从耐久 Invocation 读取初始 UNKNOWN，而不是相信检查点自报的阶段。

恢复先读取原 Invocation，再选择绑定方式。未启动仍完整组装及校验原决策；已启动只重建原 Memory 来源治理与 Session，不释放或重组已经过时的决策正文。随后调用原操作 Reconcile，继续沿当前权限保存结果与报告，不因原业务状态变化重新调用模型。后续两次 Run 均沿已有 Started 标记返回，不能再启动。

两条初步 race 用例通过（44.407s）：正常及恢复后撤销原 Memory 单次读取许可，实际目标均保持 authorized、账本 997，另一记录 submitted，原请求和唯一 Core 操作不变、Memory 分配量保持 1。正常核对得到 CONFIRMED。这里撤销的是原决策读取许可，产物读取与核对权限仍独立判断；不将该撤销声称为所有派生内容权限撤销。最终复验增加初始 UNKNOWN 断言，并回归未启动检查点。

日志为 build/18-action-effect-{red,green,final,vet,build}.log。参考时钟、单决策和独立存储之间无全局原子撤销的限制保持；完整任务终态恢复、多决策和其余整票验收仍待完成。

最终四条进程 race 用例全部通过，包含初始 UNKNOWN 断言及未启动恢复回归；相关 vet、全包 build、diff 检查通过。尚未执行最终整票 make verify 和双轴审查，新增恢复检查点也尚未并入 CLI profile。


## 恢复后的完整任务终态与产物治理

私有行动检查点新增原决策的 Lineage（受控来源引用和保留上限），不包含偏好正文。已启动操作恢复时，以登记的 Memory 来源身份验证这份清单并拒绝缺失或过期清单；清单只说明影响来源，不能授予权限，产物服务仍逐次校验当前来源及实际保存位置。未启动恢复继续使用原 Session 正式组装得到的清单。

RestorePersonalizedTask 在原 Invocation 核对及 Drain 后，通过实际 ActionBrain/Core 完成任务。恢复模型拒绝任何新生成请求并计数，不能把新决策伪装为恢复。最终验收读取决策记录、API 参数、执行结果和任务结果四类产物，确认各自保留一个 Memory 来源及保留期限；随后只撤销 Memory 披露权限，四类产物 READ 均被拒绝。

实际子进程 invoked/effect 两个边界恢复后均达到 COMPLETED，额外模型调用 0，原请求/唯一操作和单次 Memory 分配量 1 保持，实际账本 997。两项 race 用例通过（49.283s），对应日志 build/18-action-terminal-{red,green,revoke,vet,build}.log。原只核对 Invocation 的入口继续保留，不强制其生成任务终态。

参考时钟和单决策限制保持；多决策绑定、missing 派生策略、真实模型小回归、CLI 汇总更新及最终整票检查仍待完成。

原已发生效果且 Memory 读取许可撤销的 Invocation 恢复 race 复验通过（22.995s）；相关 vet、全包 build 和 diff 检查通过。本轮未执行最终整票 make verify 与双轴审查。


## 缺失元数据的显式授权边界

新增 Memory.ValidateMissing 和独立 MissingChecker，用于受信宿主证明一个记录身份当前不存在。它先检查绑定、用途、位置、有效保留期限与集合级元数据授权，再查询真实 HeadStore，返回前再次检查权限。当前已存在返回 CONTEXT_INVALIDATED；无权时不通过此状态泄露存在性。此接口不返回记录正文、不分配读取许可；调用方仍须单独固定原决策和读取身份。

MemoryAuth.Collection.MissingMetadataUntil 显式开启并限定缺失元数据保留时间，默认 0 拒绝。保存缺失信息要求 ReferenceStorage 与 memory.store_reference，处理和披露分别检查对应位置及权限。该信息来自集合自身的不存在状态，不能伪造一个 MemorySpec 或沿用未知记录的正文权限。只读 ReadPolicy 实现相同边界，可用于已有产物授权事务，不重入授权数据库。

真实授权与 Memory SQLite 验收覆盖获准不存在、默认关闭、错误用途/位置、过期或超长保留、已存在身份拒绝，以及撤权后存在/不存在均拒绝。已有授权事务中的调用也通过，定向 race 7.800s；相关 vet、全包 build、diff 检查通过。日志 build/18-missing-metadata-{red,green,final,vet,build}.log。

当前仅完成权限与实际不存在校验的基础边界，尚未接入 ContextMemory 的缺失依赖、派生来源 Resolver 和正式回答发布；不能将本轮通过视为缺失记忆端到端验收完成。


## 缺失依赖接入上下文与产物来源

ContextMemory.Config 通过 MissingChecker 与 MissingRetainUntil 同时显式开启缺失元数据路径；二者必须成对配置，宿主以决策策略固定期限。普通正文读取校验失败不能直接解释成缺失：还须通过真实 Memory.ValidateMissing 的用途、模型处理位置、快照存储位置及当前不存在检查。正文存储路径不能使用缺失元数据替代正文权限。

首次释放缺失状态使用原签名读取身份消费一次额度，并立即复核；重放和来源派生复用原身份，不生成新的 ReadID。ContextAssembler 原绑定保存 missing 状态，缺失来源以独立 memory-missing 类型进入 Lineage，不能被当作普通 memory 正文。实际产物 Resolver 使用已有外层授权视图再次检查当前不存在、用途、位置、存储/披露权限及期限；未登记来源和不支持缺失校验的实现拒绝。

真实 Memory/P-256 授权/Context SQLite/文件产物集成通过：两次组装保持一个缺失依赖、零正文 Block；派生来源带明确保留上限，原读取分配量为 1；产物可保存及读取。撤销原读取许可后上下文重放拒绝，另撤销 Memory 披露权限后产物读取拒绝。相关 race 集成 8.195s，ContextMemory/ContextAssembler 回归、vet、全包 build 与 diff 检查通过。日志为 build/18-missing-context-{red,green,artifacts,regression,vet,build}.log。

本轮使用实际产物服务验证来源治理，尚未把缺失候选接入正式回答宿主及具名 CLI。当前 grant 额度消费与上下文 missing 快照共同约束该决策，不将内部缺失校验声称为普通 SDK Get 返回了正文或完整读取结果。


## 正式缺失回答与恢复验收入口

回答宿主新增显式 missing 参考模式：不写入偏好记录，固定缺失元数据截止时间与独立组装策略版本，依靠原签名读取、集合元数据授权和真实 Memory 不存在校验形成上下文状态。确定性模型仅根据传入的 context-status 表达“没有已记录偏好”，正式发布产物带 memory-missing 来源，普通 memory 正文来源数量为 0，读取分配为 1。

缺失截止时间保存到私有回答检查点，恢复不能重置期限。实际子进程退出后正常恢复保留缺失状态并发布；撤销原许可则模型调用 0 且不发布。回答 race 用例 4.024s、两条恢复 race 用例 5.946s，通过日志为 build/18-missing-answer-{red,green,process-red,process-green,vet,build}.log。

personalized-context-v1 已扩展为 33 项必需检查：新增缺失回答 1 项、缺失回答恢复 2 项、行动 invoked/effect 恢复 4 项。行动正常恢复通过实际 Core 完成并检查四类产物；撤权路径核对原操作且不重复效果。CLI 增加行动退出探针，单项期限 60 秒。报告不再将已完成的行动恢复和缺失来源治理列为未实现；多决策与真实模型验收仍明确待办。

实际 CLI 已完成，build/personalized-context-report.json 的 33 项唯一必需检查全部 passed。相关 vet、全包 build、diff 检查通过；CLI 测试的数量断言已更新，待最终完整回归运行。本轮不声称已执行最终整票 make verify 或双轴审查。


## 逐决策绑定新的业务事实

行动宿主在 Core 已 Reserve 的决策序号下组装上下文。相同序号保留原绑定；只有紧邻且尚未记录的新决策可以创建下一个 Key。Validate 和已绑定的 StartGuard 不触发重绑，已启动操作继续保持原 Session。新 Session 复用原精确 Memory 读取材料与同一 SnapshotStore，不签发新许可或覆盖上一份快照。

新增 personalized-multistep 参考验收：实际目标初始 draft，第一次模型收到 draft/version=1 后提议 submit；第二次必须收到 submitted/version=2 后才能提议 authorize。最终账本 997、两次决策和两次操作，两个已存快照均按原摘要核对未变。两次来源读取复用同一授权结果，额度仍为 1；两份决策、两份参数、两份执行结果及任务结果共七类产物全部受到 Memory 来源与披露撤销约束。

初步 race 用例通过（44.335s），随后增加模型输入实际业务状态断言，并回归原 invoked/effect 任务恢复。实现中的 Core uint32 决策序号显式拓宽到 Context Key 的 uint64；编译失败记录保留在 green，运行证据在 runtime/final。日志 build/18-multi-context-{red,green,runtime,final,vet,build}.log。

本轮处理同一运行宿主的新决策绑定；已存在的进程恢复验收仍只涵盖单决策检查点。自动应对来源持续变化的有界重组及多决策中途的恢复需继续核对，不将该新用例声称为这些边界全部完成。新用例尚未纳入 33 项 CLI profile。

最终多决策业务事实断言及两条原任务恢复 race 回归通过；相关 vet、全包 build 和 diff 检查通过。整票最终 make verify 与双轴审查仍未运行。


## 第二个决策中途的进程恢复

行动恢复不再固定为决策 1。私有检查点记录 Core 基线对应的决策序号、原操作清单和每份已绑定快照的摘要。恢复使用原序号重建 Session，不能将第二次决策映射回第一份快照；在执行前及恢复结束后核对全部操作身份和完整快照历史，缺失或改写即拒绝。历史最多 512 份，检查点仍受原文件大小与权限限制。

新增 second-invoked/second-effect 两个真实子进程边界。目标先实际完成 submit，第二个 authorize 动作接收后或效果提交后退出；父进程沿原第二个操作恢复，不重新提交第一步。两条初步 race 用例通过（93.146s），恢复任务均为 COMPLETED、额外模型调用 0、账本 997、原 Memory 读取分配量 1、两份快照保持，并验证七类产物来源及披露撤销。

恢复只跳过原已启动决策的组装；之后若宿主依法进入下一个决策，仍需完整组装与来源治理，不能继承“只核对”模式省略新决策校验。当前参考宿主的固定时钟限制保持。日志 build/18-second-recovery-{red,green,final,vet,build}.log；最终同时回归单决策任务恢复。新增边界尚未并入 33 项 CLI profile。

最终四条单/多决策任务恢复 race 用例全部通过（141.529s）；相关 vet、全包 build 和 diff 检查通过。本轮未运行最终整票 make verify 与双轴审查。


## 真实模型成对验证：API 通过，回答尚未通过

新增 `cmd/contextcheck` 显式联网入口，排除在离线 verify 之外。`-real -suite all -max-requests 4` 每例最多一次请求，运行简洁/详细回答和默认/替代记录四例；`-suite answers -max-requests 2` 仅运行回答对照。凭证从环境或有界本地文件读取，不执行文件内容。报告记录独立实际效果、用量及错误，诊断版本保留公开合成模型输入/输出；每例临时私有 SQLite 在结束后清理。命令限额仅针对本次运行，不能据此授权反复重跑。

回答宿主原先申请 1024 输出 token，与既有 Ark 回答适配器 512 上限不匹配。已在完整发布测试边界先复现 MODEL_LIMIT_UNSUPPORTED，再将参考回答上限收紧为 512；定向 race（7.859s）、三组既有回答与新增上限回归、vet/build/diff 通过。API 仍使用 1024 上限。

本票授权共 8 次请求已全部使用，模型 `doubao-seed-2-0-lite-260428`，实际处理位置 `ark-cn-beijing`，端点由既有 Ark 适配器固定。全部返回已知用量，共 8430 输入、760 输出 token。无隐藏重试，失败也计入请求数；不再使用该额度发起新请求。

- [首轮四例](evidence/18-real-context-report.json)：API 两例均一次调用完成，实际选择 item/alternative，账本 997/995，另一记录保持 submitted，四类产物来源/撤权检查通过。回答均正式发布且事实正确，但详细回答反而较短，成对指标失败。
- [回答复验](evidence/18-real-answer-recheck.json)：参考任务统一补充如何应用 reply-style 偏好，简洁为逗号列表，详细为完整句；两个任务只改变 Memory 值。简洁成功，详细 OUTPUT_INVALID。该轮未保留原始输出，不能断言其具体格式问题。
- [回答诊断](evidence/18-real-answer-diagnostic.json)：相同任务要求，简洁正式发布；详细模型返回纯文本和独立引用数组，未遵守 answer/sources JSON 对象协议，Harness 拒绝发布。正文表现出详细偏好，但不能据此认定正式回答验收成功。

报告构建基点 d23bf3b、dirty=true；三轮之间本地修改见本节记录。保留失败原始结果，不将后续成功样本覆盖它们。词数差只用于此公开夹具的对照提示，不能替代语义质量审查或 90%/95% 基准。真实回答需要后续修复/重新授权预算验收；其余有界重组、CLI 边界补齐、整票审查与完整验证继续实施。


## 有界重组缺口核对（基点 83e1472 之前）

当前实现不能将失效后等待视为“最多两轮自动重组”已完成。Assembler 的原绑定不能覆盖；Session 的候选与用途固定；行动宿主目前对下一决策仍复用 revision=1 的候选及原签名读取材料。更新来源修订需要新的合法读取身份，并与新 Core 决策持久绑定，不能修改旧快照或拿旧单次许可读取不同语义。

ActionBrain 当前将组装/来源失效归为 PROCESSING_DENIED，并先通过 SaveDecision 保存原输入/输出，再 Record 结算。旧来源已经失效时，派生写入可被拒绝，Record 不会发生。后续看到未记录预留只能进入 model_unknown。这是保持当前拒绝边界的现状，并非自动重组路径。

后续实现顺序：先支持不携带已撤权正文的受信失败结算，准确保留已知用量和未知预留；再以持久化的最多两轮计数约束新决策，重新检查当前 Core 资格及来源权限/修订，取得新修订的合法材料。未知模型结果、已准入或已启动的动作不能借此换身份重做。重启必须保留各次尝试的决定、计数和绑定，不能只在内存中计数。需要实际业务效果、持续更正耗尽、撤权拒绝和进程恢复反例，不能仅以局部重试单测验收。


## CLI 多决策与第二步进程恢复验收

`personalized-context-v1` 从 33 项扩至 38 项：新增实际两决策任务及 second-invoked/second-effect 各正常、撤权恢复。检查原快照数、读取用量、独立业务状态/账本、原操作身份与零额外模型调用。正常第二步恢复验证七类产物治理；启动前撤权不发生第二步效果，已产生效果则核对原操作。所有子进程仍必须以检查点退出码 73 结束，不能将任意崩溃作为成功。

CLI 测试先因只有 33 项失败，再以 38 项全部通过；执行入口为 `go test ./cmd/contractcheck -run 'TestCLIReportsSuccessAndUnknownProfileFailure/personalized-context-v1' -count=1`。证据：[失败基线](evidence/18-profile-multidecision-red.log)、[完整 profile 成功日志](evidence/18-profile-multidecision-green.log)。相关 vet、全包 build、diff 检查通过。README 和报告的配置/限制同步更新。此处是本 profile 全部检查，尚未运行整票 make verify 或双轴审查；自动重组和真实回答问题仍待解决。


## 失效决策的受信结算

ActionBrain 识别当前 Context 明确失效/拒绝，将其转换为固定错误码 `CONTEXT_INVALIDATED` / `CONTEXT_DENIED`。Core 的内部 Record 接口允许这两种错误在没有 Evidence 引用时结算，但提案的 kind/reason/evidence/actions 必须全部为空；其他无证据记录仍拒绝。这里仅保留运行所需的固定错误码和模型用量，不保存旧正文、来源摘要或可执行动作，不扩大 Memory/产物权限。

模型返回后检查当前 Context，保存证据后也单独复核。保存发生错误时同样允许用独立 5 秒预算确认来源失效，随后以独立 1 秒预算结算，避免沿用已经超时的保存期限。只有明确失效/拒绝才能使用该路径；暂时不可用不构成撤权证明。已保存产物仍受原动态治理，不会因为丢弃提案而转为可发布结果。

已知调用保留实际用量并释放未使用预留；未知调用保留预留并进入 model_unknown，不能退款或重新调用。当前已知失效结算后进入 input_invalidated 等待；这只是新决策重组的基础，尚未实现重新绑定新修订或最多两轮自动重组，不能勾选该项完成。

实际 API 宿主反例覆盖模型期间相关更正、撤销读取许可并隐藏模型用量、无关更正、证据保存期间许可撤销及来源更正。相关失效均无动作和业务效果；无关更正仍完成原动作与来源治理。重复调用原 Record 和再次运行 Brain，检查整个持久运行快照保持不变，证明没有重复记账、修改原决定或重调模型。元数据记录不被误当作产物引用枚举。

证据：[首次失败](evidence/18-context-settlement-red.log)、[首次修复](evidence/18-context-settlement-green.log)、[保存窗口失败](evidence/18-context-save-race-red.log)、[保存窗口修复](evidence/18-context-save-race-green.log)、[五项实际宿主 race 回归](evidence/18-context-settlement-race.log)、[Brain/Core 回归](evidence/18-context-settlement-core-final.log)。相关 vet、全包 build、diff 检查通过。此次没有联网调用，也未运行最终整票 verify/双轴审查。下一步实现新决策下的新修订授权绑定及持久化的两轮上限，并补进程重启验证。


## 新修订下的有界行动重组

Brain 新增可选 ReassemblingContext 端口。只有尚未准入、已按元数据结算为 CONTEXT_INVALIDATED、且用量已知的决策才进入该端口；CONTEXT_DENIED、未知用量和已准入动作不走重组。下一次调用前仍必须由 Core Reserve 分配新决策，消耗现有持久 Corrections 计数。上下文重组与普通提案纠错共享最多两次的上限，不新增一套可在重启后归零的内存计数。

参考宿主从真实 Memory.Correct 的获准回执取得新修订；不读取未授权的私有 Head 元数据。它验证原签名许可未撤销、新修订允许当前处理位置，以及当前 Core 决策资格，再用真实签发器申请绑定新修订语义的新读取身份。新 Session/快照使用新决策序号，旧文档不覆盖。私有检查点携带当前修订、至多三个原读取许可 ID 和选定记录，恢复时使用原材料，核对全部快照与操作。

实际正例在第一份模型提案后更正 item 偏好为 alternative；原提案被丢弃，第二份决策获准读取 revision 2 并实际授权 alternative，账本 995，另一记录不变，四类产物受新修订治理。两份快照和两个实际读取分配均核对。持续更正用例在三次决策、两次重组后进入预算等待；重复运行不重置计数。撤权与未知请求均只有一次决策/读取，没有新业务效果。

新增 reassembled-invoked/reassembled-effect 真实子进程退出点。普通定向验证通过，恢复原第二决策的唯一动作后完成任务，保留纠错计数 1、两份原快照、两个读取分配，额外模型调用 0。原快照历史仍逐份核对。首次 race 暴露授权准备与组装共用五秒期限；已拆为授权准备的五秒预算与现有单次组装的独立五秒上限，最终 race 结果另记于下。

此实现是行动宿主的显式接入；不能据此宣称 AnswerBrain 已实现自动重组。参考恢复仍使用固定夹具时钟；新绑定形成之前的故障恢复、失败组装留下的历史缺口还需整票审计。CLI 当前 38 项尚未纳入本节新增用例，真实回答格式失败及最终整票审查/完整验证也未完成。


最终六项重组 race 用例全部通过（206.148s）。证据：[首次失败](evidence/18-reassembly-red.log)、[新修订实际效果](evidence/18-reassembly-green.log)、[上限/撤权/未知](evidence/18-reassembly-bounds.log)、[进程失败基线](evidence/18-reassembly-process-red.log)、[进程修复](evidence/18-reassembly-process-green.log)、[合并期限失败](evidence/18-reassembly-budget-failure.log)、[最终 race](evidence/18-reassembly-race.log)、[Brain/Core 回归](evidence/18-reassembly-core.log)。相关 vet、全包 build 与 diff 检查通过；继续回归旧进程恢复。


原有单决策与第二决策共四项进程恢复回归通过（56.859s），[日志](evidence/18-reassembly-recovery-regression.log)。本轮未运行整票 make verify 或最终双轴审查。


## 失败组装的快照历史

恢复清单明确区分已绑定快照与未生成快照的决策。创建检查点时逐个读取原决策的 SnapshotStore；成功绑定但尚未完成 Lineage 的快照也保存摘要。只有 Core 原记录是固定 Context 失效错误、没有证据/提案、未准入，且 Requests/UnknownRequests/Tokens 全为零，才允许在存储明确返回 Missing 时记录缺口。已有摘要对应的行丢失仍拒绝。

恢复在执行前和结束时核对同一清单与当前 Core：清单必须覆盖所有原序号且无冲突；缺口必须有上述零调用证明，存储仍明确不存在。Unavailable 或后来出现的快照不能被当作缺口；已绑定文档仍逐份核对摘要。清单最多 512 项，并绑定原任务 namespace/task_id。

实际故障通过 Memory.Correct 注入，未伪造业务效果或 Core 日志：第二决策组装前再次更正，形成两份快照加一个零调用缺口，第三决策完成实际动作；invoked/effect 两处进程退出后均恢复成功。另一例在第二份快照已经绑定、模型仍未调用时更正，其三份快照全部留在恢复清单中，不能漏掉未被模型使用的文档。

三项反例分别伪造已调用决策的缺口、删掉缺口声明、通过真实 SnapshotStore.Bind 在原缺口处加入文档。恢复均拒绝；独立检查 Invocation 未 Started、目标仍 submitted、账本仍 1000。六项进程 race 回归全部通过（271.549s）。证据：[失败基线](evidence/18-unbound-history-red.log)、[缺口恢复](evidence/18-unbound-history-green.log)、[篡改反例](evidence/18-unbound-history-negative.log)、[已绑定但未调用](evidence/18-unused-bound-history.log)、[race](evidence/18-unbound-history-race.log)。相关 vet、全包 build 和 diff 检查通过；继续正常恢复回归。

这里验证的是已形成检查点的原调用恢复，不能替代新读取材料形成/落盘期间的恢复审计。CLI 新用例、回答重组范围、真实回答及整票最终审查/验证仍待完成。


原正常任务/重组任务共四项进程恢复回归通过，见[回归日志](evidence/18-unbound-history-regression.log)。本轮未运行整票 make verify 或最终双轴审查。


### 回答 Worker 的上下文失效路由

通过真实 SQLite、Memory、更正/撤权、GenerationPort 和通用 Runner 验证，原先 ContextAssembler 错误缺少 DecisionReason，相关更正及撤权被结算成 FAILED / brain_failure。新增不依赖 Core 实现的错误映射：失效/缺失/身份冲突为 INPUT_INVALIDATED，权限拒绝为 PROCESSING_DENIED，输入预算和能力不可用沿用现有原因；非法参数仍属于实现错误。

同一实际回答 Worker 用例验证相关更正及撤权进入 WAITING 且不发布，已知请求用量保留，无关更正仍完成回答。该路径允许明确等待，不把本次修复表述为回答自动重组。定向测试、ContextAssembler/Brain/Core/适配器回归及 vet/build/diff 检查通过；九项实际 Worker/发布边界 race 用例通过（50.932s）。整票最终审查和 make verify 尚未运行。


## 整票双轴审查（304aeb3...1f36950）

### Standards

未发现文档规范硬违例。1 项可选 P3（possible Duplicated Code）：ContextMemory 的投影和派生产物路径重复原授权 GET 及响应校验，可考虑私有方法共享，动态授权检查仍应分别保留。本轮不为可选维护性建议扩大修改。

### Spec

1 项 P2：原 missing 来源后来创建为目标修订时，Assembler 同时接受 Missing 与 nil，使原缺失状态继续有效，违反“适用性变化、纠正或撤权使依赖它的提案失效”。另有明确未完成验收：真实回答对照失败；8 次预算耗尽，不能以脚本或真实 API 通过替代。

修复让 missing 依赖仅接受当前仍 Missing；来源存在时原决策 Invalidated，不能静默纳入新正文。真实 SQLite/Memory.Put 反例先观测 Validate 返回 nil，再验证 Validate 与 Assemble 均失效。回归揭示旧契约测试曾明确容许继续使用缺失状态，已按规格修正；新建记录干扰共享测试的集合查询，已把该检查移到查询验收后。相关完整包 race 通过；失败证据均保留。

汇总：Standards 0 硬违例、1 可选建议；Spec 1 功能缺陷已修复待复审、1 真实模型验收缺口。52 项 CLI 与最终 make verify 的结果另行记录，当前不据此宣称整票完成。


5485cb7 的双轴增量复审已通过，Spec 的 missing→创建问题关闭；Standards 仍仅有原可选 P3。整票完整验证采用 `go test -mod=readonly -p 1 -race -timeout 30m ./...`：新增恢复组的累计时间超过默认十分钟的可能性已由定向实测确认，单项用例期限、Core/组装期限不变。`make test` 与 `make verify` 使用同一包级串行/race/总期限设置。


52 项实际 CLI 定向验收通过（437.007s），日志见 `docs/implementation/evidence/18-cli-reassembly.log`。在修复审查 P2 后已启动首次整票 make verify；运行输出为 `docs/implementation/evidence/18-full-verify.log`，未完成前不表述为通过。


### 完整本地验证终态

首次整票 make verify 已成功退出，全部阶段通过。18个具名profile合计1526项必需检查、1531项总检查，个性化profile为52项。冻结验证二进制报告版本1077297、dirty=true，原构建状态保留；19代码在完整Go/race测试之后才开始，本次验证不覆盖19。阶段与逐项结果归档于 docs/implementation/evidence/18-final，原日志为18-full-verify.log。真实回答成对效果仍未通过且新预算未确认，不能标记18完成。
