# 04-03：实测后唯一改动——准确 Binding 字节的私有解码表示复用

## 决定与固定范围

采用 **一个** 等价性能改动：在 fixture consumer `ContextDispatcher.binding` 的既有严格 Binding 解码位置，加入单条、有明确字节上界、仅保存成功解码结果的私有表示复用。每次仍执行原 `SELECT input_id,body ... FOR UPDATE`，只有本次真实读出的完整 body 与私有自有原字节完全相等，才省去重复 `closedJSON`；返回完全独立的 typed Binding 副本。随后原 current Input 读取/严格解码、完整 DeepEqual、running/control、Permission、trusted clock 与原事务提交均原样执行。

这不是 Content 正文读取缓存，也不是当前 policy、revision、Permission 或授权结果缓存。不改变 Access 接口、数据库格式、迁移、frozen engine、Claim5/caller30/Go与外层120、原费用/次数/预算。**本次不同时优化 Content 的两次完整资格事务或 current Input 解码。** 这是有实测根据的第一步，不能据此保证完整剩余材料、Prepared、publication 与 Finish 都能进入原 5 秒。

产品固定 `5e8cad7e9d77162fda9773bde747235f8937c9ce`；诊断源码 `d0ccc114f1255f6118fb94b794a1342c93050fc4`，当前 HEAD `3669c455778851975257d03ad782def091da439d` 为后续 docs-only。本报告只读固定产品 objects、结果 Markdown/JSON 的实际成本和失败记录，未实施或运行 native。读取了 ContextDispatcher / ContextWorld、adapter Source/Publisher/ports、compiler Input/Bundle、Content processing 与准确 generated Request/TraceContext 形状。未读取对轴结论。

## 1. 支持本选择的实际证据

完整结果说明 `/workspace/lerna-content-03-137311247276/large-graph-subcost-results.md`，明细 `large-graph-subcost-results.json`；原 raw log SHA256 `8f50183586e7746a787833e65ade7defb2335d5ecd0f6206bfad948bd928033c`，141247 B。本报告核对结果与相关 JSON 明细，不另宣称独立重做 root 对全 574 行 raw log 的审计。

该单次原 B：native 3142143/start13113594/session51660，实际 exit1/groupAbsent、已 RELEASE；原 nine-file overlay 只是诊断。closure64 FAIL29.52s、static62 FAIL21.98s；均 178 attribution keys/drop0/ambiguous0。closure64 首错 Content pre.CheckPolicy deadline，在该次 Objects.Read 和第二 Tx 之前，caller 还余 .547s，最终 ValidateClaim DB clock 超原 lease 11.808ms；static62 首错 Current.InputRowScan timeout，caller 还余 8.103s，最终超 8.655ms。两者都有 running/start1 commit 正事实，无 Prepared、无 worker publication，Material 成功 18/34（调用19/35）。旧失败不重归因或删去。

| 当前 worker 前缀成本，inclusive 不重复相加 | closure64 | static62 |
|---|---:|---:|
| Current Within | 1.041688s | 1.895626s |
| Binding closed decode | .696716s / 20次 | 1.325343s / 36次 |
| 其中 strict parse / typed decode | .316057 / .380174s | .626602 / .697685s |
| Input closed decode | .160647s | .292613s |
| 完整 Input 比较 | .006265s | .012316s |
| Binding row+Scan | .034874s | .060334s |
| Content facade（含两 Tx、I/O） | 3.698028s | 2.785696s |
| 实际 Objects.Read | .003145s | .005382s |

完整 Binding 单行约 97KB，`withoutBytes` 已去掉四个 Object.Bytes，但仍包含完整 Input、四 Object refs/sources 和 Bundle metadata。每个材料前 Current 都重新跑 `v.ParseJSON`、typed unknown-field-reject decode、EOF。成本确实在重复表示转换，不是靠猜测认定 FS 慢或把完整 Input 比较当瓶颈。Content 仍有大量实际祖先政策/版本/时钟调用（730/698 policy、729/698 Lock、1503/1472 Now）；其剩余 callback 成本混合，不能以本轮数据宣布进一步删门或推算未到达的发布阶段。

## 2. 最小实现位置和精确命中条件

在 `conformance/internal/decisionfixture/context_dispatch.go` 原 `binding()` 的 `closedJSON(body, &out)` 处调用一个私有 Binding 专用解码助手，或同包单独小文件承载。其唯一职责是纯解码的值复用；不移走任何查询/锁/资格步骤。不改所有调用 `closedJSON` 的全局行为，也不改 adapter Access / domain/compiler 类型以承载缓存。

私有 entry 保存：本 dispatcher Store/owner 范围、该行实际 input_id、取得行所用 column/value 身份、自有原始 body 副本、完整已严格解码的 typed Binding。支持 column 的原三种实际路径 snapshot_key/permission_key/decision_key；不创造新的查询接口。选择将 locator 也作为 key 的保守默认，即便同一行由不同 locator 首次访问会额外 miss，也不为了更多命中弱化范围。

- **先查询成功，再比对**。仍每次原 SQL 与 FOR UPDATE、Scan 完整 body/input_id；SQL error / missing row 原样返回，不能使用上一成功值兜底。不能只用 hash、permissionKey、input_revision、指针或行“约定不可变”判相等。
- 命中须 scope/input_id/locator 一致且 `bytes.Equal(freshBody, entry.body)`。不比较归一化 JSON、不忽略未知字段或空白变化。任何字节变化均 miss，原 `closedJSON` 完整重跑，保原 strict/typed/EOF 错误。
- 只有完整成功 decode 的结果可发布到 entry；失败不记为成功、不负缓存。合法 body 改变也可作为新单条 entry，仍是原 decoder 语义，不把本来可解码的变化强加新 publication conflict。
- 一条 entry，上界默认用现有 `v.MaxBodyBytes`（`1 << 20`，1 MiB）作为**是否复用**的阈值，而非新增接受限额。更大 body 仍走既有完整 decode/原失败出口，不截断、不因缓存阈值新增拒绝。只有四个 Bundle Object.Bytes 都为 nil 的原 metadata Binding 进入复用；其它旧/异常但可解码形状继续原路径，不能清掉 Bytes 改返回值。
- 不在 Bind/Compile/启动阶段新增预热、I/O 或改计费位置。首次真实访问自然完整 decode；新 dispatcher/重开自然空 entry，不持久化它，不靠它恢复原 Binding。

可用一个私有不可变 entry 指针加 `atomic.Pointer` 或仅保护 load/store 的很短互斥；默认选不可变 entry 原子发布，避免在持有 DB 行锁时引入等待另一个 DB 操作的缓存锁。miss 的解码和返回 clone 不持缓存互斥；并发重复 decode 是允许的，只能影响成本，不能合并/跳过调用。不要建立 map/LRU/singleflight/共享全局 registry 或跨 dispatcher 复用。

**复用成功 decode 的语法事实不等于事务/授权成功**：entry 可以在稍后 current Input/Permission 检查或 Commit 失败时仍含这段已验证字节，但后续调用仍重新 SELECT 并走全部当前门。因此它不能被记录为已授权或已提交事实，也不能使 CommitUnknown 变成功。

## 3. 必须完整隔离可变值

原 `json.Decoder` 每次产生独立切片/指针；引入复用不能把内部 entry 直接返给消费者。无论首 miss 还是 hit，内部 entry 的可变对象都必须与返回值隔离，不能让 caller 改 Bundle.Materials 后影响另一请求。

按当前固定类型做窄 typed clone，不引入反射通用 copier、序列化后二次 decode、unsafe 或依赖包：

- Input：Constraints；Conditions 切片及每个 Condition.Gaps；Control.Restrictions；Budget.Unknown；Unresolved；Progress；Materials；Omitted；Gaps；Request.Payload.UseRefs；可选 Request.TraceContext 指针。其余当前 scalar/value refs 逐值复制即可。
- Bundle：Mandatory/Shell/Manifest/Lock 每个 Object 的 Sources 与 Bytes（通用本地 clone 可保留 Bytes；入 memo 资格仍 nil）；Materials、Processed；所有 Ref/Key/计量字段逐值复制。
- 所有切片须保留 nil 与非 nil 空切片区别，不用将 nil 一律改 `[]` 的捷径；TraceContext nil/非 nil 同理。现有 DeepEqual 与 JSON 身份依赖准确表示。新字段以后若含 slice/pointer，要显式纳入此唯一 clone 的审查。
- entry 原 body 必须自有 clone，不持 `Scan` 可复用 buffer。entry typed Binding 不通过 Observe/Binding/Current/Authorize 的返回值、Permission.UseRefs 或日志暴露为可变别名。当前 PermissionFor 从 Input.UseRefs 取 slice；返回 Binding 为独立 clone 是隔离链的一部分。

这段 clone 是引入该优化必须承担的具体正确性工作，不能口头宣称 Binding immutable 来跳过。若不能可靠完成字段隔离，则不采用浅拷贝版本。

## 4. 全部当前门和错误优先级 KEEP

原顺序保留：方法 purpose/key 输入检查 → Store.within/首次 trusted → 锁定 Binding row+Scan → 严格 Binding decode（或精确字节命中的等价成功值）→ `d.input` 当前行 FOR UPDATE、完整严格 decode → 完整 current Input 与原 bound.Input DeepEqual + running/control → trusted → 完整 Permission 比较 → 最后 trusted → 原 Commit。

缓存命中不是 current Input 命中。新 Revision、Goal、Control、Budget、Materials.Selected、限制、Request/UseRefs 等任一字段变化仍通过每次新读取与全量比较失效；不能只查 revision。trustedUntil 和原 Permission 的有效性也不缓存，原 clock 位置不提前或移除。若 Binding 字节损坏且当前 Input 也变化，仍在读取 Input 前由原 decode 报错；合法 Binding 但新 Input 未获准时仍按原顺序拒绝。不要借本次优化新增 context.Err 提前检查而改变这类优先级。

以下都完全不改：Content 每次目标 + 完整祖先 read/process、fullRef/保留期限/current clocks，真实 Objects.Read/hash/长度，I/O 后完整第二 Tx 当前门；frozen Source/worker 的完整材料、M/shell/manifest/lock 与 Proposal 字节、读预算、RuleStarts1、原费用和 Prepared/Publisher 恢复身份；各种 publication 前当前资格与 Finish/claim 门。

## 5. 一次改动后的实际验证与限度

由 sole owner 在 root 授予独占 LOCAL 后实施及执行；本决定不是 native grant。

1. 私有解码助手机械测试：冷/热返回值与原 `closedJSON` 完整 DeepEqual；嵌套 slice/TraceContext/UseRefs/Bundle sources 修改返回值不能污染下一次；nil/空保真；更换 locator/input_id/body 会 miss；malformed/duplicate/unknown/trailing JSON 原错误仍发生；超过 memo 阈值保留原 decode 语义；并发 hit/miss 的 race 检查。可断言该窄助手实际 decode 次数作为纯 CPU seam 的证据，但不把机械输入称为真实 DB 损坏/业务恢复。
2. 实际当前权限反例使用已存在公开/受信真实入口：先真实正常访问使 entry 有值，再 InstallInput 合法下一 revision 改完整 input/control，旧 worker access 必须拒绝；原 true policy revoke/expiry/post-I/O 撤权或并发等待 tracer 仍拒绝；关闭/重开 dispatcher 后仍按原源查找与校验。合法 normal 对照必须保留。缓存无法让 SQL/row 错误成功。既有相应测试能覆盖就复用，不造影子私表业务 oracle。
3. 在**原完整 B 四 exact tests、原 initial 配置和 30/5/120 限制**运行 normal 与 race；不把测试拆成重建时钟的几段、不额外预热、不失败后 renew/retry、不修改预算/收费。记录首次失败的实际 phase、原 Claim 时间、rule starts、完整 Material/read 与 Prepared/publication 进度；可沿原低开销观测方法比较 decoder hit/miss 与总体变化，但无需先再运行一轮只有诊断而无改动的原失败实验。
4. 若原 B 仍失败，这一改动只有在等价门/反例通过时才具备源与局部正确性资格；它不是完成 normal capacity 的 green。保留新失败原 log/identity/scope，按新首次实际 gate 再作窄决定。不能把尚未发生的后续 publication 当 0 成本，也不能保证节省 .697/1.325 秒即足够。Content 两 Tx 成本真实且重要，但本轮不捆绑另一优化或自动升级为缓存当前权限。

所有旧 shared-load B、独占 Claim diagnostic、subcost B 失败及原 05 overlap 纠正保留历史。七 AC / whole04 不因单项 CPU 优化提前接受。无新 ADR、schema、迁移、Task/Grant/Provider 或生产缓存方案。
