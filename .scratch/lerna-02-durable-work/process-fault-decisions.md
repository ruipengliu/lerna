# 切片 02 票 08：进程及提交确认故障边界

2026-10-03。用户授权的 `gpt-6-astra`、`high` 决策代理保存初稿，主任务核对并采用为实施准备。未启动08、未运行新故障实验；直接前置仍是04，不增加05/06/07的依赖，也不让08承担全片最终关闭。

依据：票08七项AC、本片decisions与原身份/短事务决定；实际 runtime.Admit、PG/SQLite Within、PG CommitProxy 与已存在的PG确认丢失测试、03 Claim工作实现；04 SQLite work为活跃草稿，仅只读参考。采用仓库 `.agents/skills/tdd/SKILL.md` 与 `mocking.md`：用户持续授权代决策，本片已选定Host/存储端口和公开查询观察边界；不重复要求人类确认，不把内部mock当真实数据库证据。

## 1. 最小方案与可完成范围

决定采用以下三个**分别命名**的层次，不把它们混为同一种“提交丢答复”：

| 层次                                               | PostgreSQL                  | SQLite                             | 能证明什么                                                                                  |
| -------------------------------------------------- | --------------------------- | ---------------------------------- | ------------------------------------------------------------------------------------------- |
| 真实事务提交前 SIGKILL                             | 必须                        | 必须                               | 原事务未提交时，无成功回执/部分业务和Job；同原身份可恢复处理                                |
| 真实 Commit 已确认，Host业务答复尚未发送时 SIGKILL | 必须                        | 必须                               | 客户端没收到成功不代表数据库没保存；重开查原固定决定与唯一责任                              |
| COMMIT确认在数据库协议上传递丢失                   | 复用真实PG wire proxy并运行 | 嵌入式SQLite无这一网络协议，不伪造 | PG native driver确实未收到已提交的server确认，Host正确保留unknown                           |
| 真实提交后，存储端口不交付确认                     | 可共用但不必重复堆测试      | conformance-only装饰真实TxRunner   | SQLite真实数据提交与应用消费unknown/原身份恢复协同正确；不证明native sqlite3 Commit真的报错 |

**接受 SQLite 存储端口确认丢失实验作为这一层的最小故障。** 不添加 VFS、ptrace、定制 driver、fake SQL rows 或测试专用产品 Commit hook。SQLite 原生提交错误、掉电和磁盘错误仍明确未验证；本票不以它们作为退出前提，也不得在退出结论宣称已经验证。

理由：票08要求准确记录实际注入边界、两库进程故障与原身份恢复；并未要求SQLite拥有不存在的网络确认或穷尽所有engine错误。真实PG wire证据、两库真实进程证据以及SQLite真实事务后的端口消费证据共同覆盖当前承诺。若未来要声明 SQLite native I/O错误分类/断电耐久，必须增加对应真实故障实验，不能引用本票替代。

## 2. 存储端口装饰器的严格范围

装饰器只存在于 conformance/testkit 或故障子进程装配中，包裹已有 `runtime.TxRunner` 系统存储边界，不能替换业务接纳、CommandStore、JobStore或SQL driver。它不是新的生产业务API。

一次明确指定的业务接纳调用可采用：

1. 以原ctx/owner/callback调用实际adapter.Within。callback原样执行真实输入、固定receipt和Job写入；保留真实Tx token/scope校验。
2. 实际Within返回错误时原样返回，不把callback失败、明确提交前取消/回滚等确定失败改名为unknown，也不假称Commit已成功。
3. **只有实际Within返回nil后**，故障设施记录其已见真实确认，但不把该确认交给消费方，而向上返回包裹 `runtime.ErrCommitUnknown` 的fixture错误。只拦截预定业务事务，不误拦初始化、迁移或观察读取；无需按私有SQL文本猜事务类型。
4. 经真实 Host.Record / runtime.Admit 得到 `TransportOutcome.commit_unknown`，保留准确原CommandRef和query_or_retransmit_original。测试不能直接手造TransportOutcome来满足断言。
5. 停止/关闭原writer后，用独立Store和真实数据库重新装配Host，通过原command.get与Observe核实receipt/输入/Job，再重传原raw命令确认不重复接纳。

故障设施本身“知道成功”不矛盾：故障测试的独立观察者可以知道数据库已提交，而被测消费方缺少该确认。但名称和证据必须写“storage-port confirmation loss after real commit”，不能写“sqlite3 Commit返回不确定错误”“SQLite COMMIT网络ack丢失”或“原生driver分支已覆盖”。当前SQLite adapter把实际Commit错误归为ErrCommitUnknown的代码路径，不会被这个装饰器直接执行；其原生错误路径仍未做故障覆盖。

这仍使用真实DB，不是用固定成功rows或伪driver替代耐久行为；测试断言真实Host输出与重新读取的业务事实，不断言装饰器或repository调用次数。与该故障配一个不丢确认、正常收到固定receipt的对照。

## 3. 双库真实子进程同步点

可以复用现有 transactionGate 的已有TxRunner消费边界扩展进程设施，不在产品adapter加“测试专用阶段回调”。所有故障暂停仅出现在testkit中，有有限ctx及父进程清理；不得把故障暂停设计变为正常业务事务等网络的做法。

### 提交前

真实Within的callback完成所有业务/receipt/Job写入后，在callback返回之前发送独立控制管道事件 `writes_staged_before_commit`，等待父进程许可或终止。此刻真实adapter尚未执行Commit。父进程收到完整事件后SIGKILL并Wait子进程退出，再打开独立Host观察。

期望：查询原command为not_found，没有成功输入/Job的部分保存；重传原命令可在仍有效的原接纳期限内正常接纳。不得把明确未提交的实验报告为commit_unknown。若子进程提前退出、超时或未到同步点，实验失败，不能按预期回滚算绿灯。

### 提交后、Host答复前

子进程只在真实Host.Record成功返回received且真实adapter确认Commit后，向独立控制管道发送 `commit_confirmed_before_host_reply`；业务答复通道仍被门闩禁止发送。父进程收到该阶段后SIGKILL。控制管道事件是测试观察，不交给被测调用方当成功业务答复。

重开后查询应得到准确固定applied receipt及原输入/Job；原命令重传返回同一决定，revision与Job身份不增加。正常对照释放门闩，成功业务答复可完整到达。不能把这个阶段称作adapter未知：这里adapter和Host已知道成功，丢失的是最后的Host业务答复，调用方只知道结果尚不可判定。

父进程可先确定command_id、输入和预期独立黄金；它不能从控制事件复制一个未真正查询过的receipt作为“恢复证据”。实际恢复由原公开command.get和内部Host观察完成。

## 4. PostgreSQL 既有wire证据的复用

当前 CommitProxy 转发真实COMMIT请求，观察服务器的 `CommandComplete(COMMIT)` 与 `ReadyForQuery`，隐藏确认并断开客户端连接。实际PG adapter因此经历真实Commit错误，runtime/Host返回commit_unknown，独立直连observer能读回原提交。现有 `TestPGCommitConfirmationLossPreservesUnknownAndOriginalIdentity` 是这一边界的真实证据。

票08应在本次准确集成代码/数据库配置下运行并引用它，无需再做一套相同proxy或把它冒充SIGKILL实验。server尚未确认Commit就断开属于另外的不确定情形，不能伪写为“已提交但ack丢失”；当前明确提交分支足够证明原键恢复必要性。

## 5. SQLite 单写边界与独立观察

实际SQLite Store 对数据库文件持有进程级flock；Within使用受控writer、真实文件、WAL、连接有效FULL以及有限事务。父进程不能在子writer活跃时另开第二个写Store绕过排他，不能为观察关闭/删除锁文件、改driver路径或直接查私有业务表。

进程故障：SIGKILL后必须Wait确认退出，随后新Store以同一数据库文件取得writer权威，再通过Host/command.get观察。正常端口确认丢失：原writer把unknown交给测试调用方后，真实Close完成再交给独立新Store。由另一进程/独立Store读取事实已满足恢复观察独立性，不要求违背SQLite单写约束的两个同时写Host。

数据库/WAL文件保留原状态用于恢复，不手工checkpoint、复制或重建数据库来“修复”实验结果。操作系统锁释放与SQLite恢复都走现有正常路径。缺服务、文件不能打开、子进程未真正退出或迁移不兼容应明确失败。

## 6. 跨进程Claim接替与迟到旧结果

Claim/Job/owner与新旧修订规则沿03/04，不重新定义。需要证明的是旧Claim即使携带真实旧结果到达，也不能越过已经接替的epoch；不是强制旧进程停止或外部fencing。

最小共同脚本：

1. 子进程A经真实Host worker取得原Claim和准确输入，事务已经结束；通过有界管道输出该原Claim及实际计算的投影结果，父进程把这条待完成消息留在传输门闩后，不立即提交。
2. 终止A并确认释放Store；按共同可信时钟进入lease到期后的阶段。子进程B用原数据库/owner领取同一Job，取得更高epoch，按场景处理原修订或新增修订。
3. 在B已接替后，父进程投递A先前实际产生的旧完成消息，经当前真实Host worker.Complete边界处理，必须拒绝旧Claim；最终Host观察证明未覆盖新结果，也未消除新work_revision。
4. 配对正常场景在旧Claim有效且没有新epoch时交付同样形状的消息，能完成准确投影；另按已批准脚本验证旧修订完成与新触发两种提交顺序。

这是真实跨进程Claim与**迟到旧worker完成消息**，不宣称已杀死进程稍后又执行了代码。SQLite尤其不应同时开两个写Host来模拟旧/新worker。若额外测试活着但暂停的计算worker，应把计算worker与唯一写Host分开并明确借用该Host端口；当前最低方案无需新增产品RPC服务或绕过writer锁。

## 7. 可信Clock与有限管道

测试时钟可由父进程作为明确fixture权威，记录每阶段的准确UTC时刻和递增clock generation。子进程通过受信装配取得该阶段时间，不从record payload、自报Claim时间或各自未协调的本机时钟决定资格。参与同一竞争阶段的进程先确认同一generation，再执行有限操作；只有进入下一阶段且相关事务结束后才推进时间。

最小接替脚本在A退出后启动B时注入已记录的T1（不早于原lease_until），迟到消息的验证使用当前B的时钟，不接受A保存的旧now。无需在SQL事务内远程请求父进程时间或添加时钟网络服务。正常PG默认数据库时间/SQLite默认设备时钟仍保留；这里只在已有Clock端口注入共同fixture时钟。

故障阶段等待的物理截止使用真实进程/context时间，不用被冻结的fixture时间决定是否清理。每个管道frame有固定最大长度、准确scenario/stage标识，读取与写入都有有限上下文；EOF、重复/错阶段、子进程非预期退出、超时都显式失败。子进程、goroutine、管道和连接由父进程统一有限关闭/kill+Wait。不要用长sleep猜租约或提交点。

## 8. 验收报告与TDD执行

按一个公开行为tracer建立故障用例，再补最小设施/必要实现，不一次造完整泛型故障框架。先写未满足的可观察场景/设施入口并记录真实red；已有行为已正确且新测试直接green时如实记为新增覆盖，不故意修改断言制造假red。

每条证据至少记录：准确代码/driver/数据库运行版本、PG有效隔离和同步设置、SQLite有效WAL/FULL及单写范围、原命令和对象身份（无敏感正文/凭据）、有限截止、实际同步阶段、注入层、故障前后Host查询/业务观察、正常对照和清理结果。不得用私有表查询或内部调用次数判断业务持久性。

可准确宣称：两库真实进程提交边界恢复；PG真实协议确认丢失；SQLite真实提交后的存储端口确认丢失下应用原身份恢复；跨进程原Claim/epoch隔离。必须同时列明未验证：SQLite native Commit异常分支、SQLite VFS/I/O/掉电故障、磁盘丢失、异地/3AZ耐久、生产RPO0和任何外部副作用的恰好一次。

上述未验证项不阻塞当前票08的准确边界验收，不允许被摘要省略。04退出后方可开始08；root等待全部票据后统一整片审查和退出，本票没有隐藏依赖。

补充观察约束：当前Host.Observe对底层缺行可能只给读取错误，不能把任意dependency_unavailable当成“业务对象不存在”。提交前用例先确认原command.get的准确not_found，再按仍有效的**同一原创建命令**重传并得到首次applied/revision1、一个原对象阶段Job和一致观察；若部分输入/Job曾泄漏，原有创建前态/Trigger约束应使该正常恢复不成立。若04已有明确的内部absence观察可直接复用；不要为了这条测试绕到私有SQL或制造一个把所有错误都当not_found的新接口。
