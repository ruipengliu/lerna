# 04: 独立目标与原键保证

**What to build:** 测试作者通过独立持久目标的写入、读取和原键查询观察真实测试效果，明确幂等窗口及无查询模式的保证范围。

**Blocked by:** [切片02完整退出](../../lerna-02-durable-work/exit-evidence.md)及[最终端口复核](../final-handoff.md)（均已满足；本片内无前置票）

**Status:** claimed

- [ ] 目标与组件／未来Executor账本使用独立持久身份和存储；普通write／read／query及privileged observer有明确测试边界，不能从被测系统自报Effect推断目标状态。
- [ ] 正常真实SQLite目标写入可独立读回准确字节与版本；窗口内原键同内容只产生原效果、异内容conflict，接收次数是目标公开事实而非内核私有调用数。
- [ ] 窗口起止固定在首次耐久接收，重传／重开不续期；过期返回guarantee_expired而不假承诺安全，尚可能迟到的原责任不因过期被抹掉。
- [ ] 有query模式仅报告当前原请求处理事实，not_found不等于永不发生；无query模式普通入口unsupported，privileged observer仍独立可见已写入事实，不能借它补造供应商查询保证。
- [ ] 关闭重开保留原请求、窗口、实际值和接收观察；实际SQLite运行版本、WAL／FULL及相关迁移记录来自本票，不能继承02结果冒充新表恢复。
- [ ] 测试目标只使用假材料／费用／保证，有闭合、有限的配置及I/O；缺依赖硬失败，正常与拒绝均可复演，不使用真实业务凭据或宣称供应商能力。


## Comments

2026-10-03，root依据授权Astra批准的粒度/真实edges及最终df2dbe5前置退出发布；本票验收尚未实现。已claimed，交独立工作树实施。
