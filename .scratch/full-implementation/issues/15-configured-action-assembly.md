# 15 configured-action-assembly

Status: partial
Blocked by: 01, 02, 03, 08
Implementer: storage_impl

依据：A1、A2、共同准入和准确 Capability／Binding／InstallLock；新增工具必须从真实 Task 路径进入。

将闭合驱动合同、明确配置许可和准确绑定装配至 Context、Brain 提案准备、Task 授权及 Execution 原启动。保留旧 File 行为和未结旧锁解释；先验证 GUI，再接 Search/Body、WASI。缺配置或当前权限时不扩大默认能力。

## 完成依据

实现、公开行为、真实负责方和必要故障正反例通过后记录准确提交与制品。外部资格单列；尚未完成的本地路径不得归因于缺凭据。未达成的目标保持 partial。

## Comments

2026-10-03：全项目范围复核后补入原实施任务图，未改变用户授权或领域裁决。

2026-10-03 GUI首片：development有界16项准确cap/binding注册，96KiB schema总额；完整显式Grant只development initialize导入且原ID不复活。Context非消费当前grant.check，原Snapshot/总lock耐久归档，ReadProposal typed原Observation投影与PreparedAction同Tx固化，Task UseTx与Execution Start原许可重核；旧File叶lock解释保留。真实App→HTTP模型→Task/Governance→Execution→独立手机目标及重开、cross-binding、click≠input、once/Context不消费、原Snapshot之后公开确认撤回当前head已经双库normal/race验证；准确配置/依赖/角色与开放边界在adapters/development/README.md。

2026-10-03 GUI首片验证：`go test -mod=mod -race ./adapters/development -run '^TestConfiguredGUI' -count=1`，实际SQLite/PG七个子例PASS290.484s；`-run '^TestPreparedGUIActionKeepsOriginalLeafLockAfterAssemblyChanges$'` 两库race PASS138.953s，公开Task准入后重开并增加binding改变总lock，完整原意图/叶lock不变且目标一次执行。ServiceAuth设备查询不扩权。原File Report+GUI两库normal PASS151.697s；vet、格式和包含新增README的显式文档链接检查通过。执行环境制品`action-assembly-verification.json`记录本切片边界。

2026-10-03 Source首片：显式固定源配置接入同一有界行动注册表、Context、原提案、Task Use及Execution实际HTTP。Body原参数LocalID必须明确披露，Search原QueryRef必须双声明；缺声明在Use前拒绝，0 HTTP。准确Source/Capability/Binding/InstallLock、receiver/location/资源和原完整Grant绑定持久；当前Grant head、数据许可及输入DAG在出口与缓存读取强核。正文进入真实Content介质，Search命中与Operation metadata继承实际BodyRef；直接／派生／metadata不能绕过撤回或当前保留期限。准确配置、闭合字段、凭据、用途和边界见adapters/development/INFORMATION.md。

2026-10-03 Source账务与恢复：实际1次HTTP及applied Fact后、首次billing前公开撤回原Grant并取消，最低authority-only execution_usage_proof仍按原公开执行账本及真实累计费用出版、Task与Grant各结清；它不含正文／标题／snippet／quote，也无普通Context读取资格。数据库重开保持原ProofRef、Snapshot和累计用量，0新HTTP。新增配置导致旧File待恢复publication错误换policy的实际RED已经闭合：新plan固定首次PolicyRef，旧plan只能沿准确已保存reserve恢复，不刷新原命令、期限或policy。

2026-10-03 Source首片验证：`go test -mod=mod -race ./adapters/development -run '^(TestExplicitInformation|TestConfiguredInformation)' -count=1 -timeout=15m -v`，真实SQLite/PG及HTTP完整矩阵PASS880.295s；`-run '^TestInformationConfigurationKeepsUnknownOriginalFilePublicationPolicy$'` 两库race PASS149.615s；默认File完整Report两库normal PASS163.287s。测试进程统一使用既有3分钟fixture限时，原Task5分钟、控制窗5秒及数据许可不变；原丢失exec session不记为PASS。完整日志及实际exit保存在执行环境action-information-source-matrix，制品action-information-assembly-verification.json绑定该片及其验证边界。此矩阵不裁定参考问题已答对。

2026-10-03 Source参考问答中间片：显式information_reference_answer登记闭合JSON字符串事实问题，原Goal.Body的sources/claims/时效固定；不接受模型自报Observation/pass。Context只从本Task原closed Operation/Attempt读准确journal/current Content，真实BodyRef/字节进入Snapshot材料。原要求参数与问题准确一致，独立ReferenceEvaluator读取原源JSONPointer、当前许可、获取／观察时效及完整引用后形成ConditionResult，Task走正常coverage/check/complete/Result；默认File规则不变。真实SQLite首次完整链PASS17.431s（原3模型POST/1GET、verified Result/DB重开原回执无新HTTP），错答公开fail check、禁止Result与取消后原费用结清PASS13.443s。一次测试wire漏goal_utf8导致的180s失败保留，修正fixture后重跑通过，不记失败为PASS；PostgreSQL/race、旧来源时间、当前撤回及冲突／缺口反例正在下一片验证。

2026-10-03 Source参考问答两库矩阵：`go test -mod=readonly -race ./adapters/development -run '^TestInformationReference' -count=1 -timeout=30m -v` 实际 SQLite／PostgreSQL 四组八子例全部 PASS626.122s（编译／编排636.487s）。成功路径独立条件与不可变 verified Result、原回执／DB重开无新HTTP；错答公开fail且无Result；原观察时间两小时前而获取时间为现在仍为stale；原1GET/applied后公开撤回Source Grant在答案模型出站前阻断，保持2个原POST／无Result，取消后原USD0.00048结清。没有延长原业务期限或把工具中断当成功，执行环境 action-information-reference-matrix 保存完整日志、exit0、测试文件摘要与前提。运行基线3ea375c，SourceQA生产实现07a7a08；新测试仅增加真实时效和撤权分支，旧失败仍保留。

2026-10-03 WASI Task 首片（governance_impl）：显式准确 Environment／CodeRef／InputRef 和真实 Runtime InstallLock 进入原注册表、Context、Brain、Task Use 与 Execution 栅栏。二进制代码只在完整来源集合，不转成模型正文；原 generation／namespace CAS、CPU 软限及硬限余量固定在受信准入，预算不足或 CAS 改变时不消费 once、不进入 native Attempt。只有原已闭合 applied Cell 的准确完整 namespace，经过当前来源、长度／摘要及闭合类型检查后进入后续普通材料，报告仍由独立文件回读和原条件证据裁决。配置与前提见 adapters/development/WASI.md。

2026-10-03 WASI 完整公共 Worker 验证：`TestConfiguredWASIReportCompletesFromActualNamespaceAndIndependentReadback/postgres` 使用实际 App.Run、真实 PostgreSQL、独立 Linux 隔离进程及合同 HTTP，actual exit 0／Go 287.11s／wall 287.388s。原 Result.CompletedAt 早于五分钟 Task deadline 26.957s，两项独立 verified/pass 条件、18 字节实际文件回读、完整 namespace、正文 published、USD 0.00144 与 CPU 0.004321／预留归零均通过。Worker 在正文出版及费用闭合后真实 cancel/join，原库关闭重开保持 6 个 POST、4 个 Operation 各一个原 Attempt、原 Cell spawn1 与不可变 Result。源码基线 e148d3a／root61e8abd，加 helper96c1dc04／testcd29b13b；完整 source manifest／binary／profile／exit 制品保存于执行环境 wasi15-worker-wait-publication-20261003T165143249902Z-myw7_32d。旧超期、claim 丢失和出版待办失败各保留，不由本次通过翻写；当前版本的两库来源撤回公共 Worker 矩阵与受影响 race 尚未通过。

整体15保持partial：WASI 完整 PostgreSQL 正例已通过，来源撤回交接与受影响 race 仍待本地验证；独立 Executor 沿工单16的实际范围验收。原 driver 功能证据不代替完整链，尚未完成项不归因为外部凭据缺口。参考问答本轮范围为固定公开HTTP JSON字符串事实与明确合同模型，通用自然语言质量／供应商资格另行验收；多源冲突及缺口仍按所属ReferenceEvaluator合同拒绝，不将provider检查冒称全部App场景已测。
