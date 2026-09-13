# 24：同侧 gRPC 能力执行

`grpcbinding.NewServer` 将既有 Execution 服务绑定为具名 gRPC；`grpcbinding.Dial` 返回 `sdk.Transport`，交给 `sdk.NewCapabilityClient`。启动、异步作业和 Core 报告由宿主既有执行协调负责。参考验证使用独立测试子进程，经过真实 TCP、TLS 1.3 与 HTTP/2，没有 bufconn。

## 接入

受信宿主先按 23 登记节点，创建 `nodetls.Endpoint`、本地授权及 `execution.Service`。执行服务的 namespace、subject、audience、presenter、证书摘要由本地主机构造，`MatchPeer` 将其与实际 TLS 身份比较。`Resolve` 只返回宿主的有限服务目录，不接受远程 token、私钥、数据库地址或恢复证书。

服务端打开私有 `Journal`，调用 `NewServer(endpoint, clock, config, resolve, journal)`，再以 `Serve(listener)` 启动；`Stop` 关闭连接。调用方使用 `Dial(address, expectedNode, endpoint, config)`，显式 `Negotiate(ctx, subject)` 后将 Client 传给 CapabilityClient。Close 释放客户端连接。

已提供 Invoke、GetInvocation、Reconcile、RequestCancel、GetCancel。List/Search/Describe、资源控制及其他服务未启用，具名未实现方法明确返回不支持。所有业务消息继续采用既有 CapabilityRequest/CapabilityResponse，不远程开放 Store。

Negotiate 采用固定 bootstrap 1，业务版本 1。必需能力为 capability.invoke.v1、capability.query.v1、capability.cancel.v1、invocation.snapshots.v1；原始生成客户端可选择子集。返回的有限配置通过 `harness-configuration` metadata 关联实际认证主体。服务重启、期限届满或配置容量不足时显式拒绝；客户端需要重新协商，不自动重试先前的变更。

## 结果、授权与恢复

领域错误由类型化 CapabilityFailure 保留稳定 code。gRPC 连接、协商及流错误单独表达；已提交给 gRPC 的 Invoke、Reconcile 或 RequestCancel 遇到传输失败、响应关联失效或对端认证失效时，SDK 返回 OUTCOME_UNKNOWN。它保留原请求中的 operation_id；调用方按该操作查询/核对，不能根据超时重新分配操作或重做动作。原生重试配置关闭，重试缓冲为零；库在确定没有交付应用的情况下可能进行透明连接尝试，不是已接纳变更的业务重试。

逐调用、逐订阅轮询及返回 unary 响应前检查当前节点。`ReadRemoteInvocation` 在授权事务中验证当前读取权限和原 UsePermit/授权祖先，防止已经撤销的调用结果继续远程披露；本地 GetInvocation 保持获准管理及恢复用途。取消和核对通过同一远程读取资格进入。执行准入及动作启动继续使用原有授权边界。

RPC 等待取消只结束当前等待。已接纳操作及异步作业由宿主持久状态继续管理，业务取消仍是引用原调用的独立操作。服务重启后由宿主重建同一可信绑定和执行协调；本票固定节点证书版本，证书退役后的新绑定需按 23 的受控轮换/重登记流程另行配置，旧连接不继续取得访问权。

## Exchange 与持久记录

每个 Exchange 订阅一个获准操作，两方向分别传输完整调用快照和 PERSISTED 回执。订阅不是永久读取许可。`Subscribe(ctx, operation, inbox)` 后调用 `Receive()`：可靠快照在本地 Journal 持久化后才发送回执，返回的 fresh=false 表示重复交付。它不承诺任意消费方回调或外部副作用恰好一次；客户端保留本地 inbox 供可信恢复读取。

seq 使用既有调用 revision；full_snapshot 明确表示累积快照，允许跳过未发布的中间修订，不伪装成连续事件日志。NOT_STARTED/IN_PROGRESS 为可替换观察，FINISHED/UNKNOWN 或冲突证据可靠交付。相同修订首次发布的内容固定；Core 应用进度改变而调用修订未变时，重放仍使用首次发布内容，最新进度由 unary 查询获得。

服务器发送可靠快照前写入 outbox；回执仅确认该 peer、operation、seq 的已存记录。断流和重启后重发未确认记录，重复确认幂等；新订阅也会提供当前修订快照。原操作和已发布记录持久保留，不根据猜测的客户端游标丢弃消息。本版本不清理记录、不提供任意历史游标；配置容量耗尽时拒绝新增可靠记录，既有记录继续可核对。更长运行周期需增加有业务生命周期依据的退休策略，不能直接删库续跑旧操作。

Journal 使用既有 SQLite 依赖、WAL 与 FULL synchronous；配置容量固定于文件，变更容量明确冲突。服务器和客户端分别拥有文件；主机应将文件放在受控私有目录，配置的容量为 1–4096 条，单条上限 1 MiB。

## 有界配置

| 参数 | 接受范围 | 验证配置 |
| --- | --- | --- |
| MaxConnections | 1–32 | 4 |
| MaxSessions | 1–128 | 8 |
| MaxStreams（服务端全局订阅上限） | 1–32 | 4 |
| MaxMessageBytes | 4096–1048576 | 65536 |
| SessionTTL | 1 秒–10 分钟 | 1 分钟 |
| IOTimeout | 1 毫秒–5 秒 | 1 秒 |
| PollInterval | 10 毫秒–1 秒 | 20 毫秒 |
| 协商 pending_window | 固定 1 | 1 |

每连接 HTTP/2 并发上限为 MaxStreams + 1，为满额订阅之外保留一个 unary 控制槽。每流只有一个未确认可靠快照、一个待处理入站帧及一个在途发送。发送及回执等待有时限，客户端流也受 SessionTTL 限制。HTTP/2 流/连接窗口固定 65536 字节，读写缓冲各 4096 字节，metadata 上限 8192 字节。接收、回执和发送独立推进，临时进度不挤占可靠快照的待确认位置。慢消费者可以被断流，尚未确认的可靠记录留存；这不是零延迟保证。

## 验证入口与边界

运行 `sh scripts/verify-grpc.sh`（PATH 中需要 protoc 36.1）。Go 工具链 1.26.1、Protobuf 1.36.11、grpc-go 1.83.2、protoc-gen-go-grpc 1.6.2 固定在依赖与生成入口中；完整传递依赖及校验值由 go.mod/go.sum 和报告记录。

入口输出 build/grpc：环境、完整模块清单、生成/构建/静态检查结果、新增 race 测试、受影响回归、同步执行/异步恢复/受限授权 profile 与总状态。任一阶段失败保留失败并返回非零。真实协议用例与原有领域 profile 的证据分别记录。

测试子进程通过 stdin 接收受信夹具控制（推进独立模拟作业、使时钟前进、停用节点或撤销授权）；所有能力调用、查询、取消、订阅和结果仍通过 gRPC。测试安装时只复制公共登记记录至调用方独立的身份库，服务端 token 与节点私钥分别保存。独立模拟计数器在停止执行进程后核对真实变更次数，不通过读执行库代替 RPC 查询。

本票不包含端云 WebSocket、完整事件历史、所有原生服务、真实业务 API/设备/模型质量或完整系统验收。历史 23 报告中的记忆存储失败不自动继承为本次通过；本次具体运行结果以独立报告为准。

实现依据包括仓库已确认的原生接口目录及协议映射。gRPC 的独立双向流和写入背压机制分别参考 [Go 基础](https://grpc.io/docs/languages/go/basics/)与[流控说明](https://grpc.io/docs/guides/flow-control/)；库行为同时对固定依赖源码核对。
