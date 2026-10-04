**正式采用：2026-10-04。** 前置01–03完整退出，准确03退出提交5fbb1a020658093266b4346370571370435f9630、受CI验证47ebce1/run37194868564 success。用户已授权按建议决定粒度/依赖并实施，不重复确认。root全文读取条件草稿和Astra high的具体接法，并实际核对最终exit SHA：与1aa条件pin相比仅12metadata路径改变，全部真实产品/工具/Schema/测试对象相等。下列原准备时pending描述保留为历史，当前接法由本采用及final-api-handoff确定；此时04尚无实现/验收证据。

# 04 垂直票据纲要草稿

2026-10-03；仅准备，未发布、未claimed、未运行04。配套决定见 `decisions.md`。所有首票以前置**03整片退出、准确最终SHA和实际端口复核**为硬门槛，不能因为已有/tmp设计就提前实施。下列是6条可验收行为链，不是最终issue/完整AC措辞。

## 依赖图

```text
03 whole exit + final-SHA recheck
  └─ 01 准确版本从put到get
       └─ 02 来源权限交集与撤权封新使用
            ├─ 03 必要上下文到真实规则Decision
            │    └─ 04 来源变化与完整派生登记
            └─ 05 正文清理、holder残留与孤儿关闭
                 └─ 06 发布窗口杀进程与原身份恢复
```

依赖理由：02消费01真实版本与字节；03需要02当前资格和派生发布；04消费03真实编译/派发与来源记录；05需要02封使用和清理责任；06明确验证05的失败清理/孤儿闭环，因此需要05。06不依赖03/04，无全片审查/广告的隐藏依赖。若实施后发现06只测原发布恢复而不消费05，就拆清验收并移除假边；本草稿默认06同时测“恢复或孤儿清理”两条真实出口，保留05真边。

root另在全部6票resolved后做全片审查、完整机器清单/广告、旧版回归与真实CI/证据整合；不是塞入06的一项无法并行完成的final-close验收。

## 01 — 原内容版本的耐久发布与准确读取

**使用者可见结果：** 提交有限正确字节得到固定accepted，稍后按原准确引用获得published及完全相同字节；错误声明或同版本冲突不能产生另一份内容。

- 同票形成准确1.2机器合同、Go/TS类型/codec/共同黄金和真实Content入口；保留1.0/1.1的源码、拒绝集合、摘要与协商行为。完整profile未退出前不广告。
- 真实PG Content owner准备事务把有界staging字节、声明、固定receipt和必要Job一起提交；对象适配器真正文件Sync、不可覆盖安装、目录Sync及独立回读后，短事务发布；读取preparing与published有准确区别。
- 受信主体与准确owner绑定；基础显式有限read/save/process政策fixture随正常路径交付，不先做允许所有的“以后补授权”服务。范围/源码上限及缺配置硬失败。
- 原Command幂等、原Content版本双重身份、hash/length/media_type/版本冲突、canonical base64/有限range、空字节及真实重开都有行为测试。篡改/缺失对象返回integrity/unavailable而非另找同名最新。
- 同票完成新版command.get真实原账本读取；旧只读bridge仅无损可表达范围，不改旧reason。首个新owner迁移空库和重复/checksum、旧03/02数据不受影响的必要验证。

它不是“先写合同/先建表/先写文件adapter”三张横向票。正常发布读回是第一张完整示踪链。

## 02 — 派生内容受所有来源约束，撤权先阻止新使用

**Blocked by：01。** 使用者从两份真实来源生成派生版本，只能在共同获准用途/最严保留限制内读写；任何来源撤权立即阻止下一次新使用并保存清理责任。

- 显式、耐久的测试政策fixture分别裁决read/process/save/sync/disclose，精确subject/resource/purpose/policy revision；跨tenant拒绝、无合法用途交集拒绝、跨Content owner派生在当前profile准确unsupported。
- 派生发布继承全部登记来源闭包，有限且不截断；原声明不变，当前资格仍重新核验。仅read获准不足以process/save/disclose。
- 并发撤权与发布在当前单Content owner最终Tx中裁决；先撤权则不能发表获准新派生，先发布则其后新读取/处理被当前政策阻止。撤权本身不是擦除，持久清理Job/holder责任可查询、重开保持。
- 用真实Content字节及受信policy变化验证，无权主体不泄露存在/元数据/正文。允许主体的正常发布读取对照必须存在。
- 明确外部put来源仅是声明；本票验证声明如何约束使用，不宣称推断出外部真实生成历史。06真实Grant尚未实现。

本票的垂直出口是“新使用被封且清理责任确实耐久”；物理清理完成由05实现，不在此虚报erased。

## 03 — 必要上下文完整进入规则Decision，溢出阻止派发

**Blocked by：02。** 编译器从真实Content材料和明确fixture目标输入构造准确Snapshot，经实际03 Component入口得到规则Proposal；超限时保留必要约束并明确context_overflow。

- ContextCompiler归domain/task的上下文职责，调用03 durable fixture dispatcher代表尚未实现的Task输入/派发，不创建假Task或第二个Orchestrator。
- 目标、必要条件、控制、预算、期限、未决效果完整保留；固定版本选择额外材料，省略/缺口有界记录；准确组件、策略、材料和派生关系持久。
- 按 `final-api-handoff.md` 的唯一条件接法：完整mandatory-context/1 Content作为MaterialRefs[0]，原闭合Snapshot外壳/manifest固定其准确ref，真实Content-backed Source/Publisher接原1.1 Decision。rule/2 candidate_result的公开artifact去固定前缀后必须与完整文档全字节相等；不是只检查必要字段的裸ref。外壳、manifest及规则必要产物实际经Content发布/读取，原fixture Seed正文库不冒充Content。Compiler全部processed来源在文档与真实Content闭包保留，worker实际processed单独准确声明。
- UTF-8字节策略容量+预留输出及有限总读取/重建截止严格执行；计入外壳、lock、manifest、所有材料与第一材料完整回显及Proposal开销，兼顾材料/条件/来源数量上限。默认8KiB输出预留不自动扩大，回显输出不容纳也须在派发前overflow。正常路径真实送达Decision且读回Proposal；溢出路径没有Decision接纳，独立边界观察不能用内部mock计数代替。
- 保留真实模型未装配的准确限制；不造假模型调用数或声称已验证供应商token预算。固定1aa实际端口足够，04不新增1.2 Decision、不修改1.1闭合字段/原规则/Prepared。发布本票仍硬等whole03 exit SHA及API对象复核；实际差异只针对本项重新判定。
- Snapshot和准确输入重开后可读取，同名新版本不替换已固定材料；当前撤权仍阻止新处理。

## 04 — 编译中来源变化与实际摘要的完整来源

**Blocked by：03。** 在读取后、发布前真实改变来源资格，编译不能提交混合/未经复核的材料；摘要输出只披露一项引用时仍受所有实际处理材料约束。

- 使用有限同步点驱动真实policy撤销/到期/正文不可读；Snapshot字节最终发布事务重核准确sources及政策revision，有限重建或返回明确gap，不能无限重试或悄悄缩小必要条件。
- fixture目标/控制修订CAS变化阻止派发旧Snapshot；明确这是fixture自身的真实持久竞争，不等同05 Application Task验收，也不声称Content+Task跨owner原子。
- 确定性摘要策略实际读至少两份Content，记录完整processed来源及准确转换版本/输入输出hash；最终仅披露其中一项，另一受限来源仍限制输出使用/保存。读取后未展示的材料不能从processed集合删除。
- 验证单纯出现新版本不篡改原准确旧版本；撤权/缺字节与要求current-head前态改变分别处理。每个竞争/拒绝有允许正常编译和规则Decision完成的对照。
- 发布到dispatch之间再次撤权，真实源读取/处理门禁阻止新消费；明确检查点与撤销传播范围，不以共享内存锁假装所有owner原子。

## 05 — 合法正文清理、真实holder残留及孤儿责任关闭

**Blocked by：02。** 清理原内容后，获准查询者看到gone/证据不可回读；真实删除失败/离线holder保留准确残留负责方，原身份不能重建已清理正文。

- 受信管理seam先封新使用并保存清理责任，合法清理真实PG staging与本地对象正文后才确认对应holder擦除。Command版本去重与可披露最小元数据仍保持。
- 一个实际第二holder保存过字节，其停机/失败有真实独立观察；清理失败不冒充global erased，恢复后继续原责任，不造新源身份。
- 清理与发布/原重传/迟到worker写入并发，准确原版本关闭和对象层删除fence防止正文复活；新version不被旧清理删除。版本同名不能覆盖或误清。
- 真实有登记的未发布对象可进入孤儿清理；有界枚举、自有scope、精确key和状态前态，不能扫描他人存储或把已published活引用当孤儿。
- 缺权限与授权metadata-only gone区分；正文删除并不证明WAL/备份法证擦除。观察经Content/受信holder接口及独立对象事实，不靠私有PG表计数作业务结论。

05只消费Content来源/清理事实，不需要Snapshot、真实Task或Grant，不加03/04假依赖。

## 06 — 发布关键窗口的真实进程恢复

**Blocked by：05。** 真正杀进程后，原内容版本最终恢复准确published，或保留明确失败并完成/登记孤儿清理；查询不返回缺字节的published引用。

- 真实PG+文件对象adapter，父进程有限管道阶段观察“文件同步及目录安装确认后、元数据发布前”再SIGKILL；重开从原准备/staging/Job继续，不能重造内存输入证明恢复。
- 补正常发布、提交前rollback、元数据发布后reply loss、当前政策在恢复时失效等对照。每种故障明确实际边界，文件Sync成功不被冒称云端/掉电耐久。
- 发布前杀进程原key准确恢复；无法合法发布时走05真实对象清理/残留出口。父进程独立查看对象字节和公开Content/Command事实，既不能仅靠receipt，也不能只靠文件存在认定published。
- 旧worker迟到安装/完成不越过原版本关闭与对象删除fence；租约/epoch只证明库内资格，不能独自证明文件副作用停止。并发清理范围只限自登记资源。
- 有限进程生命周期、可信Clock、失败输出及cleanup归属；缺PG/对象root配置硬失败，不跳过。原03确定性设施仅按实际可复用代码使用。

本票只证明自己的故障链；全片广告、04来源/摘要票、真实Task/Grant结论不是本票关闭条件。

## 七项spec验收映射

| spec AC | 主要票 | 实际观察 |
| --- | --- | --- |
| 1 字节写后/发布前杀进程 | 06（消费01/05） | 原版本恢复或准确孤儿/残留；无缺字节published |
| 2 hash/length/version拒绝与正常读取 | 01 | 公共put/get、原receipt、独立准确字节 |
| 3 跨tenant/撤权读取拒绝、来源无交集 | 02 | 当前受信资格与真实拒绝，正常允许对照 |
| 4 强制溢出、零模型出口、必要内容完整 | 03 | 真实规则Decision边界有/无接纳；模型未装配限制明确 |
| 5 Snapshot提交前来源变化 | 04 | Content发布Tx复核、fixture自身CAS和有限gap/重建 |
| 6 实际处理全部来源 | 04 | 确定性转换真实读取两源，披露子集不放宽政策 |
| 7 正文清理/证据不可读/残留 | 05（06恢复补证） | 实际删除、gone元数据、第二holder独立残留观察 |

## 发布纲要前仍须做的代码复核

03 whole exit最终SHA后确认：Snapshot精确形状和1.1读取语义、真实source/publisher seams、Decision receipt账本类型/桥接、runtime的Claim/容量/关闭、迁移owner组织、公开故障设施位置。按最终实际差距修订票01/03，不预设不存在函数。04草稿的本地对象durability实现也须以实际OS/filesystem能力核验，不因当前环境README有S3 smoke就直接勾验收。

真实Task修订竞争在05通过Application再验；生产Grant接入在06，真实模型/供应商证据在其所属后续切片。04退出只覆盖上述准确边界，不能把准备或本机故障测试等同生产完成。
