# 当前来源绑定的大图竞态成本测量

2026-10-05，原四项竞态用例唯一一次执行，actual exit 1，原 PID/PGID 3744673/starttick15722102；Wait 完成、当前进程组为空，无外层超时。原 caller30s / Claim5s / Go120s 未改。

Root 全文读取 [408 行原始日志](context-integrated-qualification-sql-original4b-race.log)及[完整结果](one-race-business-results.md)。closure64 仅41次材料 Content 读取成功，第42次在 Access.Current 超时；static62 完成62次材料读取和两次 Plan 后 Claim 过期。两例均未进入 Prepared、worker publication、outer readback 或 Completed，成功公开尾未执行。65节点拒绝、200000正常公开结果、262144边界拒绝及重复角色拒绝通过。

[成本数据](one-race-business-cost-results.json)保留三 owner 各273槽及实际子阶段；SQL wall 不等于服务端执行，Within 差值不等于SQL等待，测量总开销未知。不能据此接受容量或七项AC。331原源码及10替换源前后一致，独立 race binary 的身份记录保留。无重试、新 profile、预算放宽或产品修改。

[归档映射](archive-map.json)逐项保存原路径、字节数及 SHA256；归档 Go 源使用 .go.txt 后缀避免进入 module，原字节不变。32MB binary 留在原 owned 路径，未复制。历史失败、profile logical Close UNKNOWN、旧资源 UNKNOWN 不补认、不清理。Root已全文采用[实测后的最小修正决定](adopted-next-decision.md)：仅明确既有CheckPolicy锁后时钟责任并移除被覆盖的前置Now查询；只静态实施，当前新源尚未验证、不预测容量通过。票03仍 claimed，whole04仍22/41，完整profile1.2不广告。
