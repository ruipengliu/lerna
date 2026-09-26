from pathlib import Path
import heapq
import itertools
import json
import xml.etree.ElementTree as ET

OUT = Path('docs/architecture/diagrams/system-panorama.drawio')
P = dict(paper='#f5f5f5', ink='#2d3142', muted='#4f5d75', rule='#bfc0c0', accent='#eb6c36', tint='#f8ece5', link='#2e5aa8')
FONT='Geist, PingFang SC, Microsoft YaHei'
W,H=3400,2864
mxfile=ET.Element('mxfile',host='Electron',version='28.0.6',type='device')
diagram=ET.SubElement(mxfile,'diagram',id='harness-technical-panorama',name='Harness 技术全景')
model=ET.SubElement(diagram,'mxGraphModel',dx=str(W),dy=str(H),grid='1',gridSize='8',guides='1',tooltips='1',connect='1',arrows='1',fold='1',page='1',pageScale='1',pageWidth=str(W),pageHeight=str(H),background=P['paper'],math='0',shadow='0')
root=ET.SubElement(model,'root')
ET.SubElement(root,'mxCell',id='0');ET.SubElement(root,'mxCell',id='1',parent='0')
rects={}; meta={'nodes':[],'edges':[],'text':[],'size':[W,H],'style':'default'}

def vertex(cid,box,value,style,parent='1',kind='annotation',**attrs):
 x,y,w,h=box
 c=ET.SubElement(root,'mxCell',dict(id=cid,value=value,style=style,vertex='1',parent=parent,semanticKind=kind,**attrs))
 ET.SubElement(c,'mxGeometry',x=str(x),y=str(y),width=str(w),height=str(h),attrib={'as':'geometry'})
 px,py=rects.get(parent,(0,0,0,0))[:2];rects[cid]=(px+x,py+y,w,h)
 meta['nodes'].append(dict(id=cid,kind=kind,parent=parent,rect=rects[cid],value=value))
 return c

def text(cid,box,value,size=16,bold=False,color=None,parent='1',align='left',font=FONT):
 style=f'text;html=0;whiteSpace=wrap;overflow=hidden;strokeColor=none;fillColor=none;align={align};verticalAlign=middle;spacing=0;fontFamily={font};fontSize={size};fontColor={color or P["ink"]};fontStyle={1 if bold else 0};'
 vertex(cid,box,value,style,parent,'text');meta['text'].append(dict(id=cid,value=value,rect=rects[cid],size=size))

def container(cid,box,title,kind='module',accent=False,parent='1',source=''):
 fill=P['tint'] if accent else ('#f0f0f1' if kind=='zone' else '#ffffff')
 stroke=P['accent'] if accent else P['rule'] if kind=='zone' else P['ink']
 style=f'rounded=1;arcSize=2;fillColor={fill};strokeColor={stroke};strokeWidth={2 if accent else 1};container=1;collapsible=0;recursiveResize=0;'
 if kind=='zone':style+='dashed=1;dashPattern=5 4;'
 attrs={'sourceDoc':source} if source else {}
 vertex(cid,box,'',style,parent,kind,**attrs)
 text(cid+'-title',(24,16,box[2]-48,32),title,22 if kind=='module' else 18,True,parent=cid)

container('core',(64,168,2584,1584),'任务处理职责',kind='zone')
container('external',(2752,168,528,1584),'外部依赖',kind='zone')
container('governance',(64,1880,3216,624),'治理与宿主职责',kind='zone')
text('eyebrow',(64,32,1800,24),'HARNESS  /  TECHNICAL PANORAMA',12,color=P['muted'])
text('title',(64,72,2200,48),'以事实裁决组织目标推进',36,font='Instrument Serif, Songti SC, serif')
text('subtitle',(64,128,2200,24),'九个逻辑模块  ·  主要组件依赖  ·  权威对象与跨模块交接',18,color=P['muted'])

# Module positions are independent from deployment or transaction boundaries.
mods={
 'interaction':((104,248,600,568),'应用与交互','interaction'),
 'home':((872,248,840,568),'任务运行时 · Task Home','task-runtime'),
 'brain':((1944,248,600,568),'大脑','brain'),
 'memory':((104,1080,680,568),'记忆与内容','memory'),
 'collaboration':((936,1080,776,568),'Agent 协作','collaboration'),
 'execution':((1944,1080,600,568),'执行','execution'),
 'security':((104,1952,880,520),'权限与隔离','security'),
 'evaluation':((1152,1952,896,520),'观测、评测与改进','evaluation'),
 'extensions':((2240,1952,1000,520),'扩展与宿主','extensions')}
for mid,(b,title,src) in mods.items():
 parent='core' if mid in ['interaction','home','brain','memory','collaboration','execution'] else 'governance'
 px,py=rects[parent][:2];x,y,w,h=b
 container(mid,(x-px,y-py,w,h),title,parent=parent,accent=mid in ['home','execution'],source=src+'/implementation.md')

# Each primitive is editable: component rectangles and repository cylinders.
# A displayed box may group closely related components, with original names retained.
node_specs={
'interaction':[
 ('renderer',(32,96,232,72),'Renderer'),('input',(336,96,232,72),'InputService'),
 ('surface',(32,240,232,72),'SurfaceService'),('delivery',(336,416,232,80),'DeliveryWorker'),
 ('store',(32,416,232,80),'InteractionStore\nSurface · InputSubmission')],
'home':[
 ('command',(32,96,216,72),'CommandHandler'),('snapshot',(312,96,216,72),'SnapshotAssembler\nPlanMaterializer'),('runner',(592,96,216,72),'JobRunner'),
 ('coordinator',(32,240,216,80),'TaskCoordinator\nBudgetLedger'),('reducer',(592,240,216,80),'FactReducer'),
 ('store',(32,424,496,80),'Runtime Store\nTask · Plan · Intent · Result · jobs')],
'brain':[
 ('decision',(32,96,232,72),'DecisionService'),('rules',(336,96,232,72),'DecisionPolicy\nProposalValidator'),
 ('context',(32,240,232,72),'ContextReader'),('recovery',(336,240,232,72),'RecoveryWorker'),
 ('store',(32,416,232,88),'DecisionStore\nDecision · ModelCall'),('adapter',(336,416,232,88),'ModelAdapter')],
'memory':[
 ('writer',(32,96,256,72),'MemoryWriter'),('query',(392,96,256,72),'QueryService'),
 ('content',(32,240,256,72),'ContentStore'),('index',(392,240,256,72),'候选索引\n派生数据'),
 ('metadata',(32,416,256,88),'MetadataStore\nMemoryRevision · ContentRef'),('bytes',(392,416,256,88),'ByteStore\n准确版本字节')],
'collaboration':[
 ('admission',(32,96,304,72),'DelegationAdmission'),('child',(440,96,304,72),'InternalChildFactory'),
 ('reducer',(32,240,304,72),'DelegationReducer'),('adapter',(440,240,304,72),'ExternalAgentAdapter'),
 ('store',(32,416,304,88),'Collaboration Store\nDelegation · 子映射 · jobs'),('control',(440,416,304,88),'ControlPropagator\nSettlementCoordinator')],
'execution':[
 ('admission',(32,96,232,72),'接纳与控制用例'),('gate',(336,96,232,72),'GateStore\nStartBarrier'),
 ('worker',(32,240,232,72),'执行工作者'),('driver',(336,240,232,72),'固定版本 Driver'),
 ('store',(32,416,232,88),'Executor Store\nOperation · Attempt · jobs'),('resource',(336,416,232,88),'资源 owner Store\nGate · epoch · lease')],
'security':[
 ('facade',(32,96,240,72),'权限入口 facade'),('grant',(320,96,240,72),'GrantLedger'),('lease',(608,96,240,72),'LeaseLedger'),
 ('identity',(32,240,240,72),'IdentityAdapter'),('store',(320,392,240,88),'领域 repositories\nGrant · UseReceipt · Lease'),('revocation',(608,392,240,88),'RevocationWorker')],
'evaluation':[
 ('plan',(32,96,248,72),'PlanService'),('exposure',(320,96,248,72),'ExposureLedger'),('approval',(616,96,248,72),'ApprovalOwner'),
 ('run',(32,240,248,72),'RunCoordinator'),('report',(32,392,248,88),'ReportSealer'),
 ('store',(320,392,248,88),'领域 repositories\nPlan · Report · Approval'),('rollout',(616,392,248,88),'RolloutWorker')],
'extensions':[
 ('manager',(32,96,272,72),'LifecycleManager'),('verifier',(368,96,272,72),'PackageVerifier'),('approval',(696,96,272,72),'ApprovalClient'),
 ('lock',(32,240,272,72),'LockStore'),('binding',(368,240,272,72),'BindingRouter'),
 ('store',(32,392,608,88),'宿主 repositories\nInstallLock · Activation · Binding · InstanceReadiness')]
}
stores={'store','metadata','bytes','resource','index'}
for mid,ns in node_specs.items():
 for nid,box,label in ns:
  isstore=nid in stores
  sty=('shape=cylinder3;boundedLbl=1;backgroundOutline=1;size=12;' if isstore else 'rounded=1;arcSize=8;')
  sty+=f'html=0;whiteSpace=wrap;fillColor={"#eef0f3" if isstore else "#ffffff"};strokeColor={P["muted"]};strokeWidth=1;fontFamily={FONT};fontSize=17;fontColor={P["ink"]};align=center;verticalAlign=middle;spacing=8;'
  vertex(mid+'-'+nid,box,label,sty,mid,'repository' if isstore else 'component')

for cid,b,title in [
 ('model-provider',(2800,344,432,200),'本地模型 / 云模型\n输出 · 请求凭据 · 用量'),
 ('target-system',(2800,1224,432,192),'API / 文件 / 模拟设备\n目标凭据 · 效果证据'),
 ('other-agent',(2800,1464,432,144),'其他 Agent / 另一 Home\n原任务 · 结果 · 封账')]:
 px,py=rects['external'][:2];x,y,w,h=b
 vertex(cid,(x-px,y-py,w,h),title,f'rounded=1;arcSize=4;whiteSpace=wrap;html=0;fillColor=#eaecef;strokeColor=#7a8399;fontFamily={FONT};fontSize=20;spacing=16;',parent='external',kind='external')

# Edges precede component cells within their parent in the serialized model.
def edge(eid,src,dst,path,parent='1',dashed=False,blue=False,flow='',label='',label_box=None):
 color=P['link'] if blue else P['muted'];sx,sy,sw,sh=rects[src];tx,ty,tw,th=rects[dst];a,b=path[0],path[-1]
 sty=f'edgeStyle=orthogonalEdgeStyle;rounded=1;arcSize=8;orthogonalLoop=1;jettySize=0;html=0;strokeColor={color};strokeWidth={2 if flow else 1.25};endArrow=block;endFill=1;jumpStyle=arc;jumpSize=8;'
 sty+=f'exitX={(a[0]-sx)/sw};exitY={(a[1]-sy)/sh};exitDx=0;exitDy=0;exitPerimeter=0;entryX={(b[0]-tx)/tw};entryY={(b[1]-ty)/th};entryDx=0;entryDy=0;entryPerimeter=0;'
 if dashed:sty+='dashed=1;dashPattern=5 4;'
 attrs=dict(id=eid,value='',style=sty,edge='1',parent=parent,source=src,target=dst,semanticKind='relationship' if flow else 'dependency')
 if flow:attrs['flowId']=flow
 c=ET.SubElement(root,'mxCell',attrs);g=ET.SubElement(c,'mxGeometry',relative='1',attrib={'as':'geometry'})
 px,py=rects.get(parent,(0,0,0,0))[:2]
 if len(path)>2:
  arr=ET.SubElement(g,'Array',attrib={'as':'points'})
  for x,y in path[1:-1]:ET.SubElement(arr,'mxPoint',x=str(x-px),y=str(y-py))
 meta['edges'].append(dict(id=eid,source=src,target=dst,path=path,kind=attrs['semanticKind'],group=flow,dashed=dashed,label_box=label_box,label=label))
 if label_box:
  label_size=14 if not flow else 16
  ls=f'html=0;whiteSpace=wrap;fillColor={P["paper"]};strokeColor=none;fontFamily={FONT};fontSize={label_size};fontColor={color};spacing=0;'
  vertex(eid+'-label',label_box,label,ls,kind='edge-label')

# Route internal dependencies on an orthogonal grid, reserving distinct attachment
# points and forbidding shared path segments or transit through another component.
DEPS={
 'interaction':[('renderer','surface'),('renderer','input'),('surface','store'),('input','store'),('store','delivery','job')],
 'home':[('command','coordinator'),('runner','snapshot'),('snapshot','coordinator'),('runner','reducer'),('reducer','coordinator'),('coordinator','store'),('store','runner','job')],
 'brain':[('decision','context'),('decision','store'),('recovery','context'),('recovery','rules'),('recovery','adapter'),('recovery','store')],
 'memory':[('writer','content'),('writer','metadata'),('query','metadata'),('query','index'),('content','metadata'),('content','bytes')],
 'collaboration':[('admission','child'),('admission','store'),('child','store'),('store','adapter','job'),('adapter','reducer'),('reducer','store'),('store','control','job')],
 'execution':[('admission','store'),('admission','gate'),('store','worker','job'),('worker','gate'),('gate','driver'),('gate','resource'),('worker','driver')],
 'security':[('facade','identity'),('facade','grant'),('facade','lease'),('grant','store'),('lease','store'),('store','revocation','job')],
 'evaluation':[('plan','store'),('run','store'),('run','report'),('report','store'),('exposure','store'),('approval','store'),('store','rollout','job')],
 'extensions':[('manager','verifier'),('manager','approval'),('manager','lock'),('manager','binding'),('manager','store'),('lock','store'),('binding','store')]
}

def route_module(mid):
 mx,my,mw,mh=rects[mid]; obs={n:box for n,box,_ in node_specs[mid]}; occupied=set(); occupied_vertices=set(); usedports={};seq=itertools.count()
 def segkey(a,b):return tuple(sorted((a,b)))
 def forbidden(p):
  x,y=p
  return x<16 or x>mw-16 or y<72 or y>mh-16 or any(a-8<x<a+w+8 and b-8<y<b+h+8 for a,b,w,h in obs.values())
 def ports(n):
  x,y,w,h=obs[n];cx=round((x+w/2)/8)*8;cy=round((y+h/2)/8)*8
  ans=[]
  for delta in [0,-24,24,-48,48]:
   for p,d in [((cx+delta,y),(0,-1)),((cx+delta,y+h),(0,1)),((x,cy+delta),(-1,0)),((x+w,cy+delta),(1,0))]:
    if not(x+16<=p[0]<=x+w-16) and d[0]==0:continue
    if not(y+16<=p[1]<=y+h-16) and d[1]==0:continue
    if any(abs(p[0]-q[0])+abs(p[1]-q[1])<16 for q in usedports.get(n,[])):continue
    q=(p[0]+d[0]*8,p[1]+d[1]*8)
    if not forbidden(q):ans.append((p,q,d))
  return ans
 for ei,dep in enumerate(DEPS[mid]):
  src,dst=dep[:2];starts=ports(src);ends=ports(dst);targets={q:(p,d) for p,q,d in ends}
  todo=[];best={};prev={};initial={}
  for p,q,d in starts:
   if segkey(p,q) in occupied:continue
   state=(q,d);best[state]=0;initial[state]=p
   heapq.heappush(todo,(0,next(seq),state))
  found=None
  while todo:
   cost,_,state=heapq.heappop(todo);p,d=state
   if cost!=best.get(state):continue
   if p in targets:
    ep,ed=targets[p]
    if segkey(p,ep) not in occupied and d!=ed:
     found=(state,ep);break
   for nd in [(1,0),(0,1),(-1,0),(0,-1)]:
    if nd==(-d[0],-d[1]):continue
    q=(p[0]+nd[0]*8,p[1]+nd[1]*8)
    if forbidden(q) or segkey(p,q) in occupied:continue
    nc=cost+8+(32 if nd!=d else 0)+(192 if q in occupied_vertices else 0)
    ns=(q,nd)
    if nc<best.get(ns,1e20):
     best[ns]=nc;prev[ns]=state;heapq.heappush(todo,(nc,next(seq),ns))
  assert found,(mid,dep)
  state,ep=found;chain=[ep]
  while state in prev:chain.append(state[0]);state=prev[state]
  chain.extend([state[0],initial[state]]);chain.reverse()
  usedports.setdefault(src,[]).append(chain[0]);usedports.setdefault(dst,[]).append(chain[-1])
  for a,b in zip(chain,chain[1:]):
   occupied.add(segkey(a,b));occupied_vertices.update([a,b])
  # Compress only collinear steps; all actual bend coordinates remain explicit.
  compact=[chain[0]]
  for a,b,c in zip(chain,chain[1:],chain[2:]):
   if (b[0]-a[0])*(c[1]-b[1])!=(b[1]-a[1])*(c[0]-b[0]):compact.append(b)
  compact.append(chain[-1])
  path=[(x+mx,y+my) for x,y in compact]
  extra = {}
  if mid == 'execution' and src == 'worker' and dst == 'driver': extra = dict(label='查 / 停', label_box=(2216,1384,56,24))
  if mid == 'execution' and src == 'gate' and dst == 'driver': extra = dict(label='发送', label_box=(2400,1272,56,24))
  edge(mid+'-dep-'+str(ei+1),mid+'-'+src,mid+'-'+dst,path,parent=mid,dashed=len(dep)>2,**extra)
for mid in mods:route_module(mid)

edge('F01','interaction','home',[(704,488),(872,488)],flow='F01',label='01 目标 / 输入',label_box=(712,448,152,32))
edge('F02','home','brain',[(1712,488),(1944,488)],flow='F02',label='02 单轮决策',label_box=(1752,448,152,32))
edge('F03','brain','model-provider',[(2544,440),(2800,440)],flow='F03',blue=True,label='03 模型调用',label_box=(2596,400,152,32))
edge('F04','home','memory',[(944,816),(944,912),(440,912),(440,1080)],flow='F04',label='04 检索 / 内容',label_box=(584,872,192,32))
edge('F05','home','execution',[(1632,816),(1632,944),(2248,944),(2248,1080)],flow='F05',label='05 意图 / 控制',label_box=(1832,904,208,32))
edge('F07','home','collaboration',[(1328,816),(1328,1080)],flow='F07',label='07 有界委派',label_box=(1336,976,176,32))
edge('F06','execution','target-system',[(2544,1320),(2800,1320)],flow='F06',blue=True,label='06 原目标动作',label_box=(2584,1280,176,32))
edge('F08','collaboration','other-agent',[(1640,1648),(1640,1704),(2688,1704),(2688,1536),(2800,1536)],flow='F08',blue=True,label='08 原任务交接',label_box=(2064,1664,200,32))
edge('F09','core','security',[(544,1752),(544,1952)],flow='F09',label='09 使用 / 租约 / 结算',label_box=(552,1776,264,32))
edge('F10','core','evaluation',[(1568,1752),(1568,1952)],flow='F10',dashed=True,label='10 获准观测',label_box=(1576,1776,200,32))
edge('F11','evaluation','extensions',[(2048,2136),(2240,2136)],flow='F11',dashed=True,label='11 批准 / 发布',label_box=(2056,2096,176,32))
edge('F11-return','extensions','evaluation',[(2240,2320),(2048,2320)],flow='F11',dashed=True,label='核验 / 实例事实',label_box=(2056,2280,176,32))
edge('F12','extensions','core',[(3240,2392),(3336,2392),(3336,128),(2608,128),(2608,168)],flow='F12',dashed=True,label='12 装配 / 隔离 / 生命周期',label_box=(2856,88,360,32))

# Small graphical reading guide, not prose cards.
text('method-title',(64,2552,256,32),'建模顺序',18,True)
steps=['目标与条件','需裁决的事实','固定 owner','身份与版本','独立状态','提交与恢复']
for i,s in enumerate(steps):
 x=344+i*472
 vertex('method-'+str(i),(x,2548,352,48),s,f'rounded=1;arcSize=8;fillColor=#ffffff;strokeColor={P["rule"]};fontFamily={FONT};fontSize=18;',kind='method-step')
 if i:edge('method-edge-'+str(i),'method-'+str(i-1),'method-'+str(i),[(x-120,2572),(x,2572)])
text('deployment-title',(64,2632,256,32),'部署映射',18,True)
text('deployment',(344,2632,2840,32),'本地：模块化共库   /   远端：原命令与查询   /   生产：稳定 Home 分区 + 多可用区',18,color=P['muted'])
text('legend-title',(64,2720,256,32),'图例与范围',18,True)
text('legend1',(344,2704,2936,32),'模块内实线＝调用 / 读写；模块内虚线＝持久 job。F01–F12＝跨模块交接组；蓝线＝外部调用；外层虚线＝观测 / 治理 / 装配。',16,color=P['muted'])
text('legend2',(344,2744,2936,32),'09 / 10 / 12 的边界连线代表各实际参与者；主要组件依赖有选择地展开。模块 ≠ owner ≠ 进程 ≠ 提交域。',16,color=P['muted'])
text('legend3',(344,2784,2936,32),'完整字段、状态机、确认消费、其他内容调用与生产恢复条件 → technical-overview.md 及各模块详设',16,color=P['muted'])

# Containers first, their edges next, then content. Native grouping remains intact.
allcells=list(root)
def order(cell):
 kind=cell.get('semanticKind','');cid=cell.get('id','')
 if cid in ['0','1']:return 0
 if kind=='zone':return 1
 if kind=='module':return 2
 if cell.get('edge')=='1':return 3
 return 4
for c in allcells:root.remove(c)
for c in sorted(allcells,key=order):root.append(c)
ET.indent(mxfile,space='  ')
OUT.parent.mkdir(parents=True,exist_ok=True);OUT.write_bytes(ET.tostring(mxfile,encoding='utf-8',xml_declaration=True))
Path('.scratch/architecture-overview/panorama-layout.json').write_text(json.dumps(meta,ensure_ascii=False,indent=2)+'\n')
print(json.dumps({'modules':len(mods),'components_and_repositories':sum(map(len,node_specs.values())),'dependencies':sum(map(len,DEPS.values())),'external_groups':12,'edges':len(meta['edges']),'canvas':[W,H]},ensure_ascii=False))
