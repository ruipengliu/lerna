# 外部协议 Go SDK 与 Skill 内容快照锁定依据

核验日期：2026-09-09。范围：[核实外部协议 Go SDK 与 Skill 格式锁定依据](../../.scratch/harness-technical-design/issues/17-extension-sdk-evidence.md)。既定目标保持 **MCP `2026-07-28` 的 stdio、Streamable HTTP 双角色，以及 A2A `1.0` 的 HTTP+JSON 双角色**；Agent Skills 仅做内容格式兼容。本次读取官方发布 API、固定标签源码和规范，计算源文件摘要；未安装依赖、运行 SDK 或互操作测试，不能据此声明 Harness 已支持这些目标。

## 结论与固定版本

两套官方 Go SDK 都已有目标协议和传输的实现证据，无需因 SDK 落后而降低目标。但默认协议选择与一处 A2A 媒体类型处理仍须由 Adapter 明确处理。下表是可复核的依赖候选，不是已合入的依赖清单。

| 对象 | 固定标签、完整提交 | 证据与边界 |
| --- | --- | --- |
| MCP Go SDK | `github.com/modelcontextprotocol/go-sdk v1.7.0`；`bc72835f62eb94d0fb484439f886b6885b075f36`；2026-07-28 发布 | 发布记录明确声明支持 `2026-07-28`；固定 README 版本表也确认。这是表中首个支持目标的稳定系列。[发布记录](https://github.com/modelcontextprotocol/go-sdk/releases/tag/v1.7.0)、[固定 README](https://github.com/modelcontextprotocol/go-sdk/blob/bc72835f62eb94d0fb484439f886b6885b075f36/README.md#version-compatibility) |
| A2A Go SDK | `github.com/a2aproject/a2a-go/v2 v2.5.0`；`9d95b95445f4208ba77f48a137a278067937adb7`；2026-08-18 发布 | README 声明 A2A v1.0 和 REST；源码 `a2a.Version = "1.0"`。SDK v2 是模块版本，不是线上协议 v2。[发布记录](https://github.com/a2aproject/a2a-go/releases/tag/v2.5.0)、[固定 README](https://github.com/a2aproject/a2a-go/blob/9d95b95445f4208ba77f48a137a278067937adb7/README.md)、[版本常量](https://github.com/a2aproject/a2a-go/blob/9d95b95445f4208ba77f48a137a278067937adb7/a2a/core.go#L33) |
| Agent Skills 格式 | `agentskills/agentskills` 提交 `69ef37e9424c0a7ea9dd2293b559e43ec8176379`；提交时间 2026-08-09 | 固定 `docs/specification.mdx`，不发明规范版本号；快照标识与具体 Skill 内容版本分开。[提交](https://github.com/agentskills/agentskills/commit/69ef37e9424c0a7ea9dd2293b559e43ec8176379)、[格式源文件](https://github.com/agentskills/agentskills/blob/69ef37e9424c0a7ea9dd2293b559e43ec8176379/docs/specification.mdx) |

MCP 在访问日另有 `v1.8.0-pre.2`（2026-09-04），说明仍支持同一目标并增加协议版本限制、输入上限及连接修复；它是预发布，不把“最新”替代为稳定基线，也不忽略这些后续修复。实际实现前需要在固定稳定版加定向修复与采用经验证的后续版本之间作选择。[预发布记录](https://github.com/modelcontextprotocol/go-sdk/releases/tag/v1.8.0-pre.2)

## MCP：传输已具备，必须约束默认行为

- **客户端和服务端的 stdio 均有入口。** 固定 README 展示 `CommandTransport` 启动外部服务及 `StdioTransport` 运行服务端；协议文档说明客户端、服务端使用相同传输抽象。HTTP 对应 `StreamableClientTransport`、`NewStreamableHTTPHandler`。[固定 README](https://github.com/modelcontextprotocol/go-sdk/blob/bc72835f62eb94d0fb484439f886b6885b075f36/README.md#getting-started)、[固定传输文档](https://github.com/modelcontextprotocol/go-sdk/blob/bc72835f62eb94d0fb484439f886b6885b075f36/docs/protocol.md#transports)
- **HTTP 服务端必须设 `StreamableHTTPOptions.Stateless = true`。** 否则 SDK 不接受目标版本。`PropagateRequestCancellation` 是另一个默认关闭的选项；需要让 POST 断开传递到处理函数时必须显式开启，不能从 Stateless 推导已经传播取消。[HTTP 源码](https://github.com/modelcontextprotocol/go-sdk/blob/bc72835f62eb94d0fb484439f886b6885b075f36/mcp/streamable.go)、[v1.7.0 说明](https://github.com/modelcontextprotocol/go-sdk/releases/tag/v1.7.0)
- **默认发现失败会回退旧初始化流程，且允许多个旧协议。** `ClientSessionOptions` 在此版本没有公开版本限制字段；`InitializeResult().ProtocolVersion` 能读取最终版本。建议出站连接后、业务调用前校验必须为 `2026-07-28`，失败即拒绝；入站还须在业务分发前限制请求版本与旧方法。若要求连旧版探测都不发送，需要拦截或定向修改发现逻辑。仅检查最终版本不足以禁止发现阶段旧握手；仅设置 Stateless 也不足以证明旧协议已被禁用。[客户端源码](https://github.com/modelcontextprotocol/go-sdk/blob/bc72835f62eb94d0fb484439f886b6885b075f36/mcp/client.go)、[支持版本集合](https://github.com/modelcontextprotocol/go-sdk/blob/bc72835f62eb94d0fb484439f886b6885b075f36/mcp/shared.go)

上述限制是按项目目标作出的 Adapter 建议。SDK 内的 Session 命名、旧 SSE/EventStore 和历史版本兼容代码不能变成 Harness 对外支持承诺。`v1.7.0` 的核心包和文档中未检索到当前独立 Tasks 扩展的 `tasks/get`、`tasks/update`、`tasks/cancel` 实现；这次没有全面审计扩展仓库，所以**不声称 Tasks 已由该 SDK 提供**。若首期启用 Tasks，须另锁扩展规范及实现，不能把核心协议版本支持声明外推为所有可选扩展。[固定源码目录](https://github.com/modelcontextprotocol/go-sdk/tree/bc72835f62eb94d0fb484439f886b6885b075f36/mcp)、[版本化 Tasks 规范](https://tasks.extensions.modelcontextprotocol.io/specification/2026-07-28/tasks)

## A2A：HTTP+JSON 双角色证据及媒体类型差异

固定版本的客户端 `NewRESTTransport` / `WithRESTTransport` 注册 `HTTP+JSON`，实现发送消息、读取/列举/取消 Task、订阅、流式发送、推送配置和扩展 Agent Card；服务端 `NewRESTHandler` 明确实现该绑定并注册对应 HTTP 路径。这是源码覆盖证据，不等于所有路径已经互操作通过。[客户端 REST](https://github.com/a2aproject/a2a-go/blob/9d95b95445f4208ba77f48a137a278067937adb7/a2aclient/rest.go)、[服务端 REST](https://github.com/a2aproject/a2a-go/blob/9d95b95445f4208ba77f48a137a278067937adb7/a2asrv/rest.go)

默认 Factory 同时启用 JSON-RPC 和 REST，前者优先。建议使用 `WithDefaultsDisabled()` 后仅添加 `WithRESTTransport(...)`，检查 AgentInterface 的 `protocolBinding = HTTP+JSON` 与 `protocolVersion = 1.0`，不注册兼容层。SDK 模块依赖里出现旧模块不等于 Harness 支持旧 A2A。[Factory 源码](https://github.com/a2aproject/a2a-go/blob/9d95b95445f4208ba77f48a137a278067937adb7/a2aclient/factory.go)、[模块依赖](https://github.com/a2aproject/a2a-go/blob/9d95b95445f4208ba77f48a137a278067937adb7/go.mod)

**已证实的差异：** A2A 规范补丁 `v1.0.1` 在 HTTP+JSON §11.1 建议（SHOULD）使用 `application/a2a+json`。SDK v2.5.0 普通请求与响应仍使用 `application/json`，且 `internal/rest.FromRESTError` 只接受此前缀，遇到规范推荐媒体类型的错误响应会直接返回通用 `ErrServerError`，丢失结构化错误。不能将 SHOULD 误写为 MUST；但错误解码分支确有互操作风险。[固定规范 §11](https://github.com/a2aproject/A2A/blob/3303592588e388e62e0f69f701af531d2f4e3991/docs/specification.md#11-httpjsonrest-protocol-binding)、[客户端请求头](https://github.com/a2aproject/a2a-go/blob/9d95b95445f4208ba77f48a137a278067937adb7/a2aclient/rest.go#L82)、[错误解码](https://github.com/a2aproject/a2a-go/blob/9d95b95445f4208ba77f48a137a278067937adb7/internal/rest/rest.go#L263)

可行的定向适配是：在专用 HTTP RoundTripper 中为普通 JSON 请求设置所需媒体类型和 Accept；把已确认的 A2A JSON 响应媒体类型在进入 SDK 错误解析前归一化，保留原始观测值；服务端包装响应类型或补丁修改 SDK。SSE 保留 `text/event-stream`，不能统一改写全部响应。这是依据现有 HTTP 注入入口与错误代码作出的工程方案，尚未验证；也不能据此断言只存在这一处差异。规范补丁 `1.0.1` 仍对应线上 `1.0`，没有改变既定目标。[HTTP 客户端注入入口](https://github.com/a2aproject/a2a-go/blob/9d95b95445f4208ba77f48a137a278067937adb7/a2aclient/rest.go#L41)、[协议版本规则](https://github.com/a2aproject/A2A/blob/3303592588e388e62e0f69f701af531d2f4e3991/docs/specification.md#36-versioning)

## 规范内容与摘要

以下均为实际读取固定提交后，对**原始文件字节**计算的 SHA-256，不是网页 HTML、压缩包或整个仓库摘要。标签以本次解析到的完整提交为准。

| 来源快照 | 文件 | SHA-256 |
| --- | --- | --- |
| MCP 标签 `2026-07-28` → `5f5440bb26a62e2cf3440b92da5a667efa03b267` | [schema/2026-07-28/schema.ts](https://github.com/modelcontextprotocol/modelcontextprotocol/blob/5f5440bb26a62e2cf3440b92da5a667efa03b267/schema/2026-07-28/schema.ts) | `742750af0bb8c716e7030c4977c992b55d1adc4407e9e66997db5846baedc2cd` |
| 同上 | [schema/2026-07-28/schema.json](https://github.com/modelcontextprotocol/modelcontextprotocol/blob/5f5440bb26a62e2cf3440b92da5a667efa03b267/schema/2026-07-28/schema.json) | `ef70b61f99b6d2e5e3b46863822eab08dff6a45bedc7a08914e0e5b133f40203` |
| A2A 标签 `v1.0.1` → `3303592588e388e62e0f69f701af531d2f4e3991` | [specification/a2a.proto](https://github.com/a2aproject/A2A/blob/3303592588e388e62e0f69f701af531d2f4e3991/specification/a2a.proto) | `e195bf96ab630c69797851970203e1b2b6b19528f2e9803b7d904b91a5104016` |
| 同上 | [docs/specification.md](https://github.com/a2aproject/A2A/blob/3303592588e388e62e0f69f701af531d2f4e3991/docs/specification.md) | `627ccfe6ffb1be2c56811c3dcc18780deb419f41cdc98248095c939b2d9dc9cb` |
| Agent Skills 提交 `69ef37e9424c0a7ea9dd2293b559e43ec8176379` | [docs/specification.mdx](https://github.com/agentskills/agentskills/blob/69ef37e9424c0a7ea9dd2293b559e43ec8176379/docs/specification.mdx)（7166 字节） | `b9079c0c10b7930e8c6a20ff2bc10cda2a3343c55185120e3f1116a1a529b220` |

A2A 仓库明确以 `a2a.proto` 为 canonical 源，生成的 `a2a.json` 是非规范性产物且不入库。因此 Adapter 的后备依据是固定 proto **加上**同提交的 HTTP 绑定文字，不能把在线动态 JSON Schema 当作完整规范；需要生成时另锁生成器、输入依赖和输出摘要。本次未生成 A2A JSON Schema。[官方生成说明](https://github.com/a2aproject/A2A/blob/3303592588e388e62e0f69f701af531d2f4e3991/specification/json/README.md)

Agent Skills 快照要求目录至少有 YAML frontmatter 加 Markdown 的 `SKILL.md`；必填 `name`（1–64 字符、目录同名、不能首尾连字符或连续连字符）与 `description`（1–1024 字符）；可选 `compatibility`（提供时 1–500 字符）、`license`、字符串到字符串的 `metadata`、实验性 `allowed-tools`。自定义信息应放在 `metadata`，不能声称任意顶层字段已获标准定义。源码对名称字符同时写了 Unicode 小写字母数字与括号中的 ASCII 范围，存在表述歧义；建议自身生成采用 ASCII 子集，非 ASCII 导入的兼容判定另行明确，不能声称规范明文只允许 ASCII。[固定格式正文](https://github.com/agentskills/agentskills/blob/69ef37e9424c0a7ea9dd2293b559e43ec8176379/docs/specification.mdx)

`scripts/`、`references/`、`assets/` 是可选组织约定，也允许其他文件；脚本语言依宿主实现。正文、脚本和 `metadata.version` 示例没有定义可执行授权、依赖求解、安装事务或包签名；`allowed-tools` 虽描述预批准工具，其实验性与宿主差异意味着不能自动兑换为 Harness 执行权限。这是对该快照定义范围的判断；格式兼容不含脚本运行时兼容。[格式正文](https://github.com/agentskills/agentskills/blob/69ef37e9424c0a7ea9dd2293b559e43ec8176379/docs/specification.mdx)、[官方对格式权威与参考实现边界的说明](https://github.com/agentskills/agentskills/blob/69ef37e9424c0a7ea9dd2293b559e43ec8176379/AGENTS.md)

## 锁定建议与最低引入范围

1. **SDK 锁定：** 两个固定模块及各自 tag、commit、Go module 校验值一起记录，实际引入时提交 `go.mod`、`go.sum`。两版 `go.mod` 均声明 Go `1.25.0`，这是读取到的工具链下限；没有验证最低平台兼容性。MCP 只从 `mcp` 及确有需要的 `auth` 等包进入；A2A 以 `a2a`、`a2aclient`、`a2asrv` 为适配入口，不因 SDK 提供 CLI、gRPC、JSON-RPC、compat 就启用这些对外入口。[MCP 模块](https://github.com/modelcontextprotocol/go-sdk/blob/bc72835f62eb94d0fb484439f886b6885b075f36/go.mod)、[A2A 模块](https://github.com/a2aproject/a2a-go/blob/9d95b95445f4208ba77f48a137a278067937adb7/go.mod)
2. **依赖范围：** 最低项目直接 SDK 依赖是上面两个模块；其完整模块依赖仍按 Go 模块规则锁定，不能把“仅 HTTP+JSON”误解为模块图不含 gRPC/protobuf。A2A 使用了这些模块，MCP 使用 JSON Schema、URI 模板、OAuth 等库；本次不另挑所有传递依赖的版本，也不把测试/工具依赖都说成运行时导入。使用既有 SDK 类型无须先安装协议代码生成工具。[两模块依赖清单](https://github.com/a2aproject/a2a-go/blob/9d95b95445f4208ba77f48a137a278067937adb7/go.mod)、[MCP 依赖清单](https://github.com/modelcontextprotocol/go-sdk/blob/bc72835f62eb94d0fb484439f886b6885b075f36/go.mod)
3. **Skill 双快照：** 格式兼容基线锁上表提交与摘要；每个 Skill 内容另锁来源仓库、不可变 revision、子目录与完整文件清单/摘要，包含脚本、资源、许可和文件模式。`metadata.version` 可保留为展示信息，不能代替内容摘要；清单算法、路径与符号链接规则由 Harness 定义。这是项目锁定设计建议，不是 Agent Skills 新增标准字段，也不要求引入 Python `skills-ref` 作为运行依赖。
4. **升级与缺口：** 支持矩阵独立记录协议、方向、绑定、可选能力、SDK 固定版本和局部补丁。SDK 将来滞后时，按上述固定 schema/proto 与传输正文补局部 codec/HTTP Adapter；类型生成不能补齐发现、取消、流、授权与恢复语义。当前首要待验证点是 MCP 严格版本限制与取消传播、A2A 媒体类型和结构化错误，及所选可选能力。这里仅给验收边界，不要求本研究生成接口或测试实现。
