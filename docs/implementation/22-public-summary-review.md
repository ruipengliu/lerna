# 22 票公网搜索摘要链路独立语义核验

评价日期：2026-09-13。评审者为Codex独立Spec审查子代理，参与过实现和执行器审查，已看过既有材料与答案；执行锁也明确查询及一次API摘要探测发生在登记前。本轮是已知材料的集成验证，不是外部盲审或无污染泛化评价，不是被测豆包模型自评。

**正式链路运行成功；两项必要事实覆盖2/2，引用字节精确3/3，但逐主张—引用对的语义支持仅1/3。** 全部实际Summary支持答案的主要内容，不代表模型选择的每段引用均支持其对应完整主张。本报告不将运行成功等同引用质量完全通过，也不宣称全仓验收通过。

## 固定依据与归档核对

以 [execution-lock](evidence/22-public-summary-v1-32e5f6d/execution-lock.json) 的两项expected_facts、required_scope及pass_criteria为准，执行前登记40a49ed、代码32e5f6d593ce970caeedd13b37a3da677747db21。配置SHA256实测为 `c4a2dd3f388730cf9a1044dac7b6583b8dff4b1151bb6a61049b21647ace3a1b`，与锁一致。

读取public.json、public.model.json、public.reserved.json、summary、batch、config和执行锁。预留Input与实际请求Input相同，正式Record.Input与请求Input相同，展开Content与正式Answer的JSON值相同。实际1次搜索、0页面、1次外部模型；provider用量input8175/output401 token，查询46/128、NetworkCharged=1，正式answerable发布。Core ModelRequests=2包含本地行动决策，与外部模型1次不是同一计数。耗时10.320秒；4096是本次模型输出预算，401为实际报告用量。

此次只运行本地JSON/base64、SHA256及UTF-8字节切片核对，没有读取.env、访问外部服务、修补输出或改动原始证据。新两项期望不属于旧四桥梁case ID，未伪造旧NewSemanticReview用例；下文逐项明确人工判断与理由。

## 必要事实、正文及范围

| 预登记必要事实 | 分子/分母 | 依据 |
| --- | --- | --- |
| Go是开源编程语言 | 1/1 | 正式正文明确Go programming language/open source project；Summary-0开头明确同一事实。 |
| Go是静态类型语言 | 1/1 | 正式正文明确statically typed，Summary-0同样明确该属性。 |
| 合计 | 2/2 | 未增加或删减冻结事实，不用附加语言特征抵消遗漏。 |

正文支持：通过。正文的表达性、简洁、高效、并发机制、类型系统、机器码编译、垃圾回收、反射和类似动态解释语言的体验均来自Summary-0，未假装来自独立网页读取。Summary-1主要为导航/页脚和版权文字，没有被拿来佐证语言特征，也没有发现须表达的来源冲突。

范围：通过。正文以“Based on the Doubao search summaries”开头，claims给出返回的标题和URL，scope明确两个provider summary及未取得原始网页。第二条记录被称为footer summary符合其实际内容。返回URL包含查询参数的第一条与输入SourceURL相符；scope中的简写URL不改变来源身份。

时间没有被误说成网页发表时间。引用fetched_at为 `2026-09-13T04:56:27.167195847Z`，两视图相同，表示同一搜索响应的取得时间；正文没有把它当页面发布日期或原站更新时间。这里的“fully processed”仅可理解为处理了输入中完整的provider字段，不能扩展成完整读取原网页；第一份返回Summary本身末尾截在“Whe”，报告不替它补齐。

## 每对引用的独立支持判断

分母为实际主张—引用对，同一来源支撑不同主张分别计数。Claim编号与citation编号均从0开始。

| 对 | 精确范围 | 结构精确 | 语义支持 | 理由 |
| --- | --- | --- | --- | --- |
| claim0/citation0 | Summary-0 [0,512) | 是 | 通过 | 该段包含开源、生产力、表达性/简洁/效率、并发、类型系统、快速机器码编译、垃圾回收与反射，支持claim0的语言特征。来源标题/URL来自同一视图元数据。 |
| claim0/citation1 | Summary-0 [512,1024) | 是 | **不通过** | 片段从前一句的词尾“nguage”开始，随后主要为Getting Started、安装和教程目录；不支持claim0所列开源/并发/类型系统/反射等完整主张。这是一条多余且不匹配的引用，不能因为另一段支持便计本对通过。 |
| claim1/citation0 | Summary-0 [0,512) | 是 | **不通过（部分支持）** | 片段包含fast、statically typed，但截在“compiled la”；claim1还声称“feels like a dynamically typed, interpreted language”，该后半句实际在[512,...）中，本claim没有引用它。整份Summary确实支持，不等于所选片段支持完整主张。 |

引用精确性3/3：逐项核对source、start/end、quote、Summary SHA256与fetched_at全部相符。语义支持1/3；部分支持没有按完整支持计分。两个独立视图不等于两个独立上游响应，更不意味着这三条引用来自两个网站：实际三条全部使用Summary-0。

这两处缺陷属于选择片段与主张对齐问题，不是将Go静态类型判断为错误，也不是引用字节伪造。不要补选第二段、删除多余段或改写主张后覆盖本轮结果。

## 父响应及来源边界

两视图分别是同一父Content Ref加 `#summary-0`、`#summary-1`；CandidateIndex、视图后缀及ResponseRef逐项一致。两者ResponseSHA256均为 `be9caee4fd207a7bb196adeec4a33c1e1c3091d90af7633b85e618c30a1b3925`，SearchURL为豆包接口。每个Body有不同的独立SHA256，实测重算相符；它们是返回Summary字段的文本摘要，不是父HTTP响应摘要，也不是原网站页面摘要。

引用实际定位当前`external-search-evidence`的Body，正文没有用Snippet替代。归档中两个模型块只提供Summary投影；其来源映射与受控投影实现一致。不过公开归档没有父HTTP原始响应字节，故本次只能核对所保存ResponseSHA256的一致性，不能独立重算父响应摘要或单凭这些文件再次比较原始Summary与Snippet字段。对此不作超出证据的完整原响应认证。

用户已改为使用豆包搜索摘要作答，不要求这次拉取候选网页。0页面访问符合本轮要求；不应拿旧页面取得标准判该链路不合格，也不能把旧公网页面失败改写成成功。

本次满足实际搜索摘要进入正式答案及必要事实/范围要求，但逐对引用仍有两处支持缺陷。它是一次实际成功发布伴随质量限制的结果，不是所有模型质量、治理、恢复或全仓条件已经通过的证明；管理预留也不是实际账单。

## 追加：公网总结 v2 与 v3 独立复核

追加日期：2026-09-13。沿用上述评审角色、暴露说明和逐主张—引用对标准；只读取归档，没有外部调用，也没有覆盖v1评分。各轮使用各自execution-lock，不将新增分段方案追溯应用到历史输出。

| 版本 | 正式发布 | 必要事实覆盖 | 引用字节精确 | 语义引用支持 | 本轮锁定目标 |
| --- | --- | --- | --- | --- | --- |
| [v2/2183dd5](evidence/22-public-summary-v2-2183dd5/public.json) | 是，fetch_failed | 0/2 | N/A | N/A | 未满足：未取得Summary |
| [v3/ed1acdb](evidence/22-public-summary-v3-ed1acdb/public.json) | 是，answerable | 2/2 | 4/4 | 3/4 | 引用质量未完全满足 |

### v2：实际搜索被拒，缺口正式发布

输入只有external-evidence-gap，Status=denied，没有摘要视图。正式答案如实说没有成功取得豆包摘要，不能提供Go及静态类型事实；scope限定HTTP拒绝记录，无Summary或网页正文。claims为空，gap引用实际失败Ref，没有以模型常识补答。这个失败处置本身合理，但冻结两项事实分母仍为2，覆盖0/2，不能把运行成功当作摘要问答成功，不能把无引用变成100%支持。

实际搜索请求计数1、页面0、外部模型1；服务用量input1076/output170 token，查询35/128、网络操作扣额1。这里denied发生在实际搜索路径，不能仅凭状态词认定零网络；记录按实际归档保留。没有Summary可供重算摘要或评价片段支持，也不要求模型伪造返回来源标题。

### v3：事实与范围成立，末条引用仍不完整

七字段归档核对中，实际请求Input与Record.Input相同，展开Content与正式Answer的JSON值相同。四条引用全部取自Summary-0的[0,512)，逐条quote字节、Body SHA256和FetchedAt均相符；实际时间2026-09-13T05:02:24.988100214Z在scope被称为取得时间，没有误称网页发表时间。来源标题/URL来自返回元数据，scope明确仅使用两份搜索摘要、未取原网页。

两项必要事实仍各覆盖1/1：开源编程语言、静态类型，总2/2。正文其他特性在实际Summary中均有依据。逐对判断如下，编号从0开始：

| 主张/引用 | 支持判断 | 理由 |
| --- | --- | --- |
| claim0/citation0 | 通过 | 片段开头明确Go programming language、open source及生产力目的。 |
| claim1/citation0 | 通过 | 片段包含表达性/简洁/效率、并发和多核/网络机器、灵活模块化类型系统。 |
| claim2/citation0 | 通过 | 片段完整包含快速编译机器码、垃圾回收、run-time reflection。 |
| claim3/citation0 | 不通过（部分支持） | 片段末尾仍截在“compiled la”，虽包含fast/statically typed，却没有该主张的“feels like a dynamically typed, interpreted language”；该后半句在未选的下一段。不能以整份Summary支持代替选定片段支持。 |

故结构精确4/4，语义支持3/4。未补选下一段、改写claim3或放宽完整支持判定。实际模型input8316/output375 token、一次调用；搜索1、页面0、查询47/128、网络操作扣额1。它证明公网摘要正式链路工作且两项事实已回答，仍保留引用对齐缺陷。

针对搜索摘要将片段上界改为1024并优先在句号处分段，是后续新版本的候选修复，不是本次评分依据。v4需要独立预登记及新输出核验；本报告不因计划中的修复宣告v1/v3引用问题已消失，也不把任一轮成功等同全仓通过。

## 追加：公网总结 v4 独立复核

追加日期：2026-09-13。执行前登记de73b2c，被测代码d344270cd1fbe04a3974dc7a1c65f0daeeff5a67；依据 [本轮执行锁](evidence/22-public-summary-v4-d344270/execution-lock.json) 和同样的两项事实、逐对支持及scope标准。评审角色与已知材料、非盲测说明沿用前文。

**本次通过用户新搜索摘要路径的这项有限验收：正式answerable发布、事实2/2、精确引用4/4、语义支持4/4、范围与来源时间通过，搜索1、页面0。** 这是本轮结果，不覆盖v1至v3的拒绝或引用失败，不等同22全仓验证或全部模型质量通过。

逐例归档核对：reserved.Input、model.Input与Record.Input对应一致，展开Content与正式Answer的JSON值相同。四条引用均为原Summary-0的[0,919)，本地逐条检查UTF-8切片与quote完全相同，完整Summary的SHA256和引用fetched_at相符，没有拼接、改写正文或将Snippet替代Summary。

| 检查项 | 分子/分母 | 理由 |
| --- | --- | --- |
| Go为开源编程语言 | 1/1 | 正式正文及claim0明确表述，所引片段开头直接支持。 |
| Go为静态类型语言 | 1/1 | 正式正文及claim3明确statically typed，所引片段包含完整相关句。 |
| claim0/citation0支持 | 1/1 | 开源项目及提升程序员生产力均在片段中。 |
| claim1/citation0支持 | 1/1 | 表达性、简洁、效率、并发与多核/网络机器、灵活模块化类型系统均受同段支持。 |
| claim2/citation0支持 | 1/1 | 快速机器码编译、垃圾回收、反射三项完整出现。 |
| claim3/citation0支持 | 1/1 | 快、静态类型、编译语言以及类似动态类型解释语言的体验，整句都包含在[0,919)内，旧512截断缺口不再存在。 |

两项事实合计2/2；实际主张—引用对4/4，四次引用同一个视图仍分别计数。片段也含额外教程文字，但每对主张的全部内容都获其支持，故不因附加上下文判失败。没有将第二个视图当作不存在的额外支持来源。

正文支持与scope均通过：明确基于返回的豆包搜索摘要，列出第一来源的返回标题及URL，并说明第二条为go.dev品牌相关摘要，明确没有取得原网页。附加语言特性也在Summary中，没有用模型常识扩充无据结论。

四条引用取得时间均为2026-09-13T05:05:11.548427248Z，等于当前搜索响应投影时间，没有被称为网页发表/更新时间。两个视图同属父Ref `content:local:75fc38ac50eb64a3beddde31157c4e206bb5f94bb187b5813df390b936b41a57:1`，共同ResponseSHA256为 `dffb1c45e04d069eb3439cef7218e30ed2a6c77f1de78b67c440dbb5b3a124b5`；候选序号、view后缀、SourceURL与来源标题一致。公开归档仍不含父HTTP原始字节，因此这里只核对父响应绑定字段一致性，没有虚称独立重算父响应摘要。

实际模型一次、input6891/output482 token；查询49/128，网络操作扣额1，搜索1、页面0，Core模型请求2含本地规划，预留请求/token为0。没有追加调用或修补原始答案。本次成功只覆盖已锁定摘要任务，不认证全仓、完整治理矩阵、所有查询或未见材料上的泛化质量；历史各轮失败继续保留。
