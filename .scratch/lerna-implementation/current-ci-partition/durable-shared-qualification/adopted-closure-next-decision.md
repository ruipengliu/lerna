# Durable 分组候选：原 closure 60 秒失败后的唯一下一决定

**保持 d0 分组不变；当前 wholeR 资格失败且尾部未覆盖。下一步只做一次有界的执行条件与成功对照来源读证，不重试 wholeR、不先改业务算法。** 现有数据不足以选择一个具有已证实因果的性能修复；更细分组也不能修复一个已经单独运行的60秒业务。

## 实际来源与已发生边界

固定 HEAD `d0fcc61264904070ce94e1146f07327a0ef9b2aa`，WT `/tmp/lerna-worktrees/current-ci-partition`。本次全文读：

- race 原63行 `/tmp/lerna-ci-partition-durable-race-execution/partition-shared-race.log`，5309B，SHA256 `27504b39e046d3af94884242ee94507db667ef709aefb05084c4fb59debb2f71`。
- normal 原68行 `/tmp/lerna-ci-partition-durable-normal-execution/partition-shared-normal.log`，7788B，SHA256 `0cc6444494dc2a92531fba7881a3df14c88553d017342ee020a0c2aab3d2e95f`。
- 实际路径为 race root 下 `sharedrace-source-postlaunch.json`（不在 release 子目录），SHA256 `50892e3c7bff5cb13fd0d94e8b1ed48e9d501ef35f2b19735720ce2711b3963b`。记录286模块/Run30不变、原Wait及group absence；另独立核其 qualified6 当前 bytes/hash全相等。本次不重做资源审计。

race Recovery101.199成功，发现201；closure60.10、包60.170失败于 `content_closure_test.go:132` 的 i=64/stage=install。这是从测试开始的原60秒context到期，不是120秒包或1445秒外层超时，不是第64次安装本身用了60秒。此前完成0..63；64的接纳/发布/最终完整Get及65拒绝未到达，get四项实际调用数均0。原五个Go child真实Wait/退出记录完整，make2，非外层UNKNOWN；这也不等于各原scope首次Close均成功。

normal相同入口完整12调用/201及fixture尾成功，closure18.481、Recovery41.783。root报告另一准确main e0 CI closureR20.791、RecoveryR54.795；本次未取得该CI完整身份/环境，故它是待绑定对照，不能据简称当同源码同环境证据，也不能用其成功覆盖当前失败。

## 成本与源码说明

累计 install1.6958s、put34.9795s、step23.1354s；当前安装调用到失败仅约14ms。完整12 cells已读：Put Now4480/2.9087s、CheckPolicy2080/6112逻辑动作/2.6613s、LockVersion2144/4.4726s、ScheduleRetention64/19.3503s；Step对应8448/5.5600s、4160/12224动作/5.0937s、4096/9.0249s、0；Get全0。它们是客户端包围调用的wall，含编码、驱动、等待、调度等，不是服务端execution。ScheduleRetention含子调用，不能与各层总量相加，也不能把put减去几个port后余量命名SQL等待或CPU。

源码核对：`domain/content/closure.go` 仍完整中间版本、每节点当前policy/完整ref/BodySeal/当前cap和锁后clock；`policy.go:CheckPolicy` 为MATERIALIZED锁行后真实clock；`management.go:scheduleAdmission/scheduleObservedSources` 已复用同Tx锁定的完整Source记录，但仍核原保存主体/用途的当前政策、原basis、每个AdmissionTarget独立责任及原deadline。不是可以删除的重复扫描。`service.go` 调度后的最终新准入与两短Tx拒绝保持。没有证据支持弱化这些门禁。

当前四个相关blob SHA256依次为：测试 `3eaa3327a4798bf90574c3e2312773350028a1779727c6797428d12041372d31`；closure `9b7ad1b320dbecabad9d4d6593adc1d4b458bcf9d9fd33452b3338683080d1c0`；management `c6fa3aa3dfee98ab82b873aa9d37e4c4725133390147e2d0aff04be49e3606ad`；PGpolicy `3cb31490a70e0141daa24b4766295ea1a57598194877eda6f19e58371a2d45d9`。03另一WT的clock/memo改动不能未经整合资格直接搬来冒充d0修复。

## 允许的唯一下一步骤：有限比较证书

由原owner准备一份一次性、无新业务执行的比较记录，先消费已有文件/CI元数据；不启动Go/PG查询/benchmark/profile、不给环境预热、不循环采样：

1. 绑定成功CI完整commit/run和原日志，对比本case、Content调用链、依赖锁、Go/race flags与原期限；用实际blob差异，不用“相同测试名”。记录测试发现/分组顺序，闭包此前Recovery已结束，不凭曾运行过Recovery推断仍有并发。
2. 比较已有可用的实际Go工具链/构建配置、runner CPU限额/内存、PG服务端版本与连接部署类别、存储/网络类别。`psql17.11`只是客户端，不能推成服务端17。只记非秘密描述与现成证据，禁止读取DSN/凭据。
3. 如已有失败时段的quota/throttling/pressure或PG活动记录，只读其准确时段。不存在就填unknown；当前累计计数或此刻低负载不能倒证失败时段。必要的新只读环境取证另列具体缺项和有限命令给root审定，本次不执行，也不把泛环境audit作为无限前置。

这一步的出口是完整有限表（包括unknown），不是“等环境恢复”或持续探测。若证实配置偏离已约定装配，才提出该一个具体修复及原bounds验证；若来源相同且缺历史资源/子成本事实，明确尚不能归因，并依据缺失项选择一个后续测量，而非无分析重跑。当前全局变慢与不同运行的wall差异仅提示需核对，不能证明CPU争用、DB慢或缓存冷热。

## 保持的门槛

拒绝再拆closure、拆其完整64链/65反例、挪setup出60秒、提高60/120/Claim5、增加pool/DB资源、猜索引/cache/跨Txauthority复用、跳过尾部或用normal代race。当前race Content97/Durable60/other43/fixture尾均未执行；没有“race201通过”，也不能提前接受新Durable分组完整运行资格。已证明的机械/独立审查/normal仍有效，不因这次失败虚构分组实现错误。

保留原失败、所有UNKNOWN与受测源。06原剩余资格及其他独立工作继续按唯一LOCAL安排；本决定不占用它、不引入它们的新前置。本文只有STATIC分析与/tmp文档，无测试、环境探测、源码或票状态变更。
