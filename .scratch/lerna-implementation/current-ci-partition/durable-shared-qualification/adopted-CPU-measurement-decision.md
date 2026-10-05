# 原 closure60 比较收口后：一次当前源码 CPU 归属测量

**采用的唯一步骤：在原owner获得独占LOCAL后，对当前d0的原单个closure60 race做一次默认频率CPU profile，保留现有12项port wall诊断。** 不新增SQL/环境observer，不改产品或测试业务，不重试wholeR。本报告仅STATIC决定；实际构建、native执行及分析命令仍需独立有限计划与root授槽。

## 已消除的疑点与仍缺的数据

全文读比较证书 `/tmp/lerna-partition-durable-closure-execution-comparison-certificate.md`，SHA256 `2eab95cafac90d9424b1bc88d1745b85e74b01047897abbcbcd42e7216d5aa5b`，核附录hash `84112a452f81e300274623b9d2c72926e4577a3621d892969a26b9009ced8bfa`。证书实际比较：d0fcc61264904070ce94e1146f07327a0ef9b2aa 与成功CI e0c66f6dec1325f648060248207056de1b0ced06/run37305738280的282个Go文件、Make/Go锁/workflow相同；shell区别发生在单closure之后。此次不重新声称人工全文检查291个blob。

原d0 race closure60.10失败、CI20.791成功、d0 normal18.481成功均保留。配置偏离未证实，失败时段CPU/PG telemetry缺失。现时cgroup配置不能重建失败时段；所提历史PG日志最多可能发现异常，既无已知慢SQL记录保证，也不能区分正常但累计昂贵的本地编码/调度/驱动路径。因此不继续这两项为泛环境审计前置。

现有port wall已定位实际累计量，但不能回答**当前race进程究竟在哪些本地栈消耗CPU，是否有具体可等价消除的重复**。这是本次选择唯一补充的数据。PG服务端耗时、锁等待和网络耗时仍未知；不同时引入第二条测量路线。

## 一次测量的精确边界

源码固定WT `/tmp/lerna-worktrees/current-ci-partition` 的d0，先后核原manifest；原case `TestContentFullClosureIncludesIntermediateVersionsAndExact64Bound`，完整0..65输入、64 Get及65拒绝尾不变。保留原 `-race -tags=integration -p=1 -count=1 -mod=readonly -timeout=120s` 与准确单测试selector，只增加Go testing原生CPU profile输出和准确保留binary路径；不编写业务overlay、标签框架或替代fixture。原-v已有12cells可完整记录。

尤其保留源码实际配置：caller60包含NewWorld/全部安装、Put/Step/Get；本caseLease一分钟、WorkTimeout5秒、PublishBudget一分钟、MaxPublicationAttempts3，不能误套03 Decision Claim5配置或更改任何原期限。无需先跑normal预热，无第二次race择优，无全套Recovery前缀；这是新单case诊断条件，不能冒称重现原wholeR的所有前史。

冻结一份新exact root/ledger、单binary、单profile、原始log、源码/命令manifest；构建与执行由原有限控制器分别登记PID/PGID/start、输出FD与期限。业务和行政120界限不因profile停止/flush额外续期；达到任一原边界真实停止，所有未完成尾记未执行。使用默认CPU采样频率，不开启heap/block/mutex/trace/profile矩阵。原profile或旧UNKNOWN不复用覆盖。CPU输出必须正向完成才称可用；profile写入/首次Close没有独立ACK时，如实保留其FD/根UNKNOWN，不能以文件可解析、Wait或group absence补成Close成功。

执行一次后STOP/RELEASE，先记录actual Wait、group、完整原业务结果及旧/new不可混淆身份。测量开销UNKNOWN；一次profile PASS不能证明原非profile wholeR通过，一次profile FAIL不能直接当候选算法退化。

## 有限分析与可导出的下一步

仅消费这同一profile和准确binary，准备两个有界本地分析输出：全进程flat top40和cumulative top40，分别明确样本CPU秒、整profile分母、截断范围；各分析原生操作另有有限deadline/登记。不下载符号或访问服务，不再次采样。保留原始profile供核对，不能只留百分比图。

- flat本地own代码或可归到真实consumer的validation/encoding CPU若显著，且完整源码证实同一不可变值在同一有限生命周期被重复纯计算，才选**一个**精确等价修复；保存错误顺序、完整身份、当前每次SQL/锁/policy/BodySeal/clock/责任/收费，给对应真实行为控制与原60case验证。当前没有预选cache或优化承诺。
- 只有cumulative高、包含共同子调用，不能把它们相加当可省CPU。runtime/race/GC或无业务栈归属样本不能强分给某个port；过程CPU可能跨核，不能简单用“wall−CPU”称PG等待。
- 若主要是runtime/race或CPU样本不足以解释当前wall，就收口为“尚无可指认本地产品修复”；明确该结果不证明PG慢、SQL阻塞、host限额或网络原因。之后只能针对该结果确定新的必要一步，不能自动串联profiles、环境轮询或盲重跑。40项外未显示函数不等于零成本。

取舍理由：当前没有具体服务器慢SQL/等待证据，先加PG查询级钩子会改变大量本来便宜的调用并仍混合客户端CPU；当前CPU采样无需修改consumer门禁，能直接检验最小纯计算修复是否有根据。历史03不同源码/不同case的CPU不能代替它。

## 不变退出事实

不改变60/120/1445，不拆完整祖先链、不扩大pool、不给冷/热预置，不合并SQL改锁顺序、不缓存authority/整closure，不删readback或独立责任。当前wholeR失败及Content97/Durable60/other43/fixture尾未执行不变；准确成功CI不替d0新分组验收。03语义、06资格独立继续，不为此占用其当前LOCAL。

本次仅读证书/源码并写/tmp，没有Go、DB、cgroup/PG日志探测、profile、source mutation或native执行。
