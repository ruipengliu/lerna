# 领域调用与恢复夹具

[配置和方法登记](../../protocol.md) · [共同语义](../../README.md) · [独立完成判断投影](../README.md)

本目录的 52 个 JSON 文件是有界的跨调用记录序列，覆盖全部 104 个严格登记方法。`invalid-mutations.json` 保存 366 个定向反例、来源文件、变更路径和应命中的规则；校验器逐项应用到原例副本，不修改原例。每个方法的准确输入、输出、种类及错误恢复动作见[方法登记](../../schemas/methods.json)。

夹具的 `auth` 是验收工具从认证通道取得的测试前提，不属于可由调用者选择的业务正文。`capabilities` 是预先装配的准确能力参数 Schema，`input_requests`、`approvals` 与 `confirmations` 是受信负责端的有限前提快照。它们不是可由模型提交并自证权限的线接口。ID、摘要、正文长度、proof 和内容引用均为占位数据；格式合法不证明真实内容哈希、用户批准或签名已经核验。

`collection` 记录构造的集合成员、当前可披露范围与扫描位置，`activation_view` 记录客户端合并最高修订后的投影；两者都不是线字段。它们验证给定前提下的页归属、稳定遍历、缺口和迟到读合并，不证明真实权限变化或数据库快照成立。

`delegation_facts` 是计算委派只读 phase 所需的本地权威记录前提，包括原创建、输入与查询缺口、终结、费用封账和 Closure 保存。它不是领域线字段；公开 Delegation 对象不足以完整推导 phase。序列只检查给定记录的投影优先序、同修订一致性及关闭不重开，不证明这些记录已被真实事务提交。

`holder_gate` 是控制查询答复归并后的持久门禁观察，只存在于验收事件；它不随 WSS／gRPC 发送。序列可记录晚到的旧查询答复，并断言本地内容控制与 copy 两种最高修订及 closed 门禁保持。open 只说明没有已知关闭事实，不代表获得使用资格。该比较不执行真实事务竞争、当前身份认证或清理操作。

| 文件 | 观察链 |
| --- | --- |
| [01-task-loop](01-task-loop.json) | Task 接纳 → Brain 提案 → 授权使用 → 原操作与效果查询 → 准确版本 Result |
| [02-original-command](02-original-command.json) | 原答复丢失 → 原回执查询 → 同请求重投 → 同键异意图拒绝 → 当前权限裁剪 |
| [03-control-order](03-control-order.json) | resume 9 先到、pause 8 迟到、cancel 10 后旧命令到达 |
| [04-cancel-before-invoke](04-cancel-before-invoke.json) | 未知操作先存取消墓碑，再拒绝迟到 Invoke |
| [05-task-control](05-task-control.json) | 本地暂停、恢复、取消及逐端 pending |
| [06-goal-revision](06-goal-revision.json) | 原任务修订目标、控制传播、拒绝旧目标操作、原证据核验 |
| [07-input-consumption](07-input-consumption.json) | 排队、发送、撤回待核对、一次业务消费及另一回答拒绝 |
| [08-acceptance](08-acceptance.json) | 精确候选、目标修订及受信确认绑定 |
| [09-brain-recovery](09-brain-recovery.json) | accepted 到终态、原决策查询和取消另一条决策 |
| [10-use-recovery](10-use-recovery.json) | 可用性检查、一次占用及原期限查询 |
| [11-memory-management](11-memory-management.json) | 创建、准确读、替换、收紧、删除及最小管理信息 |
| [12-delegation-mapping](12-delegation-mapping.json) | 外部创建映射、原输入转交、控制和原映射核对 |
| [13-activation-online](13-activation-online.json) | 在线有限批准、激活接纳、实际 ready 及同 use 恢复 |
| [14-approval-offline](14-approval-offline.json) | 已活动实例的显式有限离线续用 |
| [15-rejected-branches](15-rejected-branches.json) | 已撤回批准、旧对象修订的明确拒绝 |
| [16-active-to-result](16-active-to-result.json) | 先前 active 快照后内部完成，随后查询原 Result |
| [17-optional-condition](17-optional-condition.json) | 可选条件失败不否决全部必需条件通过 |
| [18-unrelated-effect](18-unrelated-effect.json) | 其他任务的未知操作不混入本任务完成门槛 |
| [20-budget-allocation](20-budget-allocation.json) | 任务调额、父侧划拨及按原接收方封账结算 |
| [21-capability-catalog](21-capability-catalog.json) | 按候选查准确能力、参数及效果核对声明 |
| [22-resource-lifecycle](22-resource-lifecycle.json) | 设备占用、续期、观察、本人接管及交还 |
| [23-cross-orchestrator-budget](23-cross-orchestrator-budget.json) | 跨 Orchestrator 创建与 receiver 关闭竞争，原分配只结算一次 |
| [30-grant-lifecycle](30-grant-lifecycle.json) | 确认签发、当前读取及撤销 |
| [31-pairing-lifecycle](31-pairing-lifecycle.json) | 预认证请求、本人批准、领取及凭据撤销 |
| [32-lease-settlement](32-lease-settlement.json) | 固定实例的离线租约及最终结算 |
| [33-installation-lifecycle](33-installation-lifecycle.json) | 安装锁、停用及无引用清理 |
| [34-compatibility-release](34-compatibility-release.json) | 首装兼容报告、批准与逐目标发布 |
| [35-improvement-exposure](35-improvement-exposure.json) | 正式改善策略、保留占用及泄露后撤回 |
| [36-local-install-restart](36-local-install-restart.json) | 共库首装与重启，本次实例另取开放依据 |
| [37-remote-instance-reopen](37-remote-instance-reopen.json) | 远端新实例取得 reopen，历史激活依据保持不变 |
| [38-revoked-approval-startup](38-revoked-approval-startup.json) | 保留历史查询，撤回后拒绝新启动 |
| [39-evaluation-cancel](39-evaluation-cancel.json) | 取消实验及独立环境清理责任 |
| [40-content-lifecycle](40-content-lifecycle.json) | 上传发布、交付前登记副本、内容关闭及清理 |
| [41-memory-query-list](41-memory-query-list.json) | 准确修订读取与无正文管理 |
| [42-memory-extraction](42-memory-extraction.json) | 排队提取、有限 Orchestrator 任务与候选一次发布 |
| [43-memory-view-sync](43-memory-view-sync.json) | 快照与连续墓碑先提交再确认 |
| [44-surface-lifecycle](44-surface-lifecycle.json) | 完整快照更新与设备关闭意图 |
| [45-application-delivery](45-application-delivery.json) | 已注册处理端的原命令转交及业务消费 |
| [46-content-restriction](46-content-restriction.json) | 收紧用途，物理残留单独记录 |
| [47-declarative-form](47-declarative-form.json) | Surface 只引用请求；owner Schema 单一权威、新修订读取及旧修订拒绝 |
| [48-memory-changed-pages](48-memory-changed-pages.json) | 并发删除后的固定分页位置及披露复核 |
| [49-source-projection](49-source-projection.json) | 来源修订推进与预授权有限提取 |
| [50-online-use-settlement](50-online-use-settlement.json) | 在线使用累计费用、未知保留及最终差额释放 |
| [51-online-use-once-zero](51-online-use-once-zero.json) | 最终零费用不返还一次性使用身份 |
| [52-confirmation-grant](52-confirmation-grant.json) | Grant owner 登记挑战、本人决定及签发事务消费 |
| [53-confirmation-release](53-confirmation-release.json) | 评测 owner 确认精确发布命令并批准 |
| [54-confirmation-acceptance](54-confirmation-acceptance.json) | Orchestrator 确认精确验收命令并一次消费 |
| [55-content-holder-control](55-content-holder-control.json) | 默认／显式 bytes、关闭后自身 control 查询、其他持有者拒绝、旧控制晚到与门禁保持、清理后核对 |
| [56-owner-collection-pages](56-owner-collection-pages.json) | Operation／Activation／Grant 固定成员分页、当前记录、范围变化、截断／过期、查询槽回收与旧游标拒绝、Activation 高修订保持 |
| [57-collection-authorization-reset](57-collection-authorization-reset.json) | 调用参数错误不破坏原集合；权限 A→B→A 不能复活旧游标或旧首部，新 query_id 恢复 |
| [58-lease-open-close-ledger](58-lease-open-close-ledger.json) | 首次使用保持 open；closed 保留未知费用，全部原使用封闭后最终结算 |
| [59-delegation-phase-projection](59-delegation-phase-projection.json) | 创建未知、固定映射、普通未final用量、取消后未知费用、Closure 优先及原固定回执恢复 |

在仓库根目录运行：

```sh
python3 -m pip install -r docs/architecture/validation/requirements.txt
python3 docs/architecture/validation/validate_protocol.py
python3 docs/architecture/validation/validate.py
```

第一项检查共同及方法 Schema、动态工具参数、请求／响应关联，以及序列中已给出的原命令、门禁、输入、用途和映射事实。第二项另查完成判断投影。当前用例不是服务器实现；认证、加密证明、策略子集、真实正文、调度、数据库原子提交、网络恢复、平台隔离和性能都没有运行验证。单个示例中的物理效果或 `ready` 只是待实现系统必须提供的观察，不能据静态通过声称效果已发生。
