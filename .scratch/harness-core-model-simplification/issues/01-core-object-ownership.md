# 01：核心对象与数据归属

Status: ready-for-agent
Progress: completed
Blocked by: None

## Scope

落实[规格](../spec.md)的六组核心对象与概念分类。创建架构主干 `core-data-model.md`，作为对象归属和内部记录的导航；术语准确更新到根 CONTEXT。权威业务规则继续归原模块。

Owned files: `CONTEXT.md`、`docs/architecture/.draft/core-data-model.md`。

## Acceptance

- [x] 分类覆盖核心对象、内部子记录、派生投影、配置绑定及可选扩展；保留每项负责方、身份／关联、生命周期、事务／持久性、查询者、保留与独立存在理由。
- [x] 六组对象及 Task／Decision／Operation 主要记录都可从目录追踪；独立 Confirmation、Surface、内容用途、授权使用与结算责任不会被错误并入父对象生命周期。
- [x] 明确默认逻辑聚合和可合并存储范围，不承诺六张表或跨负责方原子性，不删除既有公共端口。
- [x] 场景说明提交、任务、回合、模型调用、工具行动与工作进程的不同身份；从根词汇进入正文无术语漂移。
- [x] 复用已有文档检查，记录检查范围；未运行的数据库及行为验证明确标注。

## Comments

- 2026-10-01：实施入口。先读规格、根 CONTEXT 和有关 ADR；按 TDD 指引评估验证切面，本票为设计文档，使用静态检查和行为规格审查，不制造运行测试或伪造 red/green。

- 2026-10-01：在基于集成分支的独立工作树完成 [核心数据模型](../../../docs/architecture/.draft/core-data-model.md) 与根术语收敛。目录按核心对象、子记录、辅助责任、投影、配置、扩展及公共执行记录列出负责方、原身份、持久点、查询与保留；补充默认共事务范围和保存报告贯穿身份示例。
- 2026-10-01：语义审查保留 OperationIntent／Operation 双方责任、可选 ModelCall、独立 Confirmation／Surface、内容用途与副本清理、不可变 UseReceipt 和累计 UseSettlement。核对已有 interaction.input_withdraw 后，明确它只承担原 InputRequest 回答撤回，未宣称已支持 Session 自由输入多 Lane／steering／follow-up。现有公开端口及机器契约未修改。
- 2026-10-01：复用 check_documents.py：本票 3 个 Markdown 文件、32 个本地链接通过；架构草稿范围 52 个 Markdown、1603 个本地链接、104 个 Mermaid 块通过结构检查；git diff --check 通过。本票未新增图，既有图只作结构检查。按 tdd 技能沿用规格既定应用／SDK 工作流切面；本票为文档设计，未制造 red/green、未执行数据库、模型或运行恢复验证。
