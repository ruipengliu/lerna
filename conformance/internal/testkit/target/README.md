# 独立 SQLite 测试目标

这是 `conformance` 可见的测试设施，不是 production adapter、Executor Effect 账本或供应商 API。它不使用业务凭据、模型调用或真实费用；请求中的材料、幂等窗口与能力只代表当前测试配置。

`Open(ctx, Config)` 使用显式绝对文件路径、测试 identity、固定 Window、闭合 QueryMode、有限 IOTimeout/BusyTimeout 和测试时钟 Now。最多 128 字节的原键及资源名、1 MiB 内容，窗口最多 24 小时，I/O 最多 30 秒。SQLite 驱动沿根 module 固定版本；Linux、CGO、C 编译器缺失时实际测试失败，不 skip。Now 在取得目标写事务后读取，由测试环境控制，不能由普通请求延长窗口；起止必须可准确表示为 UnixNano，超范围时拒绝写入。

普通消费者仅使用三个方法：

- `Write(ctx, Request)` 将原键绑定资源名及准确字节的摘要。首次成功耐久接收、原处理事实、当前资源版本和接收观察在同一目标事务提交。首次资源版本为 1，每个新原键提交增加当前资源版本；原键重传返回首次提交的版本和字节。
- `Read(ctx, resource)` 返回目标当前资源字节和版本，不替代原键查询。
- `Query(ctx, originalKey)` 返回当前已经保存的原提交事实。`ErrNotFound` 只表示当前没有该处理事实，不能证明从未接收、原请求永不会到达／生效或可以换键重做。禁用查询时返回 `ErrUnsupported`。

窗口 `[Start, Deadline)` 固定在首次耐久接收。窗口内同键同摘要返回原提交，同键异含义返回 `ErrConflict`。重传与真实 Close/Open 不续期。到期后返回 `ErrGuaranteeExpired`，不返回假原成功，不故意制造第二次写入；已保存的原提交仍可查询。到期不撤销历史效果，也不取消可能迟到的原请求责任。普通 Write 同步接收并提交；下述私有故障计划可将首次耐久接收与实际 apply 分开，窗口和计划截止均不取消待生效原请求责任。

`OpenObserver(ctx, ObserverConfig)` 是显式 privileged 测试出口，通过另一个 `mode=ro`/query-only SQLite 连接和自己的有限读取事务观察目标原提交、原窗口与追加接收观察。它不读取被测内核的表。Observer 仅提供 `Observe`，不得装配为普通 provider Query，或借它为无查询供应商补造查询保证。接收观察记录的是目标耐久处理到达（包括 replay/conflict/expiry），不统计在本地验证或 SQLite 获锁前失败的调用。故障计划的 ReceiveOnly 另行记录实际耐久排队接收，Observer 的 Pending 为 true 时 Value.Version=0，不把尚未提交的输入当实际值。

物理文件持有内核 flock，单进程单连接串行写事务；同文件第二 writer 被拒绝（包括硬链接路径）。目标有自己的 SQLite application_id、随机耐久 DatabaseID、配置身份及 `migrations/0001_target.sql` 校验账本。拒绝外来已有库和未知版本；0001 是全新测试 owner 的首次迁移；新增 0002 为当前实际 pending/plan 事实，冻结 0001。真实已发布 04 writer 创建原记录后升级到 0002，保留原 DatabaseID、窗口、字节、版本、接收观察及第一版迁移校验。`Settings` 实际读取 writer 连接的 SQLite 版本、WAL、FULL、FK、busy timeout 和迁移记录，是测试配置证据。

操作错误以有限阶段文字包装并保留 SQLite/context 原因。Open/OpenObserver 初始化失败且 Close 未确认时，返回非 nil 清理 handle 和 error；调用方必须保留 handle 与 scope。Target 的 Close 在原生关闭前有限等待活动操作退出，此阶段超时后可重试；首次实际 native Close 或 writer/file/parent-directory release 失败则缓存原结果，后续 Close 不能用 database/sql 的已关闭 nil 擦除未知。Observer 同样保留首次原生结果。原生 Close 没有可取消 context，不能启动未 join 的关闭 goroutine 再删除 scope；仅真实监督进程整体退出才可重新评估它持有的资源，父进程自己的未知 handle 仍独立保留。Commit 错误报告“提交结果未知”，没有声称原生 Commit 已被故障测到；原键 Query/observer 用于后续核对。真实故障还覆盖另一连接的 SQLite writer 锁导致 native `SQLITE_BUSY`、解除后正常提交，以及取消 context。

验证入口：`go test -count=1 ./conformance/internal/testkit/target` 和随后 `go test -race -count=1 ./conformance/internal/testkit/target`。耐久证据必须将 TMPDIR 指向真实磁盘文件系统；本环境 `/tmp` 是 tmpfs，使用独立 `/workspace` scope。测试即时 fsync 精确创建路径登记；只有已确认 observer、writer 和外部锁连接关闭后才删除该 scope。失败的未确认关闭保留 scope，不按前缀或时间猜删。


## 有限耐久故障计划（私有环境 seam）

`InstallPlan(ctx, Plan)` 固定 scenario ID、准确 uint64 Seed、Deadline 和 1–64 个唯一稳定 event ID / Kind / Input；总材料最多 4 MiB。Plan/event/原键/资源字符串必须是有效 UTF-8，防止 JSON 编码静默改变身份；内容仍是准确任意字节。Seed 记录测试作者的选择来源，执行顺序与输入明确保存在 Steps；执行器不从 seed 临时重新生成步骤。相同 ID 的任一配置变化返回 ErrPlanConflict，已有原配置重装只返回原 cursor，不刷新期限或重发已完成步骤。新隔离文件、相同 seed/input/steps 可复演相同目标逻辑，物理 DatabaseID 保持独立。

`RunEvent(ctx, scenarioID, eventID)` 每次最多推进一个配置步骤。Cursor 之后的事件返回 ErrOutOfOrder，不应用请求；已完成 event 返回固定原结果/错误，不再次发送。有效步骤依计划顺序有限推进；同一原请求可由不同 event 明确重传，其接收观察仍属目标事实。每个新步骤在目标锁内检查计划 Deadline，并以剩余期限和 IOTimeout 限制 I/O；到期 ErrPlanExpired 不执行新步骤，也不删除 pending 责任。没有后台循环、任意 sleep 或可变业务故障参数。

五种闭合 Kind：

- WriteNormally：正常原键写入，实际目标结果和该 event/cursor 在同一 SQLite 事务提交。
- ReceiveOnly：保存原请求、首次窗口、准确 pending 字节与 received 观察，和 event/cursor 同事务提交；普通 Query 仍可 not_found。
- ApplyReceived：显式释放该原请求的耐久门闩，核对原键/资源/字节摘要后应用，即使原幂等窗口已过。提交、pending 删除和 event/cursor 同事务完成；不新增原请求身份或伪造额外接收。
- DisconnectBeforeCommit：实际准备目标 SQL 后主动 Rollback，目标没有此次部分接收/值。确认回滚后，用另一短事务保存 rolled_back failure event/cursor，再返回 ErrDisconnected。回滚和失败记录是两次事务；中间进程故障可重做这个回滚步骤，不能声称它们全局原子。
- DropResponse：目标事实和 event/cursor 实际同事务 Commit，随后返回 ErrResponseLost；重复该 event 恢复同一失败，不重发写入。普通 Query/独立 Observer 证明实际提交，错误回执不能证明没有执行。

`Observer.Plan(ctx, id)` 在独立只读事务提供准确配置、cursor 和已保存 event/phase/outcome。它与 Observer.Observe 一样是 privileged 测试事实；普通 provider 不获得此查询能力。已知 conflict/expiry/pending/not_found 拒绝保存有限事件结果，重复 event 返回同一错误；未能提交的 SQLite/context 错误保留原因与未知范围，不假装已消费 cursor。

当前进程恢复测试在实际事件及 cursor SQL 保存后、真实 `tx.Commit` 前，以及该 Commit 返回 nil 后、RunEvent 回复前同步。设置入口只在测试二进制中，普通 Config/Request 没有故障参数。父进程在 Start 前登记具体子进程 holder，使用有限三条管道、唯一 Wait 和经实际 WaitStatus 验证的 SIGKILL；真实 Close/Open 后通过普通 Query/Read 和独立 Observer 验证回滚无处理事实或已提交但回复未知。正常释放使用同一个同步点和独立回复通道。新协调者重开原 scenario，依据保存的 event/cursor 读取固定结果，不重发已完成步骤。这只验证进程 SIGKILL，不证明断电、供应商或可用区耐久。

历史升级验证由 `conformance/fixtures/deterministic-target/v1` 的冻结原 writer 源完成：仅机械改包名与内嵌原 SQL，在根唯一 module 下编译并正常运行，确认 child exit 后由当前目标升级同一 overlay 文件。它不等于 SIGKILL 或断电证据。

历史 writer 的构建若失败或超时，直接 go 进程退出不足以确认编译子进程结束，测试保守保留准确 scope 并报告清理未确认；没有假设 WaitDelay 已终止全部后代。正常构建完整成功后才视为构建子进程已收束，历史 writer 也必须确认实际 exit。
