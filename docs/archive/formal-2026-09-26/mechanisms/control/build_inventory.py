"""Rebuild explicit contract-to-property inventory; does not infer coverage from words."""
import json,re,pathlib,collections
HERE=pathlib.Path(__file__).resolve().parent
import sys
sys.path.insert(0, str(HERE.parents[1]))
from archive_paths import resolve_source
D='docs/architecture/'
def src(p,n):return {'path':D+p,'line':n}
P={
 'receipt':('ControlRecovery.tla',['CommitKnowledge','NoUnknownDispatch','DurableExecution'],'核心提交、远端接纳分别提交；Unknown/未见不产生派发；接纳保存后续责任'),
 'cancel':('ControlRecovery.tla',['CancelWins','LocalControlGate'],'取消先提交阻断新准备；远端收到控制后阻断首次物理启动；迟到真实效果仍允许'),
 'lease':('ControlRecovery.tla',['CurrentLease','PhysicalSendQualified'],'过时代次不能取得新准备资格；实际发送切点必须仍持原代次且未丢失原发送资格'),
 'unknown':('ControlRecovery.tla',['EvidenceNotGuess','SafeNewAttempt'],'未见保持未知；仅可信封闭且确认无效果才能建立新尝试'),
 'decision':('Decision.tla',['AdmissionAndRepair','FiniteConsumption'],'核心重查业务版本、轮次资格、暂停/取消、固定条件和前提；模型自评不能替代'),
 'calls':('Decision.tla',['OneSendPerCall','FiniteConsumption','ResponseCorrelation','ResponseOrigin'],'work/attempt 持久身份不重发；返回先按原身份保存、只有匹配当前轮/尝试才应用；轮数与修复有界'),
 'repair':('Decision.tla',['AdmissionAndRepair'],'仅已结束、保存输出、用量已知且资格仍有效时进入下一次修复'),
 'budget':('Budget.tla',['Conservation','UnknownHeld','SealedRelease','SeparateCleanup'],'预留、结算、可用份额总量守恒；任务结束不释放未知；可信封账后释放；收尾额度独立'),
 'usage':('Budget.tla',['CumulativeSettlement','SingleChargePath','ConflictFreezesSettlement','ConflictFreezesRelease','ConflictFrozenAmounts'],'累计只结算增量；重复/旧消息不双计；可信同修订异累计冲突冻结后不再结算/释放且金额保持；根只计叶调用一次'),
 'tree':('Budget.tla',['Hierarchy','Conservation','SingleChargePath'],'有限内部委派树从现有份额转移，层级受限，无环，不重复计算父汇总'),
 'binding':('Boundaries.tla',['FixedBinding'],'精确能力版本与原意图在接纳后固定；升级及异意图重投不改绑定'),
 'tombstone':('Boundaries.tla',['NoIdentityResurrection','RecoverableIdentity'],'清理正文仍保留去重身份；仅入口封闭后删墓碑；旧键不复活'),
 'pause':('Boundaries.tla',['PauseReasonsIndependent','ActionBoundary'],'祖先/自身暂停原因独立；子动作检查权威当前暂停而非滞后投影'),
 'gui':('Boundaries.tla',['ActionBoundary'],'观察版本必须在实际动作切点仍匹配环境；陈旧观察阻断'),
 'facts':('Boundaries.tla',['FactRevisionDoesNotRegress','ConflictRetained','FixedHistoricalResult'],'旧修订不覆盖；同修订/相反结论冲突持久；历史固定答复与当前事实分离'),
 'identity':('CoreContracts.tla',['GlobalUserKey','FixedAuthority'],'同用户操作键跨任务/种类唯一并只在固定权威接纳；不同用户可用同裸键'),
 'input':('CoreContracts.tla',['OneInputConsumer','InputVersion'],'同请求至多一个消费；新请求版本拒绝旧输入；UI文字刷新不消耗请求'),
 'batch':('CoreContracts.tla',['WholeBatch'],'双操作批次参数和前提必须整体满足；拒绝不留操作或预留'),
 'adjust':('CoreContracts.tla',['AdjustmentFloor','ManagementOnce','InternalShare','ExternalContractFixed'],'管理增额幂等；不低于已用加未知预留；内部份额共同变更；外部原合同不扩额'),
}
lean={
 'receipt':['reachable_core_safe','submission_unknown_never_dispatched'],
 'cancel':['cancel_blocks_new_prepare','cancellation_is_monotone'],
 'lease':['stale_lease_blocks_new_prepare'],
 'budget':['reachable_budget_safe','unknown_allocation_not_released','cancellation_does_not_create_allocation'],
 'usage':['cumulative_charged_once','cumulative_repeat_has_zero_delta','conflict_freezes_account'],
 'tree':['share_transfer_preserves_total']}
checks=json.loads((HERE/'checks.json').read_text())['checks']
check_refs={
 'receipt':['control-safe','control-witness-unknown-commit'],
 'cancel':['control-safe','control-negative-cancelGate','control-negative-remoteGate','control-witness-late-effect'],
 'lease':['control-safe','control-negative-leaseGate','control-negative-sendAfterLoss','control-negative-sendStaleEpoch','control-witness-lost-sender-query'],
 'unknown':['control-safe','control-negative-notFoundAbsent','control-witness-safe-retry'],
 'decision':['decision-safe','decision-negative-stale','decision-negative-selfPass','decision-witness-admission','decision-witness-bill-reuse'],
 'calls':['decision-safe','decision-witness-repair','decision-negative-wrongResponseKey','decision-negative-wrongAttemptKey','decision-witness-old-return'],
 'repair':['decision-safe','decision-negative-unknownRepair','decision-witness-repair'],
 'budget':['budget-safe','budget-negative-earlyRelease','budget-witness-final'],
 'usage':['budget-safe','budget-negative-doubleBill','budget-negative-doubleParent','budget-witness-late-bill','budget-negative-settleConflict','budget-negative-releaseConflict','budget-witness-conflict-freeze'],
 'tree':['budget-safe','budget-negative-depth','budget-negative-doubleParent','budget-witness-nested'],
 'binding':['boundary-catalog','boundary-negative-switchVersion','boundary-negative-rebind','boundary-witness-old-binding'],
 'tombstone':['boundary-catalog','boundary-negative-forgetTombstone'],
 'pause':['boundary-gui-control','boundary-negative-clearChildPause','boundary-negative-staleProjection','boundary-witness-resume'],
 'gui':['boundary-gui-control','boundary-negative-staleObservation','boundary-witness-resume'],
 'facts':['boundary-facts','boundary-negative-lastArrival','boundary-negative-eraseConflict','boundary-negative-rewriteResult','boundary-witness-new-fact'],
 'identity':['core-identity','core-negative-taskScopedKey','core-negative-wrongAuthority','core-witness-identity'],
 'input':['core-input','core-negative-twoWinners','core-negative-oldInput','core-witness-input'],
 'batch':['core-batch','core-negative-partialBatch','core-negative-partialReject','core-witness-batch'],
 'adjust':['core-adjust','core-negative-belowReserved','core-negative-repeatIncrease','core-negative-expandExternal','core-witness-adjust']}
def pref(tag):
 model,props,claim=P[tag]
 return dict(model=model,properties=props,claim=claim,checks=check_refs[tag],lean_theorems=[{'source':'Control.lean','theorem':x} for x in lean.get(tag,[])])
M=[]
def mech(i,title,refs,tags,unmodeled,runtime):
 M.append(dict(id='CTL-%02d'%i,title=title,sources=[src(*x) for x in refs],formal=[pref(x) for x in tags.split()],not_modeled_rules=unmodeled,runtime_dependencies=runtime))
mech(1,'固定用户写权威与跨任务操作身份',[('task-kernel/storage-and-interfaces.md',127),('task-kernel/storage-and-interfaces.md',144),('task-kernel/lifecycle.md',9)],'identity receipt',['完整创建任务/输入/取消/管理各类载荷Schema；迁移协议当前明确后置'],['用户身份认证、真实唯一索引与错误路由拒绝、事务隔离'])
mech(2,'本地原子提交与跨域提交Unknown',[('task-kernel/storage-and-interfaces.md',149),('task-kernel/storage-and-interfaces.md',191),('capability-and-execution/execution-and-recovery.md',15)],'receipt',['全部提交步骤的精确去重键组合；A/B消息交接另见formal/handoff'],['存储持久性与事务线性化；扫描恢复最终执行'])
mech(3,'建议与核心裁决分权、批次整体准入',[('task-kernel/decision-and-work.md',116),('brain-system/decision-policy.md',61)],'decision batch',['完整action Schema及混合执行/委派批次每一前提；多维资源总量整批预留未与此批次模型组合'],['可信参数/内容/授权校验器与数据库共同提交'])
mech(4,'业务版本与纯账务版本分离',[('task-kernel/decision-and-work.md',73),('brain-system/validation.md',52)],'decision',['同义事实判定算法；密集事实的有界合并定时器与稳定后公平调度活性'],['事实分类正确性、权威时钟及调度器'])
mech(5,'领取代次、不可恢复发送资格与旧轮封闭',[('task-kernel/decision-and-work.md',154),('brain-system/contracts-and-runtime.md',30),('capability-and-execution/execution-and-recovery.md',59)],'lease decision calls',['物理旧进程隔离证明及两实际发送者互斥未模型化'],['资源入口fencing或真实隔离；owner/epoch校验'])
mech(6,'持久调用身份与有限结构修复',[('task-kernel/decision-and-work.md',129),('task-kernel/decision-and-work.md',131),('brain-system/decision-policy.md',63)],'calls repair',['每次修复新授权/once使用映射由授权模型负责；所有SDK隐藏重试配置'],['SDK无隐藏重试、持久调用序号、可信结束和费用信息'])
mech(7,'就绪前沿、持久阻塞条件与事实唤醒',[('task-kernel/decision-and-work.md',110),('task-kernel/decision-and-work.md',159),('task-kernel/decision-and-work.md',173)],'batch decision',['任意计划DAG、多个blocker及版本匹配唤醒、拒绝/失败后的后继选择和wait进展未建模'],['扫描与调度公平、可靠事实到达'])
mech(8,'取消/暂停与准备提交竞争',[('task-kernel/lifecycle.md',96),('task-kernel/control-and-management.md',29),('capability-and-execution/execution-and-recovery.md',134)],'cancel decision pause',['原取消拒绝后新取消attempt与固定答复保留窗口未单独建模'],['各权威事务线性化、控制传播最终到达'])
mech(9,'远端控制投影与本地动作门禁',[('task-kernel/control-and-management.md',33),('agent-coordination/delegation.md',118),('capability-and-execution/execution-and-recovery.md',144)],'cancel pause',['外部不支持暂停的缺口通知及设备重新开放管理协议'],['处理端实际门禁、祖先权威可达或失败关闭；没有全局瞬时暂停保证'])
mech(10,'可能启动切点与实际效果/观察知识分离',[('capability-and-execution/execution-and-recovery.md',57),('capability-and-execution/execution-and-recovery.md',85),('task-kernel/decision-and-work.md',194)],'unknown receipt',['驱动各类别的恢复profile与安全原键重交细节'],['外部真实效果不受本地事务控制；可信观测/封闭声明'])
mech(11,'先查原操作，封闭且可信无效果才新尝试',[('capability-and-execution/execution-and-recovery.md',115),('capability-and-execution/execution-and-recovery.md',117),('agent-coordination/delegation.md',35)],'unknown',['重试的新许可与新预算必须再次准入；完全相同外部幂等键可安全重交的provider profile未建模'],['查询不返回不等于未执行；真实provider关闭未来效果的能力'])
mech(12,'逻辑租约与物理隔离分离',[('capability-and-execution/execution-and-recovery.md',59),('capability-and-execution/execution-and-recovery.md',61),('brain-system/validation.md',51)],'lease',['真实物理占用/排他租约/旧进程隔离与释放过程未建模'],['设备/提供方互斥与可靠fencing，不能由逻辑代次推出物理停止'])
mech(13,'验收条件先固定、成果/证据版本核验',[('task-kernel/lifecycle.md',136),('task-kernel/lifecycle.md',138),('task-kernel/lifecycle.md',150),('brain-system/decision-policy.md',99)],'decision binding',['pass/fail/unknown、目标路径、成果版本、残余只读分支与finish规则由其他组独立模型覆盖；本组只证明固定条件门禁'],['独立验证器、真实内容与外部观察'])
mech(14,'多维事实单调合并与固定历史答复',[('task-kernel/decision-and-work.md',202),('task-kernel/lifecycle.md',174),('agent-coordination/peer-contracts.md',78)],'facts unknown',['完整接纳/执行/效果/控制/费用五轴乘积与查询投影分页未建模'],['来源身份和可信事实生产者；同修订内容相等性判定'])
mech(15,'累计用量、修订去重与冲突冻结',[('task-kernel/control-and-management.md',44),('agent-coordination/peer-contracts.md',89),('agent-coordination/peer-contracts.md',91)],'usage facts',['任意来源计费键、单位/币种不可变、超出声明上限后的显式违例记录未建模'],['账单完整可信、各计费源均可封账；不能假定终态等于final'])
mech(16,'未知预留、可信封账与目标/收尾双预算',[('task-kernel/decision-and-work.md',219),('task-kernel/decision-and-work.md',221),('agent-coordination/delegation.md',106)],'budget',['未知物理槽与货币预算联动；外部estimated/hard能力分级选择未建模'],['硬预算依赖提供方有限消费保证，TLC Report只接受allocation以内可信累计'])
mech(17,'内部委派份额、层级有界与单路径计费',[('agent-coordination/peer-contracts.md',43),('agent-coordination/peer-contracts.md',93),('task-kernel/decision-and-work.md',225)],'tree budget',['任意宽度/深度树的归纳证明；祖先/descendant合同完整属性继承与所有动态管理组合'],['固定用户写权威共同预算事务；TLC三节点树，Lean仅组件及两账户转移一般定理'])
mech(18,'委派合同、子身份与分阶段接纳',[('agent-coordination/peer-contracts.md',58),('agent-coordination/peer-contracts.md',69),('agent-coordination/delegation.md',17),('agent-coordination/peer-contracts.md',98)],'identity binding receipt cancel',['D→C/S和K→Q每种身份的完整映射未单独建模；本组通用固定键不证明所有映射正确'],['协作存储与核心存储分别可靠提交；admit_child准确路由'])
mech(19,'外部Agent固定提交键与能力合同',[('agent-coordination/delegation.md',73),('agent-coordination/delegation.md',74),('agent-coordination/delegation.md',79)],'binding unknown',['无查询/暂停/输入幂等能力禁用目标的完整能力协商表，禁止外部再委派的执行约束'],['外部提供方遵守声明、固定提交键可查询且不隐式重建'])
mech(20,'输入至多一次消费与权限分离',[('task-kernel/lifecycle.md',87),('agent-coordination/delegation.md',86),('agent-coordination/delegation.md',90),('brain-system/decision-policy.md',101)],'identity input',['外部输入本方接纳/外部应用双回执和input_request_id/version绑定未单独建模；普通输入不得签授权由trust组覆盖'],['可信用户操作身份、同一请求原子消费、外部输入profile'])
mech(21,'准确能力声明与实现版本固定',[('capability-and-execution/catalog-and-contracts.md',27),('capability-and-execution/catalog-and-contracts.md',55),('capability-and-execution/catalog-and-contracts.md',82)],'binding',['同版本异Schema冲突、activations修订、搜索分页/截断、停用卸载后的缺口策略'],['注册表不可变内容与真实驱动版本约束'])
mech(22,'GUI观察—单动作—再观察',[('brain-system/decision-policy.md',91),('capability-and-execution/validation.md',63)],'gui pause',['多设备排他、观察授权独立、动作后验证与用户接管开放协议'],['实际屏幕/焦点/方向版本探测、动作切点原子检查与设备互斥'])
mech(23,'有限主动核对与持续被动接收',[('capability-and-execution/execution-and-recovery.md',100),('agent-coordination/delegation.md',159),('agent-coordination/delegation.md',161)],'unknown calls budget',['管理Recheck去重/有限新取消轮次/通知去重与调度公平未模型化；查询预算仅ControlRecovery有限次数'],['被动合法事实送达；可信限额持久化'])
mech(24,'清理正文、身份墓碑与备份缺口关闭',[('capability-and-execution/execution-and-recovery.md',195),('capability-and-execution/execution-and-recovery.md',208),('brain-system/validation.md',62)],'tombstone',['旧备份回滚检测、账本缺失检测与受影响入口范围、实际保留窗口计时未模型化'],['入口封闭保证所有旧资格均失效；真实恢复过程不把旧备份当空新账本'])
mech(25,'容量、公平、隔离与真实效果验证',[('task-kernel/recovery-and-validation.md',140),('capability-and-execution/validation.md',68),('brain-system/validation.md',61)],'', ['容量限制与收尾保底的调度算法、公平活性、多用户资源排队未模型化'],['压力/延迟/磁盘满/跨用户不泄露/实际调用的运行验证；模型检查不能代替'])
mech(26,'管理预算修订与内外委派边界',[('task-kernel/control-and-management.md',48),('task-kernel/control-and-management.md',52),('agent-coordination/delegation.md',163)],'adjust tree',['管理命令各完整固定回执、修订竞争下多层份额调整和新D2准入责任继承'],['固定写权威预算调整事务；外部合同扩额须新委派'])
# Each active validation row has an explicitly reviewed mapping. Blank formal tags mean no local model claim.
# Tuple: tags ; precise modeled obligation ; uncovered abstract obligations ; runtime checks.
R={}
def cases(lines):
 for line in lines.strip().splitlines():
  ident,tags,claim,gap,runtime=line.split('|')
  R[ident]=(tags,claim,gap,runtime)
cases('''
TK-01|identity binding receipt|原键不可异意图改绑、提交未知不派发|任务ID与submit操作双唯一映射未逐字段建模|真实任务与Work共提交及回执恢复
TK-01a|identity receipt|固定权威接纳、用户原键唯一、Unknown保留|创建任务与提交键完整关联|真实错投路由和存储索引
TK-02|receipt facts|接纳带后续责任、固定历史结果不重写|响应交接确认与扫描公平另见handoff|崩溃后数据和扫描恢复
TK-03|input identity|单消费者、旧输入版本拒绝、文字刷新不失效|有效期真实时钟、同操作固定response载荷|两端并发争答及索引事务
TK-04|decision calls usage|业务变化/取消拒绝旧提案、调用有界、用量增量|决策与账务三个模型未全乘积组合|实际模型迟到账单归属
TK-04a|decision budget|纯账单不改业务版本且可复用提案|准入时多维当前预算复核尚未与Decision组合|实际事实同义分类
TK-04b|decision|轮次和修复有限，旧业务版本不能准入|有界合并窗口、持久等待和稳定后准入的活性|高频事实与公平调度实测
TK-05|decision lease budget|关闭旧轮与准入互斥、旧代次拒绝、未知保留|work状态全枚举共同事务|真实竞争与重启
TK-05a|calls repair usage budget|调用身份不复用、修复有限、累计去重、未知预留|每次调用完整授权键；持久多调用返回已建模但真实跨重启存储另验|取消/重启/逐调用账单注入
TK-06|batch budget|两操作整体准入或无任何操作预留；总预算另证守恒|混合执行/委派批次与资源总量共同事务、任意DAG后继|真实无部分提交和资源领取
TK-07|cancel unknown|取消先无准备、准备先保留原操作核对|取消answer窗口/重投语义未单独建模|真实两个提交顺序
TK-07a|facts|固定历史答复不被新事实改写|拒绝后新取消attempt责任链尚未建模|取消先到后调用出现的真实恢复
TK-08|receipt unknown lease|准备Unknown不重做，原发送资格丢失不补发|所有驱动安全重交profile|崩溃切点及外部真实效果
TK-09|lease decision facts|旧资格不准入、旧事实不倒退|允许哪些迟到字段的完整事实Schema|真实旧进程和来源鉴别
TK-10|receipt identity|查询未见后原事务仍可提交，禁止Unknown派发|提交步骤组成键完整枚举|真实数据库超时提交
TK-11|facts usage budget|乱序不回退、冲突保留、累计只结算增量|全事实轴依赖阻塞表|可信账单和同修订内容冲突检测
TK-11a|batch budget|后继前提未齐不派发；未封账预留保留|已拒response消费、确定零消耗释放、按blocker唤醒未完整模型化|返回拒绝及失败响应注入
TK-12|cancel facts unknown|取消后可有真实效果、历史答复固定|取消终态不重开和effects_pending全生命周期由其他组覆盖；超窗缺口|断线晚到效果与取消查询
TK-13|binding decision|旧能力不替换；固定条件才准入|内容有效性、用途授权、完成证据见其他组，本组未证明|真实引用/撤权/驱动升级
TK-13b||本组不声称覆盖完整来源闭包|来源闭包和间接摘要污染见trust组|真实组装器完整来源记录
TK-13c||本组不声称覆盖证据验证器|路径/成果版本/pass-fail-unknown见lifecycle组|可信独立读回与版本关联
TK-13a||本组不声称覆盖残余责任完成规则|必需效果/残余只读分支/完成规则见lifecycle组|真实读写分支能力保证
TK-14|tree usage cancel binding|份额守恒、不重复账单、固定通用原键、取消后仍可计费|D-C-S完整关联、父终态与子真实事实组合|内部并行委派与取消竞争
TK-15||本组不声称覆盖离线许可|时钟可信、离线锚、重连授权见trust组|设备时钟与断连测试
TK-16|lease identity|固定权威与旧代次资格的一部分|跨权威迁移协议当前后置，本组不把代次模型当迁移证明|交接消息、端点旧source隔离
TK-17|identity|同用户键域与不同用户同裸键互不冲突|跨用户内容/许可/查询不泄露见trust组|真实租户隔离及侧信道
TK-17a|identity|用户操作键跨任务/种类唯一、错权威不能接纳|分区/迁移未定义时关闭启用的配置验证|真实索引、路由、部署检查
TK-18|budget|未知预留和收尾额度不被释放或借用|接纳容量、水位和跨用户公平调度算法|存储逼满及负载压力
TK-19|unknown budget|查询预算有限且停止查询不清未知|通知去重、被动接纳与管理Recheck完整状态|迟到合法事实实际送达
TK-20|gui|旧观察不能越过实际动作切点|来源支撑/实际效果成功属于验证器与场景实测|问答和GUI真实闭环
TK-21|cancel pause decision|本地已知暂停阻止新动作/提案，真实在途效果仍可发生|正常完成门禁由lifecycle组；physical may_have_sent是否真正启动|真实队列/驱动准备切点
TK-22|pause facts|祖先原因解除不清自身暂停，陈旧事实不覆盖|旧控制修订专用协议、外部不支持暂停缺口|控制传播与provider能力
TK-23|decision pause|输入就绪或事实到达不会自动解除暂停|期限流逝与终态不复活、完成由lifecycle组|真实时钟和输入补证
TK-24|adjust tree|增额重投一次、底线、内部份额守恒、外部合同固定|多层动态调整与未知并发共同提交|真实预算管理事务
TK-25|facts|历史固定结果与当前事实分离|补证可信验证/过期/幂等验证操作见lifecycle和trust组|原验证响应恢复、取消超窗
CE-01|identity|用户键域区分|完整许可/缓存/队列隔离见trust组|实际跨用户不泄露
CE-02|binding batch|准确版本固定、坏参数批次拒绝|搜索有界分页、单有限单元、必需保证profile匹配|千项目录检索及Schema验证
CE-03|binding|已接纳原版本不随升级变化|同版本异声明、激活修订、排空卸载缺查询能力|真实目录/驱动生命周期
CE-04||本组不声称验证文件字节|两操作权限与成果版本核验见其他组|真实写入及独立读回字节匹配
CE-05|identity binding|原键唯一、意图和版本固定|完整参数/来源/端点/期限字段闭包|100次重投持久账本
CE-06|receipt|未知不启动，接纳有后续责任，未见不等于失败|完整操作/工作提交键恢复|真实事务回应丢失
CE-07||本组不声称覆盖BeginUse协议|原使用键、once、撤权/期限见trust组|真实授权存储和时钟
CE-08|lease unknown receipt|不可恢复首次发送，未见不重做|可安全原键恢复的各声明profile|真实外部未收/已执行/在途三组
CE-09|lease|旧代次不能新准备|物理资源互斥、隔离证明及安全释放未模型化|真实旧worker迟发与设备互斥
CE-10|unknown budget|过程/效果知识分离，not_found不推无效果，未知费用保留|原资源物理占用保持与关闭条件|实际provider迟到效果
CE-11|unknown|封闭加可信无效果才新尝试|新尝试重新授权与预算组合|权威负证据真实性
CE-12|cancel facts|取消先阻准备、本地先屏障阻首次Start、固定历史答复|先到取消拒绝后重试责任链、query_cancel窗口|真实取消两个顺序与超窗
CE-13|cancel unknown|取消后允许真实效果被观察|重连先控制/授权、无权读回限制见trust组|断网与迟到成功
CE-14|facts usage|过程事实不倒退、冲突保留、重复费用不双计|全轴冲突解除的可信核验程序|真实producer修订和账单
CE-15|gui|旧观察在动作切点拒绝|两个任务实际设备排他未模型化|设备动作串行与用户交互
CE-16|gui pause|陈旧观察与已知暂停阻动作|接管专用closed/open协议与动作后验证|多设备实际场景变化
CE-17||本组不声称覆盖观察授权|读取/保存/上传用途分离见trust组|本地截图及出口控制
CE-18||本组不声称验证业务结果语义|各能力成功判据、摘要vs全文证据见lifecycle组|HTTP200业务拒绝与截断测试
CE-19|tombstone facts|清正文不失去原身份，固定结果不重写|内容披露/缺字节/孤立内容GC见trust/lifecycle组|崩溃引用与发布确认
CE-20|budget|未知预算保持、目标和收尾隔离|容量/物理槽/恢复队列公平调度|压力、满盘、长期未知
CE-21|binding tombstone lease|原意图/版本固定、身份不复活、恢复不补发旧调用|原期限/预算重启映射、旧备份检测、端点迁移规则|真实路由变更与备份恢复
CE-22||本组不声称覆盖离线时钟|在线失联/离线窗口/重连顺序见trust组|真实断连与可信时间锚
CE-23|unknown budget|有限主动查询、停止不清未知|Recheck管理幂等、被动事实与通知去重完整状态|人工补证真实性与迟到事实
CE-24|binding|固定能力版本子义务|接口Schema和全profile互操作契约需静态与运行逐项核对|每个实现重跑冻结测试
CE-P1|binding batch|准确绑定和缺前提拒绝的局部规则|WSS basis完整来源/预算/身份组合与首次恢复通道见root/trust|独立端点互操作和Schema同步
CE-P2|facts usage budget|投影修订、累计去重、固定历史答复、可信封账|完整Schema/注册表字段关联和暂停事实投递|跨端乱序与清理回归
CE-25|binding decision batch|不切版本、缺已建模前提不准入|basis完整来源/预算/证明的合取见root/trust|真实schema与资格验证
CE-26|cancel pause|控制先到阻启动、在途可结束|恢复不刷新截止与物理未知不当停止见trust/运行|真实排队和已开始两组
CE-27|facts|旧投影拒绝、同修订冲突、历史结果固定|完整投影Schema逐字段合并|真实producer乱序
CE-28|usage budget|累计只补差、封账前余量保留|超额违例显式记录未建模（模型Report上界是环境假设）|恶意/错误provider超额账单
CE-29|facts identity|固定历史答复和原键唯一|管理/设备开放各具体回执、超窗及撤权查询|响应丢失和管理效果去重
CO-01|identity binding receipt|通用固定键意图、分阶段接纳/提交未知|D-C-S三身份完整映射与创建唯一未单独模型化|真实Peer和核心两个存储
CO-02|receipt budget|接纳有责任，Unknown不派发且未知预算保留|D-C-S的恢复映射与扫描公平|崩溃后的原S查询
CO-03|identity|用户操作索引跨任务唯一、错权威拒绝|D不得当S的具体类型区分|真实admit_child路由
CO-04|binding decision|准确版本与准入前提缺失时不接纳|适配器/合同profile配置关闭、禁止fallback工具|插件缺席和过期声明
CO-05||本组不声称覆盖父成果验收|子接纳/成功不足以父完成，来源/效果见lifecycle组|真实来源支撑和观察
CO-06|usage budget facts|累计差额、乱序拒绝、冲突冻结、终态不自动封账|计费单位/来源完整身份|真实重复/乱序/冲突账单
CO-07|tree budget|兄弟/孙辈份额守恒、不双计、双预算隔离|hard拒绝estimated profile选择规则未建模|外部提供方实际硬限额
CO-08|cancel unknown receipt|取消/准备竞态，未找到不等于永远封闭|D尚未到达的专用接纳与取消身份链|双存储真实故障注入
CO-09|facts|历史答复不因新事实改写|新子取消attempt有界责任链和超窗缺口|先取消拒绝后C创建
CO-10|cancel usage budget|取消后真实费用可继续记录且未知不释放|父终态、全部开放项effects_pending收敛未模型化|迟到子成果与账单
CO-11|unknown binding|原意图固定，未知只查原对象、不建替身|外部提交键到返回任务ID的具体映射与禁用profile|真实外部ID前断线
CO-12|budget decision|固定条件门禁、预算组件守恒|外部非法再委派/扩权/超额的隔离及留证流程|外部违规探针与证据验证
CO-18|decision lease identity budget|失效资格不准入、固定权威、额度守恒|离线资格和跨端缺依赖合取见trust/root|实际网络与父控制隔离
CO-20||本组不声称验证内容授权|内容引用完整性、当前披露、跨用户猜测见trust/lifecycle|真实引用丢失与不泄露
CO-21|unknown budget|有限原操作查询、未知不释放|Peer.Recheck身份/通知去重及被动接受完整状态|实际被动事实到达
CO-22|identity decision|固定权威和缺已建模前提不接纳|合同协商/授权处理器缺席关闭、stream-ready不得开放委派|真实插件与协议协商
CO-23|binding|实现版本固定子义务|完整CO-P1 Schema互操作规则和错误映射|两实现替换回归
CO-24|gui unknown|陈旧观察拒绝、断线效果未知不乱推断|父结论引用/动作后效果验证|联网资料与模拟手机真实闭环
CO-25|identity receipt|错误权威拒绝，原提交Unknown保留|固定C-S具体映射、远端不得自主下一轮|云端两部署和错误路由
CO-26|input binding|本地普通输入单消费和请求版本固定子义务|本方/外部双回执、旧请求绑定、交互provider能力禁用|外部应用后丢响应的真实恢复
CO-27|adjust tree|内部份额共同守恒，外部原合同不扩额|普通输入不能签授权由trust组；新D2与旧责任共存未模型化|并发预算管理与CO重投
CO-P1|identity receipt cancel tree input|通用身份、独立接纳、取消、份额与本地输入规则|完整Delegate/admit_child/外部输入消息字段逐项映射及恢复组合|独立端点协议互操作
CO-P2||当前设计明确后置，不生成通过结论|运行中权威迁移：唯一裁决、操作索引、旧入口隔离未定义/未建模|部署云端分别固定权威不等于迁移
BS-01|decision batch|规则可零模型调用，就绪前提才准入|任意计划图和核心完成验证由其他组/未建模部分|实际前驱事实与规则策略
BS-02|decision|条件未固定不能准入目标行动|draft阶段允许的澄清/只读例外未单独模型化|语义歧义识别和只读声明
BS-03|binding batch|精确绑定及批次整体拒绝|分页截断不当无能力、完整Schema规则|千候选实际检索
BS-04|decision|模型自评不能代替核心固定条件与有效性|数据身份/出口授权见trust组|注入语料与真实网络出口
BS-05||本组不声称覆盖来源闭包|完整来源污染与撤权见trust组|组装器与实际模型上下文
BS-06|identity|用户级键域区分|缓存/内容/grant归属见trust组|租户隔离与账单实际关联
BS-07|calls identity binding|抽象调用身份最多一次发送、原键意图固定|Generate完整输入/model/receiver/use映射未合并建模|并发同键真实适配器
BS-08|receipt lease calls|提交未知不派发，旧发送资格丢失不恢复补发|BeginUse固定映射见trust组|真实准备切点丢回应
BS-09||本组不声称覆盖多来源BeginUse|部分消费不回退、失效挡发送见trust组|真实授权存储及多来源调用
BS-10|calls unknown budget|无自动新attempt，原未知费用保留|SDK隐藏重试、模型切换与物理占位未模型化|429/超时供应商真实响应
BS-11|repair calls|已结束+已存输出+已知用量+有效资格才有限修复|新attempt重新授权与once映射见trust组|坏JSON截断和费用对照
BS-12|decision batch|自评pass不能替代固定条件/有效参数|成果/证据存在性及语义真实性见lifecycle/trust|伪造工具和证据语料
BS-13|lease decision|旧代次不能准入，丢发送资格不补发|实际旧出口隔离和物理未知槽未模型化|旧进程苏醒与隔离失败
BS-14|decision|业务修订拒旧轮、纯账单允许复用|当前预算重验未与Decision组成一个模型|实际账单先于提案
BS-15|usage budget decision|用量增量、未知保留、冲突冻结；提案轮次独立|提案准入与账本全乘积及多计费来源|真实迟到账单归属
BS-16|cancel decision usage|门禁前阻发送、门禁后可有真实效果/账单|撤权/终态完整规则由trust/lifecycle|真实门禁切点与迟到响应
BS-17|unknown|Unknown禁替身、可信封闭无效果才新尝试|各driver安全重试profile和新授权预算|文件写真实丢响应
BS-18|gui pause|陈旧观察和当前暂停阻动作|用户接管专用开放状态、独立授权、效果后验|多设备真实变化
BS-19||本组不声称判断资料质量|证据版本/来源有效性见lifecycle/trust|真实全文内容、时间和冲突
BS-20||本组不声称覆盖记忆偏好语义|计划/缓存对memory修订失效规则未建模|实际输出受合法偏好影响
BS-21|receipt budget cancel|接纳非效果成功、未知预算保留、取消竞态|协作合同缺席禁派发与不得绕工具|真实Agent合同配置
BS-22|binding|原调用固定版本不切换子义务|local-only外发/离线依赖合取见trust/root|真实云模型出口
BS-23|budget|未知额度不被清除，收尾独立|物理槽、队列容量及跨用户公平|长期未知压力/满盘
BS-24|tombstone calls|正文删除保墓碑、入口封闭后删身份、旧键不重建|备份回滚检测、受影响入口范围、保留窗口时钟|真实备份/清理/重启
BS-25|binding|固定版本子义务|冻结用例集和策略语义等价需静态与运行检查|不同策略质量费用比较
BS-26|decision batch|前提缺失不准入动作，条件固定才准入|残余只读分支finish及wait推进者由lifecycle/未模型化部分|真实取证与模型建议
BS-27|calls decision usage|旧轮响应按原键保存且不误配新轮，关闭输出不准入、账单只补增量|宿主扫描事实回送公平；旧轮返回按原键保存及新轮继续已单独见证|崩溃后扫描和重复账单
BS-28|calls receipt|抽象call身份一次发送，接纳Unknown不新发|远程适配器不得自主循环的完整接口约束|响应丢失重投查询
BS-29|decision budget pause|暂停拒旧提案、恢复业务新轮、未知预算保留|物理未知占位、远程账单回送组合|生成与暂停真实竞争
BS-30|facts tombstone|修订不倒退、冲突持久、旧键不复活|完整observation字段Schema|跨端乱序和记录清理
BS-31|decision batch|已建模前提缺失则不准入|完整来源/声明/预算/离线资格合取见root/trust|真实使用前资格复核
BS-32|decision adjust pause|管理预算不解除暂停，恢复只新有效业务轮|补证验收及正常完成见lifecycle|暂停时补证/增额实测
''')
files=[D+'task-kernel/recovery-and-validation.md',D+'capability-and-execution/validation.md',D+'agent-coordination/validation.md',D+'brain-system/validation.md']
rows=[]
for path in files:
 for line,text in enumerate(resolve_source(path).read_text().splitlines(),1):
  m=re.match(r'^\|\s*([A-Z]{2,3}-(?:\d+[a-z]?|P\d+))\b',text)
  if not m:continue
  id=m.group(1); fields=[v.strip() for v in text.strip('|').split('|')]
  tags,claim,gap,runtime=R.pop(id)
  obs=[]
  if tags:obs.append({'status':'model-property','claim':claim,'refs':[pref(x) for x in tags.split()]})
  if id=='CO-P2':obs.append({'status':'static-only','claim':'规范明确本轮后置；此行是后置协议profile，不是已实现机制通过的测试。'})
  if gap:obs.append({'status':'uncovered','claim':gap,'scope':'本组未形式化；带其他组说明的项目应由总清单以实际检查结果追加交叉引用，不能据此直接算覆盖。'})
  if runtime and id!='CO-P2':obs.append({'status':'runtime-required','claim':runtime,'scope':'形式模型是规范抽象，未验证实际代码/部署/外部副作用。'})
  rows.append({'id':id,'source':{'path':path,'line':line},'original_cells':fields,'obligations':obs})
assert not R,R
out={'schema_version':1,'scope':[D+x for x in ['task-kernel','capability-and-execution','agent-coordination','brain-system']],
 'coverage_semantics':'每行拆分规则义务与实现义务；model-property 仅对应列明的抽象子义务，不表示整行测试通过。uncovered 包括本组未建模及由其他组负责但本组未计入的义务。',
 'model_parameters':{'ControlRecovery.tla':'MaxEpoch=1, QueryBudget=1；单原操作，核心和执行两提交域','Decision.tla':'MaxRounds=2, MaxAttempts=2, MaxRevision=2, MaxEpoch=1；持久(round,attempt)调用及响应表；身份负例另分1轮/2尝试和2轮/1尝试','Budget.tla':'Nodes=0..2, Total=2,Cleanup=1,MaxDepth=2,MaxRevision=2（depth负例为1）','Boundaries.tla':'catalog/gui-control/facts 独立切片；版本0..1/事实修订0..2/控制修订最多4','CoreContracts.tla':'identity/input/batch/adjust独立切片；2用户/2任务/2操作，最多2接纳事件；2操作批次；固定数字预算调整'},
 'lean_scope':'Control.lean: 任意自然数总额/收尾额的一次预留组件及有限转移归纳，任意代次的核心取消/提交状态机；不是任意委派树或全部TLA模型的精化证明。',
 'mechanisms':M,'validation_cases':rows,
 'retired_aliases':{'CO-13..17,CO-19':'协作validation声明已移交通信模块，不作为此页活跃测试重复计数。'},
 'summary':{'mechanisms':len(M),'validation_rows':len(rows),'by_module':dict(collections.Counter(x['id'].split('-')[0] for x in rows)),'checks':len(checks)}}
(HERE/'inventory.json').write_text(json.dumps(out,ensure_ascii=False,indent=2)+'\n')
print(out['summary'])
