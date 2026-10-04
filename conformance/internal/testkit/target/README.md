# 独立 SQLite 测试目标

这是 `conformance` 可见的测试设施，不是 production adapter、Executor Effect 账本或供应商 API。它不使用业务凭据、模型调用或真实费用；请求中的材料、幂等窗口与能力只代表当前测试配置。

`Open(ctx, Config)` 使用显式绝对文件路径、测试 identity、固定 Window、闭合 QueryMode、有限 IOTimeout/BusyTimeout 和测试时钟 Now。最多 128 字节的原键及资源名、1 MiB 内容，窗口最多 24 小时，I/O 最多 30 秒。SQLite 驱动沿根 module 固定版本；Linux、CGO、C 编译器缺失时实际测试失败，不 skip。Now 在取得目标写事务后读取，由测试环境控制，不能由普通请求延长窗口；起止必须可准确表示为 UnixNano，超范围时拒绝写入。

普通消费者仅使用三个方法：

- `Write(ctx, Request)` 将原键绑定资源名及准确字节的摘要。首次成功耐久接收、原处理事实、当前资源版本和接收观察在同一目标事务提交。首次资源版本为 1，每个新原键提交增加当前资源版本；原键重传返回首次提交的版本和字节。
- `Read(ctx, resource)` 返回目标当前资源字节和版本，不替代原键查询。
- `Query(ctx, originalKey)` 返回当前已经保存的原提交事实。`ErrNotFound` 只表示当前没有该处理事实，不能证明从未接收、原请求永不会到达／生效或可以换键重做。禁用查询时返回 `ErrUnsupported`。

窗口 `[Start, Deadline)` 固定在首次耐久接收。窗口内同键同摘要返回原提交，同键异含义返回 `ErrConflict`。重传与真实 Close/Open 不续期。到期后返回 `ErrGuaranteeExpired`，不返回假原成功，不故意制造第二次写入；已保存的原提交仍可查询。到期不撤销历史效果，也不取消可能迟到的原请求责任。当前票的写入同步接收并提交；耐久排队／门闩和迟到请求由故障计划票添加并验证，不能从本票推断迟到请求已经被取消。

`OpenObserver(ctx, ObserverConfig)` 是显式 privileged 测试出口，通过另一个 `mode=ro`/query-only SQLite 连接和自己的有限读取事务观察目标原提交、原窗口与追加接收观察。它不读取被测内核的表。Observer 仅提供 `Observe`，不得装配为普通 provider Query，或借它为无查询供应商补造查询保证。接收观察记录的是目标耐久处理到达（包括 replay/conflict/expiry），不统计在本地验证或 SQLite 获锁前失败的调用。未来故障票需要另行记录实际耐久排队接收。

物理文件持有内核 flock，单进程单连接串行写事务；同文件第二 writer 被拒绝（包括硬链接路径）。目标有自己的 SQLite application_id、随机耐久 DatabaseID、配置身份及 `migrations/0001_target.sql` 校验账本。拒绝外来已有库和未知版本；这是全新测试 owner 的首次迁移，没有虚构旧目标业务数据升级。`Settings` 实际读取 writer 连接的 SQLite 版本、WAL、FULL、FK、busy timeout 和迁移记录，是测试配置证据。

操作错误保留 SQLite/context 原因。Open/OpenObserver 初始化失败且 Close 未确认时，返回非 nil 清理 handle 和 error；调用方必须保留 handle 与 scope，待再次 Close 确认后才删除。Commit 错误报告“提交结果未知”，没有声称原生 Commit 已被故障测到；原键 Query/observer 用于后续核对。当前真实故障覆盖另一连接的 SQLite writer 锁导致 native `SQLITE_BUSY`、解除后正常提交，以及取消 context。进程 SIGKILL 属后续恢复票，不证明断电耐久。

验证入口：`go test -count=1 ./conformance/internal/testkit/target` 和随后 `go test -race -count=1 ./conformance/internal/testkit/target`。耐久证据必须将 TMPDIR 指向真实磁盘文件系统；本环境 `/tmp` 是 tmpfs，使用独立 `/workspace` scope。测试即时 fsync 精确创建路径登记；只有已确认 observer、writer 和外部锁连接关闭后才删除该 scope。失败的未确认关闭保留 scope，不按前缀或时间猜删。
