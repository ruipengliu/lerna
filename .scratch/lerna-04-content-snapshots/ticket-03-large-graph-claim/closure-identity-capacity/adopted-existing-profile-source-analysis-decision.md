# 04-03：当前身份复用源码的真实 race 失败后决定

## 1. 唯一下一方向

**不批准新的产品优化，也不再次运行normal/race或采CPU；只对已经存在的唯一CPU样本做一次受限离线Source调用栈展开。** 目标是从已有证据分出 ReadSnapshot/ReadFixtureLock 下纯解析/重建的实际CPU份额，与其Content调用成本区分，之后才能选一处表示层改动。这不是新增profile、业务probe或连续诊断矩阵；不访问DB，不启动业务producer。

理由具体：本次coarse trace确定Snapshot与Lock各占较大elapsed，但包含完整Content当前授权/闭包/实体读取。实际Source读取的是四个不同正文，不能将它们叫成同一正文的可删重复。代码确有候选纯表示工作，但旧七个top30视图没有提供它们的份额，不能凭约3秒/约1.7秒的inclusive时间推定memo收益。本轮不选择某个cache，也不复用旧VersionIdentity累计量宣称还能再次省掉。

本人仅STATIC读取并写此/tmp，未运行Go/native/DB/profile或改产品。离线分析也须root按既有机械运行归属安排，不由本报告声称已执行。

## 2. 新失败与准确资格

全文读160行 `context-final-capacity-b-closure-identity-race.log`，SHA256 `7b48cbb7e9e0a8900f6fa9b514791d21f35538c85d17a9fd369fbe171bfefe6b`。native3470884/start14538660/session28924，root已核actualexit1、Wait/groupAbsent、owner STOP/RELEASE；raw末尾FAIL及NATIVE_EXIT1。无DATA RACE报告/drop0不等于业务通过。仍是原五map manifest57a357/mapba168、race binary0514，不是后来七map详细出版测量。

产品仍HEAD28e加四项明确WIP：closure `854269...`、dispatcher `5c2b8d...`、public cause test `e944e3...`、cause probe `390515...`。原caller30/Claim5/Go120/outer120/count1/各初次预算未变。root说owner正在保存finaloutcome；先前registered文件是早期事实，不能据此回填原per-resource CloseACK。

- closure64 FAIL27.87s。上游出版结束8.639953546，compile/bind13.056238637s，独立Lock观察.647465916s，dispatch.358510025s，Step5.098152194s。只有accepted及running/start1正向COMMIT；Material18次（17成功、1失败），没有Plan/Prepared/workerPublish。首Content.ReadForProcessing于27.819811411s返回pgconn deadline，历时13.764372ms，caller仍余2.180144780s；最终DB Claim晚10.042ms。该13.8ms是累计期限尾端受阻调用，不是全部失败原因。Snapshot1.465439545s、Lock1.486464546s、Materials1.964014445s及Current19次/.671258499s包含嵌套，不能相加。
- static62 FAIL21.64s。compile/bind11.255770211s仍在原caller内，Step5.076242923s。62材料与两Plan成功，Prepared COMMIT20.557477913；首次Publish实际进入Content.Put，21.567753768超时，Put历时683.881928ms、request97488B、publication正文61120B/sources63，caller余8.432241939s；最终Claim晚28.083ms。没有成功Put回执/ReadPublished/Completed正事实；超时不能证明原Content命令一定未提交，原key/Prepared/未知责任保留。Snapshot.852464498s、Lock.858757140s、Materials2.041162777s、Current67次/1.015625760s同样不相加。
- 65拒绝PASS12.00s（父39.87s）；单体正常8.14/拒绝1.22、duplicate.64均PASS。它们不抵消两个正常大图失败。

上一份f630要求的一次race资格已实际执行并失败，不能再说当前产品race尚未试过；也不能将旧profile中的caller30先耗尽覆盖本次两个原Claim先到的事实。此前同源normal失败4d40与七mapnormal373行全PASS各自保留，本次也不是由单次normalgreen保证会通过的结果。

## 3. 实际源码：可疑计算与不可省的职责

本轮全文读：`adapters/content/decision/source.go`（SHA `f3342f0bfefabd1adeb59d8fa8566e4af70dfb64ea41185aa721386a04a3df27`）、assembly.go、ports.go；`domain/task/context/{canonical.go,types.go}`（canonical SHA `1350ffdc90bafc5b07a38ac96c9cdbad4020974eac4946d6785bd53fb19f0c63`）；1.1 codec及strict JSON入口。当前closure/processing/PGfacts/policy与本人前轮全文对象SHA一致，未把05分支新增lifecycle字段或SQL混入03。

具体调用：

1. `ReadSnapshot`先新Access.Binding；实际Content读shell→closed(shell)；再实际读mandatory→DecodeMandatory；然后从当前Binding投影材料、重建expected Snapshot，比较完整Snapshot、CanonicalInput、Processed和首Mandatory ref。返回原真实shell Raw。这不是只凭Binding中bundle派生对象返回Snapshot。
2. `ReadFixtureLock`先新Access.Current；实际读lock和manifest并按原remaining扣除；各closed解码；ManifestRef跨版准确转换、当前Input材料投影和expected Snapshot；核所有rule/charge/component/digest/manifest关系，返回原两正文。worker随后还有冻结manifest对Snapshot的独立核验，不能改它。
3. `closed` 的ParseJSON承担重复键、数字、非法UTF-8/surrogate、深度/大小及尾部拒绝；typed decoder另有UnknownFields/typed shape职责。不能只剩json.Unmarshal或认为双parse必然多余。
4. `DecodeMandatory`在ParseJSON+closed typed decode后调用`EncodeMandatory(m)`并丢弃输出。后者实际先ValidateInput及全部Processed/Derivations关系，再CanonicalInput、json.Marshal、strict ParseJSON和writeCanonical。这里存在**值得定位的纯转换候选**，但丢弃字节不代表整个Encode可删除：其校验、编码后大小/深度及错误还属于现行为。尤其Marshal的HTML转义展开与原raw字节大小不同，不能仅凭原read≤262144证明后续所有表示界限恒不触发。若未来拆validation与serialization，必须证明原错误/界限等价，当前不预批准。
5. assembly.Plan也对同mandatory调用DecodeMandatory；Source后续再次处理同真实读取字节，语法可有复用机会。但需要准确新读取bytes比对、固定类型/decoder模式、有限保留、所有返回slice/pointer隔离、错误不缓存；不能用编译时结果当后来的当前资格。现在没有该复用的实际成本与生命周期收益证据，不能直接把既有Binding memo扩大成任意Content/body cache。
6. `snapshot`在Snapshot与Lock重建材料跨版refs，每项ToRule分别严格验证1.2与1.1；这是两个不同Current观测。纯投影可讨论，但不能以Component/tuple键省掉当前完整Input不同revision/metadata/ref的处理；更不能把PG LockVersion现行独立验证视作domain早已验证而省略。

现PG CheckPolicy仍MATERIALIZED锁行后clock/完整subject/actions；LockVersion仍原advisory+FOR UPDATE/JSON与影子列检查。Content每次pre/post Tx重建target+全部祖先，实际Objects.Read/hash/length保持。上述成本大不意味着是冗余授权；这也是本次不选SQL批量、跨Txclosure/policy缓存或减少门禁的原因。

## 4. 唯一有限离线证据请求

利用现有profile `/workspace/lerna-content-03-137311247276/large-graph-worker-cpu-overlay/worker-cpu.pprof`，原SHA `427bbc672bad5cf520934ed2e31116f75eca4991572fd5ecd0c3f7c3a4ff31e6`；搭配其原 `component-profile-race.test` 与binary-profile-provenance，而不是新0514 binary。原profile FD logical Close UNKNOWN继续保留，分析成功不补ACK也不清理。

由root安排**一次有界离线分析**：在原数据上按 `phase=decision_step` 过滤，只展开调用栈含 Adapter.ReadSnapshot 或 Adapter.ReadFixtureLock 的加权样本，保留case=closure64/static62标签和原完整父子栈。可用既有 `go tool pprof -traces` 配合精确函数focus；这只是已安装工具读取原artifact，无新采样、无重新编译/运行业务、无服务。先从原binary符号/现函数名构造正确regex，零匹配必须明确是零匹配，不能伪称0成本。无需分别启动七次top/list，也不再做新的normal/race。

产出一份有限结果，回答：

- 两个case标签分别在ReadSnapshot与ReadFixtureLock下采到多少CPU；其中真实Content.ReadForProcessing子树与纯Source表示分支分别多少。
- 纯分支具体是closed的strict parse/typed decode、DecodeMandatory→EncodeMandatory/ValidateInput/规范输出，还是snapshot→ToRule；保留路径，不将同名ParseJSON在Content或别的父调用下的样本混进来。
- 统计用每条sample权重作不重复父分支划分；累计节点仍注明包含，不把Decode/Encode/Parse的cum重复加总。外部runtime/race/GC未携带或无法归属的sample不强塞Source，off-CPU仍未知。
- 该旧profile是未有closure局部map的28e；Source/mandatory/codec若与本次对象精确相同，可以帮助定位其仍存在的纯计算，但不是当前4WIP新的性能量。尤其Content子树含已改closure，不能把旧子树总量当当前可省。先逐相关对象确认旧pin与当前字节，不凭名字假设。

现有七份top30以全62.18s为分母并有节点阈值，未列DecodeMandatory/closed不是0；这次读取已有样本是补缺失视图，而非再次测量不变系统。若原样本数量过低或过滤后不能分出这些分支，就准确报告“现有样本不足以选该解析优化”，不自动启动新CPU或下一轮probe。root据结果再定一项必要工作；本报告不预先授权后续循环。

## 5. 修正和闭合门槛保持

只有在这份已有样本展开加代码证明足以指向一个纯表示重复后，再单独采用一项最小改动；错误顺序/严格解析、完整当前Input/control/Permission、target与所有来源五动作、锁后clock、真实每次读取与累计收费均保持。不得在本报告名义下添加通用缓存/registry，或提前按猜想改DecodeMandatory/closed。

此后真实资格仍必须原完整4B normal/race及该改动影响的拒绝、当前撤权、并发/重开、返回值隔离控制。原caller30、Claim5、120、setup内计、完整图、容量正常对照不变。不得heartbeat、续claim、盲retry、新费用epoch、删回读、改冻结rule2/worker/SQL或扩大限制。

本次失败继续作为当前产品真实red；static原Prepared与可能已提交Content命令只按原身份核对，unknown不能视作未发送/未提交。旧所有失败/unknown scopes/关闭责任完整保留。尚无本决定产品实现或执行资格，不接受票03七AC、whole04或CI。
