# 协议官方来源复核

核验日期：2026-09-09。范围：复核[既有协议研究](harness-protocol-contracts.md)中的版本与关键生命周期事实，补充内部契约候选的规范依据。此次实际访问下列官方页面；未运行 SDK、互操作或故障实验，也未决定项目选型。

## MCP 与 A2A

| 核验项 | 官方页面支持的结论 |
| --- | --- |
| MCP 当前版本 | `latest` 本次确实重定向到 `2026-07-28`。[版本入口](https://modelcontextprotocol.io/specification/2026-07-28) |
| MCP 协商及恢复 | 此版移除初始化握手、协议会话和 SSE 重放；请求携带版本与能力元数据，服务端实现 `server/discover`。断开的响应流对应请求须用新请求 ID 重发；该传输规则不提供业务副作用幂等保证。[变更页](https://modelcontextprotocol.io/specification/2026-07-28/changelog) |
| MCP Tasks | 独立可选扩展；当前支持 `tools/call`，服务端依据请求声明决定返回任务。接口为 `tasks/get`、`tasks/update`、`tasks/cancel`。`completed` 可包含 `isError: true`；取消响应为空确认，工作可能继续，最终不保证进入 `cancelled`。[Tasks 正文](https://tasks.extensions.modelcontextprotocol.io/specification/2026-07-28/tasks) |
| A2A 版本 | `latest` 标示最新发布规范 `1.0.0`；线上协商采用 Major.Minor，即 `1.0`，补丁号不参与协议兼容判定。[版本规则](https://a2a-protocol.org/latest/specification/#36-versioning) |
| A2A 生命周期 | Agent 可以返回 Message 或 Task；Task 终态后不能重新启动，后续交互建立新任务，可关联同一 `contextId`。[任务生命周期](https://a2a-protocol.org/latest/topics/life-of-a-task/) |
| A2A 幂等与取消 | Send Message 幂等为可选行为，可使用 messageId 去重；Cancel Task 尝试取消，但不保证成功。取消操作幂等不等于取消一定生效。[操作语义](https://a2a-protocol.org/latest/specification/#33-operation-semantics)、[取消](https://a2a-protocol.org/latest/specification/#315-cancel-task) |

**仍存在官方文档内部冲突：** MCP 同版本总览的扩展段仍写初始化时协商，而其基本协议摘要、变更页和 Tasks 正文均采用按请求协商。既有研究已经指出此差异，本次再次确认。版本化具体规则足以支持讨论，但生成接口前还需比对固定版本 schema 与所用 SDK，不能复制总览段落。[总览](https://modelcontextprotocol.io/specification/2026-07-28)、[变更页](https://modelcontextprotocol.io/specification/2026-07-28/changelog)

## JSON Schema、HTTP 与 SSE

- JSON Schema 2020-12 官方提供 Core、Validation 和元 schema；`$schema` 标识方言，`$id` 和 `$ref` 支持资源标识及引用。这为跨语言共享消息结构提供规范依据，但规范本身没有要求项目采用 schema-first，也没有替项目定义任务状态转换。[2020-12 入口](https://json-schema.org/draft/2020-12)、[Core](https://json-schema.org/draft/2020-12/json-schema-core)
- `format` 的注解与断言行为有区别；默认不能假设所有验证器都会拒绝非法时间或 URI。使用该候选时，格式验证策略需要成为项目的明确约定并测试。[Validation §7](https://json-schema.org/draft/2020-12/json-schema-validation#section-7)
- WHATWG 定义 SSE 的 `text/event-stream`、UTF-8 编码、`data`/`event`/`id`/`retry` 解析及 EventSource 重连时的 `Last-Event-ID`。浏览器 EventSource 构造参数仅提供 URL 与凭据选项；不能直接假设其支持任意请求头、方法和请求体。[SSE 规范](https://html.spec.whatwg.org/multipage/server-sent-events.html)
- RFC 9110 区分 HTTP 方法幂等性：非幂等请求的自动重试需要已知幂等语义或确认原请求未应用等依据。HTTP 本身不能使任务提交自动获得业务幂等性。[HTTP 语义 §9.2.2](https://httpwg.org/specs/rfc9110.html#idempotent.methods)

由上述规范可推导：HTTP JSON 请求配合 SSE 通知是可讨论的传输候选；持久化事件保留、游标失效、断点补读、去重和任务取消仍需 Harness 明确定义。SSE 的重连标识不会自动提供这些服务端保障；内部 SSE 若采用重放，也不能将其解释为 MCP `2026-07-28` Streamable HTTP 的能力。

## 未决风险

本次关键生命周期结论均找到官方正文支持；尚未验证 Go SDK 的版本覆盖、JSON Schema 工具的一致性、代理对流的缓冲行为，以及具体远端的幂等、取消和保留期。A2A `latest` 与 WHATWG 为动态页面；发布支持声明前需固定可追溯版本或快照。RFC Editor 首次访问返回工具内部错误，随后通过 HTTP Working Group 官方镜像读取同一 RFC；此访问错误不构成规范缺失的证据。

## 追加核验：gRPC、Protobuf 与 WebSocket

用户后续指定进程间采用 gRPC、端云采用 WebSocket 全双工。本节提供相应事实；上文 HTTP JSON 与 SSE 保留为先前候选研究，不再表示当前内部传输选择。追加核验日期：2026-09-09，实际访问以下官方原文，未验证库实现。

| 核验项 | 官方依据及保障边界 |
| --- | --- |
| gRPC 双向流与顺序 | 支持 unary、服务端流、客户端流和双向流；双向流的两方向独立读写，各自保持消息顺序。顺序保障限单次 RPC，不能据此推导跨 RPC、跨重连或整个任务的全局顺序。默认使用 Protobuf 定义消息和服务，也允许其他编码选择。[核心概念](https://grpc.io/docs/what-is-grpc/core-concepts/) |
| gRPC 流控 | 官方流控指南面向 streaming RPC：框架可能阻塞写入以等待接收容量。写调用返回只表明值已交给框架，不意味着已发送到网络，更不是业务持久化确认；双方只写不读等使用方式可能死锁。[流控](https://grpc.io/docs/guides/flow-control/) |
| gRPC 取消 | RPC 取消不能回滚既有变更；服务端应用应监测取消并停止工作，不能认为任意后台业务已经自动停止。[核心概念](https://grpc.io/docs/what-is-grpc/core-concepts/)、[取消指南](https://grpc.io/docs/guides/cancellation/) |
| Protobuf 二进制演进 | 已使用字段编号不能随意更换或复用，删除编号应保留；二进制安全变更与条件兼容变更有不同约束，不能把所有字段修改视作兼容。[proto3 指南](https://protobuf.dev/programming-guides/proto3/#updating) |
| ProtoJSON 表达与兼容 | 官方明确其不能直接表达任意 JSON schema；未知字段、字段名和枚举名称带来不同于二进制的兼容约束。64 位整数默认输出十进制字符串，空值、缺失及默认值存在专门规则。因此 `.proto` 与 JSON Schema 不会因生成工具存在而自动语义等价。[ProtoJSON](https://protobuf.dev/programming-guides/json/) |
| WebSocket 双向与恢复范围 | RFC 6455 允许握手后两端独立发送；消息分片按发送顺序交付，未协商相关扩展时不同消息的分片不能交错。异常断开部分规定重连退避；协议本身未定义业务消息持久化确认、任务去重或恢复游标。由此推导：连接恢复不能直接证明之前的业务已执行或未执行。[RFC 6455 §1.2、§5.4、§7.2.3](https://www.rfc-editor.org/rfc/rfc6455.html) |

上述规范不能替项目完成应用级背压与恢复设计：gRPC 的传输流控、WebSocket 的连接与帧机制不包含任务并发额度、离线队列容量、持久化确认和跨重连去重语义。若同一契约有 Protobuf 与 JSON 两种表示，需明确字段映射和允许的值域，并分别验证缺失值、整数精度、未知字段和版本升级行为；这是根据两类表示差异得出的工程约束，尚未选择代码生成器或编码绑定。

## 追加核验：外部 Adapter 的标准传输

核验日期：2026-09-09。本次只确认规范是否提供对应绑定，不据此声明 SDK 已支持或互操作已通过。

- **MCP `2026-07-28`：stdio 与 Streamable HTTP 均是标准绑定。** stdio 使用客户端启动的子进程标准流及换行分隔消息；Streamable HTTP 使用单个 MCP 端点，响应为 JSON 或请求范围内的 SSE。两者都可以作为 Harness 调用外部服务及向外部客户端提供服务的参考传输。[传输总览](https://modelcontextprotocol.io/specification/2026-07-28/basic/transports)、[stdio](https://modelcontextprotocol.io/specification/2026-07-28/basic/transports/stdio)、[Streamable HTTP](https://modelcontextprotocol.io/specification/2026-07-28/basic/transports/streamable-http)
- **MCP 的“双向 Adapter”须区分客户端与服务端角色。** 当前规范只允许客户端发送请求/通知，服务端发送响应/通知；服务端不发起 JSON-RPC 请求。因此，入站与出站适配能力不意味着在同一绑定内任意双向发起 RPC。[传输与消息方向](https://modelcontextprotocol.io/specification/2026-07-28/basic/transports)
- **A2A `1.0`：HTTP+JSON/REST 是官方标准绑定之一。** AgentInterface 使用 `HTTP+JSON` 标识；该绑定提供标准 HTTP 方法、JSON 消息与资源路径，可分别实现调用外部 Agent 的客户端和对外提供 Agent 服务的服务端。[规范 §4.4.6、§11](https://a2a-protocol.org/latest/specification/)
- **A2A streaming 是可选能力。** `AgentCard.capabilities.streaming` 为 false 或缺省时，`SendStreamingMessage` 与 `SubscribeToTask` 必须返回 `UnsupportedOperationError`。HTTP+JSON 绑定在支持流时使用 SSE；省略内部 SSE 传输不妨碍外部 Adapter 依其标准处理 SSE。[规范 §3.3.4、§11.7](https://a2a-protocol.org/latest/specification/)

结论限于规范可行性：这些标准绑定具备实现首期双向 Adapter 的依据；具体操作覆盖、可选扩展、权限映射及互操作验收仍由项目支持矩阵约束。外部 Adapter 使用 HTTP、stdio 或 SSE 与内部进程间 gRPC、端云 WebSocket 的选择属于不同边界。
