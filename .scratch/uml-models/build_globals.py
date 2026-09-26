from pathlib import Path
import json
D=Path('.scratch/uml-models')
def dump(s): (D/(s['module']+'.json')).write_text(json.dumps(s,ensure_ascii=False,indent=2)+'\n')
modules=[('interaction','应用与交互','Interaction',['interaction.input','interaction.present']),('task-runtime','任务运行时','Task Home',['task.submit','task.read','task.cancel']),('brain','大脑','Brain',['brain.decide','brain.get']),('memory','记忆与内容','Memory / Content',['memory.query','content.get']),('execution','执行','Executor',['execution.invoke','execution.reconcile']),('collaboration','Agent 协作','Collaboration',['collaboration.delegate','collaboration.read']),('security','权限与隔离','Security',['grant.use','grant.use.settle']),('evaluation','观测、评测与改进','Evaluation',['evaluation.run','evaluation.approval_check']),('extensions','扩展与宿主','Extensions',['extensions.activate','extensions.read'])]
g={'module':'system','title':'系统','component':{'nodes':[dict(id=m,name=t+'\n'+en,kind='component',boundary='internal',methods=methods,source=m+'/README.md') for m,t,en,methods in modules],'edges':[],'notes':['逻辑模块 ≠ owner ≠ 进程 ≠ 提交域','仅画主线依赖；返回与控制细节见模块页']},'reading_notes':['全局组件页以软件依赖为视角。箭头由依赖者指向被依赖者，接口方法是所提供能力的节选；未展开全部控制、内容读取和观测采集依赖。','Security 同时覆盖多个事实职责，Extensions 的装配边也不表示用户业务授权；部署和事务边界仍按所属 owner 单独判断。'],'sources':['technical-overview.md','README.md']}
for a,b,l in [('interaction','task-runtime','目标 / 输入'),('task-runtime','brain','单轮提案'),('task-runtime','memory','检索 / 内容'),('task-runtime','execution','准入意图'),('task-runtime','collaboration','有界委派'),('task-runtime','security','使用 / 额度'),('execution','security','启动依据'),('collaboration','security','委派授权'),('evaluation','task-runtime','获准观测'),('evaluation','extensions','激活 / 查询'),('extensions','evaluation','当前批准'),('extensions','brain','装配 / 生命周期')]: g['component']['edges'].append(dict(from_=a,to=b,type='dependency',label=l))
for e in g['component']['edges']:e['from']=e.pop('from_')
dump(g)
def cls(id,name,owner,schema,attrs,source):return dict(id=id,name=name,owner=owner,schema=schema,attributes=[dict(name=n,type=t) for n,t in attrs],source=source)
def assoc(a,b,am,bm,l,source,constraint=''):return dict(**{'from':a,'to':b},type='association',fromMultiplicity=am,toMultiplicity=bm,label=l,source=source,constraint=constraint)
g={'module':'core','title':'跨 owner 核心对象','data':{'nodes':[
cls('task','Task','Task Home','Task',[('task_id','ID'),('goal_revision','Revision'),('status','TaskStatus'),('open_effects','ID[]'),('accounting_open','Boolean')],'task-runtime/README.md'),
cls('intent','OperationIntent','Task Home · 内部记录',None,[('operation_id','ID'),('task_id','ID'),('goal_revision','Revision'),('input_ref','ContentRef')],'task-runtime/README.md'),
cls('operation','Operation','Executor','Operation',[('operation_id','ID'),('execution_state','ExecutionState'),('effect','Effect'),('may_apply_later','Boolean | unknown'),('usage_final','Boolean')],'execution/README.md'),
cls('attempt','Attempt','Executor','Attempt',[('attempt_id','ID'),('prepared_at','Timestamp'),('sent_at','Timestamp?'),('target_key','String?')],'execution/README.md'),
cls('result','Result','Task Home','Result',[('task_id','ID'),('goal_revision','Revision'),('artifact_refs','ContentRef[1..100]'),('completion_basis','CompletionBasis')],'task-runtime/README.md'),
cls('content','ContentRef','值引用 · 定位内容 owner','ContentRef',[('owner_id','ID'),('content_id','ID'),('version','Revision'),('hash','Digest')],'contracts/README.md')],
'edges':[
assoc('task','intent','1','0..*','准入','task-runtime/implementation.md#data-flow'),
assoc('intent','operation','1','0..1','原 operation_id','execution/implementation.md#data-flow','仅已接纳正常 Invoke 的 Operation；取消先到的关闭索引不算接纳 Operation'),
assoc('operation','attempt','1','0..*','准备 / 发送尝试','execution/README.md','接纳尚未准备时可为 0；Attempt 不证明已发送，数组分页上限不是终身尝试数'),
assoc('task','result','1','0..1','完成时固定','task-runtime/implementation.md#data-flow','当前目标正式结果仅在 Task.status=succeeded 时存在'),
assoc('result','content','0..*','1..100','成果引用','task-runtime/README.md','引用可复用，不表示内容生命周期所有权'),
assoc('intent','content','0..*','1','输入引用','task-runtime/README.md','OperationIntent 固定 input_ref；具体内容访问另行获准'),
assoc('operation','content','0..*','0..101','证据 / 结果','execution/README.md','合并 evidence_refs 与可选 result_ref 的逻辑引用集合')],
'notes':['跨 owner 关联通过原身份核对，不是跨库事务','引用不授予权限；取消不删除未知效果与费用责任']},
'reading_notes':['全局类图聚焦从 Home 意图到 Executor 效果，再到正式结果的交接。OperationIntent 与 Operation 各有 owner；0..1 包含尚未派发或尚未接纳的阶段，不把网络答复当成持久效果。','OperationIntent → Operation 只表达已接纳正常 Invoke 的关联；取消先到时 Executor 可只有禁止迟到启动的关闭索引。关闭索引并不伪造成完整 Operation。','ContentRef 是按 owner、版本与摘要定位正文的值引用。类图按相同引用值合并展示可复用关系；引用的保留、删除和当前访问资格由所属规则决定，线段没有级联删除或数据库外键含义。'],'sources':['task-runtime/README.md','task-runtime/implementation.md#data-flow','execution/README.md','execution/implementation.md#data-flow','contracts/README.md']}
dump(g)
