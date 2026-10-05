# 04 票05：正文清理与 holder 条件实施 handoff

2026-10-04。全文读取已采用 `issues/05-body-cleanup-and-holders.md`、`decisions.md`、`ticket-02-handoff.md`、`ticket-02-oracle-decisions.md`；实际源码依据01交付 `1a7d910238eb74cddc712d92b0ba4014a72ff507` 的 domain/content、PG facts/0001与 local/store_linux.go。02工作树 management/ports 仅作未冻结形状背景，不能当正确实现或退出证据。**硬等02正式退出、整合及实际API/迁移delta复核才采用或claim05。03与05仅共同依赖02，相互无新增阻塞边。** 本次只读及/tmp文字，无native/DB/build/测试或仓库修改。

## 1. 实际基础及必须补足处

01 Objects只有Put/Read；local.Store以进程内gate串行调用，Put写 `key.epoch.tmp`、校验/Sync/Close、Link到准确key、目录Sync、独立回读、删除临时文件并再次目录Sync。Read打开准确key、验证全正文并跟踪FD；CloseContext只给drain有限期限，native Sync/Close实际返回前仍由原owner负责，firstClose错误sticky。**这些还没有跨进程效果隔离，也没有Delete或墓碑。** 05不能只多一个Remove调用或凭PG Claim认为迟到Link已停止。

PG `facts.go:SaveVersion`保存独立staging列；01成功出版同Tx置Bytes=nil/StagingHolder=false，已真实清除活动行staging。失败或未确认路径仍可能留staging/临时/最终对象；ObjectHolder=false不证明物理不存在。现Record只有最后AttemptKey，不能据它遗漏旧epoch临时文件。历史published、原tuple/Command receipt与正文可读性必须分开；0001约束的publication仍preparing/published/failed，不把gone硬塞历史publication。

02 WIP目前有PolicyChange、CleanupResponsibility（原change/ref/主体/用途/actions/deadline/staging/object/attempt/residual）、有限分页ObserveChange及ManagementRepository。这是05承接的实际责任形状，不需要另建通用Job/holder registry。其最终签名和0002内容仍须重核。

## 2. 三个阶段不能合并

**政策收紧责任：** 沿02五动作。只撤save可登记原保存依据的cleanup_pending；只撤read/process/disclose不自动授权删除。不同主体/用途不能删原保存主体/用途的正文。错误完整Ref替代原许可，对对应准确源五动作无效，按原source而非后代Ref分类；继承适用范围仍由原record.Subject/Purpose限定。

**开始不可逆正文关闭：** 受信Lifecycle入口核当前管理资格、准确原版本/保存依据及02责任，短Tx锁版本，fresh DB Now，设置单调 `body_sealed`、登记全部已知holder/attempt清理项、原deadline及本版本cleanup Job；同时阻止该版本新出版、保存/同步副本、body读取/处理/披露。此前save-only pending而body尚未sealed、read/disclose及caps仍有效时，Get正常对照继续可读，不能在02事件发生瞬间一律永久forbidden。封闭是实际删除协议的前态，不是假修改所有主体policy flags。

对迟到旧change，启动前须重核当前原保存依据：若只曾短暂布尔撤销、当前准确许可已恢复且单调cap未过期、尚未sealed，可明确核对为not_required并保留历史责任/原因。不能无条件删除，也不能只因新宽policy就抹pending。已sealed/tombstone或已过期cap均不复活；不可逆阶段不再取消清理。明确受信合法删除要求也可启动，但需准确owner/ref/用途/有限管理授权，不能把一般query或全工程授权当运行时删除许可。

**擦除确认：** Tx外实际删除及独立观察；Tx内只在匹配原seal generation/holder identity及完整结果时更新erased。deadline结束、进程退出请求、文件not_found、policy已过期、DB Claim失效各自都不是完整擦除证明。未确认保留cleanup_pending/residual、负责方、原期限与原因；有限一次执行退出不等于责任消失。

## 3. 最小消费ports与迁移

- 在 `domain/content` 增加实际Lifecycle职责（例如 `lifecycle.go/holders.go`），消费02仓储加：锁/保存不可逆body seal、保存/分页读取准确holder与全部attempt责任、原清理结果及Job推进、独立读取staging是否已去除。权威仍为Content owner；不让object adapter反写Content事实。
- 新的小介质清理端口服务真实Lifecycle消费者：准确key的 `FenceAndErase(ctx, identity, attempts)`、`ObserveErasure(ctx, identity, cursor, limit)`；identity含原版本绑定/对象key/本holder/固定seal身份。名称可局部细化，但结果明确区分fenced、body/temp残留、已核实缺失、unknown；一个nil error不得含糊代表全世界已擦除。现Put/Read签名可保持，由local实现内部遵守新协议。
- holder是具体持有者：PG staging、primary本地目录、真实secondary目录/进程。每个登记有准确ContentRef、holder ID/物理root受信绑定、原保存主体/用途、对象与attempt keys、已知/可能存在状态、seal/清理原命令、deadline、ACK依据。有限固定配置映射这两个实际文件holder，不造任意远端插件注册器。
- **追加Content owner迁移0003或02最终后的下一号**，增加seal/holder/attempt/metadata许可事实及实际cleanup phase约束；不改已发布0001/0002。原Record JSON兼容读缺新字段为“尚未作清理观察”，不能默认erased。原receipt/tuple/ref/key算法与published历史不变。
- 新publication attempt写前同Tx登记每个原key/epoch责任；完成后只按实际确认归档该attempt。旧01/02受控停writer升级，按原record/已登记scope有限回填：最后AttemptKey只是起点，原准确key的合法临时名需枚举核对。未知条目保留而非假无残留；无不受控旧writer混跑广告。该升级不要求16。
- Step只增加本票实际cleanup阶段或一个明确Lifecycle.Step，复用现Claim/Tx/有限调度；维护任务不因普通publish容量为零而永远不推进。传播仍沿02全部后代冻结水位分页，holder/attempt也有完整分页水位；不能看到首个空页便宣称全空。

## 4. 本地文件的具体跨进程协议

选择现Linux适配器内的**每准确key稳定锁文件+耐久关闭墓碑**，不是VFS。锁/墓碑只保存有限身份/关闭元数据，无正文。key仍为原VersionIdentity SHA-256，版本不同得到不同key，不能以内容hash相同就混用删除范围。

1. 在可信root下按固定安全命名打开key对应lock文件，O_NOFOLLOW、regular-file及准确路径核验，所有Put/Read/Erase/Observe都用同一个文件锁协议；锁文件一旦使用不unlink/recreate，避免两个inode形成两把锁。用Linux flock等现平台原语，非阻塞尝试配有限ctx等待，不能在PG持锁Tx里等文件锁。
2. Put从创建任何正文临时文件之前取得exclusive key lock，检查墓碑；持有至全部写/校验/Sync/原FD确认关闭/Link/目录Sync/回读/临时清理结束。每次Link前重新确认同锁下未关闭；EEXIST仍须准确原字节匹配。墓碑存在或其完整性/耐久性未知时fail closed，不以重新开Store清除它。
3. Read持shared key lock至实际正文FD确认关闭；墓碑后不返回正文。领域Get最终current gate仍保留，锁不替代当前授权。已在调用者内存或非协议外部读者的字节不承诺撤回。
4. Erase拿exclusive lock：核准确责任→持久创建/核对原key墓碑（写/文件Sync/Close+目录Sync）→删除准确final key及本版本已确认归属的全部临时正文→目录Sync→独立受锁重开/观察确认原key和所有目标临时正文缺失。重试同墓碑/原seal幂等；损坏/半写墓碑先保留封闭并核对原身份，不改成open。可采用临时元数据+原子安装以恢复半写，但不得凭一个文件名宣称durable fence。
5. 若Put先持锁，它可先完成有限安装；Erase等待其退出后再封闭删除。若Erase先完成，迟到Put必须看到墓碑并拒绝，原Claim或更高epoch都不能绕过。删除不需要假证明旧PG worker已经消失；真实效果锁+墓碑是隔离依据。故障实验必须覆盖这两个真实跨进程次序。
6. FD/Sync/Close原ownership继续：新lock/metadata/目录FD也跟踪；native调用不能靠ctx取消后丢给无人负责goroutine。drain超时可等待真实返回后重试；native Close失败sticky，不凭二次Close nil称已关闭。正文FD关闭未知时不能释放协调锁然后宣布该holder无残留；保留holder/锁FD责任，必要时等待真实持有进程退出再由独立进程重核。不得将注入诊断错误宣传为原生Close故障证据。

旧01二进制不遵守此协议，不能与05新writer混跑后声称防住它。实际升级需停止/确认旧进程退出，再开放新协议writer；测试的迟到writer必须是真实新协议consumer。PG lease或同机容器隔离不补这个条件。

## 5. staging、第二holder与孤儿

**PG staging：** seal Tx后清除准确版本staging列（已NULL则核实幂等）；与其holder结果同owner提交，并经独立连接的受信holder观察确认。不可仅置StagingHolder=false不删列，亦不可用空字节代替NULL（零长内容与不存在不同）。保留最小Record/ref/receipt，WAL/旧快照/备份不属于逻辑正文清理证明。进入Tx外Put的旧内存和FD由文件协议/进程责任另核，不把删staging等同不存在在途字节。

**第二holder：** 真实不同root、独立文件/进程打开并确实写过原字节，不是hardlink到primary、同一路径别名或一行离线标志。复制前同Content owner Tx登记该holder及原有限复制责任，核适用当前sync+保存/读取用途资格；Tx外复制，回读后才ACK。关闭与注册/复制使用同版本seal门禁：seal之后不得新登记写入资格；seal前已登记但未ACK的复制也进入清理全集。离线期间旧已发写可能迟到，仍有原key/墓碑/holder责任；重连先处理关闭/清理，再允许新动作。

primary已确认擦除而secondary离线时，管理观察必须有secondary负责方、原deadline与unknown/residual，不能报告cleanup_complete。恢复用同清理命令/原key核对；真实删除失败、ACK丢失、删除后重开都须正常对照。固定两个holder不等于持有者只有两行：实际attempt/传播数量及注册水位也要完整覆盖。

**孤儿：** 从已提交准备/attempt责任和本owner准确key空间有限列举；不是遍历全root按mtime或目录数删文件。仅对未published原版本，在短Tx重核前态并单调seal/停止其出版后，才按原key执行文件fence/delete；若竞争已published，不得走孤儿路径删除活引用，转合法正文生命周期或保持可读。更高新version/其他owner从不在其范围。

旧attempt历史未全登记时，对同key合法临时名的发现必须绑定本owner原版本/受控升级事实；无法判定归属或存在未受控writer则unknown，不猜epoch或误删外部文件。精确范围内枚举分页+禁止新Put的墓碑使其可有限收敛；最后一次独立核对仍必须在同协议锁下。锁/墓碑等元数据不计正文残留，不能继续用旧“目录必须只有N个文件/空目录”断言代替正文观察。

## 6. gone 与原身份不复活

保留原1.2 `ContentGetResponseGone{content_ref,evidence_available:false}`；不加wire字段或delete方法。推荐明确gone表示：**本Content服务的权威正文路径（PG staging+primary）已不可逆关闭且物理清理独立确认，因而原证据不能再由此服务回读**；它不表示所有离线副本/备份全局擦除。全holder cleanup_complete仅在受信管理观察中全部ACK后成立。primary删除未知时返回准确unavailable/既有授权拒绝，不能先gone。published历史与原Command progress/receipt保持，不改为从未出版。

需要最小独立 **metadata-only受信许可**：准确fullRef、当前subject、具体purpose、revision、ValidUntil，明确只准读/披露原ref及evidence_available，不含正文/来源列表。它是FixturePolicy管理的受限元数据视图资格（可用独立内部表/类型），不是第六个正文动作，不是Grant。适用来源对元数据披露的限制须有当前明确许可；不能因body许可过期就推断metadata允许，也不能借metadata许可恢复body cap。

Get先在当前资格下选择body或metadata视图，锁后fresh DB time检查AcceptBefore/当前metadata expiry；先比policy与actual record fullRef，recordnil则与requested fullRef比，再披露gone/not_found/integrity。正确policyA+错requestB+actualA可保留授权后integrity，错误policyB对actualA不能泄露存在。metadata-only不足以返回published字节或全部来源；未gone时可用既有拒绝而非扩schema。查询不创建Job或补删正文。

原Command同摘要在当前原reader授权下返回原固定receipt，不重新Put；尚未sealed且当前资格有效的版本，新Command同声明仍沿原关联规则；已sealed版本的新Command关联属于新保存准入，必须拒绝，不能借返回原历史偷放行，更不得建立任何新publish/复制责任。异声明仍version_conflict；新准确version需独立当前权限/命令/来源，可以正常完成，不能通过原version反复widen政策使旧cap或墓碑复活。

## 7. 七AC的具体公开/独立出口

| AC | 正常出口 | 故障/拒绝及恢复出口 |
| --- | --- | --- |
| 1 受信封闭与责任 | 合法Lifecycle启动，管理Observe见同原ref seal+全部holder责任，原receipt不变 | 无权/错ref/错主体用途不能删除或泄露；seal提交未知重开原责任，不新命令 |
| 2 真staging+正文删除 | 真实未发布staging清理、已published primary清理各一例；独立holder观察与准确文件检查一致，获准metadata查询gone | 文件缺失但墓碑/目录Sync/FD关闭未知不报erased；无metadata权、过期、wrongfullRef均不获最小元数据 |
| 3 真第二holder | 两实际副本各独立回读→清理各ACK→全部完成 | secondary确曾存字节后离线/真实删除失败，primary可gone但全局pending；恢复原命令删同副本并独立确认；ACK丢失不盲目重建 |
| 4 跨进程迟到效果 | Put先锁与Erase先锁两序均正常有限闭合 | 真进程停在temp Sync后/Link前及seal墓碑后；另一进程清理/重开，迟到原Put不能重建；不以mock计数或PG Claim代替 |
| 5 新version+全页 | 同content_id新version独立正常；旧版本清理后新准确字节仍可读 | 超一页后代/holder/attempt传播中断重开，全部水位登记；旧change不能扩大范围/倒退cap或误删其他用途 |
| 6 精确孤儿竞争 | 原已登记未发布attempt最终清理，其他scope独立字节不变 | 真实publish与orphan-seal两序；publish先赢保活，seal先赢晚Finish拒绝且效果被fence；未知归属保留responsible |
| 7 真实持久观察 | public Content/Command+受信Lifecycle/holder入口，独立对象字节与重开状态对应 | 有限ctx/关闭错误/离线残留均有限退出保责任；机械注入与真实PG/FS/SIGKILL证据分列，不宣称掉电/法证/生产多机 |

每个拒绝/故障需本身允许的正常对照；测试断言按原准确key/正文/回执/管理事实，不通过私表布局或内部调用次数判业务。PG实际staging物理逻辑删除由独立受信存储观察证明，不让“测试直接SELECT业务私表”取代Content生命周期入口。真实native步骤只由本票未来sole执行owner运行。

## 8. 到frontier才冻结的事项

02 final SHA/API/CI及全部责任分类、source闭包/传播watermark、当前Get及policy锁序；0002最终迁移和Job phase；已有用目录计数的测试需要改为准确正文oracle；旧01/02 writer退出与可信本地volume/flock能力范围；第二holder实际root/进程/有限故障计划/归属登记。03并行若改Content内部处理入口，只合并真实共有的seal/current gate，不新增03前置或双方重复实现生命周期。以上是条件接法，不是能力广告、七AC通过或whole04完成证据；未发现需要新ADR的规则改变。
