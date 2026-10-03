# PostgreSQL advisory lock 存储作用域修正

2026-10-03。用户授权的 `gpt-6-astra`、`high` 决策代理只读实际代码后作出决定，主任务核对并采用：**修正 adapter 的锁键；仅串行化测试不足。** 本文记录实施决定，没有宣称修复或 red/green 已完成。

## 实际依据

当前 `postgres.Config.Schema` 限制为准确 ASCII schema identifier，`Store.table` 用该 schema 限定 command_receipts、durable_inputs、jobs 等表。不同 schema 是独立持久存储集合。`Within`/token 另校验当前 Store 与逻辑 OwnerRef，保持同库同 owner 操作边界。

但 `commands.go:lockKey` 只编码 `[namespace, tenant, owner, id]`。LockCommand 用 `(1,hashtext(key))`，LockInput 与 TryLockInput 用 `(2,hashtext(key))`。PostgreSQL advisory lock 位于数据库锁空间，不自动继承表 schema；因此同库两个隔离 schema 中相同 owner/id 当前必然争同一锁。票02与票03真实 PG suites 的竞争可使后一套的 TryLockInput 合法返回未取得锁，形成空 Claim batch。该空结果本身不违反 TryLock/SKIP 行为，但暴露 Store 隔离前提不成立。

迁移锁已经包含 `Schema + ":migration"`，此处不需要重做。当前缺陷在运行时 command/input 锁键，不在已发布的 0001/0002 SQL。

## 最小修复

锁输入编码改为无歧义 tuple：

`[schema, kind, tenant_id, owner_id, id]`

继续使用现有 JSON string array 编码，保留 command/input 的区分和原 advisory 参数类别。schema 从已经验证且不可变的 Store.config 取得，不能由请求、worker 或 Tx 调用者另行指定。可将 helper 改为 Store 私有方法，或显式传入 schema；两者都是局部实现选择，不新增公开接口。

必须同步修改 LockCommand、LockInput、TryLockInput 的调用，使同一存储对象的 blocking 和 try-lock 计算完全相同的键。不得只修测试碰到的 TryLockInput。保持原命令键→业务输入→Job 的锁序、行锁和 lease/epoch 条件，不改变 Claim 遇到真实同范围竞争时可以跳过的语义。

作用域结果：

- 同一数据库、同 schema、同 kind/owner/id：不同连接、进程和 Store 实例仍共同互斥。
- 同一数据库、不同 schema：独立存储对象不再由于完全相同的 tuple 被必然合并为一把锁。
- 不同数据库：PostgreSQL 自身区分锁空间，不需将 DSN、密码或连接地址写入 lock key。
- schema 不是新的逻辑 owner，不改变任何 CommandRef、JobRef、摘要、固定回执或 owner 路由。两个独立 schema 不能同时被宣称为同一个线上 owner 的两份权威账本；本修复不授予双主部署能力。

不添加随机 instance ID、进程 ID、每次连接 nonce 或 worktree identity，否则同 schema 的真实竞争会失去互斥。也不通过更换测试 owner/id 或永久串行 suites 掩盖真实作用域问题。

保持现有 `hashtext` 是本次最小修复；其有限哈希空间仍可能产生碰撞，碰撞意味着额外争用而非分裂同键互斥。因此本修复消除的是不同 schema 的**确定性同键别名**，不声称数学上绝无跨对象哈希碰撞。无须借此引入新的全局锁目录或更换全套锁算法；相关活性仍依赖已有有限锁期限和允许竞争时扫描继续。

## 同 schema 升级约束

锁键属于参与进程之间的运行时协调协议。旧二进制与修复后二进制在同 schema 中计算不同 advisory key，不能保证对“尚不存在输入”的创建竞争仍共享所需互斥。因此本片切换采用：停止/排空旧 worker 与事务，确认它们不再操作该 schema，再启动修复版本。正常重新打开旧数据库数据不受影响。

不改已发布 0001/0002，不需要伪造 SQL 迁移；不是正文/命令合同版本变更。迁移验收的真实 v1 writer 先结束，随后升级/重开，与本约束兼容。本片不新增在线滚动混版本锁协议；不通过双拿旧全库锁继续保留现有问题。

## 真实 red→green 验收

单独修复票/工作树先建立可控竞争的回归，再改产品。沿内部 Host/实际存储端口驱动，使用真实同一 PG database 中两个由测试创建的独立 schema；不得使用环境 smoke 数据或打印 DSN。

1. **不同 schema 的输入锁隔离（主要 red）**：两 schema 写入相同 owner/object identity 的可领取 Job。A 的有限事务通过实际 input lock 端口取得锁后发同步信号，并等测试释放；在其仍持锁期间，B 的 Host worker Claim 应取回 B 的原 Job，完成实际 hash并读回准确 B 投影。旧键下 B 会被确定性跳过；修复后在同一同步阶段成功。不要靠并行概率或 sleep 猜测 A 是否已持锁。
2. **不同 schema 的 command 锁隔离**：A 持有准确原 command key 锁；B 的相同 owner/command_id、独立输入接纳能在 A 放锁前完成并查询 B 固定 receipt。用独立完成信号证明没有等 A，不以机器速度阈值当主要依据。全部等待均有有限失败截止。
3. **同 schema 的互斥仍成立**：使用两个独立 Store/连接，范围相同。A 持有 input 锁时 B 的 try Claim 可返回空；A 放锁后 B 能领取。原 command 并发/同对象前态测试仍证明只有正确原决定与修订，没有因 Store 实例变化失去隔离。
4. **两套实际 suites 同时运行**：在各自独立测试 schema 中并行执行此前冲突的真实 PG suites，原 `TestPGConcurrentNewWorkAndCompletionBothCommitOrders` 及正常对照通过；保留独立命令、版本、数据库配置与退出结果。不能把串行重跑绿灯作为本缺陷修复证据。
5. 跑受影响 PG 并发/锁序与 race 检查及通常构建检查；确认迁移文件/校验值与公共 1.0 生成物未改。没有新失败或代码变化时不无界重复 suites。

对有意竞争的同 schema用例，空 batch 是允许结果，测试需按其同步点/恢复语义判断；跨 schema red 用例特意消除同范围合法竞争，验证存储边界。若测试只是任意并行并断言“首次 Claim 永不空”，仍可能把别的合法竞争误判为产品缺陷，不能借修复建立这样的不实承诺。

## 交付范围

独立修复票处理 PG adapter + 必要回归，root负责集成和真实CI验证。SQLite实施者不抢改PG，04不增加新的业务先决边；需使用修复集成基线时正常同步即可。此处不改变领域不变量，不建新ADR，不将两次测试互相影响归咎于CI，也不把一次本地绿灯写成远端CI成功。
