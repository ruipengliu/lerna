# 切片02首票：Host演示接纳的必要细化

2026-10-03。沿用 [decisions.md](decisions.md) 的最终依赖决定、已批准的八张票据、现有runtime/contracts设计和已实现contract。这是首票实施所采用的具体行为决定；不增加 Task、公共1.0.0方法或通用仓储。

## 1. 准确内部命令及payload

沿用内部 `contract_version=host-durable-work-1`、`profile=host`、`method=durable_work.record`。严格JSON词法、公共值及规范摘要算法继续复用，但业务准入必须先通过Host专属闭合Schema。不得用公共DecodeCommand去“支持”此方法，也不得改1.0.0.methods清单；公共DecodeCommand对此内部版本继续明确拒绝。

命令保留公共信封字段command_id、target、payload、accept_before、可选expected_revision/trace_context；Host专属Schema固定上述三元身份。target闭合为OwnerRef同样的tenant_id/owner_id，加 `kind:"durable_work"`、`id:ID`，**不接受target.revision**，并发前态只放顶层expected_revision。

payload仅为：

```
{"text": "准确原文"}
```

text是必填string，允许空字符串，maxLength=65536个Unicode标量；未知payload字段拒绝。继续执行整个原始UTF-8命令1MiB与64层限制、无裸数字、无重复键、准确ID/修订/UTC时间规则。65536是此演示方法的小正文上限，不扩大公共合同，不需要大内容服务。不trim、不Unicode归一化、不大小写转换。

不在payload重复object_id、owner、主体、lane、阶段、期限、delay、Job ID或处理策略；它们分别属于target、受信上下文或Host装配。后续调度测试通过Host配置/实际需要的内部操作取得类别与等待事实，不预塞未来控制字段。

## 2. 业务事实和唯一Job

演示输入对象保存 `(tenant, owner, id)`、当前输入revision、准确text和必要创建/更新信息；同一对象唯一工作阶段命名 `project`。这个阶段的业务含义是“将指定输入修订投影为可核验的同库处理观察”，不是外部动作或Task执行。

首次成功record建立输入revision=1，并在同一事务建立原Job：work_revision=1、completed_revision=0、state=ready、due_at为本次可信接纳时点。Job ID由owner一次生成并持久保存；唯一键为owner范围内的业务对象+阶段。

新的显式record命令更新同一对象时，输入revision增加1，同一Job的work_revision增加1；该演示只有一个阶段，因此二者对应同一输入修订。即使text恰好与旧值相同，只要是通过新command_id及准确expected_revision接纳的新修改，也产生新修订；不要额外发明按正文去重。原command_id重传、格式/权限失败、固定拒绝均不增加任何修订。

首票只交付真实待处理Job和Host观察，不提前实现worker或伪造completed结果。03/04实现project时，在Claim领取事务中取对应输入修订及text的准确快照；事务外计算该text的UTF-8 SHA-256，结果事务保存 `{input_revision, text_digest}` 处理观察并推进完成修订。输入对象的revision描述输入变更，派生处理观察有自己的input_revision绑定；不为更新派生观察偷偷增加输入revision。

本演示是最新输入投影：尚未领取的中间输入可被后续输入取代，领取最新修订即可覆盖先前未领取的投影需要；不承诺每个输入修订都被物理执行一次。原回执仍证明当时record已提交。已经领取的旧快照提交只能记作该旧修订，不能声称已处理新text。新worker接替重新领取当前work_revision，不要求恢复旧进程栈。

新触发不能覆写existing Job ID/完成修订；到03/04时，更新work_revision还必须保留当前有效Claim/epoch，不通过粗暴把leased改ready绕过隔离。旧Claim完成发现较新work_revision时，使剩余工作可继续。这沿用既有决定，无需首票预建lease字段。

Host观察返回当前输入及Job/处理观察的准确修订关系；public command.get只返回固定回执和progress none，不伪造TaskProgress。none不能被解释为处理完成。

## 3. 成功点和固定前态拒绝

**本方法成功回执选择applied。** durable_work.record的成功点是准确输入修改、固定回执和后续Job已经共同提交；投影处理尚未完成不改变这个成功点。回执提供原command_ref、durable_work对象引用和新输入revision，next_action可固定为query_original。若object_ref也包含revision，使用同一新输入revision。

applied不表示project已完成、Task succeeded或任何外部效果。accepted类型继续属于公共合同，但本演示不用两个状态表达同一成功点。这里是首票明确方法语义，不更改公共类型。

前态规则如下；Schema允许expected_revision可选，状态前提由事务中准确对象事实决定：

| 对象状态 | 请求expected_revision | 决定 |
|---|---|---|
| 不存在 | 省略 | 创建revision1并applied |
| 不存在 | 任意已提供值，包括0 | 固定rejected / revision_changed |
| 已存在 | 精确等于当前输入revision | 新修订并applied |
| 已存在 | 省略或不同值 | 固定rejected / revision_changed |

不把缺省expected_revision静默填0或填当前值；创建和修改使用同一方法，但不得成为无条件upsert。前态拒绝在新原命令键下固定保存，事务不产生业务修改或新Job；之后对象状态变了，同键原内容仍返回原拒绝。确需重新决策时使用新command_id，不能改旧expected_revision重发。

检查顺序：严格格式/当前受信权限/owner绑定 -> 原键锁与摘要比较 -> 已有原决定直接返回 -> 新命令截止 -> 当前对象前态 -> 原子提交。新的过期命令先固定expired，不借此查询并泄露对象当前版本。rejected无object_ref/revision，next_action可为resolve_rejection。

int64修订必须准确检查。已达最大修订而新修改无法表示下一值时，固定拒绝unsupported，不溢出、回绕或截断，也不发明新公共错误码。

## 4. 受信主体、重传与查询权限

本演示不建立完整授权领域，也不凭payload或可读对象名称推断权限。采用Host明确注入的最小允许表：**准确SubjectBinding + 准确OwnerRef + 操作集合**。操作只需record/read；完整主体绑定包括有序delegation_chain。允许表是测试/Host可信配置，不是线上payload，未知主体默认拒绝。

record入口先确认当前主体获准对该owner record、主体tenant与target相符、Host实际绑定owner等于target，再查命令键。授权失败不建立命令接纳记录，避免攻击者占用任意原ID。认证结构与委托链由受信Host提供；这不声称真实凭据/委托链验证服务已实现。

授权以owner范围为边界，这是显式的演示策略：获准read的主体可以查询该owner的命令，不局限于命令原创建者；未获准read的主体对存在与不存在均先返回forbidden。沿用现有ReadAuthorizer，授权不依赖先读命令事实，不新增私有对象ACL系统。Host观察演示事实采用同样read权限。

为验证改主体复用命令键，可明确配置两个均有record资格的主体；第二主体使用第一主体的command_id及相同业务payload仍因主体摘要不同得到idempotency_conflict，不能返回原敏感正文或覆盖原决定。若第二主体本来无权限，则应在更早处forbidden，不能要求它越权进入去重分支。

权限已撤销的原主体不能靠“原命令重传”绕过当前准入权限；原决定本身保持不变，可由仍获read权限的主体经command.get读取。身份/权限检查与先查原键再查期限不矛盾：前者在任何业务接纳前，后者控制已获准请求的去重裁决。

public command.get仍使用准确1.0.0/read契约、有限context、当前查询自己的accept_before和现有OwnerDirectory。内部写命令的版本不同，不妨碍按原CommandRef读取其固定回执，因为当前CommandRef不限定原方法版本。目录仅解析请求指定owner；未找到/不可用不能回退默认owner、延长原期限或新建同义命令。

## 5. 真实v1 writer保留的最小方法

01/02完成时冻结：v1迁移脚本及checksum、实际Host writer对应的不可变Git代码版本、驱动/数据库版本、精确生成命令、输入命令语料和输出fixture摘要。输入至少生成一个applied且Job待处理的对象、一个固定expired拒绝，再重传原命令证明唯一责任及原决定保持。

优先保留**真实v1 writer执行后导出的便携数据库fixture**（专用测试范围内的SQL dump或等价完整逻辑导出），同时保留通过准确历史writer版本重新生成的说明/脚本。07普通测试可从已提交fixture恢复真实v1状态，再应用实际v2领取迁移；重建溯源时在独立临时源码目录构建历史Git版本，不能修改当前工作区。不得使用v2程序手填旧表/假记录取代该writer来源。

不要求产品长期保留一个legacy-v1写API，也不让正常make test每次下载Git历史。fixture生成和版本出处必须可审计；准确导出工具/格式、fixture文件布局以及如何在CI校验来源由实施者选最小可重建方案。敏感正文只用固定假数据，不能导出个人环境smoke库或其他租户数据。

03/04真正增加Claim字段及必要约束形成v2；07基于上述v1输入验证升级、重开、原键查询/重传及继续领取完成。不得把01先预建全部lease列、03仅改空版本号当升级路径。

## 6. test-integration配置与清理边界

新增明确的测试配置名（建议 `LERNA_TEST_POSTGRES_DSN`）；仅由入口读取并注入Host，领域/runtime不得自行读取环境。仓库文档仅提交占位值，错误和日志不输出连接串、密码或完整环境。

真实PG套件使用专用测试数据库连接；每次运行创建唯一命名且明确归属该测试的schema或数据库范围。优先在专用测试数据库内创建独立schema，清理只删除本轮实际创建并登记的范围；不DROP调用者指定数据库、不碰环境smoke数据。SQL标识符必须由安全内部名称生成并正确引用，业务参数继续参数绑定。

缺少配置、连接失败、迁移失败、数据库特性或工具依赖缺失都使 `make test-integration` 明确失败，不skip绿灯。第一票仅交付PG，入口与CI准确声明PG范围；02引入SQLite后扩为两库必跑，不能继续悄悄忽略SQLite。

基础make check继续无外部服务；新增存储代码可编译并有局部逻辑测试，但真实DB证据只来自integration。PG CI服务使用锁定并实际验证的版本；权限、隔离级别、synchronous_commit和有限SQL/context期限逐套件核验。不同进程采用同一显式测试配置，所有子进程与临时范围都有有限清理。

数据库自身事务I/O当然需要连接；“事务内禁止网络”是禁止数据库之外的网络/模型/上传/用户等待。PG用于接纳截止与后续lease的可信时间沿用数据库权威时间，测试以受控时钟/同步点注入证明边界，不用人为长sleep。

## 7. 留给实施者的实现细节

下列事项不预造新接口或再请求用户决策：实际包/文件位置、Host启动与观察函数名、适配器私有Tx token实现、SQL表/索引布局、确定锁序的具体语句、是否缓存schema编译、fixture导出格式和临时schema命名、驱动准确锁定版本、可重复故障同步点实现。选择必须满足上述行为、真实并发及迁移证据；发现确切冲突再通过主任务委派决策。

通用严格JSON、准确类型、CommandDigest、固定CommandReceipt、读授权与OwnerDirectory已足够复用，不再建设第二套公开合同、通用仓储/事务ORM、Grant服务或Task模型。首票无需后续lane/等待/Claim产品实现即可正确持久接纳。
