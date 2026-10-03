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

实际验收绑定 Go 1.26.8/Linux x86_64、真实持久 SQLite 和本机 PG 17.11；
Schema/profile 与完整命令保存在
`/workspace/harness-dev-environment/memory-providers-verification.json`。
最近完整 `go test ./internal/memory ./adapters/objectstore -count=1` 通过（69.583s / 0.021s），
受影响查询 race 通过（52.324s），实际 PG 新分页/Content/Memory race 通过（44.007s）；
同范围 vet/build、gofmt、diff 检查通过。此前完整 Memory/objectstore race 亦已运行，
最近新增行为另用真实 SQLite/PG 的公开接口过期反例验证。

工单保持 partial：跨 owner 权威、跨位置对象传输、端侧挖掘/自然语言提取、真实外部镜像/SDK/备份
物理清除和三 AZ 对象耐久未验收；相应入口未配置时明确拒绝。
默认闭合 RuleExtractor 不宣称自然语言质量，未知经验不自报成功。
