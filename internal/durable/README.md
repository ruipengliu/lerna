# 可靠接纳与持久作业

[共同设计](../../docs/architecture/reliable-work.md) · [运行证据](../../.scratch/reliable-work/evidence.md) · [存储适配](../storage/README.md)

本包实现内部公共框架。当前默认宿主提供诊断；领域处理器、身份门禁、业务端口及生产 ClockAdapter 尚待接入。框架中的 Receipt 是存储值，公开线协议仍需按根 contracts 严格解析与映射。

## 接纳与本地事务

宿主打开已迁移的存储，固定可信 Scope 白名单、kind 与连接预算，再创建 Engine。Engine 和 Participant 只由可信装配持有；repository port 接收 Tx，handler 接收 Work，领域规则不获得 Session、Backend 或 SQL 句柄。`Register` 的能力保留在对应存储适配器。

`FixIntent` 对原始 JSON 检查重复键、Unicode、安全整数和 JCS 摘要；`Value` 保留原数值写法的副本供方法 Schema 校验。它不替代方法字段、关联、授权及资源限制校验。`Admit` 的 Check 在每次本地尝试内运行；Disclose 对缓存决定、冲突和 gone 也执行当前披露检查。方法、回执阶段及 expected_revision 要求直接读根 methods.json。

命令唯一键为 tenant/service/command；owner 是原路由隔离，换 owner 不能重新接纳同键。Apply 只进行本地可恢复工作：业务事实、准备/回执和下一责任同事务保存。允许 accepted 的方法必须有同事务准备与作业；最终阶段方法可以保存内部准备而不产生 accepted。`Await` 区分准备与不存在，使用有界请求 context；后台生命周期独立。

事务结果为 Committed、RolledBack、CommitUnknown。只有已确认回滚的数据库竞争在固定预算内重跑闭包；unknown 不自动重放。固定业务拒绝返回 rejected 值；原 expected_revision 不改写。事务内禁止网络、工具调用和外部等待。

锁序是命令、声明且稳定排序的业务键、稳定排序的作业键。`Lock` 的闭包实际取得业务锁，`Use` 只访问已取得的键。SQL handle 只进入可信 repository，适配器负责 Scope 条件、唯一性/外键隐式锁和结果验证；这不是任意 SQL 的 RLS 沙箱。使用 Tx.Context 传递上下文，Engine.Within 的 marker 只能诊断同上下文嵌套；禁止 repository 捕获 Engine 或原 context 另开事务。Work.Within 的固定入口另拒绝嵌套，包括误用原 handler context。

Tx 不可跨 goroutine、不可逃逸闭包。并发调用拒绝并使本次事务失效，结束后拒绝所有访问；回调必须有限执行、遵守取消。Go 不能强杀不合作的回调，期限到达时不凭返回信号假定其已退出。

## 作业与工作循环

- Raise 仅用于真实新责任，必须与领域事实共同提交；同键 source 绑定固定、版本加一、保留较早到期。Hint 不建记录、不加版本、不重开 done。
- Claim 使用当前数据库时间。work_revision 与 lease_epoch 分开，Claim 的原观察不可更新；领取 unknown 仅交出独立查询确认的原候选快照。
- Guard/Finish 在业务锁之后锁作业并验证当前持有者、epoch 和期限；提交前再以当前数据库时间复查已保护期限。失败整笔回滚。合法旧观察仍可保存原事实，但 Finish 保留新版本 ready。
- 续约不改变原观察。续约被锁阻塞时，独立取消定时器仍沿此前确认截止停止；未知续约不延长本地资格。localStop 只是保守取消提示，外部入口仍需领域授权及正式 ClockAdapter 前提。
- Handler 必须显式调用 Finish；返回 nil/error/panic 都不代替 done。等待用 Waiting 保存领域理由和有限 due_at 后退出。每轮有容量、期限、步骤预算；Step 和 Within 绑定原 Work 生命周期，换 context 不能恢复已停止工作。
- Work 的 Claim()/Scope() 返回固定值副本，处理器不能替换工作入口的原领取或原 Scope；每个动作步骤显式通过 Step 扣减预算。
- 接纳请求断开不取消已持久责任。Run 排空超时返回 ErrDrain，容量仍由真实处理器退出释放；宿主在 Run 停止后调用 Wait，全部退出后才关闭存储。控制/收尾池由装配独立保留容量。
- Reconcile 使用领域未结索引及有限页/游标，核验原来源后修复缺失责任。公共 runner 限制同时一轮修复，不扫描所有历史，不负责判断业务是否结清。

`Compact(key, closedAt)` 的 closedAt 必须来自领域持久关闭证明，覆盖效果、费用、传播和清理。完整回执保留至 `max(expires_at, closedAt)+QueryRetention`；清理只留下原键、摘要、对象及决定类型，重投不重新执行。`DeleteDone` 也要求领域证明；重建 Job 使用新 ID，原业务身份与累计预算不重置。

## 验证入口

```sh
make durable-check   # 真实 PG/SQLite、独立进程、网络断点、容量与小负载证据
make sql-check       # 两方言 sqlc 产物可复现
```

[探针](../../tests/integration/README.md) 使用自己的领域表和隔离数据库；它证明公共机械约束，不能替代各模块的发送、费用、授权、发布和成功验收。实验配置与生产容量边界见[规格](../../.scratch/reliable-work/spec.md)。
