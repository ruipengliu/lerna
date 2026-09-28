#!/usr/bin/env python3
"""Constructed field/cost specimens. Does not run Harness, models or target APIs.

ASCII object keys and safe integers make encoded() agree with JCS for these
fixtures. It is not a general JCS implementation. Public IDs use secrets once,
then an on-disk fixture registry. Scripted outputs are never runtime evidence.
"""
from pathlib import Path
from collections import Counter
from copy import deepcopy as cp
from datetime import datetime, timedelta, timezone
import hashlib
import json
import secrets
import subprocess

HERE = Path(__file__).resolve().parent
ROOT = HERE.parents[2]
SCHEMA = json.loads((ROOT / 'docs/architecture/contracts/schemas/protocol.schema.json').read_text())
METHODS = json.loads((ROOT / 'docs/architecture/contracts/schemas/methods.json').read_text())['methods']
REGPATH = HERE / 'identities.json'
REG = json.loads(REGPATH.read_text()) if REGPATH.exists() else {}
PROOFS_PATH = HERE / 'control-proofs.json'
PROOFS = json.loads(PROOFS_PATH.read_text()) if PROOFS_PATH.exists() else {}


def encoded(value):
    return json.dumps(value, ensure_ascii=False, sort_keys=True, separators=(',', ':'), allow_nan=False).encode()


def digest(raw):
    return 'sha256:' + hashlib.sha256(raw).hexdigest()


def ident(alias, prefix='id'):
    if alias not in REG:
        REG[alias] = prefix + '_' + secrets.token_hex(16)
    return REG[alias]


def save(path, value):
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps(value, ensure_ascii=False, indent=2) + '\n')


def ref(owner, object_id, revision=1):
    return dict(owner_id=owner, id=object_id, revision=revision)


def amount(n, unit='fixture_credit'):
    return dict(unit=unit, amount=str(n))


def obj(properties, required=None):
    return dict(type='object', properties=properties,
                required=list(properties) if required is None else required, additionalProperties=False)


def id_schema():
    return cp(SCHEMA['$defs']['Id'])


def cr_schema():
    return cp(SCHEMA['$defs']['ContentRef'])


def comp_schema():
    return cp(SCHEMA['$defs']['ComponentRef'])


def arr(item):
    return dict(type='array', items=item, minItems=0, maxItems=100)


STR = dict(type='string', minLength=1)
INT = dict(type='integer', minimum=1)
BOOL = dict(type='boolean')
TIME = dict(type='string', format='date-time')
TENANT = ident('tenant', 'tenant')
H = ident('orchestrator', 'orchestrator')
B = ident('brain', 'brain')
E = ident('executor', 'executor')
C = ident('content-owner', 'content_owner')
G = ident('grant-owner', 'grant_owner')
V = ident('approval-owner', 'evaluation_owner')
U = ident('user', 'user')
LOC = ident('local-host', 'endpoint')
DEVICE = ident('bound-simulator', 'device')
FILE_ROOT = ident('managed-report-root', 'resource')
NAMESPACE = ident('verified-content-collection', 'collection')
MODEL = ident('local-model-recipient', 'model')
SEARCH = ident('search-provider', 'provider')
WEB = ident('fetch-provider', 'provider')
GRANT = ref(G, ident('preinstalled-continuous-grant', 'grant'))
AUTHORIZATION = dict(kind='grant', **GRANT)
COMPONENTS = {}


def component(name, description, **extra):
    if name in COMPONENTS:
        return COMPONENTS[name]['ref']
    body = dict(fixture_only=True, name=name, description=description, version='1.0.0', **extra)
    raw = encoded(body)
    path = HERE / 'components' / (name + '.json')
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_bytes(raw)
    r = dict(id=ident('component:' + name, 'component'), version='1.0.0', digest=digest(raw))
    COMPONENTS[name] = dict(ref=r, body_file=str(path.relative_to(HERE)), byte_length=len(raw), body=body)
    return r


POLICY = component('task-policy', '演示装配：规则集合固定、修订最多一次；不构成用户授权。', max_candidate_revisions=1)
PROFILE = component('model-profile', '脚本化输出账本，每个概念模型请求记 1 fixture_credit；未调用模型、token 未知。', max_cost=amount(1), max_output_tokens=4096, physical_requests=1)
RULE_BT = component('bluetooth-enabled-rule', '同一设备、新鲜观察 enabled=true；已准入动作全部核清且不迟到。', freshness_seconds=30)
RULE_Q = component('report-quality-rule', '三个维度均有论据、推荐说明适用条件；开放质量 assessed。')
RULE_C = component('report-citation-rule', '引用定位确定性检查与语义支撑评估均通过，组合报告保留组成证据。')
RULE_F = component('file-equality-rule', '受管根和路径匹配、原写入关闭、读回字节等于候选。')
VER_BT = component('bluetooth-checker', '示例确定性谓词；非已安装或已验收实现。')
VER_Q = component('quality-checker', '固定本地模型和提示配置的脚本化期望评估；不证明质量。')
VER_C = component('citation-combiner', '确定性定位与质量支撑组合。')
VER_LOC = component('citation-locator', 'UTF-8 字节范围、摘录和来源 hash 的确定性核对。')
VER_F = component('file-checker', '候选与受管读回 hash、长度及路径的确定性核对。')
PROMPT = component('assessment-prompt', '逐条件检查，保留分歧，不以执行成功冒充质量通过。')
REPLAY = component('target-key-guarantee', '示例驱动按 task/operation 固定键及原意图去重；永久保留关闭身份。')

INPUTS = {
 'observe': obj(dict(device_id=id_schema())),
 'enable': obj(dict(device_id=id_schema(), desired=dict(const=True), expected_state_version=INT)),
 'search': obj(dict(product=STR, version=STR, official_host=STR, query=STR)),
 'fetch': obj(dict(url=dict(type='string', format='uri'), official_host=STR)),
 'assess': obj(dict(task_id=id_schema(), goal_revision=INT, artifact_ref=cr_schema(), requirement_ids=arr(id_schema()), rule_refs=arr(comp_schema()), source_refs=arr(cr_schema()))),
 'write': obj(dict(root_id=id_schema(), relative_path=STR, expected_absent=dict(const=True), content_ref=cr_schema())),
 'readback': obj(dict(root_id=id_schema(), relative_path=STR, expected_file_version=INT))
}
OUTPUTS = {
 'observe': obj(dict(device_id=id_schema(), enabled=BOOL, state_version=INT, observed_at=TIME)),
 'enable': obj(dict(device_id=id_schema(), operation_id=id_schema(), previous_state_version=INT, state_version=INT, enabled=dict(const=True), closed=dict(const=True))),
 'search': obj(dict(product=STR, version=STR, query=STR, retrieved_at=TIME, hits=arr(obj(dict(url=dict(type='string', format='uri'), title=STR, snippet=STR, official_host=STR))))),
 'fetch': obj(dict(url=dict(type='string', format='uri'), final_url=dict(type='string', format='uri'), retrieved_at=TIME, http_status=dict(const=200), body_ref=cr_schema(), official_host=STR)),
 'assess': obj(dict(task_id=id_schema(), goal_revision=INT, artifact_ref=cr_schema(), rule_refs=arr(comp_schema()), evaluator_ref=comp_schema(), model_profile_ref=comp_schema(), prompt_ref=comp_schema(), source_refs=arr(cr_schema()), judgments=arr(obj(dict(requirement_id=id_schema(), verdict=dict(enum=['pass','fail','unknown']), basis=dict(const='assessed'), reason=STR))), citation_checks=arr(obj(dict(source_ref=cr_schema(), byte_start=dict(type='integer',minimum=0), byte_end=INT, quote=STR, matched=BOOL))), limitations=arr(STR))),
 'write': obj(dict(root_id=id_schema(), relative_path=STR, operation_id=id_schema(), file_version=INT, content_hash=cp(SCHEMA['$defs']['Digest']), byte_length=dict(type='integer',minimum=0), closed=dict(const=True))),
 'readback': obj(dict(root_id=id_schema(), relative_path=STR, file_version=INT, content_ref=cr_schema(), observed_at=TIME))
}
CAPS = {}
for name in INPUTS:
    cap_ref = component('capability-' + name, '仅本评审使用的显式示例适配器 Schema；不属于冻结业务 API。', input_schema=INPUTS[name], output_schema=OUTPUTS[name])
    driver = component('driver-' + name, '脚本化驱动期望输出，未运行目标或真实适配器。')
    config = component('config-' + name, '受信配置提供真实部署时的绑定；样例资源已登记。')
    target = DEVICE if name in ['observe','enable'] else FILE_ROOT if name in ['write','readback'] else SEARCH if name=='search' else WEB if name=='fetch' else MODEL
    scope = dict(resource_owner_id=E, resource_type='simulator' if name in ['observe','enable'] else 'managed_root' if name in ['write','readback'] else 'provider', selector=dict(object_ids=[target]), normalizer_version='fixture-normalizer/1')
    read = name in ['observe','fetch','search','assess','readback']
    predicate = VER_BT if name in ['observe','enable'] else VER_F if name in ['write','readback'] else VER_Q
    capability = dict(capability_id=cap_ref['id'], version=cap_ref['version'], digest=cap_ref['digest'], description=name+' 示例能力', input_schema=INPUTS[name], output_schema=OUTPUTS[name], effect_class='read_only' if read else 'target_idempotent', verification=dict(predicate_ref=predicate,evidence_kinds=['query_result'],query_supported=True,cancel_supported=False), retry=dict(max_attempts=1,initial_backoff_ms=100,max_backoff_ms=1000,reconciliation_timeout_ms=30000), authorization=dict(resource_scopes=[scope],actions=['read' if read else 'act'],purposes=['task_execution'],requires_lease=name in ['observe','enable'],requires_confirmation=False), limits=dict(max_duration_ms=30000,max_input_bytes=131072,max_output_bytes=131072,max_physical_requests=1,cost_bound='strict',max_cost=[amount(1 if name=='assess' else 0)],mutex_domains=[target] if name in ['observe','enable','write'] else []),semantic_operation_id='fixture.'+name)
    if not read:
        capability['retry'].update(key_scope='tenant/task/operation',key_retention_ms=86400000,replay_guarantee_ref=REPLAY)
    binding = dict(binding_id=ident('binding:'+name,'binding'),revision=1,capability_ref=cap_ref,executor_id=E,target_ref=ref(E,target),driver_ref=driver,configuration_ref=config,availability='ready')
    CAPS[name] = dict(ref=cap_ref,binding_ref=dict(binding_id=binding['binding_id'],revision=1),capability=capability,binding=binding)


class Scenario:
    def __init__(self, name):
        self.name = name
        self.path = HERE / name
        self.objects = {}
        self.contents = {}
        self.exchanges = []
        self.records = {}
        self.events = []
        self.generations = []
        self.uses = []
        self.copies = {}
        self.clock = datetime(2026,9,28,1,0,tzinfo=timezone.utc)
        self.taskid = ident(name+':task','task')
        self.taskref = dict(orchestrator_id=H,task_id=self.taskid)
        self.dcount = 0
        self.ocount = 0
        self.spent = 0
        self.opresults = []
        self.requirements = []
        self.plan = None
        self.task = None
        self.lease = None
        self.invokes = []
        self.task_updates = []
        self.proofs = []

    def iid(self, label, prefix='id'):
        return ident(self.name+':'+label,prefix)

    def now(self):
        self.clock += timedelta(milliseconds=10)
        return self.clock.isoformat(timespec='milliseconds').replace('+00:00','Z')

    def object(self, alias, schema, value, producer, source, storage=False, kind=None):
        self.objects[alias] = dict(schema=schema, value=cp(value), producer=producer, source=source, json_bytes=len(encoded(value)))
        if storage:
            self.records[alias] = dict(kind=kind or schema, value=cp(value), json_bytes=len(encoded(value)))
        return value

    def internal(self, alias, value, producer, source, kind):
        return self.object(alias,'INTERNAL-DESIGN-EXAMPLE',value,producer,source,True,kind)

    def exchange(self, label, method, target, payload, output, sender=H, stage='applied', expected=None, rid=None, command_id=None):
        spec = METHODS[method]
        owner = H if method.startswith('task.') else B if method.startswith('brain.') else E if method.startswith(('execution.','resource.','capability.')) else C if method.startswith('content.') else G if method.startswith('grant.') else V
        request = dict(method=method,target_id=target,payload=cp(payload))
        if spec['kind']=='command':
            request.update(command_id=command_id or self.iid(label+':command','command'),expires_at='2026-09-28T02:00:00Z')
            if expected is not None:
                request['expected_revision']=expected
            response=dict(command_id=request['command_id'],stage=stage)
            response['accepted_at' if stage=='accepted' else 'decided_at']=self.now()
            if output is not None:
                response['output']=cp(output)
            if rid:
                response['resource_id']=rid
            record=dict(logical_service_id=owner,command_id=request['command_id'],request_digest=digest(encoded(request)),request=cp(request),receipt=cp(response))
            self.records['command:'+label]=dict(kind='CommandRecord',value=record,json_bytes=len(encoded(record)))
        else:
            response=dict(output=cp(output),observed_at=self.now())
            if isinstance(output,dict) and 'revision' in output:
                response['resource_revision']=output['revision']
        auth=dict(tenant_id=TENANT,logical_service_id=owner,actor_id=U)
        if sender!=U:
            auth['sender_service_id']=sender
        ex=dict(auth=auth,request=request,response=response)
        self.exchanges.append(dict(label=label,sender=sender,receiver=owner,exchange=ex))
        return ex

    def use(self, label, operation, action, sources, recipient, usage_owner, cost=0, scope=None, purpose='task_execution', units=1):
        scope=scope or dict(resource_owner_id=C,resource_type='verified_collection',selector=dict(object_ids=[NAMESPACE]),normalizer_version='fixture-normalizer/1')
        # This fixture pins the digest domain explicitly; no public canonical
        # business-intent projection is frozen in the referenced documents.
        intent=dict(operation_id=operation,action=action,source_refs=sources,recipient=recipient,location=LOC,resource_scopes=[scope],purpose=purpose,max_units=amount(units,'use_unit'),max_cost=amount(cost))
        h=digest(encoded(intent))
        req=dict(use_id=self.iid(label+':use','use'),operation_id=operation,intent_hash=h,grant_refs=[GRANT],source_refs=cp(sources),subject=dict(tenant_id=TENANT,actor_id=U,actor_kind='user',task_id=self.taskid),resource_scopes=[scope],action=action,purpose=purpose,recipient=recipient,location=LOC,max_units=amount(units,'use_unit'),max_cost=amount(cost),cost_bound='strict',usage_owner_id=usage_owner)
        receipt=dict(use_id=req['use_id'],owner_id=G,intent_hash=h,grant_revisions=[GRANT],decision='allowed',reserved_units=req['max_units'],reserved_cost=req['max_cost'],cost_bound='strict',start_before='2026-09-28T01:05:00Z',decided_at=self.now())
        self.exchange(label+':use','grant.use',G,req,receipt,sender=usage_owner)
        self.object(label+':UseRequest','UseRequest',req,usage_owner,'原业务身份、已登记主体/资源/Grant + fixture-intent-v1 确定性投影')
        self.object(label+':UseReceipt','UseReceipt',receipt,G,'Grant owner 锁内裁决的合成期望值',True)
        self.internal(label+':IntentProjection',intent,usage_owner,'示例摘要域；不是冻结 RPC 字段','IntentProjection')
        self.uses.append(dict(label=label,request=req,receipt=receipt,cost=cost,units=units))
        return dict(kind='use',owner_id=G,id=req['use_id'],revision=1)

    def settle_uses(self):
        for u in self.uses:
            q=u['request']; label=u['label']
            closure=ref(q['usage_owner_id'],self.iid(label+':closure','closure'))
            inp=dict(operation_id=q['operation_id'],usage_owner_id=q['usage_owner_id'],grant_refs=q['grant_refs'],usage_revision=1,cumulative_units=amount(u['units'],'use_unit'),cumulative_cost=amount(u['cost']),final=True,closure_ref=closure)
            out=dict(use_id=q['use_id'],owner_id=G,operation_id=q['operation_id'],usage_owner_id=q['usage_owner_id'],grant_refs=q['grant_refs'],revision=2,usage_revision=1,state='final',consumed_once=False,reserved_units=q['max_units'],reserved_cost=q['max_cost'],cost_bound='strict',spent_units=inp['cumulative_units'],spent_cost=inp['cumulative_cost'],held_units=amount(0,'use_unit'),held_cost=amount(0),released_units=amount(0,'use_unit'),released_cost=amount(0),closure_ref=closure)
            self.exchange(label+':settle','grant.use.settle',q['use_id'],inp,out,sender=q['usage_owner_id'],expected=1)
            self.object(label+':Settlement','UseSettlementRecord',out,G,'原 use 的合成累计计量与关闭事实；不重复计入 Task',True)
            self.internal(label+':Closure',dict(**closure,operation_id=q['operation_id'],sending_closed=True,usage_final=True),q['usage_owner_id'],'内部关闭证据；未证明运行时真实性','UsageClosure')

    def content(self, alias, body, producer, sources=None, media='application/json', preinstalled=False, source_relation='derived', version=1, content_id=None):
        sources=sources or []
        raw=encoded(body) if media=='application/json' else body.encode('utf-8')
        cr=dict(tenant_id=TENANT,owner_id=C,content_id=content_id or self.iid('content:'+alias,'content'),version=version,hash=digest(raw),media_type=media,byte_length=len(raw))
        path=self.path/'bodies'/(alias+('.json' if media=='application/json' else '.md'))
        path.parent.mkdir(parents=True,exist_ok=True);path.write_bytes(raw)
        policy=dict(classification='controlled_remote',allowed_locations=[LOC],allowed_recipients=[U,H,B,E,MODEL,SEARCH,WEB],allowed_purposes=['task_execution','task_processing','task_storage','task_display'],retention_until='2026-10-28T00:00:00Z',offline_allowed=False)
        pr=ref(C,self.iid('policy:'+alias,'content_policy'))
        sb=[]
        for source in sources:
            known=next((v for v in self.contents.values() if v['ref']==source),None)
            assert known is not None,('missing source',alias,source)
            sb.append(dict(source_ref=source,relation=source_relation,observed_at=self.now(),policy_ref=known['policy_ref']))
        commit=dict(content_ref=cr,policy_ref=pr,control_revision=1,state='active')
        self.contents[alias]=dict(ref=cr,body_file=str(path.relative_to(HERE)),body=cp(body),producer=producer,source_refs=cp(sources),policy_ref=pr,preinstalled=preinstalled)
        if not preinstalled:
            upload=self.iid('upload:'+alias,'upload')
            self.use('store:'+alias,upload,'store',sources,C,C,purpose='task_storage',units=len(raw))
            inp=dict(upload_id=upload,content_ref=cr,sources=sb,policy=policy)
            self.exchange('put:'+alias,'content.put',C,inp,commit,sender=producer)
            self.internal('content:'+alias,dict(commit=commit,sources=sb,policy=policy),C,'owner 校验已上传字节后提交；上传预留接口未冻结','ContentMetadata')
        self.object(alias+':ref','ContentRef',cr,C,'真实 fixture 字节 SHA-256/长度；身份由 registry，版本由 fixture 发布序列')
        return cr

    def read(self, alias, holder, purpose='task_processing'):
        cr=self.contents[alias]['ref']
        key=(alias,holder,purpose)
        # Task-scope cache; each consumer registers one copy of an exact body.
        if key in self.copies:
            self.events.append(dict(kind='content_cache_hit',alias=alias,holder=holder,current_permission_recheck=True))
            return cr
        label='read:'+alias+':'+holder.split('_')[0]+':'+purpose
        copyid=self.iid(label+':copy','copy')
        use=self.use(label,copyid,'read',[cr],holder,C,purpose=purpose,units=cr['byte_length'])
        inp=dict(copy_id=copyid,content_ref=cr,holder_id=holder,purpose=purpose,recipient_id=holder,retention_until='2026-09-28T02:00:00Z')
        out=dict(**inp,revision=1,use_stopped=False,physical_state='pending',evidence_refs=[])
        self.exchange(label+':register','content.register_copy',C,inp,out,sender=holder)
        request=dict(content_ref=cr,copy_id=copyid,purpose=purpose,recipient_id=holder,usage_authorization_refs=[use],mode='bytes')
        result=dict(content_ref=cr,copy_id=copyid,control_revision=1,download_id=self.iid(label+':download','download'),expires_at='2026-09-28T01:05:00Z',range_supported=False)
        self.exchange(label+':get','content.get',C,request,result,sender=holder)
        self.copies[key]=dict(label=label,input=inp,ref=cr)
        self.events.append(dict(kind='download_bytes',alias=alias,holder=holder,byte_length=cr['byte_length'],download_id=result['download_id']))
        return cr

    def close_copies(self):
        for x in self.copies.values():
            inp=dict(copy_id=x['input']['copy_id'],content_ref=x['ref'],use_stopped=True,physical_state='pending',evidence_refs=[])
            out=dict(**x['input'],revision=2,use_stopped=True,physical_state='pending',evidence_refs=[])
            self.exchange(x['label']+':release','content.release_copy',C,inp,out,sender=x['input']['holder_id'])
            self.object(x['label']+':Copy','ContentCopy',out,C,'停止使用，物理清理仍 pending；没有删除证据不写 complete',True)
            self.internal(x['label']+':cleanup',dict(copy_id=x['input']['copy_id'],holder_id=x['input']['holder_id'],content_ref=x['ref'],state='pending',due_at='2026-09-28T02:00:00Z'),C,'清理责任保留至有凭据的最终回复；不阻止 Task 成功','CleanupResponsibility')

    def job(self, kind, object_id, work=1):
        label='job:'+kind+':'+object_id
        value=dict(job_id=self.iid(label,'job'),tenant_id=TENANT,task_id=self.taskid,kind=kind,object_id=object_id,state='done',due_at='2026-09-28T01:00:00Z',work_revision=work,lease_epoch=work,lease_until='2026-09-28T01:05:00Z',attempt_count=work)
        self.internal(label,value,H,'内部最小责任槽样例；work 与 lease 恰好相同仅是无重领调度假设','Job')

    def revision(self, reason, **change):
        self.task['revision']+=1
        self.task.update(change)
        self.task['budget'][0]['spent']=amount(self.spent)
        self.task_updates.append(dict(revision=self.task['revision'],reason=reason,goal_revision=self.task['goal_revision'],control_revision=self.task['control_revision']))
        self.object('Task-r'+str(self.task['revision']),'Task',self.task,H,reason)

    def submit(self, goal, capnames):
        self.capnames=capnames
        self.goal=self.content('goal',goal,U,source_relation='user_statement')
        for name in capnames:
            self.content('catalog-'+name,dict(capability=CAPS[name]['capability'],binding=CAPS[name]['binding']),E,preinstalled=True)
            self.exchange('describe:'+name,'capability.describe',E,dict(capability_ref=CAPS[name]['ref'],binding_ref=CAPS[name]['binding_ref']),dict(capability=CAPS[name]['capability'],binding=CAPS[name]['binding']))
        self.content('policy',COMPONENTS['task-policy']['body'],H,preinstalled=True)
        self.content('rules',dict(quality=COMPONENTS['report-quality-rule'],citation=COMPONENTS['report-citation-rule'],file=COMPONENTS['file-equality-rule']) if self.name=='report' else dict(bluetooth=COMPONENTS['bluetooth-enabled-rule']),H,preinstalled=True)
        # Trusted IDs for requirements/plan are allocated outside the model.
        self.content('allocated-handles',dict(requirement_ids=[self.iid('requirement:bt','requirement')] if self.name!='report' else [self.iid('requirement:'+k,'requirement') for k in ['quality','citation','file']],plan_id=self.iid('plan','plan')),H)
        for a in ['goal','policy','rules','allocated-handles']+['catalog-'+n for n in capnames]:
            self.read(a,H)
        command=self.iid('submit-command','command')
        p=dict(goal_ref=self.goal,orchestrator_id=H,constraints=[],policy_ref=POLICY,budget=[dict(unit='fixture_credit',limit='20')],deadline='2026-09-28T02:00:00Z')
        self.task=dict(tenant_id=TENANT,task_id=self.taskid,orchestrator_id=H,submit_command_id=command,goal_ref=self.goal,goal_revision=1,requirements=[],policy_ref=POLICY,revision=1,control_revision=1,status='active',control='running',wait_reasons=[],deadline=p['deadline'],budget=[dict(unit='fixture_credit',limit=amount(20),spent=amount(0),reserved=amount(0))],open_effects=[],accounting_open=False)
        self.exchange('submit','task.submit',H,p,self.task,sender=U,command_id=command)
        self.object('Task-r1','Task',self.task,H,'接纳事务：初始条件空，预算由用户受信入口给定')
        self.task_updates.append(dict(revision=1,reason='submit',goal_revision=1,control_revision=1))

    def action(self, key, name, args, requirements=None, evidence=None):
        return dict(action_key=key,type='invoke',purpose='task_execution',requirement_refs=requirements if requirements is not None else [r['requirement_id'] for r in self.requirements],evidence_refs=evidence or [],capability_ref=CAPS[name]['ref'],binding_ref=CAPS[name]['binding_ref'],arguments=args)

    def proposal(self, actions, rationale, requirements=None, plan_ref=None):
        p=dict(kind='act',rationale=rationale,evidence_refs=[self.goal],assumptions=['全部输入为合成 fixture，能力与许可未在运行环境验证。'],actions=actions)
        if requirements:
            p['requirements_proposal']=dict(base_goal_revision=1,requirements=requirements)
        if plan_ref:
            p['plan_delta']=dict(base_plan_ref=None,next_plan_ref=plan_ref)
        return p

    def decide(self, make_proposal, needed=None):
        self.dcount+=1; label='D'+str(self.dcount)
        did=self.iid(label,'decision'); mid=self.iid(label+':model-call','model_call')
        res=ref(H,self.iid(label+':reservation','reservation'))
        self.task['budget'][0]['reserved']=amount(1)
        self.task['accounting_open']=True
        self.revision(label+' 固定预算与决策准备')
        base=['goal','policy','rules','allocated-handles']+['catalog-'+n for n in self.capnames]+(needed or [])
        base=list(dict.fromkeys(base))
        manifest=[self.contents[a]['ref'] for a in base]
        facts=[dict(kind='operation',object_ref=ref(E,x['operation_id'],x['revision']),content_ref=x['result_ref']) for x in self.opresults]
        context=dict(schema_version='brain-context/1',task_ref=self.taskref,snapshot_revision=self.task['revision'],goal_revision=self.task['goal_revision'],control_revision=self.task['control_revision'],goal_ref=self.goal,requirements=cp(self.requirements),control='running',plan_ref=self.plan,facts=facts,assumptions=['无用户接管、当前已登记缺陷门禁未命中；这是 fixture 前提。'],unresolved_effects=[],materials=[dict(content_ref=self.contents[a]['ref'],role='evidence',source_refs=self.contents[a]['source_refs']) for a in base if a!='goal'],capabilities=[dict(capability_ref=CAPS[n]['ref'],binding_ref=CAPS[n]['binding_ref'],input_schema=INPUTS[n],semantic_operation_id='fixture.'+n,effect_class=CAPS[n]['capability']['effect_class']) for n in self.capnames],gaps=[],input_manifest=manifest)
        cx=self.content(label+'-context',context,H,manifest)
        self.object(label+':BrainContext','BrainContext',context,H,'当前 Task 快照＋已查证目录＋原 Operation；无新模型摘要')
        for a in base+[label+'-context']:
            self.read(a,B)
        actual=[cx]+manifest
        request=dict(decision_id=did,task_id=self.taskid,orchestrator_id=H,snapshot_revision=self.task['revision'],context_ref=cx,capability_refs=[dict(capability_ref=CAPS[n]['ref'],binding_ref=CAPS[n]['binding_ref']) for n in self.capnames],model_profile_ref=PROFILE,limits=dict(deadline=self.task['deadline'],max_output_tokens=4096,max_actions=4,max_context_requests=4,cost_reservation_ref=res),usage_authorization_refs=[AUTHORIZATION])
        accepted=dict(decision_id=did,revision=1,snapshot_revision=request['snapshot_revision'],status='accepted')
        self.exchange(label+':decide','brain.decide',B,request,accepted,stage='accepted')
        self.object(label+':DecisionRequest','DecisionRequest',request,H,'原身份、固定上下文及策略上界；不得从模型取得许可')
        self.internal(label+':model-prepare',dict(model_call_id=mid,decision_id=did,state='prepared',max_output_tokens=4096,model_profile_ref=PROFILE),B,'模型调用身份与上界先持久化，再消费用途','ModelPreparation')
        usage=self.use(label+':process',mid,'process',actual,MODEL,B,cost=1,purpose='task_processing')
        self.approval(label+':work',did,B)
        proposal=make_proposal(actual,label)
        if not any(g['label']==label for g in self.generations):
            self.generations.append(dict(label=label,generation=dict(schema_version='brain-generation/1',contents=[],proposal=cp(proposal)),actual_input_manifest=actual,resolved_proposal=proposal,resolved_contents={}))
        modelcall=dict(model_call_id=mid,provider_request_id='fixture-provider/'+mid,state='returned',usage=[amount(1)],usage_final=True)
        record=dict(decision_id=did,revision=3,snapshot_revision=request['snapshot_revision'],status='completed',proposal=proposal,model_call=modelcall)
        self.object(label+':DecisionRecord','DecisionRecord',record,B,'脚本化模型输出经 Schema/引用解析；费用来自 fixture tariff',True)
        self.object(label+':Proposal','Proposal',proposal,B,'脚本化的期望推理结果；ID/引用从输入或发布回填')
        self.object(label+':ModelCall','ModelCall',modelcall,B,'发送准备分配 ID；provider fixture 返回请求号与 1 credit')
        self.exchange(label+':get','brain.get',did,{},record)
        original=self.records['command:'+label+':decide']
        original['value']['receipt'].update(stage='applied',decided_at=self.now(),output=cp(record))
        original['json_bytes']=len(encoded(original['value']))
        self.internal(label+':model-send',dict(model_call_id=mid,decision_id=did,input_manifest=actual,input_digest=digest(encoded(actual)),usage_authorization_refs=[usage],recipient=MODEL,location=LOC,send_started=True),B,'内部发送记录；input_digest 仅清单摘要，供应商最终编码未实现','ModelSend')
        self.internal(label+':reservation',dict(**res,task_id=self.taskid,unit='fixture_credit',maximum='1',spent='1',reserved='0',final=True,source_owner=B,source_kind='brain_decision',source_id=did,source_revision=3),H,'选择 Brain 作为 Task 唯一费用来源；Grant 投影不再扣款','BudgetReservation')
        self.spent+=1
        self.task['budget'][0]['reserved']=amount(0);self.task['accounting_open']=False
        if 'requirements_proposal' in proposal:
            self.requirements=proposal['requirements_proposal']['requirements']
            self.revision(label+' 消费：条件变更，全部行动丢弃',requirements=cp(self.requirements),goal_revision=2,control_revision=2)
        elif 'plan_delta' in proposal:
            self.plan=proposal['plan_delta']['next_plan_ref']
            self.revision(label+' 消费：只安装计划，首步不准入')
        else:
            self.revision(label+' 消费：准入独立行动候选')
        self.internal(label+':consumption',dict(task_id=self.taskid,decision_id=did,snapshot_revision=request['snapshot_revision'],reason=self.task_updates[-1]['reason']),H,'Decision 唯一消费','DecisionConsumption')
        self.job('settle',did)
        self.events.append(dict(kind='brain_model_fixture',decision_id=did,model_call_id=mid,input_manifest=actual,input_bytes=sum(x['byte_length'] for x in actual),tokens_in=None,tokens_out=None,cost=amount(1)))
        return proposal

    def approval(self,label,action,owner):
        inp=dict(use_id=self.iid(label+':approval-use','approval_use'),approval_id=ident('preinstalled-approval:'+owner,'approval'),target_id=owner,lock_id=ident('lock:'+owner,'lock'),instance_id=ident('instance:'+owner,'instance'),action_kind='work',action_id=action)
        out=dict(**inp,approval_revision=1,start_before='2026-09-28T01:05:00Z')
        self.exchange(label+':approval','evaluation.approval_check',inp['approval_id'],inp,out,sender=owner)
        self.object(label+':ApprovalUse','ApprovalUse',out,V,'合成预装批准行 + 本次工作身份；不是授权替代物',True)

    def control(self, gate):
        c=dict(gate=cp(gate),executor_id=E,issued_at=self.now(),start_before='2026-09-28T01:05:00Z')
        payload=dict(issuer=H,tenant_id=TENANT,audience=E,**{k:c[k] for k in ['gate','issued_at','start_before']})
        key=digest(encoded(payload))
        if key not in PROOFS:
            r=subprocess.run(['node',str(HERE/'proofs.mjs')],input=json.dumps(dict(payload=payload)),text=True,capture_output=True,check=True)
            PROOFS[key]=json.loads(r.stdout)['proof']
        c['orchestrator_proof']=PROOFS[key]
        self.proofs.append(dict(proof=c['orchestrator_proof'],payload=payload))
        return c

    def acquire(self):
        self.use('resource-manage',self.iid('resource-manage-action','operation'),'manage',[],E,E,scope=dict(resource_owner_id=E,resource_type='simulator',selector=dict(object_ids=[DEVICE]),normalizer_version='fixture-normalizer/1'))
        inp=dict(holder_id=E,instance_id=ident('instance:'+E,'instance'),expires_at='2026-09-28T01:05:00Z',authorization_refs=[AUTHORIZATION])
        self.lease=dict(lease_id=self.iid('resource-lease','lease'),revision=1,resource_owner_id=E,resource_id=DEVICE,holder_id=E,instance_id=inp['instance_id'],control_epoch=1,expires_at=inp['expires_at'],state='active')
        self.exchange('resource-acquire','resource.acquire',DEVICE,inp,self.lease,sender=E)
        self.object('ResourceLease','ResourceLease',self.lease,E,'资源 owner 当前控制代次与唯一占用的合成前提',True)

    def operation(self,name,args,output_fn,sources=None,step=None):
        self.ocount+=1; label='O'+str(self.ocount); opid=self.iid(label,'operation')
        source_refs=[self.contents[a]['ref'] for a in (sources or [])]
        for a in sources or []:
            self.read(a,E)
        cap=CAPS[name]
        self.revision(label+' 行动准入')
        gate=dict(**self.taskref,control_revision=self.task['control_revision'],goal_revision=self.task['goal_revision'],status='active',control='running')
        cs=self.control(gate)
        intent=dict(operation_id=opid,**self.taskref,goal_revision=2,capability_ref=cap['ref'],binding_ref=cap['binding_ref'],arguments=args)
        ih=digest(encoded(intent))
        res=ref(H,self.iid(label+':reservation','reservation'))
        invoke=dict(operation_id=opid,**self.taskref,goal_revision=2,control_snapshot=cs,capability_ref=cap['ref'],binding_ref=cap['binding_ref'],arguments=args,intent_hash=ih,authorization_refs=[AUTHORIZATION],reservation_ref=res,deadline=self.task['deadline'])
        accepted=dict(operation_id=opid,revision=1,execution_state='accepted',effect='not_started',may_apply_later=False,attempts=[],evidence_refs=[],usage=[],usage_final=False,next_action='wait')
        ex=self.exchange(label+':invoke','execution.invoke',E,invoke,accepted)
        self.object(label+':Invoke','Invoke',invoke,H,'准入从 Action/计划物化＋受信门禁、身份、预留生成')
        self.object(label+':Accepted','Operation',accepted,E,'持久接纳；尚未执行')
        self.internal(label+':IntentProjection',intent,H,'fixture-intent-v1 投影；摘要域非正式冻结合同','IntentProjection')
        self.internal(label+':OperationIntent',dict(task_id=self.taskid,operation_id=opid,invoke=invoke,command_id=ex['request']['command_id'],source=dict(plan_ref=self.plan,step_id=step) if step else dict(decision_id=self.iid('D'+str(self.dcount),'decision'))),H,'保存原命令，恢复不重拼 Invoke','OperationIntent')
        self.approval(label+':work',opid,E)
        action='read' if name in ['observe','search','fetch','readback'] else 'process' if name=='assess' else 'act'
        self.use(label+':target',opid,action,source_refs,MODEL if name=='assess' else E,E,cost=1 if name=='assess' else 0,scope=cap['capability']['authorization']['resource_scopes'][0])
        if name in ['search','fetch']:
            self.use(label+':disclose',opid,'disclose',source_refs,SEARCH if name=='search' else WEB,E,scope=cap['capability']['authorization']['resource_scopes'][0])
        body,extra=output_fn(opid)
        output_sources=source_refs+extra
        outref=self.content(label+'-output',body,E,output_sources)
        attempt=dict(attempt_id=self.iid(label+':attempt','attempt'),prepared_at=self.now(),sent_at=self.now(),target_key=opid)
        result=dict(operation_id=opid,revision=3,execution_state='closed',effect='applied',may_apply_later=False,attempts=[attempt],evidence_refs=[outref],result_ref=outref,usage=[amount(1 if name=='assess' else 0)],usage_final=True,next_action='none')
        if name in ['enable','write']:
            result['target_receipt_ref']=outref
        self.object(label+':Operation','Operation',result,E,'脚本化驱动返回；真实输出由 owner 写字节后固定引用',True)
        self.exchange(label+':get','execution.get',opid,{},result)
        self.read(label+'-output',H)
        self.opresults.append(result);self.invokes.append(invoke)
        self.internal(label+':ReceivedFact',dict(owner=E,object_id=opid,revision=3,digest=digest(encoded(result)),content_ref=outref),H,'按 owner/object/revision 唯一归并','ReceivedFact')
        self.internal(label+':reservation',dict(**res,task_id=self.taskid,unit='fixture_credit',maximum=str(1 if name=='assess' else 0),spent=str(1 if name=='assess' else 0),reserved='0',final=True,source_owner=E,source_kind='execution_operation',source_id=opid,source_revision=3),H,'唯一费用来源选 Executor，Grant/评估内部模型不二次计费','BudgetReservation')
        if step:
            self.internal(label+':StepAdmission',dict(task_id=self.taskid,plan_ref=self.plan,step_id=step,operation_id=opid,arguments=cp(args),candidate_digest=digest(encoded(args))),H,'同计划版本步骤唯一准入；物化不是 Decision','PlanStepAdmission')
        self.spent+=1 if name=='assess' else 0
        self.revision(label+' 终结事实与费用归并')
        self.events.append(dict(kind='target_fixture',capability=name,operation_id=opid,physical_requests=1,model_requests=1 if name=='assess' else 0))
        for kind in ['dispatch','poll','settle']:
            self.job(kind,opid)
        return outref,body

    def check(self,key,artifact,evidence,verdict='pass',basis='verified',evaluator=None,dependencies=None,operation_id=None):
        req=next(r for r in self.requirements if r['requirement_id']==self.iid('requirement:'+key,'requirement'))
        result=dict(requirement_id=req['requirement_id'],goal_revision=2,artifact_ref=artifact,verdict=verdict,basis=basis,evidence_refs=evidence,evaluator_ref=evaluator or VER_BT)
        checkid=self.iid('check:'+key,'check')
        self.approval('check:'+key,checkid,H)
        self.object('ConditionResult-'+key,'ConditionResult',result,H,'固定规则与当前证据的脚本化期望判断')
        internal=dict(check_id=checkid,task_id=self.taskid,goal_revision=2,requirement_id=req['requirement_id'],artifact_ref=artifact,rule_ref=req['rule_ref'],evaluator_ref=result['evaluator_ref'],policy_ref=POLICY,result=result,applicability='usable',selected=True,dependency_check_ids=dependencies or [],evidence_gate_revision=1)
        if operation_id:
            internal['operation_id']=operation_id
        self.internal('check:'+key,internal,H,'内部 ConditionCheck，公开判断沿 requirement 找 rule','ConditionCheck')
        return result

    def finish(self,artifact,conditions):
        # Control shutdown is after result; target resource release is distinct.
        result=dict(task_id=self.taskid,goal_revision=2,artifact_refs=[artifact],completion_basis='assessed' if self.name=='report' else 'verified',condition_results=conditions,limitations=['合成静态样例；未运行模型、官方网站、数据库或真实设备。','只反映指定观察时点；当前缺陷范围仅 fixture 内已登记记录。'],completed_at=self.now())
        rr=self.content('result',result,H,[artifact]+list({c['content_id']:c for x in conditions for c in x['evidence_refs'] if c!=artifact}.values()))
        self.object('Result','Result',result,H,'当前全部必要条件汇总，最弱依据 assessed/verified',True)
        self.revision('最终核验成功与关闭资格',status='succeeded',control_revision=3,result_ref=rr,open_effects=[],accounting_open=False)
        self.records['task-final']=dict(kind='Task',value=cp(self.task),json_bytes=len(encoded(self.task)))
        self.object('Task-final','Task',self.task,H,'fixture 提交序列的最终状态')
        gate=dict(**self.taskref,control_revision=3,goal_revision=2,status='succeeded',control='running')
        cs=self.control(gate)
        control_result=dict(gate=gate,enforced_control_revision=3,entrances=[dict(entrance_id=ident('executor-entrance','entrance'),enforced_control_revision=3)],inflight_operation_ids=[],observed_at=self.now())
        self.exchange('final-control','execution.control',E,cs,control_result)
        self.object('ControlReceipt','ControlReceipt',control_result,E,'fixture 单一入口已关闭；不证明分布式即时停机',True)
        self.object('TaskGate','TaskGate',gate,E,'最终控制消息单调应用后的持久启动门禁',True)
        self.internal('executor-binding',dict(task_id=self.taskid,executor_id=E,goal_revision=2,control_revision=3),H,'首次 Invoke 登记，终态传播仍能枚举原执行端','TaskExecutorBinding')
        if self.lease:
            state=dict(resource_owner_id=E,resource_id=DEVICE,control_epoch=1,user_control=False,inflight_operation_ids=[])
            self.exchange('resource-release','resource.release',DEVICE,dict(expected_control_epoch=1,authorization_refs=[AUTHORIZATION]),state,sender=E)
            self.lease['revision']=2;self.lease['state']='released'
            self.records['ResourceLease']['value']=cp(self.lease);self.records['ResourceLease']['json_bytes']=len(encoded(self.lease))
        self.exchange('task-read','task.read',self.taskid,{},self.task,sender=U)
        self.exchange('task-result','task.result',self.taskid,{},dict(status='succeeded',result=result),sender=U)
        self.read('result',U,'task_display')
        if self.name=='report':
            self.read('report',U,'task_display')
        self.close_copies();self.settle_uses()
        self.job('decide',self.taskid,self.dcount+(0 if not self.plan else 2 if self.name!='report' else 3))
        self.job('verify',self.taskid)
        self.job('control',E)
        # Explicit minimum closure example, not a claim of complete physical DB.
        closing=[('task',self.taskid,H,digest(encoded(self.task)))]
        closing += [('decision',self.iid('D'+str(i),'decision'),B,digest(encoded(self.objects['D'+str(i)+':DecisionRequest']['value']))) for i in range(1,self.dcount+1)]
        closing += [('operation',x['operation_id'],E,x['intent_hash']) for x in self.invokes]
        closing += [('command',x['value']['command_id'],x['value']['logical_service_id'],x['value']['request_digest']) for x in list(self.records.values()) if x['kind']=='CommandRecord']
        closing += [('use',x['request']['use_id'],G,x['request']['intent_hash']) for x in self.uses]
        closures=[dict(tenant_id=TENANT,owner_id=owner,identity_kind=kind,object_id=iid,request_digest=dg,decision='closed',revision=1) for kind,iid,owner,dg in closing]
        methods=Counter(x['exchange']['request']['method'] for x in self.exchanges)
        rows=Counter(x['kind'] for x in self.records.values())
        newcontents=[x for x in self.contents.values() if not x['preinstalled']]
        wire=sum(len(encoded(x['exchange']['request']))+len(encoded(x['exchange']['response'])) for x in self.exchanges)
        logical=sum(x['json_bytes'] for x in self.records.values())
        stats=dict(brain_calls=self.dcount,assessment_model_calls=1 if self.name=='report' else 0,operations=self.ocount,methods=dict(sorted(methods.items())),protocol_pairs=len(self.exchanges),request_response_json_bytes=wire,retained_record_kinds=dict(sorted(rows.items())),retained_record_count=len(self.records),retained_record_json_bytes=logical,new_body_count=len(newcontents),new_body_bytes=sum(x['ref']['byte_length'] for x in newcontents),preinstalled_body_count=len(self.contents)-len(newcontents),preinstalled_body_bytes=sum(x['ref']['byte_length'] for x in self.contents.values() if x['preinstalled']),copy_count=len(self.copies),download_bytes=sum(x['byte_length'] for x in self.events if x['kind']=='download_bytes'),use_count=len(self.uses),minimum_closure_count=len(closures),minimum_closure_json_bytes=sum(len(encoded(x)) for x in closures),fixture_credit=self.spent,model_input_body_bytes=sum(x['input_bytes'] for x in self.events if x['kind']=='brain_model_fixture'),model_tokens=None,physical_db_bytes=None)
        bundle=dict(schema='scenario-review-fixture/1',warning='Constructed expectations only; no Harness/model/target runtime executed.',task_id=self.taskid,objects=self.objects,contents=self.contents,exchanges=self.exchanges,records=self.records,minimum_closures=closures,task_revision_log=self.task_updates,events=self.events,generations=self.generations,control_proofs=self.proofs,statistics=stats)
        save(self.path/'scenario.json',bundle)
        return bundle


def req(s,key,kind,rule):
    return dict(requirement_id=s.iid('requirement:'+key,'requirement'),kind=kind,source_ref=s.goal,rule_ref=rule,required=True)


def plan_publish(s,label,actual,steps,report=None):
    plan=dict(schema_version='brain-plan/1',plan_id=s.iid('plan','plan'),revision=1,task_ref=s.taskref,goal_revision=2,steps=steps,source_refs=actual+([report] if report else []))
    p=s.content('plan',plan,B,plan['source_refs'])
    s.object('BrainPlan','BrainPlan',plan,B,'预分配 plan_id、模型规划模板、Brain 完整来源回填')
    return p


def bluetooth(initial):
    s=Scenario('bluetooth-on' if initial else 'bluetooth-off')
    s.submit(dict(user_text='打开绑定手机蓝牙',device_id=DEVICE,binding_origin='受信会话已绑定的有状态模拟设备',desired=True),['observe','enable'])
    requirements=[req(s,'bt','effect',RULE_BT)]
    observation=s.action('observe','observe',dict(device_id=DEVICE),requirements=[requirements[0]['requirement_id']])
    s.decide(lambda actual,label:s.proposal([observation],'把原用户目标补全为固定条件。',requirements))
    s.decide(lambda actual,label:s.proposal([observation],'读取设备当前状态。'))
    s.acquire()
    a1,b1=s.operation('observe',dict(device_id=DEVICE),lambda op:(dict(device_id=DEVICE,enabled=initial,state_version=1,observed_at=s.now()),[]),sources=['goal'])
    if initial:
        return s.finish(a1,[s.check('bt',a1,[a1])])
    def make_plan(actual,label):
        steps=[dict(step_id='enable',requirement_refs=[requirements[0]['requirement_id']],depends_on=[],instruction='按刚取得版本设为开启。',action_template=s.action('enable','enable',dict(device_id=DEVICE,desired=True,expected_state_version=1),evidence=[a1])),dict(step_id='observe_after',requirement_refs=[requirements[0]['requirement_id']],depends_on=['enable'],instruction='设置核清后重新观察。',action_template=s.action('observe_after','observe',dict(device_id=DEVICE)))]
        p=plan_publish(s,label,actual,steps)
        proposal=s.proposal([],'固定两步计划；安装后分别物化。',plan_ref=p)
        template=cp(proposal);template['plan_delta']['next_plan_ref']={'$local_ref':'plan'}
        body=cp(s.contents['plan']['body']);body['source_refs']=[]
        s.generations.append(dict(label=label,generation=dict(schema_version='brain-generation/1',contents=[dict(local_id='plan',media_type='application/json',body=body)],proposal=template),actual_input_manifest=actual,resolved_proposal=proposal,resolved_contents=dict(plan=p)))
        return proposal
    s.decide(make_plan,['O1-output'])
    a2,b2=s.operation('enable',dict(device_id=DEVICE,desired=True,expected_state_version=1),lambda op:(dict(device_id=DEVICE,operation_id=op,previous_state_version=1,state_version=2,enabled=True,closed=True),[]),sources=['O1-output'],step='enable')
    a3,b3=s.operation('observe',dict(device_id=DEVICE),lambda op:(dict(device_id=DEVICE,enabled=True,state_version=2,observed_at=s.now()),[]),sources=['O2-output'],step='observe_after')
    return s.finish(a3,[s.check('bt',a3,[a3,a2])])


def report():
    s=Scenario('report')
    s.submit(dict(user_text='比较 Atlas 1.0 与 Boreal 1.0，核实官方来源，按部署、功能限制、维护成本给出推荐并保存读回。',products=[dict(name='Atlas',version='1.0',official_host='atlas.example'),dict(name='Boreal',version='1.0',official_host='boreal.example')],dimensions=['部署方式','功能限制','维护成本'],root_id=FILE_ROOT,relative_path='reports/comparison.md',fixture_notice='产品、域名和资料均虚构；未进行官方来源核实。'),['search','fetch','assess','write','readback'])
    requirements=[req(s,'quality','quality',RULE_Q),req(s,'citation','quality',RULE_C),req(s,'file','effect',RULE_F)]
    def search_args(product):
        return dict(product=product,version='1.0',official_host=product.lower()+'.example',query=product+' 1.0 deployment limits maintenance')
    actions=[s.action('search-'+p.lower(),'search',search_args(p),requirements=[r['requirement_id'] for r in requirements]) for p in ['Atlas','Boreal']]
    s.decide(lambda actual,label:s.proposal(actions,'固定比较、引用和文件三项条件。',requirements))
    s.decide(lambda actual,label:s.proposal(actions,'分别定位两个产品的官方资料。'))
    urls=[]
    for p in ['Atlas','Boreal']:
        pa=search_args(p)
        hits=[dict(url='https://'+pa['official_host']+'/1.0/'+x,title=p+' '+x,snippet='合成搜索摘录，只用于定位。',official_host=pa['official_host']) for x in ['deployment','limits']]
        urls+=hits
        s.operation('search',pa,lambda op,p=p,pa=pa,hits=hits:(dict(product=p,version='1.0',query=pa['query'],retrieved_at=s.now(),hits=hits),[]),sources=['goal'])
    fetchactions=[s.action('fetch-'+str(i+1),'fetch',dict(url=x['url'],official_host=x['official_host']),evidence=[s.contents['O1-output' if i<2 else 'O2-output']['ref']]) for i,x in enumerate(urls)]
    s.decide(lambda actual,label:s.proposal(fetchactions,'从实际搜索输出复制 URL，读取四篇正文。'),['O1-output','O2-output'])
    texts=[
      '# Atlas 1.0 deployment\nAtlas runs in one process with an embedded store.\n',
      '# Atlas 1.0 limits\nAtlas has no multi-node failover. Backups are operator-managed.\n',
      '# Boreal 1.0 deployment\nBoreal requires an external database and two workers.\n',
      '# Boreal 1.0 limits\nBoreal supports worker replacement. Operators maintain database backups.\n'
    ]
    src=[]
    for i,x in enumerate(urls):
        def fetchout(op,i=i,x=x):
            bodyref=s.content('source-'+str(i+1),texts[i],E,[s.contents['O1-output' if i<2 else 'O2-output']['ref']],media='text/markdown',source_relation='observation')
            src.append(bodyref)
            return dict(url=x['url'],final_url=x['url'],retrieved_at=s.now(),http_status=200,body_ref=bodyref,official_host=x['official_host']),[bodyref]
        s.operation('fetch',dict(url=x['url'],official_host=x['official_host']),fetchout,sources=['O1-output' if i<2 else 'O2-output'])
    reporttext='# Atlas 与 Boreal 比较（合成资料示例）\n\n| 维度 | Atlas 1.0 | Boreal 1.0 |\n| --- | --- | --- |\n| 部署 | 单进程、嵌入式存储 [A1] | 外部数据库、两个 worker [B1] |\n| 限制 | 不支持多节点故障转移 [A2] | 支持 worker 替换 [B2] |\n| 维护 | 操作者管理备份 [A2] | 操作者维护数据库备份 [B2] |\n\n小型单机使用优先评估 Atlas；需要 worker 替换时评估 Boreal，并承担外部数据库维护。资料没有人时或费用测量，不能断言 Boreal 的总维护成本必然更高。\n\n[A1]: https://atlas.example/1.0/deployment\n[A2]: https://atlas.example/1.0/limits\n[B1]: https://boreal.example/1.0/deployment\n[B2]: https://boreal.example/1.0/limits\n\n这些是虚构产品和合成来源，报告不用于真实软件选型。\n'
    def synthesize(actual,label):
        candidate=s.content('report',reporttext,B,actual,media='text/markdown')
        assess=s.action('assess','assess',dict(task_id=s.taskid,goal_revision=2,artifact_ref=candidate,requirement_ids=[r['requirement_id'] for r in requirements[:2]],rule_refs=[RULE_Q,RULE_C],source_refs=src))
        write=s.action('write','write',dict(root_id=FILE_ROOT,relative_path='reports/comparison.md',expected_absent=True,content_ref=candidate))
        read=s.action('readback','readback',dict(root_id=FILE_ROOT,relative_path='reports/comparison.md'))
        steps=[dict(step_id='assess',requirement_refs=[r['requirement_id'] for r in requirements[:2]],depends_on=[],instruction='按固定规则联合评估。',action_template=assess),dict(step_id='write',requirement_refs=[requirements[2]['requirement_id']],depends_on=['assess'],pass_conditions=[dict(requirement_id=r['requirement_id'],rule_ref=r['rule_ref'],artifact_ref=candidate) for r in requirements[:2]],instruction='两项当前有效 pass 后写入。',action_template=write),dict(step_id='readback',requirement_refs=[requirements[2]['requirement_id']],depends_on=['write'],instruction='从原写入输出读取版本，再读回。',action_template=read,argument_bindings=[dict(target_pointer='/arguments/expected_file_version',source=dict(kind='step_output',step_id='write',source_pointer='/file_version'))])]
        p=plan_publish(s,label,actual,steps,candidate)
        # Model-facing output shows new bodies with local refs, never invented digests.
        planbody=cp(s.contents['plan']['body']);planbody['source_refs']=[]
        def localize(v):
            if isinstance(v,dict):
                if v==candidate:return {'$local_ref':'report'}
                if v==p:return {'$local_ref':'plan'}
                return {k:localize(x) for k,x in v.items()}
            if isinstance(v,list):return [localize(x) for x in v]
            return v
        proposal=s.proposal([],'综合实际取得的四篇资料，并固定评估、写入和读回。',plan_ref=p)
        generation=dict(schema_version='brain-generation/1',contents=[dict(local_id='report',media_type='text/markdown',body=reporttext),dict(local_id='plan',media_type='application/json',body=localize(planbody))],proposal=localize(proposal))
        s.generations.append(dict(label=label,generation=generation,actual_input_manifest=actual,resolved_proposal=proposal,resolved_contents=dict(report=candidate,plan=p)))
        return proposal
    s.decide(synthesize,['O1-output','O2-output']+['O'+str(i)+'-output' for i in range(3,7)]+['source-'+str(i) for i in range(1,5)])
    candidate=s.contents['report']['ref']
    for a in ['source-'+str(i) for i in range(1,5)]:
        s.read(a,H)
    def assessout(op):
        checks=[]
        for i,t in enumerate(texts):
            quote=t.splitlines()[1];raw=t.encode();b=raw.index(quote.encode())
            checks.append(dict(source_ref=src[i],byte_start=b,byte_end=b+len(quote.encode()),quote=quote,matched=True))
        return dict(task_id=s.taskid,goal_revision=2,artifact_ref=candidate,rule_refs=[RULE_Q,RULE_C],evaluator_ref=VER_Q,model_profile_ref=PROFILE,prompt_ref=PROMPT,source_refs=src,judgments=[dict(requirement_id=r['requirement_id'],verdict='pass',basis='assessed',reason='脚本化期望：维度齐全且引用支持；不是实际模型结论。') for r in requirements[:2]],citation_checks=checks,limitations=['语义支撑是 assessed；产品和网页 fixture 不证明真实官方来源。']),[]
    assessargs=s.contents['plan']['body']['steps'][0]['action_template']['arguments']
    assessment,assessmentbody=s.operation('assess',assessargs,assessout,sources=['report']+['source-'+str(i) for i in range(1,5)],step='assess')
    selfop=s.opresults[-1]['operation_id']
    q=s.check('quality',candidate,[assessment]+src,basis='assessed',evaluator=VER_Q,operation_id=selfop)
    sub=s.iid('check:citation-location','check')
    s.approval('check:citation-location',sub,H)
    s.internal('check:citation-location',dict(check_id=sub,task_id=s.taskid,goal_revision=2,requirement_id=requirements[1]['requirement_id'],artifact_ref=candidate,rule_ref=RULE_C,evaluator_ref=VER_LOC,verdict='pass',evidence_refs=[assessment]+src,applicability='usable',evidence_gate_revision=1),H,'UTF-8 定位检查组成记录，不是新增公共 Requirement','ConditionCheck')
    c=s.check('citation',candidate,[assessment]+src,basis='assessed',evaluator=VER_C,dependencies=[sub,s.iid('check:quality','check')],operation_id=selfop)
    writeargs=s.contents['plan']['body']['steps'][1]['action_template']['arguments']
    write,writebody=s.operation('write',writeargs,lambda op:(dict(root_id=FILE_ROOT,relative_path='reports/comparison.md',operation_id=op,file_version=1,content_hash=candidate['hash'],byte_length=candidate['byte_length'],closed=True),[]),sources=['report','O7-output'],step='write')
    # Readback has independent identity and provenance even when bytes are equal.
    def readout(op):
        rb=s.content('readback-bytes',reporttext,E,[write],media='text/markdown',source_relation='observation')
        return dict(root_id=FILE_ROOT,relative_path='reports/comparison.md',file_version=writebody['file_version'],content_ref=rb,observed_at=s.now()),[rb]
    rb,rbody=s.operation('readback',dict(root_id=FILE_ROOT,relative_path='reports/comparison.md',expected_file_version=writebody['file_version']),readout,sources=['O8-output'],step='readback')
    s.read('readback-bytes',H)
    f=s.check('file',candidate,[write,rb,s.contents['readback-bytes']['ref']],evaluator=VER_F)
    return s.finish(candidate,[q,c,f])


def main():
    scenarios={}
    for fn in [lambda:bluetooth(True),lambda:bluetooth(False),report]:
        bundle=fn()
        name=next(k for k in ['bluetooth-on','bluetooth-off','report'] if bundle['task_id']==ident(k+':task','task'))
        scenarios[name]=bundle['statistics']
    save(REGPATH,REG)
    save(PROOFS_PATH,PROOFS)
    save(HERE/'shared.json',dict(warning='Fixture identities/components/configuration are not live grants, releases or runtime evidence.',identities={k:v for k,v in REG.items() if not ':' in k},components=COMPONENTS,capabilities=CAPS,grant_ref=GRANT,grant_precondition=dict(authorization_source='受信用户预授权数据库快照是假设；本包没有伪造已完成 Confirmation。',mode='continuous',state='active',revision=1,allowed_actions=['read','process','store','act','disclose','manage'],scope_membership='精确已登记资源和 verified-content-collection 的受信成员；归属查询接口待实现',quota='fixture 足够，未访问真实额度'),statistics=scenarios))
    save(HERE/'statistics.json',scenarios)
    print(json.dumps({k:{p:v[p] for p in ['brain_calls','assessment_model_calls','operations','protocol_pairs','retained_record_count','retained_record_json_bytes','new_body_count','new_body_bytes','use_count','minimum_closure_count']} for k,v in scenarios.items()},ensure_ascii=False,indent=2))


if __name__=='__main__':
    main()
