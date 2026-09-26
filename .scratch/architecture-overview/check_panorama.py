from pathlib import Path
from collections import Counter
import json
import xml.etree.ElementTree as ET

base=Path('.scratch/architecture-overview')
p=Path('docs/architecture/diagrams/system-panorama.drawio')
tree=ET.parse(p);cells=tree.findall('.//mxCell');ids=[c.get('id') for c in cells]
assert len(ids)==len(set(ids)), 'duplicate ids'
lookup={c.get('id'):c for c in cells}
for c in cells:
 for attr in ['parent','source','target']:
  if c.get(attr):assert c.get(attr) in lookup,(c.get('id'),attr)
 seen=set();a=c
 while a.get('parent'):
  assert a.get('id') not in seen,'parent cycle'
  seen.add(a.get('id'));a=lookup[a.get('parent')]
 assert 'image=' not in c.get('style','') and not c.get('style','').startswith('image'), 'flattened image'
 assert c.get('style','').find('editable=0')==-1,'locked content'
counts=Counter(c.get('semanticKind') for c in cells)
assert counts['module']==9 and counts['external']==3 and counts['zone']==3,counts
assert counts['component']+counts['repository']==54,counts
assert len({c.get('flowId') for c in cells if c.get('flowId')})==12
assert sum(c.get('semanticKind')=='dependency' and c.get('parent')!='1' for c in cells)==58
for c in cells:
 if c.get('sourceDoc'):assert Path('docs/architecture',c.get('sourceDoc')).is_file()
layout=json.loads((base/'panorama-layout.json').read_text())
nodes={n['id']:n for n in layout['nodes']}

def ancestors(cid):
 ans=set()
 while cid in lookup and lookup[cid].get('parent'):
  cid=lookup[cid].get('parent');ans.add(cid)
 return ans

def intersects_rect(a,b,r):
 x,y,w,h=r
 if a[0]==b[0]:return x<a[0]<x+w and max(min(a[1],b[1]),y)<min(max(a[1],b[1]),y+h)
 return y<a[1]<y+h and max(min(a[0],b[0]),x)<min(max(a[0],b[0]),x+w)

def overlap(a,b):
 x,y,w,h=a;u,v,k,l=b
 return max(x,u)<min(x+w,u+k) and max(y,v)<min(y+h,v+l)

segments=[];attach={}
for e in layout['edges']:
 path=e['path'];assert all(a[0]==b[0] or a[1]==b[1] for a,b in zip(path,path[1:])),e['id']
 allowed={e['source'],e['target']}|ancestors(e['source'])|ancestors(e['target'])
 for n in nodes.values():
  if n['id'] in allowed or n['kind'] in ['text','edge-label']:continue
  assert not any(intersects_rect(a,b,n['rect']) for a,b in zip(path,path[1:])),('transit',e['id'],n['id'])
 if e.get('label_box'):
  for n in nodes.values():
   if n['kind'] in ['zone','text','edge-label'] or n['id'] in ancestors(e['source']) & ancestors(e['target']):continue
   assert not overlap(e['label_box'],n['rect']),('label overlaps',e['id'],n['id'])
 for a,b in zip(path,path[1:]):segments.append((e['id'],a,b))
 for name,point in [(e['source'],path[0]),(e['target'],path[-1])]:
  for prior,other in attach.get(name,[]):
   assert abs(point[0]-other[0])+abs(point[1]-other[1])>=16,('shared port',name,prior,e['id'])
  attach.setdefault(name,[]).append((e['id'],point))
for i,(eid,a,b) in enumerate(segments):
 for fid,c,d in segments[i+1:]:
  if eid==fid:continue
  if a[0]==b[0]==c[0]==d[0]:
   assert max(min(a[1],b[1]),min(c[1],d[1]))>=min(max(a[1],b[1]),max(c[1],d[1])),('shared segment',eid,fid)
  if a[1]==b[1]==c[1]==d[1]:
   assert max(min(a[0],b[0]),min(c[0],d[0]))>=min(max(a[0],b[0]),max(c[0],d[0])),('shared segment',eid,fid)
# All component shapes are disjoint and inside their native parent.
for n in nodes.values():
 if n['kind'] not in ['component','repository']:continue
 x,y,w,h=n['rect'];px,py,pw,ph=nodes[n['parent']]['rect']
 assert px<=x and py<=y and x+w<=px+pw and y+h<=py+ph,n['id']
 for m in nodes.values():
  if m['id']>n['id'] and m['parent']==n['parent'] and m['kind'] in ['component','repository']:
   assert not overlap(n['rect'],m['rect']),(n['id'],m['id'])
result={'cells':len(cells),'modules':9,'components_and_repositories':54,'internal_dependencies':58,'external_nodes':3,'handoff_groups':12,'all_edges':len(layout['edges']),'structure':'PASS','geometry':'PASS'}
# draw.io serializes custom attrs as a UserObject wrapper; normalize both forms.
roundtrip=base/'panorama-roundtrip.xml'
if roundtrip.exists():
 def norm(t):
  out={}
  for c in t.findall('.//mxCell'):
   attrs=dict(c.attrib);cid=attrs.get('id')
   if not cid:
    parent=next((p for p in t.iter() if c in list(p)),None)
    if parent is not None:attrs={**parent.attrib,**attrs};cid=attrs.get('id');attrs['value']=attrs.pop('label',attrs.get('value',''))
   if not cid:continue
   out[cid]=(attrs,ET.tostring(c.find('mxGeometry'),encoding='unicode') if c.find('mxGeometry') is not None else '')
  return out
 left=norm(tree);right=norm(ET.parse(roundtrip))
 assert set(left)==set(right),'roundtrip ID set changed'
 def geom(g):return ET.tostring(ET.fromstring(g),encoding='unicode').strip() if g else ''
 for cid,(attrs,g) in left.items():
  other,h=right[cid]
  for key in ['value','style','parent','source','target','vertex','edge','semanticKind','flowId','sourceDoc']:
   assert attrs.get(key,'')==other.get(key,''),(cid,key,attrs.get(key),other.get(key))
  # Compare numeric geometry recursively, ignoring whitespace and formatting.
  def signature(s):
   if not s:return None
   e=ET.fromstring(s)
   def rec(n):return(n.tag,tuple(sorted((k,str(float(v)) if k in ['x','y','width','height'] else v) for k,v in n.attrib.items())),tuple(rec(c) for c in n))
   return rec(e)
  assert signature(g)==signature(h),('roundtrip geometry',cid)
 result['drawio_roundtrip']='PASS'
(base/'panorama-check.json').write_text(json.dumps(result,ensure_ascii=False,indent=2)+'\n')
print(json.dumps(result,ensure_ascii=False))
