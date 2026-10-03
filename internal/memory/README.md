# Content 与 Memory 参考实现

本模块实现 [Memory 设计](../../docs/architecture/memory/README.md)中的同 owner、显式同数据库用例。准确正文由 ObjectStore 保存，Memory 保留断言、来源和版本；检索投影不成为权威。公开方法的闭合输入、输出和摘要由 `Service.Register` 登记并交给根生成器生成。

## 装配

`New(store, objects)` 不安装许可，也不发送外部请求。宿主在登记方法和启动 worker 前固定 `Location`、`Authorization` 和 `ConfigureParticipants("platform", "governance")` 等同库参与者。默认参与者为 `content`、`memory`，默认处理地点为 `local`。

`InstallPolicy` / `InstallPolicyTx` 是受信管理入口。`PolicyValues` 显式固定 subject、purpose、location、最晚保留期、continuous 和 independent_derived；`PolicyRef.Digest` 必须等于这些值的 JCS 摘要。未知许可不允许读取。额外授权端口的 `Check` / `Visibility` 必须在同一 Tx 读取当前凭据及授权代次，不能 RPC。所有管理、控制和普通数据入口都核当前凭据；数据读取还逐项核当前用途及来源。跨 owner 引用缺少权威接入时返回 `dependency_unavailable`。

可调用的正文端口：

```go
Upload(ctx, scope, auth, PublicationRequest, body) (api.ContentRef, error)
LookupTransfer(ctx, scope, auth, transferID) (TransferStatus, error)
ReceiveTransferBytes(ctx, scope, auth, transferID, body) (TransferStatus, error)
Read(ctx, scope, auth, ref, purpose) ([]byte, error)
ReadBytes(ctx, scope, auth, ref, purpose, recipientLocation) ([]byte, error)
PublishInTx(ctx, tx, auth, PutInput) (api.ContentRef, error)
CheckContentTx(ctx, tx, auth, ref, purpose, location, continuous) (ContentVersion, error)
```

`PublicationRequest` 的 content/version/hash、原 reserve/put CommandID、TransferID、来源、许可和期限必须在首次调用前固定。协议路径是原 `content.upload_reserve` → HTTPS 准确字节上传 → 原 `content.put`。字节最多 16 MiB；`ready` 尚不等于已发布。ObjectStore 的写入、读回和删除均在元数据事务外；读回后再次核当前控制门禁。`ReadBytes` 同时核服务实际处理地点和接收地点，参数不能改变实际处理地点。

上传写入或 ready 提交未知时停止发布，沿原 TransferID 查询并恢复。发布、不可变版本、来源边、到期 Job 和原命令回执在短事务共同保存。同一 content/version 不接受不同摘要。只有已发布的准确来源可加入 processed DAG；disclosed 必须是 processed 子集。未引用输出中已处理的来源仍参与限制交集、撤销和纠正。

## 方法范围

| 方法 | 行为 |
| --- | --- |
| `content.policy.install` | 显式许可安装；相同摘要和输入可恢复 |
| `content.upload_reserve` / `content.put` / `content.transfer.read` | 原票据、准确字节、ready 与原子发布 |
| `content.register_copy` / `content.release_copy` / `content.get` | 持有者登记、当前控制、停止使用与清理回执；bytes 模式最多 2048 字节，更大正文使用准确字节端口 |
| `content.close` | CAS 主动关闭；包括自然到期后的进一步主动撤销 |
| `content.mirror_reserve` / `content.mirror_complete` | 本实例已配置地点的准确镜像票据与单个副本身份；固定原用途、地点、主体和期限 |
| `memory.create` / `replace` / `restrict` / `delete` | 四种记忆、显式纠正、仅收窄限制、删除墓碑 |
| `memory.read` / `inspect` / `list` | 当前可读版本、受信管理元数据、有界冻结分页 |
| `memory.query` / `memory.index.inspect` | 可解释中英文词法/字面检索、连续元数据水位与补扫模式 |
| `memory.extract` / `extract.read` / `extract.cancel` | 有限提取责任、原主体与代次、保存 checkpoint、取消不越过最后已提交边界 |
| `memory.candidate.list` / `read` / `replace` / `reject` | 有期限候选、修订与至多一个对应 Memory |
| `memory.view.open` / `pull` / `ack` | 同一提交水位的快照与连续变化、受控持有者、原发页重读与接收回执 |

查询在读取候选正文和计算相关性之前过滤当前许可。默认词法策略使用中文连续双字词、英文小写词和唯一重合计分，并以 MemoryID 固定同分顺序。查询文本最多 4088 字节、100 个解释项，候选最多 200；分页最多 20 条，冻结集合与 TTL 5 分钟不会随读取刷新。授权代次扩张、收窄或失联均不借用旧页面。读取字节、权限检查和截止时点有累计上限，未覆盖部分明确返回 `partial` / `gaps`。当前索引是连续元数据投影，查询采用 `metadata_authority_scan`；没有宣称向量质量或全集覆盖。

统一 Dispatcher 提供原 QueryBinding 时，query/list 首次快照期限还取该绑定
期限与 5 分钟上限的较早者。后续分页或更晚的查询绑定不能延长原快照。
每页还在同一 Tx 核对冻结的 QueryRef、ScopeRef、TextRef 及其实际来源闭包。
自然到期直接依据当前保留期限拒绝旧页，不等待到期 Job 或权限水位投影；
缺少冻结输入来源的旧快照必须重新查询。输入门禁检查也计入原累计权限预算。
预算用尽返回无披露的 partial/gaps，保留未遍历位置，不把剩余候选称为 exhausted。
当前来源门禁的 SQL、取消及提交未知错误保留原类别；只有明确来源业务拒绝要求重建快照。
冻结候选和三个准确输入引用分条保存，每条固定原 revision 1 及 JCS 摘要；
最多 203 条分片与快照头共同提交，读取分片时验证准确摘要，不读取 latest。
解释文本不在单条记录中重复 200 次。每页最多 20 条，还按实际 JSON 字节上限提前分页，
为后续可能出现的许可 gaps 和最长游标预留有限尾部，保留未发候选的位置；
不能装入一份合法回复的单个匹配返回明确的输出限额错误。

事实、偏好、推断和经验分别保留 Type。默认 `RuleExtractor()` 只接受闭合的 `ExtractionDocument`，是有限的显式值导入器；没有配置模型提取器。默认 review_only；preapproved 需要显式 `SavingAuthorization` 验证原 SavingGrant，未配置返回 `unsupported`。保存同一候选的 Memory、候选状态、去重键、变化头、回执和 Job 一起提交。

经验正文须使用 `application/vnd.harness.experience+json`。`unknown` 可以明确保存；success/failure/cancelled 要求已配置 `ExperienceAuthority` 在 Tx 外核准确原效果及证据。文本不能自行成为成功依据。

## 清理与支持边界

主动关闭立即阻止来源及派生的普通使用。自然到期只按原保留期执行；双方明确允许 independent_derived 时，派生可以在自己的许可期限内保留，随后主动 close 仍会撤销。Memory 纠正或删除会立即关闭旧断言的派生用途，并有界推进 needs_review 或清理；原历史身份不复用。

副本自身到期只关闭该副本，不关闭或删除原文。外部持有者未报告停止使用时，cleanup 保持待处理；`use_stopped`、`complete`、`residual`、`unknown` 分别保存。complete 报告必须含存在且摘要一致的 Content 证据。Memory 自有 `metadata_reference` 只表示原引用元数据，不宣称曾复制来源正文。关闭、全体停止使用、原介质删除和其它副本清理分别推进；其它副本报告未知时不能宣称全部清理完成。

`adapters/objectstore.Local` 使用文件 fsync、不可覆盖链接和目录 fsync，并独立验证 hash/length。耐久等级仅 `local_fsync`，将服务地点设为 cloud 不会改变该事实。当前未接跨地点目标介质、跨 owner 权威凭据、SDK/备份物理擦除验证或三 AZ 对象存储；相应镜像或来源入口明确拒绝，外部清理报告不升级为全介质擦除证明。

## 实际验证

```sh
go test ./internal/memory ./adapters/objectstore
go test -race ./internal/memory ./adapters/objectstore
go vet ./internal/memory ./adapters/objectstore
go build ./internal/memory ./adapters/objectstore
HARNESS_TEST_POSTGRES_DSN=... go test ./internal/memory -run TestPostgres -count=1 -v
```

普通行为测试使用迁移后的真实 SQLite 文件与本地准确介质。PG 测试使用真实连接，未提供 DSN 时明确 skip，不能记为数据库验证通过。测试显式安装有 subject/purpose/local 约束的一小时许可；撤权端口和时钟只替换已授权的外部权限/可信时间边界。提交未知注入位于真实 SQLite COMMIT 之后；介质删除丢回复注入发生在实际 unlink/fsync 之后。验证覆盖原票据重开恢复、清理幂等、来源主动关闭、自然到期与独立派生、准确 hash、权限先于正文读取、权限扩张后的旧页拒绝、固定 TTL、单候选唯一保存、经验未知状态和清理墓碑。
