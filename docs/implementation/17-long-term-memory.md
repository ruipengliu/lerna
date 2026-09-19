# 17 票长期记忆：已完成

最终代码候选 9adc8f6，完整 make verify 通过，本票 22 项、累计 1474 项必需检查通过。固定审查基点 0fda52b；双轴审查和证据已归档。以下保留实施过程及边界，最终结论见文末“最终验收”。

## 当前存储切片

消费方 memory 定义 Ref、Revision、Change、Receipt 和 Store。Ref 包含 namespace、collection 和 record key；Revision 的编码内容只接受未来受信 Memory 服务在验证 Schema、来源和驻留后提交。Store 本身不是外部插件访问入口，不提供授权保证。Receipt 无正文，后续披露必须重新检查当前政策。

SQLite Adapter 使用真实数据库、WAL 与 synchronous FULL。Commit 在同一事务内检查预期修订、保存不可变记录修订、原操作回执与集合变更位置。先取得写锁再读取当前版本，两个连接竞争同一修订最多一方成功；失败不留下操作回执。相同操作身份按原语义和原正文核对，已纠正后仍可恢复原提交回执。Commit 的不可用错误必须视为可能未知，先 LookupOperation 核对。

Read 只读精确修订，缺失不以最新值替代。ReadChanges 按集合位置进行有界读取，是可信内部接口，不是获准同步视图。数据库最多 512 份修订，每份文档最多 16384 字节；一次调用最多 2 秒，SQLite busy timeout 为 1 秒，达到容量明确拒绝，不自动驱逐回执。文件要求普通 0600 文件，父目录需由宿主在获准位置管理。

## 已运行

第一个公开 Store 测试在 Memory 包不存在时失败，实施后通过。真实 SQLite 关闭重开验证记录、回执、变更位置和重复操作一致；双连接竞争纠正验证恰好一次成功、另一方版本冲突、历史修订可精确读取、缺失修订明确不可用、原身份改义拒绝、失败纠正无回执。

```sh
go test -race ./memory ./adapters/memory/sqlite
go vet ./memory ./adapters/memory/sqlite
```

以上定向命令通过，日志为 build/17-store-red.log、17-store-green.log、17-store-concurrency.log 和 17-store-vet.log。未运行本票完整 make verify，不用 16 票的旧全量证据宣称本候选通过。

## 记录与受控写入检查点

MemoryRef、MemorySource、MemoryConfidence、MemorySpec、MemoryRecord 与 MemoryWrite 已加入 Protobuf；动态内容使用已登记 Schema 的精确身份/版本/摘要。时间的缺失使用 optional 表达，置信判断保留依据及方法。Service.Put/Correct 保留完整来源和前修订关系，对固定元数据、未知字段、动态内容和大小进行校验，动态 JSON 与落盘 Proto JSON 规范化以保持跨重试比较稳定。

Memory Authority 是消费方接口。参考 memoryauth Adapter 依赖当前 Harness ViewActions 和来源策略接口；受信集合配置绑定真实授权资源、策略引用、用途、允许 Schema 和存储/处理/披露位置。构造时复制配置，调用方后续修改原集合不能改变 Adapter 的授权范围。写入与纠正检查新内容及旧修订的当前限制，落盘前再校验；回执披露单独检查 discover。并不宣称独立授权库和 MemoryStore 具有跨库原子撤销。

实际 Harness policy + continuous Grant 集成测试验证端侧获准、云侧拒绝、策略改写拒绝、来源修订不匹配拒绝和政策撤销；来源接口在该测试中是限定一个公开来源的确定性夹具，不冒充完整产物来源服务。四种记忆的服务测试验证保存、纠正与原提交恢复；额外测试确认字段/Schema/策略/留存错误和撤销不会产生新修订。

## 读取结果绑定存储检查点

DisclosureStore.BindRead 在独立持久表中绑定 namespace、读取身份、主体、语义摘要、许可身份、覆盖状态及精确结果修订。相同身份只能恢复原选择；不同语义/许可明确拒绝，即使新的候选集不同也返回原集合。最多 512 份绑定、每份 32 个结果，文档最多 16384 字节；没有正文副本，结果引用必须在事务内存在。

真实 SQLite 测试先绑定修订 1，再纠正为修订 2、关闭重开；重试仍返回修订 1。空结果绑定后不能变为有命中。该接口是受信内部存储，不是签名单次授权的替代品；尚未提供绕过授权的 Get 或 Query。

以上新切片分别经历失败测试和实现通过，日志为 build/17-service-red.log、17-service-green.log、17-auth-red.log、17-auth-green.log、17-read-binding-red.log、17-read-binding-green.log。最新完整相关包 race/vet 与全包 build 通过，见 build/17-checkpoint-tests.log、17-checkpoint-vet.log、17-checkpoint-build.log。没有执行本票最终全量验证。

## 有界读取与真实许可检查点

Reader.Query 在已获准的集合范围内取得 SQLite 一次有界快照；Store 最多扫描 512 个当前记录，SQL 的同一次读取不拼接不同快照。当前版本按记录键排列，旧修订不参加普通查询；类型与描述对象精确过滤，文本以内容 JSON 的不区分大小写子串匹配，未声称语义召回。最多 32 个结果，查询文本最多 1024 字节，max_bytes 限制编码记录字段（1–65536 字节），固定覆盖元数据另行有界。单次 Service 截止时间继续约束全部步骤。

逐记录先检查 discover，再检查 read/处理/披露与用途，最后应用匹配条件和可见结果预算；隐藏记录不计入结果预算或覆盖原因。可见来源暂不可用与预算不足分别返回 partial_unavailable、budget_exhausted，同时出现时为 partial_and_budget_exhausted。完整零命中为 complete + 空结果。来源端口只允许把已获准但不可用的操作标为 Unavailable；隐藏来源仍是 Denied。Get 严格读取指定修订，首次缺失采用不披露存在性的拒绝结果。

首次读出正文前调用 BindRead；相同身份后续走原持久结果，空结果也固定。release 逐个恢复精确修订，并重新检查当前政策、来源、用途、留存与注册 Schema；无法恢复返回 READ_IRRECOVERABLE，不重新查询填充。请求语义、主体、用途、接收位置和处理位置共同进入签名意图摘要；DescribeQuery/DescribeGet 提供签发方可复现的描述，不授予权限。

memoryauth.ReadPermits 将集合读取动作映射到真实 Harness policy 和签名 GrantAuthority。ReserveUse 持久预留一个单位，Validate 复核当前签名链、原预留、动作摘要和许可身份；未知额度不回收。Peer 绑定由受信宿主传入，不能从普通请求或签名载荷自选。该测试使用模拟的已认证 peer 元数据，不冒充本票完成了跨端双向 TLS。

真实 P-256/JWS 签名、独立授权 SQLite、记忆 SQLite 与 Query 的集成通过：读取和重复读取成功，签名 Grant 分配量保持 1，改义被拒，政策撤销后旧查询不能再披露。内核测试还验证纠正后的原修订重放、精确历史 Get、原空结果不可扩大、隐藏/不可达/预算覆盖。首次签发集成因测试漏传授权修订真实失败，修复后通过；覆盖组合与来源错误映射也分别有失败再修复的证据。

最新相关包 race/vet 和全包 build 通过。日志包括 build/17-scan-red.log、17-scan-green.log、17-query-red.log、17-query-green.log、17-read-permit-red.log、17-read-permit-integration-failure.log、17-read-permit-green.log、17-signed-query-tests.log、17-coverage-red.log、17-coverage-green.log、17-source-coverage-red.log，以及 17-reader-checkpoint-tests/vet/build.log。未运行最终全量验证。

## 变更操作接纳与未知结果检查点

授权端 ReserveMemoryOperation 在当前 policy + Grant 检查下预留 Memory 变更身份、语义摘要、动作和固定窗口截止时间；重复预留不延长期限。窗口过期/关闭后不能新提交原变更。最多保留 512 份预留，不通过驱逐旧身份允许复活；这些不是业务回执，记忆正文和正式修订仍由独立 MemoryStore 管理。

该身份在任务、执行、内容、授权管理及签名许可领域互斥，原领域保留占用。Memory 服务先检查当前权限并核对已有 Store 回执，已提交的原语义重试不依赖一个新窗口；未提交才取得预留，并将剩余期限加入提交 context。授权库与记忆库不具有跨库原子撤销保证；预留有效性在准入时判断，事务回包未知必须继续查询原回执。

InspectOperation 先核对当前 MemoryStore：已提交时返回经当前权限检查的原回执；确无预留且窗口开放为 not_admitted；从未预留但窗口关闭为 admission_expired。授权端已经预留、记忆库尚无回执时返回 unknown，包括预留后来过期的情况，因为不能把可能仍在提交的请求错误判成从未发生。未知状态不生成新的业务身份。

真实授权 SQLite 重开验证预留、固定截止时间、语义冲突及窗口过期；管理/内容/任务接口拒绝复用 Memory 身份。服务测试证明接纳过期后新写入被拒而旧提交仍可核对，实际记忆 SQLite 提交成功但回包丢失时 InspectOperation 返回原提交，同操作重试只有一个变更位置。接口故障包装器只注入丢回包，没有替代持久 Adapter。

通过的相关 race 回归包括 authorization、tasks、artifacts、profiles/grants、memory、memoryauth 和 sqlitememory；execution 包编译通过（无独立测试）。相关 vet 与全包 build 通过。日志为 build/17-admission-red.log、17-admission-green.log、17-admission-service-red.log、17-admission-service-green.log、17-admission-checkpoint-tests/vet/build.log；最终完整 make verify 仍未运行。

## 正式 SDK 入口与共同存储契约检查点

MemoryRequest/MemoryResponse、MemoryReceipt 与 MemoryOperationState 已加入固定 Protobuf，并登记 MemoryService 描述符。sdk.MemoryClient.Exchange 接受 PUT、CORRECT、QUERY、GET 和 LOOKUP；每次生成新的消息尝试身份，保持原变更/读取身份。参考 memorylocal Transport 在 Protobuf 字节边界外绑定实际主体、凭据、处理位置和接收位置，不让普通请求自行选择这些身份。

请求限制 65536 字节、签名材料 32768 字节，响应最多 131072 字节。SDK 拒绝混合请求、响应关联或 namespace 错误、失败响应携带正文、未知错误码、读取扩量、历史 Get 被最新修订替代等情况；服务错误经固定代码投影，不输出内部诊断。该入口是可运行的进程内 Transport，不宣称已交付 gRPC/WebSocket 网络服务。

宿主组装后的调用形式如下（service、reader 和 binding 来自已授权宿主配置）：

```go
client := sdk.NewMemoryClient(memorylocal.Bind(service, reader, binding), binding.Namespace)
response, err := client.Exchange(ctx, &wire.MemoryRequest{
    Method: "PUT",
    Write: &wire.MemoryWrite{OperationId: operationID, Ref: ref, Spec: spec},
})
```

operationID 使用当前授权端 NewOperation 取得；QUERY/GET 另提供与 DescribeQuery/DescribeGet 摘要匹配的真实签名材料，不能把描述接口当授权。既有授权 SDK 可负责签发。可重复运行的完整 SDK 集成入口：

```sh
go test -race ./adapters/memory/auth -run TestCurrentHarnessPolicyAndResidencyConstrainMemory -count=1 -v
go test -race ./sdk -run TestMemoryClient -count=1 -v
```

第一项实际经过 SDK 保存、纠正、签名查询和操作核对；纠正后同一签名查询仍返回原修订，授权端分配量保持 1，撤销后正文不能重放。测试使用真实授权/记忆 SQLite 与 P-256/JWS，不使用用户数据或外部服务。

memory/storecontract 提供可替换 Factory 的共同检查，每次必须创建空的可丢弃 Store，单项期限为 10 秒。五项覆盖原子历史/变更位置、原身份及版本冲突、竞争纠正、固定/空披露绑定、取消提交。SQLite 已接入并通过；这不等于已有第二耐久后端，也不能替代进程退出证明。

最新 SDK、Memory、授权 Adapter 与 SQLite 包 race 测试、相关 vet 和全包 build 通过。日志为 build/17-sdk-red.log、17-sdk-green.log、17-sdk-boundary-tests.log、17-sdk-complete-path-tests.log、17-store-contract-red.log、17-store-contract-green.log、17-sdk-contract-checkpoint-tests/vet/build.log。共同套件初次编译出现清理函数名遮蔽 close 的错误，修复后通过，失败日志保留在 17-store-contract-compile-failure.log。

## 具名 profile 与真实进程检查点

long-term-memory-v1 已登记到 contractcheck 和 make verify，当前 22 项：12 项真实 SDK/授权/SQLite 行为、5 项共同 Store 契约、5 项实际子进程退出恢复。四类记录、Schema 字段拒绝、驻留拒绝、撤销、空结果绑定、预算、精确历史、竞争读取和无效操作均有独立用例。

子进程分别在 MemoryStore 提交前/后、签名读取预留后、结果绑定前/后退出，父进程验证退出码 73 后重新打开两个数据库。提交前状态为 unknown，提交后为 committed；同操作恢复只出现一个正式修订和变更位置。读绑定前退出允许在首次披露前继续选择；绑定后退出则即使记忆已纠正也必须返回原修订。两次后续读取保持相同结果，签名授权端 Allocated 为 1。

相关测试通过，日志为 build/17-process-red.log、17-process-green.log 和 17-profile-tests.log。报告入口为 `go run ./cmd/contractcheck -profile long-term-memory-v1`。这不是最终全量验收结论，仍需候选版本具名运行、双轴审查及完整 make verify。

## 完成边界

候选具名运行、最终全量验证和双轴审查已完成。上下文组装/真实模型个性化效果归 18，删除传播和派生失效归 19，但不省略本票基础权限及持久语义。

## 审查修复

双轴审查发现并修复两项规格问题。覆盖标志现在为每类原因保存至多一个原修订见证，SQLite 原子绑定并校验存在性；每次释放结果同时复核见证的 discover/read 权限，获准但暂时不可用仅可支持 partial 见证。缺失见证的旧绑定拒绝恢复，不能重新查询或泄露已撤权的覆盖事实。

LOOKUP 将历史 committed 与 content_availability（available/unavailable）分别返回。最小收据依据当前授权端的原操作权限与主体检查，正文缺失、保留期限过期或来源不可用不抹掉已提交事实。正文状态仅表示当前策略及 Schema 下的可用性，不返回正文，也不替代单次读取授权。未提交状态不携带正文可用性字段。

新增先失败后通过的撤权覆盖重放及过期/缺失正文核对回归；memory、SQLite、授权 Adapter、SDK、具名 profile 的 race 测试通过，见 build/17-coverage-review-{red,green}.log、17-status-review-red.log、17-review-fixes-tests.log。最终全量验收仍待完成。

完整 make verify 首次运行在 CLI 报告路由失败：长期记忆分支被后续独立 if/else 覆盖为 unsupported，虽然包内 22 项测试通过，但独立候选报告实际未通过。修复为同一互斥分支链，并将 long-term-memory-v1 纳入实际编译 CLI 的回归。先前“已登记”描述仅表示接线意图，不能替代本次发现的入口失败；保留 build/17-first-full-verify-failed.log、17-first-full-stages-failed.json、17-first-memory-report-failed.json 和 17-cli-red.log。修复后的完整验证结果另记。


## 最终验收

代码候选 `9adc8f69c1fe2260be69fd036ffdc92a3bbd92a1`，完整 `make verify` 通过：依赖、生成一致性、编译、vet、race 测试及全部具名入口通过。长期记忆 22/22，累计 1474 项必需检查通过，含可选共 1479 项。报告内 candidate 一致、dirty=false。可选未运行/未实现项不计作通过，不宣称完整系统验收或真实个性化效果。

[长期记忆报告](evidence/17-long-term-memory-report.json)、[验证阶段](evidence/17-verification-stages.json)、[完整日志](evidence/17-final-verify.log)、[首次失败](evidence/17-first-memory-report-failed.json)、[CLI 修复回归](evidence/17-cli-green.log)、[双轴审查](17-long-term-memory-review.md)、[证据 SHA-256](evidence/17-evidence-sha256.json)。开发日志和既有能力回归报告同目录归档；真实进程、接口故障注入和单元测试的证据类型分别保留。
