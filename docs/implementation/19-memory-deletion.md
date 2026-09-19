# 19：记录删除与派生内容失效

原范围见 [19 票 Agent Brief](../../.scratch/harness-implementation/issues/19-deletion.md)。当前实施中，最终审查基点 `fee7662`。18 的真实回答验收未完成，本票不能替代它。

## 原子存储切片

新增受信消费方 DeletionStore，暂未暴露未经授权的 SDK/网络删除入口。SQLite 在同一写事务内检查预期修订、删除全部保留修订、写入最小墓碑、清除旧操作的载荷比较摘要，并记录新的删除操作及集合位置。未知提交沿原 LookupOperation 核对。

Head 保留删除修订；旧正文 Read 返回不可用，Scan 不再返回该记录。相同删除操作重放返回原结果，旧写入载荷因比较依据已清理返回 OPERATION_RESULT_ONLY，提示后续 Service/SDK 查询原状态。新 Put/Correct 不能复用被删除的记录身份。墓碑不自动回收，纳入既有 512 项容量上限，达到容量明确拒绝。

该切片只证明受信 Store 的逻辑删除与耐久元数据。尚未证明 SQLite 空闲页/WAL/备份的正文清理，不据此报告“物理清理完成”。受控授权、删除回执协议、派生清理及备份隔离仍待实现。

真实 SQLite 测试验证删除后重开、历史读取、扫描、原操作状态、变更位置及原身份拒绝复活；双连接竞争验证 Delete/Correct 只有一方提交，失败方为版本冲突。`go test -race ./adapters/memory/sqlite ./memory -count=1` 通过。证据：[初始失败](evidence/19-delete-store-red.log)、[存储切片](evidence/19-delete-store-green.log)、[race](evidence/19-delete-store-race.log)。

## 后续交付

- 当前授权下的 Memory.Delete、原删除及已清理历史操作状态、协议/SDK/CLI；不得为状态披露保留正文。
- 当前读取、原绑定读取、上下文和派生产物的失效；用途收紧与混合来源治理。
- 有界可恢复清理、通知/消费者位置和四维完成度；实际进程退出证明。
- 旧备份隔离与可信删除水位核验；没有接入的远端、归档、外部披露明确不在已完成范围。
- 具名统一验收、双轴审查及最终完整验证。

18 的 make verify 使用启动时冻结的验证二进制继续运行；19 的变更有独立定向验证，不能并入18的冻结代码验证结论。

相关 vet、全包 build 和 diff 检查通过。19 整票完整验证与最终审查尚未运行。


## 当前授权与协议/SDK 删除

Memory.Service 的 Delete 通过独立 DeletionAuthority 核对当前主体、命名空间、集合的受信资源、用途及实际位置，使用 memory.delete 权限。删除不要求重新获准读取/保存已过期正文，也不接受调用者选择策略。原操作身份由现有授权侧准入预留，业务提交沿原 Store；当前权限在提交前及披露回执前再次检查。

MemoryDelete 协议携带 operation_id、ref、expected_revision、purpose，MemoryRequest 使用新增字段 9；旧字段号保留。SDK/本地绑定严格隔离请求分支，新行为需宿主实现删除端口，未实现明确不可用。LOOKUP 对已清理正文允许空比较摘要且仅在内容 unavailable 时接受；PUT/CORRECT/DELETE 成功回执仍要求完整摘要。OPERATION_RESULT_ONLY 已映射为明确领域错误。

真实授权测试先移除 memory.delete 权限并证明拒绝，恢复权限后通过 SDK 删除；不同用途拒绝，原删除重放保持回执。原写入 LOOKUP 不释放正文/比较摘要；SDK 和 Service 补交旧载荷均拒绝。再次撤权后删除回执和操作状态不可披露。

五个包完整 race 通过：Memory、SQLite Memory、MemoryAuth、Authorization、SDK；相关 vet、全包 build 和 diff 检查通过。证据：[服务失败基线](evidence/19-delete-service-red.log)、[服务验证](evidence/19-delete-service-green.log)、[SDK失败基线](evidence/19-delete-sdk-red.log)、[SDK验证](evidence/19-delete-sdk-green.log)、[race](evidence/19-delete-sdk-race.log)。

本次仍不等同于全部删除清理完成：MemoryStore 已移除正文和旧比较摘要，但授权侧原准入、其他派生存储及备份的元数据清理/保留策略尚需联动核查。四维完成度、可恢复消费者清理、备份隔离及统一 CLI 删除验收继续待办。19 最终全量验证和双轴审查尚未执行。


## 受控产物的持久来源失效

新增受信 InvalidateSource，按命名空间、来源类型/键和最高失效修订，在原产物事务中保留失效水位并把依赖记录整体置为 cleaning。最多512条来源水位，不按时间驱逐；重复或较旧通知保持最高水位。普通请求不暴露该维护端口，跨命名空间请求拒绝。

所有后续产物写事务检查水位，旧来源不能被过时适配器重新 PUT。现有 Clean 分批移除正文文件及敏感产物字段，再标记 cleaned。失效通知重放返回当前清理/完成数量，不重复推进生命周期。它是消费者端口，尚需接入 Memory 变更消费位置和四维完成度报告。

真实 SQLite/文件测试覆盖失效后重开、读取拒绝、旧来源重新保存拒绝、实际正文文件删除；混合来源产物整体失效，无关独立产物继续可读。Artifacts 完整包 race 通过（1.696s），相关vet/build/diff通过。证据为19-source-invalidation-{red,green,race}.log。此处不是全系统删除完成，后续消费者、上下文清理和备份隔离仍待办。


## 来源事件与已应用位置

新增受信 SourceEvents，以每集合已提交位置返回 created/corrected/deleted 事件。SQLite 从同一事务产生的原操作和永久墓碑判定事件类型，不依赖已经删除的正文或载荷摘要；每次最多32项。公开字段只有引用、修订、位置和类型，不是普通用户的查询/同步授权接口。

ConsumerProgress 在副作用前绑定命名空间、集合、消费者和有效配置摘要，配置变化拒绝复用旧位置。AckEvent 每次只能推进一个实际存在的相邻事件，重放已确认位置幂等，不能跳过/回退；最多512个消费绑定，不驱逐恢复身份。消费者确认完成之后才应调用AckEvent，Store本身不代替外部效果证明。

真实SQLite删除/重开验证原创建、纠正、删除事件完整且分页隔离；消费者重开保留位置，配置替换、跳号和不存在事件的确认均拒绝。Memory/SQLite完整包race通过（2.129s/2.219s），相关vet/build/diff通过。证据：19-source-events-{red,green}.log、19-consumer-progress-{red,green}.log。

本轮交付事件与耐久进度端口，尚未把消费者副作用绑定到AckEvent。下一步连接实际产物清理及上下文清理，并验证清理成功但确认丢失的进程恢复；用途收紧的通知来源、四维报告和备份隔离仍待实现。

## 产物清理消费者

SourceConsumer 先绑定有效配置和读取持久位置，再处理最多32个连续事件；整批元数据校验先于副作用。宿主固定消费方有效配置摘要，每次调用最长10秒。只有 SourceSink 确认自身受控清理完成，才逐事件 AckEvent；待清理、失败和未知确认均不在本次结果中提升已确认位置。下一次 Run 先读取实际持久位置，区分需要重放和确认已经提交的情况。

MemoryCleanup 的 Artifacts 适配器复用 ContextMemory 来源编码。创建永久作废此前的缺失声明；纠正作废旧正文修订；删除作废该永久记录身份的全部正文和缺失来源。先持久失效全部受影响来源，再执行一次有界 Clean；复核受影响对象无 cleaning 才确认该消费者完成。该状态只覆盖已接入的产物存储，不代表全系统删除。

真实 Memory 授权、SQLite 事件流和文件产物的集成验证覆盖：创建事件不误删当前正文；删除后先清理文件再确认；确认未提交时持久位置不前移，重放原事件恢复；确认已经提交但响应丢失时，下次先核对位置，不重复处理。故障替身仅作用于确认边界，清理效果和持久状态均使用真实实现；这还不是进程退出证据。

相关完整包 race 通过：Memory 2.186s、SQLite Memory 2.283s、MemoryAuth 9.762s、Artifacts 1.827s；MemoryCleanup 经 MemoryAuth 集成调用覆盖。vet、全包 build 和 diff 检查通过。证据为19-cleanup-consumer-{red,green,race}.log。上下文清理、真实进程恢复、用途收紧通知、四维完成度及备份隔离继续待办，整票最终验证/审查未执行。

## 上下文快照清理与进程恢复

新增 ContextAssembly 的受信来源失效端口。文档依赖解释由 ContextAssembly 负责，SQLite 适配器不自创文档字段。失效按正文来源/缺失声明分别保存包含性修订水位；来源已创建后缺失声明永久失效，纠正失效旧正文修订，删除失效该记录身份的全部修订。Contexts 消费适配器实现通用 SourceSink，可与既有 SourceConsumer/持久进度连接。

SQLite 在同一事务中保存水位、作废全部受影响快照并移除整份文档（包含正文、来源指纹和事实摘要），同时清除快照主体和语义摘要。保留最小决策身份作废标记；旧身份不能重绑，新的决策身份也不能绑定已失效来源。混合快照整体作废，不在原决策中偷偷改写为新上下文。水位最多512项、不驱逐；扫描受快照总容量512和2秒事务期限约束，超时/未知提交不能宣称完成。不可解释文档导致清理失败，不能被误报为无依赖。

实际 Memory/签名读许可/SQLite 上下文集成验证了清理前有快照，清理后不披露正文或摘要、无关无来源快照保留，重开后原决策和新身份的旧来源绑定均被拒绝。独立子进程以公开确定性来源生成真实快照，分别在清理前和完成后 os.Exit(74)，不运行 Close；父进程恢复后核对原状态、重复清理和拒绝旧来源重绑。子进程使用 Contexts 事件适配器，并证明 created 不误删当前正文、deleted 清理后退出不复活。此证据覆盖上下文清理提交边界，不替代 Memory 删除提交或消费确认边界的完整进程验收。

最终相关包 race 通过：ContextAssembly 1.838s、MemoryAuth 9.584s，SQLiteContext 复用前次同代码通过结果；新 Contexts 适配器由真实进程用例覆盖。vet、全包 build、diff检查通过。证据：19-context-erasure-{red,green,race}.log、19-context-erasure-process.log、19-context-sink-{red,race}.log。

这是当前受控数据库的逻辑清理，不是 SQLite 空闲页/WAL/旧备份的物理擦除证明。尚需用途收紧事件、其他运行载荷/授权准入元数据清理、清理批次和确认边界的进程退出、四维完成度、备份隔离以及整票CLI/profile/最终审查。

## 删除事件消费确认的真实退出验证

新增组合进程验收：实际 SQLite Memory 提交创建和删除，SourceConsumer 从真实变更流读取，Contexts 清理实际 SQLite Context，并把应用位置保存回 Memory。公开确定性来源用于构造快照；不替代此前 Memory 授权集成验收。故障注入仅在确认边界执行 os.Exit(75)，不模拟持久状态或清理成功。

三个退出点分别是删除提交后尚未清理、清理返回完成但 AckEvent 尚未提交、AckEvent 已提交但尚未返回。父进程重开两份数据库，核对删除修订2、历史正文不可读、上下文实际清理状态以及已应用位置1/2；恢复只重放未确认的原删除事件，最后全部到位置2，再次运行无新增应用。前两个阶段恢复应用1项，第三个阶段应用0项。该证据不覆盖文件产物清理批次中的退出，也不证明备份已同步。

定向真实进程 race 测试通过（1.650s），证据19-cleanup-ack-process.log；相关 vet 和 diff 检查通过。下一步仍需授权侧旧比较摘要、用途收紧通知及其他运行载荷的清理、四维状态/备份隔离和最终整票验收。

## 授权准入比较摘要清理

Admissions 消费器先由 ErasedOperationStore 核对真实永久删除修订，并取得该记录全部原写入的最小回执，再调用授权维护端口原子清除相应 MemoryAdmission.SemanticSHA256。SQLite 查询把墓碑和原写入放在同一查询快照中验证；未提交/错误删除修订拒绝，摘要必须已由 Memory 删除事务清空，不接受一条调用方事件作为唯一证明。每条记录保留的修订上限512约束此次列表和授权清理批次；调用有界。

授权清理端口检查命名空间、原操作身份、原主体和写入动作类型，不开放普通协议入口，不删除身份占用或当前结果查询所需的原动作。清理后 ReserveMemoryOperation 明确返回 OPERATION_RESULT_ONLY，Memory 适配器映射同名既有错误；不能因为摘要消失而重新接纳旧操作。InspectMemoryOperation 仍执行当前身份/权限检查，Memory 的历史已提交状态与正文不可用分别返回。无关写入准入及删除操作的元数据比较基础不被此清理移除。

真实授权/Memory/SQLite/SourceConsumer 集成覆盖跨命名空间和原主体不符拒绝、删除事件驱动摘要清理、旧载荷重提交拒绝、历史结果继续可查、无关准入保留。真实重开验证摘要保持清空、身份保留、重复清理及重新准入拒绝；Storage 反例验证未提交删除及错误修订不能取得清理凭据。

相关完整包 race 通过：Authorization 3.117s、Memory 2.283s、SQLite Memory 2.412s、MemoryAuth 9.446s；新增重开反例定向race分别1.207s/1.135s。vet、全包 build 和 diff通过。证据：19-admission-cleanup-{red,green,race}.log、19-admission-erasure-reopen.log。当前仍不宣称WAL/备份物理擦除或全部运行载荷清理；用途收紧通知、产物批次退出、四维报告、备份隔离及整票验收继续待办。

## 文件产物清理批次退出

真实授权/SQLite/文件产物先保存17份超过内联阈值的公开测试正文，再把来源失效。子进程使用原存储和配置执行 Clean，仅在第一次实际 Remove 成功后 os.Exit(76)，在该对象 cleaned 状态提交前终止，且不执行关闭或解锁清理。

父进程重开后核对文件剩16份，而17条记录均仍为 cleaning、0条 cleaned；所有原引用读取均拒绝。原批次恢复可处理文件已不存在的未知结果，一次有界 Clean 后16条 cleaned/1条 cleaning、1份正文文件；第二批完成17条且文件归零。这验证清理效果与完成确认分离、真实锁释放和批次边界，不以传输或返回成功替代持久状态。

定向真实进程 race 通过（1.909s），vet及diff通过，证据19-file-cleanup-process.log。

### 用途收紧接入前的边界核查

当前 Authorization.Execute 和签名 GrantAuthority.Mutate 都会推进 State.Revision；普通管理 Changes 可被 CleanRecords 回收，签名授权另有通知结构。ViewActions 的版本还纳入期限边界，但仅反映当前状态。因此不能直接把现有管理回执列表当作完整、永久的用途失效日志，也不能只在消费时重查最新策略：先撤销再恢复可能掩盖中间必须处理的失效。

来源修订水位适合纠正/删除，不适合把某个主体/用途的撤权映射为整条来源永久失效。用途清理必须保留受影响的原使用范围及授权变更依据，避免误删仍获准的其他主体/用途，并与当前使用阻断分开验收。此处仅记录已核查约束；用途收紧通知及其清理消费者仍未实现，不用现有删除事件测试冒充通过。

## 原使用范围的持久失效通知

新增授权内部 UseSpec 登记接口：宿主在保存受控对象之前绑定稳定使用身份、消费方及配置摘要、原资源/动作/用途/位置列表、保留期限。登记检查当前凭证和全部本地动作权限，持久化的只含凭证摘要，不保存明文凭证或对象正文。每个对象最多16个动作，最多512个使用身份；消费方配置不得静默替换，失效/完成身份不回收重用。

授权更新在同一个存储快照内，于变更前处理已到期限的原使用，再于变更后复核受政策版本影响的使用。原使用失去条件时记录第一次观察到失效的修订和时间，随后恢复策略也不能重新激活。无关仍获准用途不失效。期限边界由下一次授权调用/清理轮询推进；没有独立后台时钟承诺，普通使用仍需当前权限校验。

PendingUses 提供最多32项待清理通知，不依赖可回收管理回执；ConfirmUseCleaned 只接受匹配的原通知身份/修订/时间。确认代表宿主已证明实际清理，不能用于仅收到通知的场景。当前实现提供这两个受信端口，尚未连接产物/上下文保存登记和真实清理确认，因此不宣称用途清理已完成。

真实SQLite授权测试覆盖登记重放、两用途并存、撤销assist后恢复策略、重开仍补读首次失效通知、原身份不能重新登记、错误修订确认拒绝、正确确认后离开待处理集合，以及research使用到期形成新的通知。相关完整包race通过：Authorization 4.219s、MemoryAuth 10.198s、Artifacts 3.106s、ContextAssembly 2.948s；vet/build/diff通过。证据19-use-invalidation-{red,green,race}.log。

当前 UseSpec 覆盖本地授权动作条件，不能替代签名读许可链及来源修订检查；签名依赖撤销必须明确接入，不能仅因管理修订变化便声称已覆盖。后续须在实际对象保存前登记、在策略恢复后仍阻止原失效对象释放、把逐对象清理绑定到确认，并为既有未登记对象确定隔离/迁移规则。

## 产物登记、释放阻断与用途清理

ContentTransaction 新增原使用登记、状态读取和实际清理完成端口，均操作同一个授权快照，不嵌套调用存储。产物最终保存事务捕获成功的直接内容授权和来源回调授权检查（去重后最多16项），以 artifact.<key> 绑定原主体、期限和版本化配置摘要，与对象记录原子落盘。来源回调的成功 Authorize 属于原使用必要条件，不能用于探测并忽略可选权限；来源修订和签名许可仍各自独立验证。

每次产物事务先检查原使用状态。已失效对象整体进入 cleaning，移除内联正文；即使策略恢复，读取也不能绕过。原使用证明缺失的旧格式对象不自动补发许可，先置为不可用，只有显式受信 Clean 才移除正文。非空UseID却缺少授权记录视为状态不一致而失败，不伪造新的原使用。文件正文在既有批次中实际移除后，cleaned对象状态和原使用完成标记同事务提交；源删除/显式删除同样结束原使用，不留下可复活登记。

真实产物测试覆盖两种用途并存、撤销assist后恢复并重开、旧assist读取拒绝/research仍可读、实际Clean后旧操作不可重做，而恢复策略下新创建的assist对象正常可用。Memory来源回调的实际memory.disclose授权也被捕获：撤销会自动推进产物生命周期，旧ExpectedRevision=1的手动删除明确版本冲突，随后确认cleaning修订2并完成正文清理。原测试关于撤权后仍处于available的假设已按这一新行为更新，未放宽版本检查。

初次相关回归因此暴露一个旧测试预期失败，保留在19-artifact-purpose-race.log；修正预期后最终包race通过：Authorization缓存通过、Artifacts 4.205s、MemoryAuth 11.315s、Content profile 5.982s。独立Memory定向race9.986s，vet/build/diff通过。证据另含19-artifact-purpose-{red,green,memory,final-race}.log。

上下文原使用登记、签名读许可链依赖、其他运行载荷清理、四维状态、备份隔离和整票最终验收仍待完成；不能将此处产物通过扩大为全系统用途清理完成。

## 原使用的签名许可分配依赖

UseSpec 可携带最多16项原 UsePermit。登记不是重新签发或重新分配：必须与当前授权存储已有分配逐字段相等，原命名空间/主体匹配，分配动作摘要对应此次必要动作之一。调用者改变接收端、呈现者、分配身份或其他已绑定字段不能把已知许可ID伪装成有效依赖。分配副本在异步操作前由服务持有，不保存签名材料或正文。

后续复核重用现有完整授权链检查，包括原叶子及祖先撤销、当前策略、期限、主体和委派约束；下一次期限检查纳入每个祖先及相关主体期限。无需额外签名或扣用量。撤销祖先在同一个授权更新事务内形成原使用的永久失效通知，恢复其他权限不重新激活该身份。未声明签名依赖的纯本地使用仍按其原本地条件判断。

实际JOSE签名/SQLite分配测试覆盖根→子授权、真实用量分配、伪造呈现者拒绝、原分配登记、祖先撤销只影响依赖对象；独立到期分支让签名许可先于本地权限/对象保留期限到期，只有签名依赖对象进入清理。相关完整包race通过：Authorization 4.672s、Artifacts 4.132s、MemoryAuth 10.185s；vet/build/diff通过。证据19-use-grants-{red,green,race}.log。

这一步完成授权登记能力，ContextMemory 尚未把原读分配传入登记，ContextAssembler 也尚未绑定原使用状态；实际上下文用途清理仍未完成。下一步从原 ReadPermits.LookupUse/Verify 恢复精确分配与必要动作，连接保存前登记、重放时失效阻断和逐快照清理确认，不能重新申请读身份。

## 恢复原读登记依据

ReadPermits.OriginalUse 根据既有受信读计划、真实签名Verify和LookupUse返回原UsePermit、必要本地动作及叶子许可期限。它不会调用ReserveUse，未实际分配的签名材料不能被描述为“原使用”。分配必须对应已验证许可、原动作摘要和单次1单位；读计划与输出动作使用同一生成函数，避免后续扩展时漏记读条件。

返回值只是登记依据，不是可离线使用的授权令牌。后续RegisterUse还必须在授权事务里重新核对分配及当前链状态。它不替代记录保留期限、上下文存储位置或来源修订校验。

真实Context/Memory/JOSE分配集成覆盖：首次读前OriginalUse拒绝且Allocated仍0；实际组装后返回原GrantID/ReadID和4个读动作；替换ReadID拒绝、提取后Allocated仍1；撤销许可后拒绝提取。MemoryAuth完整包race10.098s、Authorization缓存通过，最终补充分配前反例及共享动作函数后的定向race10.116s；vet/build/diff通过。证据19-original-read-use-{red,race,final}.log。

上下文保存前登记及逐快照清理消费者仍待实现，不能将这个恢复接口视为已接通完整用途清理。

## 决策身份清理与迟到保存

为上下文用途清理新增受信RetirementStore.Retire：按原决策键原子清除快照正文/比较摘要并永久作废身份。清理先于Bind到达时也持久保留作废标记，不能因当时没有正文便把事件当作无事发生。重开后Read返回CONTEXT_INVALIDATED，原候选迟到Bind被拒绝；已经存在的快照同样被整体清除。

SQLite读取在单一查询快照中同时检查正文和作废标记，Bind/Retire共用写事务锁。只有有效正文的旧计数改为正文身份与作废身份的去重总数，统一上限512；满容量下原作废重放仍可成功，新身份明确容量拒绝，不按时间驱逐。该端口不接收普通用户命令，须由核对过授权通知及其目标映射的宿主调用。

真实SQLite验证清理先到→重开→迟到保存拒绝、已保存正文清除、无关决策保持可用、双连接Bind/Retire竞争最终始终作废；新增512个未绑定作废身份的容量反例确认不能绕过配额。相关包race通过：SQLiteContext 1.384s、ContextAssembly 3.067s、MemoryAuth 10.850s，最终含容量用例定向race5.355s；vet/build/diff通过。证据19-context-retirement-{red,race,final}.log。

下一步仍需原使用通知到决策键的受信持久映射、ContextMemory保存前登记及ContextAssembler释放检查，再把Retire和清理确认接通。此处不宣称上下文用途清理已经端到端完成。

## 授权通知到上下文决策的清理连接

UseSpec新增有界Target元数据，纳入原登记语义并原样保留在首次失效通知中；可选字段不改变旧空目标登记摘要。InspectUse为受信宿主返回原使用活性和目标，不创建缺失登记、不提供正文披露权限。

ContextPolicy适配器把命名空间/TaskID/Decision编码为规范目标，以摘要生成稳定使用身份，并由宿主固定消费方及配置摘要。Prepare只覆盖这些路由字段，必要动作、原签名分配和期限仍须由已授权上下文适配器提供。Validate检查原身份活性及目标一致性，恢复策略不重新激活。Clean最多处理32项，先整批验证目标规范编码、命名空间和身份摘要，再逐项Retire实际快照，成功后才确认精确通知；未知确认不计完成，下次重复原Retire核对。

真实授权/SQLite Context组合测试覆盖登记→保存→撤销/恢复→原使用阻断→实际快照清理；清理失败时通知不丢，真实Retire后确认失败时正文已不可读但通知仍待处理，恢复重复清理再确认。另验证先登记、未保存便清理，随后迟到Bind拒绝。故障替身仅作用于Retire失败和确认失败边界，其余权限/状态/清理均真实。

相关完整包race通过：Authorization 4.210s、SQLiteContext 6.308s、Artifacts 3.618s、MemoryAuth 10.758s；新增未知确认反例最终定向race1.437s，vet/build/diff通过。证据19-context-policy-{red,green,race,final}.log。

ContextPolicy的真实清理链已接通，但ContextMemory尚未自动构造完整存储/引用授权条件并调用Prepare，ContextAssembler尚未自动执行原使用Validate。因此普通上下文调用路径的用途清理仍未完成，不提前标记整票通过。


## 上下文保存与普通宿主的原授权绑定

ContextMemory 按原选择恢复已分配读许可及必要读取动作，补齐模型处理、引用存储、正文保留条件；期限取原许可、记录保留/有效期或缺失元数据期限的最早值。额外读取沿用原 ReadID，不申请新分配。选中、缺失、不适用、裁剪均保留其依赖，只有实际保存正文才登记正文保留动作；超过16个去重必要动作明确容量拒绝。

ContextAssembler.NewWithPolicy 在 Bind 前登记，释放前后检查原使用活性。快照记录是否已绑定策略；配置了策略的宿主不能直接释放旧未登记快照，旧组装器也不能绕过已登记快照的策略检查。ContextPolicy.Bind 连接受信令牌、需求适配器及固定消费者配置。回答与行动两个真实个性化宿主已接入；泛型组装器测试仍可使用独立来源接口。

真实 Memory/JOSE/SQLite 集成证明原读取额度仍为1；撤销引用存储权限后即使恢复，原快照仍不能重放。相关包 race 通过；接入缺失记忆路径后的 MemoryAuth 完整 race 12.072s。回答正常/缺失用例15.622s、行动选择/重开/多决策用例65.611s；引用式与缺失记忆真实进程恢复、实际 Worker 等待用例17.198s。vet、全项目build、diff检查通过。证据为19-context-policy-binding-{red,green,race}.log、19-context-policy-{hosts,missing-race,recovery}.log；hosts日志的MemoryAuth条目无匹配测试，以独立missing-race完整包结果为准。

用途通知的清理端口已经实现，但普通宿主的定期消费调度和清理状态展示仍待接通；此处不能宣称后台已自动清完所有正文。其他运行载荷、四维状态、备份隔离及19整票最终验收仍未完成。


## 删除阻止发布与首次动作

回答和行动参考宿主的精确故障入口现在可调用真实Memory.Service.Delete；测试主体显式获得memory.delete，并使用本地权威位置/接收端绑定。模型只在既有确定性模型边界运行，没有新增外部请求。

回答分别在模型接收输入后、产物已持久保存但正式发布前删除来源，两者都不发布，读取额度仍为1。行动分别在操作接纳后尚未分派、调用已持久化但首次启动前删除来源：任务进入WAITING，余额仍1000，目标仍submitted，StartedInvocations为0；保留一个决策、一个原操作和一次原读。前一分支未分派，后一分支确实存在一个已分派调用。

红灯证据保留：回答入口起初拒绝delete模式；行动未接入删除故障时正常完成并扣至997，测试因此失败。回答首次接入时误用面向模型的接收端而被真实删除权限检查拒绝；改为本地绑定后通过。最终定向race：回答11.215s、行动48.730s，vet/build/diff通过。证据19-delete-publication-{red,denied,green}.log及19-delete-action-{red,green}.log。

这些结果仅证明新发布/首次动作阻断；不宣称撤回已发模型内容或已发生外部效果。检索边界的删除竞态、Started/未知效果后的原操作核对、其他载荷清理和状态/备份要求仍须补齐。


## 已发生效果在来源与快照清理后恢复

新增真实进程恢复入口RestorePersonalizedInvocationAfterDeletion：子进程通过真实执行驱动产生业务效果，在效果确认前退出73。恢复时先通过当前本地Memory权限删除原记录，再由真实SourceConsumer消费创建/删除事件、清理SQLite上下文并确认快照Read返回CONTEXT_INVALIDATED，随后仅按原持久调用与Core动作身份核对效果。

已Started调用的单独效果核对不再要求决策正文或快照摘要仍可读取。首次执行和完整任务继续路径仍执行各自上下文检查；不把“可以核对原效果”扩大为“可以基于已删内容产生新结果”。固定原请求身份和Core动作数量仍核验，真实驱动重复Run不增加效果。

红灯先证明入口缺失，首次接入还暴露恢复宿主没有启动时MemoryWrite而导致空指针。删除故障入口改为受信固定记录引用、用途和原修订，不依赖旧正文。初始真实删除恢复race27.152s通过；随后加入实际快照清理，最终race27.788s通过：初始效果UNKNOWN，Started保持，原调用身份保持，最终CONFIRMED，余额仅扣一次至997，无关记录submitted，原读额度1。正常/撤权已Started恢复回归20.716s通过，尚未启动的正常/撤权真实进程恢复回归21.608s通过，vet/build/diff通过。证据19-delete-effect-{red,recovery-failure,green}.log、19-delete-erased-effect-{red,green}.log及19-effect-recovery-regression.log、19-unstarted-recovery-regression.log。

此入口证实执行事实可独立于已清理上下文恢复，不代表所有运行证据正文已经清理。普通宿主持续清理调度、四维报告、旧备份隔离及整票验证继续待办。


## 按已确认消费位置报告删除范围

新增ConsumerInspection只读端口，SQLite使用既有配置与位置读取，不创建消费方或推进游标。Missing表示没有持久登记，不能当作完成。DeletionReporter接受宿主固定的最多32项集合清理目标，深拷贝配置；先验证确实存在与引用、修订、位置完全相符的权威删除事件，再读取各消费方已确认位置。

报告分权威提交、local、replicas、derived_archives四个维度。消费方未登记或其位置落后于删除时为pending；位置达到删除事件才是applied，且只证明该固定配置声明的受控范围。没有接入的目标或整维度为not_covered，没有全系统完成字段。配置冲突、读取失败或伪造删除位置直接返回错误，不用默认值填成成功。多个单调游标分别读取，不宣称跨存储原子完成快照。

真实SQLite测试覆盖状态读取不登记、创建位置不能代表删除完成、删除确认跨重开保留、未提交删除拒绝、同位置不同来源拒绝以及新配置不能借用旧位置。Memory完整race2.225s、SQLiteMemory完整race2.713s通过。真实进程删除恢复入口接入报告器，从实际SourceConsumer进度找到已提交删除事件，再报告上下文目标applied；没有接入此入口的本地综合清理和远端副本仍not_covered。

真实清理恢复入口race26.864s通过，vet/build/diff通过。证据19-deletion-status-{red,green,race,host-red,host-green}.log。

该报告器是受信内部端口，不是普通用户查询API；用户侧SDK/CLI必须先执行当前权限与原操作检查。本地停止使用/所有正文清理的细分事实、其他消费者调度、用途通知状态、公开协议接入及备份隔离仍待补齐。不能将内部四维报告形状视为整票四维验收已经完成。


## 删除完成度的当前授权查询

Memory.Service.WithDeletionReporter由宿主固定受信报告来源，返回服务副本；客户端不能携带消费方清单改变报告范围。DeletionStatus请求原操作身份、记录引用和用途，在读取操作及进度前校验当前记录删除权限与原操作权限，已提交回执还须匹配原主体及引用。报告来自实际已提交删除事件，不把创建/纠正回执当作删除证据。

查询不读取记忆正文、不创建操作或消费方。没有业务提交时，仍按既有原准入事实区分not_admitted、admission_expired和unknown；unknown不转换为删除完成。进度读取结束再次检查当前记录及原操作权限，查询途中撤权就丢弃整个报告。报告器未配置、原准入与回执证据不一致或实际读取失败均不构造成功报告。

真实Authorization/SQLite测试覆盖获权查询、错误主体/用途/命名空间、已撤权查询及精确进度读取后撤权；故障包装只在真实报告器返回后执行真实策略修改。初始定向race10.488s通过；完整相关包race：Memory2.234s、MemoryAuth11.789s、SDK1.143s，MemoryLocal无独立测试；vet/build/diff通过。证据19-authorized-status-{red,green,race}.log。

这一层已形成当前授权的Go服务接口；Memory协议、SDK与CLI尚未暴露该查询，不能提前声称用户入口已完成。清理调度、其他载荷、四维本地细分状态和备份隔离仍在19票范围内。


## Memory协议与SDK删除进度查询

MemoryRequest新增DELETION_STATUS方法及MemoryDeletionQuery（原operation_id、ref、purpose）；MemoryResponse新增独立MemoryDeletionState。既有字段编号保持，Protobuf按项目固定工具版本重新生成。状态响应回显原操作、记录和用途；仅committed携带权威删除修订/集合位置及local、replicas、derived_archives三组进度，其他准入状态不携带报告。

memorylocal在既有受信身份绑定之外解码请求，将查询交给已配置报告器的Memory.Service，错误响应清空进度及其他结果载荷。SDK验证消息关联、原操作/记录/用途精确匹配以及结果互斥。每组必须明确呈现范围；applied的确认位置必须达到删除位置，pending低于删除位置，not_covered不携带已确认位置。未知完成状态、缺失范围、过多条目、重复组内名称、错误中夹带报告或报告中混入读取结果均拒绝。客户端只能检查结构和语义一致性，不能凭协议证明远端诚实或覆盖未声明的库存。

真实Authorization/SQLite→MemoryLocal→SDK验证有权查询及撤权后拒绝；SDK传输边界反例覆盖错配操作、来源、用途、提前applied、未覆盖却带位置、遗漏副本范围、虚构完成态、unknown/error携带报告及混合正文结果。定向真实SDK链race10.886s；最终相关包race为SDK1.132s、MemoryAuth11.722s，MemoryLocal/MemoryWire无独立测试，实际字节链由集成覆盖；vet/build/diff通过。证据19-status-sdk-{red,green,race}.log。

CLI、具名整票profile与本地停止/综合清理细分状态尚未接通，普通宿主持续清理及备份隔离也未完成。19保持in-progress。


## 可重复CLI与具名删除profile

新增`contractcheck -profile memory-deletion-v1`。本阶段用例通过真实Memory SDK依次创建/读取、Delete原操作重放、拒绝原查询披露、查询pending状态、运行真实准入比较信息清理消费者、确认原位置、重复消费无新增工作、关闭并重开授权及Memory数据库，再验证原载荷补交拒绝、历史回执不带比较摘要以及applied位置持久保留。输出只将具名admission-comparisons目标标为applied，replicas和derived_archives仍not_covered。

可复验命令：

```sh
go build -o build/contractcheck-19 ./cmd/contractcheck
build/contractcheck-19 -profile memory-deletion-v1
```

CLI已实际运行成功，JSON证据`19-deletion-cli-report.json`保留7b57bee+dirty构建状态、Go/依赖版本、输入、预期/实际结果及范围限制。初始定向race1.650s、MemoryCheck完整包race8.944s、新CLI子用例1.522s通过；vet/build/diff及verify脚本语法检查通过。相关日志19-deletion-cli-{red,green,profile-race,command-test}.log。

新profile已登记在后续make verify流程，并单独记录memory_deletion阶段；此轮未运行整票完整验证。profile当前只有上述具名范围，尚需扩充原票其余删除/竞态/进程/备份验收，不能用一个通过的CLI用例替代整票。该命令操作独立临时合成数据，不是用户生产数据管理命令。


## 修复检索释放中的真实删除竞态

真实SDK/签名许可测试先绑定原查询，再通过Store.Read边界包装返回已经读取的真实记录，同时调用实际Delete提交删除。旧Reader只重新检查缓存记录的来源权限，没有重新检查该历史修订仍保留；红灯证据确认它错误返回成功，可能披露刚删除的正文。

QueryStore新增必需ValidateVersions端口：最多34个精确历史引用（32项结果加2项覆盖依据），在一个存储快照内只检查保留状态、不读取正文。SQLite用单条带参数EXISTS组合查询完成。Reader在释放前检查结果和覆盖依据；任一缺失返回READ_IRRECOVERABLE，保留原读身份、原许可与原选择，不重新查询或分配。检查只证明该存储观察时点，不承诺跨存储授权事务或返回之后的有效期。

补充仅包含budget_exhausted元数据、零条正文的查询反例：其覆盖依据在加载后删除，同样不得返回旧覆盖结论。共享Store契约增加live-revisions，确保仍保留的历史修订可通过、批次中缺失或错命名空间的引用不能被其他修订替代。

两条真实删除竞态定向race2.089s通过；相关完整包race：Memory2.632s、SQLiteMemory3.080s、MemoryCheck10.718s、MemoryAuth12.468s；vet/build/diff通过。删除CLI/profile新增两条用例，实际3项全部通过，证据19-read-delete-cli-report.json保留f918a76+dirty构建信息。红灯及回归证据19-read-delete-race-{red,green,regression}.log、19-read-delete-coverage.log。

此处修复实际披露缺陷；普通宿主持续清理、其他运行载荷、完整四维本地细分状态、旧备份隔离和整票最终验证仍未完成。


## 有界清理调度与独立失败

新增cleanup.Worker，宿主固定1至16个具名Job；每次调用只执行一个消费批次，独立消费方仍拥有原身份、持久位置和未知确认核对。Sweep执行一轮，Run按固定间隔持续执行；单目标失败/超时仍继续其他目标。每个Job须遵守context及自身有界接口，调度器不通过丢弃后台goroutine伪造超时完成。并发Sweep/Run返回Busy，宿主取消并等待Run退出后才能关闭存储。

Status仅保留每个目标最近一次尝试状态和错误，succeeded表示这次批次调用成功，不能解释为积压清空或全系统删除完成；对外完成度仍由DeletionReporter读取各消费者持久位置。调度器没有复制消费游标，也不记录正文或自动向外发送通知。

真实Memory/Authorization/SQLite测试中，第一目标持续Unavailable，第二目标仍消费创建/删除事件、真正清除旧准入比较信息并确认到位置2，多轮执行不阻塞；取消后全部步骤退出。独立边界测试覆盖并发启动拒绝、在途步骤退出后Run才返回、一个步骤DeadlineExceeded后仍执行下一项。Cleanup完整race1.023s、MemoryCheck完整race9.982s，vet/build/diff通过。

删除profile已改用Worker.Sweep执行并核对实际消费位置；CLI重新构建运行3项通过，证据19-cleanup-worker-cli-report.json保留c20cc14+dirty构建信息。其他证据19-cleanup-worker-{red,green,race}.log。持续Run已由实际消费方集成验证，但回答/行动普通宿主尚未绑定自动启动/停止生命周期，不能宣称这些宿主的后台清理已全面启动。后续还须其他运行载荷、本地状态细分、旧备份隔离与整票最终验证。


## 回答宿主的清理生命周期

新增cleanup.Start/Running，宿主拥有取消及等待责任。它不继承构造请求的短暂context，避免请求结束后后台清理意外停止；Close取消并等待唯一运行循环退出，可重复调用，最后才允许宿主关闭存储。现有Worker仍负责独立批次、超时与失败状态。

回答个性化宿主已登记两个100ms轮询目标：来源事件消费者按创建/纠正/删除清理上下文，ContextPolicy消费者按原使用失效通知退休快照。两者分别使用固定配置和原持久消费身份；构造完成后启动，personalizedMemory.close先关闭Running再关闭Context/Memory数据库。没有将瞬时任务ctx当作后台生命周期。

新增真实宿主自动清理测试。红灯证明此前删除后快照仍保留超过观察期限；接入后删除分支定向race4.472s通过。另加入撤销原签名许可分支，验证不直接调用清理端口也能观察到快照不可读。受管生命周期端口验证关闭等待在途Job退出且可重复关闭。初次完整回答包race在186.781s结束，发现两项恢复问题，修复与复验如下。

这一轮尚未接入行动宿主后台生命周期，也尚未启动全部受控产物、准入比较或其他运行载荷清理。不能将回答上下文自动清理扩大为全系统清理完成。


自动清理接入后，初次回归发现撤权输出恢复在最后核对快照摘要时遇到CONTEXT_INVALIDATED。恢复现在允许已被拒绝且未发布的原决策快照退休，仍保留原请求用量和发布结果核对；不要求重新保存被禁止正文。

引用式恢复还出现VERSION_CONFLICT。手动恢复只在入口续租一次，重建、校验、保存可能跨越原10秒租期；恢复改为入口成功续租后，仅对原资格定期续租。续租失败停止循环，后续Core操作仍检查当前拥有者/租期/控制状态；不接管过期资格，也不重置原决策。退出时取消并等待续租循环，先于关闭其授权/任务存储。新增真实跨原租期测试证明原资格与输出操作不变且租约仍有效。

修复后的失败分支、引用式恢复及过期资格拒绝定向race57.999s；跨租期用例11.287s；其他普通/缺失偏好进程恢复、实际Worker撤权等待及宿主自动删除/撤权清理定向race45.122s。vet/build/diff通过。初次完整失败记录未覆写，见19-answer-cleanup-race.log；修复证据19-answer-cleanup-{recovery-fixes,lease,final-targeted}.log，启动/关闭端口证据19-cleanup-lifecycle-{red,green}.log。整票完整验证仍待剩余要求完成后统一运行。

## 行动宿主持续清理与恢复历史

行动个性化宿主接入固定身份的上下文来源消费者及原使用清理消费者，构造成功后启动，关闭存储前取消并等待退出。自动删除测试通过（19-action-cleanup-green.log，race25.207s）。原删除效果恢复入口只观察后台消费位置及实际快照失效，不再用相同身份启动第二个消费者争用游标。

初次进程恢复回归失败（19-action-cleanup-recovery.log）：恢复检查要求读取已作废的历史正文。检查点增加RetiredContexts，与仍有摘要的快照、Core可证明的未绑定决策分别记录；写入新检查点时不携带已作废快照摘要。恢复核对完整、不重叠的决策覆盖及受信Store的持久退休状态；当前执行所需快照仍须完整。检查点后发生的旧决策退休可以被受信Store证明，不能把Missing或Unavailable解释为已清理。

后续测试暴露两项验证顺序问题。第一，恢复流程的产物检查主动撤销Memory披露权限，后台可能立即退休当前快照；完整性核对应放在这次主动撤权之前。第二，尚未Started的调用遇到已退休当前快照，应返回原调用的验证拒绝并阻止Start，继续保留原身份及效果状态，不补发新操作。失败日志19-action-history-green.log、19-action-history-final.log均保留，不将文件名视为通过标记。篡改历史反例扩充到将未绑定间隙或当前决策伪报为退休。

这尚不意味着已存检查点中的历史摘要、运行载荷和全部产物均已自动最小化；这些仍是19的独立待办。

## 回答宿主派生产物自动清理

回答宿主新增固定身份answer-artifacts来源消费者，调用实际Artifacts.InvalidateSource/Clean；另以独立Job定期Clean处理原使用失效。来源水位与逐事件确认仍由既有消费者持久化，不使用调度状态代替完成证明。

测试经真实宿主和Content端口保存超过inline上限的Memory派生正文，删除实际来源，再通过文件存储公开端口观察对应文件消失，同时确认普通READ仍被拒绝。未接入Job时失败（19-answer-derived-file-red.log，3.581s）；接入后race1.766s通过（19-answer-derived-file-green.log）。最早测试错误地要求删除后LOOKUP仍可观察cleaned，但LOOKUP也受当前来源权限约束，故修正为上述实际文件效果；19-answer-derived-cleanup-{red,green}.log均为该错误观察条件导致的失败，不作功能通过证据。

相关宿主恢复、派生来源和权限回归仍在运行；整票最终完整验证、最终双轴审查、其他运行载荷清理、备份隔离及完整范围报告尚未完成。

行动恢复的最终检查还覆盖“初次检查后、拒绝Start后才完成退休”的竞态：当实际调用未Started且已有验证拒绝，不再依赖被禁止正文作最终核对；依然核对原Invocation请求、Core动作和真实业务效果。该反例初次失败记录19-action-cleanup-revoked-recovery.log保留；修复后定向race38.946s通过（19-action-cleanup-revoked-final.log）。已绑定但未使用的上下文恢复定向race69.164s通过（19-action-history-bound-final.log）。ContextSnapshots统计live/retired身份，不表示被清理正文仍存在；明确未绑定间隙单独统计。

新增产物Job后，初次较广回答回归在221.586s失败（19-answer-derived-regression.log）：无关来源更新的手动发布越过租期，Worker的个别决策也在完成模型请求前越过预算。增加的后台整份授权/产物事务使100ms轮询与前台争用。回答宿主的清理间隔现为1秒；批次和5秒清理调用上限不变，新使用仍同步检查当前来源/许可。未增加前台决策或租约时限。调整后的全部原失败分支、实际Worker及两类宿主自动清理定向race72.176s通过（19-answer-derived-scheduling.log）。此轮没有再次运行完整回答包，不能把定向修复复验写成全包通过。

行动历史扩展定向race最终430.368s通过（19-action-history-regression-final.log），实际覆盖未绑定失败决策的两类进程退出、五种篡改历史拒绝，以及真实删除后原Started效果核对。较早19-action-history-regression.log的gap-effect失败保留；后续恢复顺序修复后此用例复验通过。额外bound历史与撤权恢复的独立通过证据如上。最终vet（Answer/CatalogCheck/Cleanup）、全仓build及diff检查通过；尚未运行整票最终全套verify或双轴审查。

## 行动产物与双宿主准入清理

行动宿主新增固定身份action-artifacts来源消费者和产物原使用清理Job；轮询间隔为1秒，单步仍为有界批次/5秒上限。模型依据和动作参数已进入既有受控产物，删除后的SourceConsumer真正调用来源失效、文件/元数据Clean，再确认位置。原动作身份与业务事实不作为新操作重建。

回答宿主新增answer-admissions，行动宿主新增action-admissions：均通过MemoryCleanup.Admissions验证实际删除修订及原写入回执，清除授权准入旧比较摘要，保留原接纳身份。回答测试核对清理前摘要存在、实际删除后自动变为空、原准入仍reserved，并证明补交原载荷得到ResultOnly；不为去重继续保存禁止比较信息。

行动本地验证报告现在从实际Delete回执建立SourceEvent，分别观察artifacts、contexts及admission-comparisons持久位置。archives显式未接入，replicas仍not_covered；这些具名目标applied不意味着其他运行载荷已清理。原Started效果恢复也复用同一报告范围，不再遗漏已自动接入的消费方。报告读取不驱动清理，也不是绕过Memory.Service授权的普通用户接口。

红灯证据：19-action-artifact-host-red.log显示实际删除已提交、产物仍pending@0；19-action-admission-host-red.log显示产物已applied@2但准入仍pending@0且摘要仍存在。回答红灯19-answer-admission-host-red.log在原摘要仍保留时到达观察期限，底层返回OUTCOME_UNKNOWN，未把超时视为完成。接入后单项race为行动产物24.202s、回答准入1.592s；两项行动清理58.172s，回答自动清理/发布/Worker回归76.474s。行动产物接入后的首次动作阻断、更正重组、实际删除后原效果恢复及上下文清理回归156.867s通过。分别见19-action-artifact-host-{green,regression}.log、19-answer-admission-host-green.log、19-action-admission-host-green.log、19-host-admission-answer-regression.log。

统一报告范围后的最后行动恢复回归仍在运行。其他运行载荷、旧检查点/受控备份恢复隔离、完整CLI/profile覆盖和整票最终verify/双轴审查尚待完成，19保持in-progress。

统一范围后的最终行动定向race173.108s通过（19-host-cleanup-action-final.log），包含两个自动清理用例、真实删除后原Started效果核对、两类更正重组进程退出后的恢复。最终vet、全仓build、diff检查通过。上述范围均为定向回归，整票最终verify和审查仍未执行。

运行载荷补查：Execution.Record持久保存Request.InputRef和结果Reference，实际Call.Input/Observation.Output只在调用边界流转；Core.Action/DecisionRecord/ExecutionReport保存InputRef、Evidence和Reference。Brain的自由文本等待问题经SaveQuestion保存为受控产物；ChargeQuery只记录任务版本/序号/查询种类。回答输出、行动原模型输入/输出、参数、执行输出及问题正文经现有Content端口管理来源，已经纳入上述产物清理。此结论针对当前Brain与参考宿主的实际保存路径，不声称任意第三方Worker都遵守约定。原checkpoint的SnapshotSHA256/ContextDigests及恢复材料尚未完成持久最小化；还需逐项核对这些显式副本与备份隔离，不能仅凭Core主体多为引用就关闭运行载荷要求。

## 受控检查点完整性依据

新增ContextAssembly.CheckpointStore受信端口。BindCheckpoint在原快照身份上固定调用方已知的正文SHA-256，不创建快照、不授予使用权限；VerifyCheckpoint校验同一存储观察中的原正文与比较依据，缺少依据的live快照返回Missing。SQLite将比较依据保存于context_checkpoints，记录数由原快照容量约束，直接Retire和来源InvalidateSource在清除正文的同一事务中删除比较依据。VerifyCheckpoint只有在退休已持久化且比较依据确已移除时才返回Invalidated，不导出摘要。

真实Store测试覆盖错误正文比较拒绝、相同绑定重放、重开保留、退休后清除、迟到绑定拒绝；双连接BindCheckpoint/Retire竞争也不能留下比较依据。来源清理测试用真实Assembler和MemoryCleanup.Contexts生成/删除快照；精确移除来源清理中的DELETE语句时，VerifyCheckpoint返回Unavailable，恢复语句后通过，证明不仅仅把已退休行隐藏。证据19-context-integrity-{red,green,source-red,source-green,race,regression}.log；相关完整包race为SQLiteContext5.724s、ContextAssembly2.753s。

回答及行动新检查点改为format 2，生成前绑定原比较依据；文件不再写SnapshotSHA256、SnapshotSHA或ContextDigests。行动文件按BoundContexts、RetiredContexts、AbsentContexts记录完整覆盖，恢复仍核对Core决策身份、明确缺失证据和当前快照完整性。旧format 1仍使用文件中已知摘要校验并导入受控存储，不能从任意恢复正文自行计算新依据代替原校验；旧文件的持久迁移/清理尚未接入，不宣称这些历史副本已清理。

真实进程证据：回答新文件无摘要且原决策恢复通过（race10.048s）；把实际正文复制到缺少原比较依据的新Context数据库时，新格式恢复拒绝且Core没有模型请求/输出（race4.604s）。该反例在临时绕过VerifyCheckpoint时实际错误执行，红灯证据19-answer-checkpoint-missing-red.log保留。回答普通、已发送/输出已存、引用式、缺失偏好和自动清理定向race71.786s通过。行动更正重组后的新文件无比较摘要，实际原动作恢复race50.146s通过。其余行动历史和副作用回归仍运行，见19-action-checkpoint-history-regression.log。

旧文件原地持久迁移、恢复材料治理及受控备份隔离仍待完成；新比较存储不证明一份旧备份是当前权威。整票最终verify/双轴审查尚未执行。

补充原有真实os.Exit清理探针：退出前为实际Assembler快照绑定比较依据；清理前退出后仍可校验，清理后退出重开必须确认比较依据已移除，恢复清理后也不能重绑。定向race1.282s通过（19-context-integrity-process.log）。双连接绑定/退休竞争定向race1.127s通过（19-context-integrity-race.log）。

行动历史最终定向race424.191s通过（19-action-checkpoint-history-regression.log）：五类篡改、两类未绑定间隙恢复、已绑定未使用历史，以及实际删除后原Started副作用核对。补充回答旧格式兼容：用实际原正文及其原摘要构造format 1、替换为未携带比较依据的Context库，恢复仍校验旧摘要、保留原读分配和原决策；没有该原依据的format 2同样替换仍拒绝，两分支race14.357s通过（19-answer-checkpoint-legacy.log）。旧文件未被自动重写，持久迁移仍是下一步。最终vet/build/diff通过，本阶段不代替19整票完整验证。

## 旧检查点文件的持久迁移

新增checkpointfile本地Adapter，用受信宿主固定的文件名清单操作最多1MiB的0600文件。先在进程间锁下比较原始文件字节，再同步暂存文件、原子重命名并同步目录；原文件不截断，过期写入拒绝，完全相同的未知结果补交可核对成功。每个固定文件名只使用一个保留的pending路径。目录不能允许组/其他用户写入，文件不能为符号链接或非普通文件；命名管道通过非阻塞打开后类型检查拒绝，不在打开时无限等待。该端口只用于宿主控制的检查点，不是对端文件管理接口。

回答和行动宿主在恢复路径及生命周期清理Job中迁移format 1。有效快照必须用文件内原摘要通过受控CheckpointStore验证，已退休快照必须证明退休及比较依据移除；核对完成后仅移除文件内摘要、转换format 2并保留原任务、读取凭据、调用、输出及决策历史。行动逐项检查完整历史和Core原动作，当前快照允许退休只用于维护；首次动作仍要求有效上下文。未核对成功不重写旧文件，不通过重新散列不可信正文发明原校验依据。

回答恢复的租约拒绝路径也会进行必要的检查点维护：只打开ContextStore核对原记录，不建立Memory读取、不续租或接管新决策。真实过期租约仍报告VERSION_CONFLICT，已发送模型请求的原保留额度和输出操作保持。正在运行的回答宿主另验证了旧文件迟到：先实际删除来源并等待后台退休，再放入旧检查点；宿主自动迁移，原身份不变且退休比较依据不复活。

文件端口最初的19-checkpoint-file-{red,green}.log均是目录权限约束与TempDir目录模式不符造成的失败，不能当作通过证据。修正目录边界后，用暂时拒绝Replace产生契约红灯（19-checkpoint-file-replace-red.log），恢复实现后race1.023s通过。并发写入、私有文件边界、真实进程在提交前/后退出及补交核对race1.081s通过；进程测试不声称覆盖精确的暂存写入中途断电。命名管道反例真实阻塞超过取消期限（19-checkpoint-file-nonregular-red.log），修复后的完整checkpointfile包race1.086s通过（19-checkpoint-file-final.log）。

回答旧文件恢复红灯证明原文件仍留有SnapshotSHA256，迁移后race9.950s通过；后台迟到文件清理race4.739s通过。过期租约路径原本不清理文件的红灯也已记录，修复后race14.755s通过。相关回答恢复/删除回归race93.567s通过（19-checkpoint-migration-answer-regression.log）。行动旧文件恢复红灯48.686s证明文件仍含摘要，接入后race51.465s通过；已退休原效果恢复及篡改摘要拒绝定向race98.010s通过。补充实际Invocation/目标状态检查、动作历史扩展回归和独立后台行动宿主验证仍在运行。

本阶段的legacy-checkpoint Job是固定本地文件维护，还不是具备来源事件确认游标的全范围删除证明；不能用其最近成功状态提升其他目标的删除完成度。恢复凭据仍须留在授权私有目录，未活动宿主不会自行运行清理。受控备份隔离、完整范围报告及具名CLI/profile覆盖、整票verify和最终双轴审查仍待完成，19保持in-progress。

本阶段最终动作回归race382.891s通过（19-checkpoint-migration-action-regression.log），包含对实际Invocation/目标状态的篡改拒绝检查、五类决策历史篡改、原动作完成及删除后原效果核对。独立后台行动宿主只启动维护生命周期，未调用Restore或迁移函数即完成旧文件迁移，Core快照前后完全相同；随后仍能按原Started身份核对效果，race49.313s通过。另补充真实硬链接反例：单名字替换会留下旧内容，原实现错误返回成功；现在要求目录/文件属于当前进程用户，受控文件及锁不得有额外硬链接，不满足时拒绝而不宣称完成。19-checkpoint-file-hardlink-red.log保留失败，修复后的完整文件端口race1.086s通过（19-checkpoint-file-hardlink-green.log）。最终vet、全仓build和diff检查通过；上述仍是19分阶段验证。

## 显式备份恢复入口的隔离

SQLite Memory新增OpenRestored维护入口，只接受已有私有数据库。返回前持久设置memory_recovery隔离标记；返回的Restore不实现Memory.Store，也不转交普通读取/写入句柄。普通Open发现持久隔离后返回RESTORE_QUARANTINED，重复打开或进程重启均不自动解除。正常运行句柄在整个生命周期持有共享文件锁，恢复要求独占同一文件；锁竞争有2秒上限，维护不能在普通运行句柄仍活动时修改隔离状态。数据库文件检查实际所有者、0600权限、普通文件类型和单硬链接；关闭数据库后释放文件锁，重复Close仍保持原有幂等约束。

真实测试先保存Memory记录并复制关闭后的SQLite数据库，再在当前数据库执行Delete。旧副本进入显式恢复后，普通Open拒绝；重新进入维护仍处于隔离。另在成功持久隔离后实际os.Exit(71)，父进程重开仍拒绝普通访问。双角色锁测试验证正在运行的数据库不能同时进入维护，维护期间普通打开也不能绕过；关闭后释放锁不会清除隔离记录。初次API缺失产生编译红灯（19-backup-quarantine-api-red.log）；为验证实际拒绝行为，临时绕过普通Open隔离检查时，测试确实读取到了已删正文（19-backup-quarantine-release-red.log），恢复检查后SQLite Memory及Memory完整race6.532s/缓存通过。重复Close的新回归失败也保留（19-backup-quarantine-close-red.log），已用同步关闭修复。进程及互斥定向race5.214s通过（19-backup-quarantine-process-lock.log）。

此入口只落实显式恢复流程的隔离阶段。它不自动辨认任意外部原地覆盖/回滚，宿主必须按恢复流程打开待恢复文件，并在失败时继续将该路径排除在正常运行配置之外。目前没有解除隔离入口，不能把一直拒绝读取当作最终恢复实现：仍须取得独立当前权威及删除水位证明、清理受影响内容、保留可用数据，并验证数据使用与新调度/同步资格各自的开放条件；不能让备份内旧配置或水位自证当前。尚须接入参考恢复流程和完整CLI/profile验收，19继续in-progress。

隔离入口的相邻集成回归：MemoryAuth完整race11.497s、MemoryCheck参考宿主完整race10.949s通过（19-backup-quarantine-host-regression.log），包含真实授权/清理消费者和SDK删除及重开。ContextMemory完整race1.028s通过；MemoryCleanup包没有独立测试文件，不把该包的编译输出当作集成测试。目标包vet、全仓build及diff检查通过。

## 当前权威恢复证明

新增Memory.RecoveryStateSource内部端口，按既有命名空间/集合输出RecoverySnapshot。SQLite在同一个只读事务中读取所有保留修订的比较依据、永久删除修订及原操作历史的一页；集合位置和这些内容属于同一观察点。保留修订与永久删除合计最多512项，历史按After每页最多512个原操作，Position表示当前集合头。保留修订数加各删除修订之和必须等于集合位置；活跃记录修订连续，页内原操作位置连续，每个原修订由保留记录或明确删除覆盖。已删记录不携带原正文比较，非删除记录保留原比较；不以缺行自行推导缺失。快照不携带正文，也不因清理一个集合而包含其他集合的记录。输出为受信维护元数据，比较摘要仍有敏感性，不是普通用户披露接口。

新增recoveryproof参考Adapter，实现RecoverySource和RecoveryVerifier。签发者固定权威、世代、集合及备份之外的Ed25519密钥，每次收到挑战都重新调用当前StateSource取得事务快照。验证者独立固定公钥、权威、世代、集合及最低可信位置；这些配置不能从待验证证明或同一份旧备份推导。证明绑定本次32字节挑战、完整状态和签发范围，旧证明不能用于另一个挑战。最低可信位置还会拒绝低于该水位的实际旧数据库状态，即使错误运行的签发者持有有效密钥、重新给旧库签了名。

签名字节使用memory-recovery-proof-v2域前缀（将分页After纳入签名）及明确字段顺序的参考JSON编码，空重复字段统一编码为空数组；身份字段要求有效UTF-8，避免替换字符产生歧义。输入历史和标签长度先受约束，签名消息最多8MiB；签发调用使用5秒上限，底层SQLite读取仍受2秒约束。证明只认证一个观察点的状态，不是持续有效的读取许可，也不授予披露、驻留、同步、修改或调度权限。使用方仍需按新的核验轮次生成新挑战，并执行当前权限和释放检查。

真实SQLite验证覆盖当前Delete、保留有效数据的比较依据、无关集合隔离及旧快照不被后续读取修改。两连接并发删除的八轮验证只能观察到完整的删除前或删除后状态，race1.194s通过（19-backup-current-state-race.log）。签名验证使用独立配置的密钥及实际旧数据库副本，覆盖旧挑战重放、修改挑战/状态、错误公钥、错误集合/世代及低于外部水位的回滚状态。临时移除挑战匹配或最低位置检查时分别出现真实误接受，红灯记录于19-backup-live-proof-{challenge,floor}-red.log；恢复检查后通过。UTF-8身份拒绝及空集合表示兼容也分别保留红灯证据，修复后完整proof/SQLite Memory/Memory包race通过。最后proof包1.131s通过，其余两包复用已验证缓存；此前完整SQLite Memory7.045s、Memory2.186s通过。命令和输出见19-backup-live-proof-{identity-green,complete}.log；目标包vet、全仓build及diff检查通过。

目前状态获取和证明验证已实现，隔离副本尚未消费这些证明执行原子清理及核验后开放；普通Open仍保持拒绝。下一阶段必须将当前证明、实际清理结果和有效数据恢复连接起来，持久保留必要水位并按实际覆盖范围报告。可信配置的恢复接入、参考CLI/profile与整票最终验证仍待完成；不能把这些独立端口的通过当作19备份恢复验收完成。


## 隔离副本的分页核验与实际清理

恢复证明接入真实Restore.Reconcile。宿主在备份之外提供当前权威Source与独立Verifier，每轮由恢复端生成随机挑战，核对签名、范围、世代和最低可信位置。Verifier.Configuration返回固定信任配置的摘要；修改配置会从原历史起点重新核验，不能沿用旧配置游标。此配置摘要只绑定维护进度，不构成授权。

发现并修复原证明端口的容量错误：删除会释放正文容量，但保留原操作历史，所以合法集合可有超过512个历史操作。真实512修订、一次删除和另一次写入形成514个原操作，原端口返回CAPACITY_EXCEEDED（19-backup-history-capacity-red.log）。现在按既有集合位置分页，末页仍返回完整当前保留/删除状态；不会新增业务操作身份或降低既有容量。分页完整三包race通过，见19-backup-history-paging-green.log。

每次Reconcile最多核对一页原操作的ID、主体、记录、修订、位置和仍可比较的语义。对于当前已删除的旧正文操作，只核对允许保留的身份事实，不要求恢复被清除的比较摘要。原历史全部核验后，在同一SQLite写事务内清除已删除正文及旧操作比较，保存最小删除修订、当前已应用位置和保留/缺失数量；实际清除结果再次查询确认后才能提交。失败不推进位置，并发维护用事务内重读进度拒绝过期提交。无关集合不受影响。新权威修订在旧副本中不存在时明确计入Missing，不伪造正文，也不把清理完成等同完整同步。

Restore.ReadVerified是受信存储维护端口：先完成当前证明核验及清理，按当前已签名比较依据核对实际本地正文，并在返回前再次取得新挑战证明，阻止读取过程中发生删除后的释放。已保存sanitized进度不能替代实时证明。普通Open始终拒绝该维护文件，Reconcile不授予新修改权威、同步资格或任务调度权。用户披露仍须接入现有Memory权限、来源、驻留及读取授权检查；本阶段未将维护端口直接暴露为SDK读取。

真实测试覆盖删除与有效数据同时存在、重开后进度与有效正文保留、无关集合、缺失新修订、不同原操作身份、源不可用、旧挑战重放、伪造签名以及释放前再次删除。实际515项原历史在第512项提交后os.Exit(71)，重启从该位置核对剩余历史；清理提交后再次os.Exit，父进程重开确认已应用516、被删正文不可读而保留正文可用（19-backup-reconcile-process-green.log，race4.302s）。这是事务提交前后的进程恢复证据，不是存储介质断电物理擦除证明。

临时使实际正文DELETE或比较清除UPDATE不生效时，恢复完成测试均因UNAVAILABLE失败；临时跳过释放前比较时，测试实际读出了删除期间的旧正文。红灯分别为19-backup-body-erasure-red.log、19-backup-comparison-erasure-red.log、19-backup-release-recheck-red.log；语句恢复后的完整SQLite Memory/recoveryproof/Memory race分别14.152s、1.165s、2.283s（19-backup-reconcile-regression.log）。初始恢复API缺失和首次清理通过记录于19-backup-reconcile-api-red.log及19-backup-reconcile-green.log。

维护游标与其他本地运行状态一样依赖受控私有数据库；不声称抵御能够任意篡改进程/数据库的攻击者，也不把文件inode作为当前权威证明。显式外部覆盖或回滚必须重新走受控恢复，普通文件复制不授予运行资格。当前权威配置必须独立于待恢复备份，并能由运维设定已知最低位置。

仍需接入受控SDK/参考恢复流程、目标范围报告及具名CLI/profile，完成整票verify和双轴审查；19保持in-progress。


## 恢复数据接入现有SDK治理

Restore.ReadView将固定集合的恢复维护数据适配到现有Memory.QueryStore，供Memory.Service/Reader与SDK读取；宿主继续提供备份之外的当前授权库、来源规则、驻留配置及读取凭据。视图不提升角色，Commit、LookupOperation、ReadChanges拒绝普通修改/权威操作，也不实现Delete或同步/调度接口。普通Open的持久隔离保持。

集合Scan在当前核验/清理完成后核对每个真实本地最新正文的签名比较，并在返回前复查权威集合头；副本缺失当前修订时返回Unavailable，不能将旧版本冒充当前结果或宣称完整覆盖。精确历史Get仍可读取具有当前证明的已保留修订。读取结果继续通过原Reader绑定读取身份、校验当前权限和源限制，并在最后ValidateVersions重新取得当前证明；本地sanitized状态不能替代该检查。视图只登记原有读取披露元数据，不生成修改权威或业务操作身份。

真实SDK测试创建两条记录并复制数据库，在当前权威删除其中一条，再用恢复视图查询：仅返回有效记录。当前授权政策撤销后原查询重放拒绝。独立签发与实际接收方绑定的有效读取凭据后，cloud接收方仍因当前驻留限制被拒绝，device-a精确读取通过。最后一次读取凭据复查时实际提交Delete，返回前的新权威检查阻止已重建正文释放；临时改为仅检查备份本地版本时，测试真实释放旧正文，红灯见19-backup-sdk-release-red.log。

API缺失红灯见19-backup-sdk-api-red.log；定向SDK/释放/驻留测试通过，见19-backup-sdk-{green,release-green,residency-green}.log。最后完整SQLite Memory/recoveryproof/Memory/memorycheck/memoryauth race分别16.089s/1.173s/2.536s/14.664s/13.170s（19-backup-sdk-regression.log）；目标vet、全仓build及diff检查通过。本阶段尚未将恢复例程注册到命名CLI/profile，目标范围报告和整票verify/双轴审查仍待完成。


## 恢复流程的命名CLI验收

memory-deletion-v1增加必验sdk-backup-recovery-governance。用例通过正式CheckRecovery例程创建实际SDK记录、复制关闭后的SQLite数据库、在当前权威Delete，再用外部当前授权库与独立权威信任配置恢复旧副本。核对sanitized@3、原历史核验2、保留1/缺失0；SDK QUERY仅返回有效记录，具有独立有效凭据的cloud接收方仍受驻留约束，device-a精确GET通过。维护文件关闭重开后普通Open仍拒绝、原读绑定可按当前权限重放；当前政策撤销后返回拒绝。原包测试与CLI共享恢复例程，避免两套实现偏离。

原profile没有恢复用例时测试失败（19-backup-profile-red.log）；接入后恢复及SDK定向race3.695s通过（19-backup-profile-green.log）。完整memorycheck包race12.427s通过（19-backup-profile-regression.log），相关vet/diff检查通过。使用go1.26.1在linux/amd64从0b890aa加本阶段改动构建build/contractcheck-19，执行：

```sh
go build -o build/contractcheck-19 ./cmd/contractcheck
build/contractcheck-19 -profile memory-deletion-v1
```

4个必验用例全部通过，实际构建版本、dirty标记、依赖、配置、预期/实际结果及限制见evidence/19-backup-profile-cli.json。CLI恢复为有序关闭/重开，真实os.Exit的分页与清理证据仍由前述独立进程测试提供，不能混称为CLI崩溃验证。该profile仍不是19全部验收；受控清理目标的完整范围报告、其他既有能力的整票verify及最终双轴审查继续待办。


## 受控备份的删除范围报告

Restore.CleanupTarget把一个实际已打开的私有Memory备份文件接入既有DeletionReporter。目标绑定固定名称、集合、独立Verifier配置及实际文件设备/inode；生成的配置摘要仅表示该报告目标的有效范围，不能作为当前权威证明。ConsumerInspection只读取该文件已提交的恢复维护状态，不运行Reconcile、不发SourceEvent确认、不联系权威。只有同一信任配置下原历史已全部核对并完成sanitized事务，才返回实际AppliedPosition；没有进度、尚在核验或更换信任配置时保持pending。传入其他目标配置拒绝。

正式CheckRecovery的SDK DELETION_STATUS先验证当前权威原删除和用户权限，再读取上述目标。实际QUERY触发恢复前，controlled-memory-backup为pending@0；清理后以及重开后均为applied@3。报告同时保留unconnected-archive、未注册远端和其他本地目标为not_covered；该单文件完成不表示Context、产物、其他备份或整个运行目录已清理。该范围目标只证明过去位置的删除清理，不授予同步、修改、读取或新调度资格。

API缺失红灯见19-backup-report-api-red.log。只读状态不执行清理、完成状态限制、不同目标配置拒绝以及更换独立信任配置不能复用旧完成均通过；临时移除信任配置比较时确实误复用了原applied，红灯见19-backup-report-trust-red.log。恢复后的完整SQLite Memory/Memory/memorycheck race15.902s/2.493s/14.373s通过（19-backup-report-regression.log），目标vet和diff通过。

从439eb44加本阶段改动重新构建build/contractcheck-19并执行memory-deletion-v1，4项必验全部通过；实际备份用例返回backup-report=applied@3、other-targets=not_covered。具备构建版本、依赖、配置和逐项实际结果的报告见evidence/19-backup-report-cli.json。现有回答/行动宿主的legacy-checkpoint维护虽已完成实际迁移，尚须在最终目标清单中准确表述其独立进度覆盖；19整票verify与双轴审查尚待完成。


## 最终宿主清理目标清单

| 受控内容 | 实际处理与完成依据 | 当前范围报告 |
| --- | --- | --- |
| Memory原正文与原比较 | Delete同事务清除历史正文/原比较，保留删除修订与原操作事实 | authority=committed；不推导其他目标完成 |
| Context及混合快照比较 | 来源失效后整体退休，清除正文和checkpoint pin；来源消费者逐事件确认 | 已注册contexts按其真实水位报告；用途收紧另走原使用清理 |
| 运行输入/输出、参数、问题及派生产物 | 来源依赖整体失效，实际文件Clean后确认；旧来源禁止再保存 | 已注册artifacts按其真实水位报告 |
| 准入比较 | 真实来源删除/原操作证明后清除授权库比较，保留原接受事实 | admission-comparisons按其真实水位报告 |
| 旧回答/行动检查点 | 固定文件/任务范围消费者先CAS迁移、只读确认，再持久确认来源位置；完成查询前后复查迟到文件 | legacy-checkpoints按真实绑定和已确认位置报告；异常/迟到旧文件阻止复用完成 |
| 显式受控Memory备份 | 隔离、独立当前证明、核对原历史、实际清理与持久位置；SDK受当前权限治理 | 具体文件通过CleanupTarget注册后据实报告；普通行动宿主未注册文件时controlled-backups=not_covered |
| 其他归档、独立导出及远端副本 | 当前宿主未接入；外部披露不承诺撤回 | archives及replicas明确not_covered |

行动宿主报告新增legacy-checkpoints和controlled-backups两项，保持其他既有目标顺序；报告并不触发清理。实际首次行动前删除的宿主测试通过，race24.353s（19-final-host-inventory.log）。这张清单区分实际已实现的维护和可查询的逐事件完成证据，未将无证据目标标为applied。开始整票完整verify和以fee7662为固定基点的双轴审查；审查发现缺口仍须修复后才能关闭19。


## 审查修复：旧检查点持久完成确认

初次双轴审查明确指出仅把旧检查点报not_covered不能满足现有受控内容的消费登记要求，现按原需求补齐，未缩小票据范围。新增MemoryCleanup.Checkpoints，复用SourceConsumer逐事件确认与配置绑定；固定文件/任务/宿主路径纳入有效配置摘要，不同配置产生不同消费者名且完整配置摘要仍防止错误复用。宿主继续承担原任务、原比较或已退休证明的验证及文件CAS迁移，通用消费适配器不替代该责任。

Apply在实际维护和只读文件确认成功后才允许SourceConsumer提交位置。没有新事件时也维护迟到旧格式文件；状态接口在读取持久位置前后各检查实际文件，异常/旧文件返回未完成而不是沿用历史applied。报告查询不执行迁移或确认。回答及行动的生命周期Job均驱动该消费者，行动的legacy-checkpoints目标连接实际绑定；其他未注册备份/归档范围不变。

回答宿主实际删除、退役、迟到旧文件、原位置确认及再次迟到后清理通过，race4.781s；实际清理确认前/确认提交后的os.Exit恢复通过，补交保持原来源位置，异常文件不推进游标。报告读取位置期间注入实际迟到文件时拒绝完成；临时移除第二次文件检查时错误报告2，红灯19-checkpoint-confirmation-read-race-red.log，恢复检查后MemoryCleanup定向race1.504s通过。行动原效果/后台迁移及清理范围定向race67.941s通过，独立行动迟到文件加强验证继续运行。

Standards提出的重复退役效果已收敛为同一私有事务函数retireSnapshot；容量/来源水位/提交仍由原入口管理，完整SQLiteContext/MemoryCleanup race5.495s/1.591s通过（19-review-retirement-regression.log）。只读视图的宽查询接口适配仍保留为P3设计建议，原因与边界见19-code-review.md。

第一轮完整make verify已因审查P2修复主动停止，进程组1018577终止，工具会话20864返回143；日志保留为19-full-verify-pre-review-interrupted.log，不将中断算通过。修复后的最终完整验证尚需重新执行。

行动迟到检查点加强验证race44.953s通过（19-checkpoint-confirmation-action-late.log）：后台完成位置2，停止周期任务后重新放入实际旧格式文件，完成查询拒绝；显式运行维护后仍为原位置2且Applied=0，Core事实未改变。最终相关vet/diff通过。

## 完整验证发现的恢复报告回归

第二轮完整验证在 cmd/contractcheck 的四个撤权进程恢复用例中报告 original context history changed。定向实际进程复现显示原身份及检查点完整性仍成立，但 ContextSnapshots 为零；原因是拒绝新 Start 或仅核对原 Started 效果时跳过最终历史报告。已将该轮日志保留为 19-full-verify-history-failed.log；失败后终止剩余测试，会话返回143，不计为通过。

先在实际进程恢复测试补充快照、缺失项、纠正次数断言，撤权子用例实际红灯（19-revoked-history-test-red.log）。随后使报告始终核验原有效或持久退休身份并填写计数；首次执行依然要求有效快照及 StartGuard，未改变原 CLI 验收断言。正常/撤权调用恢复和原效果核对四个子用例通过，50.345s（19-revoked-history-test-green.log）。增量双轴审查均无新缺陷。

四个原失败 CLI 用例已逐一返回 verified（19-revoked-history-cli-green.log）；删除后原效果核对及五种历史篡改回归通过（19-revoked-history-retirement-green.log）。临时诊断程序已删除，仓库实现没有诊断探针。随后重新运行最终 make verify。

## 完整验证的整包累计超时

代码113be57的第三轮make verify已实际退出2；CLI完整测试470.722s通过，回答模块224.385s通过。catalogcheck包在1800.054s触发整包30分钟超时，唯一在运行的TestDeletedMemoryPreservesOriginalEffectAfterProcessExit仅执行19s，堆栈位于实际子进程等待，其自身仍受60s上下文限制。不是该用例触及自身时限。该用例此前在删除/篡改定向回归中通过。保留19-full-verify-package-timeout.log与19-package-timeout-stages.json，不将此轮记作通过。

随19新增真实进程、删除及旧检查点用例，串行完整包已超出原整体预算。make test及verify统一将整包超时改为45分钟，保持每个用例现有期限、race、串行包调度及全部用例。此调整仅修正整体运行预算，不放宽行为断言或单例时限；是否足够须重新完整运行证明。

## 补充发现：旧执行样本的保留授权缺失

审查补查发现上一轮日志还包含TestActualV10ExecutionUpgrade独立失败（0.19s，任务QUEUED而预期COMPLETED）。此前只报告整包超时遗漏该失败，现予以纠正；45分钟预算不构成对此缺陷的修复。定向实际旧编码测试再次失败0.257s（19-upgrade10-repro.log），因此停止新的完整重跑；会话45680实际退出143，日志19-full-verify-upgrade-interrupted.log不计通过。

定向探针确认任务版本2与原Qualification匹配，模拟时间1788998400早于截止1788998700；报告顺利送达，但Effect=CONFIRMED、Result=FAILURE且无结果引用。保存边界的metadata读取返回PERMISSION_DENIED（19-upgrade10-save-probe.log）。旧fa498ce样本中的输入产物没有UseID，artifacts.invalidateUses将缺少原保留授权证明的旧对象置cleaning，因而执行结果保存无法读取来源元数据。未修改原编码样本或放宽权限。探针已从实现及测试中清理，兼容与安全语义仍须核对后修复；19保持未完成。

## 旧编码升级的显式保留迁移

新增本地受信端口artifacts.MigrateLegacyUse，不通过SDK暴露。升级配置独立固定原ContentRecord编码摘要、引用、主体及PUT操作；调用宿主先检查原Started执行及输入关联。迁移核对原对象仍available、原PUT、当前身份与store/process/retain/discover及所有来源约束、失效水位、期限和正文完整性；两次事务之间验证实际正文，最终事务复核同一对象并原子登记当前Use及绑定原对象。不改写原正文/操作/期限、不补造历史授权、不重新执行目标。已失效Use不会重新激活，普通访问仍拒绝无证明旧对象。

实际fa498ce夹具保留原二进制和checkpoint，只新增升级前离线固定清单及显式宿主调用。原失败测试在迁移后保持COMPLETED、成功结果与引用、原许可和一次目标效果，race1.288s通过。六项拒绝/重开重放边界race2.611s通过；进一步加入提交前73/提交后74实际退出再重开，完整定向race3.140s通过。没有把QUEUED改成可接受的正常升级结果。详细证据为19-legacy-migration-*日志，双轴审查无新缺陷；相关完整回归与整票verify另记实际结果。

artifacts及executioncheck完整race回归实际通过，见19-legacy-migration-regression.log；相关go vet、gofmt及diff检查通过。随后重新执行整票make verify，结果未出前保持19未完成。


## 最终完整验收

2026-09-12，7b32e5a代码的make verify实际退出0；25阶段通过、1531项必验CLI结果通过。catalogcheck完整包1974.111s、executioncheck18.414s，包含此前失败路径。原始日志、全部JSON报告、原票需求映射及限制见[最终验收](19-final-acceptance.md)。19原范围完成；第18票真实模型效果保持独立未完成。
