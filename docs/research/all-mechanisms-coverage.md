# 全机制与验收用例覆盖索引

本索引由各组库存合并，逐项关联当前方案。**登记了覆盖关系不等于完整用例通过。** model-property 只表示已建模子性质；static-only 只表示结构／固定样本；runtime-required 表示需要实际提供方；uncovered 表示规则尚未进入模型。

当前登记 104 个机制族、291 条模块原编号用例及 6 条内容专题本地编号用例。原始义务、模型属性和剩余边界见 [机器可读库存](../../formal/mechanisms/evidence/coverage.json)。实际工具结果见 [总报告](all-mechanisms-verification-results.md)。

## communication：机制

[组内结果与模型边界](../../formal/mechanisms/communication/README.md)

| ID | 机制 | 库存覆盖声明 |
| --- | --- | --- |
| COM-01 | 本地持久接纳与跨域责任交接 | partial-abstract-rule |
| COM-02 | 固定身份、作用域及原意图不可改绑 | partial-abstract-rule |
| COM-03 | 乱序暂存、连续ACK与连续业务交接 | partial-abstract-rule |
| COM-04 | 中继保管不代表目标接收 | partial-abstract-rule |
| COM-05 | 提交未知、事实与知识分离 | partial-abstract-rule |
| COM-06 | 超窗先保留缺口后删除载荷 | partial-abstract-rule |
| COM-07 | 重连位置单调、回退进入核对 | partial-abstract-rule |
| COM-08 | 资源本地已应用证据向量、失效屏障及迟到事实 | partial-abstract-rule |
| COM-09 | 原操作未知不自动重做 | partial-abstract-rule |
| COM-10 | 有限重试、预算跨重启保留、被动收尾 | partial-abstract-rule |
| COM-11 | 用户汇总配额、保留份额与有条件公平 | partial-abstract-rule |
| COM-12 | 最终目标和全路径能力协商 | partial-abstract-rule |
| COM-13 | 已知可选项校验与required最低约束 | partial-abstract-rule |
| COM-14 | 兼容视图不丢必需输入 | partial-abstract-rule |
| COM-15 | 严格编码、消息关联及发布资产版本 | structural-only |
| COM-16 | 连接、领域接纳、正式结果和内容可用分开 | structural-only |
| COM-17 | 部署、离线可信根、备份及互操作 | runtime-required |

### communication：原编号用例

下列分类针对抽象义务；真实实现的整条运行验收均未执行。每行完整刺激／判据及未覆盖细项在 JSON 库存中保留。

| 用例与原文 | 证据分类 | 抽象范围／剩余边界 |
| --- | --- | --- |
| [EC-01](../architecture/endpoint-communication/validation.md)（第 28 行） | model-property | partial; complete runtime case NOT executed |
| [EC-02](../architecture/endpoint-communication/validation.md)（第 29 行） | model-property | partial; complete runtime case NOT executed |
| [EC-03](../architecture/endpoint-communication/validation.md)（第 30 行） | model-property | partial; complete runtime case NOT executed |
| [EC-04](../architecture/endpoint-communication/validation.md)（第 31 行） | model-property | partial; complete runtime case NOT executed |
| [EC-05](../architecture/endpoint-communication/validation.md)（第 32 行） | model-property | partial; complete runtime case NOT executed |
| [EC-06](../architecture/endpoint-communication/validation.md)（第 33 行） | model-property | partial; complete runtime case NOT executed |
| [EC-13](../architecture/endpoint-communication/validation.md)（第 34 行） | model-property | partial; complete runtime case NOT executed |
| [EC-14](../architecture/endpoint-communication/validation.md)（第 35 行） | model-property | partial; complete runtime case NOT executed |
| [EC-15](../architecture/endpoint-communication/validation.md)（第 36 行） | model-property | partial; complete runtime case NOT executed |
| [EC-16](../architecture/endpoint-communication/validation.md)（第 37 行） | model-property | partial; complete runtime case NOT executed |
| [EC-17](../architecture/endpoint-communication/validation.md)（第 38 行） | model-property | partial; complete runtime case NOT executed |
| [EC-19](../architecture/endpoint-communication/validation.md)（第 39 行） | model-property | partial; complete runtime case NOT executed |

## control：机制

[组内结果与模型边界](../../formal/mechanisms/control/README.md)

| ID | 机制 | 库存覆盖声明 |
| --- | --- | --- |
| CTL-01 | 固定用户写权威与跨任务操作身份 | abstract-partial |
| CTL-02 | 本地原子提交与跨域提交Unknown | abstract-partial |
| CTL-03 | 建议与核心裁决分权、批次整体准入 | abstract-partial |
| CTL-04 | 业务版本与纯账务版本分离 | abstract-partial |
| CTL-05 | 领取代次、不可恢复发送资格与旧轮封闭 | abstract-partial |
| CTL-06 | 持久调用身份与有限结构修复 | abstract-partial |
| CTL-07 | 就绪前沿、持久阻塞条件与事实唤醒 | abstract-partial |
| CTL-08 | 取消/暂停与准备提交竞争 | abstract-partial |
| CTL-09 | 远端控制投影与本地动作门禁 | abstract-partial |
| CTL-10 | 可能启动切点与实际效果/观察知识分离 | abstract-partial |
| CTL-11 | 先查原操作，封闭且可信无效果才新尝试 | abstract-partial |
| CTL-12 | 逻辑租约与物理隔离分离 | abstract-partial |
| CTL-13 | 验收条件先固定、成果/证据版本核验 | abstract-partial |
| CTL-14 | 多维事实单调合并与固定历史答复 | abstract-partial |
| CTL-15 | 累计用量、修订去重与冲突冻结 | abstract-partial |
| CTL-16 | 未知预留、可信封账与目标/收尾双预算 | abstract-partial |
| CTL-17 | 内部委派份额、层级有界与单路径计费 | abstract-partial |
| CTL-18 | 委派合同、子身份与分阶段接纳 | abstract-partial |
| CTL-19 | 外部Agent固定提交键与能力合同 | abstract-partial |
| CTL-20 | 输入至多一次消费与权限分离 | abstract-partial |
| CTL-21 | 准确能力声明与实现版本固定 | abstract-partial |
| CTL-22 | GUI观察—单动作—再观察 | abstract-partial |
| CTL-23 | 有限主动核对与持续被动接收 | abstract-partial |
| CTL-24 | 清理正文、身份墓碑与备份缺口关闭 | abstract-partial |
| CTL-25 | 容量、公平、隔离与真实效果验证 | partial-reused（跨组通用规则；原组状态保留） |
| CTL-26 | 管理预算修订与内外委派边界 | abstract-partial |

### control：原编号用例

下列分类针对抽象义务；真实实现的整条运行验收均未执行。每行完整刺激／判据及未覆盖细项在 JSON 库存中保留。

| 用例与原文 | 证据分类 | 抽象范围／剩余边界 |
| --- | --- | --- |
| [TK-01](../architecture/task-kernel/recovery-and-validation.md)（第 113 行） | 逐义务分类见库存 | 逐义务见库存 |
| [TK-01a](../architecture/task-kernel/recovery-and-validation.md)（第 114 行） | 逐义务分类见库存 | 逐义务见库存 |
| [TK-02](../architecture/task-kernel/recovery-and-validation.md)（第 115 行） | 逐义务分类见库存 | 逐义务见库存 |
| [TK-03](../architecture/task-kernel/recovery-and-validation.md)（第 116 行） | 逐义务分类见库存 | 逐义务见库存 |
| [TK-04](../architecture/task-kernel/recovery-and-validation.md)（第 117 行） | 逐义务分类见库存 | 逐义务见库存 |
| [TK-04a](../architecture/task-kernel/recovery-and-validation.md)（第 118 行） | 逐义务分类见库存 | 逐义务见库存 |
| [TK-04b](../architecture/task-kernel/recovery-and-validation.md)（第 119 行） | 逐义务分类见库存 | 逐义务见库存 |
| [TK-05](../architecture/task-kernel/recovery-and-validation.md)（第 120 行） | 逐义务分类见库存 | 逐义务见库存 |
| [TK-05a](../architecture/task-kernel/recovery-and-validation.md)（第 121 行） | 逐义务分类见库存 | 逐义务见库存 |
| [TK-06](../architecture/task-kernel/recovery-and-validation.md)（第 122 行） | 逐义务分类见库存 | 逐义务见库存 |
| [TK-07](../architecture/task-kernel/recovery-and-validation.md)（第 123 行） | 逐义务分类见库存 | 逐义务见库存 |
| [TK-07a](../architecture/task-kernel/recovery-and-validation.md)（第 124 行） | 逐义务分类见库存 | 逐义务见库存 |
| [TK-08](../architecture/task-kernel/recovery-and-validation.md)（第 125 行） | 逐义务分类见库存 | 逐义务见库存 |
| [TK-09](../architecture/task-kernel/recovery-and-validation.md)（第 126 行） | 逐义务分类见库存 | 逐义务见库存 |
| [TK-10](../architecture/task-kernel/recovery-and-validation.md)（第 127 行） | 逐义务分类见库存 | 逐义务见库存 |
| [TK-11](../architecture/task-kernel/recovery-and-validation.md)（第 128 行） | 逐义务分类见库存 | 逐义务见库存 |
| [TK-11a](../architecture/task-kernel/recovery-and-validation.md)（第 129 行） | 逐义务分类见库存 | 逐义务见库存 |
| [TK-12](../architecture/task-kernel/recovery-and-validation.md)（第 130 行） | 逐义务分类见库存 | 逐义务见库存 |
| [TK-13](../architecture/task-kernel/recovery-and-validation.md)（第 131 行） | 逐义务分类见库存 | 逐义务见库存 |
| [TK-13b](../architecture/task-kernel/recovery-and-validation.md)（第 132 行） | 逐义务分类见库存 | 逐义务见库存 |
| [TK-13c](../architecture/task-kernel/recovery-and-validation.md)（第 133 行） | 逐义务分类见库存 | 逐义务见库存 |
| [TK-13a](../architecture/task-kernel/recovery-and-validation.md)（第 134 行） | 逐义务分类见库存 | 逐义务见库存 |
| [TK-14](../architecture/task-kernel/recovery-and-validation.md)（第 135 行） | 逐义务分类见库存 | 逐义务见库存 |
| [TK-15](../architecture/task-kernel/recovery-and-validation.md)（第 136 行） | 逐义务分类见库存 | 逐义务见库存 |
| [TK-16](../architecture/task-kernel/recovery-and-validation.md)（第 137 行） | 逐义务分类见库存 | 逐义务见库存 |
| [TK-17](../architecture/task-kernel/recovery-and-validation.md)（第 138 行） | 逐义务分类见库存 | 逐义务见库存 |
| [TK-17a](../architecture/task-kernel/recovery-and-validation.md)（第 139 行） | 逐义务分类见库存 | 逐义务见库存 |
| [TK-18](../architecture/task-kernel/recovery-and-validation.md)（第 140 行） | 逐义务分类见库存 | 逐义务见库存 |
| [TK-19](../architecture/task-kernel/recovery-and-validation.md)（第 141 行） | 逐义务分类见库存 | 逐义务见库存 |
| [TK-20](../architecture/task-kernel/recovery-and-validation.md)（第 142 行） | 逐义务分类见库存 | 逐义务见库存 |
| [TK-21](../architecture/task-kernel/recovery-and-validation.md)（第 143 行） | 逐义务分类见库存 | 逐义务见库存 |
| [TK-22](../architecture/task-kernel/recovery-and-validation.md)（第 144 行） | 逐义务分类见库存 | 逐义务见库存 |
| [TK-23](../architecture/task-kernel/recovery-and-validation.md)（第 145 行） | 逐义务分类见库存 | 逐义务见库存 |
| [TK-24](../architecture/task-kernel/recovery-and-validation.md)（第 146 行） | 逐义务分类见库存 | 逐义务见库存 |
| [TK-25](../architecture/task-kernel/recovery-and-validation.md)（第 147 行） | 逐义务分类见库存 | 逐义务见库存 |
| [CE-01](../architecture/capability-and-execution/validation.md)（第 49 行） | 逐义务分类见库存 | 逐义务见库存 |
| [CE-02](../architecture/capability-and-execution/validation.md)（第 50 行） | 逐义务分类见库存 | 逐义务见库存 |
| [CE-03](../architecture/capability-and-execution/validation.md)（第 51 行） | 逐义务分类见库存 | 逐义务见库存 |
| [CE-04](../architecture/capability-and-execution/validation.md)（第 52 行） | 逐义务分类见库存 | 逐义务见库存 |
| [CE-05](../architecture/capability-and-execution/validation.md)（第 53 行） | 逐义务分类见库存 | 逐义务见库存 |
| [CE-06](../architecture/capability-and-execution/validation.md)（第 54 行） | 逐义务分类见库存 | 逐义务见库存 |
| [CE-07](../architecture/capability-and-execution/validation.md)（第 55 行） | 逐义务分类见库存 | 逐义务见库存 |
| [CE-08](../architecture/capability-and-execution/validation.md)（第 56 行） | 逐义务分类见库存 | 逐义务见库存 |
| [CE-09](../architecture/capability-and-execution/validation.md)（第 57 行） | 逐义务分类见库存 | 逐义务见库存 |
| [CE-10](../architecture/capability-and-execution/validation.md)（第 58 行） | 逐义务分类见库存 | 逐义务见库存 |
| [CE-11](../architecture/capability-and-execution/validation.md)（第 59 行） | 逐义务分类见库存 | 逐义务见库存 |
| [CE-12](../architecture/capability-and-execution/validation.md)（第 60 行） | 逐义务分类见库存 | 逐义务见库存 |
| [CE-13](../architecture/capability-and-execution/validation.md)（第 61 行） | 逐义务分类见库存 | 逐义务见库存 |
| [CE-14](../architecture/capability-and-execution/validation.md)（第 62 行） | 逐义务分类见库存 | 逐义务见库存 |
| [CE-15](../architecture/capability-and-execution/validation.md)（第 63 行） | 逐义务分类见库存 | 逐义务见库存 |
| [CE-16](../architecture/capability-and-execution/validation.md)（第 64 行） | 逐义务分类见库存 | 逐义务见库存 |
| [CE-17](../architecture/capability-and-execution/validation.md)（第 65 行） | 逐义务分类见库存 | 逐义务见库存 |
| [CE-18](../architecture/capability-and-execution/validation.md)（第 66 行） | 逐义务分类见库存 | 逐义务见库存 |
| [CE-19](../architecture/capability-and-execution/validation.md)（第 67 行） | 逐义务分类见库存 | 逐义务见库存 |
| [CE-20](../architecture/capability-and-execution/validation.md)（第 68 行） | 逐义务分类见库存 | 逐义务见库存 |
| [CE-21](../architecture/capability-and-execution/validation.md)（第 69 行） | 逐义务分类见库存 | 逐义务见库存 |
| [CE-22](../architecture/capability-and-execution/validation.md)（第 70 行） | 逐义务分类见库存 | 逐义务见库存 |
| [CE-23](../architecture/capability-and-execution/validation.md)（第 71 行） | 逐义务分类见库存 | 逐义务见库存 |
| [CE-24](../architecture/capability-and-execution/validation.md)（第 72 行） | 逐义务分类见库存 | 逐义务见库存 |
| [CE-P1](../architecture/capability-and-execution/validation.md)（第 93 行） | 逐义务分类见库存 | 逐义务见库存 |
| [CE-P2](../architecture/capability-and-execution/validation.md)（第 94 行） | 逐义务分类见库存 | 逐义务见库存 |
| [CE-25](../architecture/capability-and-execution/validation.md)（第 101 行） | 逐义务分类见库存 | 逐义务见库存 |
| [CE-26](../architecture/capability-and-execution/validation.md)（第 102 行） | 逐义务分类见库存 | 逐义务见库存 |
| [CE-27](../architecture/capability-and-execution/validation.md)（第 103 行） | 逐义务分类见库存 | 逐义务见库存 |
| [CE-28](../architecture/capability-and-execution/validation.md)（第 104 行） | 逐义务分类见库存 | 逐义务见库存 |
| [CE-29](../architecture/capability-and-execution/validation.md)（第 105 行） | 逐义务分类见库存 | 逐义务见库存 |
| [CO-01](../architecture/agent-coordination/validation.md)（第 29 行） | 逐义务分类见库存 | 逐义务见库存 |
| [CO-02](../architecture/agent-coordination/validation.md)（第 30 行） | 逐义务分类见库存 | 逐义务见库存 |
| [CO-03](../architecture/agent-coordination/validation.md)（第 31 行） | 逐义务分类见库存 | 逐义务见库存 |
| [CO-04](../architecture/agent-coordination/validation.md)（第 32 行） | 逐义务分类见库存 | 逐义务见库存 |
| [CO-05](../architecture/agent-coordination/validation.md)（第 33 行） | 逐义务分类见库存 | 逐义务见库存 |
| [CO-06](../architecture/agent-coordination/validation.md)（第 34 行） | 逐义务分类见库存 | 逐义务见库存 |
| [CO-07](../architecture/agent-coordination/validation.md)（第 35 行） | 逐义务分类见库存 | 逐义务见库存 |
| [CO-08](../architecture/agent-coordination/validation.md)（第 36 行） | 逐义务分类见库存 | 逐义务见库存 |
| [CO-09](../architecture/agent-coordination/validation.md)（第 37 行） | 逐义务分类见库存 | 逐义务见库存 |
| [CO-10](../architecture/agent-coordination/validation.md)（第 38 行） | 逐义务分类见库存 | 逐义务见库存 |
| [CO-11](../architecture/agent-coordination/validation.md)（第 39 行） | 逐义务分类见库存 | 逐义务见库存 |
| [CO-12](../architecture/agent-coordination/validation.md)（第 40 行） | 逐义务分类见库存 | 逐义务见库存 |
| [CO-18](../architecture/agent-coordination/validation.md)（第 41 行） | 逐义务分类见库存 | 逐义务见库存 |
| [CO-20](../architecture/agent-coordination/validation.md)（第 42 行） | 逐义务分类见库存 | 逐义务见库存 |
| [CO-21](../architecture/agent-coordination/validation.md)（第 43 行） | 逐义务分类见库存 | 逐义务见库存 |
| [CO-22](../architecture/agent-coordination/validation.md)（第 44 行） | 逐义务分类见库存 | 逐义务见库存 |
| [CO-23](../architecture/agent-coordination/validation.md)（第 45 行） | 逐义务分类见库存 | 逐义务见库存 |
| [CO-24](../architecture/agent-coordination/validation.md)（第 46 行） | 逐义务分类见库存 | 逐义务见库存 |
| [CO-25](../architecture/agent-coordination/validation.md)（第 47 行） | 逐义务分类见库存 | 逐义务见库存 |
| [CO-26](../architecture/agent-coordination/validation.md)（第 48 行） | 逐义务分类见库存 | 逐义务见库存 |
| [CO-27](../architecture/agent-coordination/validation.md)（第 49 行） | 逐义务分类见库存 | 逐义务见库存 |
| [CO-P1](../architecture/agent-coordination/validation.md)（第 69 行） | 逐义务分类见库存 | 逐义务见库存 |
| [CO-P2](../architecture/agent-coordination/validation.md)（第 71 行） | 逐义务分类见库存 | 逐义务见库存 |
| [BS-01](../architecture/brain-system/validation.md)（第 39 行） | 逐义务分类见库存 | 逐义务见库存 |
| [BS-02](../architecture/brain-system/validation.md)（第 40 行） | 逐义务分类见库存 | 逐义务见库存 |
| [BS-03](../architecture/brain-system/validation.md)（第 41 行） | 逐义务分类见库存 | 逐义务见库存 |
| [BS-04](../architecture/brain-system/validation.md)（第 42 行） | 逐义务分类见库存 | 逐义务见库存 |
| [BS-05](../architecture/brain-system/validation.md)（第 43 行） | 逐义务分类见库存 | 逐义务见库存 |
| [BS-06](../architecture/brain-system/validation.md)（第 44 行） | 逐义务分类见库存 | 逐义务见库存 |
| [BS-07](../architecture/brain-system/validation.md)（第 45 行） | 逐义务分类见库存 | 逐义务见库存 |
| [BS-08](../architecture/brain-system/validation.md)（第 46 行） | 逐义务分类见库存 | 逐义务见库存 |
| [BS-09](../architecture/brain-system/validation.md)（第 47 行） | 逐义务分类见库存 | 逐义务见库存 |
| [BS-10](../architecture/brain-system/validation.md)（第 48 行） | 逐义务分类见库存 | 逐义务见库存 |
| [BS-11](../architecture/brain-system/validation.md)（第 49 行） | 逐义务分类见库存 | 逐义务见库存 |
| [BS-12](../architecture/brain-system/validation.md)（第 50 行） | 逐义务分类见库存 | 逐义务见库存 |
| [BS-13](../architecture/brain-system/validation.md)（第 51 行） | 逐义务分类见库存 | 逐义务见库存 |
| [BS-14](../architecture/brain-system/validation.md)（第 52 行） | 逐义务分类见库存 | 逐义务见库存 |
| [BS-15](../architecture/brain-system/validation.md)（第 53 行） | 逐义务分类见库存 | 逐义务见库存 |
| [BS-16](../architecture/brain-system/validation.md)（第 54 行） | 逐义务分类见库存 | 逐义务见库存 |
| [BS-17](../architecture/brain-system/validation.md)（第 55 行） | 逐义务分类见库存 | 逐义务见库存 |
| [BS-18](../architecture/brain-system/validation.md)（第 56 行） | 逐义务分类见库存 | 逐义务见库存 |
| [BS-19](../architecture/brain-system/validation.md)（第 57 行） | 逐义务分类见库存 | 逐义务见库存 |
| [BS-20](../architecture/brain-system/validation.md)（第 58 行） | 逐义务分类见库存 | 逐义务见库存 |
| [BS-21](../architecture/brain-system/validation.md)（第 59 行） | 逐义务分类见库存 | 逐义务见库存 |
| [BS-22](../architecture/brain-system/validation.md)（第 60 行） | 逐义务分类见库存 | 逐义务见库存 |
| [BS-23](../architecture/brain-system/validation.md)（第 61 行） | 逐义务分类见库存 | 逐义务见库存 |
| [BS-24](../architecture/brain-system/validation.md)（第 62 行） | 逐义务分类见库存 | 逐义务见库存 |
| [BS-25](../architecture/brain-system/validation.md)（第 63 行） | 逐义务分类见库存 | 逐义务见库存 |
| [BS-26](../architecture/brain-system/validation.md)（第 64 行） | 逐义务分类见库存 | 逐义务见库存 |
| [BS-27](../architecture/brain-system/validation.md)（第 65 行） | 逐义务分类见库存 | 逐义务见库存 |
| [BS-28](../architecture/brain-system/validation.md)（第 86 行） | 逐义务分类见库存 | 逐义务见库存 |
| [BS-29](../architecture/brain-system/validation.md)（第 87 行） | 逐义务分类见库存 | 逐义务见库存 |
| [BS-30](../architecture/brain-system/validation.md)（第 88 行） | 逐义务分类见库存 | 逐义务见库存 |
| [BS-31](../architecture/brain-system/validation.md)（第 89 行） | 逐义务分类见库存 | 逐义务见库存 |
| [BS-32](../architecture/brain-system/validation.md)（第 90 行） | 逐义务分类见库存 | 逐义务见库存 |

## lifecycle：机制

[组内结果与模型边界](../../formal/mechanisms/lifecycle/README.md)

| ID | 机制 | 库存覆盖声明 |
| --- | --- | --- |
| ER-lock | 精确制品与依赖闭包 | abstract-partial |
| ER-command | 持久管理命令与分阶段激活 | abstract-partial |
| ER-reference | 使用前登记固定引用 | abstract-partial |
| ER-reclaim | 关闭新引用、排空、计划先存、可恢复回收 | abstract-partial |
| ER-withdraw | 普通排空与安全撤回分离 | abstract-partial |
| ER-rollback | 新命令回退代码，历史事实不回退 | abstract-partial |
| ER-isolate | 宿主身份映射与资源隔离资格 | abstract-partial |
| ER-lifecycle | 依赖DAG与按行为就绪 | abstract-partial |
| ER-bounded-recovery | 持久有界恢复与公平扫描 | abstract-partial |
| ER-backup | 完整提交域恢复与旧写者隔离 | abstract-partial |
| ER-release | 逐目标批准新鲜度与部分生效 | abstract-partial |
| UI-input | 持久输入接管与父子操作映射 | abstract-partial |
| UI-content | 完整快照与条件增量 | abstract-partial |
| UI-intent | 显式意图、待决展示与旧响应隔离 | abstract-partial |
| UI-result | 正式结果与展示独立 | abstract-partial |
| UI-directory | 固定权威目录与显式订阅 | abstract-partial |
| UI-preview | 可信预览与精确批准绑定 | abstract-partial |
| UI-session | 会话代次及当前用户约束 | abstract-partial |
| UI-render | 非受信内容受限呈现 | abstract-partial |
| OI-observe | 领域事实、观测与独立评估证据分离 | abstract-partial |
| OI-freeze | 冻结候选/计划/保留集 | abstract-partial |
| OI-denominator | 固定分母、未知不算成功 | abstract-partial |
| OI-run | 持久run与测量/结算分离 | abstract-partial |
| OI-source | 来源关闭与派生/发布提交串行 | abstract-partial |
| OI-approval | 精确候选/报告批准及新鲜提交 | abstract-partial |
| OI-rollout | 有限批次、持久调用资格与原键核对 | abstract-partial |
| OI-history | 撤回/停用/回退与历史记录分离 | abstract-partial |
| OI-governance | 监测有界与新批次显式批准 | abstract-partial |
| OI-optin | 成功任务记忆提取与候选发布分离 | abstract-partial |

### lifecycle：原编号用例

下列分类针对抽象义务；真实实现的整条运行验收均未执行。每行完整刺激／判据及未覆盖细项在 JSON 库存中保留。

| 用例与原文 | 证据分类 | 抽象范围／剩余边界 |
| --- | --- | --- |
| [ER-01](../architecture/extensions-and-runtime/validation.md)（第 71 行） | model-property | abstract-partial |
| [ER-02](../architecture/extensions-and-runtime/validation.md)（第 72 行） | model-property | abstract-partial |
| [ER-03](../architecture/extensions-and-runtime/validation.md)（第 73 行） | runtime-required | not-formally-covered |
| [ER-04](../architecture/extensions-and-runtime/validation.md)（第 74 行） | runtime-required | not-formally-covered |
| [ER-05](../architecture/extensions-and-runtime/validation.md)（第 75 行） | model-property | abstract-partial |
| [ER-06](../architecture/extensions-and-runtime/validation.md)（第 76 行） | model-property | abstract-partial |
| [ER-07](../architecture/extensions-and-runtime/validation.md)（第 77 行） | model-property | abstract-partial |
| [ER-08](../architecture/extensions-and-runtime/validation.md)（第 78 行） | model-property | abstract-partial |
| [ER-09](../architecture/extensions-and-runtime/validation.md)（第 79 行） | model-property | abstract-partial |
| [ER-10](../architecture/extensions-and-runtime/validation.md)（第 80 行） | model-property | abstract-partial |
| [ER-11](../architecture/extensions-and-runtime/validation.md)（第 81 行） | model-property | abstract-partial |
| [ER-12](../architecture/extensions-and-runtime/validation.md)（第 82 行） | model-property | abstract-partial |
| [ER-13](../architecture/extensions-and-runtime/validation.md)（第 83 行） | model-property | abstract-partial |
| [ER-14](../architecture/extensions-and-runtime/validation.md)（第 84 行） | model-property | abstract-partial |
| [ER-15](../architecture/extensions-and-runtime/validation.md)（第 85 行） | model-property | abstract-partial |
| [ER-16](../architecture/extensions-and-runtime/validation.md)（第 86 行） | model-property | abstract-partial |
| [ER-17](../architecture/extensions-and-runtime/validation.md)（第 87 行） | model-property | abstract-partial |
| [ER-18](../architecture/extensions-and-runtime/validation.md)（第 88 行） | model-property | abstract-partial |
| [ER-19](../architecture/extensions-and-runtime/validation.md)（第 89 行） | model-property | abstract-partial |
| [ER-20](../architecture/extensions-and-runtime/validation.md)（第 90 行） | model-property | abstract-partial |
| [ER-21](../architecture/extensions-and-runtime/validation.md)（第 91 行） | model-property | abstract-partial |
| [ER-22](../architecture/extensions-and-runtime/validation.md)（第 92 行） | model-property | abstract-partial |
| [ER-23](../architecture/extensions-and-runtime/validation.md)（第 93 行） | model-property | abstract-partial |
| [ER-24](../architecture/extensions-and-runtime/validation.md)（第 94 行） | model-property | abstract-partial |
| [ER-25](../architecture/extensions-and-runtime/validation.md)（第 95 行） | model-property | abstract-partial |
| [ER-26](../architecture/extensions-and-runtime/validation.md)（第 96 行） | runtime-required | not-formally-covered |
| [ER-27](../architecture/extensions-and-runtime/validation.md)（第 97 行） | model-property | abstract-partial |
| [ER-28](../architecture/extensions-and-runtime/validation.md)（第 98 行） | model-property | abstract-partial |
| [ER-29](../architecture/extensions-and-runtime/validation.md)（第 99 行） | model-property | abstract-partial |
| [ER-P1](../architecture/extensions-and-runtime/validation.md)（第 110 行） | model-property | abstract-partial |
| [ER-P2](../architecture/extensions-and-runtime/validation.md)（第 111 行） | model-property | abstract-partial |
| [ER-P3](../architecture/extensions-and-runtime/validation.md)（第 112 行） | static-only | policy-exclusion-only |
| [ER-30](../architecture/extensions-and-runtime/validation.md)（第 118 行） | model-property | abstract-partial |
| [ER-31](../architecture/extensions-and-runtime/validation.md)（第 119 行） | model-property | abstract-partial |
| [ER-32](../architecture/extensions-and-runtime/validation.md)（第 120 行） | model-property | abstract-partial |
| [ER-33](../architecture/extensions-and-runtime/validation.md)（第 121 行） | model-property | abstract-partial |
| [UI-01](../architecture/application-and-interaction/validation.md)（第 12 行） | model-property | abstract-partial |
| [UI-02](../architecture/application-and-interaction/validation.md)（第 13 行） | model-property | abstract-partial |
| [UI-03](../architecture/application-and-interaction/validation.md)（第 14 行） | model-property | abstract-partial |
| [UI-04](../architecture/application-and-interaction/validation.md)（第 15 行） | model-property | abstract-partial |
| [UI-05](../architecture/application-and-interaction/validation.md)（第 16 行） | model-property | abstract-partial |
| [UI-06](../architecture/application-and-interaction/validation.md)（第 17 行） | model-property | abstract-partial |
| [UI-07](../architecture/application-and-interaction/validation.md)（第 18 行） | model-property | abstract-partial |
| [UI-08](../architecture/application-and-interaction/validation.md)（第 19 行） | model-property | abstract-partial |
| [UI-09](../architecture/application-and-interaction/validation.md)（第 20 行） | model-property | abstract-partial |
| [UI-10](../architecture/application-and-interaction/validation.md)（第 21 行） | model-property | abstract-partial |
| [UI-11](../architecture/application-and-interaction/validation.md)（第 22 行） | model-property | abstract-partial |
| [UI-12](../architecture/application-and-interaction/validation.md)（第 23 行） | model-property | abstract-partial |
| [UI-13](../architecture/application-and-interaction/validation.md)（第 24 行） | model-property | abstract-partial |
| [UI-14](../architecture/application-and-interaction/validation.md)（第 25 行） | model-property | abstract-partial |
| [UI-15](../architecture/application-and-interaction/validation.md)（第 26 行） | model-property | abstract-partial |
| [UI-16](../architecture/application-and-interaction/validation.md)（第 27 行） | model-property | abstract-partial |
| [UI-17](../architecture/application-and-interaction/validation.md)（第 28 行） | model-property | abstract-partial |
| [UI-18](../architecture/application-and-interaction/validation.md)（第 29 行） | runtime-required | not-formally-covered |
| [UI-19](../architecture/application-and-interaction/validation.md)（第 30 行） | model-property | abstract-partial |
| [UI-20](../architecture/application-and-interaction/validation.md)（第 31 行） | model-property | abstract-partial |
| [UI-21](../architecture/application-and-interaction/validation.md)（第 32 行） | runtime-required | not-formally-covered |
| [UI-22](../architecture/application-and-interaction/validation.md)（第 33 行） | model-property | abstract-partial |
| [UI-23](../architecture/application-and-interaction/validation.md)（第 34 行） | model-property | abstract-partial |
| [UI-24](../architecture/application-and-interaction/validation.md)（第 35 行） | model-property | abstract-partial |
| [UI-25](../architecture/application-and-interaction/validation.md)（第 60 行） | model-property | abstract-partial |
| [UI-26](../architecture/application-and-interaction/validation.md)（第 61 行） | model-property | abstract-partial |
| [UI-27](../architecture/application-and-interaction/validation.md)（第 62 行） | model-property | abstract-partial |
| [UI-28](../architecture/application-and-interaction/validation.md)（第 63 行） | model-property | abstract-partial |
| [UI-29](../architecture/application-and-interaction/validation.md)（第 64 行） | model-property | abstract-partial |
| [UI-30](../architecture/application-and-interaction/validation.md)（第 65 行） | model-property | abstract-partial |
| [UI-31](../architecture/application-and-interaction/validation.md)（第 66 行） | model-property | abstract-partial |
| [OI-01](../architecture/observation-and-improvement/validation.md)（第 49 行） | model-property | abstract-partial |
| [OI-02](../architecture/observation-and-improvement/validation.md)（第 50 行） | model-property | abstract-partial |
| [OI-03](../architecture/observation-and-improvement/validation.md)（第 51 行） | runtime-required | not-formally-covered |
| [OI-04](../architecture/observation-and-improvement/validation.md)（第 52 行） | model-property | abstract-partial |
| [OI-05](../architecture/observation-and-improvement/validation.md)（第 53 行） | runtime-required | not-formally-covered |
| [OI-06](../architecture/observation-and-improvement/validation.md)（第 54 行） | model-property | abstract-partial |
| [OI-07](../architecture/observation-and-improvement/validation.md)（第 55 行） | model-property | abstract-partial |
| [OI-08](../architecture/observation-and-improvement/validation.md)（第 56 行） | model-property | abstract-partial |
| [OI-09](../architecture/observation-and-improvement/validation.md)（第 57 行） | model-property | abstract-partial |
| [OI-10](../architecture/observation-and-improvement/validation.md)（第 58 行） | model-property | abstract-partial |
| [OI-11](../architecture/observation-and-improvement/validation.md)（第 59 行） | model-property | abstract-partial |
| [OI-12](../architecture/observation-and-improvement/validation.md)（第 60 行） | model-property | abstract-partial |
| [OI-13](../architecture/observation-and-improvement/validation.md)（第 61 行） | model-property | abstract-partial |
| [OI-14](../architecture/observation-and-improvement/validation.md)（第 62 行） | model-property | abstract-partial |
| [OI-15](../architecture/observation-and-improvement/validation.md)（第 63 行） | model-property | abstract-partial |
| [OI-16](../architecture/observation-and-improvement/validation.md)（第 64 行） | model-property | abstract-partial |
| [OI-17](../architecture/observation-and-improvement/validation.md)（第 65 行） | model-property | abstract-partial |
| [OI-18](../architecture/observation-and-improvement/validation.md)（第 66 行） | model-property | abstract-partial |
| [OI-19](../architecture/observation-and-improvement/validation.md)（第 67 行） | model-property | abstract-partial |
| [OI-20](../architecture/observation-and-improvement/validation.md)（第 68 行） | model-property | abstract-partial |
| [OI-20a](../architecture/observation-and-improvement/validation.md)（第 69 行） | model-property | abstract-partial |
| [OI-21](../architecture/observation-and-improvement/validation.md)（第 70 行） | model-property | abstract-partial |
| [OI-22](../architecture/observation-and-improvement/validation.md)（第 71 行） | model-property | abstract-partial |
| [OI-23](../architecture/observation-and-improvement/validation.md)（第 72 行） | model-property | abstract-partial |
| [OI-24](../architecture/observation-and-improvement/validation.md)（第 73 行） | model-property | abstract-partial |
| [OI-P1](../architecture/observation-and-improvement/validation.md)（第 89 行） | model-property | abstract-partial |
| [OI-P2](../architecture/observation-and-improvement/validation.md)（第 90 行） | model-property | abstract-partial |
| [MS-P3](../architecture/observation-and-improvement/validation.md)（第 91 行） | model-property | abstract-partial |
| [OI-P3](../architecture/observation-and-improvement/validation.md)（第 92 行） | static-only | policy-exclusion-only |
| [OI-25](../architecture/observation-and-improvement/validation.md)（第 98 行） | model-property | abstract-partial |
| [OI-26](../architecture/observation-and-improvement/validation.md)（第 99 行） | model-property | abstract-partial |
| [OI-27](../architecture/observation-and-improvement/validation.md)（第 100 行） | model-property | abstract-partial |

## trust：机制

[组内结果与模型边界](../../formal/mechanisms/trust/README.md)

| ID | 机制 | 库存覆盖声明 |
| --- | --- | --- |
| TR-01 | 受信身份、用户、行动者和处理者绑定 | partial-model |
| TR-02 | 单父链委派范围只能收缩 | partial-model |
| TR-03 | 预检缓存与实际消费/披露分离 | partial-model |
| TR-04 | 单次占用永久绑定原操作 | partial-model |
| TR-05 | 撤销、历史事实和新行动分别处理 | partial-model |
| TR-06 | 离线许可、可信时间与防回滚 | partial-model |
| TR-07 | 授权连续恢复 | partial-model |
| TR-08 | 受限恢复引导与本人管理分离 | partial-model |
| TR-09 | 受信确认与用户所见范围绑定 | partial-model |
| TR-10 | 固定权威、事务记录及最小审计保留 | partial-reused（跨组通用规则；原组状态保留） |
| TR-11 | 带类型/版本/时间/范围的不可变记忆 | partial-model |
| TR-12 | 完整来源闭包与自依赖拒绝 | partial-model |
| TR-13 | 候选索引回权威复核 | partial-model |
| TR-14 | 索引水位补漏与确定性有界排序 | partial-model |
| TR-15 | 记忆命令CAS/原决定/关闭竞争 | partial-model |
| TR-16 | 墓碑不可复活 | partial-model |
| TR-17 | 派生失效不等待反向清理 | partial-model |
| TR-18 | 逻辑禁用和物理清理独立 | partial-model |
| TR-19 | 控制删除不依赖旧正文继续可读 | partial-model |
| TR-20 | 获准只读视图与领域写权威分离 | partial-model |
| TR-21 | 快照暂存/连续补齐/可见代次切换 | partial-model |
| TR-22 | 在线视图恢复不产生永久使用资格 | partial-model |
| TR-23 | 索引重建和变化日志保留 | partial-model |
| TR-24 | 有限读取单元和冻结once结果 | partial-model |
| TR-25 | 独立opt-in提取及预算 | partial-model |
| TR-26 | 数据驻留与派生产物限制继承 | partial-model |
| TR-27 | 字节、来源、授权和业务采用分别裁决 | partial-model |
| TR-28 | 先有限保留再发布领域引用 | partial-model |
| TR-29 | 持有者登记与源关闭串行竞争 | partial-model |
| TR-30 | 清理正文后最小依据防复活 | partial-reused（跨组通用规则；原组状态保留） |
| TR-31 | 过载和有限恢复不丢既有责任 | partial-reused（跨组通用规则；原组状态保留） |
| TR-32 | 协议版本、规范化器和未知类型拒绝 | partial-reused（跨组通用规则；原组状态保留） |

### trust：原编号用例

下列分类针对抽象义务；真实实现的整条运行验收均未执行。每行完整刺激／判据及未覆盖细项在 JSON 库存中保留。

| 用例与原文 | 证据分类 | 抽象范围／剩余边界 |
| --- | --- | --- |
| [IA-01](../architecture/identity-and-authorization/validation.md)（第 30 行） | model-property | 不同身份类别不能获新消费/披露 |
| [IA-02](../architecture/identity-and-authorization/validation.md)（第 31 行） | model-property | otherActor和otherProcessor与owner分离 |
| [IA-03](../architecture/identity-and-authorization/validation.md)（第 32 行） | runtime-required | none |
| [IA-04](../architecture/identity-and-authorization/validation.md)（第 33 行） | model-property | Evaluate不消费、BeginUse唯一绑定；完整确认绑定一次签发 |
| [IA-05](../architecture/identity-and-authorization/validation.md)（第 34 行） | model-property | 不同用途和主体不能沿旧资格消费 |
| [IA-06](../architecture/identity-and-authorization/validation.md)（第 35 行） | model-property | 集合范围只能收缩、撤销后旧缓存不能使用 |
| [IA-07](../architecture/identity-and-authorization/validation.md)（第 36 行） | model-property | 两个原操作竞争once、相同操作重入不重分配 |
| [IA-08](../architecture/identity-and-authorization/validation.md)（第 37 行） | model-property | 旧预检不替代当前判定、到期不再新消费 |
| [IA-09](../architecture/identity-and-authorization/validation.md)（第 38 行） | runtime-required | none |
| [IA-10](../architecture/identity-and-authorization/validation.md)（第 39 行） | model-property | 撤销与消费交错后新消费受限 |
| [IA-11](../architecture/identity-and-authorization/validation.md)（第 40 行） | model-property | 撤权后接收历史事实、占用不退回 |
| [IA-12](../architecture/identity-and-authorization/validation.md)（第 41 行） | model-property | 当前权限再次校验披露、旧缓存不能披露 |
| [IA-13](../architecture/identity-and-authorization/validation.md)（第 42 行） | model-property | 保守区间全域在有效期内；缺时间锚/防回滚依据即不准使用 |
| [IA-14](../architecture/identity-and-authorization/validation.md)（第 43 行） | model-property | 最短截止越过后不新增使用；时间/控制/预算等条件合取 |
| [IA-15](../architecture/identity-and-authorization/validation.md)（第 44 行） | model-property | 完整页和覆盖区间、旧轮/owner增量拒绝 |
| [IA-16](../architecture/identity-and-authorization/validation.md)（第 45 行） | runtime-required | none |
| [IA-17](../architecture/identity-and-authorization/validation.md)（第 46 行） | runtime-required | none |
| [IA-18](../architecture/identity-and-authorization/validation.md)（第 47 行） | model-property | 用途收缩、全部真实输入闭包不得遗漏 |
| [IA-19](../architecture/identity-and-authorization/validation.md)（第 48 行） | runtime-required | none |
| [IA-20](../architecture/identity-and-authorization/validation.md)（第 49 行） | model-property | 抽象已关闭原命令不能迟到提交 |
| [IA-21](../architecture/identity-and-authorization/validation.md)（第 62 行） | model-property | 恢复白名单不能换正文/新行动/签发/确认/其他命令查询；绑定/host/子集/新鲜度必要 |
| [IA-22](../architecture/identity-and-authorization/validation.md)（第 63 行） | model-property | 完整绑定不变、不同proof绑定拒绝、一次签发与已消费重入 |
| [IA-23](../architecture/identity-and-authorization/validation.md)（第 64 行） | model-property | 切点连续覆盖与旧owner增量拒绝 |
| [IA-24](../architecture/identity-and-authorization/validation.md)（第 65 行） | runtime-required | none |
| [IA-25](../architecture/identity-and-authorization/validation.md)（第 66 行） | model-property | 逻辑关闭不能当物理完成 |
| [IA-26](../architecture/identity-and-authorization/validation.md)（第 67 行） | runtime-required | none |
| [MS-01](../architecture/memory-system/validation.md)（第 42 行） | model-property | 获准用途和记录修改的抽象规则 |
| [MS-02](../architecture/memory-system/validation.md)（第 43 行） | runtime-required | none |
| [MS-03](../architecture/memory-system/validation.md)（第 44 行） | model-property | 未批准用途不允许抽象使用/披露 |
| [MS-04](../architecture/memory-system/validation.md)（第 45 行） | model-property | 实际private/public输入闭包不能由声称引用缩小 |
| [MS-05](../architecture/memory-system/validation.md)（第 46 行） | model-property | 不同用户不得抽象使用/披露 |
| [MS-06](../architecture/memory-system/validation.md)（第 47 行） | runtime-required | none |
| [MS-07](../architecture/memory-system/validation.md)（第 48 行） | model-property | W..S逐步补扫、删除抑制旧候选、当前修订/许可复核；缺覆盖标partial |
| [MS-08](../architecture/memory-system/validation.md)（第 49 行） | model-property | 两命令CAS竞争仅一条成功；原结果固定 |
| [MS-09](../architecture/memory-system/validation.md)（第 50 行） | model-property | 抽象提交与结果状态共同变化 |
| [MS-10](../architecture/memory-system/validation.md)（第 51 行） | model-property | 关闭早到阻止迟到提交，提交在先保持结果 |
| [MS-11](../architecture/memory-system/validation.md)（第 52 行） | model-property | once固定占用、processor不同被拒 |
| [MS-12](../architecture/memory-system/validation.md)（第 53 行） | model-property | 来源/版本变化后旧缓存拒绝新使用 |
| [MS-13](../architecture/memory-system/validation.md)（第 54 行） | model-property | 真实依赖关闭即阻止新使用，与清理完成无关 |
| [MS-14](../architecture/memory-system/validation.md)（第 55 行） | model-property | 墓碑阻止旧修改，旧视图不能代替当前版本 |
| [MS-15](../architecture/memory-system/validation.md)（第 56 行） | model-property | 逻辑关闭与平台回执/物理完成分离 |
| [MS-16](../architecture/memory-system/validation.md)（第 57 行） | model-property | 未完整页不可见，R..H连续覆盖后发布 |
| [MS-17](../architecture/memory-system/validation.md)（第 58 行） | model-property | 空变化区间可覆盖、乱序不推进、旧owner增量拒绝 |
| [MS-18](../architecture/memory-system/validation.md)（第 59 行） | model-property | 权限与数据版本独立变化时读取重核、清理另报 |
| [MS-19](../architecture/memory-system/validation.md)（第 60 行） | model-property | 到期阻止新使用的离散抽象 |
| [MS-20](../architecture/memory-system/validation.md)（第 61 行） | model-property | 派生不丢来源，未获准用途拒绝 |
| [MS-21](../architecture/memory-system/validation.md)（第 62 行） | runtime-required | none |
| [MS-22](../architecture/memory-system/validation.md)（第 63 行） | runtime-required | none |
| [MS-23](../architecture/memory-system/validation.md)（第 64 行） | model-property | 四类消费者共同约束日志回收；先失效并登记重建责任，追平后恢复索引；索引内容水位同步推进 |
| [MS-24](../architecture/memory-system/validation.md)（第 65 行） | model-property | 每个原命令结果固定且不会二次应用 |
| [MS-25](../architecture/memory-system/validation.md)（第 66 行） | runtime-required | none |
| [MS-26](../architecture/memory-system/validation.md)（第 67 行） | model-property | 实际读旧目标版本则自依赖拒绝，独立来源可保存 |
| [MS-27](../architecture/memory-system/validation.md)（第 68 行） | model-property | 关闭不以旧正文可用为前提 |
| [MS-28](../architecture/memory-system/validation.md)（第 87 行） | model-property | 连续页/轮次owner、完整闭包与CAS抽象 |
| [MS-29](../architecture/memory-system/validation.md)（第 88 行） | model-property | 登记/关闭串行覆盖，未登记不能发布 |
| [MS-30](../architecture/memory-system/validation.md)（第 89 行） | model-property | 无物理回执不报告清理完成 |
| [MS-31](../architecture/memory-system/validation.md)（第 90 行） | model-property | 成功+optin+当前许可触发一次，使用独立预算 |
| [MS-32](../architecture/memory-system/validation.md)（第 91 行） | model-property | 缓存候选遇关闭/撤权不新触发，原任务预算不变 |
| [MS-33](../architecture/memory-system/validation.md)（第 92 行） | model-property | 自依赖/来源遗漏拒绝，清理不冒称已完成 |

## 跨组通用规则复用

以下映射补充原组未形式化的通用子规则，保留原库存与未建模边界，不增加检查次数。逐项性质、检查 ID 和假设见 [跨组映射](../../formal/mechanisms/cross-group-mappings.json)。

| 机制 | 可复用的局部规则 | 仍未覆盖 |
| --- | --- | --- |
| TR-10 | 固定用户权威与用户级原操作键的通用关系；一次提交的真实状态和调用方已知结果分开；只读未见不产生拒绝事实；持久接纳保存后续责任，只有有持久来源的确认才可卸责 | 授权表全部唯一键、确认/签发/占用/修订/审计的完整联合事务尚未组合证明；完整 command_id/use-key/规范意图及跨用户缓存键未在这些通用模型中展开；旧账本封闭、敏感审计字段最小化和全部保留截止仍未形式化 |
| TR-30 | 正文清除后保留最小原身份记录；删除该记录前先关闭旧入口；旧重投不能把已接纳身份重新创建 | 跨端 ledger/identity 退休及其关闭尾部恢复未建模；部分备份恢复、旧 owner 隔离与新安装身份分配未建模；清理合法保留依据和来源连接器/提取资格撤销不在该切片内 |
| TR-31 | 用户额度按用户及工作类别汇总，多连接不增加份额，已接纳责任不被腾空间淘汰；既有工作具有独立收尾/控制份额；逐工作弱公平作为前提时有限已接纳集合可排空；交接发送有有限持久额度，耗尽保存 gap 和未决责任，迟到合法确认仍可收尾 | 记忆后台工作完整领取/租约/取消、槽位释放及 blocked 后原责任重试尚未组合；真实字节/期限/磁盘资源、多用户动态持续到达、指数退避/抖动与一次通知去重尚未建模；安全的额度上界不等于任意部署的公平性或尾延迟保证 |
| TR-32 | 已知可选扩展的数据仍须合法，声明 required 不能降级，未知必要扩展拒绝；现行注册/Schema/固定消息及非法样例已完成结构验证 | 协议命题针对扩展 required 规则，不能直接证明所有未知动作类型/版本、未知资源约束、obligations 的运行时拒绝；规范化器版本锁定、JSON/参数语义完整等价、各领域错误映射和跨实现版本协商未整体形式化；静态用例通过不代表全部合法/非法输入已穷尽 |
| CTL-25 | 用户维度的固定有限接纳额度跨连接汇总，已接纳工作在 queued/done 间守恒；ordinary/cleanup/control 份额隔离；逐工作服务弱公平作为外部假设时已接纳集合最终排空 | 没有实现调度选择算法或证明真实调度公平；固定六项工作不能代表无限动态负载；字节/成本/墙钟期限多维额度、磁盘满与模型执行/外部调用的组合均未建模；隔离侧信道、真实效果及服务容量不因该通用调度切片而得到证明 |

## 内容专题无编号场景

CP-LOCAL 编号只用于本次库存，不是新架构规范 ID。

| 本地索引 | 覆盖范围 |
| --- | --- |
| CP-LOCAL-01 | 发布有字节/保留/登记/来源条件 |
| CP-LOCAL-02 | 先保留后发布的抽象顺序 |
| CP-LOCAL-03 | 关闭后旧缓存不新使用 |
| CP-LOCAL-04 | 关闭与登记串行、清理另证 |
| CP-LOCAL-05 | 版本变化后缓存不代替权威 |
| CP-LOCAL-06 | 异主体不能使用、旧命令不复活 |
