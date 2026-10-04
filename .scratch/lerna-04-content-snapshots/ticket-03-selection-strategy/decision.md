# 04票03：选择来源B必须实际决定材料选择

**采用实施者的两token确定性选择规则，并把准确选择版本固定进输入。** 不以“读取/hash/登记过B”代替B实际影响选择；不改旧 `fixture-rule/2`、其candidate_result、Prepared、计费或1.1合同。

只读范围：integration `d225b79f881a2a6ec034918117db5017306318c7`；03部分源码 `db4920892d4f70a858768497396e9e37b87e25ba`，WT `/tmp/lerna-worktrees/content-snapshots-03`。读取票03/spec/decisions、已采用handoff §3/4/5、实际compiler/types/canonical、ContextWorld与assembly。本文没有native、DB或执行证据，不是整体架构审查。

## 1. 当前准确缺口

`domain/task/context/compiler.go`顺序ReadForProcessing全部Materials，然后仅根据可信输入的 `Material.Selected`写Derivation.Output；没有解释任何selection正文。`types.go`的 `Policy.Version=rule-fixture-utf8-bytes-v1`目前固定容量字节策略；`canonical.go`只核derivation与输入标志投影。`ContextWorld`真实B为 `include-primary\n`、role=selection、Selected=false，但改变B正文目前不能决定A是否送入worker。

`adapters/content/decision/assembly.go:Plan`实际按Selected构造MaterialRefs，第一项始终M，M的sources为全部processed。这个接法保留；在Plan之前新增真实确定性选择与一致性裁决，即可关闭该缺口，不增加模型/另一个决策引擎。

## 2. 最小准确版本与语法

推荐在本票未发布的内部 `Input.Policy`加入明确 `SelectionVersion`（JSON `selection_version`），本场景固定值 **`fixture-primary-selection/1`**。容量 `Policy.Version`仍为既定ByteStrategy，两种版本分别保存并进入原输入CAS、M及原映射，不偷偷把旧字节计量名重新解释成已存在的选择策略。

无需registry或策略插件。若保留其他不含selection角色的原静态材料场景，缺SelectionVersion只表示原“声明Selected”的模式，不宣称正文决定选择；带selection角色却缺准确版本、未知非空版本一律拒绝。新选择mode及旧静态mode显式区分，不能对已经持久化的旧输入/绑定默认填新版本。准确M格式/独立黄金随本票内部新字段更新；不改任何已冻结合同schema/golden或旧规则manifest。

选择mode的闭合规则：

- 恰有一个 `role=primary` 的A、一个不同准确ref的 `role=selection` 的B；B必须Selected=false。重复角色、缺A/B、同身份异ref均拒绝。其余实际材料仍允许既有有界声明，不把两源场景强加为整个容量测试只能两项；该规则只决定唯一primary，不能宣称额外材料也由B选出。
- B通过真实Content当前read+process、完整hash/length读取后，正文必须逐字节等于UTF-8 **`include-primary\n`（16 bytes）**或 **`omit-primary\n`（13 bytes）**。不trim、不接受CRLF/BOM、额外空格/行、未知token或无效UTF-8。准确内容长度在读取前已知且必须是候选长度之一；实际正文仍须回读核验，不能只看hash或长度推断选择。
- 计算 `computedSelected = (B == include-primary\n)`；与原受信 `Input.Materials[A].Selected`比较。匹配才继续EncodeMandatory/Plan/Publish；不匹配返回明确本地selection-binding拒绝，不能重写Input、自动更新revision或把普通不一致冒称context_overflow。错误语法/布局亦为明确无效输入；不新开公共wire错误或方法。
- 原输入被冻结后，改变B正文必须使用新准确Content版本/合法新input revision与身份绑定；不能同Content版本改hash/bytes，也不能借重试给旧Snapshot换选择依据。

## 3. 处理事实与M输出准确性

保持当前一次有界顺序真实读取A+B，再判断选择；包括omit分支的A也确实被compiler处理，不优化为跳读A却保留“processed A”。compiler完整processed及M直接sources在两个正常分支均含A+B，原顺序/准确hash/用途及继承闭包不变。

Derivation要准确区分“影响选择”和“被处理后省略”：B记录该SelectionVersion及实际 `include-primary`/`omit-primary`选择结果；A记录同选择版本及 `material`/`processed_omitted`结果。不得把omit的A也一概标成selection_only。可复用现有Derivation的Policy/Role/Output字段，无需通用表达式或额外关系表；canonical验证按准确mode、role和可信Selected核一致性，M.Input保留完整原输入不被encoder改写。字节容量版本仍在Input.Policy.Version。

真实worker直接材料：include是M+A（及其他原本选中的材料），omit是M（及其他原本选中的材料）；B始终不成为直接材料。omit时worker不直接读A/B正文，但仍读取含其准确处理关系的M；不得说它完全不知道A/B，也不得把M中的ref当它读取原文。公开Proposal.ProcessedSourceRefs沿旧口径是manifest+实际MaterialRefs，compiler处理集合另由M与真实Content闭包证明。artifact仍只是rule/2逐字节回显M，不解释选择、自然语言质量或Task成功。

省略事实至少在上述准确derivation和原Selected=false中可回读；若场景Input.Omitted含省略说明，应由可信输入提供且保留，compiler不得为了匹配偷偷改写原说明。

## 4. 必须有的真实正常/拒绝观察

| 场景 | 准确出口 |
|---|---|
| include正常 | B真实16字节、原A.Selected=true；真实Component接纳/完成，实际材料有A，artifact去前缀等于完整M；M与可获准来源观察含A+B。 |
| omit正常 | 新准确B13字节、原A.Selected=false；同样真实完成，worker直接材料无A/B，compiler仍实际读A+B且来源闭包完整；不能仅靠测试手改Selected无B解释形成对照。 |
| 两向mismatch、非法token/重复选择角色 | 分别配合法控制；在派生出版/Bind/Decide前拒绝，不改原输入。按已采用no-dispatch oracle，原Command当前授权not_found、准确fixture无binding/dispatch、真实Component入口零到达；不是Decision.Get伪not_found。 |
| B process撤销 | 独立准确policy场景，read仍可允许但新的真实ReadForProcessing/Compile拒绝；无新成功bundle/派发。 |
| B save撤销 | 独立场景保持read/process允许；允许实际处理，但在派生保存/最终出版门拒绝。若要证明读后撤销，用有限实际门在处理完成后安装真实policy再继续保存；原已发Content责任/字节未知保留，不假称无任何工作。 |
| B disclose撤销 | 先真实完成并有artifact/M，随后只撤B disclose而保留read/process/save；实际public Content Get不得披露M/artifact正文，原Decision历史/receipt不改。查询失败不能当删除；当前获准内部处理是其独立正常对照。 |

三动作拒绝使用独立policy版本/准确用途，避免先撤process使后续save/disclose“拒绝”根本没到目标门。include与omit均须保留B约束，至少对omit完成的派生链验证撤权，不能因B不在worker直接材料中逃掉来源交集。可复演正常控制采用独立合法新版本/输入，不宽化政策复活已收紧过期cap。

## 5. 原预算边界仅延续、不扩展

选择解析不增加额外I/O或重建轮次：A/B读取均扣原累计read budget，解析现有有限字节，默认一次Compile即可。新选择结果只改变worker直接材料集合，其余完整M/manifest/Proposal尺寸仍重新准确计量；不得把omit理解成删必要goal/条件/控制/责任。

当前compiler函数局部remaining每调用初始化，只能证明一次调用的有限读取；本文不据此宣称跨重建/重开总预算已经实现。既有handoff最多3轮、原绝对deadline与累计读取义务保持：mismatch默认有限拒绝，不为修Selected自启新轮；若正常输入竞争触发现有重建路径，必须继续原累计预算/原身份责任，不能借新选择策略刷新。该累计问题由本票原义务完成，不新增选择专属调度器或后票前置。

以上是必要窄默认，可由同一实施者落实并给真实正常/拒绝证据；不对当前部分源码宣称AC7通过。
