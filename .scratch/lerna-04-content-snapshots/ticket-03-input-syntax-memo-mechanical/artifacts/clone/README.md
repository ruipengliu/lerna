# Binding 完整 typed clone：唯一静态修正

已真实取得首行为 RED：既有 Binding memo 的两处 Revision 指针各 cold/hot返回污染，四子例FAIL，nil counterpart PASS。原20行raw、原before源码、原格式化test/未格式化snapshot与实际ACK全部保留。不重跑未变RED。

本候选仅改 `conformance/internal/decisionfixture/context_binding_decode.go`：把原 Input 的准确typed slice/TraceContext复制提取为 `cloneInput`，补足 Unresolved 每个 OperationRef.Revision 和 Request.Payload.TaskRef.Revision 的非nil指针复制；cloneBinding调用它。其余 Bundle复制、memo范围与成功eligibility、owned字节key、decoder与原错误返回、所有实际业务消费者保持原字节。不是Inputmemo实现，也无Input dispatcher/预算/SQL/clock/authority变化。

`mutable-inventory-static.json`逐字段盘点生成合同中的全部mutable成员，尤其两处原表遗漏的Revision pointer；Material/UseRef/ConditionRef中的准确引用均为值字段。copyBindingSlice原函数不动，保留nil/empty；两新增pointer copy只在非nil时分配，nil原值保持。`before`和`candidate`整文件用.go.txt保存，不加入编译。

当前331源中唯一变化为该helper，另外330整文件byteequal；七份先前关键源盘点中仅该helper新SHA，其余六份不变。既有clock/target+ancestor BodySeal/accepted05 source/PG/Source/frozenworker均不动。HEAD与原WIP保留，test proposal未复制到WT，undefined Inputproposal仍隔离。

未来独占授权的exact commands在commands-static.json：仅该产品文件gofmt→原全部Binding测试 normal→仅actualPASS/Wait/currentgroupempty再same race。selector `^TestContextBinding`，5个原测试+新Revision测试，原Go/outer120/p1/integration/count1/read-only。overlay唯一虚拟test路径仍指向已格式化3128B/788392c8实际首RED源码，产品文件直接读取当前WT。无shim、无Inputundefined测试、无PG业务或容量通过承诺。

fmt需要保留actual差异/形成后current331与前置ACK；每阶段按strict82 supervisor原规则在native前登记PID/PGID/tick与owner/scope并fsync，完成actualWait/currentgroupabsence、原raw/ownFD各自Close事实。首failureSTOP不自动fix/retry。正常/race通过只闭合typed Binding深拷贝资格；下一Input compile RED与memo产品实现须根另授，不属于当前候选。

内部typed表示隔离不改变公开合同或领域决定，无需虚构ADR。全部历史大图业务FAIL、原CPU/Go输出FD UNKNOWN、firstPut原因/possibleeffect UNKNOWN与observeroverheadUNKNOWN均保持。

当前只有STATIC产品编辑和静态证据，无gofmt/Go/test/native。本文件不宣称修正已green，也不接受票03。
