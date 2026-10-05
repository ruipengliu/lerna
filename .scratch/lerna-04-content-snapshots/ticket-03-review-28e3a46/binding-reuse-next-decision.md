# 04-03：Binding memo 后的新阶段失败与唯一下一测量

## 决定

固定产品 `28e3a468c86d4323d3b497836de23bb311a58952`。**本轮尚不批准第二项产品优化；先做一次当前产品、原完整 B、原有限边界的低开销 Go CPU sampling。** 保留当前五文件 coarse phase overlay，仅为原 Decision.Step 标记两个实际 case 的 scope/phase，使采样能区别 worker 与编译/装配。不要重用旧九文件 overlay、不要重复诊断矩阵，也不再不加新信息地重跑原失败。

理由：新的真实数据足以确认 Current 的单次成功表示复用没有保证全链 race 5s；足以把优先调查点放在当前 Content full closure 的计算/SQL路径；**尚不足以在同Tx事实复用、身份编码复用、JSON解码或SQL机制间选出有实测依据的下一等价产品改动**。一次有 scope 标签的 CPU 栈采样回答目前缺的最小问题：worker 消耗的 Go CPU 究竟落在哪个具体重复计算上，还是大量 wall time 无法由 Go CPU 栈解释。后一结果仍叫 unknown/off-CPU，不自动等于 PG锁/网络。

本报告纯只读/static；05 当前独占 LOCAL 的正常控制不受影响。任何诊断构建/执行仍须原 owner 在 root 独占授权后做。未实施、未 native、未读03另一轴结论。

## 1. 本轮完整原始证据

已 FULL 读 `/workspace/lerna-content-03-137311247276/context-final-capacity-b-binding-reuse-race.log` **160行**，并全文读 `large-graph-binding-reuse-results.md`。实际计算 raw SHA256 `d27d6966cbcf63d9348b80d0ef216c3a2dbdebf34fc0a8a91de90b97136ee0e7`，结果稿记26367B。race native3274419/start13677185/session54896，实际exit1/groupAbsent、noTimeout、已release；没有 DATA RACE 报告，失败是原有限 worker 执行未完成。原五文件manifest `f98350889216691304ef107a1a9f25fce25c5d9bb987ac61c3690e410c68cd04`；race binary `ad814ad72be02f423d81b8789323b653f13cda1c694a82e817f34f8220584260`。旧诊断不覆盖这些新事实。

### closure64

真实 normal分支 FAIL29.24s；Claim DB02:49:17.724990Z，原lease02:49:22.724990Z；running/start1 COMMIT正向确认。Step5.073903s，结束时caller剩.811975s；最后ValidateClaim实钟晚16.702ms。首实际依赖错误是29.171088833s的Content.ReadForProcessing deadline，caller剩.828880s。Snapshot1.409594s/Lock2.038247s完成，Material13调用、12成功，Current14调用。.363620s的Current与4.380435s的Content处理是inclusive，不能连同Snapshot/Lock/Material再相加。

**没有Prepared或worker Publisher。不能据本coarse日志确定失败Content调用到达preTx/Objects/postTx哪一步；旧subcost的pre.CheckPolicy定位只属于旧样本。** 也不能把最后失败的4.7ms调用当此前全部耗时的根因。

### static62

真实 FAIL22.69s；Claim DB02:49:54.467283Z，原lease02:49:59.467283Z。running/start1正向COMMIT；Snapshot1.068449s、Lock1.244193s、全部62 Material（inclusive2.154960s）成功，Plan2。**首次实际Prepared COMMIT发生于22.319658377s**；这一事实不同于此前所有未Prepared失败，不能抹去。

之后首次Publisher.Publish内部Access.PrepareContent于22.618722623s报pgconn timeout，caller余7.381271s；Publish本次225.060ms，原61120B/63source artifact。没有ReadPublished、第二Publish、completed COMMIT；这也没有正向Content.Put接纳证据。最后Claim DB晚28.744ms，Step5.079594s。最后成功pool gate只剩248.472ms。不能把超时落点命名成“InstallPolicy自身特别慢”或认定优化这3.37ms就足够，其大部分原lease已在前面消耗。

### 其它和 normal 资格

同一次race的closure65预期overflow、Content正常/提前拒绝、duplicate selector拒绝均实际PASS，不能因两个失败丢掉它们。观察buffer120/15/290、drop0。

owner结果稿及root资格记同源码独立normal exact4全PASS：closure64 Step3.708556s、static62 Step3.044059s，均原running/start1、Prepared、两Publish/ReadPublished和completed COMMIT，末Claim余1.333787/1.998534s。normal binary `da82f3dd832d15dcc6691683bfee80f07e73078eb883f17040c95d80289d5740`。本报告不声称亲自运行或另审normal raw；正常通过保留它的正常范围，不能线性推出race成本/5s余量。

## 2. 当前源结构核对与不能直接采纳的捷径

实际读固定objects：`context_binding_decode.go`、`context_dispatch.go`的binding/current/StagePublication/PrepareContent、`domain/content/closure.go`与`processing.go`、PG policy/facts中的CheckPolicy/LockVersion，以及adapter Publisher。

- Memo现在是scope+inputID+column/value+fresh full bytes准确命中、typed深拷贝。binding仍先真实FOR UPDATE取整行，再新读严格current Input、完整DeepEqual/running、Permission和trusted clocks。当前日志未给typed clone、current Input decode各自新成本；不能凭旧1.3s Binding成本再次假设当前memo没生效或再扩成授权cache。
- Content每次真实ReadForProcessing仍先目标policy/锁version/新clock，完整祖先遍历，再finalclock；Tx外真实Objects.Read+hash/length，之后独立第二Tx全部重核。每个唯一祖先Now→CheckPolicy→LockVersion→Now、fullRef/currentcutoff、完整子图及确定性排序。遍历内部已有refs去重；并不是每条重复边都重复锁/政策读取。
- CheckPolicy当前已用MATERIALIZED锁行后clock；LockVersion先advisory再真实FOR UPDATE+body解析与列一致性。未测当前CPU栈前，不能任意合并这几个query、删now、跨Tx复用已锁记录，或宣称SQL往返是剩余全部代价。
- Publisher当前原StagePublication先保存准确request，再PrepareContent真实InstallPolicy，之后才Content.Put。Prepared已存在不授予跳过任何新的current/publication/save资格，也不允许把fixture policy安装挪进setup以人为抬高本次lease内余量。

所以本轮不采用：新的current Input memo、Content body/policy/revision缓存、跨read或跨Tx closure缓存、跳过full refs/ancestors/双动作、取消post-I/O gate、batch锁提前、并行材料读取、schema/index/migration、缩材料图、重置caller/setup、heartbeat/renew/第二rule start/费用reset。单Tx内纯身份转换复用在有当前CPU栈支持后可以是候选，但此刻仍不是授权实现项。

## 3. 唯一必要测量——原 B 的 worker CPU 栈

目标只回答“当前 worker 的主要可归因 Go CPU 栈在哪里”，不是再次铺开所有端口/阶段计时。采用Go标准CPU采样，保留原五文件低开销日志；不叠加旧九文件的逐SQL/闭包 timing，不加trace/block/mutex/heap profiling，不开pprof HTTP服务。

具体默认：

1. 从固定28e产品与当前五文件overlay生成**新的独立诊断目录/manifest/binary**；旧manifest、mapping、rawlog、binary不覆盖。检查map里没有旧 `context_dispatch.go`/memo/budget-peer替换把28e退回。最多仅在现测试/phase helper给两实际`Decision.Step`调用加`runtime/pprof.Do`诊断标签（case=closure64/static62，phase=decision_step）。原ctx向下传递，只新增profiling labels；不改变deadline、不以label携带业务资格、不包新的timeout/retry。
2. 一次标准CPU profile覆盖原完整B binary执行，默认采样率，不提高频率。通过标准test binary profile选项或等价一次有严格Stop/输出关闭的host helper启停；profile file是本次预注册自有诊断产物，路径有限、原有文件不覆盖。test filtering仍同四exact tests；不要为了profile只跑某个易过子例、改材料/原预算或串行化/并行化产品。
3. 原30/5/120全部不变，原ClaimDBNow/lease、首次error、Prepared/Completed真实COMMIT、material/Publisher阶段日志继续原样。采样成本非零，失败可能更早/阶段不同；新样本只解释自身栈，不倒写旧失败阶段。不得把profile结束/写文件耗时加成产品额外宽限。
4. 输出一个有界结果表：各标签原Step wall、采样总CPU、Content processing/registeredClosure+sortRefs/VersionIdentity/contract.Encode与validation/PG policy+record decode/current Input decode/typed clone/runtime race+GC等主要**实际出现**的栈，flat和cum分开，重叠不能相加。该列表是分析分类，不是预言它们必为热点。保留unlabelled/unattributable量，不能硬分摊GC或异步pgconn栈到某个调用。
5. 若有清楚占比和源码对应的纯重复CPU热点，下一步选择一个范围受限的等价改动，并保原故障/错误顺序。若CPU样本不足或大部分wall不在Go CPU内，明确一次测量仍不足；不能将wall-CPU直接称SQL/锁/网络、更不能凭profile没出现就删除步骤。再报告最窄剩余证据缺口，不自动开后续矩阵或连续原样重跑。

该一次测量不负责证明Content失败究竟pre/post哪一gate，已有coarse定位不足继续如实保留；若之后性能修正需要锁等待或某SQL结果的细节，必须针对那个实际候选另限定证明。无需为补齐所有未知现在扩大到九文件全subcost。

## 4. 实际 red / Prepared 恢复资格与修正后的门

当前race是完整原B的真实性能/完成性red，normal是同源码真实正常对照。它还不是未选定某个新优化的因果A/B证明，也不是新memory/module correctness bug证据；不为了TDD名义制造无关业务红。

static62已有Prepared，原Decision仍有固定publication责任；失败不退回noPrepared，也不refund/reset ruleStarts/cost。后续若执行**另一个恢复tracer**，必须先reopen/read原Prepared，沿原key/bytes/ref继续原发布身份、当前权限与有限原预算；这不得替代本case一次原Step的正常容量oracle，也不得盲重算/换新key。closure64则无Prepared，不得假冒能复用原output。此决定没有授权现在重跑任一失败scope或清理其未知效果。

下一项真正产品修改在root读测量并选择后，由solefixer完成；受影响真实资格至少保持：当前Input/control变化、target和完整祖先read/process拒绝、锁等待到期、post-I/O撤权与准确fullRef优先级；若改pure表示/identity还需nil/空、错误、字段变更、别名和并发隔离。原完整B normal/race仍是最终同界验证，不能用小图/规则0额外读取/拆阶段新ctx替代。

无新整体架构结论、无七AC接受/whole04退出、无CI重用宣称；所有旧B失败和原并发纠正历史保持不变。
