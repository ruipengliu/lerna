# 22 票真实答案模型 v4 独立评价

评价日期：2026-09-13。评审者为Codex独立Spec审查子代理，参与过实现及执行器审查，已看过公开虚构材料、协议答案及v2/v3输出；不是外部盲审或被测豆包模型自评。此次只读取归档、运行本地评分接口，未调用公网/模型、未读取凭证，未修改原始执行结果或提交文件。

**运行成功6/8，按冻结标准语义合格6/8，失败2/8。** 两例fetch_failed均因OUTPUT_INVALID无正式答案，保留在八例分母中。其余六例的12对引用全部支持相应主张，适用必要事实共8/8；这不能抵消两个失败，也不能证明完整V1门槛、全仓验收或公网搜索通过。

## 依据、绑定和方法

以 [v4计划](22-prospective-evaluation-v4.md)、[原评分方法](22-prospective-evaluation.md)、[冻结期望](../../profiles/searchcheck/testdata/expectations-v1.json)及 [执行锁](evidence/22-evaluation-v4-d703cc0/execution-lock.json)为准；代码d703cc014c291667d2bfc1f391b8e1e76079f587，预登记提交cbe38b4。锁中十项文件摘要均核对相符。v4是看过先前失败后改进的已知材料回归，不替换v2/v3任何结果。

八例reserved.Input与model.Input相同。六份成功记录的Record.Input与实际模型输入相同，展开后的Content与正式Record.Answer的JSON值相同。两例失败输入和ProviderContent仍在model/reserved文件中，case的空Answer不代表模型没有返回文字。

六例通过本地临时Go工具调用NewSemanticReview，以归档case中Record.Answer及Record.Input字段的原始JSON字节绑定答案和输入、先检查结构，再填写独立判断并调用SummarizeSemanticReview。结果和逐项理由在 [semantic-review/summary.json](evidence/22-evaluation-v4-d703cc0/semantic-review/summary.json)及同目录六组review/summary文件，全部ReviewComplete=true、无未评项。该标志只表示填写完整。

两例无正式答案，未伪造SemanticReview接口成功；各写独立failure-review记录，绑定原case和model文件摘要。正式正文/范围/处置/禁止项均不可评价，整例失败；固定必要事实0/0记N/A，正式引用支持N/A，绝不换算100%。

## 逐例判定

| 顺序/用例 | 正式答案 | 必要事实覆盖 | 引用支持 | 正文/范围/处置/禁止项 | 结论 |
| --- | --- | --- | --- | --- | --- |
| [1 loopback/answerable](evidence/22-evaluation-v4-d703cc0/semantic-review/loopback-answerable.review.json) | 有 | 2/2 | 2/2 | 全通过 | 合格 |
| [2 loopback/insufficient](evidence/22-evaluation-v4-d703cc0/semantic-review/loopback-insufficient.review.json) | 有 | 0/0，N/A | 2/2 | 全通过 | 合格 |
| [3 loopback/conflicting](evidence/22-evaluation-v4-d703cc0/semantic-review/loopback-conflicting.review.json) | 有 | 2/2 | 2/2 | 全通过 | 合格 |
| [4 loopback/fetch_failed](evidence/22-evaluation-v4-d703cc0/semantic-review/loopback-fetch_failed.review.json) | 无，OUTPUT_INVALID | 0/0，N/A | N/A | 无正式答案，不可评价 | 失败 |
| [5 replay/answerable](evidence/22-evaluation-v4-d703cc0/semantic-review/replay-answerable.review.json) | 有 | 2/2 | 2/2 | 全通过 | 合格 |
| [6 replay/insufficient](evidence/22-evaluation-v4-d703cc0/semantic-review/replay-insufficient.review.json) | 有 | 0/0，N/A | 2/2 | 全通过 | 合格 |
| [7 replay/conflicting](evidence/22-evaluation-v4-d703cc0/semantic-review/replay-conflicting.review.json) | 有 | 2/2 | 2/2 | 全通过 | 合格 |
| [8 replay/fetch_failed](evidence/22-evaluation-v4-d703cc0/semantic-review/replay-fetch_failed.review.json) | 无，OUTPUT_INVALID | 0/0，N/A | N/A | 无正式答案，不可评价 | 失败 |

两模式的分母始终各为2/0/2/0，未从期望中删除失败、遗漏或零事实用例。

- **loopback answerable：** 两对引用分别直接支持14 May 1998与84 metres；正文和第二主张明确original，scope限定2026-01记录，取得时间真实可追溯。“没有其他记录”按当前可用来源范围理解，不作全球资料不存在的结论。日期、长度无误，没有以索引摘要作页面证据。
- **replay answerable：** 同样两项事实与两对支持成立，正文/主张限定原桥，scope限定唯一提供的2026-01记录。引用保留本次fixed-replay取得时间，没有借用HTTP时间；没有虚构改建后事实。
- **loopback insufficient：** 一对引用支持跨运河/2005的背景，另一对支持记录未说明造价。正文、scope及gap明确只凭提供的单份记录无法确定造价；没有编金额，也没有将背景年份当作造价回答。没有推断造价普遍不存在。
- **replay insufficient：** 两对引用同样受原文支持；正文说现有材料未记载造价，scope限定已取得记录。“provided or accessible”在本句的当前来源集合语境下理解，不作全球不存在任何相关文献的断言。gap引用实际记录，不把证据不足当作金额事实。
- **loopback conflicting：** 两对引用明确归属register/2001与archive/2003，并支持未解释开幕/公众通行区别。正文的“unresolvable”受available sources和scope限定为当前材料不能解决，不是永远无法查明；gap保留两值及来源，没有择新或虚构调和。
- **replay conflicting：** 两值及对应来源各一对引用，正文承认现有记录未给出权威解决，scope限定两份取得记录；没有选2003或用开幕/通行猜想解决冲突。提及该区别仅说明原文没有提供解释。

## 两份被拒原输出诊断（不计正式成绩）

两例真实输入均有search-candidates（实际discovered索引，含“12 tonnes不是页面证据”的摘要）及external-evidence-gap（denied）。两份ProviderContent均生成一个citations=[]的claim，描述索引提到未证实的12吨；原Brain契约要求claim至少一条页面证据引用，因此OUTPUT_INVALID，无正式发布。没有将这些原始claim补上引用、删除后重新评分或视作成功。

loopback草稿的scope正确区分搜索候选元数据和未取得页面，正文也明确12吨未验证；但又把未取得页面描述成“containing the actual/verified ... load limit”，这暗示已知缺失页面包含什么，当前输入不能支持。该无据暗示只记录为诊断，不能因其他段落改善而通过失败例。

replay草稿明确索引未验证、页面未取得，没有声称真实HTTP403或编造页面取得时间；仍因无引用claim不符合契约而失败。两份草稿都不能用来给正式正文、范围、禁止项授予通过。

## 请求、快照及限制

核对Started顺序为先loopback四例再replay四例，每例仅1次外部模型尝试。服务已知token总计input10578/output2262，等于原summary；各例output为178/328/448/265/130/232/399/282，均小于512。未重试、退款或替换失败用例。

查询依序59/60/76/53/58/58/75/52，均在128内；网络操作扣额每例2，冲突例3。loopback物理搜索各1、页面1/1/2/1；replay搜索和页面物理HTTP均0，外部模型仍联网。耗时依序7.485/10.398/12.731/8.148/6.293/8.722/12.906/9.329秒。

**两例失败的Core用量尚有预留：** ModelRequests=2、ReservedModelRequests=1、ReservedModelTokens=229888；六例成功ModelRequests=3、预留为0。provider归档虽有已知token，不能把它偷换成Core已经结清，也不能把预留当零或退款。这里报告的是归档时快照，未操作恢复或结算。

管理预留不是实际账单，本评价无账单访问；也没有额外验证治理、撤权、恢复或全仓测试。后续模型契约改进及公网验证属于另行版本/计划，必须保留本轮两个失败。
