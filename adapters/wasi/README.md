# Linux 受限 WASI 运行时

本包实现 [Execution 环境合同](../../docs/architecture/execution/README.md#7-交互式计算环境)的
`environment.run_cell` 驱动，不拥有 Task 完成、Grant 或命名空间 head。选定运行时是
Linux amd64、wazero **v1.10.1 解释器**和 WASI Preview1；没有自定义 guest hostcall。
输入为准确 WASM Content 和已提交被动 namespace；输出只有完整、闭合的被动 namespace。
原生文件、网络、环境变量、凭据和子进程都不向 guest 开放。

## 部署与实测锁

在 Linux 安装 `bubblewrap` 和 `util-linux`，静态构建 worker：

```sh
CGO_ENABLED=0 go build -trimpath -o /absolute/private/bin/wasi-worker ./cmd/wasi-worker
sha256sum /absolute/private/bin/wasi-worker
```

宿主必须显式登记绝对 worker 路径、准确 `sha256:` 摘要、私有 0700 journal 目录、scope、
当前 Content/Authority 端口和 1–4 的并发额度。默认开发装配 `WASIConfig` 为 nil 时不开放
该程序入口；配置不会自动签发 Grant 或增加 Content 用途。新增用途可通过
`RequiredWASIContentPurposes()` 取得，仍须与原数据许可取交集。

`New` 与每次环境准备都实际启动固定探针，不执行用户代码。worker 必须是静态 ELF。
探针核实原 runtime/module 版本和 module sum、guest 真值 42、隔离 root 只有 worker、
进程环境只有固定 GOMAXPROCS/GOMEMLIMIT/PWD、网络 namespace 只有 lo，以及实际 AS/CPU/FD
硬限制。解释器 artifact、worker、bwrap、prlimit、内核、平台、探针与并发额度进入不可变
`RuntimeManifest` 和准确 `InstallLockRef`；deployment scope/journal root 另进入 ConfigRef。
变更不能沿原 Attempt 偷换 runtime，必须产生新准确配置与部署身份。

本机实际验证版本为 Linux 6.18.44 amd64、bubblewrap 0.12.0、util-linux 2.41.5、Go 1.26.8。
CI 安装这两个系统依赖并运行真实隔离探针；其他版本须通过自身实际 artifact/probe 锁，
平台不支持 user namespace 或资源限制时明确返回 `unsupported` 并失败资格检查。
[生成器工具链锁](../../scripts/toolchain-lock.json)仅管理构建与生成器，不是生产运行时 InstallLock。
非 worker 进程只能从原私有 manifest 读取 `Profile` 来登记同版合同；不会启动进程或宣称
环境 ready。因此共享部署应先启动 worker 初始化 manifest。

## 边界与恢复

每个 cell 是独立 bwrap 进程：unshare all、空 root、清空环境、drop capabilities、无网络，
只有准确静态 worker 只读绑定。prlimit 设置 AS 1 GiB、CPU 1–5 秒、FD 64 和 core 0；
解释器内存最多 256 页（16 MiB），worker Go 内存目标 64 MiB、线程上限 16。
wall 1–10000 毫秒、stdout 1–65536 字节、stderr 4096 字节；namespace 最多 65536 字节、
100 个 binding，WASM 最多 512 KiB。原 CostBound 必须覆盖 CPU 硬限制加一秒的粒度余量。
原使用量来自实际子进程 wait 的 user+system CPU，独立于 Task 费用账。

cells 与平台 probes 共享有界活动槽，只有实际 wait 完成才释放。Root 用私有单宿主 flock
持有所有权。Prepare 只固定准确代码/输入/namespace、generation、CAS 和完整来源；原
数据库 barrier 提交成功后，fsync 原 Attempt 入口 journal 才允许一次 spawn。
提交未知不 spawn；原 journal 最多 10000 项。spawn 次数未知时明确标记未知，不填确定零次。
原日志写入前保守占目录额度；rename 已完成后的目录同步失败仍保留该额度，当前进程
的早期失败也可提前耗尽。重开按原独占目录实际日志计数，已有 Attempt 核对/更新不追加
额度，不删除原终态日志。`Config.JournalFault` 只用于准确 native rename 后的目录同步
故障验收；默认未配置，不替换程序效果或使用量。

成功输出、准确来源、新 namespace head、原 Operation 和 Job 在原短事务共同 CAS 提交。
trap、OOM 拒绝、超限和取消都丢弃 provisional stdout。当前 generation/namespace 冲突不发布
旧实例输出。Reconcile 只读原 journal/Content，绝不重建或重跑已经开始的程序。
取消会杀原进程组并 join；宿主死亡使用 parent-death/namespace 生命周期隔离，恢复按原
PID/start-time fence，不能仅凭 context 取消报告退出。实际退出已确认但原结果/CPU丢失时，
保持 unknown 结果/账单。原 barrier 未进入且 journal 缺失、原调用已退出时封存零次入口；
已经结清的原 journal 缺失则明确报错，不改写已知费用。

## 验证范围

[公开边界验收](../../conformance/integration/wasi_test.go)使用真实 SQLite/PostgreSQL、
实际静态 worker 与独立字面 WASM：42、stdio、环境/FS/socket 拒绝、trap 后输出不提交、
内存/CPU/wall/输出限制、旧 CAS、原控制取消、丢内容答复后删原代码并重开恢复。
[真实宿主 SIGKILL 验收](../../conformance/integration/wasi_crash_test.go)独立观察内核
PID/start-time，确认旧 worker 已退出，重开原 Attempt 保留未知与原 namespace，不重放。
[目录容量故障验收](../../conformance/integration/wasi_journal_capacity_test.go)用有界历史文件
预置 9999 项目录前态，准确 rename 后注入 EIO，核下一新 ID 拒绝、原日志及重开核对。
该前态不代表实际运行过 9999 次程序。[环境收尾故障验收](../../conformance/integration/environment_cleanup_fault_test.go)
使用原 SQLite 文件的真实 INSERT trigger 故障，核环境、原拒绝回执及必需 cleanup Job
共同回滚；移除故障后同一 Claim 完成准确拒绝和实际关闭。
这支持选定本机 Linux profile；其他 OS/架构、任意原生程序、自定义 guest hostcall、
跨设备/跨 owner 权威、生产 AZ/断电与多物理 Attempt 未由这些测试证明。
