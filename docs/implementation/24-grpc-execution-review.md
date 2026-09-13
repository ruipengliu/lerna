# 24：实现审查

范围：当前分支上 24 号票的未提交变更，基线 `c3f00a4fa60a47052e73c002425aaed442cc1c5d`。两名独立代理分别审查 Standards 与 Spec；固定输入包含所有在范围内的 diff、新文件、生成绑定和实施说明，后续修复通过第二份固定快照复核。最终报告与最后追加的配置借用测试由主代理检查。

## Standards

首次及增量复核均无可操作发现。依据为 AGENTS.md 所引用的本地 tracker、领域约定、已确认架构及 code-review 的 smell baseline。接口复用、SQLite 持久交付、资源上限和部署边界未发现规范违反；工具检查不作为重复人工发现。

## Spec

首次提出 3 项，其中 2 项成立并修复：

- P1：4 个 Exchange 占满 HTTP/2 并发槽后，同连接 RequestCancel 被排队。每连接上限改为 MaxStreams+1，全局 Exchange 上限保持 MaxStreams；真实链路测试验证满订阅时取消仍能受理。
- P2：畸形变更回包的响应分支或操作身份错误可能落到 SDK 的 INVALID_ARGUMENT。传输 Adapter 在交给 SDK 前验证响应载荷、操作与修订，保持 OUTCOME_UNKNOWN；真实 TLS 测试覆盖错分支、错操作、错修订和未知失败码。

临时移除两项修复时，对应回归分别失败；恢复后 race 检查通过。原 Spec 审查代理复核并关闭两项，未发现新增可操作问题。

第三项“Reconcile 期间撤销后仍披露”经审查代理继续追踪后撤回：executionlocal 在 Reconcile 后经动态分派调用 remoteService.GetInvocation，再次进入 ReadRemoteInvocation 验证原授权。无需重复增加该校验。流快照则已在持久化准备后、实际发送前重新验证节点和披露资格。

补充证据包括客户端真实进程重启去重、服务重启后的旧配置失效、其他已登记 TLS 主体借用配置被拒绝，以及未完成握手的服务端时限。

覆盖限制：发送及待确认窗口有界，实际慢消费者通过不发送持久化回执验证；未单独模拟无限期底层 socket Write 阻塞。参考实现不传输大产物流，完整事件历史、其他原生服务和端云绑定不在本票验收中。所有已声明限制均保留在实施说明和报告。

Standards：0 项；Spec：2 项有效发现已关闭、1 项撤回；无开放可操作发现。
