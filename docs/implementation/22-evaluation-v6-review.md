# 22 票真实答案模型 v6 独立评价

评价日期：2026-09-13。评审者为Codex独立Spec审查子代理，参与过实现和执行器审查，已看过公开虚构材料及既有协议答案、v2至v5输出。不是外部盲审、隐藏测试或被测豆包模型自评。本次未调用公网/模型、未读取凭证，未修改原始执行结果或提交文件。

**运行成功7/8，按冻结标准语义合格7/8，失败1/8。真实模型仍会产生不合法输出，质量问题没有全部解决。** replay/fetch_failed因畸形JSON没有正式答案，保留失败，不补考、不修复原文、不从分母删除。小型独立评价报告已经有可复核的实际成功与失败；不能为凑8/8反复刷新同一材料，也不能据此声称完整V1质量门槛、全仓或公网验收通过。

## 依据及方法

依据 [本轮执行锁](evidence/22-evaluation-v6-37c22f7/execution-lock.json)、[原冻结方法](22-prospective-evaluation.md)及 [必要事实和禁止项](../../profiles/searchcheck/testdata/expectations-v1.json)，被测代码37c22f7。v6是看过历史失败后改进的已知材料回归，历史各轮结果保持独立。

八例reserved.Input均与实际model.Input一致；七例成功的Record.Input与实际请求Input相同，展开Content的JSON值等于正式Record.Answer。通过本地临时Go工具，使用case文件Record.Answer与Record.Input字段的原始JSON字节调用NewSemanticReview，先检查结构，再填写逐项独立语义理由，调用SummarizeSemanticReview验证绑定与完整性。七例ReviewComplete=true，无未评项；此标志不替代语义判断。

[semantic-review/summary.json](evidence/22-evaluation-v6-37c22f7/semantic-review/summary.json)及逐例review/summary文件保存理由与绑定摘要。无正式答案的例外使用failure-review记录，绑定原case/model文件SHA256，未伪造SemanticReview成功。

## 八例结果

| 顺序/用例 | 正式答案 | 固定必要事实覆盖 | 主张—引用支持 | 正文/范围/处置/禁止项 | 结论 |
| --- | --- | --- | --- | --- | --- |
| [1 loopback/answerable](evidence/22-evaluation-v6-37c22f7/semantic-review/loopback-answerable.review.json) | 有 | 2/2 | 2/2 | 全通过 | 合格 |
| [2 loopback/insufficient](evidence/22-evaluation-v6-37c22f7/semantic-review/loopback-insufficient.review.json) | 有 | 0/0，N/A | 1/1 | 全通过 | 合格 |
| [3 loopback/conflicting](evidence/22-evaluation-v6-37c22f7/semantic-review/loopback-conflicting.review.json) | 有 | 2/2 | 2/2 | 全通过 | 合格 |
| [4 loopback/fetch_failed](evidence/22-evaluation-v6-37c22f7/semantic-review/loopback-fetch_failed.review.json) | 有 | 0/0，N/A | N/A | 全通过 | 合格 |
| [5 replay/answerable](evidence/22-evaluation-v6-37c22f7/semantic-review/replay-answerable.review.json) | 有 | 2/2 | 2/2 | 全通过 | 合格 |
| [6 replay/insufficient](evidence/22-evaluation-v6-37c22f7/semantic-review/replay-insufficient.review.json) | 有 | 0/0，N/A | N/A | 全通过 | 合格 |
| [7 replay/conflicting](evidence/22-evaluation-v6-37c22f7/semantic-review/replay-conflicting.review.json) | 有 | 2/2 | N/A | 全通过 | 合格 |
| [8 replay/fetch_failed](evidence/22-evaluation-v6-37c22f7/semantic-review/replay-fetch_failed.review.json) | 无，OUTPUT_INVALID | 0/0，N/A | N/A | 无正式答案，不可评价 | 失败 |

每模式必要事实分母仍为2/0/2/0。实际七对主张—引用全部支持；没有claims的例子不凭gap.sources虚增引用分母。零分母为N/A，绝不是100%。适用必要事实共8/8，不抵消失败。

- **loopback answerable：** 日期14 May 1998及长度84 metres各由原文直接支持，正文与第二主张明确原桥，scope限定2026-01记录及当前Ref，fetched_at来自本次输入。没有错误数值或把索引作为正文。
- **replay answerable：** 同样两项事实各一对引用；正文限定原桥，scope限定唯一取得记录，引用保留本次回放取得时间，未借用HTTP时间或扩大到其他时期。
- **loopback insufficient：** 唯一claim为运河/2005背景，受原文直接支持；正文和gap说明单份记录未记载造价，与原文最后一句一致。scope中的缩写记录标识对应当前Ref；没有编金额、推断造价普遍不存在或拿背景年份作造价答案。
- **replay insufficient：** claims为空；正文与gap仍被实际记录“does not state its construction cost”支持，scope限定2026-01单份记录及与输入相符的取得时间。没有断言金额或全球文献缺失，处置通过，引用支持N/A。
- **loopback conflicting：** register/2001和archive/2003各一对支持，来源未解释开幕/公众通行区别也来自原文。正文、scope、gap明确当前材料冲突未解决；没有择新或虚构调和。
- **replay conflicting：** claims为空，但正式gap明确两个年份、各自来源及未解决差异，两个实际Ref保留，因此固定事实覆盖2/2。正文、scope及gap没有选定某年或虚构区别；引用支持仍N/A，不以覆盖替代引用分母。
- **loopback fetch_failed：** 当前输入含真实搜索候选metadata及denied失败。正文拒绝确认承载力，scope明确只观察索引snippet、页面未取得、请求被拒；候选URL与输入一致。gap仅引用失败Ref，不把索引升级为页面证据；没有12吨已建立的断言，没有编造页面正文或取得时间。范围及处置均通过。

## 保留的真实失败

replay/fetch_failed原始ProviderContent在gaps数组之后提前关闭顶层对象，随后又出现逗号和status字段，即 `...]},"status":"fetch_failed"}`。本地JSON解析报告Extra data（char765），与运行OUTPUT_INVALID一致。模型确实返回文本，并非没有外部请求；原文、用量及失败case均保留。

局部草稿可见索引与页面未取得的区分，但它不是正式答案，不能给正文/范围/处置/禁止项评分通过；未删右大括号、未重新展开、未补考。该失败说明即使外部接口要求结构化输出，真实模型仍可能违约；运行时正确拒绝并不等于生成质量已解决。

## 实际用量与适用边界

原summary记录8次外部请求、7次运行成功，服务已知input11337/output2039 token。失败请求同样计入，不退款或另起任务替代。固定回放仍调用真实模型，不能称完全离线；本轮为loopback材料与回放对照，不是公网搜索质量试验。

管理预留不是实际账单，本评价没有账单访问。此有限样本不认证完整治理、撤权、恢复或全仓状态；不将同材料多轮观察推断为无污染泛化正确率。保留全部历史失败与本轮失败，以真实结果完成独立报告，不继续为凑齐8/8刷题。
