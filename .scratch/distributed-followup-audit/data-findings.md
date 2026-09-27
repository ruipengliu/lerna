# 分布式补充审查：数据、事务与恢复

审查日期：2026-09-27。范围为当前落盘方案；只读检查架构、契约及相关实现设计，未修改它们。以下为事务交错推演，不是已执行的数据库或生产故障实验。保留两个能推翻现有保证的缺口；均可在已确认的单地域三 AZ、托管优先、固定 Home、维护窗口迁移边界内修正。

## 1. P1：同责任槽的新工作可能被旧领取者标成完成或延后

**位置。** [Task jobs 字段](/Volumes/Data/proj/lerna-docs/docs/architecture/task-runtime/implementation.md:63)、[责任槽与提前 due_at](/Volumes/Data/proj/lerna-docs/docs/architecture/task-runtime/implementation.md:72)、[领取后提交条件](/Volumes/Data/proj/lerna-docs/docs/architecture/task-runtime/implementation.md:272)、[恢复扫描](/Volumes/Data/proj/lerna-docs/docs/architecture/task-runtime/implementation.md:276)。该机制还承载[合并最高控制修订](/Volumes/Data/proj/lerna-docs/docs/architecture/security/implementation.md:317)和[恢复时合并控制](/Volumes/Data/proj/lerna-docs/docs/architecture/deployment-production.md:300)。

**可复现交错。**

1. W 领取唯一 control 槽，取得 `lease_epoch=e`，读取当前待发送修订 r，事务外等待原远端调用。
2. 另一事务提交 r+1，保存新的逐端责任，并按现有规则提前同槽的 due_at；此时 W 的领取没有过期或被另一 worker 替换，`lease_epoch` 仍为 e。
3. W 得到 r 的答复，按当前唯一明确的提交条件通过 e 校验，将槽写为 done；或者回写一次更晚的 waiting/due_at。
4. r+1 的责任仍在领域记录，但该槽没有及时再次运行。done 分支要等“缺少槽”的额外修复，waiting 分支甚至仍有槽，只是新要求被旧退避延后。

**为何现有约束不足。** `lease_epoch` 防止已经失去领取资格的 worker 回写，无法检测领取期间新增的同槽责任。候选准入的 Task.revision 校验不等于所有工作槽完成时的源修订校验；保存最高控制和原决定也不约束完成槽的条件。周期扫描已定义责任发现，但没有规定如何将仍 waiting 的槽与最新领域责任对账，因而不能据此证明关闭传播预算。

**最小补充。** 完成或退避事务按既定锁序重查责任来源和工作槽；只有本次处理覆盖当前要求、且没有新增领域责任，才能结束槽。可显式保存 desired/processed generation，也可锁内重核领域责任；否则保留 ready 或当前更早 due_at，不允许旧处理者延后新唤醒。原固定命令身份不变，已经可能发送的旧命令仍独立核对。

**验收反例。** 在远端答复前注入新控制，再分别让旧 worker 返回成功、可重试失败和超时；三者均不能使新修订消失或晚于其原到期。此项不需用户改变已确认边界。

## 2. P1：Memory 的连续水位尚未定义提交有序的分配规则

**位置。** [memory_changes 与 checkpoint](/Volumes/Data/proj/lerna-docs/docs/architecture/memory/implementation.md:123)、[索引覆盖和补扫区间](/Volumes/Data/proj/lerna-docs/docs/architecture/memory/implementation.md:323)、[视图快照转增量](/Volumes/Data/proj/lerna-docs/docs/architecture/memory/implementation.md:525)。

**可复现交错。**

1. 按常见的 PostgreSQL sequence 为 change_sequence 取号：T1 取得 101，修改和变化项尚未提交；T2 取得 102 并先提交。
2. 索引或视图目前只能看到 102。如果把已提交最大值作为权威切点 R 并推进覆盖点 I 到 102，随后 T1 提交 101，新变更就永久落在 `(I,R]` 补扫区间和增量游标之后。
3. 若将“连续”解释为必须等到每个整数出现，T1 回滚留下的 101 空洞又会永久阻止水位推进。当前没有区分“仍未提交”与“永远不会出现”的依据。

**为何现有约束不足。** 唯一、单调及“业务修改与变化项同事务”不能建立取号顺序与提交顺序的一致性。PostgreSQL 明确说明 `nextval` 的取号不随事务回滚回收，因此普通 sequence 不提供无空洞的提交序列。[PostgreSQL 18：Sequence Manipulation Functions](https://www.postgresql.org/docs/18/functions-sequence.html)

[生产部署的提示发布器](/Volumes/Data/proj/lerna-docs/docs/architecture/deployment-production.md:134)已经通过待发布项和提示头解决 WSS 提示的同类问题，但 [Memory 视图明确采用自己的领域分页与 ACK](/Volumes/Data/proj/lerna-docs/docs/architecture/memory/implementation.md:530)，没有规定 memory_changes 使用该发布水位；不能把两个独立游标默认为一个。

**最小补充。** 为每个 Memory owner 定义事务头；Memory 变更在同一短事务中锁头、取得 change_sequence、写变化项并推进头，使后继取号必须等前驱提交或回滚。索引 R、视图快照切点和增量读取明确使用这个已提交头；锁头纳入统一锁序并测量这个 owner 内的串行成本。也可选择明确的已提交变更发布算法，但必须同时说明未发布变更如何参与实时补扫，不能仅换一个序列名称。

**验收反例。** 两事务倒序尝试提交，以及前一个事务分别回滚、崩溃；新记忆必须可由索引补扫或增量读取取得，回滚不能造成永久等待。此项不需用户改变已确认边界，但需在正文选定一种实现机制。

## 已覆盖项

下表仅表示未在本次推演中发现新的反例，不代表实现或运行验收已经通过。

| 审查主题 | 现有充分约束与位置 |
| --- | --- |
| 同提交域与并发占用 | [Task 锁序](/Volumes/Data/proj/lerna-docs/docs/architecture/task-runtime/implementation.md:107)规定原命令、祖先、预算、操作与槽的次序；[Grant 多许可锁序](/Volumes/Data/proj/lerna-docs/docs/architecture/security/implementation.md:127)及[确认共同消费事务](/Volumes/Data/proj/lerna-docs/docs/architecture/security/implementation.md:179)避免靠进程内读后写消费。工作槽的新责任覆盖问题另列第 1 项。 |
| 跨 owner 额度交接 | [接收门禁与子创建](/Volumes/Data/proj/lerna-docs/docs/architecture/task-runtime/implementation.md:320)争用同一记录，未知关闭先持久禁止；父只凭最终关闭证明结算，原创建失答复不释放预留。 |
| 内容 GC 与跨库持有者 | [上传采用和孤儿清理](/Volumes/Data/proj/lerna-docs/docs/architecture/contracts/transport.md:170)锁同一元数据；[跨库发布](/Volumes/Data/proj/lerna-docs/docs/architecture/memory/implementation.md:220)明示 TOCTOU 窗口及在线复核；[双修订门禁](/Volumes/Data/proj/lerna-docs/docs/architecture/memory/implementation.md:228)不可重开；[实际停止及物理清理](/Volumes/Data/proj/lerna-docs/docs/architecture/memory/implementation.md:549)不由接纳回执推出。 |
| 长期增长与热表 | [存储布局](/Volumes/Data/proj/lerna-docs/docs/architecture/storage-and-middleware.md:108)区分活跃索引、历史和完整原键墓碑；[维护预算](/Volumes/Data/proj/lerna-docs/docs/architecture/storage-and-middleware.md:119)涵盖 vacuum、WAL、副本与重建；最后禁止依据不按时间清理，容量未声称已达标。 |
| 备份和灾备依赖 | [备份清单](/Volumes/Data/proj/lerna-docs/docs/architecture/deployment.md:196)绑定一致切点、对象版本、安装锁及凭据代次；恢复缺切点后的关闭、撤权、命令或效果事实时只能只读诊断。[地域故障边界](/Volumes/Data/proj/lerna-docs/docs/architecture/deployment-production.md:224)未承诺旧备份自动恢复写入。 |
| 物理迁移与格式迁移 | [分区搬迁](/Volumes/Data/proj/lerna-docs/docs/architecture/storage-and-middleware.md:35)隔离旧写者、固定逻辑 Home、按完整切点恢复后切映射；[格式迁移](/Volumes/Data/proj/lerna-docs/docs/architecture/deployment-production.md:324)分批检查点和兼容回退，不回滚领域历史。 |

本轮无架构、Schema、方法或示例改动；未运行全量回归、渲染或真实数据库并发实验。
