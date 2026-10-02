# 正式架构的审校与证据状态

[总览](README.md) · [验收入口](validation/README.md) · [来源与验收追踪](coverage.md)

审校日期：2026-10-02。当前正式正文由核心执行、交互治理和存储运行三组分段编写，全局入口、对象、场景及契约统一整合，再按应用接入、模块实现、运行维护三个独立读者角色审校。设计完整性、静态契约、真实运行与性能分别记录。

## 1. 正文与来源

正式目录包含九模块、完整任务与应用工作流、核心对象和记录关系、共同契约、可靠工作、存储、工程、部署及验收。原 `.draft` 与研究材料保持原样；[来源映射](source-map.json)的 185 项原文件摘要已核对，所有正式维护目标存在。四份既有正式正文按映射中的固定 Git 修订核对，详细规则分别进入任务、记录、预算、核验和公共框架专题。

[设计追踪](coverage.md)给出每次交接的接口、负责记录、共同提交、恢复和验收位置；[研究依据](research-basis.md)区分已采用机制、候选实验与历史证明。54 条行为要求和 24 项实施选择保留原编号及完整[逐项追踪](validation/core-model-traceability.md)。SourceMap 的摘要证明核对的是哪些材料，语义覆盖由作者逐章盘点及读者审校判断，不能从文件数量推导设计正确。

整理中统一了 InputRequest 的实际业务负责方，补齐 Session 消息／完整原命令／outbox／Task 关联和 ApprovalLease 的持久映射。原参数、事务条件、准入及收尾区别保留；未定义的自由聊天队列、完整分支、公共 Schedule 和通用环境合同继续明确为按需待交付能力。

## 2. 独立读者审校

三个读者先只阅读正式正文，独立复述正常链及恢复责任；形成理解后再定向核接口、字段与运行边界。反馈修订后交原读者复核，范围内没有剩余正文阻塞。

| 读者与报告 | 发现及正式修订 | 最终复核 |
| --- | --- | --- |
| [应用接入](../../.scratch/architecture-formalization/reader-application.md) | APP-01 定义 input-answer/1 的实际字节及五类字段；APP-02 保证无 Surface 的准确请求发现和共同更新；APP-03 将预固定消费者／Confirmation 连接到验收队列 | 三项 resolved；另读取并运行真实字节／Schema／发现及消费者关联检查 |
| [模块实现](../../.scratch/architecture-formalization/reader-module.md) | MOD-01 无模板步骤先修订完整计划，再按新版步骤准入；MOD-02 区分只读资料未知与业务副作用，保留必要条件及原读取／费用收尾 | 两项 resolved；计划与只读定向静态向量通过，拒绝调用无法重分类原写操作 |
| [运行维护](../../.scratch/architecture-formalization/reader-operations.md) | O-R01 明确组件绑定切换与执行宿主退出的两类排空：后者封闭新实际启动，原效果／停止／费用继续核对 | resolved；部署、扩展及工程的引用规则一致 |

计划修订的旧 Operation 输出复用使用 operation_output；旧 Delegation 的准确 result_ref 经获准材料和新模板字面值复用，不冒充 operation_id，也不跨计划修订复用 step_output。只读例外仅基于原固定且可核验的精确 read_only 声明，不能按工具名称推断。两项均未增加公共字段或第二套准入来源。

## 3. 当前静态检查

以下结果来自当前正式资产。环境为 Python 3.12.14、Node 25.9.0、jsonschema 4.23.0；Protobuf 描述符与 Python 类型由 grpcio-tools 1.80.0／protobuf 6.33.6 生成。完整输出保存在[本轮检查记录](../../.scratch/architecture-formalization/static-checks.json)。

| 检查入口 | 已取得的结果 | 证明边界 |
| --- | --- | --- |
| [文档结构](validation/check_documents.py) | 65 份 Markdown、0 个链接／锚点／表格／围栏错误；112 个 Mermaid 块已识别 | 本地结构，不检查远端链接或证明语义 |
| [完成投影](validation/validate.py) | 5 正例、9 反例通过 | 抽取状态及完成关系，不是服务结果 |
| [领域协议](validation/validate_protocol.py) | 56 有效序列、371 定向变更反例；全部 105 方法覆盖；只读门禁、账务更正及集合边界的附加构造通过 | 给定身份／能力／事实前提下的结构与关联 |
| [回答字节](validation/validate_input_answers.py) | 9 份真实 JCS 字节、112 回答反例、3 发现投影、10 发现反例；9 个协议 answer_ref 及 1 个 input 等待关联核对 | 本地字节／摘要、准确字段及给定确认／命令关系 |
| [传输](validation/validate_transport.py) | 100 向量、228 定向反例；8 请求序列、12 反例 | 不执行真实网络、内容传输或存储 |
| [公开签名与 JCS](validation/verify_transport_proofs.mjs) | 2 ES256 向量、3 规范编码向量、23 负例 | 构造字节及固定密钥绑定，不证明实际身份或密钥保管 |
| [Brain 产出与计划](validation/validate_brain.py) | 生成／恢复、5 计划安装检查、11 生成反例、15 物化分支及 7 展开／修订依赖检查通过 | 已给定准确材料、操作输出及所选条件，不运行模型或工具 |
| [离线费用更正](validation/validate_lease.py) | 2 有效序列、11 反例通过 | 不证明账单真实性或耐久交付 |
| [批准与运行身份](validation/validate_release_recovery.py) | 13 定向场景通过 | 不证明本人呈现、隔离环境或持久恢复 |
| [gRPC 消息封装](validation/check_grpc_envelope.py) | 描述符实际编译；2 RPC、16 Call 请求、22 Call 答复、57 ChannelFrame JSON 往返通过 | 生成类型保留记录 JSON，不证明认证、入口拒绝或真实服务互操作 |

相较来源新增或修改的三个 Mermaid 图体已实际渲染并查看，文字及正常分支可读；109 个相同图体保留原材料。记录见[图形核对](../../.scratch/architecture-formalization/diagrams/manifest.json)。不能将图体相同或结构检查通过写成全部图形经过本轮重新渲染。

## 4. 复现检查

在仓库根目录运行；需可用的 Node。Python 依赖可放入临时环境，生成目录明确为本轮检查资产：

```sh
python3 -m venv /tmp/harness-architecture-checks
/tmp/harness-architecture-checks/bin/pip install -r docs/architecture/validation/requirements.txt grpcio-tools==1.80.0 protobuf==6.33.6
/tmp/harness-architecture-checks/bin/python docs/architecture/validation/check_documents.py
/tmp/harness-architecture-checks/bin/python docs/architecture/validation/validate.py
/tmp/harness-architecture-checks/bin/python docs/architecture/validation/validate_protocol.py
/tmp/harness-architecture-checks/bin/python docs/architecture/validation/validate_input_answers.py
/tmp/harness-architecture-checks/bin/python docs/architecture/validation/validate_transport.py
/tmp/harness-architecture-checks/bin/python docs/architecture/validation/validate_brain.py
/tmp/harness-architecture-checks/bin/python docs/architecture/validation/validate_lease.py
/tmp/harness-architecture-checks/bin/python docs/architecture/validation/validate_release_recovery.py
node docs/architecture/validation/verify_transport_proofs.mjs
mkdir -p .scratch/architecture-formalization/proto
/tmp/harness-architecture-checks/bin/python -m grpc_tools.protoc -I docs/architecture/contracts --descriptor_set_out=.scratch/architecture-formalization/proto/harness.pb --python_out=.scratch/architecture-formalization/proto docs/architecture/contracts/harness.proto
/tmp/harness-architecture-checks/bin/python docs/architecture/validation/check_grpc_envelope.py .scratch/architecture-formalization/proto
```

图形记录对应本轮实际源摘要，保留了渲染和人工查看结果；源码未变时无需重复生成。静态夹具中的认证、能力声明、候选、批准和权威记录始终是构造前提，检查不能提升其真实性。

## 5. 运行证据边界

本仓库尚无 Harness 运行内核、数据库适配器或生产平台部署。真实事务竞争、模型发送和费用、工具效果、权限隔离、恢复与质量／容量目标均需按 validation 的实验取得证据。归档形式化、静态正反例和文档审校不替代这些结果。

首个实施切片和退出条件见[工程阶段](engineering.md#phases)；运行故障点见[故障实验](validation/fault-experiments.md)，模块的 RT／BI／EX 等用例与系统 SYS／HAR、生产 PROD 分别保持待运行。公司平台适配、账本单区 RPO=0、控制／查询 RTO≤60 秒、文件根故障域、质量与最终规模均须另取真实证据。
