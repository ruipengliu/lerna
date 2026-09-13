# 02 票代码审查

审查基点为实施开始前的 `cf3eb4dbfeb06f9d6ac260b7d2796bb9bf40c6a8`，首次实现提交为 `2bd6856f47834a79a2b73259fc7f231f7703c622`。比较命令为 `git diff cf3eb4d...2bd6856`；修复复核包含当时未提交的工作区差异，最终修复提交为 `43dd75e1080a9a424e929d6ea31c3fa311d1c1dc`。

两名独立审查 Agent 分别核对仓库规范和 [02 票](../../.scratch/harness-implementation/issues/02-auth.md) 已确认的 Agent Brief；最终测试由实施方统一执行，复核本身不作为全套测试证据。

## Standards

初次发现 2 项，均已修复并通过只读复核：

- P2，明确规范：`authlocal.Binding` 直接依赖具体 `*authorization.Service`，不符合工作包中“适用实现依赖消费方 Interface”。改为由适配器声明所需 `Authority` 接口，真实服务按方法集满足接口。
- P3，命名判断：消息、操作随机部分和权威标识使用 `NewCredential`，容易混淆凭据和普通标识。提取中性的内部 `randomid.New`，领域凭据入口保留 `NewCredential`。

复核未发现上述修复引入的新规范问题。未解决 0 项。

## Spec

审查及修复复核共发现 3 项 P1，均以公开接口回归测试先复现失败，修复后通过定向验证，并由原审查 Agent 只读复核关闭：

- 操作 ID 的 Base64URL 替代编码可通过相同 MAC 核验，却产生不同字符串去重键。现在严格解码并比较重新编码后的规范文本，拒绝非规范尾部位及换行；测试 `TestOperationIdentityRejectsAlternateEncoding`。
- 只比较当前资源展开集合，会允许有限精确/集合授权签发树范围，随后新增资源可能扩大权限。现在保留选择器结构含义，有限集合不能包含未来扩展的子树；测试 `TestFiniteResourceSetCannotIssueFutureSubtree`。
- 操作记录满容量时，提前检查会连清理提交一起拒绝，导致无法回收。现在在独立快照完成变更后检查剩余容量，原子保存清理结果和回执；失败不提交该快照。测试 `TestFullOperationHistoryCanBeCleanedThroughPublicAPI`。

未发现需要保留的上述问题，未解决 0 项。范围仍限于本地持续授权及真实 SQLite 管理恢复，不扩展到跨节点签发、单次消费或任务执行。

Standards：初次 2 项，最高 P2，均已关闭。Spec：3 项 P1，均已关闭。最终统一验证见 [验收记录](02-local-authorization.md#验收记录)。
