# d0 分组候选：必要最终检查计划（STATIC，未执行）

固定 WT `/tmp/lerna-worktrees/current-ci-partition`，HEAD `d0fcc61264904070ce94e1146f07327a0ef9b2aa`，已接受基线 `b9db5cc`。依据根 AGENTS.md 的统一检查与准确证据规则、`.scratch/lerna-implementation/issues/current-ci-partition.md` 的完整入口要求。四源之外，accepted Run30、原 Go/build286 及六个资格源保持原 pin；执行前/后重新核验。HEAD或产品字节变化则停止本计划，按新候选范围重新判断，不能继承本候选资格。

## 输入和当前事实

- 机械13控制与两个独立轴已完成；P3重复flags保持KEEP。架构复核没有必要冲突。
- 新normal根 `/tmp/lerna-ci-partition-durable-normal-execution`，原ACK dev33/ino465377/mode0700。`sharednormal-release.json`、`sharednormal-source-{pre,post}launch.json`、`sharednormal-selector-proof.json`、`sharednormal-owned-scope-sidecar.json`、`protected-unknown-inventory.json`及原raw/逐child ACK为输入。其记录normal完整PASS，原12 children0024–0035，实际counter24→36，原scope区间1–1081；本计划仅读取元数据，不替root原日志独立核验。
- 新race根 `/tmp/lerna-ci-partition-durable-race-execution`，原ACK dev33/ino474553/mode0700。起草时已出现 `sharedrace-release.json`：原whole PID4094347/start17177230实际exit2、groupAbsent/noTimeout、STOP/RELEASE，counter36→41，原scope区间1–294。完整R不是PASS。原raw、实际children及missing tail必须保留，不能继续尾段或盲重跑。本计划不解释该失败原因，也不授权修改产品。R的最终scope/UNKNOWN sidecar仍需原owner准确提供。
- 原normal/race根的账本从各自line1开始，不能混为旧账本续行。根和raw保留；敏感DSN和完整credential记录不输出。

## 一次基础检查

在原owner完成R退出并由root确认soleLOCAL后，可单独调度 **一次原 `make check`**，cwd为固定WT。它检验lint、生成一致性、Node工具消费者（含13分组控制）、非integration Go、TypeScript、实际双向合同及构建；不另重复单独13控制，不另补同型测试。此基础检查独立于R失败，不用于声称缺失的shared R尾已通过。

使用原锁定环境：GOTOOLCHAIN=local、GOFLAGS=-mod=readonly；真实Go不经当前whole专用shim。沿用现有 `run-native.py` 的原120s有限监督及5s终止Wait，原构建工具内部期限不变。由root/owner在首次效果前分配单一0700 final-check根，固定dev/ino、ledger与raw名；复制原controller而不是新建资源框架。命令参数为原 `make check`，不得注入测试过滤、缓存清理、额外warmup或改Makefile。原PID/PGID/starttick、实际Wait/groupabsence与raw fsync/firstClose分开记录；失败立即停止，不续预算/重试。生成一致性检查若出现真实产品差异，也停止保留，不自动改源。

## 一次有限只读资源审核

审核与其他native串行。前置为两个新root的实际STOP/Release及完成的owner scope提取；只读取上述N/R完整原区间以及本次make check精确区间，冻结行数/字节/SHA。N输入已给出598条PG登记、586个unique schema与309个FS路径；R及make check数量从实际登记计算，不猜零或补登记。

复用已有只读catalog审核方式。若owner需要新的精确入口文件，只写本次有限审核脚本/SQL及清单，先让root静态核pin，不建通用框架。原命令、数据库配置读取方式、监督器在root放行前必须具名；秘密仅经既有受限文件加载。禁止全库前缀扫描或把无登记对象纳入scope。

数据库只开一次有限审核连接/事务：caller20s、连接3s、事务只读，statement2s/lock1s；失败即停，不重试。仅对冻结的**精确登记集合**参数化查询：

```sql
BEGIN READ ONLY;
SET LOCAL statement_timeout = '2s';
SET LOCAL lock_timeout = '1s';
SELECT nspname FROM pg_catalog.pg_namespace
WHERE nspname = ANY($1::text[]);
-- 仅当原记录具备PID、backend_start及原database：
SELECT a.pid, a.backend_start, a.datname
FROM pg_catalog.pg_stat_activity a
JOIN unnest($1::integer[], $2::timestamptz[]) expected(pid, started)
ON a.pid=expected.pid AND a.backend_start=expected.started
WHERE a.datname=$3;
ROLLBACK;
```

SQL为参数化接口说明，不能把`$1`等未绑定文本直接当psql脚本执行。只有PID而无原start的记录单独报身份不完整，不能用现在观测补原代次。新审核连接需原scope意图、实际server identity和自身Close结果；事务Rollback/连接Close/进程Wait/current absence分别留证。

FS与/proc仅迭代冻结清单：每个原exact path一次lstat，不递归glob、不follow symlink；对原PID/start/PGID逐项观察当前代次/组是否存在。保留原identity与当前观测区别，`not found`不补logicalClose。结果只输出审核集合、实际presence/absence、原ACK、未知及限制，不输出凭据。

## 永久保留，不纳入清理或重认证

- `/tmp/lerna-current-ci-partition-execution/lerna-local-lifetime-4236457110`：原stickyUNKNOWN dev33/ino431541。
- 新normal `/tmp/lerna-ci-partition-durable-normal-execution/lerna-local-lifetime-3810108689`：SIGKILL原Store.Close UNKNOWN；当前观测dev33/ino473830只是后置观测，不能补首次身份/Close。
- R原区间可能增加新的UNKNOWN；必须从原记录提取并保留，不由whole退出猜已关。原owner补充准确sidecar后root再核。
- 旧root `/tmp/lerna-current-ci-partition-execution` 原1436行/counter15，`/tmp/lerna-current-ci-partition-after-run-execution` 原982行/counter24；机械根原159行。只校验既有prefix pins不变，旧Run接受范围、旧02/03/05和所有历史UNKNOWN不重开、不迁移、不清理、不补证。

本计划没有DROP、对象删除、杀旧进程、重新打开业务Store、绑定旧root或修复未知Close步骤。只读审核不能授权cleanup。必要交付仍须R失败按准确证据解决、完整共享资格和新CI；不能因基础检查/当前absence把claimed改为Done。
