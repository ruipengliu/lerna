> 历史结果：下面保留正式22合流时的报告范围与数值。最终23指标联合 Schema 已重采与实际测量，独立完整性／来源核验通过，最终完整验收尚未完成；不得把这些旧数值作为最终证据。原字节报告保存在 `history/formal22-protobuf-h1.md`。

# Protobuf 实对象验证（H1）

| 日期 | 修订说明 |
| --- | --- |
| 2026-10-05 | 建立 Go 实对象采集、绑定清单、体积与运行开销测量；记录主线局部样本，不宣称最终 M1 或端云验证通过。 |
| 2026-10-06 | 合入安全重发与运行记录正式契约，补采原发送历史版本、六个负责模块的实际来源记录及交接缺口；性能数据仍为旧样本基线。 |
| 2026-10-06 | 合入正式 FILE 契约，补采受管理根、文件资源、真实创建／替换／读取与原发布历史；性能尚未重测。 |
| 2026-10-06 | 合入正式取消契约，新增独立真实取消采集切片；固定 FILE 样本、覆盖清单及旧测量未更新。 |
| 2026-10-06 | 合入正式非成功关闭契约，增加关闭与拒绝继续的独立采集要求；固定语料和测量仍待统一更新。 |
| 2026-10-06 | 合入正式有限推理契约，补充四类治理提议、条件确认与宿主推进的真实公共生产要求；只做独立命名采集，不更新固定语料或测量。 |
| 2026-10-06 | 正式 API 联合语料统一采集 608 个对象；补齐公开 PendingGoal，完成三轮编解码、语义入口、链接增量与外部 RSS 测量；最终指标契约仍需重采重测。 |
| 2026-10-06 | 完整检查实际通过并独立复核全部 747 个受测文件；关闭当前正式 192 种消息 Schema 的 M1 Go H1 验证，保留最终 23 指标的重新采集与测量要求。 |

- 状态：正式 18 联合 Schema 的 M1 Go H1 验证通过，完整检查与受测版本已独立复核；最终 23 新增指标契约仍须重采重测
- 上游：[核心契约 7.5 与 H1](../../architecture/core/contracts/README.md#75-schema-技术首选-protobuf待实际验证)、[ADR 0004](../../adr/0004-language-and-stack.md)

## 最终23当前采集阶段（完整验收待完成）

正式b7与23公开接口联合后，`TestCaptureProtobufMainline` 真实采集630个对象；全部原608及更早229个场景名称和类型保留。新增22个样本从真实 `Harness.QueryMetrics` 及其嵌套对象提取，十一种指标均出现。实际生成绑定203种、递归出现199种、1367个非默认／presence字段；四种未采样类型仍为AncestorControl、MemoryDependency、ObservationHandoff、SendConsumption，分别属于M1不开放或内部whole-object未提供公开查询。类型出现不表示所有optional字段和分支完成；可选诊断只采集真实DISABLED，dropped与staleness缺失，核对duration仍MISSING。

原公开指标人口独立POST1／GET1／effect1；UNKNOWN→READY→PAUSED→COMPLETED由原owner流程产生，查询前后原Operation、Budget、来源和目标计数不变。真实关闭存储后仍返回可用进程指标及两个不可用owner；重启不重构旧准入单调时间、不新增发送。新采集及630份wire／ProtoJSON往返实际通过，详细来源在[采集记录](capture-provenance.json)。这是采集／codec资格；fresh测量已实际完成并通过独立完整性／来源核验，whole最终验收仍PENDING，不引用下面历史22成本数值作为当前结果。

旧联合fixture在第一次公共Configure使用未实现的reference-v1，恢复正确拒绝；原失败和cleanup次生panic均保留。仅新producer首次配置改用已有保守simulator-opaque，原MODEL1／bill1／fee7、target0、固定CANCELLED及重启零新I/O断言保留。没有升级保存历史或放宽恢复预检。另一个编译签名错误和一次拼错选择器导致无测试运行，分别归为fixture／工具调用错误，未计产品RED或GREEN。

## 1 结论与范围

当前样本证明，已运行的 Go 主线对象可以用生成绑定完成二进制往返，保留精确引用、可选费用的缺失与零值差异、64 位整数和二进制未知字段。未知安全字段仍由语义入口拒绝，结构解码成功不产生业务执行资格。本轮真实对象与成本测量支持 M1 Go 阶段继续采用 7.5 的 Protobuf 首选；没有据此更换选型或修订 ADR 0004 的依据。

这不是端云 H1 的最终通过结论。正式 18 联合语料已采集成功的模拟 API 主线、64 KiB 输入、两类确认与消费、撤回后拒绝、授权撤销和未发送预留释放、计费冲突、未知效果下的迟到账单、会话回答／修改、规划命令与完成封闭。非成功关闭、拒绝继续、默认推理和原生 API 适配器的对象已统一重采；可观测性指标的最终对象仍待正式 23 集成并再次测量。内容登记、正文回执、版本、派生与接替对象已经随当前内容治理实现补采；模型调用也已采集请求、上下文快照、调用、输出、用量、提议结果及停止后迟到结果。核对场景已采集确认型查询、独立关闭准入、原写与查询责任、结论及进度交接，也覆盖弱证据与检查上限暂停。安全重发场景已采集原发送、独立计费来源和不可变历史状态；运行记录场景已采集六个负责模块的真实来源事件、接纳、源端回执、缺口、索引与固定截点查询。文件场景已采集受管理根及绑定命令、资源登记及资源记录、创建／替换／读取的真实原生 I/O 请求和返回、文件参数／提交／证据、独立账单和原发布历史。绑定清单逐次生成，未采样类型如实列在 `unsampled_types`，不以空对象凑足覆盖率。`populated_fields` 只代表当前样本中出现的字段，不代表全部字段、枚举或状态组合已经验证。

Go 是 M1 当前唯一绑定目标。TypeScript、手机端、混合版本及受限设备的包体积、峰值内存和消息上限仍待 M3／H3／H6 的独立验证。当前测量没有容量门槛，不能把某个耗时或 RSS 数字解释为端侧准入资格。完整 M1 样本补齐并重新测量后，再决定本阶段是否保留该选型。

## 2 样本怎样产生

[`TestCaptureProtobufMainline`](../../../conformance/admission/protobuf_capture_test.go) 使用生产共用的进程内装配，只调用公共命令和查询。流程是目标、受信条件、提议、授权、预算、准入、实际模拟发送、可信观察、用量、轨迹、核验和成功 Result；同时断言模拟目标只收到一次调用且只产生一次效果。授权与预算分支核验消费／释放后的持久状态和目标调用次数；原发送命令的采样重放也必须拿回旧决定而不产生新发送。大输入也经 `SubmitGoal` 和持久回执查询被接纳。

实际 I/O 请求和响应由委托包装器采集：与 `assembly.Open` 相同的真实模块、文件锁和 `egressio.HTTP` performer，只在边界复制实际传入／返回的对象，不模拟响应、不拼装样本。该场景同样检查持久效果及模拟目标的一次调用、一次效果。

FILE 边界包装器同样委托正式 `Files.PerformChecked`，保持原生执行和最终资格复查。单个真实 fixture 执行创建 A、按 A 版本替换为 B、读取 B；核验当前指针与实际对象字节、两份版本对象、三次原生调用、每步账单结清，以及原命令重放不再次进入原生执行。公共历史查询仍返回 A 的原证据；跨用户根与资源查询被拒绝。根绑定与资源命令由实际调用边界复制，根／资源／观察由公共查询取得；没有直读业务数据库。此处只补采普通真实路径，不重跑 ADR 0007 的原生故障或联合存储矩阵。

取消样本采集使用正式 [ADR 0009](../../adr/0009-cancellation-closure.md) 的公开流程：先准入但暂不交付原动作，再实际取消并查询原范围、封闭命令、意图、回执和证明。CLI 视图在原动作尚未到达时必须保留等待原准入的限定事实，不能填入虚构的 Operation；原交接到达后才采集同一身份的 `SEALED/NOT_APPLIED` 动作。采集同时检查重放及恢复不改变原命令、回执与引用，实际目标调用、效果和账单均为零。独立窄测试直接调用同一个采集函数，检查五种新增类型的真实字段和二进制／ProtoJSON 往返；完整主线采集也调用该函数。

先前独立切片保留的 229 对象 FILE 基线及原八份数据，现按原字节归档在 `protobuf-h1-data/history/formal17-file-baseline/`。本轮完整主线重新实际执行全部采集器，产生当前固定语料；历史文件不用于当前覆盖或性能结论。

`TestCaptureCancellationPublicObjectsRoundTrip` 已从该真实流程取得 15 个对象并通过 race 检查；先前缺少五种类型的采集断言已在实现采集器前失败。该独立结果与本轮统一重采日志分别保留。

非成功关闭采集沿用 [ADR 0010](../../adr/0010-nonsuccess-task-closing.md) 的实际公开流程，分别保存 `FAILED` 与 `CANCELLED` 的真实开始命令、关闭范围、源意图及端点命令、准确回执与证明、原执行及结算后续记录，以及带原负责方工作版本的关闭视图。原发送的真实回报和账单晚到时，当前效果与费用更新，固定 Result 的字节和引用必须保持。前 P2 的独立流程采集等待原准入的视图与 NoSend tombstone，原交接到达前不得填入 Operation 或 UNKNOWN。目标调用、效果和费用分别实测。

拒绝后的继续请求必须由实际原动作的可靠否定证据触发。采集 `REJECTED` Verification 的非空 `continuation_request_ref`、对应的 ProposalRequest、ContextSnapshot 和唯一 PROPOSE 工作；重复处理与恢复不得创建新请求。成功样本中该字段缺失不构成此分支的覆盖。新增采集器先通过直接命名往返测试，再由完整主线入口调用；独立切片的对象数不相加推导总量，当前总量以实际完整主线保存的文件为准。

`TestCaptureTaskClosingPublicObjectsRoundTrip` 已直接采得 64 个真实对象并通过 race 往返检查，覆盖八种关闭类型、两种关闭结果和前 P2 限定视图。两个实际已发送分支各收到一次调用、产生一次效果和一笔收费；晚交接原观察与账单后，当前结算 120、欠额 40，Result 保持原预留 30。前 P2 分支实际调用／效果／收费均为零。`TestCaptureRejectedContinuationPublicObjectsRoundTrip` 已采得 13 个真实对象并通过 race 往返，可靠否定的一次调用产生零效果及一笔收费 25，重复处理和恢复只保留一个 PROPOSE 工作且没有 Result。两个采集器各自的缺样本断言均先失败，再实现真实生产流程。此处对象数仅描述独立用例，不是主线固定文件的新总量。

固定文件 [`mainline.json`](../../../conformance/protobuf/testdata/mainline.json) 是这些真实响应和已被受理命令的 ProtoJSON，含运行生成的 UUID、时间和本机模拟目标地址；内容全部为合成数据。重放测量不会访问样本中的地址，不需要 SQLite、模型供应商或外网。重新采集会生成新的标识和时间，不要求采集文件逐字节相同；比较两次测量应使用同一个固定文件，或同时保存文件 SHA-256。

正式16新增的13种消息及七种已有消息中的21个字段，必须从实际公共生产流程采集。默认实现的描述来自 `DescribeReasoner`；模型设置来自原 `QueryModelCall` 的规范设置；四类提议来自原 `RunReasoner` 和 `ReadProposal`，不得把供应商 JSON 手工拼回 `QueryProposal` 或回报中的结构投影。实际 `Content.Read` 的派生正文与参数、原请求快照和进展引用必须独立保存并核对。问题及条件变更仍由真实发布和用户输入推进；主观条件确认须保留原 PENDING、APPROVED、CONSUMED 版本及消费核验引用，不能直接构造批准或消费记录。

有限宿主采集使用公共配置、有限推进、当前与历史 driver 查询，保留原 Request、Outcome、Admission、Question 和权限配置引用；真实命令与返回对象分别采集。完整正文、结构投影和来源必须保持同一原身份，读取不可用不得从投影恢复正文。每个独立采集器先取得缺样本或缺字段的真实失败，再验证二进制、ProtoJSON 和临时样本文件往返；该阶段未更新原 229 固定样本；本轮正式 18 联合采集已经执行这些相同生产器，历史覆盖与测量另行归档。

独立流程分别覆盖实际 QUESTION 的有限推进、停用和重启，以及原 ACTION 的真实批准、准入消费、发送前取消、非成功封闭和固定 CANCELLED Result；driver 进入 COMPLETED 后仍保留原调用和准入。操作准入确认的真实事项摘要补充 `SnapshotConfirmation.proposal_ref`，不能与主观条件事项拼成一条记录。拒绝完成样本使用原目标的可靠否定证据，保存真实 UNSATISFIED、REJECTED 及其唯一续行请求／快照／工作。原模型回报的提议结构载荷不含已分配的 Proposal Ref，该引用独立保存在 `ProposalOutcome.proposal_ref`；主观确认消费绑定开始核验的原修订，最终 Result 固定同一核验的后续修订，二者须分别查询保存。可选 `ReasonerDriverPolicy.model_confirmation_ref` 尚未实际行使，不制造批准引用填满字段。各切片的真实次数与结果随原始命名日志保存；独立切片不是统一语料总数或端云 H1 完整资格。

正式 API 采集从 [ADR 0008](../../adr/0008-fixed-api-identity-and-platform-credentials.md) 的真实受信网络边界及公共查询取得三类 API 消息。隔离 Keychain 只保存合成凭据；观察包装器委托正式 APIHTTP 的凭据预检和原生请求，不替换发送、回报或门禁。RATE、CONCURRENCY、RESOURCE_CONFLICT 与未识别类别各由目标真实返回 429，保存原观察、等待、发送、动作和来源；等待不构成未生效证据。另一真实目标回显合成秘密，采集的受信回报与持久观察必须只留下遮蔽标记，不含秘密或平台仓库路径。描述中的原账户、origin、资源、凭据引用和参数摘要须与原准入及发送逐项一致。

默认 MODEL 与 API 的联合样本由实际 CLI 和公共 driver 推进取得，保留真实提供方收到的快照、原模型请求／结果／治理正文，以及原生 API 准入、执行、账单、完成核验与 Result 的关联。不得把供应商 JSON 手工变成核心提议或虚构引用。新增采集器先经过命名的二进制／ProtoJSON／临时文件往返，再由完整主线统一重采；旧八份固定数据先按原字节保留为历史，当前覆盖和测量只能引用实际新生成的语料与 Schema 摘要。最终可观测性契约合入后仍须再次真实采集和测量，不将本阶段数据标为最终 M1 Schema 的证据。

`PendingGoal` 必须从实际 `SubmitGoal` 产生的原 `Job.Goal` 采集：公共 `QueryJob` 和 `ExecuteJob` 的领取回执都可返回此载荷。它不是不可访问的内部记录。采集应保留处理前工作、领取命令／回执、正文引用、处理后的原决定与重放，检查工作载荷不复制目标正文、凭据或追踪文本；不能直接构造 PendingGoal。

当前尚未采样的已有类型如下。它们不从分母中隐藏；完整 M1 审核还须检查后续新增类型。

| 类型 | 当前原因与后续处理 |
| --- | --- |
| `AncestorControl`、`MemoryDependency` | M1 不支持子任务祖先链与记忆依赖；属于显式不支持范围，不构造伪造活跃对象 |
| `ObservationHandoff`、`SendConsumption` | 当前是模块内部保存用的 Proto 记录，没有公共查询返回整个对象；本次公共切面不直读数据库。相应观察交接回执、原命令、任务输入与预算发送计数由公共流程核验，但不计作这两种消息的采样覆盖 |

`ObservationHandoff` 在 `core/content/observations.go` 的私有 `observationStore` 中创建与保存，公开 `Content.QueryObservation` 仅返回其中的 `RawObservation`；`ProcessObservations` 从原命令恢复交接并保存接收回执。`SendConsumption` 在 `core/budget/sends.go` 的私有 `sendStore` 中创建与检查，公开预算切面返回 Reservation 和 BillingSource，并不返回消费记录。这两种活跃内部存储消息已经在真实发送、观察交接、费用占用及恢复流程中经 SQLite 保存和读取；`infra/sqlite/observations.go` 与 `infra/sqlite/start.go` 使用真实 Protobuf 存取路径。这是运行路径证据，不是它们的独立公共对象编解码与尺寸样本；因此仍列在未采样清单，没有直读存储或手工拼装它们。两种绑定均由当前 Inspect 反射确认存在。

扩展样本应继续从公共接口取得对象，并保存对象来源和测试判据。新增契约会自动进入绑定清单；最终审核必须逐项处理未采样对象：补充真实场景，或说明它不属于 M1 活跃公共对象的理由。

## 3 复现

在仓库根目录执行：

```sh
# 仅在显式更新样本时运行；普通测试不写文件。
LERNA_PROTOBUF_CAPTURE="$PWD/conformance/protobuf/testdata/mainline.json" \
  go test ./conformance/admission -run '^TestCaptureProtobufMainline$' -count=1 -v

go test -race ./conformance/protobuf
scripts/measure-protobuf.sh /Volumes/Data/proj/lerna-m1-context/ticket23-final-measurement
```

测量脚本先编译后计时，保存环境、提交、工作区状态、Go 与 Protobuf 版本、样本摘要、Schema 摘要，以及以下原始文件：

| 文件 | 方法与解释 |
| --- | --- |
| `inventory.json` | 遍历当前 `lerna.v1` 描述符，逐个核验生成 Go 类型；递归记录样本类型与字段；每个顶层对象分别量二进制与 ProtoJSON 字节数 |
| `sizes.txt` | 源文件总字节数；同一工具链、`-trimpath -ldflags='-s -w'` 下最小 Go 程序及链接生成绑定／Protobuf 的程序大小。两者差值是该构建的可执行文件增量，包含运行库和描述符，不等于通用的“Protobuf 库包大小” |
| `bench.txt` | 每个固定样本的二进制／JSON 编解码，`100ms`、重复 3 次；每次解码新建对象，不复用或池化；报告 `ns/op`、`B/op`、`allocs/op` |
| `peak-memory.txt` | Darwin 的 `/usr/bin/time -l` 或 Linux 的 `-v` 记录整个已编译测量进程最大 RSS。包含 Go 运行库、测试框架、样本和 GC；不包含编译器与 SQLite。Darwin RSS 单位是字节，Linux 是 KiB；不将累计分配量冒充峰值 |
| `binaries-sha256.txt`、`source-sha256.txt`、`corpus-sha256.txt`、`artifacts-sha256.txt` | 记录实际构建输入、两份最小程序源码及三个已编译程序、固定语料与原始测量文件的 SHA-256；可设置 `LERNA_PROTOBUF_BINARY_ARCHIVE` 保留二进制供独立核对 |

Protobuf Go 1.36.12 的 JSON 编码器会按程序二进制确定的种子，在结构逗号后增加可选空格；同一二进制内稳定，跨构建不保证稳定。本轮 race 覆盖进程与测量进程的两份清单，在 Schema、类型、字段、样本名及二进制尺寸上完全相同；608 个 JSON 尺寸的差值逐一等于相应载荷的结构逗号数。两份原文件均保留，下面 JSON 尺寸只采用同一归档测量程序产生的 `inventory.json`，不把 ProtoJSON 当作规范化字节表示。

语义耗时只测实际生产函数 `command.ValidateGoal` 与 `command.ValidateHeader`，分别标为 `semantic-goal`、`semantic-header`。它们验证输入、契约版本、必理解特性和嵌套未知字段；不包含权限持久读取、状态机、证据核验、跨记录事务或完整门禁。没有通用的“所有对象语义验证器”，报告不得把这些数值外推为完整业务校验成本。相关业务规则继续由主线与故障一致性测试验证。

测量在共享开发机上执行，其他工作可能影响时延；原始三次结果保留，不使用单次最好值作性能承诺。后续容量验收需在目标机器、固定负载与消息规模下重新测量。

普通完整 race 检查的 admission 包在正式 18 已用时 537.443 秒；本票新增完整实际采集单次已用时 41.42 秒，此外还有独立生产器往返用例。为容纳增长后的完整用例，普通 `make test` 使用有限的 20 分钟超时；这只调整测试运行预算，不改变生产规则、用例选择或断言。原故障套件的 120 分钟超时和全部矩阵保持。

## 4 正式 18 联合覆盖与当前测量

当前父提交为正式 18 的 `0d42b1c3d3e3bf8a50cab058b66b3c132e9017c9`，带本验证工具修改。实际统一采集取得 **608 个对象，递归覆盖 188/192 种消息**；Schema SHA-256 为 `9de27d45cd64139a16d3c1cb10b9f52e942296696447fda1ac89b4d0fe0ac98d`。旧 229 个场景名全部保留，没有重名；未采样四种类型与上表相同。`populated_fields` 包含 1303 个实际出现字段；可选 `ReasonerDriverPolicy.model_confirmation_ref` 仍未实际行使，不计作已验证。

统一采集的 race 检查通过，固定语料的二进制／ProtoJSON 往返与语义边界测试通过。正式 13、14、16 的独立生产器全部在同一次采集中实际执行。原生 API 的四类等待和脱敏真实产生 82 个公开对象；默认 CLI MODEL→API 流程真实产生两次 MODEL、一次 API 请求、一个效果、三笔账单，结算 21、预留 0，最终 SUCCEEDED。这些次数分别属于对应场景，不相加冒充系统吞吐量。

本轮测量于 2026-10-06 11:08:15–11:23:43 UTC 实际完成。平台为 Apple M4、Darwin arm64、Go 1.27.1、Protobuf Go 1.36.12。固定语料 SHA-256 为 `ec5f39d50051f019a0dc37efa5341230ace9c4dd3db49176d067218a17e2feb7`；`measured-corpus.json` 是其相同字节副本。

- 608 个样本的四项编解码和 105 个实际语义入口，共 2537 个测量项，各运行三轮 `100ms`，保留 7611 行完整结果。
- 生成 Go 源码 821,476 字节，Proto 源码 54,037 字节；相同构建选项下最小程序 1,571,186 字节，带绑定程序 4,210,930 字节，增量 2,639,744 字节。
- 已编译测量进程最大 RSS 为 43,958,272 字节，约 41.92 MiB；这是测量进程峰值，不是业务宿主或端侧资格。
- 原始数据、测量程序、语料、源文件和八份历史数据的摘要已逐项独立核对；测量期间源码和语料未改变。

以下为实际三轮时延中位数，单位 ns/op。全部样本的分配字节数、分配次数和每轮数值保留在 `bench.txt`。

| Sample | Binary bytes | JSON bytes | Binary encode | Binary decode | JSON encode | JSON decode |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| `start-command` | 1551 | 3417 | 1766.0 | 2733.0 | 13365.0 | 21821.0 |
| `result` | 1357 | 3051 | 1758.0 | 2893.0 | 13131.0 | 21793.0 |
| `large-goal-command` | 65599 | 65721 | 6022.0 | 5446.0 | 52107.0 | 51601.0 |
| `file-io-result-REPLACE` | 1896 | 3319 | 1546.0 | 2490.0 | 11729.0 | 18063.0 |
| `closing-failed-result-late` | 1189 | 2732 | 1632.0 | 2573.0 | 12142.0 | 19816.0 |
| `api-native-RATE-wait` | 201 | 470 | 273.0 | 441.4 | 2033.0 | 3284.0 |
| `api-native-REDACTED-observation` | 616 | 1328 | 730.2 | 1185.0 | 4925.0 | 8590.0 |
| `default-model-api-result` | 1705 | 3750 | 2200.0 | 3759.0 | 17185.0 | 27968.0 |
| `default-model-api-request-1-model-call` | 1624 | 3504 | 2064.0 | 3326.0 | 14575.0 | 24100.0 |
| `pending-goal-payload` | 148 | 375 | 257.0 | 425.5 | 1787.0 | 2801.0 |

生产语义入口中位数为：`start-command/semantic-header` 2992 ns/op、`begin-completion-command/semantic-header` 487 ns/op、`goal-command/semantic-goal` 7.676 ns/op、`large-goal-command/semantic-goal` 7.682 ns/op。语义入口范围仍以第 3 节限定为准，不能外推为完整门禁成本。

历史 100 对象性能数据和 229 对象 FILE 覆盖清单保留在历史目录；它们不代表当前 608 对象成本。正式 23 的新增指标契约必须在合入后重新执行实际采集、覆盖清单、三轮测量、外部 RSS 及最终完整检查，不能用本轮绿色结果代替最终 Schema 的资格。

## 5 当前 Schema 的完整验收证据

本轮验收只针对正式 18 父提交 `0d42b1c3d3e3bf8a50cab058b66b3c132e9017c9` 与本票采集、测量工具组成的当前 192 种消息 Schema。2026-10-06 11:27:04–13:06:47 UTC，在上述 Darwin arm64 环境实际运行 `GOFLAGS=-v BUF_BASE=0d42b1c3d3e3bf8a50cab058b66b3c132e9017c9 make check`，5983.141 秒后退出码为 0。20 个有测试结果的包全部重新执行，没有缓存结果；普通 admission 包为 614.143 秒，完整 fault 包为 5360.026 秒。普通 20 分钟和故障 120 分钟预算均未改变用例、断言或故障矩阵。

完整日志有 475 个唯一顶层测试，其中 439 个通过、36 个按入口约定跳过；后者是 35 个仅由父测试启动的子进程入口，以及普通检查不应写测量文件的 `TestWriteProtobufMeasurementInventory`。另有 16,721 条子测试通过记录，合计 17,160 条实际通过记录。故障包的 128 个顶层入口包括 93 个通过及上述 35 个子进程入口。首次所有者统计误把父测试日志中嵌入的三条 `TestReasonerDriverPlanningBoundaryChild` 标为额外顶层通过；独立核对后，按未缩进的顶层结果修正，原错误统计与修正说明均保存，原始日志没有修改。

API 崩溃恢复、429 到期唤醒和参数来源三组实际矩阵分别完整通过 42、144、15 个叶子场景。真实原生 API、纯 API 与共用生产 driver 的完成及 FAILED/CANCELLED 收尾、原生文件与组合存储、存储断电、初始化、负向控制、同步与目录同步错误场景均包含在原完整检查中；14 个当前采集测试入口全部通过。存储证据是实际捕获写入的有限恢复映像与故障注入，不等同于物理设备断电或所有平台资格。

全量检查前后的 747 个文件（包括 460 个源码／构建输入）逐字节、大小和模式相同。验收文字更新前，完整树已保存为 `5400ed9c4f2d9f6ca978fc4b727b1674d29cb8e3`，保护提交为 `f90a923141c5383c5460136f05875749ce4013c1`，标签为 `codex/ticket22-tested-20261006`；保护操作不移动正式父提交。独立复核逐项验证全部 Git blob、可执行位及路径集合与冻结清单一致。后续本票提交只在该受测版本上更新本报告和 issue 22 的验收文字。

原始验收证据保存在外部目录 `/Volumes/Data/proj/lerna-m1-context`：`ticket22-formal18-whole-final.log`、同名前缀的 `.json` 与 `-freeze.json` 记录原命令、环境、时间、退出码和完整文件清单；`ticket22-tested-tree-protection.json` 记录受测 Git 树；`ticket22-root-full-result-summary.json` 记录独立核对。原始日志 SHA-256 为 `1eb34453684c07b4ecfee4acd27a6a4d05f0a5a61c3a63d82bd3b512419aaec0`。首次计数与修正另保存在 `ticket22-formal18-whole-final-log-audit-first-count-assumption.json`、`audit-ticket22-final-log-first-count-assumption.py` 和 `ticket22-owner-audit-count-correction.json`；修正后的所有者核对为 `ticket22-formal18-whole-final-log-audit.json`。

结合第 4 节的真实语料、绑定覆盖、完整三轮成本数据与本轮完整检查，当前正式 192 种消息 Schema 的 M1 Go H1 验证通过，继续采用 Protobuf，issue 22 关闭以解除 issue 23 的依赖。这不覆盖最终 23 尚未合入的 11 种指标消息；该 Schema 合入后必须再次真实采集、覆盖核对、三轮测量、外部 RSS 和最终完整检查，再补记新的 H1 验收结果。上述四种未采样类型、可选模型确认分支及跨平台验证范围仍按前文保留，不宣称最终 M1、所有公开对象或端云验证完成。

## 最终23实际测量（最终完整验收待完成）

2026-10-06 15:02:00.757989–15:18:04.023004 UTC，本次实际测量退出0，用时963.254841秒。固定630份公开对象，Schema摘要 `e94238af6ecb83742b2416de23e31eb54b8aaa6f803e84a07f57d730b000b011`，语料摘要 `df1ec71fb0b822a8f3a1caf9fd959151371d811f0f51270907b65204cd77bb4c`。同一归档测量程序执行命名inventory并PASS，然后执行四项codec及3项goal／102项header语义入口，共2625项各三轮、7875行。生成Go866560字节／Proto57193字节，最小程序1571186字节／绑定程序4262322字节，增量2691136字节；外部RSS44384256字节（约42.33MiB）。数字表示本机实际测量，不表示容量门槛或准入SLO。

当前采集清单与该测量清单在Schema、出现类型、字段、样本名和wire尺寸上相同；逐样本ProtoJSON尺寸差为结构逗号数乘-1，合法的0个结构逗号仍为0。原两份清单都保留，原采集 `current-inventory.json` 未覆盖。五份实际构建的源码／二进制归档在永久context，测量程序SHA `041ae73825c1d5485ebd062cdc70d2bfd397643c6450da1c4b93c93fddbd2e00`，inventory与benchmark前后均一致。

[本次原始数据、真实步骤与独立核验](protobuf-h1-data/final23/copy-manifest.json)按原字节另存，原外部manifest路径未改写。源509输入和语料在运行前后相同，并与受保护6a2快照绑定；原608及229历史数据未覆盖。独立审计只确认测量完整性，ROOT另核对公共producer语义；[最终23记录](final-acceptance.md)的原Make完整运行及逐条资格仍待实际完成，因此当前不得称最终M1或端云H1通过。
