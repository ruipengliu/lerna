#!/usr/bin/env python3
"""Render Mermaid bodies changed from the original source inventory."""
from concurrent.futures import ThreadPoolExecutor
import hashlib
import json
from pathlib import Path
import re
import subprocess

ROOT = Path(__file__).resolve().parents[2]
BASE = ROOT / 'docs/architecture'
OUT = ROOT / '.scratch/architecture-formalization/diagrams'
FENCE = re.compile(r'^```mermaid[^\n]*\n(.*?)^```\s*$', re.M | re.S)
OUT.mkdir(parents=True, exist_ok=True)
prior_path = OUT / 'manifest.json'
prior = json.loads(prior_path.read_text()) if prior_path.exists() else {}
cache = {(item['source'], item['diagram'], item['sha256']): item for item in prior.get('changed_diagrams', [])}
known = {m.group(1).strip() for p in (BASE / '.draft').rglob('*.md') for m in FENCE.finditer(p.read_text())}
for rel in ('docs/architecture/README.md', 'docs/architecture/ochestrator/README.md', 'docs/architecture/ochestrator/task-lifecycle.md', 'docs/architecture/ochestrator/durable-work.md'):
    text = subprocess.check_output(['git', 'show', 'HEAD:' + rel], cwd=ROOT, text=True)
    known.update(m.group(1).strip() for m in FENCE.finditer(text))
config = OUT / 'puppeteer.json'
config.write_text(json.dumps({'executablePath': '/Applications/Google Chrome.app/Contents/MacOS/Google Chrome'}))
items = []
unchanged = 0
for p in sorted(BASE.rglob('*.md')):
    if '.draft' in p.relative_to(BASE).parts:
        continue
    for index, match in enumerate(FENCE.finditer(p.read_text()), 1):
        body = match.group(1).strip() + '\n'
        if body.strip() in known:
            unchanged += 1
            continue
        stem = p.relative_to(BASE).with_suffix('').as_posix().replace('/', '-') + f'-{index}'
        source = OUT / (stem + '.mmd')
        source.write_text(body)
        items.append({'source': str(p.relative_to(ROOT)), 'diagram': index, 'sha256': hashlib.sha256(body.encode()).hexdigest(), 'mermaid': str(source.relative_to(ROOT)), 'render': str(source.with_suffix('.png').relative_to(ROOT))})
def render(item):
    previous = cache.get((item['source'], item['diagram'], item['sha256']))
    if previous and previous.get('render_status') == 'passed' and (ROOT / previous['render']).exists():
        item['render_status'] = 'passed'
        item['log'] = 'Reused matching current-source render.'
        item['visual_review'] = previous.get('visual_review', 'pending')
        return item
    result = subprocess.run(['mmdc', '-i', str(ROOT / item['mermaid']), '-o', str(ROOT / item['render']), '-p', str(config), '-w', '1800', '-q'], cwd=ROOT, capture_output=True, text=True)
    item['render_status'] = 'passed' if result.returncode == 0 else 'failed'
    item['log'] = result.stderr.strip() or result.stdout.strip()
    return item
with ThreadPoolExecutor(max_workers=3) as pool:
    items = list(pool.map(render, items))
(OUT / 'manifest.json').write_text(json.dumps({'changed_diagrams': items, 'unchanged_bodies_skipped': unchanged, 'scope': 'Rendering verifies Mermaid syntax for changed bodies; semantic and visual review are separate.'}, ensure_ascii=False, indent=2) + '\n')
for item in items:
    print(item['render_status'], item['source'], item['diagram'])
print(f'Rendered {len(items)} changed Mermaid blocks; {unchanged} unchanged bodies retained.')
raise SystemExit(any(item['render_status'] != 'passed' for item in items))
