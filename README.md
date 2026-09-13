# Lerna Harness

面向通用 Agent 的端云 Harness。架构、能力目标及后续实施任务见 [架构入口](docs/architecture/README.md) 和 [实施票据](.scratch/harness-implementation/README.md)。

当前可运行内容包括 **01 票的 Go SDK 契约样例**、**02 票的本地身份、策略与授权**、**03 票的持久任务接纳与查询**、**04 票的有限 Worker 运行与恢复**、**05 票的任务控制**、**06 票的受限签名授权**、**07 票的受控证据与产物**、**08 票的有界模型问答**、**09 票的补充输入与重新决策**、**10 票的同步 API 执行与效果确认**、**11 票的异步调用与未知效果恢复**、**12 票的共享资源控制**和 **13 票的千级能力目录**，尚不是完整任务运行系统。

```sh
make sample                    # 可导入 SDK → Protobuf → 内存契约夹具
go run ./cmd/contractcheck      # JSON 契约报告；失败返回非零退出码
make verify                    # 生成一致性、编译、静态检查、race 测试和样例
```

参考工具链为 Go 1.26.1、protoc 36.1；Go 模块最低要求 1.26.0，Makefile 固定实际验证补丁版本。已提交生成代码，单独构建和运行不需要 protoc。首次获取 Go 模块需要网络，之后样例本身不联网，不需要模型、数据库或设备服务。

[SDK 使用、验证方法与边界](docs/implementation/01-sdk-contract.md) 说明协议子集、报告含义及依赖固定方式。本地模块路径暂用 `lerna`，尚未绑定公开托管地址。

[本地身份与授权指南](docs/implementation/02-local-authorization.md) 提供 `authctl` 初始化及管理入口。参考实现使用进程内接口与真实 SQLite 文件，无需独立数据库服务；执行 `go run ./cmd/contractcheck -profile local-auth-v1` 可验证授权和进程恢复。`make verify` 同时验证已实现票据的实现。

[持久任务指南](docs/implementation/03-durable-tasks.md) 说明 SDK 组装、授权条件和 RunStore 恢复契约。运行 `go run ./cmd/contractcheck -profile durable-tasks-v1` 验证真实 SQLite 任务接纳。

[有限 Worker 指南](docs/implementation/04-bounded-worker.md) 说明脚本 Brain、领取与续租、预算及恢复。运行 `go run ./cmd/contractcheck -profile bounded-worker-v1` 验证正式结果、竞争与真实子进程恢复；不需要模型或外部设备。

[任务控制指南](docs/implementation/05-task-control.md) 说明暂停、取消、明确恢复与在途核对。运行 `go run ./cmd/contractcheck -profile task-control-v1` 验证控制接纳、实际落实和真实进程恢复。

06 的签名授权、有限委派与撤销使用说明见 [受限授权实现](docs/implementation/06-restricted-grants.md)。

07 的受控引用、正文读取与清理说明见 [受控证据与产物](docs/implementation/07-controlled-content.md)。

08 的 Brain/Model 分离、生成预算、受控答案与恢复见 [有界问答实现](docs/implementation/08-bounded-answer.md)。`go run ./cmd/contractcheck -profile bounded-answer-v1` 验证本地契约；实际模型验收单独运行 `go run ./cmd/answercheck -env-file .env`，每次最多发出一个付费请求，不纳入 `make verify`。

09 的等待项回答、事实/目标更新和额度调整见 [任务更新实现](docs/implementation/09-task-updates.md)。运行 `go run ./cmd/contractcheck -profile task-updates-v1` 验证受控模型、并发回应和 SQLite 恢复，不调用真实模型。

10 的正式能力入口、单次许可消费、独立目标核验及报告交接见 [同步执行实现](docs/implementation/10-synchronous-execution.md)。运行 `go run ./cmd/contractcheck -profile synchronous-execution-v1` 验证持久模拟 API 与真实 SQLite 恢复，不调用真实业务 API 或模型。

11 的异步句柄、有限恢复、独立取消、可信进度与冲突证据说明见 [异步恢复实现](docs/implementation/11-async-recovery.md)。运行 `go run ./cmd/contractcheck -profile async-recovery-v1` 验证独立持久模拟作业、取消竞态和进程恢复。

12 的跨能力接管、恢复与目标围栏见 [资源控制实现](docs/implementation/12-resource-control.md)。

13 的 List/Search/Describe、当前权限过滤、准确版本准入与 1008 项模拟 API 见 [能力目录实现](docs/implementation/13-capability-catalog.md)。运行 `go run ./cmd/contractcheck -profile capability-catalog-v1` 验证完整目录下逐项 SDK 执行、独立业务效果、索引故障和真实进程恢复；不调用模型或第三方业务服务。

14 已提供 [模型提案与 API 执行循环](docs/implementation/14-api-brain.md)，支持同 Task 多步推进、整批准入、有限纠错、恢复及缺输入交互。`go run ./cmd/contractcheck -profile api-brain-v1` 验证 12 项离线行为；真实模型小回归已完成：一个提案驱动两个依赖动作；首次失败、修复与 token 用量均保留，Ark 单步包络限制仍适用。

18 正在实现 [受控个性化上下文](docs/implementation/18-personalized-context.md)。`go run ./cmd/contractcheck -profile personalized-context-v1` 验证 52 项本地契约，包括适用记忆改变回答/API 选择、来源失效阻止发布或启动，以及原身份下的回答、行动进程恢复、第二决策恢复、失效结算、有界重组及失败组装历史恢复与缺失元数据治理；使用确定性模型，不调用外部服务。该入口覆盖的契约及尚未完成的范围列在 JSON 报告中，不代表整票或模型质量验收完成。
