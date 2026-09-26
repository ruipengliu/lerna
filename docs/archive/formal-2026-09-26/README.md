# 旧架构形式化验证归档

本目录于 2026-09-26 从根目录 `formal/` 迁入，与[旧架构](../architecture-2026-09-26/README.md)配套保存。[交接验证包](handoff/README.md)和[机制验证包](mechanisms/README.md)中的模型、证明、反例、清单和通过结论均针对归档前的旧方案，不能作为现行 `docs/architecture/` 的验证结果。

## 历史路径与证据

原始日志、JSON 清单与库存、工具信息、失败记录、`history/` 以及 `evidence/` 内的源码快照、补丁和图像保持原样。它们记录的绝对工作区路径、时间、行号及 SHA-256 是当时的事实，不随此次归档改写。

| 历史路径前缀 | 归档后的查阅位置 |
| --- | --- |
| `formal/` | `docs/archive/formal-2026-09-26/` |
| `docs/architecture/` | `docs/archive/architecture-2026-09-26/` |
| `docs/research/` | 原位置；相关报告只解释旧架构证据 |

[路径映射器](archive_paths.py)只将前两种历史路径解析到归档目录，保留清单原有名称和摘要。可运行入口已适配迁移后的目录层级；`evidence/` 和 `history/` 内的脚本属于原始源码快照，未适配为独立入口。快照内 Markdown 的相对链接也保留原文，应按其原文件所在位置解释。

旧架构正文为保持导航所做的链接调整，以及本目录的入口脚本适配，都会改变相关文件摘要。此次迁移没有刷新历史摘要、审计通过状态或形式验证结论。新审计仍会报告这些源码差异；路径映射不豁免哈希校验。归档前已有部分架构与协议文件不同于历史基线，因此不能将新审计的全部差异归因于此次迁移。历史清单中的行号同样是原版本定位信息，不保证与迁移后正文完全一致。

## 复跑与重建边界

从仓库根目录使用下列入口，将新执行结果保存到尚不存在的独立目录：

```sh
python3 docs/archive/formal-2026-09-26/handoff/run_checks.py --output /tmp/handoff-archive-rerun
python3 docs/archive/formal-2026-09-26/mechanisms/run_checks.py --output /tmp/mechanisms-archive-rerun
```

工具版本、参数和模型检查逻辑延续各验证包说明；新清单会记录实际归档路径、适配后的入口及路径映射器摘要。能否复跑取决于指定版本的 TLC、Lean 和 Java 是否可用。本次归档检查了路径解析、Python 语法、入口参数和证据文件完整性，并执行只读证据审计；没有重新执行 TLC 或 Lean。

`mechanisms/audit_evidence.py` 默认仅向终端输出新审计，可用 `--output /tmp/formal-archive-audit.json` 写入尚不存在的文件。旧 `evidence/static-checks.json` 始终保留原审计结果，不能解读为此次迁移检查结果。

库存生成器、`build_coverage.py`、`make_checks.py` 和组内 `run_local.py` 保留开发期写入行为；它们可能覆盖库存、配置、日志或研究报告，因此仅在独立副本中使用。库存与覆盖生成器已经固定读取归档架构，不会转向现行架构。重新生成库存不等于重新验证模型，修改模型或契约后须另行执行并保存新的验证证据。
