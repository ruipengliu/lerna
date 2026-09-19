# 18 票验收审计

审计基准为 `5485cb7` 的功能实现与 `1077297` 的完整验证入口。原始范围以 [18 票 Agent Brief](../../.scratch/harness-implementation/issues/18-context.md) 为准。本文是证据索引，不替代原验收项，也不表示整票完成。

| 要求 | 实现与可核查证据 | 当前结论 |
| --- | --- | --- |
| 适用记忆改变实际回答与有状态 API 行动 | `profiles/answer/personalized_test.go` 经正式产物发布核对简短、详细及缺失回答；`profiles/catalogcheck/personalized_test.go` 经实际 Invocation 和独立目标核对 item/alternative 及账本。CLI 同时覆盖适用与不适用记忆。 | 本地契约已有证据；真实效果见单独一行。 |
| 固定事实、用途、位置、策略和来源修订；原身份恢复 | `adapters/context/task/facts.go` 核对 Core 资格和事实摘要；`contextassembly/assembler.go` 绑定不可替换文档；`adapters/context/sqlite/store_test.go` 验证双连接竞争及重开。`profiles/catalogcheck/personalized_process_test.go` 核对多决策快照清单和原操作身份。 | 已实现；整票回归已通过。 |
| 缺失、不适用、冲突、裁剪和必需预算不足可观察 | `contextassembly/assembler_test.go` 覆盖冲突来源差异、可选裁剪、必需优先与不足不落盘；`adapters/memory/auth/missing_context_test.go` 使用真实 Memory 和原签名读取材料验证缺失元数据授权、撤权及缺失变存在。 | 已有定向证据；审查发现的缺失变存在问题已修复并复审通过。 |
| 模型前后、发布和首次动作前动态校验 | `brain/answer.go`、`brain/action.go`、`answers/port.go` 及 Execution 的首次启动守卫保留当前校验；回答、行动的 personalized invalidation 测试通过真实更正和撤权检查不发布、不启动及无关更正正常执行。 | 已有真实宿主故障边界证据；没有宣称跨库全局原子撤销。 |
| 未知调用不退款、不换身份；失效后有界重组 | `profiles/contextcheck/reassembly.go` 验证已知/未知结算、重放、一次修订改变实际效果、最多两次纠错、持续变化停止及撤权不重组；进程恢复核对已经发生的原动作。 | 新增用例已纳入 52 项 CLI；原未知效果与故障日志保留。 |
| 只保存获准正文，受控引用恢复仍需原许可 | `adapters/context/memory`、`adapters/memory/auth` 和正式回答/行动宿主；`profiles/answer/personalized_recovery_test.go` 覆盖 reference-only、过期、已发模型及输出后恢复；`18-unbound-history-*` 记录缺口篡改拒绝且目标无副作用。 | 已有定向证据；原快照不可恢复时允许明确失效，不能替换新版本。 |
| 数量、容量和调用期限明确 | Assembler 输入 32768 字节、候选 16、输出块 32、文档 65536 字节；SQLite 最多 512 份快照且不驱逐；组装/校验各 5 秒；纠错计数由 Core 持久保存并限 2。测试包总期限 30 分钟与上述业务期限分开。 | 实现可检查；整票回归已通过；总配置不代替具体边界的独立证据。 |
| 真实回答与 API 成对小回归 | [首次报告](evidence/18-real-context-report.json)、[回答重查](evidence/18-real-answer-recheck.json)、[回答诊断](evidence/18-real-answer-diagnostic.json)。8 次真实请求合计 8430 输入、760 输出 token；API 成对效果通过，详细回答的 JSON 协议失败仍是失败。 | **未完成**。已澄清风格仅约束 answer 字段，追加最多两次请求的预算问题待答复；不得自动调用或宣称 90%/95% 质量通过。 |
| 统一入口、进程证明、完整回归及双轴审查 | [52 项 CLI 日志](evidence/18-cli-reassembly.log) 通过；[实施记录](18-personalized-context.md) 保留两轴原结果及增量复审。Standards 无硬违例、1 可选 P3；Spec 的 P2 已关闭。完整验证运行日志为 `evidence/18-full-verify.log`。 | CLI 和审查已完成；**完整 make verify 已通过，1526 项必需检查、1531 项总检查**。 |

`go test`、CLI profile 与真实模型评估分别证明不同范围。只有完整验证终态和真实回答效果都有可核查结果后，才能重新审计是否关闭 18 票。19–88 票仍属于完整目标，未被本票或本索引替代。


最终本地验证证据归档于 [18-final](evidence/18-final/verification-stages.json)，固定二进制报告代码版本为1077297。构建记录dirty=true，保留原记录（当时有待归档日志/文档）；19代码在全部Go包测试完成后才开始，不包含在本次冻结验证中。真实回答效果仍未完成，18不关闭。
