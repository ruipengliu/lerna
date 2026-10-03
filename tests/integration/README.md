# 真实持久框架套件

`make durable-check` 对 PostgreSQL/SQLite 运行同一公共探针。Go 普通测试使用临时 SQLite；没有 LERNA_TEST_ADMIN_URL / LERNA_TEST_APP_URL 时 PG 显式 skip。CI 可提供可建隔离数据库的测试身份及受限 lerna_app 连接；本机工具读取 dev/.env。每个 PG 用例创建独立随机数据库并只删除自己的库，不使用生产/主开发库承载断点。

| 文件 | 观察边界 |
| --- | --- |
| durable_test.go | 原命令/业务键去重、准备与最终阶段、旧 Claim、责任合并、长期身份、锁序与有限重试 |
| boundaries_test.go | 命令命名空间、当前披露、关闭后保留期、提交前过期、方法修订拒绝、阻塞续租与受限 Work |
| process_test.go | 真子进程在接纳提交前后被终止、独立 PG worker 崩溃与接替 |
| commit_proxy_test.go | PG 已执行 COMMIT 后截断网络确认，原命令核对与候选 Claim 独立确认 |
| runner_test.go | 取消后实际容量、请求独立、全丢通知扫描、独立控制池 |
| reconcile_test.go | 领域未结索引/有限页修复、SKIP LOCKED、通知与监听重连 |
| cost_test.go | 固定小负载的实际事务、SQL、p95/p99、连接等待、积压及 WAL 观测 |
| orchestrator_scheduler_test.go | 真实领域准入产生派发作业，共享 provider/resource 限额、独立容量、领取结束后释放及旧路由投影恢复 |

durable_probe 是测试专用领域事实表；新责任经真实事务入口产生。唯一直接删作业的用例明确注入迁移遗漏，用来验证有来源证明的修复；它不作为正常责任创建方式。断点/观察端口不暴露给产品 handler。FW-02/05/07 的真实发布、目标效果、费用与清理仍需要对应模块测试。

[本轮证据](../../.scratch/reliable-work/evidence.md)分别记录 pass/inconclusive。测试不把构造回执、数据库调用数或 SQLite 逻辑 worker 视为公开协议互操作、业务成功或 PG 多进程证明。
