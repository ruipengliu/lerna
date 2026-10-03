# 切片 03 六票草稿复核

**正式采用：2026-10-03，前置02已按源码5548744/退出文档df2dbe5完整退出；授权决策代理完成最终接法复核，root据[最终交接](final-handoff.md)采用本记录。下文准备时的pin/pending状态保留为历史，当前接法及采用门槛以最终交接为准；没有宣称03已经实现。**


2026-10-03。结论：六票、42 条验收保持 tracer 粒度，依赖与 spec 六项覆盖完整。已窄修订四份 `/tmp/lerna-03-ticket-drafts/` 草稿；没有修改仓库或对外写入。本结论是**可在切片 02 完整退出、并以届时实际 runtime 复核接法后发布**，不是现在启动 03 的授权或验收通过。

依据：03 spec、`decisions.md`、六份草稿，以及实际 `contract/readfacts.go`、`contract/query.go`、1.0.0 values.json、generator 和 negotiation。当前仅复核准备文本，没有实现或运行 03 集成测试。

## 逐票结论

| 票 | AC | 粒度及真实依赖 | 结论 |
| --- | --- | --- | --- |
| 01 durable-rule-proposal | 9 | 02整片退出；decide接纳→同事务事实/receipt/Job→确定性Proposal与必要发布→get是真实完整出口。Schema/生成/存储随这一出口交付，不能拆为各自半实现票。 | 保留。补明确版本导出、可达摘要、root广告责任，以及不存在原请求的cancelled状态如何表达。 |
| 02 bounded-proposals | 7 | 只依01。新增候选、来源与非法提案的可观察状态有正常和恢复对照，不依取消。 | 保留。补固定私有fixture配置摘要，防止错误输出注入暗改同一准确策略身份。 |
| 03 cancel-and-limits | 8 | 只依01。正常候选足以验证取消/完成竞争、资源边界和墓碑；不要求02所有分支。 | 保留。补fixture签发身份和先取消的准确get；特殊恢复由本票承担。 |
| 04 durable-test-target | 6 | 02整片退出；与01/02/03的Decision内容无数据依赖。独立SQLite目标及query/observer构成当前可消费测试设施。 | 保留。将外部先决写明，不再将Blocked by写成容易误读的“无”。 |
| 05 durable-fault-plans | 6 | 只依04。已存在真实目标后，故障计划才有实际提交/迟到事实可观察。 | 原稿保留。seed重演与原cursor恢复清楚，有限截止/清理明确。 |
| 06 process-recovery | 6 | 只依01+05。正常Decision和target故障足以证明本票发布、提交、plan的进程接替；不需要全部候选和取消。 | 原稿保留。没有02/03隐藏关闭边，没有把全片整合放进本票。 |

六票的AC数量为9+7+8+6+6+6=42。依赖链为：02整片→01/04；01→02/03；04→05；01+05→06。这里左侧“02整片”和本片候选票02须在正式发布时明确区分，并按仓库票据格式使用准确链接。

## spec 验收覆盖

| spec原验收 | 负责票与可观察事实 |
| --- | --- |
| 1 固定输入、原Decision去重、Proposal/必要内容恢复 | 01公共decide/get、固定双身份和实际发布；06发布/完成阶段进程恢复。 |
| 2 含混提案及有依赖行动拒绝 | 02闭合推进分支、max4、输入包含与未产生结果依赖反例；合法delta+单分支保留。 |
| 3 实际写入丢答复、迟到与not_found界限 | 04独立目标事实出口；05提交后丢响应与耐久门闩迟到。 |
| 4 窗口内幂等，过期/无查询保证不足 | 04原键窗口/guarantee_expired/unsupported；05验证迟到责任不被窗口或not_found抹除。 |
| 5 原身份和计划跨重启保存 | 01/04真实重开；05耐久cursor；06真子进程故障。03另自证取消墓碑恢复，02自证新增候选/失败状态恢复。 |
| 6 正常对照、每次故障有限结束 | 每票明确正常路径和有限I/O/等待；05/06单独验证计划及进程清理，不靠任意sleep。 |

## 与当前代码边界的核对

1. 旧 `CommandFactReader` 返回旧 `CommandGetResponse`；`ReadCommandFacts` 对 reader error、编码失败均返回 `unavailable / dependency_unavailable`。其 reason 为 const，不能通过所谓typed error穿透变成 version_unsupported。草稿采用实际可行路径：旧adapter无损表示才返回事实，否则返回error；新版reader/1.1 get准确提供新固定receipt。未发现草稿要求修改旧映射或伪造旧reason。
2. 旧 `DecodeCommand` 只返回 CommandGetRequest，旧 negotiation 只支持其准确版本。新1.1入口、生成目录及隔离缓存确实需要随01增加；不能将旧入口改为全新命令联合、扩张旧清单或强转。现有generator单源硬编码不能直接完成双版，但有限两个源的显式配置属于已批准01工作，不是阻止发布的设计冲突。
3. 现有窄生成器支持named discriminator oneOf、allOf/if/then/else与数组bounds，可承载Proposal advance与状态变体。local_key唯一及准确来源包含不能假装现有Schema已经验证；正文可判定规则由双方codec共同验证，Snapshot相关规则由真实组件验证。无需预先加未被支持的uniqueItems或开放任意对象。
4. `ContentRef` 当前有准确owner、content_id、version、hash、media_type、byte_length；`Amount` 当前有unit与integer_value。fixture可以准确填入并耐久读回，不能伪造Content已发布或把fixture unit当真实费用。源/publisher所属owner及短事务边界已在票01说明；它们并不承诺04/05真实领域服务已经实现。
5. **先取消的数据形状是必须预留的实际约束**：cancel-before-decide只持有原Decision/task/input digest及控制依据，不必也不能伪造完整decide payload或Snapshot。新get的cancelled关闭状态应只要求实际已知字段；其他状态再按各自变体要求准确输入/Proposal。因此窄补01/03，使实现者不会先在所有Decision状态强制必填Snapshot而在03被迫撒谎或破坏新版Schema。仍不提前交付取消业务。
6. 42条中无要求复制02全部数据库适配或预建供应商框架。规则组件真实PG、独立目标SQLite及各自新增迁移/恢复是恰当范围。01的“受影响旧版升级明确”不要求虚构尚不存在的Decision旧业务库；新owner说明首次初始化，若修改02实际共享表/机制则验证真实受影响旧路径。

## 广告、发布门槛和最终整合

草稿可以先有1.1闭合机器类型与直接Component测试入口，但未完成decision_engine profile不能出现在可用协商结果中。01中已写清完整清单由root在全票退出整合时开放；不得提前以Schema存在宣称方法已实现。root最终检查应包括准确inventory/完整可达摘要、旧1.0源和黄金不变、新版共同往返、全部六项证据与审查结果。06可以在自身01+05先决完成后独立关闭，不等待02/03而冒称全片关闭。

正式发布前只有两项执行先决：

1. 切片02全部八票及其代码/架构审查、真实数据库与故障证据完成，root记录完整退出，而非只看到首票或部分runtime已合入。
2. 以届时实际Tx/Job/Claim/Clock、命令账本、进程设施和owner迁移接法复核本片小接口；发现实际接法差异只做必要局部细化，不提前冻结当前不存在的函数签名。确认代码依赖遵守consumer-owned端口和fixture的Go internal可见性。

满足后可按现有六票发布；目前保持/tmp草稿。没有需要再向用户提问的领域选择，也没有需要新增ADR的领域改变。
