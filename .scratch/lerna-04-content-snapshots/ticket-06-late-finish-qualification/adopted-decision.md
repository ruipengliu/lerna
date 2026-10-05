# 04票06：已安装后封存、同原live worker Finish——静态决定

**采用一个下一纵向场景：原真实local.Put已完成安装 → 原父句柄真实SealOrphan/全部原holder ACK → 释放同一存活子进程完成原publication Claim。** 只追加consumer测试及其观察，不修改producer/gate、产品、迁移或旧预算。先normal、再该顺序、再配对race，各次实际失败即停止。此处没有执行、业务RED或green，也不接受票06/whole04。

## 固定来源

完整读取计划 `/tmp/lerna-04-ticket06-latewriter-static/plan.md`，SHA256 `8f13c6221b0566700c4885373e1ab3901722cc8eb7d7e9c4c157f440ebe2b20f`，及commands、actual-port-pins、current26-verification。读取实际相关产品和原consumer路径；独立核对actual-port-pins全部12文件长度/SHA均匹配。基点 `d9dce0c3840f558c544750c92bfba2b6329ad8af`，原26manifest `0f42b6aa02006f6b74fd294fd934261466472a52e0e07d3a09b65ba810132e3d`；原consumer47305B/SHA `ccc880a2bde4ade261ade2dca3db5fffab74e2679eff0889ccbfb0e811a7a2cf`。26清单的其余项在本轮为已提供资格，未冒称再次逐项核对。

实际源码：`conformance/component/content_process_recovery_test.go:170` 先真实local.Put返回nil，才Emit/Receive原durable gate；`:394` 原Service.Step；`:451` 实际CloseACK/Wait/Stop。`domain/content/service.go:804` 原Step、`:906`后的最终版本锁/可信Clock/ValidateClaim、`:979` BodySeal拒绝；`domain/content/lifecycle.go:142`原版本锁/原保存主体、`:175` sealLocked、`:313` Step、`:542` completeCleanup；`adapters/objectstore/local/store_linux.go:245`原Put与`erasure_linux.go:174`同key真实flock和耐久seal。

## 一项必要观察修正

计划中的“底层真实Put已经成功”有充分gate来源：local.Put在进入gate前已完成bytes/hash/readback/Sync/第一Close及flock释放。**但现有closed帧只报告Service.Step，不报告gate Receive的ioErr。** Receive若在原I/O4秒到期，Service仍可能以BodySeal优先收口failed/forbidden并返回nil；Claim5秒尚有效也不能排除该情况。不要把closed(nil)单独写成“包装Put成功返回nil”，也不要捏造ErrClaim。

推荐不改helper的最小防误资格：在调用现有 `startContentPublicationChild` **之前**记录父单调时刻t0（早于真正run即可，不须改helper）；固定观察截止Q=t0+4秒。收到真实gate后要求原caller期限覆盖Q、`gate.LeaseUntil > Q`。由于原Claim的LeaseUntil≤原policy IO截止，而工作/I/O上下文创建在t0以后，且child20/caller30均未提前取消，原I/O实际截止不早于Q。要求发送正确原release、收到真实closed帧并实际Wait成功均发生在Q之前；随后仍实际Stop/物理pipe关闭。该要求只收紧这次场景资格，不给任何工作新期限、不替换原ctx、不缩改产品WorkTimeout。对比时间用父同一单调时钟；真实LeaseUntil只用于已存在的绝对下界，不反推可写授权。

这排除本次已知“gate耗尽4秒却被Step业务收口遮盖”的成功误判；仍不将包装Receive.nil当单独实测事实。准确结论是：**真实底层Put成功安装，原live worker在原期限内被释放并完成原Claim，但最终public publication因既存BodySeal未成立。** 若该保守窗口不够或任何检查失败，保留原raw/原因，STOP，不去掉限定、延长I/O、重试到green或冒称产品bug。只有未来确需单独证明包装Put返回类别时再提出明确consumer witness；不是本次producer修改授权。

## 两个实际出口

1. **normal_release**：单次原caller30/初始Trust，原policy/Command reader；固定原Command/ref/receipt/bytes/Job。实际原child到gate后独立读全6字节、原key/hash/length，公开preparing。立即release，真实closed/Wait/Stop；仅此后parent Reopen。真实Get全字节published、Command原receipt与published progress均准确，禁止只靠accepted。
2. **original_fence_before_late_finish**：同样前态；child留在原gate，父保持原Store/Objects句柄，禁止live Reopen/Kill/新worker。初始固定 `SealDeadline=min(initialNow+20s, originalTrust)`，请求准确ref/purpose/sealID，原完整saving subject，PolicyChangeKey空。先公开seal和两个真实holder（pg-staging、primary），尚未全部ACK。最多四次实际Lifecycle.Step，只是有限上界；每次按实际holder_id观察，不能断言staging先或恰好四次。独立ObserveStaging、真实Objects.ObserveErasure和精确body/原attempt缺失，在release前证明两原holder erased、CleanupComplete、完整identity/fence与无residual/cursor。禁止目录数量或私表作为业务oracle。

随后释放**同PID/PGID/start、同原Store/Objects/目录FD、同Claim/epoch/原bytes**，无新claim、Deadline或Job改写。最终Service允许 `worked=true, err=nil`；必须实际闭合原Claim并公开failed，而不是测试要求Step ErrClaim。底层seal和原版本门禁共同保持BodyGone，`ioErr==nil && !BodyGone` 不复活当前ObjectHolder。原历史holder责任与当前primary事实分开；不要要求历史CleanupPending字段被全部粗清。

子进程真正关闭与join后才parent Reopen。复核同seal/fullbinding/原deadline/两holder全ACK、独立staging/primary/原attempt缺失；原Command receipt字节不变，progress保持failed与准确ref。在原Trust内一次签发准确metadata-only许可，Get为Gone且evidence_available=false；未签发时按实际权限检查拒绝，不能要求同一次权限状态同时公开Failed和Gone。此查询不披露正文/来源，也不恢复任何body权限。空PolicyChangeKey不ACK无关policy duty，不伪造secondary或所有业务责任关闭。

## 有限性与后续边界

保留原I/O4、Claim5、Work4、child20、caller30、Publish20、managementWork20、Tx3/statement2/lock1与Go/外层120，原max3/PageSize2、count1/p1/readonly不变。期限都在初始固定，不在gate后续期；无sleep/预热/重跑/借token。源码可支持该顺序，是否实际能在原窗口完成只能由后续真实执行证明。

任一first Close/Wait/pipe结果未知则新scope保持UNKNOWN并执行既有有限退出责任；body ALLACK不代替native关闭。历史8 UNKNOWN原样保留，不由新一代成功或group absence补证。既有原in-process lateFinish与local跨进程flock测试只是依据，不冒充本次live独立子进程资格。

另一个顺序“原worker尚未实际Put取得key锁 → seal/fence → 原Put真实ErrBodySealed”仍单独设计/取证，本次不追加gate或矩阵。独立secondary离线/残留、其余06验收、资源审计/评审/CI与whole04仍未由本场景覆盖。
