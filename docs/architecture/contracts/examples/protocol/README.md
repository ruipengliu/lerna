# 核心调用与恢复夹具

[配置和方法登记](../../protocol.md) · [共同语义](../../README.md) · [独立完成判断投影](../README.md)

本目录的 18 个 JSON 文件是有界的跨调用记录序列，涵盖 40 个严格登记方法。`invalid-mutations.json` 保存 99 个反例、来源文件、变更路径和应命中的规则；校验器逐项应用到原例副本，不修改原例。方法登记还列出当前不能宣称支持的 53 个 reserved 方法。

夹具的 `auth` 是验收工具从认证通道取得的测试前提，不属于可由调用者选择的业务正文。`capabilities` 是预先装配的准确能力参数 Schema，`input_requests` 与 `approvals` 是受信负责端的有限前提快照。它们不是可由模型提交并自证权限的线接口。ID、摘要、正文长度、proof 和内容引用均为占位数据；格式合法不证明真实内容哈希、用户批准或签名已经核验。

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

在仓库根目录运行：

```sh
python3 -m pip install -r docs/architecture/validation/requirements.txt
python3 docs/architecture/validation/validate_protocol.py
python3 docs/architecture/validation/validate.py
```

第一项检查共同及方法 Schema、动态工具参数、请求／响应关联，以及序列中已给出的原命令、门禁、输入、用途和映射事实。第二项另查完成判断投影。当前用例不是服务器实现；认证、加密证明、策略子集、真实正文、调度、数据库原子提交、网络恢复、平台隔离和性能都没有运行验证。单个示例中的物理效果或 `ready` 只是待实现系统必须提供的观察，不能据静态通过声称效果已发生。
