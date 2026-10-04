# 开发范围与实现前检查

本文供开发者安排实现和评审。核心业务规则已有明确负责方、输入输出、状态和失败处理，仓库已建立领域处理器、存储、SDK 和运行装配的有界参考实现。实际开放范围与运行证据见[实施覆盖报告](implementation-coverage.md)；未完成能力和生产资格仍须逐项补齐，文档检查通过不表示服务可发布。

先实现一条用户目标闭环，再按需启用扩展。不得为一个可选功能先建设通用服务。遇到本文明确不支持的范围，必须返回unsupported，不得临时猜测另一种语义。

## 1 按这个顺序开发

| 步骤 | 开发者应产出的内容 | 开始下一步的依据 |
| --- | --- | --- |
| 1 身份与持久责任 | 原命令去重、回执、Job/Claim、可信时间、云端 PG 与端侧 SQLite 的各自事务适配 | 原提交未知和旧worker竞争测试通过；不得先接外部副作用 |
| 2 输入与目标 | Session/Submission、Task、GoalDocument、候选采纳、规则与覆盖 | 原文可追溯；空条件不能完成；steer接纳后旧目标不能继续准入 |
| 3 决策与执行 | 固定Snapshot/Decision、闭合Proposal、原Intent、授权使用、Attempt与效果归并 | 模型至多一次真实请求；回执丢失沿原身份核对；批量准入全收或全拒 |
| 4 核验与交付 | 当前条件判断、证据gate、权威 Result、云端 Task 完成事务与独立 Content 导出 | 必要效果不能用用户验收替代；导出失败不重开已完成目标 |
| 5 收尾与交互 | 费用差额、确认等待、取消、数据清理、当前授权呈现 | once不复活；未知费用不释放；UI能区分接纳、效果、完成与交付 |
| 6 可选扩展 | Schedule、环境、ChildHandle或远端资格，每次只开放通过合同验收的范围 | 下节接口与状态规则已编码，故障正反例和实际平台探针通过 |

字段和表的拆合是实现自由度；负责方、唯一键、原子提交集合、门禁和保留责任不得改变。开发者建议先写存储接口的合同测试，再写真实适配器。偏离建议时应在实现报告中说明影响。

## 2 已固定的关键业务选择

以下问题不再留给实现者自行决定。细节紧邻对应模块流程。

- 候选条件是增量upsert。未提及条件保留，先按规范语义键判重；替换绑定旧版本。第一版不隐式删除、拆分或合并条件。见[任务条件](../orchestrator/README.md#2-把目标变成条件)
- 一份 Decision 最多 4 个独立行动，在 Orchestrator 所属库中全收或全拒。外部执行不保证全成或全败。见[统一准入](../orchestrator/README.md#4-统一行动准入)
- 原命令等待确认时是accepted；本人决定是另一个命令；原业务重新核验后才一次消费。见[可信确认](../security/README.md#3-确认由真正的业务-owner-消费)
- 控制事实和启动窗口分别有身份。窗口到期不恢复once；不能证明未启动时继续核原效果。见[控制与恢复](../execution/README.md#4-取消未知与迟到)
- 原Result先在业务事务中固定，Content导出随后恢复。见[完成事务](../orchestrator/README.md#8-完成事务)
- Schedule固定时间边界、错过策略和recorded→sending切点。满槽只记录skip，不动旧槽。见[未来规则](../interaction/README.md#6-未来和周期规则)
- cell只提交完整成功的被动命名空间；已发生hostcall不随变量回滚。第一版不续跑已开始的cell。见[程序环境](../execution/README.md#7-程序化工具与可复用环境)
- ChildHandle换目标必须等待旧目标和效果关闭；费用可继续核对。见[子会话](../collaboration/README.md#6-可复用子会话)
- 远端资格检查与holder登记在完整gate锁域共同提交；成功Result的后续缺陷通知不因凭据到期丢失。见[证据资格](../evaluation/README.md#3-跨域当前资格的新增合同)
- 同一部署目标用稳定BindingHead裁决新激活、重启与停用。封存报告若被迟到事实推翻，发布资格单独失效。见[实例就绪](../extensions/README.md#2-从准备到当前实例就绪)与[实验封存](../evaluation/README.md#6-统计口径与优化门禁)

## 3 第一版明确的边界

“可选”表示部署可以不开启；开启后必须遵守对应合同，不能用部分实现冒充完整能力。

- Schedule可选。首版只有once、固定interval及daily/weekly/monthly，不支持任意RRULE、补跑或静默更换tzdb
- Environment与ChildHandle可选。首版不保存程序运行栈，不重放已开始cell，不迁移ChildHandle的owner
- 组合检查可选。一个组合DAG必须由同一治理authority负责；多authority组合暂不支持。Task的不同必要检查可以分别核验各自authority
- 发布与正式评测可选。首版要求报告资格gate、ReleaseApproval和ApprovalUse签发同治理owner/事务；跨owner发布证据资格暂不开放
- 有限Plan、typed_decision和向量检索是优化扩展，首个基本闭环不依赖它们。公开扩展前必须单独冻结闭合Schema、实际支持范围和测试

这些限制允许先实现正确的分布式主线。它们不是必须新增全局服务的理由，也不将生产目标退回单进程。

<a id="coverage"></a>
## 4 实施覆盖与开放条件

本表是完整目标的开发索引。业务语义由所链章节定义；机器合同以同版 Schema 和方法登记为准。“待交付内容”列说明各切片的完整要求，已实现与仍缺失的部分由[实施覆盖报告](implementation-coverage.md)区分，不表示该切片尚无代码。新能力仍须满足整条合同后才能开放。

| 实施切片与负责模块 | 已定义的语义与数据入口 | 当前机器合同覆盖 | 待交付内容与开放依据 |
| --- | --- | --- | --- |
| 接纳与恢复：Runtime、Orchestrator | [命令与 Job](../runtime/README.md)、[同库事务与锁序](../data/storage.md#3-同一数据库事务具体包含哪些记录) | Command/Receipt/QueryBinding/Job/Claim及typed方法登记、原决定/查询、Go/TS journal和发现已编码；公开方法由实际Registry与配置冻结 | 去重与领取处理器、PG/SQLite 各自适配及恢复 SDK；取得 F01–F04 的重复、提交未知和旧 worker 竞争证据 |
| 输入与目标：Interaction、Orchestrator | [输入与任务](../interaction/README.md)、[条件形成](../data/requirement-lifecycle.md) | Session/Message/Submission/InputRequest/GoalRevision/SourceEvidence/Condition/Coverage及task/input/branch/control闭合输入输出与恢复已登记；可选Transfer按准确配置开放 | 原文／Input／SourceEvidence可追溯、输入终态窗口不误消费、Session cutoff与actor撤回两库有据；现代默认有限Goal Form已登记实现及两库race／SDK-Web6／unsafe22通过，最后实际浏览器仍待 |
| 决策与行动：Brain、Execution、Security | [固定决策](../brain/README.md)、[真实执行入口](../execution/README.md)、[授权消费](../security/README.md) | Snapshot/Decision/Proposal/Operation/Attempt/Effect/Grant/Use/Resource及Brain/Execution方法族已闭合；行动batch≤4、显式disclosure、在线delegation与离线Lease分别限制 | CurrentStart先原Task根／预算再完整当前parent强门；normal四项和truePause8两库race通过。Saved最新同源十項证明实际consumer当前许可；原5s窗口／30sparentProof／身份／来源／Claim门不减，最后新根共享false fixture分支与整套仍待 |
| 核验与交付：Orchestrator、Evaluation、Memory | [完成事务](../orchestrator/README.md#8-完成事务)、[证据资格](../evaluation/README.md)、[内容发布](../memory/README.md#2-发布准确内容) | ConditionResult/Check/EvidenceGate/Result/Content及独立check/import/准确upload-reserve/ready/put/holder方法已登记；原Result与后续导出责任分离 | 独立验证器、完整关系索引、当前资格与结果导出恢复；F05–F07、F11 证明完成门禁及导出失败后继续责任 |
| 收尾与使用：Accounting、Security、Memory、Interaction | [账务](../accounting/README.md)、[清理与使用](../memory/README.md)、[准确输入](../interaction/README.md) | Usage/预算预留/确认/AllocationClosure/Content与Memory cleanup/query/view/getter及Grant settlement/list方法已编码；远程fresh双库完整正常报告已验证三层Closure、原Task／Grant费用、必要proof和原Job／join／重开；原871／875失败保留，后继preparednormal4PASS／actor两库race与原r5正确恢复分别记录 | normalRemote必要Job／proof／三层Closure、NoChild永久拒绝三层真证明／once原账和原r5正确恢复均有准确双库／原Scope证据；Gov完整关闭Snapshot重复query复用原Proof两库race通过，变化／未闭／当前权限继续原强门。最后合成版本资格另验收 |
| 可选能力：Schedule、Environment、Collaboration、Extensions、Evaluation | [模块内部字段](../data/module-records.md)、本文第 3 节及对应模块方法表 | Schedule/Environment/ChildHandle/Extension/Evaluation/Knowledge/Foreign原引用的typed合同已登记或配置开放；WASI/GUI/Source/Native范围有准确profile，Agent完整正常报告已在SQLite／PG父库通过，Session／history cutoff有准确双库有限资格，原Sessionquery撤权真实FAIL后继两库race已过；NoChild三层Closure、现代默认有限Goal Form及当前parent Pause8两库race均有后继资格；旧no-child FAIL／rendererunsupported RED及prepared240.307176s未到pauseProbe的FAIL独立保留 | 各开启能力只按同版有限profile开放；GUI／WASI／Source／Native／Device／Remote／Knowledge的准确原refs、controls和Grant交集已有定点证据，最后NoChild／Pause／Saved本地门禁已补完。未开放profile和生产/全图/浏览器资格另外记录，不能归为同一全绿声明 |
| 传输与生产：Protocol、Production | [端云和 gRPC](../protocol/README.md)、[部署与恢复](../production/README.md) | 同版gRPC JSON外壳、HTTPS/WSS帧/认证发现/回执、EndpointChannel原connection-binding/Reply-Ack与SDK已实现；静态公开进程/分类worker有限配置已验收 | WSS/gRPC、发现/认证、背压、迁移和观测适配；F21–F24 取得通道恢复、跨区、容量和灾备证据 |

机器合同与历史运行证据不等于最终合成版本已验证。完整方法资产以受信Registry/API生成器为唯一源；
未登记方法或未配置profile仍明确unsupported。实际剩余本地链和外部生产资格见实施覆盖报告，
不能从本表的已编码记录推断所有可选组件或部署组合通过。

F 编号的完整刺激和判断依据统一见[故障矩阵](../validation/README.md#2-关键故障矩阵)。表内列出开发关注点；能力还须满足其依赖的授权、来源、费用和故障要求，不能只通过表内几个编号就宣布全部支持。

### 线协议怎样补齐

core.schema.json维护共同公开记录和五条共同命令，实际领域与可选方法由受信Registry各自登记同版闭合输入/输出Schema，并通过唯一生成器形成Go/TS/Native方法资产和恢复decoder。上述方法族已有实现，设计方法表本身仍不能证明任一完整profile已开放。新增或变更方法必须同步合同、登记、生成物和实际SDK恢复正反例。

五条命令及其准确范围见[协议覆盖](../protocol/README.md#schema-coverage)。每个拟开放方法都须登记：负责方、所属 profile、请求/响应/错误 Schema、回执阶段、原命令查询方式、版本与权限前提、SDK 恢复行为和正反例。字段或语义出现缺口时先在所属模块补合同，再更新登记；新增业务取舍需进入明确的设计评审。

Schema只能检查结构。来源认证、权限、事务、作用域、真实效果和当前证据资格必须由处理器及存储实现验证。状态模型与设计样例不能替代这些工作。

### 部署和外部服务

以下条件取决于实际账户、平台或供应商。相关能力启用前必须取得证据，缺失时应关闭该能力并报告具体原因。

- 模型/工具的原请求查询能力、幂等保留期、真实计费上界及是否存在隐式重试
- 文件系统刷盘/原子替换、GUI观察与动作边界、WASI/进程隔离、实际退出与旧实例隔离
- 公司身份/密钥、可信时钟、三可用区同步提交与旧主隔离、对象介质耐久和恢复
- 外部Agent的原创建键、控制、完整效果关闭和费用更正能力
- 自然语言提炼保真、目标覆盖、判断校准和端到端质量；模型自评不能代替独立真值

TaskPolicy的预算、期限、风险类别、具体业务阈值及开放能力必须由产品/运营以准确配置确认。文中的初值是测试输入，不是未经实测即可承诺的容量或质量保证。

## 5 怎样完成本轮实现评审

开发者应选择一条具体请求，逐步指出每个事务的输入、原身份、写入记录、失败返回和下一项Job。再执行重复、乱序、超时、取消、恢复及跨租户反例。无法指出原记录或唯一负责方时，必须暂停该能力开放并补合同。

设计阶段取得了独立设计复核与设计资产检查。实现阶段另有真实 PG/SQLite、文件、模拟设备、测试 HTTP 出口和浏览器证据，准确版本及边界见[实施覆盖报告](implementation-coverage.md)。真实目标读者使用、供应商质量与对账、跨区故障和生产容量仍须独立验收。本文借鉴简明语言原则组织，不构成ISO标准符合性声明。

历史阶段精确集成：Session13prod Root881、13tests Root54f86、Remote8tests Rootbb175与双地点正式叶Root57980ff均有compile／vet元数据。有限行为仍只绑定各原source／binary；正常双库报告103.121656s／109.541635s不代替上述真实缺口、未运行项或最终同图验证。

当前限定本地实现已完成逐项定点验收：远端Agent／独立设备的正常报告、current actor与父控制、无子Task永久拒绝的三层Closure／原once与费用责任、现代默认Goal Form、原关闭证明复用及Saved五秒证明消费门禁均有准确证据。Saved最新同一911路径源／race binary的五场景×SQL／PG十项actualPASS，正式7路径叶17662e564d121732b0fbac8ddbed938a7a7baabb已合当前CODE 58c9898af58babce7170cb37cbbdfc63d693af9e. 17按已开放有限静态profile记resolved；09仍partial等待最终固定版本完整检查、原cfg浏览器重开／全生命周期、两份独立全图审查与发布步骤，07保留浏览器验收待项。新prepared共享false测试分支的观察边界仍由最终整套检查取得资格。代码集成和各旧source行为资格分开，不能声明当前根全套已通过。真实账户／公司身份／开放自然语言质量／物理设备／其他OS／规模与多AZ为另外明确的未验收范围；旧FAIL／SKIP／NOTRUN及原业务身份、权限和期限保留。

历史prepared第一次race240.307176s未取得pause资格：原Rooted0／source897 c01ed55c／binary8574b49e实际EXIT1。日志remote_create只是receiver Job标签，handler内部HTTP POST超时，不确定具体方法；真实create已accepted／Incoming preparing，无子Task／Op／pauseProbe。原normal153.070004s通过保留，后继真实暂停race146.115743／163.071059s见最新资格，不回填原FAIL。原30s证明与Saved5s证明分开，未持久的历史typed cause不猜测。

现代默认brain.GoalSchema的有限Goal Form已实现，原Schema及digest不改：准确ContentRef／固定hash pattern、正整数version、有限enum与unique choice数组由Go呈现门禁及TS原组件支持；非登记复杂schema仍明确拒绝。原真实renderer_schema_unsupported RED2.575433s、首次SQL cwd fixtureFAIL0.121690s保留；同source901修runner cwd后Go SQL／PG normal2.645041s／2.888794s、SDK／Web三个文件六测试1.086276s、22个unsafe拒例通过。7路径3647d7e已合Rootd67，Rootd67两库focused race11.691060s／11.304801s actualPASS／noSkip／noDataRace／源码binary稳定，索引 /workspace/harness-dev-environment/modern-default-form-root-d67-race-20261004T0512/owner-readonly-qualified-index.json（SHA8d33f732…）。这是原现代公开InputRequest与表单数据合同的有限实现资格，不能把组件SSR／SDK通过代作最后浏览器生命周期、任意JSON Schema或整个参考部署的通过。

当前根58c9898af58babce7170cb37cbbdfc63d693af9e的whole Go BUILD已实际通过：go build -p 2 ./...，2026-10-04 08:24:10.539295→08:24:22.127860 UTC，EXIT0／11.691s，clean911路径／源码摘要f8053bfc66e06e132d4457d0f90f2e8e05f2c0b9e6c7853b146787800cd01962前后稳定。索引 /workspace/harness-dev-environment/full-go-build-58c9898af58b-20261004T082410Z.json（SHA0f696c89…）。Root精确Saved七路径集成 compile／vet／fmt／diff均通过，904受保护路径保持，索引 /workspace/harness-dev-environment/source-integration-saved-consumer-awaiting-all10-w6xdiyqa/verification.json（SHA4dbb2181…）。编译与静态检查不代表当前根whole行为Suite；scripts/check、完整NORMAL／RACE、浏览器、两份新独立全图review及push尚未执行。

统一阶段说明：当前CODE 58c9898af58babce7170cb37cbbdfc63d693af9e；本文实际通过只按所列原source／binary／selector及数据库制品限定。最新Saved十项、NoChild／Pause／现代Form／Closure后继资格已取得；最后完整检查／浏览器／全图审查尚未结束，旧失败／未跑记录不回填。
