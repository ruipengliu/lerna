from pathlib import Path
import xml.etree.ElementTree as E,subprocess,json
ROOT=Path.cwd();OUT=ROOT/'.scratch/design-ablation/drawio';OUT.mkdir(parents=True,exist_ok=True);items=[]
for p in (ROOT/'docs/architecture/diagrams').glob('*.drawio'):
 old=E.fromstring(subprocess.check_output(['git','show','HEAD:'+str(p.relative_to(ROOT))]));new=E.parse(p).getroot()
 for n,(a,b) in enumerate(zip(old.findall('diagram'),new.findall('diagram')),1):
  if [c.get('value') for c in a.findall('.//mxCell')]==[c.get('value') for c in b.findall('.//mxCell')]:continue
  out=OUT/(p.stem+f'-{n:02d}.png')
  r=subprocess.run(['/Applications/draw.io.app/Contents/MacOS/draw.io','-x','-f','png','-p',str(n),'--width','2400','-b','24','-o',str(out),str(p)],capture_output=True,text=True,timeout=60)
  items.append({'source':str(p.relative_to(ROOT)),'page':n,'name':b.get('name'),'render':str(out.relative_to(ROOT)),'passed':r.returncode==0 and out.exists()})
  (OUT/'manifest.json').write_text(json.dumps(items,ensure_ascii=False,indent=2)+'\n')
  print(f'{p.name} page {n}: {r.returncode}',flush=True)
  assert r.returncode==0,(r.stdout,r.stderr)
 r=subprocess.run(['/Applications/draw.io.app/Contents/MacOS/draw.io','-x','-f','xml','-u','-o',str(OUT/(p.stem+'-roundtrip.drawio')),str(p)],capture_output=True,text=True,timeout=60)
 assert r.returncode==0,(r.stdout,r.stderr)
print(f'Rendered {len(items)} changed drawio pages.',flush=True)
