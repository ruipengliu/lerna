# 04票02：6a9固定源码架构复核

2026-10-04 17:56 UTC。BASE `f05b2f1068958ba6b63e6c1dc58d6cd1684446cc`；PRIOR `75202ee0077b5bf12b432b5017ad97a1dfcab681`；SOURCE **`6a9aa658c912be5c8fed022ceda2511212d19a70`**。只读 `/tmp/lerna-worktrees/content-snapshots-02` 固定 Git objects、自身原3f44/752架构报告和已采用窄决定；没有读取Std/Spec报告，没有Go/native/DB/build、环境/凭据/清理或仓库修改。沿已授权 improve-codebase-architecture/codebase-design；词汇依据根CONTEXT，无新增GLOSSARY、子agent或权限询问。

**结论：0项新增 Strong 必须重构。已采用迟到准入、同Tx完整来源观察及联合动作资格，在本固定源码中兑现；保留现 module，不造通用框架。完整64/65 race仍实际失败，七AC和whole04尚未退出。** 这是源码架构资格，不是性能问题已解决或最终接受。

## 1. 固定范围及继承证据

实际 base→source：9 commits / 54 paths / 8419增211删；prior→source：13 paths / 1330增222删。完整tree从910到912条目，**899个条目的mode/type/blob完全相同**；13差异均100644 blob，11修改、2新增，无隐藏mode/type变化。本次完整读13项差量，两个新增测试文件全文、closure及工具入口完整读；其余准确相同部分承接自身既有阅读，不以另一审核轴代替。

13项为 Makefile；PG content facts/management/policy；domain content closure/management/ports/service；content_closure_test、content_policy_actions_test、content_post_schedule_test；scripts component-integration-race.test.mjs/test-component-integration-race.sh。

0001/0002 SQL、全部三组原writer archive、row readers及其测试、旧恢复fixture、旧处理期限测试皆准确相同，保留此前mode/type/blob与原SHA资格，不重写原归档也不把本次差量review冒完整重新archive审计。旧expired archive原dump112782 bytes、SHA256 `f0be7063ce24ab1f491e316ec4028df82e5f630e545a9f443d708b2d96d8d829`及全部6项原清单资格承接3f44；原0001 checksum保持 `00363b79dafb6eb1f373be9ef08e915fe23cc3db0fa55345e8ae6c051ce0c1ed`。

关键最终blob：service `68789baacde422ed80b5b8a9615c751a0d032f5d`；closure `f78c51e10d52eea2c7e7a7537622fe27d84a4735`；domain management `3e07b466d896748497b8933a86f16075d9c47e42`；ports `2ca2ffa51ad61adfed1642aeae0de36c22af15e7`；PG policy `eeb5a5777d41477783cfd5d786dfd739fbd91856`；post_schedule_test `49bb76200416b7355f0d774552b21c4de0139084`；policy_actions_test `eec69d92537c3e053644fbd587838425b94636ef`。

## 2. A — 准入完整回滚与原键固定结果：Strong，源码闭合

**Files/问题：** service.go:87、243、343；post_schedule_test.go。原调度后的阻塞可以跨过原准入资格；用旧reject返回nil会一起提交部分版本/来源/责任/Job。此前必要决定是准确回滚，不是把SQL撤销知识移给调用方。

**当前implementation：** 每次Put唯一非零大小 `lateAdmissionAbort` 指针；所有accepted暂存写后，finalAdmission重锁目标消费管理可能收紧的实际Current cap，检查当前reader并重新DB Now，失败清除暂存receipt、返回该指针。仅 `err == late` 开最多一次同原ctx第二短Tx；不使用宽泛errors.As吞并其他cause。第二Tx原键锁前后均核reader，同主体/摘要优先真实并发原receipt，异摘要冲突；否则仅写原拒绝Command、写后再核reader。任一真实CommitUnknown沿原command_ref查询/重传，不变成确定拒绝。

**interface/test surface：** 测试经真实Put/GetCommand/Get与管理观察，机械seam只插在实际ScheduleRetention之后、真实回滚之后或真实Commit之后。新代码覆盖四种截止正常/迟到、alias原主体/cap、extra-cause不固定、并发winner/异摘要、accepted/rejected提交未知、命令reader锁等待/二次Save等待。机械“Commit成功后返回未知”明确不是PG网络故障；callback额外cause不冒nativeRollback/Close失败。

**depth/locality/leverage：** 私有两阶段裁决集中于Content准入module，公共interface未增方法；新版本和alias两实际分支复用finalAdmission。deletion test：删除它会把最终cap/时钟/回滚决定复制进两个分支，或迫使host补偿已提交业务，复杂度不会消失。保留现seam，不提通用两阶段命令框架。

Before：暂存版本/责任→迟到reject(nil)→部分事实commit风险。
After：暂存全部事实→当前最终门→失败整Tx abort→重获原键→winner或仅固定拒绝；unknown独立。

## 3. B — 完整闭包事实复用：Strong，源码闭合

**Files/问题：** closure.go:12/32；service.go:219/285/330/501/847；management.go:496/614/625；ports.go:81；PG management.go:474。旧完整遍历后多个消费方立刻重复读同一来源，知识与成本分散；先前私有观察和窄调度参数正是对此实际摩擦的调整。

**当前implementation：** 私有closureObservation同时返回完整排序refs、已锁Record及当前动作的valid/retain最严值；保持64/去重/fullRef冲突/环/owner/中间版本，不把结构集合当生成事实。纯排序提前算身份，不在比较器重复编码。nil动作结构路径无无用时钟；实际动作路径完整检查。Put/Get/publication/AuthorizeUse消费同Tx观察，不跨I/O或Tx缓存。

ScheduleRetention新增 `[]Record` 的实际interface注释限定同Tx成功完整观察、空集合完整、禁止装饰器替换。PG只转发；管理新私有scheduleObservedSources复用同一责任循环，仍逐来源以**目标原保存record.Subject/Purpose** LockPolicy与原basis，不是alias新主体或source自有主体。legacy独立结构遍历保留；restoreLegacyMaintenance改source后重新LockVersion/ReadChange。目标被即时维护收紧时，A最终门重取其真实cap。

**depth/locality/leverage：** 域拥有闭包/资格规则，PG adapter拥有事实锁和持久化。删除观察module会把重复资格计算重新摊给四个实际消费位置；删除PG小转发会绕开Repository装饰器或让域依赖具体存储，未减少复杂度。当前只有一个真实PG storage adapter；多个消费方/故障装饰器不冒称多个业务数据库adapter。根AGENTS要求消费方声明实际接口，优先于“一adapter即删端口”的机械套用。无需导出可复用授权token或任意图遍历框架。

Before：完整遍历→各caller重读→管理重遍历。
After：一次完整锁定观察→各自同Tx最终门；管理只复用结构，另核原保存政策/独立责任。

## 4. C — 有限动作交集与元数据前当前资格：Strong，源码闭合

**Files：** PG policy.go:34–100；closure.go:70–114；service.go Get目标段；policy_actions_test.go。CheckPolicy现有interface改为单ref/主体/用途的有限actions集合，不再额外建立可绕过装饰器的并行批量端口。PG仍先FOR SHARE再独立core.Now，完整subject/tuple/purpose/ValidUntil核验后逐flag检查；0、>5、重复、未知集合均nil拒绝。完整相等主体只省重复纯编码；LockVersion完整Ref/id/key全相等才省重复ValidateIdentity，其他路径保原错误。

域在进入来源LockVersion前核policy.Ref；锁返回后现有Now移至元数据判断前，先policy.ValidUntil→forbidden，再RetainUntil→expired，然后record缺失/fullRef/publication与Current cap。明确这是已采用的保守当前资格次序调整，不说旧逐纳秒/metadata顺序原样未变。Get目标两单动作/F1实际recordRef授权先披露保持。

**interface is the test surface：** 四种真实记录锁等待加mechanical nil/mismatch组合、六种正常/独立flag、七个内部合法/非法向量、原policy锁等待正常/迟到都有准确消费入口。损坏metadata替换是机械故障，不冒真实PG丢行。诊断装饰器按新集合签名转发，同时计calls/logical_actions，未以次数当业务oracle。

**locality/leverage与deletion test：** 单一政策module收拢同一行独立动作交集；删除会把flags组合与重复SQL摊回所有closure callers。现有原字符串动作保持闭合小集合，当前不需要权限DSL或新ADT框架。保留可直接核验的政策和时钟实现。

Before：同ref逐动作重复读政策→锁record→先metadata再时钟。
After：单锁政策逐flag交集→锁record→当前时间资格→metadata→原完整最终门。

## 5. D — 动态四组入口与机械工具验证：Worth exploring / KEEP

**Files：** Makefile:36–43、test-component-integration-race.sh全文、component-integration-race.test.mjs全文。脚本动态发现Test/Example/Fuzz seed，以准确正向集合分closure/content/durable/other；duplicate/empty/歧义发现拒绝，空组不传空selector，未知将来合法名称仍归入实际组。每组保留-p=1/-count=1/integration/120s，normal仅去-race；完整64/65单项仍自身60秒，没有按链片段拆开。Make normal入口复用同脚本，其余真实fixture/objectstore套件保持。

机械Node测试使用现boundedBuild/ownConformanceScope，检查发现失败/分组native失败保status、Unicode/Example/Fuzz/未来case完整选中、normal相同partition。它只证明外部Go命令协议，不是PG业务执行或实际全套CI。已有生命周期module责任不移入业务fixture，unknown scope仍不能因脚本分组假关闭。

**depth/locality/leverage：** 一个脚本interface服务normal/race两个实际入口，删除会复制准确发现与分组知识。四组来自实际新增套件压力；不建立可配置通用测试调度器。未来实际build tag集合若变需准确复核发现范围，本次不假想第二runner来扩interface。

Before：normal整包/旧race分组各自入口。
After：动态完整inventory→同四组→normal或race；任何组失败保持失败。

## 6. 原F1与七AC职责承接

原8d64 F1“迟到初始历史页漏原自然到期交接”保留原finding和3f44修复历史；6a9 management.go:974仍按phase==policy_change切原ExpiryDue/ExpiryDeadline、原watermark、cursor重置，已逾原截止residual。不能把后来修复倒写为旧pin通过。processing_deadline_test准确沿752的Manager/Content两个真实consumer正常/迟到四场景，逐blob未变；历史pending合并及六类具体typed readers继续KEEP，无新框架。

| 票02 AC | 固定源码责任与本次影响 | 当前资格 |
|---|---|---|
| 1 五动作/准确主体用途政策 | 集合逐flag、fullRef/subject、锁后clock；原管理revision不变 | 源码保留，最终执行待完成 |
| 2 完整≤64闭包 | closureObservation保全部中间版本/去重/环/owner；计时不截断 | normal全链已有，race全链失败 |
| 3 最严用途与期限 | 完整bound；alias原主体维护；调度后实际target cap+整体rollback | 新公开正常/故障源码覆盖 |
| 4 发布最终Tx当前门 | publicationPolicy各独立Tx重新完整观察；不复用前一Tx许可 | 原责任保留，不冒物理瞬停 |
| 5 封新用+耐久传播holder责任 | 原policy/current gates、原D/A义务/水位/阶段/原deadline；legacy改后重读 | 0002/历史archive不变；05物理删除未实现 |
| 6 不泄露元数据/字节 | 来源记录等待后当前资格优先；Get目标F1保持；合法对照 | 本次新顺序已代码化，非全权限证明 |
| 7 公开行为与真实恢复 | 新测试公开receipt/Get/管理观察/reopen/独立字节；计数只诊断 | 不把fixture当Grant/Use或生产授权 |

whole04 spec的准确字节/当前来源约束由当前模块承接；Snapshot mandatory context、提交竞争与最终物理删除/holder全清理属于后续已发布票，本次不替其验收，也不把后票变成票02隐藏前置。CONTEXT及ADR0004/0006/0007职责无新冲突；没有Task/Grant/Memory/Provider业务外溢，无需新ADR。

## 7. 执行限制及下一步

root提供/已核：whole64 normal31.931s PASS；联合动作完整race60.08s FAIL/native1/group absent；新增受影响controls race29.796s native0/group absent。本代理未执行这些命令，本次以源码审查为主，不用子集通过覆盖完整race失败或最终CI待办。原first schema fixture失败与真实业务red不可混同；历史unknown隔离scope仍保留，不由本报告授权清理。

下一 **MATERIALIZED锁行CTE+外层clock及有限profile overlay** 是 `/tmp/lerna-04-ticket-02-after-policy-group-perf-decision.md` 的独立未来候选，**6a9并不存在该SQL**（policy.go:44/53仍两次查询）。先取得真实计划/锁后clock证据与完整原时限结果，再仅按实际新objects复核。不能把候选的预期收益写进当前架构closure。

Top recommendation：保留已加深的Content module和准确消费seam，推进有据有限性能诊断与一处候选；不为代码长度/抽象优雅再重构。0新增必要结构finding不免除已知性能及最终normal/race/check/audit/CI待办。七AC未resolved，whole04未退出。

HTML：`/tmp/architecture-review-20261004T175605Z-content02-6a9.html`；提供Tailwind/Mermaid CDN增强和inline CSS/静态before-after fallback。实际执行 `xdg-open` 返回 **exit3**：无可用 www-browser/links/elinks/lynx/w3m 打开方式。headless未浏览、CDN未验证；没有下载CDN、安装浏览器或启动Go/DB。静态文字与方框图无需CDN。
