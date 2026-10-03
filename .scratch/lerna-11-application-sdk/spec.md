# 11 · 应用 SDK、持久投递与受信交互

Status: ready-for-agent
Phase: C
Implementation: not-started
Depends on: [09](../lerna-09-task-control/spec.md)

## Problem Statement

应用在刷新、断线或用户确认期间容易丢失原请求身份，且界面往往把输入已保存、Task 已接纳和成果已完成混为一谈。

## Solution

提供 Go / TypeScript Application SDK、Session 投递与受信交互组件。开发者能持久提交目标、恢复观察、呈现准确确认正文，并保留业务状态差异。

## User Stories

1. As an application developer, I want a durable SDK outbox, so that retries reuse the original command after restart.
2. As a user, I want input saved before sending, so that refreshing the page does not lose my submission.
3. As a user, I want submission and task states distinguished, so that queued input is not shown as completed work.
4. As a user, I want exact confirmation previews, so that approval applies to the action I was shown.
5. As an application developer, I want resumable subscriptions, so that missed notifications can be recovered by queries.
6. As a user, I want session closure independent of tasks, so that closing a window does not cancel a goal.
7. As a user, I want stale answers rejected, so that a delayed interaction cannot apply to a changed request.
8. As a developer without sessions, I want direct task submission, so that a simple application can keep the same reliability guarantees.
9. As a tenant user, I want authorized pagination, so that task lists cannot reveal another user's data.

## Implementation Decisions

- 实现 session.create、submit_goal、steer、submission.get、interaction.answer 与 Task 查询订阅封装；Interaction 保存 Session、原 Message、Submission 及到固定 owner 的投递责任。
- 浏览器先把完整原命令、owner、期限和状态提交 IndexedDB 再发送，失败则不发送；Go SDK 提供耐久 outbox 适配合同与默认本地持久实现，服务端也可用所属数据库。
- session.submit_goal 与 task.submit 是可选入口，同一意图只走一个。输入已保存与目标已接纳分别展示，原身份丢失时从受信目录恢复，不按相似文本重建。
- SDK 隐藏编解码、重连与有界分页，但保留 commit_unknown、业务拒绝、未结效果；订阅只是变化提示，cursor_expired 时重建获准快照。
- 受信 Renderer 获取完整准确正文且成功呈现后才启用相关确认；确认绑定原请求、摘要、本人、修订和期限，普通聊天答案不当授权。
- TaskView 展示状态、等待、输入请求、结果、限制及三类收尾状态，模型流标记 provisional；归档 Session 不改 Task。
- 本切片可通过直接 Application 绑定验收，传输可替换；真实 WSS 故障套件在 13 复跑，不为 SDK 引入第二套业务语义。

## Testing Decisions

通过 SDK 暴露的 Application 方法与受信 UI 用户操作测试；浏览器验证 IndexedDB 刷新恢复和确认展示。复用 01 的 Go / TS 合同以及 09 的完整 Task，不读取 SDK 私有队列结构作为业务断言。

测试只断言公开行为、持久恢复结果和独立目标状态；不依赖私有表布局或内部调用次数。每个故障或拒绝案例必须配允许正常完成的对照。

验收条件：

1. 提交后刷新、断网和重启，SDK 重用原命令与固定 owner，只生成一个 Task；本地持久化失败时服务端未收到请求。
2. Session 已保存但 Orchestrator 暂不可用时显示待投递，恢复后为原目标；不同时调用两个创建入口。
3. 关闭或归档 Session 不取消 Task，重新连接仍可取得状态与正式 Result。
4. 预览未完成、正文加载失败或版本已过期时不能确认；正确呈现的有效请求可由本人一次确认。
5. 过期答案和错误请求修订被拒绝，合法澄清唤醒原任务；恶意正文不能伪造受信按钮动作。
6. 丢订阅通知或 cursor 失效后重建快照，TaskView 明示 partial / gaps，不从流事件猜测成功。
7. Go 与 TypeScript 在同一用例下得到同版回执、拒绝与结果；跨租户分页和查询不泄露数据。

## Out of Scope

- 通用 Surface profile、任意界面编排框架、移动原生 SDK。
- 用浏览器会话寿命托管 Task 或用普通文字“同意”替代绑定确认。

## Further Notes

本切片形成 SDK 与受信组件；12 才组装真实报告应用。SDK 对业务合同的测试和 13 的真实传输测试各有独立证据。

依赖项表示实现先决条件；`ready-for-agent` 表示规格已明确，不表示依赖已完成或能力已开放。全部验收通过并附准确版本、环境、命令、结果和限制后，才可将本切片记为完成。共同执行与证据规则见[切片索引](../lerna-implementation/README.md)。

依据：[contracts](../../docs/architecture/contracts.md#应用接入方法)、[contracts](../../docs/architecture/contracts.md#一次应用接入)、[governance](../../docs/architecture/governance.md#用户确认与界面责任)、[data-model](../../docs/architecture/data-model.md#六组核心对象)、[ADR-0001](../../docs/adr/0001-task-session-separation.md)、[ADR-0006](../../docs/adr/0006-authorization-budget-at-action-boundaries.md)、[ADR-0008](../../docs/adr/0008-internal-contracts-external-protocol-adapters.md)。
