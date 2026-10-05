#!/usr/bin/env python3
# One fixed read-only trace classifier; no subprocess, network, profile decoder, or source edits.
import sys,re,json,hashlib
from pathlib import Path
from collections import defaultdict
p=Path(sys.argv[1]); data=p.read_bytes()
if len(data)>16*1024*1024: raise SystemExit('raw exceeds 16MiB; preserve raw, STOP without partial totals')
text=data.decode('utf-8',errors='strict')
sep='-----------+-------------------------------------------------------'
D='github.com/ruipengliu/lerna/adapters/content/decision.'
C='github.com/ruipengliu/lerna/domain/task/context.'
CONTENT='github.com/ruipengliu/lerna/domain/content.(*Service).ReadForProcessing'
roots=[D+'(*Adapter).ReadSnapshot',D+'(*Adapter).ReadFixtureLock']
rows=[]; totals=defaultdict(lambda: {'cpu_ns':0,'profile_sample_rows':0})
def has(frames,name): return any(f.split(' ')[0]==name for f in frames)
def bucket(frames):
    if has(frames,CONTENT): return 'Content.ReadForProcessing-subtree-old-closure'
    if any('decisionfixture.(*ContextDispatcher).Binding' in f or 'decisionfixture.(*ContextDispatcher).Current' in f for f in frames): return 'Access-current-input-and-binding-subtree'
    if has(frames,C+'DecodeMandatory'):
        if has(frames,C+'ValidateInput'): return 'mandatory.EncodeMandatory.ValidateInput-subtree'
        if has(frames,C+'CanonicalInput'): return 'mandatory.EncodeMandatory.CanonicalInput-subtree'
        if has(frames,C+'writeCanonical') or has(frames,C+'writeCanonicalString'): return 'mandatory.EncodeMandatory.canonical-write-subtree'
        encoded=has(frames,C+'EncodeMandatory')
        if has(frames,'github.com/ruipengliu/lerna/contract/v1_1.ParseJSON'): return 'mandatory.EncodeMandatory.strict-output-parse-subtree' if encoded else 'mandatory.strict-input-parse-subtree'
        if encoded and has(frames,'encoding/json.Marshal'): return 'mandatory.EncodeMandatory.marshal-subtree'
        if encoded: return 'mandatory.EncodeMandatory.other-validation-or-rebuild'
        if has(frames,'encoding/json.(*Decoder).Decode'): return 'mandatory.typed-decode-subtree'
        return 'mandatory.other-decode-glue'
    if has(frames,D+'closed'):
        if has(frames,'github.com/ruipengliu/lerna/contract/v1_1.ParseJSON'): return 'closed.strict-parse-subtree'
        if has(frames,'encoding/json.(*Decoder).Decode'): return 'closed.typed-decode-subtree'
        return 'closed.other-glue'
    if has(frames,D+'(*Adapter).snapshot'):
        return 'snapshot.ToRule-subtree' if has(frames,D+'ToRule') else 'snapshot.other-projection'
    if has(frames,D+'ToRule'): return 'direct.ToRule-subtree'
    if has(frames,C+'CanonicalInput'): return 'direct.CanonicalInput-subtree'
    return 'Source-or-other-child-unresolved'
for block in text.split(sep)[1:]:
    lines=block.strip('\n').splitlines()
    if not lines or not any(re.match(r'^\s*(?:[0-9]+(?:\.0+)?ns|0)\s{3}',x) for x in lines):
        if any('ns   ' in x for x in lines): raise SystemExit('unsupported weight format; preserve raw, STOP')
        continue
    labels={}; frames=[]; weight=None
    for line in lines:
        m=re.match(r'^\s*([^:]+):  (.*)$',line)
        if m and not frames and weight is None:
            labels[m.group(1).strip()]=m.group(2); continue
        m=re.match(r'^\s*([0-9]+(?:\.0+)?ns|0)\s{3}(.*)$',line)
        if m:
            if weight is not None: raise SystemExit('multiple sample weights in one block')
            weight=int(m.group(1).removesuffix('ns').split('.')[0]); frames.append(m.group(2)); continue
        if weight is not None:
            if not line.startswith('             '): raise SystemExit('unsupported continuation format; preserve raw, STOP')
            frames.append(line.strip())
    if weight is None or not frames: raise SystemExit('missing sample weight/frames')
    if weight>2**53 or len(frames)>256 or len(rows)>=10000: raise SystemExit('bound exceeded; preserve raw, STOP without partial totals')
    found=[name for name in roots if has(frames,name)]
    if len(found)!=1: raise SystemExit('source focus membership ambiguous/missing; no duplicate allocation')
    if labels.get('phase')!='decision_step': raise SystemExit('unexpected phase label')
    case=labels.get('case','MISSING')
    b=bucket(frames); key=(case,found[0].split('.')[-1],b)
    totals[key]['cpu_ns']+=weight; totals[key]['profile_sample_rows']+=1
    rows.append({'cpu_ns':weight,'labels':labels,'source':found[0],'disjoint_bucket':b,'leaf_to_root_frames':frames})
result={'raw_sha256':hashlib.sha256(data).hexdigest(),'raw_bytes':len(data),'sample_rows':len(rows),'matched_cpu_ns':sum(r['cpu_ns'] for r in rows),'zero_match':not rows,'zero_match_means':'no retained matching sample, not zero actual CPU cost','totals':[dict(case=k[0],source=k[1],disjoint_bucket=k[2],**v) for k,v in sorted(totals.items())],'samples':rows,'limitations':['Each original profile sample row allocated once; integer ns weights retained; no cum parent/child addition.','Runtime/race frames under retained named ancestors follow that path; missing ancestors remain unresolved.','Unlabelled/outside-source/empty-stack samples excluded by pprof cannot be allocated; off-CPU unknown.','Old profile28e precedes current closure map; Content CPU is historical, not current removable cost.','No sample-count confidence claim; profile rows may represent merged timer ticks.','Original testing CPU descriptor logical Close UNKNOWN; parsing does not supply Close ACK.']}
print(json.dumps(result,ensure_ascii=False,indent=2))
