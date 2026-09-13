# 05 双轴代码审查

维护者确认基点 `6395a00da1c7d5ebf8444f7f98d9703d77813d2d`，采用 `git diff 6395a00...HEAD`。首次审查覆盖 `25f3abf`；修复 `3d944ba`、`849fc86` 均经原审查者只读复核。最终代码范围为 `6395a00...849fc86`。

两个独立审查者并行执行 Standards 与 Spec。Spec 来源为 [05 票及 Agent Brief](../../.scratch/harness-implementation/issues/05-control.md)、共享实施规格与 04 适用回归要求；Standards 来源为 AGENTS.md、CONTEXT.md、docs/agents 约定和 code-review 的代码异味基线。

## Standards

文档规范违规 0 项。初次发现 1 项 P3 判断性异味：TaskClient 与 ControlClient 重复消息交换、相关性、未知字段和错误解码。现由私有 exchangeTask 统一处理，客户端保留各自返回值的语义校验。原审查者确认关闭，最终授权修复增量未引入新问题。

## Spec

初次发现 2 项 P1：普通超时或持久 start 后崩溃因 InFlight 无法清理而永久阻断重试；期限到达可将尚未知的在途效果封成 FAILED。

修复后，Runner 退出保留实际脚本返回观察；无控制的在途工作可被 ListPending 发现，由 PrepareDisposition 固定有限处置配置，再核对原来源事实。期限在途保存 WAITING/deadline/reconciliation，可信完成仍可登记。普通超时后成功重试、期限迟到事实和无用户控制的 after-start 子进程恢复均有验证。

复核另发现 1 项 P2：无控制脚本 STOPPED 强制要求 task.execute，阻断已撤回执行权限但仍有核对权限的收尾。现优先接受 task.reconcile，仅脚本自身无控制返回允许 task.execute 兼容路径。新增契约检查证明可以清理 InFlight，同时保留 authorization 等待和已用额度。

原审查者确认 2 项 P1、1 项 P2 全部关闭，未解决 0 项。完成先提交的旧版本取消另验证返回 already_COMPLETED，不改变原结果或任务版本。

Standards：累计 1 项 P3，已关闭 1 项，未解决 0 项；Spec：累计 2 项 P1、1 项 P2，已关闭 3 项，未解决 0 项。

最终修订通过 [完整验证](evidence/05-verification-stages.json)。首次全量通过后发现上述 P2，因此修复后重新执行 make verify；归档的是最终 `849fc86` 的结果。报告如实记录 dirty=true：验证时存在无关的 docs/architecture/README.md 与 skills-lock.json 修改，本票未纳入提交。
