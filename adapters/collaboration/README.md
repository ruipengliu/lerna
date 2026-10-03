# 同 owner 协作适配

`New(Config)` 接收已经迁移的 Store、同版 Registry、固定 owner、受信 Service Auth 和纯事务 `SubjectGate`。`BindTask` 在宿主装配时恰好调用一次；随后配置为 `task.Ports.Collaboration`。事务参与者明确包含 `collaboration`、`task` 和身份记录所在的 `platform`，无需为 Go import 推断共享事务。

ChildHandle 的原 `session.create`、Session/Branch 身份、准确配置、原主体代次/角色和 deadline 先保存在 `collaboration.command_intents`，随后在事务外调用真实 Dispatcher。丢失发送或查询答复时沿同一个 command 查询，准备态不能被猜成 open。跨 owner 和非 internal Delegation 在责任准入之前返回 unsupported。

Internal Delegation 仍由 Task 原事务建立子 Task 与预算。continue_existing 的 steer/answer 沿准确子映射、原 request、成果引用、输入来源和固定期限产生唯一转交 Command；源门禁和原主体当前代次先由纯 Tx 核验。原消费方 accepted 时继续等待，只有准确 applied 才结束转交；原 rejected 回执保存为独立失败事实，坏答案不消费请求，正确新答案仍可提交。所有恢复以原回执为依据，不刷新期限、借服务身份改变原输入主体或重建目标。

默认 Delivery 使用真实同库 Dispatcher。额外 Delivery 可以实现故障注入或受控传输；其 Lookup 必须允许受信工作者核对已接纳原命令，不能因为原提交者撤权而丢弃尚未归并的事实。开始新的发送仍必须核当前源门禁和主体。

`internal/task/collaboration_adapter_test.go` 在持久 SQLite、实际 Interaction、Memory/ObjectStore、Task 和 DevIdentity 上验证原 Session 丢答复恢复、内部 Task 映射、原 steer 与答案消费，以及准确 rejected/修正答案。边界中的目标文本和证据规则采用明确预批准夹具；该测试不证明文本质量、多 owner 协议或付费供应商。
