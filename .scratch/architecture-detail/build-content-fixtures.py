import json,copy,hashlib
from pathlib import Path
BASE=Path('docs/architecture/contracts/examples/protocol')
patch=json.load(open('.scratch/architecture-detail/content-protocol-patch.json'))
reg=json.load(open('docs/architecture/contracts/schemas/methods.json'))['methods'];reg.update(patch['methods'])
T='2026-09-26T00:00:00Z';F='2026-09-26T00:10:00Z';END='2026-09-27T00:00:00Z'
def id(s):return s+'_'+hashlib.md5(s.encode()).hexdigest()
def dig(s):return 'sha256:'+hashlib.sha256(s.encode()).hexdigest()
def hashv(v):return dig(json.dumps(v,sort_keys=True,separators=(',',':'),ensure_ascii=False))
def cr(s,owner=None):return {'tenant_id':id('tenant'),'owner_id':owner or id('content_owner'),'content_id':id(s),'version':1,'hash':dig(s),'media_type':'text/plain','byte_length':16}
def ore(s,owner=None,rev=1):return {'owner_id':owner or id('memory_owner'),'id':id(s),'revision':rev}
def comp(s):return {'id':id(s),'version':'1.0.0','digest':dig(s)}
def scope():return {'task_types':['technical_review'],'resource_ids':[],'purpose_tags':['personalization']}
def policy():return {'classification':'controlled_remote','allowed_locations':[id('local_endpoint'),id('remote_endpoint')],'allowed_recipients':[id('actor'),id('model')],'allowed_purposes':['review','memory'],'retention_until':END,'offline_allowed':False}
def source():return {'source_ref':cr('source'),'relation':'user_statement','observed_at':T,'policy_ref':ore('source_policy',id('content_owner'))}
def memory(s):return {'memory_id':id(s),'owner_id':id('memory_owner'),'revision':1,'type':'preference','content_ref':cr(s+'_content'),'sources':[source()],'scope':scope(),'observed_at':T,'state':'active','policy_ref':ore('memory_policy')}
def memwrite(m):return {k:copy.deepcopy(v) for k,v in m.items() if k in ('type','content_ref','sources','scope','observed_at','confidence','policy_ref')}
def control(m):return {k:copy.deepcopy(m[k]) for k in ('memory_id','owner_id','revision','state','policy_ref')}
def snap(label='Ready',source_revision=None):
 v={'title':'Document review','blocks':[{'kind':'status','block_id':'progress','code':'working','label':label}],'request_refs':[],'expires_at':END}
 if source_revision is not None:v['source_revision']=source_revision
 return v
def surface(s='surface',snapshot=None,task=False):
 v={'surface_id':id(s),'surface_owner_id':id('surface_owner'),'app_binding':comp('memory_manager'),'revision':1,'snapshot':snapshot or snap()}
 if task:v['task_ref']={'home_id':id('home'),'task_id':id('task')}
 return v
counter=0

def ex(name,p,out=None,target=None,expected=None,error=None):
 global counter
 counter+=1;sp=reg[name];command=sp['kind']=='command'
 if target is None:
  if name.startswith('content.'):target=p['content_ref']['owner_id']
  elif name=='interaction.request_read':target=p['request_ref']['owner_id']
  elif name.startswith('interaction.'):target=id('surface_owner') if name.endswith(('create','list')) else p.get('surface_id',id('surface'))
  elif name.startswith('memory.view.') and not name.endswith('open'):target=p['view_id']
  elif name=='memory.cleanup.get':target=p['object_ref']['owner_id']
  else:target=id('memory_owner')
 auth={'tenant_id':id('tenant'),'logical_service_id': id('surface_owner') if name.startswith('interaction.') else (id('content_owner') if name.startswith('content.') else id('memory_owner')),'actor_id':id('actor')}
 if name=='interaction.request_read':auth['logical_service_id']=p['request_ref']['owner_id']
 r={'method':name,'target_id':target,'payload':copy.deepcopy(p)}
 if command:r={'command_id':id('command_'+str(counter)),**r,'expires_at':F}
 if expected is not None:r['expected_revision']=expected
 if error:
  o={'error':{'code':error,'message':'The original request is not applicable.','retry':'after_change'}}
  if command:o.update(command_id=r['command_id'],stage='rejected',decided_at=T)
 elif command:o={'command_id':r['command_id'],'stage':'applied','decided_at':T,'output':copy.deepcopy(out)}
 else:o={'output':copy.deepcopy(out),'observed_at':T}
 return {'at':T,'exchange':{'auth':auth,'request':r,'response':o}}

def trace(n,desc,events,input_requests=None):return {'name':n,'description':desc,'capabilities':[],'input_requests':input_requests or [],'approvals':[],'events':events}
fixtures={}
# 40: immutable bytes, pre-delivery registration, closed source and remaining cleanup.
c=cr('report');pol=policy();copy1={'copy_id':id('copy'),'content_ref':c,'holder_id':id('ui_holder'),'purpose':'review','recipient_id':id('actor'),'retention_until':END,'revision':1,'use_stopped':False,'physical_state':'pending','evidence_refs':[]}
putp={'upload_id':id('upload'),'content_ref':c,'sources':[source()],'policy':pol};puto={'content_ref':c,'policy_ref':ore('content_policy',id('content_owner')),'control_revision':1,'state':'active'}
regp={k:copy.deepcopy(copy1[k]) for k in ('copy_id','content_ref','holder_id','purpose','recipient_id','retention_until')}
getp={'content_ref':c,'copy_id':id('copy'),'purpose':'review','recipient_id':id('actor'),'usage_authorization_refs':[{'kind':'use','owner_id':id('grant_owner'),'id':id('use'),'revision':1}]}
geto={'content_ref':c,'copy_id':id('copy'),'control_revision':1,'download_id':id('download'),'expires_at':F,'range_supported':True}
closep={'content_ref':c,'mode':'close','reason':'User closed the original source.'};closeo={'content_ref':c,'control_revision':2,'state':'closed','policy_ref':puto['policy_ref'],'cleanup_ref':ore('cleanup',id('content_owner'),2)}
releasep={'copy_id':id('copy'),'content_ref':c,'use_stopped':True,'physical_state':'complete','evidence_refs':[cr('cleanup_evidence')]};releaseo=copy.deepcopy(copy1);releaseo.update(releasep,revision=2)
cleanup_p={'object_ref':ore('report',id('content_owner'),1),'closure_revision':2}
cleanup_o={**cleanup_p,'holders':[{'holder_id':id('ui_holder'),'copy_id':id('copy'),'use_stopped':True,'physical_state':'complete'}],'physical_state':'complete'}
evs=[ex('content.put',putp,puto),ex('content.register_copy',regp,copy1),ex('content.get',getp,geto),ex('content.close',closep,closeo,expected=1),ex('content.release_copy',releasep,releaseo),ex('memory.cleanup.get',cleanup_p,cleanup_o)]
fixtures['40-content-lifecycle.json']=trace('content-lifecycle','Upload adoption, pre-delivery copy, bounded download, source close and separate cleanup.',evs)
# 41: query and management list preserve exact owner and stable finite results.
m=memory('preference');q={'query_id':id('query'),'owner_ids':[id('memory_owner')],'text_terms':['review'],'types':['preference'],'scope':scope(),'purpose':'review','recipient_id':id('actor'),'limit':20}
page={'query_id':id('query'),'owner_id':id('memory_owner'),'items':[m],'position':0,'scanned_count':1,'skipped_count':0,'exhausted':True,'partial':False,'changed':False,'gaps':[]}
listp={'query_id':id('management_query'),'types':['preference'],'states':['active'],'limit':20}
listo={'query_id':id('management_query'),'owner_id':id('memory_owner'),'items':[control(m)],'exhausted':True,'partial':False,'gaps':[]}
fixtures['41-memory-query-list.json']=trace('memory-query-list','Read exact active revisions; management uses controls without requiring old content.',[ex('memory.create',memwrite(m),m),ex('memory.query',q,page),ex('memory.list',listp,listo)])
# 42: task creation is not publication; explicit candidate confirmation reuses create.
extractp={'extraction_id':id('extraction'),'home_id':id('home'),'input_refs':[cr('source')],'rule_ref':comp('extraction_policy'),'save_mode':'confirm','usage_authorization_refs':[{'kind':'use','owner_id':id('grant_owner'),'id':id('extraction_use'),'revision':1}],'limits':{'max_candidates':10,'max_input_bytes':1024,'deadline':F,'cost_reservation_ref':ore('extraction_budget',id('home'))}}
extracto={'extraction_id':id('extraction'),'extraction_task_id':id('extraction_task'),'home_id':id('home'),'owner_id':id('memory_owner'),'input_digest':hashv(extractp['input_refs']),'state':'queued'}
mp=memwrite(m);mp['extraction_candidate_ref']=ore('candidate')
fixtures['42-memory-extraction.json']=trace('memory-extraction','Accepted finite extraction maps to one Home task; an explicit candidate create publishes once.',[ex('memory.extract',extractp,extracto),ex('memory.create',mp,m)])
# 43: only a delivered original page may be acknowledged.
vp={'view_id':id('view'),'recipient_id':id('endpoint'),'filter':{'types':['preference'],'scope':scope()},'projection':'metadata','purpose':'review','retention_until':END,'expires_at':END}
vo={**copy.deepcopy(vp),'owner_id':id('memory_owner'),'revision':1,'snapshot_cursor':'snapshot:0','state':'active'}
pp={'view_id':id('view'),'cursor':'snapshot:0','limit':20};po={'view_id':id('view'),'page_id':id('page'),'phase':'snapshot','from_cursor':'snapshot:0','next_cursor':'changes:10','through_sequence':10,'items':[{'kind':'upsert','sequence':10,'record':control(m)}],'exhausted':True}
ap={'view_id':id('view'),'page_id':id('page'),'through_sequence':10,'applied_cursor':'changes:10'};ao={'view_id':id('view'),'page_id':id('page'),'acked_sequence':10,'acked_cursor':'changes:10'}
pp2={**pp,'cursor':'changes:10'};po2={'view_id':id('view'),'page_id':id('page_two'),'phase':'changes','from_cursor':'changes:10','next_cursor':'changes:11','through_sequence':11,'items':[{'kind':'tombstone','sequence':11,'memory_id':m['memory_id'],'revision':2}],'exhausted':True}
ap2={'view_id':id('view'),'page_id':id('page_two'),'through_sequence':11,'applied_cursor':'changes:11'};ao2={'view_id':id('view'),'page_id':id('page_two'),'acked_sequence':11,'acked_cursor':'changes:11'}
fixtures['43-memory-view-sync.json']=trace('memory-view-sync','Snapshot then continuous tombstone, each persisted before original-page ACK.',[ex('memory.view.open',vp,vo),ex('memory.view.pull',pp,po),ex('memory.view.ack',ap,ao),ex('memory.view.pull',pp2,po2),ex('memory.view.ack',ap2,ao2)])
# 44: Surface lifecycle, not-modified and independent presentation intent.
s=surface();cp={k:copy.deepcopy(s[k]) for k in ('surface_id','app_binding','snapshot')};s2=copy.deepcopy(s);s2['revision']=2;s2['snapshot']=snap('Done');s2['snapshot']['blocks'][0]['code']='done'
sp={'query_id':id('surface_query'),'filters':{'app_ids':[],'task_refs':[],'include_expired':False},'limit':20}
summary={k:copy.deepcopy(s2[k]) for k in ('surface_id','surface_owner_id','app_binding','revision')};summary.update(title=s2['snapshot']['title'],expires_at=END)
so={'query_id':id('surface_query'),'owner_id':id('surface_owner'),'items':[summary],'exhausted':True,'partial':False,'gaps':[],'unreachable_endpoints':[]}
pres={'endpoint_id':id('endpoint'),'open':False,'seen_revision':2};preso={'surface_id':id('surface'),'endpoint_id':id('endpoint'),'intent_revision':2,'open':False,'seen_revision':2}
fixtures['44-surface-lifecycle.json']=trace('surface-lifecycle','Full snapshots update independently of a device close intent.',[ex('interaction.surface_create',cp,s),ex('interaction.surface_read',{}, {'status':'snapshot','surface':s,'gaps':[]}),ex('interaction.surface_update',{'snapshot':s2['snapshot']},s2,expected=1),ex('interaction.surface_read',{'known_revision':2},{'status':'not_modified','surface_id':id('surface'),'revision':2,'gaps':[]}),ex('interaction.surface_list',sp,so),ex('interaction.present',pres,preso,expected=1)])
# 45: application events keep an immutable target-service/command mapping.
asurf=surface('app_surface');asurf['app_binding']=comp('memory_candidates');acp={k:copy.deepcopy(asurf[k]) for k in ('surface_id','app_binding','snapshot')}
aep={'event_id':id('event'),'surface_id':id('app_surface'),'surface_revision':1,'app_binding':comp('memory_candidates'),'event_type':'candidate.reject','payload_ref':cr('candidate_rejection'),'preview_refs':[]};aeo={**copy.deepcopy(aep),'target_service_id':id('memory_owner'),'target_command_id':id('candidate_command'),'state':'queued'}
fixtures['45-application-delivery.json']=trace('application-delivery','Fixed independent handler registers one target command; queue state is not domain consumption.',[ex('interaction.surface_create',acp,asurf),ex('interaction.application_event',aep,aeo)])
# 46: restriction narrows policy; incomplete holder cleanup remains residual.
rp=copy.deepcopy(closep);rp['mode']='restrict';rp['reason']='Narrow use scope';rp['policy']=copy.deepcopy(pol);rp['policy']['allowed_recipients']=[id('actor')];rp['policy']['allowed_purposes']=['review']
ro=copy.deepcopy(closeo);ro['state']='restricted';res_p=copy.deepcopy(releasep);res_p.update(physical_state='residual',residual_reason='An offline cache is not yet erased.');res_o=copy.deepcopy(releaseo);res_o.update(res_p)
clo=copy.deepcopy(cleanup_o);clo['physical_state']='residual';clo['holders'][0].update(physical_state='residual',residual_reason='Offline cache remains.')
fixtures['46-content-restriction.json']=trace('content-restriction','Restrict only narrows current policy, while residual bytes remain an explicit cleanup result.',[ex('content.put',putp,puto),ex('content.register_copy',regp,copy1),ex('content.close',rp,ro,expected=1),ex('content.release_copy',res_p,res_o),ex('memory.cleanup.get',cleanup_p,clo)])
# 47: input fields and referenced request are strict declarative data.
sf=surface('form_surface');rr=ore('request',id('home'),2)
sf['snapshot']['request_refs']=[rr];sf['snapshot']['blocks']=[{'kind':'input','block_id':'directory','request_ref':rr,'label':'Save directory','input_schema':{'fields':[{'name':'directory','label':'Directory','type':'text','required':True,'max_length':512}]}},{'kind':'action','block_id':'confirm','request_ref':rr,'action_id':'submit','label':'Confirm'},{'kind':'table','block_id':'summary','columns':['Name','Value'],'rows':[['Target','report.md']]}]
sfp={k:copy.deepcopy(sf[k]) for k in ('surface_id','app_binding','snapshot')}
fixtures['47-declarative-form.json']=trace('declarative-form','Typed form and action share an exact request reference; no executable UI payload exists.',[ex('interaction.surface_create',sfp,sf),ex('interaction.surface_read',{}, {'status':'snapshot','surface':sf,'gaps':[]},target=id('form_surface'))])
request_view={'request_id':id('request'),'owner_id':id('home'),'revision':2,'kind':'clarification','task_ref':{'home_id':id('home'),'task_id':id('task')},'schema':sf['snapshot']['blocks'][0]['input_schema'],'question_ref':cr('directory_question'),'deadline':F,'required_content_refs':[cr('directory_preview')],'allowed_actions':['submit'],'state':'open'}
fixtures['47-declarative-form.json']['events'].append(ex('interaction.request_read',{'request_ref':rr},{'request':request_view,'gaps':[]}))
# 48: a deleted record is skipped rather than exposing a frozen old revision.
ma=memory('first_memory');mb=memory('second_memory');q1=copy.deepcopy(q);q1['query_id']=id('paged_query');q1['limit']=1
p1=copy.deepcopy(page);p1.update(query_id=id('paged_query'),items=[ma],next_cursor='query:1',exhausted=False)
q2={**copy.deepcopy(q1),'cursor':'query:1'};p2=copy.deepcopy(page);p2.update(query_id=id('paged_query'),items=[],position=1,scanned_count=1,skipped_count=1,changed=True)
delp={'reason':'User removed the second item.'};delout=control(mb);delout.update(revision=2,state='deleted',cleanup_ref=ore('memory_cleanup',rev=2))
fixtures['48-memory-changed-pages.json']=trace('memory-changed-pages','Frozen position continues after concurrent delete; skipped identity is never returned.',[ex('memory.create',memwrite(ma),ma),ex('memory.create',memwrite(mb),mb),ex('memory.query',q1,p1),ex('memory.delete',delp,delout,target=mb['memory_id'],expected=1),ex('memory.query',q2,p2)])
# 49: source projection order and bounded preauthorized extraction reuse existing policies.
ts=surface('task_surface',snap('Working',3),True);tcp={k:copy.deepcopy(ts[k]) for k in ('surface_id','app_binding','task_ref','snapshot')};ts2=copy.deepcopy(ts);ts2['revision']=2;ts2['snapshot']=snap('Working with fresh facts',4)
ep2=copy.deepcopy(extractp);ep2['extraction_id']=id('auto_extraction');ep2['save_mode']='preauthorized';eo2=copy.deepcopy(extracto);eo2.update(extraction_id=id('auto_extraction'),extraction_task_id=id('auto_task'))
fixtures['49-source-projection.json']=trace('source-projection','Task projections advance source revision; finite preauthorized extraction still only queues a Home task.',[ex('interaction.surface_create',tcp,ts),ex('interaction.surface_update',{'snapshot':ts2['snapshot']},ts2,target=id('task_surface'),expected=1),ex('memory.extract',ep2,eo2)])
for name,value in fixtures.items():(BASE/name).write_text(json.dumps(value,ensure_ascii=False,indent=2)+'\n')
# One input and one output unknown-field mutation per newly frozen method.
cases=[];locations={}
for name,t in fixtures.items():
 for i,event in enumerate(t['events']):
  n=event['exchange']['request']['method']
  if n in patch['methods'] and n not in locations:locations[n]=(name,i)
for name,(filename,i) in locations.items():
 for part in ('request/payload','response/output'):
  cases.append({'name':name+' rejects unknown '+part.split('/')[0]+' field','fixture':filename,'edits':[{'op':'set','path':f'/events/{i}/exchange/{part}/unknown_field','value':True}],'expect':'schema'})
def mut(method,path,value,expect,filename=None,event=None):
 f,i=locations[method]
 if filename:f=filename
 if event is not None:i=event
 cases.append({'name':method+' '+expect+' '+str(len(cases)), 'fixture':f,'edits':[{'op':'set','path':f'/events/{i}/exchange/'+path,'value':value}],'expect':expect})
mut('content.put','response/output/content_ref/hash',dig('changed'),'content_binding')
mut('content.get','response/output/copy_id',id('wrong_copy'),'content_binding')
mut('content.register_copy','response/output/holder_id',id('wrong_holder'),'copy_binding')
mut('content.release_copy','response/output/use_stopped',False,'copy_cleanup')
mut('content.close','response/output/state','active','content_closure')
mut('memory.query','response/output/items/0/state','disabled','query_active')
mut('memory.list','response/output/items/0/owner_id',id('other_owner'),'query_binding')
mut('memory.extract','response/output/extraction_task_id',id('new_task'),'extraction_immutable')
# Above needs two same extraction records with distinct command IDs.
case=cases.pop();dup=copy.deepcopy(fixtures['42-memory-extraction.json']['events'][0]);dup['exchange']['request']['command_id']=id('repeat_extraction_command');dup['exchange']['response']['command_id']=id('repeat_extraction_command');dup['exchange']['response']['output']['extraction_task_id']=id('new_task');cases.append({'name':'memory.extract preserves original task mapping','fixture':'42-memory-extraction.json','edits':[{'op':'set','path':'/events/1','value':dup}],'expect':'extraction_immutable'})
mut('memory.view.open','response/output/recipient_id',id('wrong_recipient'),'view_binding')
mut('memory.view.pull','response/output/from_cursor','not-original','view_cursor')
mut('memory.view.ack','response/output/acked_cursor','not-original','view_ack')
mut('memory.cleanup.get','response/output/holders/0/physical_state','unknown','cleanup_complete')
mut('interaction.surface_create','response/output/app_binding/id',id('wrong_app'),'surface_binding')
mut('interaction.surface_read','response/output/surface/surface_id',id('wrong_surface'),'surface_binding')
mut('interaction.surface_update','response/output/revision',0,'schema')
mut('interaction.surface_list','response/output/items/0/surface_owner_id',id('wrong_owner'),'query_binding')
mut('interaction.present','response/output/intent_revision',0,'schema')
mut('interaction.application_event','response/output/target_service_id',id('new_service'),'application_immutable')
case=cases.pop();dup=copy.deepcopy(fixtures['45-application-delivery.json']['events'][1]);dup['exchange']['request']['command_id']=id('event_replayed_new_command');dup['exchange']['response']['command_id']=id('event_replayed_new_command');dup['exchange']['response']['output']['target_service_id']=id('new_service');cases.append({'name':'application event target cannot change','fixture':'45-application-delivery.json','edits':[{'op':'set','path':'/events','value':fixtures['45-application-delivery.json']['events']+[dup]}],'expect':'application_immutable'})
mut('interaction.request_read','response/output/request/owner_id',id('wrong_request_owner'),'request_owner')
# Additional useful runtime-association mutations (not just equal-field mirrors).
cases += [
 {'name':'download requires previously registered copy','fixture':'40-content-lifecycle.json','edits':[{'op':'remove','path':'/events/1'}],'expect':'copy_access'},
 {'name':'late download after source close is rejected','fixture':'40-content-lifecycle.json','edits':[{'op':'set','path':'/events','value':[evs[0],evs[1],evs[3],evs[2]]}],'expect':'content_closed'},
 {'name':'cannot widen policy in restriction','fixture':'46-content-restriction.json','edits':[{'op':'set','path':'/events/2/exchange/request/payload/policy/offline_allowed','value':True}],'expect':'content_policy_widening'},
 {'name':'ACK must name a delivered page','fixture':'43-memory-view-sync.json','edits':[{'op':'remove','path':'/events/1'}],'expect':'view_ack_page'},
 {'name':'metadata view cannot disclose memory content','fixture':'43-memory-view-sync.json','edits':[{'op':'set','path':'/events/1/exchange/response/output/items/0/record','value':m}],'expect':'view_projection'},
 {'name':'task source projection cannot move backward','fixture':'49-source-projection.json','edits':[{'op':'set','path':'/events/1/exchange/request/payload/snapshot/source_revision','value':2},{'op':'set','path':'/events/1/exchange/response/output/snapshot/source_revision','value':2}],'expect':'surface_source_order'},
 {'name':'surface update cannot replace the original handler','fixture':'44-surface-lifecycle.json','edits':[{'op':'set','path':'/events/2/exchange/response/output/app_binding/id','value':id('another_handler')}],'expect':'surface_immutable_binding'},
 {'name':'presentation cannot claim a future snapshot','fixture':'44-surface-lifecycle.json','edits':[{'op':'set','path':'/events/5/exchange/request/payload/seen_revision','value':9},{'op':'set','path':'/events/5/exchange/response/output/seen_revision','value':9}],'expect':'presentation_seen'},
 {'name':'not modified requires exact known revision','fixture':'44-surface-lifecycle.json','edits':[{'op':'set','path':'/events/3/exchange/request/payload/known_revision','value':1}],'expect':'surface_not_modified'},
 {'name':'input component must bind a listed request','fixture':'47-declarative-form.json','edits':[{'op':'set','path':'/events/0/exchange/request/payload/snapshot/request_refs','value':[]},{'op':'set','path':'/events/0/exchange/response/output/snapshot/request_refs','value':[]}],'expect':'surface_request'},
 {'name':'bounded field type needs the correct constraints','fixture':'47-declarative-form.json','edits':[{'op':'remove','path':'/events/0/exchange/request/payload/snapshot/blocks/0/input_schema/fields/0/max_length'},{'op':'remove','path':'/events/0/exchange/response/output/snapshot/blocks/0/input_schema/fields/0/max_length'}],'expect':'surface_fields'},
 {'name':'empty skipped page still consumes original positions','fixture':'48-memory-changed-pages.json','edits':[{'op':'set','path':'/events/4/exchange/response/output/position','value':0}],'expect':'query_continuity'},
]
Path('.scratch/architecture-detail/content-mutations.json').write_text(json.dumps(cases,ensure_ascii=False,indent=2)+'\n')
print(len(fixtures),'traces;',len(cases),'mutations;',len(locations),'methods')
