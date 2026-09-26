"""Build an auditable, partial-coverage inventory from current source rows."""
import json,re
from pathlib import Path
HERE=Path(__file__).resolve().parent
import sys
sys.path.insert(0, str(HERE.parents[1]))
from archive_paths import historical_name, resolve_source

BASE='docs/architecture/'
def src(path,line):return {'path':BASE+path,'line':line}
def model(name,*props):return {'model':name+'.tla','properties':list(props)}
A=model('Authorization','Attenuation','NoUnsafeUse','NoUnsafeDisclosure','OnceUnique','ReceiptBinding')
V=model('RecoveryView','ReadyComplete','ValueBound','NoBadApply','NoStaleRead')
S=model('SourceGovernance','ClosureCoverage','RegisteredBeforePublish','NoUnsafeUse','PhysicalEvidence','NoFalseComplete')
C=model('MemoryCommand','ClosedNeverCommits','CASRespected','NoResurrection','OncePerCommand','OutcomeFixed')
L=model('Lineage','CompleteClosure','NoSelfDependency')
X=model('ExtractionPolicy','NoUnsafeTrigger','OnceTrigger','SeparateBudget')
I=model('IndexSearch','IndexAligned','NoStaleResult','NoFalseComplete')
G=model('LogRetention','ConsumerCoverage','RebuildRetained')
B={'model':'TrustRules.lean','properties':['bootstrap_requires_current_binding','bootstrap_never_authorizes_action','bootstrap_never_authorizes_body','bootstrap_never_authorizes_confirmation','bootstrap_never_authorizes_delegation','bootstrap_never_authorizes_foreign_query']}
F={'model':'TrustRules.lean','properties':['offline_interval_is_conservative','offline_no_rollback_evidence_blocks','offline_lost_anchor_blocks','offline_insufficient_budget_blocks','offline_expired_blocks']}
H={'model':'TrustRules.lean','properties':['confirmation_reachable_safe','confirmation_at_most_once','confirmation_binding_fixed','changed_confirmation_binding_cannot_issue','consumed_confirmation_cannot_reissue','rejected_confirmation_cannot_issue']}
mechanisms=[]
def mech(id,title,path,line,rule,mappings,uncovered,runtime,lean=()):
 mechanisms.append({'id':id,'title':title,'sources':[src(path,line)],'properties':[rule],
   'coverage':'partial-model' if mappings or lean else 'not-formalized','formal_mappings':mappings,
   'lean_theorems':list(lean),'uncovered_details':uncovered,'runtime_dependencies':runtime})
mech('TR-01','受信身份、用户、行动者和处理者绑定','identity-and-authorization/contracts.md',10,'其他用户、actor、processor 的请求不能成为本主体的新使用',[A],['载荷伪造检测、对象存在性不泄露、会话到期、资源路径规范化、每种缓存键'],['受信身份入口、宿主actor隔离、网络认证、文件和GUI实际对象绑定'])
mech('TR-02','单父链委派范围只能收缩','identity-and-authorization/mechanisms.md',25,'每级子范围为父范围子集；任意级链不超根范围',[A],['多字段条目整体包含、委派深度上限、单次父禁止再委派、祖先撤销查询、不可判定包含拒绝'],['安装目录与资源选择器包含关系的可信实现'],['scope_reachable_attenuates'])
mech('TR-03','预检缓存与实际消费/披露分离','identity-and-authorization/mechanisms.md',43,'权威撤销、版本变化、到期后旧缓存不能产生新消费或披露',[A],['deny与indeterminate区分、短期远程start_before竞争窗口、参数/obligations完整检查'],['使用边界真实执行门禁，跨权威传播窗口不为零'],['gate_reachable_safe','revoked_cannot_add_use','stale_cache_cannot_add_use'])
mech('TR-04','单次占用永久绑定原操作','identity-and-authorization/contracts.md',96,'预检不消费；原操作重入不重新分配；两个操作不能同时占用',[A],['完整意图摘要比较、source_slot共享原截止、无上限操作集合的once归纳、持续使用单元生成规则'],['原子唯一占用、处理端固定意图记录'])
mech('TR-05','撤销、历史事实和新行动分别处理','identity-and-authorization/mechanisms.md',79,'撤销后可接收已有操作最小事实，同时禁止新行动',[A],['实际查询/控制权限与正文披露权限的独立记录、传播位置、远端效果和本地接管'],['最小事实通道认证；外部效果独立核验'],['late_fact_without_new_use_witness'])
mech('TR-06','离线许可、可信时间与防回滚','identity-and-authorization/mechanisms.md',112,'许可/来源/任务最早截止约束整个可信时间区间；预算/时间锚/防回滚条件不足不能新使用',[A,F],['时间区间取信与单调增量估算、休眠误差、离线证明唯一消费端及重启恢复组合未建模'],['可信单调时间、快照外防回滚、硬件/监督器隔离、JWS密钥与撤销传播'])
mech('TR-07','授权连续恢复','identity-and-authorization/mechanisms.md',139,'固定快照切点后完整分页和连续区间成立才发布本轮视图',[V],['页manifest/hash真实性、授权祖先集合、密钥/策略版本、恢复租约和日志截断处理、跨领域门禁组合'],['一致快照、日志保留及持久应用'])
mech('TR-08','受限恢复引导与本人管理分离','identity-and-authorization/cross-endpoint.md',8,'恢复白名单不授予action/正文/签发/确认，绑定和允许集合必须当前成立',[B],['currentBinding整体Bool尚未展开完整endpoint/owner/ledger元组；management-grants近期认证及安装管理目录尚未模型化'],['认证接入与部署目录、真实用户认证、hostOnly和集合包含判定'])
mech('TR-09','受信确认与用户所见范围绑定','identity-and-authorization/cross-endpoint.md',75,'确认完整绑定不可变且只签发一次；更换任意绑定字段、已消费或拒绝不新签发',[H],['单确认参数归纳；多confirmation/command命名空间、new confirmation流程、拒绝历史回执与并发未知提交尚未组合建模'],['真实用户presence、可信宿主UI、完整元组规范化/签名/nonce实现、消费签发原子提交'])
mech('TR-10','固定权威、事务记录及最小审计保留','identity-and-authorization/mechanisms.md',177,'正确性记录不因缓存淘汰或新任务过载丢失',[],['授权事务未知恢复、历史清理与旧账本封闭、配额/公平调度尚未在本组模型化'],['数据库耐久、唯一写者/锁、备份防回滚、审计不泄密'])
mech('TR-11','带类型/版本/时间/范围的不可变记忆','memory-system/contracts.md',8,'事实、偏好、推断、经验保留来源与保证强度',[C,L],['类型提升禁止、置信度校准、业务适用时间与保留期、自然语言冲突/具体范围优先级尚未模型化'],['可信提取依据和任务效果评测'])
mech('TR-12','完整来源闭包与自依赖拒绝','memory-system/contracts.md',24,'使用过的输入闭包不可按模型引用缩小；目标旧修订参与输入时不可替换目标',[L],['TLC固定4个源token/两类目标，未验证一般DAG展开、循环检测与闭包超限算法'],['受信快照必须捕获真实全部输入；证明解析与参数规范化'],['closed_dependency_blocks'])
mech('TR-13','候选索引回权威复核','memory-system/mechanisms.md',10,'索引候选与尾部补扫均在返回前核对当前版本和许可；并发变化不冒称完整',[I,A,V,S],['TLC仅2记录/3次变更、match-all查询；用户/用途/来源的完整跨模型组合和实际缓存键未模型化'],['实际索引适配器和数据出口门禁'])
mech('TR-14','索引水位补漏与确定性有界排序','memory-system/mechanisms.md',26,'连续补扫W..S，未补齐或切点变动则partial；删除抑制旧候选',[I],['排序/冲突组规则、短查询扫描数值预算、分页绑定尚未模型化；complete仅声明match-all有限实例的集合覆盖'],['FTS5中文/短词语义、容量与召回实测'])
mech('TR-15','记忆命令CAS/原决定/关闭竞争','memory-system/mechanisms.md',49,'预期修订同域裁决；先关闭永不提交，已提交原决定不被关闭改写',[C],['模型固定2命令/最多2次修改；完整规范意图冲突、prepare崩溃/提交未知和使用回执两域组合未建模'],['数据库唯一键、CAS与回执/维护工作的原子提交'])
mech('TR-16','墓碑不可复活','memory-system/mechanisms.md',76,'删除后的旧修改不能重建原记录',[C],['新用户意图用新recordID、原命令/墓碑清理与旧身份封闭未建模'],['旧备份恢复前的关闭尾部与隔离证明'])
mech('TR-17','派生失效不等待反向清理','memory-system/mechanisms.md',91,'任一真实依赖关闭即停止派生新使用，物理字节仍在也不得使用',[S,A],['多层动态依赖图与跨端有限新鲜度/断连窗口未建模'],['来源当前证明、受控模型/存储出口'],['closed_dependency_blocks','closed_cannot_add_use'])
mech('TR-18','逻辑禁用和物理清理独立','memory-system/mechanisms.md',112,'无平台处理依据不报告物理完成；关闭清单覆盖所有已登记受影响持有者',[S],['载体逐项期限、WAL/FTS/缓存/备份清单、离线端截止、不可改备份到期未建模'],['真实平台清理回执，介质删除不能由模型证明'])
mech('TR-19','控制删除不依赖旧正文继续可读','memory-system/mechanisms.md',49,'来源失效不阻止获准管理关闭和清理责任',[S,C],['模型Close不读正文，未建模管理权限与restrict可证明收紧'],['独立受信管理资格与实际删除接口'])
mech('TR-20','获准只读视图与领域写权威分离','memory-system/synchronization.md',10,'投影不获得修改权，当前使用重新核验权威版本',[V,A],['字段选择/用途/接收方变更需新view代次、多用途不可拼接未完整建模'],['身份隔离、投影视图过滤与外发限制'])
mech('TR-21','快照暂存/连续补齐/可见代次切换','memory-system/synchronization.md',42,'两页完整后追到H；空变更页也证明覆盖；不连续页和旧轮owner不能推进',[V],['完整快照原子替换旧成员集合、多记录remove、manifest/hash、日志裁剪/租约并发未建模'],['一致读切点、连续日志、持久游标与数据同提交'])
mech('TR-22','在线视图恢复不产生永久使用资格','memory-system/synchronization.md',90,'权威变化后旧ready视图也须复核并拒绝旧版本新读取',[V,A],['离线来源新鲜度签发、旧编辑上传CAS和本地待提交意图组合未建模'],['可信时间、防回滚、源在线状态与有限离线授权'])
mech('TR-23','索引重建和变化日志保留','memory-system/mechanisms.md',42,'日志不能越过任一有效view/index/rebuild/query消费位置；失效且保存重建责任后才能释放，追平后恢复索引',[G],['四消费者/3变化；真实代次/旧读者释放/页应用幂等与崩溃组合、按字节回收策略未建模'],['索引适配器原子水位、资源限额和存储维护'])
mech('TR-24','有限读取单元和冻结once结果','memory-system/mechanisms.md',129,'动态Search不接受once，固定Read重投不得换版本/来源/窗口',[A],['仅映射抽象once占用，冻结字节、查询阶段/返回阶段身份、处理器转移、预算不刷新未模型化'],['可信冻结存储、原请求身份、内容字节完整性'])
mech('TR-25','独立opt-in提取及预算','memory-system/contracts.md',116,'仅已同意的成功任务触发一次，撤权/关闭策略后旧候选不能新触发，不借原任务预算',[X],['触发键含task/revision/policy版本、独立提交未知恢复、部分候选取消和提取结果类型未模型化'],['策略本人认证、真实任务终态、预算账本与来源闭包'])
mech('TR-26','数据驻留与派生产物限制继承','memory-system/mechanisms.md',137,'派生为摘要/向量/布尔结果不产生新的用途权限',[A,L],['具体位置/接收方/保留约束复合包含，云端备用路径和插件网络封锁未模型化'],['实际存储/进程/日志/网络强制隔离'])
mech('TR-27','字节、来源、授权和业务采用分别裁决','content-and-provenance.md',10,'保存字节不证明当前来源或用途；业务事实不因字节缺失被抹除',[A,S,L],['内容ID、SHA-256、字节范围/截断与固定观察绑定未模型化'],['内容hash校验、可信来源适配器、实际观察证据'])
mech('TR-28','先有限保留再发布领域引用','content-and-provenance.md',63,'发布前字节、保留及来源持有者登记已经成立',[S],['保留deadline覆盖恢复期、孤立上传、两域提交未知、期限耗尽/原引用不可替换尚未模型化'],['内容持久保存与有限保管承诺'])
mech('TR-29','持有者登记与源关闭串行竞争','content-and-provenance.md',145,'关闭先发生则拒绝登记；登记在先必被纳入关闭清单',[S],['跨来源部分登记的孤立holder清理、多层反向传播、分页水位与proof验证未模型化'],['来源权威串行提交、全部真实持有者受控登记'])
mech('TR-30','清理正文后最小依据防复活','content-and-provenance.md',164,'最小去重依据必须删前先封闭旧身份/账本接纳',[],['退休身份与恢复旧备份的关闭尾部尚未模型化'],['不可回滚关闭记录、备份恢复流程、接收端强制执行'])
mech('TR-31','过载和有限恢复不丢既有责任','memory-system/mechanisms.md',152,'空间不足拒绝新接纳，控制/清理保留份额，重试不无限',[],['多用户配额、公平调度、有限工作领取、重试耗尽和通知去重未在本组建模'],['负载测试、数据库资源和实际调度'])
mech('TR-32','协议版本、规范化器和未知类型拒绝','identity-and-authorization/contracts.md',126,'已安装Schema、版本与规范化器固定；未知安全约束不可忽略',[],['精确字段映射、兼容协商及各种JSON规范化未模型化'],['静态Schema检验与双实现互操作运行验收'])

# Explicit mappings: every numbered acceptance row is preserved verbatim.
# tuple: model mechanisms, abstract aspect covered, residual formal detail, real dependency
ia={
1:([A], '不同身份类别不能获新消费/披露','所有缓存键、回执及存在性侧信道','真实认证、存储用户隔离'),
2:([A], 'otherActor和otherProcessor与owner分离','宿主代表关系与日志归因','宿主actor隔离'),
3:([], '', '连接票据单次消费、welcome丢失和重新取票据','票据存储与会话适配器'),
4:([A,H], 'Evaluate不消费、BeginUse唯一绑定；完整确认绑定一次签发','写入与读回的不同许可及效果、确认提交未知','用户确认入口、执行端效果'),
5:([A], '不同用途和主体不能沿旧资格消费','完整参数、显示摘要、target与symlink绑定','资源规范化、打开时防对象替换'),
6:([A], '集合范围只能收缩、撤销后旧缓存不能使用','期限/地点/保留多字段含义及祖先链查询','许可链解析和资源包含判定'),
7:([A], '两个原操作竞争once、相同操作重入不重分配','100次重投、slot统一期限、下一单元分配','原子占用、驱动有限单元'),
8:([A], '旧预检不替代当前判定、到期不再新消费','提交未知、只读未见、授权回执的原start_before','同权威提交回执恢复'),
9:([], '', '授权后执行启动前后崩溃与原操作核对未组合','执行驱动、数据库崩溃注入'),
10:([A], '撤销与消费交错后新消费受限','远端传播与实际启动窗口、已知断网门禁','网络/端点传播和真实驱动'),
11:([A], '撤权后接收历史事实、占用不退回','管理查询权/正文读回权与未知效果恢复','原执行查询和最小控制授权'),
12:([A], '当前权限再次校验披露、旧缓存不能披露','历史正文最小化投影和实际带敏感载荷','数据出口/日志隔离'),
13:([F], '保守区间全域在有效期内；缺时间锚/防回滚依据即不准使用','单调计时含休眠的实际误差与跨重启信任建立未建模','可信时间、防回滚硬件/外部监督器'),
14:([A,F], '最短截止越过后不新增使用；时间/控制/预算等条件合取','离线撤销传播窗口、重连恢复与上传重新授权','可信时间、在线恢复和外发门禁'),
15:([V], '完整页和覆盖区间、旧轮/owner增量拒绝','manifest、授权过滤集合、日志截断、快照时旧正文撤权','一致快照及可靠日志保留'),
16:([], '', '活动owner接替、离线旧实例隔离','进程/网络/执行入口fencing'),
17:([], '', '本地接管屏障与云离线、重开意图','GUI驱动真实动作/中断'),
18:([A,L], '用途收缩、全部真实输入闭包不得遗漏','日志/评测/记忆各用途目录、义务可执行性','受控模型和持久数据出口'),
19:([], '', '用户公平性、控制预留和历史回执保留','负载/存储压测'),
20:([C], '抽象已关闭原命令不能迟到提交','授权旧账本/消费记录恢复及退休身份','备份连续性、唯一权威隔离'),
21:([B], '恢复白名单不能换正文/新行动/签发/确认/其他命令查询；绑定/host/子集/新鲜度必要','Bool整体绑定未展开完整元组、发行原命令与响应未知','认证接入/恢复适配器'),
22:([H], '完整绑定不变、不同proof绑定拒绝、一次签发与已消费重入','跨用户/多确认的命名空间与未知提交恢复未组合','真实presence、可信显示/签名入口及完整绑定规范化'),
23:([V], '切点连续覆盖与旧owner增量拒绝','跨端完整proof编码、快照owner绑定与日志保留','两端适配器互操作'),
24:([], '', 'JWT typ/alg/kid/aud和父链/时间锚校验','成熟密码实现及密钥管理'),
25:([S], '逻辑关闭不能当物理完成','离线最晚使用期限、各端传播位置','设备真实执行和介质清理'),
26:([], '', 'management引导、近期认证与Agent分离','本人管理认证入口'),
}
ms={
1:([A,C], '获准用途和记录修改的抽象规则','偏好适用范围对任务输出的真实影响','端到端任务对照评测'),
2:([], '', 'fact/preference/inference/experience保持和当前指令优先','提取模型质量及事实依据'),
3:([A], '未批准用途不允许抽象使用/披露','读取/提取/保存/向量API/同步真实动作绑定','处理端强制出口'),
4:([L], '实际private/public输入闭包不能由声称引用缩小','一般循环与超限闭包算法','受信输入捕获'),
5:([A], '不同用户不得抽象使用/披露','各种键、计数存在性、游标和配额隔离','存储/缓存/日志的用户命名空间'),
6:([], '', '短词分词、有限扫描、partial和同义召回','FTS5运行及检索评测'),
7:([I,V,A], 'W..S逐步补扫、删除抑制旧候选、当前修订/许可复核；缺覆盖标partial','一般查询算法召回/排序和多来源当前资格组合','索引/权威实际集成'),
8:([C], '两命令CAS竞争仅一条成功；原结果固定','同命令异参数规范化比较','数据库并发与唯一约束'),
9:([C], '抽象提交与结果状态共同变化','prepared/提交未知、答复丢失与晚事务查询','数据库故障注入和业务回执'),
10:([C], '关闭早到阻止迟到提交，提交在先保持结果','网络失序与业务接口适配','原子命令裁决'),
11:([A], 'once固定占用、processor不同被拒','once Search拒绝、冻结Read字节、原窗口不刷新','冻结存储与相同主体处理链'),
12:([A,V,S], '来源/版本变化后旧缓存拒绝新使用','每个模型/准入/发布阶段出口调用的实际连通','跨模块来源适配器'),
13:([S,L], '真实依赖关闭即阻止新使用，与清理完成无关','任意动态深链、不可达source_gap三值语义','当前来源证明与网络失败'),
14:([C,V], '墓碑阻止旧修改，旧视图不能代替当前版本','原命令规范正文清理、墓碑保留和所有旧消息通道','备份/索引恢复'),
15:([S], '逻辑关闭与平台回执/物理完成分离','完整载体清单和到期重启恢复','FTS/WAL/磁盘/备份真实清理'),
16:([V], '未完整页不可见，R..H连续覆盖后发布','旧快照正文撤权强制重开、真实manifest','一致快照及页面传输'),
17:([V], '空变化区间可覆盖、乱序不推进、旧owner增量拒绝','日志截断、重复页原回执查询、快照摘要绑定','可靠日志与恢复适配器'),
18:([A,V,S], '权限与数据版本独立变化时读取重核、清理另报','remove应用回执丢失/旧代次查询','受管副本恢复与当前证明'),
19:([A], '到期阻止新使用的离散抽象','可信时间/防回滚/来源租约、重连复核','真实离线授权环境'),
20:([A,L], '派生不丢来源，未获准用途拒绝','本地/云模型选择与布尔/向量结果策略','网络/进程强制隔离'),
21:([], '', '扫描限额、全局句柄、后台清理公平与partial','负载和资源压测'),
22:([], '', '旧备份/账本丢失与旧进程重新启动接纳关闭','唯一实例隔离与备份连续记录'),
23:([G,V,I], '四类消费者共同约束日志回收；先失效并登记重建责任，追平后恢复索引；索引内容水位同步推进','完整代次与旧读者释放、apply重复回执、崩溃恢复和跨模型组合','索引适配器事务/崩溃测试'),
24:([C], '每个原命令结果固定且不会二次应用','多项提取取消/结算及原结果交回','任务/执行/记忆集成'),
25:([], '', '跨实现语义对照、召回和个性化收益','双实现互操作及效果/性能评测'),
26:([L,C], '实际读旧目标版本则自依赖拒绝，独立来源可保存','一般来源图、可信实际输入与Mutate组合','提取上下文和CAS真实实现'),
27:([S,C], '关闭不以旧正文可用为前提','管理权限/restrict子集判断、不可达三值处理','受信管理接口'),
28:([V,L,C], '连续页/轮次owner、完整闭包与CAS抽象','原意图完整比较、编码互操作和应用证明','两种实现交换'),
29:([S], '登记/关闭串行覆盖，未登记不能发布','跨源部分成功和孤立holder处理','真实持有者注册覆盖'),
30:([S], '无物理回执不报告清理完成','离线截止、不可改备份生命周期和永久失联','真实平台回执与保留政策'),
31:([X], '成功+optin+当前许可触发一次，使用独立预算','多任务/多策略修订的完整触发键','应用策略和预算账本'),
32:([X,A], '缓存候选遇关闭/撤权不新触发，原任务预算不变','触发提交未知/崩溃恢复和额度耗尽管理','提取任务持久接纳与当前授权'),
33:([L,S], '自依赖/来源遗漏拒绝，清理不冒称已完成','供应方不能满足必要清理时接纳前拒绝','外部供应方能力验证'),
}
cases=[]
for folder,prefix,mapping in [('identity-and-authorization','IA',ia),('memory-system','MS',ms)]:
 path=BASE+folder+'/validation.md'
 for line,text in enumerate(resolve_source(path).read_text().splitlines(),1):
  match=re.match(r'^\|\s*('+prefix+r'-(\d+))\b',text)
  if not match:continue
  id,n=match.group(1),int(match.group(2));models,covered,unmodeled,dep=mapping[n]
  cases.append({'id':id,'source':{'path':path,'line':line},'original_row':text,
    'classification':'model-property' if models else 'runtime-required','coverage':'partial' if models else 'none',
    'model_mappings':models,'covered_abstraction':covered,'uncovered_formal_details':unmodeled,
    'runtime_required':dep,'whole_acceptance_case_verified':False})
assert {c['id'] for c in cases}=={f'IA-{n:02d}' for n in range(1,27)}|{f'MS-{n:02d}' for n in range(1,34)}
content=BASE+'content-and-provenance.md'
cp=[([S,A,L],'发布有字节/保留/登记/来源条件','字节摘要、真实观察、原操作和模型出口'),([S],'先保留后发布的抽象顺序','上传丢回执、领域提交未知和原关联查询'),([S,A],'关闭后旧缓存不新使用','实际到期、在途有限披露与领域缺口'),([S],'关闭与登记串行、清理另证','retain并发/清理重启/精确载体'),([A,V],'版本变化后缓存不代替权威','来源不可达三值、网页新鲜度和实际读取范围'),([A,C],'异主体不能使用、旧命令不复活','旧备份、真实摘要和来源绑定校验')]
supplement=[]
for i,(models,covered,remaining) in enumerate(cp,1):
 line=115+i;text=resolve_source(content).read_text().splitlines()[line-1]
 supplement.append({'id':f'CP-LOCAL-{i:02d}','id_origin':'本库存为原文无编号验收行分配的本地编号，不是架构规范ID','source':{'path':content,'line':line},'original_row':text,'classification':'model-property','coverage':'partial','model_mappings':models,'covered_abstraction':covered,'uncovered_formal_details':remaining,'runtime_required':'真实内容/来源/身份/存储适配器和故障注入','whole_acceptance_case_verified':False})
headings=[]
for p in sorted([*(resolve_source(BASE)/'identity-and-authorization').glob('*.md'),*(resolve_source(BASE)/'memory-system').glob('*.md'),resolve_source(content)]):
 for line,text in enumerate(p.read_text().splitlines(),1):
  if re.match(r'^#{2,3} ',text):headings.append({'path':historical_name(p),'line':line,'heading':text})
data={'schema_version':1,'group':'trust','scope':[BASE+'identity-and-authorization',BASE+'memory-system',content],
 'coverage_policy':'model-property只覆盖所列有限抽象或Lean命题，不代表完整运行用例通过；uncovered_formal_details与现实依赖分别列出。未使用相似抽象把整项标为已验证。',
 'mechanisms':mechanisms,'validation_cases':cases,'supplemental_cases':supplement,'source_sections':headings,
 'summary':{'mechanisms':len(mechanisms),'numbered_validation_cases':len(cases),'local_content_cases':len(supplement),'full_runtime_cases_verified':0}}
(HERE/'inventory.json').write_text(json.dumps(data,indent=2,ensure_ascii=False)+'\n')
print(data['summary'])
