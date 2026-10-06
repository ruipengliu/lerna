# SQLite 本地档存储故障验收方法

| 日期 | 修订说明 |
| --- | --- |
| 2026-10-06 | 按文档规范拆分方法与实际结果；保留原报告字节，明确全部SQL/native联合有限模型与负对照。 |

本文规定G3本地档的有限等价存储故障模型与判据，实际平台准入见[部署表](../topics/deployment.md#11-本地档平台准入表)，实际版本、次数和限制见[实施报告](../../implementation/m1/local-durability.md)。原报告已原字节归档；不在方法中固定旧I/O次数。

## 1 真实公共生产与独立确认

使用公共 `assembly.Open`、生产同一go-sqlite3与严格同步VFS；包装器和驱动SQLite source ID必须相同。已有库与全新不存在路径分别采集，bootstrap从零prefix开始。公共SubmitGoal/CLAIM/ProcessClaim原回执由独立父进程实际收到后建立确认判据，不能从恢复数据库推定哪些记录已确认。等待真实租约到期，不能提前接替；子进程直接退出，不Close或清理checkpoint。恢复原identity、回执、输入/任务/来源、claim fencing与未确认原命令的原子性，同时检查完整性与外键。

## 2 模型与矩阵

记录实际持久MAIN/WAL/主journal成功写入、截断、同步、名称与前缀；屏障仅在生产VFS真正成功后记录。全部已定义prefix和lost/all/reverse/even/torn五种byte政策保留；屏障前未同步字节可丢失/部分保留，屏障后不得任意损坏已稳定字节。模型声明固定sector与powersafe-overwrite，读取/锁/SHM交给真实VFS，恢复重建SHM，不rematerialize未来字节。匿名SUBJOURNAL保持独立真实句柄和错误转发，不当主journal或假持久cut；P4保存点溢出、回滚、写失败单独实际验证。

原生FILE采集所有实际提交屏障与namespace事件，保留全部byte×namespace政策、零prefix和独立native负对照；联合SQL/native paired images按同一原历史恢复。SQLite事务与FILE发布之间不能假造跨域原子性。每轮从当前实际producer重新量测I/O、ACK、events、cuts、images/pairs与完整同步次数，不能借用旧schema总数或重复乘namespace系数。

## 3 持久配置与负对照

写连接验证本地档WAL/FULL/严格F_FULLFSYNC与固定平台构建；同步、ENOTSUP、目录同步和真实出口前屏障失败必须不确认新责任、不产生新business I/O。NORMAL/OFF/omit-sync、syncEIO、directorysync以及各原生omit-barrier负对照均按原suite保留，证明测试能检测实际缺屏障；生产构建不含fault配置或记录包装器。

成功系统调用依赖OS/设备履行屏障与名称假设，不证明设备未谎报。该有限模型没有穷举所有字节子集/排列，不覆盖多库工作负载、无限介质寿命或物理断电；平台声明不得外推。

## 4 复现

```sh
go test -race -tags fault ./conformance/fault -run '^TestStorage' -count=1 -timeout 120m -v
GOFLAGS=-v make check
```

单例只用于诊断，不能替代完整门禁。expanded fault目标还包括format/compatibility/metrics/必要WAL、所有nativeFILE/API及infra/sqlite occurrence selector。TMPDIR决定临时数据库所在卷；复验其他卷先设置到该卷真实目录。保留actual环境、构建、每个case/政策、独立确认、raw日志和完整源冻结证据；实施报告记录结果与未覆盖项。
