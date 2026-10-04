# 04 票02：race unknown 隔离与有限继续裁决

2026-10-04。必要决定，纯只读。源码固定 `57ea60c628f82140f5700503107d3e2fe4a86dec`；已读本轮实际 `run-native.py` 的ACK、120秒wait、killpg/Wait与group_absent判定。以下进程事实由root提供，本代理未运行/probe进程、FD、DB或测试。

**采用：原run永久保留未确认资格，隔离其准确scope；在root/sole owner确认只有原身份的Z成员且无其他活执行者后，允许唯一LOCAL槽使用全新独立已ACK scopes继续。** unknown-cleanup 与仍有可执行producer是不同事实，不能因未回收Z无限禁止无关隔离工作；也不能用继续执行的授权抹掉原unknown。

## 精确保留的原事实

- race toolsession25210，外层120秒到期；原go PID/PGID `2278478`，SIGKILL后直接parent Wait=`-9`，wrapper exit125，`group_absent=False`。这次race不是green、不是已完成测试，也不是正常Close证据。
- 精确group唯一被报告成员：`component.test` PID `2279009`，state Z、PPid1、Uid1000、FDSize0、Threads1；fd目录EACCES。Z表示该进程已结束执行，不能继续用户态/内核系统调用或再次生产测试输出；**FDSize0不是打开FD读取结果，不能代替FD/close ACK**。无权等待的已收养grandchild尚未被回收，不伪造其Wait。
- 原 `lerna_test_6b8f60ee6b09466c308df88e`、`lerna-content-objects-4130439993`（device27/inode431524）及该run其他未确认责任均保留；本次不能凭Z/group signal重试清除、猜目录归属、删除schema或补写success。已确认的14组exit0/1+absent只证明各自范围，不替本次证据。

## 继续执行的具体条件

1. 原run/其scope记录为quarantined、cleanup unknown，保留原PID/PGID、若已有则starttime、wrapper原日志/ACK和精确dev/inode；引用当前事实截点。sole owner在继续前核准确原group无非Z成员、无其他活动wrapper/producer/原清理调用。若看到可执行成员或无法界定原责任，先不启动新native。
2. 原Z不会从Z恢复运行；后续PID/PGID重用不能被误当原进程或成为kill理由。不得宽泛kill PID1/相邻进程，不尝试替非parent伪造wait/reap，不反复kill一个Z来“证明关闭”。将来若独立观察确认原group真消失，仍须走原责任的明确核对资格，不能自动删除所有旧资源。
3. 新run使用新的已确认原生root/注册账本/准确scope身份；父root ACK 27/431344不能替子scope ACK。新consumer不得采用、枚举清理或碰触原quarantined目录/schema；所有旧不确定资源继续占据真实容量，不能把它们假扣成已释放。有限资源不足则停止该run，不通过清unknown凑容量。
4. 唯一GLOBAL LOCAL所有者保持。新run正常退出仍要求真正直接Wait、pipes结束、准确group absence、原生close/scope资格；不能把本次Z裁决推广为以后“只要超时就继续”。每次timeout/nonzero按真实事实记录，未完成suite不得标通过。

## 下一次最小执行形式（仅批准方案，非本代理执行）

优先将本次混在 `go test -race` 里的编译与运行分为两个有限受控步骤：

- 在新的owned builddir做准确source/flags下 `go test -race -c`，只在build真实exit0、直接Wait/pipes/group资格完整后使用其准确binary。编译失败/timeout保持该scope未知，不能运行半成品。记录源码SHA、包、编译flags和产物hash。
- 编译完成后另一个独立owned run执行该真实binary，保留原 `-test.timeout=120s`/原race环境及fixture lease/budget；外层仍有限120秒及原有限终止/Wait协议，不扩业务deadline，也不把切分计为一次未中断旧run。父子结果分别记录，不能把build green等同race green。
- 如果实际运行本身确实无法在原有限窗口完成，可按有实质边界的测试名单分组，每组精确、互相覆盖全部原受影响suite；保留共同race flags/所有子用例及必要同组并发场景。记录完整manifest和所有分母，不以正则漏测/跳过慢用例取得green。该做法是有界调度，不允许修改业务lease或吞失败。

拆分只能去掉编译耗时与执行耗时混淆，不保证下一次必定green。原120秒与binary120秒相撞仍可能被外层先杀，必须如实处理；不要延长旧run后改称其已通过。若受控deadline内无完成结果，报告失败/未知，按实际耗时调整有限分组而非改业务上限。

本裁决不新增生命周期框架、不修改repo或runner，不承认真实native Close failure或物理擦除。root采用后才由sole owner开始新步骤；原unknown作为未关闭责任留在最终证据/cleanup freeze中。
