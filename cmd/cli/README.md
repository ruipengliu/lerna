# 原命令 CLI

`go build -o bin/harness-cli ./cmd/cli`。所有 flags 放在最后的操作名称之前。输出是原回执、严格查询结果或已核验的发现 JSON；错误只输出稳定类别、原因及恢复建议。

```sh
bin/harness-cli --endpoint https://service.example --token-file /private/token discover
bin/harness-cli --endpoint https://service.example --token-file /private/token --journal /private/journal --request /private/original-command.json command
bin/harness-cli --endpoint https://service.example --token-file /private/token --request /private/original-query.json query
bin/harness-cli --endpoint https://service.example --token-file /private/token --journal /private/journal --command-id command_0123456789abcdef0123456789abcdef receipt
bin/harness-cli --endpoint https://service.example --token-file /private/token --journal /private/journal recover
```

请求文件必须包含公开 Command/Query 的全部准确外壳与闭合 payload。CLI 不生成业务身份、owner、CAS、默认值或截止时间。`command` 收到持久 rejected 时仍输出原回执并以非零状态退出；`receipt`/`recover` 返回原决定，不把拒绝改为新请求。

凭据只接受 `--token-file` 或 `--token-envref ENV_NAME`。文件须为私有普通文件，不接受最终 symlink；没有传入 token 值的参数。`--ca-file` 可追加明确的 TLS 信任根。默认超时 10 秒，`--timeout` 范围 1–30 秒，只改变等待期限。

WSS/grpcs 使用 `--endpoint wss://host/connect` 或 `--endpoint grpcs://host:port`，同时提供 `--discovery https://host`。发现核方法及 core 摘要，业务传输保持发现的固定 logical owner。明文 `http/ws/grpc` 只在显式 `--development-loopback` 与实际字面量 loopback 地址下开放；不允许凭据 URL、重定向或跳过证书验证。

Go SDK 在业务出站前 fsync 原命令文件，再 rename 并 sync 目录。journal 固定主体域、core/profile、准确方法 Schema 摘要和原命令摘要，最终决定不能被替换。多 handle 的保存使用文件锁；同 CLI journal 只运行一个进程。恢复先查原回执，明确 not_found 后才沿原身份发送，不刷新期限。原方法解码器变化或旧记录缺摘要时返回 unsupported，保留原责任。

单条 journal 组合上限 1 MiB，域请求及回执分别仍受公开 256 KiB 限制。一次恢复至多 128 个 pending，目录扫描至多 4096 条；不提供墓碑清理、自动归档或无限生产容量保证。当前没有 Content publish、watch、订阅或 endpoint 自动配对操作。

真实进程测试覆盖完整发现 TLS/凭据拒绝、WSS/grpcs 闭合查询、丢答复前持久化、重新打开后的原回执恢复、完成命令重复发送以及改截止冲突。三个角色的独立进程验证通过 application gRPC 接纳、worker 保存报告和独立文件读回。
