# 18: 全流程历史与可靠性联合验收

**What to build:** 所有改进在同一生产宿主中保持原公开与持久契约，完整检查给出可追溯的通过证据。

**Blocked by:** 01（执行一致性套件通过窄 Interface 验收）、11（模型、模拟目标与查询完成受信规则迁移）、12（三种封闭共享发送关闭算法）、13（初始目标通过单个 Interface 创建任务）、15（非成功关闭接入共同封闭交付）、17（启动恢复接入统一宿主入口）.

**Status:** resolved

- [x] 审计所有必需依赖及迁移调用；无残余必需能力运行时发现、无调用过渡接口、重复同版本规则或重复封闭交付实现；明确可选能力保留已定义的 fallback 或失败行为。
- [x] 正常生产输入→提议→准入→执行→核对→完成，以及取消、非成功关闭、费用与迟到事实在同一受测树联合运行。
- [x] 所有支持原历史可读；未知格式与未知实现按原不同保证拒绝；回执、发送、逐发送费用、闭合视图及 Result 字节保持。
- [x] API、FILE、模型、模拟目标的请求、效果和账单来自独立观察；恢复及命令重放不得盲目重发或新增业务发送，显式安全重发仍经过原门禁并保留原身份；收尾与后续责任不丢。
- [x] 完整 make check 通过，包括普通/fault race、依赖 lint、协议兼容、规则及生成代码检查，记录实际源码修订、命令、结果和证据。
- [x] 相关设计与实施结果准确对应最终结构；不修改或关闭父规格，不新增业务功能、schema 或迁移。
- [x] 该票仅承担联合验证与必要残余清理，不接收前序票遗漏的协议迁移或必需依赖实现。

## Comments

- 2026-10-07: Claimed on `codex/interfaces-18` after all implementation dependencies were resolved. This ticket remains claimed until residual cleanup, independent review repairs and the final complete check pass.

## Answer

2026-10-07：01–17 已 resolved，残余清理、独立两轴评审及修复完成后，最终完整 `make check` 在主集成 `ba1024bc6453690c0c444f381b8e0bb9e603b139`、tree `f3df2d3b13116eacc5aef70a1fe8ea2471e11894` 上真实通过。以下七项按同一最终受测源联合验收；历史切片的准确 source、命令和观察边界保留在 `/tmp/lerna-module-interfaces-implementation/coverage-audit.md` 及各票 notes/Answer，不以测试数量或早期 green 代替最终记录。

1. 必需依赖和真实调用已逐路径审计。九个 Core constructor 返回 checked error，完整 Store/Work/Decisions/Start/Source 依赖、typed nil、真实环与 complete-before-use 均有公开 consuming-interface 和装配负例；41 个 With 链接均有真实生产调用，无剩余必需能力临时发现或无人调用的过渡接口。三个明确可选 Core 能力保留原 Compile、P4/P5 或 UNSUPPORTED_CAPABILITY 行为，CheckedIO 缺席不绕过托管 FILE 资格。CLI 可选命令以命名 Interface 保留 UNSUPPORTED_FEATURE、解析与权限顺序；删除无人读取的空 observation 字段，宿主 ObservationProgress 仍连接。固定受信规则选择器由纯资格和 Interpret 共用；三个私有 Task delivery kind 共用原交付和 ACK callback，Ledger 三种 seal 共用原逐发送关闭算法。静态明细见 `18-final-source-audit.md`、`core-port-audit.md` 与 review 修复报告；审计中的旧 candidate 仅表示历史快照。

2. 正常生产 input→proposal→admit→execute→reconcile→complete，取消、FAILED/CANCELLED、费用、迟到事实与 followups 由最终普通和完整 fault 检查统一覆盖。公共场景核对唯一初始 Task/Requirement/路由及 GOAL 与 legacy 回执区别、原 claim/lease/due、driver position/唯一 continuation、Open 与编译 CLI 的真实调用。17 的原 21 个启动方法及 issuer 顺序保持；Tasks/Ledger 纯资格先于业务，原 65 秒 Open deadline、两次 OperationProgress 和 enabled driver 位置不变。ManualProgress 保留原 caller/context/权限与首错停止，不接管未到期 claim，不提前 future due，不吸收 Startup 阶段。

3. 原 API、FILE、simulator、model 支持历史通过公开 queries、receipt、Send、逐发送 Reservation/BillingSource、closed view 和固定 Result bytes 验收；拒绝未知实现前保留另一原 READY 责任。资格不 Compile、解析 body、读取凭据或访问 target。未知物理格式/身份保持原 file group；识别格式中的未知实现保持业务事实，不增加 WAL/SHM 字节完全不变承诺。contracts/proto、contracts/gen、SQLite migrations、原身份与版本均未改，protected script 核对通过。

4. 独立观察与持久责任分别验收。Simulator 的 Target.Snapshot requests/effects 与 Bills、API handler 分计 POST/GET/effects/bills、ModelProvider.Calls/Bills 证明恢复及重放无新增 business send；授权的 query GET 不归为原 POST。安全显式 resend 仍过原 gate、保留 external key/Attempt，以不同 Send 对应一个 effect。FILE 使用真实 pointer/object/manifest、readback、rename/read/barrier 及 namespace/bytes/inode/mtime；费用证据是原零费用 profile 和逐发送 BillingSource，没有虚构外部 Bills。装配的资格/恢复/IO probe 零与独立目标零分开记录。三种 closure 仅原 receipt NOT_FOUND 才交付，全 proof 后在原 owner tx 保存 receipt、required source event 和 claim-fenced Job；缺历史保守 UNKNOWN，source 故障原子回滚。原物理 IO 临界区、完整历史发送、晚到 marker、late billing/followups 与固定 Result 保持。

5. 最终命令是原入口 `make check`，cwd `/Volumes/Data/proj/lerna-docs`；macOS 27.0.1 arm64，Go 1.27.1 darwin/arm64，CGO_ENABLED=1。UTC `2026-10-07T05:53:39.175516+00:00` 至 `2026-10-07T07:15:37.957118+00:00`，4918.696 秒，exit 0。普通 `go test -race -timeout 20m ./...` 通过（admission 538.200s）；完整 `go test -race -timeout 120m -tags fault ./conformance/... ./infra/sqlite/...` 通过（admission 725.878s，whole fault package 4363.039s）。部分包按日志使用 Go 缓存。普通/fault lint 均 0，格式、buf format/lint、规则和完整入口尾部的静默生成 diff 检查通过。原 `main` 没有 protocols，buf breaking 按既有 Makefile 规则跳过，未宣称历史协议基线比对通过。准确 source/result/log 为同目录 `final-check-source.json`、`final-check-result.json`、`final-make-check.log`，亦归档在 `final-attempt-02/`。before/after HEAD、tree、config hashes 与原用户 diff 完全一致，`same_source_and_inputs: true`；user diff SHA256 为 `d5d23bb59b5545466cc9e61129cbcf075fcf9859db03e787662ca52672897677`。配置全量 hashes 见 source/result，原 Makefile SHA256 为 `86b8340b293063ba27fec57fc1ac64a060b410168516a6cb441c99fa309918b5`。

6. 设计与最终实现一致，规则变化先更新模块设计。独立 Standards 初次两条 P3 分别为 conventions §5 修订记录违例和重复固定 selector 的 smell 判断，已补 API/FILE dated revision 并以私有 fixed selector 共用选择；Spec 初次零 findings。两轴各自复核 review 修复和 registry 修复，均零新 findings，分别保存在 `standards-review.md`、`spec-review.md` 及 `*-review-fixes.md`、`*-review-registry-fix.md`；review 结论不替代 runtime 验收。修复经正常 no-ff 合入 082237b、ba1024b，两次 507/507 source hash 核对记录在 `18-review-merge.md`、`18-registry-merge.md`。本次仅闭合本票文档，按 development §4.1 复用未变化的受测源码/测试/配置并补做 `make check-docs`；父 spec 字节及 ready-for-agent 状态、CLAUDE.md/Makefile/docs/development.md 三份原用户改动保持，不新增业务、schema 或 migration。

7. 本票未接管前序实现遗漏。必要 residual cleanup、原取消失败和最窄修复见 `ticket-18.md`、`18-final-source-audit.md`。原公开取消测试在冻结基线真实 RED 8/20，未修改的原测试修复后 GREEN 20/20；问题是已取消 context 的 BeginTx 自动 rollback 影响 pinned Conn 可用性，fresh-owner 原 SUBMITTED/READY/epoch0 责任仍在。guard 仅在原锁内 BeginTx 前拒绝已取消入口，保留 DEPENDENCY_UNAVAILABLE/NOT_SUBMITTED；不承诺 in-flight cancellation，不增加 reconnect/retry/context replacement 或持久语义。

第一次完整检查仍为真实失败：`final-attempt-01/` 保存 HEAD `082237bab03f73a4c25a44f026dac8a8d286d646` / tree `cb3e2d6118b0aea85efb3244cb2ba48ae8353096` 的完整 source/result/log/checksum，exit 2，4919.016 秒，same inputs true。普通检查通过，但 whole fault/matrix 因 `TestEveryPersistencePointIsRegistered` 失败，未到达 rules/gencheck；不可回填为 PASS。动态 `delivery.point` 未被严格 registry 识别，completion/task-closure 两个注册点缺静态 Transaction 匹配。`18-registry-repair.md` 保存真实 RED 两项→GREEN 2/2、三类 required-source rollback/public proof 和真实 12 个 ACK before/after/lost-receipt fault 用例；修复仅将三个原 literal Transaction 共用一个 ACK callback，checker/registry/hooks 不变。切片只证明其选择范围，第二轮完整检查另行通过。05/07/10 早期 stopped/incomplete、exit 143 记录继续保持未通过。

收尾的实际 docs-only commit、源码等价、protected、文档检查与 index/status 证据另存 `/tmp/lerna-module-interfaces-implementation/18-final-resolution.md`。本 feature 无 map 文件，无额外指针可同步；父 spec 不关闭，未 stage 用户三文件，未 push 或创建 PR。
