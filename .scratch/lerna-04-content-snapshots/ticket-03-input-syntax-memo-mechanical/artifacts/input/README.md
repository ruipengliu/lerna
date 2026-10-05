# fresh Input 成功语法：唯一静态候选

原 Input 新内部API compile RED真实发生（9条undefined inputDecodeMemo、测试体未执行）；现只实现7b885/94ab已采纳的这一项。当前尚无Input测试green/性能/容量结果，不重复旧大图业务RED。

两个产品路径：新增38行/1323B `context_input_decode.go`，以及dispatcher新增私有Input项并替换成功Scan后的原closedJSON handoff。单项atomic pointer持有immutable entry；scope用既有bindingDecodeScope准确区分Store实例、完整TenantID/OwnerID、schema，加准确inputID、拥有的全部body字节。命中只返回已正常/race资格通过的cloneInput深拷贝，不缓存任何当前资格。

冷路径仍调用原closedJSON，失败直接返回同一decoder的原typed error、字符串与partial Input，不写入memo。成功且body<=原MaxBodyBytes才发布owned raw bytes和独立cloneInput；冷返回原decoder输出，其可变字段不与已clone的cache共享；热返回另外deepclone。只空白变化也不命中。每memo最多一个项、Input与Binding项独立，无global/跨Dispatcher/持久项/TTL/InstallInput prewarm。并发cold miss允许重复解析，immutable atomic项与每次独立返回保证安全，不声称exact-once解析。

**实际消费者接入**位于ContextDispatcher.input原完整SELECT body/FOR UPDATE与Scan成功之后。原SQL文本/参数/Scan和SQL失败返回零Input与原error分支逐字不变。除此两处dispatcher编辑，新helper之前不存在，完整inverse可恢复原dispatch字节；另外330原源文件包括currentAttempt整份context_budget.go完全byteequal。existing complete cloneInput/helperfe6不改，其normal/race真实资格与原4个行为RED保留。

不改变当前Input全量DeepEqual/Control/Permission、trusted clock、原预算currentAttempt jsonBytes/digest/完整tuple/revision/control/持久attempt核验、锁顺序、两ContentTx/target与ancestor BodySeal/Gone/actions/真实Objects IO+hash、peer/worker/frozen663d以及caller30/Claim5/Goouter120。语法成功项在后来权限/COMMIT失败后留下不表示授权；下次仍完整行读与原业务裁决。旧测量8map源码全部保持，不能用于覆盖此新dispatcher/helper；后续容量应重新绑定原5coarse映射。

## 下一有限资格计划（尚未授权native）

commands-static.json冻结：只fmt两个新改产品路径，保存before/diff/formed332；只test-only两map（已格式化788392 Binding pointer test+abd495 Inputtest，无产品替换）运行normal，只有actualPASS/Wait/currentgroupempty才same race。原readonly/local/p1/integration/count1/Goouter120。

selector ^(TestContextBinding|TestContextInputDecode)，12top测试（原Binding5+新Binding指针1+Input6），不是Input7top；README七控制类别只是语义分组。全mutable cold/hot污染包含两指针；原bodykey、完整scope/fullbyte/只空白变化、单项A/B/B/A、nil/empty、strict重复错误/typedpartial输出、1MiB边界、有限8×20并发均实际测试后才能记录资格。私有parsecount只证明语法复用机制，不冒公开业务或净收益。

当前候选与原331差异为一个dispatcher改变+一个新helper，所以当前完整named-source inventory为332，其中旧331的330整文件不变。metadata包含Makefile等绑定文件，不把332这个清单数冒充实际Go编译文件数。两个test仅virtual overlay，未复制到WT；新helper是真实WT文件。strict82在native前登记PID/PGID/tick/owner/scope、fsync与ACK；每次真实Wait/currentgroupabsence和ownraw writer Close事实分开；任一failureSTOP不repair/retry。

后续真实PG正常与warm后当前输入/Control/trusted期限拒绝、policy行锁时钟、预算累计/reopen、target/ancestorSeal和Process等受影响公开资格由root另外选取授权。当前机械N/R尚未执行，也没有弥补已有Now transport覆盖缺口。更后面的新fresh原4B normal→条件race、实际publication/readback/Completed完整尾也没有授予。

这是fixture私有确定语法表示的内部复用，领域与公开contract不变，无需虚构ADR。所有旧业务FAIL/原Go及CPU FD Close UNKNOWN、firstPut原因/possibleeffectUNKNOWN与观测开销UNKNOWN保留；本候选不接受票03或whole04。

本轮只STATIC写上述两个产品源与证据，无Go/gofmt/Node/PG/native。
