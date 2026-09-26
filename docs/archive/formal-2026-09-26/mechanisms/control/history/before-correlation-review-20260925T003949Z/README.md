# 控制、决策、执行恢复与委派预算的形式验证

本目录对四个方案模块的 **26 类机制、121 条 validation/profile 行**建立了可审计清单。已经实际执行 **10 个 TLC 正向配置、29 个故意错误的负对照、16 个可达见证和 1 次 Lean 编译**，56 项结果均符合各自预期。它不表示 121 条运行用例整体通过，也不表示所有规则都已形式化。

原始输出在 [logs](logs/)，执行摘要在 [results.json](results.json)，逐机制来源与逐用例的子义务映射在 [inventory.json](inventory.json)。其中 TK 35、CE 31、CO 23、BS 32；CE-P1/P2 与 CO-P1/P2 是协议 profile 行，尤其 CO-P2 明确属于后置迁移契约，不计作已经实现或已经证明的迁移能力。

## 模型边界与真实运行结果

各配置检查有限状态空间内的所有行为交错。没有通过状态约束排除反例；不同主题采用显式 Feature 切片控制规模。它们的组合保证仅限明确组合的模型，不能把各个独立通过的性质自动相乘，推出完整系统已通过。当前没有验证公平性或最终完成的时序性质；可达见证只证明相应路径存在。

| 模型／正向配置 | 状态与规则 | distinct states | 原始结果 |
|---|---|---:|---|
| ControlRecovery / control-safe | 核心准备事务、提交知识、网络接纳、执行工作、独立本地控制、发送资格、真实效果与观察知识、封闭和原操作查询 | 3,128 | exit 0；无错误 |
| Decision / decision-safe | 两轮、每轮最多两次尝试，业务版本与账单变化分离，建议准入、关闭旧轮、有限结构修复 | 84,800 | exit 0；无错误 |
| Budget / budget-safe | 三节点内部委派树、份额转移、累计用量、未知预留、冲突、可信封账、独立收尾预算 | 1,403,156 | exit 0；无错误 |
| Boundaries / boundary-catalog | 准确版本及意图绑定、清载荷、墓碑、封闭旧入口 | 53 | exit 0；无错误 |
| Boundaries / boundary-gui-control | 祖先与自身暂停原因、滞后投影、环境变化、观察、动作切点 | 299 | exit 0；无错误 |
| Boundaries / boundary-facts | 事实修订、矛盾与同修订冲突、固定历史结果 | 66 | exit 0；无错误 |
| CoreContracts / core-identity | 两用户、两任务、两操作，最多两个接纳事件；用户级键、固定权威 | 833 | exit 0；无错误 |
| CoreContracts / core-input | 请求替换、到期、文字刷新、两个竞争消费者、重投 | 56 | exit 0；无错误 |
| CoreContracts / core-batch | 两操作批次、参数与前提、整体准入／整体拒绝 | 17 | exit 0；无错误 |
| CoreContracts / core-adjust | 调整底线、原管理操作重复、内部份额共同变更、外部合同上限固定 | 14 | exit 0；无错误 |

完整参数在各 `.cfg` 和 inventory 的 `model_parameters`。Budget 主配置的 Total=2、Cleanup=1、MaxDepth=2、MaxRevision=2；深度负例使用 MaxDepth=1。CoreContracts 的两个事件上界是有限实例范围，不声称真实系统最多只接纳两个请求。

核心与执行接纳在 ControlRecovery 中是不同动作。核心取消提交不原子改变远端控制；已经发出的请求仍可能在稍后被远端接纳，远端收到控制后才阻断本地首次发送。真实效果可以在本地屏障生效后迟到。`CloseOriginal` 抽象可信的外部入口封闭证明，不能从超时、未见或本地 Work 结束推导得到。

Decision 中的 `fixed` 只表示验收条件已经由核心固定，`valid` 和 `ready` 是外部校验所得抽象输入；它没有证明校验器本身正确。模型自评 `selfPass` 不提供这些权限。账单事件不推进业务版本，准入可沿原轮继续；预算重验与多维批次共同事务尚未组合在这个模型内。

Budget 将来源对累计值的可信报告限制为 `used ≤ allocation`。这是硬上限提供方必须满足的环境契约，**不是证明外部提供方绝不会超额**。`Seal` 抽象全部计费来源已经可信封闭，任务 done 本身不能封账。同修订冲突保持冻结。每个节点只有一个计费组件；三节点实验覆盖兄弟及孙辈份额组合，不是任意树的形式归纳。

## 负对照与非真空

29 个负对照均只在指定配置启用一个错误变体，且全部返回 exit 12 和预期不变量失败。它们是检查性质是否能捕获指定错误，不能当成当前设计发现了 29 个缺陷。

| 错误变体分组 | 数量 | 捕获的错误 |
|---|---:|---|
| 控制恢复 | 4 | 取消后仍准备、过时代次取得资格、把 not_found 当无效果、忽略已知远端屏障 |
| 决策 | 3 | 接受旧业务版本、采用模型自评、费用未知仍修复 |
| 预算 | 4 | 重复账单再次计费、任务结束即释放未知、父汇总重复计费、绕过深度上限 |
| 边界 | 9 | 已接纳操作切能力版本、改意图、提前删墓碑、恢复祖先清掉自身暂停、依赖旧控制投影、旧观察动作、按到达顺序覆盖、冲突被擦除、重写历史结果 |
| 核心合同 | 9 | 把键缩成任务级、错权威接纳、输入双消费、旧请求输入、无效批次接纳、拒绝留下部分工作、预算低于预留、重投增额、原外部合同扩额 |

16 个见证配置故意检查“该目标永不发生”的反命题；exit 12 的轨迹就是目标可达的证据，而非安全检查失败。包括成功准入、有限修复、纯账单后沿原提案准入、原提交未见后仍提交、可信无效果后允许新尝试、孙辈委派、可信封账释放、升级后原绑定不变、固定答复后当前事实继续变化、不同用户同裸键、整体有效批次及幂等管理。

涉及顺序的见证使用动作内持久历史标记，避免仅凭最终状态合取误报：

- `control-witness-late-effect`：原请求发送后取消，远端原发送先发生；随后 `ApplyCancelRemote → ExternalEffect → ObservePositive`，证明屏障并不抹去真实迟到效果。
- `budget-witness-late-bill`：`Reserve → Cancel → Report(正增量) → Duplicate`，证明取消后仍能结算真实费用且重复不双计。
- `boundary-witness-resume`：`CreateChild → PauseParent → PauseSelf → ResumeParent → ResumeSelf → Observe → Click`。
- `core-witness-input`：`RefreshText → Consume → ReplayInput`。

这些标记只观察动作发生的历史，不参与合法动作门禁。全部 trace 可在同名原始日志中逐步复核。

## Lean 一般参数证明

[Control.lean](Control.lean) 只导入 Std，定义完整枚举的 `BudgetStep`、`CoreStep`，分别证明初态、每个动作的保持性，再对 `BudgetReachable` / `CoreReachable` 做有限轨迹归纳。待证不变量没有加入 Step guard。编译 exit 0，13 个导出核心定理的 `#print axioms` 原文在 [control-proof.log](logs/control-proof.log)。

| 范围 | 一般性与核心定理 | 公理依赖 |
|---|---|---|
| 一次预留组件 | 任意 Nat total、cleanup、累计报告和有限长度轨迹；`reachable_budget_safe`、`cumulative_charged_once`、`unknown_allocation_not_released` | `propext`, `Quot.sound` |
| 取消后不新预留 | `cancellation_does_not_create_allocation` | `propext` |
| 重复累计差额与份额转移 | `cumulative_repeat_has_zero_delta`；任意 Nat 两账户 `share_transfer_preserves_total` | `propext`, `Quot.sound` |
| 核心提交/取消/资格 | 任意 Nat 代次和有限长度轨迹；`reachable_core_safe`、`cancel_blocks_new_prepare`、`stale_lease_blocks_new_prepare`、`submission_unknown_never_dispatched`、`cancellation_is_monotone` | `propext` |

源码没有 `sorry`、`admit`、自定义公理或 `native_decide`；成功输出中没有 `sorryAx`。Lean 证明的是预算组件及核心提交状态机，并未证明五个 TLA 文件到实现的精化关系，也未证明任意委派树。Core 的 `send` 是发送资格事实的抽象写入，重复写同一事实是幂等的；一次物理发送与不可恢复发送资格由 TLC 的执行侧模型单独检查。

## 未覆盖规则与运行前提

[inventory.json](inventory.json) 为每条 validation 行保留原始文本与准确路径/行号，并分别列：

- `model-property`：已经实际检查的具体抽象子义务及模型/性质/定理映射。
- `uncovered`：本组未形式化的规则，包括应由其他组以实际结果补充的内容。没有因为“与某模式相似”或“其他组负责”就记为覆盖。
- `runtime-required`：需要真实代码、持久存储、外部提供方、数据或设备才能验证的性质。
- `static-only`：例如 CO-P2 的明确后置范围事实，不是形式证明。

仍可进一步建模的重点包括：任意计划 DAG 与持久 blocker 唤醒；D→C/S、K→Q 完整身份映射；取消先到被拒后有界新取消责任链；外部输入的本方接纳/外部应用双回执；通知与 Recheck 管理去重；预算/条件/多维资源整批共同准入；备份回滚检测；实际物理占位与隔离；任意树和动态管理修订的组合。输入、引用、来源闭包、证据版本和终态等其他组模型必须通过总报告的真实交叉引用关联，不能由本目录的局部通过替代。

实际效果真实性、硬限额实现、物理 fencing、存储持久性、时钟、租户不泄露、吞吐/容量/公平和各驱动 profile 均仍需要运行验证。当前有限安全检查不保证活性；要证明稳定后最终完成，需明确环境最终提供可信事实、查询额度足够、控制与资格最终有效以及调度公平等前提。

## 复跑与证据

在仓库根执行：

```sh
python3 formal/mechanisms/control/run_local.py
```

也可在后面传一个或多个 `checks.json` 中的 check id。脚本为每项创建独立临时目录，同时隔离 JVM 的 `java.io.tmpdir` 和 TLC `-metadir`，避免并行解包内置模块冲突。每项 `logs/<id>.json` 保存实际命令数组及其 SHA-256、起始 UTC、耗时、源码/配置 SHA-256、退出码、预期诊断和原始日志 SHA-256。负例与见证预期为 12，正向与 Lean 预期为 0；仅比较进程退出成功会误读结果。

工具是 OpenJDK 25.0.2、TLA+ tools 发行包 v1.7.4（jar 输出 `TLC2 Version 2.19 of 08 August 2024`）、Lean 4.19.0。可执行文件路径、版本原文与 SHA-256 见 [toolchain.json](toolchain.json)。统一总复跑器可直接消费 [checks.json](checks.json)；本目录 logs/results 记录本组实际运行，总目录 evidence 另保留统一复跑结果。`build_inventory.py` 只重建人工逐行审核的覆盖映射，不从关键词自动推断覆盖。
