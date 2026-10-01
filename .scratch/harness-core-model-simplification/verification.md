# 核心模型收敛验证记录

日期：2026-10-01。交付范围为设计文档与静态检查，运行行为及实测收益分别登记。

## 1. 比较点与交付入口

| 项目 | 固定范围或入口 |
| --- | --- |
| Review base | `1b647dc970317173f349296e47efd41a4cf3a23c`，本轮全部实施差异的起点 |
| 06 集成起点 | `d291e03c8285475988dfa51c1e46891bb2286e79`，包含已完成且合入的 01～05 |
| 集成分支 | `codex/harness-core-model-simplification` |
| 06 分支 | `codex/core-model-06` |
| 设计整合提交 | `0bffbdc8013194631bb28c49278b4de3cfb85fcf`；此后仅补记固定提交号及整合检查结果 |
| 规格与实施 | [spec](spec.md)、[实施图](README.md)、[票据 06](issues/06-document-integration.md)、[54 条故事与 24 项决策追踪](traceability.md) |
| 架构阅读链 | [主入口](../../docs/architecture/README.md) → [六组对象](../../docs/architecture/.draft/core-data-model.md) → [默认应用](../../docs/architecture/.draft/application-workflow.md) → [四类读写](../../docs/architecture/.draft/request-data-flows.md) → [参考覆盖](../../docs/research/agent-harness-comparison/core-model-semantic-coverage.md) → [17 组验收](../../docs/architecture/.draft/validation/core-model-scenarios.md) |
| 设计整合说明 | [架构交付记录](../../docs/architecture/.draft/review.md#core-model-review)；行为规则仍归九模块，矩阵与目录不构成第二套规范 |

本票据只修改七份 Owned 架构文档、本记录及票据 06，共九份 Markdown；feature README、spec 与最终双轴审查记录由根协调者维护。`docs/architecture/ochestrator/` 沿用既有目录名。对整轮差异使用固定 review base，对本票据差异使用 06 集成起点，不将此前架构刷新计入本次实施。

固定设计差异可分别读取整轮和本票范围；最终双轴审查还须包含该设计提交之后的验证记录及统一修复提交。

```sh
git diff 1b647dc970317173f349296e47efd41a4cf3a23c 0bffbdc8013194631bb28c49278b4de3cfb85fcf
git diff d291e03c8285475988dfa51c1e46891bb2286e79 0bffbdc8013194631bb28c49278b4de3cfb85fcf
```

## 2. 实际静态检查

复用原 `check_documents.py`，导入后仅为每次调用设定 `ROOT`，未修改或复制检查器。检查本地链接和锚点、围栏、表格列数、尾随空格及对另一架构基线的引用；不检查外链可达性、参考源码语义或 Mermaid 渲染。

```sh
python3 -B - <<'PY'
import importlib.util
from pathlib import Path

path = Path('docs/architecture/.draft/validation/check_documents.py')
spec = importlib.util.spec_from_file_location('check_documents', path)
checks = importlib.util.module_from_spec(spec)
spec.loader.exec_module(checks)
failed = False
for scope in (
    'docs/architecture',
    'docs/adr',
    'docs/research/agent-harness-comparison',
    'docs/research/codex',
    'docs/research/pi',
    'docs/research/deepseek-harness',
    'docs/research/prime-agent',
    'docs/research/crush',
    '.scratch/harness-core-model-simplification',
):
    print(scope)
    checks.ROOT = Path(scope).resolve()
    failed |= checks.main()
raise SystemExit(failed)
PY
```

| ROOT 范围 | Markdown | 本地链接 | Mermaid 块 | 结果 |
| --- | --- | --- | --- | --- |
| docs/architecture | 60 | 1846 | 124 | PASS，0 错误 |
| docs/adr | 10 | 13 | 0 | PASS，0 错误 |
| docs/research/agent-harness-comparison | 9 | 179 | 1 | PASS，0 错误 |
| docs/research/codex | 1 | 47 | 3 | PASS，0 错误 |
| docs/research/pi | 1 | 45 | 2 | PASS，0 错误 |
| docs/research/deepseek-harness | 1 | 12 | 2 | PASS，0 错误 |
| docs/research/prime-agent | 1 | 28 | 4 | PASS，0 错误 |
| docs/research/crush | 1 | 30 | 4 | PASS，0 错误 |
| .scratch/harness-core-model-simplification | 10 | 233 | 0 | PASS，0 错误 |

范围按独立 ROOT 分别计数；Mermaid 数量仅表示检查器识别的代码围栏数。研究范围不包含其他日期化报告或历史架构目录。矩阵的 209 个固定源码路径／行号及五个快照检查沿用[票据 02 的实际记录](issues/02-reference-semantic-coverage.md#comments)，本票未重复运行源码检查或上游测试。

一次性静态编号核对通过：traceability 的 US-01～54、ID-01～24 及验收 CM-01～17 均连续且无重复。01～05 的 Progress 均为 completed，验收项全部勾选，其提交已包含在 `d291e03` 中。这些检查只证明追踪结构与依赖状态，不增加运行证据。

## 3. 契约、ADR 与历史来源

从仓库根目录执行以下差异检查；边界包括整个 contracts、validation 下全部 Python／JavaScript 验证程序及依赖清单、十份 ADR、历史来源和架构基线。

```sh
git diff --exit-code 1b647dc970317173f349296e47efd41a4cf3a23c -- docs/architecture/.draft/contracts ':(glob)docs/architecture/.draft/validation/**/*.py' ':(glob)docs/architecture/.draft/validation/**/*.mjs' docs/architecture/.draft/validation/requirements.txt docs/adr docs/research/agent-harness-comparison/sources.json docs/research/agent-harness-comparison/architecture-baseline.md
python3 -B - <<'PY'
import json
from pathlib import Path
methods = json.loads(Path('docs/architecture/.draft/contracts/schemas/methods.json').read_text())['methods']
print('Registered domain methods:', len(methods))
assert len(methods) == 105
PY
git diff --check 1b647dc970317173f349296e47efd41a4cf3a23c
```

结果：保护范围的 `git diff --exit-code` 退出 0，无输出；登记仍为 105 个领域方法；`git diff --check` 退出 0。保护范围实际包含 contracts 的 84 份受版本控制文件、16 份验证程序及依赖清单、10 份 ADR 和 2 份历史来源／基线文件。Schema、方法、示例、验证程序与 ADR 决定均未改变，历史哈希未重写。

票据 06 未重复运行已有协议、Brain、传输、签名、租约或发布构造套件。整轮实施中，[票据 04](issues/04-domain-and-extension-semantics.md#comments)已实际执行协议静态检查和 Brain 静态向量；各通过记录保留原执行范围，不代表运行系统已经通过验证。

## 4. 设计检查与未运行项目

导航核对以六组对象目录、默认应用入口、四类读写、SM-01～16／G-01～07 和 CM-01～17 为主线。必要的原命令、未知效果、可信确认、准确绑定、费用收尾及当前继续条件由原负责模块保留；静态配置可用于最小开发装配，按需能力与生产 ADR 另有明确入口。本记录只登记该整合核对，标准／规格双轴审查尚未完成。

本轮未实现或运行 Harness 内核、SDK、数据库／存储适配器、真实模型或搜索、平台隔离和生产恢复实验；未运行参考项目、未做 Mermaid 渲染及 IO／性能 benchmark。CM-01～17 全部仍是待运行规格。A／B／D 的设计调用和持久阶段数不能写成实测 SQL、事务、sync 或物理 IO，C 的重建与后续续行须分别计量。

## 5. 整合交接状态

01～05 已完成并合入本票起点，06 的文档整合与检查证据由本记录及票据 Comments 交付。traceability 中 ID-24 已同步为设计整合完成、运行未验证。双轴审查、发现项统一修复、整个 spec 的关闭及工作树清理由根协调者收尾；在完成前保留 pending，不把本票完成解释为整轮已关闭。

设计提交后执行 `git merge --no-edit codex/harness-core-model-simplification`，当时集成 tip 仍为 `d291e03c8285475988dfa51c1e46891bb2286e79`，输出 `Already up to date.`；工作树检查为空。随后补记本节固定引用，供集成代理合入和审查。
