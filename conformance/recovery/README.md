# 真实进程恢复验收

本包的 `integration` 测试使用真实 SQLite 文件或独立 PostgreSQL schema，运行有限测试子进程。必须配置真实磁盘上的 `TMPDIR` 和绝对路径 `LERNA_TEST_OWNED_SCOPE_REGISTRY`；PG 用 `LERNA_TEST_POSTGRES_DSN`，缺少必需依赖时失败。凭据仅通过私有环境继承，子进程帧不包含 DSN。

`decision_process_test.go` 通过公开 Decision Component 和独立 fixture Publisher 读取原 Proposal、产物、身份及固定回执。父进程分别创建、立即登记并 fsync Source 和 Decision scope；同一个具体 Child 在两个 owner 登记后才 Start。子进程仅打开原 scope，不能创建、迁移或删除 owner。`SourceDescriptor` 只含 Schema 与 Owner。

三个私有同步点均有正常释放与实际 SIGKILL 对照：

- Source Publish 成功返回后、Decision Finish 前；此时独立读取已经发布的原产物，公开 Decision 仍为 running。
- 原完成事务 callback 成功执行 SaveDecision 与 Complete 后、Core 实际 COMMIT 前；此时公开 Decision 仍为 running，终止后原未提交完成记录回滚。
- 原生 Store.Within 返回 nil 后、回复前；此时公开 Decision 已为 completed，重开后的公开 Step 不重新处理该工作。

未完成工作按实际 Claim 返回的 LeaseUntil 恢复，不刷新期限。原 prepared 输入、发布 key、摘要、refs、当前权限与公开 usage 保留，未知测量保持原下界及 MeasurementsComplete。独立 publisher 事实不能证明 Source 与 Decision 跨 owner 原子提交。

`conformance/internal/testkit/process` 由本包原 durable demo 和独立目标共同使用：有限三条管道、唯一 Wait、Start 完成屏障、Stop-before-Start 门禁和首次 FD 关闭结果。确认退出与历史错误分别返回；未知退出或未知 native Close 保留精确 scope。父进程自己的未知连接不能由子进程退出擦除。原 demo 每代具体 holder 持续保留，前代 FD 未知会阻止整个 scope 删除；下一代关闭成功不能替前代确认。最多16代，在 New 分配管道前拒绝超限。原生无 context 的关闭需要实际进程监督，不能遗弃关闭 goroutine 后删除目录。

运行 Decision 四个故事（先 normal 实际退出，再运行 race）：

```sh
go test -p=1 -tags=integration -count=1 -timeout=120s ./conformance/recovery -run '^TestDecision(NormalChildPublishesOriginalProposalAndReceipt|SIGKILLAfterPublicationRecoversOriginalRefs|SIGKILLBeforeCompletionCommitRestoresOriginalProposal|SIGKILLAfterCompletionCommitRetainsOriginalReplyFact)$'
```

同一选择器加 `-race` 检查受影响并发。独立目标的实际 COMMIT 前后、pending/late apply 及保存 event/cursor 的进程故事见[目标说明](../internal/testkit/target/README.md)。隔离同 Seed73 的有限重演使用两条明确保存的正常/DropResponse 步骤；计划内响应丢失与物理 SIGKILL 分别验证。

本包只证明所测进程 SIGKILL 与正常恢复范围，不证明断电、供应商幂等、生产容量、可用区耐久或 Executor Effect。本票直接前置为正常 Decision 与持久故障计划；候选规则、取消及整片关闭由各自票负责。
