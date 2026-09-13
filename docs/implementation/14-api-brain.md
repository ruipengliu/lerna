# 14：模型提案、批次准入与 API 执行循环

本票把同一个 Task 的目录检索、模型提案、Core 准入、实际 API 效果与下一轮决策接通。一次 API 成功只产生执行事实；受信目标检查器依据独立业务状态决定任务完成。确定性夹具与真实模型回归使用不同入口，不能相互替代。

## 运行

```sh
go run ./cmd/contractcheck -profile api-brain-v1 > build/api-brain-report.json
go test ./tasks -run 'TestAction|TestBatchAdmission|TestUnknownAdmission|TestUnknownAction|TestWrongWrite'
go test ./brain ./adapters/arkmodel
make verify
```

离线 profile 不读取密钥、访问模型或使用外部数据库。十二项本地夹具覆盖单步、两轮多步、同批依赖、缺输入等待及回复继续、首次参数错误纠正、只读目录查询错误纠正、错误写入、无进展停止、提案/派发后重建 Brain 继续执行和恢复前来源撤权。只读纠错针对真实 Describe 查询缺失版本；不将它称为第三方只读业务 API 集成。

实际模型入口需要显式授权和总请求上限：

```sh
go build -mod=readonly -o build/apicheck ./cmd/apicheck
./build/apicheck -real -max-requests 12 -env-file .env > build/real-api-brain-report.json
```

命令将失败尝试计入上限，不自动切换模型或隐藏重试。只发送公开模拟材料。真实模型的原始输入、输出、Core 快照及 SQLite 数据保留在报告指向的私有 StateDirectory，包含本地身份凭据，不应提交。默认 `make verify` 不运行该入口。

## 模块与可信边界

- `brain.ActionBrain` 通过消费方定义的 ActionCore、ActionEnvironment 和 Model 接口工作，不依赖 SQLite、供应商客户端或具体业务驱动。宿主提供输入授权、目录、受控内容和独立效果判断。一个共享实例限制四个任务与四个实际模型请求；超时未返回的实现继续占用请求槽位。
- `tasks.ActionPort` 冻结预算、保存决策、原子准入有限批次、分配待执行工作并处理最终证据。普通答案 Proposal 保持原契约，行动任务不能借旧答案发布或普通 Worker 完成路径绕过目标检查。
- `execution` 保持签名授权、资源围栏、输入 Schema、稳定操作及 Outbox；新增可选强校验将实际描述符、受控输入和资源/控制版本绑定到 Core 已准入动作。已准入请求在真正启动时再次校验。
- `arkmodel` 通过显式 `harness_actions_v1` 合同生成结构化提案；空合同仍为原有 answer/sources 合同。模型只给局部 key、准确候选摘要、依赖、参数和预期资源版本，不获得操作身份、凭据或执行权。
- 参考组装复用 13 票 `profiles/catalogcheck` 的完整 1008 项目录、真实授权和六个业务执行器。Search 的输入为用户目标与任务声明的资源命名空间，不传预期能力 ID；Describe 加载六个候选的准确版本 Schema。模型选择动作与次序。

ActionEnvironment 是受信宿主端口：Prepare 必须验证准确 Schema、保存受控输入并申请操作身份；Assess 必须查验真实业务效果，不能把模型的自报成功当事实。这里的参考目标是采购订单授权，判断依据是目标状态 authorized 和账本 997；通用内核不内置该业务规则。

## 准入、预算与恢复

每次决策先持久化请求额度，再进行有界检索和生成。检索、描述、目标观察、资格检查与恢复查询在 I/O 前消耗辅助查询额度。模型内部若引入重排或辅助模型，必须通过同一可计费 Model 边界，不能放入不计费 Ranking 或 Environment 实现。

模型原始输入/输出保存在受控内容中；Core 保存引用、用量、首个错误、标准化提案和准入状态。同一 decision number 的 Record 重放必须完全一致。首次参数错误、Schema 错误、候选缺失或没有形成提案均保留；后续纠错另建决策，不能覆盖首个记录。正常两步推进不增加纠错计数。

Core 在同一个 RuntimeStore 事务中验证任务版本、owner/epoch、Worker、控制意图、租约与截止时间，验证最多八个动作、唯一局部键与操作身份、依赖图无环和剩余操作额度，然后保存完整操作映射。任何失败都不准入部分动作。宿主申请但尚未准入的身份及内容引用不产生外部效果。

参考调度串行执行依赖满足的动作。失败依赖的下游标记跳过，独立分支仍可推进。每个动作在第一次派发时固定 Qualification，恢复保持原身份与原输入；执行结果只更新该动作并保持 Task 运行。外部效果不是原子批次：前一个成功、后一个失败会保留各自事实。

提交返回不明时，使用原决策编号和原 Qualification 调用 Admit 核对/重放；不重新生成操作映射。若已恢复新租约，未准入提案使用新 Worker 资格准入，原提案资格和操作映射仍保留。已派发动作先查询原 Invocation；派发落盘后、Invoke 前中断时，重放同一不可变请求，经全部当前准入门槛继续。Invoke 幂等，Started 操作只能核对，不再次 Start。未知效果阻止新决策和替代身份，已知事实通过原 Outbox 消费后才能继续。仅有未完成的模型请求预留而没有响应记录时明确等待 model_unknown；未知 token 用量继续占用完整上界。

租约续期仅修改元数据，不改变决策版本。过期后 EnsureLease 增加 Worker generation，旧绑定继续被围栏；恢复宿主用独立的当前资格绑定接收原 Invocation，请求身份与指纹不改写。两项端到端恢复夹具将时钟推进两分钟，超过 Worker 租约和身份签发窗口，再重建 Brain 完成原任务；身份在 Prepare 时已预留到执行域，不能因签发窗口结束而改号。三个另行的子进程退出测试验证 Core 的 SQLite/WAL 持久化。这两类证据分别报告，不把内存中断称为完整子进程执行恢复。

缺输入提案将具体问题保存为受控引用，原子建立标准 Pending Interaction 并释放决策占用。用户通过原有 ProvideInput 回复后重新获取租约；下一次模型输入包含绑定到该交互的新回复。改变目标会使旧未派发动作失效，原外部事实仍保留；本端口不迁移 owner。现有 01–13 的任务与执行协议继续使用原路径。

| 边界 | 单步 | 多步 |
| --- | --- | --- |
| 模型请求 / tokens | 4 / 65536 | 12 / 262144 |
| 新操作 / 辅助查询 | 3 / 32 | 12 / 128 |
| 总时间 | 120 秒 | 600 秒 |
| 纠错 / 单批动作 | 2 / 8 | 2 / 8 |

行动输入 JSON 最多 32768 字节、响应最多 8192 字节，输出最多 1024 token。任务状态存储继续使用原 gob Format 1，新增可选 ActionState，既有任务缺少该字段时走原逻辑；整体保留原 8 MiB 上限。Execution Format 2 未改变。

## 数据与模型限制

输入和目录拥有独立来源策略，分别检查真实模型位置的 process/disclose；主体授权也必须允许该位置。执行授权不隐含数据上云授权。参考夹具明确把公开业务清单与公开输入配置为 local/ark-cn-beijing 可处理；这不是生产隐私数据的默认放行策略。模型调用前后复核，撤权后的提案不能新获执行权限。

Ark 适配器沿用 `doubao-seed-2-0-lite-260428`、北京端点、关闭 thinking、显式 JSON Schema 与供应商输出上限。模型与协议资料入口为[官方模型列表](https://www.volcengine.com/docs/82379/1330310)；此次查询该页面只返回目录壳，不能据此声称验证了新的 tokenizer 或更小的硬输入界限。

因此输入继续按既有 224K token 最大值保守预留，含输出每请求预留 230400 tokens。已知用量结算后释放差额；未知用量不释放。该绑定无法进入单步 65536 token 包络，多步可在真实用量结算后继续。没有把字节估算当硬 token 上限，也没有提高验收门槛。单步本地结构验收使用有独立固定用量的脚本模型，不能证明 Ark 单步验收通过。

## 验证状态

实现提交 `5e15924`，审查修复提交 `167051f`，真实回归修复 `ddc6903`。最终 `make verify` 退出 0，报告版本为 `ddc6903` 对应完整提交，dirty=false。环境为 linux/amd64、Go 1.26.1、protoc 36.1、Protobuf 生成器 v1.36.11；依赖版本与配置见具名报告。

本票 12/12 项离线 profile 通过；01–13 共 1395 项必需回归通过，合计 1407 项。完整 `go test -mod=readonly -p 1 -race ./...`、依赖验证、生成一致性、全包编译、go vet 及 SDK 样例通过。定向及包测试另外覆盖整批拒绝、控制/Worker 围栏、未知预算、三个真实 SQLite 子进程退出点、两分钟租约恢复、预留身份跨窗口及许可分配后 Invocation 前中断；它们不重复计入 profile 项数。

Standards 初审两项 P3 建议、Spec 初审三项 P1 和一项 P2 均已修复，并由原独立代理复核关闭。许可恢复继续检查原授权当前有效性，不能凭身份预留获得执行权。

证据：[12 项本地报告](evidence/14-api-brain-report.json)、[验证阶段](evidence/14-verification-stages.json)、[完整日志](evidence/14-verification.log)、[恢复定向测试](evidence/14-recovery-targeted.log)、[审查修复定向测试](evidence/14-review-targeted.log)、[双轴审查](14-api-brain-review.md)、[真实模型状态](evidence/14-real-model-status.json)。同目录 `14-*-regression-report.json` 保存 01–13 本轮证据。

## 真实模型回归（2026-09-11）

用户授权本轮合计最多 12 次请求、每次最多 1024 输出 token，仅公开模拟材料。实际运行两次任务，合计 2 次模型调用，输入 4982、输出 246，共 5228 token；完成后停止调用。

初次任务失败：模型等待 record 输入，未产生外部动作，账本仍为 1000。原始输入显示宿主没有交付已提交的 InputRefs 正文。新增模型接口测试先失败，修复后通过：Assemble 按真实模型位置授权读取并交付正文，仍保留回复关联。另修正完整参数场景下不必要等待的首次正确标记。原始失败报告保留不变，其中旧 FirstCorrect=true 是已确认的报告错误，汇总中明确更正为 false；不将开发修复后的新任务称为原任务纠错成功。

`ddc6903` 的新任务通过：完整 1008 项目录、六个准确候选 Schema；模型一次提出 submit→authorize 的两个依赖动作，Core 准入后执行器依据前一步效果推进，独立目标状态为 authorized、账本 997。请求用量 2541 输入 + 198 输出，纠错 0 轮。此结果证明一次真实模型提案可驱动多动作任务，不代表已测真实模型多轮重新规划或完整千级质量。

修复后重新运行完整 make verify，1407 项通过；新增两项定向测试、既有云位置授权与缺参数回复测试通过；独立双轴增量复审各 0 项。代码之外仅补充报告和文档，无需再次调用模型。

[失败原始报告](evidence/14-real-model-initial-report.json)、[修复后报告](evidence/14-real-model-report.json)、[公开决策输入与输出](evidence/14-real-model-public-decisions.json)、[预算与结论汇总](evidence/14-real-model-status.json)、[修复前测试](evidence/14-input-red.log)、[修复后测试](evidence/14-input-green.log)。首次 go run 没有内嵌 VCS 版本，运行前工作区干净且 HEAD 为 2ea05f3；第二次使用 go build，报告内嵌 ddc6903、dirty=false。

本票限定范围验收完成；Ark 单步包络仍不支持，其本地结构验收与真实多动作回归分别报告。千级完整 90% 首次正确率 / 95% 有限纠错成功率不属于本次小回归结论。
