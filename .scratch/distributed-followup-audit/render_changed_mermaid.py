#!/usr/bin/env python3
"""Extract and render changed Mermaid blocks relative to the current HEAD."""
import hashlib
import json
from pathlib import Path
import re
import shutil
import subprocess
import sys

ROOT = Path(__file__).resolve().parents[2]
OUT = ROOT / '.scratch/distributed-followup-audit/render'
FENCE = re.compile(r'^```mermaid[^\n]*\n(.*?)^```\s*$', re.M | re.S)


def run(*args):
    return subprocess.check_output(args, cwd=ROOT, stderr=subprocess.DEVNULL)


def blocks(text):
    return [(m.group(1).rstrip() + '\n', text.count('\n', 0, m.start()) + 1)
            for m in FENCE.finditer(text)]


OUT.mkdir(parents=True, exist_ok=True)
paths = set(run('git', 'diff', '--name-only', 'HEAD', '-z').decode().split('\0'))
paths.update(run('git', 'ls-files', '--others', '--exclude-standard', '-z').decode().split('\0'))
manifest = []
unchanged = []
manifest_path = OUT / 'manifest.json'
prior_manifest = json.loads(manifest_path.read_text()) if manifest_path.exists() else {}
prior_index = {(item['source'], item['diagram_number'], item['source_sha256']): item
               for item in prior_manifest.get('changed_diagrams', [])}
for rel in sorted(p for p in paths if p.startswith('docs/') and p.endswith('.md')):
    path = ROOT / rel
    if not path.is_file():
        continue
    text = path.read_text()
    try:
        old_text = run('git', 'show', f'HEAD:{rel}').decode()
    except subprocess.CalledProcessError:
        old_text = ''
    old_blocks = {body for body, _ in blocks(old_text)}
    for index, (body, line) in enumerate(blocks(text), 1):
        identity = {'source': rel, 'diagram_number': index, 'fence_line': line}
        if body in old_blocks:
            unchanged.append(identity)
            continue
        stem = rel.removeprefix('docs/architecture/').removesuffix('.md').replace('/', '-')
        name = f'{stem}-diagram-{index}'
        mermaid = OUT / f'{name}.mmd'
        mermaid.write_text(body)
        item = {**identity,
                         'source_sha256': hashlib.sha256(body.encode()).hexdigest(),
                         'mermaid': str(mermaid.relative_to(ROOT)),
                         'render': str(mermaid.with_suffix('.png').relative_to(ROOT)),
                         'render_status': 'pending',
                         'visual_review': 'pending'}
        prior = prior_index.get((rel, index, item['source_sha256']))
        if prior and (ROOT / prior['render']).exists():
            item.update(prior)
            item['fence_line'] = line
        manifest.append(item)
config = OUT / 'puppeteer.json'
config.write_text(json.dumps({'executablePath': '/Applications/Google Chrome.app/Contents/MacOS/Google Chrome'}, indent=2) + '\n')
(OUT / 'manifest.json').write_text(json.dumps({'base_commit': run('git', 'rev-parse', 'HEAD').decode().strip(),
                                               'changed_diagrams': manifest,
                                               'unchanged_diagrams_skipped': unchanged}, ensure_ascii=False, indent=2) + '\n')
print(f'Extracted {len(manifest)} changed/new diagrams; skipped {len(unchanged)} unchanged diagrams.')
for item in manifest:
    print(f"{item['source']} figure {item['diagram_number']} (line {item['fence_line']})")
if '--render' in sys.argv:
    for item in manifest:
        if item['render_status'] == 'passed' and (ROOT / item['render']).exists():
            print(f"unchanged render: {item['source']} figure {item['diagram_number']}", flush=True)
            continue
        existing_stem = item['source'].removeprefix('docs/').removesuffix('.md').replace('/', '-')
        if existing_stem.startswith('architecture-memory-') or existing_stem.startswith('architecture-execution-'):
            existing_stem = existing_stem.removeprefix('architecture-')
        existing = ROOT / '.scratch/production-distributed/storage-render' / f"{existing_stem}-{item['diagram_number']}"
        if existing.with_suffix('.mmd').exists() and existing.with_suffix('.png').exists() and existing.with_suffix('.mmd').read_text().rstrip() == (ROOT / item['mermaid']).read_text().rstrip():
            shutil.copy2(existing.with_suffix('.png'), ROOT / item['render'])
            item['render_status'] = 'passed'
            item['render_log'] = 'Reused matching-source mmdc render from storage agent.'
            item['render_origin'] = str(existing.with_suffix('.png').relative_to(ROOT))
            print(f"reused: {item['source']} figure {item['diagram_number']}", flush=True)
            continue
        result = subprocess.run(['mmdc', '-i', str(ROOT / item['mermaid']), '-o', str(ROOT / item['render']), '-p', str(config), '-w', '2000', '-q'], cwd=ROOT, capture_output=True, text=True)
        item['render_status'] = 'passed' if result.returncode == 0 else 'failed'
        item['render_log'] = result.stderr.strip() or result.stdout.strip()
        print(f"{item['render_status']}: {item['source']} figure {item['diagram_number']}", flush=True)
        (OUT / 'manifest.json').write_text(json.dumps({'base_commit': run('git', 'rev-parse', 'HEAD').decode().strip(), 'changed_diagrams': manifest, 'unchanged_diagrams_skipped': unchanged}, ensure_ascii=False, indent=2) + '\n')

(OUT / 'manifest.json').write_text(json.dumps({'base_commit': run('git', 'rev-parse', 'HEAD').decode().strip(), 'changed_diagrams': manifest, 'unchanged_diagrams_skipped': unchanged}, ensure_ascii=False, indent=2) + '\n')
