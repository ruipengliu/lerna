# 04-05：过期正文 Job 不得遮挡后续合法工作

## 1. 决定与准确范围

基线 `1373112472761d32c22ed6d7603d806d8ef9a3c7`，候选 `1a4e1d2d704261a59491dfc6e0c5d26b80ec5107`；只读固定 objects，WT `/tmp/lerna-worktrees/content-snapshots-05`。本轮只作窄架构决定，未运行 native/Go/DB、未改源码；05 当前 final17 独占执行，不得并行启动本反例。此 finding 仍是实际源码可达性推导，**没有新 business red 执行证据**。

采用 **Content consumer 自有、在 LIMIT 前按实际职责选择候选**。原过期 Job、seal、holder、policy responsibility、原 deadline、字节与回执都保留，不以 Complete/ACK/续期或伪 erased 来排出队列。不改 frozen runtime Core.Scan/Job 语义、任何已发布 schema/SQL migration，不新增通用 scheduler/parking 状态/游标框架。

最小两项内部 port：普通 Content phase 候选扫描（真实 Service/Manager 两消费者）及 Lifecycle 本 scope 的可执行 body-cleanup 候选扫描。后者 deadline 仅作排队过滤；**锁后完整身份、当前受信资格、原 fresh clock/Claim/所有独立 ACK 仍是唯一执行门**。源修正必须等本反例真实 red、root FULL-read/adoption 后由原 fixer 实施，不能先把本报告当 green。

## 2. 实际源码因果

- `adapters/postgres/internal/pgstore/jobs.go:Scan`：owner 内 `state <> 'done' AND scan_at <= now AND lease_epoch < MaxInt64`，`ORDER BY scan_at,job_id LIMIT 64`。扫描不更新状态，ready/waiting 的 scan_at 沿原 due。
- `adapters/postgres/content/facts.go:Scan` 仅委派此 Core.Scan。
- `domain/content/lifecycle.go:Step` 约313先取 mixed Scan64，之后才筛 body_cleanup；约335在 fresh clock 发现 seal deadline 已过直接 continue，原 Job 仍原 due。
- `domain/content/service.go:Step` 约817同 mixed Scan64，再对已截断切片按 publish 排序；约831遇 body_cleanup 跳过。排序无法取得第65条。
- `domain/content/management.go:Manager.Step` 同样 mixed Scan64 之后才筛 policy_propagation；因此仅修两处可留下第三个实际消费者被同一积压遮挡。

真实合法流程能留下64个到期而未全部 holder ACK 的 cleanup Job。它们持续排在新 publish/live cleanup/policy Job 前；反复 Step 和 reopen 不会移动它们。过去失败、原未知 holder 未闭合是合法耐久事实，但不应阻塞另外一个原已授权工作身份。

已读独立 Spec 对此项的具体 trace，并直接核对上述源、Record/BodySeal/Repository/LifecycleRepository 及既有 PG holder事实与 Claim/Defer/Trigger。未借 Standards 可选重复建议扩成重构；不把本报告当 whole04 或七 AC review。

## 3. 具体最小接法

### 3.1 普通 phase 候选

在 Content 的 consumer-owned Repository 增加窄 `ScanContentPhase(ctx, tx, now, phase, limit) ([]runtime.Job, error)`（名称可按当地风格微调）。接受集合仅 `publish` / `policy_propagation`，limit仍1..64，其它phase/空phase拒绝。它不是 runtime 通用可选 predicate API。

PG Content adapter 使用现有 jobs 原表、owner/state/scan_at/lease_epoch 条件、原 `scan_at,job_id` 顺序，**WHERE phase 精确匹配必须在 LIMIT 前**。不需要 job row lock；下游仍先锁实际 Content record，再原 Claim/ValidateClaim。实现位置可独立小 `work.go`；完整 Rows.Err/Close 原因不得吞。Core.Scan及其其它 consumers不变，不改旧 host migration。

Service.Step：有限一次 publish页（<=64），按原资格尝试最多一个实际工作；无可选 publication 才有限一次 policy页（<=64），仍最多执行一个原 work。这是明确上界最多两页/128候选，不循环至全库，不放宽 caller/lease/工作预算。原 late failure、publication gate、policy advance 和错误传播保留。取消原 mixed页内排序已无必要；不把 priority 写成无限 drain publication。

Manager.Step：仅一次 policy_propagation页（<=64），继续原 advanceJob。不能按 source record creator.Subject 过滤政策工作：实际一个 source 的多 subject/purpose 政策责任由原 Management 逻辑分类；管理消费不能借新扫描 port扩大主体授权或删除非本保存主体的正文。

### 3.2 Lifecycle 本 scope 可执行候选

在 LifecycleRepository 增加窄 `ScanBodyCleanup(ctx, tx, now, trustedSubject, primaryHolderID, limit) ([]runtime.Job, error)`。实际唯一 consumer 为 Lifecycle.Step，Subject/primaryHolderID来自其已验证固定 config，不来自公开payload。不需要为“两个 adapters”虚构实现。

PG 同 owner/jobs机械资格 + `phase='body_cleanup'`，关联现有 content_versions 的原 body，仅预选本完整保存 Subject（含 delegation chain）与本 PrimaryHolderID、原 seal 尚可能在 deadline 内的候选，**这些条件也在 LIMIT 前**。使用原 `scan_at,job_id` 顺序和<=64上限；无 OFFSET全表遍历、无全namespace内存筛选、无独立迁移/index先验。SQL可能检查很多历史行，不承诺常数DB代价，但结果/单次尝试/原query deadline有限；不能把返回64个相同过期候选再Go-filter称为完成修复。

具体范围与错误边界：

- 合法同owner不同保存主体或不同 PrimaryHolderID 的 cleanup 是另一消费者的工作，扫描可以排除；不能像现源码一样先选中它再让本主体 ErrScope 阻断自己的后续工作。匹配需完整 Subject，不仅SubjectID/tenant。所有purpose仍由被选record原Purpose处理，不擅造额外purpose通配授权。
- 配置的 Objects.Binding 与原PrimaryHolderBinding **不能为了避错而自动兼容**；被选后的旧 ErrHolderBinding/current identity 资格仍保留。该 port 不采纳 legacy unknown root，也不把错误 root 当本消费者可清理对象。
- 缺失 Content record、无 seal、非法JSON/字段/期限形状不能被随意COALESCE成“已过期所以没有工作”。只排**已能准确识别的合法其他scope/确定过期**候选；同scope不一致记录应继续进入原领域校验或明确返回错误，而非正常空页。LEFT JOIN/显式异常分支比无条件inner join静默遗失责任更合适。SQL解析错误保留实际错误，不改成not_found。
- DB条件仅是 selection hint，不是授权/删除判断。仍按原顺序 LockObject→完整 Ref/Record/Seal/保存主体/PrimaryHolder 资格→manager.current→新Now→原seal deadline→全部holder页→新Now→原Claim；不由scan先锁Job而逆转Version→Job顺序。
- BodySeal.Deadline是Go精确时间，SQL timestamptz只有微秒：如直接转换作候选过滤，必须保守处理精度，不能把SQL四舍五入当最终资格。默认对SQLdeadline向后留1微秒选取容差，最终仍由原Go full body时间和锁后fresh clock精确拒绝；这只可能多看极短暂已过期候选，不延长任何可执行窗口。禁止字符串非规范字典序比较、parse失败当zero或改变旧deadline存储。初始red等待deadline+20ms可明确超过该保守容差。

候选是在原now观察下选择；随后锁等待跨期限仍照原 continue / final gate 失败，不得续期。下次调用新now自然滤掉已到期行；它不要求把本轮每个候选执行完才返回。全页被竞争耗尽时可以worked=false，不能据空候选推断全owner cleanup已完成。

### 3.3 不采用其它路线

- 不用 Complete 把未ACK清理伪装为 done；也不先发一个晚于原seal期限的新Claim只为了Defer/Complete。
- 不改expired seal期限、due/attempts/责任state去“停车”；当前冻结Job状态没有所需真实独立residual-execution表达，添加它比本窄候选口复杂。
- 不用内存cursor跨调用回绕：reopen会丢进度；不引入耐久cursor与迁移只为绕过已知不属执行集的行。
- 仅phase过滤不能解除同phase64expired body的阻塞；仅expired过滤不能解除64live body对Service/Manager的mixed-page阻塞。两项候选限定是一项清楚的职责修复。
- 不提前把所有phase重构成registry/strategy。三种phase已有真实消费者，未知phase不得当publish运行；原通用runtime观察仍可看到未消费Job，不承诺本Content consumer扫描所有外部任意phase。

## 4. 首 business red 的有限真实准备

本次新测试开始前固定所有时间/次数；不按失败结果临时抬限。推荐单case caller90s、原native/外层120s，使用现Lifecycle WorkBudget/TrustedUntil原上界；准备期全部政策/保留期限在首次安装时显式充足且不需要事后续期。不能为本测试改生产默认lease/配额。若准备本身超时，先报告准备失败，不把它当目标调度red，也不无限加预算。

1. **先出版全部64个小正文原版本**：每个真实Put/Step/Command/Get与独立holder字节可确认，均为同owner/同保存主体/同primaryholder、不同准确ref；顺序出版，避免累计preparing超过既有配额。选一个已有ref的V2作为后续新版本，真实hash/字节不同。所有ref在Seal前均真实正常，不能私表INSERT伪造Job。
2. 完成全部出版后，用真实可信Seal逐个登记原短finite deadline（例如本批Seal开始时一次固定`now+15s`；该值是此新测试的初始预算，不能中途续期）。64个Seal均须在原期限内成功，记录原SealID/Ref/deadline/holders/receipt历史。该阶段不运行Lifecycle.Step；等所有原期限到达+20ms，若中途Seal本身expired则本case未形成目标prestate，报告准备失败。
3. **低于满页的独立正常对照**：最好另一个自有fresh scope先做1个同样expired cleanup，再新Put/V2，有限Step和独立字节确认可达，随后新live cleanup全ACK。它用于证明不是新publication/holder配置本身坏；不能拿该freshscope替代64积压红点。为免一case等待预算叠加，可单独明确有限test，两者各原caller和独立资源，不重置同业务deadline。
4. 64expired后真实Close/reopen当前所有owner/store，保留原scope，不升级/迁移/重Put旧body。保存原64条公开Lifecycle.Observe（有限holder页）的不完成事实和原字节观察；有当前metadata许可则Get sealed/不可用状态按原合同，不能以Get拒绝代替原字节存在。
5. 接受V2真实新Command与当前原政策，有限Service.Step（建议至多4次；不轮询直到timeout）。原候选预计始终preparing而非published，从真实Command progress+independentV2字节缺失证明目标red；不得查私有jobs表当业务oracle。第65个Job的实际来源由上述真实sealed64+后续Put时序保证，不通过修改due/jobID排序。
6. 后续live cleanup可预先在seal前出版一个额外控制ref，expired64形成后才对它生成新的合法live Seal（同原固定live WorkBudget、仍有限）。原源码Lifecycle.Step有限调用应不能到达它。若首publication断言已失败，此尾部没有运行就标未执行；修正后必须执行两个出口，或拆成第二个明确red，不把静态推导当第二实测。

不把64旧expired原cleanup改成cleaned来做测试清场。fixture最终拥有者按实际Close/FD/Wait/独立精确scope规则处理已确认范围，未确认原资源继续unknown保留；测试业务层不假ACK。这不授权触碰此前02/05旧unknown roots或原Z进程scope。

## 5. 修正资格与最小额外边界

实施后同原输入/时间/次数的正常与race应完成：新V2published/真实bytes、后续live seal独立全holderACK；重开后仍可继续新的合法工作。每个旧64责任依旧原seal/ref/deadline/holder history、cleanup_complete=false/未ACK、原字节未被新consumer删除；原Command固定receipt/publication历史不变，旧封闭ref不得重放复活。

最少补两个实际边界，不造泛矩阵：

- 让实际policy_propagation在64expired body之后到期/登记，Manager独立正常推进并保原watermark/deadline/原subjectpurpose分类，证明第三真实消费者不是还卡mixed页；Service优先publish后policy的既有正常对照保留。
- 同owner另一完整保存Subject（可用现有真实delegation case）有先到的合法cleanup，本Lifecycle只推进自己范围，另一主体的原正文/责任不变；匹配主体的Lifecycle正常对照可推进它。不是把非法受信主体授权进本配置。无需凑64个跨主体案例，首64expired已经证明LIMIT位置，少量跨主体对照证明限定正确。

保留原锁等待跨deadline和holder/原root-binding错误反例；新预选绝不能使错误变成功或输出跨主体metadata。窄port机械SQL/解析错误测试仅证明failclosed/错误保留，不能声称真实业务故障发生。所有当前Rows.Close原因与实际decorator转发需随新增port核对，不借机会处理独立optional replay duplication。

只有真实first red、源码修正、同原finite case green/race、受影响现suite及固定新pin审查后才有该finding的闭合资格。当前final17/此前CI不能覆盖尚未发生的新source delta。Whole04仍15/41、profile OFF、七AC未接受；本决定不发布、不提前接受票05或整片。
