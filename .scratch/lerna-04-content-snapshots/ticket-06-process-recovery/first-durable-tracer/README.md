# 首个真实发布进程恢复场景

06 来自准确 dadd801，独立部分提交 `4620ac336ca7fe1aa354872e89c7b53e67d89398` 保存首个场景；尚未合入或接受整票。它使用真实独立子进程、原 PG staging/Job 和 local 对象耐久化，先在实际 Put 完成而发布未提交的位置停下。

- [正常对照](durable-normal-control.log)：包 0.374s，实际 exit0，native3630391/start15227863；子进程真实 Store/Objects Close ACK、Wait 和 parent pipe Stop，原正文、固定回执、发布状态及重开尾通过。
- [同位置 SIGKILL](durable-sigkill-fault.log)：包 5.396s，实际 exit0，native3639190/start15265079；实际 SIGKILL/WaitStatus、EOF/Stop 后，原 Claim 自然到期，代次 2 从原持久 Job/staging 恢复，真实 Close/Wait、第二重开及原正文/回执/状态尾通过。

两次原组均当前不存在，无 native timeout。root 全文读 8/10 行日志、完整机器核对 17 源与 snapshot、准确 prelaunch/actual outcome 和登记区间。初次格式实际 3629434/exit0/absence；原窗口 caller30/child20/Claim5/work4/Tx3/statement2/lock1 及 outer120 均未改变。

被杀代次首次 Store/Objects Close 永久 UNKNOWN。准确 `lerna_test_a77911089367a14459f3d9e8` 与 `objects4136084765`（dev33/ino410968）保留；继任者 ACK、父进程正常关闭及内核退出均不补旧 Close。[normal](normal-ledger-interval.txt) 与 [fault](fault-ledger-interval.txt) 登记区间分别保留。尚未进行当前 catalog/path 独立审计，未执行 race 或剩余 AC。

[归档映射](archive-map.json) 记录原路径、字节与 SHA。Go 源副本使用 `.go.txt`；原 prelaunch/source manifest 的实际路径与源 SHA 不重写。完整 1.2 仍关闭。

后续[同场景普通／SIGKILL 配对竞态](durable-paired-race.log)实际通过：包 9.498s，native3659350/start15349458、Wait exit0、当前组消失、无超时或 DATA RACE；17 原资格源当时保持不变。root 全文读 13 行日志并完整机器核验 outcome 与源 snapshot。新增被杀代次 `lerna_test_c79770996c493281433e9ff1`／objects403060689（dev33/ino412800）的首次 Close 永久 UNKNOWN，准确[登记行21–35](race-ledger-interval.txt)与[first-race-release](first-race-release.json)保存事实；上文“未执行 race”是前一归档截点。提交前及其余 AC 尚未接受。
