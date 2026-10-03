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

整体15保持partial：Source参考问答的独立条件、成功Result、WASI及独立Executor公共Task装配仍待本地后续切片；原driver功能证据不代替完整链，尚未完成项不归因为外部凭据缺口。
