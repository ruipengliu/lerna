# 03 正式采用前最终交接

2026-10-03。结论：**whole02退出及03采用前的窄端口复核门槛现已满足，可以由root正式采用并发布已批准六票，随后按真实frontier启动01与04。** 本记录本身没有发布票或启动实现，没有新领域选择、待问用户问题或需要新增的ADR。

## 精确基线与本次动作

- 最终受测修复代码：`f56d93095304f0956c23b5641d9b7b1e222c40c1`。
- 准确整合代码/CI：`554874470d5abeb71fa743708580f3121b8944f1`。
- 正式退出文档及本轮只读HEAD：`df2dbe5624120bc258dc7419ea022a08ebc0d6b0`，读取时工作区clean。
- 前次源码pin：`c52e68b46c619df0c8e5df1b27a0b5dded3ef65f`；前次文档pin：`56f9d7b0d4c544ad34f54549de0c303b65cea32d`。

已读取tracked exit-evidence、spec Implementation completed、最后两轴及架构/CI更新；按git实际比较c52→f56/554→df2。增量源码仅owned_fixture、owned_pg_fault及新增owned_cleanup_failure三个recovery测试文件；产品runtime/internal/adapters/host、contract/sdk/generator以及conformance冻结来源无差异。f56与554产品/测试源码相同；554→df2只有正式退出文档。这不是重新运行数据库或测试，测试/CI结果引用root已核实且纳入仓库的准确证据。

02记录原八票53项+09五项+10十项，共68项resolved；两轴原发现与后置P2均关闭，新增各0，架构收益已闭合。准确CI37162569420 success：工具生命周期race2.407s、完整双库20.985s/race45.463s、27项hash；本地准确f56顺序normal50.431s/race93.834s。基线/服务/故障范围及此前失败历史以 `.scratch/lerna-02-durable-work/exit-evidence.md` 为准，不以本次只读复核声称再次实测。

## 生命周期最后修复与可复用边界

修复将holder的**退出确认**与历史事务错误分开。join确认结束时保留原SQL/context错误并继续安全Close/Drop/admin收尾；未确认退出仍保留scope，writer/peer未确认Close同样不删除。实际child Wait已确认退出但带错误时也保留诊断而允许安全后续步骤。Cleanup先有独立有限holder join context，再有有限清理context；没有变成无界wait、吞错误或猜删资源。新增真实数据库失败/有限未退出对照及现有正常路径已由退出证据覆盖。

ownedFixture仍是当前demo专用的waitStore、PG/SQLite writer、holder和hostProcess组合；票10没有生成通用owner factory。03要强类型Decision fixture与独立source/publisher/target装配，不能补假demo接口。只有真实第二consumer需要时才提取小的scope/admin或有限进程机械部分，不把历史loader/故障配置改成资源插件注册框架、不恢复caller的Store-pointer登记。最终清理修复不改变这个边界。

## 实际产品接法确认

1. **Tx与账本：** same-Store/owner/active opaque token、短Tx/Clock、cause Unwrap全部保持。runtime没有通用Store；CommandRecord/Admit仍具体1.0。新Decision采用独立PG owner/schema、自己的迁移ledger、强类型1.1账本及FK到真实Decision的Job，不插demo durable_inputs，也不拿旧Store token跨owner写入。
2. **容量：** demo.PoolRepository/State仍具体耦合project/schedule，实际第二consumer引入时才提升共用容量值/纯FIFO及必要PG机械SQL；consumer声明自己的小端口，不搬整个demo业务或复制完整Store。最多64显式members、同scope tenant×lane、命名范围和锁序保持。
3. **唤醒：** 实际PoolNextWake涵盖整个登记pool的未来due/lease/scan及current/live claimed deadline，不只是anchor。Decision按自身真实阶段/截止实现相同责任，没可变输入就不伪造demo revisions。quota满/0和已due不可领采用正fallback与有限维护；Timer在Tx外，control/reconciliation有真实独立机会。
4. **实际资格：** pool→业务对象→Job；prepareClaim在可能等待取得Job锁后重新取得可信时间并复查PoolClaim/Claim，Start/Finish共用，旧Complete也不绕过Start。新Decision必须从首票落实当前limits/deadline、原Claim/epoch、pool占位和真实启动门禁；票03负责完整取消/极值语义反例，不替首票补一个无界入口。
5. **新合同：** 1.1仍须真实生成/实现，当前generator及Go/TS公开入口只有1.0。按已批准两个明确source配置、独立package/subpath与Schema/cache实现；旧1.0源码、方法、黄金和严格wire行为冻结，不扩旧错误集合。旧reader只无损桥接，新回执不可表达沿原unavailable/dependency_unavailable，不强cast或假typed-error穿透。
6. **取消与原身份：** 原Command/Decision双摘要域、受信fixture Task owner、准确Decision/task/input digest及单调control依据不变。先取消无Snapshot则闭合状态不要求假Snapshot；decide/cancel不用通用expected_revision破坏创建/先取消规则。接纳/当前Start检查原limits，工作epoch不刷新预算；跨owner发布按原key/digest恢复。
7. **冻结/迁移：** 旧公开1.0、已发布PG/SQLite host0001–0005及四组27个历史sources/artifacts在本次diff中未变，hash通过属上述准确退出证据。03新owner初始化无需虚构不存在的Decision旧库；真正后续新增migration用真实前版数据验证。共享机械提取仍须保留02两库受影响正常与故障路径。

没有发现会改变现有03六票或上述领域决定的新冲突。具体Go签名、DDL/索引、typed fixture文件组织由首票实施者根据这个实际基线作最小选择，不需要再重复grill。此处承认03的先决已满足，不承认1.1/Decision/fixture source或新的Component行为已经实现。

## 正式采用文件与真正依赖

本记录与 `decisions.md`、`storage-handoff.md`、`capacity-handoff.md`、`port-recheck-history.md` 合用；最后一个报告中“等待P2/whole02”的历史状态由本记录更新，其技术接法保留。六份 `/tmp/lerna-03-ticket-drafts` 已仅更新01/03/06最新接法注记为当前最终依据，没有新增AC或scope。

- 01：9 AC，前置whole02现已满足；新合同随正常decide→accepted+Job→Proposal/发布→get tracer交付。
- 02：7 AC，仅blocked01；全部候选及其特殊失败/恢复。
- 03：8 AC，仅blocked01；取消/限制及其特殊状态恢复。
- 04：6 AC，前置whole02现已满足；独立SQLite目标真实原键/query/observer。
- 05：6 AC，仅blocked04；有限耐久故障计划。
- 06：6 AC，仅blocked01+05；正常Decision/target进程接替，无02/03或全片final-close隐含边。

合计42 AC；图为 **whole02→01/04；01→02/03；04→05；01+05→06**。root在六票全部退出后另做完整profile广告、准确清单/可达摘要、全片兼容/审查/证据与CI，不把这些变成06隐藏业务依赖。01和04现在可以成为首个实施frontier，由root负责发布/分配。

## 继续保留的限制

退出证据的285 PG/265 SQLite absent仅限最后fix确证登记项，不能与前轮321/319混算。旧04未知PG scope、票10首版red两次成功CREATE但遗失准确名字的scope、票07unknown CREATE/不完整CID均未猜删，无法确认清理；本复核没有操作它们。SIGKILL不等于断电/跨区耐久，SQLite postcommit storage-port故障不等于原生Commit故障，Claim fencing不证明外部效果停止，FIFO和维护需要服务机会。这些限制不被03先决满足所抹去。
