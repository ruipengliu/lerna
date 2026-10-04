# 03 旧 driver 资格判断：最小只读决定

## 决定

**采用有限、显式、hash 核验后的 restoration-only 补丁。** 仅修复 970 `added_driver` 恢复副本的 Claim 原因保留和 `publication_running` 的 RunClaim 预期错误判断；与现有两处 post-READY RELEASE 保责补丁组成一个明确可审查的转换。原归档 `.txt`、生产源码、provenance、SHA256SUMS 均保持原字节。FINAL01 control driver 不在生产 archive 内，直接在其当前 driver 源中作同等判断修复。

这会**收紧业务场景 oracle**，不能继续称“每个 business oracle 都未改变”。准确范围是：旧生产执行、原请求、正常业务事实生成及 READY 结构不变；新增 driver 的验收资格和失败诊断被修正。无需新 ADR、协议版本、producer 代理、通用补丁框架或用户再次批准。

## 实际读取与缺陷边界

- 03 固定对象：`1e752c28f9d48af9787add3a23306359901cbf2b`；02：`25287d5a08ff01d5265e56b06faf87cd37a09006`。标准审查 `/tmp/lerna-03-ticket-03-standards-review.md` 的基线是 `949c39237fda562e8bda994a8e1454a27232dc72`。本次没有把 dirty 修改当完成事实。
- 970 原 driver：`conformance/internal/decisionfixture/testdata/legacy-970fd90/conformance/internal/decisionfixture/legacy_writer_test.go.txt`。`publication_running` 中 `Claim` 错误被泛化成 missing，`RunClaim` 仅拒绝 nil；依赖故障也能通过该判断。
- 原 970 `components/decision_engine/worker.go.txt:490` 起 Finish 经原 Store.ValidateClaim 校验，`:554` 起 RunClaim 调用实际 Start/计算/Finish。`runtime/work.go.txt:11` 定义 `ErrClaim` 为 invalid、expired 或 replaced，故 sentinel 本身不独占“过期”含义。
- 03 当前独立 driver：`conformance/internal/decisionfixture/testdata/final01_control_writer_test.go.txt:90–104` 有同类问题，正是标准审查第一项及第二项的 Claim 部分。
- 02 独立 `testdata/final01_proposal_writer_test.go.txt:143` 检查 `errors.Is(err, context.Canceled)`；它制造的是另一个已明确的中断边界，不改为 ErrClaim，也不以其正确判断代替旧 970 场景。
- 共享恢复与编译 owner：`conformance/internal/decisionfixture/legacy_upgrade_test.go`。`:848` 起先核验清单、每项 provenance 角色/字节/hash 及总数，再写恢复目录；`:975` 调用 `guard970AddedDriverRelease`。当前 guard 原 hash 为 `266f74ee54198afde427b64dc0468bde8a123ec11a03ddef58c33594c38d42bc`，只替换两个精确 RELEASE 片段。

## 冻结范围与补丁规格

1. 970 库存维持 **68 个 archived_production / 422534 B，69 个 payload / 430396 B**；driver 的角色仍是 `added_driver`。FINAL01 696ac49 的 69 个生产文件 / 448760 B 也保持冻结。新增当前 driver 不成为原生产源码。
2. 在完整原 archive/provenance 校验成功后，仅允许当前 owned restore 目录的 `conformance/internal/decisionfixture/legacy_writer_test.go` 接受转换。继续以原完整 driver hash 为输入前提，精确匹配各预期片段且次数必须为一；缺失、重复、意外角色或未知 hash 均在 build 前拒绝。不能在整个目录作模糊替换。
3. 对原 driver 的有限变更为：精确加入标准库 `errors` 与**归档自身** `github.com/ruipengliu/lerna/runtime` import；分开处理实际 Claim error 和 nil claim；RunClaim 后使用 `!errors.Is(err, runtime.ErrClaim)` 拒绝 nil 与其它错误；保留已有两处 RELEASE 保责出口。不改 lease、发布装饰器、读回、Case 列表、请求/receipt、原数据库或运行迁移。
4. 推荐在同一小型私有转换中完成，名称/注释如需调整应明确包含 release guard 与 qualification，而非继续声称只改两个出口。可以保留原函数名以缩小 diff，但注释必须准确。无需抽出可插拔 patch registry。
5. 记录原 driver hash、转换版本/准确实施 SHA、允许的片段变化及**转换后** driver hash。转换后 hash 记在当前测试说明/运行证据，不能写回旧 SHA256SUMS 冒充原字节。消费方必须能区别“原生产 + 原 added_driver 的受限修正版”与“整个旧归档逐字原样编译”。

## 精确判断与原因保留

Claim：先 `err != nil` 则带原 cause 失败；再 `claim == nil` 给独立 missing 判断。RunClaim：先取得原 err；只有 `errors.Is(err, runtime.ErrClaim)` 才有资格进入该场景后续观察。不能用 `err != nil`、字符串匹配、context 超时或任意 PG 错误代替。nil 也应明确报告未得到预期资格拒绝。

仍须保留独立观察：原 claim 的 LeaseUntil、真实延迟越过该时间、两份实际 Source 输出与准确读回、原 Decision running、原 receipt 及 writer drain。`ErrClaim` 与这些事实一起支持此**受控场景**的过期 Finish 判断；不能把它泛化成所有 ErrClaim 都证明 lease 过期。若出现 joined error 同时带其它原因，保留全部原因并明确实际故障，不能因 `errors.Is` 命中就隐去其它异常。

错误链在仍持有 error 对象的阶段应用 `%w` 或 `errors.Join` 保留；通过现有有限、安全的诊断边界输出，不增加 DSN、环境或完整配置打印。driver 如缺安全 wrapper，可增加仅本 driver 使用的 stage error（安全 Error 文本 + Unwrap 原 cause），不引入公共错误体系。原 cause 必须在资格判断前完整保留；跨子进程文本/exit code 不声称仍可对原 Go error 作 `errors.Is`。父侧记录阶段、已确认退出与有限安全诊断即可，不能凭 exit failure 反推特定业务原因。

共享 compiler Wait 原因修复同属 routine，按标准审查处理：cleanup 接收 Wait 后保存/报告其 err；正常路径不得在 groupErr 时提前丢失已有 waitErr 与有限诊断。可先分别包装再 Join，让两个阶段均可判断。**非 nil Wait error 不自动表示 direct process 未退出**；仍用实际 Wait/ProcessState 与独立 group 观察判断资源可清理性。已确认 direct exit、历史错误、group unknown 是不同事实；修诊断不得放宽 scope 保留。

## 实施归属、验证与结论限度

由 03 作为共享恢复 helper 的唯一 fixer 实施，root 读准确 diff/固定 SHA 后，02 只 pick 同一共享修复。03 自身 FINAL01 control driver 同次修；02 自身 proposal driver 保持它的实际中断语义。不重写生产 archive、不改两套业务 driver 为共同 producer，也不把错误资格修复推迟到 whole 架构重构。

必要验证（由持有测试槽的实施方后续执行，本次均未运行）：

- 静态核对原 archive、provenance、清单和全部生产字节零差异；恢复后差异严格限于允许的 added-driver import、错误判断/诊断及既有 RELEASE 出口。确认两个 consumer 使用同一已提交 helper。
- 有意义的有限转换检查：准确原 driver 可得到唯一预期输出；变动 hash 或缺失/重复片段拒绝。若局部错误分类有测试面，验证 nil、无关错误、wrapped ErrClaim 的差别；不要为此修改生产端口或发明数据库故障。
- **实际 03 与 02 两个升级 consumer，各自 normal + race**：真实旧 970 writer 场景经更严格资格检查后仍产生原事实，升级后查询/原 receipt/费用边界及各自控制或提案行为满足自身 oracle；其 FINAL01 升级场景也按实际代码运行。独立消费者之间不互相替验收。
- 共享编译/清理修复的相关机械失败出口须验证原因不丢及 unknown 继续保留；不能以一次 build 成功证明 Wait/group 失败分支。真实缺陷若无可重复 native 触发，标记机械 seam 的证明范围，不造 native failure 证据。

旧静态 hash 与旧正常/race 结果继续证明当时执行的字节和实际事实，但**不足以证明其 RunClaim 返回了 ErrClaim**；不能据新代码追认旧日志已经验证该错误类型。修复后的新 pins、新命令、新日志形成新增证据。失败 builddir `3540254111` 等已有 unknown 责任不因修复或新测试通过而清除。

本文件是必要技术选择的只读决定；未实施、未测试、未占用 build/DB 槽，不是 03 整片退出或架构审查结论。
