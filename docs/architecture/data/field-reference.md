# 核心对象字段字典

本文件由 core.schema.json 生成，保持字段、类型、必填性和枚举同源。它定义逻辑值与读视图，不等同于数据库 DDL。模块内部完整记录另见[模块字段附录](module-records.md)，包括 Snapshot、Intent、Attempt、记忆、预算使用、协作、安装、评测和生产目录。对象归责及关联见[整体数据设计](README.md)。

所有持久记录的物理封装必须有受信 tenant/owner、记录时间与格式版本；可变头有业务 revision。未在读视图中重复的作用域由受信外层继承，不能从正文任意指定。跨域 ObjectRef 明确带租户。Requirement、BudgetBalance 等嵌入样例不是无作用域的独立资源。

“条件”须结合下方状态规则；未出现字段表示未知或不适用，不能默认为已通过/零费用。额外字段拒绝，新增合同要换 profile。数组有上限；可增长集合使用关系表与分页，不靠截断满足 Schema。

ID 格式为类型前缀加32位小写十六进制；业务修订从1开始。金额保留十进制字符串与单位；时间是UTC RFC3339。摘要使用准确字节或协议声明的JCS输入，不能拿未校验摘要证明内容存在或用户授权。

## 1 公共值与引用

无独立生命周期；外层记录提供原作用域。

### Id

字段语义如下。

值类型：string。格式：`^[a-z][a-z0-9_]*_[0-9a-f]{32}$`。

### Revision

字段语义如下。

值类型：integer 1..9007199254740991。

### Count

字段语义如下。

值类型：integer 0..9007199254740991。

### Digest

字段语义如下。

值类型：string。格式：`^sha256:[0-9a-f]{64}$`。

### Time

字段语义如下。

值类型：string (date-time)。格式：`Z$`。

### Decimal

字段语义如下。

值类型：string。格式：`^(0|[1-9][0-9]*)(\.[0-9]{1,9})?$`。

### Amount

字段语义如下。

| 字段 | 类型 | 必填 | 语义 |
| --- | --- | --- | --- |
| unit | string | 是 | 准确计量单位；不同单位不可直接相加 |
| value | Decimal | 是 | 该单位的非负十进制数量 |

### ObjectRef

字段语义如下。

| 字段 | 类型 | 必填 | 语义 |
| --- | --- | --- | --- |
| owner_id | Id | 是 | 该事实的逻辑负责方，不是当前 worker |
| object_id | Id | 是 | 所属类型的原对象身份 |
| revision | Revision | 是 | 当前对象可见修订，从1开始；不与其他修订混用 |
| tenant_id | Id | 是 | 受信租户；引用须与认证数据域一致 |

### ComponentRef

字段语义如下。

| 字段 | 类型 | 必填 | 语义 |
| --- | --- | --- | --- |
| component_id | Id | 是 | 不可变版本化组件的稳定身份 |
| version | string | 是 | 准确版本；禁止解析为 latest |
| digest | Digest | 是 | 准确对象或配置摘要，不提供授权 |

### ContentRef

字段语义如下。

| 字段 | 类型 | 必填 | 语义 |
| --- | --- | --- | --- |
| tenant_id | Id | 是 | 受信租户范围；载荷必须与认证上下文一致 |
| owner_id | Id | 是 | 该事实的逻辑负责方，不是当前 worker |
| content_id | Id | 是 | 原内容身份 |
| version | Revision | 是 | 准确版本；禁止解析为 latest |
| hash | Digest | 是 | 准确字节的 SHA256 摘要 |
| media_type | string | 是 | 准确字节的媒体类型 |
| byte_length | Count | 是 | 准确字节长度；不按字符数代替 |

### CollectionSummary

字段语义如下。

| 字段 | 类型 | 必填 | 语义 |
| --- | --- | --- | --- |
| collection_revision | Revision | 是 | 关系集合修订，变更与原 owner 事务共同提交 |
| total_count | Count | 是 | 原查询范围的全部关系数量 |
| unresolved_count | Count | 是 | 其中仍未核清的关系数量 |
| items_cursor | string | 否 | 当前集合的有界分页入口；省略不自动证明完整 |
| complete | boolean | 是 | 原 owner 关系索引完整性，不是业务完成 |

## 2 原始输入与请求

Session/Branch/Message/Submission 归应用；InputRequest 归实际业务消费 owner。

### Session

应用会话；不承担目标执行生命周期

| 字段 | 类型 | 必填 | 语义 |
| --- | --- | --- | --- |
| session_id | Id | 是 | 应用会话稳定身份；归档不取消其中Task |
| tenant_id | Id | 是 | 受信租户范围；载荷必须与认证上下文一致 |
| owner_id | Id | 是 | 该事实的逻辑负责方，不是当前 worker |
| revision | Revision | 是 | 当前对象可见修订，从1开始；不与其他修订混用 |
| state | open / archived / deleted | 是 | 该记录自己的生命周期；具体取值见类型列 |
| default_branch_id | Id | 是 | 该会话默认选择的分支，必须属于本Session |
| created_at | Time | 是 | 创建原记录的可信时点 |

### Branch

应用维护的历史选择；head 用 CAS

| 字段 | 类型 | 必填 | 语义 |
| --- | --- | --- | --- |
| branch_id | Id | 是 | 本条消息或输入所属原分支；迟到结果不迁移分支 |
| session_ref | ObjectRef | 是 | 原应用会话及准确修订；不表示执行归属 |
| revision | Revision | 是 | 当前对象可见修订，从1开始；不与其他修订混用 |
| head_message_id | Id | 否 | 当前分支末条消息；空分支可省略，更新使用CAS |
| source_branch_ref | ObjectRef | 否 | 创建分支时引用的准确源分支；原生分支可省略 |
| history_cutoff | Count | 是 | 源会话已提交 seq 上界；不是时间戳 |
| config_ref | ComponentRef | 是 | 本对象固定的配置组件版本与摘要 |

### Message

不可变消息；role 不提供信任，仅受信提交链可识别本人输入

| 字段 | 类型 | 必填 | 语义 |
| --- | --- | --- | --- |
| message_id | Id | 是 | 不可变原消息身份；编辑必须保存新的Message/Submission，不覆盖已提交原文 |
| session_ref | ObjectRef | 是 | 原应用会话及准确修订；不表示执行归属 |
| branch_id | Id | 是 | 本条消息或输入所属原分支；迟到结果不迁移分支 |
| seq | Revision | 是 | 同Session内已提交消息序号；唯一且单调，不按墙钟排序 |
| parent_message_id | Id | 否 | 本消息引用的前序消息；首条可省略，不得形成循环 |
| role | user / assistant / system / tool | 是 | 展示/上下文角色；user标签本身不证明本人身份或授权 |
| content_ref | ContentRef | 是 | 原正文的准确版本；当前读取仍检查用途与权限 |
| submission_ref | ObjectRef | 否 | 产生本用户输入的原应用投递记录；普通输出可省略 |
| created_at | Time | 是 | 创建原记录的可信时点 |

### Submission

不可变输入意图和可修订交付头；应用只决定投递，不裁决目标含义

| 字段 | 类型 | 必填 | 语义 |
| --- | --- | --- | --- |
| submission_id | Id | 是 | 应用首次保存的原输入/投递身份；重连或重发不换ID |
| tenant_id | Id | 是 | 受信租户范围；载荷必须与认证上下文一致 |
| owner_id | Id | 是 | 该事实的逻辑负责方，不是当前 worker |
| revision | Revision | 是 | 当前对象可见修订，从1开始；不与其他修订混用 |
| session_ref | ObjectRef | 是 | 原应用会话及准确修订；不表示执行归属 |
| branch_id | Id | 是 | 本条消息或输入所属原分支；迟到结果不迁移分支 |
| message_id | Id | 是 | 不可变原消息身份；编辑保存新准确正文及明确版本 |
| seq | Revision | 是 | 同Session内已提交消息序号；唯一且单调，不按墙钟排序 |
| kind | submit_goal / steer / input / enqueue_goal_after | 是 | 本类型声明的业务种类 |
| content_ref | ContentRef | 是 | 原正文的准确版本；当前读取仍检查用途与权限 |
| history_cutoff | Count | 是 | 首次固定的会话已提交seq上界，不随模型运行读取最新历史 |
| target_task_ref | ObjectRef | 条件 | 本次steer或Task型回答的准确原Task；不是新目标路由 |
| predecessor_task_ref | ObjectRef | 条件 | 后续目标等待其工作封闭的原Task；仅终态不证明封闭 |
| expected_goal_revision | Revision | 条件 | 原输入要作用的目标版本；冲突时不得自动改成当前值 |
| request_ref | ObjectRef | 条件 | 实际业务owner保存的准确InputRequest与请求版本 |
| dispatch_command_ref | ObjectRef | 条件 | 首次发送前固定；不是网络发送尝试 |
| state | queued / sending / applied / rejected / withdrawn | 是 | 该记录自己的生命周期；具体取值见类型列 |
| withdrawal_requested | boolean | 是 | 用户请求撤回；sending后为true不等于业务已撤回 |
| task_ref | ObjectRef | 条件 | Task型输入的原接纳结果映射；独立应用请求可以没有Task |
| created_at | Time | 是 | 创建原记录的可信时点 |

### InputRequest

原业务 owner 保存；明确语义不替代高影响动作 Confirmation

| 字段 | 类型 | 必填 | 语义 |
| --- | --- | --- | --- |
| request_id | Id | 是 | 原请求/确认身份 |
| tenant_id | Id | 是 | 受信租户范围；载荷必须与认证上下文一致 |
| owner_id | Id | 是 | 该事实的逻辑负责方，不是当前 worker |
| revision | Revision | 是 | 当前对象可见修订，从1开始；不与其他修订混用 |
| target_ref | ObjectRef | 是 | 此请求实际消费的业务对象；可为Task或独立应用对象 |
| goal_revision | Revision | 条件 | 完整目标及有效条件集合版本 |
| purpose | clarify_goal / supply_context / accept_quality / business_input | 是 | 请求的业务用途；澄清目标不等于批准高影响动作 |
| question_ref | ContentRef | 是 | 原owner固定的准确问题正文及来源上下文 |
| answer_schema_ref | ComponentRef | 是 | 原 owner 登记的闭合 Schema |
| preview_refs | ContentRef[] 0..100 | 是 | 原界面需呈现的准确正文；不是阅读证明 |
| expires_at | Time | 是 | 原资格或首次接纳截止；查询不延长 |
| state | pending / answered / expired / withdrawn | 是 | 该记录自己的生命周期；具体取值见类型列 |
| answer_ref | ContentRef | 条件 | 已一次消费的准确回答 |
| consumed_by | Id | 条件 | 一次消费原命令 |
| answered_at | Time | 条件 | 回答消费时间 |
| candidate_ref | ContentRef | 条件 | accept_quality 所验收的准确成果版本与摘要 |
| limitations_ref | ContentRef | 条件 | accept_quality 必须呈现的准确限制正文 |

### SourceEvidence

来源定位；引用可核验不等于授权，引用类型由受信接纳链判定

| 字段 | 类型 | 必填 | 语义 |
| --- | --- | --- | --- |
| content_ref | ContentRef | 是 | 准确原始消息、附件或派生材料版本 |
| source_kind | user_input / trusted_template / external_data / model_output | 是 | 来源类别；不能由模型自报可信身份 |
| submission_ref | ObjectRef | 否 | 受信应用原提交；user_input 的本人来源须在原 owner 可核验 |
| locator | string | 否 | 准确版本中的位置；文本用 Unicode code point 半开区间 text:start:end，结构化数据用 JSON Pointer |

### ScheduleSpec

首版有限调度规则，日历依外层固定时区；不接受cron/RRULE或任意脚本

#### once_at

| 字段 | 类型 | 必填 | 语义 |
| --- | --- | --- | --- |
| type | once_at | 是 | 闭合规则种类 |
| at | Time | 是 | 准确UTC计划时点；创建要求严格晚于effective_after |

#### interval

| 字段 | 类型 | 必填 | 语义 |
| --- | --- | --- | --- |
| type | interval | 是 | 闭合规则种类 |
| anchor_at | Time | 是 | UTC锚点，重启不改变 |
| every_seconds | integer 1..9007199254740991 | 是 | 固定正整数间隔 |

#### daily

| 字段 | 类型 | 必填 | 语义 |
| --- | --- | --- | --- |
| type | daily | 是 | 闭合规则种类 |
| local_time | string | 是 | 当地HH:MM:SS，秒精度；无时区或闰秒字段 |

#### weekly

| 字段 | 类型 | 必填 | 语义 |
| --- | --- | --- | --- |
| type | weekly | 是 | 闭合规则种类 |
| local_time | string | 是 | 当地HH:MM:SS，秒精度；无时区或闰秒字段 |
| weekdays | integer 1..7[] 1..7 | 是 | ISO星期；必须去重升序 |

#### monthly

| 字段 | 类型 | 必填 | 语义 |
| --- | --- | --- | --- |
| type | monthly | 是 | 闭合规则种类 |
| local_time | string | 是 | 当地HH:MM:SS，秒精度；无时区或闰秒字段 |
| monthdays | integer 1..31[] 1..31 | 是 | 当地月日；必须去重升序，不存在的日期跳过 |


## 3 目标与条件

以下存储记录归原 Orchestrator。候选和引用是嵌入值，不能脱离 Task/原来源独立寻址。

### GoalDocument

原Task owner确定性生成的完整目标包；解释语义发生在条件提炼阶段

| 字段 | 类型 | 必填 | 语义 |
| --- | --- | --- | --- |
| format_version | 1 | 是 | 确定性目标包格式版本 |
| initial_goal_ref | ContentRef | 是 | 完整初始原文；若已是目标包，owner须读取并保留其最初引用，不无限嵌套 |
| amendment_refs | ContentRef[] 0..100 | 是 | 按受信提交顺序保存的准确本人补充；上限触发明确整理/新目标，不截断 |

### RequirementCandidate

Brain、受信模板或调用方提出的候选；无裁决权限

| 字段 | 类型 | 必填 | 语义 |
| --- | --- | --- | --- |
| candidate_key | string | 是 | 本次提案内唯一局部键；不是权威 requirement_id |
| replaces_requirement_id | Id | 条件 | 可选已有条件候选；只允许同一 Task |
| kind | effect / quality | 是 | 本类型声明的业务种类 |
| statement_ref | ContentRef | 是 | 可检查的条件描述；尚未成为权威条件 |
| source_refs | SourceEvidence[] 1..100 | 是 | 完整依据与原输入定位 |
| origin | explicit_user / derived | 是 | 显式要求或为实现目标推导；不是授权等级 |
| rule_ref | ComponentRef | 是 | 已登记判断规则的准确版本 |
| rule_parameters_ref | ContentRef | 否 | 符合该 rule 输入 Schema 的准确参数 |
| required | boolean | 是 | 建议是否必要；不能用 false 覆盖显式硬要求 |
| open_questions | string[] 0..20 | 是 | 尚未明确的含义/参数；非空不得接纳该候选 |
| replaces_revision | Revision | 条件 | 替换时必须绑定当前准确旧条件版本 |

### RequirementDelta

增量upsert候选；未提及条件保留。同批重复/多重匹配拒绝；不支持隐式删除拆分合并

| 字段 | 类型 | 必填 | 语义 |
| --- | --- | --- | --- |
| base_goal_revision | Revision | 是 | 候选或采纳基于的目标版本；必须与接纳前Task一致 |
| candidates | RequirementCandidate[] 1..100 | 是 | 本原来源提出的完整有界候选数组，candidate_key不可重复 |
| reason_ref | ContentRef | 是 | 完整提议理由，不作为授权依据 |

### RequirementRef

Task 内的准确条件版本；tenant、owner、task 由外层固定

| 字段 | 类型 | 必填 | 语义 |
| --- | --- | --- | --- |
| requirement_id | Id | 是 | 同Task内稳定条件身份；跨Task须带作用域 |
| revision | Revision | 是 | 当前对象可见修订，从1开始；不与其他修订混用 |

### Requirement

Orchestrator 已接纳的不可变条件版本；生效集合由 GoalRevision 选择

| 字段 | 类型 | 必填 | 语义 |
| --- | --- | --- | --- |
| requirement_id | Id | 是 | 同 Task 内稳定身份；删后不重用 |
| revision | Revision | 是 | 定义版本；同版本不可改 |
| kind | effect / quality | 是 | 本类型声明的业务种类 |
| statement_ref | ContentRef | 是 | 准确条件描述 |
| source_refs | SourceEvidence[] 1..100 | 是 | 准确原始输入、附件或衍生依据集合；不把引用升级成权限 |
| origin | explicit_user / derived | 是 | 条件来自显式用户要求或必要推导；不等于授权等级 |
| rule_ref | ComponentRef | 是 | 定义通过谓词的准确规则，不是执行实现 |
| rule_parameters_ref | ContentRef | 否 | 符合原rule闭合Schema的准确目标、范围和阈值参数 |
| required | boolean | 是 | 是否为必要条件；显式硬要求不能由模型降低 |
| adoption_id | Id | 是 | 原 Task owner 的采纳决定 |

### RequirementMapping

一次采纳中的局部候选到权威条件版本映射

| 字段 | 类型 | 必填 | 语义 |
| --- | --- | --- | --- |
| candidate_key | string | 是 | 原提案内局部候选键，用于映射；不是权威条件ID |
| requirement_id | Id | 是 | 同Task内稳定条件身份；跨Task须带作用域 |
| revision | Revision | 是 | 当前对象可见修订，从1开始；不与其他修订混用 |

### GoalRevision

Task 内不可变版本快照；条件变化也增加 goal_revision

| 字段 | 类型 | 必填 | 语义 |
| --- | --- | --- | --- |
| task_ref | ObjectRef | 是 | 本次提交后 Task 版本；非另一任务身份 |
| goal_revision | Revision | 是 | 完整目标及有效条件集合版本 |
| goal_ref | ContentRef | 是 | 当前完整目标，不是模型摘要；原输入与后续补充都保留 |
| source_refs | SourceEvidence[] 1..100 | 是 | 准确原始输入、附件或衍生依据集合；不把引用升级成权限 |
| requirements | RequirementRef[] 0..100 | 是 | 当前目标选中的完整条件版本集合；初值上限100 |
| requirements_digest | Digest | 是 | 按条件 ID 排序的完整 Requirement 版本数组的 JCS SHA256 |
| change_kind | initial / user_revision / requirements_adopted | 是 | 初始目标、本人修订或已接纳条件变化；保留各自原因 |
| cause_ref | ObjectRef | 是 | 原 Submission、Command、输入消费或 Decision |
| created_at | Time | 是 | 创建原记录的可信时点 |

### RequirementAdoption

Task owner 唯一消费原来源后保存的决定；与条件、目标修订和 Job 共事务

| 字段 | 类型 | 必填 | 语义 |
| --- | --- | --- | --- |
| adoption_id | Id | 是 | 原Task owner保存的唯一采纳决定身份 |
| task_ref | ObjectRef | 是 | 准确原Task及其owner/tenant/修订 |
| source_kind | submission / decision / input / command | 是 | 本采纳实际消费的Submission、Decision、输入或直连Command来源 |
| source_ref | ObjectRef | 是 | 原事实或责任来源的准确引用 |
| base_goal_revision | Revision | 是 | 候选或采纳基于的目标版本；必须与接纳前Task一致 |
| result_goal_revision | Revision | 是 | 本决定提交后的目标版本；unchanged/rejected不增加 |
| outcome | accepted / unchanged / rejected | 是 | accepted有实质变化；unchanged复用原定义；rejected保存原拒绝 |
| mappings | RequirementMapping[] 0..100 | 是 | 本次候选局部键到同Task权威条件ID/定义版本的映射 |
| validation_report_ref | ContentRef | 是 | 来源/覆盖初检/规则/权限边界检查报告；不替代 GoalCoverage |
| reason_codes | string[] 0..20 | 是 | 结构化的校验或拒绝原因，可为空；不含凭据或秘密 |
| decided_at | Time | 是 | 原业务裁决时间 |

### GoalCoverage

覆盖原判断不可变；当前适用性为证据 gate 的独立投影

| 字段 | 类型 | 必填 | 语义 |
| --- | --- | --- | --- |
| coverage_id | Id | 是 | 原完整目标覆盖检查身份，不复用为逐条件判断 |
| task_ref | ObjectRef | 是 | 准确原Task及其owner/tenant/修订 |
| goal_revision | Revision | 是 | 完整目标及有效条件集合版本 |
| goal_ref | ContentRef | 是 | 当前完整目标的准确正文，不能以模型摘要替代 |
| requirements_digest | Digest | 是 | 按条件ID排序的完整已接纳Requirement数组的JCS SHA256摘要 |
| mapping_report_ref | ContentRef | 是 | 完整目标各条款到条件的映射、遗漏、冲突和局限 |
| rule_ref | ComponentRef | 是 | 定义通过谓词的准确规则，不是执行实现 |
| evaluator_ref | ComponentRef | 是 | 执行判断的准确实现，与rule分开 |
| operation_ref | ObjectRef | 否 | 有外部或模型核验时的原获准 Operation |
| verdict | pass / fail / unknown | 是 | 不可变原判断pass/fail/unknown |
| applicability | usable / unknown / inapplicable | 是 | 当前证据是否仍适用，不覆写原verdict |
| checked_at | Time | 是 | 该原检查报告形成时点，不代替目标实际观察时间 |
| revision | Revision | 是 | 覆盖记录读视图修订；原 verdict 不变，当前 applicability 变化会推进此修订 |

### RuleDefinition

原规则owner登记的不可变业务配置；模型不能自行扩大allowed_basis

| 字段 | 类型 | 必填 | 语义 |
| --- | --- | --- | --- |
| rule_ref | ComponentRef | 是 | 规则注册表的准确版本 |
| kind | effect / quality | 是 | 效果条件或质量条件 |
| parameters_schema_ref | ComponentRef | 是 | 参数的闭合Schema |
| predicate | historical_effect / current_state / quality | 是 | 原历史效果、当前状态或质量谓词 |
| allowed_basis | verified / assessed / user_accepted[] 1..3 | 是 | 该规则允许采用的判断依据 |
| allow_user_acceptance | boolean | 是 | 是否允许本人验收替代该质量判断 |
| required_evidence_schema_ref | ComponentRef | 是 | 证据类型及最低要求 |
| scope_schema_ref | ComponentRef | 是 | 资源、覆盖和限制的闭合范围 |
| max_observation_age_seconds | Count | 条件 | current_state必须给有限观察年龄 |
| risk_class | ordinary / high_impact | 是 | 高影响自动判断要求独立校准和零陈旧门禁 |
| applicability_policy_ref | ComponentRef | 是 | 证据适用性及缺陷处理政策 |

### WaitReason

字段语义如下。

| 字段 | 类型 | 必填 | 语义 |
| --- | --- | --- | --- |
| kind | input / authorization / dependency / budget / effect / capacity / evidence | 是 | 本类型声明的业务种类 |
| object_ref | ObjectRef | 否 | 阻塞或等待的准确原对象 |
| resume_condition | string | 是 | 可机械判断的具体恢复前提 |
| next_check_at | Time | 否 | 下次有限核对时点；不表示届时必然恢复 |

### Task

字段语义如下。

| 字段 | 类型 | 必填 | 语义 |
| --- | --- | --- | --- |
| tenant_id | Id | 是 | 受信租户范围；载荷必须与认证上下文一致 |
| task_id | Id | 是 | 原Task身份；不随重试更换 |
| orchestrator_id | Id | 是 | Task终身归属的逻辑编排器 |
| submit_command_id | Id | 是 | 唯一创建Task的原命令 |
| goal_ref | ContentRef | 是 | 当前完整目标的准确正文，不能以模型摘要替代 |
| goal_revision | Revision | 是 | 完整目标及有效条件集合版本 |
| control_revision | Revision | 是 | 启动门禁版本；pause后resume仍递增 |
| revision | Revision | 是 | 当前对象可见修订，从1开始；不与其他修订混用 |
| policy_ref | ComponentRef | 是 | 固定的准确策略及摘要 |
| requirements | Requirement[] 0..100 | 是 | 当前目标选中的完整条件版本集合；初值上限100 |
| deadline | Time | 是 | 目标或责任的领域截止，不等于RPC截止 |
| status | active / succeeded / failed / cancelled | 是 | 本对象的独立业务状态，不外推为目标效果 |
| control | running / paused | 是 | 本任务自身running或paused，另需检查祖先限制 |
| wait_reasons | WaitReason[] 0..100 | 是 | 并存依赖集合；空集不独立证明可行动 |
| budget | BudgetBalance[] 1..100 | 是 | 逐单位任务预算摘要，不复制上层可花额度 |
| open_effects | CollectionSummary | 是 | 原Task完整未结操作关系的摘要 |
| accounting_open | boolean | 是 | 是否仍有原费用或结算责任 |
| result_ref | ObjectRef | 条件 | 原Orchestrator不可变权威Result；Content导出副本独立发布 |
| acceptance_ref | ObjectRef | 否 | estimate风险的原本人策略接受记录 |
| requirements_state | collecting / awaiting_input / validating / ready | 是 | 条件准备门禁；不改变 Task 执行状态机 |
| requirements_digest | Digest | 是 | 当前完整条件版本数组的规范摘要 |
| current_coverage_ref | ObjectRef | 条件 | 当前 GoalCoverage；ready 时必需 |

### ConditionResult

字段语义如下。

| 字段 | 类型 | 必填 | 语义 |
| --- | --- | --- | --- |
| check_id | Id | 是 | 稳定原检查身份 |
| task_id | Id | 是 | 原Task身份；不随重试更换 |
| goal_revision | Revision | 是 | 完整目标及有效条件集合版本 |
| requirement_id | Id | 是 | 同Task内稳定条件身份；跨Task须带作用域 |
| artifact_ref | ContentRef | 是 | 本次判断针对的准确成果版本 |
| rule_ref | ComponentRef | 是 | 定义通过谓词的准确规则，不是执行实现 |
| evaluator_ref | ComponentRef | 是 | 执行判断的准确实现，与rule分开 |
| verdict | pass / fail / unknown | 是 | 不可变原判断pass/fail/unknown |
| applicability | usable / unknown / inapplicable | 是 | 当前证据是否仍适用，不覆写原verdict |
| basis | verified / assessed / user_accepted | 是 | verified/assessed/user_accepted；必要效果不能用户验收替代 |
| evidence_refs | ContentRef[] 1..100 | 是 | 支撑当前判断的准确证据，非全部处理来源 |
| requirement_revision | Revision | 是 | 准确条件定义版本 |
| observed_at | Time | 条件 | 目标证据的实际观察时点；未知时不能形成 usable pass |
| scope_ref | ContentRef | 是 | 固定资源、时点、覆盖与限制的判断范围 |
| checked_at | Time | 是 | 检查报告形成时点 |

### Result

字段语义如下。

| 字段 | 类型 | 必填 | 语义 |
| --- | --- | --- | --- |
| task_id | Id | 是 | 原Task身份；不随重试更换 |
| goal_revision | Revision | 是 | 完整目标及有效条件集合版本 |
| artifact_refs | ContentRef[] 1..100 | 是 | 本次正式交付的准确成果集合 |
| completion_basis | verified / assessed / user_accepted | 是 | 必要条件中最弱依据，不由可选条件失败降低 |
| condition_results | ConditionResult[] 1..100 | 是 | 最终选中的逐条件判断；每必要条件恰一项 |
| coverage_ref | ObjectRef | 是 | 准确 GoalCoverage；正文报告由该记录引用 |
| limitations | string[] 0..100 | 是 | 正式结果的可见局限；不能隐藏必要失败 |
| completed_at | Time | 是 | 原完成事务的可信裁决时间 |
| result_id | Id | 是 | 完成事务固定的结果身份，不是Content身份 |
| revision | 1 | 是 | 权威Result不可变；缺陷notice和导出状态另存 |

## 4 决策与执行

DispatchIntent 归 Orchestrator；DecisionRecord 归 Brain；Operation 归 Executor；ControlSnapshot 是原控制的传输值。

### DecisionDispatchIntent

字段语义如下。

| 字段 | 类型 | 必填 | 语义 |
| --- | --- | --- | --- |
| decision_id | Id | 是 | 固定的一次Brain判断身份 |
| task_ref | ObjectRef | 是 | 准确原Task及其owner/tenant/修订 |
| snapshot_ref | ContentRef | 是 | Orchestrator封存的不可变输入快照 |
| snapshot_revision | Revision | 是 | 该原快照版本，不可换当前输入 |
| model_profile_ref | ComponentRef | 是 | 准确模型及编码/限制配置 |
| brain_owner_id | Id | 是 | 固定接收本次Decision的Brain |
| command_id | Id | 是 | 原逻辑命令身份，与传输序号不同 |
| intent_hash | Digest | 是 | 完整准备后业务意图的规范摘要 |

### DecisionRecord

字段语义如下。

| 字段 | 类型 | 必填 | 语义 |
| --- | --- | --- | --- |
| decision_id | Id | 是 | 固定的一次Brain判断身份 |
| owner_id | Id | 是 | 该事实的逻辑负责方，不是当前 worker |
| revision | Revision | 是 | 当前对象可见修订，从1开始；不与其他修订混用 |
| snapshot_revision | Revision | 是 | 该原快照版本，不可换当前输入 |
| status | accepted / running / completed / failed / cancelled | 是 | 本对象的独立业务状态，不外推为目标效果 |
| send_started | boolean | 是 | 真实发送可能性屏障已持久，不能因此透明重发 |
| physical_request_count | integer 0..1 | 是 | 本Decision物理模型请求数，范围0..1 |
| proposal_ref | ContentRef | 条件 | 原提案的准确内容；接纳另由Task owner裁决 |
| usage | Amount[] 0..100 | 是 | 累计实际用量；不因超限或终态拒绝记录 |
| usage_final | boolean | 是 | 当前取得的账单是否最终；更正仍可增加新修订 |

### Operation

字段语义如下。

| 字段 | 类型 | 必填 | 语义 |
| --- | --- | --- | --- |
| operation_id | Id | 是 | 稳定获准行动身份；不是物理尝试 |
| owner_id | Id | 是 | 该事实的逻辑负责方，不是当前 worker |
| task_ref | ObjectRef | 是 | 准确原Task及其owner/tenant/修订 |
| revision | Revision | 是 | 当前对象可见修订，从1开始；不与其他修订混用 |
| execution_state | accepted / started / closed | 是 | accepted/started/closed只描述执行责任 |
| effect | not_started / applied / not_applied / unknown | 是 | 声明效果的已知事实；unknown不可猜成未执行 |
| may_apply_later | boolean 或 unknown | 是 | 是否仍可能发生迟到效果，与effect独立 |
| attempts | CollectionSummary | 是 | 原Operation内物理尝试集合摘要 |
| evidence_refs | ContentRef[] 0..100 | 是 | 支撑当前判断的准确证据，非全部处理来源 |
| result_ref | ContentRef | 否 | 正式准确结果引用；不由流式文字代替 |
| usage | Amount[] 0..100 | 是 | 累计实际用量；不因超限或终态拒绝记录 |
| usage_final | boolean | 是 | 当前取得的账单是否最终；更正仍可增加新修订 |
| next_action | query_original / provide_authorization / provide_evidence / wait / none | 是 | 原owner的恢复或处置下一步，非新授权 |

### ControlSnapshot

字段语义如下。

| 字段 | 类型 | 必填 | 语义 |
| --- | --- | --- | --- |
| orchestrator_id | Id | 是 | Task终身归属的逻辑编排器 |
| task_id | Id | 是 | 原Task身份；不随重试更换 |
| goal_revision | Revision | 是 | 完整目标及有效条件集合版本 |
| control_revision | Revision | 是 | 启动门禁版本；pause后resume仍递增 |
| status | active / succeeded / failed / cancelled | 是 | 本对象的独立业务状态，不外推为目标效果 |
| control | running / paused | 是 | 本任务自身running或paused，另需检查祖先限制 |
| issued_at | Time | 是 | 原声明签发时点 |
| start_before | Time | 是 | 最晚实际启动时间；所有窗口取最紧 |
| proof_ref | ContentRef | 是 | 原owner可核验依据或签名载体；引用本身不是授权 |
| window_id | Id | 是 | 独立有限窗口身份；同control_revision可签发新窗口，不改控制事实 |

## 5 授权预算和交接

Grant 归授权 owner，Confirmation 归原业务 owner；BudgetBalance 是 Task 每单位投影；UsageSnapshot 归实际计费源；Closure 和 DelegationContext 保持原双方身份。

### Grant

字段语义如下。

| 字段 | 类型 | 必填 | 语义 |
| --- | --- | --- | --- |
| grant_id | Id | 是 | 原授权许可身份 |
| owner_id | Id | 是 | 该事实的逻辑负责方，不是当前 worker |
| revision | Revision | 是 | 当前对象可见修订，从1开始；不与其他修订混用 |
| subject_ref | ObjectRef | 是 | 准确使用主体，不从正文自行认证 |
| resources | string[] 1..100 | 是 | 准许的规范资源范围 |
| actions | string[] 1..100 | 是 | 准许的语义行动集合 |
| purposes | string[] 1..100 | 是 | 分别授予的处理/披露/保存等用途 |
| recipients | string[] 0..100 | 是 | 允许的实际数据接收方 |
| locations | string[] 1..100 | 是 | 允许处理或保存的位置 |
| mode | once / continuous | 是 | once或continuous；退款不恢复once |
| state | active / revoked | 是 | 该记录自己的生命周期；具体取值见类型列 |
| not_before | Time | 是 | 资格开始时点 |
| expires_at | Time | 是 | 原资格或首次接纳截止；查询不延长 |
| limits | Amount[] 0..100 | 是 | 逐单位许可或消费上限 |

### Confirmation

字段语义如下。

| 字段 | 类型 | 必填 | 语义 |
| --- | --- | --- | --- |
| request_id | Id | 是 | 原请求/确认身份 |
| owner_id | Id | 是 | 该事实的逻辑负责方，不是当前 worker |
| revision | Revision | 是 | 当前对象可见修订，从1开始；不与其他修订混用 |
| original_command_id | Id | 是 | 本人决定绑定的准确业务命令ID |
| intent_hash | Digest | 是 | 完整准备后业务意图的规范摘要 |
| preview_refs | ContentRef[] 0..100 | 是 | 原界面需呈现的准确正文；不是阅读证明 |
| expires_at | Time | 是 | 原资格或首次接纳截止；查询不延长 |
| state | pending / approved / denied / expired / consumed | 是 | 该记录自己的生命周期；具体取值见类型列 |
| consumed_by | Id | 条件 | 唯一消费此请求的原命令 |
| consumed_at | Time | 条件 | 原业务一次消费提交的时间 |
| challenge | string | 是 | 受信会话绑定的单用途确认挑战，不保存用户密码 |
| trusted_user_session_ref | ObjectRef | 是 | 业务确认绑定的受信本人会话 |
| decided_by | Id | 条件 | 受信身份链取得的实际决定者 |
| decided_at | Time | 条件 | 原业务裁决时间 |
| original_command_ref | ContentRef | 是 | 准确原命令正文及版本 |

### BudgetBalance

字段语义如下。

| 字段 | 类型 | 必填 | 语义 |
| --- | --- | --- | --- |
| unit | string | 是 | 准确计量单位；不同单位不可直接相加 |
| limit | Decimal | 是 | 未来承诺准入上限 |
| reserved | Decimal | 是 | 尚未释放的费用承诺 |
| spent | Decimal | 是 | 原真实累计 gross_spent，可因真实更正超出limit |

### UsageSnapshot

字段语义如下。

| 字段 | 类型 | 必填 | 语义 |
| --- | --- | --- | --- |
| source_ref | ObjectRef | 是 | 原事实或责任来源的准确引用 |
| usage_revision | Revision | 是 | 同原计费源的累计账单修订 |
| usage_digest | Digest | 是 | 完整原账单修订摘要 |
| cumulative | Amount[] 1..100 | 是 | 逐单位实际累计用量，只追可信增量 |
| spending_closed | boolean | 是 | 已封闭该责任新的消费，不代表费用最终 |
| usage_final | boolean | 是 | 当前取得的账单是否最终；更正仍可增加新修订 |
| proof_refs | ContentRef[] 1..100 | 是 | 受信原事实的可核验依据 |

### AllocationClosure

字段语义如下。

| 字段 | 类型 | 必填 | 语义 |
| --- | --- | --- | --- |
| allocation_id | Id | 是 | 固定跨域预算分配身份 |
| parent_owner_id | Id | 是 | 原父分配owner |
| receiver_id | Id | 是 | 唯一获配接收方 |
| spending_closed | True | 是 | 已封闭该责任新的消费，不代表费用最终 |
| closed_at | Time | 是 | 原消费门禁关闭时间；更正不改 |
| usage_revision | Revision | 是 | 同原计费源的累计账单修订 |
| final_usage | Amount[] 1..100 | 是 | 关闭时当前最终累计用量；可信更正另增修订 |
| proof_ref | ContentRef | 是 | 原owner可核验依据或签名载体；引用本身不是授权 |

### DelegationContext

字段语义如下。

| 字段 | 类型 | 必填 | 语义 |
| --- | --- | --- | --- |
| delegation_id | Id | 是 | 原父子目标交接身份 |
| parent_task_ref | ObjectRef | 是 | 准确原父Task |
| parent_goal_revision | Revision | 是 | 委派对应的原父目标版本 |
| ancestor_task_refs | ObjectRef[] 1..4 | 是 | 来自可核关系的真实祖先，不接受调用者自造 |
| agent_binding_ref | ObjectRef | 是 | 准确子Agent配置和接收绑定 |
| allocation_ref | ObjectRef | 是 | 父方原预算分配，不是另发额度 |
| permission_refs | ObjectRef[] 1..100 | 是 | 原可核验收缩许可集合 |
| parent_control_snapshot | ControlSnapshot | 是 | 原父控制的有限声明 |
| context_digest | Digest | 是 | 准确委派上下文摘要 |
| parent_proof_ref | ContentRef | 是 | 父关系与原委派的受信证明 |

## 6 持久责任与命令

Job 在每个业务 owner 本地保存；Claim 是固定领取凭据；Command 是跨网络不变的原请求。

### Job

字段语义如下。

| 字段 | 类型 | 必填 | 语义 |
| --- | --- | --- | --- |
| job_id | Id | 是 | 不可复用的持久责任实例 |
| owner_id | Id | 是 | 该事实的逻辑负责方，不是当前 worker |
| tenant_id | Id | 是 | 受信租户范围；载荷必须与认证上下文一致 |
| responsibility_key | string | 是 | 领域责任唯一槽；done后的新事实可Raise |
| kind | string | 是 | 本类型声明的业务种类 |
| source_ref | ObjectRef | 是 | 原事实或责任来源的准确引用 |
| state | ready / leased / waiting / done | 是 | 该记录自己的生命周期；具体取值见类型列 |
| due_at | Time | 是 | 最早应处理时点，不是任务deadline |
| work_revision | Revision | 是 | 新领域责任版本；通知和轮询不递增 |
| lease_epoch | Count | 是 | 本次领取代次，旧worker不可写 |
| holder_id | Id | 条件 | 本次启动随机身份，不复活旧boot |
| lease_until | Time | 条件 | 原库裁决的领取期限 |

### Claim

本次领取凭据；Job 的当前 work_revision 可以随后变大

| 字段 | 类型 | 必填 | 语义 |
| --- | --- | --- | --- |
| job_id | Id | 是 | 不可复用的持久责任实例 |
| owner_id | Id | 是 | 该事实的逻辑负责方，不是当前 worker |
| tenant_id | Id | 是 | 受信租户范围；载荷必须与认证上下文一致 |
| holder_id | Id | 是 | 本次启动随机身份，不复活旧boot |
| lease_epoch | Count | 是 | 本次领取代次，旧worker不可写 |
| lease_until | Time | 是 | 原库裁决的领取期限 |
| observed_work_revision | Revision | 是 | Claim 固定值；不能在 Renew/Guard/Finish 时刷新 |

### Command

字段语义如下。

| 字段 | 类型 | 必填 | 语义 |
| --- | --- | --- | --- |
| protocol | harness/1 | 是 | 固定协议族 |
| profile | architecture-2026-10-data1 | 是 | 完整字段语义及解码器版本 |
| logical_service_id | Id | 是 | 原逻辑接收服务；重试不改投默认owner |
| command_id | Id | 是 | 原逻辑命令身份，与传输序号不同 |
| method | task.submit / task.cancel / execution.invoke / execution.cancel / task.billing_reconcile | 是 | 固定业务方法，不由插件任意构造 |
| target_id | Id | 是 | 方法固定目标，必须与载荷和原服务一致 |
| expires_at | Time | 是 | 原资格或首次接纳截止；查询不延长 |
| expected_revision | Revision | 条件 | 原CAS前提，重试不可刷新 |
| payload | object | 是 | 该method的闭合载荷，详见本字典命令部分 |

## 7 状态和跨字段约束

- Task.succeeded 必须有指向原owner权威Result的ObjectRef result_ref、ready、完整且未结为零的效果集合；非成功状态没有成功 result_ref。ready 至少一条条件，且 current_coverage_ref 对应当前 pass/usable 的覆盖记录
- GoalCoverage 的 verdict、目标与报告不改写；当前 applicability 变化推进其读视图 revision。GoalRevision 保留原集合，旧引用不静默解析为较新版本
- ConditionResult 的 observed_at 仅在观察已知时保存；pass 且 usable 必须存在。必要 effect 条件的 basis 必须 verified，不能由 user_accepted 或质量打分替代
- Submission.steer 固定Task和 expected_goal_revision；input 固定 request_ref，仅Task型回答要求目标与目标修订，独立应用回答可以没有Task；enqueue_goal_after 固定 predecessor_task_ref。sending/applied 必须有原 dispatch；已应用的Task型输入保留Task映射
- InputRequest.answered 必须有 answer_ref、consumed_by 和 answered_at；accept_quality 还固定 candidate_ref、limitations_ref 和 goal_revision，预览必须覆盖这些准确内容
- Confirmation.consumed 必须保存原 consumed_by/at；原本人决定和命令必须绑定。Use、金额、权限仍须业务端重新核验
- Job.leased 才有 holder_id/lease_until。Claim.observed_work_revision 在本次领取中固定；当前 Job.work_revision 可以更高
- 同一目标快照中 requirement_id、同一候选来源中 candidate_key、同一最终 Result 中所选必要条件不得重复。Schema 的 uniqueItems 不能替代按业务键唯一
- tenant/owner/task、条件版本、内容摘要、期限、真实来源、当前门禁及原授权的关系由语义校验和实际 owner 事务检查，不能仅靠 JSON 结构证明

## 8 五条核心命令载荷

外层 Command 的身份、profile、首次接纳期限、原 CAS 和摘要不可在重传时刷新。以下只列已形式化的五个方法；其他模块端口必须单独发布闭合合同，不能用任意 payload 绕过版本管理。

### task.submit

| 字段 | 类型 | 必填 | 语义 |
| --- | --- | --- | --- |
| orchestrator_id | Id | 是 | Task终身归属的逻辑编排器 |
| goal_ref | ContentRef | 是 | 当前完整目标的准确正文，不能以模型摘要替代 |
| policy_ref | ComponentRef | 是 | 固定的准确策略及摘要 |
| deadline | Time | 是 | 目标或责任的领域截止，不等于RPC截止 |
| budget | Amount[] 1..100 | 是 | 逐单位任务预算摘要，不复制上层可花额度 |
| delegation_context | DelegationContext | 否 | 本方法固定字段，按原输入保存 |
| acceptance_ref | ObjectRef | 否 | estimate风险的原本人策略接受记录 |
| requirement_candidates | RequirementCandidate[] 0..100 | 否 | 可选提示；只能经原 owner 校验接纳 |
| source_submission_ref | ObjectRef | 否 | 应用原输入；直连 API 省略时来源为受信原 Command |

### task.cancel

| 字段 | 类型 | 必填 | 语义 |
| --- | --- | --- | --- |
| task_id | Id | 是 | 原Task身份；不随重试更换 |
| reason | string | 是 | 本方法固定字段，按原输入保存 |

### execution.invoke

| 字段 | 类型 | 必填 | 语义 |
| --- | --- | --- | --- |
| operation_id | Id | 是 | 稳定获准行动身份；不是物理尝试 |
| task_ref | ObjectRef | 是 | 准确原Task及其owner/tenant/修订 |
| goal_revision | Revision | 是 | 完整目标及有效条件集合版本 |
| control_revision | Revision | 是 | 启动门禁版本；pause后resume仍递增 |
| capability_ref | ComponentRef | 是 | 本方法固定字段，按原输入保存 |
| binding_ref | ObjectRef | 是 | 本方法固定字段，按原输入保存 |
| intent_ref | ContentRef | 是 | 本方法固定字段，按原输入保存 |
| intent_hash | Digest | 是 | 完整准备后业务意图的规范摘要 |
| use_refs | ObjectRef[] 1..100 | 是 | 本方法固定字段，按原输入保存 |
| deadline | Time | 是 | 目标或责任的领域截止，不等于RPC截止 |
| control_snapshot | ControlSnapshot | 是 | 本方法固定字段，按原输入保存 |
| reservation_ref | ObjectRef | 是 | 本方法固定字段，按原输入保存 |

### execution.cancel

| 字段 | 类型 | 必填 | 语义 |
| --- | --- | --- | --- |
| operation_id | Id | 是 | 稳定获准行动身份；不是物理尝试 |
| reason | string | 是 | 本方法固定字段，按原输入保存 |
| task_ref | ObjectRef | 是 | 准确原Task及其owner/tenant/修订 |
| orchestrator_id | Id | 是 | Task终身归属的逻辑编排器 |

### task.billing_reconcile

| 字段 | 类型 | 必填 | 语义 |
| --- | --- | --- | --- |
| task_id | Id | 是 | 原Task身份；不随重试更换 |
| source_ref | ObjectRef | 是 | 原事实或责任来源的准确引用 |
| source_kind | brain_decision / execution_operation / grant_use / budget_allocation | 是 | 本方法固定字段，按原输入保存 |
| usage_revision | Revision | 是 | 同原计费源的累计账单修订 |
| usage_digest | Digest | 是 | 完整原账单修订摘要 |

task.submit 可不带 requirement_candidates。它们只作候选，权威条件由 Task owner 接纳。task.cancel 必须提供原 expected_revision；账单唤醒按原计费源修订归并，不要求发送者猜 Task 当前修订。

重新生成：运行 `python docs/architecture/validation/build_field_reference.py`；只检查漂移时加 `--check`。这个脚本不连接数据库或外部服务。
