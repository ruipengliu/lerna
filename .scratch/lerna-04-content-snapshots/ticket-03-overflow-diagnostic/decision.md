# 04票03：有界溢出诊断决定

**采用最小内部 OverflowDiagnostic。** 它暴露真实已测量的规划结果和实际拒绝维度，帮助AC4/5分别观察metadata输入与Proposal输出主因；不增加权威、公共合同或成功Bundle，不另造Capacity/Bounds框架。

本次读取03 WT固定HEAD **`e3cee59b27492df398f58c2684ec017e7dd49cf7`** 的types/capacity/compiler/assembly及票03；已采用handoff读取固定integration **`d225b79f881a2a6ec034918117db5017306318c7`**。另只读当前三文件WIP差量：ErrBodyLimit及单Content对象检查、adapter映射；它们只作正在实施的上下文，不作为固定交付/green。root所述后续纯docs采用不改变此源码cutoff。本次无native、DB或总体审查。

## 1. 为什么此处有实际需要

当前 `CheckCapacity`对集合、配置、完整I、完整O均返回同一个ErrOverflow；`Compiler.Compile`在Plan失败时返回空Bundle。`Adapter.Plan`已编码shell/lock/manifest、M/其他材料、准确rule/2尺寸Proposal、原请求和public Decision保守预览，并构造 `Measurement`；因此不必新增重复计算器或读取私表，就可以在既有真实失败出口保留其数值。

已采用handoff §4规定I为三份metadata加实际材料、O为回显artifact加Proposal；§6要求metadata和Proposal各有独立within/overflow对照。只观察ErrOverflow不能识别哪个边界实际先拒绝；把全部尺寸复制到test helper中再断言其自算结果也不足。这是有实际消费者的窄诊断，不是通用可观测平台。

## 2. 推荐准确形状及接线

在 `domain/task/context`定义一个错误类型，最小字段为 **Dimension + 可选 `*Measurement`**。Dimension为内部闭合集（例如input_bytes、output_bytes、material_count、condition_count、closure_count、reserve_config）；按当前实际拒绝分支细分即可，不开任意字符串注册。`Unwrap() error`返回原ErrOverflow；原 `errors.Is(err, ErrOverflow)`、fixture overflow记录与no-dispatch流程保持。

`CheckCapacity`在其实际触发的分支给出Dimension。Plan仅在所有实际编码及closure测量完成后，把当次已有的完整Measurement附到该错误，再原样经Compile返回。可在CheckCapacity中直接携带完整合格测量，或由Plan补入；默认采用**CheckCapacity给维度，Plan附完整测量**，避免单元调用者/早期guard的默认零字段被解释为完整规划。复制MaterialBytes/ClosureCounts切片，不共享可变backing数组。

Measurement指针非nil只表示这次**完整规划测量已经形成**；nil明确未完整测得，不表示每项为0。不需要再加数据库记录、Task字段或诊断响应schema。错误字符串仅给有限维度/安全数字，不输出正文、ref、subject、原请求或拼接外部错误内容。诊断经当前内部已授权调用链返回，不新开查询许可。

完整快照仅来自通过前置有界输入的实际Plan；MaterialBytes遵守当前有限材料集合、ClosureCounts为实际有限规划根，不能把任意长数组复制入错误。先期数量guard可仅给维度，无完整Measurement；不为“诊断齐全”继续读取/编码被拒绝的大输入。无新增Bounds对象或改CheckCapacity签名的必要。

Compiler继续在Plan失败返回空Bundle；诊断在错误链中获取，不让一个非空部分Bundle被误当成功Snapshot，亦不因诊断而继续PublishBundle/Bind/Decide。错误聚合时保留原cause，不能把储存/权限/ctx失败改包装成业务overflow。

## 3. 测量与归因的准确边界

- `SnapshotBytes/LockBytes/ManifestBytes/MaterialBytes`是实际已规划序列化字节的长度，MaterialBytes[0]为M；不是已派发输入或模型token。
- `ArtifactBytes`是准确规则前缀加M的规划长度；`ProposalBytes`来自既有固定rule/2尺寸器真实编码的有限schema-shaped预览。它们是派发前预测/上界，不是worker已经生成Proposal或花费的证据。`PublicDecisionBytes`包含保守revision/usage占位，须标作预览上界，不能称真实完成响应大小。
- 诊断给 **input_bytes/output_bytes**，不直接命名“metadata_overflow”或“Proposal_overflow”：那些主因须由本case的独立不等式证明。多个限制同时超限时只报告实际先拒绝的维度，不能据它声称其他限制均通过。
- 早期结构数量拒绝、无效字节/负测量、编码失败、schema/wire单体超限或读取预算失败没有完整测量时保持相应原分类/缺测量。尤其WIP `ErrBodyLimit`→原adapter InputLimit不得被这个seam吞成ErrOverflow；预览编码失败也不能伪报已经知道Proposal字节数。
- 只用整数和原溢出安全检查；原60/120、编译deadline、读budget、64KiB/8KiB与原Decision limits不扩大，检查顺序不为得到喜欢的Dimension而改变。

## 4. 两个真实tracer如何使用

**metadata输入主因：** 实际Compile返回errors.Is(ErrOverflow)、errors.As诊断Dimension=input_bytes，完整Measurement存在。令 `B_I=min(capacity-reserve, MaxInputBytes)`，独立核 **len(M)≤B_I，但I=三份metadata+Σ材料>B_I**；若还有其他材料，最好再证Σ材料≤B_I以单独隔离metadata增量。输出O及集合/闭包/Content/wire等其他界限在该固定输入下均满足。配缩短合法metadata的实际成功对照；不能仅把M变小而未经核对改变另一边界。

**Proposal输出主因：** 实际Compile返回output_bytes完整诊断。令 `B_O=min(reserve, MaxOutputBytes)`，独立核 **ArtifactBytes≤B_O，但ArtifactBytes+ProposalBytes>B_O**，并保持完整I/数量/闭包/外层界限合法。更多合法条件/长ref的重复Evidence造成增量；配减少该增量的实际成功对照。若要称“预留输出主因”，须额外固定MaxOutputBytes不比reserve更严，不能把原Decision limit更小的拒绝冒充reserve。

数值断言不能只重新调用同一CheckCapacity作为oracle：保留已采用独立手算/审阅黄金，成功路径从实际Content回读shell/lock/manifest/M与真实Proposal/usage交叉核对尺寸器。在失败路径，诊断是实际编译边界观察，不要求为展示字节先出版失败Bundle；真实来源读取、准确输入和独立黄金共同支持数值资格。

两个拒绝均继续已采用no-dispatch联合观察：无成功fixture绑定/派发、当前原Command not_found、真实Component入口零到达；Decision.Get result_unavailable仅辅助。每个正常控制真实接纳并完成。诊断本身不是无派发、权限通过或持久恢复证明。

## 5. 采用范围

需要的修改仅中立错误类型、现有实际容量分支的维度、Plan错误出口附已有测量及对应观察测试；无公共wire/数据库/Task/新provider接口。selection策略若稍后改变M字节，重新按真实新输入测量并保持独立黄金，不能复用旧数值冒充新源。当前决定没有执行测试、没有断言当前WIP正确或AC4/5通过。
