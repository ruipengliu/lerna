# 28：实施审查

范围：相对 `7bd33ab7981a511832fa39791bbcd3b4c22cdbb2` 的本票工作区改动及新增文件。两个独立代理分别检查 Standards 和 Spec，依据票据 Agent Brief、共享规格、协作架构、AGENTS.md、tracker/domain 约定及 code-review 十二项代码异味基线。

## Standards

未发现文档标准硬性违反。初审一个 P3 判断性 Duplicated Code：已处置状态在父终态检查及调度循环重复表达。已统一复用 `childDisposed`；增量复查关闭，无未关闭标准发现。

## Spec

初审三个问题已修复并验证：

- P1：子权限门禁缺少实际工具启动边界及后代委派。加入必需 `Core.CheckExecution`，裸子端口拒绝，后代独立检查 delegate；真实签名夹具验证先接纳工具、后撤销委派时 Started 仍为 false，叶子授权不能继续委派。
- P2：正常查询与取消共用检查计数。改为独立持久正常/处置计数，子端报告修订也保留取消额度；验证正常额度耗尽仍能完成取消核对。
- P2：用量未知的终态释放并发槽位。现在保持 CONFLICT 和预留，未知不是结算依据。

后续策略审查发现并关闭三处边界：结果须在外部 assess 前检查当前策略；取消/查询使用不含原始数据的 ChildReference；PublishResults 须在保存前检查全部历史报告。保存回调缩小为任务与经检查报告集合，Approve/Complete 复用同一检查。实际测试验证策略拒绝时 assess/save 不被调用。

增量复核未发现新增实质问题；其时完整竞态回归尚在执行，最终运行结果以 [验证报告](evidence/28-agents-report.json) 为准。单元、合成模型、真实进程及本机网络证据分别列出，不替代真实模型质量或完整系统验收。

Standards：1 项判断建议关闭，0 项未关闭。Spec：6 项边界问题关闭，0 项未关闭；最终运行与基线限制另列。
