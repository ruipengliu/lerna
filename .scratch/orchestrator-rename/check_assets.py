from pathlib import Path
import xml.etree.ElementTree as E,json,subprocess,re,hashlib
ROOT=Path.cwd();D=ROOT/'.scratch/orchestrator-rename';schemas=json.loads((ROOT/'docs/architecture/contracts/schemas/protocol.schema.json').read_text())['$defs'];errors=[];report={}
def need(cond,msg):
 if not cond:errors.append(msg)
def normalized(e):
 a=dict(e.attrib)
 if e.tag in ('mxGeometry','mxPoint'):
  for k in ('x','y','width','height') if e.tag=='mxGeometry' else ('x','y'):a[k]=round(float(a.get(k,0)),6)
 return [e.tag,sorted(a.items()),[normalized(c) for c in e]]
for p in (ROOT/'docs/architecture/diagrams').glob('*.drawio'):
 tree=E.parse(p).getroot();rt=E.parse(D/'drawio'/(p.stem+'-roundtrip.drawio')).getroot();counts={'pages':0,'cells':0,'edges':0,'source_links':0,'schema_fields':0}
 need(len(tree.findall('diagram'))==len(rt.findall('diagram')),f'{p.name}: roundtrip page count')
 for page,back in zip(tree.findall('diagram'),rt.findall('diagram')):
  counts['pages']+=1;cells=page.findall('.//mxCell');ids={c.get('id'):c for c in cells};counts['cells']+=len(cells)
  need(len(ids)==len(cells),f'{p.name}/{page.get("id")}: duplicate ID')
  need(page.get('id')==back.get('id') and page.get('name')==back.get('name'),f'{p.name}: page metadata')
  need({c.get('id'):normalized(c) for c in cells}=={c.get('id'):normalized(c) for c in back.findall('.//mxCell')},f'{p.name}/{page.get("id")}: roundtrip drift')
  for c in cells:
   for a in ('parent','source','target'):
    if c.get(a):need(c.get(a) in ids,f'{p.name}: broken {a} {c.get(a)}')
   counts['edges']+=c.get('edge')=='1'
   if c.get('sourceDoc'):
    src=c.get('sourceDoc').split('#')[0];counts['source_links']+=1
    need((ROOT/'docs/architecture'/src).is_file(),f'{p.name}: missing source {src}')
   if c.get('schemaRef'):need(c.get('schemaRef') in schemas,f'{p.name}: missing schema {c.get("schemaRef")}')
   parent=ids.get(c.get('parent'));ref=parent.get('schemaRef') if parent is not None else None
   if ref and '-attr-' in c.get('id',''):
    name=c.get('value','').split(':')[0].strip();props=set(schemas[ref].get('properties',{}))
    for branch in schemas[ref].get('oneOf',[]):props.update(branch.get('properties',{}))
    need(name in props,f'{p.name}: {ref}.{name} missing');counts['schema_fields']+=1
 report[p.name]=counts
pat=re.compile(r'^```mermaid[^\n]*\n(.*?)^```\s*$',re.M|re.S)
manifest=json.loads((D/'mermaid/manifest.json').read_text())
for item in manifest:
 blocks=list(pat.finditer((ROOT/item['source']).read_text()));body=blocks[item['number']-1].group(1).rstrip()
 need(item['passed'] and (ROOT/item['render']).is_file(),f'Mermaid failed: {item["source"]}:{item["number"]}')
 need(item['sha256']==hashlib.sha256(body.encode()).hexdigest(),f'Mermaid source changed: {item["source"]}')
report['mermaid_rendered']=len(manifest);report['errors']=errors
(D/'asset-checks.json').write_text(json.dumps(report,ensure_ascii=False,indent=2)+'\n');print(json.dumps(report,ensure_ascii=False,indent=2));raise SystemExit(bool(errors))
