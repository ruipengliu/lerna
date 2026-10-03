# 切片 02 远端 CI 证据

## PG 原子接纳检查点

2026-10-03，通过 GitHub Actions API 和实际 job 日志核实：

- 准确提交：`e5f26b87fb8914dc16bb6837abff6607a50cddb1`，集成分支 `codex/lerna-implementation`。
- push run：[37145113569](https://github.com/ruipengliu/lerna/actions/runs/37145113569)，`completed / success`。
- `contracts` job `111267327816`：锁定工具安装、`make bootstrap`、`make check` 及清理全部 success。
- `postgres-admission` job `111267327696`：准确 PostgreSQL 镜像启动、`make test-integration`、真实集成 race 及清理全部 success。
- v1 fixture 的迁移、命令、真实 dump、writer 观察及 writer revision 五项校验和全部 OK。
- 日志中的真实套件结果：`conformance/recovery` 集成 `1.778s`，race `3.551s`；两次均未显示 `(cached)`。

该提交验证 PG 接纳、原固定回执、提交未知及 v1 来源输入范围。SQLite、Claim、调度、SIGKILL 和整片退出仍待后续票据，不将 CI 配置或 Git push 成功当作验收成功。

后续必需真实服务入口将增加 `-count=1`，以保证服务重新创建后重新执行集成行为，不复用 Go 测试结果缓存；本检查点日志已证实实际执行。

## PG 修订领取检查点

2026-10-03，通过实际 Actions API/job 日志核实提交 `e8e3384e4cce1f63187ff84f281283a023e07ab1`：push run [37146622433](https://github.com/ruipengliu/lerna/actions/runs/37146622433) 为 `completed / success`。`contracts` job `111271789953`、`postgres-admission` job `111271790111` 均 success。

真实29项PG套件集成 `2.534s`、race `4.866s`，未显示 cached；原五项 v1 artifact 校验和仍全部 OK。票03的代码 `18b80ce` 已整合并在此远端提交验证。范围包括一致领取快照、修订竞争、续租、到期／接替及受控数据库写入隔离；SQLite、调度、完整历史数据升级、SIGKILL与切片02整体仍未退出。
