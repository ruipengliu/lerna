# 首票整合的准确 CI 验证

2026-10-04，root读取 GitHub run、两个 jobs 的全部 step 状态及实际日志。
[run 37174778053](https://github.com/ruipengliu/lerna/actions/runs/37174778053)
准确 head 为 `c9de1baff7f481c2a9e4dde3c7873af43151819b`，push/check，completed/success；
job `111355029191`（durable-admission）和 `111355029318`（contracts）均全部 step success。
这是01/04/05整合检查点，不能替代未实施02/03/06或whole03退出。

| 实际执行范围 | 日志结果 |
| --- | --- |
| 固定 PostgreSQL restore client | psql 18.6；工具生命周期 race 2.618s |
| 原 PG/SQLite recovery | normal 23.166s，race 50.347s |
| 新真实 PG Component | normal 21.061s，race 33.892s |
| 新真实 PG Source／旧writer升级 | normal 7.243s，race 9.385s |
| 历史 v1/v2 manifest | 27项逐项 OK |
| 契约 | 新81／旧158，共同正反序；positive实际 Go→TS 和 TS→Go byte roundtrip |
| 基础检查 | bootstrap、format/vet、生成一致性、Go/TS测试、build全部通过 |

数据库测试均 `-count=1 -timeout=120s`，新增 PG 包普通/race按 `-p=1`顺序运行。
一些不变 Go unit checks在共同往返脚本第二次运行显示 cached，不能将全CI称为全部无缓存；
真实数据库运行及两语言字节harness均有本次执行日志。远端target/fault-plan普通测试1.176s，
其本地race3.615s见05证据，此次CI没有独立target race，不扩大验证范围。

01最终受测源696ac49，交付d806a92，正式merge5307702；c9仅新增checkpoint文档。
本记录没有重新运行低影响文档测试，也没有改变已发布迁移、旧codec或归档字节。
历史未知scope/CID限制、机械driver与native数据库故障的区别仍按退出证据保留。
