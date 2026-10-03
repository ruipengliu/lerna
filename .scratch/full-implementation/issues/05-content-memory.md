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
[23 外来内容登记](23-foreign-content-registration.md)。领域矩阵已通过；独立设备/Task 的
传输宿主装配仍独立验收，不把网络 adapter 接口可编译称为完整交接通过。

工单保持 partial：生产跨位置对象传输、端侧挖掘/自然语言提取、真实外部镜像/SDK/备份
物理清除和三 AZ 对象耐久未验收；相应入口未配置时明确拒绝。
默认闭合 RuleExtractor 不宣称自然语言质量，未知经验不自报成功。
