# 两域持久责任交接验证包

结果与边界集中见 [TLC 检查与 Lean 证明结果](../../docs/research/handoff-verification-results.md)。本包只验证两个独立持久提交域之间的一项交接，B 的 Work 表示后续责任，不表示外部动作已经执行。

| 文件 | 用途 |
| --- | --- |
| [Handoff.tla](Handoff.tla) | 基准状态机、性质、两种刻意错误变体 |
| [safety.cfg](safety.cfg)、[liveness.cfg](liveness.cfg)、[healthy.cfg](healthy.cfg) | 完整有限状态安全检查及有条件活性检查 |
| `mutant-*.cfg`、`no-fairness.cfg` | 应当产生反例的负面对照 |
| `witness-*.cfg` | 用不变量反例建立成功、缺口与活性前提的可达性 |
| [Lean 模型与证明](lean/Handoff.lean) | 一般参数下的安全证明及具体恢复轨迹见证 |
| [run_checks.py](run_checks.py) | 运行检查，保留完整日志、命令、退出码和 SHA-256 |
| [最终运行清单](evidence/checked/manifest.json) | 交付版本的实际运行证据索引 |
| [首轮清单](evidence/tlc-initial/manifest.json) | 增加生命周期检查前的 TLC 结果，原规格与配置在其 `sources/` 子目录 |

## 复跑

工具固定为 TLA+ 发布包 v1.7.4 中的 TLC 2.19、Lean 4.19.0，使用 Lean 随附的 `Std`。工具下载来源和哈希见 [工具来源](evidence/toolchain.json)。仓库不包含工具二进制。

在本机、仓库根目录运行下列命令，会在一个新目录保存证据，不覆盖交付记录：

```sh
python3 formal/handoff/run_checks.py --output /tmp/handoff-check-20260925
```

其他机器通过 `--java /path/to/java --tlc /path/to/tla2tools.jar --lean /path/to/lean` 指定同版本工具。TLC 的 jar 哈希必须相同；Lean 校验版本并记录实际二进制哈希，不要求不同平台二进制相同。仅使用 Python 标准库，单个检查限时 300 秒。

脚本对每项检查核对退出码和预期诊断：基准必须完成且无反例；错误变体必须命中指定性质；可达性检查必须生成指定见证。`all_expectations_matched=true` 表示这组预期均成立，不能理解为错误变体也满足不变量。

所有配置关闭一般死锁报告，允许完成或资源耗尽后停留；需要最终推进的场景由显式时序性质检查。没有 `CONSTRAINT`、`ACTION_CONSTRAINT`、对称约简或随机模拟；容量和预算是状态机自身的有限参数。`-workers 1 -seed 1 -fp 0` 固定本次探索配置。TLC 状态指纹碰撞估计保留在原日志中。

## 开发记录

首轮基准与负面对照均符合预期，未发现现行交接契约的反例。独立审查后增加了 `PhaseMonotonic` 和 Lean 对应生命周期定理，防止将来新增“重置为 idle”动作时责任性质真空成立；同时补充 ACK 丢失后崩溃、重投并完成的 Lean 见证。这些是验证覆盖补强，没有修改架构契约。

增加 TLA+ 动作时序性质时，第一次使用裸 `[]A` 被语义分析器拒绝，日志保留在 [monotonic-initial.log](evidence/monotonic-initial.log)；改成 `[][A]_s` 后 [检查完成](evidence/monotonic-fixed.log)。这是规格语法修正，不是系统设计反例。最终结果以 `evidence/checked/` 为准。
