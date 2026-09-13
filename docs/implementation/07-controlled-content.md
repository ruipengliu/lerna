# 07 受控证据与产物

本票提供 `artifacts.Service`、正式 Protobuf/SDK、本地受信绑定、可替换正文接口和 SQLite 内容分区。默认无需外部服务，参考文件 Adapter 支持 Linux，使用 owner-only 目录、os.Root 和跨进程 flock。单次上传最多 1 MiB，可在此上界内缓冲；分块读取最多 64 KiB。更大的流式上传尚不支持，明确拒绝而非无限缓冲。

## 接口与权限

`ContentClient.Call` 接收 PUT、GET、READ、DELETE、LOOKUP。PUT 包含来源与精确来源修订、获取/生成时间、媒体类型、长度、SHA-256 及留存期限；内容引用固定为 namespace/key/revision。此票创建不可变正文修订 1；生命周期修订用于删除并发检查，不代表内容变更。READ 指定精确引用、用途、偏移和大小。DELETE 使用稳定 operation_id 与预期生命周期修订；LOOKUP 区分历史提交与当前可用状态。

本地 Binding 在协议外配置凭证、命名空间、实际处理位置和接收位置。使用独立 `content.store/process/retain/discover/disclose/delete` 动作；普通持续授权和来源政策必须同时允许，管理员无内容政策豁免。请求用途必须与内容用途一致。数据源配置通过 `contentpolicy.Policy` 的受信安装/更新端口注入；每个来源按精确修订匹配，多个来源分别检查。规则可分别允许发现与正文披露、读取与再次存储；未知来源或修订拒绝。配置更新与数据事务不构成跨模块全局原子撤销；在本地发布和分块返回前重新检查当前可知限制。

引用及原操作查询在查找记录前统一认证；错误、禁用及过期凭证不能通过错误差异探知存在性。GET、LOOKUP 和 PUT 重放对尚未清理的正文核验当前可读性，返回 available/missing/corrupt/unavailable；该状态是检查时观察，历史提交仍保留。

对无权方，缺少引用与不可发现记录均返回 PERMISSION_DENIED；授权后正文缺失为 CONTENT_MISSING，摘要/长度错误为 CONTENT_CORRUPT，存储问题为 UNAVAILABLE。到期/删除阻止新正文读取并返回 CONTENT_UNAVAILABLE。普通协议错误仅含固定错误码，不含正文或宿主路径。受信来源配置只证明来源政策，没有声称证据内容真实。

## 持久化与恢复

内容拥有独立 ContentData 与 ContentOperations 分区；任务 RuntimeData 不被覆盖。共享授权 Store 的 CAS 提交把当前授权核对、时间水位、内容状态及操作身份持久保存；不同领域不能复用原操作身份。

小正文随引用与回执同一次提交。大正文在受控目录写入独占临时文件，完成文件同步、重命名及目录同步后校验可读性与摘要，再提交引用。文件操作与 SQLite 并非同一事务；提交不明须查原操作，不能交付未经确认的结果。未发布文件可作为孤立文件清理。

同一内容目录的调用在跨进程锁内协调，超时停止等待；参考实现选择串行目录操作，吞吐并发上限为 1。文件操作本身使用有限文件大小和分段上下文检查；不承诺故障内核/磁盘系统调用可被 Go context 强制打断。一个数据库的所有内容服务必须由受信部署绑定同一个专用目录，不能混用不同数据库或旁路写入。该 Adapter 不声称隔离同 UID 的恶意宿主程序。

删除先提交 cleaning 与生命周期修订，立即抑制正文读取；维护入口 `Clean` 有界清除正文，再提交 cleaned。重启可从 cleaning 继续，正文已清理但进展丢失也可安全重做。过期内容在每次读取时立即停止使用，维护入口完成后续清理。目录锁协调发布、读取和清理；当前没有跨引用物理去重。

清理移除正文、媒体类型、摘要、生成时间和大小，以及 PUT 原载荷指纹。保留最小内容引用、资源/用途、来源政策定位、留存边界、生命周期和操作状态，仍需当前发现权限；这些策略定位元数据的允许保留是受信部署配置责任。清理后重传 PUT 返回 CONTENT_UNAVAILABLE，改用 LOOKUP 核对历史；不为比较重试偷偷保留已删正文。记录和原操作身份总数有上限，容量满时拒绝新内容；此票不自动清掉水位然后重新接纳旧身份。

## 可重复验证

```sh
go test ./artifacts ./profiles/content ./sdk
go build -o build/contractcheck ./cmd/contractcheck
./build/contractcheck -profile controlled-content-v1
make verify
```

Profile 使用真实 SQLite、受控文件和独立客户端，配置为小正文阈值 16 字节、对象 1 MiB、总正文 2 MiB、32 引用、64 文件、清理批次 16、每次操作 1 秒、最大留存 1 小时。策略时钟固定在 2026-09-10。来源政策为本地受信配置，处理及接收位置为模拟标签。

当前有 31 项契约检查及 5 项真实子进程中断检查：部分暂存、文件准备完成、发布提交后回包丢失、删除决定提交、正文已清理但进展未确认。部分暂存使用故障 Adapter 在真实目录留下同步后的部分文件再退出，不声称已在任意内核写系统调用中精确终止生产 Adapter。其他中断点在实际 Adapter/Store 返回后退出；所有恢复都重开原 SQLite 和目录核对。

旧存储编码夹具及来源见 [说明](../../profiles/content/testdata/README.md)。尚未提供远程传输、长期记忆、联合检索、单次签名许可消费、备份清理或外部副本撤回；保留原任务事实不等于完成这些业务模块的内容迁移。

## 完成与验证记录（2026-09-10）

实现提交 `2f492c2`，审查修复 `d32529e`，验证调度调整 `c213865`。最终 make verify 全部通过：依赖校验、生成一致性、编译、go vet、全量 race 测试、SDK 样例及全部适用 profile。

- [受控内容报告](evidence/07-controlled-content-report.json)：36 项必需检查，31 项契约及 5 项真实子进程恢复。
- [最终阶段报告](evidence/07-verification-stages.json)：所有阶段 passed。
- 01–06 回归：[SDK 32 项](evidence/07-sdk-regression-report.json)、[授权 30 项](evidence/07-auth-regression-report.json)、[任务 13 项](evidence/07-tasks-regression-report.json)、[Worker 26 项](evidence/07-worker-regression-report.json)、[控制 31 项](evidence/07-control-regression-report.json)、[签名授权 30 项](evidence/07-grants-regression-report.json)。各 profile 的限制仅描述其自身验证范围。
- [双轴审查](07-controlled-content-review.md)：Standards 1 项 P3、Spec 2 项 P2，均修复并复核关闭。
- [首次并行验证失败记录](evidence/07-parallel-verification-failure.md) 单独保留。

最终报告对应修订 `c213865184dc95ad4c3df01a2b654f8ec5a1664f`，dirty=false，Go 1.26.1、linux/amd64。后续完成记录提交只包含文档与归档报告。

## 验证调度调整

首次全套 race 验证在多包并行负载下，旧 TestRunningPauseWaitsForActualStop 与 uncooperative-pause 返回 UNAVAILABLE；内容检查通过。上述两个测试定向重复 5 次均通过，失败与并行负载相关的判断属于推断，未据此宣称已定位生产故障。参考验证脚本将测试包并发固定为 1，以限定多个 SQLite/子进程套件同时运行的磁盘负载；各用例内部的真实并发、race 检查和业务时限不变。失败阶段记录单独保留，不覆盖成通过。
