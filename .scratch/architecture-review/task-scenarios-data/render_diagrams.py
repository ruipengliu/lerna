#!/usr/bin/env python3
"""Render only this review's Mermaid diagrams; no application runtime test."""
import hashlib
import json
from pathlib import Path
import re
import subprocess

root=Path(__file__).resolve().parent
out=root/'render'
out.mkdir(exist_ok=True)
cfg=out/'puppeteer.json'
cfg.write_text(json.dumps({'executablePath':'/Applications/Google Chrome.app/Contents/MacOS/Google Chrome'}))
text=(root.parent/'task-scenarios-input-output-2026-09-28.md').read_text()
results=[]
for i,body in enumerate(re.findall(r'```mermaid\n(.*?)\n```',text,re.S),1):
    src=out/f'scenario-{i}.mmd';png=out/f'scenario-{i}.png'
    src.write_text(body+'\n')
    p=subprocess.run(['mmdc','-i',str(src),'-o',str(png),'-p',str(cfg),'-w','2000','-q'],capture_output=True,text=True)
    results.append({'figure':i,'status':'passed' if p.returncode==0 else 'failed','source_sha256':hashlib.sha256(src.read_bytes()).hexdigest(),'log':p.stderr or p.stdout,'path':str(png.relative_to(root))})
(root/'render-results.json').write_text(json.dumps(results,ensure_ascii=False,indent=2)+'\n')
print(json.dumps(results,ensure_ascii=False,indent=2))
assert len(results)==2 and all(r['status']=='passed' for r in results)
