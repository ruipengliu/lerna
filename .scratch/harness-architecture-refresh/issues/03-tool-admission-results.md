# 03: 最终工具参数准入与完整效果交付

**What to build:** 设计一个候选行动从参数变换、准确绑定、授权准入到结果交付的完整路径，使调用者能够分辨正确执行、部分覆盖和未知效果。

**Blocked by:** None (can start immediately).

**Status:** ready-for-agent

**Progress:** completed

- [x] hook、模板和包装器变换先完成，再作严格校验、资源规范化及准确目录与安装绑定；后续变化不得静默改变原 Operation 的输入。
- [x] 实际入口重新核验当前控制、Grant、预算、目标及资源，hook 的 allow 不能产生权限。
- [x] 区分逻辑调用源序和完成顺序，保留准确原结果、必要的大字节引用、分页、截断、筛选、观察时间与部分覆盖。
- [x] 明确实时流、最终结果、适配器占位和费用结算的不同含义，流中断或合成响应不消除未知效果。
- [x] 覆盖最后参数改写、同名工具、旧目录、撤权、路径变化、乱序、漏页、合法空集及媒体超限；每种拒绝有相应合法正例。

## Comments

2026-10-01：设计文档完成。仅修改 [Execution 主线](../../../docs/architecture/.draft/execution/README.md)、[Execution 实现](../../../docs/architecture/.draft/execution/implementation.md)、[Security 主线](../../../docs/architecture/.draft/security/README.md)、[Security 实现](../../../docs/architecture/.draft/security/implementation.md)及本票据。

- 最后输入经有界变换、固定 Schema、规范资源及准确安装绑定后交原 Orchestrator 准入；Executor 重验同一输入，实际入口再查原 use、当前控制／预算／安装及资源。编码仅保持原语义，完整请求和每个真实出口均在声明及当前用途内，hook allow 不生成许可。
- 准备、编码、覆盖和来源位置均为原 Operation／Attempt 的内部组或准确引用，可合并存储；不新增公共字段／方法、独立准备服务或 owner 全局完成序号账本。临时 live 片段可先显示、可丢，不逐片建 job 或提交；业务生效 hint 等原事实可读后发送。
- 原结果逐项保留允许取得的文本／媒体和部分范围，区分源序、收集完成位置、占位、效果凭据与累计费用；合成 interrupted 不改原 unknown。副作用证据与正文保存缺口分别记录，禁止保存的字节不为诊断落盘。
- 新稳定锚点：Execution README 的 `final-tool-admission`、`result-provenance`；Execution implementation 的 `final-tool-admission`、`dispatch-encoding`、`result-provenance`；Security README 的 `online-revocation`、`final-tool-use`；Security implementation 的 `final-tool-use-check`。两个 README 已补实现导读。
- 待共享故障语料接入：Execution `EX-21～31` 与 Security `S-I27～31`，覆盖最终改参／准确组合／入口撤权及预算／真实路径与网络出口／乱序／空集与分页／全部媒体／临时流及提交未知／保存用途及原效果／每次物理请求计量，各保留合法正例。未运行真实目标、数据库、隔离或费用实验。
- 局部静态检查：限定上述文件和本票据的 `git diff --check` 通过；标准库脚本检查四文件本地目标存在、8 个新稳定锚点唯一、13 个新锚点引用可达、代码围栏成对及尾随空白，无错误。不运行全套共享验证，不把设计完成记作运行能力。
- 未发现 ADR／公开协议冲突。实现需控制两项成本：最后输入与实际资源核查复用同一规范化实现，避免多个包装器各自改义；原请求／结果诊断采用最小依据和一份准确 Content，避免多份全量字节副本及逐片持久化。
