# 端侧记忆提取验证入口

在仓库根目录运行：

```sh
sh scripts/verify-extraction.sh
```

需要 Linux 和支持 race 检查的 Go 1.26.1 工具链。首次构建需要可用的 Go 模块依赖；运行期间使用隔离的本地目录、文件和 SQLite，不要求外部数据库、模型服务或 `.env` 凭证。

脚本构建质量 CLI，运行相关包的 vet、race 契约测试及真实子进程退出测试，最后输出固定数据集质量报告。测试包串行运行，各并发用例仍保留实际竞争；单包总超时为 45 分钟。脚本遇错退出，后续阶段保持 `not_run`，已失败阶段保留 `failed`。

输出位于 `build/extraction-verification/`：

| 文件 | 含义 |
| --- | --- |
| `stages.json` | 脚本结束时写入的各阶段状态。`ticket_acceptance: not_evaluated` 表示此脚本不代替整票验收。 |
| `tests.jsonl` | `go test -json` 原始事件，包括实际运行的测试名、子用例、通过/失败和耗时。 |
| `quality.json` | 固定公开数据集的摘要、样本结果、正确/缺失/误报、覆盖范围及限制。 |
| `extractioncheck` | 本次构建的本地质量验证 CLI。 |

单独生成质量报告：

```sh
go run ./cmd/extractioncheck -profile local-rules-quality-v1
```

CLI 仅运行 `local-rules-v1` 与预先固定的 `profiles/extractioncheck/testdata/quality-v1.json`。质量失败或运行失败返回非零退出码；未知 profile 和多余参数拒绝。报告中的外部模型请求数为零。规则质量、确定性契约模型的集成结果和真实模型效果分别解释，不能相互替代。

当前契约测试覆盖偏好/推断提取保存、独立任务读取与发布、条件不适用、来源失效和已接入报告消费者清理、触发登记/执行绑定、撤权/过期、原提交/保存/不支持事实的恢复等已实现路径。实际执行范围以 `tests.jsonl` 为准。

宿主可以通过 `extractionexecution.WithRetentionDeadline(driver, unixSeconds)` 为特定提取能力增加绝对保留期限。该配置只收缩期限，不接受来源正文或模型输出作为配置；恢复宿主时须恢复同一截止时间，不能重算相对 TTL。候选保存原有效期限，自动 Memory 写入继承它；期限已过的新操作在来源读取前拒绝。此配置不追溯删除原已提交内容，也不代替来源失效及消费者清理流程。`TestInferenceRetentionCapSurvivesAutomaticSave` 和 `TestExpiredRetentionConfigurationCannotCreateCandidateOrSaveIntent` 验证真实 SDK/候选/Memory 路径。

持续扫描的宿主轮询入口是 `extraction.NewTriggerPoller(dispatcher, currentSources)`，每次显式唤醒调用 `Poll(ctx, triggerID, sourceScope)`。一次调用至多查询一个登记来源的修订元数据并分派一个任务，总超时五秒；不创建后台 goroutine，也不读取来源正文。宿主负责唤醒频率及逐来源调度，提取正文仍由已有 Core/Execution 执行路径处理。轮询使用当前快照语义：多个未观测中间修订可以合并为最新修订；需要逐事件交付的宿主应从自己的持久事件源调用 Dispatcher，不能将快照轮询报告为无损事件日志。

Dispatcher 的输入准备实现可使用 `adapters/extractioninputs.New(content, clock, binding, resource, allocate)`。它把实际触发修订写入受控 JSON 输入，并以同一修订设置 Content 来源标签；输入留存期和任务截止时间均不晚于登记期限，单轮最多五分钟。Content PUT 独立检查该修订的存储策略，不再借用固定测试来源的权限。Content 操作与任务提交操作保持不同身份，任务恢复仍沿 Dispatcher 已持久化的原提交；本适配器不读取来源正文。当前 JSON 输入契约对修订的精确整数支持上限为 2^53，超出范围在准备阶段拒绝，不静默截断。

本地 `localextractionsource.Source.Current` 从可信宿主清单选择最高修订，再检查当前计算位置、用途、披露位置和期限。最新修订被拒绝时不回退到旧修订。清单更新与文件观察由宿主负责，返回元数据不证明文件正文可读；任务读取时仍须通过真实文件校验。登记/轮次/原提交由 SQLite 恢复，撤权和取消在元数据读取前及分派前复查。测试包括原任务重用、总轮次限制、元数据读取期间撤权，以及 Core 提交前后真实进程退出后的轮询恢复。

运行时来源纠正/策略变更使用 `Source.Change(ctx, fence, invalidation, replacement)`：宿主提供权威的命名空间和旧修订截止值，替代 Entry 必须使用同一来源的新修订，策略变更同样不能复用旧修订。方法先持久化旧修订失效，再启用新配置；失败或未知回包不启用替代配置。`replacement == nil` 移除截至该修订的登记，不删除宿主源文件，也不移除已有更高修订。所有业务读取继续使用 SourceGuard，原始 Replace 仅用于可信装载/配置，不能替代运行时失效联动。宿主仍负责清单持久化和重启后重放同一权威变更；退休数量只表示本地候选，不表示 Memory 或外部内容已清理。

原始来源派生的 Content 服务使用 `extractionsourceguard.NewContentPolicy(sourcePolicy, fence, namespace)`。它在当前来源策略检查前后复查同一持久屏障，既约束正文也约束元数据，并转发 Content 的同步授权视图。该路径只读取来源策略及屏障，不为元数据校验打开原始文件。参考宿主的任务输入、执行产物和报告回退来源都接入它。屏障拒绝释放与物理清理分别处理：宿主仍须向 Content 的可信 InvalidateSource/Clean 端口交付来源事件，确认实际清理，不能把拒绝读取当作内容已删除。

宿主通过绑定真实身份的 `TriggerRegistry.Cancel(ctx, triggerID)` 取消原登记。该路径要求独立的 `memory.scan.cancel` 当前权限；`memory.scan`、来源读取或保存权限都不隐含取消权。取消不依赖扫描权仍有效，也不依赖登记未过期，避免停止操作被原工作权限的撤销阻断。调用前及存储变更前分别复查权限和登记主体/位置/用途；以原登记 ID 幂等，不新建取消操作 ID。重复调用仍需当前取消权。它只停止该登记后续扫描效果，不伪称撤回已经发生的保存或 Core 任务效果；底层 `CancelTrigger` 仍仅供可信存储层使用。

候选到期维护入口为 `extraction.NewExpiryWorker(candidateStore, clock, namespace, subject, limit)`，`limit` 范围 1–16。宿主显式调用 `Run(ctx)`，按可信时钟、命名空间和主体退休最早到期的候选；随后运行既有 `CleanupWorker`，处理这些候选的原 Memory 保存。SQLite 在同一事务内删除候选活动正文、清除保存意图正文并写入原操作退休事实。返回数量只表示候选及保存意图退休，不能据此宣称 Memory 或外部消费者已清理。宿主需要持续安排维护唤醒；本入口没有隐含后台循环。这里的删除指活动存储内容清理，不证明介质空闲页、备份或未接入副本的数据擦除。

第 20 票仍在实施：上下文装配的进程恢复、持续调度及完整出口/消费者库存等要求仍需核对完成，之后还须运行仓库完整验证与双轴审查。不要仅凭本脚本成功勾选全部验收项。功能与证据记录见 [实施记录](20-memory-extraction.md)，最终验收依据仍是原票 Agent Brief。

原始来源到 Content 的持久清理发现入口为 `extraction.NewSourceCleanupWorker(store, sink, binding, limit)`，SQLite `ListSourceFences` 从已持久化屏障枚举最新失效截止修订，不依赖候选正文存在。Content 接收器使用 `extractioncleanup.NewContent(content)`，依次持久化失效、执行一次有界清理、核对剩余 cleaning 数量。宿主显式唤醒 `Run`；每轮最多 16 个来源、总计五秒、单来源一秒。来源失败仍推进调度位置，到末尾清空游标后再扫描，因此较早来源失败不会永久阻塞后续来源，游标之前提高的截止修订也会在下一轮发现。

该游标按来源身份稳定排序，不是事件序号，也不是删除确认。来源工作器必须使用区别于候选清理工作器的 Consumer 和配置摘要；进度通过现有 SQLite CAS 保存。`Complete` 只说明本次配置的接收器在核对时完成，不涵盖其他 Memory、上下文、运行材料存储或副本。后台调度由宿主负责，不因登记工作器自动启动。`TestSourceCleanupRecoversUndeliveredFenceAfterProcessExit` 在零候选情况下持久屏障后直接退出子进程，在 Content 尚未收到通知时重启，验证实际输入文件从存在到删除；公平性测试验证失败项不误报完成、后续项继续、下一轮恢复重试。输入显式超过内联阈值，避免以从未存在的文件作为删除证据。

全仓入口 `sh scripts/verify.sh` 已加入 `memory_extraction_quality` 阶段，输出 `build/memory-extraction-quality-report.json`。完整 race 阶段覆盖提取执行和恢复，新增阶段单独运行固定本地规则数据集，避免把规则质量与执行契约合并评分。当前运行与审查结果应以原始日志和 `20-review-findings.md` 为准；具备入口不等于本票最终验收通过。

逐修订本地 Memory 清理适配器为 `extractioncleanup.NewMemory(store)`，绑定独立 Consumer 后交给 SourceCleanupWorker 调度。输入仅接受可信宿主已经持久化的权威来源失效，不向插件/用户暴露裸 Store 权限。适配器完成值不包括精确事件的下游确认；当前精确消费者和备份恢复扩展仍待完成，不能将本地擦除报告为全链路完成。

精确 SourceErased 事件的三个消费者现均有实现：`memorycleanup.Admissions` 先核对持久擦除证明再清除原授权比较材料；`memorycleanup.Contexts` 使用 Context 的 `InvalidateRevision` 退役匹配快照/检查点；`memorycleanup.Artifacts` 使用 Content 的 `InvalidateRevision` 并核对实际文件清理结果。它们通过现有 SourceConsumer 在效果确认后推进各自位置，完成值只覆盖配置的消费者。来源屏障及精确事件的备份恢复扩展仍未完成，当前不可用该路径宣称恢复成功。

后续恢复增量已补齐上述待办：恢复 v3 清单包含原始来源屏障和精确擦除事件，Restore.Reconcile 验证并事务执行精确擦除，持久拒绝屏障/事实回退。sanitized 仍不解除普通运行时 quarantine；ReadVerified/ReadView 每次读取需要实时权威证明，宿主继续控制授权及数据驻留。精确消费者与隔离恢复现有实现和定向证据，但全链路验收以最终完整验证和规格复核为准。

最终状态：代码候选 `0899d9f` 已完成全仓验证和双轴复核，第 20 票原范围验收完成。上文“待完成”是实施阶段记录，当前结果以 [最终验收](20-final-acceptance.md) 及所附原始报告为准。专项脚本仍只报告自身范围，不自动判定整票或模型质量。
