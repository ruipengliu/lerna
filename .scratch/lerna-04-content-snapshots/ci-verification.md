# Content检查点准确CI

2026-10-04，root实际读取GitHub run/jobs与两个完整日志，按安全白名单核对结果，未打印凭据/原始环境。

准确push head **`7c0bce515f7b2fa443e2d00e1b14cfee22dccfa9`**，event push，[run37207013464](https://github.com/ruipengliu/lerna/actions/runs/37207013464) completed **success**。两个jobs所有steps成功：contracts `111450202691`、durable-admission `111450202489`。

| 实际范围 | 结果 |
| --- | --- |
| locked bootstrap/check/build | 35工具测试、44TS，format/vet/typechecks/generated相等，Go/TS build成功 |
| 同版共同黄金 | 158旧1.0＋89旧1.1＋101新1.2，各自两序真实Go↔TS往返成功 |
| 固定PG restore client | 实际18.6；真实container生命周期race2.973s |
| 真实normal | Recovery23.346s；Component52.653s；Decisionfixture13.205s；Contentfixture0.053s；Localobjects0.048s |
| 真实race | Recovery52.987s；动态全部133 Component＝60 durable61.550s＋73 other24.421s；Decisionfixture16.724s、Contentfixture1.178s、Localobjects1.065s |
| 历史durable来源 | 四份manifest共27项目，normal/race各校验一次，共54条实际OK；不将完整静态178对象资格冒充CI全部运行 |

所有Go集成保留-count1、timeout120，Component及race串行-p1；动态正向分组包含完整当前Test/Example/Fuzz种子，原时间上限未扩。新Content源码在此CI head真实存在，不能用此前64纯文档CI替代。

7c是首票交付1a7整合eba后的验收metadata，不改受测可执行源码；本地14测试、4ace普通注释资格及资源审计另见[首票退出](ticket-01-exit-evidence.md)。此CI没有运行来源权限票02的新代码，也没有证明全片04、生产/S3/断电或远端全环境无资源；首票1.2完整profile仍不广告。该检查点之后的f05只是02采用/认领文档，无产品/工具/测试变化，不能提前称02已验证。


## 票02准确904ec5f的CI

2026-10-04，root实际核 [run37227467190](https://github.com/ruipengliu/lerna/actions/runs/37227467190)，准确push head **904ec5fdf03d2e60fe2e9c85f34ba889d0110958**，completed success（updated19:22:45Z）。contracts111509992201、durable-admission111509992027全部steps success。root读取两份完整过滤日志，永久[捕获材料](ticket-02-ci-904ec5f/README.md)保留其过滤资格，不冒称原始日志逐字副本。

| 实际范围 | normal | race |
| --- | --- | --- |
| 原真实双库Recovery | 25.988s | 61.392s |
| 动态全部170 Component：closure1 | 11.065s | 27.656s |
| content_a33 | 27.754s | 39.117s |
| content_b33 | 34.504s | 42.228s |
| durable60 | 43.470s | 70.646s |
| other43 | 0.937s | 8.551s |
| decisionfixture | 14.960s | 19.394s |
| contentfixture | 0.071s | 1.133s |
| local objects | 0.051s | 1.073s |

动态完整inventory170、正向互斥五组及全部native package结果已实际读取；表中为各独立包结果，不相加作整体wall time。Count1/timeout120及Component p1不变，closure原完整64/65/finalGet未拆分。CI非verbose日志没有逐名RUN/PASS；每名一次的本地详细资格另在ticket-02-execution，不能把CI摘要当逐行用例日志。

Locked bootstrap/check为37Node/44TS、generator13拒绝+8变化、1.1 probes、gofmt/Prettier/vet/types/generated、Go/TS build；158旧1.0+89旧1.1+101新1.2两序实际Go↔TS成功。部分旧Go reverse子调用cached/no-test资格保留，不称全uncached。PG restore client/server实际18.6，真实container lifecycle race2.484s；四份durable manifest 27项每模式一次共54 actual OK。PG日志中预期写拒绝/锁超时与恢复故障保留，不能仅凭ERROR字样否定已通过的故障正常对照。

904包含票02正式merge6f88740及接受metadata，所有受测产品/tool对象与交付651036相等；先前7c首票CI仍仅证明原cutoff。新03/05独立WT代码不在904，15/41不等于whole04退出，完整1.2仍OFF。远端job成功/容器停止不补本地Z/Close unknown、不声称远端或本地全环境zero。
