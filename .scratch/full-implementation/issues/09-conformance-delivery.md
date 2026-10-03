# 09 conformance-delivery

Status: partial
Blocked by: 08 的未开放能力、完整生产与外部验收

依据 [实施规格](../spec.md) 与根 AGENTS.md，运行证据必须来自真实负责方和验收边界。

## Comments

2026-10-03：已建立统一 `scripts/check`、锁定生成器、CI 定义与分层合同/存储/集成/
故障/浏览器测试。PG/SQLite 独立取证，未配置数据库的 skip 不算该数据库通过。
实际原命令去重与丢回执、进程丢失与重开、旧 Claim、撤权/跨租户、原文件与模拟设备
真值、TLS WSS/gRPC 和独立 Gateway/Application/Worker 的证据见
[实施覆盖报告](../../../docs/architecture/engineering/implementation-coverage.md)。

完整 PG 浏览器 `877730f` 已验证三份 Result 的准确引用和正文、原投递恢复、澄清、
控制、首次 Surface 事件、内容拒绝、Memory、窄屏及退出；同一首 Task 的原生两次
35 秒心跳读取保持准确 Result/文件，未新建业务命令。较早失败制品保留。

最终双轴审查分别检查开发规范与设计符合度，确认问题由一个实现者修复；锁序源码
违反不写成已复现死锁。统一检查、后续修复回归和实际实现 commit 在覆盖报告中逐项
记录，不把定义了 CI 或本地检查通过写成托管 CI 已执行。

未完成：F01–F25 完整矩阵、1000 API/生产规模与性能、三 AZ RPO/RTO、真实供应商及
自然语言质量、真实设备和不可信组件资格验收。Search/Body、完整 GUI 手势、远端
权威等本地未实现项也保持 partial，不能全归为外部凭据 blocked。
