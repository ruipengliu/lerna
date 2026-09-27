from pathlib import Path
import re,subprocess,json,hashlib
ROOT=Path.cwd();OUT=ROOT/'.scratch/design-ablation/mermaid';OUT.mkdir(parents=True,exist_ok=True)
config=OUT/'puppeteer.json';config.write_text(json.dumps({'executablePath':'/Applications/Google Chrome.app/Contents/MacOS/Google Chrome'}))
pat=re.compile(r'^```mermaid[^\n]*\n(.*?)^```\s*$',re.M|re.S);items=[]
for p in sorted((ROOT/'docs/architecture').rglob('*.md')):
 rel=str(p.relative_to(ROOT));oldrel=rel
 old=subprocess.check_output(['git','show','HEAD:'+oldrel],text=True)
 previous={m.group(1).rstrip() for m in pat.finditer(old)}
 for n,m in enumerate(pat.finditer(p.read_text()),1):
  body=m.group(1).rstrip()
  if body in previous:continue
  stem=rel.removeprefix('docs/architecture/').removesuffix('.md').replace('/','-')+f'-{n}'
  source=OUT/(stem+'.mmd');source.write_text(body+'\n');out=source.with_suffix('.png')
  r=subprocess.run(['mmdc','-i',str(source),'-o',str(out),'-p',str(config),'-w','2400','-q'],capture_output=True,text=True,timeout=60)
  items.append({'source':rel,'number':n,'sha256':hashlib.sha256(body.encode()).hexdigest(),'render':str(out.relative_to(ROOT)),'passed':r.returncode==0,'log':r.stderr})
  (OUT/'manifest.json').write_text(json.dumps(items,ensure_ascii=False,indent=2)+'\n')
  print(f'{len(items)} {stem}: {r.returncode}',flush=True)
  if r.returncode:raise RuntimeError(r.stderr)
print(f'Rendered {len(items)} changed Mermaid diagrams.',flush=True)
