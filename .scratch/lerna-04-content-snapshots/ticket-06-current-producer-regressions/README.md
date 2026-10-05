# 06 当前共享 producer：原五场景回归

冻结 `c9a09814…` consumer／`df707fae…` 26 文件树，原 durable、precommit、postcommit、policy-orphan、late-Finish 各正常／故障／配对竞态，共 15 条原命令全部通过。所有原期限保持。157 行完整原日志分别保存在 `actual/`；未省略公开正文、固定回执、历史、独立对象及重开尾。逐条时间与身份见[原汇总](current-producer-regression-compact-summary.md)，准确原件与三个源码快照见[索引](archive-index.json)。

首 normal 的真实测试通过后，metadata 检查器错误地要求 `ok` 后立即为 tab，因标准 Go 空白失败并停止。`first-failure/` 和首 release 保存该实际失败；`v2-checker/` 修正规则并补核同一份日志，随后才执行剩余 14 条。首 normal 没有重跑，原失败没有改写。

15 次 native 均实际 exit 0／Wait／当前组为空，自身原日志 writer 首次 Close 确认。20 个原 scope、26 个 child generation 中，18 个实际双 Close ACK，8 个被杀 generation 首次 Close UNKNOWN。此前八个 UNKNOWN 和新增八个都精确保留；恢复成功、对象责任 allACK 或进程消失不能补首次 Close。PG catalog 审计尚未执行。

独立 Spec 代码 findings 0。Standards 有一项 P2：启动前错误丢失原因链；已采用安全 wrapper 与 Read／首次 Close 双原因的最小修复决定。首测试取得真实缺 helper 的编译失败，测试体尚未执行；最小实现已静态完成并核对完整前态字节，首 GREEN 与后续资格待执行。重复 holder 断言的可选 P3 保留。这里的 15 次通过绑定原 c9a 树，不能冒称修正后验证。最终检查、资源核验、整合与 root 接受仍待，票 06 继续 claimed；完整 1.2 仍关闭。
