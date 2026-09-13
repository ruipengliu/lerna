# 02：本地身份、策略与授权

对应 [02 票](../../.scratch/harness-implementation/issues/02-auth.md)。本实现提供单机本地授权权威、持续授权判定、SQLite 管理提交及接纳窗口；不提供任务执行或跨节点授权。

## 运行入口

```sh
mkdir -m 700 /tmp/lerna-local-auth

go run ./cmd/authctl init \
  -db /tmp/lerna-local-auth/auth.db \
  -credential-file /tmp/lerna-local-auth/admin.credential \
  -config examples/local-auth/config.json

printf '%s\n' '{"messageId":"query-1","namespace":"local","getPolicy":true}' |
  go run ./cmd/authctl call \
    -db /tmp/lerna-local-auth/auth.db \
    -credential-file /tmp/lerna-local-auth/admin.credential \
    -config examples/local-auth/config.json

go run ./cmd/contractcheck -profile local-auth-v1
make verify
```

示例目录必须是新的。CLI 初始化先以 0600、独占创建方式保存随机凭据并同步文件及目录，再调用受信 Initialize；已存在的凭据文件不会被覆盖。安装响应未知时，用该文件调用 GetPolicy/Authenticate 核对，不删除数据库重新初始化。普通业务协议没有 Bootstrap/Initialize 方法。宿主也可直接组装可导入 Go 库，先持久保管 NewCredential 的结果，再调用 Initialize；Bootstrap 是便捷安装包装，不适合作为有丢失回包风险的远程安装协议。

凭据只用于受信本地 Binding；显示名称无法认证，普通 Protobuf 请求没有用户凭据字段。SDK 的 AuthorizationClient 使用 Transport 抽象，凭据绑定留在 authlocal Adapter。CLI 的 ProtoJSON 仅为本地调试/管理输入输出，内部实际通过 Protobuf 二进制往返，不新增生产 JSON 网络协议。

所有期限和求值限制必须显式配置。示例为凭据最长24小时、授权最长1小时、接纳窗口1分钟、回执保留2分钟；求值最多32条策略、128个资源、16层资源树、4096个工作单位及1秒。配置还受有限硬上限约束；持续授权不无限有效。配置在初始化时持久固定，不能通过重启换配置放宽已有约束，本票不提供在线配置迁移。

## 实现及接口边界

- `authorization.Service` 实现初始化、认证、管理操作、Evaluate 和操作核对。调用者依赖 Store/Clock，领域接口不包含 SQL 连接或驱动错误。
- `IdentityVerifier` 的实际行为由 Authenticate 提供。凭据使用随机256位材料，库中只存 SHA-256 摘要；凭据材料需要由受信宿主管理，不作为口令认证方案。
- 本地管理员管理当前数据库对应的命名空间，管理权限定在该权威的资源树。管理员的安装身份不自动使业务 Evaluate 放行；业务仍同时需要策略和有效授权。
- 策略支持精确资源、枚举集合和登记树子范围，动作、用途及位置是有限集合。资源父子关系只来自受保护的登记，不能由业务请求提供。硬性拒绝优先；未知限制、未登记资源、无法证明包含关系和求值超限均不得放行。
- 普通持续授权不包含签发权。显式 may_issue 仅允许本地签发其范围内、更短或相同期的普通持续授权，不能再赋予 may_issue；这不是跨节点派生、祖先撤销或单次消费。
- 包含关系保留选择器的未来含义：精确引用或有限集合不能签发可覆盖未来新增后代的树范围，即使当前展开结果相同。
- GetPolicy/GetGrant 属于本地管理员管理视图，不向普通执行主体开放全部元数据。LookupOperation 核对当前身份及管理权限，未授权主体不能靠操作 ID 得到回执。
- `proto/harness/v1/authorization.proto` 是本票固定结构来源，保留 AuthorizationService 的具名管理方法描述。实际仅实现进程内二进制绑定，尚无 gRPC/WebSocket 服务。

## SQLite 与原子提交

参考 Adapter 固定 **modernc.org/sqlite v1.58.0**，传递依赖及校验值见 go.mod/go.sum。该驱动不要求 CGo 或独立数据库服务，参见[驱动文档](https://pkg.go.dev/modernc.org/sqlite@v1.58.0)。事务配置采用 WAL 和 synchronous=FULL，机制依据见 [SQLite WAL](https://www.sqlite.org/wal.html)；本项目的实际进程恢复证据以验证报告为准。

AuthorizationStore 使用独立快照读取与版本条件提交；一个 SQLite 行内原子保存身份、策略、授权、操作语义、回执、变更记录及关闭水位。并发提交以数据库版本 CAS 冲突重试，并重新认证与判定；业务修订独立于存储修订，认证/读取不会改变业务预期版本。

这是有界单机参考实现，单快照最大16 MiB，操作明细数量受配置工作量上限约束。私有持久格式为带 Format=1 的 Go gob，并注册生成类型的 oneof 包装；它不是对外协议或跨语言存储标准。后续更换表布局/后端必须保持同一提交契约并提供格式迁移，不能直接更换格式后声称兼容。

存储目录由受信宿主管理；数据库和凭据须为受限文件，已有宽权限数据库或符号链接被拒绝。数据库含身份摘要及操作认证密钥，不是加密数据库。本地文件保护不防御已控制宿主或持有存储接口的任意进程内代码。凭据加密库、节点私钥及其轮换属于后续票。

## 身份、窗口和恢复顺序

管理处理顺序为认证/当前权限、原操作语义比较与去重、新操作窗口及版本检查、原子提交。同操作重试不重复变更，即使业务修订已推进也返回原回执；相同操作身份换主体或语义被拒绝。执行授权不隐式变成管理权限。

SDK 从受信权威取得不透明 operation_id，使用标准 HMAC-SHA256 绑定权威、命名空间、窗口世代和随机部分；窗口不可由请求改写或换到另一数据库权威使用。这只是本地操作身份认证，不是跨节点签名授权。

操作身份要求唯一的规范 Base64URL 编码；换行和非零尾部填充位等替代写法被拒绝，不能用同一认证材料构造不同去重键。

一个权威同时维护一个开放窗口，过期或显式关闭后新建下一世代，持久 ClosedThrough 不回退。已接纳操作仍可在当前权限允许时核对。CloseWindows 与 CleanRecords 是独立受权管理提交：先落盘关闭水位，再清理已关闭且超过保留期的已终结管理记录及对应变更记录。本票没有待传播远端材料，故不会清理仍在远端执行或需消费核对的记录；后续扩展必须加入相应恢复义务。

明细已清理时，原身份仍能验证窗口，但返回 ADMISSION_EXPIRED，不能把找不到记录解释为可重新执行。长期授权的到期与接纳窗口期限分别检查。时间来源可替换，持久时间水位拒绝回拨；本地权威重启重新读取本地策略和受信本机时间，不依赖云端。它不防御已控制宿主的时钟/备份回滚，也不声明远端离线时间资格。

容量在变更后的独立快照上检查，再原子提交。记录已满时仍可通过公开清理接口回收满足条件的明细；若回收后仍无空间保存清理回执，整个变更失败，不提交部分清理。

## 验证与限制

`local-auth-v1` 使用隔离的真实 SQLite 文件，记录实际 sqlite_version()、驱动与依赖版本、环境和配置；覆盖初始化重开、身份伪造、管理范围、持续使用、硬性拒绝、期限、范围包含、求值上限、并发修订、重复/冲突、窗口关闭与清理。

四项进程测试在子进程提交前或提交后退出，父进程重开数据库，经 SDK 验证状态和回执。`contractcheck auth-crash-probe` 是显式验证用子进程入口，只由 profile 对临时目录内的计划调用；故障注入位于验证 Store 包装器，不在生产授权实现中。使用受控测试时钟，不把这些测试等同断电、任意磁盘损坏或敌对宿主验证。

本地管理结果为 local_authorization_commit，与 01 的 contract_fixture 分开。跨节点签发/委派、单次消费、任务控制、浏览器登录及真实服务绑定不受支持；请求不能静默转成更宽持续授权。完整 M1 或后续里程碑不能由本票通过推导。

## 验收记录

2026-09-10 在 linux/amd64、Go 1.26.1、protoc 36.1 下运行 `make verify`。实际源码修订为 `43dd75e1080a9a424e929d6ea31c3fa311d1c1dc`，构建时工作区 clean；归档文档在验证后提交。驱动 modernc.org/sqlite v1.58.0，运行时 SQLite 3.53.4。

- 依赖校验、生成代码一致性、编译、go vet、全量 `go test -race ./...` 和独立 SDK 样例均通过，见[验证阶段报告](evidence/02-verification-stages.json)。
- [本地授权报告](evidence/02-local-auth-report.json)：30 项必需检查通过，包含 4 项真实 SQLite 子进程恢复检查；另 1 项分布式能力明确为 unsupported，不计入通过数。
- [SDK 回归报告](evidence/02-sdk-regression-report.json)：32 项必需契约检查通过，原有未实现、未运行、不支持边界保留。
- 操作身份规范编码、有限资源集合不能签发未来子树、满容量记录可公开清理三项回归在 Go 测试中执行，不额外计入上述 profile 数量。
- [双轴审查](02-local-authorization-review.md)：Standards 的 2 项发现和 Spec 的 3 项 P1 均已修复并复核，未解决 0 项。
