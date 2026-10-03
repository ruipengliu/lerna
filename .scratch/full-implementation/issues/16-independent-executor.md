# 16 independent-executor

Status: in-progress
Blocked by: 01, 02, 04, 08
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
