# 04: 固定回执与只读查询

**What to build:** 调用方经 command.get 公开合同入口读取原决定和当前异步进展，准确区分接纳事实、业务进展和传输不确定性；同版 Go／TypeScript 能编解码这些结果而不误报 Task 完成。

**Blocked by:** 02 — 严格命令解析与验证。

**Status:** ready-for-agent

- [ ] CommandReceipt 的闭合 Schema 与生成类型包含原 command_ref、accepted／applied／rejected、关联 object_ref、revision 及适用的 reason、next_action；字段存在条件明确，不用空字符串代替不存在的引用。
- [ ] command.get 输出分别表达原固定回执与当前异步进展；accepted 表示接纳责任，applied 表示该方法的修改成功点，均不能推导为 Task succeeded。
- [ ] commit_unknown 属于传输结果，不能成为 CommandReceipt.state；conflict 作为固定拒绝原因表达，不能成为第四种接纳状态。
- [ ] 不可用、not_found、gone 和可读取原决定可明确区分；not_found 只表示当前原 owner 未找到，gone 保留最小身份，不能把这些结果当作新建同义工作的许可。
- [ ] 通过可控只读事实源和公开查询入口，演示进展从处理中变为成功或失败时，原回执仍保持同一决定；所有结果均有 Go／TypeScript 共同正反例与状态组合检查。
- [ ] 查询入口只读取已有事实，不触发模型、安装、动作或新的业务责任；用公开可观察事实验证只读性，不依赖私有函数调用次数。
- [ ] 正常查询、暂时不可用、未找到、正文已清理及无权视图的合同都可验证；明确区分合同夹具与真正持久化恢复证据。

## Scope

本任务使用可控只读事实源验证公开行为，不建立生产命令账本，也不实现接纳事务或网络恢复。入口的受信鉴权与 owner 隔离由任务 05 完成，真实持久事实由后续实现切片提供。
