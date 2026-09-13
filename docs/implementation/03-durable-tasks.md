# 03：提交并查询持久任务

对应 [03 票](../../.scratch/harness-implementation/issues/03-tasks.md)。本实现接纳任务并持久保存 QUEUED 状态、初始工作、执行记录及回执；它不启动 Worker、Brain 或外部动作。

## 使用与验证

```sh
go run ./cmd/contractcheck -profile durable-tasks-v1
make verify
```

profile 在受限临时目录初始化本地身份、策略和授权，通过 SDK 的 Protobuf 二进制绑定提交任务并核对结果。实际宿主可复用 02 的 `authctl init` 和授权管理能力，在同一数据库上组装：

```go
// store: sqliteauth.Open 的真实文件库；authCfg: 已持久固定的 02 配置。
// credential: 宿主安全读取的凭据，不来自任务消息。
authority, err := authorization.New(store, authorization.SystemClock{}, authCfg)
// 检查 err。
service, err := tasks.New(authority, tasks.Config{
    Namespace: "local", Resource: "root", Owner: "local-owner",
    MaxTasks: 100, MaxPage: 10,
})
// 检查 err。
client := sdk.NewTaskClient(tasklocal.Bind(service, credential), "local")
operationID, err := authority.NewOperation(ctx, credential)
// 检查 err，并在发送前保管原操作身份及其语义，供未知结果时核对。
task, err := client.Submit(ctx, tasks.Submission{
    Namespace: "local", OperationID: operationID, Goal: "整理回答",
    InputRefs: []string{"evidence/reference"},
    Constraints: tasks.Constraints{
        MaxSteps: 3, DeadlineUnix: time.Now().Add(time.Minute).Unix(),
    },
})
// 已知成功后可 client.Get(ctx, task.Ref)。结果未知则用原 operationID
// 调用 client.LookupOperation；不得改用新的操作身份猜测重试。
```

安装身份不会自动获得任务业务权限。预先为主体配置匹配 `Resource` 的允许策略和 continuous Grant：动作 `task.submit`、`task.read`，用途 `task`，位置 `local`，有限有效期。此参考部署内，资源映射由受信宿主配置并持久固定；普通请求不能选择别人的资源根或伪造 owner。查询还限定原创建主体，暂不提供任务共享。

目标为非空 UTF-8，最大 64 KiB；最多 64 个有界输入引用，MaxSteps 为 1–10000，首次接纳要求 DeadlineUnix 尚未到期。输入引用只保存为有序、不透明引用，不在本票读取产物；步数和任务期限作为后续运行约束保存，不声称已执行预算控制。固定消息未知字段、未实现方法或缺失约束被拒绝。

## 模块与原子边界

`tasks.Service` 提供 TaskService 的 Submit/Get/LookupOperation，并实现受信 RunStore 的 Load/Commit/LookupCommit/ListRecoverable。消费者依赖 `RuntimeStore` 接口，其回调参数 `RuntimeTransaction` 也是公开行为接口；外部实现不需构造私有授权对象。SDK 依赖 Transport，tasklocal 依赖消费方 TaskService 接口。没有 SQL、连接或驱动错误泄漏到领域参数。

本地参考实现使用 `authorization.UpdateRuntime` 的受信事务回调，在 02 已有 SQLite CAS 快照内共同保存运行分区和操作身份占用。每次回调都基于独立快照；管理修改抢先提交会导致 CAS 冲突，回调重新读取认证、策略、窗口并判定，不能将旧判定无条件写入。时间使用每次尝试读取的受信本机时间，保留回拨拒绝语义；本票不证明敌对宿主时间安全。

运行分区为有版本的私有 gob 数据，授权快照可向前读取新增分区为空的旧文件。首次成功访问固定任务 Config，后续使用不同资源、owner 或容量配置被拒绝；这不是在线迁移机制。运行分区最多 8 MiB、共同快照最多 16 MiB，最大任务数须显式配置且不超过 10000。容量不足拒绝新接纳，不清理未完成任务。旧版程序不理解新运行分区，不支持写入降级；升级前应备份并单向使用新二进制。

所有业务任务接纳、授权变更和窗口操作共享一个 CAS 顺序。运行记录的变化、业务操作索引、内部提交回执、初始执行记录和工作与操作身份占用一起提交；失败丢弃整个快照。未引入跨库事务或外部队列，代价是单个权威内读写共享快照且读取也更新受信时间水位，适用于有界单机参考实现。替换存储必须保持同样原子性和冲突重验证，不只是实现同名方法。

## 身份、授权与恢复

- operation_id 在命名空间内唯一，授权管理与任务接纳共同核验占用，操作类型或载荷改变产生冲突，换主体不获得新操作。规范编码和窗口认证沿用 02。
- change_id 只在任务内标识原子提交，不出现在 TaskRequest/TaskResponse；内部 Load 返回 LastCommit 供 LookupCommit 恢复核对。原内部提交重试先核对身份和语义，再判断新版本前提；空输入引用统一为同一种语义表示，避免 gob 将空切片恢复为 nil 后误判冲突。
- 创建明确要求不存在，初始 Version=1、OwnerEpoch=1、State=QUEUED。RunStore 不是远程业务接口；本票拒绝既有任务的任意状态覆盖、伪造拥有者及终态推进，后续 04 按所有权和 Worker 资格扩展受约束变更。
- Get 与 LookupOperation 核对当前 `task.read` 和原创建主体。Submit 重试核对当前提交及读取权限，原操作已成功时可返回原任务，即使原窗口或任务期限已经结束；这不授权继续执行任务。
- 未接纳的旧窗口操作被拒绝。已接纳任务的身份占用、业务/内部回执及工作独立于短期管理回执清理保留，窗口关闭不会取消任务。任务清理、归档及销毁整个权威后的身份迁移不在本票支持范围内。
- Lookup 未查到不构成“旧请求永远不会提交”的证据。结果未知时继续核对或按原身份与原语义重试；驱动异常保留 OUTCOME_UNKNOWN，不制造假成功或自动新建操作。

ListRecoverable 是受信内部接口，使用命名空间、明确页上限和 task_id 排序的 After 游标。静态集合分页无重复、无遗漏；并发新增且排序落在已扫描位置之前的任务须下一轮从头扫描发现，不承诺跨页一致快照。游标只是范围内的位置，不授予权限；返回候选也不领取工作。

## 协议与验证边界

`tasks.proto` 定义持久任务请求和快照。DurableTaskService 是本票具名描述，01 的 TaskService/Submit 内存契约描述及 SDK 样例保留；后续协议统一须显式处理兼容性，不能将旧夹具证据静默升级成真实接纳。实际只提供本地 Protobuf 绑定，返回证据 `durable_task_admission`，不实现 gRPC/WebSocket。

Go 测试经公开 TaskService/RunStore 覆盖提交、内部提交核对、同操作并发、相反方向的跨模块身份冲突、当前权限、窗口清理、分页、容量和非法终态。通过包装真实 Store 暂停一次提交，与另一数据库句柄的策略修改、主体失效或窗口关闭交错，核对 CAS 重验证阻止旧资格提交；故障包装不进入生产服务。

`durable-tasks-v1` 有 13 项必需检查，包含 2 项真实子进程提交前/提交后中断。`contractcheck task-crash-probe` 为显式验证子进程入口，计划和凭据只保存在受限临时目录，测试结束删除。这里的终止测试不等于断电、磁盘损坏或敌对宿主验证。Worker 执行单独标记 not_implemented，后续 04 扩展。

## 验收记录

2026-09-10 在 linux/amd64、Go 1.26.1、protoc 36.1、modernc.org/sqlite v1.58.0（SQLite 3.53.4）下执行 `make verify`。报告的源码修订为 `ea82bcfbc6e0d02c44702ae0732f7c150d763a9e`，构建时工作区 clean；本段及归档证据随后提交。

- 依赖校验、生成一致性、编译、go vet、全量 `go test -race ./...` 和 SDK 样例通过，见[阶段报告](evidence/03-verification-stages.json)。
- [持久任务报告](evidence/03-durable-tasks-report.json)：13 项必需检查通过，包含 2 项真实 SQLite 子进程中断恢复；Worker 执行独立标为 not_implemented，不计入通过数。
- [SDK 回归](evidence/03-sdk-regression-report.json)的 32 项、[授权回归](evidence/03-auth-regression-report.json)的 30 项必需检查全部通过，保留原有未实现/未运行/不支持边界。
- Go 测试另覆盖并发提交、策略/主体/窗口与提交交错、容量、原主体读取、内部同提交语义、禁止任意终态及独立事务实现。这些不重复计入 profile 数量。
- [双轴审查](03-durable-tasks-review.md)：Standards 和 Spec 各 1 项 P2，均已修复并复核，未解决 0 项。
