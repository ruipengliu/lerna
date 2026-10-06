# 03: 持久化点崩溃钩子与重启

**What to build:** 开发者可以在任意命名的持久化点注入"提交前崩溃""提交后、回执前崩溃""回执丢失"，然后让 harness 从同一个 SQLite 文件重启继续。钩子只在 `fault` 构建标签下编译。首个用例：输入已持久保存、任务尚未建立时崩溃，恢复后只有一个任务。

**Blocked by:** 02（提交目标与创建任务）

**Status:** resolved

- [x] 故障注入点按名称注册，覆盖 02 已有的持久化点；新增持久化点时有统一的登记方式
- [x] 生产构建不包含钩子代码（用构建标签验证）
- [x] harness 能在同一测试中崩溃并重启，重启后待处理命令自动恢复
- [x] 故障测试：输入持久保存后、任务建立前崩溃，恢复后只创建一个任务（交互适配器 M1 演示 1，标注 G3）
- [x] `make test-fault` 只运行带 `fault` 标签的测试

## Evidence

- `conformance/restart_test.go`：先看到 SUBMITTED 的红灯，再实现生产与测试共用的启动恢复。
- `conformance/fault/restart_test.go`：3 个现有提交点 × 提交前/提交后崩溃，共 6 个独立子进程；退出码 86 验证命中故障点，`os.Exit` 不运行 Close/defer。受理后崩溃自动恢复一个任务、一个输入；重投拿回原决定。
- `conformance/fault/receipt_test.go`：3 个回执丢失用例，分别保留 NOT_FOUND、SUBMITTED、DECIDED；调用方拿不到被丢弃的确认，只得到 UNKNOWN。
- `conformance/fault/registry_test.go`：检查所有事务名称有登记、登记项存在调用点，并以普通 `go list` 验证生产文件选择排除故障配置。
- `make check` 通过（含普通/故障 lint、race、fault、规则与协议生成检查）；本工单不宣称掉电验证完成，见 21。


## Answer

命名持久化点的提交前、提交后和回执丢失注入仅存在于 fault 构建。公共提交与重启恢复保留原 CommandIdentity、原回执和一个任务；独立父进程核验实际故障退出及原始责任。原 Evidence 和更早运行记录全部保留。当前全量重跑的阶段／包／完整测试路径已经独立绑定；进程崩溃证据不外推物理掉电。

当前逐项语义见 [ROOT 当前验收认定](/Volumes/Data/proj/lerna-m1-context/ticket23-root-standards-fix-current-specific-semantic-qualified-01.json)，绑定 TESTED `8d951ad2d89d06282560280666ca162c957e9e68`；修复提交 `94f77d187e0b65010c4332302beb93854674fedb` 已经独立合并并经 [ROOT POST](/Volumes/Data/proj/lerna-m1-context/ticket23-root-standards-fix-formal-merge-independent-audit-01.json) 核验。此处记录本地 Markdown 收尾；该收尾提交的独立合并 POST 和工作树清理仍待完成。
