# 实现进度

集成分支：`codex/lerna-implementation`。实现前基线：`1c042ec`。

2026-10-03 用户授权按依赖实现全部切片，逐片架构审查、提交并推送；需要决策时由 `gpt-6-astra`、`high` 智能体代为分析决定。后续 tickets 的粒度和依赖决策沿用此授权，不重复要求用户确认。测试沿用各规格已确定的 Application／Component／Host 公开验证范围。

## 当前状态

2026-10-05 01:12 UTC，05整页原责任CAS资格partial `1a52d24a051442cd8ccdd3c3ef01ff7707cc84a7` 普通0.555s／race2.272s通过：公开页固定实际first／second，另一消费者提交second的not_required后，旧页第一条真实写入、第二条真实ManagementConflict，公开完整状态确认整页回滚；fresh同原页恢复／重开、三正文／固定回执／V2保持。随后[祖先上限／准确AdmissionTarget首red](../lerna-04-content-snapshots/ticket-05-expired-accepted-cap/inherited-admission-first-red/README.md)真实2.223s失败：两祖先与target正常、原2s cap续宽仍expired、原责任deadline live及独立原字节保持后，首Consume原admissionKey被拒绝。封存／擦除及红后回执重放尾段未执行；产品未修，Astra high正作最小身份与因果决定。

03新九文件子成本overlay已全文审查，编译期／worker Current混合计时归属在运行前修正；全部原文件及SHA、dev／inode／mode核准，原已运行五文件overlay保持不变。当前唯一LOCAL归03机械格式／独立race二进制编译及一次原B实验，原5s Claim／30s caller／120s外界／业务tuple保持，暂无新测量结论。whole04仍15/41，两票未接受或合并、完整1.2关闭。

2026-10-05 00:56 UTC，旧holder升级[三条故障恢复](../lerna-04-content-snapshots/ticket-05-legacy-holder-binding/qualification/README.md)已分别真实通过普通／race：记录竞争2.273／3.852s；已commit后机械丢返回2.316／3.832s；初次固定700ms资格跨界整Tx回滚2.702／4.133s。完整源码partial `331154b132c5fd631fb8f663e05a12642cd014cd` clean；原qualification、字节、回执／历史和V2保持。六native均实际exit0及group absence，无原界扩大；原producer全部逻辑Close与actualWait先于消费者。最后释放后LOCAL为空，05仅静态准备整页责任真实竞争测试，03仅静态准备已采用子成本新overlay。尚不接受／合并两票或广告完整1.2，仍15/41。

2026-10-05 00:50 UTC，05记录竞争测试partial `2402fd79d5093e0a29605f4e28f2da0c1cac8cac` 普通2.273s／race3.852s通过：真实旧Read后合法Rev2收紧原保存期，旧完整前态CAS精确拒绝；同原qualification与deadline在两次真实重开后恢复绑定，V2／固定回执／发布历史／字节保持。不是新产品修复或伪造业务red；当前继续唯一槽执行提交成功后机械丢返回测试，原截止越界仍待。采用[子阶段成本决定](../lerna-04-content-snapshots/ticket-03-large-graph-claim/source-phase-cost-decision.md)，03仅静态准备新有限overlay，未优化／运行。

2026-10-05 00:46 UTC，原界独占大图B诊断race实际74.623s失败，native3086554／start12876878／exit1及group absence、session完成后明确释放。完整[原日志与结果](../lerna-04-content-snapshots/ticket-03-large-graph-claim/diagnostic/results.md)保留：64祖先和62材料都在calculate中由Access.Current先返回deadline，随后真实ValidateClaim的新DB时间分别晚原5s lease21.425／24.545ms；caller30s仍余1.161／6.921s。running／start1已经真实commit，尚无Prepared提交或worker publication；Material25／31次包括末次失败，成功24／30次。该新独占scope不解释历史共享负载失败。嵌套Current与Content读取成本不可相加，SQL／decode／对象读取子成本未知；已请求原Astra high审查下一次必要有限拆分，不预选缓存／索引、不扩期限或盲重试。原生产／测试字节仍等于5e8cad7，诊断overlay五文件／原文件身份及SHA全部重新机械核准，未进入集成产品。

05旧holder升级三条最小故障恢复测试已静态全文审查；当前唯一LOCAL授记录竞争的准确normal及同例race，另两条提交后丢返回／原资格700ms越界尚未执行。原writer停止与所有Close／Wait资格、原qualification身份和截止保持；两票仍未接受／合入，whole04仍15/41，完整1.2广告关闭。

2026-10-05 00:28 UTC，03其余23个Context控制普通三组7.061s／10.917s／15.902s及race17.093s／26.246s／36.470s全部通过。格式检查首失败为一个测试文件尾部多余空行，保留原日志；仅删除该空行的固定源 `5e8cad7e9d77162fda9773bde747235f8937c9ce` 随后17项锁定检查全通过，包括Go普通／race、vet、生成／构建、44项TS、37项执行工具、生成探针及158／89／101共同合同正反序。合同脚本部分旧Go测试显示cached，照实保留；大图race仍未通过，有限overlay诊断正在静态准备。

05旧正文绑定完整partial源码固定 `bb6e1f2ef05e49d88e2e4fb94ca04b0449981c52`。新原截止真实锁等待资格普通5.276s、测试race6.552s通过：两独立首次700ms WorkBudget／2s cap装配均观察真实policy阻塞；正常及时释放后原seal及实际全holder ACK／独立缺失／重开成立，跨原截止20ms释放后完整原pending／字节保持，无seal及可执行cleanup。原回执、新V2和deadline不变。race后校验和命令写错旧迁移路径，组合native exit1如实保留；单独正确三路径校验native exit0，未重跑已green测试。无产品修复或预算扩大；竞争CAS等剩余资格、最终受影响检查及两轴／架构审查仍待，两票未接受或合入，15/41与完整1.2关闭保持。

2026-10-05，采用[大图claim复核决定](../lerna-04-content-snapshots/ticket-03-large-graph-claim/README.md)：保持原5s claim／30s caller／120s外界，一次独占完整B race加有限阶段诊断，保留第一错误、原Claim及Prepared实际提交范围；不盲重试、不预选性能补丁或扩大setup期限。05旧正文同准确测试独占race4.267s通过，错误介质／首次seal及原attempt页三个相关race1.727s／1.846s／2.496s通过。冻结旧producer所有原CloseACK、actualWait及group absence先于当前消费者，旧SQL三项校验和保持；竞争CAS、丢提交回执及截止越界仍待资格。03其余23个Context控制正按三组执行普通及race；两票未接受或正式合并，15/41与完整1.2关闭保持。

2026-10-05 00:15 UTC，03容量最终分组普通A11.726s／B34.271s及race A30.122s通过；race B74.336s失败，64祖先和62直接材料的真实Decision.Step分别返回原claim失效，其余65拒绝、Content边界和重复角色通过。原caller30s、worker lease5s、原Snapshot预算均未放宽；失败保留，正由已授权Astra复核最小闭合路径，不能称容量race已通过。root执行授权前后不一致，05首次compileFAIL在03 race B后段真实重叠，故该组不声称独占负载；64祖先失败发生在05启动前，62材料的末段可能重叠，不能仅凭重叠解释失败。日志没有更细的RunClaim阶段观察，不将静态期限上界当成实际因果证明。

05旧正文升级首green辅助unused import导致compileFAIL已保留；仅修正import后，准确原测试2.694s通过。冻结1a7编译器与旧producer的实际Wait、group absence和所有原逻辑Close ACK已核，完整原scope的两版本绑定、重开、错误介质拒绝、新fence清理及原回执／历史保持已执行。该正常运行发生在03组结束后；当前明确唯一执行槽授05相同测试race及相关原holder控制，尚未声称race、竞争CAS、期限越界或本票退出。两票仍claimed，whole04仍15/41，完整1.2不广告；全部22切片授权继续，所有历史unknown scope保留。

2026-10-04 23:50 UTC，03真实旧0003升级资格普通9.810s／当前consumer race11.874s通过。144个生产文件共1,419,513B逐项与冻结33811016原git对象相等；独立driver在原进程身份fsync后才放行，真实三次旧编译和原排队绑定完成，所有原holder CloseACK及actualWait／group absence先于当前0004追加。旧0001–0003校验和、完整输入及排队映射不变；新编译精确HistoricalUnknown且无新Content读取，旧scope不补零账、不换Snapshot洗掉未知。独立当前scope正常Completed；旧生产者为普通冻结构建，不声称旧代码race资格。05真实三attempt的2＋1分页资格partial `864399c` 普通0.857s／race2.305s通过：原未登记setup临时文件保留inode／字节／residual，原owner确切移除后沿原Defer全ACK，独立V2与原回执不变。以上仍为各WT的partial资格，两票尚待最终受影响检查、独立评审和正式合并；15/41与完整1.2关闭保持。

2026-10-04 23:35 UTC，03原Prepared资格partial `d41785c`：普通2.335s／race7.011s通过。原三轮18次真实编译读取27840字节耗尽后，真实Content.put已accepted，再触发20ms响应等待截止；三个owner重开按原WakeAt恢复两份原Prepared，无再编译、再规划或再读取Source，原artifact raw／回执、完整M、refs／sources／绝对界、规则start1／cost1和原预算全保持。该故障不声称物理Put未提交。05真实冻结1a7构建成功；首轮辅助类型比较compileFAIL保留，修正后0.401s真正business red发生在最后受信绑定stub。此前旧V1／V2真实正常发布、未停止拒绝、所有旧逻辑Close ACK及actualWait／group absence、当前unbound读取／seal拒绝、原字节／回执／published历史均已执行。正按既定决定静态实施窄CAS回填与可复现冻结构建，尚无green或本票退出；当前15/41与完整1.2不广告状态不变。

2026-10-04 23:29 UTC，03原预算真实锁等待及并发资格partial `554fe04`：三个准确测试普通4.503s／race8.818s通过。真实PG等待跨过原deadline后拒绝，原轮数与读取预约仍0；两客户端竞争原最后一轮仅一方取得第三轮；两客户端竞争原6字节仅一方进入真实读取，Reserved／Confirmed6、实际读取1次。每轮五个实际peer均有原native CloseACK，进程退出与group absence已核。原65536／三轮／绝对deadline不变。Prepared原命令恢复正在独立资格验证；真实旧0003上下文升级及旧0001正文holder升级已静态准备，尚未宣称通过。两票仍claimed、未正式合入，whole04仍15/41、完整1.2不广告；全部22切片授权继续，所有旧unknown scope保留。

2026-10-04 23:10 UTC，05真实跨页资格partial `1af6593ae290266357e28aab390b71747943565c`：PageSize2的原政策责任partial后重开，完整5条责任保持；4个原保存主体版本清理、另一保存主体的独立字节不误删。实际secondary关闭／nilport下，3个holder的2＋1页保留准确副本责任；两个World重开恢复后才全ACK。正常0.962s及该case＋原offline两项race3.607s通过，原水位／期限／copy绑定不变。03三次原数量溢出资格partial `6f6d97d`，修正过严测试诊断断言后2.039s通过：原三轮均消耗次数、读取预约仍0、第四轮精确次数拒绝及重开未派发。仍待真实原期限／并发／Prepared／旧源及清理attempt／升级／时钟CAS边界和最终审查，两票均未接受或正式合入。

2026-10-04 22:53 UTC，原cap清理partial `d849b7a1`：非法持久时间拒绝unit0.010s、真实清理链2.469s、cap／当前撤save／恢复save／自然phase四项race6.864s通过；原seal key／责任Deadline、独立物理缺失、重开ACK、新version与固定回执均核准，旧0001／0002校验和保持。03完整材料投影修复partial `d427c49`，正常2.609s及投影＋include／omit相关race14.541s通过。原三次早期数量溢出计数资格首次1.853s因额外要求诊断维度而失败，真实正常对照已完成、原返回准确context_overflow；仅收紧测试断言后复验，未宣称三轮已通过。采用[旧holder绑定升级决定](../lerna-04-content-snapshots/ticket-05-legacy-holder-binding/README.md)，只支持可完整核准的原scope，真实旧writer／停止／原介质／完整attempts及CAS仍待实施；不操作任何历史unknown scope。两票依然claimed、whole04仍15/41。

2026-10-04 22:44 UTC，采用[原接受保留期限清理决定](../lerna-04-content-snapshots/ticket-05-expired-accepted-cap/README.md)：严格解析并重新核对原持久cap到期，完整保存主体／用途／准确ref和结构闭包不变，原清理执行期限不续期，复用原seal及全holder ACK；最小修复正在静态实施。03独立正常Completed对照1.63s后，真实出版的坏shell／manifest／lock一致遗漏原Selected A，Source却接受，实际2.580s业务失败已保留；将共享核对M＋全部原selected材料的完整投影，后续Lock／重开尚未执行。两项均不构成新票退出或整片完成。

2026-10-04 22:40 UTC，03读取响应丢失资格 `963b463` 实际2.320s通过：真实6字节读取后响应丢失，原预约重开仍占6字节，重放预约不获新读取许可；第二轮真实编译后Reserved9286／Confirmed9280／Unknown6保持。输入修订资格1.799s通过：同原Snapshot合法更新goal后，原65536额度及三轮上限保持，18次真实回读累计27926字节；扩额到65537准确拒绝且当前输入和计数不变，第四轮准确拒绝。05 metadata资格固定 `e5b38a57`，实际最终测试已核完整三层来源和准确授权，仍为既有协议的独立资格。

05原接受保留期限的真实2.216s业务失败已保留：原2秒cap自然到期、原清理执行期限仍live、当前宽策略续期不能复活旧cap；旧读取返回expired且独立旧字节仍在，新准确version正常。消费者未建立原seal，后续erase／ACK未执行，不称已通过；最小因果分支正由已授权Astra审定。两票仍claimed、独立WT未正式合入，仍15/41AC、完整1.2不广告。进度提交 `1a5d71f0d8cd8e7529bd26440d52859aa1ee5610` 已实际push；所有历史未知scope保持保留。

2026-10-04 22:27 UTC，03原Snapshot预算partial `9534d2c`：原三轮跨重开真实出版／第四精确次数拒绝1.624s通过，后续关闭错误保留及verbose资格1.521s证明原65536内18次材料／四产物回读共27840字节、每轮9280。首次固定20000的累计额度资格1.373s通过：两轮正常，第三A／B后18582字节，M验证预约不足且未到达真实Content，重开不重置。首次compileFAIL保留，原red实际Go watchdog120s、本green30s，caller30s不变。05同原责任自然phase保erased正常2.293s／race5.570s已通过；真实删除后单次replyloss沿确认Defer100ms及原seal／期限恢复，原合法纳秒metadata列编码修复正常0.437s／race1.793s通过，早期失败与fixture截断对照单列。metadata三层完整来源／delegation／用途／绑定及实际祖先许可到期正常1.229s／相关race4.035s通过。两票仍在独立WT未接受或正式合入，剩余unknown／修订／期限／Prepared、过期cap／完整分页／真实升级继续，仍15/41AC及完整1.2不广告，全部22切片授权有效。

2026-10-04，采用[原擦除结论保持决定](../lerna-04-content-snapshots/ticket-05-terminal-erasure-history/README.md)：同原责任真实全ACK后，普通自然phase须保留erased和完整历史，仍执行动作union及原期限min；其他change独立，普通入口不制造新ACK。真实自然到期2.309s失败已复现结论倒退及旧holder历史丢失，修复待验证。迟到真实成功Put的Finish已修正当前holder不复活，正常0.282s及四项相关race2.990s通过。03合法长度但错误正文选材拒绝0.753s通过；原快照预算正在静态实现，尚无green。全部22切片继续，当前仍15/41项AC、两票未接受及完整1.2不广告。

2026-10-04，采用[原Snapshot耐久编译预算决定](../lerna-04-content-snapshots/ticket-03-original-budget/README.md)：原完整SnapshotRef固定三轮、累计真实读取和绝对期限，显式consumer-owned端口及追加fixture0004，编译材料与四份产物验证回读共用原预算，worker保留原Prepared／费用。原三轮完整出版及第三前重开后第四轮仍成功的真实1.974s失败已保留；预算实现与独立字节／期限／未知验证待完成。05权威擦除后新责任ObjectHolder标记修复正常0.365s及四项相关race3.649s通过，旧责任历史union保持；迟到Finish仍待独立验证。03反向include冲突0.611s通过。两票仍未接受，15/41项AC及全22授权不变。

2026-10-04 21:46 UTC，05原政策seal／全holder ACK的真实正常链路0.465s、五个受影响控制的race3.659s通过；初次0.249s列时间精度拒绝保留，修正只比较pgx可表达的微秒值，不改原policy或deadline。准确partial `641020d` 仍在独立WT，尚未正式合入或接受。03 B-disclose在include／omit两种真实Completed对照之后，仅撤披露，立即及三个owner重开后M和实际artifact均准确拒绝，内部完整M处理与原回执／历史保持；实际2.923s通过，partial `1148a1f`。05权威擦除后新责任仍误记ObjectHolder=true的真实0.335s失败已复现，最小修复正在验证。原Snapshot三轮／累计字节／绝对期限的耐久门和其他清理边界继续实施；两票未接受，仍15/41项AC，完整1.2不广告，全部22切片授权继续。

2026-10-04 21:35 UTC，采用[原政策责任与全holder ACK决定](../lerna-04-content-snapshots/ticket-05-policy-seal-ack/README.md)：原准确policy key和deadline同Tx绑定seal、holder、Job及责任，真实全holder确认后才将原责任记erased；已过期预算不续期、不同seal不改身份。正常恢复save的消费0.366s及race1.745s已通过；save仍撤销时无seal的真实0.406s失败保留，最小绿色链路在实施。03正文选材mismatch修复0.775／canonical0.087、include／omit正常5.187通过；B-process1.806、B-save1.832证明各自独立权限门与真实原拒绝回执。初始及更新policy的Sync均false，没有附带Sync变化。B-disclose、原Snapshot累积预算／恢复及其余票据边界尚待验证，两票未接受或合入，当前仍15/41项AC，全部22切片继续。

2026-10-04 21:18 UTC，采用[政策清理资格决定](../lerna-04-content-snapshots/ticket-05-policy-cleanup-qualification/README.md)：原责任完整比较和当前save／cap资格允许无物理效果的 `not_required` 核对，普通pending规则、历史期限与其他holder责任不变；全页版本／policy资格先于责任锁，最后采新鲜DB时间。真实首red0.246s已复现消费stub缺失，green与ObjectHolder相邻修正分别待验证。03部分真实metadata主因2.160、Proposal主因2.217、UTF8必要约束2.131、62直接材料完整Lock64链路11.540、完整当前Found回复1.474均通过；1MiB边界有独立red→green，不声称真实装配能达到所有单独上限。B正文要求omit但原A仍selected的真实0.904s失败已保留，正文选材最小修复正在实施。两票尚未正式接受或整合，全部22切片授权继续。

2026-10-04，采用[票03内部溢出诊断决定](../lerna-04-content-snapshots/ticket-03-overflow-diagnostic/README.md)：实际拒绝维度与可选完整测量沿原错误链返回，保留独立 Content 限制及缺测量，不新增公共合同、数据库或成功 Bundle。真实 metadata／Proposal 主因测试仍待实施。03部分单Content边界已真实unit red→green，合法原Decision限额的真实链路2.314s通过：20万字目标完整回显，262144字目标加完整metadata由Content层拒绝且无派发。第一次非法2MiB限额被schema挡住的失败保留，不算业务red。05跨进程第一顺序0.050s实际通过，写者关闭有独立ACK；反序与第一对照0.081s通过，真实fence后第二进程迟到Put拒绝，原eraser SIGKILL／Wait后重开确认正文缺失。被杀holder无逻辑Close ACK，其准确root4116685529（dev33/inode326495）保持unknown并保留，kernel退出及后续观察不推逻辑关闭。其余AC仍待验证，两票尚未正式合入或接受，全部22切片继续按依赖实施。

- 切片 01：**completed**。原任务 01–06 与后置架构任务 07 均 resolved；正确性修复、独立架构优化和 CI portability 已合入。受测实现提交 `23bac17ba0909c7a4d49d846eb08bc63391b99f0` 的真实远端 CI success，全部退出证据见[spec](../lerna-01-command-contracts/spec.md#切片退出证据2026-10-03)。
- 切片 02：**completed**。10票68项AC resolved；最终源码f56d930、整合5548744。本地完整顺序count1集成50.431s/race93.834s，独立两轴全部原发现关闭/新增0，fixture架构收益复核闭合；准确CI37162569420 success，完整退出及旧未知schema/CID限制见[证据](../lerna-02-durable-work/exit-evidence.md)。
- 切片 03：**completed**。六票42/42AC及原六项验收完成；受测882e97b/6bf5750、交付1aa21fc、整合391b4d8，完整profile/共享104项race及两轴/架构闭合。准确47ebce1 CI37194868564 success；10worktrees清理/分支保留。完整[退出](../lerna-03-deterministic-harness/exit-evidence.md)保留失败和未知资源。A阶段01–03完整退出，下一frontier04。
- 切片 04：**in-progress**。03完整退出5fbb1a0后实际API复核相等，采用具体Content-backed mandatory-context映射，发布六票41AC；01/02已resolved，15/41AC完成；02交付651036/正式合并6f88740/验收904ec5f已push，准确CI37227467190已success。03/05已claimed并在独立WT实施；完整1.2 profile仍不广告，其余票依图推进。
- 切片 05–22：**not-started**。仅条件准备，依赖未满足，不代表实现或验收。

## 切片 01 过程检查点（历史记录）

- 任务 01 证据：锁定依赖干净重装、`make check`、`make test-race` 通过；41 项共同正反夹具及合法值的真实 Go→TS／TS→Go 往返通过。详细记录见[任务 01](../lerna-01-command-contracts/issues/01-exact-contract-roundtrip.md#comments)。这些只证明公共值合同范围。
- 任务 02 证据：`make bootstrap`、`make check`、`make test-race` 通过；共同语料共 109 项（41 值、68 命令），两端错误分类及合法输入真实往返通过；6 项生成器拒绝探针通过。详细记录见[任务 02](../lerna-01-command-contracts/issues/02-strict-command-validation.md#comments)。已按决策消除 TypeScript 内部模块反向导入公共 facade 的循环依赖。
- 任务 03 证据：`make check` 通过；46 项共同摘要案例（31 独立预期、15 拒绝），覆盖 UTF-16 键排序、业务／主体变化、重传及 1 MiB 边界。详细记录见[任务 03](../lerna-01-command-contracts/issues/03-canonical-command-digest.md#comments)。
- 任务 04 证据：合并后 `make check`、`make test-race`，最终别名修复后 `make lint test build` 与生成一致性通过。共同语料为 153 项值／命令／响应往返夹具，加 46 项摘要案例；最终 21 个 TS 测试。详细记录见[任务 04](../lerna-01-command-contracts/issues/04-receipt-and-readonly-query.md#comments)。只读接口返回独立观察，并保留独立的原输入引用，避免调用方或回调修改绑定及源事实；受信入口仍待任务 05。
- 任务 05 证据：`make check`、`make test-race` 通过；补充拒绝案例后 Go / TS 受信查询套件与 lint 通过。28 项共同权限夹具覆盖租户、委托、固定 owner、目录接替、原身份与截止；额外验证只读观察、回调隔离及取消。详细记录见[任务 05](../lerna-01-command-contracts/issues/05-authenticated-owner-isolation.md#comments)。Go 协作者须遵守有限 context；TS 单一总计时器限制整个查询，不能强制终止不合作的外部回调。这些不构成真实网络认证或持久事实查询证据。
- 任务 06 证据：`make check`、Go component race、合入前最新分支的生成一致性与 diff 检查通过；共同夹具包括 158 项编解码双向往返、46 项命令摘要、28 项受信查询、15 项协商与 2 项独立 Schema 黄金摘要；13 项生成器拒绝、8 项变更敏感性探针通过，32 个 TS 测试。详见[任务 06](../lerna-01-command-contracts/issues/06-version-and-method-negotiation.md#comments)。
- 两轴审查固定 `b825c2d`，发现 Go 默认转义膨胀导致合法往返拒绝，以及 TS 可变 Schema 放宽规则但摘要不变；另有 Unicode predicate 重复维护建议。单一修复已通过 `make check`、`make test-race`（34 个 TS 测试），既有摘要不变；修复与后续证据见[审查记录](../lerna-01-command-contracts/code-review.md)。验收尚未完成。
- gh 的 Actions API 查询返回 Forbidden；后续已连接 GitHub 工具实际核实 `4874226` / `70cbf66` 两次 push CI 均因格式脚本 `rg ENOENT` 失败。正在独立修复并将核对新提交实际 CI，不能把 Git push 成功当 CI 成功。详见[CI 记录](../lerna-01-command-contracts/ci-verification.md)。
- 切片完成必须有实际验收证据；规格明确、任务发布和代码合并均不等于验收完成。
- 真实外部接入、独立组件、生产故障域、评测样本与容量结论按验收矩阵分别记录，不用本地模拟结果代替。

## 过程约定

- 每个实现任务使用独立 worktree／分支，先核对集成分支基线，采用 TDD 后合并集成分支最新提交再报告。
- 任务依赖以实际完成记录为准；集成后运行相关验证及两轴代码审查，修复后关闭任务。
- 每片结束执行架构审查，按用户授权由决策智能体选择需要立即解决的问题，保留审查报告与决定，不增加尚无使用者的抽象。
- 记录命令、环境、准确版本、结果和限制后更新切片及验收索引，提交并推送。

## 切片 01 后置优化检查点

任务 07 已 resolved，基线同负载实测 119.217268 秒，最终 10.909816 秒；Go / TS fixture runner 各由 226 次启动降为 1 次。额外反序与 16 项生命周期测试成本单独记录，详见[架构审查](../lerna-01-command-contracts/architecture-review.md)；本机测量不作为生产容量保证。后续独立 Standards / Spec 审查均为 0 新发现，原问题已关闭；整片仍等待真实新 CI 成功与退出证据。

## 切片 01 完整退出

准确代码 `23bac17`，合同 1.0.0 仅 command.get，生成器 1.0.0。本地 check / 受影响 race、两轴复查、独立架构优化和 [远端 CI](https://github.com/ruipengliu/lerna/actions/runs/37141974245) 全部通过；旧 CI 失败保留并明确修复。6 项 spec 验收逐项映射、环境及限制见 spec Further Notes。尚无持久接纳、网络认证或业务组件第二实现；F02 摘要证据不替代切片02的数据库去重，G3 互操作仍待15。此前等待 / in-progress 记录为过程检查点，不代表当前状态。下一可开始切片是02。

## 切片 02 启动

前置01完整退出后，按已授权决策发布[八张票据](../lerna-02-durable-work/ticket-review.md)，共53项验收。内部依赖01→02/03，02+03→04，04→05/07/08，05→06。首票PG原子接纳已 claimed；采用独立 Host 版本 host-durable-work-1，不扩展公共1.0.0清单；真实数据库、迁移、耐久及进程故障均待产品验证。前片九个已完成 implementer worktree 已 clean 安全清理。

切片01退出文档 `8dc82ca` 和02票据检查点 `8e7438e` 的后续 push CI也实际 success（runs 37142242216 / 37142480320）；原受测实现23bac17不变。02首票目录裁决见 layout-decision.md，业务演示不进入host装配职责。

## 切片 02 首票合入与新 frontier

PG票01已 resolved并合入 `f12fab1533df5869d45d1504b4630381bd4955fc`，真实18项PG行为、本地完整检查及受影响race通过；COMMIT确认丢失与Host答复丢失分开验证。v1真实writer `988f8b7`、fixture `e808aae`及独立恢复证据已保留，准确来源见票01。新远端PG job待push后核验，不能把本地成功说成CI成功。

票02 SQLite接纳、票03 PG领取修订均已 claimed，从同一最新integration建立独立worktree并行推进。两票不共用写工作树；03/02冲突通过merger处理，接口按实际消费最小扩展。Claim、SQLite、调度、进程SIGKILL及切片02整体尚未验收。03后续六票/42AC准备经授权代理审定仍在/tmp，必须等02整片退出后按实际runtime复核，未发布。

PG 首票检查点 `e5f26b87fb8914dc16bb6837abff6607a50cddb1` 的真实远端 [CI 37145113569](https://github.com/ruipengliu/lerna/actions/runs/37145113569) 已 success：基础合同 job 与真实 PG 集成／race job 均通过，原 v1 artifacts 校验和通过；集成 1.778s、race 3.551s，日志未使用测试结果缓存。准确证据及范围见[切片02 CI记录](../lerna-02-durable-work/ci-verification.md)。此前“待push后核验”为历史检查点。

05/06 的[调度细化](../lerna-02-durable-work/scheduling-decisions.md) 已由授权决策代理分析并采用：同一纯 project 的可信持久策略、等待门槛、有限尝试、责任关闭与成功投影分别观察；同库数据库配额及有限竞争公平界。这只是待实施决定，05/06仍未开始，依赖图不变。并行新迁移在整合时核对编号，不改已发布来源。

票03 PG领取已 resolved，feature `18b80ce`、worker tip `f83f1b3` 经merger合入 `e8e3384`。真实29项PG测试、本地check/race及integration-race通过；准确 push [CI 37146622433](https://github.com/ruipengliu/lerna/actions/runs/37146622433) 也实际 success，集成2.534s、race4.866s且非cached。票02尚在实施，票04仍等待02完成；后续等待／配额／墓碑／进程恢复均未验收。

票02 SQLite接纳已 resolved，真实v1 writer `f4fb057`、fixture `80aebbf`、clean worker `874db65`经merger合入 `770f764`；共享13项接纳行为，两库顺序最终integration7.501s、race15.058s通过。03仍只有PG Claim，SQLite Claim待04；目前原八张核心票已完成01/02/03。

04已 claimed，在同版实际双库基础实现SQLite Claim和共同work suite。并行真实PG suites暴露不同schema共享advisory key的确定性锁耦合，失败保留在02Comments；授权代理决定修正存储锁范围，额外独立[票09](../lerna-02-durable-work/issues/09-pg-storage-lock-scope.md)已 claimed，与04并行，不修改原业务依赖图。当前只是决定，修复仍待真实red→green及并行复验。准确范围见[锁范围决定](../lerna-02-durable-work/pg-lock-scope-decision.md)。

`930d3ca` 真实双库接纳/PG领取 push [CI 37147496071](https://github.com/ruipengliu/lerna/actions/runs/37147496071) 已 success：基础合同、PG+SQLite必需集成2.868s/race8.739s均通过且-count=1重新执行，两份v1校验和全部OK。不包含尚未合入04/09的行为。

额外09已 resolved，feature `eed3ef8`经merger合入 `edb189e`：跨schema真实Claim空/Record锁超时先red，统一三路径schema tuple后四项正常与竞争green，完整两套integration/race并行9.583s/17.244s通过。09未修改迁移/源fixture/公开生成物，有限hash碰撞和同schema升级须排空旧协调协议的限制已记录；准确新远端CI待核验。04仍active，原八票01/02/03完成，不提前关闭整片。

09整合检查点 `3a7f1f8` 真实 push [CI 37148346512](https://github.com/ruipengliu/lerna/actions/runs/37148346512) 已 success；两库实际集成4.277s/race10.252s均-count=1，v1校验和不变。

04已 resolved，feature3376e08、准确受测修复ff22936、clean worker c235689经merger合入 `dcb6f44`。PG/SQLite共同13个工作行为、SQLite真实v1来源→v2→原Job Claim完成、UTC固定精度时间边界和关闭重开通过；最终两库integration11.269s、race22.544s通过。一个失败轮的PG cleanup连接已关闭问题修复后，当前成功轮登记scope正常清理；失败轮有一个名称只留在退出进程的schema无法重新确认归属，按安全裁决保留并注明，未按数据形状猜测删除。详见04Comments，尚不声称04远端CI成功。

原八票01/02/03/04完成、额外09完成。05/07/08已 claimed，各从最新集成独立worktree开始：等待/退避采用scheduling-decisions，清理/真实升级采用新[retention-decisions](../lerna-02-durable-work/retention-decisions.md)，故障采用[process-fault-decisions](../lerna-02-durable-work/process-fault-decisions.md)。07的psql17.11完整恢复18.6 dump工具探针已实际通过并清理自己登记scope，不能替代07产品升级/清理验收；08的SQLite存储端口确认丢失不宣称原生COMMIT异常分支。06仍等05；整片退出等待全部核心票、额外修复、审查/架构和真实CI。

## 切片 02 SQLite 接纳票据检查点

票02已 resolved；实际 v1 writer `f4fb057` 在代码提交后从历史 Git archive 构建，完整 SQLite file 与 checksum/provenance 保留。PG与SQLite共同运行13项同版接纳行为；SQLite WAL/FULL运行时核验、Linux跨进程写Host排除/关闭接替、busy、取消、完整Tx关闭生命周期、scope和真实v1文件恢复通过。实际内置SQLite3.53.4/go-sqlite3v1.14.52/CGO，不依赖个人头文件。有限Close失败时保留锁，回调退出后可重试。具体命令与范围见[票02 Comments](../lerna-02-durable-work/issues/02-sqlite-durable-admission.md#comments)。

切片02仍 in-progress；票03 PG Claim已合入，SQLite Claim留给04，调度/SIGKILL/完整升级出口尚未完成。两库必需integration与CI race已加-count=1；缺PG配置/服务或CGO均硬失败。新SQLite远端tip CI待root核实，历史PG成功不替代新两库CI。默认/tmp为tmpfs，实际最终两库integration/race和v1writer另在/workspace下本轮自登记overlayfs临时范围运行；未宣称断电或生产故障域耐久。

04双库领取检查点 `b1674b2` 的准确远端 [CI 37149178541](https://github.com/ruipengliu/lerna/actions/runs/37149178541) 已 success：必需真实集成8.417s/race15.292s均-count=1，原十项v1校验和不变。

05补充采用[legacy策略](../lerna-02-durable-work/legacy-scheduling-decisions.md)及[实际处理门禁](../lerna-02-durable-work/processing-gate-decisions.md)：新接纳及首次接管legacy均绑定5分钟/最多3次启动，重开不刷新；实际Host成功提交必须经过真实Start/权限/期限，旧Complete不得绕过。纯hash及可信runtime存储机制保持各自职责，04/07/08在05整合时更新实际调用与同owner Clock，依赖图不变。07/08本票完成不关闭05或整片02。

08已resolved并经merger合入d89e789；准确push [CI37151050492](https://github.com/ruipengliu/lerna/actions/runs/37151050492) actual success，两库-count=1集成9.718s/race26.419s。SIGKILL提交前/Host回复前、原Claim跨进程接替、SQLite存储端口确认丢失均有正常对照；SQLite原生Commit未知分支与断电不在此证据。

07已resolved，feature4a0871a、工具生命周期修正75a5ac3及最新root整合0ec6386经merger合入0f27475。两库真实v1→v2→0003retention的原回执/原Job继续、版本保存故障回滚/重试、正文清理与独立command gone已通过；mandatory整合24.840s、race49.665s，固定psql18.6工具/历史升级选择race62.746s。CID登记后的真实取消精确清理/absence已验证；未知CREATE无完整ID不启动psql且可能遗留未启动容器，实际失败轮限制保留，不猜删除。详细证据见07Comments。准确新远端CI仍待核验。

原八票现01/02/03/04/07/08完成，额外09完成；05仍实施并承担整合07/08真实Start/统一Clock，06仍等05。06提前采用[容量交接](../lerna-02-durable-work/capacity-handoff.md)：同库同schema/file≤64明确OwnerRef、统一command→pool→input→Job锁序、所有实际消费入口有限耐久pool门禁，历史鉴权查询/原键优先保留，未配置不产生新责任或无限处理；尚不表示06实现。整片仍待05/06、两轴审查、架构优化及真实CI退出。

2026-10-03，票05已resolved，准确受测产品8a1df46（已merge root07/08及418415f）；显式Start/当前资格/immutable revision policy、持久gate wait/有限retry/deadline、原Job丢通知扫描与责任关闭/成功Projection分开在同一真实PG/SQLite suite通过。07/08业务fixture一并真实Start，root已发布v1/v2/v3原迁移及旧夹具保持，新增wait迁移v4。最终mandatory count1 integration26.132s、whole integration-race57.172s/消费者1.658s，fmt/check/test-race/module verify/checksum/diff check均通过；准确red/green、真实v2旧Claim来源、版本/限制见[05 Comments](../lerna-02-durable-work/issues/05-persistent-wait-and-scan.md#comments)。06frontier打开，whole02仍active，容量/配额与后续审查、架构、准确CI未退出。此前05未开始/实施中的记录为历史检查点，不代表当前状态。

07整合准确418415f的[CI37152615344](https://github.com/ruipengliu/lerna/actions/runs/37152615344)已success：固定18.6工具取消race2.378s、真实两库集成11.242s/race28.948s均-count=1，十项v1源hash不变。

05已resolved并经merger合入f28b69d（tested8a1df46/tip887e839）：0004wait、有限policy/legacy首次接管、等待/退避/丢通知、严格Start/Finish及07/08真实处理整合通过两库whole26.132s/race57.172s；真实v2旧writer b1674b2的完整PG dump/SQLite file另行冻结，原v1/已发布001–003不变。根全range diff核验补齐v2原始dump的窄.gitattributes，dump字节不改。06已claimed，按scheduling/capacity-handoff实际入口门禁开始独立worktree实施；原八票只有06尚未完成，额外09已完成。整片退出仍待06、两轴审查、架构优化和准确最终CI。

05整合准确7fa7594的[CI37153111359](https://github.com/ruipengliu/lerna/actions/runs/37153111359)已success：工具race1.567s、两库-count1集成14.404s/race34.816s及27项真实v1/v2来源manifest OK。06额外采用[无执行额度时的到期关闭](../lerna-02-durable-work/capacity-expiry-decisions.md)：可信维护短Tx无需新执行Claim即可按原policy/revision准确关闭到期责任；不算hash/attempt/公平执行机会，不抹新修订或异revision Claim。有限维护服务机会与配置可用前提明确；仅决定，等待真实实施验证。

06继续实施；授权代理根据实际静态索引环的差距采用[动态资格公平次序](../lerna-02-durable-work/fair-eligibility-decisions.md)：每lane持久有界tenant FIFO、已等待者保序、新/恢复合格者入尾、分页解析绑定准确head、成功Claim同Tx移尾，配置变更按身份保序。已通过的固定N=2轮转不替代新增动态资格反例；尾部规则仍待真实两库实施验证。03当前端口复核与04准备仍仅在/tmp，不提前发布或启动依赖切片。

## 切片 02 全部核心票与整片审查

06最终worker cdc7ae6 经merger合入96a0ecc；原八张核心票53条AC与额外09的5条AC均resolved。本地必需两库count1集成60.305s、race91.786s，来源27项hash不变。准确根检查点26c9100的[CI37156883508](https://github.com/ruipengliu/lerna/actions/runs/37156883508)实际success：工具生命周期race1.569s、两库集成21.892s/race47.390s、27项来源校验全部OK。

独立Standards/Spec分别3/1项发现，原报告见[两轴审查](../lerna-02-durable-work/code-review.md)。单一实现分支正在修复全部发现；实际Run固定fallback可能使500ms期限内100ms退避的合法重试错过，PG启动错误也丢失可判断原因。绿色审查前CI不关闭发现。整片02仍等待修复后独立复核、最终架构审查、准确新CI与退出证据；03/04仅/tmp准备未开始。

## 切片 02 审查修复与独立复核

单一修复产品4311585、clean workercc6a053经merger合入6783307；check/base-race、完整count1两库集成50.359s/race103.345s在原timeout120通过，27历史源hash及001–005不变。原Standards三项与Spec一项经同两位独立审查者按完整baseline8e7438e...6783307复核均关闭，新增0项；原因、失败轮诊断与验证细节见[审查记录](../lerna-02-durable-work/code-review.md)。

架构只读探索按6783307最终刷新，保留一个Worth exploring候选：测试fixture稳定scope归属与writer generations。临时HTML已生成，xdg-open因无GUI实际失败；授权代理正在选择/grill，不据此宣称重构已实现。6783307已push，准确[CI37158656088](https://github.com/ruipengliu/lerna/actions/runs/37158656088)已实际success：工具race1.560s、两库集成21.011s/race46.969s，27源hash不变。整片02未退出，03及以后未开始。

## 切片 02 后置 fixture 架构任务

准确6783307只读架构探索保留1 Worth exploring（0 Strong/Speculative）；授权Astra经5轮11项grilling选择现在实施，root采用[决定](../lerna-02-durable-work/architecture-decision.md)并发布唯一[票10](../lerna-02-durable-work/issues/10-owned-fixture-lifetime.md)，目前claimed。稳定fixture handle将统一本轮确证scope归属、writer接替与有限退出；历史来源/故障loader、process Kill/Wait和backend专有故障仍各自负责。待实施后证明普通caller已移除creator保活与指针登记知识，不能只移动helpers/maps。Whole02仍in-progress，03仅准备。

## 切片02架构实施后审查待修复

票10代码1863fc4、clean worker c5d5d40经merger合入c52e68b；完整顺序双库集成52.406s/race93.619s通过，产品/合同/001–005及27来源不变。准确[CI37160694293](https://github.com/ruipengliu/lerna/actions/runs/37160694293)已success。独立收益复核确认稳定fixture handle消除了六类caller及pool的生命周期知识和三张指针maps。两轴各新增1项P2：已确认退出但失败holder的历史错误永久阻断清理。票10重新claimed，原单一review fixer负责全部新增发现，whole02继续in-progress。首版red新遗留两个无法按准确名称确认的PG scopes，与旧04未知schema/07无CID限制独立保留；321PG/319目录absent仅指确证登记项。03/04/05仍仅/tmp准备。

## 切片02正式退出

准确5548744 CI37162569420已success：工具race2.407s、两库count1集成20.985s/race45.463s、27源hash。十票68AC、双轴新增0和唯一架构收益闭合，最后fixer登记285PG/265目录全absent；未知两prototype/旧04schema/07CID限制保留。详见[完整退出](../lerna-02-durable-work/exit-evidence.md)。下一步只在安全确认clean且tip已整合后清理11个02实施工作树、保留分支；03再按最终SHA复核并发布。以下旧章节记录历史阶段，不改写当时失败或状态。

## 切片03启动

02的11个实施工作树均先核对clean且tip为df2dbe5祖先，再安全移除，所有分支保留；仅root工作树留存，未知DB/container不操作。记录/tmp/lerna-02-worktree-cleanup.txt。03最终授权交接见[final-handoff](../lerna-03-deterministic-harness/final-handoff.md)，六张独立issues总42AC，01+04是真正并行frontier；不把schema/表/worker拆成横向半票。实现采用独立owner真实事实、强类型新版codec/账本、actualStart和全members唤醒，独立目标不冒充Executor/Effect。

## 切片03独立目标完成与故障计划启动

04受测cac8e92／clean tip792ee85经merger整合f11135b，6AC resolved。新owner真实SQLite3.53.4/WAL/FULL/FK及0001迁移、普通write/read/query与独立Observer、原键冲突/不续期/准确到期/无query边界通过；final normal0.356s/race1.483s各10项、makecheck/module verify及旧27hash全部通过。Standards两P2及清理followups经原reviewer复核关闭，Spec/Architecture无发现；101个精确登记scope全部absent。证据见[04](../lerna-03-deterministic-harness/issues/04-durable-test-target.md)。不声称供应商、nativeCommit未知、SIGKILL或断电验证，准确新远端CI仍待核验。

05直接前置已满足并claimed，从最新integration新工作树实施耐久fault plan/迟到原效果/seed与cursor；01继续完成自身9AC，02/03等待01，06等待01+05。整个03尚未完成，不提前广告完整decision_engine profile。

04整合检查点270ae4ae8abe2c5245328da2e5c4cdfe514ecf40的准确远端[CI37164714040](https://github.com/ruipengliu/lerna/actions/runs/37164714040)已实际success，两个job所有step通过。基础检查包含新target测试0.087s和旧158共同夹具双向/正反序；既有真实双库恢复count1 normal24.421s/race51.850s、psql18.6工具生命周期race1.550s及27源hash通过。远端race范围为既有recovery，不能当作新target race证据（新target本地race1.483s见04）；未合入的Decision与05fault plan均不在此CI范围。此前“新远端CI待核验”为历史检查点。

## 切片03耐久故障计划完成

05准确受测a0198ed、clean tip e3c3426经独立merger整合为077f61647d484e938402ff14bcaa7d3564abb196，合并tree与worker相同；6AC resolved。私有InstallPlan/RunEvent保存明确步骤、原身份、seed与cursor，ReceiveOnly/ApplyReceived保留迟到责任；实际Rollback与提交后丢响应分别用独立目标事实核验。新增0002实际由冻结04历史writer写出的同一SQLite文件升级，原0001及三项源hash保持。最终19项count1 normal2.264s/race3.615s、bootstrap/check/module verify及旧27hash全部通过；两项Standards P2已复核关闭，Spec/Architecture新增0。120个精确登记scope全部absent，所有sessions结束。准确版本、边界和逐项证据见[05](../lerna-03-deterministic-harness/issues/05-durable-fault-plans.md)。

目前03只完成04/05的12项AC，不提前关闭整片42AC；01继续验证规则启动计量、prepared恢复与真实旧writer迁移，02/03尚未开始，06仍等待01完成。本票未做SIGKILL、原生Commit未知、断电或供应商验证。新push CI尚待按准确head核验；既有04检查点CI不能替代新05代码验证。

05整合检查点682961aecf79d2c2fe08b526e11af225aabfecdf的准确远端[CI37166918488](https://github.com/ruipengliu/lerna/actions/runs/37166918488)已实际success，两个job所有step成功。基础检查包含新target/fault-plan套件1.168s与旧158共同夹具正反序及实际双向往返；既有真实双库恢复count1 normal22.733s/race49.646s、固定psql18.6工具生命周期race2.371s及27来源hash全部OK。该远端race范围仍为旧recovery，新05 target race为本地3.615s；未合入的Decision/0002计量迁移不在此CI。whole03仍in-progress。

## 切片03原Decision规则票完成

01九项AC已resolved；最终源696ac49846105a16f33e5de86dc621a3858651b2、文档交付d806a92eb443b8e0c989cde4e4d7c20c7d61f49c，经merger合入530770274a457356fafe5a2e06832b7ee33f1f2e，双parent和完整tree等于worker已核验。严格1.1 Go/TS、原双身份accepted+Decision+FKJob短Tx、真实独立Source/Publisher、durable Start与完整prepared/原键发布读回/Finish，以及真实旧writer升级通过；[证据](../lerna-03-deterministic-harness/ticket-01-exit-evidence.md)和[实际端口](../lerna-03-deterministic-harness/ticket-01-api-handoff.md)给后票准确接法。

产品机制f96的完整新PG normal25.137/8.208s、race42.337/11.485s，原PG/SQLite恢复normal27.357s/race60.405s均count1/timeout120顺序通过；b43窄历史pipe修复后的真实五状态旧writer升级5.760/9.080s、696测试登记修复后真实pipe0.010/1.058s通过，前41cb makecheck覆盖未变81新+158旧共同字节及JS/生成。两个独立最终轴各0open/0new，46commits/187paths；1074成功ack中的818PG/256FS全部absent，exact空overlayroot rmdir0，所有sessions已退出。历史失败与未知旧scope/CID限制保留，不猜删，也不冒称native driver故障。

本检查点新push/准确远端CI尚待核验，旧05 CI不能替代新Decision。01/04/05合计21/42AC完成不等于whole03；候选全族、取消/资源完整矩阵、SIGKILL仍分别归02/03/06，所有六票完成后root再完成支持清单、架构及整片退出。

## 切片03后续三票实施

准确c9de1ba的[CI37174778053](../lerna-03-deterministic-harness/ci-verification.md)已success：新真实PG Component normal21.061s/race33.892s、Source与真实旧writer升级7.243s/9.385s、原双库recovery23.166s/50.347s及81新/158旧共同双向字节全部通过。前述CI待核验记录为历史。

02/03/06直接依赖已全部resolved，root已正式claimed三个独立票，采用实际首票API及规则兼容、控制、生命周期决定；分别在新worktree/branch按TDD实施。当前已完成仍为01/04/05的21/42AC，不广告完整profile，不关闭whole03。构建与数据库槽独占；新票特殊恢复归自身，全部42AC后root负责整片审查、架构、支持清单、最终CI与安全工作树清理。


## 切片03进程恢复票正式整合

06六AC已resolved；实际代码a5005ab、独立退出文档6904b2d，经merger正式合入58f8f0f855d4e0bce1bc7dda9be4e000ad0d2385。两个父提交准确、完整tree等于worker、双方clean。真实Source发布后／Decision完成事务提交前后、target COMMIT前后、pending迟到与同seed隔离重演均有正常／SIGKILL边界证据，见[最终交接](../lerna-03-deterministic-harness/ticket-06-api-handoff.md#最终检查与本票退出)。

审查原前代holder丢失及同gate正常对照问题经原implementer修正；Standards复核0hard／0smell，Spec a0／b0／c0，固定15commits／24paths。修后makecheck和完整base-race实际exit0，模块校验及当前六manifest共27＋71＋3逐项通过。184外部target目录、44recovery目录、118PG与另列六local-only目录全absent；工具cache首次非空guard失败保留，准确工具退出／登记后仅清理自己的已检查cache与空overlay。旧未知PG／CID／其他票未确认构建责任不动，不声称全环境zero或断电／生产耐久。

当前27/42AC完成，02／03仍claimed并独立实现，整片03未退出。全部工作树保留至whole03，完整profile仍未广告。新push的准确CI待核验；04–22只有条件草案，后续仍按真实整片依赖继续。


## 切片03有界候选正式整合

票02交付5b11dc32c9d4a458b5f28b2be74d390d97128d73，受测/最终两轴源8f94f26；
merger正式合入cad6905ed64bde26883ef226455ebe1e221e97c2，parents949c392＋5b11dc3，
完整tree66fe8067b1c395281ef99cfa4e15cac399746aa8与worker相同。七AC均resolved，
详细实际检查、失败历史、1628PG／64targetFS／244recoveryFS／15archive／27确认组
及owned root准确absence见[逐AC证据](../lerna-03-deterministic-harness/ticket-02-exit-evidence.md)。
真实更严格旧writer检查normal13.311s/race15.403s，两轴最终无遗留；最后5b11只变十份文档，
未扩大此前native故障证据或广告完整profile。当前34/42，取消／资源与整片仍in-progress。

06准确949c392的[远端CI](../lerna-03-deterministic-harness/ci-verification.md)已实际核验success；
候选本检查点push的新CI仍待核验，不能用949替代新代码。取消工作树的新真实并发、
两个实际旧writer升级及两阶段进程恢复结果尚属于该工作树，最终双prepared合流、
完整profile、整片两轴／架构／CI由root在实际组合后确认。04–22仍仅条件准备。


## 切片03取消与限额票正式整合

03八AC resolved，受测源882e97b596ac2baa034881766041944b0da2e05e、纯文档交付8385b9b1aed0832b7ef9cfd75312eff6f84ae4ec，正式merge4b94cb65da2a7118e68f4cef7563793b8c97430f的parents为5bc＋8385，完整tree16f27d8与worker相同。原终态／回执与单调Stop分离，真实两种并发提交顺序、nilInput先到取消、原资源／期限及双prepared每次发布门禁完整通过；见[八AC证据](../lerna-03-deterministic-harness/ticket-03-exit-evidence.md)。

882标准check／fresh基础race／模块与七manifest172项0；真实Component／Source正常60.881／29.966，Source race28.434。Component整包race120.073超时仍是失败，真实102项互斥完整60／42分组race110.803／11.781通过，时限不变；原PG／SQLite及当前native Decision恢复正常37.201／race87.979通过。两轴0遗留，1545PG／244SQLite／64target登记范围absent，原3540254111未知目录／overlay保留。全部工具会话已退出。

前检查点5bc的[准确CI37189797348](../lerna-03-deterministic-harness/ci-verification.md#02准确5bc检查点的真实ci)已真实success，只证明当时83夹具／旧两迁移等范围；不替代882取消／Source0002／Decision0003／89夹具组合。当前42/42子AC不等于whole03退出。先完成准确完整profile及新的有限CI入口，再由root执行整片审查／架构／push／最终CI；04–22继续依真实whole依赖，不因子AC完成而提前启动。


## 切片03完整profile与整体审查检查点

2026-10-04，正式整合391b4d8的whole tree与交付1aa21fc一致，准确parents3845110+1aa21fc。三项广告flag实际生成后，1.1 command.get与三个Decision方法准确协商开放；原1.0和158旧fixtures冻结、89新版原payload未变。本地新shared入口实际串行Recoveryrace79.934s、动态全部104Component=60项102.204s+44项11.633s、Source28.271s，nativefinalexit0，期限仍120s。完整两轴[报告](../lerna-03-deterministic-harness/code-review.md)最终0，历史绝对链接P2已闭合；[架构](../lerna-03-deterministic-harness/architecture-review.md)0必要新重构。

[本地证据](../lerna-03-deterministic-harness/whole-exit-evidence.md)严格区分6bf受测与五项空白/文档followup、589ACK中的414uniquePG及122SQLite/32Target/3archive/6groups全absent、自身overlay/cache197保留和历史未知资源保护。原整包Component120.073失败继续保留。此处不引用旧5bcCI证明新源码；本次准确push CI待核，Implementation继续in-progress。


## 切片03完整退出

2026-10-04，准确47ebce1 CI37194868564两job/全部steps/实际日志success，104动态分组/Recovery/Source范围已核，[完整退出](../lerna-03-deterministic-harness/exit-evidence.md)保存各证据。414PG/122SQLite/32Target/3archive/6groups全absent；197确切缓存/3空目录清理、自身overlayabsent、10worktrees正常移除/分支保留；旧3540/失名PG/CID保留。历史pending和失败不改写，03正式completed，04按最终API复核再发布。


## 切片04正式实施

2026-10-04，准确03退出5fbb1a0后采用最终API映射，f706fe4发布六票41AC；图为01→02→03→04及02→05→06，只有01claimed。首票在独立Content工作树/分支实施，实际PG公开put的首条业务red为“正确有限字节应被耐久接纳”却返回unavailable，native exit1且确切schema清理完成；尚未得到green或本票退出。

root全文读取[Astra六票复核](../lerna-04-content-snapshots/published-ticket-review.md)并采用[1.2形状决定](../lerna-04-content-snapshots/contract-shape-decision.md)。Content自己的短Tx/字节介质/准确版本与固定回执边界明确；保留期到不冒称gone，完整1.2 profile仍不广告。当前只有首票持有本地构建/测试/数据库槽，其他审阅只读；历史未知资源保持保护。05–22继续等待其真实整片依赖。

## 切片04首票接受并整合

2026-10-04，01八AC均resolved，交付1a7、受测14ead及两行普通注释4ace资格，合并eba167d双parents/完整tree等于worker。真实PG+本地对象、当前完整授权双门禁、准确身份及固定回执、有限范围、真实重开和旧reader正常/拒绝通过；[首票证据](../lerna-04-content-snapshots/ticket-01-exit-evidence.md)保留各原pin及失败。最终两轴Standards0hard/1可选KEEP P3、Spec a0b0c0，必要架构F1关闭；受测完整check35工具/44TS/三版158+89+101两序双向及Get race22.361 native0。353组/289schema/65PID/518scope无live/residual；worker释放LOCAL槽，overlay/日志/cache/WT保留到整片清理，旧未知不动。

当前8/41不等于04整片退出；完整来源闭包、Snapshot、holder/物理清理和SIGKILL仍待。新准确push CI待核；下一frontier02，05–22仍依真实整片退出。

2026-10-04，首票检查点7c0bce5已真实push（nativeexit0），新CI待准确head核验。01独立8AC接受/mergetree已核，不把推送当CI成功。02已按实际ports/SQL相等采用交接并claimed；新worker独立worktree，持有唯一LOCAL执行槽。当前仍8/41，03–06未开始，04整片未退出。

2026-10-04，首票准确7c0bce5 CI37207013464已success，root读取两个job全部steps和完整日志：35Node/44TS/158+89+101两序双向、Recovery normal23.346/race52.987、动态133 Component正常52.653及race60组61.550＋73组24.421、Source13.205/16.724、Contentfixture0.053/1.178、Local0.048/1.065，54条旧manifest实际OK。见[CI](../lerna-04-content-snapshots/ci-verification.md)。02正在独立worktree实施，新代码不在7cCI内；whole04继续8/41。

2026-10-04，04票02继续claimed。隐藏祖先撤销场景已实际business red→green（Component0.363/0.563s，native1/0且group absent）；管理入口初始red是API缺失编译失败，不冒称业务red。两处旧预期调整及准确fullRef责任分类已由授权Astra high裁决、root采用[决定](../lerna-04-content-snapshots/ticket-02-oracle-decisions.md)，实现与完整验证仍待。唯一LOCAL执行owner仍02；root未并发构建/测试/DB，whole04保持8/41。


## 切片04来源政策票接受并整合

2026-10-04，票02七AC resolved，交付651036/产品4c621/工具5c024，正式merge6f88740双parents及完整tree等于worker；[逐AC证据](../lerna-04-content-snapshots/ticket-02-exit-evidence.md)保留所有实际测试/审查与unknown范围。完整170 normal/race每模式一次，fixture/Decision/base race/locked check/module verify通过；最终双轴0hard/1可选P3 KEEP、Spec a0b0c0、架构0necessary。5 schemas/8 roots、旧Z与空selector per-Close unknown保留，未cleanup。当前15/41，whole04 in-progress、完整1.2广告OFF；03/05直接前置已满足。新准确push CI待核，旧首票7c CI不替代新源。

2026-10-04，02接受push904ec5f的准确CI37227467190两job/全部steps success；root读取37Node/44TS/158+89+101两序、完整170 normal/race五组、Recovery25.988/61.392及fixture各包，见[CI](../lerna-04-content-snapshots/ci-verification.md#票02准确904ec5f的ci)。早期pending保留历史，03/05已独立claimed，LOCAL按自然TDD边界串行交接；旧unknown不动，whole04仍15/41。

## 切片04上下文未派发观察差量

2026-10-04 19:53 UTC，采用[窄oracle决定](../lerna-04-content-snapshots/ticket-03-no-dispatch-oracle/README.md)：冻结1.1 Decision.Get没有not_found，仅把其result_unavailable作辅助，以当前授权原Command not_found、可信fixture准确无binding/dispatch和真实Decide入口有限观察共同证明。原handoff字节不改、七AC不减。03部分9bb61d4真实完整上下文三owner重开/规则全文回显normal1.561，echo reserve拒绝与正常对照1.902通过；入口完整观察及其余容量/恢复/竞态尚未完成，不标resolved。05部分8986829正文封闭normal/race通过，实际删除和剩余holder/并发/孤儿出口仍在实施。当前15/41AC，所有22切片授权继续。

## 切片04选择依据与实际holder绑定检查点

2026-10-04 20:11 UTC，采用[确定性选择决定](../lerna-04-content-snapshots/ticket-03-selection-strategy/README.md)：单独版本固定选择材料正文语法，实际计算并核对可信Selected；include/omit均保留compiler实际处理及完整来源。原rule/2、字节策略和五动作权限不变，撤权分别验证。该决定无执行声明。

03部分45eb007已在独立WT固定反序集合完整投影修复，真实反序与原normal3.979通过；未完成全部容量/恢复/来源出口。05部分70d746e固定原发布介质FD身份至准确version、seal与清理责任，错误root不能代原holder ACK；exactnormal0.382通过，相关race Component2.758/Local1.116两个测试命令通过。该组合末尾错误迁移文件名导致原native exit1并永久保留，单独正确哈希命令exit0；不把组合改称exit0。首次Seal错root、真实第二holder、跨进程、孤儿竞争及旧版本升级尚待验证。双方部分提交尚未合入主集成；15/41AC与完整profile不广告状态不变。
