# 治理实现及装配边界

本包实现 [安全](../../docs/architecture/security/README.md)、[扩展](../../docs/architecture/extensions/README.md)和[证据与评测](../../docs/architecture/evaluation/README.md)的持久领域裁决。公开方法使用 `api.Contract[I,O]` 闭合合同，全部事实保存到宿主显式声明的 `governance` 事务参与者中。进程缓存、模型输出和 Renderer 参数都不是权限权威。

`New(store, Options)` 由宿主装配。`Options.Participants` 必须列出实际共同数据库的 Content、当前凭据、TaskGate 等参与者；不能由 Go import 关系推断共享事务。Content、真实用量源、实例宿主和独立 runner 的 I/O 在事务之外执行。证明签名与验证只使用本地登记密钥。

## 权限与可信确认

`grant.issue`、`grant.revoke`、`policy.acceptance.create`、`release.approval.create` 先接纳原命令，再由准确本人会话确认。`confirmation.read` 返回原完整闭合命令及其 JCS 摘要，Renderer 必须准确展示完整输入和全部预览字节。批准回执只记录决定；原业务消费确认、当前预览披露门禁与业务写入共同提交后，原命令才 applied。`PreviewGate` 只证明当前允许披露，不声称本人已阅读。

Use 的原 ID、请求摘要、准确目标与有限窗口不变。父许可链按统一顺序锁当前许可及账本，任一父撤回、到期、once 已消费或累计金额不足都会阻断子使用。结算只归并核验过的原计费源修订；实际超额和迟到费用保留。数额预留可在确认关闭且费用 final 后释放，once 永不复活。派生许可当前只支持同 owner、同 subject 的交集。

Estimate acceptance 必须预览准确范围和非硬上限解释，逐计费准入同库核当前 subject/policy/Task/能力/数额范围；跨 owner、allocation 和离线仍只支持 strict。

离线租约必须具备 `Proof`、宿主固定 `EndpointID` / `InstanceID` 与 `OfflineGate`。设备导入核验原云签名、有限 allocation、准确设备实例；新实例不能继承旧租约。设备每次使用只核本库已知撤权和原准入/控制/资源代次，取最紧截止，不在 Tx 内 RPC 猜测当前全局状态。设备 ledger 完整核验后才可向原云账本结算释放数额。

跨域使用证明分别以 `UseReceiptDigest`、`ApprovalUseDigest`、`EligibilityReceiptDigest`、`EvidenceChangesDigest` 绑定完整闭合事实，排除签名字段。`RequestDigest` 另保存原意图，不能代替输出事实的签名。Proof 端口必须固定 kid/ES256、tenant、issuer、audience、purpose、准确 object/digest 和原有限窗口；禁止从正文采信公钥或 URL。

## 当前证据与持续缺陷

`RegisterRuleTx` / `RegisterCheckTx` 登记准确不可变副本。`CheckEvidenceTx` 遍历全部依赖，限制 128 个检查、深度 8；完整 gate 集合先锁，随后登记 authority head 和 holder。Task 完成必须在相同 Tx 将实际 `ResultRef` 传入回绑原 holder；后续缺陷保留成功 Result notice，不改写原 Result。

远端 eligibility 包含完整同 authority 依赖绑定、holder/epoch/cursor 和有限有效期。changes 页包含准确起止 cursor、整页摘要及原签名，连续导入后才能 ack。重复页幂等，缺口保留关闭门禁。受信传输收到 `authority_changed` / `snapshot_required` 后，宿主以 `MarkEvidenceGapTx` 持久关闭旧基线；重读原 receipt 不能续期或清除缺口。高影响规则缺独立 `CalibrationGate` 时拒绝，即使输入声称 `Calibrated=true`。

## 发布和评测

Target 的稳定 BindingHead 先登记。prepare 不发布 current/ready；activate 外部初始化与独立 self-test 后，再以当前 head CAS、批准、格式及实例代次共同发布。A 的迟到回调只收尾 A，不覆盖 B。deactivate/reopen/stop 绑定准确实例；停止、效果关闭、费用和物理销毁分别记录。分批扩张要求当前开放批次每个准确实例的真实观察；回退使用旧 InstallLock 自己的独立当前批准。

`Lifecycle` 缺失时报告具体不可用。实际宿主必须验证 allowlist 或真实隔离、准确 artifact/config/profile、cancel 后实际退出及 residual；调用方的 `TrustedBuiltin` 和布尔自述不能代替这些证明。本包没有声明任意不可信原生扩展可运行。

Plan 冻结完整 manifest、样本数、阈值、预算与 cutoff。Formal 还需要独立 `FormalPlanGate` 核验准确数据谱系、holdout partition、candidate 谱系和冻结政策；缺失时在创建责任前拒绝。正式次数、release request 与 holdout 占用永久保留，取消不返还。当前只支持本 owner partition。

Runner 必须在候选不能写的真值边界中预检两臂、按原环境键执行/查询、核真正结果并观察停止。它还必须执行全 run 的真实资源/计费预算，不能把每臂收到的 Plan budget 当成可重复的新额度。缺少 `Runner` 时明确 blocked/not_run；本包没有生产供应商、任意任务数据集或不可信代码隔离的替代实现。

每个 sample-arm 唯一，未知启动查原 attempt，不另发物理运行。取消分页覆盖未启动行，原分母不缩小。cutoff 后封存不可改写报告；迟到事实继续归并原 SampleRun，若推翻原结论，同事务失效资格及相关发布依据。报告与逐样本反馈在实际披露前耐久登记 Exposure；封存前暴露使正式资格失效，正常封存后反馈保留原报告自身资格。目标达成、统计门槛和实用改善分别报告。

## 验证范围

`go test ./internal/governance` 使用真实 SQLite 文件。设置 `HARNESS_GOVERNANCE_POSTGRES_DSN` 后同套件改用真实 PostgreSQL；未配置不会声称 PG 通过。`go test -race ./internal/governance` 验并发门禁和过期回调。

夹具明确预置当前用户/会话、准确预览来源、正式数据绑定和计费/实例外部边界；不以 mock 数据库证明事务。真实文件夹具验证 A/B 回调竞争、完整当前批次、独立批准回退、原设备租约及 ES256 篡改拒绝；12 个固定字节样本在两个独立实际目录运行并以独立真值读回，验证原统计及迟到失败。1001 个样本取消验证 2002 个 sample-arm 分母及分页恢复。它们不证明自然语言质量、1000 目标 API、生产数据治理、真实供应商计价、跨 AZ 或任意扩展隔离。
