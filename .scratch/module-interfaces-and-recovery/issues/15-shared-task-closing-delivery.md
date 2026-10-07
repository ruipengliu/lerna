# 15: 非成功关闭接入共同封闭交付

**What to build:** 非成功任务关闭复用已有内部交付协议，保留原关闭证明、执行/结算后续责任和固定 Result。

**Blocked by:** 14（完成与取消共用原封闭回执交付）.

**Status:** claimed

- [ ] TaskClosing 的原封闭意图、回执查询、交付与源确认迁移到 14 的私有机制，删除对应重复协议代码。
- [ ] 保持关闭命令的独立范围、当前依据与准确原身份；封闭投递和固定 Result 仍由不同原阶段裁决。
- [ ] 准确范围的原取消 ACK、关闭 ACK 与交接到齐之前不提前固定 CANCELLED Result。
- [ ] FAILED/CANCELLED Result 固定后，迟到观察、账单、弱查询与原核对继续归原执行及预算责任，不改 Result 字节。
- [ ] 普通控制不提前完成原工作，旧领取和丢回执恢复同一 intent、seal 与 followup。
- [ ] 相关设计、非成功关闭公开场景与提交前/后/回执丢失故障场景通过。

## Comments

- 2026-10-07: Claimed on `codex/interfaces-15` after 14 was resolved in integration.
