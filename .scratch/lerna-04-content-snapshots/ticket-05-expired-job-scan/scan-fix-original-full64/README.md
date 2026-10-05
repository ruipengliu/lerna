# 原始 64 条过期积压：修复后的完整尾部

固定 HEAD85a1067 加六个明确 WIP 产品源，原测试60f817未变。保存的七份源与两次执行的预启动 pin 逐字相同；三份已发布迁移校验和保持。预启动 manifest 的「尚未执行」描述是当时事实，未改写；执行结果另见两个 outcome。

同原 caller90s / 初次seal15s / later-live seal45s / 四次Step / Go及外界120s / count1：普通20.418s、竞态26.476s，actual exit0、group absent、无timeout。root全文读取两份原日志及outcome，独立核验十个当前源/迁移SHA、七份源副本及两原进程组不存在。

两次均执行真实新V2出版及beta字节读回、later-live原两holder全ACK和独立正文缺失、第二次重开后旧64条完整责任/截止/holder/alpha正文/固定回执/发布历史保持。原expired责任没有完成确认或改期限；原业务首red及准备失败仍保留在上级目录。

此证据只证明该原完整反例的修复。低积压新源对照、第三政策消费者、完整Subject及合法delegation、异常记录/Seal编码的机械资格、当前受影响检查、独立两轴审查、资源核准及正式合并CI仍待。进程组不存在不能补造缺失的逻辑CloseACK，历史unknown原样保留。未接受票05或whole04，也未开放完整1.2 profile。
