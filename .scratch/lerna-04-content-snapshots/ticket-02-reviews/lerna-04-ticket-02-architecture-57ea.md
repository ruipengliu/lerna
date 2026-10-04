# 04 票02：固定57ea架构与原传播期限裁决

2026-10-04。固定source `57ea60c628f82140f5700503107d3e2fe4a86dec`，base `f05b2f1068958ba6b63e6c1dc58d6cd1684446cc`；实际diff为5 commits/39 paths。重点读domain/content管理/闭包/ports/service、PG管理/政策/迁移、真实管理/闭包测试及有限旧writer恢复设施。全文复核票02七AC及已采用handoff/oracle；使用improve-codebase-architecture、codebase-design/DEEPENING与HTML scaffold，领域词汇沿根CONTEXT，根AGENTS覆盖技能的假想第二adapter/新GLOSSARY建议。本任务禁止新agent，未委派、未读两轴finding，未运行build/test/DB。

**结论：一项Strong必要修正——共享advanceJob的原change期限资格；不需要新增架构层、registry或广泛重构。** 其余闭包/管理模块已有实际depth，保留现有seam。这里是固定source判断，不是七AC通过或whole04退出；原race未知隔离另见 `lerna-04-ticket-02-native-race-boundary-decision.md`，不被本报告清零。

## A. Strong：期限属于共享传播module的最终资格

**准确位置：** `domain/content/management.go:514–614`。532 PendingChanges可能阻塞；544以此前524的now核Deadline；561 Descendants、566 LockVersion、573 register之后仍用旧now。577–584可记complete，甚至转scheduled并改用原未来ExpiryDeadline。600虽重采Now，604只ValidateClaim，不再核每个本轮change的原Deadline。因此Claim仍活、管理TrustedUntil仍有效时，某一页锁等待跨Deadline仍可能提交完成。

现 `content_management_test.go:469` 测的是管理TrustedUntil跨页等待后rollback；`:524` 测的是开始前预算已过期并保留known holder。两者均不证明“开始时预算有效、锁后跨原Deadline”的行为。此缺口来自实际source，不借两轴报告，也未实际注入red。

### 采用的最小默认

1. 在共享advanceJob内部保留本次每个change的**原Key、Deadline、Cursor、Watermark、Due/原phase**；不得从后来scheduled分支覆盖后的值反推本轮预算。过去已提交页的cursor不得倒退；本轮未确认整页仍保留本轮页起始cursor。
2. PendingChanges等待后、每个change开始、Descendants返回后、每个LockVersion返回后与开始下个后代前重采DB Now。到期立即停止新增后代遍历/登记，不继续处理剩余页；原source已知holder责任的有限登记/收尾仍可按现544–555语义保留，它不声称扫描完成。
3. 最后SaveChange/NextPolicyDue等可能阻塞，故还须在本owner Tx的**最终完成资格点**（freshNow及Claim校验邻近）检查本轮拟推进/complete/转scheduled的每个change原Deadline。到期则保存 `residual/original_deadline_expired`、原Deadline/Watermark/页起始Cursor，不选择未来ExpiryDeadline、不改成now+budget。已在本轮及时登记的责任是事实，可保留；保留旧cursor是保守的未确认整页，不伪称这些登记从未发生。
4. 当前事务中已经访问的所有change若受后续锁等待影响，也须在最终资格点复核，而不是只看最后一个；仅当前页起始检测或单独缩Claim lease都不够。最终资格检查之后持有的原change行锁下保存结果，定义检查时刻为本次决议点，不声称物理COMMIT恰在Deadline前。
5. Claim已过期、ctx/SQL错误或Manager当前管理权限失效仍rollback；不能为提交residual而绕过这些门禁。重开/下一次原工作只作到期责任收尾并保留原预算，不再遍历原过期范围。无需savepoint、独立重试框架或改runtime语义。
6. 完成/延期Job前按修改后的changes重新算NextPolicyDue，不能沿旧scheduled nextDue继续唤醒一个实际residual。没有可执行pending/scheduled时可Complete本Job；这只表示无本次可执行工作，公开PolicyChange仍residual且holder未确认，不得广告传播成功/清理完成。

这允许已获得事实的单调收紧/责任登记留存，不允许预算耗尽后假complete。若solefixer采用更保守的“整页rollback，再短Tx登记原source残留”也可以，但不是默认要求；额外Tx必须保持原身份、当前权限/Claim和原cursor，不能刷新预算。

### 必要公开red/正常对照

- 用真实Content put/get建立源与至少两个后代，trusted Manager安装同一原change，设置小而有限WorkBudget；TrustedUntil、Claim lease、ctx均足够长。事前ObserveChange冻结原Deadline/Watermark/Cursor，body/save-only等既有正常对照保留。
- 用现 `contentfixture.HoldVersion` 锁住本页实际后代；启动真实Manager.Step，机械pg_blocking_pids只证明实际等待（不读业务私表做oracle）。待DB实际时间跨原Deadline、但尚未触发LockTimeout/Claim截止，释放锁；所有配置仍有限且无需扩大原业务预算。若默认锁超时更早，选相容的小WorkBudget和有限等待，而非放大产品上限。
- 公开ObserveChange必须见原deadline不变、state residual/reason正确、未确认页cursor不前移、全部既有责任仍可查；重开再观察相同。不得从nil error/Step true推断complete；异常result按真实原因保留。对照在Deadline前释放同类锁，整页正常完成或按原设计进入原定expiry阶段。
- 同一共享module有**两个实际消费者**：Manager.Step(:502)与Content Service.Step→PG forwarding→AdvancePolicyJob(:622)。两入口都要有正常/跨deadline证据；特别Service入口没有Manager.TrustedUntil最终门禁，不能只借管理权限过期测试使它看似安全。
- 至少一个多change场景/有限最终写入等待对照，证明早先处理的change不会因后续耗时而漏最终原Deadline复核。真实测试由sole owner执行；本报告没有red/green结果。

**架构判断：** private推进module隐藏同Tx时间/Claim/原游标资格，interface仍Manager/Content公开推进与观察。deletion test：拆掉共享推进会把规则复制到两个消费者，locality降低；只修内部资格同时获得两路径leverage。PG adapter只给原Clock/锁/事实，不引入一个只有假第二adapter的时间策略seam。Before：旧now→锁/登记→complete→只验Claim。After：原change边界→锁后freshNow→最终逐change资格→progress或residual。

## B. KEEP：完整来源闭包已是有depth的module

**Files：** `domain/content/closure.go:12–101`，`service.go:211/423/798`，`management.go:158`。

实际一个私有registeredClosure供五类使用：Put（含新关联）、Get两gate、publicationPolicy（启动/最终）、AuthorizeUse、legacy RebuildSources。它集中exact VersionIdentity/fullRef去重、活动路径环检查、64唯一祖先上限、同owner范围、当前动作检查、来源准确published与规范排序。原Record.Sources/tuple未被展开集合覆写。后续调用者的再资格循环负责各自截止交集/响应语义，不全是可删重复。

**候选强度：Worth exploring，但现在KEEP。** 若未来真实修改证明重复时钟/全Ref/截止交集规则漂移，可把“闭包+资格观察”私有返回值集中；当前不为了缩行数造新公共policy接口。deletion test：删registeredClosure会将遍历/去重/环限制摊回五类调用者，明显失去locality与leverage。interface是公开Put/Get/AuthorizeUse/管理观察，现closure测试经这些入口且真Content，不要求为纯DFS复制一份假storage adapter。Before=五入口→共享闭包+各自决议；After=保持，只有实际新漂移才深化私有结果。

## C. KEEP：管理module/PG seam职责集中，forwarding不扩框架

**Files：** `domain/content/management.go:56–77/234–403/618–624`、`domain/content/ports.go:80–82`、`adapters/postgres/content/management.go:335–339`、`policy.go:29`、`service.go:655`。

InstallPolicy、兼容malformed fixture、原版本重放、responsibility分类、来源回填/维护全部进入同一个domain实现。PG LockPolicy/SavePolicy/索引/分页/责任保存是具体storage adapter；原InstallFixturePolicy已只转发领域步骤，没保留第二套只改policy行的业务路径。Manager.Step和Service.Step是真实两个调用入口，**不是两个数据库adapter**。现实际主adapter为PG；测试decorators/机械等待不是独立生产实现。

**候选强度：Worth exploring，KEEP当前两条narrow forwarding。** Service通过Repository.ScheduleRetention/AdvancePolicyJob回调同domain函数的路径需要读者跳一次文件，存在导航摩擦；但不携带第二份业务算法，且保留已有Repository decorators的必需行为。直接删除转发而把业务放PG会违反owner/locality；为消除两条转发把所有ManagementRepository方法推给每个Service caller，会扩大interface而无当前leverage。仅将路径在README说明足够，不以假第二adapter或runtime handler registry替换。Before/After均为两个真实入口→同一管理module→PG事实；当前必要修正A就在这个module闭合。

## D. KEEP：升级/故障设施保留有意义的测试seam

**Files：** `0002_source_policies.sql`、PG migrate/management、`conformance/internal/contentfixture/{legacy,policy_lock,world}.go`、管理/legacy公开测试。

0002追加source generation/edges/change/responsibility/work，旧0001 checksum不重写；legacy版本及policy分别有durable分页进展，推进前检查未回填范围。旧writer实际导出恢复有准确原ref/receipt及文件；其原数据/归档provenance不在本报告重做全部审核。HoldVersion/HoldPolicy是实际PG锁机械fixture；测试结果通过Content/Manager观察，机械SQL不冒业务oracle。World infrastructureClosers/原FD firstClose/sticky错误和RetainInvocation保留未知scope职责。

**候选强度：Speculative，KEEP。** 多个具体hold方法与legacy restore不是通用资源framework的理由；删除这些设施会将精准锁与关闭知识复制入真实测试，损失locality。当前两个恢复数据集是两个场景，不是两个storage adapters。Before/After保持现有限owned fixture；继续用完整原身份/独立字节验收，不能用过度mock替代PG。无需新增架构重构或ADR。

## 判断及执行资格

A为当前必要源修正，交solefixer；B/C/D不成为本票或未来票退出要求。旧accepted/currentauth/fullRef、五动作独立、原保存主体/用途、immutable caps和pending非erased保持。没有发现需要推翻ADR0004/0006/0007或根CONTEXT的新规则；“管理Deadline != Claim lease !=管理TrustedUntil”只是落实既有有限责任边界。

本扫描只证明上述固定源码结构与必要缺口，没有运行native，也不承诺整个39paths Spec/Standards审查通过。57ea新race的timeout125/group_absent=false仍独立不完整；root/soleowner以后成功的新scope不能反写旧run。HTML报告附可离线阅读的正文/图形fallback；xdg-open结果将单独记入本文件，不以创建HTML代替浏览成功。

HTML：`/tmp/architecture-review-20261004T151634Z-content02-57ea.html`。实际 `xdg-open` exit3：无可用GUI/文本浏览器打开方法；未浏览、未验证CDN渲染。报告自带inline CSS和完整文字/方框fallback，无需安装或下载。只执行本次获准打开尝试，未运行native测试/DB。
