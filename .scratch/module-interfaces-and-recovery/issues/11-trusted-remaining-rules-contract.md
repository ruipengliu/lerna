# 11: 模型、模拟目标与查询完成受信规则迁移

**What to build:** 所有现有目标证据均通过同一受信 Interface 解释，原历史仍按原规则读取与恢复。

**Blocked by:** 10（FILE 证据与原版本查询使用受信规则 Adapter）.

**Status:** claimed

- [ ] 迁移 simulator、model 及剩余 QUERY 的固定证据规则；Execution Manager 保留冲突、效果历史与收尾裁决。
- [ ] 删除已迁移协议的过渡分派、旧解析实现和无调用 helper；不存在两套可选择的同版本解释路径。
- [ ] 模型回报、模拟写入、独立查询、弱否定证明、查询回执丢失和迟到效果通过公共流程验收。
- [ ] 原规则标识、结果、迟到可能性、等待原因及目标/模型/账单计数保持；查询终局不代替原动作终局。
- [ ] 所有原记录，包括完成、停用和被替代历史，按原支持版本纯核验；未知版本恢复前拒绝且 READY 工作不领取。
- [ ] 不改变 schema、格式、默认 prompt、费用解释或当前 credential-unavailable 行为；设计和相关普通与故障检查通过。

## Comments

2026-10-07: Claimed after ticket 10 resolved, on codex/interfaces-11.
