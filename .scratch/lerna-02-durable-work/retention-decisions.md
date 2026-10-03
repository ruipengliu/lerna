# 切片 02 票 07：真实正文清理、墓碑与升级

2026-10-03。用户授权的 `gpt-6-astra`、`high` 决策代理保存初稿，主任务核对并采用。仅为实施准备，07仍只依赖04；除经root授权的隔离schema恢复工具探针外，没有运行清理／升级产品实验，探针不算07验收通过。

依据：本片spec、issues/07、decisions/admission-decisions/layout-decision、真实command metadata/digest/receipt存储、PG/SQLite v1 artifacts、已合入PG Claim及04 SQLite工作草稿。04草稿即使suite已绿仍以root实际集成为准，不冻结函数名。

## 1. 清理什么，保留什么

现在没有原Command正文表。`command_receipts.metadata`只有方法、主体、目标、原期限/前态等去重和查询所需信息；digest与固定receipt同样是最小身份依据。不能为了验收新建一份无用途Command body，再把它删掉。

真正可清理的大正文只有 `durable_inputs.text_value` 的当前project输入。决定仅提供**准确已完成输入修订的正文清理**，不提供任意表TTL、对象删除、墓碑删除或全owner批量扫描。

必须长期保留：tenant/owner/command_id、准确原摘要、方法/目标/必要主体和期限/前态信息、固定原receipt；对象identity/current input revision；原Job identity及工作/完成修订；真实成功投影的输入修订与hash。正文清理不增加输入revision，不创建project工作，不把Job换ID，不更改原receipt内容，也不推断任何Task或外部效果。

## 2. 最小受信Host seam与授权

消费者仍是 `internal/durableworkdemo`，Host只装配；adapter实现这个消费方实际需要的清理端口。最小内部操作语义为：**清理原 CommandRef 所绑定的准确 applied 输入修订正文**，输入包括原CommandRef、expected input revision、受信SubjectBinding与有限context。Go方法名由实施者按04集成结果选择，不新增公共1.0方法或改变record payload。

在现有精确SubjectBinding+OwnerRef权限表中增加单独的 `cleanup` 能力（例如一个显式bool），默认拒绝；record/read权限不能自动授权删除正文。主体仍来自Host受信上下文，完整delegation_chain按既有规范绑定。先鉴权且核对目标owner，后查记录，不向无权主体泄漏存在/gone/当前revision。过期/取消context、未知/跨租户/错owner同样fail closed；不增加完整授权服务。

清理原command必须是这个内部record方法的成功applied，receipt目标为准确durable_work对象，receipt revision与调用方expected revision相同。rejected/其他方法没有这个可清理正文，明确不适用，不把“没有正文”伪装成已删掉大正文。当前方案不处理任意老command的批量归档。

操作可返回内部明确结果（cleaned/already_gone、前态变化、仍有工作、无权/不可用），不需要为管理操作创建新的公共固定CommandReceipt，也不把这些内部分类塞进冻结的ErrorCode枚举。重复相同原引用及修订是幂等的。

## 3. 必须满足的事务前态与真实字节删除

在同owner短事务中按既有顺序锁定原command key→业务input→原project Job，读取真实状态并一次完成：

1. 原command绑定正确，expected revision为其实际applied revision。
2. 当前输入revision仍等于该revision；不能拿r1清理r2的正文。
3. 原project已完成到当前work_revision/input revision，Job为done，无当前有效/未交回的Claim或未结新修订。当前04的成功投影也须绑定该输入revision；本票先限定成功project的可清理路径，不依赖05新增永久失败/取消语义。
4. 实际清空当前text_value，保存“该输入正文已清理”的状态，并将**该准确原command的读取墓碑**一起标为gone。上述变更同事务成功或回滚；仅置gone标志不删除实际字节不算实现。

最小跨库物理表示：沿用原 `text_value NOT NULL`，写入真正的零长度bytea/BLOB，并新增明确body_gone标志。有效空字符串仍是body_gone=false；清理后是body_gone=true，两者不可混淆。这样不必为SQLite移除NOT NULL而重建带Job外键的整个父表。应有真实DB约束 `body_gone => stored text length = 0`，读取观察也核验一致性，不能仅在序列化时遮住未清理的非空值。若当前adapter已有更合适的nullable表示，可选等价准确方案，但不能省略真实存储清空。

清理状态/命令读取墓碑需要实际向前迁移（通常v3或届时下一个未占用编号），不能改已发布0001/0002。没有必要新增历史正文表、清理工作流或全局retention框架。必要的轻量清理时刻可保存，但不作为删除去重身份的期限。

未结ready/leased/waiting/backoff、旧Claim完成但有新work_revision等状态均拒绝当前清理，保留正文/Job。租约过期不表示责任已结束，不能因此允许删掉待处理输入。已有成功投影与固定回执可继续保留，清理不抹证据。

## 4. gone、重传与后续新修订

内部Host观察显式区分正文present与gone：present可以包含准确空字符串；gone不再返回旧正文，并保留对象/input revision、Job与真实投影信息。不要用空字符串或dependency_unavailable代替body-gone。受信清理观察需能证明实际保留字节数为0（可由adapter实际读取的字节长度形成观察，或由真实DB约束和读取一致性校验保证），不能把标志本身当作已经删除字节的证据。

`CommandGetResponseGone` 已是1.0准确Schema，只有status和原command_ref，够用无需扩合同。adapter的只读command查询在本command的gone标志存在时返回它；调用链保持原鉴权和准确引用。这里gone表示所绑定正文已按策略清理，内部仍保留不可删除的去重及固定决定依据。

`LockCommand`/接纳路径不能把gone当作记录不存在。原raw record重传在当前权限仍允许时，比较原digest并返回原固定receipt，不回写正文、不再触发Job；变更正文/期限/主体/前态等的同键请求仍为idempotency_conflict。原期限已过去同样如此。receipt仍固定，gone只是当前读取保留视图，不是把applied改rejected。

同对象允许用**新command_id + 精确当前expected_revision**提交新text。它保存新的输入revision、恢复这个新revision的正文present并触发原Job的新work_revision；旧command记录的gone标志必须独立保留，不能由对象body_gone恢复false而让旧command重新found。新command自己的查询返回新决定，旧command仍gone，旧command重传仍只返回旧固定receipt且不覆盖新正文。

当前最小清理操作一次只归档其明确指定的原command，不顺带遍历/重写该对象所有历史命令。已经被后续revision替换的旧正文没有单独保存，不能以它为理由清掉最新正文；对未经清理标记的旧command请求返回revision_changed/不适用的内部前态结果。该限制是明确范围，不伪称全历史正文归档服务。

## 5. 并发的两个合法次序

- clean(r)先提交：r正文实清且old command gone；随后新record以expected r正常建立r+1/present/原Job新责任。再次clean原r得到幂等already_gone，不得触碰r+1。
- 新record(r+1)先提交：原r清理发现当前revision变化，明确拒绝，r+1正文和Job完整保存。不能先读r、脱离锁后清空最新行。
- query与clean并发可线性化为found或gone，但不能not_found、错误原引用或半份receipt。重传与clean无论先后都返回同一固定原receipt且不加revision。
- Claim/完成与clean竞争按input→Job同锁序约束；未完成不能被清理；成功完成提交后可清理。拒绝与成功对照必须经真实两库并发同步点验证，不能仅串行调用假称竞争。

query的gone标志和receipt读取须为同一一致观察。清理不要锁多个无关command再倒序取input；一次准确原command已足够，无需引入新的跨对象锁图。

## 6. 实际v1→v2→当前迁移输入

使用已保留的真实writer artifact，不手造旧表/行、不拿最新migration建库再倒填旧数据：

- PG writer准确源 `988f8b7ec2a8fd3a28db44b11cf5a863af4593b2`，完整plain dump，原pending Job、applied与expired固定决定及真实checksum。
- SQLite writer准确源 `f4fb0576bc0a1e3371fb88ab43f729beb9ddf118`，完整已关闭database.sqlite。先校验SHA256SUMS，复制进当前测试独占临时文件；从不在checked-in文件上打开writer。
- 实際v2是03/04的Claim功能迁移。本票实际retention列用后续迁移；测试从真实v1运行当前Migrate时可以自然经过v2再到当前版本。公开/内部migration观察要保留v1/v2准确checksum；不为捕捉中间态新增通用任意版本迁移API或无用途迁移。
- 升级后经公开command.get读回原两份固定receipt，通过Host观察原input、原Job identity与pending修订；worker继续处理该**原Job**并读回准确hash。重开/原command重传不重复责任，再验证本票真实正文清理。新迁移不能重写v1/v2 checksum或让未结旧Job失去正文。

默认以版本化migration清单自然排序应用；若05/06先合入并占了版本号，集成时使用下一个准确编号并保留实际路径，不因此给07增加业务依赖。07可在04基线上独立实现真实retention迁移。

## 7. 迁移失败的真实故障边界

采用测试DB的schema环境故障是可行的最小方案：在恢复的v1上安装一个明确test-only的schema_migrations BEFORE INSERT拒绝触发器，仅拒绝version=2记录。真实Migrate执行v2 DDL后记录版本时触发真实数据库错误，事务必须回滚。SQLite可用RAISE(ABORT)，PG可用当前测试schema下的受控函数/trigger；不更改产品SQL、driver返回或业务行来冒充失败。

随后通过已有migration观察确认只保留v1；公开原command读取仍正确。移除这个准确故障trigger/函数后，在同一真实库正常重试Migrate，必须成功、准确v1/v2 checksum仍在、原工作能继续。若v2 DDL曾半提交，正常重试的DDL/约束通常会真实失败；不能通过IF NOT EXISTS吞掉残留而掩盖半迁移。

安装/移除触发器属于已声明的测试数据库边界控制；业务验收仍走Host/command.get，不以私有表查询或fake rows判断原业务事实。失败前后、移除故障和正常对照均有有限上下文。无需新建产品migration失败hook。该实验验证这组事务型DDL/元数据写入的回滚和重试，不声称覆盖磁盘掉电或一切迁移错误。

## 8. PG plain dump恢复工具：已实测选择

决定使用**真正的psql处理完整可信plain dump**，不把它交给database/sql.Exec，不手写COPY/psql元命令解析器，不为此安装新的客户端主版本。

本轮实际结果：

- 客户端 `psql (PostgreSQL) 17.11 (Debian 17.11-0+deb13u1)`；服务端 `18.6 (Debian 18.6-1.pgdg12+2)`。
- 校验原SHA256SUMS，完整dump SHA256为 `1d2195606544b288e14a0e2819844ee187b07e097fe7e96f83513013f1ad8b60`。
- 原dump中的 `\\restrict/\\unrestrict`、COPY与其终止标记均保留，实际psql恢复成功，输出COPY行数依次2、1、1、1。
- 通过当前受信公开GetCommand分别读回applied-original的applied/revision1和expired-original的rejected/expired；已有MigrationVersions观察仍只含原v1及准确checksum，未执行任何升级。
- 结果文件 `/tmp/lerna-retention-restore-wnx0k5it/result.json`；临时探针 `/tmp/lerna-retention-restore-probe.py` 与 `.go`。本轮随机schema已成功清理。第一次完整恢复同样成功，但观察helper的Go字段类型编译错误；修正后重新完整运行成功，两轮各自创建范围均清理。没有把第一次不完整观察记作全探针成功。

安全恢复流程：读取受信版本化artifact和hash；生成符合配置规则的随机schema；先单独CREATE成功并登记owns；在/tmp副本中仅删除原来唯一CREATE SCHEMA语句、替换准确固定schema标识，其他原身份/字节/COPY保持；运行 `psql -X -v ON_ERROR_STOP=1 --single-transaction --file <owned temp file>`，父进程施加有限超时；最后仅在owns=true时DROP自己创建的schema。不得drop整个数据库或根据随机名字猜创建成功。原fixture fake数据不含被替换schema标识，校验固定artifact保证这次简单替换适用，不宣称可变SQL任意重写工具。

凭据只从root授权的mode600专用配置读取进子进程环境，不打印、不传入仓库、不读取services.env。没有假设18.6恢复CLI已安装；当前18.6生成器版本说明与17.11恢复器验证分别记录。正式07需更新相邻README对“必须18.6 psql”的过强描述为准确已验证工具矩阵，并在本地/CI提供并记录可执行psql版本。缺工具或恢复失败必须硬失败，不skip绿灯。环境个人wrapper路径不成为仓库依赖，按PATH显式发现/版本检查即可。

本探针仅证明这份受信18.6 artifact能由17.11客户端恢复到18.6服务端并读回原固定决定，不证明任意未来dump兼容、不证明v1→v2升级或07清理通过。升级和pending Job继续处理仍由正式07在真实两库完成。

## 9. 双库最小验收与限制

共同行为套件至少包含：有非空正文的成功project→清理→实际body gone/原command gone；原raw重传原receipt不增revision；changed同键冲突；清理后重开上述语义不变；合法空正文与gone不同；pending/有效Claim/租约已过但责任未结均不可清理；同对象新record后旧command保持gone、旧重传不覆盖新正文；clean/newrevision和clean/query/replay的受控两种顺序；跨tenant/owner与无cleanup权限拒绝；正常未清理对象继续完成。

升级路径用两份真实v1 artifacts，运行实际v2/当前迁移、故障回滚/正常重试、原引用查询和原Job继续处理。保留原artifact哈希、writer源、migration checksum、客户端/driver/server版本、配置和准确命令及结果。未完成07前不得把本工具探针或04 suite绿灯写成07完成。

正文清理指live数据库记录不再保留业务正文，不承诺WAL、MVCC旧页、备份、复制、内存、客户端原请求或日志已做取证级擦除；不为本票加入VACUUM/secure_delete/备份销毁体系。最小身份与摘要/固定receipt按既有决定继续保留，本片没有墓碑回收。普通目录和存储表示细化无需新ADR；不改领域不变量，不等05/06才交付07。
