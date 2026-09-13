# 22 票真实答案模型八例独立评价

评价时间：2026-09-13T03:48:48Z。评审角色：Codex 独立 Spec 审查子代理，参与过本实现和执行器的 Spec 审查，已看过公开冻结材料及既有协议答案；不是外部盲审，也不是被测豆包模型自评。本次只读取归档并进行本地 JSON/base64/字节范围核对，没有调用公网或模型，没有读取凭证，没有改动原始证据。

本轮结论：**正式答案合格 0/8，失败 8/8；八例全部保留。** 这是已知虚构材料的小型回归结果，不能推断完整 V1 的质量门槛，也不构成公网搜索验收或整票通过。

## 依据与归档一致性

按 [执行锁](evidence/22-evaluation-v2-8f221c9/execution-lock.json)、[预先登记的方法](22-prospective-evaluation.md)及 [冻结期望](../../profiles/searchcheck/testdata/expectations-v1.json)评价。执行器提交为 `8f221c9cc6c2b87dfcb24499df8864a032429607`；锁中的计划、材料、期望、两个评分实现、执行器、go.mod/go.sum 共八项 SHA256 均与当前对应文件相符。计划正文仍保留锁定前状态；本轮执行与后来获得的累计模型费用小于500元授权，以 execution-lock 为准，不追认历史实现前预注册。

读取了全部八份 case JSON、八份 `.reserved.json` 和八份 `.model.json`，并核对 [summary.json](evidence/22-evaluation-v2-8f221c9/summary.json)。每例仅一个归档模型尝试，预留中的完整 Input 与模型归档 Input 相等，MaxInput=229376、MaxOutput=512。case 的 Started 时间符合先 loopback 四例、再 replay 四例，各组顺序 answerable、insufficient、conflicting、fetch_failed；summary 的字母排序不代表实际执行顺序。

每例均返回失败，Record.Answer 是空值；case 的 Record.Input/ModelOutput 也为空，实际输入输出保存在对应 model/reserved 文件中，不能把这些空字段解释为模型没有收到证据。归档没有正式发布答案，故不存在可提交给 NewSemanticReview 的正式答案字节；本次不伪造 SemanticReview/SummarizeSemanticReview 成功结果。下面采用预登记的失败保留规则，原输出诊断与正式质量评分分开。

## 八例正式评价

“覆盖”衡量正式交付答案覆盖的固定必要事实；没有正式答案时分子为0，分母不删除。引用支持为 N/A（无正式答案可评），不能写作100%。正文、范围、处置和禁止项均未获得正式答案层面的通过认证；即使草稿包含合理文字，整例仍失败。

| 顺序 | 模式/用例（原始 case） | 运行错误 | 正式答案 | 必要事实覆盖 | 正式引用支持 | 结论 |
| --- | --- | --- | --- | --- | --- | --- |
| 1 | [loopback/answerable](evidence/22-evaluation-v2-8f221c9/loopback-answerable.json) | OUTPUT_INVALID | 缺失 | 0/2 | N/A | 失败 |
| 2 | [loopback/insufficient](evidence/22-evaluation-v2-8f221c9/loopback-insufficient.json) | OUTPUT_INVALID | 缺失 | 0/0，N/A | N/A | 失败 |
| 3 | [loopback/conflicting](evidence/22-evaluation-v2-8f221c9/loopback-conflicting.json) | OUTPUT_TRUNCATED | 缺失 | 0/2 | N/A | 失败 |
| 4 | [loopback/fetch_failed](evidence/22-evaluation-v2-8f221c9/loopback-fetch_failed.json) | OUTPUT_INVALID | 缺失 | 0/0，N/A | N/A | 失败 |
| 5 | [replay/answerable](evidence/22-evaluation-v2-8f221c9/replay-answerable.json) | OUTPUT_INVALID | 缺失 | 0/2 | N/A | 失败 |
| 6 | [replay/insufficient](evidence/22-evaluation-v2-8f221c9/replay-insufficient.json) | OUTPUT_INVALID | 缺失 | 0/0，N/A | N/A | 失败 |
| 7 | [replay/conflicting](evidence/22-evaluation-v2-8f221c9/replay-conflicting.json) | OUTPUT_TRUNCATED | 缺失 | 0/2 | N/A | 失败 |
| 8 | [replay/fetch_failed](evidence/22-evaluation-v2-8f221c9/replay-fetch_failed.json) | OUTPUT_INVALID | 缺失 | 0/0，N/A | N/A | 失败 |

answerable 两项固定事实是 14 May 1998 和 84 metres，限定于2026-01记录的原桥；conflicting 两项是 register 的2001与 archive 的2003，并须保留未解决冲突。insufficient 须说明已取得记录不建立造价，不能编造金额或推断造价不存在；fetch_failed 须说明页面未取得、承载力未核实，不得将索引中的12 tonnes冒充页面证据。八例的这些正式交付要求都未满足，不能因零事实分母而豁免处置要求。

## 原始输出诊断（不计正式答案成绩）

以下逐例依据同名 `.model.json` 中 base64 解码后的 Result.Content，并与该文件 Input 中的实际证据比较。它们说明可见缺陷，不声称完整定位运行错误的唯一根因。

| 用例 | 正文、范围、处置与禁止项诊断 |
| --- | --- |
| loopback/answerable | 草稿提到正确日期、长度和原桥/2026-01范围，未见把索引当正文。但两条引用范围43:70、71:122均不等于所附 quote；例如43:70实际为 `e Northbank footbridge open`。来源句子语义相关不能代替精确引用通过。 |
| loopback/insufficient | answer为空；gap将缺口限制于已咨询记录，没有编造金额，也没有断言世上不存在造价。另列2005年为背景claim，不是正式造价答案；其51:87范围与quote不符。合理的gap不能弥补空正文及错误引用。 |
| loopback/conflicting | Finish=length，输出512 token，JSON在gap中截断。可见部分保留2001、2003及未解决冲突，未见选择较新记录或编造开幕/通行调和；因输出不完整，不能判定完整处置及全部禁止项通过。 |
| loopback/fetch_failed | answer为空；gap描述取得被拒、没有取得承载力数据，没有将12 tonnes当事实，也没有编造页面正文或取得时间。实际输入仅有Mode=http、Status=denied、Requests=1，不应从这份模型输入推断具体HTTP状态码。范围只说明提问对象，未形成正式的“页面未取得”回答。 |
| replay/answerable | 草稿包含正确日期、长度及原桥/2026-01范围，未见索引证据冒用。唯一引用39:94不等于quote（实际片段从句点开始并在 `Its l` 处结束），故精确可追溯条件不成立。 |
| replay/insufficient | answer为空；草稿及gap明确咨询记录未提供造价，未编金额或推断普遍不存在，范围限定2026-01记录。引用0:124截断在 `does not `，与所引完整正文不符；不能计作正式合格的不足处置。 |
| replay/conflicting | Finish=length，输出512 token，JSON在第二条引用的fetched_at字符串中截断。可见两个年份及对应来源，没有可见的择新或虚构调和，但没有完整gap/最终处置可核验，不能据局部合理内容通过。 |
| replay/fetch_failed | answer为空；gap保留denied及没有取得可用数据，没有断言12 tonnes或编造页面正文/时间，也未声称实际HTTP403。scope主要罗列任务预算/期限，未明确限定“只观察索引、未取得页面”的证据范围；实际输入是fixed-replay、Requests=0，不能表述为真实页面联网失败。 |

空answer违反当前 brain/evidence.go 的非空正文要求；已解析的answerable和insufficient引用范围均通过本地UTF-8字节切片核对发现不匹配。两例冲突输出未尝试修补JSON、补齐引用或重新生成。未将这些诊断转换成被拒草稿的正式支持率。

## 实际请求、用量与耗时

以下为归档快照与服务返回的已知token用量；每例外部模型请求均为1，Core模型请求均为3，查询上限均为128，预留请求/token均已为0。物理HTTP列仅为搜索/页面，不含模型API。

| 模式/用例 | 外部模型 input/output token | 查询 | 网络操作扣额 | 物理搜索/页面HTTP | 耗时秒 |
| --- | --- | --- | --- | --- | --- |
| loopback/answerable | 1063/508 | 51 | 2 | 1/1 | 12.730 |
| loopback/insufficient | 1058/369 | 51 | 2 | 1/1 | 10.650 |
| loopback/conflicting | 1366/512 | 65 | 3 | 1/2 | 13.744 |
| loopback/fetch_failed | 843/137 | 48 | 2 | 1/1 | 5.455 |
| replay/answerable | 1068/325 | 50 | 2 | 0/0 | 9.120 |
| replay/insufficient | 1059/426 | 50 | 2 | 0/0 | 9.836 |
| replay/conflicting | 1368/512 | 64 | 3 | 0/0 | 12.904 |
| replay/fetch_failed | 845/168 | 47 | 2 | 0/0 | 6.638 |

合计8次外部请求，模型服务报告input8670、output2957 token，与summary一致。全部失败仍计入请求数；没有从八例总体中删除失败、归零计量或重试。8元是管理预留，不是实际账单；本评价没有账单访问，不能认证累计真实费用。回放没有搜索/页面HTTP，但确实调用外部模型且保留网络操作预算计费。HTTP与回放的输入Mode、URL及取得时间分别记录，没有把A时间复制成B时间。

归档只足以支持这批输出的失败与上述可见诊断，不能据此认证所有撤权、隐私、恢复或全仓条件已通过。不得以调整512 token上界、修补模型输出或重跑个例的方式改写本轮结论；后续语义变更或再次评价应另立计划和执行锁，保留这八例原始失败。
