# 端云协议规范

[端点通信模块](README.md) · [标准注册表](schemas/standard-registry.json) · [契约校验与互操作](validation/README.md)

本入口供独立客户端、服务端、领域处理器及第三方适配器实现同一套端云契约。协议 Interface 包括消息结构、身份与操作关联、成功含义、先后约束、权限、恢复及错误处理；合法 JSON 不足以证明符合协议。v1 尚未发布，当前规范、领域合同、Schema 和示例是同一份草案，运行实现与互操作仍待验证。

规范采用经获准网关的 WSS／UTF-8 JSON 消息通道，HTTPS 承担认证、票据、受限恢复资格引导和大内容。支持的业务类型按领域安装和协商；连接成功不默认启用全部领域。第三方实现可以使用自己的语言、存储和调度器，仍须兑现本入口所引用的持久交接及可观察行为。

<a id="reading"></a>
## 1. 完整阅读顺序

先读共同契约，再读自己提供或调用的领域规范；调用方、处理方和透明中继承担不同责任，不能只实现发送的 payload 而省略响应、权限或恢复行为。

| 顺序 | 规范性文档 | 实现必须明确的行为 |
| --- | --- | --- |
| 1 | [消息契约与可靠交接](message-contract.md) | 原消息与原操作、两处持久交接、目标回执、可靠／临时类别、内容交付及身份隔离 |
| 2 | [传输与会话](transport.md) | HTTPS 与 WSS 绑定、票据及来源、活动所有者、最终目标协商、scope／lane／流注册、连续 ACK、窗口与接收限制 |
| 3 | [恢复与控制](recovery-and-control.md) | 连接、流及业务行为分别判定；领域证据、切点与连续变化、未知效果、离线、取消及后置交接边界 |
| 4 | [跨端领域共同合同](domain-profiles.md) | 受限身份引导、领域作用域、共享来源和证明、各领域交接及其权威文档 |
| 5 | [任务、执行与 UI 线映射](task-and-ui.md)及下节所需领域合同 | 请求／响应／事实配对、状态与结果含义、输入消费、完整快照、每端呈现、控制和原答复查询 |
| 6 | [扩展与兼容](extensions.md) | 受控安装、类型／视图协商、必要附加项、fallback、第三方声明和版本冻结 |
| 7 | [线格式与字段](wire-format.md)、[标准注册表](schemas/standard-registry.json)及所选 Schema | 严格解析、字段关系、错误与后续动作、类型及交付类别的机器约束 |
| 8 | [契约校验与互操作](validation/README.md) | 固定同一规范版本，以合法／非法例子、失败恢复和独立实现交换验证上述行为 |

公共规则对所有已启用消息生效。领域规范只能按已注册类型收紧公共约束，不能自行更改消息身份、回执、权限或恢复含义；不支持必要语义时拒绝该消息或交互。

<a id="domains"></a>
## 2. 领域规范性引用

下表指向行为权威，属于所选领域的规范阅读范围。声明支持的类型及其必需依赖所涉及的请求、响应、状态、成功条件、原操作恢复和拒绝行为均须实现；内部表名、工作者组织和默认宿主方案属于实现说明。完整线字段由同版 Schema 限定，不能因内部逻辑接口有额外字段就直接发送。

| 领域 | 行为与合同权威 | 机器资产 |
| --- | --- | --- |
| 任务生命周期、结果及用户控制 | [任务生命周期](../task-kernel/lifecycle.md)、[核心接口及固定结果查询](../task-kernel/storage-and-interfaces.md#result-recovery)、[控制与管理](../task-kernel/control-and-management.md)、[任务恢复](../task-kernel/recovery-and-validation.md)；线映射见[任务消息](task-and-ui.md) | task、task-control |
| 准确目录、执行与设备管理 | [执行合同](../capability-and-execution/catalog-and-contracts.md)、[跨端执行](../capability-and-execution/remote-contracts.md) | execution、execution-control |
| 身份、许可与恢复引导 | [身份接口](../identity-and-authorization/contracts.md)、[判定与恢复](../identity-and-authorization/mechanisms.md)、[跨端授权](../identity-and-authorization/cross-endpoint.md)，包括同权威 [BeginUseSet](../identity-and-authorization/mechanisms.md#use-set) 及原集合查询 | identity、domain-common、http |
| 远程大脑 | [调用与运行合同](../brain-system/contracts-and-runtime.md)中的原调用、资格、结果／用量及恢复规则 | brain |
| 记忆与同步 | [记忆接口](../memory-system/contracts.md)、[同步与恢复](../memory-system/synchronization.md)，包括独立管理元数据读取与[有限查询集合](../memory-system/mechanisms.md#pagination) | memory |
| 内容、来源与受管副本 | [共同内容与来源合同](../content-and-provenance.md)，内容 HTTP 绑定见[传输规范](transport.md#content) | common、domain-common、governance、http |
| 用户交互 | [交互语义与领域映射](../application-and-interaction/interaction-contract.md)，包括[目录](../application-and-interaction/interaction-contract.md#directory)、[预览](../application-and-interaction/interaction-contract.md#preview)及[管理](../application-and-interaction/interaction-contract.md#management)；消息配对见[UI 线映射](task-and-ui.md) | ui、interaction |
| Agent 协作 | [协作规范入口](../agent-coordination/peer-contracts.md#normative)及其指定的[委派处理规则](../agent-coordination/delegation.md) | coordination |
| 批准与跨节点发布 | [改进合同](../observation-and-improvement/contracts.md)、[发布合同](../extensions-and-runtime/contracts.md)中的批准、原管理交接和回执规则 | release |

资产均集中在 [schemas/](schemas/standard-message.schema.json)，具体消息注册、Schema 引用、lane、作用域解析与恢复要求以[注册表](schemas/standard-registry.json)为准。调用方须理解当前接纳、最终结果与效果保证；例如 UI accepted 只确认转交责任，委派 recorded 不证明子任务已接纳，执行 finished 不证明效果已知。各领域是这些结论的裁决方，通用交付不替它们推断。

## 3. 互操作必须保留的交接

下表用于检查完整阅读后是否遗漏行为，详细规则以链接指向的规范为准。

| 交接 | 可观察的成功边界 | 断线、重复或未知后的责任 |
| --- | --- | --- |
| 可靠消息 | 最终目标已持久保存消息及待处理责任，ACK 只覆盖连续前缀 | 重投原消息；中继保管不触发源端提前清理；窗口耗尽留下缺口 |
| 业务请求 | 处理方已经保存适用的接纳／拒绝决定及必要后续工作；只读查询按其类型返回 | 有操作身份的请求沿原 operation_id 核对原决定；ACK、超时或一次未见不能替代业务事实 |
| 业务响应与事实 | 保留原请求、原操作和受信生产者关联，按领域修订解释 | 迟到事实按当前接收／披露权限处理，不重新开放已取消目标工作 |
| UI 输入与呈现 | 输入由业务权威消费；完整内容与每端开闭分别恢复 | 父子操作关联持续保存；关闭不撤回已接纳输入，后台快照不自动打开 |
| 结果与内容 | 固定原结果与当前状态分开；必需字节获准取得并验证后才算完整交付 | 通过领域查询原结果，不重做动作恢复内容，不以摘要代替不可用成果 |
| 恢复与新行动 | 当前轮次及 owner 下，领域依据已应用且后续变化连续 | 缺口只限制依赖它的行为；查询、控制和事实收尾不能被旧行动的门禁一并阻断 |

业务状态、领域裁决与各自后续责任不要求共享全局事务。实现可以合并本地存储步骤，但不能消除调用方可观察的交接差异，或让返回成功先于可恢复事实。

<a id="versions"></a>
## 4. 规范版本与实现选择

当前草案整体更新公共正文、领域合同、Schema、注册表与示例，不混用不同草案却都自称 v1 的资产。正式发布时须固定一份完整规范清单：本入口的共同文档、所发布领域的规范性正文及其递归引用、精确类型版本、Schema／注册表和一致性示例，记录其版本与内容摘要并随发布保存。仅锁定 JSON Schema 或指向领域设计的可变最新链接，不能保证行为兼容。

引用文档可以同时包含规范与实现说明。发布清单须标明所引用的行为章节或锚点；这些章节的必要交叉引用也在同一快照中固定。若正文和机器资产矛盾，该组合不能宣称符合规范，须修正后再发布；不能由实现自行挑选更宽松的一方。已发布同名同版结构与行为冻结，变更按[扩展版本规则](extensions.md#version)演进。

[消息交付实现](delivery-and-recovery.md)中的逻辑接口、EndpointLedger／DeliveryWork／RecoveryRound 记录、本地事务、工作领取和默认部署用于落实本模块的参考实现；它们不要求第三方复制表结构或采用同一数据库。第三方仍必须以其实现证明持久接收、未知提交核对、唯一活动所有者、按行为恢复与有界容量成立。规范里的“持久保存后确认”是保证，“在哪张表或哪次扫描完成”是实现选择。

<a id="conformance"></a>
## 5. 版本声明与验证边界

实现声明须列出协议版本、可接收的 type／type_version／kind、附加项和视图、必要路径能力、接收限制，以及尚未开放的条件能力。原始来源和权限不能由声明自行授予；协商只选择已经安装且通过验证的语义。

静态资产按[校验命令](validation/README.md#static)验证；独立实现按[互操作矩阵](validation/README.md#interop)交换真实消息并注入失败；持久事务、owner 隔离、收尾与负载依据[通信运行验收](validation.md)取证。支持的领域再执行各自验收，默认同宿主示例通过不代表跨端或全部领域可用。端端直连、跨主机接替和任务权威迁移仍按[范围限制](README.md#scope)保持关闭。
