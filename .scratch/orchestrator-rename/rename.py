from pathlib import Path
import re,json,subprocess
root=Path.cwd()
paths=[Path('CONTEXT.md'),*Path('docs/architecture').rglob('*'),*Path('docs/adr').glob('*.md')]
changed=[]
for p in paths:
 if not p.is_file() or p.suffix not in {'.md','.json','.py','.mjs','.proto','.drawio'}:continue
 old=p.read_text();text=old
 for a,b in [('Task Home','Orchestrator'),('Task Runtime','Orchestrator'),('TaskRuntime','Orchestrator'),('task-runtime','orchestrator'),('任务运行时','任务编排器'),('Runtime Store','Orchestrator Store')]:text=text.replace(a,b)
 text=text.replace('Home','Orchestrator').replace('home','orchestrator')
 if text!=old:p.write_text(text);changed.append(str(p))
Path('docs/architecture/task-runtime').rename('docs/architecture/orchestrator')
Path('docs/architecture/contracts/examples/protocol/23-cross-home-budget.json').rename('docs/architecture/contracts/examples/protocol/23-cross-orchestrator-budget.json')
Path('.scratch/orchestrator-rename/changed-files.json').write_text(json.dumps(changed,ensure_ascii=False,indent=2)+'\n')
print(f'Updated {len(changed)} files and renamed module directory and cross-owner fixture.')
