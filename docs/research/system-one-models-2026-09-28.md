# System One 模型调研：Jev 与相邻实现路线

核对日期：2026-09-28。本文依据供应商文档、官方 SDK 源码、项目作者发布的代码与实验材料；没有调用付费 API、读取凭据、下载模型权重或复现性能实验。文中“官方自报”表示材料作者对自己的产品或实现所作的测量，不表示本项目已验证。

**建议把 Jev 作为 Brain 中有界语义判断的候选实现，优先评估只读候选选择。** 它能够替换原本需要生成模型判断的一处调用，但不能把模型判断变成确定规则，也不会直接生成完整计划、任意参数或报告正文。确定字段已有且规则可以裁决时，继续使用零模型 shortcut。部署和恢复能力尚有公开资料缺口，不能仅凭低价格和类型正确保证进入严格预算的正式调用链。

本文集中保存外部证据；Brain 的接入职责、异常行为与评估方案由 [Brain 应用方案](../architecture/brain/decision-paths.md)定义，不在这里改变现行架构契约。

## 1. 身份与产品边界

TypeSafe AI 官网为 [typesafe.ai](https://typesafe.ai/)，首页的 Docs 链接指向 [docs.typesafe.ai](https://docs.typesafe.ai/introduction)。官方文档的 GitHub 链接指向 [typesafe-ai](https://github.com/typesafe-ai)，该组织又反向链接官网。本文据这条双向归属链核对来源；没有把含 Jev / TypeSafe 名称的其他域名或社区网关作为官方接口依据。

Jev 是 TypeSafe 于 2026-09-15 宣布 early access 的 System One 模型。“System One”在这里是供应商对快速、聚焦、带类型的判断模型的称呼，不是行业统一的能力等级。官方宣称采用新架构、并行采样与 RLCD（Reinforcement Learning for Calibrated Decisions）；本次未找到足以独立复现这些训练与架构声明的完整公开模型实现。[发布文章](https://typesafe.ai/blog/introducing-system-one-models-and-jev)

它接收自然语言和结构化状态，输出已枚举的选择、评分或真假概率；不生成文本、代码或推理解释。概率校准描述一组预测的统计表现，不保证某次判断正确。供应商的“零幻觉”宣传所支持的是输出服从预先定义的类型；不能据此推出判断正确、权限成立、证据充分或动作安全。[System One 概念](https://docs.typesafe.ai/concepts/system-one)

本次核对到的是官方托管 API、开放 SDK 与 LLM 对照适配器。没有找到官方 Jev 权重下载、权重许可或可执行的本地部署说明；因此不能把开源 SDK 写成开源 Jev。模型页还明确当前不提供按客户微调或 LoRA 适配，领域知识通过请求内容提供。[模型说明](https://docs.typesafe.ai/models)、[官方仓库](https://github.com/typesafe-ai)

## 2. 接口能做什么

HTTP 入口为 `POST https://api.typesafe.ai/v1/systemone`，Bearer 认证；请求顶层是 `model`、`state`、`questions`，其中 `questions` 是命名问题映射。响应包括实际 `model`、同名 `answers` 与 `usage.input_tokens / output_tokens`。问题映射的 key 用于关联答案，不传给底层模型；真正的问题必须写在 `instructions` 中。[HTTP API](https://docs.typesafe.ai/api)

| 原语 | 输入约束 | 输出含义 | 对 Brain 的用途 |
|---|---|---|---|
| Choice | `criteria` 为选项名到说明的映射，最多 255 项；选项名和说明都会进入模型 | `choice` 是最大概率选项，`probabilities` 为各选项分布，另有 `confidence` | 从已经构造且有版本的候选中选择一个；候选不完备时显式提供“均不适合” |
| Score | `criteria` 为 2–10 个有序等级描述 | `score` 是从 0 开始的等级索引期望，另有 `legend`、分布、`confidence` | 单维相关性或质量等级，不能拿小数插值恢复精确金额、时间或参数 |
| Noul | 真假问题，可附 `true / false` 的判定说明 | 只有 `noul`，范围 0–1，表示回答 yes 的概率；没有独立 `confidence` 字段 | 为某一明确语义条件提供判断信号，由代码结合其他条件决定是否采用 |

字段与语义分别核对 [Choice](https://docs.typesafe.ai/primitives/choice)、[Score](https://docs.typesafe.ai/primitives/score)、[Noul](https://docs.typesafe.ai/primitives/noul)。Choice 的封闭输出集合不验证输入候选是否真实可用；候选与工具绑定、可执行性和授权仍须由本项目检查。

`state` 可为字符串、JSON 对象或数组。每个问题都独立读取同一 state，不能读取其他问题或其他答案；有数据依赖的后续问题必须由代码准备新输入。接口仅接收文本与结构化文本，不接收原始图像、音频或视频。页面、截图或音频转换成事实的过程有自己的成本与可信度边界。[State](https://docs.typesafe.ai/concepts/state)

可把同一已知 state 上的独立判断放在一个请求中。官方称增加问题通常对延迟影响小，但每个额外问题仍消耗输入 token；这不等于无限问题、免费并行或可以跨越观察后的因果依赖。本次没有在公开 API 文档中找到独立的问题个数上限，仍受总输入限制。[并行问题模式](https://docs.typesafe.ai/patterns/fan-out)、[Choice 批量建议](https://docs.typesafe.ai/primitives/choice)

### 概率、confidence 和校准不能互换

Choice / Score 的 `confidence` 是从整个概率分布形状计算的统计量，不是另一次模型验证，也不等于最大选项概率。官方目前没有在该文档公布完整计算公式；本项目应保存原始分布、原始 confidence 与模型版本，不能自行猜测公式复算或跨供应方比较同名字段。[Confidence](https://docs.typesafe.ai/confidence)

官方限制说明承认：同一语义分别用 Noul 与 Choice 表达时，数值不保证相同；分别询问正命题与否命题也不保证概率相加为 1。阈值必须绑定具体问题、原语和版本。英语是主要训练语言，中文等输入需单独验证；精确计算、长状态中的无关信息、多跳关系与对抗内容均属于已知薄弱处。[Jev 1.13 已知限制](https://docs.typesafe.ai/model-jaggedness/jev-1.13)、[语言支持](https://docs.typesafe.ai/models)

独立研究也应纳入验收设计。Sun 等人的预印本通过交换选项名与评分说明的绑定，展示“输出类型始终合法，但语义判断随名称极性明显变化”的反例；包含托管 Jev 在英文 Choice 条件下的检验。它是作者实验，未由本项目复现，也不代表自然流量错误率。对本项目最直接的启示是加入候选改名、重排、否定与说明冲突测试，而不是把类型合法率当判断准确率。[论文 v2](https://arxiv.org/html/2609.26758v2)

## 3. 版本、价格与性能证据

截至核对日，模型页公布 `jev-1.13.0`；`jev-latest` 与 `jev-preview` 当前均指向它，别名会移动。正式评估应固定版本，并核对返回的实际版本。输入上限有两层：每请求 64k token，且 `state + 最长单个问题` 不超过 32k；不能把 64k 全留给 state。公布限额为 250,000 token/s、1,200 请求/min，同时明说 early access 期间限额可能无通知调整。[模型与别名](https://docs.typesafe.ai/models)

| 证据 | 官方或作者公开结果 | 测量条件与适用边界 |
|---|---|---|
| Jev 牌价 | 输入 $0.042 / 百万 token，输出免费 | 当前公开定价，不是本项目账单；请求仍报告输出 token。按输入 2,000 token 计算为 $0.000084，仅是牌价算例 |
| Jev 时延宣传 | 70–500 ms 端到端；首页另有 193.6 倍快、444.6 倍便宜 | 官方自报；主要从美国西海岸笔记本测量，服务在同一区域；倍率来自四个官方 workflow，官方承认处于预期真实收益较高的一端 |
| 官方 workflow eval | 四种工作流按等权聚合成本、时间和准确度 | 参考标签来自两个大模型概率的平均，非人工真值；被测模型使用各供应方默认 reasoning，不能当本项目生产准确率 |
| Jev 同状态批量问题 | 13 问合并请求为 $0.000497、0.27 s；逐问为 $0.006090、2.71 s | 官方 cookbook 使用 `jev-1.12`，约 54,000 字符文章、两种策略各 5 次；逐问时间按串行累加，不能与并发逐问混淆 |

价格来自[模型页](https://docs.typesafe.ai/models)；时延与倍率来自[发布文章](https://typesafe.ai/blog/introducing-system-one-models-and-jev)；评估方法来自[官方 eval](https://evals.typesafe.ai/)；批量数据来自[Parallel questions cookbook](https://docs.typesafe.ai/cookbooks/parallel_questions)。以上均不是本项目实测，不能合并为统一 benchmark 或许诺中国网络环境的端到端时延。

## 4. 与 Harness 调用契约的适配缺口

低成本不能替代调用事实。即使没有自由文本输出，一次 Jev 远端推断仍须计为一次模型调用，保留发送、预算、版本与结果事实。下面区分“公开可核验”与“本次未找到契约”；后者不表示供应商一定没有能力，而表示当前没有依据依赖它。

| 能力 | 核对结果 | 对接时的约束 |
|---|---|---|
| 用量 | HTTP 响应含输入与输出 token；当前 Python SDK 允许二者缺失为 `None` | 只按实际报告记账，缺失不得补 0；不把估计量登记成最终账单 |
| 请求追踪 | JS SDK 从 `x-typesafe-request-id` 读取 requestId，也允许不存在 | 保存供应方 ID；该字段本身不证明幂等、可查询或唯一计费 |
| SDK 自动重试 | JS / Python 默认初次后最多 2 次重试；覆盖 408、429、5xx、连接错误和超时 | 进入“一次物理调用”协议前关闭自动重试；升级或重试必须由上层单独登记和准入 |
| 关闭重试 | JS `retry: { maxRetries: 0 }`；Python `RetryPolicy(max_retries=0)` | 同时检查自定义传输、代理或网关是否另有重试，不能只关一层 |
| 超时与取消 | JS 默认每次尝试 10 s，无总重试预算；支持 AbortSignal；Python 当前 retry 总预算默认 30 s，用于阻止继续重试 | 客户端超时或 abort 不是服务端停止与费用终结证据；Python retry budget 也不是强制截止当前推断 |
| 服务端幂等与原结果查询 | 公开 API 与已核源码未找到已承诺的幂等 key 语义、按原调用查询结果接口 | 响应丢失不能通过重复 POST 冒充恢复原调用；保持未知事实，交回既有恢复机制 |
| 最终账单与取消结算 | 未找到按原请求查询最终费用、取消后费用终结或“不执行”证明的公开接口 | 不能在本地等待结束时自行释放原调用的所有预留 |
| 单次硬费用上界 | 有输入牌价和上下文限制，未找到 `max_cost`、精确预计数接口、计费 token 与限制 token 等价及失败计费的完整承诺 | 牌价乘输入上限可用于估算，不能直接升格为协议保证；严格准入所需的可信上界待供应方或受控服务补齐 |

源码核对固定为：

- JS SDK：[`66880cc` types](https://github.com/typesafe-ai/typesafe-sdk-js/blob/66880ccded6cb642dc1809620c2b108c33730214/src/types.ts)、[retry](https://github.com/typesafe-ai/typesafe-sdk-js/blob/66880ccded6cb642dc1809620c2b108c33730214/src/retry.ts)、[client](https://github.com/typesafe-ai/typesafe-sdk-js/blob/66880ccded6cb642dc1809620c2b108c33730214/src/client.ts)、[requestId](https://github.com/typesafe-ai/typesafe-sdk-js/blob/66880ccded6cb642dc1809620c2b108c33730214/src/api-promise.ts)。
- Python SDK：[`f078f1e` retry](https://github.com/typesafe-ai/typesafe-sdk-python/blob/f078f1e208a0d885154dc758344ae4fce77ac168/src/typesafe_sdk/_core/retry.py)、[response types](https://github.com/typesafe-ai/typesafe-sdk-python/blob/f078f1e208a0d885154dc758344ae4fce77ac168/src/typesafe_sdk/_core/response_types.py)。
- 检索范围：[HTTP API](https://docs.typesafe.ai/api)、[完整公开文档索引](https://docs.typesafe.ai/llms.txt)、上述 SDK 的请求、重试与响应实现。无认证的公开阅读不能验证账户专有能力或合同条款。

文档和 SDK 的输入宽松程度还有差异：HTTP 参考将 `instructions` 标为必需；JS 类型允许省略或 null，SDK 的 state 类型也允许 null。应用方案应使用两者的收敛子集——显式问题、非空有效 state、显式候选说明——不依赖这些宽松边界。JS 只做静态类型转换，不因此省略本项目对原始响应键、类型、有限数值、分布和选项绑定的检查。[JS types 与 client](https://github.com/typesafe-ai/typesafe-sdk-js/tree/66880ccded6cb642dc1809620c2b108c33730214/src)

## 5. 相邻路线不能按产品名视作等价模型

| 路线 | 实际机制 | 适用条件与主要代价 |
|---|---|---|
| Jev | 托管的非文本生成判断模型 | API 改造较小；需实测领域判断与中文输入，受网络、版本和供应方恢复能力约束 |
| AnyJev | 在现有 LLM 上读取标签概率或隐藏状态，附加去偏、校准及每问题判断 head | 固定高频问题、已有领域标注、可自托管时值得评估；问题与模型变化需维护校准产物，成本由标注和部署团队承担 |
| TypeLLM | 基于 SGLang，为自回归 LLM 做类型约束解码，可保留文本生成和 thinking | 可作为结构化 LLM 对照；“一个输出 token”只适用于 enum / Boolean 字段，不能推广为每任务一次推断或恒定低延迟 |
| TypeSafe System One Adapter | 用 LLM API 模拟相同判断形状 | 用于同题对照；概率可由 LLM 生成再归一化，且存在纠错请求，不代表具备 Jev 的训练、并行或校准保证 |

AnyJev `v0.1.0` 固定于 [`45add30`](https://github.com/nokia-applied-research/AnyJev/tree/45add301a7aa60ed3420c83d15c061e84e5bce61)。L0 对 K 选项 Choice 使用 K 份旋转 prompt，通过旋转平均降低位置偏差，不保证任意重排后答案完全不变；L1 用同题标签做温度校准；L2 对固定模型、固定问题以标注数据求解小型 head，通常需要 100–300 条标签并可提前结束模型层计算。L2 不能自动泛化到任意新问题；当前字母读取路线最多 26 选项，conformal abstention 仍在计划中。源码采用 Apache-2.0，基础模型和数据许可另计。[分层契约](https://github.com/nokia-applied-research/AnyJev/blob/45add301a7aa60ed3420c83d15c061e84e5bce61/docs/levels.md)、[限制与许可](https://github.com/nokia-applied-research/AnyJev/blob/45add301a7aa60ed3420c83d15c061e84e5bce61/README.md)

AnyJev 作者在 H100 NVL、bf16、1,000-token state 上报告：Qwen3-4B 截至第 24/36 层时，单 prompt forward 为 38.9 ms，完整 forward 为 55.0 ms；批量摊销数据另列。这里不包括 Brain 状态整理、网络、模型装载或完整任务循环，也不是托管 Jev 的同场测量。本项目没有复现。[作者时延表](https://github.com/nokia-applied-research/AnyJev/blob/45add301a7aa60ed3420c83d15c061e84e5bce61/docs/results_exit.md)

TypeLLM `v0.2.4` 固定于 [`1bef509`](https://github.com/TypeLLM/TypeLLM/tree/1bef50919bb43fd07055d6ef9452bb797d7e3ef3)，代码 Apache-2.0。它支持最多 24 值的 enum；单个 SDK 调用可能展开为多次 SGLang 请求，须按真实物理请求审查 Harness 适配。返回的标签分布是约束候选 token 概率，不能因字段同名就复用 Jev 的 confidence 门槛。开启 thinking 或自由文本后仍有正常生成开销。[README](https://github.com/TypeLLM/TypeLLM/blob/1bef50919bb43fd07055d6ef9452bb797d7e3ef3/README.md)

TypeLLM 作者在 231 个公开 JevBench 题目的一次实验中报告：无 thinking 的请求 p50/p95 为 1.296/2.150 s；开启 thinking 为 6.677/61.083 s。环境为 RTX PRO 6000 Blackwell、SGLang 0.5.19、Qwen3.8-27B 的 NVFP4 权重与 BF16 LM head；计时包含客户端和 SSH/IAP 网络，缓存条件并非匹配的冷启动比较，费用未计量。它说明类型约束与低延迟必须分开评估，不能据不同项目的表格宣布相对优劣。[实验方法](https://github.com/TypeLLM/TypeLLM/blob/1bef50919bb43fd07055d6ef9452bb797d7e3ef3/evals/jevbench/METHOD.md)、[结果](https://github.com/TypeLLM/TypeLLM/blob/1bef50919bb43fd07055d6ef9452bb797d7e3ef3/evals/jevbench/README.md)

官方 LLM 对照适配器在 [`e1d4cc9`](https://github.com/typesafe-ai/system-one-adapter-python/blob/e1d4cc938204b22fc5a3c3aca7044072fe3f712d/README.md) 中明确区分结构化输出、概率或离散答案，以及结构错误纠正重试。用于本项目对照时，应记录每次底层调用，关闭或另行登记纠错与瞬态重试；不能把一次 adapter 方法调用统计成一次模型调用。

## 6. 进入 Brain 试验前要回答的问题

优先将规则已经无法解决、原本就要调用模型的只读选择作为试验点，避免在所有任务前多加一次语义路由。高 confidence 只参与是否采用语义建议的判断；版本、新鲜度、预算、能力可用性、授权与效果确认仍由现有责任方裁决。

后续评估必须同时回答：

1. **判断是否可靠。** 用真实候选与人工可核标签测试选择准确率、选中后错误率、拒判覆盖率、概率校准；独立覆盖中文、候选缺项、重排改名、否定、陈旧状态和注入内容。通过确定性门禁与语义判断分别记分。
2. **整条链是否更快、更省。** 比较规则、原 LLM、Jev，以及“Jev 未采用后再走 LLM”的总次数、输入、费用、p50/p95；计入状态准备、排队、持久化、网络、后续升级。不能只比较供应方的单次 forward。
3. **失败能否按原契约恢复。** 核实自动重试确实关闭；模拟发送前崩溃、发送后丢响应、缺 usage、版本漂移与预算不足。没有可信用量或终结证据时，保留未知，而不是因为结果便宜就推断无费用。
4. **准入是否有完整依据。** 向供应方或受控服务确认单次计费上界与结算终结语义。该问题未解决时，研究与离线重放仍可继续，严格预算的正式链路不能宣称已可接入。

本次完成的是外部资料与源码语义核对；没有提交模型请求，没有验证本项目数据上的准确率、费用、时延或恢复行为。上述待核条件应在应用方案入口可见，而不是隐藏在性能结论之后。
