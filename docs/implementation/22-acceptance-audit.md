# 22 票整体验收审计

当前代码范围为 `6d40e16...8ff71c6`，以下为最新状态；后文按时间保留历史审计，不追认旧失败。

| 当前验收项 | 已有证据与限制 |
| --- | --- |
| 正式联网任务与实际证据 | 豆包总结v4：搜索1次、原网页0次、正式可查询答案；实际Summary二手证据，不声称取得全文。 |
| 引用与独立质量核验 | v4必要事实2/2、精确引用4/4、语义支持4/4；只支持这一有限已知材料任务。 |
| 四类结果、治理与恢复 | 真实本地HTTP及固定回放分别覆盖；当前权限、预算与原身份恢复已有定向回归，完整race已通过。 |
| 真实模型评价 | v2–v6及公网各轮原始结果保留；v6运行/语义7/8，不冒充完整V1达标。 |
| 双轴代码审查 | 无新增阻断实现发现；1项非阻断重复转换维护建议，详见22-review-466c70f.md。 |
| 全仓验证 | 8ff71c6最终make verify退出0，28阶段全部passed；旧失败及修订依据分别保留。 |

## 历史审计：60f8a97

当时审计对象为 `6d40e16...60f8a97`，保留原票六项验收条件，不以参考链路通过替代整票完成。

| 原要求 | 当前证据与结论 |
| --- | --- |
| 搜索、取得页面、Brain、正式答案 | JSON 和 DuckDuckGo HTML 完整参考任务已通过；公网动态来源装配仍未完成。 |
| 实际片段、摘要、时间、范围 | 参考材料的结构化引用与获取事实可核对；引用语义支持与范围的独立评价尚未完整交付。 |
| 可回答、不足、冲突、获取失败 | 四类参考任务、回放和重开通过；协议模型结果不代表真实模型效果。 |
| 原任务预算、来源限制、恢复 | 查询及网络计量、来源撤权等已有回归；整票静态审查发现以下两条恢复疑点，需运行复现。 |
| 独立小型评价 | 已保存覆盖工作表；原 answerable 占位答案为 0/2。缺少引用支持、范围、缺口等完整独立判定，不能写成质量通过。 |
| 可复现入口、全仓检查、整票审查 | CLI 和参考报告已交付；全仓 make verify 对 60f8a97 正在运行。整票双轴静态审查已完成，但问题尚未处置。 |

## 整票静态审查

Standards：无明确规范违反；一项判断性建议——搜索与页面驱动重复了恢复及 Outcome 转 Observation 逻辑，修改时需防止两侧行为分歧。

Spec：以下三项不能由已有绿色参考测试排除。

1. `profiles/fetchcheck/research_host.go` 的 Recover 直接调用原执行服务。UNKNOWN 导致任务版本变化后，是否需要显式 `WithActionRecovery` 重绑原操作的恢复资格，当前路径未见装配。应复现 UNKNOWN → 原事实已保存 → 原操作核对，不允许重搜或重置预算。
2. `adapters/fetchqueries/queries.go` 的已提交事实缓存也检查原 RUN 资格，未命中时通过同一执行资格扣费。取消后有限结果事实能否收敛，现有 caller-cancellation 测试只检查 OutcomeStore，未覆盖启用任务查询计量后的 Inspect/正式回执。应核验有限事实恢复，同时禁止恢复正文披露或放宽旧执行资格。
3. `profiles/searchcheck/review.go` 只有必要事实覆盖工作表。完整小型评价仍需引用支持/覆盖分子分母、不适用项及范围、缺口、禁用结论的独立判定。

前两项是待运行证实的静态发现，不能仅按审查意见修改授权边界。现有 `driver_query_test.go` 明确要求已失效执行资格不能被静默升级，此约束必须保留。

### UNKNOWN 恢复复现与隔离修复

在 `/tmp/lerna22-recovery-60f8a97` 的 `codex/22-recovery` 工作树中新增真实组件用例：HTTP 获取实际完成，仅由委托包装器丢弃首次 Inspect 响应，后续观察仍调用原驱动。未修复时 race 在 1.342 秒失败，最终为 WAITING/reconciliation，而非答案阶段。

隔离修复使用当前任务的显式 WithActionRecovery 绑定，沿用原 ActionLimits、请求与操作身份，重建仅用于核对的执行/观察装配。搜索装配保留原提供方，只替换观察作用域。修复后该用例 race 通过（2.688 秒），HTTP 次数和网络预留均为 1；包含既有旧资格拒绝及原预算测试的组合通过（8.209 秒），go vet 通过。此时修复尚未移入主工作树，取消后的有限事实问题也未修复。

Standards / Spec 增量复查均无新增可操作发现，修复已提交为 `fbc7f11`（codex/22-recovery）。修复版本的 fetchcheck 全包 race 已启动，尚未判定通过；主工作树仍保持原版本，以等待同一全仓验证进程完成。

### 取消后有限事实复现

同一隔离工作树新增 `TestCancelledActionPublishesFiniteAcquisitionFact`：通过真实 ActionBrain/SDK 发起已计量获取，在 HTTP 请求期间发出 Core CANCEL，核对获取账本确有一次请求且无正文引用，然后检查正式执行结果。当前 race 在 2.084 秒失败：Result/Effect 均为 UNKNOWN，任务为 WAITING。第二项静态发现因此已被运行证实。

取消复现仍为未提交的红色测试；测试装配公共部分提取为 runActionProcess，既有答案验收断言保留。下一步须为已授权原操作设计有限事实核对，不允许通过放开 RUN 检查恢复正文或重启请求。Execution.finish 已支持无受控结果的内部失败事实，故需先定位获取观察路径，不能无依据扩大执行结果权限。

后续隔离修复已完成：Core 新增 ChargeExecutionFactQuery，只在 PAUSE/CANCEL 期间校验当前 task.execute 权限、主体/Owner、Worker、原 ActionBinding 和原 ActionLimits 后扣费；该入口不授权 Content 或 Start。fetchqueries 仅通过它核对已保存、已知且无 Reference 的失败事实，成功获取记录不放行。

最初取消复现修复后 race 通过（2.723 秒）；与 UNKNOWN 恢复、旧资格拒绝的组合通过（9.801 秒）。扩为 PAUSE/CANCEL × 原/重建观察作用域后四例通过（7.550 秒），核对控制 APPLIED、任务状态正确、无输出引用、事实查询和网络额度均为 1。Core 身份、额度及已有恢复隔离组合通过（2.249 秒）；go vet 与 Standards/Spec 增量复查通过。此证明仅覆盖失败事实收敛，不代表成功获取正文可以在控制期间披露，也未解决并发版本冲突。

## 外部与模型接入条件

用户已指定免费 DuckDuckGo。先前一次公网诊断得到 HTTP 202 验证页面；没有绕过验证码，参考 HTTP 成功不替代公网成功。

参考模型入口当前只允许 local、InputUpper ≤ 2048；既有 Ark 模型声明 ark-cn-beijing、InputUpper = 224 × 1024。实际模型不能直接代入参考装配，更不能改标签冒充本地处理；需要实际处理位置、来源披露与输入预算的一致装配。额外有限调用额度已请求，在答复前不读取 .env 或发起付费调用。

## 全仓验证

本次从干净工作树启动 `make verify`，Go 源码版本为 `60f8a97`。依赖校验、生成对比、构建及静态分析已执行完毕，正在全仓 race 测试。最终以该进程退出状态与 `build/verification-stages.json` 为准；当前不标记通过。

### 已观察到的验证失败

修复分支 fbc7f11 的 fetchcheck 全包 race 以失败结束（295.889 秒）：参考 answerable 及并发研究任务在 27–28 次查询时进入 budget 等待。该失败保留，不以此前定向通过覆盖。

隔离工作树对 `TestConcurrentResearchTasksPreserveBudgets` 做临时定点观察，三轮运行在 22.006 秒失败，捕获 Reserve 的实际错误为 CORRECTION_LIMIT；因此不是查询上限耗尽。随后三轮在 13.584 秒失败，前置获取事实既有 denied，也有 acquired 但正式结果为 FAILURE/CONFIRMED 且无输出引用。最后两轮在 15.952 秒失败，观察到任务检查 UNAVAILABLE/context canceled 与结果保存 VERSION_CONFLICT。当前证据尚不足以归并为单一根因，不能直接提高额度或超时来宣布解决。所有 DEBUG-22 临时插桩已清理，原修复提交内容保持。

主工作树 make verify 进程仍在执行后续包，但已输出 profiles/answer 失败（277.263 秒）：当前记忆控制发布的 unrelated 场景，以及 output/restore、output/revoke 进程恢复场景出现 VERSION_CONFLICT。整仓结果不能写为通过；不得重复启动该验证来覆盖首次失败。

### 参考执行等待范围修正

后续三轮定点观察在 22.246 秒失败，明确看到原执行资格版本 3、当前版本 4、WAITING/RUN，同时输出保存的 input/acquired 元数据检查报 VERSION_CONFLICT。原因链为：参考 Execution 的 1 秒等待覆盖获取、观察与发布，而网络本身允许用满 1 秒；先返回 UNKNOWN 后，任务推进版本，迟到保存遇到旧资格。

曾试验拒绝旧执行修订的迟到完成，三轮仍在 20.835 秒失败；该做法也不适合作为解决方案，因为已有 TestLateInspectorRetainsEvidence 要求保留迟到证据。实验已撤销，Execution 内核没有保留改动。

仅将参考宿主总 DriverTimeout 改为 5 秒后，三轮并发 race 通过（25.141 秒）。网络 Timeout、IOTimeout 仍各 1 秒，查询额度及任务 deadline 不变。取消/任务截止/UNKNOWN 恢复组合通过（13.572 秒），独立迟到证据测试通过（2.881 秒），go vet 通过。

共享 config() 同时影响 v1/v2 参考宿主，不能称冻结运行配置完全不变。新增逐例 ResearchUsage.ExecutionWaitMillis 报告实际配置上限 5000 毫秒；它不是实际耗时。旧报告与旧失败保持原样，新结果不得直接沿用旧等待条件的结论。修复提交为 codex/22-recovery 上的 `ed290b5`；此前控制失败事实修复为 `d5483e9`。新版本 fetchcheck 全包 race 已启动，尚不记为通过。原全仓 profiles/answer 失败尚未解决。


### 修复分支完整回归与答案租约诊断

`ed290b5` 的 `go test -race -timeout=15m ./profiles/fetchcheck -count=1` 已以 0 退出，通过（317.869 秒）。这是修复分支的完整 fetchcheck 回归，尚未覆盖全仓。

全仓发现的三个 personalized answer 场景在隔离工作树定向复现失败（32.604 秒），错误均为 VERSION_CONFLICT。临时提交边界探针显示任务资格未变化、租约只剩 734 毫秒；随后发布失败。另一轮同例通过（10.749 秒），说明边界存在时间敏感性。探针已删除；正在通过真实公开模型边界延迟超过原租约，验证宿主是否缺少续租，不通过扩大租约或放宽 Core 检查掩盖问题。


公开入口的跨租约回归 `TestPersonalizedAnswerPublishesAfterOriginalLeaseExpires` 修前在 15.874 秒失败，实际错误为 VERSION_CONFLICT；测试使用可取消的延迟模型协议夹具，未发出外部模型调用。现三个直接驱动答案的宿主入口在 start 后启动已有续租器，并在关闭宿主前停止；保持原资格、期限及额度。go vet 与两轴增量审查无可操作发现，组合回归仍在执行。


续租修复后的组合回归以失败结束（121.756 秒）：原 publication/unrelated、output 恢复、新增长调用和续租身份场景未报告失败，但 model/unrelated 出现另一错误 CONTEXT_UNAVAILABLE，不能写为组合通过。针对该上下文错误启动三轮同例复查，未更改超时或放宽上下文校验。


随后 model/unrelated 定向三轮 race 通过（32.796 秒）；尚不能据此排除偶发上下文超时，仍保留组合失败。续租修复经两轴增量审查提交为隔离分支 `ca61123`；主工作树尚未合入，等待原全仓验证进程终止后统一集成。此次没有新增付费调用或公网绕过。


### 独立引用支持评价

隔离分支新增 NewSemanticReview/SummarizeSemanticReview，绑定实际答案、输入与冻结期望，并在结构追溯检查后记录主张—引用对支持、正文、范围、处置及禁止项的独立判断。原覆盖分母保持；无引用为 0/0 N/A；ReviewComplete 不是质量通过。测试先因缺接口编译失败，最终 searchcheck race 通过（1.059 秒），vet 通过。

四份原历史协议输出的补评位于 `docs/implementation/evidence/22-semantic-review/`：answerable 支持 0/1、覆盖 0/2，conflicting 支持 2/2、覆盖 2/2，insufficient 与 fetch_failed 两项均 0/0 N/A；fetch_failed 处置项失败。复核程序实际退出 0，核对每份原始字节绑定及已保存汇总。格式为本次事后新增，不冒充历史运行前冻结，也不替代真实模型或公网验收。

Standards/Spec 增量审查均无可操作发现；Spec 确认此前缺少独立引用支持接口及记录的 P2 已解决。全部原票验收框仍保留，其他外部条件和全仓失败未据此消除。


### 原全仓验证终止与集成

`60f8a97` 的 make verify 已正式以退出码 2 结束；阶段结果归档在 `evidence/22-verification-60f8a97/stages.json`。依赖、生成、编译、静态检查通过，全仓 tests 失败，后续 profile gates 全部未运行。除前述 profiles/answer 失败外，profiles/fetchcheck 的 reference replay/insufficient 报 research actions: UNAVAILABLE（该包总耗时 235.878 秒）。catalogcheck 最终通过（1970.114 秒），长时间无输出并非终止。

已将五项审查过的修复与评价合入主分支：f849403、f24aa90、b356572、e5ed19b、d2171ce。修复分支原提交与主分支代码对应；原全仓失败不被覆盖。答案完整回归仍在运行，已对本次新增观察到的回放失败启动三轮定向检查，暂不重复启动全仓 verify。


### 搜索响应额度配置与回放复查

在主分支对原全仓失败的 reference replay/insufficient 做三轮定向 race，通过（16.221 秒）；此结果不覆盖原失败，也不等于全仓通过。修复分支答案完整回归仍在执行。

公网装配调查确认：参考 HTTP 仅允许预置 exact URL/网络范围，搜索 descriptor 与 driver 原来固定 MaxBytes=1024；不能把现有 loopback CLI 标作公网入口。新增受信宿主 searchLimits.MaxBytes，旧入口保持 1024，同一显式额度写入能力 Schema、执行驱动及恢复配置。4096 字节配置通过真实 SDK 取得超过 2KiB HTML，保留完整内容及一次网络计费；额度+1 的请求在 HTTP 前拒绝。定向 race 通过（3.201 秒），vet 和两轴增量审查通过。尚未实现公网目标范围配置、外部运行入口和实际模型装配，未新发公网请求或读取凭证。


### 答案完整回归终止

修复分支 e03c286 启动的 `go test -race -timeout=15m ./profiles/answer -count=1` 已以 1 退出（269.733 秒）。本轮唯一报告失败为 TestAnswerHostMigratesLegacyCheckpointAfterDeletion，在 personalized_checkpoint_test.go:45 报 CONTEXT_UNAVAILABLE（5.90 秒）。原租约冲突场景未再报告，但答案整包仍失败；下一步须定位旧检查点迁移的上下文不可用路径，不以定向通过代替此结果。


### 首次上下文装配诊断

旧检查点用例的失败实际位于删除之前的首次 p.session.Assemble（line 45），不是迁移逻辑返回。原 5.90 秒失败与 Assembler 内部 5 秒硬时限相邻。主分支定向三轮 race 通过（12.256 秒）；GOMAXPROCS=1 加 CPU 采样的三轮也通过（12.369 秒）。采样总 CPU 6.22 秒，Authorization.update 累计 2.64 秒，SQLite authority Commit 1.78 秒，提示授权及存储与 race 开销，不能直接据此断言超时根因。

未修改超时、授权或上下文校验。由于先前失败期间同时运行其他重测试，现启动单独的答案完整回归，期间不再启动额外重测试；原失败仍保留，不将其排除出原全仓结果。


### 单独答案完整回归通过

主分支 c560cbe 启动的单独 `go test -race -timeout=15m ./profiles/answer -count=1` 已以 0 退出（243.883 秒）。期间未启动其他重测试；未修改 Assembler 5 秒时限、来源校验或授权逻辑。该结果覆盖答案完整包，不能据此断言此前失败必然来自测试竞争，更不能覆盖原全仓失败。当前无测试进程遗留。

公网装配后续还必须明确服务接收位置：现 bindSearchProviderLimits 的 privacy guard 仍绑定 local，实际外部提供方不能沿用这一假设。接收位置、端点、响应/请求额度与已接受运行配置需要固定并随恢复核对，不能只增大 MaxBytes 就宣称公网受控运行已完成。


### 搜索接收位置进入受控装配

`searchProviderConfig`（原 searchLimits）新增受信宿主 Recipient，空值仍为 local。实际 QueryGuard 按该位置核对查询处理和披露权限；非 local 的位置以 JSON Schema 标准 $comment 绑定进入 Capability.Digest，旧调用不能在新接收位置下重用。注释不是授权，原本地描述字节保持不变。恢复工厂沿用原 QueryGuard；这尚不是通用持久配置清单的恢复验收。

首个 SDK 测试因缺 Recipient 字段编译失败；实现后拒绝用例 race 通过（1.583 秒）。扩为 authority/source-policy 四组合后通过（3.365 秒）：只有双允许才有一次 HTTP，其余均为 denied、零 HTTP、无引用；重复执行保留一次网络预留，更换接收位置拒绝旧请求。包含既有 DuckDuckGo 响应预算、SDK 原始发现和查询计量的组合 race 通过（9.252 秒），vet 与 Standards/Spec 增量审查通过。

这些使用真实本地 HTTP 验证位置许可语义，不是实际公网传输或物理区域验证；新接收位置用例未启用 Actions 查询端口。公网外部入口、目标范围、持久配置和实际模型仍未完成，没有新增外部请求或读取凭证。


### 已配置服务进入同一研究任务路径

从固定资料 HTTP 工厂提取 runPreparedResearch/researchRunSpec。参考入口继续提供原材料、动作/模型/HTTP 次数断言；新内部入口使用调用方已配置的搜索端点与页面，复用 Core、ActionBrain、SDK、受控答案发布，无独立旁路执行循环。请求用量来自行动宿主已持有的原获取 Outcome；fixed-replay 不计物理 HTTP，不为报告新增未计量业务读取。

新调用方服务用例先因缺入口编译失败，实现后整链 race 通过（4.893 秒）：搜索/页面各一次，网络预留二次，正式答案引文包含调用方页面实际内容。vet 与两轴增量审查通过；fetchcheck 全包回归正在执行。使用本地 HTTP 和协议模型，不代表公网入口、实际模型质量或整票完成。


该次 `go test -race -timeout=15m ./profiles/fetchcheck -count=1` 最终以 0 退出，通过（241.838 秒）。此结果覆盖新入口与既有 fetchcheck 全包，不替代仍未通过的全仓 make verify 或实际公网/模型验收。

### 完整任务使用搜索配置并发布已确认的发现失败

researchRunSpec 接收 SearchConfig，统一将响应额度和接收位置传入搜索绑定、协议行动参数及运行记录；默认仍为 1024 字节、local。配置测试通过真实授权和来源策略建立远端接收许可，4096 字节配置取得超过 2KiB 的搜索响应并完成正文引用；来源许可拒绝时搜索和页面 HTTP 均为零，保留一次网络预留并正式发布 fetch_failed。该拒绝是初始策略拒绝，不是运行中撤权测试。

新增本地 HTTP 202 搜索失败测试修前失败（1.513 秒）：一次搜索后进入 WAITING/budget，未观察到第二次 HTTP。现仅在行动已结束、效果为 CONFIRMED/NOT_OCCURRED 且原结果为已知有限失败时进入答案路径；UNKNOWN 仍须核对。失败材料沿原受控来源及当前许可检查进入应答，成功搜索仍不作为正文证据。

全部 PreparedResearch 定向 race 通过（12.798 秒）；涵盖参考研究、DuckDuckGo、原任务用量、UNKNOWN、SDK 搜索预算与配置、答案交接及恢复来源策略的组合 race 通过（127.062 秒）。go vet 与 Standards/Spec 增量审查无可操作发现。没有新增公网或模型调用、没有读取凭证；公网入口、持久配置、实际模型装配和整票验收仍未完成。

### 宿主网络范围显式配置

新增 openWithNetwork，以受信宿主传入的网络范围和 loopback HTTP 开关装配原 httpfetch Adapter；复制网络切片，保留独立 exact URL、当前授权、来源策略与 DNS/重定向检查。原 open 保持 127.0.0.0/8 默认。研究检查点重开显式沿用原网络配置；这尚不是配置清单的跨进程持久化验证。

完整研究测试首先因缺入口编译失败；随后发现单 URL 测试沿用了参考输入的 start/final 双来源，导致 PERMISSION_DENIED。测试改为显式使用已有 task-goal 来源后，允许/拒绝两例 race 通过（5.487 秒）：同一已许可 URL 在网络范围拒绝时零 HTTP、fetch_failed，在允许时一次 HTTP、空搜索结果对应 insufficient，两例均保留一次网络预留并经过答案重开。重开后不重新联网，不能将此测试称为恢复后的网络拒绝验证。

go vet、diff 检查和 Standards/Spec 增量静态审查通过；fetchcheck 完整 race 回归已启动，结果待记录。没有新增外部请求或模型调用。外部宿主仍需原始查询来源、目标范围和接收位置的完整受信配置，不能把参考输入默认值用于一般公网任务。

该次 `go test -race -timeout=15m ./profiles/fetchcheck -count=1` 已以 0 退出，通过（249.788 秒）。期间无其他重测试并行。此结果不替代全仓 verify、实际公网或真实模型质量验收。

### 传输范围在运行目录中固定

新增私有 transport-config.json，在首次宿主启动、身份初始化之前独占创建，保存版本、原 URL 列表、网络范围及 loopback HTTP 开关，并同步文件与目录。重开只核对有界原文件，缺失、损坏、权限异常或配置不同均拒绝，不从调用方参数重建清单。宿主同时复制 URL 和网络切片。当前授权与来源策略仍逐次检查，清单自身不授予权限，也不防可信本地宿主直接篡改文件。

扩展整链测试修前失败（4.685 秒）：任务结束后使用改变的网络范围重开仍被接受。修后网络变化、URL 增加均拒绝，原配置仍可重开；定向 race 通过（5.666 秒）。go vet、diff 检查和 Standards/Spec 增量审查通过；含既有跨进程恢复的 fetchcheck 全包 race 已启动，结果待记录。

兼容边界：没有清单的旧运行目录不会自动迁移；URL/网段顺序变化也作为已选配置变化拒绝。旧运行证据继续保留，不把重新配置的新运行冒充原任务恢复。该清单目前仅覆盖传输范围；搜索提供方及模型的完整配置绑定、外部入口和公网/模型验收仍未完成。本轮无外部请求、无凭证读取。

该次 `go test -race -timeout=15m ./profiles/fetchcheck -count=1` 最终以 0 退出，通过（253.392 秒），覆盖现有跨进程与存储恢复用例；没有重启或并行启动其他重测试。全仓 verify 与实际公网/模型验收仍未据此完成。

### 答案模型的处理位置与输入预留

内部 researchRunSpec 新增显式 ModelTokens，零值仍为原 32768；答案生成保留至少原 2048 输入额度，并覆盖模型声明的更大 InputUpper，输出仍为 512。模型实际 Location 传入证据接收位置、答案上下文及发布复核，结果仍存储在 local。远端证据读取使用独立实例，不复用本地读取缓存；没有自动增发位置授权或来源许可。本地公开参考入口的模型限制仍保留。

模型边界协议测试声明非本地位置及 224*1024 输入上界，显式任务预算为 256*1024；允许时一次生成并正式发布，仅有主体授权或仅有来源许可时均不调用模型。初始编译因缺 ModelTokens 字段及测试变量类型错误失败；首轮两组合通过（8.340 秒），补充独立权限组合及本地可替换模型、未知用量和回放计量回归通过（25.630 秒）。这些是本地协议测试，不是实际火山调用或效果验收。

Spec 审查发现 P1：FailureReader 使用能力的 local 位置覆盖接收位置。首个反例停在另一参考路径的 WAITING/budget，实际 VERSION_CONFLICT，未触及披露边界，已删除。改用完整研究流程后明确复现：原任务与搜索输入可外发、失败行动输入仅限 local，模型仍调用一次且正式路径返回成功（修前 3.780 秒）。修复将失败读取能力的位置绑定实际模型位置；与远端权限测试组合 race 通过（15.402 秒），首次与缓存失败读取均按实际接收位置核对。Spec 复核无新增发现，P1 已关闭；Standards 原增量无可操作发现，vet/diff 检查通过。

fetchcheck 整包 race 回归已启动，结果待记录。外部服务完整宿主装配、配置绑定、实际模型及公网验收仍未完成；本轮未读取凭证、未增加外部请求。

该次 `go test -race -timeout=15m ./profiles/fetchcheck -count=1` 最终以 0 退出，通过（263.056 秒）。完整包通过不替代实际火山调用或全仓 verify；全部原票验收项继续保留。

### 可配置服务的公开 Go 入口

新增 RunResearch/RuntimeConfig，调用方提供私有空目录、模型、搜索格式与端点、明确 URL/IP 范围、接收位置、披露许可及任务预算。通过既有正式任务链执行，不创建旁路循环。初始化仅增加内容发现、处理、披露所需的数据位置授权，未授予远端执行；原始输入使用 task-goal 来源，页面保留实际获取来源。配置、模型能力和本地 token 保存在私有 research-host.json，复用原独占创建与同步原语；该函数仅启动新任务，不重新授权已有目录。

外部包测试先因缺公开入口编译失败；首次装配因缺 content.discover 的接收位置许可失败（1.669 秒），补齐数据发现权限后，测试模型的 nil gaps 导致 OUTPUT_INVALID（3.561 秒），将协议输出改为显式空数组后通过（5.035 秒）。扩为 JSON/DuckDuckGo HTML 两条实际本地 HTTP 链及缺披露许可的零 HTTP/模型调用反例，通过（9.017 秒）。go vet、diff 检查和两轴增量审查通过；整包 race 回归已启动，结果待记录。

入口说明见 `22-runtime-entry.md`。当前仍沿用参考页面大小及时限，行动规划仍为协议实现；配置文件的公开恢复函数、真实公网及火山效果验收未完成。本轮无外部服务请求或凭证读取；新入口能力不意味着 22 票全部完成。

该次 `go test -race -timeout=15m ./profiles/fetchcheck -count=1` 已以 0 退出，通过（274.949 秒）。当前无该测试进程遗留；全仓 verify 和真实外部验收继续单独记账。

### 页面额度与任务网络额度统一装配

RuntimeConfig 新增 PageMaxBytes，零值保留 1024，显式范围为 1 至 1MiB；同值进入页面能力 Schema、实际驱动、行动参数和重开配置。非默认值写入传输清单，默认字段省略以保留原清单字节。初始测试因缺字段编译失败，实现中修正 uint32/int64 转换后，公开 DuckDuckGo 协议入口用 4096 字节额度完整获取超过 2KiB 的页面并发布完整引用（4.911 秒）。

将该用例 NetworkLimit 设为 3 后复现公开入口仍将页面驱动固定为 2 的问题，任务停在 WAITING/reconciliation（2.919 秒）。页面驱动初始化从参考工厂移到共同运行入口，绑定原 NetworkLimit 与 PageMaxBytes；原参考恢复逻辑仍使用同一路径。修后通过（4.926 秒），搜索/页面各一次并共计两次网络预留，不要求耗尽配置的三次额度。

补充 32 字节页面额度反例，正式应答保留 too_large 失败事实、无页面结论且保留两次预留；重开改变页面额度拒绝，原配置可重开。包含原默认 JSON/DDG 链的组合 race 通过（16.866 秒），vet、diff 检查及两轴增量审查通过。整包 race 回归已启动，结果待记录。

Content 和模型输入边界独立生效；配置更大的获取额度不承诺全部内容可一次送入模型。入口文档已明确该限制，本轮无公网或外部模型调用。

该次 `go test -race -timeout=15m ./profiles/fetchcheck -count=1` 最终以 0 退出，通过（279.820 秒）。全仓 verify 和外部验收仍未由本结果替代。

### 较慢答案模型调用的 Worker 续租

公开入口增加可取消的 11 秒模型协议延迟测试，修前在 14.644 秒失败，实际错误为答案阶段 INPUT_INVALIDATED。答案生成预留成功后，宿主现以原 GenerationPort 和 Qualification 定时提交 renew；不改变任务期限、拥有者、工作代次或模型预算，当前资格仍由 Core 检查。调用结束、父 context 取消或续租失败时退出，函数返回前取消并等待续租 goroutine，随后宿主才关闭。

相同跨租约模型测试修后通过（16.569 秒），沿用完整正式答案、引用及单次模型/网络计费断言。vet、diff 检查及两轴增量审查通过；完整 fetchcheck race 回归已启动，结果待记录。该定向成功不单独证明撤权、截止时间或全部恢复场景通过。模型为本地协议实现，无外部调用。

该次 `go test -race -timeout=15m ./profiles/fetchcheck -count=1` 最终以 0 退出，通过（300.357 秒）。该结果覆盖当前包的恢复、撤权、查询计量及新增长调用测试，不替代全仓 verify 或真实外部验收。

### 全票审查与获取完成后的控制事实

以 6d40e16 至 4442dce 的全部改动开展两轴审查。Standards 无硬规则违反，建议集中两个执行适配器重复的观察结果转换；这是维护性建议，当前未扩展重构范围。Spec 发现两项：获取成功后、首次观察前暂停或取消不能收敛（P1）；公开 Runtime 尚无读取既有配置并恢复原任务的入口（P2）。后者仍待实现。

P1 的真实 HTTP/Core/SDK 回归先复现 UNKNOWN、任务 WAITING（1.686 秒）。修复在原行动绑定、当前控制状态及事实查询预算验证后，以有限类型化事实传递获取模式和请求数，不返回正文或引用；两个执行适配器据此报告 FAILURE/CONFIRMED、无 Output，原持久化获取记录保持 acquired。该失败表示结果不能交付，并不抹去已发生的获取。

暂停/取消与原观察作用域/重建作用域四种组合，加原控制事实四种组合，race 测试通过（13.321 秒）。断言 HTTP 与网络预留各一次、事实查询一次、任务控制已应用，以及重新绑定原行动仍不能读取正文。相关 vet 与 diff 检查通过；Spec 增量复核关闭 P1、无新增发现，Standards 无硬规则违反，重复转换的维护性建议保留。

另在干净工作树 /tmp/lerna22-verification-4442dce 对冻结提交 4442dce 启动完整 make verify，日志为 /tmp/lerna22-verification-4442dce.log。当前仍在全仓 race 测试阶段，尚无最终结论；该冻结运行不包含上述 P1 修复，不能将其结果算作修复后的全仓通过。本轮没有读取凭证或新增公网、外部模型请求；原票验收项继续保留未完成状态。

### 原目录查询已发布答案

公开运行入口在任务提交成功后、外部行动前，将原 task_ref 写入私有 research-task.json；配置清单使用具名类型保持原序列化字段。新增 QueryResearch，从原目录加载身份与配置，经正式 SDK/answers.Query 查询原任务当前发布内容，不提交任务、不装配模型、不重装来源策略或授权。私有清单读取复用已有有界、防符号链接原语，并拒绝未知 JSON 字段及尾随内容；缺失清单不能通过创建新任务修复。

外部包测试先因 QueryResearch 不存在编译失败；实现后 JSON 全链回归通过（6.846 秒）。随后 JSON 与 DuckDuckGo HTML 两条实际本地 HTTP 链分别验证原答案可查询且搜索、页面与模型调用仍各一次，使用真实持久化来源策略接口撤销规则后，重开查询不能返回正文。组合 race 通过（13.073 秒），vet 通过。两轴增量审查无新发现。

这是 P2 的原任务查询前置切片：尚无未完成任务执行恢复入口，不关闭 P2，也不宣称已完成全部中断恢复。冻结 4442dce 的全仓 verify 仍在运行，且不覆盖本轮；本轮未读取凭证、未发起公网或外部模型请求。

该冻结全仓运行随后报告 profiles/answer 失败（256.269 秒）：TestPersonalizedAnswerWorkerWaitsForInvalidatedContext/unrelated 返回 CONTEXT_UNAVAILABLE。进程仍继续执行后续包，尚未有最终阶段报告；本次全仓运行已不能记为通过，需保留结果并单独诊断。

同一 unrelated 用例在主树独立 race 运行也失败（6.119 秒），这次停在用量断言：原生成已结算但 Started=0、Requests=0，尚未发生模型请求。与全仓错误表现不同，不能据此直接认定根因相同；下一步需定位上下文准备/工作续租的实际失败原因，当前未放宽超时或修改测试预期。

### 定位组合发布的外层 I/O 超时

沿原 unrelated 回归增加临时阶段诊断，两次均复现全仓同类 CONTEXT_UNAVAILABLE。后一次 Decide 在 11.193 秒成功，随后 complete 恰在 1.001 秒因 context deadline exceeded 失败，定位到 Runner 的 1 秒外层提交期限。只将 answer 参考宿主 RunLimits.IOTimeout 配为 5 秒后，同一用例通过（12.926 秒），complete 实测 1.285 秒。全部临时诊断已移除，测试只保留失败时的任务状态/停止原因以利后续定位。

清理后三种 related/revoke/unrelated 原回归共同通过（25.278 秒），继续核对原模型用量、相关变化等待、撤权拒绝和无关变化完成；vet 通过。两轴增量审查无阻断问题。Spec 提醒该配置也影响 Runner 的其他端口调用及续租调用上界，不只影响发布：底层单次存储/授权期限、租约、续租周期、任务期限及模型预算未修改。旧任务冻结的 RunLimits 不迁移，1 秒旧检查点不能假定与新配置兼容。

该证据定位并修复了完成提交的确定超时；此前 Started=0 的独立表现未取得同一根因证据，仍不能宣称全部不稳定性已消除。冻结 4442dce 的全仓运行继续保留原失败记录；尚未运行包含本次修复的新版本全仓验收。

### 原答案阶段的公开恢复

新增 ResumeResearch，复用原清单装配、token、task_ref 与当前策略。已完成任务只经正式查询返回；未完成任务必须行动已落定、有 AnswerQualification 且没有答案 Generation，才核对原模型能力并使用持久化 ActionLimits 进入既有发布链。未支持的检查点明确返回 RESULT_UNRESOLVED，不重新提交任务、重新获取或替换未知模型调用。

外部包回归先因缺公开函数编译失败；在模型能力边界取消父 context，使真实搜索/页面均已完成但答案模型尚未调用，再重开目录继续原任务，初次通过（7.299 秒）。原任务引用不变、模型使用数只增加一次、引用保持原取得正文；再次恢复完成任务不产生外部调用。加入模型契约变化拒绝、来源撤权后零答案模型/正文反例，与原 JSON/DDG 查询链组合 race 通过（21.609 秒）。两轴增量审查无新阻断发现。Standards 提醒测试中断定位依赖第二次 Capabilities 调用，模型元数据读取调整时需复核中断是否仍落在目标阶段。

这不是进程强杀测试；执行阶段中断和已有答案生成预留的恢复仍未接入公开入口，P2 继续未关闭。未增加公网或外部模型请求；冻结全仓验证仍为旧提交，不覆盖此变更。

### 原输出操作恢复已保存答案

已有答案 Generation 时，公开恢复现在核对原 GenerationLimits，只续租原资格，并复用原 ActionLimits、证据/来源/lineage 构造链进入 answers.Port.Recover；不调用 EnsureLease 接管、不预留新 Generation、不调用模型。原资格失效或没有已存输出时拒绝，不以新请求替代未知结果。

通过消费方 Call/Clean 接口依赖真实 Content；故障包装器在真实 PUT 成功后取消调用，构造已保存但未发布窗口。先复现 RESULT_UNRESOLVED（4.942 秒）；初次接入错误使用外部签名操作身份作为内部续租 change_id，返回 INVALID_ARGUMENT（5.377 秒），改用随机内部变更身份后通过（6.387 秒）。没有改变领域标识含义或放宽标识校验。

已保存、响应后未保存、保存后撤权三种窗口，与原答案生成前恢复/撤权测试组合 race 通过（26.757 秒）。反例核对无额外 HTTP/模型调用、原输出操作及原预留/已用预算不变，不能发布或返回正文。两轴增量审查无可操作发现。仍非进程强杀测试，公开执行阶段恢复继续未完成；冻结全仓验证仍不包含本轮，旧失败记录保留。

### 原行动恢复及执行服务存储生命周期

runPreparedResearch 增加原任务恢复分支：读取原 task_ref 并核对任务约束和 ActionLimits，不重新提交、不重存初始输入、不替换能力目录。公开 ResumeResearch 将行动阶段接回同一 Core/SDK 循环。真实搜索证据读取后取消调用，留下已 acquired 的搜索事实和 DISPATCHED 行动；测试初期匹配了错误的序列化字段，未命中中断，不算恢复证据。改按已取得结果的 RequestedURL 匹配后，明确复现 RESULT_UNRESOLVED（2.230 秒），接入原任务分支后首次通过（6.398 秒）。

加强原操作/Outcome/查询和网络预算断言后，组合回归失败（30.337 秒）：仅 27 条查询却进入 WAITING/budget。独立重跑有通过也有失败。临时边界诊断定位为取消后 Execution.Run 已返回，但后台观察/结果保存仍在运行；宿主关闭存储使来源检查返回 UNAVAILABLE，被呈现为 PERMISSION_DENIED，保存失败随后触发 CORRECTION_LIMIT，并非耗尽 128 条查询。

新增 Execution.WaitIdle 及研究宿主共享执行生命周期登记。初始页面/搜索服务和独立恢复服务都在使用前登记；关闭后拒绝新登记，全部回调空闲后才释放存储。前台最多等待 8 秒，超时保留后台依赖而非提前关闭；测试目录也在实际关闭后才删除。初版只等待初始两服务且忽略等待超时，审查指出 P1，已用统一登记和延迟释放修复，Spec/Standards 复核无新增可操作发现。

原失败用例连续三次通过（17.683 秒）；清除全部临时诊断后，行动恢复/撤权、已存答案/未知响应/撤权、答案生成前恢复/撤权组合在最终生命周期实现上 race 通过（36.181 秒），execution/fetchcheck vet 通过。断言搜索仅一次、页面仅一次、答案模型仅一次，原搜索操作和 Outcome 不变、查询计入原上限、共享网络预留仍为两次；撤权后不抓取页面、不调用模型、不返回正文。

仍未证明全部启动窗口恢复；RUNNING 且尚无 ActionState 的中间检查点保持 unresolved。不协作回调可能长期保留资源，前台关闭返回不代表清理已完成。未运行新版全仓验收，本轮无公网或外部模型调用。

冻结 4442dce 的原全仓进程仍在继续，随后另报 catalogcheck 失败（1839.975 秒）：TestPersonalizedActionReopensOriginalMemoryBinding 返回 CONTEXT_UNAVAILABLE。该失败与此前 answer 失败均保留，尚无最终阶段文件，不将后续包通过算作全仓通过。

### catalogcheck 失败的独立核验

在主树单独运行 TestPersonalizedActionReopensOriginalMemoryBinding，race 通过（38.280 秒）；原冻结全仓中该用例在 7.65 秒失败。当前未复现根因，不能把独立通过替代全仓失败，也未据此放宽超时或修改业务断言。

仅在 RunActionCase 的首次 Memory Context 装配与原绑定重开两个宿主返回边界增加阶段错误前缀，使用 %w 保留原错误链。没有包装 Brain 内部判定错误，也没有改变授权和恢复策略。原读取撤权拒绝回归 TestReopenedActionMemoryRejectsRevokedOriginalRead 通过（13.788 秒），继续通过 errors.Is 核对原拒绝类型；vet 及两轴增量审查通过。此为后续诊断改进，不是 CONTEXT_UNAVAILABLE 的修复证据。


### 启动窗口恢复与冻结验证终态

通过真实 Core 提交、领取、启动 API 构造尚无 ActionState 的三个窗口，再关闭并调用公开 ResumeResearch。测试装配最初缺少能力目录必填描述，修正后在已领取窗口复现 RESULT_UNRESOLVED（0.339 秒）。恢复分支补齐尚未执行的 start/Initialize；原 Worker 有效租约、控制和权限继续由 Core 检查，不接管过期初始化租约。三个窗口 race 回归通过（12.962 秒），核对原任务身份、一次领取、总共三次模型预算使用及一次搜索/一次页面/一次答案生成。不是进程强杀验收。

冻结 4442dce 的 make verify 已结束，退出码 2。依赖、生成、编译、静态分析通过；测试阶段失败，后续 profile gates 未运行。阶段结果归档至 evidence/22-verification-4442dce/stages.json。失败包括此前记录的 answer 和 catalogcheck CONTEXT_UNAVAILABLE；fetchcheck 在该次全仓运行中通过（334.853 秒）。此结果不覆盖 4442dce 后的主树变更，也不能用于关闭 22 票。本轮没有公网或付费模型调用。

本轮 fetchcheck vet 通过；Spec 与 Standards 两轴增量审查均无可操作发现。


### 下一轮评价的方法登记

新增 22-prospective-evaluation.md，固定下一轮四类已知虚构材料、期望及评分实现的 SHA256，定义真实 loopback HTTP 与固定回放两模式各四例的顺序、失败保留、分母和逐例合格规则。明确已知材料回归不代表隐藏泛化评价，历史输出不能追认为实现前预注册。

实际核对四份摘要一致；Spec 与 Standards 独立审查均无可操作发现。本轮仅登记材料与方法，尚未完成 execution-lock.json，也没有运行模型。八次拟议外部请求仍待额外预算授权。现有回放参考入口只允许 local 模型及 InputUpper ≤ 2048，下一步需先完善显式云端处理授权的回放装配；不允许通过重标模型位置使用火山 API。公网 DuckDuckGo 验收独立，仍未通过。此计划不关闭原验收项。


### 参考回放的显式模型披露

新增 ResearchConfig.DiscloseTo/ModelTokens，复用原数据位置授权装配；零值保持旧 local 限制，显式模型保留原位置、InputUpper 及任务预算。Runtime 与参考入口共用模型契约/披露校验，参考预算另保留两次行动决策的预留。冻结材料回放不发送搜索/页面请求，模型 API 是独立外部通道，不把回放表述为完全离线。

先以公开 CheckReferenceResearch 验证外部位置、224Ki 输入上界及正式双来源答案，旧接口因缺字段编译失败；实现后 race 通过（6.318 秒）。追加缺许可/错误许可/低预算拒绝和重开未知用量组合，后者失败（组合10.135秒）：reopenResearchCheckpoint 丢失 fixed-replay 页面能力标识，页面被当成发现结果，答案返回 fetch unsupported response。恢复原标识后组合通过（11.725秒），原大额未知预留保留、无重复网络获取。

本轮没有读取 .env，没有公网或付费模型调用。方法登记中的回放装配缺口在本增量补齐；execution-lock.json、模型预算授权、真实效果和公网成功仍未完成。

共享校验后的最终组合 race 通过（25.909 秒），包括新配置、重开未知预算、旧本地替换模型/拒绝规则和公开 HTTP 入口。fetchcheck vet 通过，Spec 与 Standards 复核无新增可操作发现。完整 fetchcheck 包回归另行运行，不将定向通过扩写成全包或全仓通过。


### 整票复核与已登记执行启动恢复

以 6d40e16…6174dbf 为整票范围，结合此前审查，两个独立审查重点复核 4442dce 后的整合。Standards 无新增硬规则问题，保留搜索/页面驱动结果映射重复的设计建议。Spec 指出恢复 P2 仍部分未完成：Core 已派发但尚未 Invoke、Invoke 已登记但尚未 Run，以及答案 Generation 已预留但尚未 BeginRequest。外部验收和评价锁定缺口仍保持；没有据静态复核宣布整票通过。

新增 TestActionHostStartsOriginalAdmittedExecution 复用真实 Core/SDK 和关闭重开边界，经宿主 Recover 继续已登记未启动的原操作。先复现无 Outcome、UNKNOWN（0.677秒）；加入原操作 Run 后又复现已知 denied（组合1.993秒），显示驱动仍使用旧 Worker 守卫。页面驱动新增 WithRecovery 保留原传输/网络限制并重绑当前恢复守卫与观察范围；搜索驱动同时重绑当前守卫及隐私查询端口。Execution.Run 对 Started 已落盘操作不重发，随后继续原 Reconcile/Drain。

两条等待恢复测试通过（3.186秒）；与原搜索已取得事实恢复/撤权和任务初始化窗口组合 race 通过（29.946秒）。fetchexecution/fetchcheck vet 通过；Spec 与 Standards 增量复核无新可操作问题。当前测试直接覆盖页面的已登记未启动窗口，不能扩写成全部搜索/页面或所有中断点的验证。尚未 Invoke 的行动和未 BeginRequest 的生成预留继续保留 P2。

旧全仓失败的 TestPersonalizedActionReopensOriginalMemoryBinding 再次独立通过（38.785秒），仍未复现其 CONTEXT_UNAVAILABLE 根因，不据此宣称修复。6174dbf 启动的完整 fetchcheck 进程仍活跃；该二进制不包含本轮恢复改动，结果需另记。本轮没有公网或外部模型调用。


### 未登记行动的原请求恢复

6174dbf 的完整 fetchcheck race 回归已结束并通过（382.982秒）；不覆盖其后 44d3c7d 及本轮变更，也不替代失败的旧全仓验证。

新增“Core 已 Next 派发但 SDK 尚未 Invoke”重开用例，原实现返回 PERMISSION_DENIED（0.354秒）。GetInvocation 有意不区分不存在与未授权，不能据该错误自动补交。新增 Execution.LookupAdmission 按完整原 Request 复用 Invoke 的接纳核验：合法缺失返回 found=false，拒绝和身份冲突仍返回错误。宿主仅在合法缺失时签发绑定原操作/语义的许可，经正式 SDK Invoke 登记，再沿原 Run/Reconcile/Drain；查询不是对后续执行的授权，Invoke 和 Run 保留各自当前检查。

定向修复通过（1.866秒）。与已登记恢复、原 SDK 等待恢复、篡改原资源版本拒绝以及搜索已取得事实恢复/撤权组合 race 通过（9.551秒）。直接启动窗口测试核对原 Request、原 Action Qualification、累计查询七次及单次 HTTP；篡改原请求时零 HTTP。execution 包命令仅完成编译（该包无测试文件），不计作运行测试；execution/fetchcheck vet 通过。

答案 Generation 已预留但尚未 BeginRequest 的窗口仍待实现，P2 未全部关闭。本轮没有公网或外部模型请求。


本增量 Spec 审查发现已有记录也要求 invoke 权限，会阻断只保留 read/reconcile 的恢复。新增真实请求完成后撤销 invoke 的反例；测试初次使用空输入未到目标边界，改用有效获取参数后复现 found=false/PERMISSION_DENIED（0.651秒）。LookupAdmission 现对已有记录检查当前 read、服务归属、原操作所有权和完整 Request；仅缺失记录走 Invoke 准入核验。Run 已 Started 分支不重发，未启动记录仍检查启动许可。

最终组合 race 通过（10.170秒），含该撤权反例、篡改请求、未登记/已登记恢复、原 SDK 等待恢复及已取得搜索事实恢复/撤权。vet 通过，Spec/Standards 复核无新增可操作发现。已有回执不依赖 invoke 的发现关闭；未 BeginRequest 的答案预留仍保留。


### 答案预留尚未 BeginRequest 的恢复

真实 Core ReserveDecision 后关闭存储，再用公开 ResumeResearch 继续原预留，旧实现因只查找已保存输出返回 PERMISSION_DENIED（3.250秒）。现在保留原预留、输出操作和 Qualification，仅原 Started==0 且未 Settled 时交给原 writer 首次生成；已开始或已结算仍只查找原输出。BeginRequest 的事务单次门保留，并发恢复也不能重复派发。完成用量校验区分已有预留与本次新增预留，恢复不再错误要求多出一次用量。

定向通过（4.935秒），随后原未开始、已 BeginRequest、零请求已结算、来源撤权四类，与已保存答案、响应未保存、保存后撤权组合 race 通过（26.260秒）。正例核对仅一次答案调用、HTTP不重取、原 Generation/OutputOperation/Qualification 不变；已开始及已结算反例保留原用量，不生成替代请求。撤权后零模型调用且不返回正文，尚未派发的零请求可按既有结算语义结束，不假称它消耗了外部请求。

fetchcheck vet 通过，Spec/Standards 增量复核无新增可操作发现。此前整票复核指出的三个具体恢复窗口已各有代码修复及对应边界测试；不将这些服务边界测试扩写成全部进程强杀或过期资格接管的证明。公网正向、实际模型效果、评价执行锁定和新版全仓验证仍未完成。本轮无公网或付费模型请求。


### 0ebbc06 整合审查

结合此前整票审查，Spec/Standards 独立复核 6174dbf…0ebbc06，未发现新增实现问题；三个已列明恢复 P2 具体窗口关闭，结果映射重复的既有维护建议保留。完整结论及实际审阅范围见 22-review-0ebbc06.md。固定 0ebbc06 的 make verify 已进入测试阶段且仍活跃，尚无终态；原外部验收及评价执行锁定缺口保留。本轮不根据审查通过关闭票据。


### 0ebbc06 全仓验证终态

固定工作树 `/tmp/lerna22-verification-0ebbc06` 的 `make verify` 已以退出码 0 完成。依赖、生成、编译、静态检查、全仓 race 测试及脚本列出的全部协议/参考验收关卡均为 passed，原始阶段报告见 [stages.json](evidence/22-verification-0ebbc06/stages.json)，命令输出见 [verify.log](evidence/22-verification-0ebbc06/verify.log)。本轮 profiles/answer 249.975 秒、profiles/catalogcheck 1783.793 秒、profiles/fetchcheck 387.689 秒通过。此前失败记录保留；本轮通过不构成先前 catalog 偶发失败根因已修复的证明。

这次验证覆盖固定代码 0ebbc06；主工作树随后仅增加整合审查文档。验证未访问凭证或执行新增付费模型请求，也不证明 DuckDuckGo 公网正向、真实模型语义质量或评价 execution-lock 已完成。22 票保持进行中，原外部验收缺口保留。


### 真实模型评价 v2：8 例失败保留

用户追加授权开发验证模型累计费用低于 500 元，替代此前等待追加请求数的条件。执行器 8f221c9 在提交 execution-lock 后按 loopback 四例、replay 四例顺序运行；每例仅一次真实火山答案请求，共 8 次，已知输入 8670 token、输出 2957 token。按请求预留的 8 元是保守管理额度，不是供应商账单，历史实际账单未由本工具读取。

运行正式答案成功数 0/8；两种模式各有三例 OUTPUT_INVALID、一例 conflicting 因 OUTPUT_TRUNCATED 失败。原始模型请求、响应、Core/网络用量快照及失败均保留在 evidence/22-evaluation-v2-8f221c9。执行器退出码 0 仅表示完成采集，不代表评价通过；空正式答案不计合格，不以模型原始文本替代正式发布。下一轮若修改输出或引用生成策略，须另行版本化计划，保留此次全部失败。

执行前审查修复：搜索响应上限实际按计划4096；原始输入先于外部调用落盘；失败用量在宿主关闭前经Core和预算接口采集；重开失败无可用宿主标记 unavailable。参考模型回归 race 84.093 秒，失败/重开失败用量 race 11.983 秒；build/vet与两轴增量复核通过。固定7f54976的全仓验证仍运行，它不覆盖随后评价工具及失败报告改动。

### 2026-09-13 真实模型及公网执行结果

用户授权开发阶段累计模型费用低于500元，已进行五轮各8例真实模型已知材料回归。v2运行0/8，v3运行8/8但语义6/8，v4运行/语义6/8，v5与v6均7/8。原始输入、ProviderContent、展开结果、正式答案和独立评价分别保留，失败不删除；不能视为盲测或完整V1指标。

公网v1在创建任务前失败，修复私有任务目录后v2已通过真实治理链取得搜索候选；两个越权目标拒绝、两个页面超时，无正文，模型512token截断，未发布答案。公网完整问答未通过。五轮及公网共41次模型调用，输入52816/输出12221token，管理预留不是实际账单。详见22-public-acceptance.md。

增量测试：失败发现上下文及用量恢复组合race20.571秒；Ark/Brain契约与CLI配置限制race通过；Doubao宿主延迟及恢复组合race19.302秒；相关vet通过。旧工作树7f54976的全仓验证在catalogcheck/TestActionCheckpointDoesNotDuplicateSnapshotDigests报告PERMISSION_DENIED，定向一次61.433秒通过；该次全仓不能记为通过，也未覆盖后续代码。继续保留未完成状态。

全仓7f54976现已结束，退出2（make），测试阶段失败；fetchcheck包435.045秒通过，不能抵消catalogcheck失败。三次定向复查181.339秒也出现memory binding: CONTEXT_UNAVAILABLE，未得到稳定通过证据。完整日志与阶段JSON保存于evidence/22-verification-7f54976。

用户随后授权取消512输出上限。已将公网输出额度改为可配置，CLI默认4096；保留旧任务零值512恢复兼容，原Generation额度、实际用量和总预算继续一致。8192字节答案界独立保留。新增4096真实HTTP及原任务恢复测试先红后绿，Spec/Standards增量无明确发现。新公网验收另行登记，不重写历史。

4096配置公网v3已运行：模型917输入/207输出，发布fetch_failed；失败发生在搜索阶段，没有页面请求。模型却归因为页面请求被拒，独立复核判定语义误归因。未重现原四来源输入，不能以本轮未截断认定原多来源问题已解决。完整记录见22-public-acceptance.md；原失败与未通过状态保留。

用户要求直接使用豆包总结后，NeedSummary/独立Summary视图/无抓页模式已实现。总结v4真实搜索1、页面0、模型6891/482tokens，正式答案；独立评价事实2/2、引用精确4/4及语义4/4通过。历史总结v1/v2/v3保留失败。详细见22-search-summary-mode.md及22-public-summary-review.md，整票全仓回归未完成不混同本项成功。

### 466c70f 收尾验证启动

最终双轴整合审查见22-review-466c70f.md：无新增阻断代码发现，保留一项非阻断重复转换维护建议，当前全仓验证仍是待完成项。

为定位原catalog恢复失败，临时记录Session内组装/复核阶段及父ctx状态，再运行 `go test -race ./profiles/catalogcheck -run '^TestActionCheckpointDoesNotDuplicateSnapshotDigests$' -count=3 -timeout=5m -v`。三例均通过，总132.639秒，各43.16–44.25秒，组装/复核各约0.95–1.03秒，父ctx未失效。原始观察见[evidence/22-recovery-observation-466c70f](evidence/22-recovery-observation-466c70f/observation.json)。该结果没有捕获失败，不能证明旧失败根因已修复；未放宽超时、跳过授权或更改业务实现。临时诊断已移除。

当前代码466c70f启动 `make verify`，保留原脚本全部阶段及race/进程恢复测试，不通过单例结果追认旧失败。执行期间仅整理验收文档，未改Go代码或测试配置。未新增公网或付费模型请求。


### 466c70f 全仓终态与搜索回归修订

该轮make verify退出2。catalogcheck完整1762.094秒通过，包含此前间歇失败的恢复测试；不声明旧失败根因已修复。fetchcheck在413.024秒汇总四个失败，其余包通过；后续参考验收关卡未运行。原始记录见[evidence/22-verification-466c70f](evidence/22-verification-466c70f/metadata.json)。

四项失败均为d703cc0保留搜索发现上下文后未同步的旧断言：三个测试把所有输入块当成页面或页面拒绝，第四个仍使用未包含搜索上下文的旧计量基线。修订只涉及四个测试文件，不改变运行实现、权限或额度：按Role区分search-candidates与页面/失败；检查两条失败引用属于实际失败集合；回放仍必须零HTTP且同时保留discovered和denied。

计量保留精确检查。搜索响应新增一次长度GET及五次正文READ，分别用于组装、合并上下文复核、模型调用前、模型返回后、发布前；因此固定回放为61总查询/53 Content查询/2 Outcome查询。loopback最低62总查询，上限64不变，不重复原Input GET。

上述四项及loopback计量测试组合race通过，48.240秒。Spec与Standards分别复核，无新增可操作发现；没有以提高额度或删除断言掩盖失败。需要最终完整验证成功后才能关闭票据。


### 8ff71c6 最终通过与关闭

最终make verify已退出0，28阶段全部passed。修改后的fetchcheck完整race409.514秒通过；未变更包由Go测试缓存合法复用，包含前一轮实际执行通过的catalogcheck1762.094秒。脚本全部参考验收关卡实际执行，原始报告、构建信息与命令日志归档于[evidence/22-verification-8ff71c6](evidence/22-verification-8ff71c6/metadata.json)。vcs.modified=true标记原样保留，私有本地验收目录未入库；本轮执行期间受跟踪代码和测试无变化。

两轴最终审查及四处测试修订增量无未关闭阻断发现。22票更新为complete，导航同步；[最终验收汇总](22-final-acceptance.md)列明完成范围和限制。旧catalog间歇失败根因仍未证实；原失败、模型已知材料限制及非阻断重复转换建议保留。本次收尾不增加外部请求或模型费用。
