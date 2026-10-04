# 16 independent-executor

Status: in-progress
Implementer: execution_impl, storage_impl

独立SQLite Executor不写云端Task，固定原owner/数据库/受信TLS端点，使用有限GrantLease及本机Authority/Resource/Attempt/Usage。准确入口与配置见[Executor](../../../adapters/executor/README.md)及[云端装配](../../../adapters/development/REMOTE_EXECUTORS.md)。

## 完成依据

独立cmd/executor setup/serve/migrate、真实SQLite/native File、签名Admission/ControlWindow/原peer身份、准确Source Policy/subject generation与所有来源交集、原SDK journal、原Use的Lease用量/签名补传。真实TLS双SQLite/source close/control不授正文/新generation仅收尾/过期cleanup/原登记丢回复、PG有限Grant预留和重复late补传不双扣、CLI子进程SIGTERM/join与未知介质重开都有证据。完整Executor race133.181s及准确历史decoder升级TLS54.939s、SDK10.767s独立记录，预置Authority/TaskRef fixture不能冒称完整Cloud Task。

真实Cloud Task模型/Source/原Use装配已完成后继切片：三POST/一设备read/原Task failed但费用USD0.00072 closed、真实signed freshcarrier nested同Auth成功/新Query、新Job或换roles/purpose拒绝；PG138.14s/SQLite99.826s race分别通过，旧221.298s账单head冲突FAIL保留。success-read Result SQLite25.603s/PG65.044s及原cfg/token/DB重开另有准确source。

最新完整write/readback正例使用原Task/条件/Grant而非预置Result：source9f533523fb86cccf2f92d29a7c20fe62d7694cb1+全部source_hashes、binaryc1ea7438…，SQLite36.367635s/PG51.996529s实际exit0且source/binary/driver稳定，PG noSKIP。4POST/2独立设备Operation、87B完整字面文件、2pass verified checks、不可变Result published、两方accounting_closed，真实join与原active-config/token/数据库/原Result重开。制品device-task-regression/saved-rule-complete-frontier-142g56e5/verification.json（b1162038…）及saved-rule-postgres-driver-o9_snwbx/verification.json（6879b48…）。

旧purpose／holder／Entry proof／policy缓存和Saved失败保留，不向旧Signed Scope追加许可。完整写／独立读回报告两库normal、12SQL／6适用PG guards，以及Saved最新同源十项race均实际通过；后继精确7路径叶17662e5已合当前CODE 58c9898af58babce7170cb37cbbdfc63d693af9e。原运行资格仍保source9f533／source911各自范围，最后当前根整套检查与独立审查待跑，所以工单暂in-progress；生产证书／真机／其他OS／离线长运行／容量／多AZ另有未验收边界。

Saved公共拒例的历史结果保留：原root454整轮4PASS／read_before_write SQL FAIL；c6d五反例×两库normal10PASS299.396453s，随后race首SQL76.168772s／whole76.355284s proof expired而余9NOTRUN。rx源SQL120.814652s PASS／PG123.744358s FAIL，原read在五秒窗口只余14ms时永久not_started；hmp PG130.623157s FAIL发生在第四Brain Decision已cancel／send0时，设备两action已有StartedAt／Result。该历史泛化取消的原typed gate原因未持久，不推断为同一个expiry原因。最新9fqx受测Root02cb＋精确7生产路径／source911 manifest1d2f7bd7…／race binary3a75278a…保原公开测试、主体、Source／Grant、断言和5秒窗口；在Tx外按原Task／实际祖先元数据准备准确当前材料，原提供证明后以空consumer provider取得同CopyID最新Current，避免旧explicit证明遮蔽。五场景scope_artifact_source、credential_revoked、read_before_write、different_device、wrong_native_bytes各SQL／PG共十项实际PASS，逐项noSKIP／noDataRace／原源与binary、driver及环境摘要稳定；SQL／PG耗时分别120.314535／143.009623、117.387776／139.664585、125.831579／145.363275、132.189945／152.084879、118.056649／143.076346s。最后八项08:15:14.441075 UTC整体EXIT0／1075.865s。Root独立十项索引 /workspace/harness-dev-environment/saved-consumer-remaining-eight-race-20261004T0757Z/root-independent-all10-qualified.json（SHA07239f97…）；正式叶17662e5仅metadata commit、全部911原字节不变，事实 /workspace/harness-dev-environment/saved-consumer-leaf-truth-qq_kn9_g/index.json（SHAbbe3b521…）。当前CODE 58c9898af58babce7170cb37cbbdfc63d693af9e精确集成与这批受测Root02cb＋叶17662e5资格分开；不覆旧FAIL，不延TTL，不把它当作最终全项目测试／浏览器已运行。

当前限定本地实现已完成逐项定点验收：远端Agent／独立设备的正常报告、current actor与父控制、无子Task永久拒绝的三层Closure／原once与费用责任、现代默认Goal Form、原关闭证明复用及Saved五秒证明消费门禁均有准确证据。Saved最新同一911路径源／race binary的五场景×SQL／PG十项actualPASS，正式7路径叶17662e564d121732b0fbac8ddbed938a7a7baabb已合当前CODE 58c9898af58babce7170cb37cbbdfc63d693af9e. 17按已开放有限静态profile记resolved；09仍partial等待最终固定版本完整检查、原cfg浏览器重开／全生命周期、两份独立全图审查与发布步骤，07保留浏览器验收待项。新prepared共享false测试分支的观察边界仍由最终整套检查取得资格。代码集成和各旧source行为资格分开，不能声明当前根全套已通过。真实账户／公司身份／开放自然语言质量／物理设备／其他OS／规模与多AZ为另外明确的未验收范围；旧FAIL／SKIP／NOTRUN及原业务身份、权限和期限保留。

统一阶段说明：当前CODE 58c9898af58babce7170cb37cbbdfc63d693af9e；本文实际通过只按所列原source／binary／selector及数据库制品限定。最新Saved十项、NoChild／Pause／现代Form／Closure后继资格已取得；最后完整检查／浏览器／全图审查尚未结束，旧失败／未跑记录不回填。
