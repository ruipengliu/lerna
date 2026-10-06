# 06 setup-cause：首 GREEN 后的必要机械覆盖与最终出口（仅 STATIC）

采用 `/tmp/lerna-06-setup-cause-next-decision.md`（5978B，SHA256 `2e0a50228b09af75ce4351eeb85bcbd4c793382a34220492b9e4ec3aef119dbb`）。首真实 compile RED 是 helper 未定义；首机械 GREEN 已实际通过。本文只提出下一最小测试和有限命令，没有写新增测试，没有 fmt/Go/PG/objects 效果。consumer 保持 81369B/a4fe8bc8559dd1f21aa3726efbc61f6c595a3e39107023f230675742d38ee771，首测试保持1758B/9410c9925c375fbc17321e52410e495a6cf541f8a0cf45921d945a3d968b5c47。

## 已执行边界

唯一两文件 fmt 实际0，diff0B，source27未变化。首 `TestContentProcessSetupRetainsReadAndFirstCloseCauses` 实际0，原raw5行；真实 Wait 后当前 PGID4175607 无成员，raw first Close ACK，无 timeout。`/tmp/lerna-04-ticket06-execution/setup-cause-first-green-release.json` 已 fsync STOP_RELEASE。27源/对应原快照全字节相同，原345行账本前缀/16历史 UNKNOWN 原 dev/inode/0700 不变。新346–349仅两个 native start/completion 对，没有 PG/object/child scope。机械注入读错/关闭错不是实测 native FD 故障，也不是业务 RED/GREEN。

## 下一最小测试改动提案

仅扩展 `/tmp/lerna-worktrees/content-snapshots-06/conformance/component/content_process_setup_test.go`，main consumer、其他25源不改。首 tracer 本身保持；计数 fakeReadCloser 增加可选输入字节，未指定仍读取原 `FAKE_BODY_SECRET`，原首 tracer 输入/原因/计数不变。新增的所有入口名统一 `TestContentProcessSetup...`，一组 N、条件 R 使用一个精确 prefix selector。新增用例可能直接 GREEN；不得修改 working implementation 或加 baseline shim 伪造 RED。

| 必要用例 | 原入口/输入 | 独立断言 |
| --- | --- | --- |
| 成功解析 | contentProcessReadIdentity；固定 proc literal `41 (worker (inner) name) S 1 2 3 4 5 6 7 8 9 10 11 12 13 14 15 16 17 18 777 20` | 返回传入41/40及原start字符串777，恰好一Read和一首次Close；含嵌套括号的进程名证明使用最后右括号、原第19索引 |
| read-only | PathError 包 EIO，firstClose nil | 零失败tuple，一Read/一Close；Is/As 原 PathError，native_io；固定安全阶段不含假路径/DSN/body marker |
| close-only | 原成功literal，firstClose 包 os.ErrClosed | 不能成功返回身份；Is 原关闭原因、native_fd_closed、一Read/一Close；不 secondClose |
| read+close | 已有首 tracer | 保留两原因和原 PathError；已有实际GREEN，不重复实现新入口 |
| 恰4096无原因 | 有4096输入、Read/Close nil | 原固定容量拒绝，零tuple，一Read/一Close；不能误取末尾可解析数字 |
| 恰4096有原因 | 同容量并返回 EIO + os.ErrClosed | 错误链两原因优先保留，不能被容量拒绝覆盖；安全诊断有限 |
| malformed/短fields | 无右括号或最后右括号后不足20字段 | 原固定形状/缺start拒绝，零tuple，一Read/一Close，无伪造 native_io/closed 类别，marker不出现在Error/类别 |
| ParseUint真实 typedcause | 同成功literal但start=`FAKE_DSN_SECRET_START`；另start超uint64范围 | As 原 *strconv.NumError、Func/Num 与原输入相同，Is ErrSyntax/ErrRange；外层是私有stage wrapper，不能把外层type当原typedcause；安全Error/类别不含 Num |
| nil wrapper/diagnostic | contentProcessCause 固定stage,nil + CauseClasses(nil) | 都是 nil，不凭空制造原因 |
| 实际 bounded ReadFrame typed JSON | 真实 process.ReadFrame；本地有限 framed bytes（4-byte大端长度，payload<=FrameLimit），目标明确struct；已知field带假DSN/body然后非法字符 | As真实 *json.SyntaxError，wrapper保留同一原 error 链；Error/类别/固定stage不含payloadmarker；不是业务child/原生pipe失效 |
| 实际 JSON secret-field拒绝 | 真实 ReadFrame，unknown field名为 `FAKE_DSN_SECRET_FIELD` | 原 DisallowUnknownFields error 确含marker，由同一私有固定stage wrapper安全输出；Is 原error，有限<=12类别，不打印原err |
| 取消类别 | 真实 ReadFrame + io.Pipe，原1秒有限context立即cancel；另立即已过期deadline context | physicalIO 实际关闭 in-memory reader、Read被唤醒后返回原Canceled/DeadlineExceeded；Is与bounded classes一致，不新超时或阻塞等待；两端由原defer有限关闭，不创建业务children |
| joined取消链有限性 | errors.Join(Canceled, DeadlineExceeded, os.ErrClosed, EIO) 经同一固定stagewrapper | 多个known类同时保留，类别唯一<=12，每个type文本<=128；没有原message/tree序列化，不扩大公共enum |

成功literal必须由源码中的固定20个tail字段构造或固定literal核对，不从 helper 计算期望 start；独立 read/close及错误原因期望来自测试输入。ReadFrame输入可用 bytes.Reader/io.NopCloser，不给测试授予额外全OS mock框架。取消使用真实 io.Pipe 和原有限 context，无 sleep、实时lease刷新或假nativeClose。所有失败消息只固定文本，不 `%v/%+v` 输出原error；敏感marker只用合成假数据。

## 精确有限回归与检查

`commands.json` 提供完整六个原argv/strict90a wrapper，而非运行许可。新测试 STATIC diff/source27candidate 待 root FULL 采用后才可唯一LOCAL fmt新增test→mechanical N；实际 N 通过/Wait/group消失/sourcepins不变/原raw关闭后，R还需 root阶段授权。原120 Go/outer，count1/p1/readonly/local保持。

默认成功分支最小资格是既有 `TestContentSIGKILLAfterDurableObjectsRecoversOriginalPublication/normal_release` N与同 normal-only R。真实preopen→已登记身份→原publication→独立 Store/Object Close ACK、Wait/Stop→原公开完整receipt/独立全部字节/reopen尾不可省。最终既定 affected Content N/R 若精确实际包含此 normal case，可以履行而不重复运行。不再重跑旧五父场景15条，不增加 secondary/offline/ENOTEMPTY 新child组合。原旧五场景当前producer资格对应 c9，错误修正按 wholebyte 成功路径证明承接；不宣称 a4 源故障全部重跑。

最终 required `make check` 的全命令有唯一120 outer；`go test -race -count=1 -p=1 -mod=readonly -timeout=120s ./...` 为 fullmodule race120。Makefile check 包括 lint（Go format/Prettier/vet/TS lint）、generated、unit/contract/build。最终 integration affected Content normal/race、fixture/local域沿原项目入口另定精确argv，不能把这里两个命令冒称带tags的全integration。ownWT product仍 dadd lineage，root current primary19b01ca/b9共享runtime尚未同步；最终 merge/sync 后必须重新冻结实际最终源/脚本/全部受影响检查参数，再授soleLOCAL。旧资格不成为隐藏03/wholeCI业务出口。每条首失败真实保留、STOP_RELEASE，无修改budget/repair/retry。

## 狭义 owned resource 审计准备

`owned-audit-targets-at349.json` 仅从自己的完整原账本1–349提取，固定SHA；45个schema、45个dev/inode已登记object目录、45个实际native/child PGID（group可能含多个不同child starttick）、56个child generation记录。16 sticky UNKNOWN 独立inventory/hash固定。没有执行新的FS/proc/PG审计，targets不能当观察结果。

参考已accepted05原 `/tmp/lerna-05-own-resource-audit-static/audit.go` 和 plan 的边界：唯一supervisor真实 preACK 登记身份后执行；原 caller5/Tx3/statement2/lock1，Go编译与outer120独立；只 parameterized namespace目录和实际已登记 backend PID 当前state，没有body/SQL文本/DSN日志。pgx.Connect返回后立刻defer首次Close（caller原5，不刷新），将实际 protocol PgConn PID 以安全数值fsync own账本后才 BeginTx(ReadOnly)/catalog queries；Tx获得后立刻deferRollback并将真实关闭/rollback错误 join。所有原 sourcepins和 input/ledger interval 先重核。

本06原账本没有登记历史PG backend PID。因此 inputs.backend_pids=[] 必须明确表示**没有历史PID所有权证据**，不能报成“所有历史backend已消失”，不能借其他agent/shared库backend。新审计自己的实际连接PID可登记并核Close；如root要求更强backend出口，先依据真实原连接记录界定，不凭搜索SQL/路径假定归属。

FS只对原exact targets Lstat（不follow symlink、不树遍历），记录absent/originaldevino present/mismatch/readerror；UNKNOWN16必须原devino0700保留，无cleanup授权。正常历史scope即使absent也只表当前absence，真实Close责任从原generation Close记录独立列出。/proc最大32768entries，每entry检查同一caller deadline；只比较已登记PGID，输出真实PID/starttick/state供PID复用核对。kernelabsence不补逻辑Close。若任何present/mismatch/原Close未知，留真实finding而不DROP/unlink/RemoveAll/signal。

审计源码和exact prelaunch 在最终检查新scopes登记后另行 STATIC准备；本次不复制05产品或编译其observer，计数仅349cutoff准备值，未来最终cutoff必须更新。新的 own Go audit源置于 /tmp 独立静态目录，避免进入repo历史gofmt扫描。root FULL具体可执行源码/argv+pins 后另soleLOCAL执行。最终独立规格/代码与架构轴review、P2关闭证明、scope资源事实、finalsource sync/integration仍pending，whole06保持claimed。
