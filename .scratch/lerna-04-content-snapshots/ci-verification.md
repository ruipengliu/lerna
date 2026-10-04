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
