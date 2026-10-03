# 静态端点与分类 worker

`Config.EndpointChannels` 显式启用本机 PG 参考宿主的公开
`cmd/gateway` → mTLS `cmd/application` EndpointChannel。省略时沿用原 Unary
开发装配。固定配置最多两个应用地址，地址只接受 `grpcs://host:port`。
Application 使用同一 owner、tenant、真实 database_id、原 identity/key 和
同版方法 manifest；每个实际应用进程必须有不同的 `application_instance_id`。

受信配置固定 `gateway_instance_id`、`gateway_identities`（客户端证书的
SPIFFE URI SAN）与 `registrations`（原 tenant/subject/credential generation、
endpoint/instance/generation/recipient）。当前 App 注册一个配置中的用户，
credential generation 为 1；端点配置只接受该原主体，不能由消息添加用户或
变更 receiver。注册租户或主体不符时，读取配置及创建 App 均在打开数据库前拒绝。
后续凭据核验仍调用原当前 IdentityProvider，不以静态登记替代当前状态。

TLS 文件采用绝对路径，私钥是权限不对其他用户开放的普通文件：

```json
{
  "gateway_tls": {"certificate_file": "/private/gateway.pem", "key_file": "/private/gateway-key.pem"},
  "application_tls": {"certificate_file": "/private/application.pem", "key_file": "/private/application-key.pem", "ca_file": "/private/gateway-ca.pem"},
  "gateway_client_tls": {"certificate_file": "/private/gateway-client.pem", "key_file": "/private/gateway-client-key.pem", "ca_file": "/private/application-ca.pem"}
}
```

Gateway 的公开 listener 使用 HTTPS/WSS；内部应用 listener 最低 TLS 1.3，
要求客户端证书与登记的 gateway URI SAN，同时核原用户 Bearer。开发 HTTP 入口
仍要求明确 loopback 地址；内部 TLS listener 使用固定 GRPCAddr，不提供公司发现
或生产证书管理。原外 `connection_id`、
endpoint/instance/generation 与 methods digest 固定；取得完整候选内部 Ready 后
切换新 binding，不再发送外 Ready。重绑期间原回执查询可收到
`endpoint_channel_rebinding`；消费者使用递增外 seq 查询原 command，不能刷新 TTL
或盲重发未知命令。HTTP Unary 转发只使用第一个静态地址，不自动重发命令。

公开 App 尚未配置原业务 Delivery receiver/proof 端口，因此 Delivery 保持关闭。
独立 adapter 和 Go SDK 的显式 signed Delivery/Reply/Ack 合同与故障验证不等于
本装配自动生产、消费业务 Reply。

`Config.WorkerPool` 固定 worker 的 `pool_id`、`job_kinds` 和 `concurrency`（1–64）。
只有登记的准确 JobKinds 可以领取；重复或 `execution.*` 类别被拒绝。配置该池的
`cmd/worker` 不取得 managed files/phone 目标锁，不能执行物理驱动。省略配置的
原 worker 继续明确拥有物理目标；独立设备使用 `cmd/executor` 的独立 SQLite、
有限 Authority/Lease 配置，不借云端 worker 冒充。

真实公开进程验收见
`cmd/internal/runner/channel_process_test.go`：实际 PG、公开 HTTPS/WSS Gateway、
两个公开 mTLS Application、原应用 SIGKILL、原回执与单调序号、实际 SIGTERM 退出。
同目录 `channel_workers_test.go` 联合启动两分类 `cmd/worker` 与独立
`cmd/executor setup/serve`：两个原领域 Job 各领取一次并完成，未准入 execution
责任未被分类池领取，所有云端进程不占 Files/Phones 锁；独立 SQLite Executor
持有实际文件锁，沿真实 PG 的一次有限 Lease 预留和明确签名准入夹具完成一次文件
写入、独立读回与原用量闭合，全部正常进程 SIGTERM 实际退出。夹具的 TaskRef 与
来源许可由受信测试预置，不冒充公开 Task 决策或 Content 当前登记；真实 public
Task 远端装配按工单 16 独立验收。旧输出写门禁与双向 Reply/Ack 的细粒度故障
证据见 `adapters/endpointchannel/README.md`，公共正常重绑不冒充该故障注入。
