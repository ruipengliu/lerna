# 研究关键结果

本目录保留影响设计与开发选择的结论、主要来源及验证边界。实施以[现行架构](../architecture/README.md)、[领域术语](../../CONTEXT.md)和[开发准备度](../architecture/engineering/implementation-readiness.md)为准。研究日期表示原资料核对时间；本次精简没有重新核验外部网站或运行上游程序。

## 阅读入口

| 主题 | 保留内容 |
| --- | --- |
| [Agent Harness 比较](agent-harness-comparison/README.md) | 五个参考项目的关键机制、适用边界与十二项工程建议 |
| [核心对象与语义](agent-harness-comparison/core-model-semantic-coverage.md) | 六组对象的职责、十六类语义及可选能力边界 |
| [数据与读写](agent-harness-comparison/data-flow-io-comparison.md) | 指定场景的静态计数结论、持久确认差异与本项目计量方法 |
| [记忆库](agent-memory-libraries-2026-09-28.md) | 七组实现可借鉴的部分、接入代价、授权和删除限制 |
| [记忆论文](agent-memory-papers-2026-09-28.md) | 写入、时序、检索、经验与撤销的关键研究结果 |
| [AI 研究启示](ai-report-2026-09-project-implications.md) | 对完成判断、工具、上下文、Skill 与评测的设计启示 |
| [System One 模型](system-one-models-2026-09-28.md) | Jev 等候选的能力边界、恢复缺口和试验前提 |
| [形式化验证](formal-methods-for-architecture.md) | 历史验证的关键结果、适用条件与当前验收入口 |

文档写作研究的可用结论已归入 [AGENTS.md](../../AGENTS.md)。逐项目摘录、检索过程、逐用例库存及旧检查产物可在[清理前版本](https://github.com/ruipengliu/lerna/tree/51c574affc68e806a8c1fb8a814d38e63c066ffc/docs/research)追溯。

<a id="source-download"></a>
## 上游源码按需下载

日常阅读和本项目文档检查只需这些结论页。后续需要复核源码、移植机制或执行上游试验时，再下载相关项目的固定版本即可；本地没有 `.reference/` 不属于文档检查失败。

五个 Harness 的仓库、commit、tree 和建议本地路径见[源码版本清单](agent-harness-comparison/sources.json)。记忆库和模型 SDK 的固定版本在对应结论页的来源链接中。下载时使用引用中的 commit，不用最新分支替代原研究版本；若研究新版本，单独更新结论与版本记录。

以下是首次准备 Codex 参考目录的示例。已有目录时先核对 origin 与本地修改，再取得所需提交：

```sh
git init .reference/codex
git -C .reference/codex remote add origin https://github.com/openai/codex.git
git -C .reference/codex fetch --depth 1 origin d4a475adda850d80b6149c76454de94e0cf4fd51
git -C .reference/codex checkout --detach FETCH_HEAD
git -C .reference/codex rev-parse HEAD HEAD^{tree}
```

将输出的 commit 和 tree 与清单核对。`.reference/` 已被项目 Git 忽略。读取源码后若要复制或修改，核对固定版本的实际许可证；Crush 研究版本为 FSL-1.1-MIT，不能当作当前已全部采用 MIT。下载完成也不表示已安装依赖、运行测试或复现研究结论。

<a id="documentation-checks"></a>
## 文档维护检查

从仓库根目录运行，要求 Python 3.10 或以上及 Git：

```sh
python3 docs/check_documentation.py
python3 -m unittest discover -s docs -p test_documentation_checks.py
```

检查覆盖当前本地链接、资源、锚点及本地 Git 中可取得的项目历史引用；代码示例与外部网站不在可达性检查范围内。上游源码与性能复核在实际使用时另行执行，结果须绑定源码版本、环境和测量方法。其余设计检查见[架构验收](../architecture/validation/README.md)。
