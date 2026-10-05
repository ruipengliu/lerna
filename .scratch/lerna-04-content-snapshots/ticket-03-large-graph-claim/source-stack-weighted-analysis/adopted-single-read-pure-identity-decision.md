# 04-03：已有Source栈样本后的单次读取内纯身份复用

## 1. 唯一采用方向

**采用一处产品优化：将现有成功 full ContentRef→VersionIdentity ID 的≤65项局部memo，在一次 `ReadForProcessing` 调用的前、后两个观察之间复用。** 只复用纯值函数的成功结果；两次观察的全部Record/Policy/refs/records/active/closureObservation/时间资格仍各自新建和读取。原真实Objects.Read/hash/length照旧。既有registeredClosure的其他调用者仍每次新memo。

这是对原 `2adc...` 决定“post-I/O另一Tx也新建身份memo”的**明确窄修订**，不是暗改该旧报告。原决定不让授权跨Tx复用的边界不变；这里延长的只是由完整不可变ref和固定算法决定的纯字符串结果，生命期限定一次读取。不是whole closure或current-policy缓存，也不把domain已校验结果传给PG免检。

现在不再诊断、不新profile、不改Source解码/表示、不同时优化TupleDigest/subjectKey/SQL或PG。先由solefixer实现这一小处并做原资格；不能预言它足够让全部race容量通过。本人仅STATIC读取与写报告，无Go/native/DB或产品修改。

## 2. 新分析证据与限制

已读 `source-stack-weighted-analysis/{commands-static.json,analysis-outcome.json,README.md,classify-traces.py}`，读取weighted JSON的全部189样本作纯文件分支汇总，结合root独立 `/tmp/lerna-root-source-traces-audit.json`。不声称本代理重新执行了pprof，也不把首个过大工具输出的截断称完整人工逐行审计。原raw完整身份为6058行/472556B，SHA `f610d853b0a43883351415deedb0f90a33b6620c37d01dae70ea84f7babfc26e`；root已独立逐189样本核全部frame/标签/权重。唯一pprof PID3507329/start14701529，actual0/Wait/groupAbsent/explicitRelease。

原commands中的parser SHA `7c1919...` 是准备cutoff；actual parser为 `1c383da33ae21487d84d4a6e97f066707ea9cb512a9a65bc24cd19bae38c73c7`，仅补精确inlined parseJSON识别。weighted输出 SHA `fb56be1ad93d2c322a8d9d979d3fd168a67fc9fc8a6902592770ee1ef723e2fa`，189项/1.890s，无重复归属。README标题仍STATIC是早期说明，actual-outcome才提供已完成资格。

按原Source根分支：Content1.740s、Access .060s、其他Source .090s；后者含snapshot→ToRule .060、mandatory typed .020、mandatory Encode输出strict parse .010。未采到closed/ValidateInput/writeCanonical不等于实际零成本。该样本没有支持优先扩大Source解码memo；也没有把先前coarse Snapshot/Lock全部耗时确认为重复解析。

为核对剩余纯身份路径，本轮仅在**已有189样本完整frames**上做互斥归属（没有新native/pprof）：

| 历史Content子树，秒 | closure64 | static62 |
|---|---:|---:|
| VersionIdentity，经PG LockVersion | .070 | .080 |
| VersionIdentity，经domain路径 | .410 | .280 |
| subjectKey | .020 | .030 |
| 其余Content路径 | .430 | .420 |

domain身份分支再分旧sort路径 .190/.140、旧visit或target .220/.140。这些是同一批样本的子分区，不能再加到1.740之上。旧profile28e尚无现closure局部memo，**.410/.280不是新改动的可省量**；旧sort/visit重复已有一部分被当前map消除。它仍证明准确纯函数调用在真实Content路径中有成本；当前源码另证明前后观察尚有同fullRef成功纯计算的重复，二者共同支持下面有限改动。没有当前节省毫秒值或保证完成5秒的推断。

原profile SHA `427bbc...`、binary963905仍只属于历史样本。标准testing原profile FD Close UNKNOWN继续保持；后续解析/Wait不补ACK。来源等价文件证明Source/mandatory/codec对应对象与旧pin相同，但Content closure已变，不能把整个旧Content成本外推当前性能。

## 3. 当前源码与最小接法

WT `/tmp/lerna-worktrees/content-snapshots-03`，HEAD28e加四项原WIP不变。本轮重读当前两处完整相关路径并核SHA：

- `domain/content/closure.go`：`8542696150191da3fafd6b0df90e38f272fd934de807317442933c536fb1331c`。
- `domain/content/processing.go`：`32f14cd27d48a55069eed5a89aab39a3465f03d6b05189875be9d8382d9a6f32`。
- `domain/content/identity.go`：`e093a7918be9af0aeaec08c62b472de1141314812a71e3da386583237bbf60f0`。
- PG facts/policy仍分别`8d53cb...`/`3cb314...`，真实SQL/锁/时钟不变。

当前registeredClosure在入口创建65项身份map，其原sort/visit使用本次map。ReadForProcessing的同一observe函数在Objects.Read前后各由Within执行，故会创建两个互不相干的map。成功pre观察中已算过的target及full refs，在post原相同位置又算一遍。`VersionIdentity`只执行固定1.2 ref验证、位置数组编码和固定hash；结果不含DB数据、subject、purpose、action、当前时间或授权状态。

最小具体实现仅两个domain文件：

1. 把当前局部map/identityID提为同文件私有、有限 `closureIdentityMemo`（名称可随实际风格），存 `map[v.ContentRef]string`，成功才写、最多65、满时继续原VersionIdentity计算而不写；不缓存error、不改VersionIdentity函数/算法/返回错误。
2. 只有一份真实闭包遍历主体。现registeredClosure作为私有便捷入口，为所有原调用创建新memo并委托主体；一个私有入口让ReadForProcessing提供本次memo。不得复制遍历，或新增public interface、generic memo/registry、策略选择、可外部注入identity算法。
3. ReadForProcessing在所有原前置校验之后、observe定义处创建一份空memo；observe仍在原target gate之后调用同一闭包主体，只传该memo。创建空map不提前做任何ref校验。两个Within共享的仅此纯身份表；post重新从新r.Sources遍历，绝不复用pre的roots/records/排序结果/seen/active/bounds。
4. 读取函数返回（成功、拒绝、I/O错误、取消皆然）即丢弃memo；不挂Service、Store、Access、Binding、Config、context.Value或worker；不跨另一材料/ReadForProcessing/Publish verification/外层ReadPublished/Step/重开。每个调用独占map，内部前后顺序执行，不新增锁/后台清理/Close资源。

65来自原target+64闭包，不提高业务64。post若出现更多不同full refs，已有memo可满，但只影响命中率；依旧原函数验证、原64裁决和错误。当前error/拒绝不保存为成功，不能通过memo使非法ref变合法。

## 4. 等价、错误顺序和当前权限证明

键必须是整个值型ContentRef：Owner两项、ContentID、Version、Hash、MediaType、ByteLength；不以tuple ID/hash/指针替代。相同tuple不同fullmetadata必是miss，再经原校验与原record/ref关系检查。持有的字符串为不可变值，不含可被调用者修改的slice/Record。

hit只能代表“同一固定ref在此次读取内已成功执行相同纯函数，ID相同”，不代表该版本还存在、还published、原来源图不变、当前政策允许或原cap未到。因而：

- 原 `refs`、`records`、`active` map及`closureObservation`每次观察重新分配；所有中间版本和新来源都真实遍历，deterministic排序及cycle/fullRef/64检查保持。
- target identity、sort逐输入ref校验、visit owner→identity→cycle/duplicate→64的原位置不动。单元素sort仍不提前校验；新非法输入的首错误顺序不变。成功hit不能产生新错误，失败miss返回原error。
- 每个原会访问的来源仍执行Now→CheckPolicy全actions（含PG锁后freshclock）→LockVersion→postNow→authority/retention→metadata/cap；顺序不变。即使身份hit，缺行、来源变更、revoke、expired、same tuple错误fullRef都按当前事实拒绝。
- PG LockVersion仍**自己**调用原VersionIdentity、原advisory、真实FOR UPDATE、完整body/影子列核对；PG不读取domain memo，不减少端口调用。CheckPolicy/current fullSubject/purpose/五动作/F1完整绑定也不减少。
- ReadForProcessing原入口v.Encode(ref)、remaining检查、target policy/ref门、每个Tx末freshNow、真实Objects.Read及hash/length/bounded.Err保持；post所有授权与原before最小cap依然重新计算。更短的纯计算不允许刷新deadline、扩大budget或把错误资格转成成功。
- ReadSnapshot/Lock/材料/Publisher的真实Current/fullInput/control和收费仍原样；不修改冻结worker/rule2/SQL/公开字段。资源Close/unknown边界不受此纯内存表影响，不能据此清理历史责任。

这仅承认schema与VersionIdentity代码在同一process中固定、fullRef值相同的纯函数等价，不引入跨owner原子性或可复用权限。未出现动态合同升级；无需为未来假设新增版本策略框架。

## 5. 真实纵向资格与停止条件

当前原race7b48是容量真实red，不需要再盲跑一次未改产品的red。solefixer先补确切等价保护，再只实现上述生命期调整：

- 正常原ref且完整64图：前后观察同ref时纯身份函数可命中，但两次真实Store.Policy/Lock/Now、实际Objects.Read及返回refs/bytes必须保持。局部机械测试可验证纯函数调用少一次；这只是机械证据，不代替PG公开链。
- 真post-I/O窗口：原ref已命中后撤销read或process、收窄cap/到期、换当前policy的fullRef、来源不可用/完整祖先资格改变，都必须由后观察拒绝。现有公开故障seam能覆盖则复用，不写私表镜像oracle。
- 反例：fullRef同tuple但hash/media/length变化不命中；新非法ref原错误优先；cycle/target自引用/64与65；post新refs使纯memo超过65后仍fallback而不增加业务拒绝。不要为测试允许生产变更已发布immutable来源；这类不可达损坏变化用现机械repository seam标清资格。
- 新一次读取/并发另一次读取/重开无memo继承；第一Tx失败不返回旧成功，I/O失败不跳后续责任；私有返回值无新增可变alias。无须制造物理Close错误来测这个纯map。
- 固定新source与上述控制后，原exact4B normal与独立race各一次，仍原30/5/120/count1/全材料、两Publish两ReadPublished、Completed及原公开tail；不得预热/retry/延长lease或把setup搬走。每次资格由root安排soleLOCAL，按原原子注册/Wait/FD/unknown规则记录。测试未在本报告执行。

若仍失败，保该新源码首真实阶段与完整责任并停止，不自动扩大memo生命期/数量、引入PG cache、删门或连续profile。新优化的语义资格与最终容量是否满足分开评价；此报告不承诺性能数值。

原normal4d40失败、七map373行normal通过、原五maprace7b48失败三项各保原事实。static62原Prepared/Put超时不能当未提交；所有原key/余额/费用/关闭unknown保持。票03七AC、whole04、最终审查和CI仍未通过。本次只决定这一处必要产品候选，未实施、未声称退出。
