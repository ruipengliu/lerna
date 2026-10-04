# 03: 完整必要上下文进入规则Decision，溢出阻止派发

**What to build:** 从真实Content和受信当前输入编译不可变完整Snapshot，正常路径经规则Decision回读完整必要正文，超限不派发。

**Blocked by:** 02 — 所有来源约束派生内容，撤权阻止新使用

**Status:** ready-for-agent

- [ ] ContextCompiler承担Task上下文职责并消费小端口；当前目标/控制仍是显式耐久fixture，不创建Task服务/表/Grant或第二Orchestrator。
- [ ] 闭合canonical mandatory-context/1正文完整保存goal、全部必要条件、控制、fixture预算/绝对期限/未决责任、来源与策略；有独立编码黄金，正常测试含真实非空unknown责任，不删必要字段。
- [ ] 完整正文作为第一准确Content材料，外壳/lock/manifest及规则产物真实经Content-backed Source/Publisher接现有1.1 rule/2；公开artifact去固定前缀后全字节等于原正文，不以裸ref或旧Seed正文库替代。
- [ ] 输入容量包含SnapshotRaw/LockRaw/ManifestRaw和所有实际材料，输出包含全文回显及Proposal；默认64KiB总容量/8KiB预留及原Decision限额/1MiB/63材料/64条件/64闭包同时执行，明确版本字节策略非供应商token。
- [ ] 必要输入/metadata/输出/数量超限在编译前准确context_overflow，无成功Snapshot/Decision接纳；不能仅在accepted后output_over_limit或自动扩大预留，每类有可容纳正常对照。
- [ ] 准确原Snapshot/ref/条件投影/Goal-control-component-limit绑定及manifest固定映射耐久，重开不换latest；原key Publisher超时恢复原Prepared/原字节/原fixture计费，不热换旧装配。
- [ ] compiler全部实际processed与worker实际processed分别登记并经Content闭包关联；真正规则Component边界正常有/溢出无请求，模型未装配如实记录，不造模型计数或证明真实Provider门禁。

## Comments

2026-10-04，按用户授权与最终API复核发布；前置03完整退出5fbb1a0，采用decisions/final-api-handoff的具体映射。本票独立垂直出口，不将全片广告/审查/CI作为隐藏关闭依赖。
