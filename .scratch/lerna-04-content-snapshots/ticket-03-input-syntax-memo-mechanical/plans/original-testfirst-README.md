# Input 表示复用：分开的 test-first 静态方案

当前仅两个测试 proposal，产品源码与当前331源完全不变；未运行格式化、Go、数据库或测试。不是 RED 结果，也不是容量通过。两份 proposal 留在 owned 目录，未复制到工作树。

已采用 `/tmp/lerna-03-fresh-input-cost-next-decision.md` 与 `/tmp/lerna-03-input-memo-tdd-next-decision.md`。先真实 Binding 隔离缺口，再新 Input 类型；不创建 test-only baseline shim。先前 Input 大图实际业务 RED 独立保留，不能用私有 parse 次数覆盖。

## 首阶段：既有真实 Binding seam

`context_binding_revision_clone_test.go.txt` 新增唯一 `TestContextBindingDecodeOwnsNestedRevisionPointers`。引用已存在 bindingDecodeMemo.decode，合法闭合 JSON 含两处非nil Revision。四个反例分别污染 cold/hot 返回的 `Input.Unresolved[0].OperationRef.Revision`、`Input.Request.Payload.TaskRef.Revision`；再次读取相同原字节必须仍等于完整原 Binding。正常 counterpart 先验证 cold/hot 等于原值；另有 nil counterpart 与 caller-owned raw body 变更控制。

既有 cloneBinding 对两处指针尚为浅复制。这只是静态预测；实际首运行若进入测试体且失败，报告真实绑定隔离行为 RED；编译、格式、setup 等失败不得冒此行为 RED。测试无 PG、无私表、无授权或性能断言。先使用 binding-only overlay，绝不让引用不存在 Input 类型的测试进入该次编译。

根分别授权：仅该 proposal 的格式化（保留原字节/差异/新SHA）→原 selector normal count1/Go120/outer120。strict82 wrapper 将实际 PID/PGID/tick 与 scope登记 fsync 后 ACK 放行，父 phase 另存 source331/prelaunch/argv/offset/raw；完成真实 Wait/current group absence、own raw FD fsync/Close，再 STOP_RELEASE。失败不自动修复/重试。

真实 RED 经根确认后才能最小提取 cloneInput 补足两指针；原全部 Binding机械资格 normal/race 使用原 `^TestContextBinding` selectors，全部成功后才进入下一阶段。新实现源需重新绑定，不伪称静态 source331 是将来新产品字节。

## 后续独立 Input proposal

`context_input_decode_test.go.txt` 引用拟定接口：

`inputDecodeMemo.decode(bindingDecodeScope, inputID string, body []byte, decode func([]byte, any) error) (compiler.Input, error)`

scope 复用现有精确 Store 实例、owner（tenant与owner均区分）、schema；Input项独立于Binding项。当前 inputDecodeMemo 不存在，实际引入后的首运行只可能记录新API的 compile RED，测试体未执行；不伪称复用行为断言已失败。

七个有限机械控制：完整 fresh byte 的冷热原语法一致与 raw byte 所有权；14种 mutable 返回污染（cold/hot）；nil/present-empty；store/tenant/owner/schema/inputID/完整值字节/只空白变化；A/B/B/A 单项替换与独立 memo；严格错误重复不缓存及原1MiB exact-limit正常；8 goroutine×20有限调用且返回互不污染。所有期望通过原 closedJSON 实际语法或原 fixture 确定，不复制 memo/clone 算法，不以私有函数/次数冒业务验收。

全 mutable 清单：Constraints；Conditions及每项Gaps；Control.Restrictions；Budget.Unknown；Unresolved及每项OperationRef.Revision指针；Progress；Materials；Omitted；Gaps；Request.Payload.UseRefs；Request.Payload.TaskRef.Revision指针；Request.TraceContext指针。其余当前值类型不含可变引用。完整 marshal/unmarshal 冷对照保存所有其他字段，nil revision与nil trace不同于非nil值；slice 的nil与present-empty不同。exact-limit用实际closedJSON，超界真实严格失败；没有虚构“超界仍成功”的decoder来制造业务前态。并发安全不要求竞态冷miss恰好只解析一次。

Input memo 在未来实际完整 SELECT/FOR UPDATE 与 Scan 成功之后才可能接入；SQL/error 顺序、currentAttempt jsonBytes/digest/tuple/control、原authority/clock/tx/budget/Content/IO均必须继续每次原样执行。未来真正 PG warm→输入/control/trusted期限变化、行锁时钟、累计预算/reopen等资格另由根选择并授权，当前这些私有机械控制没有证明这些业务门。已知 Now transport缺口仍未覆盖。

## 冻结与限制

`source-manifest-static.json` 保存当前完整331 pins、精确工作树状态、七份关键 before源、两 adopted decisions 和两个 proposal SHA。所有 before 的 Go 原字节用 `.go.txt` 保存，不进入编译。两份 overlay 各只新增一个之前不存在的 `_test.go` 虚拟路径，无产品覆盖。commands-static.json 都是未来分阶段 argv，无 native 授权；first_binding 之外各阶段必须先绑定当时新源再执行。root owner dev/inode由原 root-ack约束。

原大图两正例 FAIL、Measured observer开销 UNKNOWN、原首Put原因/possibleeffect UNKNOWN、原Go输出FD和旧CPU原descriptor logicalClose UNKNOWN保持，新的 owned读取/关闭事实不能倒补这些责任。
