# 04: SQLite 工作接替与双适配器一致性

**What to build:** 同一个 Host 工作行为套件在 SQLite 与 PG 验证原 Job 接替、修订竞争和旧 Claim 隔离。

**Blocked by:** 02 — SQLite 同版接纳；03 — PG 修订领取

**Status:** claimed

- [ ] 在真实单写 SQLite 上实现相同领取／续租／条件提交语义，不降低修订或 epoch 绑定；两 adapter 经相同 Host 套件观察独立事实。
- [ ] 新增工作与旧完成两种同步顺序均保留较新工作；过期 Claim、旧 worker 和篡改 claimed_revision 被拒，新 worker 正常推进。
- [ ] 关闭重开后 Job、work／completed／claimed 修订及 epoch 可恢复，业务对象和固定回执不变；查询仍通过原 owner。
- [ ] 扫描和 Claim 持久化使用有限短事务和可信 owner 时间，处理在事务外；单写协调、取消和 busy 均有正常对照。
- [ ] 真实 v2 领取迁移可从已保留的 v1 writer 输入向前升级；不改写已应用脚本、不删库重建，不用伪造旧 schema 代替旧版本。
- [ ] 记录两库实际版本、PG 隔离／同步提交、SQLite 每连接 WAL／FULL 和并发结果；为等待／配额／故障提供真实共同基础。
