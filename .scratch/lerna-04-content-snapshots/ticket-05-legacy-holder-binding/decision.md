# 04票05：真实停止旧 writer 后的准确 primary holder 回填

**采用本票内的窄受信回填，而不是把所有旧数据永久拒绝作为本票升级完成。** 无法证明原介质与旧 writer 停止的 scope 必须继续明确拒绝。两者不矛盾：支持一条真实、有限、可重复核验的原 scope 升级路径；不承诺任意旧部署自动迁移，也不创建生产通用停止证明或公共 API。

本次读取 WT `/tmp/lerna-worktrees/content-snapshots-05`，HEAD `e5b38a57dab458be56574237052f1ad7465bf321`；源码结论针对该 pin。依据 root AGENTS 的真实旧版本升级/在途兼容要求、04 spec AC1/7、票05 AC4/5/7、ADR0007/0009、当前 `ticket-05-api-handoff.md:64–81` 的原 binding 决定及 `/tmp/lerna-05-remaining-obligations-after-holder-facts.md` 的 legacy 义务。当前没有本条 legacy native tracer；下述是静态方案与待执行验收，不是 red/green。

## 1. 实际门禁正确，缺少受信升级入口

`0003_body_cleanup.sql` 将旧 primary_holder_binding 初始化为空，明确 unknown，不能自动填当前 Objects.Binding。`local/store_linux.go:Open` 从实际已打开目录 FD 的 Stat 固定 dev/inode；字节相等、路径相同均不能替代原介质身份。

`service.go` 的 Get/出版入口和 Finish、`lifecycle.go:sealLocked`、secondary/实际擦除均拒绝空或不匹配 binding。PG `facts.go:LockVersion` 核 JSON 与独立 binding 列一致；普通 SaveVersion 要求旧新 binding 相等。这些保护继续保留，不能为升级放宽为“空则接受当前值”。Command 原固定 receipt/history 在当前 reader 资格下仍可观察；正文不可读须准确 unavailable，不冒充 gone、未存在或历史未出版。

旧 `NewLegacy` 会恢复旧 dump 并把原字节复制到新的目录。它可以证明旧逻辑事实兼容，**不能证明新目录就是原 holder**；不能顺手给该 helper 中所有 restored 记录填当前 binding。原旧02三个 scope、任何 Close 未确认资源均不在本次可采用或清理集合。

## 2. 升级的现实信任边界

新建且预登记的独立 scope，由受信 fixture/host 完整负责。至少持久记录 scope ID、准确数据库 namespace/owner、完整 frozen writer SHA、原对象目录身份、完整 Ref/保存 Subject/Purpose/ObjectKey、原 Command/receipt 与状态、原 attempt 的真实有限身份集合。证据具有固定摘要，重新运行不能用同 ID 替换内容。

在旧 producer 获准产生效果前，监督方先登记实际 PID/PGID/starttime 和 root dev/inode，并通过现有启动/effect gate ACK 放行。不得把“PID 在 Start 前已知道”写成不可能保证；必须是资源先登记、实际子进程身份确认后、业务效果前。需要验证旧 producer 打开的确是该受信 owned volume，整个交接期间目录不替换、不删后重建；保留拥有关系及准确目录身份观察，不能只在新进程启动时重新读路径。

旧产品仍用冻结 `1a7d910238eb74cddc712d92b0ba4014a72ff507` 的真实 0001 源与校验和。可新增**独立测试 producer/监督协议**来收集生命周期证据，不修改其产品包、迁移或既有归档 SHA。显式获知所有旧 writer 的 Store/Object Close 结果，以及 producer 的 actual Wait/PGID absence；控制/ACK 文件等资源也按实际归属关闭。仅 Wait/group absent 不补发旧 Close ACK，任一 unknown 保留 scope 且本轮不回填。旧程序以后不得重新获准运行；旧代码不遵新文件 fence，DB Claim、租约和新 marker 均不能证明它已停止。

受信升级消费者由宿主注入一份**准确 scope 的不可变交接依据**，包含原 binding/证据摘要及有限当前升级截止。它不能接受公共 payload 的 `writers_stopped=true`。domain 不自行扫描 /proc 或判生产停止证据；本机监督方的真实判断和证据来自上述实际控制，这就是本片有限开发资格，不冒充多机 fencing。

## 3. 最小内部业务 seam 与持久 CAS

建议新增 Lifecycle 内部受信 `BindLegacyPrimary`（命名可按代码调整），显式接收准确 Ref/Purpose 与宿主已核准的原 scope 交接依据。资格对象只用于此实际 legacy 消费者，不扩普通 Service.Put/Get/Seal 参数，也不建 registry。完整 scope 依据由 host 固定注入，调用请求只能选择其中的原准确版本，不能自行指定任意新 root 或声明已停止。

步骤：

1. 独立短 Tx 读取原 Record，核当前受信管理 subject/owner、准确完整 Ref/原保存主体用途、ValidateIdentity、binding 为空、无 seal/gone，以及原 scope 清单确有该版本。保持原所有业务字段；收集精确 expected Record 和 staging 状态。
2. Tx 外通过**原 owned volume**真实有限读取原 ObjectKey，并独立核 hash/length 与原观察；同时确认实际打开对象 Binding 等于交接前固定的原 root 身份。正确字节存在于复制 root 仍拒绝。读取的内部维护权限来自准确升级资格，不伪造普通用户 read/disclose 许可；不得输出正文。文件读/关闭错误或介质未知均不回填。
3. 再短 Tx 锁同准确 Record，核完整 expected 未变、当前管理及原升级截止，调用专用 PG `BindLegacyPrimary` CAS：仅允许空→准确原 binding；同时更新 JSON 与独立列、递增内部 Revision，并持久绑定此次固定资格 ID/证据摘要。可用 Record 的窄内部 JSON 字段保存资格出处，无需新增 SQL 表/公共合同。若无需独立资格列，现有 0003 已足够；0001/0002 和已实际应用的 0003 都不改 checksum。
4. 同 Tx 登记本次已实际核准的旧 publication attempts，最后再取 DB fresh Now 核资格截止。普通 SaveVersion 的 immutable-binding 条件不放宽；专用 CAS 校验原 body/完整身份/revision/staging 等 expected，失配回滚。锁序仍 Version→attempt/附属事实，无 Tx 内文件 I/O。交接期间宿主禁止旧 writer 重启；新 writer 对未绑定记录仍被原门禁挡住。

同资格、同原 binding 重放必须幂等观察，不再次递增历史或重设 deadline；已有非空且不同 binding/资格是拒绝，不是“迁移到新目录”。commit_unknown 后只查询/重放原准确资格；不可重新 Put、换 Command、换 root 试成功。受信可观察返回准确 Ref/binding/资格 ID/状态，不返回正文，也不以私表读取充当业务 oracle。

## 4. 原 attempt 与未支持状态的边界

不能只绑定 final body 而漏掉旧 `.tmp` 责任。当前 `PublicationAttempts` 只遍历0003登记表；旧表为空不证明旧 writer 没写临时文件。回填前须从**该 frozen writer 的实际路径与原有限记录/监督观察**明确所有可能承担责任的准确 attempt key，并用现有 SavePublicationAttempt 同 Tx 登记原 binding；不得靠目录全扫、名字猜测或当前 MaxAttempts 重新推演新身份。若不能完整核定，保持 unbound/unknown，不能出具全holder ACK。

首个正常链可明确支持已 published 且原 final bytes/完整有限 attempts 均可独立核准的旧记录；无正文的 failed/preparing、未知 attempt 或不完整原 volume 资格不自动套用“发布正常”证明。它们保持原 failed/preparing/receipt/staging/残留，明确拒绝回填或另以准确保存介质与原 attempt 责任完成独立 tracer。不要把 failed 无 final file 当原 scope 已无效果。这个有限支持声明不得写成全部在途旧状态已经兼容。

绑定只识别旧责任位置，不新发出版 Job、不改 Publication/Failure、Command receipt、来源、原 caps、Attempts、AttemptKey 或 cleanup Deadline。记录 Revision 与新增迁移出处是内部实际变更，不把历史业务内容整体重生成。若旧维护窗口已经过去，回填也不续期；之后发物理清理必须有既有合法原窗口，或另一个显式授权且准确关联的新有限责任，不能借升级隐式刷新旧预算。

## 5. 必要 native 验收方案（待 sole owner 授权槽后执行）

- **真实旧正常前态**：在新独立 namespace/root，使用冻结0001产品写出准确 Content，保存原回执/出版历史与独立字节。至少原 published 与一个独立正常版本用于误删对照；需要 failed/staging兼容时另保真实该前态。预效果登记和停止资格按§2完整记录，所有原 scope 从未借用旧未知资源。
- **真实升级拒绝**：同原scope应用未改0002/0003；当前 Get/Seal 对空 binding 明确不可用/ErrHolderBinding，原 Command/history仍准确。空root、copied-correct-bytes root、错 ref/保存主体/用途或不完整停止依据分别拒绝，且原 bytes/receipt不变。缺停止资格的测试在 effect gate 持有旧 producer 时拒绝，随后正常释放并完整 Close/Wait；不让未停旧 writer 与新擦除并跑来“证明安全”。
- **原scope正常回填与重开**：准确原介质和原证据经 seam 一次绑定，关闭/重开当前 owner 后，在真实当前五动作/来源资格下读回原字节；原 Command固定回执/Publication/来源/原 cap与期限完全保持。同资格重放不新工作，不同root仍拒绝。
- **真实新协议清理**：对已核准的原版本按既有有限 lifecycle 路径 seal，真实 staging/primary/原 attempts 独立确认后 gone/全部holder状态准确，重开保持；独立版本继续可读，旧原 receipt/history不改变。测试时间输入须从开始就给足准确有限窗口，不能为升级续老 deadline。
- **准确失败恢复**：专用 CAS 的实际失配/回填回执丢失后原资格重开观察；并行新状态或时钟跨界导致整 Tx回滚。机制注入只标对应机制，不冒充旧 native Close 故障。未确认 scope 原样保留；没有原物理 erase/ACK 就不清理其测试 root/schema。

上述是本票必要的最小受信旧源升级资格；尚未执行。生产跨机停止、任意导入副本、旧未知 scope回收、自动迁移所有历史状态均不因此获得支持。若实际交接条件不足，明确拒绝该 scope 是正确安全结果，但不能用它替代这条已经明确要求的正常升级对照，或宣布票05/whole04完成。无需新领域 ADR。
