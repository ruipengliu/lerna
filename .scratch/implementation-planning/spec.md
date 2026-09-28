# 技术栈、工程目录与分阶段实现计划

状态：关键选择已全部确认，工程方案已定稿。

## 用户目标

依据现行技术架构确定技术栈、工程目录和分阶段实现计划，不估算工时。使用 grill-with-docs，逐轮澄清关键选择并记录文档；本轮不创建运行工程、云资源或部署服务。

## 已确认决策

| 编号 | 结论 | 记录位置 |
| --- | --- | --- |
| Q1 | 一个实现 monorepo，Go 初期单 module，默认组件、SDK、CLI/Web 与契约一同维护 | [ADR-0010](../../docs/adr/0010-monorepo-shared-contract-release.md) |
| Q2 | 生产使用公司自有平台，先预留相关控制入口；开发不考虑复杂部署设施 | [ADR-0003 补充](../../docs/adr/0003-production-distributed.md)、[平台边界](../../docs/architecture/engineering.md#platform) |
| Q3 | PostgreSQL 用 pgx + sqlc 与显式 SQL；SQLite 独立方言和适配器 | [技术栈](../../docs/architecture/engineering.md#stack) |
| Q4 | React + TypeScript + Vite 的轻量参考 Web | [技术栈](../../docs/architecture/engineering.md#stack) |
| Q5 | 外部依赖通过默认适配器接入，具体供应方由 Q6～Q8 落定 | [外部接入](../../docs/architecture/engineering.md#integrations) |
| Q6 | 使用兼容 OpenAI 接口的火山方舟模型；参考路径为 Chat Completions + openai-go/v3，固定 profile 并关闭透明重试 | [模型接入](../../docs/architecture/engineering.md#61-火山方舟模型) |
| Q7 | 搜索使用字节豆包搜索；参考路径为独立 Custom 版 WebSearch，由 Executor 执行，正文单独获准获取 | [搜索接入](../../docs/architecture/engineering.md#62-豆包搜索与正文获取) |
| Q8 | 开发固定受限测试身份，生产预留 OIDC 公司身份适配器 | [身份接入](../../docs/architecture/engineering.md#63-开发身份与公司登录) |

## 当前决策树

- 同仓与 Go module：已定；公开 api/runtime/sdk/go、internal 实现与 TS workspace 的目录已写入方案。
- 公司平台：已定；本地启动、静态发现、健康、排空与退出先实现，具体公司适配后接。开发与首次生产准入分别验收，不放宽生产 RPO/RTO 或未知效果门禁。
- PostgreSQL/SQLite：显式 SQL 已定；常规驱动、迁移及生成组织已有推荐，并说明兼容性须在工程建立后实测。
- Web：基础栈已定；SDK 原命令 IndexedDB、WSS 恢复和客户端测试进入切片。
- 外部接入：方舟模型、豆包搜索和身份方案已定，已落实目录、切片和缺失依赖行为。费用和权限沿用原契约，不因选型默认改成估算模式。
- 待部署时固定：精确模型版本、凭据引用、账户限额及公司平台参数；这些运行配置不再构成规划访谈的未决分支。

## 当前产物

[工程落地方案](../../docs/architecture/engineering.md)是技术栈、目录及详细开发切片的维护入口；[验收建设顺序](../../docs/architecture/validation/README.md#5-建设顺序与退出条件)保存系统级阶段与生产准入。总览、部署、目标、存储及优化入口已同步开发/生产边界。CONTEXT.md 没有新增领域概念，本轮无需变更。

## 验证记录

已完成本轮影响范围的语义核对；静态链接/结构与图示渲染分开执行。图示产物保存在 render/，审查范围记录于 [交付审查](../../docs/architecture/review.md#engineering-review)。本轮通过 ArkCLI 文档读取核对方舟 OpenAI 兼容及内置搜索路径，通过官方文档网页核对豆包搜索 Custom API 的认证、参数与响应；SDK 重试依据来自其官方仓库。未编译、运行 Harness，未调用付费模型/搜索 API，未接入公司平台。
