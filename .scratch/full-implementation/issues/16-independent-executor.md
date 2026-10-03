# 16 independent-executor

Status: in-progress
Blocked by: 01, 02, 04, 08, 23
Implementer: execution_impl

依据：A3、生产端侧 SQLite 权威边界、有限 GrantLease、真实发送门禁和 F07／F08／F21。

补齐独立 Executor 入口、原 owner 路由、受信端点及远端 Authority 装配；设备只保存本机执行、资源、许可和补传，不写云端 Task。真实 PG 云端与独立 SQLite 设备验证原准入、丢回复、断连、撤权、有限离线许可、重开及迟到账务；三 AZ 与真机仍另行资格验收。

## 完成依据

实现、公开行为、真实负责方和必要故障正反例通过后记录准确提交与制品。外部资格单列；尚未完成的本地路径不得归因于缺凭据。未达成的目标保持 partial。

## Comments

2026-10-03：全项目范围复核后补入原实施任务图，未改变用户授权或领域裁决。

2026-10-03：execution_impl 领取。实现独立 SQLite 设备宿主、原云端引用与受信静态端点、有限 GrantLease、Tx 外签名依据准备及 Tx 内本机准入核验。以真实 PostgreSQL 云端和独立 TLS 设备进程验证原命令恢复、有限离线、撤权与迟到用量补传；不扩大原控制窗口，不装配云端 Task participant。

2026-10-03：第一行为片保存独立 Host 与闭合 AdmissionBundle、原 Control JWS 和有界内容缓存；真实设备 SQLite/File 通过签名绑定、原命令/Attempt 重放、原五秒窗口过期、取消先到、已知授权撤权、文件 rename 后结果不明与数据库/目标重开核对。`go test -race ./adapters/executor -count=1 -v` 实际 31.549 秒通过，`go vet ./adapters/executor` 通过。签名 cloud lease 是此片的受信预置前提；完整原云端 Task/PG、TLS 两进程与补传仍待后续片，不据此关闭工单。

2026-10-03：第二行为片提供固定 owner/instance/database 的 RemoteClient、真实 TLS、原命令 GoSDK journal、独立 `cmd/executor setup/serve/migrate`、同原 use 的本机 lease 用量及签名补传。真实 CLI／SQLite／TLS 子进程 SIGTERM 退出通过 4.578 秒；实际 PostgreSQL Authority 原一次 USD 1 预留、SQLite 原执行、签名迟到账单与重复补传不双扣，通过 5.308 秒。后者明确预置批准的 Grant 和原 Task 引用，未冒充完整公开 Task 装配。受影响 TLS 丢回复与 lease 归并 race 验证 10.094 秒、vet 通过；原数据库身份绑定变更后 Admission/TLS/CLI 定向回归通过 8.236 秒。工单保持 in-progress，依赖 23 foreign source 登记和云端 Task 路由共同完成。

2026-10-03：第三行为片实现设备原 source `content.register_copy/get/release_copy`、准确 PolicyValues／原主体代次与全部 processed/disclosed 策略交集、有限 ES256 `mode=use/control` 证明及纯本方 Tx 验法。普通字节逐块核原登记/current gate，SDK 原登记丢回复按原 receipt 恢复；旧 nil 策略只保原缓存／Attempt／账务，新普通副本拒绝。真实 TLS、两个 SQLite owner、当前关闭／control 不授正文／新代收尾／过期后原有限 cleanup TTL／未知清理，以及 subject gen1 撤权、gen2 新签准入、旧准入不复活均有正反例。完整 `go test -race ./adapters/executor -count=1 -v`（实际配置 PostgreSQL）通过 133.181 秒，含 CLI 子进程 SIGTERM、PG 原 allocation/lease settlement 与未知效果重开；vet/diff 通过。日志 `/workspace/harness-dev-environment/executor-source-race.log`；夹具仍预置受信 Grant/Task 引用与原 holder/refintent，不表示公开 Task／Memory 跨 owner 全链已验，工单继续 in-progress。

2026-10-03：设备 `execution.control.get` 原先把 TaskID 当作 Operation admission 键，导致先到停止门禁无法查询。公开 stop-gate-before-admission 用例 RED 复现后改为只读本机原控制事实；不注册／读取云端 Task。新用例与 cancel-first race 验证通过 9.779 秒，vet/diff 通过。

2026-10-03：GoSDK 新增显式受信 `RetainDecoder`；Executor Dial 保存两次真实历史升级的四份固定 admission 合同。旧 journal 的输入／回执按原准确 method/schema digest 解码，保留当前 owner/profile/core 与完整身份／数据库 scope，原责任先 lookup。实际 TLS old journal→new Dial 从 `original_decoder_unavailable` RED 到 GREEN：原 admission 回执、原执行命令与唯一 Attempt、真实文件字节／物理 journal=1、异参 TTL、未知 Schema、错误设备数据库 scope 均验证；定向 race 通过 54.939 秒，完整 SDK race 通过 10.767 秒，vet/diff 通过。首轮测试只有测试宿主 30 秒 wall deadline 在最后错误 scope 阶段用尽，扩展到 90 秒仅影响测试生命周期，不延长业务 TTL／原五秒 ControlWindow。日志 `/workspace/harness-dev-environment/executor-upgrade-final-race.log`；未据此关闭尚待公开 Task／foreign Memory 装配的工单。

2026-10-03：storage_impl 补云端显式 `RemoteExecutors`、准确 ActionBinding/Grant、原 lease 准入与签名 bundle、同一份 LeaseReport basis 的两方账务、当前 source/holder 载体及最终派发 Claim/Task/Grant 强核。公开 Cloud Task→真实 HTTP 模型→TLS/独立 SQLite File read→原 Source Content→下一轮模型，未预置 Task/lease/use；最终模型明确 fail，三 POST/一原设备 Attempt/原 USD 0.00072 费用闭合两库通过 41.181 秒。另实际 SQLite 28.693 秒与 PG 37.333 秒验证签名来源的 nested 同 Auth 正例，以及不同角色/用途、新 Query/新 Job 空载体拒绝；实现日志在 `device-task-regression/`。这是 partial checkpoint，不表示成功 Result：新增成功提案真实 RED 180.02 秒保留原 Task active、三 POST、账务 closed，定位新 completion Job 缺本次来源证明；可选 Completion prepare、结果出版/原库重开与剩余故障正反例在后续。配置及实际边界见 [REMOTE_EXECUTORS.md](../../../adapters/development/REMOTE_EXECUTORS.md)。

最后载体 race：PG 子例通过 138.14 秒，同次 SQLite 因设备账单 head 继续推进的明确 `revision_conflict:device_usage_source_advanced` 立即失败，整组退出 1 的 221.298 秒日志保留；仅让该准确已回滚冲突沿原 Job/Claim 恢复后，SQLite 独立 race 通过 99.826 秒。未改产品自动重试、原 Command/TTL 或 Task billing。默认 File 独立读回报告与原 publication 提交未知后 policy 重开兼容，两库普通回归合计 112.680 秒通过；最后 vet、文档导航 119 文件/490 链接及 diff-check 通过。Task 的 Completion/Advance 可选接口与 Agent 纯 parent carrier 已合并；本片仍不纳入成功 Result 与 Completion host 接线的未完成验证。

2026-10-03：第二宿主片接入 Task 原可选 Completion preparation，并为原 Evidence.Check 的 `task.context` 用途显式登记／取得本次准确已配对来源。公开 Checks=0 的真实 RED 180.050 秒原样返回 `dependency_unavailable:foreign_reference_not_registered`，尚未进入 Completion preparation，未改 Task 核心。该原 Task／submit／deadline 沿保留 SQLite 一致快照真实到期 failed、原 USD 0.00072／零预留／账务 closed，通过 0.806 秒；原失败运行时配置未保存，不能伪称完整 App 重开。新的成功提案 SQLite 切片实际通过 25.603 秒：三次模型 POST、唯一设备读取、原 verified Result 出版、Task／Grant 原费用结清，云端和设备实际 Close/join 后原库／原密钥／原目标重开保持同一 Result 与 Attempt。准确配置／介质／journals 保存在私有 fixture，日志 `device-task-regression/verified-result-sqlite-host-check-preflight-first.log`。PostgreSQL／race、必要拒绝故障和完整 Task 写入链仍待后续，不因单库正例关闭工单。

2026-10-03：同一已提交宿主片 `04c7799` 的 PostgreSQL successResult 切片真实通过 65.044 秒，原 Task `task_ae1904ff57123ad628a39ba1500d74c6` 的 verified Result／内容引用、原三次 POST／USD 0.00072／零预留、原设备唯一 Attempt、云端与设备 actual Close/join 后原数据库重开均核验。一处明确 `device_usage_source_advanced` 回滚沿原 Job／Claim 恢复，无新 Use／Attempt。与 Task minimum-invoice race session21099 有短暂并行，未称独占；准确源码、日志摘要、Scope／Command／Operation／Attempt／Control／Result 引用及私有 fixture 记录在 `device-task-regression/verified-result-postgres-verification.json`。必要拒绝矩阵／race 和完整 Task 写入链仍 pending，工单保持 in-progress。

2026-10-03：完整写入／独立读回宿主片在固定 `9f533523fb86cccf2f92d29a7c20fe62d7694cb1`／854 项源码清单下实际通过 SQLite 36.368 秒与 PostgreSQL 云端 51.997 秒普通验证。公开 Task／真实模型 HTTP／TLS／独立 SQLite 设备，不预置 Task／lease／Use；四个原模型 POST、两个不同设备 Operation／Attempt，原写入 87 字节与独立读回摘要相同，Cloud 目标不存在。两项 usable／verified／pass 条件、immutable Result 与正文出版、原 USD 0.00096／零预留／账务 closed，真实云端及设备 join 和原配置／数据库／目标／journals 重开同身份均断言通过。原缓存跨 Operation 的准确许可和 SavedRule 设备目标路由分别由 Task 正式修复 `3a2489e`、`0844981`，早先完整链失败保留，不改旧 bundle 或业务期限。完整索引在执行环境 `device-task-regression/saved-rule-complete-frontier-142g56e5/verification.json`、`saved-rule-postgres-driver-o9_snwbx/verification.json`。

同一受测源码的成熟边界 race 顺序通过 SQLite 12 项／102.646 秒与适用 PostgreSQL 6 项／60.134 秒，实际退出 0、无 skip／缺项且源码／binary／driver／集合前后稳定。当前来源、holder／主体代次／用途、新入口空证明、撤权／取消／原窗口／Claim 及 CommitUnknown 强门禁均沿公开原消费端验；PG Memory 提交未知一项为 PG source 加 SQLite consumer，固定 SQLite 设备与 Task 用例未重复冒充 PG 资格。准确集合及四方索引在 `device-task-regression/final-guards-frozen-hy788u1x/`。这是各边界的 race，完整报告 race、SavedRule 专门公共拒绝矩阵及最终统一检查／双轴审查仍 pending，工单保持 in-progress；后续 CPU／Session 集成不改变受测源码身份。
