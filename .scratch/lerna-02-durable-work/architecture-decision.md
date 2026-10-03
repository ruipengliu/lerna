# 02 最终架构选择与逐轮委托决定

日期2026-10-03。只读代码pin：`6783307ebe7d802f78f9aa7bdb1a1464ec5749e1`。已读两轴followup（0 new，原4项closed）、final architecture exploration、实际HTML `/tmp/architecture-review-1791066712436.html`、当前callers及四组skill：improve-codebase-architecture、codebase-design/DEEPENING、grilling、domain-modeling。HTML已生成；root实际xdg-open exit3，无GUI，**没有声称浏览器渲染已验证**。CI37158656088的已知状态为running，不代其报告success。

**选择：现在实施唯一Worth exploring候选，交付一张独立vertical票10。** 它是当前测试fixture module的depth改善，不是未关闭产品正确性缺陷。0 Strong/0 Speculative不改写成虚构候选评级。以下frontier由持续用户授权的决策代理代答并确认；用户已授权全部必要选择，技能中的逐题等待人类/共享理解确认由此次明确委托完成。没有省略问题或重复向用户quiz，也没有授权修改domain/ADR或开始03。

## Round 1 — 候选与不可变约束

**Q1 现在做还是延期？** 推荐：现在做，但验收必须删除普通caller对creator、replacement注册和后端重开顺序的知识。委托决定：采纳。当前admission/work/wait/retention/process/history和新增pool都有实际使用；work:608需保PG creator，retention:242另写两后端重开，process_fixture:284抽map准备child，pool:1315手工关库复制。过去04清理失效、07replacement登记遗漏是真实维护摩擦；收益不靠“未来03可能复用”。

**Q2 哪些不纳入？** 推荐：产品代码、runtime/Host合同、domain词汇/ADR、0001–0005、全部27项冻结source/artifact校验、真实故障语义不变。委托决定：采纳。禁止ORM、通用resource manager、未来Decision adapter提取、生产scope cleanup、猜删历史未知schema/CID。PG/SQLite都是实际adapter，本次不是增加内存替身来扩测。

Round 1 frontier已闭合：可以确定fixture的interface，而不是讨论未来domain职责。

## Round 2 — module、interface和归属

**Q3 seam在哪里，调用方究竟少学什么？** 推荐：在测试创建/重开/交接真实数据库处引入稳定的fixture handle；普通行为factory返回该handle，调用方从它取得实际Store并请求替换writer，不再以Store指针查询全局配置/reopener。委托决定：采纳。现有三个global maps应从普通路径彻底删除，不将它们搬到新文件继续以Store为主身份。

拟议module保持在`conformance/recovery`的测试私有实现（例如一个fixture `_test.go`及两backend adapter文件）；仅当前package使用，不先建立跨全仓testkit产品。它的interface是以下有限能力及规则，局部Go名字由实施者定：

- 创建一个新、确证自有的scope，安装当前schema并打开首个业务writer；获得其实际Store供原Host/seam使用。
- 明确关闭当前writer、重新打开/替换当前writer；重复替换仍关联同一个scope。
- 为现有process module交出本scope的private child描述，按实际排他规则先释放父writer，child退出确认后重新打开父writer。
- 最终有限cleanup所有本scope登记的writer/peer，再清理确证自有scope；可在生命周期测试显式调用，普通测试自动注册。

返回实际产品Store，不能用fixture proxy隐藏Store identity/Tx foreign-token行为。可以继续以现有admissionStore/workStore交给Host，不给测试增加业务CRUD wrapper。fixture handle是物理测试scope的能力，不是逻辑OwnerRef；一个scope依旧可含多个已声明业务owner。

**Q4 谁持有稳定清理权，creator可以交给caller吗？** 推荐：fixture私有admin独立于所有可替换writer。委托决定：采纳。

PG adapter用私有admin Store执行CreateSchema，**只在真实CREATE成功后**登记owns，再开业务writer；admin不得作为普通Store返回。关闭任何writer或连续重开都不丢created=true的清理权，最终DropTestSchema用该独立admin。各peer共用准确scope但不继承删除权。失败未知CREATE不登记owns、不根据随机名字猜测授权；报告未知范围并关闭自己已打开的连接。

SQLite adapter拥有本轮新建临时目录/准确文件路径，路径归属独立于writer。单writer Close成功后才Open替代；Close未排空/报错时保留当前句柄和状态，不把它清空后再强开。不可关闭失败就RemoveAll文件掩盖活跃writer。清理只处理owned path及其WAL/SHM/锁文件，不触碰源fixture、smoke或caller任意path。

Round 2已确定stable ownership与replaceable writer，下面只处理当前真实特殊用法。

## Round 3 — backend variation、历史与child

**Q5 专有故障配置如何保留又不泄漏基础配置map？** 推荐：PG测试adapter提供显式同scope peer能力，接受本故事需要的有限参数覆盖；fixture自动登记和关闭peer。委托决定：采纳。single-connection wait继续显式MaxOpenConnections=1；PG独立事务/连接竞争仍是真实多Store；10s fault-holder只覆盖holder Tx，业务writer仍3s，测试有限释放及11s join保持。不能把所有新writer默认为10s。

SQLite第二writer拒绝故事、Close排空和完整闭库复制仍是显式SQLite专有测试；专有adapter提供准确本轮path/借用描述或复制能力，普通behavior不学backend type switch。真实copy必须从已经关闭的完整DB产生新的owned path；新scope的打开/cleanup由其新handle负责，保留pool nonce不等于同物理scope。不得把复制改成SQL手造同形行。

**Q6 历史loader是否合并进去？** 推荐：不合并；只消费新module管理的scope/writer生命周期。委托决定：采纳。历史module继续验证全部immutable来源、writer SHA、dump metacommands/COPY、schema substitution约束及真实迁移fault。它需要“创建owned空scope但尚未Migrate/Open当前writer”的专有构造阶段，然后自行完成准确restore，再请fixture开业务writer；SQLite先在owned路径写真实artifact，再打开。该阶段只服务现有历史loader，不公开任意资源注册/cleanup回调万能接口。历史故障配置留其原adapter，不能把current Migrate藏入每次Reopen让旧格式测试失真。

**Q7 child的进程退出谁负责，scope cleanup能否假设t.Cleanup顺序？** 推荐：process module继续管理真实帧、Kill/Wait/reap和场景；fixture拥有交接租用状态，与它做窄的内部协作。委托决定：采纳。普通process故事不再取map或自己判断SQLite要先Close。准备交接自动关父业务writer；child描述只提供准确打开范围，不含创建/删除授权，DSN仍走既有私有输入途径且不打印。

已有process launcher在获得实际child句柄后向fixture的内部交接记录关联有限cancel+Wait完成手段，正常Wait/Kill+Wait确认后结束借用；启动失败关闭其已获得管道/句柄并解除已确认无child的借用。fixture cleanup先调用已有process有限停止/Wait协作，只有确认child已退出，才关闭writer/admin或删除SQLite路径。未知spawn/Wait结果保留借用未解除和准确诊断，不把无回执当已退出。不重写通用进程框架、不把psql/Docker CID生命周期吞进数据库module。

Round 3没有增加无真实caller的插件点；开始裁决失败和测试surface。

## Round 4 — error、有限收尾与真实test surface

**Q8 失败怎样报告，部分setup怎样清理？** 推荐：操作返回可判断的错误，含安全operation/backend/自有scope标识，不打印DSN；保留errors.Is/As原因。委托决定：采纳。状态只在已确认操作成功后推进。Create未成功关闭已Open admin；Create成功但Migrate/首writer失败时，仍用已登记admin有限Drop自有scope；Reopen失败不丢scope且不产生虚假当前writer。失败Open产生的半句柄必须由adapter关闭。

cleanup采用独立有限context，不复用已经取消的业务context；先拒绝新writer、取消/释放已登记借用、join现有process/holder、逐一Close登记writer/peer，确认后Drop或移除owned path，最后Close admin。失败聚合报告而非吞掉；重复cleanup安全，成功的步骤不重复制造故障，未完成步骤仍有归属并可在期限内重试。未确认停止的writer/child存在时，不删除其DB/文件；不谎报all cleaned。普通caller无需安排creator与writer的Cleanup顺序。

不得通过无限goroutine等待、background无界Close包装、随意sleep、放大业务timeout或放宽whole-suite timeout让故事变绿。真实产品期限3s、故障holder10s/失败join11s及whole normal/race timeout120保持；现有caller的有限同步点/cleanup handler负责其故事，不隐藏或省略释放证明。

**Q9 tests如何真正跨新interface而不是镜像maps？** 推荐：用既有真实业务生命周期故事作为tracer，并增加/提升有限setup-failure和scope释放观察。委托决定：采纳。interface就是test surface：Host Record→真实Claim+Start→writer关闭/接替→公共command.get/Host Observe→原事实/Job/Projection不变→fixture显式cleanup。PG同时拥有同scope peer后也可Close/reopen，最终只删本轮scope；SQLite确认前writer退出后新writer成功，真实排他拒绝与复制scope拒绝继续存在。

准确scope cleanup的验收是基础设施事实，不是私有业务表：受控观察本轮已登记PG namespace由存在到不存在，或owned SQLite目录/文件确实清理；邻近独立owned fixture仍可经Host读取自己的原receipt。允许使用受信数据库目录/文件系统观察自身scope，禁止读取durable_inputs/jobs私有行冒充业务断言。不得以map长度、登记回调次数、期望内部函数调用序列验证深度。

TDD先用这个新fixture seam写真实双库“连续两次替换+原Claim/receipt持久+cleanup不伤别scope”的垂直失败测试，并观察真实red（编译缺新interface可为最初red，随后必须有真正可运行行为red/green）。补失败/cancel释放测试应使用真实Close排空/上下文和历史迁移故障等现成seam，不虚构driver成功/伪行。再实现最小normal生命周期，一条故事green后迁移其余caller。

旧业务/故障断言不删。若删除旧浅helper专属镜像测试，只能在新interface相同行为已覆盖后替换；不能“replace, don't layer”成为删掉唯一PG/SQLite真实恢复反例的理由。不为纯文档、简单包装增加镜像测试。

## Round 5 — 完成标准与共享理解确认

**Q10 什么证明是deepening而不是搬文件？** 推荐：评审diff中普通admission/work/wait/retention/process/history生命周期caller+新pool不再持三张pointer maps、不再注册replacement、不再为PG creator保活写分支；每条backend专有故事仍显式说出自己的真实变体。委托决定：采纳。历史module的restore/fault知识和process module的OS退出知识保持locality，仅共同scope/writer知识集中。

**Q11 单票范围、验证和后续准入？** 推荐：一张10，当前02核心/修复已合入作为实现基线；业务全片exit不藏在10测试里，但02最终退出要等待10自身review/证据及最后准确CI。委托决定：采纳。相关旧真实suite保留，做必要targeted red→green和完整顺序count1 integration/race，各timeout120，不为了架构工作另开未来03产品重构。已完成核心的真实CI记录不倒写为架构后的证据。

**确认共享理解：** 代理代表已授权用户确认以上11项；当前决策frontier为空，无待猜测的领域/归属/失败/验证选择。局部名字/文件数/内部adapter写法留实施者，但不得削弱列明interface/cleanup规则。允许root将本决定和唯一票10草稿落入repo后正常启动实施；本代理本轮只写/tmp，没有产品实现、DB操作或新agent。

## 取舍、depth与限制

采用稳定handle会改动现有测试factory及部分caller，而不是最小行数搬移。这一代价换来真实leverage：一处实现负责creator/owns、连续writer generations、scope与child交接及失败收尾；普通caller只描述业务故事。deletion test：删掉该module，这些排序/登记规则必须回到多个callers；若caller仍需知道它们，实施不算验收。

不新增GLOSSARY/ADR：根AGENTS指定CONTEXT为单一领域语言，fixture scope是测试基础设施概念，不是新业务权威；本次常规可逆内部目录/生命周期细化不改变任何领域不变量。技能的词汇记录要求不应导致重复领域上下文或与本轮TMP-only冲突。

历史未知schema与无完整exact CID的Docker CREATE仍不能重建归属；新module不追溯宣称清除。不把有限SIGKILL/文件观察当断电或生产恢复证据。当前HTML未渲染验证、架构尚未实现、CI尚未核实完成均照实保留。

## Root 采用记录

2026-10-03，root已核实准确6783307的CI37158656088为completed/success（两库21.011s/race46.969s、27源hash）。上述代理选择时running为其历史观察，实际最新CI见[CI证据](ci-verification.md#两轴修复整合检查点)。采用该决定并发布单张[票10](issues/10-owned-fixture-lifetime.md)，前置核心及修复复核均完成；本票实施/审查/准确新CI仍待完成，whole02未退出。临时HTML已生成但当前机器无GUI，未声称渲染验证。
