# Harness 技术实现方案决策地图

Labels: wayfinder:map
Status: resolved

## Destination

以 [项目目标基线](../../docs/harness-project-goals.md) 为约束，形成保存在 `docs/architecture/` 的多文件技术实现方案，覆盖内核、模块接口、协议、端云运行、数据模型、设备、授权、扩展、观测评测与自进化，并明确能力里程碑，达到可以据此拆分实现任务的程度。

## Notes

- 本地图采用本地 Markdown 票据，遵循 [Issue tracker](../../docs/agents/issue-tracker.md)。票据位于本目录 `issues/`；阻塞关系使用 `Blocked by:`；只领取未解决、未领取且所有依赖已解决的票据。
- 目的地来自用户明确要求：技术方案以多个 Markdown 文件保存到 docs。规划期间可维护方案草案，决策解决后同步规范；本次授权不包含业务代码实现或部署。
- 用户已确认的目标、默认权限、C1–C9、A1–A4 与 V1–V2 继续有效。使用 [领域词汇](../../CONTEXT.md)，不把九项能力机械映射为九个进程。
- 建图输入已由用户补齐：核心语言 Go；开发期单机，采用 SQLite 等不依赖独立外部服务的组件；模块按 Interface 依赖，可由业务或生产环境替换 Implementation；以多模拟设备验证能力，无真机测试要求。完整记录见 [设计输入与硬约束](../../docs/architecture/00-design-constraints.md)。
- 原“确定最小参考部署与技术硬约束”是建图输入收集，其问题已由用户直接回答；已移除这张临时票及其阻塞边，避免将已知输入重新当作待决选型。用户答复保存在设计输入文档中。
- 每次会话先读地图，再按需读取票据。适用技能：wayfinder、grilling、domain-modeling；模块接口设计使用 codebase-design；外部事实研究使用 research。
- 决策问题、依据与答复保留在对应票据；地图只记简述和链接。技术方案文档记录可实现的契约与约束，并链接决策依据。
- 建图会话不解决人工决策票。研究票可以并行处理；后续每次最多解决一个非研究票。需要用户答复的票不得由 Agent 自行代答。
- 研究在独立 worktree 的 `research/harness-*` 分支保存，报告整合到 `docs/research/`。外部规范与项目现状都须注明来源和核对时间。
- 方案入口：[技术实现方案](../../docs/architecture/README.md)。

## Decisions so far

<!-- 每个已解决票据一行，以标题链接包住身份；详细结论只保留在票据和关联研究资产中。 -->
- [核实现行协议与 Skill 规范的适配边界](issues/02-protocol-evidence.md)：已核实版本与适配缺口，内部任务、授权与恢复仍需独立契约。
- [核对持久运行机制与存储的部署约束](issues/03-runtime-evidence.md)：已区分检查点、Workflow 与数据库的保障，给出 Go 单机设计可参考的机制与验证题。

- [确定 Go 内核模块与本地持久化 Interface](issues/05-kernel-stack.md)：已确定内核状态权威、任务级原子提交、Go 库集成及按契约和耐久性分别验证替换。

- [定义内部契约与外部协议映射](issues/06-protocol-contracts.md)：已确定 gRPC 与 WebSocket 绑定、编码及交付兼容契约，已采用简化标识，并补入用户要求的可扩展 UI 交互契约。

- [确定端云任务所有权与恢复语义](issues/07-edge-cloud-recovery.md)：已确定任务拥有者与协作移交、离线控制、局部恢复及过期操作拒绝规则。

- [确定上下文、记忆与同步的数据模型](issues/08-memory-context.md)：已确定记忆生命周期、上下文失效、同步恢复，以及端云驻留、联合检索和端侧挖掘策略。

- [确定大脑循环与多 Agent 协作方式](issues/09-brain-agents.md)：已确定有限决策提案、模型替换、持续交互、任务委派及预算与收尾恢复。

- [确定多模拟设备与工具执行的验证方案](issues/10-execution-devices.md)：已确定 API 优先、千级准确调用目标、执行恢复、资源接管及独立模拟验证。

- [核实认证绑定、签名授权与离线时间的机制边界](issues/15-auth-mechanism-evidence.md)：已核实节点与浏览器认证、签名授权、离线时间及本地密钥保护的机制限制。

- [确定身份、授权与凭证的执行位置](issues/11-authorization.md)：已确定身份认证、委派与撤销、有限离线授权、凭证使用及受控管理操作。

- [核实 Go 扩展运行器与隔离能力](issues/16-extension-runtime-evidence.md)：已核实编译扩展、子进程与嵌入式 Wasm 的加载和隔离边界。

- [核实外部协议 Go SDK 与 Skill 格式锁定依据](issues/17-extension-sdk-evidence.md)：已固定规范及 SDK 候选版本，明确默认行为与互操作差异。

- [确定插件、Skill 与外部 Agent 的接入生命周期](issues/12-extension-lifecycle.md)：已确定清单与版本锁定、管理/运行接口、受限执行基线及逐节点激活恢复。

- [核实能力评测的统计与比较方法](issues/18-evaluation-statistics-evidence.md)：已区分固定目录得分、统计推断、配对比较及保留集复用的适用边界。

- [确定观测评测与自进化的发布闭环](issues/13-observation-evolution.md)：已确定记录/评分/发布职责、千级基准与预算、统计比较及分阶段启用和回滚条件。

- [核实第二持久化后端的事务与恢复边界](issues/19-persistent-backend-evidence.md)：已核实 bbolt 的本地事务、快照和并发限制，以及迁移与耐久性需由 Adapter 承担的职责。

- [确定能力里程碑与实施依赖](issues/14-implementation-milestones.md)：已确定四阶段、专项质量与预算、第二持久化验证及迁移、实施工作包和完整发布证据。

## Not yet specified

无剩余的本轮架构决策。跨模块收尾检查及实测未知项的验证/失败处置已归入 [确定能力里程碑与实施依赖](issues/14-implementation-milestones.md)，实施证据不作为本地图已取得的结果。

## Out of scope

- 实现业务代码、启动生产部署或采购服务：本次目的地是技术方案。
- 重新定义业务目标或默认自主发布权限：如需变更，先更新目标基线。
- 首版真实手机平台选择与真机验证：[核实手机与桌面执行的可实现范围](issues/04-device-evidence.md) 已按用户指定的模拟验证范围关闭，报告保留作未来真实 Adapter 的背景。
