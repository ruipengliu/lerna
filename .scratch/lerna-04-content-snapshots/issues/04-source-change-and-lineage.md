# 04: 编译中来源变化和摘要完整来源

**What to build:** 读取后来源或当前输入前态变化时，有限重建或明确缺口；只披露一个引用的摘要仍受全部实际处理来源约束。

**Blocked by:** 03 — 完整必要上下文进入规则Decision，溢出阻止派发

**Status:** ready-for-agent

- [ ] 通过有限真实同步点改变来源许可/到期/字节可读性，Content最终发布Tx重核准确sources/policy，变化后重建或明确gap，不保存未经复核的混合输入。
- [ ] fixture自身当前输入/goal/control/budget/deadline/责任修订在自身Tx CAS绑定Snapshot与派发责任；前态变化不发旧Snapshot，不能宣传Content+Task跨owner原子。
- [ ] 发布后CAS前、CAS后排队/实际派发前分别检验控制与源资格；最多3次重建且受原总读取/期限累计约束，不刷新budget或靠共享内存锁。
- [ ] 确定性摘要实际读至少两份真实Content，登记所有读取/丢弃/影响选择来源及转换版本/原字节hash；只披露一项仍真实继承另一受限来源，不能仅写隐藏ID文本。
- [ ] 仅新版本出现不篡改已固定旧引用；current-head前态、撤权、缺失字节分别准确处理，后续新使用仍重新检查当前资格。
- [ ] 各竞争/拒绝有完整正文正常编译及实际规则Decision完成对照，公开Source/Content/Proposal读回证明；生产Task/Grant竞争由05/06另验。

## Comments

2026-10-04，按用户授权与最终API复核发布；前置03完整退出5fbb1a0，采用decisions/final-api-handoff的具体映射。本票独立垂直出口，不将全片广告/审查/CI作为隐藏关闭依赖。
