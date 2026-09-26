"""Add checked local and cross-group abstract obligations to the source inventory."""
import json,pathlib,re,hashlib
p=pathlib.Path(__file__).resolve().parent
import sys
sys.path.insert(0, str(p.parents[1]))
from archive_paths import resolve_source
f=p/'inventory.json';d=json.loads(f.read_text())
cross={
'ER-lock':['control/Boundaries.tla#FixedBinding'],
'ER-command':['control/ControlRecovery.tla#CommitKnowledge','control/ControlRecovery.tla#DurableExecution','control/Boundaries.tla#NoIdentityResurrection'],
'ER-rollback':['lifecycle/Rollback.tla#CurrentRestoreBasis','lifecycle/Rollback.tla#RestoredRequiresActivation','lifecycle/Rollback.tla#NoFactRewind'],
'ER-isolate':['communication/Composition.tla#Safety','trust/Authorization.tla#NoUnsafeUse'],
'ER-lifecycle':['communication/Composition.tla#Safety','control/ControlRecovery.tla#LocalControlGate'],
'ER-bounded-recovery':['communication/Scheduling.tla#EveryAcceptedFinishes','control/Decision.tla#FiniteConsumption','control/ControlRecovery.tla#CurrentLease'],
'ER-backup':['control/ControlRecovery.tla#NoUnknownDispatch','control/Boundaries.tla#RecoverableIdentity'],
'ER-release':['lifecycle/Evaluation.tla#TargetQualification','trust/Authorization.tla#NoUnsafeUse'],
'UI-input':['lifecycle/Input.tla#MappingBeforeForward','lifecycle/Input.tla#OneConsumer','lifecycle/Input.tla#MeaningBound','lifecycle/Input.tla#NoParentRegression'],
'UI-directory':['trust/RecoveryView.tla#ReadyComplete','trust/RecoveryView.tla#NoStaleRead'],
'UI-preview':['lifecycle/Preview.tla#CompleteCurrentPreview','lifecycle/Preview.tla#RetainedBeforeAcquisition','trust/Authorization.tla#ReceiptBinding','trust/SourceGovernance.tla#RegisteredBeforePublish','communication/Composition.tla#Safety'],
'UI-session':['trust/Authorization.tla#NoUnsafeDisclosure'],
'UI-render':['communication/Composition.tla#Safety'],
'OI-freeze':['lifecycle/EvaluationRules.lean#reachable_inv','lifecycle/EvaluationRules.lean#original_score_preserved'],
'OI-denominator':['lifecycle/EvaluationRules.lean#pass_iff_all','lifecycle/EvaluationRules.lean#successes_bounded','lifecycle/EvaluationRules.lean#unknown_is_not_success','lifecycle/EvaluationRules.lean#frozen_denominator'],
'OI-run':['lifecycle/EvaluationRules.lean#reachable_inv','lifecycle/EvaluationRules.lean#original_score_preserved','lifecycle/EvaluationRules.lean#closing_monotone','lifecycle/EvaluationRules.lean#old_report_survives_late_cost','control/ControlRecovery.tla#CurrentLease'],
'OI-source':['trust/SourceGovernance.tla#NoUnsafeUse','trust/SourceGovernance.tla#ClosureCoverage','trust/SourceGovernance.tla#NoFalseComplete'],
'OI-approval':['trust/Authorization.tla#Attenuation','trust/Authorization.tla#NoUnsafeUse','lifecycle/Evaluation.tla#TargetQualification'],
'OI-history':['lifecycle/Rollback.tla#CurrentRestoreBasis','lifecycle/Rollback.tla#RestoredRequiresActivation','lifecycle/Rollback.tla#DispositionHonest','lifecycle/Rollback.tla#NoFactRewind'],
'OI-governance':['communication/Scheduling.tla#Safety','communication/Scheduling.tla#EveryAcceptedFinishes','control/Decision.tla#FiniteConsumption'],
'OI-optin':['trust/ExtractionPolicy.tla#Safety']}
# What is still logically absent, as distinct from external implementation assumptions.
unmodeled={
'ER-lock':['完整依赖闭包解析算法、环/导出冲突检测、包归一化与摘要验证未建模'],
'ER-command':['多阶段实例准备/目录登记/激活的全部状态及多目标CAS未逐阶段建模'],
'ER-reference':['跨模块长任务后继引用树与多层能力/驱动引用合并未建模'],
'ER-reclaim':['所有真实持有者种类及保留期限算法未枚举'],
'ER-withdraw':['获准查询白名单及外部进程隔离证据生成未建模'],
'ER-rollback':['在线格式迁移、兼容性判定算法与多库回退未建模'],
'ER-isolate':['IPC身份映射算法、具体沙箱规则、资源对象权限未建模'],
'ER-lifecycle':['完整依赖DAG启动/逆序停止及可选组件降级算法未建模'],
'ER-bounded-recovery':['实际分页扫描与持久RecoveryEvent小时窗口/健康重置规则未建模'],
'ER-backup':['完整快照/增量尾对齐/恢复代次/fencing流程未建模，仅复用未知拒发和身份保留子规则'],
'ER-release':['真实可信时间/反回滚时钟与可配置离线窗口上界的计算未建模'],
'UI-input':['任意多用户唯一索引、操作过期/回执窗口、完整请求Schema未建模'],
'UI-content':['具体文本/Markdown字节与输入表单结构验证未建模'],
'UI-intent':['真实权威presentation独立变化、条件CAS及ui.get完整读现状未建模；Reply抽象为带可用当前依据的响应'],
'UI-result':['正式成果多载体保留及result_unavailable处理未建模'],
'UI-directory':['目录查询筛选/游标到期与显式订阅续期状态机未建模；只复用固定切点完整恢复与当前读取子规则'],
'UI-preview':['必需预览集合发现算法、所有proof完整绑定字段/签名、投影类型策略变更算法未建模；当前集合完备、取得前保留、投影修订与来源开放提交已建模'],
'UI-session':['真实缓存键空间及所有异步回调的会话分区未建模'],
'UI-render':['渲染解析器、HTML/脚本/URL拒绝逻辑未建模；资格门禁复用仅说明依赖不齐不能开启'],
'OI-observe':['trace冲突解析、时钟域/延迟计算、缓冲导出算法未建模'],
'OI-freeze':['保留集暴露/候选选择统计独立性、完整计划字段的不可变约束未建模'],
'OI-denominator':['API去重/Wilson区间/配对统计/多层阈值与成本判定完整算法未建模'],
'OI-run':['全部样本调度/环境quarantine/评分冲突导致报告不可用/迟到证据时间窗未建模'],
'OI-source':['全来源图和跨机构持有者恢复工作未枚举'],
'OI-approval':['完整批准字段、跨权威签名与在线监测报告可用性切换未建模'],
'OI-rollout':['观察窗口/所有批次阶段及受信恢复选择、目标批准离线窗口数值未建模'],
'OI-history':['实际数据格式兼容性检查、多目标处置完成聚合未建模'],
'OI-governance':['退化判据/分层样本/监测暂停与下一批的完整策略未建模'],
'OI-optin':['记忆提取质量及独立候选显式Propose入口鉴权未建模']}
def normalize(prop):
    if '/' in prop: return prop
    module,name=prop.split('.',1)
    kind='lean' if name=='reachable_safe' else 'tla'
    return 'lifecycle/'+module+'.'+kind+'#'+name
for m in d['mechanisms']:
    m['formal_properties']=[normalize(x) for x in m['formal_properties']]
    m['cross_group_properties']=cross.get(m['id'],[])
    m['unmodeled_rules']=unmodeled.get(m['id'],[])
    m['verified_abstract_rules']=m['formal_properties']+m['cross_group_properties']
    m['coverage']='abstract-partial' if m['verified_abstract_rules'] else 'runtime-required'
    if m['id']=='OI-source':m['sources'][0]['line']=124
all_map={
'ER-01':['ER-command','ER-lock'],'ER-02':['ER-lock'],'ER-03':['ER-isolate'],'ER-04':['ER-lifecycle'],
'ER-05':['ER-command'],'ER-06':['ER-command','ER-lock'],'ER-07':['ER-command'],'ER-08':['ER-reference','ER-reclaim'],
'ER-09':['ER-lock','ER-bounded-recovery'],'ER-10':['ER-reference','ER-withdraw'],'ER-11':['ER-isolate','ER-backup'],
'ER-12':['ER-isolate'],'ER-13':['ER-isolate'],'ER-14':['ER-bounded-recovery'],'ER-15':['ER-isolate'],'ER-16':['OI-approval','ER-rollback'],
'ER-17':['ER-lifecycle'],'ER-18':['ER-withdraw'],'ER-19':['ER-bounded-recovery'],'ER-20':['ER-bounded-recovery'],
'ER-21':['ER-bounded-recovery','ER-command'],'ER-22':['ER-reclaim'],'ER-23':['ER-backup','ER-rollback'],
'ER-24':['ER-release'],'ER-25':['OI-observe','ER-command'],'ER-26':['ER-lifecycle'],'ER-27':['OI-source','ER-lock'],
'ER-28':['ER-command'],'ER-29':['ER-rollback','OI-history'],'ER-30':['ER-release'],'ER-31':['ER-release'],
'ER-32':['OI-history'],'ER-33':['OI-approval'],'ER-P1':['OI-rollout'],'ER-P2':['ER-release'],
'UI-01':['UI-input','UI-result'],'UI-02':['UI-input'],'UI-03':['UI-input'],'UI-04':['UI-input','UI-intent'],
'UI-05':['UI-input'],'UI-06':['UI-input'],'UI-07':['UI-input'],'UI-08':['UI-content'],'UI-09':['UI-intent','UI-result'],
'UI-10':['UI-intent'],'UI-11':['UI-intent'],'UI-12':['UI-result'],'UI-13':['OI-history'],'UI-14':['ER-withdraw'],
'UI-15':['OI-approval'],'UI-16':['OI-source'],'UI-17':['UI-session'],'UI-18':['UI-render','UI-input'],
'UI-19':['ER-bounded-recovery'],'UI-20':['ER-backup','UI-intent'],'UI-21':['ER-lifecycle'],'UI-22':['UI-result','OI-source'],
'UI-23':['OI-run'],'UI-24':['UI-preview'],'UI-25':['UI-directory'],'UI-26':['UI-directory'],'UI-27':['UI-preview'],
'UI-28':['OI-approval'],'UI-29':['ER-withdraw'],'UI-30':['OI-source'],'UI-31':['UI-input'],
'OI-01':['OI-observe','ER-command'],'OI-02':['OI-observe'],'OI-04':['OI-freeze'],'OI-05':['OI-denominator'],
'OI-06':['OI-denominator'],'OI-07':['OI-run','ER-bounded-recovery'],'OI-08':['OI-freeze','OI-denominator'],
'OI-09':['ER-isolate','OI-denominator'],'OI-10':['ER-command','OI-observe'],'OI-11':['OI-run'],
'OI-12':['UI-input','ER-withdraw'],'OI-13':['OI-source'],'OI-14':['OI-source'],'OI-15':['OI-source','ER-backup'],
'OI-16':['ER-isolate','ER-bounded-recovery'],'OI-17':['OI-approval'],'OI-18':['OI-rollout'],'OI-19':['OI-rollout'],
'OI-20':['OI-history'],'OI-20a':['OI-history'],'OI-21':['OI-history','OI-run'],'OI-22':['OI-run','ER-command'],
'OI-23':['OI-governance'],'OI-24':['OI-run'],'OI-25':['OI-optin'],'OI-26':['OI-rollout','OI-history'],'OI-27':['OI-source'],
'OI-P1':['OI-rollout'],'OI-P2':['OI-source'],'MS-P3':['OI-optin']}
# Additional precisely named cross-group subproperties.
extra={
'ER-14':['communication/Scheduling.tla#Safety'],
'ER-18':['communication/Composition.tla#Safety','control/ControlRecovery.tla#LocalControlGate'],
'ER-28':['control/Boundaries.tla#ActionBoundary'],
'UI-13':['control/ControlRecovery.tla#LocalControlGate','control/Boundaries.tla#FixedHistoricalResult'],
'UI-14':['control/Boundaries.tla#ActionBoundary'],'UI-16':['trust/MemoryCommand.tla#CASRespected','trust/MemoryCommand.tla#NoResurrection'],
'UI-29':['control/Boundaries.tla#PauseReasonsIndependent','control/Budget.tla#UnknownHeld'],
'OI-24':['control/Budget.tla#UnknownHeld'],'OI-13':['trust/Authorization.tla#NoUnsafeUse']}
# A few rows consist entirely of unimplemented/physical rules; do not assign a weak unrelated invariant.
runtime_only={'ER-03','ER-04','ER-26','OI-03','OI-05','UI-18','UI-21'}
d['validation_cases']=[]
for module,prefix in [('extensions-and-runtime','ER'),('application-and-interaction','UI'),('observation-and-improvement','OI')]:
    source='docs/architecture/'+module+'/validation.md'
    for line,text in enumerate(resolve_source(source).read_text().splitlines(),1):
        mm=re.match(r'^\|\s*([A-Z]{2,3}-(?:\d+[a-z]?|P\d+))\b',text)
        if not mm:continue
        ident=mm.group(1); ids=all_map.get(ident,[]); selected=[m for m in d['mechanisms'] if m['id'] in ids]
        props=sorted(set(z for m in selected for z in m['verified_abstract_rules'])|set(extra.get(ident,[])))
        excluded=ident in {'ER-P3','OI-P3'}
        classification='static-only' if excluded else ('runtime-required' if ident in runtime_only or not props else 'model-property')
        if classification!='model-property':props=[]
        d['validation_cases'].append({'id':ident,'source':{'path':source,'line':line},'original_case':text,
          'classification':classification,'coverage':'abstract-partial' if classification=='model-property' else ('policy-exclusion-only' if excluded else 'not-formally-covered'),
          'mechanism_ids':ids,'model_properties':props,
          'verified_abstract_rules':props,
          'unmodeled_rules':(['文档明确未采用/后置；仅检查已发布边界，不声明行为实现或形式证明'] if excluded else sum([m['unmodeled_rules'] for m in selected],[]) or ['该行全部具体规则尚未形式建模']),
          'runtime_dependencies':sum([m['real_world_dependencies'] for m in selected],[]) or ['真实实现、数据与环境验收'],
          'uncovered_details':'整行验收未通过。仅列明抽象逻辑已检查；真实依赖和未建模规则必须分别完成。'})
d['formal_status']='executed-see-checks-and-evidence'
d['semantics']='分类model-property仅表示本行列明的抽象子义务，完整验证行仍部分覆盖；跨组模型引用为共享机制的复用，不等于建立该模块实现精化。static-only仅核对未采用/后置政策文字。'
d['source_hashes']={f:hashlib.sha256(resolve_source(f).read_bytes()).hexdigest() for f in d['source_hashes']}
f.write_text(json.dumps(d,ensure_ascii=False,indent=2)+'\n')
from collections import Counter
print('cases',len(d['validation_cases']),Counter(x['classification'] for x in d['validation_cases']))
