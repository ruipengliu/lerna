# Harness 实施票据

用户已确认本轮 88 张实施票的拆分与范围。发布检查补齐了 M3 汇合票漏列的必要前置，见[规格中的依赖说明](spec.md#依赖与实施顺序)。此目录按一票一文件发布；Status 表示分诊状态，实施完成以各票 Implementation 与验收记录为准。01 已完成 SDK 契约样例，02 已完成本地身份、策略与授权，03 已完成持久任务接纳与查询，04 已完成有限 Worker 运行与恢复，05 已完成持久控制与在途核对，06 已完成受限签名授权与委派撤销，07 已完成受控证据与产物，08 已完成有界模型问答及真实验收，09 已完成补充输入与重新决策，10 已完成同步 API 执行与效果确认，11 已完成异步调用与未知效果恢复，12 已完成共享资源接管与恢复，13 已完成千级能力目录、准确版本执行与来源策略，14 已完成本地行动循环与真实多动作小回归；15–17、19–22 亦已完成各自范围，18 实施中，其他票尚未实施。

先读[工作包规格与共同约束](spec.md)，再阅读待认领票及其规范依据。本文只提供导航，交付行为和验收条件以独立票据为准。原架构决策地图保持不变。

## 执行入口

已完成：[01：运行 SDK 契约验证样例](issues/01-sdk.md)、[02：初始化本地身份、策略与授权](issues/02-auth.md)、[03：提交并查询持久任务](issues/03-tasks.md)、[04：运行并恢复有限任务](issues/04-worker.md)、[05：暂停、取消和恢复任务](issues/05-control.md)、[06：签发、派生和撤销受限授权](issues/06-grants.md)、[07：保存并使用受控证据与产物](issues/07-artifacts.md)、[08：在预算内生成并发布答案](issues/08-answer.md)、[09：接收补充输入并重新决策](issues/09-updates.md)、[10：执行并确认同步 API 动作](issues/10-execute.md)、[11：恢复异步与未知效果调用](issues/11-unknown.md)、[12：接管并恢复共享资源](issues/12-takeover.md)、[13：在千级目录中检索准确版本](issues/13-catalog.md)。已完成：[14：让大脑选择 API 并完成任务](issues/14-api-brain.md)（限定范围与模型限制见票据）。已完成：[15：绑定目标使用加密凭证](issues/15-secrets.md)、[16：轮换密钥和凭证并恢复中断](issues/16-rotation.md)（限定范围见票据）。已完成：[17：查看、保存和纠正长期记忆](issues/17-memory.md)。已完成：[19：删除记忆并使派生内容失效](issues/19-deletion.md)（[最终验收](../../docs/implementation/19-final-acceptance.md)）。已完成：[20：从授权来源自动提取端侧记忆](issues/20-extraction.md)（[最终验收](../../docs/implementation/20-final-acceptance.md)）。实施中：[18：让适用记忆改变回答和行动](issues/18-context.md)（真实模型质量仍未通过）。已完成：[21：获取内容并交付可核对证据](issues/21-fetch.md)、[22：搜索、综合证据并回答问题](issues/22-search.md)（[最终验收](../../docs/implementation/22-final-acceptance.md)）。当前可开始：[23：登记节点并验证双向 TLS 身份](issues/23-enroll.md)、[34：通过结构树完成模拟 GUI 动作](issues/34-gui-tree.md)、[43：构建并核验千级单步基准](issues/43-single-data.md)、[50：准备并激活受信能力插件](issues/50-extension.md)、[67：在 bbolt 上运行并恢复任务](issues/67-bolt-run.md)。编号按依赖拓扑排列；一个票的全部阻塞票完成后才可开始，并非必须串行做完所有较小编号。

ready-for-agent 表示规格已准备好，不表示没有阻塞。执行者核对阻塞票的完成记录和适用证据，不仅看标签。完整评测完成与质量通过分别判断；未通过的必要门槛不能用于里程碑晋级或参考发布。

## 能力覆盖导航

| 能力 | 主要实施票 | 完整证据汇合 |
| --- | --- | --- |
| C1 任务理解、规划与决策 | 04、08–09、14、18、22、28、39、62 | 80–87 |
| C2 上下文、记忆与个性化 | 07、17–20、37–39、47、63、70 | 80、85–87 |
| C3 信息获取与证据处理 | 07、21–22、45、65 | 80、83、86 |
| C4 工具调用与设备操作 | 10–16、34–36、43–44、46、54、64、68 | 80–87 |
| C5 任务运行与端云协同 | 02–05、09–12、23–29、37、67–71、77–79 | 80、86–88 |
| C6 扩展接入与协议互操作 | 01、23–26、30–33、50–71 | 80、86–88 |
| C7 授权与用户控制 | 02、05–07、09–12、15–16、19、23、27、30–41、69、74–76 | 各入口验收及 85–88 |
| C8 运行观测与能力评测 | 01、40–49、66、77 | 80–88 |
| C9 受控自进化 | 20、49、51–52、61、72–77 | 80、86–88 |
| A1–A4 架构约束 | 各相关入口、独立替换及端云票 | 80、86、88 |
| V1 联网问答 | 21–22、45、65 | 83、88 |
| V2 手机 GUI | 12、34–36、46 | 84、88 |

索引中的范围表示导航分组，不额外建立阻塞边。各票的 Blocked by 才是已确认的直接阻塞关系。

## 基础运行与 API 执行

| 票据 | 直接阻塞票 |
| --- | --- |
| [01：运行 SDK 契约验证样例](issues/01-sdk.md) | 无 |
| [02：初始化本地身份、策略与授权](issues/02-auth.md) | [01](issues/01-sdk.md) |
| [03：提交并查询持久任务](issues/03-tasks.md) | [02](issues/02-auth.md) |
| [04：运行并恢复有限任务](issues/04-worker.md) | [03](issues/03-tasks.md) |
| [05：暂停、取消和恢复任务](issues/05-control.md) | [04](issues/04-worker.md) |
| [06：签发、派生和撤销受限授权](issues/06-grants.md) | [02](issues/02-auth.md) |
| [07：保存并使用受控证据与产物](issues/07-artifacts.md) | [02](issues/02-auth.md) |
| [08：在预算内生成并发布答案](issues/08-answer.md) | [04](issues/04-worker.md)、[06](issues/06-grants.md)、[07](issues/07-artifacts.md) |
| [09：接收补充输入并重新决策](issues/09-updates.md) | [05](issues/05-control.md)、[08](issues/08-answer.md) |
| [10：执行并确认同步 API 动作](issues/10-execute.md) | [04](issues/04-worker.md)、[06](issues/06-grants.md)、[07](issues/07-artifacts.md) |
| [11：恢复异步与未知效果调用](issues/11-unknown.md) | [10](issues/10-execute.md) |
| [12：接管并恢复共享资源](issues/12-takeover.md) | [11](issues/11-unknown.md) |
| [13：在千级目录中检索准确版本](issues/13-catalog.md) | [10](issues/10-execute.md) |
| [14：让大脑选择 API 并完成任务](issues/14-api-brain.md) | [08](issues/08-answer.md)、[11](issues/11-unknown.md)、[13](issues/13-catalog.md) |
| [15：绑定目标使用加密凭证](issues/15-secrets.md) | [10](issues/10-execute.md) |
| [16：轮换密钥和凭证并恢复中断](issues/16-rotation.md) | [15](issues/15-secrets.md) |

## 记忆、上下文与信息获取

| 票据 | 直接阻塞票 |
| --- | --- |
| [17：查看、保存和纠正长期记忆](issues/17-memory.md) | [07](issues/07-artifacts.md) |
| [18：让适用记忆改变回答和行动](issues/18-context.md) | [14](issues/14-api-brain.md)、[17](issues/17-memory.md) |
| [19：删除记忆并使派生内容失效](issues/19-deletion.md) | [18](issues/18-context.md) |
| [20：从授权来源自动提取端侧记忆](issues/20-extraction.md) | [19](issues/19-deletion.md) |
| [21：获取内容并交付可核对证据](issues/21-fetch.md) | [14](issues/14-api-brain.md) |
| [22：搜索、综合证据并回答问题](issues/22-search.md) | [21](issues/21-fetch.md) |

## 端云、用户交互与模拟设备

| 票据 | 直接阻塞票 |
| --- | --- |
| [23：登记节点并验证双向 TLS 身份](issues/23-enroll.md) | [06](issues/06-grants.md) |
| [24：跨进程执行并核对能力调用](issues/24-grpc.md) | [11](issues/11-unknown.md)、[23](issues/23-enroll.md) |
| [25：通过端云通道双向查询和推送](issues/25-ws.md) | [13](issues/13-catalog.md)、[23](issues/23-enroll.md) |
| [26：断连重启后恢复可靠端云操作](issues/26-reconnect.md) | [11](issues/11-unknown.md)、[25](issues/25-ws.md) |
| [27：同步授权并落实有限离线运行](issues/27-offline.md) | [26](issues/26-reconnect.md) |
| [28：委派子任务并收集结果](issues/28-agents.md) | [14](issues/14-api-brain.md)、[26](issues/26-reconnect.md) |
| [29：协作移交 Task Owner](issues/29-handoff.md) | [05](issues/05-control.md)、[27](issues/27-offline.md)、[28](issues/28-agents.md) |
| [30：通过浏览器会话访问受控入口](issues/30-browser.md) | [26](issues/26-reconnect.md) |
| [31：呈现并提交无任务 UI 交互](issues/31-ui.md) | [30](issues/30-browser.md) |
| [32：从 UI 回应任务及授权等待](issues/32-input-ui.md) | [09](issues/09-updates.md)、[31](issues/31-ui.md) |
| [33：恢复多端多窗口的 UI 状态](issues/33-surfaces.md) | [31](issues/31-ui.md) |
| [34：通过结构树完成模拟 GUI 动作](issues/34-gui-tree.md) | [12](issues/12-takeover.md)、[14](issues/14-api-brain.md) |
| [35：通过图像完成模拟 GUI 动作](issues/35-gui-image.md) | [34](issues/34-gui-tree.md) |
| [36：按资格从 API 切换到 GUI](issues/36-fallback.md) | [34](issues/34-gui-tree.md) |
| [37：同步获准记忆并恢复离线修改](issues/37-mem-sync.md) | [19](issues/19-deletion.md)、[27](issues/27-offline.md) |
| [38：检索端侧数据并只返回许可投影](issues/38-federated.md) | [19](issues/19-deletion.md)、[27](issues/27-offline.md) |
| [39：在端侧完成私有推理与交付](issues/39-private.md) | [28](issues/28-agents.md)、[32](issues/32-input-ui.md)、[38](issues/38-federated.md) |

## 观测、评测机制与测试材料

| 票据 | 直接阻塞票 |
| --- | --- |
| [40：按任务和操作查询运行事实](issues/40-observe.md) | [11](issues/11-unknown.md) |
| [41：跨端观测并执行资料保留策略](issues/41-retention.md) | [19](issues/19-deletion.md)、[26](issues/26-reconnect.md)、[40](issues/40-observe.md) |
| [42：运行冻结计划并生成评测报告](issues/42-evaluation.md) | [14](issues/14-api-brain.md)、[40](issues/40-observe.md) |
| [43：构建并核验千级单步基准](issues/43-single-data.md) | [13](issues/13-catalog.md) |
| [44：构建多步与正确不执行基准](issues/44-multi-data.md) | [43](issues/43-single-data.md) |
| [45：构建联网问答证据与判定集](issues/45-qa-data.md) | [21](issues/21-fetch.md) |
| [46：构建 GUI 两种观察模式基准](issues/46-gui-data.md) | [35](issues/35-gui-image.md)、[36](issues/36-fallback.md) |
| [47：构建个性化成对对照基准](issues/47-personal-data.md) | [38](issues/38-federated.md) |
| [48：恢复、取消和重新判读评测](issues/48-eval-recovery.md) | [41](issues/41-retention.md)、[42](issues/42-evaluation.md) |
| [49：比较候选并验证统计适用性](issues/49-statistics.md) | [42](issues/42-evaluation.md) |

## 插件、Skill 与外部 Agent

| 票据 | 直接阻塞票 |
| --- | --- |
| [50：准备并激活受信能力插件](issues/50-extension.md) | [13](issues/13-catalog.md) |
| [51：升级、停用和回滚固定版本扩展](issues/51-upgrade.md) | [11](issues/11-unknown.md)、[50](issues/50-extension.md) |
| [52：导入用户 Skill 并用于任务](issues/52-skills.md) | [18](issues/18-context.md)、[50](issues/50-extension.md) |
| [53：运行受限 Wasm 计算能力](issues/53-wasm.md) | [50](issues/50-extension.md) |
| [54：让 Wasm 执行受控子动作](issues/54-hostcalls.md) | [14](issues/14-api-brain.md)、[53](issues/53-wasm.md) |
| [55：安装并使用自定义 Renderer](issues/55-renderer.md) | [33](issues/33-surfaces.md)、[50](issues/50-extension.md) |
| [56：通过 MCP stdio 调用工具与资源](issues/56-mcp-client.md) | [11](issues/11-unknown.md)、[15](issues/15-secrets.md)、[50](issues/50-extension.md) |
| [57：通过 MCP stdio 暴露受控能力](issues/57-mcp-server.md) | [11](issues/11-unknown.md)、[50](issues/50-extension.md) |
| [58：验证 MCP Streamable HTTP 双角色](issues/58-mcp-http.md) | [56](issues/56-mcp-client.md)、[57](issues/57-mcp-server.md) |
| [59：向 A2A 外部 Agent 委派任务](issues/59-a2a-client.md) | [15](issues/15-secrets.md)、[28](issues/28-agents.md)、[50](issues/50-extension.md) |
| [60：通过 A2A 受理外部任务](issues/60-a2a-server.md) | [28](issues/28-agents.md)、[50](issues/50-extension.md) |
| [61：逐节点应用扩展版本](issues/61-rollout.md) | [27](issues/27-offline.md)、[51](issues/51-upgrade.md) |

## 独立实现替换

| 票据 | 直接阻塞票 |
| --- | --- |
| [62：替换 Brain 策略与 Model Adapter](issues/62-brain-swap.md) | [18](issues/18-context.md)、[51](issues/51-upgrade.md) |
| [63：替换记忆检索与组织实现](issues/63-memory-swap.md) | [38](issues/38-federated.md)、[50](issues/50-extension.md) |
| [64：替换独立 API Adapter](issues/64-api-swap.md) | [11](issues/11-unknown.md)、[50](issues/50-extension.md) |
| [65：替换搜索与内容获取 Adapter](issues/65-info-swap.md) | [22](issues/22-search.md)、[50](issues/50-extension.md) |
| [66：用独立评分器比较相同运行](issues/66-score-swap.md) | [48](issues/48-eval-recovery.md)、[49](issues/49-statistics.md)、[50](issues/50-extension.md) |
| [67：在 bbolt 上运行并恢复任务](issues/67-bolt-run.md) | [04](issues/04-worker.md) |
| [68：在 bbolt 上执行与接管资源](issues/68-bolt-exec.md) | [12](issues/12-takeover.md)、[67](issues/67-bolt-run.md) |
| [69：在 bbolt 上管理身份与授权](issues/69-bolt-auth.md) | [16](issues/16-rotation.md)、[27](issues/27-offline.md)、[67](issues/67-bolt-run.md) |
| [70：在 bbolt 上使用与同步记忆](issues/70-bolt-memory.md) | [37](issues/37-mem-sync.md)、[39](issues/39-private.md)、[67](issues/67-bolt-run.md) |
| [71：在 bbolt 上管理扩展激活](issues/71-bolt-ext.md) | [61](issues/61-rollout.md)、[67](issues/67-bolt-run.md) |

## 受控自进化与存储迁移

| 票据 | 直接阻塞票 |
| --- | --- |
| [72：从运行证据提出改进候选](issues/72-candidate.md) | [48](issues/48-eval-recovery.md)、[51](issues/51-upgrade.md)、[52](issues/52-skills.md) |
| [73：用独立保留材料判定发布资格](issues/73-qualify.md) | [44](issues/44-multi-data.md)、[49](issues/49-statistics.md)、[72](issues/72-candidate.md) |
| [74：按阶段激活获准改进](issues/74-stages.md) | [61](issues/61-rollout.md)、[73](issues/73-qualify.md) |
| [75：检测退化并停止或回滚候选](issues/75-rollback.md) | [12](issues/12-takeover.md)、[74](issues/74-stages.md) |
| [76：因证据失效撤销后续发布资格](issues/76-evidence-lifecycle.md) | [75](issues/75-rollback.md) |
| [77：在 bbolt 上评测并推进改进](issues/77-bolt-eval.md) | [66](issues/66-score-swap.md)、[67](issues/67-bolt-run.md)、[76](issues/76-evidence-lifecycle.md) |
| [78：停写并校验跨后端逻辑导出导入](issues/78-export.md) | [68](issues/68-bolt-exec.md)、[69](issues/69-bolt-auth.md)、[70](issues/70-bolt-memory.md)、[71](issues/71-bolt-ext.md)、[77](issues/77-bolt-eval.md) |
| [79：双向切换存储并恢复迁移故障](issues/79-migration.md) | [78](issues/78-export.md) |
| [80：验证独立实现和部署位置互换](issues/80-interop.md) | [20](issues/20-extraction.md)、[24](issues/24-grpc.md)、[29](issues/29-handoff.md)、[35](issues/35-gui-image.md)、[36](issues/36-fallback.md)、[53](issues/53-wasm.md)、[55](issues/55-renderer.md)、[58](issues/58-mcp-http.md)、[59](issues/59-a2a-client.md)、[60](issues/60-a2a-server.md)、[62](issues/62-brain-swap.md)、[63](issues/63-memory-swap.md)、[64](issues/64-api-swap.md)、[65](issues/65-info-swap.md)、[79](issues/79-migration.md) |

## 完整质量与开源交接

| 票据 | 直接阻塞票 |
| --- | --- |
| [81：验收完整目录的单步 API 质量](issues/81-single-gate.md) | [43](issues/43-single-data.md)、[48](issues/48-eval-recovery.md)、[52](issues/52-skills.md)、[62](issues/62-brain-swap.md)、[64](issues/64-api-swap.md) |
| [82：验收多步与正确不执行质量](issues/82-multi-gate.md) | [44](issues/44-multi-data.md)、[48](issues/48-eval-recovery.md)、[62](issues/62-brain-swap.md)、[64](issues/64-api-swap.md) |
| [83：验收联网问答专项质量](issues/83-qa-gate.md) | [45](issues/45-qa-data.md)、[48](issues/48-eval-recovery.md)、[62](issues/62-brain-swap.md)、[65](issues/65-info-swap.md) |
| [84：验收 GUI 两种观察模式质量](issues/84-gui-gate.md) | [46](issues/46-gui-data.md)、[48](issues/48-eval-recovery.md)、[62](issues/62-brain-swap.md) |
| [85：验收个性化效果与隐私出口](issues/85-personal-gate.md) | [20](issues/20-extraction.md)、[39](issues/39-private.md)、[47](issues/47-personal-data.md)、[48](issues/48-eval-recovery.md)、[63](issues/63-memory-swap.md) |
| [86：汇总运行恢复与治理完整证据](issues/86-fault-gate.md) | [54](issues/54-hostcalls.md)、[80](issues/80-interop.md) |
| [87：验证候选的实际改善和回滚](issues/87-improvement-gate.md) | [81](issues/81-single-gate.md)、[82](issues/82-multi-gate.md)、[83](issues/83-qa-gate.md)、[84](issues/84-gui-gate.md)、[85](issues/85-personal-gate.md)、[86](issues/86-fault-gate.md) |
| [88：交付可复现的开源参考版本](issues/88-handover.md) | [87](issues/87-improvement-gate.md) |
