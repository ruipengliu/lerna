# 10：执行并确认同步 API 动作

本票建立固定能力的同步执行闭环：SDK 接纳操作，Execution 持久登记工作，在同一单机原子边界核验当前任务资格并消费许可，Driver 改变独立目标，Inspect 核验效果，Outbox 将报告交给 Core。参考能力为 `counter.add@1`，实现为 `sqlite-counter@1`，输入为有界增量，目标具备版本前提及独立持久操作记录。

## 组装与接口

`execution.Service` 依赖 Authority、Core、Content、Driver 四个消费方接口及有界配置；不依赖 SQLite、HTTP 或供应商 SDK。能力/实现版本、输入输出 JSON Schema 2020-12 和完整描述摘要固定，当前组装只支持本地、同步、独占资源。配置不支持的处理位置或能力保证明确拒绝，不推断可以切换另一实现。

| 边界 | 职责 |
| --- | --- |
| CapabilityService / CapabilityClient | Invoke、GetInvocation、Reconcile；本地二进制 Protobuf 绑定；读取只返回受控引用和元数据 |
| Execution 协调模块 | 接纳、工作、单次启动、核对次数、报告及 Outbox 的持久推进 |
| ExecutionDriver | Start 改变目标；Inspect 只查询原操作；无自动副作用重试 |
| GrantAuthority / ExecutionTransaction | 真实许可及当前政策核验；单机执行/任务分区共用 CAS，保持不同账本格式 |
| Core GuardExecution / ConsumeExecution | 资格与动作预算绑定、接收原操作报告、幂等推进任务 |
| executioncontent | 读取受控 JSON 输入，按来源、位置、用途和留存保存输出/效果证据 |
| simapi | 独立 SQLite 业务目标，初值 0、版本 1；成功变更才增加目标版本 |

可运行组装见 `profiles/executioncheck`。运行器是受信本地模块端口 `Run`，不作为公共 RPC 任意启动 Driver；Invoke 只接纳及安排可恢复工作。ListRecoverable 有界枚举，处理前仍须核验资格。LookupCommit 核对原 admit/start 提交，业务重放返回原接纳回执。

```go
client := sdk.NewCapabilityClient(executionlocal.Bind(service, "local"), "local")
receipt, err := client.Invoke(ctx, request, signedMaterial)
// 接纳完成后，受信执行调度端调用 service.Run(ctx, request.OperationID)。
// 回包未知先查询原操作，不能换 operation_id 重发动作。
current, queryErr := client.GetInvocation(ctx, request.OperationID)
_ = receipt
_ = err
_ = current
_ = queryErr
```

请求固定 operation_id、任务/Worker 工作代次资格、能力/实现版本、描述摘要、精确输入引用和资源预期版本。语义指纹使用固定 Go 结构的 JSON 编码再 SHA-256，不包含可更换的签名材料；字段顺序固定，不依赖 map 顺序。材料变化不改变原业务语义或原已绑定许可。宿主提供实际主体、命名空间、接收方、出示者及 Worker；普通协议请求不能覆盖这些身份。

参考宿主使用本地受信身份绑定，测试中的证书摘要只是该绑定的固定标识；没有声称进行了新的网络 mTLS 握手。使用真实 ES256 签名适配器与现有 GrantAuthority 验证材料及接收方，私钥只存临时受限文件，不进入报告或业务账本。

## 原子启动与预算

Invoke 先执行当前 `capability.invoke` 授权与操作身份核对；已接纳请求额外检查当前 `capability.read` 后返回原回执。新操作校验版本/资格、输入和容量，再由 GrantAuthority 预留一份绑定原操作与接收方的真实许可。最终接纳时复验许可与当前任务资格，保存接纳、去重、回执及固定输出内容操作身份。原授权预留与执行接纳不是一次事务，失败/未知时保留预留，不自动退款。

ExecutionTransaction 为单机参考实现提供一个受信本地事务：授权数据、独立 ExecutionData 和已有 RuntimeData 共用 authority 的 Store CAS，业务格式分别管理。它允许同一合法执行操作从签名预留进入执行分区，同时继续拒绝管理、任务、内容等不同业务复用身份。伪造的 UsePermit 与持久分配不匹配时拒绝。

Run 在事务外重新读取/核验受控参数，随后原子复验当前主体、许可及祖先限制、任务拥有者/版本/控制、Worker/租约、资源独占状态和预算，并同时写入单次消费、工作绑定及“可能已发送”。这次提交错误或结果未知时不调用 Driver，需核对原 start 提交。外部动作始终发生在事务外。

本票明确采用每个已计费的 effect-aware Worker 代次最多一个执行动作：领取该代次已经消费 Task.Attempts/MaxSteps 的一份；GuardExecution 将原操作绑定该代次，不增加隐形动作额度。一个代次可接纳竞争候选，但最多一个真正启动，其余不能以另一许可复用该份任务预算。Task 预算与 Grant 单次额度必须同时满足，不重置已用步数或未知份额。多动作提案和模型选 API 留给后续票。

独占由同一持久 Execution 分区的 Active 操作串行化，跨同库协调实例也不能同时授予；目标还原子核验资源版本。目标和 authority 使用独立数据库，不存在把启动账本和业务变更一并提交的捷径。仅适用于所有 Harness 变更都由此协调方控制的私有模拟资源，不是跨任意第三方入口的资源锁。

## 事实、恢复与交接

执行报告区分 Phase、Result、Effect。只有 Inspect 根据独立目标记录确认实际变化，且输出 Schema 和受控保存均通过，才形成可交给 Core 的成功结果。目标成功返回但 Inspect 不明确时保持 UNKNOWN；输出不合约或证据不能保存时可以保留已确认效果，Result 为 FAILURE，不伪造任务完成。

模拟目标持久保存原操作指纹、是否发生、当时结果及版本。记录存在且匹配才能核验；查无记录不证明旧请求以后不会执行，因此发送前崩溃也可能保持 UNKNOWN。目标在其有限记录容量内不清理幂等历史，原相同操作再次到达不再次变更；这项模拟目标保证不外推为任意外部 exactly-once。

Run 对已登记启动的操作只返回当前事实；Reconcile 只 Inspect，不重新 Start。恢复尝试先持久扣减次数，丢失核验回包也不返还次数。不配合退出的 Driver/Inspect 占住单个进程槽位直到实际返回，调用方在配置期限后得到未知状态；持久资源占用保持到可靠效果/未执行证据。迟到 Driver/Inspector 返回可以保存原效果，不依赖原任务仍有执行资格。公共 Reconcile 返回快照前复验 capability.read；受信核对只需收尾权限，不能借此向无读取权限的调用者返回结果。

报告与 Outbox 同次提交，Core 按原操作和报告修订消费，Core 成功后执行方另行确认。确认丢失重放原报告，不重新执行。Core 记录报告自身引起的版本推进，仅让同一未被外部更新打断的决策沿这些报告完成；任务更新/控制的版本变化不能被后来的 UNKNOWN 报告洗掉。旧效果照常保留，不能直接完成修改后的目标。

证据正文使用原输出内容操作身份，受控保存继承输入来源；查询 Task/Invocation 的完成状态不保证内容现在仍可披露。来源撤销/到期可导致原结果不可读，错误、回执及 Outbox 仅保留固定状态与引用。当前受控内容与执行启动之间是有界新鲜度检查，不声称跨政策权威和目标事务的全局原子撤销。

## 支持范围与容量

| 参考配置 | 数值 |
| --- | --- |
| 能力/实现 | counter.add@1 / sqlite-counter@1 |
| 输入 / 输出 / 单份效果证据 | 32 KiB / 8 KiB / 8 KiB |
| 输入/输出 Schema 文档 | 各至多 16 KiB，受信静态配置，无远程加载 |
| 执行操作 / 每操作报告 / 待交接报告 | 32 / 8 / 64 |
| 每操作主动核验 | 4 次，耗尽后保留原事实 |
| Driver 与 I/O 截止 | 各 1 秒 |
| 每进程 Driver/Inspect 槽位 | 1，超时不等于释放实际调用 |
| 每代次动作 / 每任务 Worker 代次 | 1 / 3 |
| Execution 分区 / 模拟目标操作记录 | 4 MiB / 64，容量满拒绝新增 |
| 受控结果留存 | 最多 5 分钟，且不晚于输入来源 |

不支持异步作业/取消协议、共享资源接管、远端 quota 回收、跨端传输、真实第三方服务、GUI 或千级能力选择。没有新的付费模型或真实业务 API 调用。单机 SQLite 可整体备份；旧版本不了解新增分区，回滚写入应配合升级前一致备份。

## 可重复验证

```sh
go test ./authorization ./tasks ./profiles/executioncheck ./sdk
go build -o build/contractcheck ./cmd/contractcheck
./build/contractcheck -profile synchronous-execution-v1
make verify
```

额外集成测试覆盖两个独立协调实例竞争、最后一次 Inspector 超时后的迟到确证、内部存储等待截止，以及撤销读取权限后公共核对不泄露快照。

具名 profile 覆盖实际目标变化、SDK 重放/拒绝、并发启动、单次代次预算、当前授权/控制/输入、资源版本、未知保持、迟到效果、存储启动失败/提交未知、Outbox/Core 幂等交接、协议未知字段及伪造回执。五个真实子进程分别在接纳后、启动登记后发送前、目标已提交后、报告/Outbox 提交后、Core 消费后确认前退出 73，随后独立重开 authority 与目标核对。

升级样本使用实际 `19a323b` 旧编码器生成的已接纳输入账本；新 Execution 完整运行前后逐项比较旧 Runtime 记录，验证分区未覆盖。01–09 原 profile 继续纳入完整回归。最终运行环境、准确结果和审查修复记录见下文。


## 最终验证记录（2026-09-10）

实现提交 `58bd30a`，审查修复 `676dc67`。最终完整 `make verify` 退出 0，报告构建版本为 `676dc67606bb4e4a391c5f520563d1100a7d7325`、dirty=false；环境 linux/amd64、Go 1.26.1、protoc 36.1、生成器 v1.36.11。各报告包含完整依赖摘要及逐项输入、预期和实际结果。

10 profile 必需项 35/35 通过，01–09 回归 247/247 通过，共 282 项。生成一致性、全包编译、go vet、完整 `go test -mod=readonly -p 1 -race ./...` 和 SDK 样例全部通过。五个真实子进程退出/SQLite 重开、旧编码升级、独立目标变化及审查新增的四项集成回归均实际运行。

证据：[10 契约报告](evidence/10-synchronous-execution-report.json)、[验证阶段](evidence/10-verification-stages.json)、[完整验证日志](evidence/10-verification.log)、[独立审查](10-synchronous-execution-review.md)。同目录 `10-*-regression-report.json` 保存本轮 01–09 回归。

初审 Standards 0 项、Spec 3 项；三个问题均有失败复现和修复后回归，独立复核后剩余均为 0。本票未调用真实业务 API、设备或付费模型；模拟状态与恢复证据不表示千级 API 选择质量已达标。
