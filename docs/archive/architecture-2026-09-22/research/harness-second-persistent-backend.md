# 第二个嵌入式持久化后端：bbolt 的契约适配边界

访问日期：2026-09-10。范围：核实第二个持久化验证后端的原语及限制；不替换默认 SQLite，不安装或运行依赖，不提供性能或故障测试结果。

## 结论与固定依据

**建议候选为 `go.etcd.io/bbolt v1.5.0`，通过现有 Store 接口适配。** 它能为单个数据库文件提供本地事务及读快照，适合验证核心脱离 SQL 后仍保持相同持久化语义。多进程通过持有文件的存储服务进程及已有 gRPC Adapter 访问；不能让多个进程同时直接读写同一文件。此处为可实现性判断，尚非适配或耐久性已验收。

| 锁定项 | 官方事实 |
| --- | --- |
| 稳定发行版 | v1.5.0，2026-06-21 发布；实时 release API 与发行页一致，搜索索引仍可能显示旧 v1.4.3。 |
| 完整提交 | `e7a8b2dd498494a3766ba24dd94d3509e5588485`。 |
| Go 要求 | `go.mod` 声明 `go 1.25.0`，另有 `toolchain go1.25.11`；两者分别是模块最低 Go 版本和建议工具链，不应混为要求调用项目固定该补丁版本。 |
| 依赖形态 | 纯 Go 嵌入式库，无独立数据库服务；模块仍有 x/sys、x/sync 及工具/测试依赖，不能表述为零 Go 模块依赖。实际集成时锁定完整依赖图与 go.sum。 |

来源：[发行页](https://github.com/etcd-io/bbolt/releases/tag/v1.5.0)、[固定提交](https://github.com/etcd-io/bbolt/commit/e7a8b2dd498494a3766ba24dd94d3509e5588485)、[go.mod](https://github.com/etcd-io/bbolt/blob/e7a8b2dd498494a3766ba24dd94d3509e5588485/go.mod)、[包说明](https://github.com/etcd-io/bbolt/blob/e7a8b2dd498494a3766ba24dd94d3509e5588485/doc.go)。

包说明列出 Windows、macOS、Linux；固定版本工作流实际列出 Linux amd64/arm64 和 Windows amd64 单元测试任务，另有交叉编译任务。**编译列表不是运行验证记录，也不是最低 OS 版本承诺。** 本次未发现覆盖全部 OS、CPU、文件系统的最低版本矩阵，建议 Harness 首个参考验证范围固定为 Linux amd64、本地受支持文件系统；其他平台分别取证。不能据手机相关编译目标宣称真实设备验证完成。[上游 Linux 测试配置](https://github.com/etcd-io/bbolt/blob/e7a8b2dd498494a3766ba24dd94d3509e5588485/.github/workflows/tests_amd64.yaml)、[ARM64 配置](https://github.com/etcd-io/bbolt/blob/e7a8b2dd498494a3766ba24dd94d3509e5588485/.github/workflows/tests_arm64.yaml)、[Windows 配置](https://github.com/etcd-io/bbolt/blob/e7a8b2dd498494a3766ba24dd94d3509e5588485/.github/workflows/tests_windows.yml)、[交叉编译配置](https://github.com/etcd-io/bbolt/blob/e7a8b2dd498494a3766ba24dd94d3509e5588485/.github/workflows/cross-arch-test.yaml)。

## 已核实的持久化与并发原语

- `DB.Update` 在同一写事务中修改多个 bucket；闭包错误回滚。只有一个写事务，多读事务各持有开始时的数据视图；事务对象不能跨 goroutine 并发操作。返回值中的字节必须在事务内复制后交给上层，不能泄露 mmap 引用。[事务说明](https://github.com/etcd-io/bbolt/blob/e7a8b2dd498494a3766ba24dd94d3509e5588485/README.md#transactions)、[Bucket 源码](https://github.com/etcd-io/bbolt/blob/e7a8b2dd498494a3766ba24dd94d3509e5588485/bucket.go)。
- 提交先写数据页并同步，再写 metadata 页并同步；Linux 路径调用 `fdatasync`。参考耐久性配置应保持 `NoSync=false`、`NoGrowSync=false`，先保留默认 freelist 同步。不能把页缓存可见视为已持久化；硬件和文件系统的同步保证仍需环境及故障验证。[提交实现](https://github.com/etcd-io/bbolt/blob/e7a8b2dd498494a3766ba24dd94d3509e5588485/tx.go)、[Linux 同步实现](https://github.com/etcd-io/bbolt/blob/e7a8b2dd498494a3766ba24dd94d3509e5588485/bolt_linux.go)。
- **结果不明是 Harness 要显式处理的情况。** metadata 已写后同步可能返回错误；进程也可能在成功提交后、响应前退出。根据上述源码，不能将所有 Commit 错误或调用超时归类为确定未提交。建议存储单元遇到持久化 I/O 故障停止新变更，恢复至可确认耐久状态后按稳定回执核对；不能在同一故障实例中看到缓存回执就报告耐久成功。这是由提交顺序推导的适配要求。
- Unix 使用 advisory 文件锁；读写打开独占，多个只读打开共享且会阻挡读写打开。`Options.Timeout` 限制打开时等待文件锁，不是每个事务的截止时间。文件锁不授予任务所有权、不隔离恶意直接文件写入，也不是跨端租约。[文件锁源码](https://github.com/etcd-io/bbolt/blob/e7a8b2dd498494a3766ba24dd94d3509e5588485/bolt_unix.go)、[打开实现及选项](https://github.com/etcd-io/bbolt/blob/e7a8b2dd498494a3766ba24dd94d3509e5588485/db.go)。
- `Begin/Update` 不接收 context，等待写锁及同步 I/O 没有原生按调用 context 强制取消。建议 Adapter 使用有界准入队列，在开始事务前取消排队请求；一旦进入提交仍须保留结果核对责任。`DB.Batch` 的闭包可能重复执行，因此默认用 Update；任何事务闭包均不得调用模型、网络或设备。[事务入口和 Batch 实现](https://github.com/etcd-io/bbolt/blob/e7a8b2dd498494a3766ba24dd94d3509e5588485/db.go)。

## 映射到 Harness 契约的建议

以下是 Harness Adapter 需要实现的行为，不是 bbolt 已提供的业务接口。

| 契约 | bbolt 原语及项目责任 |
| --- | --- |
| RunStore / 工作存储 | 单次 Update 先核对 change 回执及语义指纹，再校验预期版本、拥有者世代和领取代次，原子写状态、记录、工作和回执。同一运行持久化单元不得拆成两个无法共同提交的文件。 |
| ExecutionStore / 资源控制 | 操作事实、外部关联、资格、资源接管屏障、已启动登记及报告 Outbox 在约定本地原子边界内提交；动作仍发生在事务外，UNKNOWN 保留。 |
| MemoryStore | 记录版本、operation 回执、删除墓碑、变更序号和同步日志一起更新；键范围扫描可实现 Scan/ReadChanges，但排序键、过滤、授权视图及过期游标规则由项目定义。 |
| ReadSnapshot / 同步分页 | 一次 View 提供一致视图；跨请求快照不能每页开新 View 后冒充同一快照。建议先在有界读事务中生成受控、可分页的固定快照产物并记录变更边界，或维护有期限的快照句柄；关闭/重启失效后明确重取，持续变更从同一边界衔接。 |
| AuthorizationStore / ExtensionStore / 评测发布存储 | 可用相同事务原语实现版本条件提交、回执、变更与待处理工作；各模块仍拥有自己的语义和 Interface。共享文件不允许调用者访问其他模块 bucket。 |
| 查询索引 | KV 游标及项目维护的索引键支持有界查询；本次未核实内置全文、向量或 SQL 能力，不能声称 bbolt 提供这些接口。参考方案可由获准权威记录重建内存文本索引，记录索引版本/覆盖边界，未就绪显式返回或采用可核对扫描，不能将缺失索引当无命中。 |

CAS、幂等回执、清理后的拒绝水位、Outbox 投递、权限再校验及跨模块交接均由 Harness 实现。bbolt 的事务序号不直接替代项目 change_id、operation_id 或跨节点世代；跨文件、数据库与外部动作、端云之间没有共同事务，更没有由此自动得到分布式 exactly-once。

## 备份、空间和恢复边界

一致性备份使用读事务 `Tx.WriteTo` / `CopyFile`，不能在活动写入期间直接复制原文件后宣称一致。**CopyFile 源码仅写入并关闭目标，没有显式 Sync**；建议备份工具自行对目标文件同步，并在原子替换后按支持平台同步目录，校验完成后才发布备份清单。[备份接口实现](https://github.com/etcd-io/bbolt/blob/e7a8b2dd498494a3766ba24dd94d3509e5588485/tx.go)。

长读会保留旧页，重映射也可能等待读事务；备份或同步不能无限期持有读事务。删除不会自动缩小数据库文件；v1.5.0 的 MaxSize 约束文件增长而非进程 RSS。压缩将存活内容写入另一数据库，目标可能分批提交，中断后不能直接作为正式库启用。[空间限制说明](https://github.com/etcd-io/bbolt/blob/e7a8b2dd498494a3766ba24dd94d3509e5588485/README.md#caveats--limitations)、[MaxSize 定义](https://github.com/etcd-io/bbolt/blob/e7a8b2dd498494a3766ba24dd94d3509e5588485/db.go)、[Compact 实现](https://github.com/etcd-io/bbolt/blob/e7a8b2dd498494a3766ba24dd94d3509e5588485/compact.go)。

建议迁移 SQLite ↔ bbolt 使用版本化逻辑导出导入及停写屏障：停止新动作准入，排空或冻结本地写入，保存跨模块交接位置，导入全部恢复事实/回执/授权水位，重建索引，核对后切换唯一写入入口。普通文件备份只覆盖该文件；多文件和外部产物的一致切点、切换确认及失败回退需项目管理。成功切换后若已产生新写入，不能直接恢复旧库继续执行。恢复旧备份仍遵循既定旧身份隔离与未知效果核对规则。

原文件有字节序限制；本次不承诺跨不同字节序直接复制可用。上游固定 README 还记录 Linux ext4 fast_commit 在若干旧内核补丁级的损坏问题；这说明最低平台验收需核对文件系统、挂载选项和内核修复，不能只写“Linux”。打开时检查 metadata 及事务恢复不等于修复任意硬盘损坏，更不等于恢复备份之后的业务事实。[文件格式限制与已知问题](https://github.com/etcd-io/bbolt/blob/e7a8b2dd498494a3766ba24dd94d3509e5588485/README.md#known-issues)。

## 尚需实现阶段提供的证据

同一套 Store 耐久性用例须覆盖提交各阶段崩溃、回执丢失、CAS/资格竞争、磁盘满和同步错误、重启恢复、快照分页与变更衔接、索引丢失重建、迁移中断和旧备份隔离。多进程 RPC 语义、原文件并发打开拒绝、平台最低要求、数据量和延迟上限分别记录。本文未执行上述验证；其通过之前只能声明候选原语满足设计需要，不能宣称后端可无条件互换或生产性能达标。
