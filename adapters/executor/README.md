# 独立设备 Executor

此 adapter 把 Execution、有限 GrantLease、本机 Content 缓存和补传装配到独立 SQLite。设备不注册 Task/Brain/云端 Memory participant，不连接云端 PostgreSQL，也不决定云端 Task 成功。受信静态设备、P-256 密钥和 TLS peer 配对必须在管理配置中给出；缺失时在接纳业务前拒绝。

云端先在原 Task 准入事务中调用原 `AllocateLeaseTx`，固定一次预留。`SealAdmissionTx` 只签名和保存原准入：它同时绑定原 Orchestrator OperationIntent 摘要、Executor ExecutionIntent 摘要、原命令 ID、Capability/Binding/InstallLock、设备 owner/instance、原 GrantLease 和每个准确 Content 的用途及来源。它不再调用普通 Grant Use。传输凭据仅赋予 `executor_peer`；设备消费签名的原主体与代次，不复制用户或父服务的完整 token。

`executor.admission.install` 安装闭合签名 bundle。`executor.content.stage` 以原命令身份暂存最多 96 KiB 的原字节块；Job 在事务外核全量 hash/size 并完成 fsync，不提供任意内容签名入口。云端通过 `executor.admission.get` 确认原输入全部就绪，随后才签发原五秒 ControlWindow。`executor.control.install` 安装该准确 JWS；设备保留原 source/audience，不把本机 owner 冒充签发者。原 `execution.invoke` 原样进入设备 Dispatcher，其 principal 来自准确 bundle。

实际 StartBarrier 在设备事务内只核静态已登记签名、原 Task/Operation/lease/主体、最紧截止、本机已知撤权、Control/TaskGate 和资源 epoch。离线仅能运行已经完整缓存并接纳的原行动；五秒控制窗口、原 TTL 和有限分配均不刷新。过期后可以查询原 Attempt、迟到效果与用量；查询不再执行物理动作。

输出、证据和 UsageProof 保留设备的原 ContentRef 与准确来源。云端按原设备 owner 查询和补传，Task 与 Grant 只归并自己的账本。缺失来源正文必须返回明确缺口，不能生成同名替代 Content 或把 GoalRef 当作计费证明。真实双进程及故障验收结果将在工单 16 的完成依据中记录；本文的实现结构不代表外部真机、三 AZ 或生产资格。
