# 04票02：4c621单adapter实现差量架构资格

2026-10-04。BASE `f05b2f1068958ba6b63e6c1dc58d6cd1684446cc`；PRIOR `6a9aa658c912be5c8fed022ceda2511212d19a70`；SOURCE **`4c6219fb420cdd63ea67f6db4c0106779ea9ccfa`**。从 `/tmp/lerna-worktrees/content-snapshots-02` 读取固定Git objects，承接自身6a9完整范围报告；未读Std/Spec、未运行Go/native/DB/build、未读取环境/凭据、未改源码或清理资源。

**结论：0项新增必要架构重构。此前锁行后取时钟候选已在本pin成为一处PG adapter实现；domain module/interface未变，准确直接port测试避免后续域时钟掩盖早采样。** root报告原完整64/65+最终Get、原60秒race54.75s实际通过，关闭这一个具体性能失败；不等于七AC、最终全套normal/race/check/audit/CI或whole04完成。

## 1. 精确范围承接

实际 base→source：10 commits / 54 paths / 8548增218删。prior→source：**3 paths / 129增7删**，均100644 blob；全tree其余909个mode/type/blob条目完全相同。base完整54项中51项精确承接6a9；没有新文件、mode变更或隐藏迁移/归档差异。

| 对象 | 6a9 blob | 4c621 blob |
|---|---|---|
| adapters/postgres/content/policy.go | eeb5a5777d41477783cfd5d786dfd739fbd91856 | b1952900799084c64366f064e738045faf811b72 |
| conformance/component/content_policy_actions_test.go | eec69d92537c3e053644fbd587838425b94636ef | eb3793ad92c91e432a9c7801fc26a93644a7b140 |
| conformance/internal/contentfixture/policy_lock.go | 75197823b793f2fe2ac3b8685d08a5fc9c635e15 | 16b589f806c0a5e5252b4f9e66071461c297c438 |

三项完整差量已读；policy与fixture全文读，新测试完整新增正文读，原测试正文按精确blob继承自身6a9阅读。domain全部源码、原private closureObservation、两短Tx/唯一callback marker、当前reader/原键winner/CommitUnknown、post-record授权先metadata、Get目标F1以及管理原D/A期限/历史责任均未改。

0001/0002、三组真实旧writer归档和恢复观察继续精确承接原raw资格：包括expired archive原dump112782 bytes、SHA256 `f0be7063ce24ab1f491e316ec4028df82e5f630e545a9f443d708b2d96d8d829`及原清单全部digest；未重新执行dump/restore或全archive审核。管理LockPolicy的advisory(3)+FOR UPDATE与ReadChange FOR UPDATE均保留，未用政策快照偷换锁语义。

## 2. 锁后时钟：Strong已采用，源码闭合

**准确位置：** PG policy.go:44–54。原 `SELECT body ... FOR SHARE` 后独立 core.Now，改成 `WITH locked_policy AS MATERIALIZED (SELECT body ... FOR SHARE) SELECT body,clock_timestamp() FROM locked_policy`，一次Scan body/now。clock在锁定行CTE外层投影，不在内层LockRows输出、独立时钟CTE或无关联LATERAL；无行/SQL错误仍原nil/error。

主体严格验证/准确相等CPU复用、原tenant/owner/purpose/tuple/ValidUntil及所有独立flags未变；domain仍核fullRef并执行每节点与整体最终clock，当前Get目标双动作/F1保持。未新增接口、SQL迁移、缓存或第二storage adapter。当前Pg实现的具体往返优化没有上移到host/domain。

**module/interface/depth：** 现政策module的同一个消费interface隐藏一次锁行/可信时间的真实PG知识；多个Put/出版/Get/AuthorizeUse消费者获得leverage，调用方无需知道CTE。locality集中在一个adapter方法。deletion test：抽出新时钟/查询框架只会让锁定依赖关系跨文件，删除现必要seam会把PG知识推给domain。当前不需要新的deepening。

Before：PG锁行查询 → 第二往返clock → 当前政策交集。
After：物化锁定行 → 外层真实clock同结果 → 同一当前政策交集。

## 3. 直接port与计划观察：Worth exploring / KEEP

**准确位置：** 新 `TestContentPolicyPortClockFollowsLockedRow`（policy_actions_test.go:285）、`World.PolicyClockPlan`（policy_lock.go:153）。有限12秒fixture，原实际政策行锁与blocked确认，截止前释放返回准确policy、跨ValidUntil释放必须nil；直接消费真实Repository.CheckPolicy，没有后续domain freshNow来“救回”错误早时钟。原公开Put政策锁正常/迟到与五动作/metadata组合测试仍在，直接port观察补充adapter时序而不替代public业务出口。

PolicyClockPlan仅对自己fixture准确查询 `EXPLAIN (VERBOSE,COSTS OFF)`，未ANALYZE读取业务正文；剔除Cond/Filter参数行，输出CTE Scan/LockRows/外层clock形状。参数绑定/主体key算法与实际policy查询一致。它是当前查询计划的机械观察，不是私表业务oracle，也不是另一个生产adapter。查询文本两处当前准确一致；未来若改SQL须同步验证，不因此现在抽出通用query builder或暴露产品SQL接口。

新连接先登记到World infrastructureClosers再Ping；Close以sync.Once保留第一次错误，失败不能靠再次nil洗成确认。Rows Scan/Err/Close聚合原因；DB Close/精确scope由原World收尾。源码符合现责任接法，不宣称本次实际注入过native Close失败。

interface is the test surface：原领域消费口直接承载资格与锁等待验证；计划辅助只解释同adapter实现。deletion test：删除直接port测试会让public末端clock遮住这处SQL资格；删除小fixture方法会将owner连接登记/关闭与计划读取散进测试。KEEP此小seam，不新建模块。

## 4. 历史、执行证据与待办

root已实际核对并向本代理提供：focused7.218s native0/group absent；明确早钟机械fault1.069s native1/group absent；故障输入在failure后捕获，不写成编译前冻结；实际PG18计划为CTE Scan外层clock、其子LockRows。原完整64/65链及最终Get、原60秒ctx的race54.75s native0/group absent，binary27447014 bytes及root已核SHA。这里准确引用root资格，本代理未运行命令/独立重做日志审计，不补全未提供的完整binary SHA。

6a9及此前60.08/60.09等真实失败继续保留在自身原报告；54.75新通过只关闭同原完整用例的新源码性能基线，不倒写旧pin通过、不据单次值承诺一般吞吐或稳定余量。原8d64 F1历史页漏自然阶段finding仍保留，3f44/752/6a9已修源码在4c621逐blob相同，不能被本次SQL小改重新解释为原本无缺陷。

票02七AC职责表完整承接6a9，特别是五动作/64完整闭包/最严期限/最终Tx/传播holder/不泄露/公开恢复；本次只补政策时钟资格与原完整性能出口，不替代其他尚待最终验证。root报告当前170个native inventory（67Content/60Durable/43other）；这不是170个都已在最终pin完整通过的声明。

**Pending：最终全normal/race/check/audit/CI、delivery与七AC正式接受。** whole04未退出，物理擦除仍属05。下一可能tool-only CI“Content66拆两33、共五组”不在本source，本报告不提前认可；需要它的实际新pin差量另核。原unknown资源仍保留，不从通过一项测试推导清理授权。

Top recommendation：接受这一处adapter实现的源码架构资格，保持现module/interface；继续原最终验证，不新增框架或ADR。根CONTEXT与ADR0004/0006/0007无新冲突。

可视说明：保留6a9原HTML原pin及失败历史；新增 `/tmp/architecture-review-20261004T180904Z-content02-4c621.html`，只解释三项变化与新资格，含Tailwind/Mermaid CDN和inline CSS/静态before-after fallback。实际 `xdg-open` **exit3**：无可用www-browser/links/elinks/lynx/w3m打开方式。headless未浏览，CDN未验证；未下载依赖或安装浏览器。静态文字、方框和Mermaid原文不依赖CDN可读。
