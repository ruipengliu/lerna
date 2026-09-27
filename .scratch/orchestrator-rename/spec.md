# Orchestrator 命名统一

Status: resolved

用户确认将当前方案中的 Task Home 模块改名为 Orchestrator（任务编排器）。统一当前架构正文、模块目录、术语、尚未发布的机器契约、构造示例、校验器及当前图示；历史归档、日期化评审材料与以往工作证据保留。

名称映射：Task Home / Home → Orchestrator；任务运行时模块 → 任务编排器；task-runtime → orchestrator；home_id / sender_home_id / home_proof → orchestrator_id / sender_orchestrator_id / orchestrator_proof。示例身份和签名配置同步命名，已构造的签名须重新签发并验证。任务方法 task.* 和内部 TaskCoordinator 的职责不改变。

语义约束：一个任务唯一且固定的逻辑归属；多个工作进程可以共享该归属的权威库；Brain 提案、Orchestrator 准入、Executor 效果事实三者保持分工；重启和重新连接沿原任务、命令及操作身份恢复。

图示沿用已确认的画布、布局和风格，只更新名称并调整必要的标签空间。验证分为术语与语义核对、Schema／构造序列回归、文档链接、图示结构和实际渲染；本轮不涉及服务运行验证。

## 交付与验证

- 模块入口改为 `docs/architecture/orchestrator/README.md`；正文、术语、ADR、链接、Schema、方法登记、示例与校验器同步名称。方法仍为 104 个，`task.*` 等业务方法及状态行为不变。
- 正常接纳、已提交后断线、进程替换、控制乱序与跨编排器委派仍沿原任务和命令身份恢复；逻辑归属字段不表示进程实例。
- 构造序列：50 正例、343 定向反例；传输：100 正例、218 定向反例；签名：2 ES256、3 JCS、23 反例；成果投影：5 正例、9 反例，全部通过。
- 文档本地链接与结构：38 Markdown、945 链接、82 Mermaid，零错误。变更的 36 张 Mermaid 已渲染，抽查内部组件、端云交接与生产恢复图。
- 两份 drawio 的 11 个变化页面已渲染并查看；全部 21 页结构、231 个来源链接、254 个 Schema 属性及真实引擎 XML 回读一致性通过。标签空间不足处改为短标签或换行。Orchestrator 组件页的共进程旧说明同步现有生产分工。
- 概念图通过内置 image_gen 更新三处文字；提示词与保存路径见 [生成记录](concept-image-prompt.md)。日期化 PPT、历史归档及以往验证证据保持快照含义。
- 本轮未运行服务、并发、故障、容量或互操作实验；静态回归不替代这些证据。
