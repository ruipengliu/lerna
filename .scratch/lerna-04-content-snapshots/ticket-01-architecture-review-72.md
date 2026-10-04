# 04 ticket 01 固定交付源码架构复核

2026-10-04。本稿按已授权 improve-codebase-architecture/codebase-design 做固定源码 follow-up，不替代独立 Spec/Standards 两轴、首票验收或04整片退出。

- 原复核 pin：`92d27992f37f0a425621bc75c6d5b4a073fa06a6`。
- 本次 tested-source pin：**`46d6ca26e4c2a4e9cf6db95e890b641f6bf2aa97`**。
- delivery pin：**`72da5ee820a69695553ff208e10a8ba31804d0b8`**。
- 实际计算原 pin→source 为1个 commit、22路径、1826 additions / 551 deletions；source→delivery 为10个文档/审查记录路径变化，无产品、工具或测试源码变化。
- 只读 immutable objects，复核全部22路径源码差量及新测试；读取根 AGENTS/CONTEXT、正式 read-gate/tool-ownership adoption、实际 handoff/evidence。没有读取任何独立两轴发现报告正文，没有执行 native/build/test/DB、清理、merge/push，也未读 moving tree。

**结论：已采用的架构深化和主要门禁结构全部落实；没有必要新增 module/interface/framework。但准确 target-policy 绑定仍有一项必要局部门禁修正 F1，因此不能把本稿记成“所有准确读取义务已关闭”。** F1 属既有 observe 内的小修，不需要重开领域设计或扩大后票依赖。其余 A/B/A1/A2/B1、当前直接来源/新关联/原重放结构及五消费者 ownership 提取均具备源码关闭资格。

## F1 — Strong 必要局部修正：Get target policy 完整 ref 尚未核对

**准确位置：** `domain/content/service.go:338–352,377–381`；`adapters/postgres/content/policy.go:94`。

target 的 read/disclose CheckPolicy 返回非 nil 就使用其期限。PG policy 查询与自身 guard 仅比较 owner/content_id/version/purpose/subject/action，不比较 hash/media_type/byte_length。随后 record.Ref==request.ref 只保证读取了请求的准确对象，不能证明允许它的 **policy.Ref** 也绑定该准确声明。

**源码可达反例（未执行）：** 已发布准确 target A；通过现有受信 fixture 管理入口安装同 owner/id/version、但 hash 或 media/length 不同的完整 Ref B policy，并允许 read/disclose；Get 请求正确 A。policy 查询可返回 B，record 检查仍通过 A；目前读前和最终披露共用 observe 都会放行。管理入口验证 B 自身编码和修订，但没有查 Content A 的完整 ref 来排除此情况。准确 source gate、Put 和 publicationPolicy 已各自做完整 policy.Ref 比较（`:207,404,742`），target Get 是遗漏，不是新的“只按 id 授权”模型。

**最小接法（授权顺序已澄清）：** 在 observe 保存两项 target policy 的完整 ref；锁后可信时间/期限裁决保留。record 非 nil 时，**先**核每项 policy.Ref==实际 record.Ref，否则 forbidden；获准确授权后才比较 `record.Ref != request.ref → rejected integrity`。因此正确 policy A/request B/actual A 仍是 integrity，而错误 policy B/request C/actual A 不得通过 integrity 泄露存在性。record 为 nil 时核每项 policy.Ref==request.Ref，匹配才 not_found，否则 forbidden。共用 observe 同时覆盖读前和返回前；不把 CheckPolicy 一概改成 policy 必须等于请求而误伤原正常反例。此顺序修正先前“after mismatch”的不充分建议，独立依据见 `/tmp/lerna-04-target-policy-ordering-clarification.md`；固定46的F1仍未关闭。

**必要有限观察：** 正常准确 policy/ref 读回对照；受信 policy 换成同版本异完整声明后，准确 target Get 不披露；若在真实 Read 暂停期间换错声明，最终 gate 同样拒绝。原 Command 历史进展和对象正文不由查询改写。无需新 SQL、合同字段、来源递归或生产 Grant。已发 root 供 sole fixer 处理；本稿仅源码推断，不冒称 runnable red。

## 已采用 obligation 的源码闭合

| obligation | 固定源码与判断 |
| --- | --- |
| A：启动/完成 current publication gate | `service.go:614,687,735` 仍复用 publicationPolicy；准确 target save、direct-source read/process/save、原出版期限和当前最严 cap 保留。policy 行锁后时钟及 owner 完成前时钟仍存在 |
| A1：暂时 defer 保存收紧界限 | `:706–713` Revision/SaveVersion 在同 Tx 内先于 DeferClaim；新差量未退回只写内存的分支 |
| A2：原 Claim 限制 I/O | `:658–665` IODeadline 与原 Claim.LeaseUntil、WorkTimeout 取 min；已过期 context 不进 Objects.Put。没有将合作取消当物理强制停止 |
| B/B1：本地 holder 与有限 drain | local Store 保留 root/directory/child 首次 Close 事实；CloseContext 只限制 drain，native Sync/Close 同步归原 invocation；原两处裸等待仍已删除，失败进入有限 cleanup |
| ReadGate：读前/披露前 | Get 私有 `observe` 在初始短 Tx 和完整 Objects.Read 后的短 Tx 复用；当前 read/disclose 和版本状态重核。readBefore 跨两次只收紧。对象 I/O 仍在 Tx 外；F1 是该内部准确绑定的剩余点 |
| 本次读取的准入时间 | observe 在 target 和各 direct-source 阻塞锁后重采 Now；not_found/mismatch 也先裁决 envelope。最终披露用 admission=false，未把 AcceptBefore 改为 I/O完成期限 |
| CommandGet 当前门禁 | 私有 finalRead 在 found/not_found 输出前重核 reader 并 Store.Now，包含 version 等待后的 reader.ValidUntil/AcceptBefore；历史 publication 不被 query 改写 |
| Direct Get | 从耐久 record.Sources 取集合，≤64、同 owner；每个核当前 caller/purpose 的 read+disclose、完整 policy.Ref、准确 published 和当前保留 cap；不额外要求 process/save/sync，不递归 source.Sources |
| 新 Command 关联同声明 | 取消提前 accepted；走准确 target save/direct-source read+process+save，交旧 Current/Effective cap，锁后重新 reader/Now。收紧 cap 同 Tx SaveVersion；关联在容量增量前返回，不创建 Job/正文，不重置原出版期限或 failed |
| 原 Command 重放 | LockCommand 后 checkReader，原主体/digest/receipt 优先；不重新适用旧 put AcceptBefore 或新的保存许可。新拒绝/准入后续等待也复核 reader |

policy adapter 不再把 RetainUntil 过期藏进 nil policy（仍校 ValidUntil/主体/action），当前 cap 交给 domain 精确裁决 expired。Domain 的各实际 gate 都纳入返回的 RetainUntil；这是错误分类和责任 locality 的收敛，没有开放无期限许可。

```text
先前：初始读取门禁 → Tx 外读正文 → 直接返回
当前：共享 observe(准入) → Tx 外读全文/核验 → observe(披露) → 返回
      target + 已登记 direct sources           同集合/更严当前 cap
      F1：两次 target policy 还需完整 ref 匹配

原 Command：锁后当前 reader → 原 digest/receipt
新关联：当前 put/source gate → 单调 current cap → 新 receipt/原责任
```

## 本地/World 生命周期的附加修复

`local/store_linux.go:58–74` 的真实 Lstat/EvalSymlinks/OpenRoot causes 与 ErrUnavailable 合并；partial Open 仍在持有 root 后登记 native firstClose，未知关闭返回 nonnil holder+原 causes。局部 lifetime 新正常/缺路径测试通过 errors.Is 检查原 os.ErrNotExist，旧有限 drain/child close 责任没有改变。

World.register 现在是 World 所属方法：ledger 与 parent FD 打开后立即 ownSetupFile；所有 registration/Sync/Close 错进入 setupCloseErr。Cleanup 先确认原 invocation/infrastructure/current/object，再尝试 setup firstClose，任何 sticky setup 错阻止 Drop/RemoveAll；准确 dev/ino 仍在物理删除前检查。新增 setupNativeClose 只供同包机械测试，未扩 public 业务 interface。

World.setup_lifetime_test 和 local lifetime 中的诊断钩子先观察真实 Close，再注入错误；反复 Cleanup 必须保留原 unknown。测试夹具只凭独立实际关闭与准确身份做自己的清理，没有清掉被测 holder 的未知标志。它们不是 native Close 物理失败证据。真实行锁 fixture 用 pg_blocking_pids 观察准确 blocker 只是机械同步，不拿私表当业务 oracle。

## 私有 ownership module：五个实际消费者完成接线

`scripts/conformance-ownership.mjs` 实际 interface 只有 `ownConformanceScope(dir, kind)` 返回 start/exited/finish；kind 限定 contract/generator、role 限定 compiler/producer。native 参数是同 module 机械 seam，不是生产替换平台。

- 初始化保存 dev/ino，Sync scope/parent，完整写 ledger 并 Sync；初始化失败携原 scope 与 causes。
- start 先保存原实体绑定，再完成 PGID/ACK；compiler 必须 group leader，producer 可继承 group。失败留在 faults，不能被后来的成功 ACK 或另一个 entity 的 exit 洗掉。
- exited 只登记消费者传来的原实体观察；缺失登记/非法后调用进入错误事实，不从 `/proc` 缺 pid 推断 Wait。真正 spawn/stop/Wait/pipes/group absence 仍在 boundedBuild/requireBuild/startRunner，源码没有搬入 helper。
- finish 对每个 exit ACK 分别捕获错误后继续汇总；聚合 primary、native cleanup、FD/ACK/stat/remove causes。全部 entity 确认且无 sticky faults 才按原 dev/ino 删除；删除后外部 removed ACK 失败准确保留 removed=true，不虚报目录尚存。
- 五个实际消费者均删除了重复 useFD/record/删除资格 finally：三份 contract 持有 runner 后登记，最终 allSettled 原 close，再传 exitConfirmed；两份 generator 从 onStart capture 原 pid、从 boundedBuild 原结果传退出事实并先聚合 result.error/cleanupErrors。requireBuild 的 void 返回未被拿来读 pid。

**Depth / deletion test / locality / leverage：** 删除此 module 会使 FD/ACK/unknown/精确删除协议重回五个消费者；它集中的是原 cause 和删除资格不变量，并非只转发 fs 调用。五个真实消费者获得同一修复，不需要假设第二 OS adapter。业务 fixtures、生成规则、各版 codec 仍分别保留，未建立 version registry、process manager 或 callback DSL。

机械测试从 start/exited/finish 观察实际台账、scope 存在性与原 cause，而非读内部 faults/map。覆盖 primary+secondary Close、write/Sync/parentSync ACK sticky、缺一个原 exit、identity 变化、初始化、remove失败和删除后 ACK失败；独立 fixture 先 ACK 自己的 scope、追踪真实 descriptor/原 bounded child，才允许清理被注入诊断所保留的测试目录。`Makefile` 已把该 suite 接入原 test entry。

## 执行证据资格与退出范围

本代理读取 delivery 的 tracked evidence/adoption/handoff，**未重新执行或独立核对原日志/资源现场**。其中报告的最终 modified-source `make check` session33660 exit0、35工具测试、两 generator、44 TS、158/89/101三版双序 Go↔TS 和 build，只作为原实现者的固定执行记录引用。affected race session95320 记录 Component40.887s/codec1.678s/local1.153s/fixture1.218s；domain 选定 pattern 无测试也准确注明，没有把仅 race compile 冒充 domain 用例执行。

记录分别保留新 target revoke、读取锁等待、direct-source、association/replay 的 red→green；机械诊断与真实 policy/对象行为分开。旧的 CLI void-PID失败、测试预期修正没有被重写为成功或业务 red。资源审计数量及退出事实来自该记录，不是本次扫描新增核实；历史02/03未知 scope 不因本稿变为可删除。

根 AGENTS/CONTEXT 的单上下文、消费方端口、短 Tx、unknown 责任与真实证据限制保持；本轮无必要新增 ADR。没有必要借修正 F1 扩展到02全闭包、05 holders、06 Grant、Task/Provider 或04 whole广告。C/D/E旧候选不在本轮重开，也不要求为了形式继续抽取 Domain helper。

**交付判断：** Strong architecture extraction 已兑现，新增必要框架重构数为0；**F1 需 sole fixer 小范围关闭并补对应实际观察**，之后只复核实际 changed objects 与受影响证据 pin。本文不把72da delivery 当作首票8AC已接受，不替root的独立两轴结论、merge/push/CI或04整片退出。

## 后续同 pin 窄裁决：四处 generator native-result qualification

Root 另提出四个实际位置：`scripts/test-generator.mjs:167–178,221–232,341–352` 和 `scripts/test-generator-v1_1.mjs:58–69`。本代理直接读固定源码；未读取 Standards 或 Spec 发现报告。

**决定：Worth exploring，当前保留，不追加为本轮必要提取或退出条件。** 四处确实共同聚合 result.error、cleanupErrors 和 !exitConfirmed，然后抛 AggregateError；可提取，有真实四调用点的 leverage。但固定实现四处都完整保留相同原 native facts，未观察到此段之间新的资格分歧。它们不再掌握 FD/ACK sticky 状态、exact scope 删除或猜 Wait，这些复杂责任已在真正 module 中收敛。剩余部分是把原 boundedBuild 结果映射到本场景 failure，再由既有 scope.finish 保 cause；没有必要为了把十余行压短而再次改变已受测工具 interface。

**Deletion test 与 locality 的权衡：** 若有一个 qualification helper，删除它会把相同表达式散回四处，故并非无价值 pass-through；但目前能隐藏的只是有限结果归并，没有另外一套资源责任状态机。潜在 locality 收益成立，强度仍低于本轮已发现的真实 target 授权遗漏。当前修正窗口优先 F1，避免将每个静态重复都升级成首票前置。

若后续实际新增 native result 字段/资格规则，或发生此四处 cause 分歧，正确位置是已有 `scripts/bounded-build.mjs`，不是 conformance-ownership：采用一个具体的 `nativeResultErrors(result)` 纯归并函数，返回原 error/cleanupErrors 与未确认退出的诊断，供这些调用点及 requireBuild 的 native 部分共享；requireBuild 继续自行追加非零 status，generator 继续各自区分预期 schema refusal 与成功。不能把“非零=失败”混进 native qualifier，不能合并各业务断言，更不能让它执行 Wait 或猜 exitConfirmed。此形状仅记录将来维护方向，本轮不要求实现或新增测试。

该裁决与 F1 分开：F1 是必要且待新固定源码/真实观察关闭；四处归并的保留不阻止其他已通过修正，也不降低现有 native closure/错误聚合要求。
