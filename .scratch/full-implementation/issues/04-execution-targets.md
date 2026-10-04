# 04 execution-targets

Status: partial
Implementer: execution_impl

准确原 Operation、Attempt、Effect、ControlGate/Window、真实 StartBarrier、费用和资源责任已实现。完成范围见[Execution 参考实现](../../../internal/execution/README.md)及[实施覆盖](../../../docs/architecture/engineering/implementation-coverage.md)。

## 完成依据

真实 SQLite/PG 的公开执行与恢复、原 Claim/提交未知、当前控制/once、资源 epoch、真实文件 Root/CAS/哈希/刷盘及目录同步 EIO 后真值核对有独立证据。原 prepared 编码耐久修复后保留原已准备字节和 digest；恢复不重编码、不新建 Attempt，不清除原 unknown 或费用。

本地扩展已有真实实现和验收：工单12完整模拟点击/滑动/输入/返回及18手势、Task/Gov/Execution当前许可；工单13锁定 Linux 受限WASI、原程序/namespace/CAS、真实隔离进程退出、SIGKILL journal恢复；工单16独立 SQLite Executor 的TLS准入/有限GrantLease/签名Source与原迟到用量。独立设备Task完整写入/读回/Result/费用/join重开在9f533523+c1ea准确pin上两库通过，生产叶已准确集成，适用新负例与最终固定同图资格仍另计。不得再把上述本地代码列为未实现。

StartGatePreparer正向prepared入口、CurrentStart最终强核与原sent／unknown／终态收尾范围保持。原窗口、Claim、来源、角色和八暂停谓词不弱化；原preparedPause两库race已真正取得因果资格，其他历史失败与新的fixture观察边界分列。完整设备normal、适用guards与Saved五反例×两库race通过；当前根完整Suite仍待。

此广泛工单仍partial：最终合成提交全故障检查、真实Android/iOS/其他平台、任意恶意插件及通用多物理安全重试、生产容量和介质资格另需验收。单物理恢复合同不承诺多次效果；未来开放能力须有准确驱动能力证据，不能自动重发。

Saved公共拒例的历史结果保留：原root454整轮4PASS／read_before_write SQL FAIL；c6d五反例×两库normal10PASS299.396453s，随后race首SQL76.168772s／whole76.355284s proof expired而余9NOTRUN。rx源SQL120.814652s PASS／PG123.744358s FAIL，原read在五秒窗口只余14ms时永久not_started；hmp PG130.623157s FAIL发生在第四Brain Decision已cancel／send0时，设备两action已有StartedAt／Result。该历史泛化取消的原typed gate原因未持久，不推断为同一个expiry原因。最新9fqx受测Root02cb＋精确7生产路径／source911 manifest1d2f7bd7…／race binary3a75278a…保原公开测试、主体、Source／Grant、断言和5秒窗口；在Tx外按原Task／实际祖先元数据准备准确当前材料，原提供证明后以空consumer provider取得同CopyID最新Current，避免旧explicit证明遮蔽。五场景scope_artifact_source、credential_revoked、read_before_write、different_device、wrong_native_bytes各SQL／PG共十项实际PASS，逐项noSKIP／noDataRace／原源与binary、driver及环境摘要稳定；SQL／PG耗时分别120.314535／143.009623、117.387776／139.664585、125.831579／145.363275、132.189945／152.084879、118.056649／143.076346s。最后八项08:15:14.441075 UTC整体EXIT0／1075.865s。Root独立十项索引 /workspace/harness-dev-environment/saved-consumer-remaining-eight-race-20261004T0757Z/root-independent-all10-qualified.json（SHA07239f97…）；正式叶17662e5仅metadata commit、全部911原字节不变，事实 /workspace/harness-dev-environment/saved-consumer-leaf-truth-qq_kn9_g/index.json（SHAbbe3b521…）。当前CODE 58c9898af58babce7170cb37cbbdfc63d693af9e精确集成与这批受测Root02cb＋叶17662e5资格分开；不覆旧FAIL，不延TTL，不把它当作最终全项目测试／浏览器已运行。

prepared当前父暂停门禁的准确后继资格：原871 SQL parentpaused@9／Scope Allowed=false后55ms仍Start且native read111B为真实历史FAIL，旧PG prepared仅use_window_expired。正式VerifyStart当前Task／parent强核的四normal153.070004s在两库取得真实remote_parent_scope_denied与原Attempt不变／零Start的资格。其后三次race240.307176／240.305543／240.288231s整体FAIL保留；remote_create是configuredAgentStep的receiver JobKind标签，不能识别为具体首次POSTCREATE方法。真实已接纳Incoming@2 preparing，无childTask／Op；最后一轮exactPolicy initialequal／skipTrue已到execution-intent reserve APPLIED、Put无持久receipt时耗尽2m observer，具体IO／性能与历史typed gate原因未证。对应原Parent／service证明30s仍有效，不能混同Saved五秒消费门禁。准确TaskPolicy工厂与两项fixture已合Root7c65，原task policy值／digest保持；最新受测source902的原prepared暂停八谓词两库race146.115743s／163.071059s actualPASS，均真实forbidden:remote_parent_scope_denied／sameAttempt／StartedAt空／Resultnil／not_started，并实际join／原cfg-token重开。新边界明确为fixture setup2m＋独立observer2m，替换旧wholecase2m；parent5m／child3m／command1m／Use5s／parentProof30s业务期限不变。新共享false测试分支尚未以本轮race验证，留给最终整套检查，不能由旧normal四项或新true分支推过。正式精确四路径与过程见 /workspace/harness-dev-environment/prepared-observer-fixture-formal-leaves-20261004/handoff.json（SHAef9157af…）；旧阶段只读 /workspace/harness-dev-environment/prepared-exact-policy-original-readonly-wkf35u2o/phase-summary-index.json（SHA6d15d8d3…）。

历史prepared第一次race240.307176s未取得pause资格：原Rooted0／source897 c01ed55c／binary8574b49e实际EXIT1。日志remote_create只是receiver Job标签，handler内部HTTP POST超时，不确定具体方法；真实create已accepted／Incoming preparing，无子Task／Op／pauseProbe。原normal153.070004s通过保留，后继真实暂停race146.115743／163.071059s见最新资格，不回填原FAIL。原30s证明与Saved5s证明分开，未持久的历史typed cause不猜测。

当前限定本地实现已完成逐项定点验收：远端Agent／独立设备的正常报告、current actor与父控制、无子Task永久拒绝的三层Closure／原once与费用责任、现代默认Goal Form、原关闭证明复用及Saved五秒证明消费门禁均有准确证据。Saved最新同一911路径源／race binary的五场景×SQL／PG十项actualPASS，正式7路径叶17662e564d121732b0fbac8ddbed938a7a7baabb已合当前CODE 58c9898af58babce7170cb37cbbdfc63d693af9e. 17按已开放有限静态profile记resolved；09仍partial等待最终固定版本完整检查、原cfg浏览器重开／全生命周期、两份独立全图审查与发布步骤，07保留浏览器验收待项。新prepared共享false测试分支的观察边界仍由最终整套检查取得资格。代码集成和各旧source行为资格分开，不能声明当前根全套已通过。真实账户／公司身份／开放自然语言质量／物理设备／其他OS／规模与多AZ为另外明确的未验收范围；旧FAIL／SKIP／NOTRUN及原业务身份、权限和期限保留。

统一阶段说明：当前CODE 58c9898af58babce7170cb37cbbdfc63d693af9e；本文实际通过只按所列原source／binary／selector及数据库制品限定。最新Saved十项、NoChild／Pause／现代Form／Closure后继资格已取得；最后完整检查／浏览器／全图审查尚未结束，旧失败／未跑记录不回填。
