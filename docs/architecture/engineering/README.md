# 工程实现与交付顺序

实现从一条真实闭环开始，但生产分布式合同从第一天保留。先把“报告生成、保存、读回、核验”跑通，再增加端云和规模；每个阶段交付对应证据，不能用单机演示代替生产恢复。

首次接触本项目时，先读[Harness 架构讲解](../guide.html)，理解职责、数据交接和恢复边界，再按本文安排实现。

## 1 代码组织

采用单monorepo、初期一个Go module，遵循[ADR 0010](../../adr/0010-monorepo-shared-contract-release.md)。下列是拟实现目录，不代表仓库已有运行内核：

```text
cmd/                  gateway / application / worker / executor / cli
api/                  同版Schema、方法注册和gRPC外壳
runtime/              接纳模板、Tx、JobStore、Clock、身份端口
internal/task/        Task、条件、控制、预算与事实归并
internal/brain/       决策、模型请求与产出发布
internal/execution/   Operation、资源、驱动与环境
internal/memory/      Memory、Content、来源和投影
internal/interaction/ Session、Surface、输入与Schedule
internal/governance/  Grant、扩展批准与评测
adapters/             PostgreSQL、SQLite、对象存储、WSS、gRPC、供应商
sdk/go/ sdk/ts/       持久命令、恢复与类型安全客户端
apps/web/             受信Renderer与管理界面
conformance/          同版正反例、跨实现和故障验收
```

领域依赖小型port和显式repository，不依赖连接对象、SQL驱动或前端框架。Tx 仅在显式声明的同一数据库范围内共享。公开SDK与内部领域包分开，不把所有内部结构承诺为稳定API。

## 2 默认技术栈

核心/宿主/CLI使用Go受支持稳定版本；实际补丁、生成器及依赖摘要通过构建和合同测试后固定InstallLock，不把研究日期的最新版抄成当前保证。

默认云端 Orchestrator 的 Task 等权威记录使用 PG；端侧 SQLite 只实现本机执行、门禁、恢复和补传账本。端侧自有 Task 属于单独的可选 Orchestrator 部署，不能因适配器可用就默认启用。PG 采用 pgx/sqlc 显式 SQL；SQLite 用独立 database/sql 适配、WAL/FULL/foreign_keys 和单写队列，分别维护迁移。生产使用受限角色和事务池，本机PG集成先有界直连。迁移由独立管理命令执行，不让每个副本启动时竞争改表。

WSS用Go适配器，服务间grpc-go/Protobuf外壳携严格JSON，同进程直接接口。浏览器为React/TypeScript/Vite与pnpm workspace，SDK用原生WebSocket及IndexedDB持久原命令。Schema2020-12和规范化规则跨Go/TS同版；原始JSON重复键、Unicode和数值检查早于普通解码。

方舟OpenAI兼容模型是首个适配目标，固定endpoint/profile/凭据引用和可核验版本，关闭隐式重试。豆包搜索与正文获取分别作为Execution能力，搜索摘要不冒充已读正文；默认不启用模型供应商内置联网绕过逐行动准入。具体SDK和计费限制须查官方合同并在真实账户验证，不能由“兼容”推导完全相同语义。

平台适配包括配置、实例发现、身份/密钥、时钟、存储、健康、排空及观测；开发用配置文件、静态发现和进程脚本，可选Compose，不建设公司控制平面。slog/OpenTelemetry只输出获准遥测，不替代业务账本。

## 3 四条实施切片

以下切片表示工程依赖顺序，产品阶段的交付与验收范围以[项目目标](../../harness-project-goals.md#首阶段交付)为准。首阶段选取研究报告所需能力并完成真实应用接入；下表中的跨任务记忆、手机 GUI 等后续能力不因位于同一工程切片而成为首阶段前置条件。

| 切片 | 交付与退出证据 |
| --- | --- |
| 真实基本闭环 | Task/Content/Grant、规则Brain、受管文件写和独立读回、当前条件核验；云端 Task/原命令/Job 在 PG 恢复，设备执行/门禁/补传在 SQLite 恢复 |
| 智能与交互 | 真实模型输出发布/费用、联网实际来源、Web持久输入与预览、Memory提取纠正、至少三台独立有状态模拟手机 |
| 分布式与替换 | 本机独立网关/应用/两worker/执行宿主；真实WSS/gRPC丢答复、端云混合、三系统第二实现、外部委派与离线额度 |
| 受控改进与规模 | 冻结实验、正式保留集、发布/独立旧批准回退、生产三AZ恢复、逐级容量与最终API/质量目标 |

首条切片可用单进程调试，但第二/第三切片不得把分布式语义留待以后重写。首次上线独立冻结实际开放能力和负载，满足[生产门槛](../production/README.md)，尚未实现的扩展不声明支持。

Schedule、复用环境、child会话和跨域证据的首版业务选择、方法字段、前态及错误已在对应模块中固定。实现者必须按[开发范围与检查](implementation-readiness.md)选择开放范围，再补齐该范围的同版Schema、SDK和互操作测试。这里剩下的是代码与证据工作，不得借“生成接口”重新选择另一种业务语义。核心Schema仍不是全部API的完整代码生成源。

## 4 首先测四种请求

A直接回答：一次提交、固定Decision、准确产出、必要核验和Result。B读后回答：加入原读取Operation、真实覆盖与下一快照。C冷恢复：只恢复原责任先不自动新调用，再按当前资格续行。D保存并读回：写与独立读分别有Operation，写答复丢失沿原键核对。

每条分开记录领域事件E、外部请求R、实际读写W、数据库事务Tx、flush/sync、WAL字节、内容字节、等待及费用。表数量、接口次数或“六组对象”不能换算物理IOPS。比较优化前后时固定输入、模型回放、存储装配、流片段和故障点，暖路径和冷恢复分别报。

## 5 支持与发布声明

Linux是生产参考平台，Linux/macOS及amd64/arm64按实际构建和平台探针分别声明。SDK可编译、Schema向量通过、本机恢复、跨实现互操作、跨AZ容灾和任务质量是不同证据等级。

Go context取消仅发停止信号，不证明goroutine或目标动作结束；释放本地并发槽须观察实际退出。真实凭据/公司平台缺失记录待接入，继续无依赖切片，不使用空库、空目录或伪造成功输出通过验收。

代码提交与运行报告必须绑定commit、schema/method/profile摘要、配置、环境、数据集、故障注入和完整结果。只有达到相应验收门槛才扩大发布声明。静态文档与小模型检查的结果单独说明，不能作为工程实现完成率。
