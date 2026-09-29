import hashlib
import json
from pathlib import Path
import subprocess
import xml.etree.ElementTree as ET

ROOT = Path(__file__).resolve().parents[2]
OUT = ROOT / '.scratch/terminology-cleanup/native-render'
OUT.mkdir(parents=True, exist_ok=True)
manifest_path = ROOT / '.scratch/terminology-cleanup/drawio-manifest.json'
manifest = json.loads(manifest_path.read_text())
sources = sorted({x['source'] for x in manifest})
checks = []
for source in sources:
    current = ET.fromstring((ROOT / source).read_text())
    previous = ET.fromstring(subprocess.check_output(['git', 'show', 'HEAD:' + source], cwd=ROOT))
    def structural(tree):
        return [(d.attrib, [(c.tag, {k:v for k,v in c.attrib.items() if k not in ('value', 'label', 'tooltip')}, [ET.tostring(g).decode() for g in c.findall('mxGeometry')]) for c in d.findall('.//mxCell')]) for d in tree.findall('diagram')]
    assert structural(current) == structural(previous), source + ': topology or geometry changed'
    checks.append({'source': source, 'pages': len(current.findall('diagram')), 'topology_and_geometry': 'unchanged', 'sha256': hashlib.sha256((ROOT/source).read_bytes()).hexdigest()})
for item in manifest:
    number = item['page_index'] + 1  # draw.io CLI uses 1-based page indexes.
    output = OUT / (Path(item['source']).stem + '-' + str(number).zfill(2) + '.png')
    result = subprocess.run(['/Applications/draw.io.app/Contents/MacOS/draw.io', '-x', '-f', 'png', '-p', str(number), '--width', '2000', '-b', '24', '-o', str(output), str(ROOT / item['source'])], capture_output=True, text=True)
    item.update(render=str(output.relative_to(ROOT)), render_status='passed' if result.returncode == 0 and output.exists() else 'failed', render_log=result.stdout.strip() + result.stderr.strip(), visual_review='pending')
    print(item['render_status'], item['page_name'], flush=True)
    manifest_path.write_text(json.dumps(manifest, ensure_ascii=False, indent=2) + '\n')
(OUT / 'structure-check.json').write_text(json.dumps(checks, ensure_ascii=False, indent=2) + '\n')
assert all(x['render_status']=='passed' for x in manifest)
