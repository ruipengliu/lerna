# 04票02：联合政策检查之后的有限诊断与下一 SQL 候选

2026-10-04，授权 Astra/high 只读窄决定。基准仍为 `75202ee0077b5bf12b432b5017ad97a1dfcab681`，本次直接读当前 WIP 的 domain/content closure/service/ports/management 与 PG content policy/facts/management 相关路径及指定日志。root 已核实际10对象 manifest/binary身份；本稿不把其组合摘要冒作新 Git commit，也不代替 root 的逐字资格。未运行 native/DB/build、未改源码、未读其他审核轴。

## 1. 判断及默认顺序

**下一候选仅改 PG CheckPolicy 的取行/锁后取时钟这两次往返：用锁定行的 MATERIALIZED CTE，再在外层投影 clock_timestamp。** 先以当前同源 binary 的有限诊断补足最新耗时分解，然后做该候选的真实锁等待资格与原完整用例对照；不增加端口，不放宽时限，不再盲挤 encoder。

管理调度中复用已有 policy/basis 不是本轮默认：实际 `LockPolicy` 先取 advisory(3) 再 `FOR UPDATE`，而 CheckPolicy 只有 `FOR SHARE`。用后者的记录值替换前者会同时删除两个实际锁责任，不能以“政策已锁”称等价。`ReadChange` 对原 source/revision 的 basis 使用 FOR UPDATE，每个 D/A又有不同精确目标 change。当前源码不足以把这些读当无意义重复而跳过；保留已采用完整 source Record 同 Tx 复用，不扩成管理政策缓存。

## 2. 已见数据能说什么

实际读 `closure-policy-actions-race.log`：完整64/65用例在 i58 Step、60.08s失败，native1/group_absent=True。Install1.967 / Put33.196 / Step24.460秒。新 CheckPolicy 的 Put1770次/5192逻辑动作约3.291s，Step3435次/10071逻辑动作约7.102s；LockVersion约4.141/8.087s，Now约2.849/5.514s。ScheduleRetention59次约17.476s包含其子调用，不能与子项求和。

实际还读现 `closure-policy-actions-normal.log`：完整到 i65并最终 Get，31.92s、native0/group_absent=True。该日志是独立正常运行证据，源与binary关联仍由 owner/root 原 manifest 负责；本稿没有运行它。normal与race构建不同，不能用31.92对60.08差值归因某一机制。

单次样本中共同端口平均时间明显抬高，既不证明“联合动作导致退化”，也不证明网络、PG、CPU、调度或系统争用是哪一个原因。之前 identity/closure CPU profile 属旧 binary；不能直接代表目前热点。新旧 CheckPolicy 调用单位不同，只能比较相同资源集合/实际逻辑动作与分阶段成本，不能用次数比例宣称性能倍数。

## 3. sole owner 的有限诊断计划

1. 冻结当前实际源码快照与 race binary身份，沿原完整测试、60秒ctx/120秒外层、独占 native 槽及独立 ACK scope，最多进行一次当前 binary 的 CPU profile 诊断运行。已有有效同源最新 profile 则直接使用；不为“再看一次是否偶然过”无限重跑。profile有开销，若失败仍真实失败，不能替代最终无profile验收。
2. 现有阶段/端口计数继续使用，补足**有限临时诊断**：CheckPolicy 分 subjectKey/SQL QueryRow+Scan/独立 core.Now/解码与验证；LockVersion 分身份计算/advisory Exec/row Scan/解码；ScheduleRetention 分 LockPolicy（其advisory与row分开）、ReadChange、目标change读/写/Trigger及fresh clock。可在独立诊断构建中包围这些实际阶段累积计数，不向生产 Repository 添加 Metrics/Tracer 框架、不输出 SQL参数或业务正文。
3. 避免把 owner 内重叠 span 相加；报调用数、总时间、可得的有限分布/最大值和当次已完成 i，包含失败尾部。客户端计时包围 Scan 只能叫“数据库调用端到端等待”，不能声称已经分离网络与服务器执行。需要区分计划时，仅对自己 fixture 的准确 SQL/实际版本用有限 EXPLAIN/锁等待观察，不以私表业务内容作验收 oracle，不观察别人 scope。
4. 结合已有normal31.92，诊断后直接实施下述一处候选并测其资格；不要求先新增大观测系统。若新 profile 显示该重复往返成本很小而另一个实际阶段占主导，可停止此候选并带准确阶段证据给 root，不自动升级成其他批量/索引方案。

本代理不执行这些步骤；sole owner 可与当前机械CI工作按其原独占顺序推进，不需新用户确认或资源许可。

## 4. 一处 SQL 的准确建议

将 CheckPolicy 当前“锁定行读取 body；然后 core.Now”改为以下形状（仍使用现可信 table 构造和原参数）：

```sql
WITH locked_policy AS MATERIALIZED (
  SELECT body
  FROM <content_fixture_policies>
  WHERE tenant_id=$1 AND owner_id=$2 AND subject_key=$3
    AND content_id=$4 AND version=$5 AND purpose=$6
  FOR SHARE
)
SELECT body, clock_timestamp() FROM locked_policy
```

扫描 body 与 DB time；后续完整主体、tuple/purpose、ValidUntil、全部独立 flags、nil/error以及域层 fullRef 顺序保持。无行仍 sql.ErrNoRows→nil，没有凭空的“当前时间即存在”结果。政策值未知或时钟/扫描错误照原失败，不回退到旧 now。

采用该形状的理由是建立**行依赖**：物化 CTE 的锁定行必须由 LockRows 获得并输出，外层从该行取结果时才投影 volatile clock_timestamp。时钟不能出现在加锁 SELECT 的内层输出列表，也不能独立放在一个无关联 CTE/LATERAL/InitPlan 中；这些写法可能在等待行锁前执行表达式。禁止使用事务开始时固定的 now()/transaction_timestamp 或语句开始时固定的 statement_timestamp 代替。

MATERIALIZED 是执行边界，不仅是美观 SQL。本稿给出源码/执行依赖推理，**不声称已在目标 PostgreSQL 证明实际计划或时间顺序**。owner须核当前实际PG版本支持此语法，并在实际查询计划确认 CTE锁定行/外层时钟位置；若计划/真实等待不能满足，保留现两次查询，不用“看起来像”等价宣称通过。只改此方法，不同时改 CheckCommandReader、LockVersion、Core.Now 或管理 SQL，以便定位效果与回归。

## 5. 当前授权与时钟资格必须保留

- domain closure 的 pre-node Now、记录锁返回后的 Now/先Policy.ValidUntil→forbidden再RetainUntil→expired再metadata、source Current cap，以及所有 caller最终 freshNow均不改。只替代 PG CheckPolicy 内部已有锁后取时钟的往返，不减少这些观察门。
- 完整 policy Ref gate、Get目标两次/F1、五动作交集、单主体用途、原 Command优先、post-schedule回滚及原deadline不改。不同Txn不复用时间或policy。
- 原真实政策行锁 holder：先启动 public调用并确认它实际等待，再跨原 ValidUntil释放；必须拒绝，且有原截止前释放成功对照。候选的锁等待验证应直接覆盖 CheckPolicy 自己使用的时刻，不能仅依赖更后的域时钟把错误早采样“救成拒绝”。可在受信 fixture 调用同一真实端口观察 nil结果，另保留public结果对照。
- 红例必须是可证明在锁前采样的明确旧时钟变体/真实机械门；不能强称任何随手写的单SELECT必然红。候选实际计划与跨锁clock还须用同一实际PG验证，不能用Go假Clock替代。
- 保留联合动作 normal/flags/非法集合、真实记录锁等待和mechanical nil/mismatch组合，以及读返回前撤权/原budget/原D/A责任恢复。没有source/API迁移或旧archive改动。

## 6. 效果与退出限度

这处候选在每次实际有行的 CheckPolicy 内省一次网络/driver往返及其编解码，保留同一锁后时间事实；不能保证总耗时少到60秒以内。最终仍要原无profile完整 normal/race、64准入/65拒绝、所有逻辑动作及最终 Get真实退出；锁等待资格过了只证明这一处时间语义，不等于大用例已过。

若继续失败，报告冻结源/binary、实际阶段和已完成工作量，由 root 再决定一个有证据的小改。不得扩大60/120、减少来源/中间版本/动作/责任、刷新budget或deadline、改已发布0002checksum、加盲索引、跨事务缓存、跳suite或把诊断工具exit0叫业务green。七AC与全片04状态不变。
