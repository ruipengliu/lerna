# 12：共享资源接管与恢复

两个不同模拟 API（add 与 scale）操作同一独立 SQLite 业务资源，第三个同步入口验证在途同步请求。所有入口使用相同持久控制权威。TAKEOVER 接纳后共同准入立即拒绝新启动；目标端完成代次隔离、原调用效果被核对后才显示 APPLIED。RESUME 另行授权、重新观察目标业务版本，再恢复自动化准入。

## 契约与组装

Capability SDK 和本地二进制 Protobuf 增加 RequestResourceControl、GetResourceControl、LookupResourceControl。资源引用由 namespace/kind/key 组成，宿主通过 `Service.WithResourceControl(ResourceScope, ResourceDriver)` 固定资源、别名、权威、授权资源、用途、位置和准确能力描述摘要。请求不能自报“不冲突”。同范围可用较宽的授权资源表达保守范围；全部控制权限都按这个完整授权资源核验，未知入口拒绝，不隐式扩权。

配置在首次调用前冻结。不同协调实例必须提供相同范围配置；别名/权威/参与者冲突拒绝；有历史调用的未受控描述符不能在线重映射为受控范围。每个独立目标须有独立准确描述符，现阶段一个描述符只绑定一个控制范围。目标身份在独立数据库内保存，不能换 key 重开同一文件。新 GUI 驱动可实现相同消费方接口，完整 GUI/UI/网络部署不在本票内。

请求使用稳定 operation_id、TAKEOVER/RESUME、expected_version。别名先归一化，同身份同语义返回原接纳回执；不同语义或复用调用/取消/预留取消身份拒绝。新操作检查当前控制版本；旧 RESUME 重放仅返回当时回执，不重新打开后来 TAKEOVER 的资源。查询单独读取当前意图、ACCEPTED/APPLIED、控制版本、最近目标业务版本及阻塞数量，不返回其他调用的身份、关联、正文或许可。

ACCEPTED 表示已持久接纳。查询的 limitation 为 TARGET_UNCONFIRMED、INVOCATIONS_UNRESOLVED 或 BUDGET_EXHAUSTED，APPLIED 时为空。能力调用 control_version、目标业务 resource_version、Task/Worker 资格各自保留作用；不会通过改写旧资格恢复工作。

## 原子边界和目标证明

Execution 私有存储升级为 Format 2，按准确描述摘要保存原 Format 1 分区，并在同一原子权威事务中维护共享资源控制。CAS 保持最多 8 次尝试，冲突后使用 1–64 毫秒的可取消退避，避免独立实例在同一提交突发内耗尽全部尝试；调用方期限仍优先。解码分区在单次事务内复用，不重复解析自身分区。原始旧分区按原描述符保存，不换 Schema/版本，也不覆盖取消、许可或 Outbox。调用身份与取消预留的跨分区索引由持久记录重建；分区隔离不允许跨操作类型复用身份。

新启动登记与 TAKEOVER 接纳在同一 SQLite CAS 边界排序。接管先提交则启动拒绝；启动先登记即进入共享在途集合。已接纳未启动的旧版本调用只能按 PreStart 交接未发生结论。提交失败不发送，提交结果未知先通过原记录核对；已登记 Start 永不自动重发。

`ResourceDriver.ObserveResource` 是只读观察；`ApplyResourceControl` 在目标端将稳定控制身份和新代次持久化。模拟目标每次 Start/Complete 都核对实际目标控制代次；TAKEOVER 原子停止旧作业并拒绝以后才到达的旧 Start。即使查无作业，也只有更高目标代次才能提供“以后不会再发生”的否定证据。调用已有的完成事实保留，不回滚业务效果。作业查询和代次读取在同一数据库快照内完成，避免把旧快照的“无作业”与新快照的隔离代次拼成错误否定证明；关联句柄不匹配明确拒绝。

控制发送先持久标记 possibly sent，再进行 I/O。结果丢失后只观察原目标，不盲目重发。若进程在标记发送后、真正发送前退出，无法确认发生，保持 ACCEPTED；显式新控制可推进新版本，系统不凭超时重新发送。控制命令本身就是目标级处置身份；参考目标能原子隔离全部旧请求，因此不额外派生逐调用 Cancel。原有逐调用取消和 Task Cancel 身份、权限、预算均保留，核对沿用原操作。

目标代次与范围必须准确匹配，所有已启动原调用必须具有无冲突效果结论，才能落实接管。取消回包、租约过期、本地无槽位或查无记录均不独立构成证明。后到矛盾证据保留，并使 TAKEOVER 查询重新显示待核对，阻止 RESUME；不自动消解矛盾。

## 恢复与有限性

宿主对每个绑定实例调用 `AdvanceResourceControl` 和 `ReconcileResource`。后者只处理该受信主体自己的原调用、使用持久轮转游标，结果通过原报告交给 Core。`RecoverResources` 每轮访问最多 32 个参与者，每个最多 16 项；某范围失败不会跳过其他范围。调用方按照冻结的轮询周期安排下一轮。目标控制、业务核验分别计量；不重置原许可或已用核验额度。

参考配置：8 个范围、每范围最多 16 个参与者和 16 个别名、32 个控制操作、8 次控制核验；32 个描述符分区；每分区 32 次调用、32 次取消、每调用 8 个报告、64 个 Outbox、4 次调用核验。控制轮询 1 秒、租约 3 秒、窗口 1 分钟、I/O 1 秒；执行存储总量 4 MiB。目标最多 64 个作业。调用槽位有界，不能配合退出的 Driver 保留槽位。控制记录容量不足拒绝新增，未知不清理、不回收业务额度。

控制代次和核验租约只在已有权威内工作。失联不会按心跳创建第二权威；不支持任意分布式锁、在线重映射或自动权威迁移。Harness 外部写入不能由本地锁阻止：模拟用户在 TAKEOVER 期间独立修改业务数据；RESUME 重新观察，目标在同一事务中比较业务版本。观察后用户再次修改使恢复失败并保留待落实。旧业务前提不能覆盖用户修改。

资源恢复不改变 Task 的 PAUSE/CANCEL 或其他等待；Core 仍按原工作证据和合法新资格决定后续推进。

## 运行与证据

```sh
go test ./profiles/takeover ./profiles/asynccheck ./profiles/executioncheck ./sdk
go build -o build/contractcheck ./cmd/contractcheck
./build/contractcheck -profile resource-control-v1
make verify
```

本地故障入口使用已有空目录：`contractcheck resource-crash-probe <目录> <故障点>` 在指定边界退出 73；随后 `contractcheck resource-recover <同目录> <同故障点>` 重开权威和目标核验。参考恢复入口用于这些合成检查点，不是对任意生产目录的迁移工具；测试目录保留随机测试登录值和签名私钥，应保持私有权限。

具名 profile 有 31 项契约/边界/并发检查和 10 个真实子进程边界：接管接纳、启动登记、控制发送登记、目标停止、目标完成、接管落实、恢复观察、恢复发送登记、恢复目标提交和恢复落实。包含两个 API 真实变更同一资源、同步在途隔离、不同资源公平恢复、提交失败/未知、当前权限、Task 控制、迟到冲突和目标观察竞争。

旧编码测试数据由实际 `d59cf76` 构建的 11 票程序在 cancel-registered/report-saved 边界生成，未用新编码器重建。升级后写入第二分区，再验证原未知/完成效果、取消、许可、预算和 Outbox。实际同步旧编码仍通过既有 10 票快照回归验证。

## 最终验证记录（2026-09-10）

实现 `9e37227`，修复 `eed2658`。最终完整 `make verify` 退出 0，构建版本为 `eed2658`、dirty=false；linux/amd64、Go 1.26.1、protoc 36.1、Protobuf 生成器 v1.36.11，依赖及配置摘要保存在各 profile 报告中。

12 profile 41/41 必需项通过，其中 10 项为真实子进程退出与重开。01–11 的 305 项回归通过，合计 346 项。生成一致性、全包编译、go vet、完整 `go test -mod=readonly -p 1 -race ./...` 及 SDK 样例全部通过。实际 11 票两个旧编码快照、既有同步旧编码、目标同快照证明、有限可取消 CAS、Task 控制和权限等集成测试均已运行。

首轮完整检查的旧并发回归失败已复现并修复，旧双协调器测试连续 30 次通过后重新运行上述完整检查；初次失败日志单独留存，不替代最终证据。Standards 初始 1 项判断性建议、Spec 初始 1 项 P1，均修复且独立复核关闭，最终各 0 项。本票未调用真实设备、业务服务或付费模型。

证据：[12 契约报告](evidence/12-resource-control-report.json)、[验证阶段](evidence/12-verification-stages.json)、[完整日志](evidence/12-verification.log)、[初轮失败日志](evidence/12-initial-verification.log)、[独立审查](12-resource-control-review.md)。同目录 `12-*-regression-report.json` 保存本轮 01–11 报告。
