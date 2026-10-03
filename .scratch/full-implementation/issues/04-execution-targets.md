# 04 execution-targets

Status: partial
Blocked by: 01

依据 [实施规格](../spec.md) 与根 AGENTS.md，保留准确身份、负责方、事务、门禁和恢复。实际编译、公开接口行为、正反例及所需平台证据均通过后才关闭。

## Comments

实现与准确范围见 [Execution 参考实现](../../../internal/execution/README.md)。已实现 durable Operation/Attempt/Effect、独立 gate/window、真实入口 StartBarrier、取消墓碑、原目标恢复、费用与迟到事实、真实 Root 文件驱动、三台独立状态模拟手机、资源 epoch、被动 Environment 与受信纯计算、持久 hostcall 映射和 snapshot cursor。

公开接口验收在真实 SQLite 与 PostgreSQL 均通过；真实文件与设备状态探针、race、vet 记录由执行分支提交及验证证据固定。不可据此关闭完整工单：不可信 WASI 隔离、目标多物理安全重试、真机与其他生产平台仍缺具体证据，参考实现明确不开放这些能力。控制大集合的分页批量推进仍为明确的有界缺口。
