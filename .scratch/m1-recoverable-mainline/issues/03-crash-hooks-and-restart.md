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
