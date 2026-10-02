"""Check current contract ownership and diagram references, without replaying historical renderers."""
import json
import xml.etree.ElementTree as ET
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
ARCHITECTURE = ROOT / 'docs/architecture'

for name in ['schemas', 'examples', 'harness.proto']:
    assert (ROOT / 'contracts' / name).exists(), f'missing root contract asset: {name}'
    assert not (ARCHITECTURE / 'contracts' / name).exists(), f'duplicate contract authority: {name}'

source_map = json.loads((ARCHITECTURE / 'source-map.json').read_text())
def check_targets(value):
    if isinstance(value, dict):
        for key, item in value.items():
            if key == 'formal_target':
                assert (ROOT / item).exists(), f'missing current source-map target: {item}'
            else:
                check_targets(item)
    elif isinstance(value, list):
        for item in value:
            check_targets(item)
check_targets(source_map)

count = 0
tree = ET.parse(ARCHITECTURE / 'diagrams/uml-models.drawio')
for element in tree.iter():
    source = element.get('sourceDoc')
    if source:
        filename = source.split('#', 1)[0]
        assert (ARCHITECTURE / filename).exists(), f'missing diagram source: {source}'
        count += 1
print(f'PASS: single contract source, current source-map targets and {count} diagram sources')
