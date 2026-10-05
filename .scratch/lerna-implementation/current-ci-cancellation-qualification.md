# Run cancellation：本地资格与交付边界

固定资格源码3570180514ce0a6cee4daa27430540003574533d，独立review基线75f0429d709d95b6a63137c397fcd9d80203ed5f。产品SHA256为65e16f218a9fda70240a8e01b42ee7b85404c586214fb4089ddcd129ec223a60；Host两测试6e6/e3a，真实PG夹具fcfe。各阶段22源码pin前后一致；最后安装/规范检查另保存8个锁定入口pin。

修复归属切片02的Run/pool生命周期，测试沿用切片03有限故障规则。Run保留原caller context，内部取消后join三个lane，errors.Join全部实际lane原因，仅原caller.Err非nil时加入真实caller原因。driver、CommitUnknown及合法sibling cancellation继续可由errors.Is判断。原期限、Claim、配额、SQL、迁移和合同/profile沿用。

| 行为 | 实际结果 |
| --- | --- |
| `TestPoolRunCallerCancellationRetainsEveryIndependentLaneCause` | 原0f28产品RED exit1：三entry/join后仅注入BadConn保留。修复后同例normal0.009s/race1.051s：真实caller取消及四个独立诊断原因全部保留。 |
| `TestPoolRunFirstFaultKeepsCallerLiveAndRetainsJoinedDiagnostics` | normal0.008s/race1.056s：原caller.Err仍nil，实际内部停止、三entry join，独立非context原因保留。 |
| `TestPGPoolRunBlockedSQLNormalRelease` | 首1027夹具exit1/package0.255s：三个真实SQL waits后opaqueDSN引起scope配置拒绝，公开尾未完成。忠实修正fcfe后normal0.505s/race1.869s，公开尾完整。 |
| `TestPGPoolRunBlockedSQLCallerCancellation` | 首夹具失败后未运行。修正后normal0.562s/race1.740s：原SQL等待、实际取消/原因/join及公开尾完整。 |

机械BadConn/CommitUnknown明确是注入诊断，首RED不声称重现原CI未知native故障语句。采用851a9e8忠实夹具决定：同一parseableDSN Store/Repository/Clock/Runner/worker，三条有限ordinary准备事务建立原连接身份，没有额外LockPool预运行。角色intent在Open前登记，实际PID/backend_start/database/user/tag在启动后、Run前登记；原SQL等待绑定这些身份。这不是首次SQL前或终身全部连接census。

真实PG两路径的N/R均完成原control/reconciliation/ordinary projections、ordinary active reservation、后续原Claim Complete及三个固定receipt replay。原caller15s包含全部准备；Tx3/statement2/lock1、default16/idle4、lease1min/fallback10ms、cleanupRun1s、fixture holder join11s、Go/outer120均不变。

原九个受影响selector各独立normal→race，共18项实际通过：

| Selector | Normal | Race |
| --- | --- | --- |
| `TestPGPoolRunReservedLaneProgress` | 0.471s | 1.750s |
| `TestSQLitePoolRunReservedLaneProgress` | 0.105s | 1.333s |
| `TestPGPoolRunPreservesRetryWindow` | 0.332s | 1.785s |
| `TestSQLitePoolRunPreservesRetryWindow` | 0.077s | 1.360s |
| `TestPGPoolRunCurrentRevisionDeadlineAndZeroQuota` | 0.392s | 1.923s |
| `TestSQLitePoolRunCurrentRevisionDeadlineAndZeroQuota` | 0.094s | 1.396s |
| `TestPGPoolWakeKeepsEarlierClaimedDeadline` | 0.220s | 1.478s |
| `TestSQLitePoolWakeKeepsEarlierClaimedDeadline` | 0.076s | 1.355s |
| `TestPGPoolLockedPagePreservesFairHeadAndMaintenanceCursor` | 1.860s | 3.992s |

各项原argv、caller/policy/quota/lock/lease窗口、readonly/p1/count1/Go及outer120不变；每项实际Wait及当前group absence确认后才运行下一项，无重试。[18项summary](current-ci-cancellation-evidence/execution/affected18-summary.json)保存每项raw/source-pre/post/outcome及原owned区间。

最后一次离线frozen/ignore-scripts安装成功，downloaded0且锁文件不变；一次原make check GOFLAGS="-mod=readonly -p=1"实际通过格式、vet、类型、生成一致性、Go普通模块测试、TS、合同及build；一次完整非integration go test -race -v -mod=readonly -p=1 -count=1 -timeout=120s ./...实际通过474 RUN/PASS，包含两条Host机械测试。integration资格来自前述18项及新PG N/R。[三项summary](current-ci-cancellation-evidence/execution/final-three-checks-summary.json)保存原raw/退出与源码证据。

[Standards](current-ci-cancellation-evidence/review/lerna-ci-cancellation-standards-review.md)和[Spec](current-ci-cancellation-evidence/review/lerna-ci-cancellation-spec-review.md)固定75...357，必要代码findings为0；报告当时的资格pending由后续实际证据补充，原报告未改写。

[单次只读观察](current-ci-cancellation-evidence/execution/final-owned-readonly-audit-v3-summary.json)实际通过：15登记schema、26登记backend PID、133登记path当前均缺失，有限登记PID/PGID当前无members。observer协议PID635928在首SQL前fsync登记，backend_start在SQL后登记；其原firstClose与rollback均成功，native3863590实际Wait/group absence/noTimeout。各阶段均STOP/RELEASE，观察未清理资源。

原504-line census、实际509登记和510含Release快照分别保留。61条path有原dev/ino；37个native登记有starttick，76 compiler及12producer没有原tick，不补代际身份。3837808原confirmed=false仍sticky；3837792原confirmed=true缺start ACK仍是unbound PID-only责任。first1027六个protocol-only backend及全部历史UNKNOWN继续保留。当前absence、nativeWait、终端removed receipt均不补原logicalClose。

[归档](current-ci-cancellation-evidence/archive-map.json)保存309个原字节文件，原165份及100份增量不变；Go副本用.go.txt，原失败、旧STATIC版本、raw路径与manifest不重写。issue仍claimed：最终primary merge/push及新CI待完成，最后项不resolve。见[架构评估](current-ci-cancellation-architecture-assessment.md)和[退出说明](current-ci-cancellation-exit-evidence.md)。
