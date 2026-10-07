# 01: 执行一致性套件通过窄 Interface 验收

**What to build:** 相同的 API、FILE 场景可以通过现有执行命令和查询接入套件，保留独立目标的调用与效果计数。

**Blocked by:** None (can start immediately).

**Status:** claimed

- [ ] 套件仅要求 Invoke、QueryOperation、QueryObservation、QueryBillingSource；不要求具体 Harness 或 core 类型。
- [ ] 生产宿主通过这组现有 Interface 运行原 one-per-permit、descriptor-bound、unknown-dispatch 场景。
- [ ] 原回执、尝试、发送、观察及逐发送费用身份保持；回执重放不增加目标请求或效果。
- [ ] 独立目标计数与故障注入仍归 Fixture；不建立第二核心实现，不复制另一套完整断言。
- [ ] 先记录现有场景基线，再修改相关测试设计并验证普通与故障构建。

## Comments

2026-10-07: Claimed for implementation on the integration branch.
