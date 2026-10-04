# 05 content-memory

Status: partial
Blocked by: 01

依据 [实施规格](../spec.md) 与根 AGENTS.md，保留准确身份、负责方、事务、门禁和恢复。实际编译、公开接口行为、正反例及所需平台证据均通过后才关闭。

## Comments

Content/Memory 参考实现已集成，模块与端口范围见
[Memory 实现说明](../../../internal/memory/README.md)。已实现准确不可变字节与原上传票据、
ready/metadata 原子发布、当前许可与实际 processed 来源闭包、主体/用途/地点限制交集、
holder 登记、close/release/cleanup 独立责任，以及四种 Memory、唯一候选保存、纠正影响、
删除墓碑、连续 change_head、许可先于相关性的中英文检索与受控 view/pull/ack。
本机对象介质实际 fsync、核 hash/length，只声明 local_fsync。

修复 commit `883f066` 将 query/list 首次快照期限取原 QueryBinding 与五分钟上限的较早者。
`418ed04` 冻结 QueryRef/ScopeRef/TextRef，逐页同 Tx 核当前保留期和全部实际来源，
即使到期 Job 未执行也拒绝旧页；无冻结来源旧快照 fail closed，门禁检查计入原累计预算。
`0220d89` 补足 Spec/Standards 复核两项 P2：预算不足不推进未遍历位置，
exhausted 只表示冻结候选已遍历完；SQL/取消/提交未知错误原样返回，
不伪报来源变更。SQLite/PG 公开许可端口检查计数、context.Canceled 与 pgconn 40P01
故障反例红→绿，受影响 normal/race 15.851s/61.788s、PG race 50.341s 通过。
40P01 为获授权当前许可端口注入，不称真实数据库死锁实验。

`9368334` 修复合法 200 候选和 4088-byte 长解释挤进单条 QueryView 后超过
256 KiB 的持久上限。最多 200 Match 与三个准确来源各自固定 revision 1/JCS 摘要，
全部与原快照头同 Tx 保存，恢复不读取 latest。公开分页同时依据实际 JSON 字节提前分小页，
不丢候选、不改原游标位置/权限预算/TTL；词法与 literal 各遍历十页、原 query_id
重读仍为相同 sealed 结果。转义解释的单页限额及损坏 manifest 的准确摘要反例也已验证。
受影响 SQLite normal/race 30.329s/75.575s、实际 PG race 115.453s 通过；
同范围 vet/build、gofmt 与 diff 检查通过。没有扩大 Runtime 的 256 KiB 上限。

`d4ef33e` 聚焦修复 Standards 新确认的最终 JSON 尾部：合法三项回复接近 256 KiB 后，
后续实际权限不足仍可能增加 gaps/推进 cursor，导致完整回复被 Dispatcher 拒绝。
公开真实 SQLite 反例红 3.255s；保守预留两种有限许可 gap、最长 cursor 和最长 boolean 后，
SQLite normal/race 2.029s/15.740s、实际 PG race 16.566s 通过，200 长解释与转义分页回归
6.922s 通过。满页立即停在已发位置；次页实际预算不足 empty/partial/gap 保原 cursor。
Spec/Standards 均只读复核闭合该 P2，无新 confirmed 缺陷。

实际验收绑定 Go 1.26.8/Linux x86_64、真实持久 SQLite 和本机 PG 17.11；
Schema/profile 与完整命令保存在
`/workspace/harness-dev-environment/memory-providers-verification.json`。
最近完整 `go test -p 2 ./internal/memory ./adapters/objectstore -count=1` 在 `9368334`
通过（81.631s / 0.036s）；最终输出尾部增量另按上述双库/race 验证。
受影响查询 race 通过（75.575s），实际 PG 当前来源/分页/快照 race 通过（115.453s）；
同范围 vet/build、gofmt、diff 检查通过。此前完整 Memory/objectstore race 亦已运行，
最近新增行为另用真实 SQLite/PG 的公开接口过期反例验证。

`683bb2f` 补入显式跨 owner 当前来源门禁和 Memory 原引用 holder，范围及双库证据见
[23 外来内容登记](23-foreign-content-registration.md)。领域矩阵已通过；Native默认App.Run HTTPS、独立设备Task完整写报告/Result与费用、Remote双方业务报告有后继准确证据。它们分别绑定各自source/平台/故障范围；Remote正常双方报告／三层Closure／Task及原Grant费用与必要Job已在fresh双库通过；旧r5与当前Session no-child失败、剩余故障和最终组合资格独立列明，不能用adapter可编译替代交接通过。

正式来源成本leaf8dd3013以本次已强锁Content/Policy元数据有限复用，保每条来源边/历史路径/当前auth/Now/权限预算，不跨entry缓存许可；两库实际RED12content/24policy→GREEN8/2，当前auth核次数仍12，affectedrace181.473s通过。恢复与新entry/主动close/两种independent路径/原分页错误cause均保留。

工单保持 partial：生产跨位置对象传输、端侧挖掘/自然语言提取、真实外部镜像/SDK/备份
物理清除和三 AZ 对象耐久未验收；相应入口未配置时明确拒绝。
默认闭合 RuleExtractor 不宣称自然语言质量，未知经验不自报成功。

unused证明失败收尾的公开两库正例17.224983s通过：原reserve过期终态rejected且put／transfer／native不存在，保留Published=false、原Job真实DONE、Task不变与原库重开。后续旧whole race469.422861s整体FAIL保留，其中10条unknown／native／Claim／CommitUnknown／accepted-reserve guards与2条过期失败正例实际PASS；仅2条原PUT已applied且Published=true的fixture随后读取未声明task.closure用途而失败。只修该fixture为准确content.read后，两库原PUT focused race70.652624s通过，生产字节不变，不把不同source的通过拼为旧whole绿色。完整索引 /workspace/harness-dev-environment/proof-publication-terminal-verification/final-exact-leaf-20261004T014127823091Z/verification.json（SHA6e3bd143…），旧whole /workspace/harness-dev-environment/proof-publication-terminal-verification/guards-race-ready-20261004T011920684672Z/process-result.json（SHAca737c21…）与focused /workspace/harness-dev-environment/proof-publication-terminal-verification/guards-original-put-focused-purpose-ready-20261004T013656470428Z/process-result.json（SHA6f4d8607…）分别保存。5文件正式叶已精确合Root373，集成编译与上述有限运行分开；后继原r5同Scope正确恢复已17.535538s通过，独立RO847ea998确认真实证明、失败事实和必要Job；其旧128.633s整体FAIL／原CID／TTL／Published=false仍保留，不虚称失败证明已出版。

原r5同Scope的corrected recovery已实际PASS17.535538s：Root579／895源与binary、原cfg／keys／token／数据库前后稳定，不新Task／Goal／Grant Use或续TTL。真实三层Closure／immutable Ack、父Task／原Grant reserve0、once保持、原双方Result不变；重开前后准确26份Published证明核原bytes／hash，额外旧unused18fce保Published=false／失败不可读，原expired reserve拒绝及无PUT／transfer／native事实不改。101项原必要Job实际DONE（parent44／child57），原publisher f162／f801与delegation／allocation／billing按原责任结束；四个实际handler join在两个Store.Close之前，LoadConfig原凭据重开再核全部断言。过程 /workspace/harness-dev-environment/normal-delegation-accounting-verification/original-r5-proof-set-corrected-ready-20261004T030949030145Z/process-result.json（SHA542916f1…），独立只读资格 /workspace/harness-dev-environment/normal-delegation-accounting-verification/original-r5-proof-terminal-independent-qualified-20261004T032244349699Z/index.json（SHA847ea998…）及 /workspace/harness-dev-environment/normal-delegation-accounting-verification/original-r5-proof-terminal-independent-qualified-20261004T032244349699Z/verification-result.json（SHA9c647396…）。原300.218s／128.773s／typed8.852s／严格128.633s与首次proof-set观察失败均保留，旧report终态rejected不改。只限定此原SQLite双owner恢复，不替代fresh SQL／PG正常报告或完整故障矩阵；原r5阶段观察到Task.Closure新IssuedAt产生另一proof／Job的历史保留；后继Gov三路径限定完整关闭Snapshot复用原首次proof，并有72.632s两库race证据，不扩为所有当前State查询幂等。

当前限定本地实现已完成逐项定点验收：远端Agent／独立设备的正常报告、current actor与父控制、无子Task永久拒绝的三层Closure／原once与费用责任、现代默认Goal Form、原关闭证明复用及Saved五秒证明消费门禁均有准确证据。Saved最新同一911路径源／race binary的五场景×SQL／PG十项actualPASS，正式7路径叶17662e564d121732b0fbac8ddbed938a7a7baabb已合当前CODE 58c9898af58babce7170cb37cbbdfc63d693af9e. 17按已开放有限静态profile记resolved；09仍partial等待最终固定版本完整检查、原cfg浏览器重开／全生命周期、两份独立全图审查与发布步骤，07保留浏览器验收待项。新prepared共享false测试分支的观察边界仍由最终整套检查取得资格。代码集成和各旧source行为资格分开，不能声明当前根全套已通过。真实账户／公司身份／开放自然语言质量／物理设备／其他OS／规模与多AZ为另外明确的未验收范围；旧FAIL／SKIP／NOTRUN及原业务身份、权限和期限保留。

原关闭Task证明复用的独立修复已完成：初始真实公共RED9.255281s确认同事实query按新IssuedAt产生不同Proof／Job。Gov精确三路径c3bf647已合Root699，仅真正完整关闭且同原Snapshot／当前披露资格时复用首次持久封存证明，仍强核当前主体／完整关系／来源；变化或未闭snapshot继续原流程。首GREEN46.761691s整体FAIL仅为新fixture随后用未声明task.closure用途读证明；两处测试改为原准确content.read、生产不变后，真实两库race72.632443s通过：三次同Closure／ProofRef／IssuedAt／bytes、一个原publication Job、实际Content全文hash／长度、join／原cfg数据库重开及零新证明Job。索引 /workspace/harness-dev-environment/task-closure-query-verification/final-qualified-leaf-7ocl3b52/ready-leaf-index.json（SHA0f9129f7…），集成SHA67a6f5c6…仅metadata。原r5恢复17.535538s／101必要Job／26Published＋1failed仍只绑定其原snapshot；本轮未迁移或重跑该旧Scope，也不把有限终态缓存扩为当前State签名的通用幂等声明。

统一阶段说明：当前CODE 58c9898af58babce7170cb37cbbdfc63d693af9e；本文实际通过只按所列原source／binary／selector及数据库制品限定。最新Saved十项、NoChild／Pause／现代Form／Closure后继资格已取得；最后完整检查／浏览器／全图审查尚未结束，旧失败／未跑记录不回填。
