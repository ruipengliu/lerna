# 07 双轴代码审查

用户确认以 `20890db` 为基点。初次范围 `git diff 20890db...2f492c2`，修复复核至 `d32529e`，验证调度补充复核至 `c213865`。两个独立审查者并行只读核对；Spec 来源为 [07 票 Agent Brief](../../.scratch/harness-implementation/issues/07-artifacts.md) 与适用架构/共同约束，Standards 来源为 AGENTS.md、CONTEXT.md、docs/agents、共享实施规格及代码异味基线。

## Standards

初次 1 项 P3 判断性建议：ContentTransaction 与 RuntimeTransaction 的操作身份/窗口校验重复。修复抽取私有 partitionOperation，共享窗口、跨领域身份冲突和登记规则，保留不同事务接口、授权语义与数据分区。原审查者复核关闭；硬规则违规 0 项，剩余 0 项。

## Spec

初次 2 项 P2：

- 在认证前查询引用/操作是否存在，导致失效凭证通过 UNAUTHENTICATED 与 PERMISSION_DENIED 的差异探知存在性。现先统一认证，再查询记录；对错误、禁用和过期凭证分别测试 GET/READ/DELETE/LOOKUP 的存在/不存在对照。
- GET/LOOKUP/PUT 重放只读取持久发布状态，缺失或损坏正文仍显示 available。现核验正文，返回 missing/corrupt/unavailable 等当前可用状态，核验后重新检查权限；真实文件故障检查覆盖三种入口。

原审查者确认 2 项 P2 均关闭，未发现新增问题或范围膨胀，剩余 0 项。

两位审查者补充核对 `c213865`：测试包并发设为 1 保留全套测试、race、用例内部竞争和业务时限，没有削弱验收；文档区分已观测事实和负载相关推断，并保留首次失败记录。

最终 [完整验证](evidence/07-verification-stages.json) 通过，报告修订 `c213865184dc95ad4c3df01a2b654f8ec5a1664f`，dirty=false。Standards 累计 1 项 P3、Spec 累计 2 项 P2，均关闭，未解决 0 项。
