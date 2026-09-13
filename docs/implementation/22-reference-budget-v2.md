# 22 票参考预算 v2 与收敛方向

64次查询是原参考装配的工程选择，不是业务要求。计量补齐后，正常多来源工作超过它。继续将所有实现压回64会使工程限制反过来支配架构，因此保留原计划及失败结果，另设明确版本的运行配置。

## 实现与版本边界

原 CheckFrozenResearch、Replay、Reopen 及 frozen-*-v1 CLI 入口仍使用64，拒绝查询额度覆盖。新增 CheckReferenceResearch(ctx, caseID, ResearchConfig) 和 reference-loopback-v2/reference-replay-v2：MaxQueries 在运行前配置，范围1–128；128沿用内核既有保护上限，没有修改内核的预算语义。CLI v2 默认128，可用 -queries 指定更低额度。

额度在原任务 ActionPort.Initialize 时固定，恢复沿用已存 Actions.Limits 与查询记录，不按新工作代次重置。所有实际观察继续计量；没有删除必要权限检查，也没有将任务观察挪到外部核验者。CLI报告QueryLimit；API记录的Usage.QueryLimit直接取完成任务的实际Core限制。

原模型请求、Token、行动操作、网络请求及时间上限保持。可选AnswerModel沿用原本地处理及输入上界校验，不开放未经授权的远程模型处理。

## 证据

新增CLI反例最初因不支持配置而失败。实现后v2冲突定向race通过（6.299s），CLI全包race通过（19.607s），包含：新预算明确报告、非法额度在执行前拒绝、v1不可覆盖、v1冲突继续失败、v2极低预算停止且无正式结果。随后加强v1失败必须来自queries=64/64的断言，最终验证结果另记。

实际命令 `go run ./cmd/searchcheck -profile reference-loopback-v2 -case all` 退出0，报告保存在 `evidence/22-reference-v2/loopback.json`。四例查询分别为可回答59、证据不足59、来源冲突76、获取失败56；模型请求均3次，网络搜索均1次，页面请求分别1/1/2/1。此报告产生于新增API逐例QueryLimit字段之前的WIP检查点，顶层QueryLimit已记录128。

这些是本地HTTP和协议模型的功能结果，SemanticQuality仍为not_evaluated；不是公网搜索、真实模型质量或原v1验收通过。功能、回放、重开和可替换模型正向测试已明确切换为v2，原64基线单页计量和冲突超限检查继续存在。两轴增量静态审查均无新增可操作发现，相关组合回归正在运行。

## 尚待完成的主线

1. 完成v2组合回归、当前变更整体验证并整理提交；不再把任意任务必须小于64作为完成门槛。
2. 实际搜索服务适配与可配置运行装配。当前只有统一JSON适配器和本地参考服务，没有公网供应商验收。
3. 真实模型接入及独立小型答案/引用评价。遵守已有凭证与费用约束，未获准的额外付费调用不执行，协议模型结果不替代它。
4. 核对原22票所有验收项，完成最终全仓门禁和以6d40e16为基点的整票双轴审查。任何未验证条件继续保留，不提前关闭票据。

已发现的行动内联目标权限缺口也在本检查点修复：当前InputRefs可读不代表task-goal可处理或披露。真实Core/Content反例旧码在仅撤销目标后仍返回模型输入（0.258s）；研究宿主现在于身份验证时检查当前目标process/disclose，装配和后续行动边界共用。目标/答案撤权及原计量组合race通过（12.865s），两轴无新增问题。

后续v2四类功能、回放、原任务重开、部分失败与替换模型组合race通过（92.294s）；加强v1必须因64耗尽失败后的CLI定向race通过（6.381s）。以8f00505为基点、覆盖全部当前源码与未跟踪测试的检查点Standards/Spec审查均无可操作发现。fetchcheck、Brain和全部adapters的完整race仍在运行，本检查点未据此关闭22票。

该完整模块回归随后结束：Brain与全部adapters通过；fetchcheck在177.844s报告唯一失败 TestResearchInputValidationChargesCurrentAuthorityProbe。测试在收紧网页权限时也移除了目标权限，新增目标守卫提前拒绝，未发生它所要断言的输入READ。现仅修正测试前置条件，保留原目标权限、继续收紧网页权限及断言被拒READ计费。目标单独撤权与此测试的定向race通过（2.953s），两轴确认修正未削弱语义。生产代码未改；此前整包失败记录保留，没有重写成全绿。最终make verify仍待整票集成阶段。
