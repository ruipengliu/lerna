# 05: 多端输入一次消费与界面恢复

**What to build:** 设计用户在两个设备提交、确认、取消或断线重连时的准确行为，确保界面能恢复原输入和任务事实。

**Blocked by:** None (can start immediately).

**Status:** ready-for-agent

**Progress:** completed

- [x] 输入接纳、转交、业务消费及可信确认分别定义持久成功点，准确绑定原请求、版本及必要正文预览。
- [x] 明确 accepted 尚未领取时的取消、新输入与旧控制的关系，以及旧运行退出不得误伤新运行的条件。
- [x] 两设备竞争只允许业务事务中的胜方生效，败方不写自动许可；超时或断线不能推断已消费。
- [x] 界面先取得可读的持久快照，再依赖可丢提示加速；只对呈现增量做合并，旧世代数据不能覆盖新 Surface。
- [x] 覆盖重复 deny/confirm、旧预览、撤权、终结提示丢失、队列切换及客户端存储失败，并给出 SDK 查询原决定的路径。

## Comments

- 2026-10-01：开始补齐交互业务事务与宿主恢复合同；仅编辑 interaction/README.md、interaction/implementation.md、contracts/transport.md，沿用既有 Command、输入身份和传输世代，不扩充机器 Schema。
- 2026-10-01：设计完成。完整规则见[持久成功点](../../../docs/architecture/.draft/interaction/implementation.md#input-durable-boundaries)、[预览与撤权](../../../docs/architecture/.draft/interaction/implementation.md#preview-consumption-boundary)、[取消与队列竞争](../../../docs/architecture/.draft/interaction/implementation.md#input-control-races)、[两端确认](../../../docs/architecture/.draft/interaction/implementation.md#confirmation-races)、[快照发布](../../../docs/architecture/.draft/interaction/implementation.md#surface-publication)、[旧流与呈现](../../../docs/architecture/.draft/interaction/implementation.md#surface-generation)及[SDK 原决定查询](../../../docs/architecture/.draft/interaction/implementation.md#sdk-original-decision)。README 加入恢复导读与 Session／Task 简化入口，transport 只链接业务合同。
- 2026-10-01：新增[II-20～31 验收向量](../../../docs/architecture/.draft/interaction/implementation.md#input-recovery-validation)，覆盖 Claim 后撤回、accepted 未推进取消、T1／T2 隔离、旧退出、双端 confirm／deny、确认消费前撤权、预览边界、旧世代、终结提示丢失、SDK 两处存储失败及仅呈现 debounce。局部检查：三份架构文档 85 个本地引用有效、显式锚点无重复、代码围栏配对、git diff --check 通过；未运行全套验证或实际数据库／客户端故障实验。provisional live/token 可先展示／合并／丢弃，无逐片持久要求；ADR-0008 的取阅保证边界及跨 owner 非原子边界保留。
