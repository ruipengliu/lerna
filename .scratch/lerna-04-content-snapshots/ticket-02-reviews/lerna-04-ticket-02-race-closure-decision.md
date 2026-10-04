# 04票02：752完整闭包race超时的窄优化决定

2026-10-04。固定source `75202ee0077b5bf12b432b5017ad97a1dfcab681`，只读完整 content_closure_test.go、domain/content/closure.go 与既已全文读取的service/management及实际PG policy/facts/management调用；补读VersionIdentity与1.2 Encode实现。实际安全日志 `/workspace/lerna-content-02-fix-57ea60c628f8/component-race-content-closure.log` 全文：唯一该用例60.03s在`:71`报context deadline exceeded，NATIVE_EXIT status=1、group_absent=True。不是outer kill未知，不是race green；本代理未运行native/profile/DB/build或读取凭据。

**默认：不扩60秒测试context、原120秒外层期限或产品预算；先消除重复纯identity排序，再把现私有闭包核对返回已经锁住的事实与有效期交集，供同一Tx实际消费者直接使用。** 保留全部64祖先/65拒绝、原全部定点义务与水位、同Tx原回执/责任、每阶段当前资格。不建立跨请求cache、新授权permit或通用批处理框架。优化是否足够由soleowner有限测量，不能从源码宣称提速倍数。

## 1. 能由源码确认的代价

测试构建65个成功版本（closure-00..64），第66个closure-65应因65祖先拒绝；每个新版本都真实InstallPolicy→Put→Step，不是仅一次64节点遍历。成功版本祖先累计0+…+64=2080；这些来源以及按祖先/后代独立的继承到期责任是已承诺的真实数据，不能通过少建版本/只测63/省中间版本/压成root义务减掉。

`:71`处是installContentPolicy调用，表示全用例共享60秒在这里耗尽；日志没有i、各阶段耗时，不能认定该次安装独占60秒，亦不能把此前3f整体normal104.650s算成此单用例耗时。

两个明确重复热点：

1. `closure.go:99–104 sortRefs`：每次排序比较两次VersionIdentity；`identity.go:14`每次VersionIdentity先完整v.Encode(ContentRef)，即JSON序列化/解析、schema与语义验证、canonical编码，再算tuple SHA。DFS最终out来自map，排序O(n log n)比较反复验证同样不可变ref；Put、publication前后、Get前后、管理继承/回填都反复走该路径。这里没有需要等待的当前事实，重复计算可直接删除。
2. `registeredClosure` 已逐祖先按action读取policy、锁准确published Record、核cap；Put随后对同一refs再读取全部三动作policy与Record来取期限，publicationPolicy同样重复，Get/AuthorizeUse也重读。管理scheduleInherited又做无action完整闭包，再逐祖先LockVersion一次。PG CheckPolicy一次=FOR SHARE+fresh clock两次SQL，LockVersion至少advisory锁+FOR UPDATE两次SQL且再ValidateIdentity。因此一个三动作祖先在闭包和紧接后置循环中约有10+8次显式SQL（不含外围工作）；Put+publish前/后皆有此形状。这里是静态调用下界/形状，不是实际查询计数或时间profile。

新增AdmissionTarget还必须逐祖先LockPolicy、读取原basis/原定点key、保存change/递增work/触发原Job；这是真实耐久责任成本，不应直接删。InstallPolicy每次先有限RebuildSources扫未登记页，正常已登记路径应快速空返；是否SQL扫描或工作队列占主要耗时需实际测量，不能凭函数名猜。

## 2. 第一项采用：预计算排序key，身份语义零改变

优先在现私有排序/闭包内部每个ref仅计算一次**原VersionIdentity算法**，排序比较已计算字符串。最终refs map本来就以该准确id为key，可直接排序keys后取refs，毋须再次Encode/hash。roots/children可一次装饰key后sort并解开；若函数需返回error，准确传递而非继续忽略非法ref错误。

不能改为按ContentID/原始JSON排，也不能删fullRef验证/tuple冲突或共享祖先检查；排序影响取锁顺序和规范输出，必须与原有效输入排序完全一致。不要在公共VersionIdentity或codec引入全局memoization/跳过验证快捷口。严格1.2 golden/原digest/存储key不变。

这是当前最小低风险改动，可先观测作用；它本身不保证60秒通过。少量同输入排序/公开已有闭包对照即可，不另造只有实现细节的巨大测试矩阵。

## 3. 第二项采用：私有一次闭包资格观察，省同Tx立即重读

在 `domain/content` 内把现private遍历的结果深化为有界完整观察：每项准确Ref/已锁Record（按值/必要字段冻结）及**实际请求动作集合**得到的policy期限交集；另有整闭包最早ValidUntil/RetainUntil/CurrentRetainUntil。精确字段按当前调用者需要选择，不导出为公共合同、不增跨owner授权票据。其接口仍只服务现有真实模块消费，不建立泛查询语言。

- Put取read/process/save全部适用policy与准确published来源，直接用返回cap/bound及refs作同Tx接纳/SaveSources，删除紧接的相同读取循环。
- publicationPolicy的启动Tx和完成Tx**各自重新**取得一次完整观察；两次不能复用同一份，因为对象I/O在Tx外且资格可能变化。两次都保留最终DB clock、Claim与原deadline核验。
- Get两个独立Tx也各自重新观察；不能复用read前到disclosure后的许可，更不能省返回前gate。AuthorizeUse每次独立当前核对。
- scheduleInherited/legacy回填需要无action的**结构事实**（合法维护也要能处理已过期policy）；用返回已锁Record代替随后重复LockVersion，但当前原保存主体/用途LockPolicy及原maintenance basis仍真实读取。不能把无action结构观察当save已获准。
- 先保持Repository.ScheduleRetention/AdvancePolicyJob既有窄转发。Put到管理scheduler之间仍可保留一次独立遍历；不要为了最后几次读把private观察强塞进导出Repository port，或给存储加任意Tx cache。若测量证明这一段仍占主导，再以两个当前实际消费者评估一次准确端口调整，不作为当前默认。

**同Tx复用的成立条件：** 每项policy已持有原FOR SHARE/UPDATE锁，Record已持有原版本锁，主体完整绑定/用途/动作/准确fullRef均已核；同一Tx后续未修改这些来源事实。所有原Action检查仍实际执行，不能只调用CheckPolicy("read")后假设save同权，也不能绕现decorator的实际policy入口。保留锁等待后fresh clock，且最终准入/披露/保存判定仍用当时fresh DB Now与已观察的完整最早界限，时间不会因行锁而停止。

这个值不是cache：只在原同步callback内、原owner/Tx与固定主体用途存在，函数返回后销毁，不跨Tx/worker/reopen/公开请求。自Tx内若真实改变source cap/policy，原观察必须失效或重读；不允许修改指针共享值后伪称原事实没变。

删除重复读取后，不得把原caller最后门禁提前到昂贵调度前；处理完实际所有阻塞工作后仍须满足现准入/当前资格语义。若期限在准备/调度中跨过，不能以“前面读过且有锁”提交新accepted。具体提交/拒绝仍沿原命令事务语义，不能为了优化将部分责任留下或刷新AcceptBefore。

## 4. 明确保留的必要成本与边界

每个准确D/A/原完整保存主体+用途/policy revision/原due各自PolicyChange，原key和Deadline/ExpiryDeadline保持；共享祖先在一个D闭包去重，不把不同D责任合并。全部来源≤64，65必须拒绝，不限深度偷截，也不能将已登记source edges当当前完整权限快照。

原历史policy_change/current自然核、F1原阶段交接、后继维护coverage、原source与target caps、原watermark/游标、每change最后deadline独立复核与pending单调保存均不变。扫描完第一页/一次Jobdone不能当ALL责任关闭。所有save/Job在原owner同Tx，rollback/commitunknown原行为不改；无新的异步best effort登记。

暂不默认SQL批量快照、CTE递归闭包、全局identity/policy cache或公开多action新端口。真实SQL批量化若后来必要，必须保精确行锁/完整集合/最后时钟/错误资格，并经独立正常故障对照；不是这个已知重复的首选解。

## 5. soleowner最小测量及验证

源码能证明重复存在，不能证明CPU与SQL各占比。soleowner可在唯一槽、原有限scope中增加非敏感阶段计时：每轮i、InstallPolicy/Put/Step累计和当前单次时长，测试末尾/失败时输出；必要时以实际Repository装饰器统计Now/CheckPolicy/LockVersion/SaveChange/Trigger的次数和累计等待，指标只诊断机制，不作业务oracle。不得日志正文/凭据。测试context仍60秒、外层原120、产品原锁/事务/发布/维护预算不变；失败全部记录，不能跳套件称成功。

采用顺序：纯排序预计算→有限观测→私有闭包观察消除确定重复→再观测。若首项已充分且总代价有余量，第二项可据实际收益决定同轮是否需要；若SQL仍主导，则落实第二项。无需用户新确认，root/solefixer按实测结果执行既定默认。不得把owner下一次run称本代理已profile。

最终必须原完整64成功/65拒绝用例normal与race真实通过，保中间来源/共享祖先/不同owner和用途/fullRef/五动作/cap、publication真实对象I/O后撤权、Get后gate及继承责任/重开/原截止测试。必要机械sort对照或计时不替这些业务观察。若仍超时，报告准确阶段耗时与原失败，继续定位；没有证据时不自动扩大时间或降低合同上界。

本决定只解决当前可实现的性能重复，不更改领域约束，不引入下一票/05条件准备为前置。旧race native1真实失败保留；此次没有任何完成/性能改善声明。
