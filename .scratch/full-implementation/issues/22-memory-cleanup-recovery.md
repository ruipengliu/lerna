# 22 memory-cleanup-recovery

Status: resolved
Blocked by: 01, 02, 05
Implementer: task_impl

依据：protocol 的已开放 Memory 修改族必须有 cleanup.get 恢复入口；第二实现同版合同核对确认默认 Go 登记仍缺此方法。

在现有当前主体／管理权限下开放只读 memory.cleanup.get；目标、修订和查询身份仍按共同合同检查。返回原 Memory 的当前 cleanup_state 及获准元数据，不触发清理、不读已撤正文、不把 Memory holder 清理完成当作所有 Content 副本物理擦除。与第二组件固定同版闭合输入／输出，并用真实双库验证删除后 pending → complete、越权／跨 scope 拒绝和原对象重开。

## 完成依据

作为最后复核确认的最低恢复接口，由同一修复 implementer 完成。登记、发现、Schema、实际行为和边界同版发布后记录准确提交与制品。

已登记默认 Go `memory.cleanup.get`，使用闭合 `ReadMemoryInput → MemoryRecord`，与 `memory.inspect` / 第二 Native TypeScript getter 同版。目标必须等于 `memory_id`；管理视图总返回当前元数据（沿既有 inspect 忽略可选 `revision`），每次核当前主体、管理权限及 `memory_admin`。不读取正文，不触发新的 cleanup，不授予普通记忆读取权。

真实公开 Dispatcher / SQLite、PostgreSQL 原对象：删除后准确 `pending`（revision 2）→原 cleanup Job `complete`（revision 3）；无管理角色、跨租户拒绝；数据库关闭重开后原元数据完全相等。原已发布 Content 的独立副本仍可读，明确不把 Memory 引用清理当作所有副本擦除。缺登记 RED 2.704s；`go test -race ./internal/memory -run '^TestPublicMemoryCleanupGetterRecoversOriginalMetadataOnlyResponsibility$' -count=1 -timeout=90s` 两库实际 exit 0，21.931s。当前受信管理视图之外的真实跨公司身份依赖仍按平台配置单列，不由该 getter 承诺。

## Comments

2026-10-03：第二独立实现核对发现公开恢复遗漏，根源码复核确认；不把现有私有 cleanup Job 当作公开方法已实现。
