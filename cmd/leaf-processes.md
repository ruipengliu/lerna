# 参考进程入口

叶入口复用 `cmd/internal/bootstrap`，只处理显式配置、管理动作及进程退出。各进程使用同一个准确数据库和 owner 配置，服务启动不会迁移数据库、生成主体或重置规则。

```sh
go build -o bin/harness-migrate ./cmd/migrate
go build -o bin/harness-gateway ./cmd/gateway
go build -o bin/harness-application ./cmd/application
go build -o bin/harness-worker ./cmd/worker
```

先由受信管理环境通过 `HARNESS_DATABASE_DSN` 提供数据库凭据，再显式初始化参考配置：

```sh
bin/harness-migrate --config /private/harness/config.json --development-init --data /private/harness --driver postgres
bin/harness-application --config /private/harness/config.json
bin/harness-worker --config /private/harness/config.json
bin/harness-gateway --config /private/harness/config.json
```

初始化会固定私有 token/key、数据库 ID、主体和开发规则；重复管理初始化保留原身份与已知撤权。普通迁移为 `harness-migrate --config /private/harness/config.json`，在任何迁移前核原数据库身份。数据库凭据和 token 值不进入参数或日志。

参考配置的 HTTP/gRPC 地址分别为 `127.0.0.1:8080` 和 `127.0.0.1:8081`。只在显式 development 配置下开放明文 loopback；company OIDC/生产 TLS/发现尚未接入的配置返回 unsupported。`HARNESS_TEST_TZDB_ROOT` 可指定真实固定 TZDB 数据目录，版本仍严格核 2026b；不能只改版本标记。

application 暴露严格 Unary，gateway 暴露认证 HTTP/WSS。worker 拥有持久 Job 池、实际受管文件和三台独立持久模拟手机，其他角色只注册同版执行合同并拒绝物理出口。SIGINT/SIGTERM 触发停止，并等待实际进程退出；`starting` 输出不是就绪证据，应使用公开当前认证发现/健康响应。

gateway 将业务命令、查询与原回执查询通过严格 gRPC 转交固定 application，保留原用户凭据和准确域字节。application 不可达时返回 `dependency_unavailable`，没有本地领域决定；CLI journal 保留原命令，在 application 恢复后沿原身份查询及恢复。开发 HTTP 内容 raw upload 当前仍由同库本地 Content owner 接收，不声明已实现独立对象上传服务或完整生产路由。

当前只有一个 worker 能持有同一文件目标根的独占锁，不声明双执行 worker 或多机目标接管已通过。SQLite 可用于显式本机整体验证，但该装配不是设备默认云端 Task 权威。没有 `cmd/executor`；独立 SQLite 设备的远程 Authority、签名有限 GrantLease 和端云恢复配置缺失时，不用 PostgreSQL App 冒充设备宿主。

不可信 WASI、真 Android/iOS、公司平台、跨 AZ 容灾与生产容量均不在这些参考入口的支持声明中。模拟手机和受信纯计算能力的范围见 `internal/execution/README.md`。EndpointChannel 的配对 authority 未装配时保持关闭。
