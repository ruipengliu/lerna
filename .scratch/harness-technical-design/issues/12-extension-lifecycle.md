# 确定插件、Skill 与外部 Agent 的接入生命周期

Type: grilling
Interaction: HITL
Labels: wayfinder:grilling
Status: resolved
Assignee: codex
Blocked by: 02, 06, 09, 10, 11, 16, 17
Parent: [Harness 技术实现方案决策地图](../map.md)

## Question

插件、Skill 与外部 Agent 如何声明、加载、适配、协商版本和依赖，并通过一致性测试进入可调度能力集合？

## Scope

区分代码实现、操作知识与独立运行时；明确扩展接入后的权限及生命周期责任。

## Expected evidence

给出扩展清单、接入流程、兼容性和依赖失败行为，以及 SDK 的最小契约。

## Output

计划关联文档：`docs/architecture/09-extension-ecosystem.md`。

## Comments

本票只解决技术决策或事实调查，不交付业务实现。

协议参考范围已确认：MCP 2026-07-28 stdio/Streamable HTTP、A2A 1.0 HTTP+JSON 双角色适配及 Agent Skills 内容兼容。可选 Tasks/流式/推送分别声明验收；本票固定格式快照、Schema 注册和 SDK/包版本，不把参考目标当作已实测支持。详见 [原生接口与消息目录](../../../docs/architecture/03-contract-message-catalog.md)。

扩展范围包括端侧 Renderer、具名 UI 类型及事件注册。固定 Protobuf 容器承载动态 Schema 数据；未支持的必需组件不得静默忽略，服务端类型声明不自动触发代码安装。类型版本和语义等价降级需纳入兼容性验证。详见 [端侧 UI 交互与协议扩展](../../../docs/architecture/03-ui-interaction-contracts.md)。

所有权管理 Adapter 可以扩展生产自动接管，但必须证明排他性和旧执行者隔离。默认实现只保证普通重启及协作移交；备份回滚或身份并行克隆不得自动当作普通重启。能力声明需分别表达取消/核对/离线及接管保障。详见 [恢复状态与故障检查表](../../../docs/architecture/04-recovery-state-and-failure-matrix.md)。

记忆扩展须声明处理位置、索引/提取能力、输出范围及保存行为；替换存储、检索或模型不得隐式外发私有数据。默认不提供集合权威自动接管或多主合并，提供此类可选 Adapter 时须明确单独契约和验证范围。 参见 [记忆设计](../../../docs/architecture/05-memory-and-context.md)。

大脑与协作方案已确认：Brain 与 Model 独立替换，声明模态、处理位置、结构化输出、用量及恢复信息兼容范围；每轮固定大脑、模型配置、Skill 和上下文组装版本。升级不清除已准入工作，不兼容检查点须迁移或等待明确处置；版本激活与回滚在本票细化。参见 [决策准入、交互与协作恢复](../../../docs/architecture/06-decision-admission-and-collaboration.md)。

用户追加 API 为主、GUI 兜底及超过 1000 个 API 准确调用要求。本票需确定能力声明的语义质量、版本与别名去重、接入验证和目录更新生命周期；千级目录不能只靠名称堆积。能力检索接口已由执行票确定。 参见 [工具执行与多模拟设备](../../../docs/architecture/07-tools-and-simulated-devices.md)。

执行接口已确认 CapabilityCatalog、ExecutionDriver 与 ExecutionStore 分工。声明须解释语义、不适用条件、资源冲突范围、异步/幂等/核对/停止能力及处理位置；能力版本和驱动版本分别固定。独立 Adapter 通过共同代表性契约，索引可重建且不作为版本权威。升级保留在途操作及其版本，不能因驱动替换清除未知结果或资源接管状态。 参见 [执行接口、资源控制与故障验证](../../../docs/architecture/07-execution-contracts-and-validation.md)。

身份授权方案已确认：IdentityVerifier、Authorizer、GrantAuthority、AuthorizationStore 与 CredentialBroker 分工；原生节点 mTLS、浏览器独立同源会话、ES256 受限授权及持有节点绑定。进程内扩展仅受信代码，不受信扩展需明确实际隔离与受控入口；本票确定可安装隔离实现及兼容生命周期，不把独立进程直接算强隔离。能力声明不能新增策略签发权，插件/Skill/模型不能读取通用凭证库。密钥及节点轮换需更新绑定材料，旧操作和撤销记录保留。 参见 [授权生命周期、凭证与控制接口](../../../docs/architecture/08-authorization-lifecycle-and-interfaces.md)。

### 本轮领取与首轮建议

已领取，读取决策地图、领域词汇、既有协议研究及执行/授权输入，沿用 wayfinder、grilling、domain-modeling 与模块接口设计原则。当前先对齐扩展生命周期方向，不将未核实 SDK 或运行器能力当作事实。

待用户答复的首轮建议：

1. 统一扩展清单和版本登记，插件安装实现、Skill 导入内容、外部 Agent 注册端点分别处理；支持本地/离线导入，不要求在线市场；固定来源与内容摘要。
2. Skill 按元信息发现、按需读取正文和资料；脚本显式登记并走受控执行，新内容形成候选版本，权限不会因内容修改扩大。
3. 受信 Go 扩展编译组装，独立进程/跨端依既有协议接入；受限脚本及插件使用声明能力的可替换运行器，无法落实限制时拒绝，不回退宿主裸执行。具体隔离实现后续核实选择。
4. 导入、验证、激活和停用分开；新版本面向新工作，旧版本保留在途恢复；升级及回滚不清除状态或重复副作用，端云分别记录实际激活进展。

资产：[插件、Skill 与 Agent 扩展生态草案](../../../docs/architecture/09-extension-ecosystem.md)。首轮建议尚无用户答复，本票保持 claimed。

### 用户接受：扩展生态基础

用户答复“接受建议”，确认首轮四项：统一登记但分别接入插件/Skill/外部 Agent；Skill 按需加载与脚本受控执行；按实际信任和能力选择运行方式；导入/验证/激活/停用分开并保留在途恢复。详细清单、运行器和版本锁定继续细化。

已委派 [核实 Go 扩展运行器与隔离能力](16-extension-runtime-evidence.md) 和 [核实外部协议 Go SDK 与 Skill 格式锁定依据](17-extension-sdk-evidence.md)，分别在 `/tmp/lerna-wayfinder-extension-runtime` 与 `/tmp/lerna-wayfinder-extension-sdk` 的 research 分支保存报告。根会话继续处理不依赖具体运行器/SDK 的清单和激活算法。

### 第二轮建议：清单、运行器与激活恢复

两项研究已完成并整合；其结论是事实输入，尚不等于用户已采纳具体实现。完整建议见 [扩展清单、运行器与激活恢复](../../../docs/architecture/09-extension-contracts-and-activation.md)。

1. 使用版本化 harness-extension.json 与不可变锁定记录，普通 Skill 由导入器生成伴随记录；依赖、权限请求、配置、Schema 与内容分别锁定。准备、验证、激活分离，静态导入不执行安装代码。
2. ExtensionManager / ExtensionRuntime / ExtensionStore 分离；本节点激活绑定原子提交，目录与 UI 等投影按修订可见，端云分别报告应用进度。旧工作固定旧版本，停用排空、隔离保留核对，回滚不覆盖业务事实。
3. 受限模块参考采用 wazero v1.12.0，显式 Interpreter、Linux amd64 先验、Core Wasm 加版本化 ABI，按需 WASI Preview 1；默认无宿主目录/网络/环境继承。限制实例化、执行与回调，任意本机脚本仅在显式受信或另验隔离后端运行。更强进程资源保证由独立实现提供。
4. MCP Go SDK v1.7.0、A2A Go SDK v2.5.0 为候选，最低 Go 1.25.0；固定规范/Skill 格式快照，补严格协议选择、取消边界和 A2A 媒体类型适配。MCP 独立 Tasks 首期不启用。原生 SDK 提供 Go 接口、生成协议绑定、校验器和共同一致性夹具；实际支持按实现与测试报告判定。

运行器与依赖选择、具体接口及上述细化尚待用户答复，本票保持 claimed。没有修改 go.mod、安装 SDK、实现运行器或执行互操作测试。

### 用户接受：具体选型与接口边界

用户再次答复“接受建议”，确认第二轮具体方案：版本化清单与不可变锁定；ExtensionManager / ExtensionRuntime / ExtensionStore 分工；节点内激活一致性、端云逐节点应用与旧工作恢复；wazero v1.12.0 的受限 Wasm 范围；MCP Go SDK v1.7.0、A2A Go SDK v2.5.0、最低 Go 1.25.0、固定 Skill 格式及明确的兼容适配。前一轮基础确认继续有效。

## Answer

扩展生命周期技术决策已完成，规范保存在 [插件、Skill 与 Agent 扩展生态](../../../docs/architecture/09-extension-ecosystem.md) 和 [扩展清单、运行器与激活恢复](../../../docs/architecture/09-extension-contracts-and-activation.md)。

- 插件、Skill 与外部 Agent 统一登记，但实现安装、内容导入和远端注册分别处理。版本化清单固定来源、贡献、依赖、运行条件与权限请求；精确锁定结果固定资产、接口、Schema 及配置，内容修改形成候选版本。
- Skill 按元信息发现、正文和资料按需加载，脚本显式受控执行；格式快照与每个包的内容摘要分别保存，内容字段不能扩大权限。端侧私有 Skill 的检索、加载、评测继续受处理和披露限制。
- 管理、运行和存储分别提供 ExtensionManager、ExtensionRuntime、ExtensionStore；ExtensionService 归入任务协作领域。准备与验证不自动激活，变更受当前授权、稳定操作身份及版本前提约束。
- 节点内原子提交激活绑定及修订，目录、Skill 与 UI 等投影不得暴露半套新声明；端云逐节点报告应用进展。已固定工作继续沿原版本恢复，正常停用排空，风险隔离保留核对，回滚不覆盖业务事实；清理遵循持久引用和各类数据生命周期。
- 受信 Go 实现编译组装，独立进程和跨端依已有协议接入；参考受限模块采用 wazero v1.12.0、Interpreter、Linux amd64 首验、版本化 ABI 与按需 WASI Preview 1。默认无宿主目录/网络/环境继承，限制实例化、执行和宿主回调；原生脚本须显式受信或使用已验证隔离后端，不能回退裸执行。
- 外部 SDK 基线为 MCP v1.7.0 和 A2A Go v2.5.0，最低 Go 1.25.0。MCP 严格版本选择、HTTP 取消边界与 A2A 媒体类型/结构化错误属于必须实现的适配；MCP 独立 Tasks 首期不启用。原生 SDK 交付 Go 接口、生成协议绑定、校验器及语言无关共同夹具。

总览、设计约束、协议目录、领域词汇与后续票据已同步。运行器有限预算、评测记录及发布监测交由 [确定观测评测与自进化的发布闭环](13-observation-evolution.md)；不同后端、运行方式和协议组合的验证进度交由 [确定能力里程碑与实施依赖](14-implementation-milestones.md)。两项研究报告保留事实与建议的历史性质，选型采纳以本答复及架构分册为准。

本次仅完成设计及文档一致性检查，没有引入依赖、实现业务代码、运行 SDK/隔离实验或宣称兼容验收通过。未发现需要另建独立决策票的阻塞问题。
