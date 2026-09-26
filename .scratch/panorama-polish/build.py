from pathlib import Path
import ast, heapq, itertools, json, html, re
import xml.etree.ElementTree as ET

BASE=Path('.scratch/panorama-polish')
old=ast.parse((BASE/'build_before.py').read_text())
olddata={}
for s in old.body:
 if isinstance(s,ast.Assign) and any(isinstance(t,ast.Name) and t.id in ['node_specs','DEPS','mods'] for t in s.targets):
  olddata[s.targets[0].id]=ast.literal_eval(s.value)
DEPS=olddata['DEPS']; FONT='Geist, PingFang SC, Microsoft YaHei'
P=dict(paper='#f5f5f5',ink='#2d3142',muted='#4f5d75',rule='#bfc0c0',accent='#eb6c36',link='#2e5aa8')
W,H=3440,2632
mxfile=ET.Element('mxfile',host='Electron',version='28.0.6',type='device')
diagram=ET.SubElement(mxfile,'diagram',id='harness-technical-panorama',name='Harness 技术全景')
model=ET.SubElement(diagram,'mxGraphModel',dx=str(W),dy=str(H),grid='1',gridSize='8',guides='1',tooltips='1',connect='1',arrows='1',fold='1',page='1',pageScale='1',pageWidth=str(W),pageHeight=str(H),background=P['paper'],math='0',shadow='0')
root=ET.SubElement(model,'root');ET.SubElement(root,'mxCell',id='0');ET.SubElement(root,'mxCell',id='1',parent='0')
rects={};meta={'nodes':[],'edges':[],'text':[],'size':[W,H],'style':'default-light-refined'}

def vertex(cid,box,value,style,parent='1',kind='annotation',**attrs):
 x,y,w,h=box
 c=ET.SubElement(root,'mxCell',dict(id=cid,value=value,style=style,vertex='1',parent=parent,semanticKind=kind,**attrs))
 ET.SubElement(c,'mxGeometry',x=str(x),y=str(y),width=str(w),height=str(h),attrib={'as':'geometry'})
 px,py=rects.get(parent,(0,0,0,0))[:2];rects[cid]=(px+x,py+y,w,h)
 meta['nodes'].append(dict(id=cid,kind=kind,parent=parent,rect=rects[cid],value=attrs.get('labelPlain',value)))
 return c

def text(cid,box,value,size=20,bold=False,color=None,parent='1',align='left',font=FONT):
 style=f'text;html=0;whiteSpace=wrap;overflow=hidden;strokeColor=none;fillColor=none;align={align};verticalAlign=middle;spacing=0;fontFamily={font};fontSize={size};fontColor={color or P["ink"]};fontStyle={1 if bold else 0};'
 vertex(cid,box,value,style,parent,'text');meta['text'].append(dict(id=cid,value=value,rect=rects[cid],size=size))

def container(cid,box,title,kind='module',accent=False,parent='1',source='',tag=''):
 iszone=kind=='zone';w=box[2]
 attrs={'sourceDoc':source} if source else {}
 stroke=P['accent'] if accent else '#aeb7c3' if not iszone else '#d5d9df'
 sty=f'rounded=1;absoluteArcSize=1;arcSize=8;fillColor={"none" if iszone else "#ffffff"};strokeColor={stroke};strokeWidth={1.6 if accent else 1};container=1;collapsible=0;recursiveResize=0;'
 if iszone:sty+='dashed=1;dashPattern=3 5;'
 vertex(cid,box,'',sty,parent,kind,**attrs)
 if iszone:
  text(cid+'-title',(24,20,w-48,32),title,22,True,color=P['muted'],parent=cid)
 else:
  vertex(cid+'-header',(1,1,w-2,64),'',f'rounded=1;absoluteArcSize=1;arcSize=8;fillColor={"#fcf0e9" if accent else "#f1f3f5"};strokeColor=none;',parent=cid,kind='header')
  text(cid+'-title',(24,16,w-224,32),title,28,True,parent=cid)
  text(cid+'-tag',(w-200,20,176,24),tag,12,color=P['accent'] if accent else '#7a8399',parent=cid,align='right',font='Geist Mono, Menlo, monospace')

container('core',(64,200,2624,1432),'任务处理',kind='zone')
container('external',(2784,200,544,1432),'外部依赖',kind='zone')
container('governance',(64,1760,3264,560),'治理与宿主',kind='zone')
text('eyebrow',(64,28,1720,24),'HARNESS  /  SYSTEM ARCHITECTURE',14,color=P['muted'])
text('title',(64,64,2320,64),'以事实裁决组织目标推进',48,font='Instrument Serif, Songti SC, serif')
text('subtitle',(64,144,2104,32),'九模块  ·  内部组件与权威对象  ·  关键交接',22,color=P['muted'])
text('page-meta',(2544,40,784,32),'LOGICAL VIEW  /  09 MODULES',14,color=P['muted'],align='right',font='Geist Mono, Menlo, monospace')
mods={
 'interaction':((112,280,688,544),'应用与交互','interaction'),
 'home':((944,280,888,544),'任务运行时 · Task Home','task-runtime'),
 'brain':((1976,280,656,544),'大脑','brain'),
 'memory':((112,1000,688,544),'记忆与内容','memory'),
 'collaboration':((944,1000,888,544),'Agent 协作','collaboration'),
 'execution':((1976,1000,656,544),'执行','execution'),
 'security':((112,1824,896,464),'权限与隔离','security'),
 'evaluation':((1168,1824,896,464),'观测、评测与改进','evaluation'),
 'extensions':((2232,1824,1048,464),'扩展与宿主','extensions')}
for mid,(b,title,src) in mods.items():
 parent='governance' if mid in ['security','evaluation','extensions'] else 'core'
 px,py=rects[parent][:2];x,y,w,h=b
 container(mid,(x-px,y-py,w,h),title,parent=parent,accent=mid in ['home','execution'],source=src+'/implementation.md',tag={'home':'RUNTIME','interaction':'INTERACTION','collaboration':'COLLABORATION'}.get(mid,mid.upper()))

# Resize and align existing components without adding or removing any.
node_specs={}
for mid,ns in olddata['node_specs'].items():
 node_specs[mid]=[]
 for nid,(x,y,w,h),label in ns:
  if mid in ['interaction','memory']:
   x=32 if x<300 else 400;w=256;y={96:112,240:240,416:416}[y]
  elif mid=='home':
   x={32:32,312:320,592:608}[x];w=536 if nid=='store' else 248;y={96:112,240:240,424:424}[y]
  elif mid in ['brain','execution']:
   x=32 if x<300 else 376;w=248;y={96:112,240:240,416:416}[y]
  elif mid=='collaboration':
   x=32 if x<400 else 528;w=328;y={96:112,240:240,416:416}[y]
  elif mid in ['security','evaluation']:
   x=32 if x<300 else 328 if x<600 else 624;w=240;y={96:96,240:224,392:352}[y];h=80 if y==352 else 64
  elif mid=='extensions':
   x=32 if x<300 else 376 if x<650 else 728;w=632 if nid=='store' else 288;y={96:96,240:224,392:352}[y];h=80 if y==352 else 64
  node_specs[mid].append((nid,(x,y,w,h),label))

stores={'store','metadata','bytes','resource','index'}
for mid,ns in node_specs.items():
 for nid,box,label in ns:
  isstore=nid in stores
  asyncnode=nid in ['runner','recovery','worker','delivery','control','revocation','rollout']
  adapter=nid in ['adapter','driver','binding']
  fill='#f2f4f7' if isstore or asyncnode else '#f0f5fc' if adapter else '#ffffff'
  sty=('shape=cylinder3;boundedLbl=1;backgroundOutline=1;size=14;' if isstore else 'rounded=1;absoluteArcSize=1;arcSize=8;')
  if isstore:
   parts=label.split('\n',1)
   value=f'<div style="font-size:20px;font-weight:600;line-height:1.3">{html.escape(parts[0])}</div>'
   if len(parts)>1:value+=f'<div style="font-size:16px;color:{P["muted"]};line-height:1.4">{html.escape(parts[1])}</div>'
  else:value=label
  sty+=f'html={1 if isstore else 0};whiteSpace=wrap;fillColor={fill};strokeColor=#aeb8c6;strokeWidth=1.1;fontFamily={FONT};fontSize=22;fontColor={P["ink"]};align=center;verticalAlign=middle;spacing=8;'
  vertex(mid+'-'+nid,box,value,sty,mid,'repository' if isstore else 'component',labelPlain=label)

for cid,b,title in [
 ('model-provider',(2832,424,448,208),'本地模型 / 云模型\n输出 · 请求凭据 · 用量'),
 ('target-system',(2832,1104,448,200),'API / 文件 / 模拟设备\n目标凭据 · 效果证据'),
 ('other-agent',(2832,1368,448,176),'其他 Agent / 另一 Home\n原任务 · 结果 · 封账')]:
 px,py=rects['external'][:2];x,y,w,h=b;head,sub=title.split('\n')
 value=f'<div style="font-size:26px;font-weight:600;line-height:1.4">{html.escape(head)}</div><div style="font-size:20px;color:{P["muted"]};line-height:1.6">{html.escape(sub)}</div>'
 vertex(cid,(x-px,y-py,w,h),value,f'rounded=1;absoluteArcSize=1;arcSize=8;whiteSpace=wrap;html=1;fillColor=#eef2f7;strokeColor=#b4bfce;fontFamily={FONT};fontSize=24;spacing=24;',parent='external',kind='external',labelPlain=title)

def edge(eid,src,dst,path,parent='1',dashed=False,blue=False,flow='',label='',label_box=None):
 color=P['link'] if blue else P['muted'];sx,sy,sw,sh=rects[src];tx,ty,tw,th=rects[dst];a,b=path[0],path[-1]
 sty=f'edgeStyle=orthogonalEdgeStyle;rounded=1;arcSize=8;orthogonalLoop=1;jettySize=0;html=0;strokeColor={color};strokeWidth={2.2 if flow else 1.4};endArrow=block;endSize={9 if flow else 7};endFill=1;jumpStyle=arc;jumpSize=8;'
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
  # Native edge-owned relative label: moving endpoints keeps the label attached.
  lx,ly,lw,lh=label_box;cx,cy=lx+lw/2,ly+lh/2
  lengths=[abs(b[0]-a[0])+abs(b[1]-a[1]) for a,b in zip(path,path[1:])];total=sum(lengths);run=0;choices=[]
  for (a,b),length in zip(zip(path,path[1:]),lengths):
   q=(a[0],min(max(cy,min(a[1],b[1])),max(a[1],b[1]))) if a[0]==b[0] else (min(max(cx,min(a[0],b[0])),max(a[0],b[0])),a[1])
   dist=abs(q[0]-cx)+abs(q[1]-cy);at=run+abs(q[0]-a[0])+abs(q[1]-a[1]);choices.append((dist,at,q));run+=length
  _,at,q=min(choices)
  ls=f'edgeLabel;html=0;whiteSpace=wrap;fillColor={"#ffffff" if parent!="1" else P["paper"]};strokeColor=none;fontFamily={FONT};fontSize={16 if not flow else 18};fontColor={color};align=center;verticalAlign=middle;spacing=0;resizable=1;'
  lab=ET.SubElement(root,'mxCell',id=eid+'-label',value=label,style=ls,vertex='1',parent=eid,semanticKind='edge-label',connectable='0')
  lg=ET.SubElement(lab,'mxGeometry',x=str(2*at/total-1),y='0',width=str(lw),height=str(lh),relative='1',attrib={'as':'geometry'})
  ET.SubElement(lg,'mxPoint',x=str(lx-q[0]),y=str(ly-q[1]),attrib={'as':'offset'})
  rects[eid+'-label']=tuple(label_box);meta['nodes'].append(dict(id=eid+'-label',kind='edge-label',parent=eid,rect=label_box,value=label))

# Original routing algorithm, with more space around components and expensive crossings.
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
    nc=cost+8+(32 if nd!=d else 0)+(2048 if q in occupied_vertices else 0)
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
  if mid == 'execution' and src == 'worker' and dst == 'driver': extra = dict(label='查 / 停', label_box=(2264,1304,80,28))
  if mid == 'execution' and src == 'gate' and dst == 'driver': extra = dict(label='发送', label_box=(2480,1200,64,28))
  edge(mid+'-dep-'+str(ei+1),mid+'-'+src,mid+'-'+dst,path,parent=mid,dashed=len(dep)>2,**extra)
for mid in mods:route_module(mid)

edge('F01','interaction','home',[(800,552),(944,552)],flow='F01',label='01 目标 / 输入',label_box=(808,512,128,32))
edge('F02','home','brain',[(1832,552),(1976,552)],flow='F02',label='02 单轮决策',label_box=(1840,512,128,32))
edge('F03','brain','model-provider',[(2632,528),(2832,528)],flow='F03',blue=True,label='03 模型调用',label_box=(2660,488,144,32))
edge('F04','home','memory',[(1016,824),(1016,880),(456,880),(456,1000)],flow='F04',label='04 检索 / 内容',label_box=(648,840,184,32))
edge('F05','home','execution',[(1736,824),(1736,912),(2304,912),(2304,1000)],flow='F05',label='05 意图 / 控制',label_box=(1928,872,200,32))
edge('F07','home','collaboration',[(1384,824),(1384,1000)],flow='F07',label='07 有界委派',label_box=(1392,936,160,32))
edge('F06','execution','target-system',[(2632,1208),(2832,1208)],flow='F06',blue=True,label='06 原目标动作',label_box=(2648,1168,168,32))
edge('F08','collaboration','other-agent',[(1720,1544),(1720,1600),(2720,1600),(2720,1456),(2832,1456)],flow='F08',blue=True,label='08 原任务交接',label_box=(2136,1560,192,32))
edge('F09','core','security',[(552,1632),(552,1824)],flow='F09',label='09 使用 / 租约 / 结算',label_box=(560,1672,248,32))
edge('F10','core','evaluation',[(1616,1632),(1616,1824)],flow='F10',dashed=True,label='10 获准观测',label_box=(1624,1672,176,32))
edge('F11','evaluation','extensions',[(2064,1984),(2232,1984)],flow='F11',dashed=True,label='11 批准 / 发布',label_box=(2072,1944,152,32))
edge('F11-return','extensions','evaluation',[(2232,2192),(2064,2192)],flow='F11',dashed=True,label='核验 / 实例事实',label_box=(2072,2152,152,32))
edge('F12','extensions','core',[(3280,2240),(3392,2240),(3392,144),(2656,144),(2656,200)],flow='F12',dashed=True,label='12 装配 / 隔离 / 生命周期',label_box=(2864,104,336,32))

text('method-title',(64,2376,224,32),'建模顺序',22,True)
steps=['目标与条件','需裁决的事实','固定 owner','身份与版本','独立状态','提交与恢复']
for i,s in enumerate(steps):
 x=344+i*496
 vertex('method-'+str(i),(x,2368,352,48),s,f'rounded=1;absoluteArcSize=1;arcSize=6;fillColor=#ffffff;strokeColor={P["rule"]};fontFamily={FONT};fontSize=22;',kind='method-step')
 if i:edge('method-edge-'+str(i),'method-'+str(i-1),'method-'+str(i),[(x-144,2392),(x,2392)])
text('deployment-title',(64,2452,224,32),'部署映射',22,True)
text('deployment',(344,2452,2920,32),'本地共库  /  远端原命令与查询  /  生产稳定 Home 分区 + 多可用区',22,color=P['muted'])
vertex('footer-rule',(64,2504,3264,1),'','line;strokeColor=#bfc0c0;strokeWidth=1;fillColor=none;',kind='separator')
text('legend-title',(64,2528,224,32),'图例与范围',22,True)
text('legend1',(344,2520,2920,32),'内部实线：调用 / 读写     内部虚线：持久 job     编号连线：业务交接     蓝线：外部调用     外层虚线：治理 / 装配',20,color=P['muted'])
text('legend2',(344,2560,2920,32),'09 / 10 / 12 作用于相关参与者  ·  模块 ≠ owner ≠ 进程 ≠ 提交域  ·  完整约束见 technical-overview.md',20,color=P['muted'])
# Keep the former explicit details scope available in native hover metadata.
root.find("mxCell[@id='legend2']").set('tooltip','完整字段、状态机、确认消费、其他内容调用与生产恢复条件见 technical-overview.md 及各模块详设。')
allcells=list(root)
def order(c):
 k=c.get('semanticKind','')
 if c.get('id') in ['0','1']:return 0
 if k=='zone':return 1
 if k=='module':return 2
 if k=='header':return 3
 if c.get('edge')=='1':return 4 if 'dashed=1' not in c.get('style','') else 5
 return 6
for c in allcells:root.remove(c)
for c in sorted(allcells,key=order):root.append(c)
ET.indent(mxfile,space='  ')
(BASE/'candidate.drawio').write_bytes(ET.tostring(mxfile,encoding='utf-8',xml_declaration=True))
(BASE/'panorama-layout.json').write_text(json.dumps(meta,ensure_ascii=False,indent=2)+'\n')
print(json.dumps({'modules':len(mods),'component_and_repository':sum(map(len,node_specs.values())),'internal_dependencies':sum(map(len,DEPS.values())),'canvas':[W,H]}))
