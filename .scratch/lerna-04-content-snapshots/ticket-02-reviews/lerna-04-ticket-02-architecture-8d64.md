# 04票02：8d64固定源码架构复核

2026-10-04。source `8d64ad1ed8eee35d07ee3fc990dd2696729ef9c8`，base `f05b2f1068958ba6b63e6c1dc58d6cd1684446cc`；实际计算全范围6 commits/42 paths，57ea→8d64为7 paths、1445增/71删。全读本次七个变更源码对象（domain management/service、PG management/typed rows测试、继承到期/处理期限/legacy公开测试），复核已采用57ea期限、继承到期、续期分类三份决定。使用 improve-codebase-architecture、codebase-design/DEEPENING及HTML scaffold，领域词汇沿根CONTEXT及ADR0004/0006/0007；不读Std/Spec报告，不将过去批准的决定误称本次独立发现。未运行Go/native/DB/build、未改仓库。

**结论：一项Strong必要闭合F1，修现共享推进module即可；没有必要新增架构层/框架。** 已完成的原期限、定点义务、自然/历史分类和单调责任保存保留；F1是其合流路径的真实遗漏，不是重新设计。源码资格与执行资格分开，本稿不表示02七AC或whole04退出。

## 1. 已采纳决定的源码资格

| 决定 | 8d64准确位置 | 源码结论 |
| --- | --- | --- |
| 原传播期限 | domain/content/management.go:798–1019 | 已保留每change原快照、锁后时钟、停止新遍历、最终所有visited原Deadline复核、原游标残留、NextPolicyDue重算及Claim校验。两实际消费者仍同实现。 |
| 晚入场继承义务 | management.go:492–658，service.go:262–268/304–311 | 原subject完整绑定；自身调度不阻断完整祖先登记；原key/due/budget；AdmissionTarget独立准确目标、同Tx保存/触发；新alias收紧cap同Tx调度；原Command优先。无需新SQL迁移。 |
| 自然/current vs 历史 | management.go:369–423/663–755 | 可选phase、历史时间原Due、当前cap freshNow、私有registerNatural共享两类真实自然消费者、当前policy完整绑定与覆盖检查。**阶段完成交接仍有F1。** |
| 后继维护覆盖 | management.go:684–732；PG management.go:261–283 | 有真实source-edge水位覆盖读，或确定性定点义务；只有可执行原维护才算覆盖，残留/未知不作无限保存依据。 |
| 历史pending不冲掉 | PG management.go:309–371 | 同Tx读旧责任并保留pending、未知holder/attempt、较早Deadline和动作并集；renewal非清理ACK。没有把清理闭合提前到02。 |
| 具体页读取保cause | PG management.go:73/291/385/430附近；management_rows_test.go全文 | 四个实际typed page readers保primary/Rows.Err/Close；private机械seam，无泛型page engine。机械driver测试显式不称PG nativeClose故障。 |

父报告 Component 18.402s、rows .018s，native exit0/groupAbsent；这些是执行owner控制下的报告，**本代理未执行也未独立核原日志**。新race、fullcheck、audit、真实old-expired 0001归档仍pending，源码表不得写成整票green。

## 2. F1 — Strong：初始传播晚于ValidUntil时不能丢自然维护

**Files/准确source：** `domain/content/management.go:369–375`（policy_change使用原Due）和 `:950–969`，尤其 `:957–965`（只有now<ExpiryDue才排natural_expiry）；公共反例接 `conformance/component/content_processing_deadline_test.go`。

**问题：** 历史分类改用原安装Due是必要且正确的；但初始阶段完成分支仍假定“到期已过，所以不用再调度自然阶段”。当 Save仍true、RetainUntil/CurrentRetainUntil都长、只有ValidUntil短，原Due有效，真实worker在 `ExpiryDue < now < original Deadline` 才完成初始页，register记历史not_required，随后直接complete，之后没有该修订自然Job可核当前失效。live Get/AuthorizeUse会拒绝，却不能代替holder耐久责任。

可达的公开步骤：先发布A及水位内D（原宽许可）；对已存在A安装新修订，所有动作仍true、仅ValidUntil改为近截止，WorkBudget长于这段有效窗口；不推进此新修订，等过ValidUntil但未过其原Deadline，再Step。旧修订的自然截止可很远，D先前的定点义务也绑定旧修订；因此它们不会及时替代这次新到期。source A自身同样可独立暴露遗漏，不依赖目录计数或私表。

**最小默认：** 保留当前module和ports。初始 `policy_change` 完整页序完成且通过本轮**原Deadline最终资格**后，耐久交接已预先固定的 `natural_expiry`，不论ExpiryDue尚未来还是已到：同key、原水位、natural游标从头、Due=原ExpiryDue、Deadline=原ExpiryDeadline。自然阶段自身完成不得重新排自己。若自然预算也已耗尽，保留原到期责任/残留而非成功完成或now+budget；初始阶段自身已超时仍由visited最终门禁还原原初始游标/截止，不得借较晚自然截止逃逸。

这是履行**安装时已保存的另一阶段原预算**，不是刷新授权/扩预算。前次续期决定禁止的是把旧deadline换成now+budget或跳过原阶段资格，不禁止及时完成初始阶段后履行原本就存在的自然义务。只在最后一页额外核当前自然状态不足以覆盖更早已提交页，故默认保留完整原水位的有限自然分页交接；不扩大水位、不重建新业务身份。

**测试interface：** Manager.Step及Content.Step各正常/迟到对照；观察原change的自然阶段/最终holderpending，原截止水位稳定、reopen不刷新、Get current拒绝但原Command receipt/publication与独立bytes不变。增加续期-before-cutoff正常对照，避免为修漏报退回历史ValidUntil误报；已有真实撤销pending合并继续成立。新反例必须仅缩ValidUntil且保留cap长，当前`:198`的RetainUntil收紧测试会被cap分支救到，不能借用它证明本路径。

**Depth / locality / leverage：** 一处private阶段完成规则同时覆盖两推进入口和任意多页，interface不扩大。deletion test：拆掉共享推进将原截止/phase/水位知识重新摊给Manager与Content；保持module，修其完整责任。Before：历史页→now已过expiry→complete漏维护。After：历史页原资格→原自然阶段→当前核/残留。

F1已提前通知root，留给solefixer。这里是固定source推断，未注入red，不宣布新版本已修。

## 3. KEEP — 有实际depth的自然/历史私有module

**Files：** domain/content/management.go:348–423/578–755/798–1019；PG management.go:261–283/309–371。

**强度：Worth exploring，当前KEEP。** registerPropagation、registerNatural、register、propagationPhase、executableMaintenance分别隐藏阶段分派、当前政策与后继覆盖、责任分类/单调cap、旧记录阶段解释、可执行维护资格。它们仍在一个domain文件内，接口没有暴露给每个Put/Get caller；不像需要跨多个浅module才能理解一个概念。

两个自然消费者是定点AdmissionTarget与source-wide自然页，两个外部推进入口是Manager.Step/Content.Step；都不是两个数据库adapter。当前真实PG adapter与机械decorator有各自证据职责，不造第二实现。

PG SaveResponsibility吸收保守持久合并；未来真正有清理ACK消费者时，可能需要更明确的领域合并值，但本票不因此预建状态registry或泛责任仓储。当前所需变化已经集中，删除私有自然module会复制current/fullRef/coverage/cap规则，损失locality和leverage。F1应留在现阶段交接，而不是扩大interface。

Before：定点current、nil历史直判存在漂移。After（8d64已实现主体）：phase→历史事实 / 共享current资格→单调责任保存；仅F1完成交接待闭合。ADR无冲突。

## 4. KEEP — 具体typed page readers与机械测试seam

**Files：** adapters/postgres/content/management.go的readPoliciesForVersionPage/readDescendantsPage/readResponsibilitiesPage/readAdmissionChangesPage；management_rows_test.go。

**强度：Worth exploring，当前KEEP。** 四类真实reader各自拥有SQL形状、解码类型、limit+1和cursor；内部function只是将真实rows消费集中为可控机械测试surface。它们没有扩成一套page callbacks/framework，正确Close因果集中在各reader有限返回点。

deletion test：删除四个private helpers只会把原实现放回四个查询方法、让机械driver更难直达同一消费路径；没有消除业务复杂度。泛型抽象也仍需四类解码/游标知识，当前leverage不抵新interface成本。保持具体reader和真实PG公开行为套件；driver注入保cause是机械证明，不能替代PG事务/恢复或nativeClose证明。

Before：具体rows消费早退可能丢Close原因。After：同具体消费→聚合primary+iteration+close，四类实际测试consumer；没有虚构新业务adapter。更广rows路径的全量Standards资格属于独立轴，本扫描不凭这四项宣布所有reader均审完。

## 5. 公开观察、legacy及退出限制

完整读新增公开测试：inherited_expiry的晚入场、续期/历史撤销、残留不冒覆盖、完整主体alias、多祖先；processing_deadline的两入口真实锁等待、最终所有change有限机械barrier、历史retentioncap当前分类。legacy测试新增原继承义务截止/phase断言，保原receipt和publication。测试走实际Content/Manager interface；独立字节与公开责任各有用途，pg锁机械设施不是业务SQL oracle。

真实过期0001生产者归档和最终升级场景尚未交付，不能仅由未来2099旧数据断言认定“过期legacy恢复已证明”。真实SIGKILL/原unknown资源/最终race与CI仍按各自证据；本报告不重复整个42paths provenance审计，不读取另一审查轴判断。

不改变CONTEXT/ADR，不引入05 Task/Grant/Memory、对象删除/第二holder或泛Job处理器；05条件准备未采用，绝不成为02退出前置。除F1外，不设新的架构退出门槛。root整片与票02最终验收另行进行，fixture/docs后续以准确changed objects复核。

HTML及xdg-open实际结果见文末补记；静态文字和方框图不依赖CDN，Mermaid/Tailwind只作增强。

HTML：`/tmp/architecture-review-20261004T160350Z-content02-8d64.html`。本次实际 `xdg-open` exit3，无可用打开方法/文本浏览器；未浏览、CDN渲染未验证。HTML保留inline CSS、完整静态文字/方框及Mermaid原文fallback；未安装浏览器、下载CDN或启动服务。
