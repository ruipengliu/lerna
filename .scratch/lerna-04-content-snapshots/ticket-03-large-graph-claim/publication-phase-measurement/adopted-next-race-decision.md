# 04-03：发布阶段实际正常出口后的唯一下一步

## 1. 决定

**不新增产品优化或测量；固定当前明确四项WIP源码，转一次原完整4B、非 profiling RACE资格。** 原30秒caller（包含setup）、5秒Claim、Go/outer各120秒、count1、原输入/容量/读取预算/RuleStarts/费用/原public oracle全部保持。由root核实际上轮Release和源码/二进制资格后授sole LOCAL；本人只做静态决定，未运行Go、DB、profile或修改产品。

这不是把本次normal PASS当稳定性能证明，而是完成已采用局部身份复用后尚缺的独立race资格。现在没有新的、足够具体的纯重复证据支持再动产品；继续猜测优化或重复细分会把验收推迟，且不能建立因果。

## 2. 本次实际事实与cutoff

全文读完373行 `context-final-capacity-b-publication-phase-normal.log`；首次工具大输出截断处已分段补读。SHA256 `3fb9d0e75bcc66c7aff0baca0c8a45849baac62d0edc1b6d0378cd63db412b82`。实际native3458066/start14483911/session69469，raw末尾PASS和`NATIVE_EXIT status=0 group_absent=True`；root已核同一真实退出。测量manifest `0d5455a89252da54ccb404640022f411eab5d67bf19233e3fab9e5f2cca5895b`，normal binary `c421c12ceee557fa59cc44620ca8196bbe7c9649c83b33285b473a988cb420fd`，22469063B/dev27/inode560558。

已读manifest、binary provenance、publisher/processing/recorder三个准确diff与有限格式资格。七个overlay映射是原五份加publisher/processing记录；其中原四份world/test替换byte-equal，recorder只新增有限记录。格式proof中原先全局分号处理是历史有限证据，后来的精确分号例外仅针对新增helper，不能把它说成产品任意token改写被允许。root已做逆变换byte-equal检查；本轮静态diff未发现新业务分支/SQL/截止时间变更。

**本次读取时 `normal-outcome.json` 仍写“registered before effect; no outcome yet”，manifest/provenance的business_unrun也是各自生成cutoff。** 本文实际业务PASS来自随后raw与root完成资格，不能把早期字段冒充已更新outcome，也不由raw推断owner已完成所有清理/Release记录。root正式放行下一LOCAL前按现流程核完成；不需要另一次native“确认PASS”。

当前HEAD `28e3a468c86d4323d3b497836de23bb311a58952` 加以下四项WIP；它们与前次normal失败的产品源码一致，不称pristine28e或新已发布pin：

- closure.go SHA `8542696150191da3fafd6b0df90e38f272fd934de807317442933c536fb1331c`。
- context_dispatch.go SHA `5c2b8d472fdbe458b9affb9a9543db3359c85c0dfa90d5357bea843995e624fb`。
- content_context_publication_cause_test.go SHA `e944e3439eabe84936eb166e1966b178166fd7e1f7b8a1972139b96b9b2768d0`。
- context_publication_probe.go SHA `39051536517c23e30d1c80e919824d9c509e2d773a8e0568fe786fe82f9d5517`。

| 本次原case | 真出口 |
|---|---|
| closure64 | PASS10.93s；Step3.136633840s；running COMMIT7.438684609、Prepared8.996331446、Completed10.538033785；两Publish/两ReadPublished及原公开尾部通过；最后成功Claim尚余1.903148s |
| closure65 | 原overflow拒绝及reopen尾部PASS5.54s；父测试16.46s，不把父时间当64单项时间 |
| static62 | PASS15.72s；compile/bind6.144216495s仍计原caller；Step3.288252381s；Prepared12.178478255、Completed13.907291718；两Publish/两回读及原公开尾部通过；最后成功Claim余1.756678s |
| Content单体 / duplicate selector | 原正常及拒绝全部PASS；没有省掉原边界对照 |

真实正常流无新cause，记录drop0。预期65的context_overflow仍是拒绝事实，不称“所有case从未出现任何错误”。最后成功Claim余量是那个DB gate的观测，不是未来任意调度下的最小余量保证。

## 3. 这次测量能支持什么

每case分别有四次worker processing read：两次Publish内部verification，两次冻结worker独立ReadPublished。其前后closure累计分别约50–129ms，占相应短Tx主要部分；Objects.Read约80–236µs、integrity约12–44µs。这是**新map源码的NORMAL样本**，不是race成本，也不是旧CPU样本升级版。

worker精确归属的Put两次：closure64约374/268ms、static62约274/288ms；Step两次约134/130ms与177/217ms。没有再把compiler的四次Put/Step或全test Content计数归给worker。Publish、verification、processing Within/callback/closure这些inclusive层级不能相加。closure内部SQL等待、编码、调度等仍混合；“closure时间较大”不等于存在可删除的政策检查或已证实某个编码热点。

旧CPU决定 `2adc632b1eec485f8d7740e8fa4c97d4e5f92ee2f721257e25faf7ff7d2ea8df` 支持的局部identity复用已在当前source，旧profile没有此map。其累计Encode/VersionIdentity成本不能再当尚可全省的新收益，更不能把normal剩余约1.8秒线性外推为race一定足够。

上一份 `bb031cfa6f192e24026310b4706de0b1bae65f912aa00f72109d571f716649db` 要求的有限问题已有正常样本答案：这条路径能在原期限内走到Completed，实际重复current checks与两次实体回读均存在；本次未重现先前失败，**不能倒推先前111.6ms timeout发生于preTx还是postTx**。不以新样本将旧失败归因为jitter、预分配、共享负载或性能修复成功。

## 4. 源码复核为何不选第二改动

实际当前 `domain/content/closure.go` 与先前全文对象SHA相同；身份map仅一趟registeredClosure、完整ref键、只缓存成功、65项后原函数fallback。原target/sort/visit顺序、owner/ref/cycle/64上界、独立Store调用不变。`identity.go` SHA `e093a7918be9af0aeaec08c62b472de1141314812a71e3da386583237bbf60f0` 的原算法仍是先完整v.Encode，再原位置数组hash。现在没有证据支持改算法、跳Encode或把同tuple不同metadata合并。

`processing.go` SHA `32f14cd27d48a55069eed5a89aab39a3465f03d6b05189875be9d8382d9a6f32` 每次前后Tx各自重建完整current target+ancestors；中间真实Objects.Read/hash/length。前后两次同ref不是同一次资格，不能复用whole closure/政策或删除第二读后门。源代码的这些重复对应真实不同裁决时刻，不属于可直接删除的纯计算重复。

当前 PG `facts.go` SHA `8d53cb773a4711ff9866f5974b399de96eec137d82a92b97886c0825802cb069` 的LockVersion独立验证ref、advisory锁、FOR UPDATE取当前body/影子列再校验；已有fullRef相同的身份复用条件保留。它和domain identity检查是两个实际边界，不能为了刚看到closure累计较大就传绕检票据或取消PG验证。

PG `policy.go` SHA `3cb31490a70e0141daa24b4766295ea1a57598194877eda6f19e58371a2d45d9` 的CheckPolicy保原完整subject/tuple/purpose、MATERIALIZED锁行后clock、反序列化、五动作集合独立拒绝；subject完全相同的既有key复用也已存在。SQL集合取锁/合并Now会触及实际锁序与fresh authority，不是本次测量支持的纯等价微改。当前F1/fullRef、metadata/error优先级和所有Close cause保持。

Publish内部verification与worker ReadPublished各有职责；既有Current对完整Input/control/Permission的实时核验不能缓存。现没有一个在同一资格时刻、同一不可变输入上的新重复，同时有这次成本证据支持，因此 **KEEP现产品**，不因有耗时就新增port/cache/framework。

## 5. 一次race资格的具体默认

由root确认前轮完成/Release、所有原资源责任记录后，安排soleowner：

1. 固定当前四源及所有既有source manifest；可以先形成可审查commit或继续使用准确WIP散列清单，commit动作本身不创造测试资格。任何业务源码改变都不能冒用这里的normal样本。
2. 默认使用**已经构建、业务从未运行**的原五map非profiling race binary `0514d380b88d3bbc3c2985afbe946e9d5bc23be19a11c0826251b84421725d02`（provenance记录29058870B/dev27/inode560808），在root/owner确认binary/source/map仍准确匹配后运行一次。它含相同四项产品WIP和原低开销phase记录，不含新两处publication细分；这项差别须记清，不能称七map已race通过。无需为本次测量另造race测量binary；不把临时观察interface纳入产品。
3. 原四项exact selector、`-test.count=1`、Go120、outer120、每test原caller30/Claim5、原初次payload预算均保持。仍是新精确注册的独立资格scope，不能拿旧失败scope续租/重新计费/抹去Prepared。无preheat、无重试循环、不把setup搬出caller、不得删static62/64normal或65/单体/duplicate拒绝。
4. 保原两Publish/两独立回读、completed COMMIT、原固定receipt/正文/RuleStarts/reopen公开尾部；仅无DATA RACE输出或binary exit0不足以替代实际所有子测试和尾部。准确保存raw、binary/source/selector、PID/PGID/starttick、Wait/groupAbsence、所有Close/unknown责任。
5. 失败则保存首实际gate、原claim/caller余量、Prepared/publication责任并停止本次资格；不自动追加profile/probe、换原期限或换算法。通过则只记该准确source一次4B race出口，可继续本票已要求的受影响门禁/完整测试和review/CI；不是七AC或whole04自动通过，也不是p95/稳健性能证明。

原同四源normal失败日志SHA `4d40b79faf796680faa5eabb5c4832980b08b2b734feefc36e7abce201d4645c` 保留为真实失败；本次诊断normal通过不是产品red→green归因，也不能消除它所暴露的运行余量不确定。所有更早profile caller30耗尽、旧lease5失败各按原源码/观察保留，不能冒作新map源码的race资格。原CPU profile FD Close UNKNOWN与所有旧scope未知责任不动。本决定不接受ticket/whole/CI，不授权清理未知资源。
