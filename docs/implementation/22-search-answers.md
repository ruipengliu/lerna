# 22 搜索与证据综合实施记录

状态：实施中；审查基点 `6d40e16`。原要求及 Agent Brief 见 `.scratch/harness-implementation/issues/22-search.md`。本记录不声明联网问答已实现。

## Adapter 包迁移

下文保留各阶段的原名称；当前代码入口已收拢：

- `fetchexecution`、`searchexecution` 合入 [acquisitionexecution](../../adapters/research/execution/driver.go)，分别使用 `NewPage`、`NewSearch`，共享身份核对与结果恢复映射。搜索的披露检查和两类输入解析分别保留。
- `fetchcontext`、`searchcontext` 合入 [researchcontext](../../adapters/research/context/context.go)，使用 `NewPages`、`NewPagesWithFailures`、`NewSearch` 或组合入口 `New`。各证据角色、读取次数及分组末尾复查保持。
- `fetchqueries`、`fetchoutput`、`searchprivacy` 通过 [taskcontent.BindExecution](../../adapters/tasks/content/execution.go) 绑定原操作的查询预算；Core 继续判断权限、恢复资格和剩余额度。

## 当前实现约束

- 普通 AnswerBrain 的输出为 answer/sources 两字段，ValidateAnswer 同时用于保存后的发布恢复。增加专项答案必须同步覆盖生成、保存、查询与恢复校验，不能只在模型出口接受新 JSON。
- Model.Request.Contract 已区分普通答案与 ActionContract；专项模型格式应显式协商，普通问答继续走原合同。Ark Adapter 当前对其他合同明确拒绝，不可静默回落。
- fetchcontext 已有 external-evidence 和 external-evidence-gap，包含实际正文、SHA256、获取时间及有限失败。专项引用应以这些已获准材料为依据；搜索候选不能冒充这类块。
- 受控答案 ContentAccess 支持来源继承和 LineageProvider；专项资料及个人信息的当前授权、期限与派生来源必须覆盖这里及后续 Query/Recovery。
- 21 的获取预算与原操作恢复可复用，但搜索和辅助查询的任务级用量仍需落实。仅限制返回数量不能证明任务预算有界。

## 首条验证路径

先通过公开答案验证接口，用固定的实际材料与独立期望片段证明：接受可追溯证据，拒绝篡改片段、时间、摘要和伪造引用；明确该检查不判断自然语言是否支持结论。随后将该契约接入真实 Core 的生成与受控发布，验证撤权及恢复仍拒绝不合法结果。再接入可替换搜索 Adapter 与实际页面读取，使同一用户任务完整运行。

## 独立小型评价计划

在运行候选前冻结四类题目和必要事实：有充分依据的回答、材料缺少所问事实、两份材料存在不能自动消解的冲突、计划内的页面获取失败。为每例预先指定关键事实、范围/时间和预期处置，报告引用支持与覆盖各自分母；无适用项记为不适用。实际输出由独立材料核对，不由生成模型自评。

固定重放、loopback HTTP、公网以及实际模型分别记录。使用本地模型协议夹具的结果仅证明契约与运行流程；外部调用费用未授权前不使用 .env，也不以夹具结果宣称真实模型质量。完整 V1 仍须由 83 独立验收。

## 已实现：证据答案结构校验

新增 `brain.ValidateEvidenceAnswer`，普通答案校验保持原样。专项结构携带回答范围、结论、逐项引用和显式缺口；引用使用实际正文的 UTF-8 字节偏移，核对原正文 SHA256、精确片段和获取时间。搜索候选不能作为已取得材料。失败缺口必须引用输入中的有限获取失败事实，拒绝伪造 acquired 状态、重放非零网络请求及无界请求数。

TDD 记录：首个引用测试先因公开接口不存在而编译失败，实现后通过；失败缺口测试先暴露只校验块角色会接受 acquired 假失败，再增加有限事实校验后通过。`go test -race ./brain`（1.029 秒）与 `go vet ./brain` 通过。测试只覆盖本次公开验证接口，不代表完整搜索或任务发布通过。

仍需将专项契约接入模型、Brain 生成、受控发布和恢复，补齐不足/冲突及格式边界测试，并完成搜索与原任务预算闭环。当前校验不推断结论语义支持或必要事实覆盖；这些必须按独立评价计划核对。

## 已接入：Brain 与受控恢复发布

`NewEvidenceAnswer` 使用显式 `harness_evidence_answer_v1` 模型合同，复用普通 AnswerBrain 的决策资格、请求预留、用量结算、当前 Context 检查和受控保存。`BindEvidencePort` 固定恢复验证器；存储内容不能选择自己的验证方式，普通答案恢复会拒绝专项结果。

真实 HTTP / Core / SQLite Context / Content 的集成测试先因缺少两个绑定入口与合同常量而失败，接入后通过。运行证明：原证据经 Brain 保存后可以沿原 OutputOperation 恢复发布并完成任务；重复恢复保持原结果；当前来源撤权阻止保存、发布后的派生答案也不可读取；原模型预留结算为一次请求。模型为明确的本地协议夹具，未调用外部模型。

验证：`go test -race ./brain ./profiles/fetchcheck -run 'Test(Evidence|BrainPublishes|AnswerSchema)' -count=1` 通过（Brain 1.034 秒、fetchcheck 14.504 秒）；`go vet ./brain ./answers ./profiles/fetchcheck` 通过。普通问答路径同时回归。本轮未运行全仓验证，仍待搜索完整切片及最终独立审查。

尚未接入 Ark 的专项结构化 Schema；该 Adapter 对未知合同继续拒绝，因此不得声称已有真实模型端到端验收。正式搜索能力、四类独立小评测与任务预算仍待完成。

## 已接入：Ark 专项模型合同

Ark Adapter 新增显式证据答案 Schema 与系统提示，包含四类状态、范围、逐项片段引用和缺口。沿已固定模型和 endpoint，专项合同每请求输出最多 1024 token，普通答案仍为 512、动作合同仍为 1024；调用者和 Core 的原任务预留必须覆盖实际请求上限。未知合同继续在网络发送前拒绝。

新增本地真实 HTTP 服务测试，以明确的测试 transport 将固定端点路由到 loopback，使用 test-only 凭证，不读取 .env。测试先得到 MODEL_CONTRACT_UNSUPPORTED；接入后核对实际发送的合同名称、strict Schema、输出上限及响应用量。服务返回的不足答案通过专项校验；1025 输出 token 和未知合同都不会到达服务。

`go test -race ./adapters/model/ark ./brain` 通过（1.060/1.031 秒），`go vet ./adapters/model/ark` 通过。该证据不代表火山远端已接受新 Schema，也不代表真实模型语义支持率；正式远端兼容性与质量仍待有界真实验收。无需为本地合同接入更新模型版本或声称新增供应商能力。

## 已实现：可替换搜索发现接口

新增 websearch.Searcher 与 jsonsearch Adapter。搜索请求采用宿主固定 JSON 服务端点和唯一 q 参数，底层消费已受治理的 fetch.Fetcher，继续由其落实精确 URL/网络/当前授权。返回候选与搜索响应 Acquisition 分开；后者为搜索服务实际响应，不是候选页面的正文。接口显式说明宿主必须先授权查询和预留原任务预算，当前 Adapter 不宣称独立完成任务治理。

参考 JSON 服务返回 results 数组，每项仅 url/title/snippet，数量、正文、查询、时间和网络请求次数有界。摘要仅为发现提示，不触发页面抓取；错误不返回部分候选或原始响应。HTTP 或固定重放模式由真实获取实现保留，未引入必需独立搜索服务器。

TDD：公开接口缺失时测试失败，实现后使用真实 HTTP 服务与既有 SQLite 授权服务通过。核对候选、实际搜索 URL/获取时间/原始响应，确认未读取候选页面；另一个未在精确授权集合内的查询被拒绝。`go test -race ./profiles/fetchcheck -run TestJSONSearch -count=1` 通过（1.193 秒），jsonsearch/websearch vet 通过。

待接入正式能力 Schema、Execution 原操作恢复及共享任务预算；查询中原始/个性化资料的披露必须在该正式链路处理，不能仅以服务 URL 可访问为授权依据。当前只完成来源发现 Adapter，不构成整票验收。

## 已实现：搜索原操作持久协调

websearch.Acquirer 复用已有 fetch.Acquirer、OutcomeStore 和受控 Evidence。类型化搜索请求保留在私有桥接对象中，不将其编码成假 URL；实际搜索响应仍使用独立授权的 EvidenceOperation 保存。搜索与页面获取须注入同一任务账本，才能共同落实网络请求上界。

真实 Core 创建任务与调用身份，输入产物保存查询及限额；测试随后在协调器接口运行搜索、保存证据，重建协调器并 Recover，再以原身份 Acquire。实际服务仅在首次直接 Adapter 验证和首次持久搜索分别收到一次请求；恢复/重放未新增请求，原引用、正文和获取时间保持，SQLite Budget.Charged 仍为 1。公开协调器入口不存在时先编译失败，实现后 race 测试通过（1.488 秒），补齐查询输入绑定后定向测试通过（0.276 秒），websearch/jsonsearch vet 通过。

这仍是受信执行内部协调接口，不是新增的对外搜索准入端点。正式 Execution 驱动还须绑定精确能力、输入指纹、当前任务资格、查询披露及原始资料来源，并验证失败和中断窗口。尚未将本测试当作 SDK 搜索整体验收。

## 已接入：SDK 搜索执行与查询位置检查

新增 searchexecution.Driver，将有界 query/max_results/max_bytes/max_requests/timeout_ms 输入接到原任务调用身份、websearch.Acquirer 与原受控输出。驱动核对完整能力摘要、主体、任务资格及输入指纹；同一持久协调机制保留失败用量和原证据，查询参数不能扩大宿主限制。已知拒绝为有限失败，未知仍沿原操作核对。

searchprivacy.Guard 通过受控 Content 重新读取并比对原查询输入，在宿主固定的搜索服务实际位置同时检查 process/disclose，不把本地可读视为远端可处理。当前 Core 资格和查询授权绑定在网络请求/拨号/返回边界；搜索协调器也在调用可替换 Searcher 前后明确检查该 guard。输入来源由原 Content 继承，调用者不能从返回网页取得授权。

新增 SDK 集成测试先因驱动/隐私绑定缺失而失败，随后使用真实 Core、签名调用、SQLite、Content 和 HTTP 验证：local 查询取得受控搜索响应；相同资料送至未获准 remote-search 在发送前拒绝，0 次实际请求且原预留 Charged=1 保持；原 SDK 调用重放不新增 HTTP。最终 `go test -race ./profiles/fetchcheck -run 'Test(SDKSearch|JSONSearch)' -count=1` 通过（2.773 秒），三个新增包 vet 通过。

仍待：查询输入来源的跨任务/个性化完整装配、搜索候选专用 Context 投影、正式 Brain 搜索→页面获取→答案闭环、失败/实际进程中断及共享预算完整验证、独立四类小评价与最终审查。此驱动与获取驱动部分有限事实代码相似，最终审查评估维护边界；不因此省略恢复或授权行为。

## 已实现：搜索候选 Task Context 投影

jsonsearch.EvidenceReader 从原受控搜索响应读取并按同一 JSON 服务契约解析，不发送新查询；在线搜索和恢复读取共用响应解码，避免不同入口接受不同候选形状。searchcontext 依赖消费方 Evidence 接口，固定任务事实、位置和受控引用，投影为 search-candidates，携带候选、原搜索时间、搜索响应 URL/摘要、实际模式与请求数。

候选响应没有被标成 external-evidence；页面正文必须另走获取路径才能成为结论依据。Context 总输出上限、同命名空间及引用唯一性约束生效，释放前重新验证当前证据。宿主应注入与所选处理位置一致的受控 Reader，并由现有 taskcontext 宿主负责当前 Core 决策资格和 Context 留存；此适配器不单独授予位置权限。

TDD：缺少投影接口时编译失败；接入后真实搜索、SQLite 证据、当前策略测试通过。验证发现候选正确、8 字节 Context 限额拒绝、来源撤权后 Validate 拒绝。最终 `go test -race ./profiles/fetchcheck -run 'Test(JSONSearch|SDKSearch)' -count=1` 通过（2.881 秒），jsonsearch/searchcontext vet 通过。

下一步仍需把来源发现与页面证据组合进同一正式 Brain 任务，以及完成不足/冲突/失败的端到端处置；纯投影成功不代表专项问答通过。

## 问答串联前的契约修正与评价冻结

搜索后无足够证据时，insufficient 缺口现在可引用实际 search-candidates 块；必须有 discovered 状态和有界候选数组。它仍不能成为事实结论的页面引用，冲突引用仍要求至少两个页面证据。新增公开验证器测试先因 OUTPUT_INVALID 失败，修正后 Brain race 测试通过（1.031 秒）。

在运行完整候选之前冻结 `profiles/searchcheck/testdata` 四类虚构材料与独立 expectations：可回答例要求日期及长度两项事实，证据不足例没有成本事实，冲突例保留两个来源不同年份，失败例拒绝把索引数字当成已取得正文。文件摘要、预算、独立判定口径和不适用项规则见该目录 README。尚未运行该组完整问答，不宣称质量通过。

现有 ActionBrain 可在 Assess 中核对受控事实，Core 自身负责后续行动准入；后续同任务宿主将分别路由搜索与页面能力，答案生成必须使用原任务 Generation 预留。不能用“取得任意页面即完成任务”的 21 票条件替代 22 的正式答案判定。

## 已实现：单决策的搜索/页面/缺口组合

新增 researchcontext，复用既有搜索与获取 Context 消费接口。宿主按原执行结果选择 Search/Pages/Failures 引用，合计最多 8 个且跨集合不得重复；每个子上下文验证同一任务事实及处理位置，合并时目标与约束必须一致。输出保留 search-candidates、external-evidence、external-evidence-gap 的原始角色，采用共同序列化尺寸上限与总 5 秒上下文，并在交付前再次检查全部来源。

TDD 测试先因组合入口缺失而失败，接入后在同一真实 Core 任务事实下组合持久搜索响应和实际 HTTP 页面。验证角色顺序、候选/页面引用互斥、共同尺寸拒绝和撤权失效；相关搜索 SDK 回归同时通过。`go test -race ./profiles/fetchcheck -run 'Test(JSONSearch|SDKSearch)' -count=1` 通过（3.142 秒），researchcontext vet 通过。

本次页面请求是 Context 接口测试的显式准备，不冒充由行动 Brain 从候选中选出并经 SDK 执行。仍需同一任务内的正式行动路由、跨能力调用及答案生成/发布闭环；测试范围不能替代这些交付条件。

## Core 必要补齐：行动转答案阶段

实现串联时核实：原 GenerationPort 在 RunSnapshot.Actions 非空时一律拒绝预留，WorkPort 也拒绝非 renew 提交。因此不能直接在行动宿主内调用 AnswerBrain，更不能删除行动记录或另建任务规避预算。

新增 ActionPort.PrepareAnswer，将原资格固定到 ActionState.AnswerQualification，保留所有行动、决策、报告、查询与用量。仅当没有 READY/DISPATCHED/未知行动、未结模型预留、未处理决策及错误副作用时允许转换；原资格重放不推进版本。转换后 ActionPort 的新行动变更被拒绝，原任务可使用 GenerationPort 预留并沿现有预备发布完成答案。尚未接入 ActionBrain 的正式转阶段调用。

TDD：入口缺失时编译失败；接入后测试暴露测试装配漏掉原有 PreparePublication，结果为 FAILED 而非伪成功；补齐授权输出身份及真实预备发布后通过。测试覆盖未结模型预留/READY 行动不能跳过、行动结果保留、转换重放不变、新行动拒绝，以及答案与行动累计 2 请求/30 token；验证范围是 Core 公共接口，不代替受控答案正文验收。

`go test -race ./tasks` 全包通过（9.013 秒），tasks vet 通过。完整全仓及最终双轴审查尚未运行。本变更涉及状态机，后续需特别核对转阶段后的租约恢复、取消和迟到回执，不能仅据正常路径判完成。

## 已接入：ActionBrain 转阶段及同任务证据答案发布

ActionEnvironment 的受信 Assessment 可返回 ReadyForAnswer，由 ActionBrain 在无待执行行动、当前上下文验证通过后调用可选 AnswerTransitionCore.PrepareAnswer。该请求不是 Satisfied，不允许同时声称已完成或错误副作用；不支持转阶段的旧 Core 明确拒绝。重入已转阶段的任务直接返回快照，不调用行动模型、不增加辅助查询，也不续租后重新启动行动。

真实 Core/Catalog/SDK/HTTP 测试先因缺少 ReadyForAnswer 而编译失败。接入后验证：行动取得受控证据→Core 转阶段→租约过期后重入不改变版本/查询/网络用量→显式恢复租约→同任务 Generation 预留→证据答案受控保存与恢复发布。最终任务 COMPLETED，模型累计 2 次（行动与答案各一次），无剩余预留，原行动与 Execution report 保持。

原单步夹具 MaxSteps=1 时租约恢复正确返回 RECOVERY_LIMIT；这不是放宽运行上界的理由。为单独测试一次过期恢复，新任务明确配置 MaxSteps=2、ModelRequests=2，生产限制未修改；此装配不属于冻结的四类质量评价，不沿用失败运行冒充通过。答案使用明确的本地协议模型，不是语义质量证明。

`go test -race ./profiles/fetchcheck -run 'TestActionBrain' -count=1` 通过（5.110 秒），brain/fetchcheck vet 通过。下一步扩展该同任务链路为搜索→候选选择→页面获取，并按已冻结材料运行四类独立核对。

## 已验证：同任务搜索→候选选取→页面获取→证据答案

新增正式运行集成装配，真实 Catalog 同时登记 web.search/web.fetch，按模型选择的精确能力摘要路由到各自固定的 SDK/Execution 服务。搜索查询取自用户受控输入；第二次行动模型决策从实际 search-candidates 块中选择 URL，未在第二次请求中另注入预制页面参数。实际搜索服务返回候选，页面服务交付正文，两个能力使用同一 SQLite 网络预算账本。

ActionBrain 经两次原任务决策完成两个动作，受信事实核对要求搜索与页面都已实际取得，随后由 Core 转入答案阶段。原任务 Generation 再预留一次答案请求，通过既有受控 Content 与答案恢复发布完成任务。核对：搜索 HTTP=1、页面 HTTP=1、网络 Charged=2、行动模型请求=2；最终任务累计模型请求=3、无剩余预留，两个行动及原执行回执保留。已完成任务再次 Run 不增加网络或模型调用。

TDD 先有未实现运行入口的编译失败，完整装配后定向测试通过（1.745 秒）；最终 `go test -race ./profiles/fetchcheck -run 'Test(OneTaskSearches|ActionBrain|SDKSearch|JSONSearch|EvidenceBrain)' -count=1` 通过（18.831 秒），fetchcheck/brain vet 通过。

本次为运行契约集成，模型明确为本地协议夹具，答案尚非冻结四类材料的语义评价；不得宣称小型真实模型质量或公网通过。下一步扩展反例、持久恢复与四类运行，整理生产可调用参考入口，并完成独立评分与最终审查。

## 已验证：页面失败/未授权候选到最终缺口答案

页面拒绝端到端测试最初停在 WAITING：参考行动宿主只接纳获取成功，不能将已确认失败交给答案阶段。现已要求读取正式受控失败产物，核对其状态及实际请求数后，允许完成取证阶段；未知结果仍不允许冒充缺口。答案 Context 引用有限失败产物，协议模型输出 fetch_failed、空 claims 与原受控失败引用，并经正式答案验证/发布；读取最终答案重新核对状态及缺口身份。

另增加搜索返回未授权候选的用例：搜索实际发生一次，候选页面零次请求，失败仍保留原网络预留；不将搜索候选扩展为页面访问许可。正常、拒绝及未授权三条路径都保持原任务、原历史、共享预算及无隐式重放。

回归发现并修复 WatchGuard 将 100ms 轮询周期误作组合授权检查期限的问题，失败记录和诊断见 `22-search-guard-diagnosis.md`。最终相关 race 通过（20.979 秒），vet 通过。新增四任务并发回归；临时日志已清理。

上述答案由本地协议夹具生成，证明有限失败事实经过完整正式路径，不是冻结四类材料的独立模型质量验收。四类小评测、真实/固定 profile、完整恢复以及最终全仓/双轴审查继续待完成。

## 已验证：空搜索到证据不足答案

新增实际 JSON 搜索返回空 candidates 的完整任务反例。测试起初运行到 WAITING/budget，搜索虽已成功但行动模型又尝试一次无法成立的候选选择。参考宿主现读取当前受控搜索响应，确认候选为空后请求 Core 转答案阶段；不以网络错误推断空结果，不用页面获取成功条件阻塞这种正确不执行。

同任务答案 Context 使用 search-candidates 引用，协议模型只在确有空候选时输出 insufficient、空 claims、原搜索引用及“仅限本次检索”的范围。最终读取正式答案核对状态与引用，不能把检索无结果写成事实不存在。任务实际搜索 1 次、页面请求 0 次、网络 Charged=1，行动模型只用 1 次请求，答案沿原任务新增自己的有界预留。

TDD 失败用例修正后单独 race 通过（3.439 秒）；相关正常/拒绝/未授权/空搜索、行动转答案、证据答案及四任务并发共同 race 通过（33.471 秒），fetchcheck vet 通过。

这是空搜索边界，不替代已冻结 insufficient 样本“页面已取得但缺少建设成本”及其独立语义核验；原四类材料和判定仍保持，尚未声明质量通过。

## 多来源回答前置：继承执行中新发现的来源

在扩展冲突材料时发现，答案仅继承最初 Task.InputRefs 不足以覆盖执行中新取得的页面。新增 researchlineage.Provider，固定原决策快照和宿主选定的搜索/页面/失败引用，通过当前受控元数据收集全部来源及最短期限。模型引用列表不能缩小依赖集合；来源版本冲突、跨命名空间、不可用材料或不匹配存储位置均拒绝，最多 8 个材料/16 个独立来源。最终 Content 保存再次检查实际存储位置的处理和留存权限。

新测试通过真实 HTTP 重定向取得最初输入中不存在的 final 来源，保存不主动引用该材料的答案，核对来源仍被继承；撤销 final 后派生答案读取和来源获取均被拒绝。测试先因 provider 缺失而编译失败，实现后 race 通过（1.784 秒）。同任务答案装配现将全部执行结果作为额外来源依赖，包含未被模型引用的材料。

相关来源、正常/拒绝/未授权/空搜索及行动转答案 race 回归通过（20.233 秒），researchlineage/fetchcheck vet 通过。冲突的两页面获取与答案处置尚待接入；本次解决其来源治理前置，不声称四类语义验收已完成。

## 已验证：两个实际候选到双来源冲突答案

新增两份页面记载不同开通年份的运行反例。初始失败证实参考行动模型只选首个候选、宿主最终答案仅保留最后一个页面，无法支撑冲突处理。参考装配现按实际候选生成多个独立有界获取动作，Core 管理同一批动作；所有页面动作结束后才转答案阶段，答案 Context 保留全部取得页面。

该用例事先配置 3 次共享网络预留（搜索 1、页面 2），普通单页仍用原 2 次预算。每个 URL 的授权资源/来源单独登记，新增页面拥有独立 source-2 来源；派生答案继承全部来源。协议模型输出两项带精确片段的主张和含两个原引用的 conflicting 缺口，最终正式答案读取逐项核对两份引用都被保留。并未修改运行中预算或生产默认上限。

单例经历首候选遗漏失败、答案状态/材料遗漏失败，修正后 race 通过（6.967 秒）；相关正常、失败、空搜索、多来源及新来源撤权回归通过（25.890 秒），fetchcheck vet 通过。

这是一项固定协议模型的多来源运行验证；两块页面被该夹具按预设冲突例处理，不能推断通用模型已能识别任意两份材料的语义关系。冻结四类材料及独立评价尚须正式运行，不能用本例替代其答案支持/覆盖指标。

## 评测前置：冻结材料的独立加载

新增 `profiles/searchcheck.LoadSources` 与 `LoadExpectations`。前者仅返回题目、查询与模拟来源，后者单独返回必要事实、范围、禁止行为及引用判定材料。两份嵌入 JSON 分别核对预登记 SHA256，修改文件后摘要不符即拒绝加载；每次解析返回独立副本，调用方修改不能污染后续运行。失败页面保留原 403 与空正文。

TDD 首次 `go test ./profiles/searchcheck` 因缺少生产加载接口编译失败。实现后 `go test -race ./profiles/searchcheck` 通过（1.013 秒），`go vet ./profiles/searchcheck` 通过。测试核对冻结摘要、来源正文、独立判定分母与候选序列化不含评分字段。

本项仅完成评测材料加载，不是四类材料端到端执行或语义评分。运行宿主仍须保证只把候选来源交给被测模型；独立判定材料不得进入搜索结果、Task Context 或模型请求。22 票继续 in-progress，原验收条件不变。

## 冻结四类材料：真实本地 HTTP 协议运行

将冻结来源接入既有同任务搜索、页面执行及答案发布装配。每例使用原问题作为 Task.Goal、原查询作为受控输入；搜索响应仅含冻结标题/摘要和实际本地页面地址，页面服务交付冻结正文或原 403。读取每个受控成功证据，核对正文与冻结来源一致且有获取时间。新增不足分支保留已取得页面作为 gap 来源，不能因取得页面就认定建设成本已知；失败例不从索引摘要生成承载力主张。

首个测试因运行入口缺失编译失败。接入后不足和失败例在模型/HTTP 调用前进入 WAITING/budget，发现参考宿主将原问题直接当能力目录查询，未匹配固定能力声明。该 research profile 现使用固定研究能力意图查目录，原问题仍作为任务目标；未扩大任何预算。修正后四类单次运行通过（8.946 秒）。这不构成通用能力检索规划器的验收。

`go test -race ./profiles/fetchcheck -run 'Test(FrozenResearch|OneTask|ActionBrainHands|ConcurrentResearch)' -count=1 -v` 通过（50.061 秒）；fetchcheck/searchcheck vet 通过。日志及正式答案保存在 `evidence/22-frozen-loopback/`。该目录说明每任务预算、模式及临时数据生命周期。

本次仍为明确配置的本地协议模型；不足响应预选、双页冲突响应预设，可回答例保留占位主张。不能据测试通过宣称独立语义评价或事实覆盖通过。独立评价、真实/固定模式、可持久化运行入口、恢复补验及最终全仓/双轴审查继续待完成；原验收条件保持未勾选。

## 证据契约修复与协议输出核验

新增反例复现模型引用 `start:null` 被解码为零、整段原文引用因而通过验证；同类反例证实失败事实 `Requests:null` 也被当作零次。证据对象的全部必填字段现显式拒绝 null，保留合法零偏移与零请求语义，避免由 JSON 解码默认值补造事实。回归也覆盖 `Mode:null`。

修复前两条反例均实际失败；修复后 Brain 与 Ark Adapter race 通过（1.033/1.076 秒），answers 编译通过（无包内测试），相关 vet 通过。正式证据答案及冻结四类材料的集成 race 通过（28.216 秒）。未调用真实模型。

对上一轮已归档输出按原 expectations 单独逐例审阅，见 `evidence/22-frozen-loopback/review.md`。可回答例覆盖为 0/2，失败例未明确拒绝及具体待核实问题；不足例文字处置符合，但 gap 原始证据未持久归档，不能证明完整通过。冲突例两项来源年份具有匹配片段、摘要及时间，支持/覆盖均为 2/2，仅证明该保留输出。辅助核对确认全部 3 个保留引用的摘要和字节范围匹配冻结来源，未以此代替语义核验。

该审阅由实施助手完成，不是外部盲审或真实模型验收。发现的占位回答与归档缺口继续约束后续 profile 工作，22 票不据协议 PASS 提前完成。

## 冻结评测归档：保留实际模型输入与原输出

为解决上一轮 gap 只有引用身份、临时 Content 删除后无法复查的问题，在虚构公开材料测试的模型边界深拷贝实际 Brain.Input 和原 JSON 输出。归档同时保存读取到的正式答案；测试核对输出结构和值一致，并使用保存的输入重新验证原输出。失败例单独核对原 `denied`、`http` 和一次请求，成功页面保留正文、摘要、URL、获取时间及原引用。

TDD 首次因记录类型缺少 Input/ModelOutput/Answer 而编译失败；接入后四例通过（7.851 秒）。相关正式答案、正常/不足/失败/冲突及四任务并发 race 回归通过（50.277 秒），fetchcheck vet 通过。新原始日志及逐例 JSON 位于 `evidence/22-frozen-snapshots/`；旧日志和逐例审阅保持不变。逐例文件从实际 `record=` 行提取，核对问题与冻结案例对应、ModelOutput 与正式 Answer 相同，没有生成新答案。

本次只对明确虚构公开材料输出测试快照，不是通用 Content 导出或生产留存方案，也未记录全部行动上下文。通用持久化 profile 仍须按来源/位置/用途/期限治理归档。占位主张和预设协议响应未改写，不能以新快照替代真实模型与独立语义验收；22 票保持 in-progress。

## 可执行入口：冻结 HTTP 研究 profile

将既有研究任务装配、参考行动宿主、协议模型与答案阶段从测试文件移到 profile 实现。新增 `fetchcheck.CheckFrozenResearch(ctx, caseID)`，以普通 error 返回失败并传播调用方取消；原冻结与相关集成测试直接调用同一实现。只公开固定四类虚构材料入口，不开放任意数据导出。原 Core/SDK/Content 行为与测试断言保留，避免创建另一套执行循环。

新增 `cmd/searchcheck`，支持 frozen-loopback-v1 和单例/全部运行。报告明确区分 RuntimeVerified 与 SemanticQuality:not_evaluated，包含实际输入/原输出/正式答案、构建信息和原任务用量；未配置模式与非法参数在运行前退出。外部模型请求为零。接口/命令测试均先因缺少实现而编译失败；原任务用量字段也经历缺失字段失败后接入当前 Core/网络账本。

相关行动、预算、证据答案、冻结四例、并发及 CLI race 通过（fetchcheck 62.544 秒，CLI 5.863 秒），两包 vet 通过。随后实际构建并执行四例 CLI，退出 0，报告位于 `evidence/22-search-cli/`。每例模型请求 3、符号 token 282；行动查询单页 15/双页 16；网络预留单页 2/双页 3，实际请求对应搜索 1 与页面 1/2。模型 token 是夹具申报值，不是真实计费观测。

入口配置和未完成边界见 `profiles/searchcheck/README.md`。这次解决了只有 `_test.go` 装配、无法由命令复用的问题；仍未交付通用配置、真实/固定对比、受控长期归档、完整恢复及独立语义验收。占位模型与原预期均未改写；22 票保持 in-progress。

## 固定回放与同版本 HTTP 对比

新增 `CheckFrozenResearchReplay` 和 CLI frozen-replay-v1，复用相同 Core/SDK/搜索/答案路径。replayfetch 原仅支持成功文本，现支持 JSON 搜索快照与显式拒绝快照；拒绝配置不得带正文、SHA256 或 MediaType，避免失败补造证据。成功文本默认行为保持。回放能力声明选定 fixed-replay 实现，并保留有限事实的模式，实际 HTTP 始终零请求；搜索/页面的原任务预留不因回放而退回。

TDD 起初缺少回放入口及快照字段编译失败，实现后四类回放单次通过（7.781 秒）；CLI 回放测试起初因未配置模式退出 2，接入后通过。相关四类 HTTP/回放、原固定获取及 CLI race 回归通过（61.748/10.718 秒），相关 vet 通过。

随后用同一二进制实际运行两种模式，均退出 0。逐例报告位于 `evidence/22-mode-comparison/`。两份报告的冻结摘要和 Build 相同，正文/摘要及状态一致；HTTP 请求单页 2、双页 3，回放全部 0；预留分别仍为 2/3。失败例 HTTP 为一次实际页面拒绝，回放为零请求的显式拒绝快照，两者不混淆。

上述对比均为本地协议模型，不是公网或真实模型语义对比。原占位答案、独立评价不足以及通用配置/受控长期归档/完整恢复等要求继续保留，22 票仍 in-progress。

## 行动结束后的存储重开与租约恢复

新增 `CheckFrozenResearchReopen`，在行动全部结算、Core 已记录答案阶段后实际关闭并重开 SQLite/Content。重开后逐项比较 Actions、执行回执、模型用量、获取预算、原获取结果与受控证据（含时间/摘要），不以重新获取替代。恢复后的新行动模型实例必须零次调用；原任务继续生成并发布答案。

重开后测试时钟前移 11 秒使旧租约过期，最终报告核对工作代次确实递增。新增入口及 Recovery 字段均先经历编译失败；三路径单次通过（6.365 秒），相关普通/回放/重开 race 通过（54.637 秒），fetchcheck vet 通过。随后实际 API 调用全部四类材料，代次均 1→2，行动模型新增调用为 0，原模型预算及 HTTP 次数未重置。报告与可复现调用程序在 `evidence/22-answer-phase-reopen/`。

该验证为真实存储关闭重开，未声称实际进程崩溃。答案请求中断、真实进程恢复、动态权限/来源重载和完整查询计量仍需验证；未改变真实模型及独立语义验收要求，22 票继续 in-progress。

## 多来源部分失败：避免最后结果覆盖其他证据

新增真实双页 HTTP 反例发现参考答案装配依赖最后一个获取结果：先失败后成功会遗漏早先失败，先成功后失败会清空已经取得的页面。两种顺序在实际双页运行中都复现答案输入缺失，补齐上下文后旧协议模型又因只看首块而输出错误状态，正式答案核对再次失败。

现按全部页面行动收集成功引用与有限失败产物，保留两者共同进入 researchcontext。空搜索路径仅在没有页面或失败事实时适用，不能覆盖已有结果。协议模型的失败响应同时保留成功材料的精确引用和所有失败引用；宿主最终核对所有失败身份都存在，无页面时禁止补造主张。原单失败冻结响应保持，未修改评分材料。

两种部分失败顺序修复后单次通过（4.658 秒）；进一步增加双页都失败用例，要求两个不同的失败引用和原 3 次网络预留，先经历缺失来源失败后接入。相关多来源、四类冻结 HTTP/回放、存储重开等 race 回归通过（98.943 秒），fetchcheck vet 通过。三种新路径均经过实际 Core/SDK/受控答案发布，不只测试上下文数组。

这些是冻结四例之外的运行契约反例；成功正文来自受控实际 HTTP，失败没有页面主张。没有因此宣称真实模型已能正确解释任意部分结果；22 票原语义、真实进程恢复及其他未完成要求保持不变。

## 搜索执行：实际进程退出的四个恢复窗口

新增真实子进程测试，在搜索操作已由 SDK 接纳后，分别于预留完成、收到响应、保存内容、提交结果四个边界直接退出（75）。父进程关闭/重开真实 SQLite 与 Content，经原 SDK Reconcile 和 Execution Run/Drain 恢复；HTTP 服务位于父进程，能够观察任何新增请求。故障包装器只在真实 I/O/提交边界退出，不替换搜索、授权、任务或存储逻辑。

首次因缺少子进程探针而失败；探针接入后四窗口单次通过（1.859 秒）。原操作输入指纹、证据保存身份、Execution 输出身份和预算均保持；已保存材料恢复原候选、正文/摘要/时间，未保存响应只能 UNKNOWN/WAITING。四窗口 HTTP 总数分别 0/1/1/1，恢复没有补发，预算预留全部仍为 1。

相关搜索进程退出、原页面获取进程退出及查询接收位置限制的 race 回归通过（8.462 秒），fetchcheck vet 通过。原始日志、窗口矩阵及复现命令在 `evidence/22-search-process/`。

本测试验证搜索能力任务的真实进程退出，不宣称完整研究答案已跨进程恢复；答案阶段和模型请求中断等剩余要求保持，22 票继续 in-progress。

## 证据答案：响应未保存与内容已保存的进程退出

新增真实子进程答案恢复测试：父进程经真实页面获取准备受控证据并预留答案 generation；子进程运行 EvidenceAnswerBrain，在本地协议模型响应后或受控答案 Save 后退出（76），不执行用量结算 defer。父进程重新打开 SQLite/Content，仅经 BindEvidencePort.Recover 核对原输出操作，不调用模型。

首次因探针缺失失败，实现后两个窗口单次通过（1.083 秒）。未保存响应恢复报错且没有正式结果；已保存答案沿原身份完成，重复恢复保持结果不变，正式引用重新经当前证据校验。两者 Started 均为 1、原请求序号不可重用、HTTP 始终 1，未知模型预留请求 1/token 2560 未退回。未保存窗口观测时为 RUNNING，未冒称已完成后续超时调度。

相关证据答案发布/撤权、答案进程退出及搜索进程退出 race 回归通过（13.859 秒），fetchcheck vet 通过。原始日志及精确范围位于 `evidence/22-answer-process/`。

本例使用独立页面获取任务和证据消费答案任务，尚不等价于完整单任务行动→答案链路的同点进程恢复；模型为协议夹具，也不替代真实 API 及语义验收。22 票剩余条件继续保留。

## 同任务行动→答案：跨进程保留行动历史与生成身份

在答案进程退出回归中增加同 TaskRef 的两个窗口。真实 ActionBrain 经 Catalog/SDK 完成获取行动，由 Core 转入答案阶段后原任务预留 generation；子进程重开真实存储，新行动模型零调用，运行证据答案后在响应或保存边界退出（76）。父进程只用原答案恢复 Port，不另提交答案任务。

新增准备入口先编译失败，实现后四窗口单次通过（2.945 秒）。恢复后原 Actions、ExecutionReports、获取预算均不变；原输出操作、请求序号及未知 token 预留保持。新例 HTTP 总数/预留均 1，模型已知请求 1（原行动）加未知预留 1（答案），预留 token 2560；已保存答案沿原身份完成，未保存响应没有正式结果。

相关行动交接、证据答案发布/撤权及新旧进程窗口 race 回归通过（16.338 秒），fetchcheck vet 通过。日志与范围说明在 `evidence/22-same-task-answer-process/`。本次补强验证，未改生产逻辑；不将一个获取行动到答案的进程恢复冒称所有多轮/跨端故障点通过。22 票其余验收条件保持。

## 已保存答案恢复时的来源撤权

为同任务答案进程退出增加 action_content_revoked：子进程 Save 后退出，父进程确认孤立答案 available，再单独撤销实际网页 web/start，保留其他来源及目标权限。Recover 必须拒绝；对网页与派生答案直接 READ 均为 PERMISSION_DENIED/零数据。Task 不能出现正式结果，行动历史/执行回执及未知模型预留不得变化。

恢复公开测试策略后沿同一保存引用完成发布，HTTP 未增加，原 generation.OutputOperation 和模型序号保持。测试入口缺失时先编译失败，接入后单次通过（1.093 秒）；相关进程恢复、生成中撤权及新来源继承 race 回归通过（17.140 秒），fetchcheck vet 通过。原始日志与范围说明位于 `evidence/22-recovery-source-revocation/`。

撤权在重开之后执行，未声称策略变更本身已跨重启持久化。生产逻辑无需修改；该验证补强当前权限复查，不替代真实模型、通用配置、完整计量与最终验收，22 票继续 in-progress。

## 可替换本地答案模型与发布前失败披露

新增 `CheckFrozenResearchWithAnswerModel`，由调用方注入 `brain.Model`，沿用真实行动任务、受控证据与答案发布路径；记录实际生成阶段的能力声明。当前入口仅允许兼容固定预算的本地处理，行动规划仍是协议夹具，未接入远程授权、CLI 模型配置或真实模型评价。

独立协议实现返回一条主张、两条精确引用，首先复现参考宿主将夹具“两条主张”断言误当成通用契约的问题。现将夹具期望与通用发布校验分开；生产证据校验继续执行。另一个反例证明成功页面可掩盖上下文内已知失败，现要求所有 external-evidence-gap 都有合法 fetch_failed 缺口，在 Save 前和 Recover 时共同校验，不能发布后才报告遗漏。

未知模型用量测试先经历报告字段缺失编译失败，再复现答案已完成但宿主错误要求全部用量已结算。现核对已结算加保留请求的守恒，报告另列 ReservedModelRequests/ReservedModelTokens。测试确认原行动已结算请求 2、答案未知预留请求 1/token 2560 保持，不退款、不重标为已知。

`go test -race ./brain ./profiles/fetchcheck -run 'Test(EvidenceAnswer|ResearchAcceptsReplaceable|ResearchRejectsUnsupportedModel|ResearchReplacement|ResearchAnswer|FrozenResearchMaterials)' -count=1` 通过（brain 1.033 秒、fetchcheck 59.418 秒），涵盖替换、失败披露、部分失败、冻结 HTTP 材料和答案进程恢复。相关 vet 与 diff 检查通过。未重跑全仓验收，未调用付费模型；22 票仍 in-progress，原全部验收条件保留。

## 独立覆盖评审的输出绑定与固定分母

增加生成结束后使用的 CoverageReview 工作表，绑定精确答案 JSON 摘要及冻结期望摘要。必要事实默认 unreviewed，明确判断需给理由；漏项、重复项、未知判断以及跨答案/评分版本复用均拒绝。汇总只计算冻结覆盖分母，不输出总质量通过标记；零分母仍是不适用。接口不验证人工判断本身，引用支持、范围和缺口不能由该计数替代。

首个公开接口用例因缺少实现编译失败，接入后通过；追加绑定篡改、重复/漏项、未评审与零分母验证。`go test -race ./profiles/searchcheck ./cmd/searchcheck -count=1` 通过（1.018/9.788 秒），相关 vet/diff 检查通过。

实际阅读已有四份快照，保存独立覆盖判断并调用接口核验：answerable 0/2、conflicting 2/2、insufficient/fetch_failed 均 0/0 不适用。记录和仅验证既有判断的复现程序在 `evidence/22-coverage-review/`。这是实施助手评审协议输出，不是模型自评分或真实模型质量结论。

同时确认现有 Ark 适配器 InputUpper 固定为 224*1024=229376 token，超过本票预登记总预算 131072；当前本地入口也不具备其处理位置授权。没有扩大预算、降低声明上限或改写处理位置来强行接入。真实模型接入仍需有依据的预算兼容方案和明确位置授权；本次未读取凭证或调用付费服务。22 票继续 in-progress。

## 答案 Content 查询纳入原 Core 额度

新增消费方接口适配器 taskcontent，绑定原任务/工作代次；GET、READ、LOOKUP 每次 I/O 前调用 Core.ChargeQuery，失败保留已扣额度，并以独立尝试身份避免并发或重开适配器复用计数。PUT/DELETE 仍由原 Content 写入及授权规则处理，不将其内部账本操作重复计作外部查询。

真实 Core/SQLite/Content 测试先因缺少适配器编译失败；接入后发现 Core 将答案阶段查询也与行动变更一并拒绝，导致失败读取未进入计费。现仅允许答案阶段的查询计费，继续检查原资格、当前控制意图、租约和任务期限；新增行动仍关闭。随后测试因读取 Limit 大于保存对象长度返回 INVALID_ARGUMENT，修正为合法的一字节读取，未改变生产读取规则。

验证覆盖失效任务资格被拒绝、无效凭证读取仍消耗一个查询、耗尽原 32 条额度后拒绝、真实关闭重开存储及重建适配器后仍拒绝。测试单次通过（1.235 秒）。答案参考装配的页面/失败证据、来源继承以及正式发布 Port 的 Content 查询已统一接入。新增 ContentQueries 子计数；替换模型路径验证它大于零且属于原 ActionQueries，不增加独立额度。组合单次通过（3.673 秒）。

尚未完成行动宿主内部读取、结果账本等全部必要查询的逐项接入；独立答案进程故障装配也需后续接入并验证。当前不宣称完整任务计量、全仓验收或 22 票完成。

首次相关 race 回归失败（92.546 秒）：两种部分失败路径返回 fetch unavailable，双失败明确返回 QUERY_BUDGET_EXCEEDED。先复用失败读取已取得的元数据，保留每个读取块的 Content 校验与最后元数据复查，避免重复前置 GET；单次仍失败，没有将优化误记为解决。

进一步发现正常写入完成后参考宿主仍调用 Recover，重复发现和读取已知答案。正常路径改为用原保存 Proposal 经 BindEvidencePort.Commit 发布，保持与 Recover 一致的输出操作派生变更身份及全部发布检查。实际中断仍使用 Recover，未删减恢复校验。三种部分失败用例随后在原额度下通过（7.298 秒），未提高 64 条查询上限。

修正后的相关 race 回归通过：`go test -race ./tasks ./profiles/fetchcheck -run 'Test(ActionTask|UnknownActionWait|ResearchContentQueries|ResearchAcceptsReplaceable|ResearchReplacement|ResearchAnswer|FrozenResearch|EvidenceAnswerRecovers|EvidenceBrain|OneTaskReportsDenied)' -count=1`（tasks 1.531 秒、fetchcheck 107.543 秒），包含原行动关闭、未知用量、部分失败、固定/HTTP/重开以及答案实际进程恢复与撤权。相关 vet/diff 检查通过。

另实际运行四类冻结 HTTP CLI，退出 0，Core 总查询分别为 33/33/47/39，其中 Content 查询 18/18/31/24，仍小于原 64；报告位于 `evidence/22-content-query-budget/`。本轮未运行最终全仓验收，22 票继续 in-progress。

## 答案真实进程恢复沿用查询额度

同任务答案进程退出装配接入 taskcontent：子进程模型输入、答案保存相关读取以及父进程 Recover 的 Content 读取共用原任务 32 条额度。最初新增断言复现子进程证据读取未计费，接入后五个普通恢复窗口通过（4.227 秒）。

进一步核对查询仅追加且原前缀保持，行动的其他事实和回执逐项不变。三个同任务窗口的查询记录分别为 7→13→14（响应未保存）、7→16→27（答案已保存）、7→16→28（保存后撤权再恢复）。撤权失败 LOOKUP 恰好计费 1 次；未知模型请求/token 保留与原 HTTP 次数不变。完成后的外部核验使用独立观察者授权，不使用终结任务资格继续扣工作额度。

相关原答案发布/撤权、实际进程恢复、读取失败及存储重开额度 race 通过（19.332 秒），fetchcheck vet/diff 检查通过；原始日志与范围见 `evidence/22-recovery-query-budget/`。独立答案任务的旧窗口没有 Actions 额度，未声称其已纳入本项计量。此次补强恢复装配与验证，完整行动查询计量及 22 票其他验收保持未完成。

## 保存答案存在但恢复查询额度已耗尽

增加 action_content_budget_exhausted：真实子进程保存答案后退出，父进程用实际授权读取耗尽原 32 条额度，独立观察者确认答案仍 available，再次关闭重开存储。首次和重复 Recover 均必须返回 QUERY_BUDGET_EXCEEDED，不能发布、退款或重新生成。原查询前缀、行动、回执、输出身份及未知模型保留保持；查询恢复前后都是 32，任务观测时 RUNNING/无正式结果。

初次装配缺失编译失败；接入后暴露重开测试宿主的 inline goal 策略尚未加载，无法独立确认答案存在。加载与正常恢复一致的公开策略后单次通过（1.252 秒）；没有以此声称策略变更自动持久化。新旧恢复及预算重开 race 通过（14.228 秒），fetchcheck vet/diff 检查通过，原始日志与限制见 `evidence/22-exhausted-answer-recovery/`。本次未改生产逻辑，22 票原剩余条件保持。

## 行动规划与完成判断的账本读取计费

研究行动宿主的 Assemble 与 Assess 在读取获取结果账本前，按原任务资格调用 Core.ChargeQuery；每次尝试使用独立身份，失败不退回。新增 OutcomeQueries 子计数，与 ContentQueries 一样包含在 ActionQueries 中，不增加预算，也不把 Core 自身资格/租约/计费状态核对计作业务账本读取。公开 profile 测试首先因缺少子计数字段编译失败，接入后揭示双失败用例超出原额度。

研究上下文由私有构造的 fetchcontext/searchcontext 部件组成，每个部件已在 Assemble 结尾 Validate。仅有一个部件时去除外层重复 Validate；多个部件仍在全部装配后统一复查，模型前后和正式发布校验保持。替换模型及三种部分失败单次通过（15.675 秒），未提高原 64 条额度。

修正前启动的 race 运行失败（111.199 秒）：双失败预算耗尽，另有一次单页拒绝路径停在行动阶段 WAITING/budget、模型调用 1。后者没有足够诊断确定根因，不能将其归因于已修复的答案重复读取；参考宿主失败消息现补充查询数和最后决策错误，以便定位复发。修正后的同范围 race 回归通过（105.594 秒），相关 vet/diff 检查通过；保留先前异常记录，不声称已证明其根因解决。

实际 CLI 四例均退出 0，总查询 37/37/52/42，Content 子计数 16/16/27/21，Outcome 子计数 6/6/9/6。报告及范围位于 `evidence/22-outcome-query-budget/`，仍为本地协议模型。行动内部 Content 读取、答案装配的部分账本读取、完整计量和 22 票其余验收未完成。

上述 race 命令为 `go test -race ./profiles/fetchcheck -run 'Test(ResearchAcceptsReplaceable|ResearchReplacement|ResearchAnswer|FrozenResearch|OneTask)' -count=1`。针对先前单页拒绝异常，带新诊断的 `go test -race ./profiles/fetchcheck -run '^TestOneTaskReportsDeniedPageAsEvidenceGap$' -count=3` 通过（14.325 秒），本次未复现；该证据不能替代根因诊断或最终全仓稳定性验收。

## 答案装配的账本读取与不可变结果复用

新增单页失败公开 profile 反例，首先观测 OutcomeQueries=6/总查询42，缺少答案阶段计费。答案阶段现按原资格读取每个已结算操作一次，随后在本次装配内复用结果作页面/失败选择和来源继承，不再多次读同一账本结果。所有引用仍通过当前 Content/来源权限检查。

直接接入后双失败用例返回 INPUT_INVALIDATED，继续收敛重复观察。行动宿主每次串行运行只复用已知终结结果，未知结果不缓存；不缓存 Content 正文/权限，不跨进程保留。SQLite OutcomeStore 对已知结果拒绝替换，为这项事实复用提供实现依据，相关契约测试通过。此优化减少实际存储读取，并非省略对已发生读取的计费。

单页失败的已知运行计数相应为行动2+答案2=4次账本读取，总查询40；三种部分失败及新增计费用例单次通过（8.782 秒）。CLI 四类实测通过，总查询35/35/49/40、Outcome4/4/6/4，报告在 `evidence/22-answer-outcome-budget/`。原 64 条上限未提高，协议输出不代表语义质量通过。行动内部其他 Content 读取与最终计量审计等剩余工作保持。

`go test -race ./adapters/research/sqlite ./profiles/fetchcheck -run 'Test(KnownFetchOutcome|ResearchAcceptsReplaceable|ResearchReplacement|ResearchAnswer|FrozenResearch|OneTask)' -count=1` 通过（sqlitefetch 1.099 秒、fetchcheck 109.752 秒），覆盖不可替换结果、答案计费、部分失败、HTTP/回放/存储重开及原单任务路径；相关 vet/diff 检查通过。先前单页拒绝异常本次未复现，未据此声称根因已解决。22 票仍 in-progress，未运行最终全仓验收。

## 行动规划的搜索候选 Content 读取计费

研究行动宿主 Assemble 已将搜索候选的 EvidenceReader 绑定到当前决策的 taskcontent，正文取得与结束复查均在 I/O 前扣原 Core 查询额度。沿用原 fetchcontent/jsonsearch/searchcontext 验证，未把 discovery 提升为页面证据，也未跳过处理位置或来源权限检查。

现有公开 profile 计数反例先失败（Outcome4/总查询40）；接入后单页失败例为 Outcome4、Content25、总查询44，新增四条是搜索候选装配及复查的两组 GET/READ。三种部分失败和该计费用例单次通过（8.637 秒）。读取装配按原任务资格绑定，不创建新的额度；原 64 条上限保持。

本轮未声称已完成行动输入读取和 Assess 的所有 Content 观察，完整计量与其余 22 票验收继续保留。未重新生成整套 CLI 快照，既有历史报告保持其原运行事实。

`go test -race ./profiles/fetchcheck -run 'Test(ResearchAnswer|ResearchAcceptsReplaceable|FrozenResearch|SDKSearchPersistsOriginalDiscoveryAndChecksRecipient|ResearchContentQueries)' -count=1` 通过（86.024 秒），覆盖冻结 HTTP/回放/重开、部分失败、接收位置约束与原查询额度重开；fetchcheck vet/diff 检查通过。22 票仍 in-progress，未运行最终全仓验收。

## Assess 计费与发布末尾重复观察的修正

本轮完成前一轮未提交的 Assess 计费改动。完成判断只作阶段交接；有待执行行动时不预读后续证据，搜索为空判断经当前任务额度读取。终结操作和已取得的空/非空事实在单次串行宿主内复用，不作为模型输入或发布授权。所有页面/失败材料仍在模型调用前读取；失败产物与原账本事实的一致性检查移至 researchFailures 实际读取边界。

双失败用例曾在发布阶段达到 queries=64/64、RUNNING 并返回 INPUT_INVALIDATED。定位到共享 Port 对同一结果先 LOOKUP 再 GET，现将原输出操作身份、结果引用和当前可用性合并为输入/来源验证之后的一次 LOOKUP，不复用早先快照。原上限保持；单页失败计数现在为 Outcome4、Content26、总查询45。失败过程和修正见 `22-assess-query-budget-diagnosis.md`。

普通查询/单任务/恢复/撤权子集通过（25.021 秒）；随后 `go test -race ./profiles/answer ./profiles/extractioncheck ./profiles/fetchcheck -run 'Test' -count=1` 全部通过（247.402/97.849/184.880 秒），对应 vet/diff 检查通过。此处因共享 Port 改动扩大了相关 profile 验证范围，未运行最终全仓验收。

新增 `22-query-accounting-audit.md` 列明调用边界、子计数、内部核对与外部观察者，并保留原任务输入读取及执行适配器内部数据读取等缺口。22 票及全工作包目标继续保持未完成。

## 输入装配计费与受控失败事实复用

核对 Content READ 实现：实际存储读取前后均检查当前权限，文件/内联存储按期望摘要校验完整内容。失败读取器现在最多保留八条已核验有限事实；命中时仍执行一次 READ，并比较原引用、摘要、大小、资源、用途与类型，检查失败直接拒绝，不回退返回缓存。没有缓存私密失败正文或权限决定；缓存受 mutex 保护。

真实 SDK/Content 反例先观测两次读取产生 6 次 Content 调用，新增实现后为首次3、复用1。进一步保持 discover 权限但撤销 process/disclose，证明 GET 仍可见时复用也拒绝；恢复权限后令产物过期，仍返回 CONTENT_UNAVAILABLE。并发复用逐次执行当前授权读取。初始单次通过（0.342 秒），加强撤权/过期后通过（0.332 秒）。

研究宿主将原任务输入装入行动模型上下文时也按当前资格绑定 taskcontent。计数反例在接入前为 Outcome4/总查询37，接入后单页失败为 Outcome4、Content22、总查询41。普通单任务、部分失败及权限复用用例通过（16.527 秒），原 64 条额度未增加。Validate 中其他输入复查及执行内部数据读取等仍未全部接入，计量清单同步更新。

`go test -race ./profiles/fetchcheck -count=1` 全部通过（157.571 秒），覆盖获取/搜索/答案、当前权限、并发复用、存储重开和实际进程恢复等现有 profile 验证；相关 vet/diff 检查通过。失败事实缓存不替代 READ 的完整存储校验，也不以 GET 的 discover 权限替代 process/disclose。22 票仍 in-progress，尚未进行最终全仓验收。

## 输入复查计量与必要观察保留

研究 Validate 现在通过原任务端口执行当前权限 READ；拒绝也消耗查询。输入装配避免重复前置读取，目录访问仍依赖自身授权。证据适配器最多保留八条已验证长度，每次复用继续实际读取并校验当前权限及完整内容。可信宿主的 SnapshotAssessor 仅作无 I/O 的阶段判断，需要新事实时仍计费。

初始双页面流程曾耗尽原 64 查询上限。合并重复观察后，冲突、两种部分失败及双失败用例恢复通过；单页失败计数为总查询52、Content38、Outcome4。详细失败历史保存在 `22-input-validation-wip.md`，未覆盖旧运行报告。

Brain/fetchcheck 完整 race 通过（1.041s/170.420s）；catalogcheck 默认十分钟超时后按仓库45分钟时限重跑通过（2034.659s）。独立增量 Standards 与 Spec 均无 actionable findings。22 票仍 in-progress，最终全仓验收未运行。
