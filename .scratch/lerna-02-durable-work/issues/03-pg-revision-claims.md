# 03: PG 修订领取与过期 Claim 隔离

**What to build:** 工作者在原 Job 领取准确修订；新增工作、过期接替和迟到提交均不丢失已保存责任。

**Blocked by:** 01 — PG 原命令原子接纳与恢复查询

**Status:** claimed

- [ ] 真实 PG 的有界持久扫描可独立领取已提交 Job，通知不是唯一恢复路径；领取固定 worker、claimed_revision、epoch 与 lease 截止。
- [ ] 领域阶段输入在领取事务中得到一致观察，处理在持锁事务外，结果用有限短事务核验 Claim 后提交；不把任意 worker 回调放入长期事务。
- [ ] 领取后加入新工作，再完成旧修订只推进 claimed_revision，较新 work_revision 仍可领取；两种并发顺序均有受控同步点及独立 Host 结果。
- [ ] 续租和完成要求原 Job／worker／epoch／claimed_revision／有效期限绑定；调用方修改 claimed_revision 或已过期未被接替的旧 Claim 均不能提交。
- [ ] 过期由同 owner 的可信时间裁决，接替增加 epoch 且保留 Job／业务对象身份；旧 worker 迟到拒绝，新 worker 正常完成，不宣称外部效果已隔离。
- [ ] 真实锁序、原键／对象／Job 竞争及扫描上界有正常并发对照和有限截止；准确修订、epoch 越界拒绝，不能回退或整数溢出。
- [ ] 领取所需的实际新增字段／约束以真实 v2 向前迁移引入，保留 v1 的准确 writer 与脚本证据，为票据 07 提供真实旧→新路径。
