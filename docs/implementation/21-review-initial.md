# 21 初次双轴审查

基点 `cab3975`，候选 `f5a84b9`，命令 `git diff cab3975...HEAD`。两个独立子 Agent 按 code-review skill 并行只读审查。此记录不是交付通过；全仓验收尚未完成。

## Standards

未发现文档标准的硬违反；1 项判断性气味。

- **P3，可选：Possible Duplicated Code。** `profiles/fetchcheck/session_test.go:35` 和 `profiles/fetchcheck/brain_test.go:46` 重复真实获取、提交消费任务、预留决策和装配上下文的准备逻辑。建议仅提取共同测试准备，保留各自 SQLite 重启、发布和撤权断言。该建议为 code-review 气味基线的判断性建议，不是硬性规范。

## Spec

发现 2 项代码缺陷、1 项部分实现，均待修复。

1. **P1：预算耗尽被写成未知效果。** `adapters/sqlitefetch/store.go:158` 在额度不足时返回 LimitExceeded，不保存操作；`adapters/fetchexecution/driver.go:96` 随后只能返回 UNKNOWN。Execution 只采纳 Inspect，因此零联网的预算拒绝变成 UNKNOWN/WAITING。违反票据“未知和不可恢复状态如实可查询”。应持久保存原操作的零请求拒绝终态，不增加消耗或退还既有额度。
2. **P1：正式查询丢失具体获取失败原因。** `adapters/fetchexecution/driver.go:132` 将具体 status/requests 仅放入 Evidence；Execution 对无 Output 的失败跳过保存，快照不保留 Evidence。SDK/Core 对多种失败最终只有 FAILURE 和空引用。现有夹具直接查私有 ledger 才能区分。违反票据“超时、拒绝、过期、取消、不可访问……分别表达”及共享规格“调用者可观察的行为”。应提供受当前授权保护的有限失败事实出口，并验证 SDK 查询。
3. **P2：Task Context 缺口路径尚未实现。** `adapters/fetchcontext/context.go:89` 遇读取失败整体返回错误，成功块固定 acquired，不能输入可披露的失败/不可恢复事实。违反票据“Task Context 使用仍保留证据来源和缺口”。应保留撤权拒绝，同时支持经过授权的有限缺口材料。

验证状态单列：公共 HTTPS 两次均在 resolve 阶段失败，原始记录保留；全仓验证尚未完成。未发现额外范围扩张。

Standards 共 1 项可选维护建议、0 项硬违反；Spec 共 3 项，最高优先级 P1。待修复后复审，不以任何一轴掩盖另一轴的结果。

## 修复后待复核

三项 Spec 问题均已增加实现和定向验证：预算拒绝原子持久化；SDK 读取受控失败产物；已授权失败产物进入 Task Context 并在撤权后拒绝。详见 `21-content-acquisition.md` 的“初次审查三项 Spec 问题的修复”。该记录仅说明修复提交，尚不等于独立复审通过。

Standards 的测试准备去重建议保持可选，本次优先修复可观察的失败语义，未据此增加生产抽象。

## 修复后 Standards 复核

复核增量 `f5a84b9...f0a8638`：0 项新增硬违反，0 项新增实质维护建议。失败状态集合已通过 fetch.IsFailureStatus 统一；受控失败产物读取与 Task Context 投影分别承担授权读取和上下文组织，未见值得合并的职责重复。预算拒绝保留原操作身份及零请求事实，与 CONTEXT.md 的 Operation、Task Context、效果确认定义一致。

此前 P3 测试准备代码重复仍为可选建议。Standards 累计 0 项硬违反、1 项可选维护建议。

## 修复后 Spec 复核

复核 `cab3975...f0a8638`，重点增量：原 3 项已解决，未发现新增具体缺陷。

1. 预算拒绝终态已解决：store.go 同一事务保存原 intent 与零请求 limit_exceeded outcome，不增加 charged；后续 Inspect 可恢复明确未执行结果。SDK 夹具核对第三次操作被拒绝、前两次用量不变、重放零新增请求。
2. 正式查询失败原因已解决：driver.go 为失败返回有限诊断 Output；Execution 保留 FAILURE 并沿 Schema/Content 出口保存。SDK 可通过受控引用区分 403/404/410，失败正文和 URL 不进入诊断产物；保留或读取不获准仍拒绝交付。
3. Task Context 缺口已解决：context.go 将宿主选定失败引用投影为 external-evidence-gap，保留状态、模式、请求数及引用；FailureReader 在释放前再次检查来源权限。总引用数和输出尺寸有界，撤权继续整体拒绝，不从读取错误猜造缺口。

验证边界：两个 Agent 独立只读核对代码与定向证据，未运行长测试。公共 HTTPS DNS 失败记录保留；主 Agent 启动的全仓 make verify 尚在运行，此处复核不等于整票验收通过。

复核后 Standards：0 项硬违反、1 项可选建议；Spec：0 项未解决代码缺陷/部分实现，原 3 项已解决。全仓验证门槛仍未完成。

## 最终交付核验

上述运行中状态为历史记录。全仓 `make verify` 会话 83654 已退出 0，28 个最终阶段及各 profile 必需用例通过。最终报告见 [21-final-verify](evidence/21-final-verify/README.md)，完整范围见 [验收核对](21-acceptance-audit.md)。Spec 无未解决具体缺陷；Standards 的一项可选测试去重建议保留。公共 HTTPS DNS 失败与早期目录回归超时记录均保留；不声明公网成功或真实模型质量通过。
