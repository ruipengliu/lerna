"""Check final sources against the actual render and review evidence."""
import hashlib
from html.parser import HTMLParser
import json
from pathlib import Path
import re
import subprocess
import xml.etree.ElementTree as ET

ROOT = Path(__file__).resolve().parents[2]
OUT = ROOT / '.scratch/terminology-cleanup'
def digest(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()
def read(name):
    return json.loads((OUT / name).read_text())
def write(name, value):
    (OUT / name).write_text(json.dumps(value, ensure_ascii=False, indent=2) + '\n')
def previous(path):
    return subprocess.check_output(['git', 'show', 'HEAD:' + path], cwd=ROOT)

mermaid = read('render/manifest.json')
reviews = read('mermaid-visual-review-a.json')['diagrams'] + read('mermaid-visual-review-b.json')['diagrams']
review_index = {(r['source'], r['diagram_number']): r for r in reviews}
for item in mermaid['changed_diagrams']:
    source = (ROOT / item['source']).read_text()
    blocks = re.findall(r'^```mermaid[^\n]*\n(.*?)^```\s*$', source, re.M | re.S)
    body = blocks[item['diagram_number'] - 1].rstrip() + '\n'
    assert hashlib.sha256(body.encode()).hexdigest() == item['source_sha256']
    assert body == (ROOT / item['mermaid']).read_text()
    review = review_index[item['source'], item['diagram_number']]
    assert review['source_sha256'] == item['source_sha256']
    assert review['visual_review'] == item['render_status'] == 'passed'
    item['visual_review'] = 'passed'
    item['render_sha256'] = digest(ROOT / item['render'])
write('render/manifest.json', mermaid)

native = read('drawio-manifest.json')
reviews = {(r['source'], r['page_index']): r for r in read('native-visual-review.json')['pages']}
checks = {}
expected_changed = set()
for path in {r['source'] for r in native}:
    old, new = ET.fromstring(previous(path)), ET.fromstring((ROOT / path).read_text())
    for i, (a, b) in enumerate(zip(old.findall('diagram'), new.findall('diagram'))):
        if ET.tostring(a) != ET.tostring(b):
            expected_changed.add((path, i))
    def structure(tree):
        for cell in tree.iter():
            for attr in ['value', 'label', 'tooltip']:
                cell.attrib.pop(attr, None)
        return ET.tostring(tree)
    assert structure(old) == structure(new), path
    checks[path] = {'pages': len(new.findall('diagram')), 'non_text_structure': 'unchanged', 'source_sha256': digest(ROOT/path)}
assert expected_changed == {(r['source'], r['page_index']) for r in native}
root_review = {'reviewer':'root', 'method':'view_image of actual PNG and browser screenshots', 'native_pages':[], 'other_artifacts':[]}
for item in native:
    key = item['source'], item['page_index']
    if key in reviews:
        review = reviews[key]
        assert review['visual_review'] == 'passed'
        assert review['render_sha256'] == digest(ROOT / item['render'])
    else:
        assert key == ('docs/architecture/diagrams/uml-models.drawio', 19)
        root_review['native_pages'].append({**item, 'visual_review':'passed', 'render_status':'passed', 'render_sha256':digest(ROOT/item['render']), 'notes':'引用安装清单标签完整，连线、多重性与字段保持；root 已实际看图。'})
    item.update(render_status='passed', visual_review='passed', render_sha256=digest(ROOT/item['render']), source_sha256=digest(ROOT/item['source']))
write('drawio-manifest.json',native)

class Structure(HTMLParser):
    def __init__(self):
        super().__init__()
        self.tags=[]
    def handle_starttag(self,tag,attrs):
        self.tags.append((tag,[(k,v) for k,v in attrs if k not in ['aria-label','title']]))
    def handle_endtag(self,tag):
        self.tags.append(('/'+tag,[]))
path='docs/architecture/diagrams/architecture-atlas.html'
a,b=Structure(),Structure()
a.feed(previous(path).decode());b.feed((ROOT/path).read_text())
assert a.tags==b.tags
checks[path]={'non_text_structure':'unchanged','source_sha256':digest(ROOT/path)}
for path in ['docs/architecture/contracts/schemas/protocol.schema.json','docs/architecture/contracts/schemas/methods.json']:
    def normalized(value):
        if isinstance(value,dict):
            return {k:normalized(v) for k,v in value.items() if k not in ['description','success']}
        if isinstance(value,list):
            return [normalized(v) for v in value]
        return 'no_idempotency_guarantee' if value=='non_repeatable' else value
    assert normalized(json.loads(previous(path))) == normalized(json.loads((ROOT/path).read_text()))
    checks[path]={'machine_structure':'unchanged except effect_class enum rename; descriptions excluded', 'source_sha256':digest(ROOT/path)}
write('structure-checks.json',checks)

for name in ['panorama','view-03','view-07','view-09','view-17','view-19','view-18-expanded','view-20-expanded']:
    path=ROOT/'output/playwright'/('terminology-atlas-'+name+'.png')
    root_review['other_artifacts'].append({'render':str(path.relative_to(ROOT)),'render_sha256':digest(path),'visual_review':'passed','notes':'实际浏览器截图，文字及关系表完整可见；使用本机回退字体。'})
path=ROOT/'docs/architecture/diagrams/design-concepts.png'
root_review['other_artifacts'].append({'render':str(path.relative_to(ROOT)),'render_sha256':digest(path),'visual_review':'passed','notes':'已核对更新后的概念文字、八项原则和模块关系；保留原风格与布局。'})
write('root-visual-review.json',root_review)
print(f"PASS: {len(mermaid['changed_diagrams'])} Mermaid source hashes/reviews; {len(native)} native pages; schema and diagram structure checks.")
