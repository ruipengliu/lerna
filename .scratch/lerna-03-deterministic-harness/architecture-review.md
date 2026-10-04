# WHOLE03 固定源码架构 refresh

2026-10-04 09:57 UTC。**必要新架构重构 finding：0。** A/B/C 已采用建议保持兑现；历史 writer 的共享机械 module 及两个独立 FINAL01 业务 consumer 保持。D 的 CurrentClosed 仅是未来 Worth exploring，不成为退出条件。新增 binding 检查、合同开放和有限 race 入口不需要新框架或 ADR。

## 固定范围与证据资格

- whole 基线：`df2dbe5624120bc258dc7419ea022a08ebc0d6b0`；本次固定源：`6bf57501a5003ff139e96aec40589ad49fcba1ec`，读取 WT `/tmp/lerna-worktrees/deterministic-harness-whole`。只读固定 git objects，不读移动业务树作为结论。
- 前次已完成热点扫描：`b0beb2da45204276029d5dd11bfcec2439d46dfa`，报告 `/tmp/lerna-03-final-architecture-scan.md`。本次实际增量 **34 paths**；whole 实际 **163 commits / 357 paths**，均通过只读 Git 计数核实。完整 commits 清单已读：`/tmp/lerna-03-whole-review-commits.txt`。
- `/tmp/lerna-03-whole-review-fixed-provenance.json` 的 `fixed_head` 是 6bf，`preliminary_head` 是旧 384；前者才是本报告 pin。provenance 的 Git object 相等性索引是范围证据，不能替代代码审查。未把该索引所有条目冒充本代理逐行审计。
- 本次读取增量产品、公开测试、Schema/生成物、Go/TS 协商、Bash/Make/CI 与相关文档变化；重核旧 reconfiguration regression 和真实 Source control-only authority 测试。未变的 Prepared/worker/Child、Source/PG control 与 archive/supervisor 延续前次实际阅读；不重读或重新 hash 整个 archive。
- AGENTS、skills、ADR 在本增量未改变；继续使用 improve-codebase-architecture/codebase-design 的 module、interface、depth、seam、adapter、locality、leverage。CONTEXT 是单一领域上下文，没有新增 GLOSSARY。
- 本报告是固定源码热点的最终 refresh，**不是 whole357路径逐行审计、测试执行、发布、push、CI 或 whole exit**。未运行 build/test/DB/服务/外部 API，未读取环境或凭据。HTML 只写临时文件并按要求尝试打开。

## A/B/C/D 采用建议闭合

| 项 | 本次实际变化/保留 | 判断 |
| --- | --- | --- |
| A Prepared 私有视图 + 每次 publication gate | `prepared_v2.go`、`worker.go` 与 b0 对象相同；新增 V2/rule3 控制公开测试 | Strong 保留；源码和测试面组合已具备，执行证据另记 |
| B current control/proof/replay + SourceOwner | `service.go` 增加两行 fresh binding 检查；控制、metadata、Source/PG control 未变 | Strong 保留；原权威及优先级未被抽象遮蔽 |
| C Child 三个实际 consumer | Child、demo、target、Decision 及借用协议未变 | Strong 保留；三个 caller，不是三个 OS adapter |
| D 历史 restore/compiler/producer | archive、恢复 module、control/proposal 两个业务 consumer 未变 | 保持已有复用；CurrentClosed 仅 Worth exploring |

## A 的测试面从“待提交”变为实际已提交

`conformance/component/durable_control_test.go` 增加 `TestDurableControlPreparedV2StopsNextPublicationAfterArtifactReadback` 和 `TestDurableControlPreparedV2StopsFinishAfterProposalReadback`。后者实际跑 candidate、delta_only、actions_four；复用 ticket02 的真实 `proposalScenario` Source 数据，不从 Prepared 上限推导一个并不存在的多 artifact 生产场景。

新增场景经公开 decide/Cancel/get/GetCommand，观察真实 Source 已发布内容与原 publication tuple、被拒绝的旧 worker、冻结 usage/receipt、both-owner reopen 后 cancelled/current_control 不变。candidate 用实际一个 artifact + Proposal 两次发布；无 artifact 的 delta/actions 不虚构产物。原 V1 场景仍在。

**Deletion test / interface。** 两格式继续在私有 `preparedOutput` 归一化，发布 orchestration 不复制。测试复用的是装配与故障同步点，各业务输出仍明确断言；不把 private helper 数量或调用次数当合同证据。该 module 的 locality/leverage 已成立，不需要两个 Prepared adapters 或策略 registry。

## B 的 fresh binding 修正：最小且有真实 regression

`components/decision_engine/service.go:257` 附近，在已有同 input、非 cancelled Decision 的**新 Command**路径，现检查当前 `Config.Component`、`durable_rule_start` 与实际支持 RuleVersion；不满足时固定 unsupported。此前该分支直接 accepted。新对象路径原来已有同一条件，现为两个明确业务分支保留相同短表达式，不为两行条件增加策略模块。

当前认证与原 Command 的固定 replay 在前；Decision mismatch、cancelled 优先于 fresh binding policy。这样不改原 receipt、不让旧组件新 Command alias 绕过现配置，也不复活已关闭对象。已存在的 `TestDurableDecisionOriginalReceiptSurvivesComponentConfigurationChange` 使用实际 Source 与 PG，明确先核原 receipt 再核 fresh-old-component unsupported；本次读了实际源码，不只引用旧审查结论。

`conformance/internal/decisionfixture/control_test.go` 的 control-only Source 场景保留 `errors.Is(err, decision.ErrForbidden)` 与原 cause；合法控制访问和 proof 不等于执行许可。该 Source authority 测试及实现相对 b0 **未变**，不把它写成 6bf 新增能力。Get 的一致 Decision/Stop 锁、proof Tx 外读取、terminal 与单调 Stop 分离也未变。

**Depth 判断。** current access、proof、replay、fresh binding、Stop 是不同语义，显式顺序提供 locality；抽成“统一许可”反而扩大 interface 的隐含规则。无需新读端口、通用 predicate chain 或第二 authority adapter。

## 新增 E — Strong：保留单一合同库存与准确公开协商

**Files。** `contract/schema/1.1.0/methods.json`、`scripts/generate.mjs`、Go/TS 1.1 generated values、对应 negotiation 公开测试及 README。

**Before。** 1.1 声明四方法，仅 command.get 可协商。**After。** 三个 Decision `advertised` flags 均为 true，与 command.get 合计四方法；Go/TS SupportedMethods 同源生成，输入/输出 Schema digests 保持原准确值。原默认 1.0 入口冻结；只改注释的 typed decoders 没有获得执行权，协商不认证、不准入、不运行请求。

新增 Go/TS negotiation 测试使用明确的四个合同身份和独立 shared digest goldens，以原始 JSON 调公开协商；验证准确唯一集合，以及错误版本、profile、任一 digest、未知方法的拒绝。删除旧“开发 profile 必拒绝”断言由新的正反例替代，不删除原 schema 隔离检查。

**Deletion test / seam。** 把广告清单手工复制进 Go、TS 或另造 runtime registry 会分散相同知识；现有生成 module 保持 interface 单一。Go/TS 是真实不同语言合同消费者，不据此声称第二 Decision 生产 adapter。结论仅为本地合同库存已开放；整片最终 CI/退出尚待，不能继续沿用前次 HTML 的“flags 仍 false”。

## 新增 F — Strong：保留有限 Bash 入口，不扩建测试调度框架

**Files。** `scripts/test-component-integration-race.sh`、`scripts/component-integration-race.test.mjs`、Makefile、`.github/workflows/check.yaml`。

**Before。** CI 自写 race 命令；整包 Component 曾真实120.073 s超时，882的固定60/42分组证明不能自动覆盖后来新增测试。**After。** Make 与 CI 共享 `make test-integration-race`，按 Recovery → 动态 Component 两组 → Source 顺序执行；保持每包120秒、count1、p1。脚本调用当前 Go `-list .`，将全部 Test/Example/Fuzz seed 按 TestDurable 前缀分为两个精确正向锚定集合，拒绝 discovery失败、重复、空总集、歧义输出；空单组不变成空 selector；native group 非零继续失败。Benchmark 排除与默认 go test 语义一致。

机械测试通过实际 Bash 入口及受控 Go-tool 可执行替身检查选择/退出协议，覆盖未来名称、Unicode、Example/Fuzz、错误传播与空组；使用已有 boundedBuild 保留未确认进程责任。这证明的是 shell interface，不是 Go/PG 业务。真实集成执行仍由 sole owner 完成。

**Deletion test。** 删除该窄入口会把列表发现、精确选择和错误协议复制回本地/CI caller；现有 module 有 leverage/locality。当前仅两组，不增加按耗时自动分片、并行资源调度或外部 runner adapter。测试内容增长导致某组再次超时应明确失败再据事实调整，不静默扩大 deadline 或漏选。

## 文档、运行证据与未关闭责任

CONTEXT 的增量准确区分六票42/42已关闭、本地完整1.1库存已开放和 whole review/最终CI待完成；当前 Component/contract/SDK README 也明确协商与执行资格不同。ticket03 handoff、partition、review cutoff 和退出文档是882的历史记录；其“当时未广告/分组待接CI”不能读成6bf的当前声明。progress/acceptance 仍保存整片待退出，不用历史5bc CI覆盖新源。

root 本次委派报告的新共享入口结果为 Recovery race **79.934 s / exit0**、Component **60项 race102.204 s / exit0**、随后 **44项 race11.633 s / exit0**；比882的60/42增加两个公开 Go negotiation tests，动态全量发现适配此变化。**Source session59859 在委派 cutoff 仍 pending**。这些是 root 提供的执行事实，本代理未执行或独立核对对应新日志，不将 Source 或整个新入口追认为 green。此前整包120.073超时仍为历史失败。

已读882资源审计的准确 scope 与 unknown 记录；没有重做资源观察。旧 `3540254111`、其 overlay 等未知责任保持，23组已确认 absent 等历史事实不能反推没有 ACK 的旧组已退出。此文无删除授权或动作。

## 最终建议与交付

保持已有深 module，**不新增必要重构、接口、ADR 或框架**。A/B/C 保留，E/F 的最小接法成立；D CurrentClosed 仅在将来真实 holder 增多/修复漂移时再考虑，不新增退出要求。完整357路径的两轴审查、最终准确CI、测试槽退出和 root whole退出独立处理。

新 HTML：`/tmp/architecture-review-20261004T095725Z.html`，包含 before/after、Tailwind/Mermaid CDN 和完整静态 CSS/文字回退。旧报告保留原 pin 和 cutoff，不覆盖成新证据。

打开记录：本次实际 `xdg-open /tmp/architecture-review-20261004T095725Z.html` 返回 **exit 3**，环境无可用浏览器，`no method available for opening`。未下载 CDN、安装浏览器或目视渲染；完整静态 CSS/文字回退已写入，文件生成不替渲染成功证明。


## Root 采用与后续本地执行事实

2026-10-04，root实际全文读取上述固定6bf扫描，采用0必要新增重构；A/B/C与E/F保持现有深模块，D仅未来候选，不增加退出前置。原报告09:57的Source pending是当时cutoff，保留原文。此后sole implementer的同一session59859实际finalexit0，Source28.271s；本地完整新shared入口已完成。最终交付1aa21fc仅五项空白/历史链接/审计文档变化，未改变产品架构；正式整合391b4d8完整tree等于该worker。独立两轴最终0，准确新push CI与整片退出仍待root核验。HTML为临时本机产物，正文及静态解释已保存在本记录；xdg-open实际exit3，未宣称渲染成功。
