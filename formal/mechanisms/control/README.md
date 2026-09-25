# 控制、决策、执行恢复与委派预算的形式验证

本目录对四个方案模块的 **26 类机制、121 条 validation/profile 行**建立了可审计清单。已经实际执行 **10 个 TLC 正向配置、35 个故意错误的负对照、19 个可达见证和 1 次 Lean 编译**，65 项结果均符合各自预期。它不表示 121 条运行用例整体通过，也不表示所有规则都已形式化。

原始输出在 [logs](logs/)，执行摘要在 [results.json](results.json)，逐机制来源与逐用例的子义务映射在 [inventory.json](inventory.json)。其中 TK 35、CE 31、CO 23、BS 32；CE-P1/P2 与 CO-P1/P2 是协议 profile 行，尤其 CO-P2 明确属于后置迁移契约，不计作已经实现或已经证明的迁移能力。

## 模型边界与真实运行结果

各配置检查有限状态空间内的所有行为交错。没有通过状态约束排除反例；不同主题采用显式 Feature 切片控制规模。它们的组合保证仅限明确组合的模型，不能把各个独立通过的性质自动相乘，推出完整系统已通过。当前没有验证公平性或最终完成的时序性质；可达见证只证明相应路径存在。

| 模型／正向配置 | 状态与规则 | distinct states | 原始结果 |
|---|---|---:|---|
| ControlRecovery / control-safe | 核心准备事务、提交知识、网络接纳、执行工作、独立本地控制、发送资格、真实效果与观察知识、封闭和原操作查询 | 3,344 | exit 0；无错误 |
| Decision / decision-safe | 两轮、每轮最多两次尝试，业务版本与账单变化分离，按持久(round,attempt)保存返回并匹配当前调用、建议准入、关闭旧轮、有限修复 | 1,070,144 | exit 0；无错误 |
| Budget / budget-safe | 三节点内部委派树、份额转移、累计用量、未知预留、冲突、可信封账、独立收尾预算 | 1,461,028 | exit 0；无错误 |
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

35 个负对照均只在指定配置启用一个错误变体，且全部返回 exit 12 和预期不变量失败。它们是检查性质是否能捕获指定错误，不能当成当前设计发现了 35 个缺陷。

| 错误变体分组 | 数量 | 捕获的错误 |
|---|---:|---|
| 控制恢复 | 6 | 取消后仍准备、过时代次取得资格、把 not_found 当无效果、忽略已知远端屏障、发送时原资格已丢失、发送时原代次已过期 |
| 决策 | 5 | 接受旧业务版本、采用模型自评、费用未知仍修复、旧轮响应误配当前轮、旧attempt响应误配当前attempt |
| 预算 | 6 | 重复账单再次计费、任务结束即释放未知、父汇总重复计费、绕过深度上限、可信冲突后继续结算、可信冲突后释放预留 |
| 边界 | 9 | 已接纳操作切能力版本、改意图、提前删墓碑、恢复祖先清掉自身暂停、依赖旧控制投影、旧观察动作、按到达顺序覆盖、冲突被擦除、重写历史结果 |
| 核心合同 | 9 | 把键缩成任务级、错权威接纳、输入双消费、旧请求输入、无效批次接纳、拒绝留下部分工作、预算低于预留、重投增额、原外部合同扩额 |

19 个见证配置故意检查“该目标永不发生”的反命题；exit 12 的轨迹就是目标可达的证据，而非安全检查失败。包括成功准入、有限修复、纯账单后沿原提案准入、原提交未见后仍提交、可信无效果后允许新尝试、孙辈委派、可信封账释放、升级后原绑定不变、固定答复后当前事实继续变化、不同用户同裸键、整体有效批次及幂等管理。

涉及顺序的见证使用动作内持久历史标记，避免仅凭最终状态合取误报：

- `control-witness-late-effect`：原请求发送后取消，远端原发送先发生；随后 `ApplyCancelRemote → ExternalEffect → ObservePositive`，证明屏障并不抹去真实迟到效果。
- `budget-witness-late-bill`：`Reserve → Cancel → Report(正增量) → Duplicate`，证明取消后仍能结算真实费用且重复不双计。
- `boundary-witness-resume`：`CreateChild → PauseParent → PauseSelf → ResumeParent → ResumeSelf → Observe → Click`。
- `core-witness-input`：`RefreshText → Consume → ReplayInput`。
- `decision-witness-old-return`：旧轮已发送后关闭，新轮开始并发送；旧轮响应仍按原键保存，新轮只读取自己的响应继续。历史标记绑定当时等待中的新调用键，不能用第三轮读取冒充这次恢复。
- `control-witness-lost-sender-query`：原调用已准备但尚未发送，原发送资格丢失后仍可查询原操作；查询不恢复发送资格。
- `budget-witness-conflict-freeze`：原预留/累计已存，接收受信同修订异累计冲突，再完成并封闭计费源；读取仍显示冲突且保留余额，不能因此结算或释放。

这些标记只观察动作发生的历史，不参与合法动作门禁。全部 trace 可在同名原始日志中逐步复核。

## Lean 一般参数证明

[Control.lean](Control.lean) 只导入 Std，定义完整枚举的 `BudgetStep`、`CoreStep`，分别证明初态、每个动作的保持性，再对 `BudgetReachable` / `CoreReachable` 做有限轨迹归纳。待证不变量没有加入 Step guard。编译 exit 0，14 个导出核心定理的 `#print axioms` 原文在 [control-proof.log](logs/control-proof.log)。

| 范围 | 一般性与核心定理 | 公理依赖 |
|---|---|---|
| 一次预留组件 | 任意 Nat total、cleanup、累计报告和有限长度轨迹；`reachable_budget_safe`、`cumulative_charged_once`、`unknown_allocation_not_released` | `propext`, `Quot.sound` |
| 活动账户冲突冻结 | `conflict_freezes_account`：任意下一步保持 spent、held、finalized；与所有 BudgetStep 构造逐项对应 | `propext` |
| 取消后不新预留 | `cancellation_does_not_create_allocation` | `propext` |
| 重复累计差额与份额转移 | `cumulative_repeat_has_zero_delta`；任意 Nat 两账户 `share_transfer_preserves_total` | `propext`, `Quot.sound` |
| 核心提交/取消/资格 | 任意 Nat 代次和有限长度轨迹；`reachable_core_safe`、`cancel_blocks_new_prepare`、`stale_lease_blocks_new_prepare`、`submission_unknown_never_dispatched`、`cancellation_is_monotone` | `propext` |

源码没有 `sorry`、`admit`、自定义公理或 `native_decide`；成功输出中没有 `sorryAx`。Lean 证明的是预算组件及核心提交状态机，并未证明五个 TLA 文件到实现的精化关系，也未证明任意委派树。Core 的 `send` 是发送资格事实的抽象写入，重复写同一事实是幂等的；一次物理发送与不可恢复发送资格由 TLC 的执行侧模型单独检查。

## 交叉审查后的模型补强

审查前证据连同源码、配置、检查清单和 56 项日志完整保留在 [history/before-correlation-review-20260925T003949Z](history/before-correlation-review-20260925T003949Z/)。该版本不能用来证明本次新增性质。此次发现的是模型表达和检验灵敏度缺口，未据此更改现行架构契约。

1. 原 Decision 返回动作没有响应调用键，无法表达旧响应错配。当前显式保留已发送身份集合及按 `(round,attempt)` 索引的持久返回表；保存返回与应用到当前轮分成两个动作。`ResponseOrigin` 检查响应来自真实已发送调用；`ResponseCorrelation` 观察实际应用键。两个删除匹配守卫的负对照分别在两轮/单次尝试、单轮/两次尝试下捕获误配。
2. 原 `CurrentLease` 仅观察取得准备资格，不能捕获 PhysicalSend 忽略 `senderLost` 或代次的问题。当前 `PhysicalSendQualified` 直接记录物理发送动作发生时的资格，删除任一守卫均产生明确反例；原 Prepare 资格检查继续独立存在。
3. 原 Budget 只把冲突作为无载荷标记，且缺少“冲突后不得推进”的性质。当前 `Conflict(n,r,u)` 明确接收可信生产者同修订、异累计值，记录冲突时的金额；`ConflictFreezesSettlement`、`ConflictFreezesRelease` 和 `ConflictFrozenAmounts` 同时约束后续动作及余额。分别移除 Report/Finalize 的冲突门禁都能被发现。鉴别来源和账单字段可信仍是环境前提。

新的动作历史标记均不参与正常动作的启用条件，不把待证目标加入 guard。当前日志 metadata 标记为 `post-correlation-review-v2`；[FREEZE.json](FREEZE.json) 记录最终模型和配置冻结时间/哈希。审查前 metadata 已归档并明确为旧证据；开发时的解析错误日志另留 history/review-development，不能算安全反例。

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
