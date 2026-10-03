# 05: 持久等待、有限退避与丢通知扫描

**What to build:** 工作者不依赖通知恢复责任，等待与退避释放资源，到期后继续原 Job。

**Blocked by:** 04 — 双适配器工作接替

**Status:** resolved

- [x] 丢弃全部唤醒通知后，真实 PG／SQLite 的有界扫描仍恢复已接纳工作，健康正常对照也完成。
- [x] 未来 due_at 不被立即领取或忙轮询；持久可检查等待条件／再检查时点，释放 Claim、worker 槽位、连接与事务。
- [x] 重开后仍保留原 Job 等待／退避阶段，沿保存的条件继续，不从任意函数入口重放或创建同义工作。
- [x] 有限执行期限、最大退避与尝试边界生效；明确完成、等待、有限重试和永久失败，Schema／权限／前态错误不无限重试。
- [x] 等待及失败只记录演示事实，不裁决不存在的 Task 终态，也不推断外部效果未发生；固定接纳决定保持不变。
- [x] 可控可信时钟及同步点验证到期、事件变化、取消与恢复，不用长 sleep 或私有函数次数冒充无忙轮询保证。

## Comments

2026-10-03，票05产品提交 214f713，合入08与已发布行为决定 c95cdc4，08严格启动适配 e533a64。独立 worktree/branch 从 clean integration b1674b2 开始，实际读取 implement-spec 与 tdd/SKILL.md、tests.md、mocking.md，沿已授权真实 Host/Tx/storage、Clock/Timer、Host Observe/ObserveSchedule 与 public GetCommand seams 验证，不查询私有业务表或用函数调用次数充当进展证明。票06容量/公平配额未实现；切片02整体继续 in-progress。

**实现与门禁。** Record 在原接纳事务为准确输入 revision 绑定不可变有限 policy、同对象固定 lane 和绝对执行 deadline。可信内部 fixture 配置强校验、有限、精确 owner/object 绑定，不来自公开 payload 或领域环境变量；后续配置不能覆盖 claimed policy snapshot，默认正常纯 project。业务在 internal/durableworkdemo，Host只装配，runtime/adapter提供机制。实际 Start 必须满足显式有限 worker allowlist、准确 Claim/epoch/revision/lease、已绑定 policy、当前资格/停止/gate/deadline/attempt；只有真正允许启动才在原事务登记 attempt/StartEpoch，同epoch确认幂等。事务外计算后 Finish，包括保留的 Complete 成功便利封装，要求已有该准确 Claim 的 Start，不暗中补登记；缺权限/能力 fail closed。Renew 不增加 attempt、不越过原执行 deadline。04共享13故事和08进程故事业务成功均接入真实 Start；旧epoch/篡改/期限负例先合法 Start，配真实成功对照。Record/worker共享同owner可信Clock，修复原2026接纳/2100处理混钟前提。

**等待与关闭。** 同owner gate_revision_at_least 控制事实持久、单调，必须显式可信控制装配。未满足gate或有限指数退避持久保存条件/理由/due，释放Claim、worker执行、连接和事务。有界scan沿原Job恢复，无通知依赖；NextWake与最多1s fallback用事务外有限相对Timer，不忙扫被其它执行者占有的已到期工作。默认执行5min、最多3次实际启动、100ms基础/5s最大退避、1s再检查，各可配值有硬上界。success/waiting/retry/permanent/expired/stopped记录准确原revision；completed_revision是处理责任关闭，只有真实success写Projection。旧关闭保留新work和以前成功Projection，不造失败hash、不裁决Task终态。ctx取消只做1s detached best-effort归还原Claim；失败保存原责任待有限lease接替，不推断外部效果未发生，与可信业务Stop分开。

**决定与legacy。** 按 scheduling-decisions.md、legacy-scheduling-decisions.md、processing-gate-decisions.md 实施。缺policy不是无限执行许可；legacy首次真实eligible接管事务与Claim一起固定legacy-adoption、trusted adopted_at和adopted_at+5min，不追溯旧created/updated/accept_before或migration时刻，不因重启/epoch刷新。保留旧v2 work/claimed/completed/epoch与原lease到期，再领取当前revision；不造缺失历史正文/成功，历史attempt不从epoch推测。部署先drain/隔离旧binary，不混版本。

**真实旧来源。** 新pg-v2/sqlite-v2冻结夹具来自不可变实际writer b1674b2d0252da73f3dfd753857484597dc0a080。保存生成源v2-writer.go.txt/脚本、原0001/0002、真实metadata、returned Claim/Host observation、完整pg_dump18.6/关闭后SQLite文件与SHA256SUMS。writer真实Record r1→Claim r1(1s)→Record r2，不手写旧表/重建旧事实；原v1 sourcefixtures、已发布0001/0002未改。PG05 loader执行完整SQL与native COPY，仅忽略客户端restrict/unrestrict指令；只替换已登记own randomschema identifier及省略已经创建的CREATE SCHEMA，保留完整DDL/所有数据。05 loader无psql依赖，whole suite独立07历史v1恢复仍有mandatory psql前提。必需验收仅读冻结来源/checksum，不依赖shallow CI的Git历史，Git archive仅用于显式生成复现，未改fetch-depth。真实v1 pending原Job首次以当前可信时间绑定期限；真实v2原r1 Claim/work r2在lease前不可接管且无policy，到期后同Job/higher epoch领取r2、固定期限、真实hello投影，late r1拒绝，原公共回执仍正常查询。

**TDD与测试。** wait_test.go共享同一真实PG/SQLite观察suite，PG业务连接实际上限1，SQLite单写，等待期间健康Record/投影与fixed receipt均正常。覆盖通知全丢、未来due/有限Timer停驻、资源释放、重开、policy snapshot、事件变化与unknown/regressing gate拒绝、有限retry成功/attempt耗尽/permanent schema/期限到期、真实撤权、缺Start/权限旁路、真实Start提交后确认未知与同epoch幂等、启动后期限阻止success、ctx取消恢复、旧失败/停止保留新工作/旧成功和真实v1/v2adoption。未知Start/取消注入在真实Tx提交边界之后，观察以权威Host事实为准。首tracer缺wait API编译red；实现后PG时间解析+00失败而SQLitegreen，native sql.NullTime修复后双库green。Stop seam缺API red，最小实现后green。补充回归直接green者不伪记red。早期legacy恢复先释放登记scope再调用psql，实际因默认socket及清理资格失败；改为从创建开始登记独立finite scope、完整pgx恢复后两库green，未猜nonce/prefix清理其它scope。

**工具与限制。** Go1.27.1、Node24.19.0、pnpm12.8.1、pgx/v5 5.11.0、go-sqlite3 1.14.52、GCC14.2；PG18.6/READ COMMITTED/synchronous_commit on，Tx3s/statement2s/lock1s；SQLite3.53.4各连接WAL/FULL/foreign_keys on/busy100ms，Host Tx3s，suite120s。Clock/Timer可控，context与cleanup仍有限真实时间。DSN仅程序读600保护文件进入明确env，未打印/提交；各轮新登记TMPDIR在/workspace overlay，SQLite用自己文件，PG只创建/清理已登记own randomschema，不drop caller DB/smoke/未知scope。04历史一个丢nonce scope限制保留，不猜名清理。生成器也只清理自己登记schema/本轮源目录。不声称本票证明断电、native SQLite COMMIT答复未知、外部效果隔离、生产故障域耐久或远端CI成功。

**先行检查，最终整合结果待追加。** bootstrap、fmt、check（158双向合同fixtures/build）、test-race、mod verify、diff check通过。08整合前mandatory双库count1 integration15.738s，08严格Start整合后Process/StoragePort选择回归8.981s，真实冻结v2升级选择回归0.213s；这些不代替07合入后的最终whole integration/race。

**最终整合与验收。** 已merge root07产品0f27475及最后doc418415f；真实受测产品代码8a1df46，包含07正文清理/完整历史恢复、08进程harness的同Clock/显式Start整合。保留root已发布0003_retention与原0001/0002逐字不变，仅把本票尚未发布的wait迁移从0003改0004并更新loader、四版metadata/current身份；PG0004 checksum sha256:5cdc0cb11fa3aec15a496c8f19419a83929299db513d98d954f8b0281a7428d5，SQLite0004 checksum sha256:9b92318b12fe176c0d22508bf5763a760c17146675e20d9858c0d93731e1ff08。原两版checksum仍用独立既知literal核验，已发布第三版与新第四版也核验准确identity。对root的0001/0002/0003及v1夹具diff为空，v1/v2四套SHA256SUMS全OK。

最终实际通过 make fmt、make check（158共享fixtures正反序均Go→TS/TS→Go真实往返及build）、make test-race、go mod verify、git diff --check；mandatory make test-integration（-count=1，真实PG/SQLite whole recovery26.132s），以及 go test -race -count=1 -tags=integration -timeout=120s ./conformance/recovery/... ./internal/durableworkdemo/...（57.172s/1.658s）。07整合后首次whole integration编译red暴露自动合并产生的重复postgres import，修复后上述whole integration/race才green，未把失败轮记成通过。psql17.11版本工具前提仍由mandatory入口实际检查，原v1完整恢复路径保留；05新v2native loader不改变这一整体要求。最终各轮自己登记scope与overlay TMPDIR有限正常清理，04未知scope与07未知CREATE未启动容器限制仍保留。

六项AC已满足，06frontier现在打开；切片02整体未退出，06实际配额/公平门禁、两轴审查/架构优化及准确最终远端CI仍由后续承担。本票只commit自己的branch，未push、未rootmerge、未PR、未cleanup worktree。

Root合入f28b69d后按整个切片基线8e7438e复核diff，发现pg-v2/database.sql原始pg_dump末尾空行触发Git whitespace检查。本地working-tree diff空并不能替代该全range检查；已沿原pg-v1的精确.gitattributes保留策略为pg-v2增加-text/仅免blank-at-eof规则，未改dump任何字节或SHA256SUMS。全range diff随后通过，不改动其他代码的空白规则。
