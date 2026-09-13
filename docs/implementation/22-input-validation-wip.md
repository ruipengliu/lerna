# 22 票输入复查计量：过程与增量验证

基于 535b630 开始的输入复查计量增量。下文按发生顺序保留失败与修正证据；增量验证结论见末尾，22 票整体验收仍未完成。

研究宿主 Validate 通过原 Task Qualification 的 taskcontent 执行一次 READ（Limit=1），检查当前 process/disclose 权限、完整存储内容校验与输入元数据。拒绝的读取也消耗额度；基础获取宿主没有配置 queries 时保留原路径。搜索执行宿主同时接入原端口。

研究装配的实际读取已有权限检查，因此移除其前置重复读取。能力目录操作只检查任务身份与目录自身授权，不消费输入产物；后续模型、保存与执行边界仍复查输入。当前输入引用也须与 Core 中的引用一致。

验证事实：

- 新测试接入前失败：`input validation bypassed task query budget`。
- `go test -race ./profiles/fetchcheck -run TestResearchInputValidationChargesCurrentAuthorityProbe -count=1` 通过（2.745s）；实际 Content 策略降为 discover-only 后拒绝处理，拒绝尝试计费。
- 单页面获取失败流程观察到 58 次总查询；其中账本 4 次、Content 39 次，输入复查新增 17 次。原精确计量断言相应更新，定向复跑通过（1.976s）。
- 双失败及来源冲突流程仍在答案决策阶段失败，均观察到 `queries=64/64 state=RUNNING`；前者返回 QUERY_BUDGET_EXCEEDED，后者经 fetch 适配器映射为 fetch unavailable。
- 两种部分失败顺序也返回 QUERY_BUDGET_EXCEEDED。

64 次硬上限保持，未增加额度、未运行外部付费调用、未将失败标成成功。下一步需要核对必要读取与重复验证的消费边界，完成双页面流程后再运行相关完整回归。此记录不替代最终全仓验证与双轴审查。

## 后续证据读取合并

fetchcontent 现在仅记住最多八个不可变引用的已验证长度。再次读取直接 READ，按本次返回的记录重新检查元数据、来源、完整 SHA 和实际正文；没有缓存正文、权限或当前可用性。每个并发调用均实际读取 Content。READ 返回 CONTENT_UNAVAILABLE 时，以另一次当前 GET 保留原过期分类；该失败路径的额外查询也计费。

真实 Content 边界测试先观察到两次读取四次查询，合并后为三次；进一步测试发现缓存路径将过期降为 unavailable，补充当前元数据查询后修正。并发读取、discover-only 撤权、过期回归 race 通过（2.094s），相关上下文/证据 Brain/控制 race 通过（11.305s），vet 与 diff 检查通过。

单页面获取失败流程的新计数为总查询 57、Content 38、Outcome 4。双失败和部分失败仍在答案决策阶段耗尽 64 次额度；双成功冲突流程已推进至最终发布，但仍耗尽额度并返回 INPUT_INVALIDATED。以上仍是未完成状态，不能沿用此前提交的完整 profile 通过结论。

## 纯快照判断与查询观察的边界

分阶段诊断进一步确认双页面行动阶段已有 47 条查询。ActionBrain 新增可选 SnapshotAssessor：可信宿主无须获取新数据即可判断时返回纯计算结果，需要观察时返回 false，沿原 goal 计费调用 Assess。两条路径共用后续的输入复查与 Core 状态转换。接口禁止 I/O、获取新事实或用旧事实替代当前授权；研究宿主仅计算阶段，旧宿主完全保持原计费路径。

研究宿主在行动待执行、没有完成事实、或所需不可变结果已读取时可以纯计算。未知账本结果及尚未判定的搜索内容仍从原端口读取和计费。单页失败从 57 降至 52，Content 38 与 Outcome 4 不变，减少的是五次没有新查询的阶段计算。

普通相关用例运行（19.230s）中，双页面冲突、两种部分失败与双失败均已通过；唯一失败是旧精确总数断言仍期待 57，已改为 52。完整相关 race 中 Brain 已通过（1.041s），fetchcheck 已通过（170.420s）；catalogcheck 在默认 10 分钟测试总时限处被 Go 终止（600.048s，当时正在 TestCurrentMemoryControlsActualActionStart），未完成全部测试。已按 Makefile 的 `-timeout 45m` 单独重跑；未更改任何任务预算或单次调用期限。最终全仓验收仍未运行。

## 增量回归与独立审查

按仓库时限重跑的 `go test -race -timeout 45m ./profiles/catalogcheck -count=1` 已通过（2034.659s）。结合上述 Brain 与 fetchcheck 完整 race，输入复查、证据长度复用及纯快照判断这批改动已完成相关回归；没有增加任务查询额度。该长测试在后续 Core 恢复修正之前编译，不作为那些新改动的验证证据。

以 b390a93 为基点，对本批工作区差异及新增文件进行独立 Standards/Spec 静态审查，两轴均无 actionable findings。研究 CLI 的执行内部计量、实际模型验收等剩余范围仍按原票保留；未运行最终 make verify。
