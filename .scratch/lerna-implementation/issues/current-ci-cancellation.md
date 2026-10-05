---
status: resolved
labels: [ready-for-agent]
---

# 保留 Run 的取消与全部 lane 错误原因

当前准确集成提交 `dadd801cfd11ca038f0496872941097f10ebe507` 的 CI [37274041977](https://github.com/ruipengliu/lerna/actions/runs/37274041977) 在 `TestPGPoolRunReservedLaneProgress` 正常取消后返回 `driver: bad connection`，Recovery 实际失败；后续 Component 与 race 未执行。[记录](../../lerna-04-content-snapshots/current-ci-dadd801/README.md) 保存原失败，不能称 native 根因已重现。

此必要修复落实切片 02 的 Run 生命周期、切片 03 的有限故障规则和 AGENTS 的错误原因保留要求，不改变 PG 全局错误、期限、Claim 或配额。已授权 Timer/Runner 机械 seam 与原真实 PG/SQLite 恢复 seam。

- [x] 最先用有限三 lane barrier 证明实际 caller 取消、所有独立 lane 原因必须保留；注入 driver 原因明确为机械诊断。
- [x] Run 保存原 caller context，内部停止后 join 全部 lane，聚合实际 lane 错误和 caller 的真实 Err；caller-live 第一故障控制保留原因。
- [x] 原 PG reserved-progress、有限 blocked-SQL 取消及 SQLite、相关 Run/wake 普通和 race 通过；不重试掩盖失败，不假 native 复现。
- [x] 必要受影响检查、独立 Standards/Spec、准确来源与资源证据完成后实际合入并 push，核对新 CI。

实现工作树 `/tmp/lerna-worktrees/current-ci-cancellation`，从 dadd801 建立。03/06 可独立推进。归档源码 suffix 修正由 root 处理，原字节不可格式化。

本地资格见[退出说明](../current-ci-cancellation-exit-evidence.md)和[架构评估](../current-ci-cancellation-architecture-assessment.md)。固定357/product65已完成本地N/R、规范检查、两轴及只读资源证据。

## Answer

已实际整合并推送 `b9db5cc67647a15d6556fe92941757034a1e80d1`，包含固定修复 `3570180514ce0a6cee4daa27430540003574533d`。准确 head 的 [CI 37295630473](https://github.com/ruipengliu/lerna/actions/runs/37295630473) 两个 job、23 个步骤全部成功；[原脱敏日志及边界](../../lerna-04-content-snapshots/current-ci-b9db5cc/README.md)已保存。原 driver 失败和资源 UNKNOWN 不倒补；此关闭不接受待验证的分组候选或票03／06。
