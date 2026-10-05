# 票05七项验收：root核定

2026-10-05，root接受受测源码 `8db74e3dfd2677adb37fbdd13bd93e76b61b5eb7` 的七项AC。固定审查差异 `1a4e1d2...8db74e3`，14文件，包含原实际64积压反例；此前产品及恢复证据继续有效。此记录接受本机真实PG/Linux范围，尚待实际整合交付；整片04与完整1.2广告仍未接受。

| AC | 实际行为与依据 |
| --- | --- |
| 1 | 受信生命周期先封新使用并持久原责任；固定Command、准确版本、最小元数据与重开保持。没有公共delete入口。见原[逐场景证据](ticket-05-evidence.md)及当前full64固定回执、发布历史对照。 |
| 2 | staging真正删除且由独立事务确认；primary实际擦除且独立介质观察后才ACK。无权查询拒绝，明确当前metadata资格返回gone。当前full64、坏deadline正常对照及合法特殊字符seal正常／竞态均执行完整ACK、字节不存在及重开尾。 |
| 3 | 第二holder独立存过字节，真实离线与ENOTEMPTY保留原责任、期限及residual；恢复推进同一责任。原完整holder／attempt分页和新政策／holder页正常／竞态保持，不能由空页推全局擦除。 |
| 4 | 原版本封闭、迟到安装与重传拒绝，物理介质使用持久准确key fence。真实跨进程两序证据保留；新正向进程正常／竞态重新通过。原SIGKILL擦除方的logical Close UNKNOWN不被新观测覆盖。 |
| 5 | 原64到期责任曾遮挡后续版本，真实反例保留；同一60f测试修复后普通20.418s／竞态26.476s执行V2、live清理及全部旧责任／字节／历史重开尾。Manager普通19.681s／竞态27.528s证明第三消费者推进；不同完整委派Subject普通6.510s／竞态13.953s只清自己的责任，其他64原live责任不变。 |
| 6 | 原已登记preparing／failed准确版本的孤儿条件竞争保留；published活引用和不同新版本不被删除。原30s资格证据有效，历史45s实验不冒充它。扫描修正仍由原Version锁、完整身份、fresh Clock和原期限裁决，不扫描其他保存主体scope。 |
| 7 | 真实Content、受信holder与独立字节驱动正常／故障／重开；新候选27项受影响检查全部实际通过，包括全module普通／竞态各472项、真实原writer升级及三固定迁移校验。没有法证擦除、已披露字节撤回或生产多机声明。 |

[当前27项检查](ticket-05-expired-job-scan/final-affected-checks/README.md)保存原日志、预启动与提交源码摘要、完整机器交叉核对；原outcome统计缺口以sidecar澄清，不改写原字节。25份短日志人工全文阅读；两份1035行module日志人工节选、全部原字节机器检查，明确两种阅读范围。

[两轴审查](ticket-05-expired-job-scan/current-reviews/README.md)：Standards 0硬性违规、1非阻塞P3；Spec缺失／范围扩张／实现错误各0。授权的Astra high[窄决定](ticket-05-expired-job-scan/current-reviews/acceptance-decision.md)采用KEEP两个场景私有观察记录，没有必要产品修正。

[只读资源核查](ticket-05-expired-job-scan/current-resource-audit/actual-summary.json)实际native3584995/start15030462退出0、Wait及组消失。142原schema、14登记backend、52原进程组均无当前残留；218准确路径中216不存在。观察连接588251在事务前登记，原首次Close及rollback均成功。原两个目录3977538271（33:315225）与4116685529（33:326495）仍匹配原inode且永久保留UNKNOWN；不存在不等于历史Close成功。没有清理、DROP或补写旧Close。

原15份源码、快照与8db commit blobs逐一匹配。新准确整合SHA、push及CI结果需随后记录；旧904 CI不替代本次交付。票06只依赖本票实际交付，不增加整片CI隐藏依赖。whole04仍15/41，直到票据交付状态同步。
