# 23：运行节点登记与双向 TLS

实现入口是 `Service.Nodes`、`nodetls.NewIssuer` 与 `nodetls.NewEndpoint`。复用已有授权 SQLite CAS 存储、受信本地管理员和 GrantAuthority；不增加数据库、证书管理服务或依赖。节点登记是本地受信 Go 管理入口，完整 gRPC/Exchange 与 WebSocket 绑定分别由 24、25 实现。

## 配置与调用

1. 受信安装者通过既有 `Service.Initialize` 或 `Bootstrap` 建立本地管理员。签发方独立保存 CA 私钥；`nodetls.GenerateCA` 是受信安装辅助函数，不可暴露为业务请求。节点自行生成 P-256 私钥，使用 owner-only PKCS8 文件保存；已有 `josegrant.LoadPrivateKey` 可读取受限密钥文件。
2. 管理者取得 `NewOperation` 和当前策略修订，构造 `NodeMutation`。节点通过 `nodetls.Request` 签署请求；CSR 绑定操作、命名空间、节点、代理主体、期限和预期修订，不能将同一证明改用于另一登记。管理者核对后调用 `NodeAuthority.Mutate`；只在确认提交后交付公开证书。
3. `ENROLL` 创建登记；`ROTATE` 由当前管理员为有效节点签发替代证书并保持代理主体；`DISABLE` 立即使当前登记失效；`REENROLL` 用于失钥后的受信重登记，同时清除旧 Grant 的证书追溯资格。管理回执按原操作保存，重复原操作只恢复原结果，不重新启用旧节点。
4. 每个节点用自己的叶证书/私钥、受信 CA 及明确的接受截止时间构造 Endpoint。身份编码固定为 `harness-node://v1/?namespace=...&node=...`，采用规范 URL 查询编码。验证链、用途、有效期、规范身份、实际叶证书摘要和当前登记；不使用 CN、请求头、DNS 名称或发现消息建立节点身份。只支持直接 CA 签发的一个叶证书，未知/中间链配置明确拒绝。
5. `ServerConfig`、`ClientConfig(target)` 提供 TLS 1.3 配置。客户端使用完整的 `VerifyConnection` 代替 DNS 验证，以核对 URI 节点身份；这不是跳过证书验证。客户端必须指定目标节点。服务器使用双向证书验证，没有浏览器或无证书回退。
6. 服务端每条业务消息通过 `Presentation` 建立受信上下文，再调用既有 GrantAuthority 和业务处理入口；客户端处理每条响应也应调用 `Authenticate`。连接成功不代替逐次准入。Endpoint 只负责认证，宿主必须限制连接数、握手/读写期限、请求大小和队列。验证宿主使用 `netutil.LimitListener(4)`、1 秒请求头/空闲期限、2 秒读写期限及 4096 字节请求头配置，并验证无握手输入的连接被关闭。
7. `RememberPresentation` 在原操作已持久预留且当前 Grant/节点资格有效时保存真实入口证书和出示关系。它与业务启动分别提交，不能作为已执行或已披露的回执。恢复者经受信本地管理员 `LookupPresentation` 读取原记录，使用 `RestorePresentation` 重新检查当前信任配置和登记，然后重新核验业务资格；不会合成一条旧 TLS 连接。轮换后原证书不再获准，须经新连接及重签材料恢复同一操作，保留原预留与入口记录。

`NodeConfig` 必须提供有限的证书期、加入请求期、I/O 期限和容量。实现上限分别为 24 小时、10 分钟、5 秒、512 节点及 4096 管理/入口记录；代理主体最多 16 个、每节点轮换追溯最多 32 项、CSR/叶证书最多 8192 字节。登记容量不自动清理，满后明确失败，管理记录为禁用保留空间；以后确需清理时须保留原操作防重放规则。验证配置使用证书 1 小时、请求 1 分钟、I/O 1 秒、16 节点及 64/128 操作。

## 轮换与授权

节点证书轮换立即停用旧叶证书，包括既有连接和恢复会话。原 Grant 的范围、祖先、业务操作、期限、证书来源和额度记录保持不变。新增的受控 `GrantMutation.Kind=REBIND` 由当前管理员核验原 Grant 后重签相同记录，将 `cnf.x5t#S256` 绑定到受信轮换后的当前证书。签发前后都核对当前版本与权限；提交未知不交付材料，原管理操作可恢复同一材料。

重签不创建新 Grant、不再次分配祖先额度、不退还未知消费。原预留保持其原证书来源；当前出示者在登记可证明的轮换关系内核对同一操作与语义。失钥重登记不会使旧材料或祖先资格复活。单次授权的操作、语义和剩余额度限制继续有效，证书合法不能绕过业务权限。

CA 接受映射的 `Trust.Until` 在每次握手、消息和本地恢复时重新检查。`josegrant.NewRotating` 则为授权签名密钥提供独立的有限接受期（最多 24 小时）；两个密钥用途不混用。新旧签发密钥的重叠由受信宿主配置截止时间，未知密钥与超期旧材料拒绝。原有 `josegrant.New` 保留本地旧配置兼容；部署节点认证时使用带接受期限的配置，不从消息加载密钥或信任根。

## 可重复验证

```sh
sh scripts/verify-enrollment.sh
PATH="$PWD/.tools/protoc-36.1/bin:$PATH" sh scripts/generate.sh
GOTOOLCHAIN=go1.26.1 go build -mod=readonly ./...
GOTOOLCHAIN=go1.26.1 go vet -mod=readonly ./...
```

生成检查需要仓库指定的 protoc 36.1；本次从官方固定版本发行包配置到忽略目录 `.tools`，Go 固定 1.26.1，protobuf 生成器固定 v1.36.11。没有改变依赖版本。

`node-enrollment-v1` 输出 `build/enrollment` 下的环境、依赖、逐项测试事件及既有 06/02 profile 报告。任一检查失败时返回非零，同时保留其他检查的实际状态。测试从 Go 公共接口执行：两个独立密钥目录、真实 loopback TLS 与实际恢复会话、读取已有受控内容模块的 `hello` 数据、持久入口身份重新打开、登记管理丢失回包、并发修订竞争及两个实际子进程退出点。HTTP 只作 TLS 验证宿主，不是新增生产协议，也不代表 24/25 完成。

本次结果见 [验证报告](evidence/23-node-enrollment-report.json) 和 [审查记录](23-node-enrollment-review.md)。记忆适配器既有回归 `TestCurrentHarnessPolicyAndResidencyConstrainMemory` 在此宿主打开 SQLite 时出现 `SQLITE_BUSY`，未修改 HEAD 副本也可复现；该失败不记为通过，也没有为本票修改记忆存储锁。运行节点测试、既有 06/02 profile 与其余适用回归分别记录。

限制：不证明远端同步、全局即时撤销、无限离线、旧备份防回滚、受控宿主被攻陷后的安全、真实设备或完整网络业务协议。登记状态使用同一受信权威持久存储；远端副本新鲜度与可靠传输仍由后续票处理。当前受控内容读取与额度预留分别核验，不将预留本身称为业务消费或外部效果。
