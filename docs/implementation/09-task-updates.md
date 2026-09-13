# 09：接收补充输入并重新决策

本票为 TaskService 增加正式输入、等待项回答和有权预算调整。输入回执、任务版本、等待项变化及工作安排在同一 SQLite RuntimeTransaction 中持久化；后台通知不是恢复依据。参考组装是 Go 单机、受控内容服务和本地二进制 Protobuf SDK，不需要外部数据库或真实模型。

## 接口与权限

| 入口 | 语义 | 当前权限 |
| --- | --- | --- |
| `SubmitUpdate(intent=append)` | 追加事实及精确受控引用；不替换目标 | `task.append` |
| `SubmitUpdate(intent=revise)` | 替换目标或必需约束的受控引用 | `task.revise` |
| `ProvideInput` | 回答指定等待项，仅消费该身份一次 | `task.input` |
| `AdjustLimits` | 显式设置步数、请求、token 或期限 | `task.adjust` |
| `Lookup` / `Interactions` | 原回执 / 当前交互元数据 | `task.read` |
| `CreateWait` | 受信模块提交问题，经 Core 准入 | `task.execute`；没有业务 RPC |

参考绑定只支持同一认证任务主体；提交载荷不能指定事实主体。`InputFact.Subject` 从认证及任务权威绑定取得。不同入口的权限不互相隐含，普通输入不签发授权、不解除暂停或取消，也不建立委派身份。新目标正文只保存在受控内容服务，Task 保存 `GoalRef`；`GuidanceRef` 表示必须进入下一轮的约束文本，不修改执行权限。

所有变更携带稳定 `OperationID`、`Ref`、严格 `ExpectedVersion`。先认证/授权，再核对原操作；同操作同载荷返回当前读取权限允许的原回执，跨类型或不同载荷复用拒绝。新操作再检查终态、版本、当前约束与容量。冲突不部分写入，有权调用方通过 Task SDK `Get` 获取当前版本再决定是否提交新操作。更新回包未知时先 `Lookup` 原操作，不能换身份盲重试。

```go
binding := tasklocal.Bind(core, token).WithUpdates(updates)
client := sdk.NewUpdateClient(binding, "local")
receipt, err := client.SubmitUpdate(ctx, tasks.UpdateRequest{
    OperationID: operationID, Ref: current.Ref,
    ExpectedVersion: current.Version, Intent: "append",
    InputRefs: []string{controlledReference},
})
// err 为提交未知时：client.Lookup(ctx, operationID)。
// 下次新的业务变更使用新操作身份和当前版本。
_ = receipt
_ = err
```

Protobuf 使用独立请求 oneof，额度字段使用 `LimitValue{Present, Value}` 区分未提供与显式零值；省略却携带非零值、未知意图和非法组合明确拒绝。SDK 核验消息关联、响应类型、命名空间、回执操作/任务/等待项身份和元数据形状。新增 RPC 声明不意味着本票已实现网络 gRPC 服务。

## 等待项与受控上下文

等待项保存独立随机身份、受控问题引用、答案约束、回应主体、到期时间和依赖。支持 UTF-8 文本或有限 choice 集合；创建请求用受信 `ChangeID + Qualification` 幂等准入，业务回答不能自行创造问题。生命周期为 pending、answered、revoked、expired；查询的 expired 从持久截止时间及受信时钟派生，重启不使其重新有效。过期不会自动解除必需问题的阻塞；本票没有自动重新提问调度器。

依赖 `goal` 在目标/必要约束修改后撤销；`facts` 在事实或回答变化后撤销；`input` 绑定精确且不可变的证据引用，无关更新保留该等待项。回答仍需最新任务版本，追加事实不会伪造一次回答。新问题总生成新身份，不复用已回答/撤销身份。等待项只存引用和有界 Schema；查询选择项前重新检查问题内容读取资格，不把问题正文放进普通任务诊断。

`answers.InputValidator` 在事务外有界检查精确内容修订、任务资源、目的、媒体类型、大小、UTF-8、来源和本地处理/披露权限。提交主体不能靠引用绕过内容政策；下一轮组装与模型调用/正式发布前继续检查当前位置及来源资格。跨权威检查仅承诺有限本地新鲜度，不承诺与 Core CAS 的分布式原子撤销。

已消费回答将问题引用与 interaction_id 持久绑定在 InputFact 中，原问题也作为受控输入保留。即使多个问题共用同一答案引用，模型仍收到逐项 ReplyTo 关联与问题正文；问题来源、处理位置和大小同样参与全链路检查。

上下文块携带实际主体及 evidence/append/revise/reply/goal/constraint 角色。追加事实保留事实角色；当前目标引用覆盖模型 Goal，必要约束与执行 Constraints 一起完整传入。旧引用保留为历史事实及来源依赖，连续替换也受 16 个引用总界限制；不会默默裁掉超界输入或过期内容。受控正文不可用、处理位置不允许或完整输入装不下时停止生成。

## 运行与额度不变量

更新推进任务版本并记录 `UpdateVersion`。未发出旧工作失效；在途工作保留原代次、参数、输出操作身份和预留，进入 reconciliation。Runner 在轮询边界感知普通更新，取消旧 Brain。实际停止、用量结算、输入接纳及下一次领取是不同事实：取消不获响应时保留在途槽位，迟到结果只结算原预留，不能发布旧答案。可信旧外部效果证据仍可持久核对，但不能完成更新后的目标。

新决策通过当前授权、版本、控制、输入、预算和期限检查，重新领取工作代次并取得新的输出操作身份。原控制和不相关等待原因保留，已用步数和冻结的 Worker 重试上限不重置。只有相关阻塞解除才能排队。

额度不得小于 `已用 + 未结算占用`，步数不得小于已发生次数；低于下限原子拒绝，保留原约束。额度为零不表示无限制，脚本/模型模式不能通过调整互换。合法增加只增加可用份额，不提升授权。缩短期限触发普通更新停止机制，后续请求通过当前期限准入；延期不重开终态、不解除暂停/取消。没有远端额度实现，也没有“已回收”伪造入口。

| 参考配置 | 上限 |
| --- | --- |
| 每任务正式更新 / 等待项 / 输入引用 | 32 / 16 / 16 |
| 单回复 / choice 数 / 完整上下文 | 4096 字节 / 16 / 32 KiB |
| 单输入内容 / Task 内部提交记录 | 32 KiB / 256（输入与等待准入检查） |
| 输入检查 I/O / 普通更新轮询 / 停止等待 | 1 秒 / 100 ms / 1 秒 |
| Worker 并发 / 工作代次 | 1 / 3 |
| 每模型请求输入 / 输出预留 | 600 / 100 token（受控模型） |
| 宿主步数 / 请求 / token 硬界 | 10,000 / 64 / 1,048,576 |

容量不足明确拒绝，不删除去重记录来重复接纳。UpdateLimits 在首次接纳时固定；与已经冻结的控制恢复配置不一致时拒绝。旧任务最多 64 引用的读取兼容保留，超过新引用界限时仍可调整额度，但不能继续扩张引用集合。Worker 运行参数和控制恢复配置沿用各自冻结边界。

## 验证与兼容

```sh
go test ./tasks ./profiles/updates ./sdk
go build -o build/contractcheck ./cmd/contractcheck
./build/contractcheck -profile task-updates-v1
make verify
```

`task-updates-v1` 覆盖 SDK 输入/目标/回答上下文、并发单次消费、回执重放、局部失效、过期与来源撤销、版本及终态冲突、额度下限/显式零/暂停、在途目标与期限变化、拒绝配合的迟到生成、旧产物发布竞争。还覆盖 start 与生成预留之间的更新收尾，以及两个不同问题共用答案引用时的问题关联。四个真实 SQLite 子进程分别在输入提交、回答消费、旧生成未知、额度提交后退出 73，重开核对回执、原工作身份和账本；不以清空状态模拟恢复。

升级样本由 `dea600f` 旧编码器实际生成，含旧任务/内容引用/工作/生成预留；验证新增字段默认值、旧身份与 700 token 未知占用保留，以及新额度操作提交后重开重放。01–08 原 profile 保持在 `make verify` 内；不迁移或重新解释原生成身份。旧版本不能继续写入新版本状态，部署回滚需配合升级前备份。

本票全部模型为可控本地实现，未发出新的付费模型调用。UI、可靠推送、WebSocket、跨主体协作、设备执行及远端预算回收未验证。执行结果与审查记录随最终证据归档，不将已存在的 08 真实模型结果作为新交互验证。

同一授权实例对 Runtime/内容/控制操作使用可取消的串行准入，避免控制轮询与密集内容核验相互耗尽本地 CAS 重试。多个实例仍依赖 Store CAS；该排队不替代持久版本校验，也不在事务内执行模型或内容 I/O。

## 最终验证记录（2026-09-10）

实现 `3674aff`、`8028fd6`，审查修复 `cb32358`。最终完整 `make verify` 退出 0，报告所用构建版本为 `cb3235853e0bd95272ede7acf54ae588f598a5fe`、dirty=false，环境 linux/amd64、Go 1.26.1、protoc 36.1、生成器 v1.36.11；逐项依赖摘要随报告记录。

09 profile 26/26 必需项通过；01–08 回归 221/221 必需项通过，共 247 项。生成一致性、全包编译、go vet、完整 `go test -mod=readonly -p 1 -race ./...`、SDK 样例均通过。旧编码升级、四项实际子进程退出/SQLite 重开与受控模型竞态均实际执行；未调用付费模型。

证据：[09 契约报告](evidence/09-task-updates-report.json)、[验证阶段](evidence/09-verification-stages.json)、[完整验证日志](evidence/09-verification.log)、[审查记录](09-task-updates-review.md)。同目录 `09-*-regression-report.json` 保存全部 01–08 本轮回归。

实施中补测曾复现 SDK 快照新增字段校验、轮询/内容请求 CAS 竞争、start 后预留前更新，以及缺失问题关联问题；最终证据记录修复后配置，未用初次失败或旧报告替代通过结果。两个独立审查维度的剩余问题均为 0。
