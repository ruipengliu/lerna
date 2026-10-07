# 09: API 执行与查询使用受信规则 Adapter

**What to build:** 真实 API 动作及其独立查询通过注入的固定受信规则解释证据，核心继续保存效果和责任。

**Blocked by:** None (can start immediately).

**Status:** claimed

- [ ] Execution Manager 声明受信规则 Interface，生产装配注入固定 API Adapter；普通执行 Adapter 无规则注册权。
- [ ] API 原动作、查询自身、被查主体及原 429 等待规则一起迁移，保留规则标识和账户、origin、键、尝试及发送绑定。
- [ ] 剩余未迁移协议仅按明确固定清单走原实现；未知版本拒绝，不使用通用 fallback 或最新规则。
- [ ] 公共真实 POST/GET、弱否定证明、遮蔽/无效正文、202/5xx、429、迟到观察及账单保持原事实与目标次数。
- [ ] 兼容检查保持纯检查；闭合 UNKNOWN、历史发送、独立 QUERY、过期原键可读；未知原版本在恢复前拒绝。
- [ ] 无 body 读取、凭据解析、重新编译或续期混入资格检查；按 ADR 更新设计并通过相关普通与故障场景。

## Comments

2026-10-07: Claimed for implementation on the integration branch.
