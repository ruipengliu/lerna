# 持久交接的 Lean 安全证明

本页及所述结果属于 2026-09-26 归档的旧架构；[归档范围与路径映射](../../README.md)说明复跑入口及历史证据边界。

本目录把一项固定意图的 A→B 交接写成状态与转移关系。A、B 各自提交；B 的接纳将固定决定和后续 Work 一起保存，A 收到确认后释放交接责任。模型没有执行 B 的业务工作，也没有表示外部副作用。

## 模型与定理

`Handoff.lean` 仅导入 Lean 随附的 `Std`，不依赖 Mathlib。`State` 的字段与同目录上层的 TLC 交接模型对应；`Step` 完整列出 22 个动作，包括重复、丢失、崩溃、恢复、稳定和不变步骤。请求与确认分别使用有限容量计数，处理良好请求或冲突探针的交错表示跨种类乱序。没有跨 A/B 提交的原子动作。

`Init` 固定初始记录，发送预算、请求容量 `reqCap` 和确认容量 `ackCap` 分别为任意自然数，初始 `stable` 为任意布尔值。`Reachable` 由初始状态和 `Step` 归纳定义，执行长度不限于某个有限上限。`Safety` 将以下性质组成归纳不变量；动作守卫没有假设这些待证性质。

| 定理 | 结论 |
| --- | --- |
| `initial_safe` | 任意初始预算和初始稳定性满足安全性质 |
| `step_preserves_safety` | 对所有 22 个动作，安全状态的一步后继仍安全 |
| `reachable_safe` | 任意容量、预算、初始稳定性和任意有限可达轨迹满足全部安全性质 |
| `released_has_durable_owner` | A 已释放责任时，B 已耐久接纳且拥有后续 Work |
| `responsibility_never_lost` | 一旦 A 开始交接，A 仍处于 pending，或 B 已保存 Work |
| `work_created_at_most_once` | 原操作创建 B 的 Work 至多一次 |
| `acceptance_and_work_agree` | B 的接纳和 Work 一致，创建次数恰好是接纳指示值 |
| `acknowledgements_have_durable_origin` | B 的回执知识、A 的已见确认、在途确认均以 B 的耐久接纳为前提 |
| `conflict_probes_follow_acceptance` | 存在冲突探针时，原操作已由 B 接纳 |
| `gap_is_pending` | 持久缺口只存在于 pending 阶段，完成释放时清除 |
| `reachable_bounds` | 请求总数、确认数及两端剩余发送预算不超过各自初始上限 |
| `step_never_returns_to_idle` | 已开始交接的状态不会通过任何一步返回 idle |
| `step_keeps_released` | 已释放状态不会通过任何一步退出 released |

`conflict_probes_follow_acceptance` 对应模型约定：不同意图探针仅在原意图已持久接纳后注入。本模型不讨论两个不同意图在首次接纳前竞争，也不把探针前提当作一般业务身份安全的完整证明。

`restart_duplicate_witness` 给出具体合法轨迹：B 已接纳并读回执，随后崩溃清除易失的回执知识，恢复后处理原请求的重复副本，Work 仍只有一份；`completion_after_restart_witness` 沿该轨迹最终完成 A 的释放。`normal_completion_witness` 另有不含丢包或崩溃的完成轨迹。

`lost_ack_recovery_witness` 使用请求／确认容量 1 和两端预算 2，证明以下具体轨迹可达：保存责任、发送、B 接纳、读回执、发确认、丢确认、B 崩溃、B 恢复、A 重投原请求、B 按重复处理、重读回执、重发确认、A 接收、A 释放。最终 B 的 Work 创建次数仍为 1。这些见证用于排除模型拒绝所有正常行为的空洞结果；存在一条完成轨迹不等于所有调度最终完成。

## 运行与证据

工具链固定为 Lean 4.19.0（darwin_aarch64）。从仓库根目录执行：

```sh
/Users/ruipengliu/.cache/lerna-formal-tools/lean-4.19.0-darwin_aarch64/bin/lean --version
/Users/ruipengliu/.cache/lerna-formal-tools/lean-4.19.0-darwin_aarch64/bin/lean docs/archive/formal-2026-09-26/handoff/lean/Handoff.lean
```

2026-09-25 已实际运行上述工具，版本输出为 `Lean (version 4.19.0, arm64-apple-darwin23.6.0, commit 6caaee842e94, Release)`。最终编译退出码为 **0**，无错误或警告；版本输出保存在 [lean.version.txt](lean.version.txt)，完整编译及 17 项公理依赖输出保存在 [lean.stdout.log](lean.stdout.log)。仓库统一验证入口的再次运行证据由上层报告记录。

| 检查范围 | 实际传递公理依赖 |
| --- | --- |
| 初始／全动作保持性、可达安全、六项安全结论及缺口性质 | `propext` |
| 两项生命周期单调性 | `propext` |
| 可达容量／预算范围 | `propext`、`Quot.sound` |
| 四项具体轨迹见证 | 无公理依赖 |

`propext` 和 `Quot.sound` 是 Lean 的标准逻辑公理；本项目未声明新公理，也没有把交接安全结论作为公理输入。最终依赖不含 `sorryAx`；源码不含未完成证明占位或原生求值证明。

编写过程中，早期自动化尚未完成 `CrashA` 的回执来源分支：忘记 A 的易失确认后，需要显式证明剩余两种来源仍包含在旧的三种来源中。该次编译失败及错误恢复时显示的 `sorryAx` 已从当时保存的工具输出归档为 [lean.failed-proof.stdout.log](lean.failed-proof.stdout.log)。它是失败版本的记录，不是最终验证结果；当时源码也没有显式引入证明占位。最终文件补齐这一命题推导后通过检查。

## 证明边界

证明涉及形式转移关系的所有有限可达历史，不依赖 TLC 的具体容量或预算实例；不包含公平调度或最终送达的活性证明。持久存储正确保存原子提交、网络不能伪造受信确认、崩溃不损坏耐久记录等边界已通过 `Step` 所允许的动作表达，现实环境是否满足这些边界须另行验证。

发送机会可以耗尽，`gap` 只保留缺口，不重新创建预算。B 接纳后的 Work 持续存在；其实际执行、执行结果、内容清理、授权撤回和多操作并发不在本模型中。因此“至多创建一次 Work”不应表述为外部效果恰好一次，Lean 检查通过也不证明尚未实现的生产程序正确。
