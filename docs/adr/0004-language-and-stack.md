# 0004 开发语言与主要技术栈

| 日期 | 修订说明 |
| --- | --- |
| 2026-10-05 | 初版。 |
| 2026-10-06 | 按[第五轮评审处理记录](../review/disposition.md)补充：SQLite 驱动的选择条件与决定时间（X-19）；手机端语言的约束（X-20）；签名表示与 WASM 运行时不由本 ADR 决定；补全影响清单（A4-03）。已有决定不变。 |

- 状态：已采纳
- 影响：[项目目标第 13 节](../architecture/project-goals.md#13-待定事项)、[分层与模块第 11 节](../architecture/layers.md#11-代码组织)、[核心契约 7.5、7.8](../architecture/core/contracts/README.md)、[开发规范](../development.md)、[数据与存储 3、5.4、7.2、9](../architecture/topics/data-and-storage.md)、[持久工作 4.4、7.2](../architecture/core/durable/README.md)、[安全 7.2](../architecture/topics/security.md)、[出口闸门 4.5](../architecture/core/egress/README.md)、[扩展管理 8](../architecture/platform/extensions/README.md)

## 背景

项目目标没有选定开发语言，把它列为待定事项。M1 开始写代码之前必须定下来，目录骨架、工具链和依赖规则都取决于它。

核心的重点是事务、持久化、并发和长期运行：准入要在一个可串行化事务里完成，执行管理要在崩溃后从持久记录恢复，出口闸门要在实际 I/O 处核验和记录。端侧（手机、电脑）要部署核心的执行部分。

## 决定

| 部分 | 语言或技术 | 开始阶段 |
| --- | --- | --- |
| 核心、受信实现、宿主、命令行 | Go | M1 |
| 公共契约的结构定义 | Protobuf，由 buf 管理检查和代码生成 | M1 |
| SDK、浏览器界面 | TypeScript（pnpm workspace） | M3 |
| 手机端的执行部分 | 未定 | M3 前决定 |
| SQLite 的 Go 驱动、内置 SQLite 版本的固定方式、迁移工具 | 未定 | M1 第一张存储工单前决定 |

- 仓库是单一的 Go 模块，模块路径 `github.com/ruipengliu/lerna`。
- 存储沿用设计文档的选择：云端裁决域用 PostgreSQL，端侧和 M1 本地用 SQLite（见[数据与存储](../architecture/topics/data-and-storage.md)）。
- 核心契约 7.5"Protobuf 作为结构定义的唯一来源"仍需按待定事项 H1 在真实对象上验证；验证不通过时，回到核心契约 7.5 重新选择，并修订本 ADR。
- **SQLite 驱动的选择条件。**选定的驱动必须能固定并核验实际链接的 SQLite 版本和构建（数据与存储 5.4 要求包含 3.51.3 的 WAL 修复），能设置并读回 `synchronous`、`fullfsync` 等同步配置，支持 `make test -race`，并说明能否用于 M3 的手机端。可选的驱动类别（cgo 绑定、纯 Go 转译、基于 WASM 的实现）各自是否满足这些条件待验证；结论补入本 ADR，并写进平台准入表。
- **手机端语言的约束。**无论手机端最终用什么语言，执行管理、出口闸门和持久工作的第二份实现都必须通过同一批与语言无关的契约场景（[开发规范 5](../development.md#5-测试)、[一致性测试](../architecture/verification/conformance.md)）；复用 Go 核心是选项之一，不预先指定。
- **与语言无关的选择。**签名的规范化表示（安全 7.2）和插件沙箱的 WASM 运行时（出口闸门 4.5）不由本 ADR 决定，分别在对应文档中选择；Go 宿主下的 WASM 运行时在 M4 前选定。

## 后果

- 目录骨架和开发规范按 Go 的习惯组织：受信实现放 `infra/`，装配入口放 `cmd/`，模块私有代码放 `core/<模块>/internal/`，用编译器落实 R6 的一部分。
- M1 只有一种语言的工具链；TypeScript 从 M3 引入。
- Go 的 `context` 取消只是停止信号，不证明 goroutine 或外部动作已经结束；涉及出口和恢复的代码不得把取消当作"未发生"。

## 考虑过的备选

| 方案 | 结论 |
| --- | --- |
| 核心用 Rust | 便于将来嵌入手机端，但开发和招募成本更高；手机端怎样实现留到 M3 单独决定 |
| 全部用 TypeScript | 开发快，但长期运行的可靠性核心、并发和单二进制部署较弱 |
| 核心用 Python | 生态方便，但并发和部署形态较弱 |
