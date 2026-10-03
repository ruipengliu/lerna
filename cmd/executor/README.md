# 独立设备进程

此入口使用独立 SQLite；它不是 `cmd/worker` 别名，也不读取云端 DSN。先在管理配置中固定设备 owner/instance、原 Authority 的 P-256 公钥、精确 Binding/InstallLock/资源/action、有限 peer token 文件和 TLS 证书。没有这些配对依据时，`setup` 在接纳业务之前拒绝。

```sh
go build -o /tmp/harness-executor ./cmd/executor
/tmp/harness-executor setup --config /absolute/device.json
/tmp/harness-executor serve --config /absolute/device.json
/tmp/harness-executor migrate --config /absolute/device.json
```

`setup` 明确创建/迁移设备库、保存真实 database_id、初始化本机有限 peer 与目标根目录。重启 `serve` 必须使用原库、原签名密钥及原 instance；它不迁移数据库或恢复已撤权的凭据。TLS 最低 1.3，设备入口不提供明文开发例外。SIGTERM 先停止接纳并等待本机 worker/RPC 实际退出，再关闭 SQLite 和目标锁。配置及私钥/token 文件不进入仓库；凭据只通过文件路径引用，不使用命令行 token。

配置合同是 [executor.Config](../../adapters/executor/host.go)，边界及准确方法见 [独立设备 adapter](../../adapters/executor/README.md)。当前文件驱动作为首个精确静态能力；多设备 GUI 与完整云端 ActionRegistry 的实际接入按工单 16 后续证据登记，不能由本入口编译通过推断。
