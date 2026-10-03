# 08 brain-interaction-assembly

Status: partial
Blocked by: 外部权威与供应商验收、未开放能力的完整业务合同

依据 [实施规格](../spec.md) 与根 AGENTS.md，保留准确身份、负责方、事务、门禁和恢复。

## Comments

2026-10-03：已实现持久 Brain、原上下文及闭合提案、单次物理模型出口、原调用费用、
Interaction 输入与未来排程、原命令 WSS/gRPC 绑定和独立进程装配。参考业务适配放在
`adapters/development`，进程入口只做装配，不复制领域裁决。

真实 SQLite/PG 报告闭环覆盖实际写文件、独立读回、当前条件和准确 Result；可配置模型
整链覆盖原历史/附件、发送前预留、一次真实测试 POST、Task/Grant 两方费用及断连保留。
公开澄清保留初始输入与回答的 SourceEvidence，验证包装来源不能冒充原提交；原文与旧
Encoding 重开沿原字节恢复。排程固定原 install_lock_ref、Occurrence 和原交付命令。

`877730f` 的真实 PG/静态服务浏览器全流程与同一首 Task 的原生两次心跳观察均通过。
WSS 会话撤销、控制槽与持久日志容量的审查修复具有独立版本及专项证据；最终统一检查
和审查状态由 [覆盖报告](../../../docs/architecture/engineering/implementation-coverage.md)
给出，不把较早的浏览器提交外推为后续代码的整轮结果。

未完成：通用自然语言质量与真实供应商对账、Search/Body 获取、完整 GUI 手势、跨 owner
远端 Authority/协作与设备部署、完整生产 profile。生产 OIDC/密钥、跨 AZ 和容量验收
仍需独立前提；这些不能用本机同库或模拟器的结果替代。
