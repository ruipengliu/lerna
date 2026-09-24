# MCP、A2A 与 Agent Skills 的适配边界

访问日期：2026-09-09。性质：技术决策的事实输入，尚未确定项目最终协议栈。目标依据：[Harness 项目目标](../harness-project-goals.md)；术语依据：[领域词汇](../../CONTEXT.md)。

## 1. 核实结论与版本

三种规范可以支持不同的扩展入口，但不能直接替代 Harness 内核的任务、授权与恢复契约。特别是 **MCP 当前版本已经改变协商和异步任务机制**，方案不能套用旧版初始化流程。

| 对象 | 本次核实版本与状态 | 对决策的影响 |
| --- | --- | --- |
| MCP | 官方 `latest` 重定向至 `2026-07-28`。 | 采用带日期的规范链接；核心和扩展分别选择兼容范围。[规范入口](https://modelcontextprotocol.io/specification/2026-07-28) |
| MCP Tasks | 有独立的 `2026-07-28` 版本页，同时另有 draft。Tasks 已移出核心，属于可选扩展。 | 不将旧版实验 Tasks、当前版本扩展和 draft 混用。[变更说明](https://modelcontextprotocol.io/specification/2026-07-28/changelog)、[版本化 Tasks](https://tasks.extensions.modelcontextprotocol.io/specification/2026-07-28/tasks) |
| A2A | 当前规范采用 `1.0` 的协议版本标识；以 Major.Minor 协商，补丁版本不参与兼容判定。 | 明确 Agent Card 中接口、协议版本、可选能力，不能仅标记“支持 A2A”。[规范 §3.6](https://a2a-protocol.org/latest/specification/#36-versioning) |
| Agent Skills | 官方格式页未标注统一的版本号；本报告按访问日快照理解。 | 内容格式兼容与宿主运行行为兼容分别验证。[格式规范](https://agentskills.io/specification) |

## 2. 已核实事实

### MCP：能力与上下文接入

MCP 用工具暴露可执行函数，用资源提供数据和上下文，也支持提示模板。其概览明确说明：协议自身不能强制落实实现方的全部安全原则。[MCP 概览](https://modelcontextprotocol.io/specification/2026-07-28)

`2026-07-28` 移除了 `initialize` 握手和协议级会话；请求在 `_meta` 携带版本和客户端能力，`server/discover` 提供服务端版本及能力发现。旧有 SSE 重放与恢复机制也被移除，断流后重发使用新的请求 ID。旧版 `tasks/result`、`tasks/list` 不属于新 Tasks 接口。[版本变更](https://modelcontextprotocol.io/specification/2026-07-28/changelog)

进展使用请求中的 `progressToken` 关联通知，服务端可以不发送进展，故不能用“没有进展”单独证明任务失效。[进展规范](https://modelcontextprotocol.io/specification/2026-07-28/basic/patterns/progress)

普通请求取消依传输而异：Streamable HTTP 关闭响应流构成取消信号；stdio 使用 `notifications/cancelled`。完成与取消存在竞态，无法取消的处理可能继续。[取消规范](https://modelcontextprotocol.io/specification/2026-07-28/basic/patterns/cancellation)

当前 Tasks 扩展通过能力声明启用，服务端决定是否为 `tools/call` 返回任务句柄。客户端用 `tasks/get` 查询，用 `tasks/update` 提供补充输入，用 `tasks/cancel` 请求取消。状态包含 `working`、`input_required`、`completed`、`failed`、`cancelled`；**`completed` 可以携带 `isError: true` 的工具结果**。任务有保留期限。取消确认仅表示接受请求，不保证工作已停止。[Tasks 规范](https://tasks.extensions.modelcontextprotocol.io/specification/2026-07-28/tasks)

MCP 授权规范面向 HTTP 传输，支持是可选的；stdio 另用环境凭据。HTTP 规范定义 OAuth 资源访问与令牌校验，要求令牌面向目标服务器，禁止任意令牌透传。它没有定义本项目的完整任务委派权限模型。[授权规范](https://modelcontextprotocol.io/specification/2026-07-28/basic/authorization)

### A2A：外部 Agent 的任务交互

A2A Agent 可以直接返回 Message，也可以创建 Task。Task 有工作、等待输入、等待授权及完成、失败、拒绝、取消等状态；终态后继续工作应创建新任务。`contextId` 关联交互，不等于跨 Agent 共享内部任务上下文。[任务生命周期](https://a2a-protocol.org/latest/topics/life-of-a-task/)

Agent Card 描述身份、技能、接口及认证要求；这里的 AgentSkill 是能力元数据。服务端执行自己的授权策略；`AUTH_REQUIRED` 可以请求客户端协助授权，凭据默认走带外渠道。取消不保证成功，Send Message 的幂等仅为可选行为。[规范 §3、§4.4、§7、§8](https://a2a-protocol.org/latest/specification/)

流式更新和推送需要相应能力支持。流中可以返回状态和产物，断开后可尝试订阅已有任务；推送需要客户端可达的 webhook。它们解决更新交付，不直接证明任务外部副作用的最终结果。[流与异步操作](https://a2a-protocol.org/latest/topics/streaming-and-async/)

### Agent Skills：知识包格式

Skill 最少包含带 YAML 元数据的 `SKILL.md`，可附带脚本、参考资料与资源。`name`、`description` 必填；`compatibility` 描述环境需要，`metadata` 可附加信息。`allowed-tools` 是实验字段，支持方式随实现变化。规范未提供强制的依赖解析、权限执行、沙箱、评测或回滚运行时；`metadata.version` 的示例也不是完整包管理协议。[格式规范](https://agentskills.io/specification)

## 3. 能力映射与适配缺口

下表是依据上述事实对项目目标的分析；“需要补齐”不等于已批准的实现选择。

| 项目边界 | 可复用的规范表面 | Harness 仍需补齐 |
| --- | --- | --- |
| C3 信息获取、C4 工具与设备操作 | MCP 工具、资源、结果与错误 | 来源时间和证据结构；设备观察、动作、结果验证；动作副作用与重试分类 |
| C5 任务运行、端云协同 | MCP Tasks 异步调用；A2A 外部任务 | 内部任务所有者、跨节点分派、断连后的状态核对、持久化和恢复；设备在线状态 |
| C1 决策、C2 上下文与记忆 | 外部消息、资源；Skill 操作知识 | 规划与上下文选择；长期记忆来源、用途、删除及同步契约 |
| C6 扩展与互操作 | MCP 能力发现、A2A Agent Card、Agent Skills 包 | 插件生命周期、版本锁定、依赖与能力协商、适配器一致性测试 |
| C7 授权与用户控制 | MCP HTTP 授权；A2A 认证与授权请求 | 统一主体/资源/动作/范围/期限，委派权限收缩，执行端校验，撤销与用户接管 |
| C8 观测与评测、C9 自进化 | 进展、状态、结果及外部产物 | 跨协议追踪、独立效果判定、评测基线、版本发布与回滚 |

## 4. 供后续决策使用的建议

1. **保留内部任务身份与所有权。** 内核任务关联外部服务器、协议版本、远端 Task ID 和调用尝试；外部协议状态作为输入，由适配器转换。不要把一次 MCP 调用或 A2A Task 直接当作用户完整任务。
2. **分开传输、执行和效果。** 请求已返回、远端已完成、业务结果已核实是不同事实。MCP Tasks 的 `completed + isError` 是必须覆盖的反例。取消也应保留“已请求”与“已确认停止”的区别。
3. **恢复先核对，再决定重试。** MCP 新请求 ID 和 A2A Message ID 都不能作为外部副作用只执行一次的充分依据。远端句柄过期或首次回包丢失时，按能力声明的核对机制处理；无法核实时保留不确定结果。
4. **把授权装入适配器边界。** 外部 OAuth scope 与内部授权不是自动等价的。适配器应声明转换关系及不能表达的限制；无法保持限制时拒绝执行或请求明确的补充授权，不能静默扩大权限。
5. **分开内容格式与运行保障。** 用户 Skill 可以采用开放格式；依赖解析、版本固定、权限检查、脚本隔离和自进化发布由 Harness 负责。A2A AgentSkill 与本项目 Skill 分别建模。

建议的一致性测试至少覆盖：版本不支持、扩展未声明、工具失败被包装为完成、取消竞态、断连重发、远端句柄过期、重复消息、权限范围不匹配，以及 Skill 元数据不能绕过执行端权限。测试应按版本和能力组合标记适用范围。

## 5. 局限与待决事项

- MCP 版本总览的扩展段仍出现“初始化时协商”的措辞，与同版本变更页及 Tasks 正文的按请求协商不一致。本报告以版本专页和具体扩展规则为依据，实施前需用相应 schema 与 SDK 验证，不能复制概览段生成接口。
- A2A 的 `latest`、Agent Skills 格式页是动态地址。本次确认协议语义，未锁定其仓库提交，也未验证所有语言 SDK 的实现覆盖率。正式支持矩阵还需固定规范/SDK 版本并跑互操作测试。
- 未核实远端实现实际任务保留期、幂等方式、取消效果、推送可靠性和权限粒度；这些必须来自各实现声明与测试。
- “Skills over MCP”是 MCP 概览列出的另一扩展，本次没有据此主张已能替代 Skill 包分发；是否纳入后续协议范围另行决定。
- 最终支持哪些 MCP 旧版、是否首期启用 Tasks、A2A 选择何种绑定，以及内部端云协议和 Skill 包管理方式，均留给技术方案决策。
