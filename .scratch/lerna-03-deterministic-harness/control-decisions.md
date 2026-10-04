**正式采用：2026-10-04，root按用户授权全文读并采用票03控制证明与单调停止。01九AC已resolved，最终源696ac49846105a16f33e5de86dc621a3858651b2、交付d806a92、整合5307702，已push检查点c9de1ba；下文候选/pending状态保留为历史，实际端口以[ticket01 API](ticket-01-api-handoff.md)为准。准确CI37174778053已按head c9de1baff7f481c2a9e4dde3c7873af43151819b核验success，见[CI记录](ci-verification.md)；本记录采用技术接法，不表示该后票已经实现或整片退出。**

# 03 票03：fixture控制签发、单调停止与固定终态

2026-10-04；仅条件技术决定。源码全文核对固定 `696ac49846105a16f33e5de86dc621a3858651b2`（在 `/tmp/lerna-worktrees/deterministic-harness-01` 用 git show）；相关产品API为 `f96f85987818a5e8dc5c0c104e5731ddf75687df`。已读 `/tmp/lerna-03-ticket-01-api-handoff.md`、root03 decisions§6/其完整前提、ticket03与spec、实际service/query/worker/ports、PG Decision store/work/迁移、fixture Source/Seed/Authority/迁移及1.1闭合值。01刚获root九项AC resolve授权，文档merge/push/最终CI尚未发生；**本稿不是01最终发布pin，不宣称03票03已claim、实施或退出。** 正式接手须复核01最终handoff及02并行改动。

## 1. 首选最小方案

保留 `SaveDecision` 对 completed/failed/cancelled 的全冻结。新增Decision owner的独立停止控制记录，原Decision锁下与cancel回执、必要Job关闭同Tx提交；更高控制更新该记录，不重写终态、原Proposal、usage或旧receipt。fixture Source另提供真实耐久的控制访问scope与proof签发/核验，独立于执行Permission/完整Snapshot。Command元数据用闭合decide/cancel分支兼容旧decide JSON，不把cancel cast成decide。公开get把“当前已采用控制”放在冻结Decision事实之外。

这是既有03取消语义的具体接法，不新增Task/Grant/Executor TaskGate或领域ADR，不依赖切片06，也不把本票特殊恢复移到本片06进程票。控制记录只表示本Decision owner已采纳的stop，不是一份Task当前状态副本。

## 2. 源码确认的约束及不能采用的捷径

- PG `SaveDecision` 的UPDATE条件明确排除三个终态。放宽为“terminal也可按更高revision更新”会允许旧路径改成果，不能作为存更高control_revision的捷径。
- `Record.Public()` 已支持 cancelled 的 `Input=nil + CloseTaskRef + InputDigest + ControlBasis`，可以真实表示先cancel后decide，无需捏造Snapshot或decide payload。现有决策表允许 cancelled deadline NULL。
- `CommandRecord.Request` 目前是非指针 `DecisionDecideRequest`；SaveCommand将整个结构放metadata，GetCommand实际只取receipt。不能塞零值/假decide请求让cancel“能入库”，也不能用新enum强转旧1.0类型。
- fixture `Seed` 必须保存完整Snapshot/manifest/lock；`Authorize(get)` 要求fixture_decisions中已有grant，command.get则查owner级fixture_permissions。直接复用它无法真实支持不先Seed的控制墓碑访问。控制权限不能依赖原执行Permission仍未到期。
- `lockedWork`/Start/Claim已有pool→Decision→当前时间/Claim检查；`StopRevision`可封原Job并清leased资格，`Complete`只允许completed/failed。cancel不应假扮worker调用Complete，也不应新建ordinary Job等待quota。
- Source `within`与Decision `Within`是不同owner短事务；不能在Decision Tx内调用Source SQL或等待proof网络/读取。Source可信issuer必须确为准确task_ref.owner，不能仅检查proof_ref格式或payload自称issuer。

## 3. fixture访问scope与proof：两个时钟，两种用途

推荐在组件消费侧声明一个小的可注入 `ControlAuthority`，职责分两步：①按当前受信完整SubjectBinding、Decision owner/准确Decision或明确command读scope、闭合目的(cancel/get/command.get)取得有限访问观察；②仅为**新cancel命令**核验完整ControlBasis和其耐久proof，返回不可变绑定及必要Snapshot控制下界。具体函数名可按实际消费者命名，不能用含所有未来能力的通用授权框架。

访问观察只包含真实需要的主体、scope、有效期限；不套 `Permission` 填假的ComponentRef/UseRefs/收费配置。Config显式注入此端口；保留01既有无cancel装配的明确范围，缺端口时不能广告完整decision_engine或绕过验证。不得靠不受约束type assertion猜测某Source“应该兼任”授权。

fixture管理端新增有限 `SeedControlAccess` / `IssueControl`（名称建议），仅测试装配可调用，不是模型可调用业务API。前者登记当前主体在准确fixture task/Decision上的cancel/get资格及明确的command.get读范围和访问期限，**无需完整Snapshot、Use、manifest或执行授权**；后者在Source自己的短Tx真实保存proof后才返回准确ContentRef。proof的可信来源是受信端口绑定的Source owner和它自己的耐久记录，不需要伪造生产签名/Grant系统。

proof闭合正文绑定：fixture类型/版本、完整主体、准确issuer/task/Decision、decision_input_digest、control_revision、valid_until，以及停止语义。issuer必须等于Source实际owner及task_ref的准确tenant/owner；Decision所属tenant、受众owner也要一致。proof_ref固定Source owner/version/hash/length，读取精确字节核对，异owner同名ref、缺行、错hash/长度、错主体/委托链/对象/摘要/期限均不授权。端口入参和返回观察深拷贝，保留01已修的跨Authority可变别名边界。

访问scope期限与proof首次采用期限分开：旧proof已过期、原accept_before已过期，**当前仍获准的原键同摘要重传**仍取原固定receipt；不得为重放续签proof、延长命令或重新应用控制。当前访问资格失效则拒绝披露/调用，即使原命令曾成功。新命令必须同时满足当前访问、accept_before和proof.valid_until；执行deadline已过或执行Use失效不能单独阻止合法停止和查询。

最小调用顺序是：严格解码/冻结请求→当前访问观察→短Tx锁原Command、锁后取now复核访问并处理原key→若确为新key则退出无修改Tx→Tx外核验proof/必要Snapshot下界→最终短Tx重新锁Command并再次处理并发原key，再按pool/Decision锁序应用。不能为省一次查询把proof I/O放入Tx，或把“先验proof有效”放在原key重放前。最终Tx锁后再次复核当前访问、首次接纳与proof期限；若准备发现对象从无到有，按下节有限补读，而非复用缺失下界。

Source储存建议：proof可复用已存在 `fixture_objects` 的准确不可变对象机制，以独立kind=`control_proof`隔离，不能把它加入执行材料白名单。为不依赖Snapshot的当前访问scope与owner级command读索引，增加Source owner的一张明确 `fixture_control_access` 表及追加迁移；不要往现有closed grant JSON硬塞新字段、修改旧Seed manifest或已发布0001。scope用途/期限和更新若需要由本owner受信管理入口明确校验，不能提供任意SQL控制器。正常Seed的原查询权限继续有效，新控制scope不自动授予decide/start/material/publish。

当前Get/GetCommand需走可覆盖“原正常fixture读许可或明确控制读scope”的真实访问入口。不得在get墓碑时回填假Permission；command.get仍沿受信owner目录和现有完整1.1回执投影。owner级command读范围只能由fixture管理端明确授予，不能凭任意proof_ref扩大；原1.0 LegacyReader仍仅无损投影，不能表示的新事实沿原unavailable，不改1.0。

## 4. Snapshot控制下界与取消前准备

新停止控制必须高于准确Snapshot.control_revision及已采用停止控制（同已采用控制的幂等例外见下节）。不能从Decision对象revision、Claim epoch、字符串字典序或假默认0推导Snapshot控制修订；使用当前Revision闭合范围及精确整数比较。

正常Decision已有Input时，取消准备在Decision Tx外用当前控制权限从Source核验该准确Snapshot/manifest的控制修订及绑定。此读取目的为停止核验，不以原执行Permission/原deadline仍有效为条件，不读任意正文或恢复执行权。Source仍校验准确原Snapshot真实存在和归属；缺失/不可核实时返回unavailable，不造控制下界。completed的Proposal也不能成为其他状态不存在Snapshot的替代真相。

取消前Decision尚不存在时，只需要真实控制scope和绑定proof；不要求先Seed或创建Snapshot。锁内若仍不存在，可直接建关闭墓碑；其ControlBasis是真实已采用的停止依据，不声称观察过尚无的Snapshot。锁内若发现并发decide已创建记录而准备阶段未核验其Snapshot，退出该未修改Tx，在有限调用期限内为准确不可变Input补核验再重入。只要求InputDigest/准确Input仍同一份，不因worker普通revision推进无限重试。输入不同直接decision_mismatch。

已有终态也按真实绑定核验合法新控制；原cancelled-before-decide有nil Input时不去创建或读假的Snapshot。后来迟到的decide不能因为它带更高Snapshot控制修订就复活该Decision：原准确关闭身份仍永久关闭，新业务需要新Decision身份。

## 5. Decision owner停止表、锁顺序与判定矩阵

新增 `DecisionStop` 持久记录，以准确tenant/owner/decision_id唯一：固定task_ref与input_digest、最高已采用control_revision、准确ControlBasis/停止绑定摘要、采用时的受信主体及原命令引用。保存stop的主体与原Decision输入主体分别记录；不拿新控制调用主体悄改原输入摘要。control只收紧，不存在allowed=true、自动到期删除或resume。

建议复用原Decision advisory lock串行“没有记录”的取消/decide竞争；无需再创一套锁命名空间。凡与工作/Job同写者保持现有顺序：**Command锁（若有）→pool锁→Decision锁→stop/Job行**。纯读取/get为取得同一观察可以取Decision锁，不反向再取pool。Source proof准备全部在这些锁外；最后在锁后取数据库当前时间核验当前访问和首次采用期限，避免等待期间失效。

| 当前事实 | 新命令的决定与同Tx变化 |
| --- | --- |
| 已有原command，同完整摘要 | 当前访问仍有效后返回原receipt；不重验旧proof的首次采用期限，不比较当前更高控制，不更新任何事实 |
| 已有原command，摘要不同（包括换方法） | idempotency_conflict；不改原记录。当前鉴权先行，不能用此口探测他人命令 |
| 新command，proof内部绑定/当前主体不合法 | forbidden或真实dependency unavailable；不因合法ref形状而写墓碑 |
| proof合法，但既有Decision/stop的task或input_digest不同 | 固定rejected/decision_mismatch；不重绑原对象 |
| 低于最高已采用control_revision，或无stop且不高于已核实Snapshot下界 | 固定rejected/revision_changed；同Snapshot修订不是一个新停止控制 |
| 与已采用control_revision相同且停止绑定相同 | 为新command保存applied原对象回执，不重复关闭、不改已有stop/终态 |
| 同control_revision但停止绑定不同 | 固定rejected/decision_mismatch；不得按较新的proof/期限偷偷替换同修订含义 |
| 合法更高控制，Decision不存在 | stop + cancelled Record（Input=nil，CloseTaskRef真实）+ applied receipt同Tx；没有Job就不建Job |
| 合法更高控制，Decision活动 | stop + 一次转cancelled + applied receipt + 原Job停止/fence同Tx；保留准确Input、累计计量/prepared等事实，public不输出可采纳Proposal |
| 合法更高控制，Decision已completed/failed/cancelled | 仅更新stop并保存该新command的applied receipt；Decision完整旧事实、revision、Proposal、usage和所有旧receipt不变，无新执行Job |

推荐“停止绑定”包含准确Decision/task/input_digest、issuer及完整ControlBasis（含proof_ref/valid_until）；reason为本次命令审计文字，不授予不同停止范围，可记录于新命令，不用它扩权。proof重签/换期限不是同一ControlBasis，应使用更高控制修订；不能借同修订幂等续期。若请求在多个维度同时错误，先原key，再已有对象绑定，再控制序，测试固定此优先级，不依SQL碰巧先报哪个错误。

新decide在当前鉴权及原Command重放之后、任何PoolQueue/Trigger和新binding费用拒绝之前，核对已存在Decision/stop的固定身份：同digest且cancelled→decision_cancelled；异digest/task→decision_mismatch；均不新增工作。原已accepted decide的**原key**重放仍是原accepted，不被后来cancel改写。新key对completed/failed同输入沿01固定accepted别名语义，不借更高stop改成新的业务决定；只证明原Decision已接纳，不代表可以再执行。

上述对象重传/墓碑优先不能绕过当前认证、语法版本、首次新command接纳期限或准确绑定检查。当前service在部分fresh Component/RuleVersion拒绝之前尚未LockDecision，实施时显式调整这个次序并保留01退役binding的新身份unsupported行为；不能只在Cancel加一段逻辑而漏迟到Decide。

## 6. 固定Decision与当前控制的公开观察

推荐最小增加 `DecisionGetResponseFound.current_control`（有真实stop才出现），使用闭合值提供准确task/input_digest/ControlBasis；必要控制说明可同时返回。该字段是当前本owner停止事实，嵌套 `decision` 仍是原固定终态。不要覆盖cancelled旧ControlBasis或completed原Proposal的control_revision来假装原成果在新控制下生成。

cancel applied的object_ref仍为原Decision，receipt.revision使用该操作完成时真实Decision revision；终态上多次停止可返回相同Decision revision，不将control_revision冒充对象revision。最新控制从get.current_control读，原cancel receipt只证明当时方法成功点。get在同一锁定观察中取Decision与stop，避免读到不存在的组合。

1.1 decision profile仍未完整发布，按03既有规则在本票同步闭合Schema、Go/TS生成类型、同版正反例/摘要、成功点和最小get字段；这是明确演进中的1.1工作，不偷偷扩张冻结1.0或宣称旧1.1 decoder能接新字段。若正式采用时root已把该准确Schema另行对外冻结，则先分配新准确版本，不静默破坏；当前pin不构成这项已发布事实。完整profile仍由root全片退出广告。

停止scope明确是“本Decision新工作已关闭”，不是全部Source发布/字节已物理删除。03没有生产cleanup系统，未引用的fixture发布由真实拥有者清理；get/current_control文档须明示此边界，不能输出cleanup_complete=true。已有completed公开成果不被抹除，活动取消后prepared/已发布未采纳内容不作为completed Proposal返回。运行中计量未知保持MeasurementsComplete=false与已记录下界；不能终态后把晚worker计量写回原Record来伪完整。

## 7. 命令元数据兼容与迁移

推荐内部metadata v2：明确字符串 `metadata_version="2"`、闭合method（仅decide/cancel），且恰有一个对应typed请求。保留旧JSON `request` 作为旧decide字段，读取旧01无version且只有合法decide request的记录时识别为legacy-decide；新写可用指针字段/命名分支避免序列化零值假请求。未知method/version、两个分支并存、缺分支、错误typed请求/command_ref/subject/digest绑定均拒绝为不可读，不能宽松降级到decide。

Reader同时无损接受旧01metadata和新closed两分支；不批量重写旧回执/metadata，不改已记录digest。SaveCommand验证准确typed请求、规范CommandDigest、ref/完整Subject及receipt关系；GetCommand只返回原receipt和既有准确progress，不能按当前control重造旧回执。当前 `json.Unmarshal` 不是闭合校验，新增兼容decoder应先有限严格JSON/重复键检查、显式形状分派，再typed验证。

metadata本来为bytea，本次双读本身不需SQL迁移。新增Decision stop表才需要Decision owner追加迁移，当前下一号预计0003；Source新增control_access表需要其owner的0002，proof复用既有objects不另造表。两个owner编号彼此独立；与票02协调实际编号，02 prepared v1/v2 JSON双读没有实际DDL需求就不占空迁移。不得改Decision0001/0002、Source0001、Host0001–0005或归档writer。

迁移只建立新存储与约束，不批量给旧终态造“已经取消”记录，不从Proposal猜取消proof，也不填收费/计量。旧01无stop表示本模块没有已采用cancel事实，不表示Snapshot控制修订为0；新cancel按真实Source核对。部署采用停止旧worker并确认退出后新writer接管原owner；旧binary不认识stop和新metadata，**不宣称混合滚动写入或自动回退安全**。同一新writer保留/2 prepared准确bytes/key/digest与票02实际兼容分支。

## 8. 旧worker、pool零额度与有限收尾

Cancel直接在自己有限短Tx内封活动Record并停止准确原decide Job，不经过普通领取、队列配额或新的rule fee；pool锁用于与Claim/Finish相同顺序串行，不要求ordinary槽可用。需要最小 `Find/LockDecisionJob` 端口取得准确Job与work_revision，再调用现有StopRevision；别捏造job ID、用任意扫描第一页或新建Job满足关闭。StopRevision当前不校验RowsAffected，新增调用必须核对真实停止结果/已done事实，不把未命中的UPDATE当fence已完成。

同Tx失败或提交未知时，stop、Record终态、Job关闭和receipt不能分裂；提交未知只查询原Command。Start/Claim/savePrepared/Finish/deferPrepared在原锁内检查已采用stop与当前Record/Claim资格，晚worker不能落completed或再领收费epoch。无stop的旧01工作仍沿原路径，不能等待未来TaskGate才执行。

发布在Tx外，不能宣称本地cancel和Source Publish原子：在每次准备新发布前核对本owner当前资格；已离开检查点或正在Source调用的旧worker仍可能在原有限I/O窗口内写下未采纳fixture字节。其最后Finish必须被拒，真实Source observer可证明残留。停止新的本地工作与确认旧物理I/O退出分开；没有真实退出/超时后的独立依据，不删除活跃或unknown测试scope。

Maintain继续独立于ordinary quota/饱和，对全注册成员按有限页执行原deadline/legacy关闭，不改成普通Job才能维护。区分执行deadline、cancel的accept_before、proof首次采用valid_until、当前访问scope期限和本次context；stop已提交不因上述期限后来过去而自动失效。控制/到期关闭失败真实重试并保留责任，无无限循环/睡眠，scope关闭沿01精确所有权与sticky Close未知规则。

## 9. 可实施次序、真实验收与风险

1. 复核01最终resolved/source/docs/CI handoff及并行票02ports/metadata改动，锁定变更基线；记录迁移owner/编号。先冻结本票控制scope、proof与公开current_control成功点，补同版strict夹具，不先广告profile。
2. Source独立control_access/proof签发与核验、Get/GetCommand当前访问接线；正常已有Seed兼容，不先伪造Snapshot。校验principal/refs/别名隔离、两种期限及真实重开。
3. Command closed metadata双读、Decision stop表/锁/查询与Cancel Tx；同时改Decide原key/墓碑优先，Job准确停止及worker门禁。先跑允许正常候选与先cancel后decide完整公开链，再扩并发。
4. 真实PG同步点验证Finish先提交→cancel保留completed，以及cancel先提交→旧Finish失败两种顺序；同时测试prepared提交/Source Publish窗口、先cancel墓碑重开、乱序高低控制、同修订异绑定、原回执丢失后过期重放与当前权限撤回/到期拒绝。每个反例都有合法对照。
5. 真实满pool、ordinary quota0、已leased及多成员超过单页的deadline/取消关闭，观察公开get/command.get/Pool状态及Source准确发布读回；费用/steps/bytes边界保持原01计量和正常样本。不能只测Control helper或手工置库来抵公开取消。
6. 用**真实最终01 writer**先创建accepted、running未prepared、prepared待发布/已发布未Finish、completed、failed与相应固定命令；停旧writer/Wait后应用新迁移并由新实现读取、合法取消/查询/恢复。旧01本来没有cancel，不伪造其旧cancelled数据；本票新writer再生成cancelled/高控制/两种metadata，真实重开复验。原970fd90历史升级及旧1.0隔离按受影响范围回归，不改归档来造兼容证据。

主要风险是：认证与proof过期错误合并导致原键不可恢复；终态更新放宽；缺Snapshot控制下界；stop/Job不同事务；跨owner发布窗口伪称即时停止；TS/Go新get不同版；新metadata被旧writer误读；未知测量被取消“清零”。这些都有上述具体反例，不靠后续06兜底。最终pin上完成要求的真实PG/迁移/受影响race、同版Go/TS与规范/架构审查，再由root裁决本票；本文未运行DB、build、测试、服务或读取凭据。
