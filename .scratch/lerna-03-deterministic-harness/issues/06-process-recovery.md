# 06: 规则组件与目标的进程接替

**What to build:** 进程在发布或提交边界退出后，接替者查询原 Decision 和目标事实并继续原故障计划，不创造新身份或重复已完成步骤。

**Blocked by:** 01 原 Decision 的耐久规则提案；05 迟到效果与耐久故障计划

**Status:** ready-for-agent

- [ ] 复用02实际子进程／同步设施，在正常Decision输出发布与本地completed提交边界受控SIGKILL；独立连接公开get恢复原Proposal、产物、身份与固定回执。
- [ ] 跨owner发布已完成但Decision引用尚未提交的退出，用原发布key和摘要恢复，原引用读回一致，不造第二产物或假称跨owner原子提交。
- [ ] 独立目标在COMMIT前及提交后／响应前受控终止，普通query／read加observer区分无效果与已写响应未知；真实存储与子进程正常控制均运行。
- [ ] 故障协调者和目标重启保持scenario／event／cursor，同一持久scenario不重复已完成步骤；另一个隔离scenario同seed有限重演的定义明确。
- [ ] 所有子进程、管道、连接和临时数据有限清理；同步点来自真实阶段，不用sleep猜测，记录准确代码／DB／计划版本、输入和结果。
- [ ] 本票只验证正常Decision及target的进程恢复，不隐含依赖所有候选／取消或承担整片关闭；仅证明进程SIGKILL，不证明断电、供应商幂等、生产容量或可用区耐久。

## 最终接法注记（whole02已退出）

最终依据 `../final-handoff.md`（代码554874470d5abeb71fa743708580f3121b8944f1，退出文档df2dbe5624120bc258dc7419ea022a08ebc0d6b0）。票10 holder历史失败阻断Cleanup的P2已修复并通过准确证据：错误保留聚合，退出确认后继续安全清理，退出未知则保留scope。ownedFixture仍是demo waitStore/Host/child，不能冒充Decision/source/publisher/target装配；实际第二consumer需要时仅共享窄scope/admin与有限进程机制，保留强类型writer与专有fault/history，不建通用registry或补假demo接口。既有未知scope/CID限制保留。本票仍仅依01+05、6条AC，不承担02/03或root全片final-close。


## Comments

2026-10-03，root依据授权Astra批准的粒度/真实edges及最终df2dbe5前置退出发布；本票验收尚未实现。等待上述直接前置resolved，不能以spec ready替代实现依赖。
