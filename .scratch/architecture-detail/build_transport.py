from pathlib import Path
import json, hashlib, copy

ROOT=Path('docs/architecture/contracts')
ID={'type':'string','pattern':'^[a-z][a-z0-9_]*_[0-9a-f]{32}$'}
DIG={'type':'string','pattern':'^sha256:[0-9a-f]{64}$'}
TIME={'type':'string','format':'date-time','pattern':'Z$'}
TEXT={'type':'string','minLength':1,'maxLength':4096}
INT={'type':'integer','minimum':0,'maximum':9007199254740991}
def ref(n):return {'$ref':'#/$defs/'+n}
def common(n):return {'$ref':'protocol.schema.json#/$defs/'+n}
def obj(p,required=None,**extra):return dict(type='object',properties=p,required=list(p) if required is None else required,additionalProperties=False,**extra)
def arr(s,n=100):return {'type':'array','items':s,'maxItems':n}
def enum(*s):return {'enum':list(s)}
defs={}
defs['Discovery']=obj({'logical_service_id':ID,'protocol':{'const':'harness/1'},'profile':{'const':'full-harness-draft-2'},'schema_digest':DIG,'methods_digest':DIG,'methods':arr(TEXT,256),'auth_profile':{'const':'opaque-bearer-1'},'proof_profile':{'const':'home-jws-es256-1'},'limits':obj({'max_json_bytes':dict(INT,minimum=1,maximum=262144),'max_page_items':dict(INT,minimum=1,maximum=100),'max_pull_items':dict(INT,minimum=1,maximum=100),'max_wait_ms':dict(INT,maximum=25000),'max_content_bytes':dict(INT,minimum=1)}),'retention':obj({'receipt_query_seconds':INT,'minimal_closed_identity':{'const':'permanent'}}),'changes_supported':{'type':'boolean'}})
defs['DiscoveryPublic']=obj({'protocol':{'const':'harness/1'},'auth_profile':{'const':'opaque-bearer-1'},'pairing_supported':{'type':'boolean'}})
defs['PullInput']=obj({'max_items':dict(INT,minimum=1,maximum=100),'wait_ms':dict(INT,maximum=25000)})
defs['Lookup']=obj({'command_id':ID})
defs['QueryError']=obj({'error':common('Error')})
delivery={'delivery_id':ID,'sender_service_id':ID,'recipient_endpoint_id':ID,'recipient_instance_id':ID,'request_digest':DIG,'kind':enum('command','query','receipt_lookup'),'request':{},'deliver_before':TIME}
defs['Delivery']={'oneOf':[obj(dict(delivery,kind={'const':kind},request=spec)) for kind,spec in [('command',common('Command')),('query',common('Query')),('receipt_lookup',ref('Lookup'))]]}
proof={'type':'string','pattern':'^[A-Za-z0-9_-]+\\.[A-Za-z0-9_-]+\\.[A-Za-z0-9_-]+$','maxLength':65536}
defs['DeliveryEnvelope']=obj({'delivery':ref('Delivery'),'delivery_proof':proof})
defs['PullOutput']=obj({'deliveries':arr(ref('DeliveryEnvelope')),'more':{'type':'boolean'}})
reply={'delivery_id':ID,'request_digest':DIG,'kind':{},'result':{}}
defs['Reply']={'oneOf':[obj(dict(reply,kind={'const':k},result=s)) for k,s in [('command',common('Receipt')),('query',{'oneOf':[common('QueryResult'),ref('QueryError')]}),('receipt_lookup',{'oneOf':[common('Receipt'),ref('QueryError')]})]]}
defs['ReplyAck']=obj({'delivery_id':ID,'stored':{'const':True},'result_digest':DIG})
defs['ControlProofPayload']=obj({'issuer':ID,'tenant_id':ID,'audience':ID,'gate':common('TaskGate'),'issued_at':TIME,'start_before':TIME})
defs['UploadIntent']=obj({'upload_id':ID,'hash':DIG,'byte_length':dict(INT,minimum=1),'media_type':TEXT,'expires_at':TIME})
defs['Upload']=obj({'upload_id':ID,'hash':DIG,'byte_length':dict(INT,minimum=1),'media_type':TEXT,'expires_at':TIME,'state':enum('reserved','ready','committed','expired'),'content_ref':common('ContentRef')},['upload_id','hash','byte_length','media_type','expires_at','state'],allOf=[{'if':{'properties':{'state':{'const':'committed'}}},'then':{'required':['content_ref']},'else':{'not':{'required':['content_ref']}}}])
defs['Change']=obj({'cursor':TEXT,'object_type':enum('task','operation','memory','surface','activation','grant'),'object_id':ID,'revision':dict(INT,minimum=1)})
defs['ClosedIdentity']=obj({'tenant_id':ID,'owner_id':ID,'object_id':ID,'kind':enum('command','operation','task'),'closed_at':TIME,'request_digest':DIG,'outcome':enum('applied','rejected','cancelled','succeeded','failed'),'payload_retained':{'type':'boolean'}},['tenant_id','owner_id','object_id','kind','closed_at','outcome','payload_retained'])
defs['ClosedIdentityLookup']=obj({'record':ref('ClosedIdentity'),'lookup_id':ID,'request_digest':DIG,'response_code':enum('gone','idempotency_conflict','precondition_failed')},['record','lookup_id','response_code'])
schema={'$schema':'https://json-schema.org/draft/2020-12/schema','$id':'https://harness.invalid/schema/transport-draft-2','$defs':defs}
(ROOT/'schemas/transport.schema.json').write_text(json.dumps(schema,ensure_ascii=False,indent=2)+'\n')
def ident(kind):return kind+'_'+hashlib.sha256(kind.encode()).hexdigest()[:32]
def digest(v):return 'sha256:'+hashlib.sha256(json.dumps(v,sort_keys=True,ensure_ascii=False,separators=(',',':')).encode()).hexdigest()
T='2026-09-26T00:00:00Z';E='2026-09-26T00:10:00Z'
baseline=json.loads((ROOT/'examples/protocol/01-task-loop.json').read_text())
ex=next(e['exchange'] for e in baseline['events'] if 'exchange' in e)
vectors=[]
def vector(name,definition,value,context=None):
 d={'name':name,'definition':definition,'value':value}
 if context is not None:d['context']=context
 vectors.append(d);return d
vector('public-discovery','DiscoveryPublic',{'protocol':'harness/1','auth_profile':'opaque-bearer-1','pairing_supported':True})
vector('discovery','Discovery',{'logical_service_id':ident('service'),'protocol':'harness/1','profile':'full-harness-draft-2','schema_digest':'sha256:'+'a'*64,'methods_digest':'sha256:'+'b'*64,'methods':['task.submit','task.read'],'auth_profile':'opaque-bearer-1','proof_profile':'home-jws-es256-1','limits':{'max_json_bytes':262144,'max_page_items':100,'max_pull_items':32,'max_wait_ms':25000,'max_content_bytes':16777216},'retention':{'receipt_query_seconds':2592000,'minimal_closed_identity':'permanent'},'changes_supported':False})
vector('bounded-pull','PullInput',{'max_items':32,'wait_ms':25000})
for kind,request,result in [('command',ex['request'],ex['response']),('query',{'method':'task.read','target_id':ex['response']['output']['task_id'],'payload':{}},{'output':ex['response']['output'],'observed_at':T}),('receipt_lookup',{'command_id':ex['request']['command_id']},ex['response'])]:
 d=dict(delivery_id=ident('delivery_'+kind),sender_service_id=ex['auth']['logical_service_id'],recipient_endpoint_id=ident('endpoint'),recipient_instance_id=ident('instance'),request_digest=digest(request),kind=kind,request=copy.deepcopy(request),deliver_before=E)
 vector(kind+'-delivery','Delivery',d)
 r={'delivery_id':d['delivery_id'],'request_digest':d['request_digest'],'kind':kind,'result':copy.deepcopy(result)}
 vector(kind+'-reply','Reply',r,{'delivery':d})
 vector(kind+'-ack','ReplyAck',{'delivery_id':d['delivery_id'],'stored':True,'result_digest':digest(result)},{'reply':r})
lookup=copy.deepcopy(vectors[-2]['value']);lookup['result']={'error':{'code':'gone','message':'Original payload removed; identity remains closed','retry':'none'}}
vector('purged-receipt-reply','Reply',lookup,{'delivery':vectors[-3]['value']})
content=copy.deepcopy(ex['request']['payload']['goal_ref'])
intent={'upload_id':ident('upload'),'hash':content['hash'],'byte_length':content['byte_length'],'media_type':content['media_type'],'expires_at':E}
vector('upload-intent','UploadIntent',intent)
vector('ready-upload','Upload',dict(intent,state='ready'))
vector('committed-upload','Upload',dict(intent,state='committed',content_ref=content))
vector('change-hint','Change',{'cursor':'page:9','object_type':'task','object_id':ex['response']['output']['task_id'],'revision':2})
for kind,outcome in [('command','applied'),('operation','cancelled'),('task','cancelled')]:
 record={'tenant_id':ex['auth']['tenant_id'],'owner_id':ident('owner'),'object_id':ident(kind),'kind':kind,'closed_at':T,'outcome':outcome,'payload_retained':False}
 if kind=='command':record['request_digest']='sha256:'+'c'*64
 value={'record':record,'lookup_id':record['object_id'],'response_code':'gone' if kind=='command' else 'precondition_failed'}
 if kind=='command':value['request_digest']=record['request_digest']
 vector('closed-'+kind,'ClosedIdentityLookup',value)
 conflict=copy.deepcopy(value)
 if kind=='command':
  conflict['request_digest']='sha256:'+'d'*64;conflict['response_code']='idempotency_conflict'
  vector('closed-command-conflict','ClosedIdentityLookup',conflict)
cases=[]
def negative(name,source,path,value,expect='transport_schema'):
 cases.append({'name':name,'vector':source,'edits':[{'op':'set','path':path,'value':value}],'expect':expect})
for v in vectors:negative(v['name']+'-unknown-field',v['name'],'/value/extra',True)
negative('unknown-discovered-method','discovery','/value/methods/0','task.launch_anything','discovery_method')
negative('changed-query-kind','query-delivery','/value/kind','command')
negative('query-with-command-id','query-delivery','/value/request/command_id',ident('command'))
negative('wrong-command-reply','command-reply','/value/result/command_id',ident('other'),'delivery_command')
negative('wrong-query-digest','query-reply','/value/request_digest','sha256:'+'0'*64,'delivery_binding')
negative('wrong-ack','command-ack','/value/result_digest','sha256:'+'0'*64,'reply_digest')
negative('upload-length-change','committed-upload','/value/content_ref/byte_length',999,'content_upload')
negative('upload-hash-change','committed-upload','/value/content_ref/hash','sha256:'+'0'*64,'content_upload')
negative('closed-command-loses-identity','closed-command','/value/response_code','precondition_failed','closed_identity')
negative('closed-command-hash-conflict','closed-command-conflict','/value/response_code','gone','closed_identity')
negative('closed-operation-not-found','closed-operation','/value/response_code','not_found')
negative('wrong-delivery-digest','query-delivery','/value/request_digest','sha256:'+'0'*64,'request_digest')
out=ROOT/'examples/transport';out.mkdir(exist_ok=True)
(out/'vectors.json').write_text(json.dumps(vectors,ensure_ascii=False,indent=2)+'\n')
(out/'invalid-mutations.json').write_text(json.dumps(cases,ensure_ascii=False,indent=2)+'\n')
print(f'{len(defs)} transport definitions, {len(vectors)} vectors, {len(cases)} mutations')
