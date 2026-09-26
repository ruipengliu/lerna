from pathlib import Path
import json, math, random, itertools, heapq, re, unicodedata
import xml.etree.ElementTree as ET
D=Path('.scratch/uml-models'); OUT=Path('docs/architecture/diagrams/uml-models.drawio')
ORDER=['task-runtime','brain','execution','memory','security','interaction','collaboration','evaluation','extensions']
FONT='Geist, PingFang SC, Microsoft YaHei'; MONO='Geist Mono, Menlo, PingFang SC'; P='#f5f5f5';INK='#2d3142'; MUT='#4f5d75'; ACC='#eb6c36';BLUE='#2e5aa8'
file=ET.Element('mxfile',host='Electron',version='28.0.6',type='device'); ALL=[]

def extent(s,size):return sum(1 if unicodedata.east_asian_width(c) in 'WF' else .57 for c in s)*size

def wrap(s,width,size):
 lines=[]
 for line in s.split('\n'):
  if extent(line,size)<=width:lines.append(line);continue
  # Preserve identifier spelling, split only at explicit separators / spaces.
  words=re.split(r'( / | ＋ | · |\s+)',line);cur=''
  for word in words:
   if cur and extent(cur+word,size)>width:lines.append(cur.strip());cur=word.strip()
   else:cur+=word
  if cur:lines.append(cur.strip())
 return '\n'.join(lines)

def overlap(a,b,g=0):return a[0]<b[0]+b[2]+g and b[0]<a[0]+a[2]+g and a[1]<b[1]+b[3]+g and b[1]<a[1]+a[3]+g

def hits(a,b,r,g=0):
 x,y,w,h=r;x-=g;y-=g;w+=2*g;h+=2*g
 return (a[0]==b[0] and x<a[0]<x+w and max(min(a[1],b[1]),y)<min(max(a[1],b[1]),y+h)) or (a[1]==b[1] and y<a[1]<y+h and max(min(a[0],b[0]),x)<min(max(a[0],b[0]),x+w))

def layout(nodes,edges,kind):
 n=len(nodes);rows=2 if n<=6 else 3
 slots=[(c,r) for r in range(rows) for c in range(3)]
 if n==7:slots=[(0,0),(1,0),(2,0),(0,1),(1,1),(2,1),(1,2)]
 elif n==8:slots=slots[:6]+[(0,2),(2,2)]
 else:slots=slots[:n]
 ids=[q['id'] for q in nodes];idx={v:i for i,v in enumerate(ids)}
 def score(perm):
  pos={k:slots[perm[i]] for i,k in enumerate(ids)};val=0
  for e in edges:
   a=pos[e['from']];b=pos[e['to']];dist=abs(a[0]-b[0])+abs(a[1]-b[1]);val+=dist*4
   if a[0]!=b[0] and a[1]!=b[1]:val+=4
   # Strongly prefer realizing components below their provided interfaces.
   if e['type']=='realization': val+=(20 if b[1]>a[1] else 0)+(3*abs(a[0]-b[0]))
  if kind=='component':
   for node in nodes:
    c,r=pos[node['id']]
    if node.get('boundary')=='provided':val+=r*7
    if node.get('boundary')=='required':val+=(rows-1-r)*3
  # Degree-based hub placement to reduce outer detours.
  for node in nodes:
   degree=sum(node['id'] in [e['from'],e['to']] for e in edges)
   if degree>=3:val+=degree*abs(pos[node['id']][0]-1)*2
  return val
 rng=random.Random(71+n);best=list(range(n));bestv=score(best)
 for _ in range(25):
  p=list(range(n));rng.shuffle(p);v=score(p)
  for k in range(500):
   a,b=rng.sample(range(n),2);q=p[:];q[a],q[b]=q[b],q[a];z=score(q);temp=max(.15,7*(1-k/500))
   if z<v or rng.random()<math.exp(min(0,(v-z)/temp)):p,v=q,z
   if v<bestv:best,bestv=p[:],v
 rects={}
 for i,node in enumerate(nodes):
  c,r=slots[best[i]];w=400
  if kind=='data':h=104+len(node['attributes'])*32
  else:
   name=wrap(node['name'],w-48,23);h=max(176,96+len(name.splitlines())*28+min(3,len(node.get('methods',[])))*25)
  h=math.ceil(h/8)*8
  rects[node['id']]=(120+c*640,280+r*432,w,h)
 return rects,1920,280+rows*432+220

def page(spec,kind,index):
 data=spec[kind];nodes=data['nodes'];edges=data['edges'];rects,W,H=layout(nodes,edges,kind);pageid='uml-'+spec['module']+'-'+kind
 title=spec['title']+' · '+('组件与接口' if kind=='component' else '领域对象')
 diagram=ET.SubElement(file,'diagram',id=pageid,name=f'{index:02d} {title}')
 model=ET.SubElement(diagram,'mxGraphModel',dx=str(W),dy=str(H),grid='1',gridSize='8',guides='1',tooltips='1',connect='1',arrows='1',fold='1',page='1',pageScale='1',pageWidth=str(W),pageHeight=str(H),background=P,math='0',shadow='0')
 root=ET.SubElement(model,'root');ET.SubElement(root,'mxCell',id='0');ET.SubElement(root,'mxCell',id='1',parent='0')
 meta=dict(id=pageid,name=title,index=index,size=[W,H],rects=rects,edges=[],labels=[],texts=[])
 def v(id,box,value='',style='',parent='1',semantic='decoration',**kw):
  c=ET.SubElement(root,'mxCell',dict(id=id,value=value,style=style,vertex='1',parent=parent,semanticKind=semantic,**kw));x,y,w,h=box
  ET.SubElement(c,'mxGeometry',x=str(x),y=str(y),width=str(w),height=str(h),attrib={'as':'geometry'});return c
 def text(id,box,value,size=20,color=INK,bold=False,parent='1',align='left',mono=False):
  v(id,box,value,f'text;html=0;whiteSpace={"nowrap" if mono else "wrap"};overflow=hidden;strokeColor=none;fillColor=none;align={align};verticalAlign=middle;spacing=0;fontFamily={MONO if mono else FONT};fontSize={size};fontColor={color};fontStyle={1 if bold else 0};',parent,'text')
  meta['texts'].append(dict(id=id,box=box,value=value,size=size,parent=parent))
 text('eyebrow',(80,30,1500,24),f'HARNESS  /  UML ATLAS  /  {index:02d}',14,MUT,mono=True)
 text('title',(80,70,1760,62),title,42,bold=True)
 subtitle='提供接口 · 内部职责 · 所需接口' if kind=='component' else '关键属性 · 事实归属 · 关联多重性'
 if spec['module']=='system':subtitle='九模块的主要软件依赖 · 接口能力节选'
 text('subtitle',(80,150,1700,32),subtitle,22,MUT)
 # Route on an 8-unit orthogonal grid, using all nodes as obstacles.
 occupied=set();occupied_vertices=set();usedports={};counter=itertools.count();blocked_labels=[]
 def segkey(a,b):return tuple(sorted((a,b)))
 def forbidden(p):
  x,y=p
  return x<40 or x>W-40 or y<216 or y>H-220 or any(a-16<x<a+w+16 and b-16<y<b+h+16 for a,b,w,h in rects.values()) or any(a-8<x<a+w+8 and b-8<y<b+h+8 for a,b,w,h in blocked_labels)
 def ports(n,stub=16):
  x,y,w,h=rects[n];cx=x+w//2;cy=y+round(h/16)*8;out=[]
  for delta in [0,-48,48,-96,96,-144,144]:
   for p,d in [((cx+delta,y),(0,-1)),((cx+delta,y+h),(0,1)),((x,cy+delta),(-1,0)),((x+w,cy+delta),(1,0))]:
    if d[0]==0 and not x+32<=p[0]<=x+w-32:continue
    if d[1]==0 and not y+32<=p[1]<=y+h-32:continue
    if any(abs(p[0]-q[0])+abs(p[1]-q[1])<32 for q in usedports.get(n,[])):continue
    q=(p[0]+d[0]*stub,p[1]+d[1]*stub)
    if not forbidden(q):out.append((p,q,d))
  return out
 paths=[]
 # Draw shortest and more constrained edges first.
 def priority(e):
  a=rects[e['from']];b=rects[e['to']]
  return (-1 if e['from']==e['to'] else 0 if e['type']=='realization' else 1,abs(a[0]-b[0])+abs(a[1]-b[1]))
 for ei,e in sorted(enumerate(edges),key=lambda q:priority(q[1])):
  stub=128 if e['from']==e['to'] else 16;starts=ports(e['from'],stub);ends=ports(e['to'],stub)
  if e['from']==e['to']:
   pairs=[(a,b) for a in starts for b in ends if a[2]==b[2] and abs(a[0][0]-b[0][0])+abs(a[0][1]-b[0][1])>=160 and not any(hits(a[1],b[1],r,16) for r in rects.values())]
   assert pairs
   a,b=min(pairs,key=lambda t: (0 if t[0][2]==(0,-1) else 1,abs(t[0][0][0]-t[1][0][0])+abs(t[0][0][1]-t[1][0][1])))
   starts=[a];ends=[b]
  targets={q:(p,d) for p,q,d in ends};todo=[];best={};prev={};initial={}
  def heur(q):return min(abs(q[0]-t[0])+abs(q[1]-t[1]) for t in targets)
  for p,q,d in starts:
   s=(q,d);best[s]=0;initial[s]=p;heapq.heappush(todo,(heur(q),0,next(counter),s))
  found=None
  while todo:
   _,cost,_,state=heapq.heappop(todo);p,d=state
   if cost!=best.get(state):continue
   if p in targets:
    ep,ed=targets[p]
    if d!=ed and segkey(p,ep) not in occupied:found=(state,ep);break
   for nd in [(1,0),(0,1),(-1,0),(0,-1)]:
    if nd==(-d[0],-d[1]):continue
    q=(p[0]+nd[0]*8,p[1]+nd[1]*8)
    if forbidden(q) or segkey(p,q) in occupied:continue
    # Repel parallel routes, permit a distinct crossing with a native jump.
    near=sum((q[0]+ox,q[1]+oy) in occupied_vertices for ox,oy in [(0,8),(0,-8),(8,0),(-8,0)])
    nc=cost+8+(48 if nd!=d else 0)+(2600 if q in occupied_vertices else 0)+near*80
    ns=(q,nd)
    if nc<best.get(ns,1e30):best[ns]=nc;prev[ns]=state;heapq.heappush(todo,(nc+heur(q),nc,next(counter),ns))
  assert found,(pageid,e)
  state,ep=found;chain=[ep]
  while state in prev:chain.append(state[0]);state=prev[state]
  chain.extend([state[0],initial[state]]);chain.reverse()
  usedports.setdefault(e['from'],[]).append(chain[0]);usedports.setdefault(e['to'],[]).append(chain[-1])
  # Expand port stubs to the same lattice as the routed middle.
  for a,b in zip(chain,chain[1:]):
   dx=(b[0]>a[0])-(b[0]<a[0]);dy=(b[1]>a[1])-(b[1]<a[1]);p=a
   while p!=b:
    q=(p[0]+8*dx,p[1]+8*dy);occupied.add(segkey(p,q));occupied_vertices.update([p,q]);p=q
  compact=[chain[0]]
  for a,b,c in zip(chain,chain[1:],chain[2:]):
   if (b[0]-a[0])*(c[1]-b[1])!=(b[1]-a[1])*(c[0]-b[0]):compact.append(b)
  compact.append(chain[-1]);paths.append((ei,e,compact))
 # Keep edge labels native, attached to their own relationship.
 def label(eid,value,box,path,role):
  lx,ly,lw,lh=box;cx,cy=lx+lw/2,ly+lh/2;lengths=[abs(b[0]-a[0])+abs(b[1]-a[1]) for a,b in zip(path,path[1:])];total=sum(lengths);run=0;choices=[]
  for (a,b),length in zip(zip(path,path[1:]),lengths):
   q=(a[0],min(max(cy,min(a[1],b[1])),max(a[1],b[1]))) if a[0]==b[0] else (min(max(cx,min(a[0],b[0])),max(a[0],b[0])),a[1])
   choices.append((abs(q[0]-cx)+abs(q[1]-cy),run+abs(q[0]-a[0])+abs(q[1]-a[1]),q));run+=length
  _,at,q=min(choices);lid=eid+'-'+role
  c=ET.SubElement(root,'mxCell',id=lid,value=value,style=f'edgeLabel;html=0;whiteSpace=wrap;fillColor={P};strokeColor=none;fontFamily={FONT};fontSize=18;fontColor={MUT};align=center;verticalAlign=middle;spacing=0;resizable=1;',vertex='1',parent=eid,semanticKind=role,connectable='0')
  g=ET.SubElement(c,'mxGeometry',x=str(2*at/total-1),y='0',width=str(lw),height=str(lh),relative='1',attrib={'as':'geometry'});ET.SubElement(g,'mxPoint',x=str(lx-q[0]),y=str(ly-q[1]),attrib={'as':'offset'})
  blocked_labels.append(box);meta['labels'].append(dict(id=lid,value=value,rect=box,role=role))
 def fits(box):
  if box[0]<32 or box[0]+box[2]>W-32 or box[1]<208 or box[1]+box[3]>H-206:return False
  if any(overlap(box,r,6) for r in rects.values()) or any(overlap(box,r,8) for r in blocked_labels):return False
  return not any(hits(a,b,box,5) for _,_,p in paths for a,b in zip(p,p[1:]))
 def multbox(p,value,end=False):
  a,b=(p[-1],p[-2]) if end else (p[0],p[1]);dx=(b[0]>a[0])-(b[0]<a[0]);dy=(b[1]>a[1])-(b[1]<a[1]);w=max(24,math.ceil(extent(value,18))+8);h=24
  for offset in [16,24,40,56,72]:
   x=a[0]+dx*offset;y=a[1]+dy*offset
   choices=[(x if dx>0 else x-w,y-h-8,w,h),(x if dx>0 else x-w,y+8,w,h)] if dx else [(x+8,y if dy>0 else y-h,w,h),(x-w-8,y if dy>0 else y-h,w,h)]
   for box in choices:
    if fits(box):return box
  raise AssertionError(('multiplicity label',pageid,value,a,b))
 for ei,e,path in paths:
  eid='e'+str(ei);src=e['from'];dst=e['to'];sx,sy,sw,sh=rects[src];tx,ty,tw,th=rects[dst];a,b=path[0],path[-1]
  dashed=e['type'] in ['dependency','realization'];arrow={'dependency':'open','realization':'block','association':'none'}[e['type']]
  style=f'edgeStyle=orthogonalEdgeStyle;rounded=1;arcSize=8;jettySize=0;html=0;strokeColor={MUT};strokeWidth=1.7;endArrow={arrow};endFill=0;endSize=12;jumpStyle=arc;jumpSize=8;'
  style+=f'exitX={(a[0]-sx)/sw};exitY={(a[1]-sy)/sh};exitPerimeter=0;entryX={(b[0]-tx)/tw};entryY={(b[1]-ty)/th};entryPerimeter=0;'
  if dashed:style+='dashed=1;dashPattern=5 4;'
  c=ET.SubElement(root,'mxCell',id=eid,value='',style=style,edge='1',parent='1',source=src,target=dst,semanticKind=e['type'],sourceDoc=e.get('source',''),tooltip=e.get('constraint',''))
  g=ET.SubElement(c,'mxGeometry',relative='1',attrib={'as':'geometry'})
  if len(path)>2:
   ar=ET.SubElement(g,'Array',attrib={'as':'points'})
   for x,y in path[1:-1]:ET.SubElement(ar,'mxPoint',x=str(x),y=str(y))
  meta['edges'].append(dict(id=eid,source=src,target=dst,path=path,kind=e['type']))
  if kind=='data':
   label(eid,e['fromMultiplicity'],multbox(path,e['fromMultiplicity']),path,'source-multiplicity')
   label(eid,e['toMultiplicity'],multbox(path,e['toMultiplicity'],True),path,'target-multiplicity')
 # Relations are positioned after all multiplicities to keep endpoint reading clear.
 for ei,e,path in sorted(paths,key=lambda t:-extent(t[1].get('label',''),18)):
  value=e.get('label','')
  if not value or value=='提供':continue
  w=math.ceil(extent(value,18))+16;h=28;candidates=[];total=sum(abs(b[0]-a[0])+abs(b[1]-a[1]) for a,b in zip(path,path[1:]));run=0
  for a,b in zip(path,path[1:]):
   length=abs(b[0]-a[0])+abs(b[1]-a[1])
   for f in [.5,.3,.7,.15,.85]:
    x=a[0]+(b[0]-a[0])*f;y=a[1]+(b[1]-a[1])*f;score=abs(run+length*f-total/2)
    if a[1]==b[1]:bs=[(x-w/2,y-h-8,w,h),(x-w/2,y+8,w,h)]
    else:bs=[(x+8,y-h/2,w,h),(x-w-8,y-h/2,w,h)]
    for k,box in enumerate(bs):
     if fits(box):candidates.append((score+k*8+(120 if a[0]==b[0] else 0),box))
   run+=length
  assert candidates,('relation label',pageid,e)
  label('e'+str(ei),value,min(candidates)[1],path,'relationship-label')
 # Node compartments are native cells under their editable parent.
 for ni,node in enumerate(nodes):
  nid=node['id'];x,y,w,h=rects[nid];focal=(nid==('task-runtime' if spec['module']=='system' else 'api') if kind=='component' else nid=={'core':'task','task-runtime':'task','brain':'decision','execution':'operation','memory':'memory','security':'use','interaction':'submission','collaboration':'delegation','evaluation':'report','extensions':'activation'}[spec['module']])
  color=ACC if focal else (BLUE if node.get('kind')=='interface' and node.get('boundary')=='provided' else '#aeb7c3')
  style=f'rounded=1;absoluteArcSize=1;arcSize=6;fillColor=#ffffff;strokeColor={color};strokeWidth={1.8 if focal else 1.3};container=1;collapsible=0;recursiveResize=0;'
  v(nid,(x,y,w,h),'',style,semantic='class' if kind=='data' else node.get('kind','component'),sourceDoc=node.get('source',''),schemaRef=node.get('schema') or '',tooltip=node.get('note',''))
  if kind=='data':
   v(nid+'-head',(1,1,w-2,96),'',f'fillColor={"#fcf0e9" if focal else "#f1f3f5"};strokeColor=none;',parent=nid,semantic='header')
   text(nid+'-name',(16,12,w-32,40),node['name'],min(26,int((w-32)/(extent(node['name'],1)))),bold=True,parent=nid,align='center')
   text(nid+'-owner',(16,60,w-32,24),node['owner'],min(17,int((w-32)/(extent(node['owner'],1)))),MUT,parent=nid,align='center')
   v(nid+'-rule',(0,96,w,1),'','line;strokeColor=#cbd1d8;',parent=nid)
   for j,attr in enumerate(node['attributes']):
    value=attr['name']+': '+attr['type'];size=min(18,math.floor((w-40)/(len(value)*.68)))
    text(nid+'-attr-'+str(j),(16,104+j*32,w-32,30),value,size,parent=nid,mono=True)
  else:
   boundary=node.get('boundary');tag='«interface»' if node.get('kind')=='interface' else '«component»'
   text(nid+'-kind',(20,14,w-40,24),tag,17,MUT,parent=nid,align='center')
   name=wrap(node['name'],w-48,23);nh=len(name.splitlines())*28
   text(nid+'-name',(24,48,w-48,nh),name,23,bold=True,parent=nid,align='center')
   methods=node.get('methods',[])[:3]
   if methods:
    for j,m in enumerate(methods):
     size=17 if extent(m,17)<w-32 else 15
     text(nid+'-method-'+str(j),(16,56+nh+j*25,w-32,24),m,size,BLUE if boundary=='provided' else MUT,parent=nid,align='center',mono=True)
   else:
    role={'internal':'内部职责','required':'所需边界','provided':'提供边界'}.get(boundary,'')
    text(nid+'-role',(16,h-36,w-32,24),role,16,MUT,parent=nid,align='center')
 # Footer is intentionally outside the routing field.
 v('foot-rule',(80,H-182,W-160,1),'','line;strokeColor=#bfc0c0;')
 if kind=='component':legend='虚线空心三角：实现接口    虚线开箭头：依赖    方法为接口节选'
 else:legend='实线：普通关联    两端：多重性    ?：可选属性    owner：事实负责方'
 text('legend',(80,H-165,W-160,28),legend,19,MUT)
 notes=data.get('notes',[])[:2]
 for j,note in enumerate(notes):text('note-'+str(j),(80,H-123+j*30,W-160,28),note,19,MUT)
 text('source-note',(80,H-48,W-160,24),'完整约束与逐页来源：docs/architecture/uml-models.md',15,MUT,mono=True)
 # Edge labels and nodes above paths; no raster content.
 cells=list(root)
 for c in cells:root.remove(c)
 def z(c):
  if c.get('id') in ['0','1']:return 0
  if c.get('edge')=='1':return 1
  return 2
 for c in sorted(cells,key=z):root.append(c)
 ALL.append(meta)
 print(pageid,len(nodes),len(edges),flush=True)

page(json.loads((D/'system.json').read_text()),'component',1)
page(json.loads((D/'core.json').read_text()),'data',2)
for i,m in enumerate(ORDER):
 spec=json.loads((D/(m+'.json')).read_text());page(spec,'component',i*2+3);page(spec,'data',i*2+4)
ET.indent(file,space='  ');OUT.write_bytes(ET.tostring(file,encoding='utf-8',xml_declaration=True));(D/'layout.json').write_text(json.dumps(ALL,ensure_ascii=False,indent=2)+'\n')
print('wrote',OUT,'pages',len(ALL))
