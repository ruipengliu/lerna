# 04 票02：同事务准入来源事实供保留期调度消费

2026-10-04，授权 Astra/high 窄性能决定；非源码或执行验收。基准固定 `75202ee0077b5bf12b432b5017ad97a1dfcab681`。本次还按授权读实际 WIP `domain/content/{closure,service,management}.go`、现 `ports.go`、PG management 转发、原有限 profile 文本；WIP 只作为候选接法事实，不当固定交付。没有 native/Go/build/DB、环境、清理或源码修改，也未读其他审核轴。

## 1. 采用具体小改，不增加另一个调度接口

**现在采用既有 `Repository.ScheduleRetention` 增加完整已锁来源记录参数 `sources []Record`；PG 仍原样转发给域内同名函数。** 参数位置可紧跟目标 `record Record`，其余 budget/tx/owner 语义不变。当前两个实际调用点是 Put 新版本准入，以及已有版本新 Command 合法收紧 cap；二者均已经在同一 Tx 完整调用 `registeredClosure`，WIP `closureObservation.records` 已包含按原身份确定性排序的全部准确来源记录。直接传这个集合，不再在管理调度入口构造 Service、从直接 Sources 重遍历和重锁同一来源图。

只增现有内部消费口一个事实参数；不加通用观察缓存、权限凭证、注册表、批量政策 SQL、索引或公开机器合同。现实际适配器是 PG 一个，不以两个 Put 分支冒称两个适配器。管理仍拥有到期/责任业务，不把它移回 Service。

理由足够具体：结构重复由现源码直接可见；原 CPU profile 显示 scheduleInherited 累计约 5.14s，Owner 报告 nil-only 原完整 race 仍 i59、60.03s 失败，累计 Install 1.209s / Put 31.051s / Step 27.022s。此前“暂保留重遍历、测量后再评估”的条件已经满足。5.14s 是包含子调用的累计 CPU 样本，不是可全部消除的墙钟时间，不能与其他累计项相加，也不能据此宣称本次必使完整测试通过。原 profile 文本的工具退出0不等于原 race 用例通过。

## 2. 参数的准确事实与来源约束

`sources` 是内部可信调用的**结构事实输入**，不是任何主体可提交的许可。使用现成 Record 无需再设计第二种生命周期 DTO；调度只消费其 Ref、CurrentRetainUntil 和现有 register/legacy 真正需要的记录字段，保持原 record 内容，不伪造/重写来源的 Subject、Purpose、Sources、holder 或原期限。不得把 bytes 复制到耐久观察账本；published 来源现有记录值随此短 Tx 消费并丢弃。

调用约束必须在 port 注释、两个调用点和实际转发测试中清楚体现：

- 仅来自本次成功结束的完整 `registeredClosure`；不是 request.Payload.Sources，不能截断到64，不能把 error 时的半成品传入。完整零来源对应真正完整空集合，不能把 nil 当“懒得验证”。
- 与当前 tx、目标准确 ref 和其不可变直接声明一致；全部中间版本、tuple 身份去重、同身份 fullRef 冲突、cycle、owner、published、≤64 均已由该次遍历核实并锁住，目标自身不在其中。
- 沿 Service → Repository decorator → PG → 域内调度的同步调用栈原样传递；不得持久化、放入 Job、保存供下一 Tx/重试、由别的 tx 的记录拼接，或令 decorator 忽略/替换集合。当前 ports 是受信内部边界，不为恶意适配器新造加密来源证明。
- 从观察到消费之间，当前两个 Put 路径只修改目标版本/来源边/待办，未修改祖先记录；来源版本锁保证并发 writer 不能改变祖先。未来若这段路径自己更新祖先，必须废弃相应观察并在同 Tx 重读真实改后记录，不能以“仍持锁”推断本地旧值仍当前。
- 维持原确定性顺序；不能用新的 map 遍历顺序改变锁顺序。轻量长度/目标排除等内部不变量检查可以保留；不再次执行整个 JSON 编码/版本图遍历来“证明”刚从域内成功遍历得到的同一集合。

这是以准确调用来源约束内部接口，未声称一个随意构造的 `[]Record` 能自行证明完整性。若实际出现额外调用方无法满足这一约束，应令它自己完成原遍历，而非默认相信其列表。

## 3. 主体、政策与调度责任不得复用错位

完整来源图与主体无关；资格不与主体无关。原 Put 是以**本次请求主体**逐来源实际 read/process/save 验证，所得 bound 只用于本次请求准入。管理随后必须保持现 `scheduleAdmission/scheduleInherited` 的原保存责任口径：

1. 目标维护政策在请求 policy.Subject/Purpose 与 **目标 record.Subject/Purpose** 不同时，仍重新 LockPolicy 取目标原保存主体/用途的当前准确政策。完整 Subject 编码比较，不只比较 subject_id。
2. 对每个来源准确 ref，仍以 **目标 record 的原保存 Subject/Purpose** 调用 LockPolicy；既不是新的 alias 请求主体，也不是该 source 自己最初保存时的 Subject/Purpose。来源记录只是结构/保留 cap 事实，不授予目标原主体任何动作。
3. 仍独立读取真实 policy revision 的原 basis，核准确绑定，按原 ValidUntil/RetainUntil/source.CurrentRetainUntil 计算 due，并执行原每个 target/source/revision/due 的定点责任键、既有同键一致性、原 budget、freshNow、registerNatural、work revision/最早 due 与 Trigger。
4. 不删除当前政策检查/原主体缺政策拒绝；不拿请求已通过的 Policy 数组代替原主体政策。每个 change 的独立义务、全主体/用途/五动作分类、旧水位覆盖、历史撤权 pending 和自然期续约分类均保持。

特别注意 scheduler 的即时 registerNatural 可能收紧**目标**记录：它不使祖先观察失效，但最终 post-schedule admission 门必须消费目标同 Tx 真正当前 cap，而不是仅沿用调度前本地 target 副本。若某调用路径可产生该变更，沿现 LockVersion 重读目标/传播实际改后值即可；不因此恢复全部祖先重遍历。原 adopted 回滚+最终当前门继续是必要条件，不能为速度跳过。

## 4. 域内实现划分与旧数据路径

把现 `scheduleInherited` 的“完整结构遍历”与“对给定完整来源记录逐项调度”在同一 management.go 内作小型私有拆分：普通准入调用后一段；legacy 独立入口保留前段，并再调用同一调度循环。无需导出新的策略或管理服务接口。两条真实调用路径共享实际义务算法，旧数据路径仍独立取得结构事实。

`RebuildSources` / restoreLegacyMaintenance 必须保留原行为：尤其 legacy 缺 basis 时会修改 source，再 `LockVersion` 读取 source 改后事实、重新读 basis 后才算 due。不能把早前回填遍历结果直接覆盖这些改后记录；本次优化不跨 legacy 改写边界复用旧值。不改旧 archive、0001/0002、既有身份/key/预算、原 deadline、水位或恢复准则。

## 5. 必须保留的验证与下一决定门槛

- 原完整64准入、65拒绝、中间版本闭包的 normal/race 与有限60秒出口仍不变；先比较相同输入阶段耗时和实际重复 LockVersion/遍历数量，再判断是否足够。没有 green 前不能称性能问题已解决。
- 新准入与 alias cap 收紧均真正经过该参数；实际 decorator（包括 post-ScheduleRetention 故障门）必须原样转发。覆盖不同完整主体/用途 alias：请求自身资格通过，但维护仍观察目标原保存政策，不能把其历史撤权/缺失/期限洗掉。无收紧 alias 不凭空新增义务。
- 全部 inherited due、续约/历史撤权、late 原 deadline、水位外新 D、独立责任、旧过期 archive 恢复的公开观察保持；legacy source 改后重读保留。保留带动作遍历的 per-node 当前资格与错误顺序，不扩大本次到 clock 消除。
- 调度后迟到门仍真实先做调度写再等待，并验证 rollback 与原键固定拒绝。源码调用次数观察仅证明重复读取消除，不替代公开义务事实与恢复测试。

如果此改后仍不足，不默认继续新增 port 或批量/缓存：由 sole owner 使用原有限条件记录新的 Put/Step/管理阶段或调用数，再依据实际瓶颈决定下一小改。当前推荐已经有真实重复路径及有限测量支持，不必先造新诊断接口才实施这个受限变更。
