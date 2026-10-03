# 03 存储接法准备决定

**正式采用：2026-10-03，前置02已按源码5548744/退出文档df2dbe5完整退出；授权决策代理完成最终接法复核，root据[最终交接](final-handoff.md)采用本记录。下文准备时的pin/pending状态保留为历史，当前接法及采用门槛以最终交接为准；没有宣称03已经实现。**


日期：2026-10-03。依据 root `d89e78900f1a46b4b3ba6cfdc1bc5795feb20839`、`/tmp/lerna-03-port-recheck.md`、实际 runtime/admission.go、PG store/commands/host migrations、AGENTS/CONTEXT、runtime/data-model 和 03 spec/票01草稿。05/06尚未作为完整退出能力采用。本记录只在 /tmp，不发布票、不实现03，不修改已发布迁移。

## 结论：独立 Decision owner 存储，显式新版接纳

采用专属 PostgreSQL Decision owner adapter、自己的迁移序列、Decision 事实表、Command 固定回执账本和 Job 表。Job 外键指向真实 Decision 身份；不插入 demo durable_inputs，不移除旧表外键，不让新版回执经过旧 CommandReceipt codec。旧 demo 的数据、迁移、账本和运行入口继续保留。

这是当前第二个实际消费者所需的存储实现，不建立通用 owner 注册器、动态表框架、所有版本的万能账本或新消息系统。Decision 组件消费的小端口声明在 components/decision_engine，adapter 实现；host 只装配。新的 adapter 按事实 owner 分包，默认放 adapters/postgres/decision_engine；迁移与其 embed 放在该 owner 包可直接管理的位置，并记录其 owner/版本/checksum。不为迁移目录形式暴露内部 SQL 或引入跨包循环。

### 1. 迁移与最小数据布局

- 新 owner 的初始迁移建立 Decision/关闭记录、固定命令账本、必要 Job 及实际索引/约束。迁移 ledger 与旧 host 序列隔离，不能把新 owner 的 0001 当成旧 host 的 0001。默认部署到明确配置的独立 PG schema；同库并不合并事务 owner。
- Decision 主键包含 tenant、逻辑 owner、decision_id；输入摘要是固定字段。状态采用已批准的闭合变体。cancel-before-decide 可以只保存真实已知的关闭绑定和摘要，不要求不存在的 Snapshot、完整 decide 输入或假 Job。初始布局应能表示该变体，取消处理仍由票03交付。
- Job 的唯一工作键按 Decision 身份及实际 phase；外键指向上述真实身份。固定输入的同 Decision 重传不推进 work_revision、不再次 Trigger。内部恢复阶段如确实产生新责任，修订由组件在同 Tx 显式推进；不得为了套用 demo 合并输入语义而改写原 Decision 输入。
- 账本主键仍为 tenant/owner/command_id，保存准确 contract_version、method/profile、受信主体、原摘要和固定 1.1 回执及必需 metadata。版本不能加进主键后允许同 owner 的同 command_id 绕过去重。Decision 身份摘要与 Command 原键摘要分别裁决。
- 不为不存在的旧 Decision 部署制造升级夹具。首票验证空库、新 owner 迁移可重复核验/checksum、旧 demo 已有数据在安装新 owner 后仍能准确读取。片内后续真正新增迁移时用上一真实版本产生的数据验证向前升级；旧 host 0001/0002 等已发布文件不得改写。新库布局没有要求迁移旧 demo 行。

### 2. 同 Tx 和机制复用边界

同一个 Decision Store 实例必须实现该 owner 所需 TxRunner/Clock、Job/Claim 机制和组件仓储/新版账本端口；接纳 callback 内的所有写入必须使用它创建的同一 active opaque token、同一 owner 和同一 sql.Tx。不能组合“旧 Store.Within + 新 Store.Repository”，即使 DSN/schema 相同也不能借用另一实例 token。

继续复用 runtime 的 Tx/Clock/Job/Claim 值和机制端口中实际适用的部分。1.1 生成的身份类型与旧 runtime 身份类型若是不同 Go 类型，在边界显式、完整、经过校验地转换相同身份字段；不得以此转换新版 receipt 或缩小其错误集合。领域状态、Proposal、Decision 限额归组件，不移进 runtime。

新 adapter 的 Claim SQL必须针对新 Job/Decision 表实现相同 fencing 保证：锁定真实 Decision 输入后领取、完整 worker/epoch/revision/lease 校验、锁后可信 Clock、终态不可复活、有限租约/事务。旧 demo 的 Trigger/StopRevision/schedule SQL不是直接可用实现。复用其规则和受影响真实测试，不宣称接口相同就已经正确。

两个实际 PG owner 若需要复用事务建立、token 生命周期、有限设置、commit-unknown 分类或机械 Claim SQL，可在适配层抽取有两个真实使用者的私有实现；抽取必须保留实例/owner/token 隔离及显式固定 SQL/table bindings。无需先建该抽象，也不得复制一份完整 demo Store 后仅改名作为新组件。局部 helper/构造函数/文件名由实施者据最终02代码确定；不能以避免代码重复为由取消 owner 外键或放松 token 检查。

fixture source/publisher 是另一实际 owner。读取、发布及读回发生在 Decision Tx 外；随后在新的短 Decision Tx 中核验原绑定、Claim 和当前状态。它们不能借同库成为跨 owner 原子事务。发布后崩溃依靠已定原 publication key/digest 恢复，不以持事务等待来处理。

### 3. 1.1 回执账本和接纳入口

选择组件消费的专属、强类型 1.1 账本端口及显式 1.1 接纳路径。其数据用 contract/v1_1 的实际 CommandReceipt/TransportOutcome 表示。旧 runtime.CommandRecord/CommandStore/Admit 保持原用途；不能强制把新版 receipt 装入旧结构，也不将它们全改成 any、未验证 raw JSON 或字符串错误码。

新路径复用 runtime.TxRunner/Clock，在组件接纳实现内组织同一短事务：

1. 验证准确版本、受信身份/授权和 owner，锁原 Command key。
2. 已存在时比较原摘要；同摘要返回原固定事实，异摘要报既定 idempotency_conflict。该判断先于新接纳截止判断，不覆盖旧账本。
3. 新命令在锁后取得可信 now，执行截止与前态裁决，再锁 Decision 身份裁决第二重幂等。需要时按最终02有效锁序取得容量协调资源，然后 Decision → Job；不得引入反序锁。
4. 首次成功同 Tx 保存 Decision、固定 accepted 和必要 Job；同 Decision 同输入的新命令仅保存关联原事实的新固定回执；异输入保存新命令的固定 decision_mismatch，不修改原 Decision。
5. 只有 COMMIT 确认才返回 received；真实不确定提交映射新版 commit_unknown，保留原 CommandRef 和 query/retransmit-original 动作。取消的专属 decision_cancelled 回执也只由新版 codec 校验和保存。

这是一条当前新版组件的实际路径，不另建可插拔版本分派框架。接纳的领域分支由组件决定；若实施中发现旧/新入口的纯事务控制有明确重复，可抽取一个内部机械函数而保留两端强类型外观。不是本票先决条件，也不能靠模板复制替代并发/提交不确定测试。

### 4. 两个查询版本与准确 owner 路由

- 新 Decision 账本只保存准确 1.1 原事实，使用1.1 codec保存与读回校验。新版 command.get 返回该事实的完整表示，不根据当前 Decision 状态改写原 accepted。必要时只读 progress none，Decision 当前状态由 decision_engine.get 返回；不得伪造 TaskProgress。
- 旧1.0入口继续使用既有闭合 Schema、错误集合、ReadCommandFacts 和 unavailable/dependency_unavailable 规则。为新 owner 装配独立的只读1.0兼容 reader：从同一个权威新账本读取原事实，仅在旧 codec 能完整校验、无损表达原回执及准确 refs 时返回；新拒绝不能表达时返回 reader error，既有旧入口映射 unavailable。不能把 decision_mismatch 改成 conflict，也不能假称旧顶层自然得到 version_unsupported。
- 旧 demo owner 的查询仍路由旧账本。若新1.1 command.get 同时服务旧 owner，用显式只读适配校验旧事实可以无损表示于1.1后返回；旧事实版本/digest不被改写。没有“新表未找到就试旧表/别的 owner”的搜索回退。
- Host 按已认证租户、准确 owner 和入口版本选择 reader；各版本 reader 可用独立 wrapper，不要求同一个 Go 方法以不同返回类型实现两个接口。原CommandRef、response ref、receipt ref仍须准确一致。兼容 reader 不给旧1.0开放 decide/cancel，也不扩大旧协商广告。
- corrupt/不可表达记录均不能变成 not_found 或新造 rejected。固定事实读取与当前状态读取分别遵守既有授权，身份字段不来自 payload 覆盖。

## 首票证据与后续采用时点

票01仍是完整 tracer：typed 1.1 Go/TS → 受信 decide → 实际 PG 同 Tx accepted+Decision+Job → 实际领取/规则 Proposal/耐久必要发布 → 新版 get/command.get及重开。不能拆成只交 Schema、只建表、只写 worker 的横向半票。

首票需用真实 PG 证明：业务故障回滚后没有孤立 accepted/Job；成功重开可完成原工作；同 Command 与不同 Command 同 Decision 的并发去重；不同输入固定拒绝的新版读回与旧 reader 不可表示路径；旧1.0原有黄金和 demo 事实不变；foreign/expired/wrong-owner Tx 拒绝；正常 Claim 完成对照后再测过期/旧代次拒绝。不能只读私有表替代 Component 行为验收。取消专属行为归票03，首票不提前宣称全 profile 完成。

**采用门槛：02整片真实退出后、发布/启动03前，复核最终 SHA 的 Tx/Claim、05资格门禁、06容量/锁序、07账本墓碑及迁移、08故障设施。** 当前选择的独立布局、强类型新版账本、旧无损读取边界已经定案；具体 helper 复用及最终协调锁接入只有那时才能锁定，不能把05草稿当成已通过机制。若最终02接口不支持新 Store 的机械复用，按上述 owner adapter 实现实际必要端口，不伪造 demo Input 或改变旧合同来迁就。

03全片退出时再次复核实际存储/迁移版本、1.1源码和 Schema 摘要、完整 profile 支持清单、所有票和故障证据，然后冻结/开放已完整实现的新能力。不能把“等03退出再复核”理解为首票在没有上述布局选择时先随意实现，也不能提前启动03。六票真实依赖不因本接法改变，root全片整合不是票06的隐藏依赖。
