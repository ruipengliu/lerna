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

Source Search/Body、WASI与独立Executor装配仍待后续实际驱动/租约ports完整验证及该工单集成；类型存在未计为开放。整体15保持partial，不以外部缺凭据替代本地待实现项。
