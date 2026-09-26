from pathlib import Path
import xml.etree.ElementTree as ET
import json,re,hashlib,math
D=Path('.scratch/uml-models');layouts=json.loads((D/'layout.json').read_text());schema=json.loads(Path('docs/architecture/contracts/schemas/protocol.schema.json').read_text())['$defs'];methods=json.loads(Path('docs/architecture/contracts/schemas/methods.json').read_text())['methods'];errors=[]
def check(ok,msg):
 if not ok:errors.append(msg)
def overlap(a,b):return a[0]<b[0]+b[2] and b[0]<a[0]+a[2] and a[1]<b[1]+b[3] and b[1]<a[1]+a[3]
def hits(a,b,r):
 x,y,w,h=r
 return (a[0]==b[0] and x<a[0]<x+w and max(min(a[1],b[1]),y)<min(max(a[1],b[1]),y+h)) or (a[1]==b[1] and y<a[1]<y+h and max(min(a[0],b[0]),x)<min(max(a[0],b[0]),x+w))
methods_n=attrs_n=classes_n=nodes_n=edges_n=labels_n=0
for specp in D.glob('*.json'):
 s=json.loads(specp.read_text())
 if 'module' not in s:continue
 for kind in ['component','data']:
  if kind not in s:continue
  d=s[kind];ids={n['id'] for n in d['nodes']}
  for n in d['nodes']:
   for m in n.get('methods',[]):check(m in methods,f'{specp}: method {m}');methods_n+=1
   if n.get('schema'):
    check(n['schema'] in schema,f'{specp}: schema {n["schema"]}')
    obj=schema.get(n['schema'],{});props=set(obj.get('properties',{}))
    # Proposal union has common properties inside oneOf alternatives.
    for branch in obj.get('oneOf',[]):props.update(branch.get('properties',{}))
    for a in n['attributes']:check(a['name'] in props,f'{specp}: field {n["name"]}.{a["name"]}');attrs_n+=1
    classes_n+=1
   source=n.get('source','').split('#')[0];check(bool(source) and (Path('docs/architecture')/source).is_file(),f'{specp}: source {source}')
  for e in d['edges']:
   check(e['from'] in ids and e['to'] in ids,f'{specp}: endpoint {e}')
   if kind=='data':check(bool(e.get('fromMultiplicity')) and bool(e.get('toMultiplicity')),f'{specp}: missing multiplicity')
for p in layouts:
 rects=p['rects'];ids=list(rects);paths=p['edges'];nodes_n+=len(ids);edges_n+=len(paths);labels_n+=len(p['labels'])
 for i,a in enumerate(ids):
  for b in ids[i+1:]:check(not overlap(rects[a],rects[b]),f'{p["id"]}: nodes {a} / {b} overlap')
 for i,l in enumerate(p['labels']):
  for n,r in rects.items():check(not overlap(l['rect'],r),f'{p["id"]}: label {l["id"]} overlaps {n}')
  for r in p['labels'][i+1:]:check(not overlap(l['rect'],r['rect']),f'{p["id"]}: labels {l["id"]} / {r["id"]}')
  for e in paths:
   for a,b in zip(e['path'],e['path'][1:]):check(not hits(a,b,l['rect']),f'{p["id"]}: path hits label {l["id"]}')
 segments=[];ports={}
 for e in paths:
  path=e['path']
  for a,b in zip(path,path[1:]):
   check(a[0]==b[0] or a[1]==b[1],f'{p["id"]}: diagonal {e["id"]}')
   for n,r in rects.items():check(not hits(a,b,r),f'{p["id"]}: edge {e["id"]} crosses {n}')
   for c,d,eid in segments:
    same=a[0]==b[0]==c[0]==d[0] and max(min(a[1],b[1]),min(c[1],d[1]))<min(max(a[1],b[1]),max(c[1],d[1]))
    same=same or (a[1]==b[1]==c[1]==d[1] and max(min(a[0],b[0]),min(c[0],d[0]))<min(max(a[0],b[0]),max(c[0],d[0])))
    check(not same,f'{p["id"]}: shared segment {e["id"]}/{eid}')
   segments.append((a,b,e['id']))
  for node,pt in [(e['source'],path[0]),(e['target'],path[-1])]:
   for q in ports.get(node,[]):check(abs(pt[0]-q[0])+abs(pt[1]-q[1])>=16,f'{p["id"]}: shared port {node}')
   ports.setdefault(node,[]).append(pt)
source=ET.parse('docs/architecture/diagrams/uml-models.drawio').getroot();check(len(source.findall('diagram'))==20,'page count')
cell_n=0
for p in source.findall('diagram'):
 cells=p.findall('.//mxCell');ids=[c.get('id') for c in cells];cell_n+=len(cells);check(len(ids)==len(set(ids)),f'{p.get("id")}: duplicate cell ids')
 for c in cells:
  check('image=' not in c.get('style',''),f'{p.get("id")}: raster cell')
  for k in ['parent','source','target']:
   if c.get(k):check(c.get(k) in ids,f'{p.get("id")}: broken {k}')
def norm(e):
 attrs=dict(e.attrib)
 if e.tag in ['mxGeometry','mxPoint']:
  for key in (['x','y','width','height'] if e.tag=='mxGeometry' else ['x','y']):
   val=attrs.pop(key,'0')
   attrs[key]=round(float(val),6)
 return [e.tag,sorted(attrs.items()),[norm(c) for c in e]]
roundtrip=D/'roundtrip.drawio'
if roundtrip.exists():
 target=ET.parse(roundtrip).getroot()
 check(len(target.findall('diagram'))==20,'roundtrip pages')
 for a,b in zip(source.findall('diagram'),target.findall('diagram')):
  check(a.get('id')==b.get('id') and a.get('name')==b.get('name'),f'{a.get("id")}: roundtrip page metadata')
  left={c.get('id'):norm(c) for c in a.findall('.//mxCell')};right={c.get('id'):norm(c) for c in b.findall('.//mxCell')}
  for key in left:check(left[key]==right.get(key),f'{a.get("id")}: roundtrip changed {key}')
baseline=json.loads((D/'baseline.json').read_text())
drift=[f for f,h in baseline['files'].items() if hashlib.sha256(Path(f).read_bytes()).hexdigest()!=h]
current=[]
for p in sorted(Path('docs/architecture').rglob('*.md')):
 for block in re.findall(r'```mermaid\n(.*?)```',p.read_text(),re.S):current.append((str(p),hashlib.sha256(block.encode()).hexdigest()))
# Baseline representation is inspected explicitly, avoiding any assumptions about ordering.
old=baseline['mermaid']
if isinstance(old,list):
 def record(x):
  if isinstance(x,dict):return (x.get('source',x.get('path',x.get('file'))),x.get('sha256',x.get('hash')))
  return tuple(x)
 old_blocks=sorted(map(record,old));mermaid_drift=[f for f,h in current if (f,h) not in old_blocks]
else:check(False,'unexpected mermaid baseline format')
result=dict(pages=len(layouts),logical_nodes=nodes_n,relationships=edges_n,edge_labels=labels_n,mxCells=cell_n,visible_method_references=sum(c.get('value') in methods for c in source.findall('.//mxCell')),spec_method_references=methods_n,schema_classes=classes_n,schema_attributes=attrs_n,baseline_files=len(baseline['files']),other_file_changes=drift,mermaid_blocks=len(current),other_mermaid_changes=sorted(set(mermaid_drift)),errors=errors)
(D/'check-result.json').write_text(json.dumps(result,ensure_ascii=False,indent=2)+'\n');print(json.dumps(result,ensure_ascii=False,indent=2));raise SystemExit(bool(errors))
