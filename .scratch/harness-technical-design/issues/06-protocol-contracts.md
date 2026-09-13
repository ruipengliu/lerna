# 定义内部契约与外部协议映射

Type: grilling
Interaction: HITL
Labels: wayfinder:grilling
Status: resolved
Assignee: codex
Blocked by: 02, 05
Parent: [Harness 技术实现方案决策地图](../map.md)

## Question

任务、能力调用、记忆访问、端云连接采用哪些内部领域契约，如何映射到外部标准并协商版本、扩展、错误与取消？

## Scope

定义逻辑对象、消息关联、能力声明和适配职责；协议版本选择必须引用规范研究。

## Expected evidence

给出可实现的 Interface 及必要消息示例，列出每项标准覆盖范围和内部补充语义。

## Output

计划关联文档：`docs/architecture/03-contracts-and-protocols.md`。

## Comments

已由 Codex 领取，继续沿用已确认的四个接口领域和 Go 内核方案；本票形成协议设计，不交付业务实现。

前置决策已确认：内核持有任务状态权威，运行存储按任务级原子变更提交。需承接 Go 可导入契约的组织方式，细化任务/调用/提交变更标识、完整字段、错误原因及结果确定性、提交核对、回执保留与版本兼容；不得泄露 SQLite 或外部框架类型。参见 [Go 内核与本地持久化](../../../docs/architecture/02-runtime-and-state.md)。

第一轮讨论草案：[内部契约与外部协议映射](../../../docs/architecture/03-contracts-and-protocols.md)。提出独立 Schema 与行为规范、HTTP JSON 加 SSE 的默认绑定、MCP/A2A 双向参考适配及 Skill 内容兼容三个取舍；均等待用户答复，尚未锁定具体外部协议或 SDK 版本。

事实复核已完成：[协议官方来源复核](../../../docs/research/harness-protocol-source-check.md)。由研究子 Agent 实际访问官方页面，确认既有版本与生命周期结论，并补充 JSON Schema、HTTP 与 SSE 的适用限制；未测试 SDK，也未据此自动批准选型。

第一轮用户答复：指定进程间使用 gRPC，以支持双工需求；端侧与云侧使用 WebSocket 全双工通道，用于数据和交互事件推送；其余接受。已据此确认独立契约和 MCP/A2A 双向适配、Skill 内容兼容，撤下原生 HTTP JSON/SSE 默认方案。

第二轮待答复：Q4 `.proto` 定义固定协议、JSON Schema 定义动态能力参数；Q5 全双工通道按可靠控制消息、临时更新和大数据分类处理；Q6 主版本与能力集协商，外部版本固定且可选扩展分别验收。Q4 是对先前 JSON Schema 推荐的明确细化调整，尚未视为已接受。

第二轮用户答复：“接受建议”，确认 `.proto` 与动态 JSON Schema 的分工、可靠控制消息和临时更新的分级交付、主版本加能力协商，以及固定外部参考协议版本。

## Answer

已完成本票协议路线和接口设计：

1. 保持任务协作、能力调用、记忆访问及端云连接四个逻辑契约领域，按行为一致性约束实现。
2. 部署侧内部跨进程使用 gRPC，端云使用 WebSocket 全双工，进程内直接使用 Go Interface；共同语义与各绑定的故障测试分别提供。
3. `.proto` 定义固定消息、服务和信封；JSON Schema 定义动态参数和结果；不维护两份固定字段权威。
4. 控制指令、必要交互和最终结果持久化交付并去重；临时更新可按声明合并，大数据受限分块或引用；回执不等于执行或效果确认。
5. 内部使用主版本加能力集协商，不支持必需语义时拒绝；MCP 目标 2026-07-28、A2A 目标 1.0，外部入站/出站与可选扩展分别声明和测试。
6. 配套目录明确服务方法、标识作用域、WebSocket 信封、协商、错误、取消、恢复查询、消息示例及一致性矩阵。原子存储接口保持内核所有，不开放通用数据库写入口。

规范见 [内部契约与外部协议映射](../../../docs/architecture/03-contracts-and-protocols.md)及[原生接口与消息目录](../../../docs/architecture/03-contract-message-catalog.md)。具体重连/所有权算法、记忆数据模型和授权机制交由已存在的后续票据，未新增待用户决定的协议路线。

官方复核支持 MCP stdio/Streamable HTTP、A2A HTTP+JSON 作为参考标准绑定；双向 Adapter 表示两种接入角色，不改变外部协议本身的请求方向限制。事实及未验证 SDK 的边界见 [来源复核](../../../docs/research/harness-protocol-source-check.md)。

本票验证为文档一致性、链接、依赖与消息示例结构检查；尚未实现 `.proto`、SDK 或互操作实验。

## 标识简化评估补充

用户要求评估 ID 与数据对象是否可以简化。已在 [标识模型简化评估](../../../docs/architecture/03-identifier-model.md)逐项分析，并同步规范：执行类调用使用 operation_id，应用请求使用 message_id；取消操作仍独立引用目标操作，内部 change_id 不变。interaction_id 按需放入载荷，connection_id 留在连接上下文，因果信息改为引用已有身份。去重作用域细化为 namespace 内 operation_id 唯一，类型和发起主体不用于复用同名 ID；访问和重放仍单独鉴权。

本次是用户提出的协议模型简化，未改变 gRPC/WebSocket、编码、授权或持久化保障路线。评估基于生命周期及故障反例，不代表已运行实现测试。后续 Schema 以更新后的消息目录为准，历史答复中的独立 invocation/request 身份划分由此替代。

确认记录：用户回复“同意修改”，正式确认上述标识简化。更新后的标识模型作为协议实现及后续端云恢复设计的输入；本票保持 resolved。

## 端侧 UI 扩展补充

用户明确要求协议具备扩展性，除执行数据外还需支持端侧各种 UI 交互。已补充 [端侧 UI 交互与协议扩展](../../../docs/architecture/03-ui-interaction-contracts.md)：在四类基础契约之上增加用户交互与呈现契约组，固定消息包含具名扩展容器，按类型/版本/Schema 注册端侧组件和事件。交互可无 task_ref，正式动作复用 operation_id，消息复用 message_id，等待项复用 interaction_id；surface 引用仅用于有作用域的 UI 定位。

规范区分呈现、输入、业务执行和授权结果，定义快照/增量、重复提交、多端竞答、未知组件、流控及重连的验收语义。未选择具体 UI 框架，也未实现 Renderer；目标基线已同步用户新增要求。本票保持 resolved，相关实现与恢复细节由既有后续票据承接。
