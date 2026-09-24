# 跨模块待决索引

[整体设计](README.md) · [系统验证](validation.md)

本页集中索引设计决策及依赖，不复制完整提案；推荐、替代、代价和联动清单由表中唯一的权威位置维护。下列提案均未采用，所属模块现有本地边界继续有效。是否需要某项增强由实际需求决定，不要求为了完成文档而一次批准全部能力。

<a id="cross-endpoint"></a>
## 1. 跨端合同

先确定受信主体、授权及完整来源语义，再按部署需要补领域载荷。共享身份依据不能替代执行、记忆、大脑或委派各自的状态与恢复合同。具体传输的 HTTPS／领域消息选择保留在身份及目录提案中统一比较。

| 提案 | 要决定的问题与当前边界 | 权威位置与联动 |
| --- | --- | --- |
| 身份 P1 | 授权解析、恢复、离线证明及受信确认的默认跨端合同；缺席时不能靠 grant_ref 或 UI 同意建立授权闭环 | [身份提案](identity-and-authorization/validation.md#proposals)；协议、核心、执行、UI、记忆及协作消费它 |
| CE-P1 | 准确能力声明及受信执行依据如何跨端取得；本地目录不能证明远端声明已验证 | [执行提案](capability-and-execution/validation.md#proposals)；依赖身份 P1 |
| CE-P2 | 执行事实修订、可信用量及完整取消答复恢复；现行保守合并与超窗缺口继续有效 | [执行提案](capability-and-execution/validation.md#proposals)；核心及协作共同使用，任务侧取消完整查询仍须联动 |
| MS-P1 | 记忆访问、修改核对和获准视图恢复的领域 profile；缺席时远程访问／同步关闭 | [记忆提案](memory-system/validation.md#proposals)；复用身份／来源规则 |
| CO-P1 | 委派合同、接纳、控制、结果与累计用量的跨端编码；本地内部委派合同不等于线协议 | [协作提案](coordination-and-cloud/validation.md#proposals)；依赖身份 P1，按需采用 CE-P1／P2 |
| BS-P1 | 独立远程 Brain 的调用资格、原调用恢复及用量回送；同进程模型适配不受此项阻塞 | [大脑提案](brain-system/validation.md#proposals)；核心与协议联动 |
| UI-P2 | 复用上述领域的远程管理合同；不另建通用 UI 授权旁路 | [交互范围](application-and-interaction/validation.md#proposals)只记录 UI 限制，完整合同归各领域 |

<a id="data-cleanup"></a>
<a id="release-approval"></a>
<a id="automation"></a>
## 2. 数据与发布治理

| 合并事项 | 要决定的问题与当前边界 | 唯一详细提案 |
| --- | --- | --- |
| MS-P2／OI-P2：外部派生副本清理 | 哪些载体受管，何时登记持有者，停止使用与物理清理如何回执；本提供端清理见[共同合同](content-and-provenance.md)，外部全链清理仍未成立 | [MS-P2](memory-system/validation.md#proposals)；观测侧只补导入与报告适用性限制 |
| OI-P1／ER-P1：自动批准与撤回交接 | 精确批准、激活前复核及撤回应用如何交接；当前受信人工核验与逐目标处置继续有效 | [OI-P1](observation-and-improvement/validation.md#proposals)；扩展侧引用接纳与激活责任 |
| MS-P3／OI-P3：终态自动触发 | 触发哪些任务、用途和预算归属、幂等键及独立责任；当前采用显式提取／Propose／普通生成任务 | [MS-P3](memory-system/validation.md#proposals)；改进侧补候选入口关联 |

以上合并只统一提案维护位置，没有采用自动发布、自动触发或全链删除。停用、旧版恢复与不可逆迁移边界属于现行[发布与回退设计](observation-and-improvement/improvement.md#monitoring)，不等待自动发布提案才能表达清楚。

<a id="interaction"></a>
## 3. 交互范围

| 提案 | 要决定的问题与当前边界 | 权威位置 |
| --- | --- | --- |
| UI-P1 | 新设备是否需要恢复未知 task_id／surface_id 的全量目录及订阅；当前只恢复宿主已知且获准对象 | [交互提案](application-and-interaction/validation.md#proposals) |
| UI-P3 | 是否提供中间成果的可恢复呈现，以及输入与必需预览的结构化绑定；当前仅开放可凭完整请求作答的输入 | [交互提案](application-and-interaction/validation.md#proposals)；协议截图场景以它为前提 |

<a id="deployment"></a>
## 4. 部署与持续运行

| 提案／边界 | 要决定的问题与维持现状的代价 | 权威位置 |
| --- | --- | --- |
| CO-P2 | 写权威迁移范围、唯一生效记录及用户操作索引；当前固定权威，失联不自动接管 | [协作提案](coordination-and-cloud/validation.md#proposals) |
| ER-P2 | 跨节点制品与管理回执；当前本地成功不证明离线节点已经生效／停用 | [扩展提案](extensions-and-runtime/validation.md#proposals) |
| ER-P3 | 有状态组件在线替换、记录兼容与迁移边界；当前排空，长任务与未知效果可能延迟升级 | [扩展提案](extensions-and-runtime/validation.md#proposals) |
| 直连与离线的条件能力 | 直连接入合同未齐；离线需许可、可信时间、唯一账本及本地依赖。已定义的条件不因适配器待实现变为新的架构决策 | [协议范围](endpoint-cloud-protocol/README.md#scope)、[身份离线机制](identity-and-authorization/mechanisms.md#offline) |

## 5. 状态维护

每项决定更新负责模块的推荐、当前行为和联动范围，并同步本索引；进入生效设计时统一更新正文、图、契约及示例，不保留无共存要求的旧草案分支。实现和运行验证进展另记，不能据 Schema 已存在把设计提案改成已启用。

UI-P0、CO-P0 的导航及邻接引用已纳入文档维护，不再占用待决项。历史审查及处理情况保留在[审查记录](maintenance/review-2026-09-24.md)，模块正文不再用一次编写任务的修改权限描述长期架构边界。
