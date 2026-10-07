# 13: 初始目标通过单个 Interface 创建任务

**What to build:** 会话用一次事务内调用创建完整初始任务，用户仍得到原有条件、输入、关联及回执阶段。

**Blocked by:** None (can start immediately).

**Status:** claimed

- [ ] Task orchestration 内聚创建、显式条件验证接纳与初始输入登记；Sessions 只维护原顺序与关联。
- [ ] 无显式条件保持 DRAFT、BoundInputVersion=0、ACCEPTED 初始输入和 REQUIREMENTS 等待；显式条件保留原接纳与 PROCESSED 语义。
- [ ] 原 goal、content、source input、AcceptedBy、输入序号和初始版本绑定准确；依赖未满足不提前创建。
- [ ] 旧 SubmitGoal 保持 SUBMITTED→DECIDE_GOAL→DECIDED，SubmitInput(GOAL) 保持同步语义；永久拒绝检查在旧路径写入之前完成。
- [ ] 任务、条件、输入历史、会话关联、源记录及决定加入同一原事务；失败无部分记录，相同命令重放只生成一个任务。
- [ ] 关闭后同会话新目标、显式条件、依赖路由与提交前/后/丢回执场景通过，更新相关设计。

## Comments

2026-10-07: Claimed for implementation on codex/interfaces-13.
