# 22 票真实答案模型 v5 独立评价

评价日期：2026-09-13。评审角色为Codex独立Spec审查子代理，参与过实现及执行器审查，已接触公开虚构材料、协议答案及v2/v3/v4结果；不是外部盲审或被测豆包模型自评。此次未调用公网/模型、未读取凭证，未修改原始结果、未提交文件。

**运行成功7/8，按冻结标准语义合格7/8，失败1/8。** loopback/fetch_failed没有正式答案，保留为失败；replay/fetch_failed正确区分索引观察与未取得页面。成功例有8对主张—引用，均有支持；replay/conflicting及replay/fetch_failed没有主张—引用对，引用支持N/A，不能写成100%。适用必要事实合计8/8，不抵消运行失败。

## 依据与复核方法

按本轮 [execution-lock](evidence/22-evaluation-v5-686ff10/execution-lock.json)、原 [评分方法](22-prospective-evaluation.md)和 [冻结期望](../../profiles/searchcheck/testdata/expectations-v1.json)评价。被测代码686ff10；这是看过先前失败后改进的已知材料回归，不替换历史结果，不推断完整V1质量、公网成功或整票通过。

八例reserved.Input均与model.Input相同。七例成功的Record.Input等于实际请求Input，展开Content的JSON值等于正式Record.Answer。评分先以case中Record.Answer/Record.Input字段的原始JSON字节调用NewSemanticReview，完成原Brain结构验证，再独立填写覆盖、每对引用、正文、范围、处置和禁止项理由，最后调用SummarizeSemanticReview核验绑定与完整性。七例ReviewComplete=true、无未评项；它表示评分完整，不替评审者认证语义。

[semantic-review/summary.json](evidence/22-evaluation-v5-686ff10/semantic-review/summary.json)和逐例review/summary文件保存可复核结果。失败例另用failure-review记录，绑定原case/model文件SHA256；因没有正式答案，不伪造SemanticReview成功，正式正文/范围/处置/禁止项不可评价。

## 八例逐项评价

| 顺序/用例 | 正式答案 | 必要事实覆盖 | 引用支持 | 正文/范围/处置/禁止项 | 结论 |
| --- | --- | --- | --- | --- | --- |
| [1 loopback/answerable](evidence/22-evaluation-v5-686ff10/semantic-review/loopback-answerable.review.json) | 有 | 2/2 | 2/2 | 全通过 | 合格 |
| [2 loopback/insufficient](evidence/22-evaluation-v5-686ff10/semantic-review/loopback-insufficient.review.json) | 有 | 0/0，N/A | 1/1 | 全通过 | 合格 |
| [3 loopback/conflicting](evidence/22-evaluation-v5-686ff10/semantic-review/loopback-conflicting.review.json) | 有 | 2/2 | 2/2 | 全通过 | 合格 |
| [4 loopback/fetch_failed](evidence/22-evaluation-v5-686ff10/semantic-review/loopback-fetch_failed.review.json) | 无，OUTPUT_INVALID | 0/0，N/A | N/A | 无正式答案，不可评价 | 失败 |
| [5 replay/answerable](evidence/22-evaluation-v5-686ff10/semantic-review/replay-answerable.review.json) | 有 | 2/2 | 2/2 | 全通过 | 合格 |
| [6 replay/insufficient](evidence/22-evaluation-v5-686ff10/semantic-review/replay-insufficient.review.json) | 有 | 0/0，N/A | 1/1 | 全通过 | 合格 |
| [7 replay/conflicting](evidence/22-evaluation-v5-686ff10/semantic-review/replay-conflicting.review.json) | 有 | 2/2 | N/A | 全通过 | 合格 |
| [8 replay/fetch_failed](evidence/22-evaluation-v5-686ff10/semantic-review/replay-fetch_failed.review.json) | 有 | 0/0，N/A | N/A | 全通过 | 合格 |

固定必要事实分母每模式仍为2/0/2/0。零事实不豁免处置，零引用不产生支持率成绩。

- **loopback answerable：** 两对引用分别支持14 May 1998与84 metres；正文和第二主张限定原桥，scope限定2026-01记录及与输入一致的实际取得时间。没有不同日期/长度，没有索引证据冒用。两项必要事实均覆盖。
- **replay answerable：** 日期及原桥长度各有直接引用；scope限定当前2026-01记录，其Ref与输入一致，引用保留本次回放取得时间。没有扩大到其他时期或来源。
- **loopback insufficient：** 唯一claim是运河/2005背景，原文直接支持；正文和gap另明确该份记录未提供造价，与原文最后一句一致。scope限定单份记录，时间与实际输入一致，不编金额、不推断造价普遍不存在，也不把背景年份当答案。
- **replay insufficient：** 唯一claim直接引用记录“does not state its construction cost”，正文与gap保持同一有限结论。scope的“no other ... provided or accessed”限制于当前材料，不是全球文献不存在的断言；金额与年份替代答案两项禁止项通过。
- **loopback conflicting：** 两对引用分别归属register/2001与archive/2003，也支持来源未解释开幕/通行区别。正文限定当前证据无法确认正确年份；scope/gap保留两来源和未解决冲突，没有因较新版选2003，没有虚构调和。
- **replay conflicting：** claims为空，两个年份及来源归属明确写在正式gap.detail，gap.sources同时保留真实register和archive Ref。独立逐句核对实际输入后，这两个必要事实仍覆盖；不强行将事实分母绑定到claims数组。正文、scope和gap限定当前材料无法解决，没有择新或虚构区别。原冻结方法按实际主张—引用对计数，所以本例引用支持N/A，不虚构两对引用，也不因N/A授予100%支持率。
- **replay fetch_failed：** 当前输入明确提供discovered索引与denied/fixed-replay失败。scope说明只有候选metadata可用、页面正文未取得；12吨明确限定为unconfirmed索引值，没有声称承载力已建立。`unacquired index snippet`措辞不精确，但同句“metadata ... was available”及后句“page content was not acquired”清楚限定了实际边界，故不据该短语独立判失败。正文拒绝确认承载力；gap只引用真实失败Ref，没有编造页面正文/取得时间或声称真实HTTP403。按冻结禁止项“不得将12吨断言为已建立事实”通过，不把新提示的更严格措辞偏好另作事后评分标准。

## loopback失败原输出诊断

原始ProviderContent具有空claims，scope能够区分索引与页面未取得，也将12吨标为未验证；但fetch_failed gap.sources同时包含search-candidates Ref和external-evidence-gap Ref。当前契约仅允许该gap引用失败事实角色，因而OUTPUT_INVALID且没有正式答案。没有删去错误Ref、重新展开或把草稿内容计入正式成绩。此例仍完整保留在八例总体中。

## 实际请求与边界

八例外部请求各1次，服务报告合计input10996/output2198 token；逐例output158/278/441/294/193/252/355/227，均低于512。查询依序59/60/76/53/58/58/75/62，均在128内。回放没有搜索/页面物理HTTP，但真实模型调用仍发生；不得因回放而称完全离线，失败也不退款或重试。

本报告的支持与覆盖是有限样本判断，不认证整体治理、权限、恢复或全仓测试；没有实际账单访问，不将管理预留称为账单。v2/v3/v4失败与本轮失败均须保留，后续改动和新运行按新计划锁定。
