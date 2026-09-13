# 21 票内容获取实施记录

本票实施中，原范围以 [Agent Brief](../../.scratch/harness-implementation/issues/21-fetch.md) 为准，最终代码审查基点 `cab3975`。本地网络接口测试不代表正式任务路径或实际公共联网验收完成。

## 真实 HTTP 响应与来源范围

消费方 `fetch.Fetcher` 接受单次 URL、正文上限、请求上限及总时限，返回实际正文和来源元数据。HTTP Adapter 的可信宿主配置必须同时提供精确 URL 清单及允许拨号的地址范围；配置切片复制，不接受调用者 HTTP 头、凭证、代理或 Cookie。解析后的全部地址必须匹配范围，实际拨号使用已核对的数字地址，避免再次解析改变目标。普通 HTTP 仅允许显式配置的数字回环地址，而且仍须通过网络范围检查。

参考调用目前只发出一次 GET，重定向全部明确不跟随，尚待后续逐跳授权/预算实现。无连接复用及隐式压缩；响应头、正文、读取与握手有界，非 200、编码不支持或超限不释放部分正文。成功记录实际请求/最终 URL、获取完成时间、媒体类型、状态和正文 SHA256；失败只返回已开始的请求尝试数及有限分类错误，不返回底层 URL/错误正文。此接口是宿主受信基础设施，不是用户端点；正式任务授权和持久预算、证据保存仍须接入。

`TestHTTPFetchReturnsActualResponseEvidence` 通过实际本地 HTTP 服务取得 `hello`，独立核对已知摘要、媒体类型、来源 URL 和真实获取时间。初次缺少实现的失败与实现后 race 通过分别保留为 `evidence/21-http-response-{red,green}.log`。

`TestHTTPFetchRejectsUnconfiguredURLAndDialAddress` 验证未登记 URL 和未获准拨号范围均未触达服务、未释放正文或来源结果。地址拒绝最初被 HTTP 包装后变成 Unavailable，红灯准确记录服务请求数为 0 而错误分类不符；修复保留 Denied 后通过。证据为 `evidence/21-http-scope-{red,green}.log`。fetch/httpfetch 定向 race、vet 与 diff 检查通过。

当前环境 Linux/amd64、Go 1.26.1；使用标准库，未新增依赖。没有公共网络请求、模型请求或 .env 读取。逐跳重定向、完整失败语义、当前授权、任务预算、受控证据/Context、原操作持久恢复、公共联网证据、CLI 及最终全仓验证与审查仍待完成；原票验收项保持未勾选。

## 有界重定向及超时/取消分类

HTTP Adapter 现支持 301/302/303/307/308 的有限跳转。每跳重新核对精确 URL 清单，再按同一请求上限和总时限发出请求；解析和数字地址拨号继续执行原网络范围。重定向响应体关闭后构造全新 GET，不带上游 Cookie、Authorization 或 Referer；不把重定向正文作为最终证据。成功记录原始与实际最终 URL、累计请求数；越权目标返回 Denied，请求额度耗尽返回 LimitExceeded，均不释放不完整正文。

实际本地服务测试覆盖允许的相对跳转、目标越权、单次额度以及两次额度内的循环。正例从原先不跟随重定向的真实失败变为实际取得目标正文；失败边界检查目标未收到超出允许范围/额度的请求。证据见 `evidence/21-http-redirect-{red,green,bounds}.log`。

取消与超时现在通过有限错误类别分别返回 Cancelled 和 TimedOut。取消前未发请求的用例保持 Requests=0；实际服务收到请求并等待取消的超时用例保持 Requests=1，无正文或最终来源结果。初次两种失败均被归为 Unavailable，修正分类后通过，证据见 `evidence/21-http-timeout-{red,green}.log`。包括前述响应与范围用例的定向 race 全部通过，vet/diff 检查通过。

本增量只完成可信宿主的静态来源与网络范围约束，尚未将动态当前授权或原任务预算持久化接入每跳。媒体类型支持矩阵、时效、受控证据/Context、原操作恢复及实际公共联网等原票要求仍待实现，不将 Adapter 通过报告为整票完成。

## 当前来源权限与释放复查

HTTP 配置现在必须提供消费方定义的 `fetch.Authority`，缺少授权器不能启动。参考 `fetchauth` Adapter 使用真实 Harness `ViewActions`，将可信宿主固定的主体、命名空间、用途、处理位置、接收位置和每个精确 URL 的资源映射用于判定；网页内容不参与身份或资源选择。请求阶段同时要求 fetch.read/fetch.process，释放阶段另要求接收位置的 fetch.disclose；这些权限不包括长期保存或扫描。

每跳发出请求前、DNS 解析及地址范围检查后实际拨号前均检查当前权限。最终正文返回前复查所有已访问 URL 的读取/处理/披露权限，包含原目标与中间重定向来源，避免只核对最终正文而泄漏已撤权来源元数据。授权失败只返回有限拒绝分类，原已开始请求数保留；取消/超时仍保留其独立分类。宿主中的 URL/网络上限和动态权限同时生效。

真实 SQLite 授权+HTTP 服务测试在服务器返回重定向或正文前执行实际策略撤销。红灯分别观察到两次请求和泄漏正文、一次请求和泄漏正文；接入后两类均只发生原先获准的一次请求，返回 Denied 且不含正文/最终 URL。另分别移除 read/process/disclose，验证前两者在请求前阻止，只有缺少 disclose 时允许原读取但不释放结果。证据见 `evidence/21-http-authority-{red,green}.log` 和 `evidence/21-http-independent-permissions.log`。

原响应、范围、重定向及超时测试全部改为真实授权服务配置，定向 race、vet、diff 检查通过。授权与外部网络不是跨系统原子事务，复查与实际拨号/释放间仍存在有限本地执行窗口；未宣称跨系统原子撤权。正式 Core 任务、持久预算、证据保存/Context 和原操作恢复尚未接入，当前仍非整票交付。

## 原任务请求额度与原操作持久身份

新增消费方 `fetch.AttemptStore` 与 SQLite 参考实现，记录原任务引用、主体、原操作 ID、执行输入指纹和请求上限，不保存 URL 或响应正文。每任务固定总请求上限（最多 128），单操作最多预留 5 个请求；任务和操作分别最多 512 条。调用有界，SQLite 以单个写事务提交操作身份及任务额度，独立连接也不能超额。

Begin 只有首次事务提交的调用者得到 fresh=true，表示首次预留；同身份重放返回 false，不能据此再次发起网络请求。相同 operation_id 携带不同任务、主体、指纹或额度返回 IdentityConflict；原操作核对优先于新额度比较。任务额度配置一旦形成不能通过更换操作或宿主重开静默提高。

TaskBudget.Charged 是原操作请求上限的累计保守预留，不是实际 HTTP 请求数。本参考账本不退还预留，包括失败和未知操作；实际次数仍须由执行结果另记，不能把 Charged 冒称 Used，也不能将未知结果当作未调用而释放额度。该额外获取额度补充 Core 的操作数预算，尚待驱动与正式任务绑定，不替代 Core qualification、授权或总截止时间。

真实测试验证预留后关闭/重开、同操作重放、剩余额度、新身份拒绝和双连接竞争。子进程在预留事务提交后、不返回结果且不 Close 时直接退出 75，重开确认原操作不能取得新的执行许可，未知上限仍占用原任务额度，替代操作被拒绝。证据为 `evidence/21-fetch-budget-{red,green,recovery}.log`。fetch/sqlitefetch/httpfetch/fetchauth 相关 race 通过，vet/diff 检查通过。

本增量未接入正式执行或保存响应结果，不据其通过宣称已完成获取恢复。后续仍须绑定实际 Core/SDK 调用、保存已知结果与受控证据引用，并验证回包丢失与 Content 提交窗口。

## 已知结果的持久事实

额度账本增加 OutcomeStore：只有已存在且完整身份匹配的原 AttemptIntent 才能提交结果。结果只包括有限状态、实际已知请求数和可选受控 Content 引用，不接受 URL 或正文作为引用；请求数不得超过原预留。acquired 要求非零请求数及同命名空间的受控引用格式，失败状态不带正文引用。引用格式通过不等于 Content 仍可用或当前可披露，实际访问仍须由受控 Content 判定。

原操作存在但没有 Outcome 时返回 known=false；不存在原操作则返回 Missing。已知结果不可替换，原样 Complete 重放成功，改变请求次数或结果返回 IdentityConflict。Complete 不退还 Charged，因此已知实际次数与保守预留始终分别表达，不把失败或未知额度变成新抓取许可。

真实 SQLite 用例验证未确认→已知超时、关闭重开、原样重放、次数更改拒绝、没有原操作不能补结果及失败后额度不复用。真实子进程测试扩展为结果提交前和提交后退出 75：前者重开仍未知，后者保留原超时与 Requests=1；两者均保持原任务 Charged=2，不批准替代请求。证据见 `evidence/21-fetch-outcome-{red,green,process}.log`。相关定向 race、vet 与 diff 检查通过。

这是持久元数据接口验证；尚未把真实 HTTP 结果交给 Content 保存或经正式任务恢复，不把测试中的有限结果记录作为真实网络证据。Content 提交/确认之间的恢复、当前披露复查及正式 Core/SDK 路径仍待接入。

## 真实获取证据的受控保存与读取

新增消费方 `fetch.Evidence` 和 `fetchcontent` Adapter，调用已有受控 Content PUT/LOOKUP/GET/READ；SQLite 请求账本仍只保存有限事实与引用。HTTP 成功结果现在携带全部实际请求 URL 的有序来源链，失败仍不释放来源或正文。可信宿主将精确 URL 映射为有版本的 ContentSource，完整跳转链参与保存及后续读取的来源约束。

证据以受控 JSON 保存实际正文、请求/最终来源、获取时间、媒体类型、HTTP 状态、请求次数与正文摘要。保存前检查来源链、次数和正文摘要；读取校验证据对象摘要、正文摘要、获取时间及 Content 来源一致性。单次获取正文上限仍为 1 MiB，序列化证据上限 2 MiB；实际可保存大小还受 Content 实例自身 MaxObject 限制。宿主固定资源、用途、保留截止时间及处理/接收位置；调用者不能在正文中扩大这些配置。

原保存 operation_id 由调用方提供，必须是已有授权系统认可的身份。原样 PUT 可重放，更改获取时间不能覆盖原操作；LOOKUP 可恢复原引用，不进行网络调用。此接口尚未替调用方完成保存操作与 AttemptIntent 的持久绑定，该集成仍待完成。

真实本地 HTTP 重定向、SQLite 授权、文件 Content 和可变来源策略组合测试已通过：hello 的已知摘要、实际获取时间及两跳来源在读回后保持；单独移除 retain 阻止保存；恢复保留权限后成功保存、原样重复及 LOOKUP 返回同引用；修改获取时间被拒绝；撤销中间来源权限后无法读出正文。首次红灯为缺失 Adapter，开发中修正测试来源规则使用裸动作名（Content 身份动作使用 content 前缀）。证据见 `evidence/21-fetch-content-{red,green}.log`。

本增量相关 fetch/httpfetch/fetchcontent/sqlitefetch race、vet 和 diff 检查通过。尚未完成正式 Core/SDK/Task Context 路径、Content 确认丢失窗口集成、媒体支持与过期分类、固定重放与公共联网验证、全仓验收及最终双轴审查，21 保持 in-progress。

## 原保存身份与确认丢失恢复

AttemptIntent 增加 EvidenceOperation，将授权系统预先分配的证据保存操作与原请求预留一起提交；重开后可检索，已有预留更换保存身份会产生 IdentityConflict。保留旧账本中空字段的读取兼容性，但新的 Acquirer 不接受空保存身份，也不为旧空身份生成替代操作。

新增内部 `fetch.Acquirer`，组合 Fetcher、OutcomeStore 和 Evidence。只有首次持久预留成功才发起 Fetch；已有预留只核对原已知事实，或沿原 EvidenceOperation 进行 LOOKUP/READ，取得真实保存证据后补交结果。已知失败沿原结果返回，未知保存/确认窗口不产生第二次 Fetch。恢复时重新执行 Content 当前权限，核对实际请求次数与原结果；撤权阻止本次披露，但不改写既有持久获取事实或退还预算。

该接口属于受资格约束的内部执行协调，尚不是 Core 用户入口。正式驱动仍须绑定原任务资格、主体、请求内容指纹、额度及授权系统分配的保存身份；本接口不能替代这些条件。保存 operation_id 不能借用已有其他业务的 Content 操作；其分配和持久绑定将在正式驱动接线时完成。

真实 HTTP、SQLite 和文件 Content 测试在成功 PUT 后注入确认回包丢失：首次保持未确认，重开请求账本后查回原 Content 引用，实际两跳请求数不增加，Charged 保持 2。另预留一个未获取任务后模拟恢复，未生成替代请求；撤销中间来源后 Recover 返回 Denied，原 acquired 记录保持不变。该测试没有声称进程被杀死；真实进程退出覆盖仍是前述账本提交窗口，完整获取链的子进程故障测试待接入。

证据为 `evidence/21-fetch-evidence-identity-{red,green}.log`、`evidence/21-fetch-recovery-{red,green}.log`。相关定向 race、vet、diff 检查通过。21 仍未完成正式 Core/SDK/Task Context 接线、过期/媒体类型等结果分类、公共联网与重放验证及最终全仓验证和双轴审查。

## Execution 获取驱动接口

新增 `fetchexecution.Driver` 实现已有 Execution.Driver。宿主绑定命名空间、主体、凭证、完整能力身份/摘要及请求额度；驱动检查原任务引用、资格版本、输入引用和能力描述身份。该检查不能证明 Core 资格已获准，仍须由 Execution 调用前执行正式 Core Guard。

首次 Start 严格解码 url/max_bytes/max_requests/timeout_ms，限制只能收缩宿主配置；重复键、额外字段和超上限输入在预留及网络之前拒绝。驱动通过授权系统 NewOperation 分配独立证据操作，和原任务/调用/执行请求指纹持久绑定后获取。重复 Start 使用原身份恢复，不重新分配保存身份；Inspect 不依赖原输入字节，核对原执行请求指纹后读取当前受控证据。

成功 Observation 仅返回有限状态和受控引用，来源/时间/正文保留在受控证据中；有限获取失败无成功正文，有实际请求的失败记录为已发生获取尝试，没有实际请求的失败记录为未发生。未知结果保持 UNKNOWN。此处尚未处理已知获取事实在正文退休后的专门 Observation 分类，及最终 Execution 输出 Content 的来源传递，需在正式集成中补齐。

测试使用真实 HTTP、SQLite 授权与请求账本、文件 Content，通过驱动 public Start/Inspect 验证两跳真实正文、原保存身份/请求指纹、重建驱动后无 Input 的恢复、重复 Start 不增加请求、替换 InputRef 拒绝，以及尺寸/请求数/时间上限、额外字段和重复键的负例。测试中的 Call 为直接构造的驱动边界夹具，不代表经过 Core/SDK。下一步正式 profile 应参考现有 extractioncheck 的 Core/授权/SDK 组装方式，绑定真实任务资格与能力 Schema，不重复建立用户执行循环。

证据见 `evidence/21-fetch-driver-{red,green}.log`；定向 race、vet、diff 检查通过。整票仍为 in-progress，完整 Core/Brain/SDK/Context 路径与全仓验收未完成。

## Core 与 Capability SDK 的实际任务验证

新增 `profiles/fetchcheck`，采用真实 SQLite 授权、Core 任务及工作资格、JOSE 单次签名授权、Execution Service、进程内 Capability SDK 绑定、文件 Content 和实际 HTTP Adapter。来源服务为明确允许的本地 HTTP 两跳夹具，当前验证入口为 `go test -race ./profiles/fetchcheck -count=1`。

任务先保存受控输入，提交 Core、取得工作资格、签发绑定原调用和语义指纹的单次授权，再经 SDK Invoke、Execution Run/Drain 完成。测试核对 Core COMPLETED、Execution SUCCESS/CONFIRMED、实际 HTTP 请求数 2、原任务 Charged=2、受控正文 hello 的固定摘要、请求/最终来源及两跳证据。原 SDK 调用及 Run 重放保持原回执、任务版本和请求数。

独立签发绑定错误任务版本的授权后，Core 拒绝该资格，获取账本无对应预留且服务端请求数为零。证明签名材料不能替代当前任务资格。

当前参考宿主在受控输入中预先列出全部获准的两跳来源；Execution 外层输出保守继承这些来源限制。测试撤销来源规则后，外层产物 GET 同样被拒绝。该设置适用于本次精确 URL 参考配置，并未证明任意动态来源组合下的输出来源合并已完成；后续通用接线必须确保取得的来源约束不因外层封装而丢失。

首次红灯为尚无 fetchcheck 实现；接入后 Core/SDK 正向及过期资格反例、原 HTTP/驱动/账本相关 race 均通过，vet/diff 检查通过。证据为 `evidence/21-fetch-task-{red,green}.log`。本轮没有运行公共联网、模型或完整全仓验证。Brain/Task Context、任务控制与截止时间传播、媒体/过期分类、完整进程恢复、具名 CLI 报告及最终审查仍待完成，21 保持 in-progress。

## 响应类别与正式任务失败事实

HTTP 参考实现限定 text/plain、text/html、text/markdown、application/json；正文须为完整 UTF-8，声明 us-ascii 时另检查 ASCII，其他字符集拒绝，JSON 还必须语法有效。不执行 HTML、网页指令、字符集转换或 JSON 修复。原字节及摘要保持一致，不将部分数据作为成功正文。

401/403 返回 Denied；410 Gone 作为来源已撤下/过期返回 Expired，404 及其他未支持 HTTP 状态仍返回 Unavailable。这不是 HTTP 缓存新鲜度计算，也不证明 200 内容在业务意义上最新；缓存时效和受控 Content 到期的独立分类仍待补齐。所有失败仅释放有限类别及实际请求次数，不携带正文或来源链。

新增真实 HTTP 响应表测试覆盖媒体/字符集/UTF-8/JSON 反例、拒绝/缺失/已撤下，以及中文文本、JSON、HTML 原字节正例。正式 Core/SDK 失败 profile 覆盖 403/404/410：两跳请求均计入原 Charged=2，结果为不可替换的 denied/unavailable/expired、Requests=2、无正文引用；Execution 保留 FAILURE/CONFIRMED 的实际获取尝试事实。参考任务 MaxSteps=3 尚有步数，因此 Core 按既有策略返回 QUEUED；原调用重放不增加请求、不推进任务版本或退还额度。未把自动重排队误报成新抓取已获准。

集成测试暴露参考签名授权使用“当前时间+1小时”可能稍晚于初始化父授权到期的问题，签名窗口改为当前时间+30分钟，保持父授权范围内。首次集成失败日志保留在 `evidence/21-fetch-response-classes-integration-failure.log`。修正后统一定向 race 运行通过，输出同时归档到 `21-fetch-response-classes-green.log` 和 `21-fetch-task-failure-green.log`，两者来自同一次实际运行，不是两次独立验收。红灯记录亦保留；vet/diff 检查通过。

21 仍为 in-progress。Brain/Task Context、控制及截止时间传播、完整进程恢复、受控证据到期、新鲜度与公共联网/重放、CLI 及最终全仓验收未完成。

## 受控证据到期、清理与已知获取事实

fetchcontent 现在要求注入与授权服务一致的可信 Clock，先由 Content GET/LOOKUP 执行当前身份及来源权限，再核对保留截止时间。到期返回 Expired；不能仅依赖 State=expired，因为 Content 的持久消费失效会先进入 cleaning，物理清理后进入 cleaned 且擦除正文元数据。保留的命名空间、资源、用途和截止时间用于有限到期分类，不恢复正文。

Driver Inspect 只有在原 acquired 结果已提交且当前授权 Content 核对明确过期时，才返回 FINISHED/FAILURE/CONFIRMED 的有限证据，保留实际已知请求次数，不返回 Output 或引用。未知结果不因此被升级为已知获取；来源撤权仍返回 Denied/UNKNOWN，不能借到期分类绕过当前权限。原持久 acquired 事实不改写成新的网络失败，不退款或重新抓取。

真实 Core/SDK 获取后，测试通过共享授权时钟推进 11 分钟触发保留失效（不是等待真实 11 分钟）。验证原正文不可读、驱动保留已发生效果、已知记录与 Charged=2 不变，实际 HTTP 请求保持 1；再运行真实文件 Content Clean，清理后仍拒绝正文并报告期限已过；随后撤销来源，Inspect 不披露任何效果证据。最初红灯为 Unavailable；进一步排查发现 cleaning 状态，改为可信时钟/保留元数据判定后通过。

证据为 `evidence/21-fetch-expired-content-{red,green}.log`，相关 Core/SDK、HTTP、SQLite race 与 vet/diff 检查通过。本次到期验证通过 Content 和驱动 Inspect；没有声称已经测试过期后的完整 Core 重新调度。原票的 Brain/Task Context、控制/截止传播、完整进程恢复、公共联网/固定重放、CLI 和最终验收仍待完成。

## 获取边界的当前 Core 资格复查

新增 `fetchtask.Guard`，通过授权 RuntimeTransaction 调用已有 WorkPort.GuardExecution，核对原任务/工作代次/调用身份、运行控制意图、租约及截止时间，consume=false，不占用第二份执行许可。正式 fetchcheck 宿主将 Guard 注入获取驱动，驱动通过仅宿主可构建的上下文携带原执行请求检查；HTTP 在每跳请求、实际拨号及正文释放的授权边界同时检查该资格。

真实 Core/SDK 反例在服务端接收到第一跳后、返回重定向前通过正式 ControlService 接受 PAUSE 或 CANCEL。红灯观察到两例均仍发出第二跳并交付成功正文；接入后两例都只保留第一跳实际 Requests=1，有限结果 denied、无受控正文引用，Execution 不产生成功结果。原请求上限预留不退款；正常 Core/SDK 正向获取仍通过。

这项检查是边界复查，不是跨数据库和网络的原子撤权，亦未实现正在等待的 HTTP 请求随任务停止即时中断。Core Guard 已包含截止时间判断，但独立截止场景及在途截止传播尚未验收。驱动 Guard 当前对独立低层夹具仍允许省略；正式任务宿主必须注入。后续收口需避免其他正式组装遗漏该约束，不能把可选低层配置当作完整任务控制保障。

证据见 `evidence/21-fetch-control-{red,green}.log`。相关 Core/SDK、HTTP、请求账本定向 race、vet、diff 检查通过。21 仍为 in-progress，Brain/Task Context、在途控制/截止、完整进程恢复、公共联网与固定重放、CLI、全仓验证及双轴审查仍待完成。

## 在途资格失效与取消后的有限事实收尾

HTTP 获取在存在可信任务 Guard 时增加串行监视：每 100ms 复查一次，单次检查上下文最多 100ms，资格检查失败取消 HTTP 子上下文，返回有限 Denied。每跳、拨号和释放的同步检查仍保留；结束时取消并等待监视退出，不留下持续后台轮询。当前 Core/SQLite Guard 遵守检查上下文；可信替换 Guard 也必须遵守截止与取消，不能阻塞退出。此策略是有界轮询，不是即时或跨系统原子停止。

真实在途测试在服务器已收到请求后通过正式 Core ControlService 接受 PAUSE/CANCEL，然后等待 HTTP 请求上下文结束。红灯中两例等到调用超时并留下未知结果；接入监视后两例都以 denied、Requests=1、无正文引用结束，不触发下一跳，原额度仍保留。正常路径与边界暂停测试继续通过。

另一个真实 Core/SDK 用例在服务器收到请求后取消调用方上下文。发现已有的 Complete 继承取消上下文，导致已经明确观察到的一次请求无法提交。现在失败事实收尾使用脱离取消但最多 1 秒的上下文，仅提交有限状态、实际已知次数及原身份，不发送请求、不保存正文、不退款。修正后 caller cancellation 持久结果为 cancelled/Requests=1，Charged=1；本测试只证明异步有限事实写入，不宣称取消后的 Core 最终调度状态已经完整验收。

证据见 `evidence/21-fetch-inflight-{red,green}.log`、`evidence/21-fetch-cancel-count-{red,green}.log`。相关定向 race、vet、diff 检查通过。独立任务截止时间测试、完整进程恢复、Brain/Task Context、公共联网/重放、CLI 与全仓/双轴验收仍待完成，21 保持 in-progress。

## 独立 Core 截止时间与超时原因传递

fetchtask 增加当前受权 TaskReader，读取原任务截止时间，并用同一授权服务 RuntimeTransaction 的 Now 及 Core Guard 进行核对。Core 明确拒绝且原任务版本/截止时间稳定、保留的截止时间已过时，返回 TimedOut；其他资格失效仍返回 Denied。元数据与 Guard 为独立事务，因此超时分类前再次核对任务版本及截止时间；任务更新或读取失败时不沿用旧时间分类。

同步请求/拨号边界和在途监视均保留此有限超时原因；HTTP 子上下文取消原因通过错误映射进入原持久结果，不再把 Core 截止统一压成一般拒绝。仍保留每次实际请求消耗及原上限预留。

真实 Core/SDK 测试把任务截止配置为 3 秒、工作租约仍为 10 秒，服务器收到第一跳后将共享授权时钟推进 4 秒；HTTP 的真实 1 秒计时仍在进行。重定向与在途等待两类都验证 timed_out/Requests=1、无正文引用、无第二次请求、Charged=2。时钟推进是明确的截止故障注入，不声称真实等待 4 秒；底层仍为实际 HTTP、SQLite、Core/SDK。初始红灯均为 denied，原因传播后通过。

证据见 `evidence/21-fetch-task-deadline-{red,green}.log`。相关定向 race、vet、diff 检查通过。当前是有界复查停止，未宣称纳秒精度截止或跨系统原子停止。21 剩余 Brain/Task Context、完整进程恢复、公共联网/重放、CLI、配置约束收口和最终验收继续保留。

## 真实进程退出后的 SDK/Core 恢复

新增真实子进程验证：父进程准备 Core 任务、受控输入和 SDK 调用授权并关闭持久组件；子进程重开同一配置，通过 Execution Run 进行实际 HTTP 获取，在指定边界直接 os.Exit(75)，不执行 Close。父进程重开原 SQLite/文件 Content，通过 Capability SDK Reconcile 恢复并向 Core Drain 原报告。

四个窗口分别为原预留提交后/HTTP 前、完整 HTTP 响应返回后/保存前、Content 保存后/结果提交前、结果提交后/Execution 确认前。预留窗口实际请求为 0，其余为真实两跳请求 2。前两种无可恢复正文，保持 UNKNOWN 且 Core WAITING；后两种沿原 Content 操作查回正文、来源及实际获取时间，恢复 SUCCESS/CONFIRMED 且 Core COMPLETED。原 Execution 输出保存身份不变，重复 Run 不增加请求，四种窗口均保持原 Charged=2。

子进程探针配置仅放在临时目录的 0600 文件中，传递临时本地令牌及测试地址，不输出令牌或将其归档。故障注入仅包裹 Fetch 返回和 Outcome 提交窗口，HTTP、授权、Core、Execution、SDK、Content 及账本均为实际实现。首次进程测试直接通过，归档 `evidence/21-fetch-process-first.log`；扩展输出身份断言时修正了 SDK 不暴露内部 OutputOperation 的测试误用，改为 Execution.GetInvocation 前后核对，未修改协议。

完整增强验证及相关定向 race 通过，见 `evidence/21-fetch-process-green.log`；vet/diff 检查通过。21 尚需 Brain/Task Context、公共联网/固定重放、CLI、正式配置约束收口及最终全仓/双轴验收，仍保持 in-progress。

## CLI 入口与首次公共联网尝试

新增 `cmd/fetchcheck` 的 local-task-v1 和显式 public-https-v1。使用说明及范围见 `21-fetchcheck-guide.md`。本地 CLI 四项通过，归档 `evidence/21-fetch-cli-local.json`；这四项不覆盖所有 Go 用例，也不是固定资料重放。定向 race、vet/diff 通过，日志为 `evidence/21-fetch-cli-tests.log`。

公网路径以精确 DNS 地址范围、一次请求、16 KiB/5 秒上限调用本票实际 HTTP Adapter，成功还须经真实 Content 保存/读取核对。2026-09-12 的首次尝试在解析 example.com 时失败，实际 HTTP Requests=0，明确报告 Passed=false/Unavailable；独立系统解析检查也失败。原始报告 `evidence/21-fetch-cli-public.json` 保留不改写。随后增加阶段字段和省略未获取时间的报告格式，但没有伪造新的公网结果。未读取 .env、未调用模型、未使用网页工具或离线结果替代公网获取。

21 仍为 in-progress。公共来源成功证据尚缺，继续推进不依赖公网的 Brain/Task Context、固定资料重放和配置收口，最终仍需整票全仓验证与双轴审查。

## 正式获取驱动强制配置 Core Guard

fetchexecution.New 现在拒绝缺少 TaskGuard 的配置，Start 无可跳过的 Guard 分支。这取代前文阶段性“独立低层夹具可省略”的状态：所有 Execution 获取驱动必须配置当前任务资格检查；独立 HTTP Adapter 的公网观测入口仍是明确的低层验证，不代表任务执行入口。

新增真实组件构造反例，初始实现接受 nil Guard，收紧后返回 Invalid 且无驱动实例。原 HTTP/Content 组合用例中手工构造任务身份的驱动片段迁移到 fetchcheck：先创建真实 Core 任务及资格，通过 SDK 接纳调用，再在受 Guard 约束的驱动边界验证输入尺寸/次数/时间上限、额外字段、重复键、原操作身份、无 Input 的 Inspect 恢复及替换 InputRef 拒绝。仍核对实际两跳 HTTP 请求及重放不新增请求，未移除这些负例以迁就配置收紧。

证据见 `evidence/21-fetch-required-guard-{red,green}.log`。相关 Core/SDK/真实进程/HTTP 定向 race、vet、diff 检查通过。21 仍为 in-progress，Brain/Task Context、固定资料重放、公共联网成功证据及最终全仓/双轴审查待完成；前次 DNS 失败不阻止继续本地工作。

## 获取证据到 Brain 输入的受控投影

新增 `fetchcontext.Context` 实现 Brain.Context，同时可供已有 taskcontext.Facts 的 Content 接口消费。宿主固定原任务事实、处理位置和最多 8 个证据引用；任务事实指纹覆盖目标、约束、输入、主体及资源，不接受网页选择引用或替换任务目标。当前 Core 决策资格及上下文保存策略仍须由外层 Facts/ContextAssembler/Session 检查，本投影不替代它们。

Assemble 经 Evidence.Read 当前授权读取正文，将 acquired 状态、原文字节的文本表示、请求/最终 URL、媒体类型、摘要、实际获取时间和完整来源链放入 external-evidence 块。块 Subject 表示当前任务归属，不声称网页由用户撰写。目标和约束来自绑定的 Core 事实。拒绝非 UTF-8 正文，不做内容修复；序列化后的完整输入受 Brain.MaxInputBytes 和本次更小上限限制，超限不静默截断，任何来源失败不返回部分块。交付前再次检查所有受控证据；没有长期 Memory 写入接口。

真实 Core/SDK 获取一段含越权指令文字的 HTTP 正文后，验证投影保留原文本及来源时间、角色明确为外部证据、任务目标未变、授权策略版本未变。另验证超小预算、替换目标和来源撤权均不能交付材料。该测试验证输入构造及权限效果，不是模型抗注入质量评测，也没有调用模型或证明完整 Brain 决策已完成。

证据为 `evidence/21-fetch-context-{red,green,final}.log`。相关定向 race、最终新增 UTF-8 检查后的目标用例及 vet/diff 检查通过。下一步仍需正式 ContextAssembler/Brain 接线、固定资料重放、公共联网成功记录及全仓/双轴验收；21 保持 in-progress。

## 真实 Core 决策的 ContextAssembler/Session 接线

新增集成用例先通过 Core/SDK 获取并完成任务，再提交引用同一受控证据的上下文消费任务。该任务通过真实 WorkPort claim/start、GenerationPort.ReserveDecision 获得决策资格，将 fetchcontext 投影接入 taskcontext.Facts、SQLite ContextAssembler 和 Brain Context Session。未直接构造假 Core 资格，也未向模型发出请求。

ContextAssembler 增加显式 DisabledMemories 配置，用于只使用任务事实的宿主：任何 Memory 候选（包括可选候选）都返回 Denied，不提供未经授权的缺失元数据。不会把网页证据塞入 Memory 通道或建立 Memory 写入服务；默认接口的其他 Memory 实现行为不变。

测试核对同一决策的上下文在 SQLite 关闭重开后保持一致，受控来源撤权后旧快照不能交付输入；快照的原事实记录不复制网页正文。额外 Memory 候选被拒绝，ModelUsedRequests 保持 0。网页正文仍由原 Content 的使用和保留权限管理；快照事实摘要及决策元数据的生命周期继续服从现有 Context 管理边界，本用例未新增全局后台清理机制。

证据见 `evidence/21-fetch-session-{red,green,regression}.log`。ContextAssembler、Task Context 及全部 fetchcheck 定向 race 回归通过，vet/diff 通过。当前完成 Brain.Context 会话集成，尚未证明 Brain.Decide 的模型/动作闭环；固定资料重放、公网成功记录和最终全仓/双轴验收亦未完成，21 保持 in-progress。

## Brain 生成、受控答案与 Core 发布闭环

新增 Brain.AnswerBrain 集成验证，前置仍为真实 HTTP 获取、Core/SDK、受控 Content、真实决策预留、Facts/ContextAssembler/Session。模型边界使用显式命名的 local-protocol-fixture，返回固定 schema 答案并引用本次真实证据块；它只用于协议与撤权窗口验证，不是语言模型、网络模型调用或效果评测。内部 Generation 用量按夹具报告结算，外部模型请求为零。

正常路径调用真实 answers.ContentAccess 保存派生答案，核对原获取引用、网页来源依赖及受限保留时间，经 GenerationPort.PreparePublication 与 Core Commit 完成消费任务。用户目标的 task-goal 来源与 web 来源分别登记，避免把用户目标误标成网页内容。保存后的答案仍受原网页来源权限约束。

反例在 Generate 返回前撤销来源，Brain 的生成后上下文复查阻止保存和发布；恢复夹具权限后按原 OutputOperation 查询，未发现保存结果。正常路径在完成后撤权，派生答案 GET 被拒绝。两条路径均通过真实 Core 结算原决策预留，ModelUsedRequests=1、ModelReservedRequests=0；该一次为本地接口夹具调用，不能计入真实模型质量或付费调用记录。

首次已有实现直接通过，证据 `evidence/21-fetch-brain-first.log`；增加保存后答案引用核对后，Brain、Task Context、fetchcheck 定向 race 回归通过，见 `evidence/21-fetch-brain-regression.log`，vet/diff 通过。此处验证获取后的答案消费闭环；尚未作为“Brain 自主选择获取 API”的独立证据。固定资料重放、公共联网成功记录、必要集成收口和最终全仓/双轴验收仍未完成，21 保持 in-progress。

## 动态获取来源进入 Execution 外层产物

新增 fetchoutput Content Adapter，作为正式获取宿主的 Execution 内容出口；普通输入读取委托既有 executioncontent。保存成功获取结果时严格解析 status/reference，分别读取受控输入和获取证据当前元数据，合并两者全部有版本来源，拒绝同一来源的版本冲突，最多 16 个来源。保留截止取输入与证据的较短期限；来源不可用或无权访问时不能借外层包装保存结果。

外层 AcquiredAt 使用两项不可变来源获取时间的较晚者，表达其材料获取时间；不将保存重放的当前时钟当作新的获取事实。原 OutputOperation 重放因此保持同一语义载荷，未另造保存身份。当前时间和来源权限仍由 Content PUT 判定，不能因保留旧来源时间绕过已到期条件。

真实 Core/SDK 反例只在输入中列出 start 来源，实际获取重定向到 final。红灯观察到旧外层 Adapter 丢失 final；新出口保存的产物同时继承 final，撤销 final 而保留 start 时，外层 GET 被拒绝。推进共享时钟后重放原外层保存，仍返回同一引用。该机制取代前文仅靠参考输入预列全部来源的阶段性限制。

证据为 `evidence/21-fetch-output-lineage-{red,green,replay}.log`。全部 fetchcheck 定向 race、补充保存重放用例、vet/diff 检查通过。21 仍为 in-progress，固定资料重放、公网成功记录、获取动作选择集成及最终全仓/双轴验收继续保留。

## 显式固定资料重放适配器

新增 replayfetch，宿主固定最多 16 项 UTF-8 资料及其 SHA-256，构造时核验摘要并复制内容；实现没有 HTTP 客户端或联网回退。读取仍检查原任务资格、来源使用与交付授权，并受原输入字节和时间上限约束。后续通过相同 Acquirer、Content、Execution、SDK 和 Core 路径完成。

Result、持久化 Outcome、上下文投影和外层执行证据传递 mode。真实 HTTP 标为 http；固定资料标为 fixed-replay，HTTPStatus 和实际 Requests 均为 0。FetchedAt 在该模式下仅表示本次本地读取时间，不代表原站点获取时间。旧记录的空 mode 保持原 HTTP 语义。任务预算仍保存原有界预留，不因离线读取退还。

固定资料通过真实 Core/SDK 任务验证，检查受控内容、外层模式标记、零 HTTP 请求、预算预留和 Core 完成。外层模式反例先因标记缺失失败，补齐后通过。CLI 新增独立 fixed-replay-v1 profile，报告 replayfetch-v1 及零 HTTP 请求检查；不将其计入公网成功证据。

相关 race 回归和 vet 通过；证据见 `evidence/21-fetch-fixed-replay-{red,green,final}.log` 及 `evidence/21-fetch-cli-fixed-replay.json`。21 继续保持 in-progress，公网成功记录、获取动作选择集成及最终全仓/双轴审查仍待完成。

## ActionBrain 选择获取能力的正式调用链

新增真实 SQLite Catalog、catalogauth 当前权限、ActionBrain、ActionPort 与既有获取宿主的集成用例。目录登记精确 web.fetch 描述，模型接口夹具从本次获准候选及受控任务输入生成动作；Core 记录、接纳并分派原操作，经 SDK/Execution 取得实际 HTTP 正文。目标满足判断重新读取已确认的受控获取证据，不采用模型的完成声明。完成后再次运行不增加模型或网络请求。

集成暴露 Core 对所有 Action 强制 ControlVersion 非零，与 Execution 未绑定共享资源控制时要求零值矛盾。现允许 Core 原样保留宿主的零值，精确操作绑定仍包含它；Execution 依据实际资源绑定验证：无共享控制只能为零，已有共享控制仍要求当前版本与 APPLIED 状态。获取动作标为非写操作，不建立虚假的共享资源控制器。

新增模型将 max_bytes 提升到 2048 的反例：正式 Schema 拒绝提案，任务进入等待，无获取请求，已发生的模型接口调用仍结算为一次。模型是 local-fetch-action-protocol-fixture，仅证明协议接线，不是实际模型效果证据。任务输入和输出使用真实 Content；本测试装配不宣称覆盖全套多轮上下文与目录变更恢复，这些仍由相应专项验证覆盖。

公网再次通过明确的 public-https-v1 入口观测，仍在 DNS resolve 阶段 unavailable，实际 HTTP 请求数为零。原始记录独立保存在 `evidence/21-fetch-cli-public-recheck.json`，不覆盖先前记录，也不写成联网成功。

全仓验证脚本新增 fetch_local 与 fetch_replay 两个独立阶段，分别执行实际 loopback HTTP 与固定资料 profile；公共 HTTPS 保持显式观测。最终全仓运行和双轴审查尚未完成，21 继续为 in-progress。

## 初次审查三项 Spec 问题的修复

预算不足时 SQLite Begin 现在在同一事务保存原 intent 和零请求 limit_exceeded outcome，既不提高 charged，也不退还先前预留。首次仍返回 LimitExceeded，后续按原身份核对；同一拒绝操作不能通过缩小参数改写，确需新动作必须使用新身份。已有 512 条全局容量上限仍生效，容量不足或提交不确定不能伪称持久成功。SQLite 重开及并发预算测试通过；真实 ActionBrain 批次前两次获取消耗原预算，第三次经 SDK 查询为 NOT_STARTED/FAILURE/NOT_OCCURRED，并能读到 limit_exceeded 受控事实，重放不新增 HTTP。

获取驱动为有限失败返回明确的 status 与空 acquired reference。Execution 允许失败携带受 Schema 和 Content 检查的诊断输出，保留 FAILURE；无输出失败继续沿用原处理。fetchoutput 对失败只继承受控输入来源与保留期限，不制造成功正文或动态来源。SDK 查询得到外层受控引用，其内容包含有限状态、mode 和实际请求数；已知 403/404/410 可区分，失败正文和 URL 不进入产物。过期成功证据的核对可表达 expired，但不恢复旧引用或旧正文。读取/保留不获准时仍不能交付该诊断产物。

fetchoutput.FailureReader 绑定宿主身份、用途和位置，当前授权读取 artifact 后严格解析有限失败事实，交付前复核来源。fetchcontext.NewWithFailures 支持宿主选定的成功证据和失败引用，总计最多 8 项；失败投影为 external-evidence-gap，保留受控引用、状态、mode 和请求数，不携带猜测的正文或原站点获取时间。任何引用读取失败或撤权仍整体拒绝，不能借“缺口”分支泄露原来无权知道的事实。未知执行继续通过 Core 原核对状态表达，不把它伪造为已取得的 Content。

新增 SDK 失败读取与 Context 缺口反例先失败，修复后通过；撤销源权限后上下文拒绝该失败产物。证据见 `21-fetch-budget-rejection-{red,green}.log`、`21-fetch-budget-sdk.log`、`21-fetch-visible-failure-{red,first-failed,final}.log`、`21-fetch-context-gap-{red,green}.log`。首次失败输出实现还暴露 Execution 只接受无 Output 的失败，已修复并保留该真实失败记录。

Execution 与获取相关 race 回归、vet 通过。先前目录全包运行因默认 10 分钟包级时限退出，当时子用例仅运行 20 秒；保留 `21-fetch-action-regression.log` 为失败记录。采用 45 分钟包级时限的动作定向回归通过，见 `21-fetch-action-focused-regression.log`。完整全仓验收和修复后的双轴复核仍待完成；上述证据均位于本目录的 evidence 子目录，21 保持 in-progress。

## 最终交付核验

上述运行中状态为历史记录。全仓 `make verify` 会话 83654 已退出 0，28 个最终阶段及各 profile 必需用例通过。最终报告见 [21-final-verify](evidence/21-final-verify/README.md)，完整范围见 [验收核对](21-acceptance-audit.md)。Spec 无未解决具体缺陷；Standards 的一项可选测试去重建议保留。公共 HTTPS DNS 失败与早期目录回归超时记录均保留；不声明公网成功或真实模型质量通过。
