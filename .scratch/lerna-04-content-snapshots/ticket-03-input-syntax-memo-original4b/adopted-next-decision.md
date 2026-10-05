# 03 Input memo 原四 B 后的唯一下一步（STATIC）

**暂不采用另一项产品优化。下一步只准备一次当前编译阶段的有限成本测量，区分纯计算与原真实读写；不盲重跑容量，不再扩大 memo。** 实际执行另由root授予唯一LOCAL；本次无Go/fmt/profile/DB/环境探测或源码改动。

## 本轮实际事实

全文读原 normal158行、race128行、已写出的两份results及race结果JSON；机器读取prelaunch/actual-outcome并比较current332，前后及实际WT均无hash差异。路径根 `/workspace/lerna-content-03-137311247276/`，CAP为 `large-graph-input-memo-qualification-overlay/`。

- normal raw SHA256 `dd12dbe11648ecb094267c8fb995c241b870ae2dccda1dd698476e0d56c12e9b`，26113B；actual PID4147966/t17398195，exit0、Wait/group空。closure64、static62均真实Prepared/Completed、Pub2/readback2及原公开尾完成。
- race raw SHA256 `95d15f6d7ce1ae2a0955fede6f5525c482b6481d2af1ef211244e96c92a89cba`，20202B；PID4162965/t17467188，exit1、Wait/group空、STOP_RELEASE。outcome SHA256 `c39be7d3dae12a0880cc19a3302914428f1ba5fd9c3bb650ed58b5d72e97e4f4`；binary `bc3fd7898b0c665ce9c1fbd7bbcbe551d845a9766fe17b1011c4522b00baa0c0`。
- closure64上游结束12.978534s，compile+bind16.859145s至29.867749s；独立锁观察在余132ms时开始，30.000776s返回deadline、零sources。没有dispatch/Claim/Prepared。零sources是此次失败返回，不是证明图缺少中间版本。
- static62 compile+bind11.576284s；Step5.079506s，Accepted/Running/Prepared均实际COMMIT（Prepared24.027724s）。首worker Publish的 `Access.Current` 返回deadline，caller仍余5.878936s；最后ValidateClaim晚31.326ms。按现源码与无丢事件，仅compiler四次Content.Put，**本次没有进入worker Content.Put**，故不能新造一个workerPut效果未知；旧轮真实Put UNKNOWN仍保留。没有本轮readback/Completed。
- 原65、200000、262144和duplicate尾PASS；normal成功不消除race失败，不证明净收益或PG原因。机械/语义N/R资格与容量分别记。

## 源码支持什么，尚不能支持什么

当前WT `/tmp/lerna-worktrees/content-snapshots-03` HEAD `b28b2e3182d97156c5fe203baf5cf2183631b7f2` 加manifest明确WIP。相关SHA256：`domain/task/context/compiler.go` `1833c2d3d8c989c01ad4c6c8dea447898303dddb5f71ebd9ba92e1e977c25dbe`；`canonical.go` `1350ffdc90bafc5b07a38ac96c9cdbad4020974eac4946d6785bd53fb19f0c63`；`selection.go` `7f1fd698ed9e53641b2b7351efe032f3a1af8cf6158dacb880fadf0ab515cd7c`；`adapters/content/decision/assembly.go` `81159617d649cead0c3ac2a6d7dd745753db3c07365d4eb8c99b3ee25ec62036`。

全文核Compile/canonical/selection/assembly/budget：Compile先原Observe/Begin，再ValidateInput，逐材料原Reserve→真实Content.ReadForProcessing→Confirm，EncodeMandatory、Plan、PublishBundle。确有纯重复候选：EncodeMandatory再次验证Input；Plan.DecodeMandatory又校验并调用EncodeMandatory；derivationProjection逐项重算selectionLayout。但这些分别也是当前边界验证，尚无当前成本证明值得改变其职责或表示。

旧CPU427样本的Compile10.85 CPU秒包含PublishBundle7.04、budgetReader5.36等相互嵌套消费者，不是10.85秒纯编码。旧Source纯Mandatory编码样本很小，也不能反推compiler成本。旧currentAttempt重新jsonBytes约186/208ms wall，不足单独解释本次16.859/11.576s编译；memo后当前剩余量未测。不把所有旧Validate/VersionIdentity CPU视为尚可再省。

当前Input仍在成功完整SQL/FOR UPDATE后按全文bytes使用成功语法memo；`currentAttempt`仍重编码完整Input、重算digest并核原tuple/revision/control/原attempt/当前clock。删除这些检查或改用任意raw JSON哈希，可能改变规范表示与错误语义，不能因“已有memo”顺手做。

## 唯一下一有限测量

在当前332和已核原5map基础上，只给实际 `Compiler.Compile` 增加固定阶段的诊断计时overlay：ObserveInput、ValidateCompileIdentity、BeginCompile、ValidateInput、材料循环整体、derivationProjection累计、checkSelectionBytes、EncodeMandatory、Assembly.Plan、Assembly.PublishBundle。保留现有compile/bind与worker粗粒度记录；不增加SQL、CPUprofile、环境probe或每行日志。每case固定cells，记录call/success/error、elapsed、未开始/未完成与drop；逐材料只累计既定projection cell，不产生动态key。

纯阶段与有I/O阶段明确分类；材料循环包含预算与Content，两者不可由余量假分SQL；projection是循环子项，不与父项相加。Plan包含原完整外壳/Proposal/publicEnvelope/closure测量，仍按原内容真实执行。时间用纯观测单调钟，不调用额外Store.Now，不移动任何业务语句、错误返回或绝对截止；observer开销UNKNOWN。overlay必须可逆证明非诊断字节恢复当前精确源码，不覆盖新memo/clone/clock/BodySeal。

固定一次原四B race、原全部输入/selector/count1、caller30（含setup）、Claim5、Go/外层120；不先warmup/normal、不延时、不将独立锁观察移出caller。保留原完整公开尾；新增stage即使PASS只作诊断，不替原非测量容量失败。prelaunch登记新binary/root/FD/manifest，actualSTOP/Wait/group与首Close证据独立；旧FD UNKNOWN不回填。

**测量出口可决定的事：** 若某一纯阶段在当前实际case中占明显成本，结合该准确函数的不可变输入及冷错误路径，才选择一个最小表示/重复计算修复并制定golden、冷热、污染、当前权限控制及原容量验证；没有“保证全部进入5秒”。若主要时间在材料循环/PublishBundle，不再优化未占成本的canonical/helper；也不能把其wall叫PG执行或网络等待。若采样改变实际失败门，保留新门，不洗旧门。到此测量即收口，不自动串联新矩阵。

编译提速即便成立，也仅可能改善closure64的caller余量；static62的原worker Claim窗口独立未闭合，不能把更早dispatch或未执行workerPut冒充修复。所有费用、次数、累计读取预算、完整SQL/policy/clock/fullRef/Seal/Gone及真实两事务门禁保持。
