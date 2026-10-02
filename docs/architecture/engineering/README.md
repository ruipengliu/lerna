# 工程实现与交付顺序

实现从一条真实闭环开始，但生产分布式合同从第一天保留。先把“报告生成、保存、读回、核验”跑通，再增加端云和规模；每个阶段交付对应证据，不能用单机演示代替生产恢复。

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

领域依赖小型port和显式repository，不依赖连接对象、SQL驱动或前端框架。Tx仅在声明的本地范围共享。公开SDK与内部领域包分开，不把所有内部结构承诺为稳定API。

## 2 默认技术栈

核心/宿主/CLI使用Go受支持稳定版本；实际补丁、生成器及依赖摘要通过构建和合同测试后固定InstallLock，不把研究日期的最新版抄成当前保证。

PostgreSQL采用pgx/sqlc显式SQL；SQLite用独立database/sql适配、WAL/FULL/foreign_keys和单写队列，分别维护迁移。生产使用受限角色和事务池，本机PG集成先有界直连。迁移由独立管理命令执行，不让每个副本启动时竞争改表。

WSS用Go适配器，服务间grpc-go/Protobuf外壳携严格JSON，同进程直接接口。浏览器为React/TypeScript/Vite与pnpm workspace，SDK用原生WebSocket及IndexedDB持久原命令。Schema2020-12和规范化规则跨Go/TS同版；原始JSON重复键、Unicode和数值检查早于普通解码。

方舟OpenAI兼容模型是首个适配目标，固定endpoint/profile/凭据引用和可核验版本，关闭隐式重试。豆包搜索与正文获取分别作为Execution能力，搜索摘要不冒充已读正文；默认不启用模型供应商内置联网绕过逐行动准入。具体SDK和计费限制须查官方合同并在真实账户验证，不能由“兼容”推导完全相同语义。

平台适配包括配置、实例发现、身份/密钥、时钟、存储、健康、排空及观测；开发用配置文件、静态发现和进程脚本，可选Compose，不建设公司控制平面。slog/OpenTelemetry只输出获准遥测，不替代业务账本。

## 3 四条实施切片

| 切片 | 交付与退出证据 |
| --- | --- |
| 真实基本闭环 | Task/Content/Grant、规则Brain、受管文件写和独立读回、当前条件核验；原命令/Job在PG与SQLite适用故障下恢复 |
| 智能与交互 | 真实模型输出发布/费用、联网实际来源、Web持久输入与预览、Memory提取纠正、至少三台独立有状态模拟手机 |
| 分布式与替换 | 本机独立网关/应用/两worker/执行宿主；真实WSS/gRPC丢答复、端云混合、三系统第二实现、外部委派与离线额度 |
| 受控改进与规模 | 冻结实验、正式保留集、发布/独立旧批准回退、生产三AZ恢复、逐级容量与最终API/质量目标 |

首条切片可用单进程调试，但第二/第三切片不能把分布式语义留待以后重写。首次上线独立冻结实际开放能力和负载，满足[生产门槛](../production/README.md)，尚未实现的扩展不声明支持。

实现Schedule、复用环境、child会话或跨域证据等新增profile时，必须先冻结其字段与方法/错误映射，补SDK和互操作套件，再开放发现。它们的设计语义已在对应章节给出；本系列的核心Schema不是全部新增API的完整代码生成源。

## 4 首先测四种请求

A直接回答：一次提交、固定Decision、准确产出、必要核验和Result。B读后回答：加入原读取Operation、真实覆盖与下一快照。C冷恢复：只恢复原责任先不自动新调用，再按当前资格续行。D保存并读回：写与独立读分别有Operation，写答复丢失沿原键核对。

每条分开记录领域事件E、外部请求R、实际读写W、数据库事务Tx、flush/sync、WAL字节、内容字节、等待及费用。表数量、接口次数或“六组对象”不能换算物理IOPS。比较优化前后时固定输入、模型回放、存储装配、流片段和故障点，暖路径和冷恢复分别报。

## 5 支持与发布声明

Linux是生产参考平台，Linux/macOS及amd64/arm64按实际构建和平台探针分别声明。SDK可编译、Schema向量通过、本机恢复、跨实现互操作、跨AZ容灾和任务质量是不同证据等级。

Go context取消仅发停止信号，不证明goroutine或目标动作结束；释放本地并发槽须观察实际退出。真实凭据/公司平台缺失记录待接入，继续无依赖切片，不使用空库、空目录或伪造成功输出通过验收。

代码提交与运行报告必须绑定commit、schema/method/profile摘要、配置、环境、数据集、故障注入和完整结果。只有达到相应验收门槛才扩大发布声明。静态文档与小模型检查的结果单独说明，不能作为工程实现完成率。
