# 工程组织与实施顺序

实现采用一个 monorepo、初期一个 Go module，核心、默认组件、SDK、CLI/Web 与验收资产随同版契约维护；先交付真实本地闭环，再验证跨进程和生产故障域。实现与验证状态只在 [review](review.md) 记录。

## 技术栈与依赖

构建固定经编译和适用验收的精确版本，不使用浮动 latest。下表保留工程选型，版本组合的启用以 InstallLock 为单位。

| 范围 | 选择 | 实现约束 |
| --- | --- | --- |
| 核心／宿主／CLI | Go 1.27 系列、标准库 | Linux/macOS，amd64/arm64 构建矩阵；cgo 和系统依赖单列 |
| Go 检查 | go test、race、vet、fuzz | 规则测试就近，跨进程验收独立 |
| 服务与端云 | net/http、grpc-go、官方 Protobuf、coder/websocket | 同进程直调；WSS 单有序写队列；原始 Proto 先严格校验再普通解码 |
| JSON | 严格解析、jsontext、santhosh-tekuri/jsonschema/v6 | 重复键、Unicode、原数字检查在转换前；开启 2020-12 格式断言，只载锁定本地 Schema |
| PostgreSQL | 18 系列、pgx/v5、sqlc、显式 SQL | 原事务句柄、条件写和提交未知可审查 |
| SQLite | modernc.org/sqlite、database/sql、独立 sqlc 查询 | 与 PostgreSQL 分别维护和验证方言；需要原生能力时再比较其他驱动 |
| 迁移 | pressly/goose/v3、分方言目录 | 显式管理入口、迁移锁和检查点；副本启动不自动迁移 |
| 字节／检索 | 本地不可变文件、公司对象存储适配、默认词法检索 | 目标文件与 ContentStore 分开；向量策略按证据启用 |
| 模型 | 火山方舟 OpenAI 兼容 Chat Completions、openai-go/v3 | 固定地址／模型 profile，关闭 SDK 透明重试，供应方扩展封装在 ModelAdapter |
| 搜索／获取 | 豆包搜索 Custom 独立 WebSearch、net/http 正文驱动 | 搜索与正文取得分别准入和计费，保存真实范围与时间 |
| Web | React、TypeScript、Vite、React Router、pnpm workspace | 静态参考客户端，UI 使用 SDK，不承担业务裁决 |
| TypeScript SDK | 原生 WebSocket、IndexedDB＋idb、Ajv | 本地事务成功后才发送；严格入站解析先于 JSON.parse |
| Web 测试 | Vitest、React Testing Library、Playwright | 刷新、断连、确认过期、本地存储失败 |
| 身份／观测 | 受限本机测试身份；生产公司 OIDC；slog＋OpenTelemetry | 生产禁用 dev 身份；平台认证不替代 Grant；日志不充当账本 |

显式 SQL 需要两套方言，但领域规则共用。生成器先通过 oneOf、封闭对象、十进制字符串、安全整数及引用结构的正反例；CI 检查重新生成无差异。库无法满足严格解析时更换实现，不能放宽契约。

方舟适配器固定模型、上下文窗口、参数、最大输出、费用模式、取消和原调用核对能力；供应商不提供可信上界时按[预算](orchestrator.md)限制启用。搜索独立配置凭据，WebSearch 返回的摘要只作为已取得范围，正文获取失败保留缺口。测试身份仅绑定本机受限入口，真实秘密不写入仓库。

## 外部服务的固定接入配置

模型适配器通过 Chat Completions 调用方舟，参考北京地址 `https://ark.cn-beijing.volces.com/api/v3`，显式配置 API Key 与 `WithMaxRetries(0)`；代理和网关也关闭透明重试。非法提案、截断、拒绝和未知发送均交回既定决策流程，适配器不追加模型调用修复。原生结构化输出仅在精确模型 profile 支持并通过对应验收时启用。可变模型别名或端点配置不能充当冻结的验收版本。

搜索使用独立 `POST https://open.feedcoopapi.com/search_api/web_search` 与 Bearer 凭据；需要公司 AK/SK 时装配同产品 TOP 接口。请求和正文获取分别经过 Executor，模型 profile 不启用供应方内置联网工具，以保持逐行动准入和记录边界。

| 搜索项 | 固定参考配置与处理 |
| --- | --- |
| Query／SearchType／Count | web，默认 10 条；查询 1～100 字符、数量上限 50，超界拒绝 |
| Filter | NeedUrl=true、NeedContent=false；这是候选筛选，不是全文取得证明 |
| QueryControl／EnableWaiting | QueryRewrite=false、EnableWaiting=false；任务负责查询意图及等待 |
| 返回判断 | 同时检查 HTTP、ResponseMetadata.Error 和 Result；接口失败不转为空结果 |
| 材料 | 保存实际存在的 Url、Title、发布时间、Snippet、Summary、Content 及来源；可选正文或片段不能据字段名当完整实时证据 |

正文驱动限定重定向、字节数和期限，重定向后目的地重新核验。抓取失败、截断、解析失败分开保存，不以摘要替代取得全文。只读搜索的有限重试仍逐次记录真实尝试和费用；发送后超时不证明未收费。

## 代码边界

公开稳定面收敛在 api、runtime 和 SDK，内部表、作业结构及生成 Proto 不作为扩展接口。领域依赖声明的 ports，适配器由入口装配；共享事务由宿主显式传递。

| 路径 | 内容 |
| --- | --- |
| `api/`、`runtime/` | 公开领域值与 ports、嵌入及默认装配入口 |
| `sdk/go/`、`sdk/ts/` | 远程客户端、原命令保存和恢复、扩展辅助；Go 初期随根 module |
| `cmd/harness/`、`harnessd/`、`harness-sim/` | CLI、单体／指定角色宿主、有状态模拟设备 |
| `internal/{orchestrator,brain,execution,memory,security,interaction,collaboration,extensions,evaluation}/` | 九个领域的用例、规则、store port 和处理器 |
| `internal/durable/`、`host/` | 公共持久框架、时钟、配置、容量、恢复和排空 |
| `internal/wire/`、`transport/` | 严格解码与映射、WSS/gRPC/HTTPS |
| `internal/storage/{postgres,sqlite,content}/` | 两种 SQL 及生成结果、内容介质 |
| `internal/adapters/`、`platform/` | 方舟、豆包、正文获取、文件、设备及平台接入 |
| `apps/web/` | Surface、输入、预览、任务和设备参考界面 |
| `contracts/`、`migrations/` | 契约唯一源、按事务边界及方言组织的迁移 |
| `tests/{conformance,integration,faults,quality,capacity,fixtures}/` | 一致性、真实数据库与进程、故障、质量、容量和受限夹具 |
| `dev/`、`packaging/`、`tools/` | 本地启动、精确制品与平台示例、生成与报告工具 |

该表定义实现布局；文档基线内机器字段源是 [contracts/schemas](contracts/README.md)。实现仓库接收契约时统一迁移维护源与生成入口，不各自维护第二套字段。

## 平台接入

公司平台和本地装配调用同一宿主生命周期入口，开发无需部署公司控制面。

| 入口 | 本地形式 | 平台责任 |
| --- | --- | --- |
| 启动 | 固定角色、配置及制品摘要 | 传入同义配置与实例身份 |
| 健康 | 存活、恢复中、可接纳、排空分开 | 按角色检查，不把存活当就绪 |
| 排空与退出 | SIGTERM 或受限管理命令 | 摘流量、宽限期和最终退出；残留回原账本 |
| 实例发现 | 静态地址集合 | 健康地址、有界新鲜度和实际流槽 |
| 存储／身份／时间 | SQLite／本机 PG、受限身份、ClockAdapter | 当前写主、准确字节、凭据代次和时钟证据 |
| 遥测 | 有限结构化输出 | 公司已有采集与告警 |

迁移走独立受信管理动作，核对精确步骤和互斥锁。平台启动参数不能绕过安装批准、实例 reopen 或业务授权。

## 实施切片与退出条件

每个切片开放具备真实依赖的能力，契约和相应用例同时接入。正式生产准入独立于本机开发顺序，依[部署](deployment.md)验证实际故障域。

| 阶段 | 切片 | 退出证据 |
| --- | --- | --- |
| 一：入口与持久基础 | 单仓／生成／SDK，受信最小宿主，真实 SQLite 和 PostgreSQL、Tx／commands／jobs、时间与身份 | 严格协议向量、合法首装批准、FW 事务及领取竞争 |
| 一：任务到文件闭环 | 目标与条件、Brain、Grant／Use、Content、文件写入与独立读回、完成核验 | 接纳重启、未知写入、取消、目标修订、预算、输入及恢复；以实际目标真值判定 |
| 一：跨进程 | 独立网关／应用／worker／执行宿主，原命令和主动交付 | 失答复、通知全丢、两个 worker 竞争、内部重绑及查询恢复 |
| 二：专项与用户控制 | V1 问答、V2 多个有状态模拟手机、记忆个性化、纠正／删除／接管 | 专项质量、控制和来源闭环；扩大分片及负载验证公平性 |
| 三：替换与跨端 | 三系统各两种实现，两向端云、外部 Agent、离线许可／额度、受管副本 | 同契约异构互操作、组合故障、格式兼容、恢复积压 |
| 四：治理与目标规模 | 隔离评测、改善发布／独立批准回退、API 与质量覆盖、最终负载矩阵 | 正式改善证据及逐目标恢复；容量和单区灾备实测 |

阶段一即建立隔离、权限、原效果核对和持久恢复，不把它们留作上线补丁。提供方真实质量与固定回放机制分别取证；1000 API 和最终规模按完整计划逐步达到。

新能力的先决交接包括：计费源终态后修订与持久交回，估算费用的受信接受记录，跨来源列表的预登记目录，正式发布对原暴露事实的同步检查。缺少相应依赖时限定为严格费用、单来源查询或关闭正式发布，而已有责任继续核对。

## 保证与限制

本地闭环、跨进程恢复、生产高可用和真实质量使用不同证据，互不替代。公开方法按启用清单声明，不支持的方法明确拒绝；Schema 完整不代表所有方法可调用。首个可分发制品须有受信契约报告和兼容批准，不能以开发模式绕过。

## 取舍

同仓同版降低早期协议与 SDK 分叉风险，维护者承担 Go 与前端构建协同；出现独立发布节奏、依赖体积或团队边界时再拆 module 或仓库。显式 SQL 保留事务可审查性，代价是分别维护两种数据库；适配器行为不等价时先修差异，不用抽象层隐藏。
