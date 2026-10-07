# 12: 三种封闭共享发送关闭算法

**What to build:** 完成、取消与非成功任务关闭共用同一发送封闭算法，每条流程仍返回原类型证明并保留未完成责任。

**Blocked by:** None (can start immediately).

**Status:** claimed

- [ ] 在 Execution Manager 内共享全部发送历史检查、REGISTERED 关闭、修订、派发封闭和执行工作更新，三条流程同时接入。
- [ ] 各自命令身份、授权来源、清单、typed seal、事件及非成功关闭 followup 分别保留。
- [ ] 三种流程各验收 P4 前、P4 后/P5 前、P5 后、迟到原交接、旧 worker、旧可能发送加最新未发送及缺失执行历史。
- [ ] 封闭与 P4/P5/实际 I/O 共用原临界区；SEALED 不等于 NoSendProven；先到封闭标记永久有效。
- [ ] 不释放预算、不返还授权使用、不清除旧未知、迟到观察或逐发送费用；后续执行和结算责任及固定 Result 字节保持。
- [ ] 原事实、seal、Job、followup 与源事件原子提交；提交前/后故障及丢回执恢复同一身份，相关设计与测试通过。

## Comments

- 2026-10-07: Claimed for implementation on `codex/interfaces-12`, based on the integration branch.
