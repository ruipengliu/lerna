# 22 票真实模型评价 v6

执行前登记，沿用 v5 的固定八例顺序、材料、原语义标准、512 输出上限及每例一次调用，无自动重试。v5 运行 7/8，失败例在 fetch_failed gap 混入了 search-candidates Ref；保留全部原始输出和失败。本轮提示明确仅使用 external-evidence-gap Ref，覆盖所有失败，仍允许部分成功正文的有据 claims；insufficient 保留真实无结果发现来源。原 Brain 校验不变。能力版本 citations-v5，计划 search-small-prospective-v6。

本轮八元预留，前四轮合计32元预留保留，均不是实际账单。累计模型费用必须低于用户授权500元。本轮为已知材料回归，不声称盲测；生成后独立评价。原代码同时新增公网单任务入口与可配置搜索超时，参考批次仍保留默认1000ms，不调用公网搜索。实际HTTP延迟及恢复组合race19.302秒通过，模型及CLI race、相关vet通过，增量Spec/Standards审查无剩余发现。锁定代码、配置与文件摘要见 execution-lock.json。
