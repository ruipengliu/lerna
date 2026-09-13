# 03 票代码审查

用户确认审查基点为 `987500319d862ac9ebb51766a754b04b6e099773`。首次实现提交为 `2ad77c3`，比较 `git diff 9875003...2ad77c3`；修复复核检查工作区改动，最终修复提交为 `ea82bcfbc6e0d02c44702ae0732f7c150d763a9e`。

两名独立审查者分别核对仓库规范和 [03 票 Agent Brief](../../.scratch/harness-implementation/issues/03-tasks.md)，最终全套测试由实施方统一执行。

## Standards

初次发现 1 项 P2：消费方虽然声明 RuntimeStore，但回调依赖 `*authorization.RuntimeTransaction`，其行为来自不可公开构造的私有函数字段，独立实现只能转委托具体授权服务。这不满足接口依赖和可替换约束。

修复为公开 `RuntimeTransaction` 行为接口，具体实现私有化。外部测试包独立实现该接口，完成提交、重复提交和查询，不构造或委托 authorization.Service。该测试只证明替换接口可用，不宣称第二耐久后端或完整授权一致性资格。

原审查者只读复核确认关闭，无新增规范问题；未解决 0 项。

## Spec

初次发现 1 项 P2：合法内部 RunChange 的空输入引用 `[]string{}` 经 gob 保存/恢复变为 nil，直接比较 Go 表示会使原 change_id、原内容的重试返回 IDENTITY_CONFLICT，违反同提交返回原结果要求。

使用公共 RunStore 和真实 SQLite 复现失败；提交比较与保存前规范化空引用后，`TestInternalCommitCanBeReconciledFromRecoveredSnapshot` 通过。原审查者再次运行该定向测试并确认修复，无未解决发现。其他已确认范围包括共享 CAS 接纳、跨模块操作占用、原子初始工作、当前权限、管理清理后保留、有界分页及真实子进程恢复；没有越界实现 Worker/Brain。

Standards：1 项 P2 已修复，未解决 0 项。Spec：1 项 P2 已修复，未解决 0 项。最终统一结果见 [验收记录](03-durable-tasks.md#验收记录)。
