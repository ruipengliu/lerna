# 切片03完整退出证据

2026-10-04，切片03 **completed**，A阶段01–03完整退出。六票9+7+8+6+6+6，共42/42AC resolved；原六项spec验收、完整本地库存、两轴审查、架构复核和准确push CI均完成。结论限定为规则组件与独立测试目标，不开放真实业务副作用。

## 准确源码与合同

组合业务检查源码882e97b596ac2baa034881766041944b0da2e05e；完整1.1库存/协商/共享入口受测6bf57501a5003ff139e96aec40589ad49fcba1ec；最终交付1aa21fcf47352c4460877bb51fc04dc302cdbae9仅有一行模板空白、七历史固定链接和三证据文件followup，759其他Gitentries相同。正式整合391b4d8b3f0641f04356402821b8b3b2eec82f86，parents3845110+1aa21fc，完整tree84dd598bd95bb0416e18d234061bdc6bdd0e1249与worker相同。

准确push/CI提交47ebce1237a34e7b535423c31b7a7d59e39e0caf另附六份审查/进度Markdown，产品/工具/测试/生成物仍为已核交付对象。后续退出文档不冒充新测试。

1.1.0准确支持command/command.get及decision_engine/decide、get、cancel四方法、完整双Schema摘要和隔离Go/TS入口；默认1.0冻结。协商只证明兼容，执行仍核当前主体、权限、预算与期限。

## 原六项验收

| 原spec验收 | 实际公开证据 |
| --- | --- |
| 1 固定输入、原Decision去重、提案及材料恢复 | [01](ticket-01-exit-evidence.md)decide/get双摘要/固定回执/独立出版；[06](ticket-06-api-handoff.md#最终检查与本票退出)真正SIGKILL原身份恢复；[02](ticket-02-exit-evidence.md)五V2分支原prepared/usage；[03](ticket-03-exit-evidence.md)双格式Stop和真实并发Finish。 |
| 2 含混候选/依赖行动拒绝 | [02七AC](ticket-02-exit-evidence.md#seven-acceptance-checks)15非法输出矩阵、准确binding/arguments/purpose；四独立动作正常，依赖/未来结果/第五动作/重复key拒绝，不裁决Task。 |
| 3 已写丢响应、迟到与not_found | [04](issues/04-durable-test-target.md)独立SQLite Query/Read/Observer；[05](issues/05-durable-fault-plans.md)真实DropResponse提交及耐久ReceiveOnly/Apply/cursor，迟到责任及正常对照。 |
| 4 原键窗口及保证不足 | 04原键不重做、guarantee_expired/unsupported；05到期不抹迟到，not_found不推出未发生。 |
| 5 原身份/计划跨重启 | 01/04重开，05耐久cursor；06同seed73两实际DB/native进程；02 FINAL01真六态producer；03 Source2/Decision3旧writer与nilInput墓碑双owner恢复。 |
| 6 正常对照/有限结束 | 各票公开允许对照、有限I/O与Wait/FD/nativeClose责任；新动态shared完整104=60+44，p1/count1/integration/race/120秒串行，不漏新增case。 |

逐AC、实际故障窗口、normal/拒绝、原producer和失败历史见各票及[本地整合](whole-exit-evidence.md)，不以私表/内部调用次数作业务oracle。

## 执行与独立复核

882本地makecheck47.259、baseRace53.914、完整PGnormal60.881/29.966、Sourcerace28.434、完整Recoverynormal37.201/race87.979、7manifests172项通过。原Component整包race120.073真实超时保留；当时完整102项有限分组60race110.803+42race11.781随后通过，不扩大期限。

6bf真实Gored0.035→actualgen最小green0.142，拒绝矩阵normal0.070/race1.401、TS五测试及makecheck25JS/40TS/89新158旧正反双向真实往返/build通过；222保护路径/全部payload零变。真实新shared session59859final0：Recovery79.934→动态104项60race102.204/44race11.633→Source28.271。TS自身prototype期望及审计配置错误仅属已明示的测试/工具失败。

[两轴](code-review.md)最终df2dbe5→1aa21fc完整164commits/360paths，Standards0hard/0smell、Speca0/b0/c0，各最高严重级别无。旧完整报告资格+实际Git相等先核；fresh及5增量完整读，04 Spec原截止后四路径独立补核。原[链接P2](whole-standards-initial-review.md)最终固定permalinks闭合；保留旧报告。各轴隔离另一轴专属发现。

[架构](architecture-review.md)固定热点刷新0必要新重构；A/B/C保持，E/F单一库存和有限入口成立，CurrentClosed仅未来候选。HTML实际xdg-open exit3无浏览器，未验证渲染；正文/静态解释随报告保存。

[准确CI37194868564](https://github.com/ruipengliu/lerna/actions/runs/37194868564)的head47ebce1、push event、completed/success、两job全部steps和实际日志已核：25JS/40TS/89+158往返，PG18.6工具race1.622；Recoverynormal30.182/race66.903、Componentnormal49.124和104动态60race80.165/44race10.196、Source normal16.517/race21.851。范围/cached辅助/Target独立race限制见[CI记录](ci-verification.md#完整03准确提交的最终ci2026-10-04)。

## 资源、清理及范围

[本轮audit](whole-integration-cleanup-audit.json)589ACK中414uniquePG/122SQLite/32Target/3archive/6实际ACKgroups全absent、sessions0。后续[自身清理](whole-owned-cleanup.json)逐197确切inode/hash文件unlink、3empty dirs rmdir/fsync，精确新overlaydev27/ino431270已absent；账本/inventory保留。各票audit独立限定其登记范围，不混为全环境计数。

10个完成且干净worktrees正常git remove，10分支及Git对象全部保留；[记录](worktree-cleanup.json)。fixture辅助分支17个中间patch未声称逐一等价，最终01源码已独立验收、原分支仍在。原02失名PG/CID、原03缺初始compilerACK的3540254111及其qfo overlay保留，无前缀/时间/PID猜删。

环境Go1.27.1/Node24.19/pnpm12.8.1/TS7.0.2、PG18.6、SQLite3.53.4/go-sqlite3v1.14.52、LinuxCGO/owned overlay。SIGKILL是进程故障证据，不证明掉电/跨区；规则收费及能力是夹具，尚无生产Task/Grant/Executor、Provider保证、第二业务实现、质量/容量证据。G2整体、G3和生产门槛留待后续切片。

下一实际frontier04：按最终源码复核Content-backed Snapshot具体接法，再发布六票，不改冻结1.1。
