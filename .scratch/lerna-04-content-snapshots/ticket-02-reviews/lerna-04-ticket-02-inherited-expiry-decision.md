# 04 票02：新派生版本继承祖先自然到期责任

2026-10-04，固定source `57ea60c628f82140f5700503107d3e2fe4a86dec`，base f05b2f1068958ba6b63e6c1dc58d6cd1684446cc。先独立重读management.scheduleAdmission、service.Put实际cap/保存/调度、registeredClosure及PG Descendants，再读 `/tmp/lerna-04-ticket-02-spec-57ea.md` 第二项。本稿为必要决定，不是源码修复或native通过证明；未读Standards轴、未运行DB/build/test。

**采用：接纳时同Tx为准确后代登记“原祖先到期→这个后代”的耐久定点义务，复用现PolicyChange/责任表和原祖先policy_propagation Job。旧传播watermark、0001/0002、原费用/正文/receipt均不改；无需新框架或SQL迁移。** 需要PolicyChange内部JSON一个可选准确目标字段、领域scheduler/推进的窄分支及受信只读观察接法；不是仅改Due一行。

## 1. 缺口与不能采用的捷径

service.go:211–252核完整来源，祖先ValidUntil仅收紧本次policyBefore，祖先RetainUntil与source.CurrentRetainUntil收紧有效保留cap。:304只调目标policy的ScheduleRetention。management.go:448–469首次排目标自然到期时固定watermark；PG Descendants:250限定generation<=watermark。晚入场的D在A旧watermark之外，A.ValidUntil早于保留cap时，D会立即失去新使用资格，却没有按这个到期原因登记自己的holder责任。

不能扩大A旧watermark、重写旧change、重新设置now+WorkBudget，亦不能把A.ValidUntil永久写入D.CurrentRetainUntil来掩盖：有效许可期限和单调正文保留cap是两个既有概念。不能只提早D自己的Due却仍用D自己的长期有效policy做affected分类，否则仍会not_required。不能期待查询时补Job或等很晚的D保留期。

## 2. 最小持久形状与准确身份

在现内部 `PolicyChange` 增加可选 `AdmissionTarget *ContentRef`（名字可局部细化）。nil保留旧source-wide变更/自然维护语义；非nil明确表示**该change.Policy准确祖先对这个准确后代的定点到期义务**，不是另一个普通政策修订。无需复制/伪造policy.Ref为D：Policy.Ref继续A，subject/purpose/revision继续真实祖先政策，完整Ref失配分类仍对A进行。

- 原change key算法与ObservePolicyChange不动。新key单独固定域前缀（例如 `content-inherited-expiry-1`）散列闭合canonical tuple：D完整Ref、A完整Ref、原subject/purpose、原policy revision、原有限due及其原因类别。保存相同key必须核完整固定tuple；异内容conflict，不覆盖旧截止/进展。
- 表中object_id继续由Policy.Ref得到A，因此复用**真实已存在A版本**的policy_propagation Job及NextPolicyWork；不造D伪source、不需改SaveChange数据库分区或新Job类型。AdmissionTarget只需现JSON body持久化；nil旧记录双读保持。
- 固定Due采用适用祖先原policy.ValidUntil、RetainUntil及祖先原CurrentRetainUntil的最早截止；D自身CurrentRetainUntil由其原目标维护义务覆盖。即使两者同日重复登记也必须幂等；不要为压缩几行责任丢失不同祖先/原因身份。
- 固定Deadline=该Due加**首次固定的有限维护预算**。优先沿被引用原policy change的 `ExpiryDeadline-ExpiryDue`，验证其正值/上界；缺该原事实时先完成受控legacy维护回填或fail closed，不凭当前worker新配置猜旧预算。新义务第一次提交后，任何重试/alias/reopen/换worker都读原Deadline，不重算。
- 上述截止与预算只是维护工作期限，不授权业务使用；费用/使用权不因维护pending扩大。PolicyChange.State到期后走现pending/residual等内部语义，holder清理仍pending，不提前删除。

## 3. 同Tx接纳、重复与多祖先

新D接纳的原短Tx中，使用已经核验完整≤64闭包与对应原主体/用途准确policy/source记录，在SaveVersion/SaveSources之后、accepted/必要Jobs同提交之前，为全部适用祖先保存上述义务并触发A原Job。触发due必须保持现NextPolicyDue最早规则；单调work revision沿A，不拿各policy修订直接当Job全局revision。任何一项失败全部rollback，不能accepted后best effort补义务。

每个D最多64个祖先，不截断；共享祖先按准确版本去重。中间版本同样是适用祖先。可以将已读事实通过一个消费方窄调度方法传入，也可scheduler在同Tx有限重读；不需要向每个Service caller暴露任意SQL或新增注册平台。目标自身维护继续原scheduleAdmission，继承义务不能被其 `State != complete` 的早退跳过。

原Command重放仍在原优先分支只还固定receipt，不加Job。新Command同声明关联不新建正文/出版责任；若当前有效cap被真实收紧，必须确保更早的自身维护截止已有义务，必要时只追加该更早截止的确定性维护身份（截止来自收紧事实，预算不从重放时刻起算）。同一D/A/policy/cutoff已有义务不重写、不反复Trigger新revision。首次接纳登记不应依赖后续alias来补齐。

多祖先各自保留原截止，不能让较晚的B覆盖较早的A。D目标cap更早时，其原cap维护先封新使用并登记holder；后来的祖先义务只幂等补充原原因。新宽policy不延长任何已收紧CurrentRetainUntil，也不清除已确认的责任历史。

## 4. 到期推进与当前权限分类

advanceJob遇AdmissionTarget时**不调用Descendants/不扩大watermark**：只锁准确A和D、核D仍为原准确版本且不可变完整声明确实含A，按原D.Subject/Purpose核适用性；然后登记这个D的责任。它是已登记的一个有限已知holder目标，不是再遍历未注册后代。普通nil分支仍沿原冻结水位分页。

实际当前资格不能只把旧Policy的到期flags应用到D：用现LockPolicy读相同A/原主体/用途的当前准确政策（过期政策也需可被管理读取，不能用CheckPolicy返回nil后丢掉分类信息），fresh DB Now检查完整Ref、五动作、ValidUntil/RetainUntil和准确A/D单调caps。

- 当前准确原保存依据确已失效/绑定错误或适用cap到期：D原保存主体/用途对应的body_cleanup=pending，保存准确A来源及D责任/原截止；read/process/disclose-only变化不自动等于save撤销。
- 其他主体/用途的policy不授权删D；使用核对可记录，不能收紧无关D全局cap/触发全局删除。对错误完整Ref比较actual A，不能拿policy.Ref与D比。
- 原policy在到期前已被合法较新同绑定政策续期、D原cap未到期：原自然义务仍是一次真实核对，但不能只因历史旧policy已过期就误称当前save无效。可明确记录本次not_required/当前依据，保留旧义务身份与历史；**必须已经有较新政策的独立原期限责任覆盖D**。该较新policy安装的冻结水位若已包含D，原变更/自然维护负责；若D在它之后入场，本节接纳登记针对这个较新revision的定点义务负责。缺覆盖则保留待核对，不把当前宽policy当无限期保存许可。
- 早于原Due的主动revoke/期限收紧由其真实新policy change传播覆盖已存在D；D若晚入场则当前gate直接拒绝或登记新revision的更早义务。原定点义务不改key/Deadline、不删历史；不会为了收紧而复活旧执行资格。

与已采用Deadline修复合并：锁等待后及最终提交资格检查本义务**原Deadline**。预算已过期时，有限记录这个已知D holder及residual/original_deadline_expired，不冒“全传播complete”；不得用当前worker的WorkBudget重开时间。若DB/Claim/管理资格使本次不能提交，保留原耐久责任等待有资格的有限核对；不能吞错误。现PendingChanges按64个change有限取页、NextPolicyDue继续余项，A后代总数不限于64。

## 5. legacy与观察的最小兼容

受控旧writer停止、0002追加及有限回填前提保持。RebuildSources在为每个原D恢复原Sources/维护时，也针对其实际完整祖先及原保存主体/用途登记遗漏的定点义务。使用已保存policy/版本的原截止和原维护预算；原截止已过去则登记已知holder残留，不设置新的future deadline。若旧policy缺原维护预算，应由现restoreLegacyMaintenance首次固定的事实提供；处理顺序可有限两遍/续页，不能靠后代先后排序跳过祖先。回填完整标志只有自身policy页和继承义务都耐久完成才成立。

现JSON表可保存新可选目标字段、key和状态，**无需新增SQL migration或改0001/0002**。legacy尚无该字段视为旧source-wide义务，不能当已有D定点覆盖。已有自然change/receipt/Record.Sources/TupleDigest/object key全部原样。

为公开验收提供最小受信只读发现方式：在Manager增加按准确D查询继承义务的有限观察（或现Observe接口的明确过滤参数），由PG按本owner+AdmissionTarget准确完整值分页返回原keys，再沿ObserveChange查责任；缺权限不泄露。可用现JSON body读取/过滤并有界owner分页，不建立泛查询语言/新客户端profile。数据量需索引时以后基于实际事实追加，不能现阶段猜必须新迁移。业务oracle不直接SELECT私表，不依内部调用次数；query不创建任何义务。

## 6. 必要公开red/green与正常出口

1. A先正常published并形成自然expiry水位；A.ValidUntil短于retention。随后D真实合法入场并published，确认D generation语义在旧水位后（通过公开流程顺序及原管理观察，无私表oracle）。到A原ValidUntil，D新使用拒绝；无任何额外query触发写入，有限推进后受信管理观察有D的原截止/准确source责任pending，独立原字节仍在，不能报erased。57ea应缺该及时责任；修后重开仍有。
2. 同题正常对照在全部期限前允许D使用；A许可提前合法续期时，原到期核对不误删D、较新原义务可见；A/B多祖先以更早者触发，B不推迟A。D自身更早cap到期也不被祖先长许可或重开复活。
3. 原Command重放、新Command正常alias、重开/重复Step均保持同义务key/原Due/Deadline，不能每次加一轮预算；body/原receipt/published历史不变。迟到work past originalDeadline保持known-D residual，结合真实锁等待修复。
4. 不同主体/用途及wrong-fullRef反例按已采用分类；早期revocation覆盖D，新revision后才入场的正常链仍有定点维护；所有正常/拒绝各自公开观察。
5. 真实01旧数据升级含晚于A水位的D、已过期祖先、超过一页policy/义务；有限回填中断重开原key/cutoff不变，不把旧unknown补成0或刷新到期。原缺配置/不可核验资料保留残留，不假完成。

本决定直接补现有维护责任缺口，不引入03/05/06或16隐藏依赖。solefixer据最终实际对象落地后仍须独立normal/race/恢复与审查；本稿没有执行证据。

## 7. 窄维护裁决：page readers保持具体实现

独立依据已读固定 `adapters/postgres/content/management.go`：PoliciesForVersion、Descendants、Responsibilities各有实际limit+1/page-cursor消费；UnindexedVersions读另一种legacy页。它们共享rows.Next/Scan/Err/Close的机械形状，但row类型、closed v.Decode或内部JSON、cursor构成与超额行处理不同。**当前KEEP具体reader，修真实Rows.Close原因即可；重复形状仅Worth，不是必要提取。** 不读另一轴报告来替代该源码判断，不增加审查门禁。

必要修正须覆盖正常末尾、limit+1提前停止、Scan/Decode失败等所有返回路径：真正调用Close并把其错误与原primary/Rows.Err聚合，保持errors.Is/As可判，不让defer丢cause或覆盖primary。不因rows.Next到EOF通常自动Close就假定提前停止也等价。针对真实消费入口/窄机械driver seam验证关闭原因；注入错误只证明机械因果，不冒称真实PG nativeClose故障。

deletion test：提取一个泛型“页框架”不会消除四类具体查询/解码/游标知识，只会把它们移进callback并扩大interface。当前locality由每个实际PG reader掌握，leverage不足以支付新框架/adapter。新继承义务的只读分页沿同一正确Close习惯实现即可；以后确有稳定同型消费者且持续维护分歧，再考虑包私有最小helper，不预建。
