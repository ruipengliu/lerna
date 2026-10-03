# System One 模型：关键结论

来源核对日期：2026-09-28。TypeSafe 的 Jev 是托管的有类型判断模型候选，可用于已有候选中的只读选择。它不生成完整计划、参数、代码或正文；输出类型受约束不等于判断正确、权限成立或目标已经完成。[官方概念](https://docs.typesafe.ai/concepts/system-one)、[模型说明](https://docs.typesafe.ai/models)

## 1 能力与接入缺口

| 方面 | 关键结果与实施要求 |
| --- | --- |
| Choice / Score / Noul | Choice 从最多 255 个候选选择；Score 对 2–10 个有序等级给期望值；Noul 给 yes 概率。Score 不能恢复精确金额或时间，Noul 没有独立 confidence 字段。[Choice](https://docs.typesafe.ai/primitives/choice)、[Score](https://docs.typesafe.ai/primitives/score)、[Noul](https://docs.typesafe.ai/primitives/noul) |
| 校准与正确性 | confidence、输出概率与校准分别解释；概率高不保证单次正确。候选缺项时需要拒判路径。[说明](https://docs.typesafe.ai/confidence) |
| 开放范围 | 当时找到托管 API 和开放 SDK，未找到官方 Jev 权重及可执行的本地部署说明。不能从开源 SDK 推导模型可自托管 |
| 版本与成本 | 历史价格、时延和倍数来自供应商自报，依赖模型版本、区域和任务；不作为本项目预算上界或性能承诺。[官方评测](https://evals.typesafe.ai/) |
| 透明重试 | JS/Python SDK 默认初次后最多两次重试；接入前关闭，并检查代理是否另有重试。[JS 源码](https://github.com/typesafe-ai/typesafe-sdk-js/blob/66880ccded6cb642dc1809620c2b108c33730214/src/retry.ts)、[Python 源码](https://github.com/typesafe-ai/typesafe-sdk-python/blob/f078f1e208a0d885154dc758344ae4fce77ac168/src/typesafe_sdk/_core/retry.py) |
| 请求和费用恢复 | 原核查未找到承诺的幂等键、原结果查询、取消后最终费用或单次硬费用上界。超时与本地取消不证明服务端停止；缺失用量不得填 0 |

## 2 相邻路线

| 路线 | 可以比较的用途 | 代价与限制 |
| --- | --- | --- |
| [AnyJev](https://github.com/nokia-applied-research/AnyJev/tree/45add301a7aa60ed3420c83d15c061e84e5bce61) | 用现有模型的概率或隐藏状态构造专用判断与校准 | 需要标注数据、固定问题和校准维护，模型/问题变化须重验 |
| [TypeLLM](https://github.com/TypeLLM/TypeLLM/tree/1bef50919bb43fd07055d6ef9452bb797d7e3ef3) | 基于 SGLang 的类型约束解码，可作结构化 LLM 对照 | “一个输出 token”只适用于 enum/Boolean 字段，不代表整个任务一次推断 |
| [System One Adapter](https://github.com/typesafe-ai/system-one-adapter-python/blob/e1d4cc938204b22fc5a3c3aca7044072fe3f712d/README.md) | 以普通 LLM API 模拟相同判断形状 | 可能生成再归一化概率并追加纠错请求，不具有相同训练、延迟或校准保证 |

## 3 试验前提

选择原本就需要模型判断的只读问题，避免给所有任务额外增加路由调用。固定模型版本、候选、中文与边界样本，测准确率、选中后错误、拒判覆盖、校准、端到端延迟及全部物理调用成本。

版本、权限、预算、效果与恢复仍由 Harness 负责。真实账户需验证原请求追踪、超时、重试、用量缺失和费用未知；能力缺口未补齐时限制开放范围。接入合同见[Brain](../architecture/brain/README.md#6-规则与小模型怎样启用)。后续实际使用时再按[源码说明](README.md#source-download)下载所引 SDK；供应商滚动文档需届时重新核对。
