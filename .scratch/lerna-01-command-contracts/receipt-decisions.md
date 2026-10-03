# Ticket04：固定回执与当前进展的引用一致性

2026-10-03。补充 `/tmp/lerna-contract-decisions.md` 第5节；此处替代其中“progress.object_ref与receipt.object_ref一致”的未细化表述。不改领域不变量，不需新增ADR，也不修改产品代码。

## 决定：比较对象身份，不对整个ObjectRef做deepEqual

对象身份定义为 `(tenant_id, owner_id, kind, id)`。`revision`是准确版本限定，不是新的对象身份。

当found返回有对象的当前进展时，progress.object_ref与receipt.object_ref的上述四项必须完全相同。两者可携带不同revision：固定回执指向原决定时的版本，当前进展指向当前观察版本。不可用整个ObjectRef的JSON或结构deepEqual判断对象相同。

例如原applied回执的receipt.revision=`5`、receipt.object_ref.revision=`5`，当前进展progress.revision=`6`、progress.object_ref.revision=`6`，身份四项相同，结果合法。原回执必须保持5，不为匹配当前进展改成6。

## 每个记录内部的revision绑定

- 原有存在条件不变：applied必须有receipt.revision；accepted是否提供receipt.revision按本版回执Schema已定义的可选条件；rejected不得提供object_ref/revision。
- receipt.object_ref.revision可省略；存在时，若receipt.revision也存在，两者必须完全相等。两者都是规范十进制字符串，因此准确字符串比较即可。
- accepted若只有object_ref.revision而没有顶层receipt.revision，允许：该嵌套revision仍提供已知的原对象版本基线，不必制造第二个必填字段。也允许accepted两处都不提供revision，表示固定回执未提供版本基线。
- 具有对象观察的TaskProgress必须有顶层progress.revision；progress.object_ref.revision可省略；若存在，必须等于progress.revision。
- 没有对象观察的none/unavailable分支不适用上述revision比较，不应填造object_ref或revision。
- ObjectRef没有revision只表示按身份引用，不能据此宣称指向准确内容版本；准确内容仍使用ContentRef及其自身版本规则。

不自动补齐、删除或重写任何revision以让结果“合法”；编码器、解码器都应保留原值及其存在性。

## 原回执与当前进展之间的单调关系

先确认对象身份完全相同，再取原版本基线：receipt.revision存在时使用它，否则使用receipt.object_ref.revision；二者都不存在则没有可比较基线。

当前progress.revision必须大于或等于已知原版本基线，按精确非负整数比较，不用字典序直接比较不同长度的字符串，更不转JS number。`5 -> 5`和`5 -> 6`都合法；`5 -> 4`不能作为该found结果中的“当前进展”返回。原回执已证明该对象达到5，旧副本的4应作为暂时取不到一致观察处理，不能把它冒充当前权威进展。

这个检查只比较**同一owner、同一kind、同一id**的修订；不比较不同对象的revision，不比较CollectionView水位，不推导跨owner顺序。不要求进展恰好原revision+1，也不把单调revision等同于Task成功、效果关闭或费用关闭。

本合同不额外承诺跨两次独立查询的线性一致性/会话单调读：若后续读接口需要这样的保证，须由其读取和存储合同明确提供。这里必须满足的是单个found结果与其原固定回执不矛盾。

## CommandRef一致性是准确原命令身份

CommandRef身份为 `(owner.tenant_id, owner.owner_id, command_id)`；当前已定义CommandRef若使用扁平字段，采用同样三元组。它不包含查询自身信封的command_id。

对于found：

```
response.command_ref == response.receipt.command_ref == request.payload.command_ref
```

三者必须准确相同。请求target中的tenant/owner必须等于原CommandRef.owner，kind必须是command，id必须等于原command_id；请求目标是原命令，而查询信封自己的command_id只是本次读取关联身份。

not_found/gone/unavailable若含command_ref，也必须等于请求原引用。不得收到另一个owner或另一command_id的结果后重写引用来匹配请求。

CommandRef.owner与receipt.object_ref.owner不应无条件强制相同：命令由固定owner接纳，其关联对象按方法合同可以是另一合法owner的引用。当前方法若明确规定同owner，可用该方法规则限制；通用回执检查不凭空增加这一领域假设。同样，TaskProgress只要求与receipt关联同一个对象，不能把原命令owner自动当成Task对象owner。

## 验证边界与失败结果

- Schema表达字段存在条件、闭合分支和格式；公开编解码入口增加上述少量明确的跨字段检查。JSON Schema 2020-12本身没有通用的属性值相等引用操作，不为避免语义检查发明Schema关键字或重复手写字段Schema。
- 对调用方提供的结果JSON进行编解码时，违反以上关系返回公共 `schema_invalid`。
- 查询服务从只读事实源收到不匹配引用、内部revision冲突或低于原版本基线的进展时，不向调用方返回该found正文；返回保留请求原CommandRef的unavailable/dependency_unavailable。它表示当前无法取得可信查询结果，不是原命令被rejected，更不能改写固定回执。不得泄漏后端错误字符串。
- 原记录确实缺少可用进展时，可采用本版已定义的progress.unavailable分支来保留可读的固定回执；但不能将错误引用的后端结果静默修补成可信回执。

## 最少必要对照

1. receipt引用revision5，progress引用revision6，同身份，合法；两侧嵌套revision都省略也合法。
2. receipt顶层5/嵌套6拒绝；progress顶层6/嵌套5拒绝；每种配相等值正常对照。
3. receipt基线5/progress4拒绝，5/5及5/6正常；用9007199254740992和9007199254740993证明比较未经过浮点。
4. accepted仅嵌套5、progress6合法；accepted无已知版本时不虚构比较基线。
5. 四项对象身份中任一改变都拒绝，即使revision相等；可存在合法不同command-owner/object-owner的通用引用，不能混淆这两个比较。
6. 响应外层/receipt/request三处原command_ref任一改变拒绝；查询自身command_id与原command_id不同是正常情况。
7. 当前进展active到succeeded或failed时，固定receipt内容逐字义保持不变；不把accepted自动解释为Task succeeded。
