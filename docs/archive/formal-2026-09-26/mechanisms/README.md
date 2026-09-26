# 全方案机制验证包

本页及所述结果属于 2026-09-26 归档的旧架构；[归档范围与路径映射](../README.md)说明复跑入口及历史证据边界。

[结果报告](../../../research/all-mechanisms-verification-results.md)解释已取得的结论、反例和限制；[覆盖索引](../../../research/all-mechanisms-coverage.md)逐项关联机制及架构验收用例。这里包含设计模型和证明，完整生产验收尚未执行。

| 组 | 对象 | 结果与范围 |
| --- | --- | --- |
| control | 核心、执行、大脑、协作、预算及控制 | [说明](control/README.md) |
| trust | 身份、授权、来源、记忆及恢复 | [说明](trust/README.md) |
| lifecycle | UI、引用生命周期、评测与版本发布 | [说明](lifecycle/README.md) |
| communication | 消息流、协议规则、配额及资格组合 | [说明](communication/README.md) |
| 既有 handoff | 两个持久域的一项责任交接 | [原验证包](../handoff/README.md) |

每组 `checks.json` 注册模型、配置、预期退出码和诊断；`inventory.json` 记录具体性质和剩余边界。统一 [运行清单](evidence/final/manifest.json)与[合并库存](evidence/coverage.json)为机器可读入口。原始运行、开发失败和组内结果保留在各 evidence 目录；`checked` 是交叉审查补强前的快照，最终交付结果以 `final` 清单为准。

```sh
python3 docs/archive/formal-2026-09-26/mechanisms/run_checks.py --output /tmp/mechanism-verification-rerun
```

`--group control` 等选项可缩小复跑范围。工具沿用已固定的 [版本与来源](../handoff/evidence/toolchain.json)；运行命令不下载依赖或改动架构设计。输出目录必须尚不存在。所有 Lean 证明只使用随工具链提供的 Std。

原协议静态校验另行运行，输出保存在 [protocol-static.log](evidence/protocol-static.log)。它不被计为 TLC、Lean 或真实运行验收。源文档基线指纹在 [architecture-baseline.json](evidence/architecture-baseline.json)。

[证据审计脚本](audit_evidence.py)核对已交付 `evidence/final` 的源文件和日志哈希、完整配置注册、Lean 公理输出、本地链接路径及用例集合；原审计结果保留在 [static-checks.json](evidence/static-checks.json)。归档后的脚本默认仅输出新审计，可用 `--output` 保存到尚不存在的文件；历史源码哈希差异仍会报告为失败，不回写旧记录。该审计与实际 TLC／Lean 运行分别记录。机制在其他组复用的具体子规则见 [cross-group-mappings.json](cross-group-mappings.json)，不增加模型或检查计数。
