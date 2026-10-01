# 03：应用入口与四类数据流程

Status: ready-for-agent
Progress: pending
Blocked by: 01

## Scope

以现有应用／SDK 主流程降低普通调用者需要管理的概念。新增 `application-workflow.md` 与 `request-data-flows.md`，更新交互模块的 Session／输入说明。

Owned files: `docs/architecture/.draft/application-workflow.md`、`request-data-flows.md`、`interaction/README.md`、`interaction/implementation.md`、`interaction/session-and-task.md`。

## Acceptance

- [ ] 从提交、查询、控制、原输入消费、结果读取和恢复连成可操作的入口说明；准确对应现有方法或明确应用内部组合，不能虚构 session.* 或新撤回协议。
- [ ] 明确原提交身份、接纳／消费、排队／撤回范围、迟到输出与 Task 状态的差异；未开放行为记为缺口，不能以取消整个 Task 代替撤回一条输入。
- [ ] 会话分支设计保留来源、父链／分支头、配置与摘要范围，首版线性范围明确；不复制活动责任或回滚真实效果。
- [ ] 直接回答 A、读取后回答 B、冷恢复 C、保存后读回 D 四类流程给出对象、权威事实、数据读取与写入阶段、可合并事务与不可合并屏障。
- [ ] 区分同库装配与跨负责方交接，记录恢复／辅助调用增量；不宣称未测 SQL／fsync 总数或每对象一事务。
- [ ] 链接／结构和方法拼写核对通过，运行场景只列待验证。

## Comments

- 2026-10-01：依赖 01 完成并合入集成分支后启动。应用入口是现有规划，不是新运行 SDK；参考原读写报告和现有 HAR 向量。
