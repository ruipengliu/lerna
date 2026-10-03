# 16 independent-executor

Status: ready-for-agent
Blocked by: 01, 02, 04, 08
Implementer: unassigned

依据：A3、生产端侧 SQLite 权威边界、有限 GrantLease、真实发送门禁和 F07／F08／F21。

补齐独立 Executor 入口、原 owner 路由、受信端点及远端 Authority 装配；设备只保存本机执行、资源、许可和补传，不写云端 Task。真实 PG 云端与独立 SQLite 设备验证原准入、丢回复、断连、撤权、有限离线许可、重开及迟到账务；三 AZ 与真机仍另行资格验收。

## 完成依据

实现、公开行为、真实负责方和必要故障正反例通过后记录准确提交与制品。外部资格单列；尚未完成的本地路径不得归因于缺凭据。未达成的目标保持 partial。

## Comments

2026-10-03：全项目范围复核后补入原实施任务图，未改变用户授权或领域裁决。
