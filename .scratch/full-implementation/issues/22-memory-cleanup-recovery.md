# 22 memory-cleanup-recovery

Status: claimed
Blocked by: 01, 02, 05
Implementer: task_impl

依据：protocol 的已开放 Memory 修改族必须有 cleanup.get 恢复入口；第二实现同版合同核对确认默认 Go 登记仍缺此方法。

在现有当前主体／管理权限下开放只读 memory.cleanup.get；目标、修订和查询身份仍按共同合同检查。返回原 Memory 的当前 cleanup_state 及获准元数据，不触发清理、不读已撤正文、不把 Memory holder 清理完成当作所有 Content 副本物理擦除。与第二组件固定同版闭合输入／输出，并用真实双库验证删除后 pending → complete、越权／跨 scope 拒绝和原对象重开。

## 完成依据

作为最后复核确认的最低恢复接口，由同一修复 implementer 完成。登记、发现、Schema、实际行为和边界同版发布后记录准确提交与制品。

## Comments

2026-10-03：第二独立实现核对发现公开恢复遗漏，根源码复核确认；不把现有私有 cleanup Job 当作公开方法已实现。
