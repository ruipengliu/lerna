"""Rebuild source-linked inventory; no architecture files are modified."""
import json, pathlib, re, hashlib
HERE=pathlib.Path(__file__).resolve().parent
import sys
sys.path.insert(0, str(HERE.parents[1]))
from archive_paths import historical_name, resolve_source
D='docs/architecture/'
def src(module,file,line): return {'path':D+module+'/'+file+'.md','line':line}
E='extensions-and-runtime'; U='application-and-interaction'; O='observation-and-improvement'
# These are mechanism families. Case-level records below retain every original validation ID.
data=[
('ER-lock','精确制品与依赖闭包',src(E,'contracts',30),'版本、摘要、依赖锁固定；改变字节形成新身份',[],['包解析、安全解包、签名、依赖解析器正确性']),
('ER-command','持久管理命令与分阶段激活',src(E,'contracts',73),'固定命令与意图，接纳/激活/当前健康分离，未知查原键',[],['实际事务原子性、实例识别、回执可靠交接']),
('ER-reference','使用前登记固定引用',src(E,'contracts',120),'读门禁、决定与提交分离；登记提交与关闭门禁串行；使用前已有原身份保留资格',['References.UseHasPin','References.FreshRegistration','References.reachable_safe'],['真实BindingReference与领域接纳跨库映射、语义能力与驱动双层引用']),
('ER-reclaim','关闭新引用、排空、计划先存、可恢复回收',src(E,'contracts',122),'关闭后零持有者才存回收计划；删除可重启续做，未知不释放',['References.NoPrematureReclaim','References.NoRecoveryWitness','References.reachable_safe'],['完整持有者枚举、文件删除/备份/暂存资产、未知效果核对']),
('ER-withdraw','普通排空与安全撤回分离',src(E,'contracts',124),'关闭新引用不阻止旧引用后继；安全停用禁止新执行，迟到完成继续记账',['References.UseHasPin','References.NoNormalWitness'],['真实进程与设备隔离、受信查询白名单、后继引用树']),
('ER-rollback','新命令回退代码，历史事实不回退',src(E,'installation-and-isolation',64),'指定旧版本须仍获准且读当前格式，原效果、账本、权限和预算保留', ['Evaluation.HistoryPreserved'],['实际旧版读取新版持久格式、预算/owner等领域账本历史']),
('ER-isolate','宿主身份映射与资源隔离资格',src(E,'installation-and-isolation',102),'插件不自行声称主体，不获DB或原始凭据，隔离依赖不齐拒绝加载',[],['操作系统沙箱、IPC认证、路径/网络/凭据隔离与聚合配额']),
('ER-lifecycle','依赖DAG与按行为就绪',src(E,'lifecycle-and-recovery',47),'生命周期串行，故障限于依赖分支；新动作与事实接纳门禁独立',[],['真实装配图、每模块就绪/恢复接口、owner排他']),
('ER-bounded-recovery','持久有界恢复与公平扫描',src(E,'lifecycle-and-recovery',107),'通知仅提示；重启不重置预算；扫描有界并最终覆盖责任',[],['扫描公平性、容量、退避、时钟与持久计数']),
('ER-backup','完整提交域恢复与旧写者隔离',src(E,'lifecycle-and-recovery',170),'备份不完整不能证明未发生，缺尾保持受限',[],['备份水位、存储连续性、旧进程/设备真实隔离']),
('ER-release','逐目标批准新鲜度与部分生效',src(E,'contracts',129),'每目标独立结果；撤回本地提交不等于远端停用',['Evaluation.DurableResponsibility','Evaluation.NoPartialWitness'],['远端批准视图、离线期限、可信时钟、严格默认离线0与目标门禁']),
('UI-input','持久输入接管与父子操作映射',src(U,'interaction-contract',32),'冻结输入意义和身份，接纳前保存责任，丢响应查原操作',[],['实际唯一消费者CAS、父子事务与核心接纳；旧代次输入资格']),
('UI-content','完整快照与条件增量',src(U,'interaction-contract',71),'增量只追加文本且基准精确匹配；同修订冲突清缓存重取',['UI.DeltaContinuity'],['具体内容字节/schema、修订冲突检测、输入结构必须完整快照']),
('UI-intent','显式意图、待决展示与旧响应隔离',src(U,'interaction-contract',82),'关闭立即隐藏；一次在途；旧答复不覆盖最新意图，内容不能自行打开',['UI.DisplayAuthority','UI.NoRecoveryWitness'],['真实存储失败、CAS/操作过期、ui.get现状校验、多窗口/多端点实现']),
('UI-result','正式结果与展示独立',src(U,'interaction-and-recovery',23),'关闭、崩溃、缓存失效不删除既有正式结果',['UI.ResultPersistence','UI.NoNormalWitness'],['成果独立持有/不可恢复标记、当前权限、核心终态与迟到费用']),
('UI-directory','固定权威目录与显式订阅',src(U,'interaction-contract',112),'目录切点与当前披露，缺口不作空目录；订阅不自动续期',[],['端点目录、分页切点、可靠通知与订阅失效']),
('UI-preview','可信预览与精确批准绑定',src(U,'interaction-contract',128),'先保留字节与持有者，必需预览齐全才生成精确输入证明',[],['可信宿主取得字节/digest、预览类型/权限、两侧验证；不证明人类阅读']),
('UI-session','会话代次及当前用户约束',src(U,'interaction-and-recovery',101),'旧会话响应不得重新显示；退出先关闭显示和发送入口',['UI.DisplayAuthority'],['多租户真实缓存/凭据清理、远端请求身份、字节披露授权']),
('UI-render','非受信内容受限呈现',src(U,'contracts-and-storage',87),'呈现不取得新领域权威；不可恢复结果如实显示',[],['脚本/HTML/URL渲染安全、附件类型、资源配额']),
('OI-observe','领域事实、观测与独立评估证据分离',src(O,'evaluation',40),'候选/遥测声明不能写独立证据，报告不回写核心终态',['Evaluation.EvidenceSeparation'],['取证通道真实隔离、来源真实性、时钟同步、注入抵抗']),
('OI-freeze','冻结候选/计划/保留集',src(O,'evaluation',7),'字节或行为变更形成新候选；评分规则与样本不可追溯修改',['Evaluation.ExactFreshQualification'],['完整制品/依赖/配置摘要、样本分组、保留集污染防护、受信判定器']),
('OI-denominator','固定分母、未知不算成功',src(O,'evaluation',53),'启动后失败/超时/未知保留分母，预算未知不记零',[],['统计公式、抽样独立性、真实成本/时钟与全局尝试限额']),
('OI-run','持久run与测量/结算分离',src(O,'evaluation',101),'报告发布仍可有结算责任，迟到事实新修订不改旧报告',['Evaluation.HistoryPreserved'],['评分唯一约束、worker代次、环境quarantine、迟到成本与报告修订']),
('OI-source','来源关闭与派生/发布提交串行',src(O,'contracts',124),'来源或证据失效封闭新使用，旧报告不能继续作为新发布依据',['Evaluation.ExactFreshQualification'],['逐来源版本与派生图、跨模块关闭回执、所有正文副本清理']),
('OI-approval','精确候选/报告批准及新鲜提交',src(O,'improvement',23),'人工受信批准独立于pass；读资格/决定/提交可交错，提交重新校验版本和撤回',['Evaluation.ExactFreshQualification'],['批准主体认证、完整scope/期限/批次/当前绑定校验']),
('OI-rollout','有限批次、持久调用资格与原键核对',src(O,'improvement',30),'提交调用资格视作可能已发；撤回不擦除责任；目标独立回复',['Evaluation.DurableResponsibility','Evaluation.NoRecoveryWitness','Evaluation.NoPartialWitness'],['本地与远端批准各自串行域、真实消息重投、单目标幂等、有限查询预算']),
('OI-history','撤回/停用/回退与历史记录分离',src(O,'improvement',78),'撤回不删除报告批准或真实激活历史，停用不能声称旧版恢复',['Evaluation.HistoryPreserved'],['真实旧代码兼容、迁移、owner/预算/授权历史、预批准处置资格']),
('OI-governance','监测有界与新批次显式批准',src(O,'improvement',72),'数据不足暂停；完成初始窗口仍保留治理；不能凭分数扩批',[],['监测完整样本、因果可比性、容量与通知送达']),
('OI-optin','成功任务记忆提取与候选发布分离',src(O,'improvement',7),'按类型opt-in的提取用独立任务预算，不自动产生代码候选或发布',[],['记忆模块真实授权、数据用途、预算隔离、触发幂等'])]
mechanisms=[]
for ident,title,s,rule,props,remainder in data:
    mechanisms.append({'id':ident,'name':title,'sources':[s],'rule':rule,'formal_properties':props,
      'coverage':'abstract-partial' if props else 'runtime-required','uncovered_details':remainder,
      'real_world_dependencies':['持久存储与原子提交正确实现','模型变量到实现状态的精化映射']+remainder})
modeled={
'ER-08':['ER-reference','ER-reclaim'],'ER-10':['ER-reference','ER-withdraw'],'ER-22':['ER-reclaim'],'ER-30':['ER-release'],'ER-31':['ER-release'],
'UI-08':['UI-content'],'UI-09':['UI-intent','UI-result'],'UI-10':['UI-intent'],'UI-11':['UI-intent'],'UI-12':['UI-result'],'UI-17':['UI-session'],
'OI-02':['OI-observe'],'OI-04':['OI-freeze'],'OI-14':['OI-source'],'OI-17':['OI-approval'],'OI-18':['OI-rollout'],'OI-19':['OI-rollout'],'OI-20a':['OI-history'],'OI-21':['OI-history'],'OI-26':['OI-rollout']}
families={'ER':E,'UI':U,'OI':O}
cases=[]
for prefix,module in families.items():
    p=resolve_source(D+module+'/validation.md')
    for ln,line in enumerate(p.read_text().splitlines(),1):
        m=re.match(r'^\|\s*([A-Z]{2,3}-(?:\d+[a-z]?|P\d+))\b',line)
        if not m: continue
        ident=m.group(1); ids=modeled.get(ident,[])
        selected=[x for x in mechanisms if x['id'] in ids]
        cases.append({'id':ident,'source':{'path':historical_name(p),'line':ln},'original_case':line,
          'classification':'model-property' if ids else 'runtime-required',
          'coverage':'abstract-partial' if ids else 'not-formally-covered', 'mechanism_ids':ids,
          'model_properties':sorted(set(y for x in selected for y in x['formal_properties'])),
          'uncovered_details':sum([x['uncovered_details'] for x in selected],[]) if ids else ['本用例的真实实现、领域细节与验收指标须运行集成/故障注入/安全或性能测试；本包没有给出形式保证'],
          'real_world_dependencies':['本行完整契约在实现上的一致性与集成测试；模型枚举不等于原用例通过']})
files=sorted({s['path'] for m in mechanisms for s in m['sources']}|{x['source']['path'] for x in cases})
out={'schema_version':1,'scope':[E,U,O],'formal_status':'models-ready-checks-pending','semantics':'model-property仅表示列明抽象性质，绝不表示整行验证用例或生产系统通过。无静态或运行验收被冒充形式证明。','mechanisms':mechanisms,'validation_cases':cases,'source_hashes':{f:hashlib.sha256(resolve_source(f).read_bytes()).hexdigest() for f in files}}
(HERE/'inventory.json').write_text(json.dumps(out,ensure_ascii=False,indent=2)+'\n')
print('mechanisms',len(mechanisms),'cases',len(cases),'per-module',{k:sum(x['id'].startswith(k+'-') for x in cases) for k in families})
