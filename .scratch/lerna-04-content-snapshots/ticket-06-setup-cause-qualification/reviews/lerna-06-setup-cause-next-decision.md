# 06 setup cause：最小必要修正与资格（STATIC）

**采用 P2 修正；P3 holder 断言重复 KEEP。** 必须等当前 remaining14 在旧源码上实际结束并释放 LOCAL，再由唯一实现者修改；不边跑边改，不将本报告当执行许可。本文无 native/源码/票状态修改。

## 来源与范围

读取 `/tmp/lerna-06-final-standards-review.md` 全文，SHA256 `538739723a8e99b95e70ca14f975e806381df606ef8ea02625c681d10201a096`；核对 WT `/tmp/lerna-worktrees/content-snapshots-06` 当前 `conformance/component/content_process_recovery_test.go` 前430行（SHA256 `c9a098141d23cda9c40d210539384d7cfb17b0d6b4f0222c16d70577308ff7e5`）、`conformance/internal/testkit/process/pipes.go` 与 AGENTS:105。没有读取另一规格轴。

实际缺陷明确：`contentProcessIdentity` 丢 Getpgid/Open/Read/首次Close/ParseUint 原因；Read+Close 可同时失败。child preopen 的 OpenInherited、Receive、Lstat、Emit 和 permission Receive 也只输出固定 Fatal。公共process底层已有私有 stageError/Unwrap，Receive/Emit 可能返回带取消、I/O、JSON 原因的错误；消费者不能再次丢掉它们。修正限本 consumer，不修改公共process模块或业务协议。

## 最小实现默认

1. 为本consumer提供私有安全 cause wrapper：`Error()` 只返回源码固定阶段说明，`Unwrap()` 返回原 error；nil 原因不凭空变错误。wrapper阶段不是外部输入。Getpgid/Open/ParseUint 返回安全包装后的原错误；`errors.Is/As` 保留真实 errno、`*os.PathError`、`*strconv.NumError` 等。禁止 `fmt.Errorf("... %w", err)` 后直接打印其 Error，因该文本会含原路径/数据。
2. Read后仍无条件且**仅一次**调用原首次Close，先 `errors.Join(readErr, closeErr)`，再包固定阶段。`n==4096` 是独立有限输入拒绝；若同时有Read/Close错误，不能用容量拒绝覆盖它们。缺括号/字段不足仍是固定形状错误，不伪造原生原因。未确认Close仍是UNKNOWN；不第二次Close试图洗绿。
3. 所有上述启动分支先捕获原err，分别处理 I/O 错误与字段/身份拒绝。`if err != nil || invalid` 应分成两个判断，使成功I/O后的非法descriptor仍为真实结构拒绝。Lstat失败先返回，避免nil info路径；不改变任何原校验。
4. 输出只使用固定阶段+现有 `contentProcessCauseClasses(err)`，不用原error `%v/%+v`、路径、payload、DSN或NumError.Num。identity调用处原 `t.Fatal(err)` 也改为安全类别。OpenInherited的非nil部分句柄先保持原defer；defer中捕获并安全报告首次pipe Close；Respond失败捕获并安全报告，不用仅generic文本吞cause。主失败与defer关闭失败都必须可见，后者不覆盖前者。已有closed frame仍按原step/objects_close/store_close独立类别及独立ACK，不改其上限、字段或真假含义。

已有 classifier固定11个known causes加一个有限type，本次无须增加公共CauseClass、枚举或无限cause树序列化。安全wrapper会改变外层type诊断，但原typed cause必须仍可通过As取得；机械测试须明确这一差别。错误文本保密不等于丢掉内部cause。

## 诚实 TDD 与最小机械 seam

优先真实既有 consumer 入口能触发的 malformed identity/取消/pipe错误。为Read+Close双错误可引入一个**本测试文件私有**的 `io.ReadCloser` 消费函数，真实 `contentProcessIdentity` 在实际Open后直接调用它；函数只承接原4096读取、一次Close、原解析及返回，不注入PID注册、PG或业务结果。不建立全OS接口/全局函数变量/资源registry。

新增该私有函数尚不存在时允许如实记录 compile RED；实现后机械行为检查应覆盖：成功原字节解析；独立read错、独立firstClose错、两者同错；长度上界；真实ParseUint类型原因；包含假DSN/路径/正文marker的PathError/NumError/JSON错误经过包装和诊断后不泄漏。用 `errors.Is/As` 检验原链及两原因，用输出字符串检验安全及有限类别。计数只用于这个机械关闭责任，不能替业务/物理擦除证据。

可以用真实process.ReadFrame的有界机械输入触发JSON decoder错误，再走同一安全输出；无需启动业务child来制造每种preopen错误。不创建test-only baseline shim制造行为RED。注入的read/close原因明确叫机械 seam，不声称真实native Close故障或网络故障。若有真实既有入口行为RED，独立记录；当前P2是STATIC发现，不冒称已执行RED。

## 回归范围与有限出口

- 先完成旧源 remaining14；它们只资格旧c9源码，失败照实保留。随后固定最小diff，机械 N/条件 R及格式检查；由独立source复核确认成功路径顺序、Read/firstClose次数、原解析、FD归属、所有gate/Claim/期限和business assertions未改变。
- 如果严格只有上述错误保留/安全输出及身份读取的等价提取，默认只补**一个现有共享child正常publication场景的 N 和同正常场景 R**，核真实preopen→registered identity→publish→双CloseACK/Wait/Stop→公开原receipt/完整字节/reopen尾。可复用 `TestContentSIGKILLAfterDurableObjectsRecoversOriginalPublication/normal_release`；这不是新增故障矩阵。不为诊断改动自动再跑原五场景15条或新增secondary组合。
- 若提取实际改变成功分支、协议、Close次序/错误到ACK映射，或正常对照失败，必须重新界定受影响场景，不能套上述缩小范围。最终既定Content正常/race入口若已完整包含该正常对照，可用其精确结果履行该项，不额外重复。
- 最终review复核本P2关闭、必要仓库检查及资源审计/六AC由root处理；旧业务资格按实际未变路径与新diff说明承接，不宣布新源所有故障已经重跑。原30caller/20child/Claim5/I/O4/Tx3/statement2/lock1/120及Q不变，历史UNKNOWN不补证。
