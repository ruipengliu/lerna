# 03：应用入口与四类数据流程

Status: ready-for-agent
Progress: completed
Blocked by: 01

## Scope

以现有应用／SDK 主流程降低普通调用者需要管理的概念。新增 `application-workflow.md` 与 `request-data-flows.md`，更新交互模块的 Session／输入说明。

Owned files: `docs/architecture/.draft/application-workflow.md`、`request-data-flows.md`、`interaction/README.md`、`interaction/implementation.md`、`interaction/session-and-task.md`。

## Acceptance

- [x] 从提交、查询、控制、原输入消费、结果读取和恢复连成可操作的入口说明；准确对应现有方法或明确应用内部组合，不能虚构 session.* 或新撤回协议。
- [x] 明确原提交身份、接纳／消费、排队／撤回范围、迟到输出与 Task 状态的差异；未开放行为记为缺口，不能以取消整个 Task 代替撤回一条输入。
- [x] 会话分支设计保留来源、父链／分支头、配置与摘要范围，首版线性范围明确；不复制活动责任或回滚真实效果。
- [x] 直接回答 A、读取后回答 B、冷恢复 C、保存后读回 D 四类流程给出对象、权威事实、数据读取与写入阶段、可合并事务与不可合并屏障。
- [x] 区分同库装配与跨负责方交接，记录恢复／辅助调用增量；不宣称未测 SQL／fsync 总数或每对象一事务。
- [x] 链接／结构和方法拼写核对通过，运行场景只列待验证。

## Comments

- 2026-10-01：依赖 01 完成并合入集成分支后启动。应用入口是现有规划，不是新运行 SDK；参考原读写报告和现有 HAR 向量。
- 2026-10-01：从集成分支提交 `1c1f4a6` 创建 `codex/core-model-03`。已读取 TDD 技能，沿用规格及 implement-spec 授权的应用／SDK 验证切面；本票仅修改架构文档，执行既有文档与契约引用检查，运行行为保留为待验证。
- 2026-10-01：交付[默认应用入口](../../../docs/architecture/.draft/application-workflow.md)、[四类数据流程](../../../docs/architecture/.draft/request-data-flows.md)及[后续分支设计边界](../../../docs/architecture/.draft/interaction/session-and-task.md#history-branches)。结构化 queued 输入沿现有撤回合同；任意聊天队列／steering／follow-up 明列缺口。按方法登记修正交互篇将 task.submit／task.cancel／task.input 准备误称 accepted 的旧说明，公共字段与阶段未改。
- 2026-10-01：检查通过：既有 `check_documents.py` 在 architecture 范围检查 58 个 Markdown、1671 个本地链接、124 个 Mermaid 块，0 错误；新增两页的 22 个明确方法引用均在 105 项登记中，9 组输入必填字段及相关回执阶段与 Schema／登记一致；`git diff --check` 通过。未修改机器契约或验证脚本，未运行模型、数据库、工具、故障实验或性能测量；A／B／C／D 仍为待运行验收场景。
