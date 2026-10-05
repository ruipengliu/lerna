# 03 Input memo：唯一诚实TDD次序（STATIC）

**不采用test-only baseline shim。先修真实既有Binding clone的两处指针隔离缺口；随后允许新Input memo的真实“缺类型编译失败”，准确配合已有大图业务RED，进入最小实现及真实PG资格。** 两种失败的证据范围分开，不为形式上的行为RED造一个未被旧消费者调用的实现。

## 依据与当前源码

本轮只读当前 `ContextDispatcher.input`、`bindingDecodeMemo.decode/cloneBinding`、既有测试及生成合同；未改源码、未运行测试。源码仍为：

- `context_dispatch.go` SHA `5c2b8d472fdbe458b9affb9a9543db3359c85c0dfa90d5357bea843995e624fb`，`:161`原实际路径是fresh SELECT/FOR UPDATE → closedJSON →返回，没有可注入解析次数的既有Input memo端口。
- `context_binding_decode.go` SHA `457b065fb760a264f0c687f82275c1dd73af3414191cccee1bba80b527f04779`，真实Binding消费者已经使用该memo；其decode有真实私有机械测试接缝，不能把它称为Input memo消费者。
- 既有 `context_binding_decode_test.go` SHA `2333886aa9349e9c1e13d0c8b3a87165ba4742287b21f25fa240fc092ed00f8d`。

根AGENTS“测试与交付检查”要求可观察行为/恢复保证，逻辑与race按实际变更验证，业务经Application/Component；不支持把私有调用计数当业务通过。本轮未发现可读取的独立tdd技能文本，不伪称其规定了“每个新type都必须先有行为RED”。以下贯彻当前已授权的真实失败、最小改动与分层验证要求。

## 先闭合真实已存在的深拷贝缺口

生成1.1类型实际有 `ObjectRef.Revision *Revision` 和 `TaskObjectRef.Revision *Revision`。现cloneBinding只复制Unresolved slice、整份Request值及TraceContext，因此 `Unresolved[i].OperationRef.Revision`、`Request.Payload.TaskRef.Revision` 仍共享指针。这是STATIC实际源码缺口，尚未本轮native RED；前一7b885建议“复用旧clone”必须理解为复用经补足验证的完整typed copy，不能原样搬浅拷贝。

1. **仅先增加真实已有Binding decode路径测试**：原合法表示含两处非nil Revision；解码cold结果后分别修改所指值，再对相同原bytes解码hot，必须仍得到原值；另修改hot返回后再次读，仍原值。保留nil与非nil不同、调用方原body隔离。直接测实际production-private helper，不复制baseline、不改旧producer。该测试应按观察报告真实行为RED；若没有按预期失败，先核对测试，不能硬写RED。
2. 最小typed `cloneInput` 补全这两个指针和值，供现Binding clone使用；原slice/嵌套gaps/UseRefs/TraceContext及nil/空语义全部保留。原Binding机械控制正常/race后，这个真实漏洞才关闭。它是复用完整表示的必要安全前提，不是另一个性能策略或新框架。

本项RED仅证明**Binding深拷贝隔离缺口**，不证明Input memo重复解析的性能，也不替代真实当前授权检查。

## 再完成单一Input memo改动

3. 再增加拟定 `inputDecodeMemo.decode(scope,inputID,fullBody,closedJSON)` 的窄机械合同测试。由于type尚不存在，允许实际编译失败；只记录精确undefined诊断、源码/命令/退出，标为**新内部API尚未实现的编译RED，测试体未执行**。不得将fixture/导入/格式等无关失败算该RED，也不能说parsecount断言已经运行失败。此时已有原四B实际容量业务RED和当前成本测量作为改动动机，不需再跑失败大图来“补红”。
4. 最小实现memo并**接入原 `ContextDispatcher.input` 的真实完整Scan之后**。一项成功语法、准确scope+原inputID+全字节命中、完整cloneInput返回；冷strict错误原样。不能只让孤立helper测试green而真实消费者仍旧直解码，也不能增加public/domain测试port或全局替换closedJSON。原7b885的currentAttempt序列化/全tuple/revision/digest/control、每次fresh row/Clock/授权/Content物理门/30-5-120预算全部继续执行。
5. 运行窄机械冷热/count、scope/byte变化、严格错误、两指针及所有可变字段cold/hot污染、nil/空与race控制。count在此只证明私有纯语法复用机制。随后真实PG公开路径验证warm后输入/控制/期限变更仍拒旧资格，正常对照、行锁等待后时钟、reopen与累计预算等原受影响门保持；既有权限/BodySeal资格不能被count测试替换。
6. 固定新源码和实际消费接法后，按原批准范围分别一次原完整4B normal、race，前一失败即停；完整公开尾、原费用/责任、Prepared/未知恢复边界不缩。结果而非机械green决定容量是否改善。无新profile/矩阵/重试或预算改变。

测试执行顺序必须保证第1项能实际进入测试体：不要先把引用不存在Input type的测试放进同包，让它遮住Binding行为RED。现有clone安全修正green与随后Input编译RED分别保存。未来若用禁用memo做mutation检查，只能标新实现mutation敏感性，不能倒写成旧产品历史行为RED；本决定不要求增加这一步。

所有native/源码变更仍由root另行授予sole owner。本文没有测试结果、票退出或whole04完成声明，也不改原采用报告字节。
