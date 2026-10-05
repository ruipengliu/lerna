# 04票03：fresh Input实测后的唯一下一成本决定

**推荐下一项仅为ContextDispatcher.input的单条、byte-exact、成功语法解码复用。** 每次先执行原完整行SELECT/FOR UPDATE，再以实际所得全部body字节匹配私有成功项；命中仅返回独立深拷贝。冷路径保持原closedJSON及错误。不要同时优化序列化、digest、比较或SQL。本报告仅STATIC建议；不自动授权源码或native，不宣称原大图已通过。

## 1. 本次实际证据及未证明之处

完整阅读320行/60072B `/workspace/lerna-content-03-137311247276/context-fresh-input-representation-original4b-race.log`，SHA256 `b7c026475cf661881fa5e420560a482b2ca1fde924a723040f63338bb32d9a15`。检查one-race-measurement-actual-outcome、成本归属计划、全部measurement差异及format差异、commands/map/形成后pins。独立逐文件核验当前331源码、formed8全部长度/SHA匹配；outcome的before/post331、formed8及binary绑定相等。实际PID/PGID3948276/start16546665，exit1、Wait/currentgroup[]、STOP_RELEASE；不是本代理执行。

- closure64实际FAIL27.70s；Step5.058721s，47次Material中末次失败，尚无Prepared/Publisher。首ReadForProcessing在27.635164s超时，caller仍2.364800s；最终Claim晚11.192ms。这次是原Claim窗口耗尽，不倒写成前一次caller30耗尽。
- static62实际FAIL22.82s；全部62 Material、2 Plan成功，Prepared真实COMMIT22.234685s；首Content.Put在22.735488s返回未细分的errorString，176.164ms只是该调用区间，不等于根因；最终Claim晚63.567ms、caller仍7.228623s。没有成功publication/readback/Completed。旧/新first-Put原因及possibleeffect UNKNOWN各自保留，不能从后来Claim错误反推Put没有效果。
- 65、单体200000/262144、duplicate-selector按原oracle通过；不能替代两个大图正向尾。

本次阶段分离的关键wall值（同角色/阶段区间，不与父Current/Compile/Step相加）：

| 路径 | closure64 | static62 |
|---|---:|---:|
| compile ReserveRead input_closed_json，65次 | 571.236ms | 597.168ms |
| compile ConfirmRead input_closed_json，65次 | 584.724ms | 580.273ms |
| worker Current input_closed_json | 48次584.146ms | 67次596.297ms |
| worker Current原Scan wall | 43.038ms | 53.862ms |
| worker Current fullInput compare | 15.555ms | 22.859ms |

所有这些已完成计数均error0，观测drop/ambiguity/open-phase为0。解码同一fresh字节的重复区间是明确实际成本；**它不是纯CPU值或保证可省时长**，observer总开销UNKNOWN，clone/byte比较仍有成本。编译节约不等于延长Claim；worker收益也未证明足以使全部材料、Prepared、两次发布/readback进入5秒。旧CPU/旧10SQL不能作当前净收益证明，不再添加一个测量矩阵。

## 2. 精确源码与单改动

当前基线 `b28b2e3182d97156c5fe203baf5cf2183631b7f2` 加已明确clock/BodySeal WIP，实际hash：

- `conformance/internal/decisionfixture/context_dispatch.go`：`5c2b8d472fdbe458b9affb9a9543db3359c85c0dfa90d5357bea843995e624fb`。`:161–169` 每次 `SELECT body ... input_id=$1 FOR UPDATE` 后closedJSON；`:261` binding每次fresh Input、DeepEqual、running和trusted检查；Current随后仍比较完整Permission。
- `context_budget.go`：`b3ad33bb1ed9c89dc079afedb81dc9e43420a0609cb2b87422d1bb009ec9078d`，`:205` currentAttempt原完整tuple/revision/digest/control及持久attempt核验。
- `context_binding_decode.go`：`457b065fb760a264f0c687f82275c1dd73af3414191cccee1bba80b527f04779`，已有实际Binding成功语法memo与完整Input深拷贝，不是授权缓存。
- `source.go`：`2698e49c206097583867ababbeba87a8c20942ed58b4606b6a8fc98fefbc7684`，`:60` jsonBytes保有限上界；`:70` closedJSON先严格ParseJSON，再typed unknown-field拒绝与EOF检查。

最小实现位置仍为fixture实际消费方私有代码，不新增domain/public port。ContextDispatcher新增一个private inputDecodeMemo（独立于Binding项，避免两种表示互相顶掉）；作用域键包含原Store实例、owner、schema、准确inputID，值为**拥有的原body全字节与成功typed Input**。最多一项、body不超现有v.MaxBodyBytes；无跨Dispatcher/global/持久cache/TTL。用不可变项加现成原子指针模式安全替换，失败或超界不加入项，不以hash/revision/subjectID或SnapshotID相等代替全字节比较。不得由InstallInput预填来跳过第一次真实row/严格解析。

保持每次同一原Tx/原SQL/完整Scan；SQL缺失、取消、传输或行锁错误先原样返回。字节变化必走原closedJSON：重复键、unknown字段、trailing、类型错误仍失败；即便合法表示仅空白变化也先重解码。只在原解码成功后存一份私有copy，所有返回（冷/热）不共享cache可变内容。允许后来业务门禁/COMMIT失败后保留该语法项，因为它没有任何资格含义，下次仍完整读取并重新裁决。

深拷贝覆盖当前Input全部slice/pointer（conditions内gaps、constraints/control/budget/unresolved/progress/materials/omitted/gaps、Request.UseRefs及TraceContext），保持nil与空不同。优先从当前cloneBinding提取准确typed cloneInput供两个真实表示消费者使用；不建反射拷贝框架、不借序列化再复制。新类型若增可变字段必须同步copy资格。

## 3. 必须逐字义保留的检查

- 当前Input全量比较、Control.State、Permission/fullSubject、真实trusted clock、各自原错误顺序都不移位。复用的是相同字节的确定语法值，绝不复用“当前合法”结果。
- **currentAttempt的jsonBytes(in)、完整CompileBudgetTuple比较、InputRevision、digest(body)==InputDigest、control、saved attempt、budgetTime与锁顺序全部原样，每次重算/核验。** 不因为测得序列化约0.09–0.11s而顺便缓存canonical bytes/digest；它们与当前完整Input绑定仍实际执行。
- 每次Content当前target+全部ancestor/fullRef/主体用途/action/BodySeal/Gone/Clock，前后两Tx、物理读取/hash及所有出版门禁不动。编译次数/真实字节预留/unknown责任不退，worker计费/RuleStarts1/frozen663d不动。
- 原setup仍在caller30内，Claim5/Go与outer120/count1不变；不预热、heartbeat、retry、费用重置、pool扩容、SQL融合/批量换锁或跨Tx缓存政策/闭包。

## 4. 最小资格次序（未来单独批准执行）

先在现有私有语法边界做真实cold/hot一致性控制：合法冷热结果相同；返回值与原body被调用方改动不能污染下一次；nil/空和所有nested mutable字段；不同store/schema/inputID不得命中；成功后换成malformed/unknown/重复键/trailing仍按原失败，不产生权限。这里仅可把parse次数作为这条机械语法复用的实现资格，不能当业务或性能验收。

再用真实PG公开路径证明“warm后仍fresh”：合法完整Input正常回路；经现有合法输入变更/控制或受信期限变化后旧Binding/attempt拒绝；并发行锁等待后 current clock到期仍拒绝；重开不重置原编译次数/累计读字节。原预算/权限/BodySeal与传输故障控制保持，已有Now transport覆盖缺口不被这条memo声称解决。不得写私表业务前态来制造green。

源码固定、独立格式/构建与受影响正常/race语义控制通过后，按原四B完整输入与完整公开尾分别ONE normal、ONE race，前一失败即停止；不使用会覆盖新memo的旧overlay。实际大图失败已经是本优化的原业务RED，不伪造新的语义失败。若新原图仍失败，保留该精确phase/Prepared/possibleeffect后再决定；本次不预授权第二优化。

## 5. Provenance与范围

outcome SHA `df10393ea7def7ca4ebef6e1ab9895ae2787d30e71a5ae9f7d00aad09afa65f3`；static manifest `b000eb4172caac97b8753228bdd7ba9c770c2e9086981c8023876c88747137d2`；formed manifest `2d2e033fdfa98744dd44de72344dc1f224f8e7188ae1849f6f93292aadf35e28`；map `d1426bbb91538247c7fef84b90234bba86eaaddcdd33ec7e03c6be7c54a14734`；commands `7b41430974feb84f12a14d0535476b589df6d20a700f5c035a555c3a8e76bb12`；本次binary `3c5f3ac04c5cadb80ceeb45458c95553d221dfe6791ee55b77adff0b5aaabb6e`/32211753B，由实际before/post记录绑定，本代理未再打开binary FD。

全部旧失败与原CPU/Go输出FD Close UNKNOWN不变，新组absence不倒补per-resource Close。该建议优化真实fixture owner消费方，不创造Task/供应商能力，不接受票03或whole04；本轮只读取与写本文，零native/源码修改。
