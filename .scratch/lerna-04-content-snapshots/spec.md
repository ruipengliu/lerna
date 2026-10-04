# 04 · 准确内容发布与上下文快照

Status: ready-for-agent
Phase: B
Implementation: in-progress
Depends on: [01](../lerna-01-command-contracts/spec.md)、[02](../lerna-02-durable-work/spec.md)、[03](../lerna-03-deterministic-harness/spec.md)

## Problem Statement

应用需要引用可回读的准确材料；对象上传中断、来源撤权或上下文过长，都可能让决策使用缺失或不再获准的内容。

## Solution

交付 Content 的发布、受控读取和来源登记，以及构造不可变 Snapshot 的能力。调用方可以检查准确版本、材料缺口与溢出原因，未成功发布的内容不会被当作成果。

## User Stories

1. As a user, I want exact artifact versions, so that the evidence matches the result I see.
2. As an application developer, I want publication status, so that incomplete uploads are not presented as readable content.
3. As a user, I want current access checks, so that a saved reference does not bypass revocation.
4. As a decision author, I want immutable snapshots, so that each proposal has reproducible inputs.
5. As a user, I want mandatory constraints preserved, so that context limits do not weaken my requirements.
6. As a data owner, I want complete processed-source lineage, so that derived content respects every source.
7. As an operator, I want recoverable upload cleanup, so that failures leave visible responsibilities.
8. As a user, I want evidence gaps disclosed, so that deleted bytes are not represented as available proof.

## Implementation Decisions

- 实现 content.put、content.get 与准确 ContentRef；发布经历 preparing、字节写入、摘要和耐久核验、元数据事务发布，异步接纳不等于 published。
- 对象字节使用对象存储适配器，元数据、来源、holder 与恢复 Job 在所属数据库提交；上传不进入持锁事务。相同版本字节不可改写。
- 读取、处理、保存、同步和披露分别询问授权接口；早期合同测试使用显式受信策略夹具，真实行动开放前接入 06 的 Grant 裁决。ContentRef 本身不授予权限。
- ContextCompiler 接收当前目标、条件、控制、预算、期限与未决效果；保留强制内容，额外材料按需读取，记录省略清单和来源缺口。
- Snapshot 固定准确输入、策略和组件版本、全部 processed_source_refs、摘要派生关系；提交前重新核验目标修订与材料资格。编译器不成为第二个 Task owner。
- 强制信息超过模型输入上限及预留输出空间时返回 context_overflow，不发送 Decision。供应商缓存不替代 Snapshot。
- 派生用途取适用来源许可交集，保留期受最严限制；无合法交集则拒绝。撤权先封新使用再登记清理，离线残留不冒充已擦除。

## Testing Decisions

主边界是 content.put / content.get；Snapshot 通过规则决策组件接收到的公开输入检查。复用 03 故障设施，在真实元数据数据库及对象存储实现上测试；编译期目标竞争由 05 接入 Application 后再次验收。

测试只断言公开行为、持久恢复结果和独立目标状态；不依赖私有表布局或内部调用次数。每个故障或拒绝案例必须配允许正常完成的对照。

验收条件：

1. 字节写入后、元数据发布前杀进程：恢复原发布或登记孤儿清理；不得返回缺字节的 published 引用。
2. 错误 hash、byte_length 或版本冲突被拒绝；完整正确内容可发布并按准确引用读回。
3. 跨租户或撤权后的引用读取失败且不泄露字节；来源用途无交集时拒绝派生。
4. 超长强制约束返回 context_overflow，观察模型出口请求数为零；可容纳输入完整保留强制字段。
5. 来源在 Snapshot 提交前变化时重建或报缺口；不得保存未经复核的混合版本输入。
6. 摘要实际处理的所有来源均登记，即使最终公开引用只有其中一项，受限来源约束仍存在。
7. 正文合法清理后查询保留可披露的最小元数据与证据不可回读标记，清理失败有残留负责方。

## Out of Scope

- 跨任务记忆提取、向量检索和任意程序化读取。
- 把测试授权夹具作为生产授权服务。

## Further Notes

最小 Content 与 Snapshot 在 B 前段完成；04 只检验授权接口的调用行为，06 接入真实授权与消费账本后才允许真实副作用闭环。

依赖项表示实现先决条件；`ready-for-agent` 表示规格已明确，不表示依赖已完成或能力已开放。全部验收通过并附准确版本、环境、命令、结果和限制后，才可将本切片记为完成。共同执行与证据规则见[切片索引](../lerna-implementation/README.md)。

依据：[capabilities](../../docs/architecture/capabilities.md#上下文构造与按需读取)、[data-model](../../docs/architecture/data-model.md#事务与数据库装配)、[governance](../../docs/architecture/governance.md#内容来源与数据生命周期)、[contracts](../../docs/architecture/contracts.md#能力模块方法)、[ADR-0007](../../docs/adr/0007-versioned-content-memory-snapshots.md)、[ADR-0006](../../docs/adr/0006-authorization-budget-at-action-boundaries.md)。


2026-10-04，前置01–03完整退出，03准确受CI验证47ebce1/run37194868564 success、退出5fbb1a0。root实际核对1aa条件API全部产品/合同/工具/测试对象与最终exit相同，采用[决定](decisions.md)、[具体接法](final-api-handoff.md)、[六票及依赖](ticket-review.md)。1.2仅新增Content合同；完整canonical必要上下文Content作为第一材料接现有1.1规则，完整来源/真实读回和输出预留同时验证。六票8+7+7+6+7+6共41AC，01已claimed，其他按真实依赖启动；当前仅计划/发布，无04实现或验收证据。


2026-10-04，首票的[1.2机器合同形状](contract-shape-decision.md)已正式采用，[六票复核](published-ticket-review.md)无必要修正。准确版本、固定有效保留上限、响应身份/range、当前权限和真实清理状态由该决定闭合；这不是已完成行为或04退出证据。

2026-10-04，首票8/8AC已resolved，交付1a7、受测14ead、合并eba167d；[独立审查及执行退出记录](ticket-01-exit-evidence.md)说明准确资格与限制。切片累计8/41、Implementation仍in-progress；后续来源闭包、上下文、正文清理/holder及SIGKILL等待对应票据。本检查点新CI待准确push head核验，不使用旧64文档CI补新源码证据。
