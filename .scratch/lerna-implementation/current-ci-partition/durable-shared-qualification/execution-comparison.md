# Closure 首次 race 失败：有限执行条件比较证书

**相同 closure 测试、282 个 Go 源码、依赖和原 60/120 秒边界，在准确成功 CI 与本次失败来源间均字节相等；目前无法把失败归因于 CPU、PG 等待、缓存或某个算法。** 分组 shell 的差异发生在 closure 调用之后，closure 的发现、独立 selector、运行顺序与 flags 保持。此证书只读取已有记录/Git blob，已经收口；不依赖持续审计或“等环境恢复”。不重试、不改产品。

## 准确来源与本次有限范围

| 来源 | 准确身份 | 实际结果及边界 |
|---|---|---|
| 失败候选 | d0fcc61264904070ce94e1146f07327a0ef9b2aa，tree7b13b76444649f7127030e9686039cd13c63d181，当前 clean | ONE wholeRace，actual Wait2/current PGID4094347 absent/no outerTimeout；children0036–0040 全部原 Wait，counter36→41 |
| 成功 CI | e0c66f6dec1325f648060248207056de1b0ced06，run37305738280，durable-admission job111748861766 | observed.json completed/success，全部实际步骤成功；原 durable 日志591行。CI 使用旧 Content49/48、Durable60，不包括 d0 分组候选 |
| 本候选普通入口 | 同一 fixed d0，normal root33:465377 | ONE wholeNormal，12 actual calls0024–0035，counter24→36，201 selectors/七组/fixture尾实际完整通过 |
| 本候选 race 原根 | race root33:474553/0700，controller3原字节不变，shim只重绑路径 | 原1445单整体 deadline173217.30429753，不续期；原每包120，首失败停止；0041–0047 未执行 |

CI 原日志的实际 race closure 段是11:58:36.5962048Z开始、11:59:01.7091506Z包完成（行413–414），包20.791s；Recovery 行409–410 包54.795s。在本次源内 Recovery 包101.199s，closure test60.10s、包60.170s。d0 normal 对应为41.783s/18.481s；CI normal 为24.312s/8.264s（行350–355）。这些 wall 差异只说明运行条件/成本需要证据，不能证明 CPU 争用、PG 变慢或冷热缓存。

本次 closure 原 pre/out 文件 mtime 分别12:33:59.708767Z、12:35:01.243210Z；父 pre/out 为12:32:13.961477Z、12:35:01.267727Z。它们是原文件写入时标，可供以后有界历史日志检索，**不是登记中的 wall-clock pre-effect ACK**。PID generation 原 starttick17187805 与同一 whole deadline 已原登记。没有该窗口 CPU/quota/pressure/PG activity 采样。

## 字节比较：调用链、锁、依赖与入口

原 module286 inventory 为282个Go源+Makefile/go.mod/go.sum/shell；准确比较285项相等，唯一不等为分组shell。5项额外workflow/restore-client/Node依赖锁相等。所有282个Go源相等，覆盖下面 helper、fixture、domain、PG/本地对象存储、runtime、contract调用链；并非仅比较同名测试。全部291项 blob/SHA256 的有限机器附录是 `/tmp/lerna-partition-durable-closure-execution-comparison-blobs.json`。

| 路径 | d0 与 CI 共用完整 Git blob | 比较 |
|---|---|---|
| `conformance/component/content_closure_test.go` | `22fbb51d6d4904b6bae802b884482500d76ada6c` | byte equal |
| `conformance/component/content_publication_test.go` | `040f5bc4bd3d906cf8a09d6294090463fe1beb94` | byte equal |
| `conformance/component/content_identity_test.go` | `c53fc5b0408bc2ec2a2acc7e29bb2b3c6401a686` | byte equal |
| `conformance/internal/contentfixture/world.go` | `c7f0d553dc2a6f76b17c2867a713a0c909b3955e` | byte equal |
| `domain/content/closure.go` | `a3ce2e65bed2872fa5d941e6efdd457f8965f687` | byte equal |
| `domain/content/management.go` | `841f934366813b05439c775ce22c243d65f0b26e` | byte equal |
| `domain/content/service.go` | `39381bdbe9de4eb6926896d129a53d853eae407e` | byte equal |
| `domain/content/lifecycle.go` | `46a27b1b3e3001c964b47bbdb04e052a8627f093` | byte equal |
| `domain/content/ports.go` | `96edd91a376cdffbc9d87ebb6347dfa01bedaa96` | byte equal |
| `adapters/postgres/content/policy.go` | `b1952900799084c64366f064e738045faf811b72` | byte equal |
| `adapters/postgres/content/management.go` | `9ad82d63fa5d25ac64118cf6bf86d82a32743b3b` | byte equal |
| `adapters/postgres/content/facts.go` | `c32c7bfea9507c41b7878b84fe1a979f6e1794bb` | byte equal |
| `adapters/postgres/content/store.go` | `f458b1ee30d4f390fc1509b70b076c5d529d23cd` | byte equal |
| `adapters/postgres/internal/pgstore/core.go` | `278ec84b595284404e37d7f89234fca908f6325e` | byte equal |
| `adapters/postgres/commands.go` | `ae0da03a39557fcd61fc9e1d83cf20ffdef2659c` | byte equal |
| `adapters/postgres/schedule.go` | `438b25fda95916c47f88a29618b3c915c1550a31` | byte equal |
| `adapters/postgres/retention.go` | `fd9ee5633b0c2b29a9123f890ce633b0bcc5acbd` | byte equal |
| `runtime/admission.go` | `8538979e2e38f790f7bc2b0086ce597c99e817a4` | byte equal |
| `runtime/work.go` | `67659ba792dee48e1b3c6bf17fc2585acdddd2f2` | byte equal |
| `adapters/objectstore/local/store_linux.go` | `b711c4706ac6a9b3ca23c3e2e008009ff817de33` | byte equal |
| `go.mod` | `1cb846251dfd02f51750d176d4e1cfe0262a657f` | byte equal |
| `go.sum` | `03de684779e4a9f36a8de5237c7d60ca9c85c49a` | byte equal |
| `Makefile` | `12610df8beeb5c6efe4d8cd0df234297a9e924e4` | byte equal |
| `.github/workflows/check.yaml` | `e91e7a269a0aec471534623ff70fd38b6d004689` | byte equal |
| `scripts/ci-psql.sh` | `e64ed844601ebcd06862d3f297e7873ea98062c4` | byte equal |

四关键诊断 blob 与 adopted decision 匹配。decision 的 management 指 domain 文件；PG adapter 同名文件另列，避免混淆。

| 关键路径 | 两来源相同 SHA256 |
|---|---|
| `conformance/component/content_closure_test.go` | `3eaa3327a4798bf90574c3e2312773350028a1779727c6797428d12041372d31` |
| `domain/content/closure.go` | `9b7ad1b320dbecabad9d4d6593adc1d4b458bcf9d9fd33452b3338683080d1c0` |
| `domain/content/management.go` | `c6fa3aa3dfee98ab82b873aa9d37e4c4725133390147e2d0aff04be49e3606ad` |
| `adapters/postgres/content/policy.go` | `3cb31490a70e0141daa24b4766295ea1a57598194877eda6f19e58371a2d45d9` |
| `adapters/postgres/content/management.go` | `83012672bfb97efd82d63516dfb20437172db736b5f34e758e8806287fd1ba62` |

两个 tree 除 `.scratch` 外仅4项不同：分组shell、外部stub控制测试和两个literal JSON fixture。shell 精确差异仅原closure调用后的Content2→3/Durable1→2数组及顺序run_group；closure selector是 `^(TestContentFullClosureIncludesIntermediateVersionsAndExact64Bound)$`，原 closure=1 两边一致。`run_group`及discovery/flags/escaping/退出合同无差异。

`domain/content/closure.go`仍在同一Tx锁定并资格检查完整中间版本、Ref、BodySeal和独立当前policy/cap；64/65完整边界不变。`CheckPolicy`仍是MATERIALIZED行锁后 `clock_timestamp()`，`Now`是真实PG clock；`domain/content/management.go:496/625`复用原同Tx完整source记录，分别保留原saving subject/purpose、policy/change/basis与每个AdmissionTarget责任和deadline。PGadapter ScheduleRetention转入该domain函数。service调度后最终准入与发布事务没有差异。没有证据允许删掉门禁、跨Tx复用权限或猜索引。

## 执行与环境：只消费已有非秘密记录

| 项目 | 准确成功 CI 已有事实 | 本次 d0 已有事实 | 比较与限制 |
|---|---|---|---|
| Go release/平台 | 原日志238/285：go1.27.1 linux/amd64；GOROOT `/opt/hostedtoolcache/go/1.27.1/x64` | 原注册realGo `/home/agent/.local/toolchains/go1.27.1/bin/go`；冻结SHA30969f97169d7f43fe6a085873d75613adc21e30818a8c61d95bd27275df4624；已有VERSION go1.27.1，2026-08-28 build时间 | release/platform一致；CI二进制SHA未保存，不能称二进制逐字节相等 |
| build/CGO | setup已有env：CGO1、GOOSlinux/GOARCHamd64、CCgcc、GOAMD64v1、GODEBUG/GOEXPERIMENT空 | make prerequisites通过原真实env调用；Go archive历史verification linux/amd64。该次完整CC/CGOFLAGS/GOEXPERIMENT/GODEBUG/GOMAXPROCS未保存 | prerequisite符合；未保存变量unknown，不新执行go env |
| GoFlags/GOTOOLCHAIN | setup日志为auto/空，是make之前；字节相同Makefile export local/readonly | manifest与原native argv明确local/`-mod=readonly`，同Makefile | 实际make recipes要求一致，不能将setup前env误称最终make flags不同 |
| closure test argv | 同shell/static blob `test -p=1 -count=1 -race -tags=integration -timeout=120s ... -run <closure exact union>` | 原0040 full argv完全登记，以上相同 | race、p1、count1、tags、pkg120和selector相同 |
| Recovery/分组顺序 | Recovery完成→fresh discovery201→单closure→Content49/48→Durable60→other43→fixture | Recovery完成→fresh discovery201→单closure失败；其后Content33/32/32、Durable30/30等未启动 | 分组差异在失败之后；不能推断旧Recovery仍并发 |
| 更多入口前史 | 同job先restore-client lifecycle race，再完整normal，再sharedrace；日志300/309/368 | earlier normal实际完成；新独立race root，期间其他任务按soleLOCAL串行调度 | 前史不完全相同；未证明缓存冷热是原因或CI成功覆盖d0 |
| 业务与外层界限 | 原test60，工作预算5/发布1min；pkg120；workflow无显式whole1445 supervisor | 原test60，工作预算5/发布1min；pkg120；原单whole1445实际未超时 | 业务/pkg一致；CI与本地行政外层不同，当前失败不是外层timeout。nominal1565未取代1445 |
| Go依赖锁 | go1.27.1；pgx/v5v5.11.0、go-sqlite3v1.14.52、jsonschemav6.0.2及同go.sum | 同blob及完整SHA | 相同，不新下载依赖或bootstrap |
| fixture连接/Tx边界 | 同world.go MaxOpen1、Tx3s、statement2s、lock1s；同core.go的ReadCommitted/synchronous_commit on | 同字节源码/同真实fixture | 配置源码相同，不证明每条SQL服务端时间相同；不增加pool/PG资源 |
| runner/OS | 原日志1–23：GitHub Hosted Compute Agent/Azure westus2，Ubuntu24.04.5 image20260927.320.1 | 既有verification描述当前开发环境及Docker；当前内核/发行版/实际CPU容量未在该次记录 | 两种执行环境不同；不得用Ubuntu标准规格猜CPU/内存 |
| CPU quota/内存/pressure | workflow无显式CPU/Memory额度；实际runner配额/历史pressure未记录 | 本次未保存cgroup限额或失败窗口counter/pressure；soleLOCAL只保证本团队授权执行互斥 | unknown；不代表整台机器隔离，也不凭当前累计值倒证原时段 |
| PG服务端版本 | 原server startup日志469/561明确18.6 Debian18.6-1.pgdg12+2；不是client推断 | 2026-10-03已有verification19记server18.6；部署README25/33记同镜像digest | 历史配置一致；本次失败时运行server实例/运行期设置未另证明，禁止新DB query |
| PG镜像 | 原startup/拉取及workflow digest3725f4e2499eef5134592b3b4ab79a543ed7f8e533b05b5b637af926630f6650 | 既有README33声明同digest | static配置相同，本次服务container当前identity未探测 |
| PG部署/网络类别 | job专属Docker service，5432 host loopback映射，workflow/原Docker服务日志 | 已有环境记录：本机Docker服务、loopback端口、持久volume `lerna-dev_postgres-data` | ephemeral job service与持久开发数据库前史不同；实际网络延迟/PG争用unknown，未读DSN/凭据 |
| client/恢复工具 | CIimmutable container restore wrapper；服务器/客户端18.6类别有原日志 | 原本次psql日志17.11，已有verification pg_dump/pg_restore18.6 wrappers | localpsql17.11仅client，不当server版本；closure本身不执行旧dump restore |
| 存储类别 | 同Linux本地对象代码、原TMPDIR为runner临时目录；PG在service容器 | 同Linux本地对象代码，独立0700 /tmp root；PG历史Docker volume | 文件系统/块设备/IOPS/缓存/网络实际性能unknown，不做环境预热或取样 |
| PG服务参数 | CIinitdb记录max_connections100/shared_buffers128MB（日志461–462） | 本次未保存运行期PG参数 | CI这是initdb记录；不能推成当前local设置不同或直接致因 |
| 失败窗口PG活动 | CI的服务端原日志含它自己的成功运行/故障fixture语句，不能覆盖local失败窗口 | 原Recovery registry含role intent6/backend observation10；closure只有原schema/FS登记，没有失败窗口PG wait/activity | unknown；before-run backend observation不是closure活动采样 |
| 原Close与scope | CI成功不用于本地资源Close补证 | Normal原SIGKILL CloseUNKNOWN33:473830及旧33:431541保持；Racescope1–294/release295、FS123当前gone但不充当Close | 原parent/childWait、当前absence和原首次Close证据分开；无cleanup/adopt/backfill |

历史verification仅2026-10-03安装记录，不冒充失败时段CPU/PG telemetry。未读取任何私有DSN文件、services/env凭据，也未启动任何Go/Node/PG查询/profile/循环采样。

## 本次真实成本边界

失败发生于 i=64/stage=install，当前安装调用14.071497ms；此前0..63完成。累计install1.695832257s、put34.979533563s、step23.135417728s。第64个接纳/发布/Get与65反例未到达；不是“第64次install运行60秒”。成功日志没有-v诊断的同口径port cells，不能伪造对比。

| phase/port | calls | logical actions | 客户端包围wall |
|---|---:|---:|---|
| put/Now | 4480 | 0 | 2.908671214s |
| put/CheckPolicy | 2080 | 6112 | 2.661288864s |
| put/LockVersion | 2144 | 0 | 4.472614994s |
| put/ScheduleRetention | 64 | 0 | 19.350320266s |
| step/Now | 8448 | 0 | 5.560025182s |
| step/CheckPolicy | 4160 | 12224 | 5.093695068s |
| step/LockVersion | 4096 | 0 | 9.024945878s |
| step/ScheduleRetention | 0 | 0 | 0s |
| get/Now | 0 | 0 | 0s |
| get/CheckPolicy | 0 | 0 | 0s |
| get/LockVersion | 0 | 0 | 0s |
| get/ScheduleRetention | 0 | 0 | 0s |

这些是含编码/驱动/调度/等待的client wall，非服务端execution。ScheduleRetention包含下层调用，不能将各层相加，也不能把put减去port余量命名SQL等待或CPU。

## 缺项与具名有限下一读取候选（未执行）

证书现已完成，包括unknown；不等环境恢复、不将环境audit变无限前置。没有发现已证实的装配配置偏离，不能据现有数据选择具体业务性能修复。若root选择继续读证，只选必要的一项、单次、有界，仍保留本次FAIL，不自动重跑业务：

| 具名候选 | 待root判断的精确只读命令 | 能证明/不能证明 |
|---|---|---|
| current-cgroup-configuration-once | `python3 -c 'from pathlib import Path; paths=["/proc/self/cgroup","/sys/fs/cgroup/cpu.max","/sys/fs/cgroup/memory.max"]; [(print(p,Path(p).read_text()[:4096]) if Path(p).is_file() else print(p,"MISSING")) for p in paths]'` | 只绑定此时可见cgroup配置；仍需尊重namespace/ancestor范围，不恢复原失败时quota/throttling/pressure，不读凭据 |
| original-PG-log-window-once | `docker compose -p lerna-dev -f /workspace/.lerna-env/compose.yaml logs --no-color --since 2026-10-05T12:33:54Z --until 2026-10-05T12:35:06Z --tail 200 postgres` | 只检索原pre/out文件mtime±5s附近历史服务log、最多200行；可能没有足够信息，不作DB query/继续等日志/原始CPU采样补证。需root确认服务别名与脱敏保存范围 |

以上两项都未执行；历史CPU缺失不能由现在一次read补证。如后续仍缺原资源和子成本事实，应明确尚不能归因，由root单独决定一个有界后续测量；不猜优化、不放宽60/120/1445、不拆完整64/65链、不先改d0。

## 原件与机器证明

| 原件/记录 | bytes | SHA256 |
|---|---:|---|
| `/workspace/lerna/.scratch/lerna-04-content-snapshots/current-ci-e0c66f6/observed.json` | 4638 | `1e8d9d33b4f4b6d9dd46eb895a83520debcb8e27aa1e26d967844eebd3afbeea` |
| `/workspace/lerna/.scratch/lerna-04-content-snapshots/current-ci-e0c66f6/durable-sanitized.log` | 53004 | `9ea2db9b14fa32f6c96737e68ac3a966e0f6799d0ee3a89bda5b8fa2ddaf98cc` |
| `/tmp/lerna-ci-partition-durable-normal-execution/partition-shared-normal.log` | 7788 | `0cc6444494dc2a92531fba7881a3df14c88553d017342ee020a0c2aab3d2e95f` |
| `/tmp/lerna-ci-partition-durable-race-execution/partition-shared-race.log` | 5309 | `27504b39e046d3af94884242ee94507db667ef709aefb05084c4fb59debb2f71` |
| `/tmp/lerna-ci-partition-durable-race-execution/sharedrace-source-postlaunch.json` | 2423 | `50892e3c7bff5cb13fd0d94e8b1ed48e9d501ef35f2b19735720ce2711b3963b` |
| `/workspace/.lerna-env/verification.log` | 1469 | `58fe880f4754d6d3fc20c5f6882d821ff265c99739b67a0861c7c6c1106e5cc1` |
| `/workspace/.lerna-env/README.md` | 3877 | `ef34f2ee5d08a088902e629e53b8c3737becd60e5354a4c843ddd0406fc99d09` |
| `/home/agent/.local/toolchains/go1.27.1/VERSION` | 35 | `25eb74b09036e1ad894fb63b86cb0f0ee53342df8d2e2e860da4ff5079607758` |
| `/tmp/lerna-partition-durable-closure-first-failure-next-decision.md` | 5925 | `2d3187691528586d4ac2deb3fed6473b3291b348c74dc5f18359c3af543dee0f` |

Machine blob appendix SHA256: `84112a452f81e300274623b9d2c72926e4577a3621d892969a26b9009ced8bfa`；全部291项比较仅Gitblob/hash，无源改动。紧凑wholeN/R归档proposal `/tmp/lerna-partition-durable-shared-qualification-archive-proposal.json` SHA70345cc8e7ea8b84250dee2351741bcf485cb825d055ce31c002d54d42afb8c1，61files/642745B，当前只是proposal，未复制/commit/merge/push。Race原release6b9f578e…仍不可改写成PASS，7尾未执行，票仍claimed。
