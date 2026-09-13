# 22 票真实答案模型 v3 独立语义评价

评价时间：2026-09-13T03:59:00Z。评审者：Codex 独立 Spec 审查子代理；参与过实现和执行器的 Spec 审查，已看过公开冻结材料、既有协议答案及 v2 输出。本评价不是外部盲审、隐藏测试或被测豆包模型自评，没有追加模型请求。

**运行正式发布8/8；按冻结要求逐项评价，语义合格6/8，失败2/8。** 两例 fetch_failed 的 scope 不符合预先冻结的证据范围要求，其中 loopback 还写错任务期限日期。引用支持12/12与适用必要事实8/8，不能抵消范围失败。v2 的8例失败保持原记录，本轮不能推断完整V1质量门槛、公网成功或整票验收通过。

## 依据及可复核绑定

依据 [v3计划](22-prospective-evaluation-v3.md)、其沿用的 [评分方法](22-prospective-evaluation.md)、[冻结期望](../../profiles/searchcheck/testdata/expectations-v1.json)与 [执行锁](evidence/22-evaluation-v3-45ff5d3/execution-lock.json)。锁中九项文件摘要全部核对相符，执行器为 `45ff5d371b16e199712341287e5c56b68032e690`。所有原始证据保持不变。

逐例读取 case、reserved、model JSON；预留 Input 等于实际模型请求 Input，其 Input 又等于正式 Record.Input。逐例解码 ProviderContent 与 Content：原始整数选择按当前输入展开后与 Content 相同，Content、Record.ModelOutput 和正式 Record.Answer 的 JSON 值相同，没有人工修复原始输出。取得的短正文均落在一个选项中，两个不同主张引用同一选项仍分别计数。

通过本地临时 Go 工具逐例调用 `NewSemanticReview`，绑定 case 文件中 **Record.Answer 和 Record.Input 字段的原始 JSON 字节**，先通过原 Brain 结构验证，再填写下述独立语义判断；最后调用 `SummarizeSemanticReview` 核对绑定、完整性和计数。原字段中的空白格式参与摘要，不能重新格式化后冒充相同字节。所有八例 ReviewComplete=true、无未评项；此字段表示评价填写完整，不表示质量通过。

逐项理由、原答案/输入/期望摘要及机器可复核计数保存在 [semantic-review](evidence/22-evaluation-v3-45ff5d3/semantic-review/summary.json)，每例同名 `.review.json` 与 `.summary.json`；这些是新增评价文件，未覆盖执行结果。临时工具只运行本地评分接口，没有运行任务、联网或读取凭证。

## 逐例结果

所有例均有正式答案且状态与预期一致。必要事实分母仍为每模式2/0/2/0；零分母 N/A，不是100%。引用支持分母为实际主张—引用对；两例失败处置没有claims，支持率为N/A。

| 顺序/用例 | 必要事实覆盖 | 引用支持 | 正文 | 范围 | 处置 | 禁止项 | 语义结论 |
| --- | --- | --- | --- | --- | --- | --- | --- |
| [1 loopback/answerable](evidence/22-evaluation-v3-45ff5d3/semantic-review/loopback-answerable.review.json) | 2/2 | 2/2 | 通过 | 通过 | 通过 | 两项通过 | 合格 |
| [2 loopback/insufficient](evidence/22-evaluation-v3-45ff5d3/semantic-review/loopback-insufficient.review.json) | 0/0，N/A | 2/2 | 通过 | 通过 | 通过 | 两项通过 | 合格 |
| [3 loopback/conflicting](evidence/22-evaluation-v3-45ff5d3/semantic-review/loopback-conflicting.review.json) | 2/2 | 2/2 | 通过 | 通过 | 通过 | 两项通过 | 合格 |
| [4 loopback/fetch_failed](evidence/22-evaluation-v3-45ff5d3/semantic-review/loopback-fetch_failed.review.json) | 0/0，N/A | N/A | 通过 | **失败** | 通过 | 两项通过 | **失败** |
| [5 replay/answerable](evidence/22-evaluation-v3-45ff5d3/semantic-review/replay-answerable.review.json) | 2/2 | 2/2 | 通过 | 通过 | 通过 | 两项通过 | 合格 |
| [6 replay/insufficient](evidence/22-evaluation-v3-45ff5d3/semantic-review/replay-insufficient.review.json) | 0/0，N/A | 2/2 | 通过 | 通过 | 通过 | 两项通过 | 合格 |
| [7 replay/conflicting](evidence/22-evaluation-v3-45ff5d3/semantic-review/replay-conflicting.review.json) | 2/2 | 2/2 | 通过 | 通过 | 通过 | 两项通过 | 合格 |
| [8 replay/fetch_failed](evidence/22-evaluation-v3-45ff5d3/semantic-review/replay-fetch_failed.review.json) | 0/0，N/A | N/A | 通过 | **失败** | 通过 | 两项通过 | **失败** |

### 具体语义判断

- **两例 answerable：** 第一主张的14 May 1998与第二主张的84 metres均受Northbank原文明确支持，各引用一对。正文同时回答两个问题；scope明确2026-01记录的原桥，引用时间来自该次取得事实，没有扩大到改建后尺寸。日期/长度无误，没有把索引摘要当正文。两项必要事实均覆盖。
- **loopback insufficient：** 两对引用分别支持跨运河/2005年背景及记录未记载造价。正文明确只凭该份记录不能确认造价；scope限定单份2026-01材料和正确取得日。未编金额，未断言造价普遍不存在，也没有把背景年份作为造价答案。gap引用该份实际记录，处置通过。
- **replay insufficient：** 两对主张和支持关系同上。scope的URL、取得时间与本次 fixed-replay 输入一致；“没有其他来源”结合明确的 supplied/acquired 范围解读为当前输入限制，而非全球资料不存在。没有声称发生真实HTTP访问，禁止项通过。
- **loopback conflicting：** register/2001与archive/2003各有一对直接支持的引用，两个固定事实保留来源归属。正文明确不能从给定材料确定正确年份，scope限定两份2026版记录；gap同时引用两份来源。没有因较新版本选择2003，提到未区分开幕和公众通行是原文内容，没有用虚构区别调和冲突。
- **replay conflicting：** 两对引用还支持主张中“未解释开幕/公众通行区别”的限定。正文、scope和gap均保留未解决冲突，不选定某一年、不引入未取得信息；两项必要事实及两项禁止项均通过。
- **loopback fetch_failed：** 正文和gap正确说明denied、没有取得承载力数据；没有断言12 tonnes、编造页面正文或页面取得时间，因此正文、处置及禁止项通过。但scope只写官方承载力目标与任务预算，没有说明冻结的“索引已观察、页面正文未取得”范围；额外声称期限为2026-09-12，实际Input.Constraints.DeadlineUnix=1789271759，即2026-09-13T03:55:59Z，日期错误。`official published`的额外限定也不是当前输入给出的事实。范围明确失败。
- **replay fetch_failed：** 正文与gap承认取得被拒、承载力未建立，无12 tonnes、页面正文、虚构取得时间或真实HTTP403断言。scope虽正确复述Unix期限，却仅描述提问目标及预算，没有限定索引与未取得页面的证据边界，按冻结required_scope判失败。该条未向模型提供索引观察块，只提供denied/fixed-replay/Requests=0；宿主所选输入不足以让模型复述完整索引范围，不应通过临时放宽评分要求消除这一链路缺口。

两例scope失败不等于其拒绝事实或正文内容全部错误，也不等于发生隐私泄露。它们说明结构校验和引用支持无法替代对范围陈述的独立核对。其余六例的合格只针对本次冻结语义标准与归档材料。

## 请求和预算记录

八例顺序由Started字段核对，先loopback四例再replay四例；每例仅一次外部模型尝试，均Finish=stop。原始服务用量合计input9416、output2253 token，与summary一致；每例输出不超过512。查询依序为59、59、76、56、58、58、75、55，均不超过128；每例Core模型请求3、网络操作扣额2（conflicting为3）。loopback物理搜索各1、页面分别1/1/2/1；replay物理搜索和页面均0，但外部模型仍被调用且原操作预算仍扣额。

单例耗时依序6.761、8.886、11.993、6.972、6.540、9.299、12.444、6.431秒。8元为本批管理预留，不是实际账单；本评价没有账单访问，不认证累计真实费用。不得因语义失败退款、移除样本或另起任务替换这两例。后续变更或新运行需另立计划与锁定。

本轮是看过v2失败后修复表达方式的公开已知材料回归，存在材料与历史答案暴露，不能称为盲测或泛化评估。没有另行验证完整治理/撤权/恢复矩阵、全仓测试或公网豆包搜索；这些不由本次语义分数证明。
