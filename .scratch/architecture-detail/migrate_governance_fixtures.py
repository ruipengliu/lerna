"""Write migrated copies, never overwrite shared examples. Root reviews and copies them."""
import json,copy
from pathlib import Path
src=Path('docs/architecture/contracts/examples/protocol')
dst=Path('.scratch/architecture-detail/governance-migrated-fixtures');dst.mkdir(exist_ok=True)
for path in sorted(src.glob('[01][0-9]-*.json')):
 data=json.loads(path.read_text());uses={};changed=False
 for event in data['events']:
  if 'exchange' not in event:continue
  ex=event['exchange'];name=ex['request']['method'];out=ex['response'].get('output')
  if not out:continue
  if name=='evaluation.approval_check':uses[out['use_id']]=out
  if name=='extensions.read' and out.get('phase')=='active':
   use=uses[out['activation_use_id']]
   evidence={'kind':'remote_use',**copy.deepcopy(use),'checked_at':out['last_observed_at']}
   out['startup_evidence']=evidence
   out['instance_readiness']={'instance_id':out['ready_instance'],'ready':True,'observed_at':out['last_observed_at'],'startup_evidence':copy.deepcopy(evidence)}
   changed=True
 if changed:
  (dst/path.name).write_text(json.dumps(data,ensure_ascii=False,indent=2)+'\n');print(dst/path.name)
