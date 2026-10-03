# Harness 全项目参考实现

Status: partial

本轮在 code-dev 实现现行技术设计及 C1–C9、A1–A4、V1–V2 的可运行参考实现。依据为 AGENTS.md、CONTEXT.md、docs/architecture/engineering/implementation-readiness.md 及其覆盖表；业务前态、负责方、原身份和恢复不得重新选择。

## 交付

- 根 Go module、锁定工具链、确定生成、独立迁移、检查入口及 CI。
- Runtime 的原命令决定、提交三态、有界 Job/Claim 和 PG/SQLite 独立实现；身份、租户、owner、namespace 和 Tx 明确约束。
- Task/目标/条件/覆盖/预算/Result、Brain 单次物理出口、Execution/Attempt/Effect/控制/文件与多台模拟手机、Content/Memory/来源/清理、Session/输入/分支/未来触发。
- 授权使用/可信确认、费用差额、协作/ChildHandle、证据资格/缺陷、安装/激活/独立批准回退及冻结评测。仅通过完整合同的平台能力才开放。
- 严格 JSON、JCS、闭合方法 Schema、回执/查询、WSS/gRPC、Go/TS 恢复 SDK、CLI 与 React/Vite Web。
- 正反例、真实 PG/SQLite、重启/重复/丢回执/旧 worker/撤权/跨租户/文件和 GUI 真值、浏览器及跨组件验证；逐项覆盖和可复现证据。

## 已授权验收边界

用户要求完整编码与功能验证，AGENTS.md 和设计已指定公开方法、repository 合同、实际数据库、目标真值、WSS/gRPC 和浏览器作为验收边界。本轮沿这些边界开展逐条行为验证，不以私有函数快照或同构 mock 代替运行证据。

## 实现装配合同

公开领域记录类型从唯一手工源 docs/architecture/protocol/core.schema.json 生成到 api。新方法以各模块的闭合 Schema 登记，生成发现 manifest；不声称所有 architecture profile 已支持。生成的 Schema 快照属于派生资产，不独立编辑。

Runtime 仅管理 Scope、Tx、记录版本/业务唯一键、命令回执与 Job。参考 SQL 将不同 namespace 的 owner 记录放在共同物理表，按 tenant/owner/namespace 键隔离；它不是中央事件库或第二份领域真相。模块提供类型化 repository/用例；Tx participants 是受信宿主声明的 namespace 根集合。默认本机云端各领域可显式同 PG owner 装配，设备 Executor 使用独立 SQLite，不写云端 Task。

记录 revision 从 1 开始，Create 不复用身份，Put 显式比较旧 revision 且保留历史版本。Related 列用于 owner 完整关系索引，不能依据公开 API 一页推断全集。所有扫描必须有界。实体 JSON 内 revision 与记录 revision 一致，由领域负责填充。

方法注册返回 Method（契约、participants、Apply 或 Query）；写入通过 Dispatcher 的原键判重、SAVEPOINT 中的业务变化、固定拒绝或 applied/accepted 回执共同提交。网络 IO 不进入 Tx。外部事实使用独立可信归并入口。作业通过 Runtime Finish 的 Guard 与 work_revision 比较共同保存；return nil 不表示 done。

## 外部前提

当前环境没有模型/搜索真实账户、公司 OIDC/密钥、Android/iOS 设备、三 AZ 同步 PG 或最终规模集群。实现可配置适配及独立探针；使用本机服务/独立有状态模拟设备证明参考行为，真实服务和生产容量/容灾保留具体未验证项。不得用测试预置结果冒充自然语言质量、1000 API 或生产指标达标。

TaskPolicy、风险及阈值通过准确配置固定。仅测试/开发配置有示例数值，生产必须显式提供并确认，不继承示例。

## 工单图

01 合同与 Runtime 端口 → 02 存储、03 Task、04 Execution、05 Memory、06 治理、07 SDK/Web。
02–07 → 08 Brain/Interaction/传输/进程装配 → 09 集成故障验证/覆盖审查。

完整范围复核增加的本地前沿：11 Search/Body、12 模拟 GUI、13 隔离 WASI → 15 配置行动闭环；
14 上下文查找、19 授权列表及 22 Memory 清理恢复归最后复核修复；16 独立设备 Executor、17 跨 owner Agent、
18 Skill/AgentConfig、20 EndpointChannel 重绑与分类 worker、21 三系统第二实现分别保留独立工单。
23 跨 owner Content 登记和来源门禁是 16／17／21 的材料交接前置；保留原引用，不能只代理字节或改写 owner。
这些是本地编码与验证范围，不能以缺生产账户或设备作为完成依据。

## 完成标准

每个工单记录实际行为检查与未满足外部前提。代码可编译但方法未完成、扩展被关闭或验收没有运行的条目保持部分/blocked，不能记 resolved。完整项目完成状态由覆盖报告判断，不由目录数量判断。

## 当前交付状态

工程已建立可运行的有界参考实现，完成范围、真实运行证据与复现命令统一见
[实施覆盖报告](../../docs/architecture/engineering/implementation-coverage.md)。状态保持
partial：Search/Body 获取、完整模拟 GUI 手势、不可信 WASI、上下文查找、授权列表恢复、
独立设备权威、远端委派、Skill 配置、Channel 重绑与三系统第二实现仍在补齐；
真实模型质量、公司身份、真机、生产规模与容灾还缺少
独立外部验收。显式 unsupported 只防止错误接纳，不代表这些要求已经完成。

工单中的 resolved 只对应其已定义参考切片，不能据此推断整个 C1–C9、A1–A4、V1–V2
或 F01–F25 全部达标。已通过与未通过、实现证据与设计模型分别记录，不把丢回执后的
同义新命令、重新初始化数据或增加业务期限作为恢复证据。
