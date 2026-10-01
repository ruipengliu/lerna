# 架构刷新验证记录

日期：2026-10-01。设计文档、源码静态比较与运行实现分别登记。

## 已执行

- 六项既有机器契约检查全部退出为 0；完整命令对应脚本名与输出保存在 [contract-checks.json](contract-checks.json)。包括协议 55 条正向序列、371 个反向变体及 105 个方法。Schema、生成契约、验证脚本均未修改。
- 架构文档检查扩展到正式入口与 .draft；ADR 回链、五份项目报告和调研对比目录另验。最终输出见 [delivery-checks.json](delivery-checks.json)。
- [link-repairs.json](link-repairs.json) 保存 .draft 相对路径及 ADR 回链的 46 项修复；ADR 文字决定保持。
- 新增 [Session 流程图](session-flow.png) 用 mmdc 和本机已安装 Chrome 渲染并目视检查，中文标签、连线及边缘无裁切。默认 mmdc 起初缺少其锁定版本浏览器，改用已有浏览器完成，未安装依赖。
- 分项目智能体完成上下文／推进、执行／安全、交互的设计及局部复核，根侧完成恢复、协作、扩展、候选策略、Session 与整合。

## 证据边界

参考项目的读写频次来自固定源码路径推导，未运行上游测试或 benchmark。本项目未实现服务、存储适配器、模型和平台故障试验；HAR 新向量是待运行设计。静态构造通过不证明真实数据库原子性、隔离、恢复、质量或性能。

本轮调研前 sources.json 及原验证快照保留，架构优化后不重新生成旧哈希。原源码提交与新引用另做定点核查。

## 最终交付检查

运行 `python3 .scratch/harness-architecture-refresh/verify-delivery.py`：PASS。覆盖 55 篇架构 Markdown、10 篇 ADR、综合报告及三个 IO 附录、五份项目报告与本任务票据。五个参考源码的 HEAD 与原固定提交一致、工作树干净；新增对比引用的 162 处固定源码路径和行号范围合法。11 项设计票据均 completed，全部验收项已勾选；diff 空白检查通过，公开机器资产和历史 sources.json 未改。

另核对 SQLite 官方 transaction 与 synchronous 文档，确认 SQL、隐式事务与 WAL 同步不能一一换算。源码语义复核区分 Codex 实际本地默认 Paginated 与 trait 默认、Crush 的完整 ToolCall 更新和读取辅助登记。主报告的跨 owner 事务、Session 有界内联与本次 Content 选型、模型请求与 Decision 次数也已经交叉复核并收窄条件。

本轮只渲染并目视检查新增一张 Session Mermaid；其他 Mermaid 为结构检查。未增加运行测试结果或跨项目性能排名。
