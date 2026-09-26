from pathlib import Path
import concurrent.futures, hashlib, json, re, subprocess

root=Path('docs/architecture')
out=Path('.scratch/architecture-detail/layout-runtime');out.mkdir(exist_ok=True)
browser=Path('/Applications/Google Chrome.app/Contents/MacOS/Google Chrome')
if not browser.exists():raise SystemExit('Rendering needs an installed headless browser')
config=out/'puppeteer.json'
config.write_text(json.dumps({'executablePath':str(browser)}))
jobs=[]
for path in [root/'task-runtime/implementation.md',root/'collaboration/implementation.md']:
    for index,match in enumerate(re.finditer(r'^```mermaid\n(.*?)^```',path.read_text(),re.M|re.S),1):
        if index != 1: continue
        code=match.group(1)
        digest=hashlib.sha256(code.encode()).hexdigest()
        name=str(path.relative_to(root)).replace('/','-').replace('.md','')+f'-{index}'
        source=out/(name+'.mmd');image=out/(name+'.png');stamp=out/(name+'.sha256')
        jobs.append({'source':str(path),'index':index,'digest':digest,'image':str(image),'name':name})
        if not image.exists() or not stamp.exists() or stamp.read_text()!=digest:
            source.write_text(code)
def render(item):
    name=item['name'];image=Path(item['image']);stamp=out/(name+'.sha256')
    if image.exists() and stamp.exists() and stamp.read_text()==item['digest']:return None
    result=subprocess.run(['mmdc','-p',str(config),'-i',str(out/(name+'.mmd')),'-o',str(image),'-b','white','-w','1600','-s','1.5'],capture_output=True,text=True)
    if result.returncode:return name+': '+result.stderr[-1800:]
    stamp.write_text(item['digest']);return None
with concurrent.futures.ThreadPoolExecutor(max_workers=3) as pool:
    errors=[error for error in pool.map(render,jobs) if error]
(out/'manifest.json').write_text(json.dumps(jobs,ensure_ascii=False,indent=2)+'\n')
for error in errors:print(error,flush=True)
print(f'{len(jobs)} diagrams; {len(errors)} render errors',flush=True)
raise SystemExit(bool(errors))
