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

## 首票文档检查点 e296 的真实 CI

准确 `e29675d75f5135995614e4eb2faadf6c2ac1581d` 的
[run 37175556877](https://github.com/ruipengliu/lerna/actions/runs/37175556877)
已 completed/success。root读取全部 job／step 和实际日志：contracts `111357314945`、
durable-admission `111357315094` 均 success；psql18.6、工具 race1.567s、旧双库
normal25.652s/race52.726s、新 Component22.831s/39.189s、Source／旧writer7.584s/10.546s，
81新／158旧夹具正反序均完成真实双向字节往返。远端target normal1.182s，仍无独立target race。
该提交只变文档，不能代表后来06或未合入02／03的代码验证。

## 06 正式整合后的 CI 边界

06实际代码 `a5005ab0d583f9a1906809cfd94c27daf89f5666`，交付文档
`6904b2d1b1fd177207342203bc6f9e4ef5a9e2f6`，正式合入
`58f8f0f855d4e0bce1bc7dda9be4e000ad0d2385`。本地检查与两轴复核见
[06最终交接](ticket-06-api-handoff.md#最终检查与本票退出)。本检查点push后的准确CI待核验；
上述历史success不关闭这一检查。实际工作流会在原recovery包normal／race中运行新增PG
Decision进程故事，基础check运行target正常套件；没有独立target race的远端步骤，
不得将远端范围扩成该证据。本地target最终race16.526s已有真实记录。


## 06准确949检查点的真实CI

Root已读取准确head949c39237fda562e8bda994a8e1454a27232dc72的
[run37185512618](https://github.com/ruipengliu/lerna/actions/runs/37185512618)、
两个job的全部step及实际日志，completed/success。contracts111386471262与
durable-admission111386471367均success；locked bootstrap、模块校验、生成一致性、
类型检查、JS20／TS37及build通过。81新／158旧共同夹具正反序均实际双向字节往返，
远端target普通1.521s；没有独立target race步骤。

固定psql18.6／工具生命周期race1.629s；旧双库与新增Decision进程恢复
normal29.421s/race66.181s；真实PG Component22.791s/38.557s、
Source及原writer7.785s/10.387s；27项历史hash逐项OK。数据库count1、有限timeout120，
新增Component／Source按p1顺序执行。部分不变unit输出cached，不称全部无缓存。

该CI仅覆盖949已整合的01／04／05／06，不覆盖后来候选票cad6905或未合入取消票。
候选本检查点的新push CI待核验；完整03profile、架构和最终退出仍未由此关闭。


## 02准确5bc检查点的真实CI

Root已读取准确head5bcdea8669adb49c341342e5b05deadffa0b8661的[run37189797348](https://github.com/ruipengliu/lerna/actions/runs/37189797348)、contracts111399418119／durable-admission111399418197的全部step及实际日志，均completed/success。locked bootstrap、模块校验、格式／vet、生成一致性、typecheck、JS20／TS37及build通过；83新／158旧夹具正反序实际Go→TS／TS→Go字节往返。远端target正常1.395，无独立target race；不变pure检查含cache。

实际psql18.6、工具race1.508；原PG／SQLite及06真实进程恢复正常25.852／race61.665；Component29.248／53.541；Source及970／两个FINAL01严格旧writer消费者10.519／14.128；原27 SHA逐项OK。DB命令count1／timeout120，新增PG包p1顺序执行。

这是02正式整合后的34/42子AC检查点；尚无后来03控制／Source0002／Decision0003／双prepared Stop合流／89夹具／完整profile。Source正常错误测试不等于native Wait或进程组故障注入；不宣称生产、断电或provider保证。本记录在下一产品整合后保存，不另以文档检查点重复触发旧CI；882及whole03准确最终CI待后续。
