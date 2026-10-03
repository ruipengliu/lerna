# 票 05：真实处理门禁与旧 Host seam 的收敛

2026-10-03。授权决策代理检查实际demo/runtime/Host、04共享工作套件及05活跃草稿后采用。此决定不把05草稿视为完成；不新增07/08对05的业务先决。

## 结论

**不保留无门禁的旧Host消费者成功路径。** 现有 `host.NewWorker → demo.Worker.Claim → Project → demo.Worker.Complete` 最后会保存业务Projection并推进Job，它是实际演示消费者，不是仅观察锁或Claim的存储机制。增加NewScheduledWorker不能让这条同等可装配的旧成功路径永久豁免deadline、资格、尝试和停止门禁。仅README标注“低层”不够，也不能为了旧测试绿而保留它。

保留的机制边界是 `runtime.ClaimStore` 及adapter在准确Tx内的条件Claim/Renew/Complete。机制不必理解演示policy，也不把业务授权/失败分类塞进SQL runtime。消费方在同事务验证业务门禁后调用它，这符合原分层。

纯 `Project(work)` 是确定性hash计算工具，可以继续无I/O、无授权输入，不把它机械改成权限API。计算出某个hash不代表获得处理资格，更不代表允许持久提交。必须封住的是实际处理启动与保存业务成功事实的消费者入口。

## 最小收敛方案

05实施者选择当前代码最小的签名调整，满足以下行为即可：

1. Host实际处理装配必须显式取得有限policy能力和受信worker allowlist。没有权限对象/必要ScheduleRepository/ScheduleStore时构造失败或调用fail closed；不能以nil表示允许旧模式。
2. 所有消费者处理使用同一个 `Start → 事务外计算/分类 → Finish` 行为。Step/Run/Process是便利组织；直接Start/Finish可保留给需要控制同步阶段的Host设施，约束完全相同。
3. 原 `Worker.Complete` 可以删除/退役，或保留为**同一严格Finish成功分支**的便利封装。若保留，必须要求已有该准确Claim/epoch/revision的持久Start事实、有效policy/资格/期限及未停止门禁，不得自己隐式Start、补attempt或假造ScheduleState。它不能继续直接调用Claims.Complete+SaveProjection绕过Schedule。
4. 原 `NewWorker` 可以改成严格装配所需依赖的constructor，或从当前Host处理面移除。若短期保留旧签名只为编译兼容，其处理方法在缺少显式权限/策略能力时必须fail closed，不能默认放行。新入口不能基于配置开关回落旧无门禁实现。
5. 不另外复制一套“测试专用消费者”来保留旧投影成功规则。直接机制测试可以使用已批准runtime/adapter存储端口验证scope、锁、epoch等，但不能将其冒充Host业务处理成功验收。

实际05草稿中，Claim目前在Permissions!=nil时才检查allowlist；旧Complete仍直接写Projection；Finish检查StartEpoch但没有显式nil权限装配检查。应统一消除这些可被旧装配走通的消费者旁路。具体帮助函数/命名由05决定，不要求为每个结构体建立新接口。

## 检查的准确位置

- Claim：可信worker scope/资格、due和现有Claim条件；需要首次legacy绑定时按已定真实事务一次性完成。以后06在这里增加真实配额，05不伪造配额检查。
- Start：有限ctx、准确Work与Claim、当前epoch/lease、原revision policy、当前worker允许、停止/gate、原deadline及attempt预算。真正允许开始才持久增加启动次数，重开不能重置。
- Finish/受门禁Complete：准确原Claim仍有效，已有该epoch/revision的Start；consumer装配本身有显式权限和必要策略能力；停止和deadline仍有效；保存明确处理结果及Job关闭/重试同事务。当前受信资格变化不能通过旧Complete绕开新路径规定的处理结果。对调用方伪造或缺少Start不能为了兼容补登记。
- Renew：仍核验原Claim，不增加attempt、不延长原执行deadline；不能成为无policy旧消费者无限续跑通道。

runtime.ClaimStore.Complete可以保持“条件推进工作修订”的机制，不要求它重新执行全部demo策略。底层Tx/SQL端口由可信装配持有，本来能写存储；本决定不声称其为不受信调用方的安全隔离边界。

## Clock与默认值

采用已批准的纯project默认：**新record与legacy均5分钟**。当前05 DefaultPolicy实读已经是5分钟，保持这一值；不因曾编码1小时倒推改决定。legacy从首次可接管事务可信now一次性绑定，新record从接纳事务可信now绑定，其余3次启动、100ms基础退避/5s最大退避等沿legacy决定。

旧套件使用真实2026接纳、固定2100处理，先前只有lease所以偶然可运行；引入正确admission执行deadline后理应过期。修复是在系统Clock边界给同owner的Host.Record、worker、replacement注入同一个可信时钟，所有阶段统一推进。不要延长执行deadline数十年、绕过Start或跳过期限检查以保留旧fixture。

原命令accept_before也要与测试base配套：可用早于2099的固定base，或由固定base确定将来的准确UTC截止；不能把recordClock也改2100却继续用2099的原创建期限，再误以为产品接纳出错。真实历史v1 fixture的原命令不能重写，旧已接纳记录先查原键仍返回原决定，其执行期限走legacy首次绑定。

相同owner/同场景所有Clock共享同一权威，context/子进程清理仍使用有限真实运行时间，不能因测试时钟冻结导致永久等待。

## 04、07、08的测试迁移与并行关系

04共享套件中的**Host业务完成**路径必须在与05集成时迁移到真实有权限的Start/Finish或受门禁Complete。初次有效Claim先Start，再计算/提交；续租后准确更新用于完成的Claim值，不造新的业务身份。低层Tx scope/非法token测试可继续直接验证存储机制，但要明确它们证明机制，不证明完整调度。

伪造Claim、旧epoch、过期lease等负例必须由已经成功Start的正常原工作派生，并配实际能成功完成的对照。否则全部只因“未Start/无权限”而拒绝，原本epoch/lease绑定退化也可能绿灯；不接受这种假覆盖。该要求同样适用于08跨进程旧完成消息：消息来自真实Start之后的旧worker，接替后拒绝须确实保护原Claim。

07的清理成功前态也应通过真实受门禁project完成建立；不能用旧Complete直接写成功Projection避开限制。07/08现在从04基线开始不新增05依赖，可各自完成04范围实现；root集成05时统一调整共享fixture/Host装配及这些消费调用，最终集成代码不得留无门禁旁路。这是同一处理入口演进的整合责任，不改变票据业务依赖图。

最少增加或保留的消费者边界反例：不调用Start直接完成、缺权限装配、领取后撤权、领取后执行deadline过期、已Start后停止/过期、预算已耗尽以及正常完成；重开不补Start、不刷新deadline/attempt。两真实DB观察原固定record回执不变、没有非法Projection、Job责任按明确结果保存。不能仅用方法存在、类型名字或私有调用次数证明旁路已封。

此收敛与既定“新代码不允许未绑定有限policy就执行或提交绕过启动门禁”一致。它不要求授权纯数学函数，不把runtime改成业务策略层，不提前实现06配额，也不新增领域ADR。
