# 23 foreign-content-registration

Status: partial
Blocked by: 01, 02
Implementer: memory_impl

依据：Memory 的跨 owner reference_intent／register_copy／held_copy_gate 合同，以及独立 Executor、外部 Agent、第二系统实现的准确来源交接。

实现明确且可关闭的 foreign Content 端口及宿主路由；源 owner、ContentRef、hash、版本与用途均不改写。网络登记、原字节及有限当前许可证明在 Tx 外取得；本方 Tx 内只核原 holder、准确范围、期限、已知撤回与来源门禁。nil 端口仍拒绝外部来源；普通镜像失联不开放新读取。派生来源与引用登记、清理分别恢复，原登记丢答复不换 copy_id。不得仅放宽 owner 比较，也不得使外部原件成为本方 Content 权威。共享参考进程的静态路由与签名密钥可显式装配，不声明跨库瞬时关闭。

## 完成依据

真实两个 owner／独立数据库证明原登记恢复、当前用途允许、跨租户／主体拒绝、原源关闭及失联拒新使用；证明独立设备输出能沿准确外部 ContentRef 被云端 Task 授权读取。其余跨 owner 材料使用均沿此端口，不建立隐藏捷径。

## Comments

2026-10-03：独立设备实现核对确认只读 bytes 路由不足以通过现有同 owner gate；从设计已有跨 owner 合同补入必要前置切片。

领域切片 `f0a9eff` / `683bb2f` 已实现原 reference_intent+Job、固定登记/释放命令、
准确字节+held gate、有限当前签名证明、control/use 分离、源关闭高水位、
原 owner 来源键、派生限制交集以及独立 Memory 元数据 holder 收尾。
纯 Tx `SourcePolicySnapshotTx` 用于独立 Executor 准入时冻结原 PolicyValues 和实际主体代次；
全部登记、当前查询、读取和释放在 Tx 外。nil 端口、旧凭据正文、失联和已知关闭均拒绝新读。

真实两 owner / 独立 SQLite 与独立 PG 数据库的公开接口矩阵通过：
SQLite foreign race 58.482s；PG foreign race 61.092s；完整 Memory normal 106.758s。
源原登记丢答复、原 intent 真实 COMMIT 后丢答复且零 RPC、原库重开、
正文 control 证明反例、跨租户/主体、较新凭据仅允许收尾、来源交集及 Memory 删除
仅清理自己的引用元数据均有实际正反例。具体命令、代码状态和边界存于
`/workspace/harness-dev-environment/foreign-content-verification.json`。

领域后继真实消费已经取得独立证据：默认App.Run HTTPS→Native Memory两Source库资格7a050797/245.487s；设备SourceClient、云端Task实际写/读回/Result/费用/join重开在9f533523准确overlay与c1ea binary下分别SQLite36.367635s/PG51.996529s通过；远程双方own报告／三层Closure／原Task和Grant费用／必要proof与Job收束／join原cfg-token重开已在fresh SQLite父库103.121656s与PG父库109.541635s通过，旧r5额外unused证明过期失败独立保留。原生产叶已准确集成，适用NoChild／Pause／Saved故障后继均有准确资格，最终同图检查／scope审查与未开放offline profile仍另列，因此广泛工单暂partial；不以领域矩阵冒称整链，也不把这些已经取得的消费证据再列为无实现。普通跨 owner 使用为在线有限证明，未开放离线新使用或
跨 owner 独立派生保留证明。清理不把本地实际删除称为源端全介质 complete。

后继精确双地点门禁资格：becf4ea已合Root57980ff，ReadContentBytes在Tx外分别准备requested与实际Memory.Location的同ref／actor／purpose当前证明，再由原Memory.ReadBytes完整门禁核验；不改Policy／主体或放宽地点。其public双库36.701s／positive race183.06s／corrected focused221.185372s PASS与旧whole guard321.71s FAIL分别记录，独立集成索引 /workspace/harness-dev-environment/source-integration-foreign-read-locations-kszdns7p/verification.json（SHAc5f176f7…）仅compile／vet元数据。旧Saved首条execution_arguments／device proof过期FAIL保留；后继最新911同源十项race实际通过准确consumer freshproof强门。终态／offline与全部第三方profile资格不由此推断。

当前限定本地实现已完成逐项定点验收：远端Agent／独立设备的正常报告、current actor与父控制、无子Task永久拒绝的三层Closure／原once与费用责任、现代默认Goal Form、原关闭证明复用及Saved五秒证明消费门禁均有准确证据。Saved最新同一911路径源／race binary的五场景×SQL／PG十项actualPASS，正式7路径叶17662e564d121732b0fbac8ddbed938a7a7baabb已合当前CODE 58c9898af58babce7170cb37cbbdfc63d693af9e. 17按已开放有限静态profile记resolved；09仍partial等待最终固定版本完整检查、原cfg浏览器重开／全生命周期、两份独立全图审查与发布步骤，07保留浏览器验收待项。新prepared共享false测试分支的观察边界仍由最终整套检查取得资格。代码集成和各旧source行为资格分开，不能声明当前根全套已通过。真实账户／公司身份／开放自然语言质量／物理设备／其他OS／规模与多AZ为另外明确的未验收范围；旧FAIL／SKIP／NOTRUN及原业务身份、权限和期限保留。

统一阶段说明：当前CODE 58c9898af58babce7170cb37cbbdfc63d693af9e；本文实际通过只按所列原source／binary／selector及数据库制品限定。最新Saved十项、NoChild／Pause／现代Form／Closure后继资格已取得；最后完整检查／浏览器／全图审查尚未结束，旧失败／未跑记录不回填。
