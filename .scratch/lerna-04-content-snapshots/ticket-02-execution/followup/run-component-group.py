# Task-local launcher: fail before exec on missing/invalid/incomplete discovery.
import sys,os,json,pathlib,re,hashlib
root=pathlib.Path(__file__).parent
ack=json.loads((root/'root-ack.json').read_text());st=root.stat();assert (st.st_dev,st.st_ino)==(ack['device'],ack['inode'])
assert len(sys.argv) in (3,4)
mode,group=sys.argv[1:3];assert mode in ('race','normal')
manifest=pathlib.Path(sys.argv[3]) if len(sys.argv)==4 else root/'materialized-runtime-manifest.json'
assert manifest.parent==root
record=json.loads(manifest.read_text());names=record['names'];assert names and len(names)==len(set(names))==record['denominator']
assert all(re.fullmatch(r'(?:Test|Example|Fuzz)\S*',name) for name in names)
all_names=sum((g['names'] for g in record['groups'].values()),[])
assert len(all_names)==len(set(all_names))==len(names) and sorted(all_names)==sorted(names)
g=record['groups'][group];selected=g['names'];assert selected and len(selected)==g['count']
selector=g['selector'];assert selector and selector.startswith('^(') and selector.endswith(')$')
special=set('.[](){}*+?^$|'+chr(92));expected='^('+'|'.join(''.join(chr(92)+c if c in special else c for c in name) for name in selected)+')$'
assert selector==expected and [name for name in names if re.fullmatch(selector,name)]==selected
binary=pathlib.Path(record['binary']['binary']);assert binary.parent==root
assert hashlib.sha256(binary.read_bytes()).hexdigest()==record['binary']['sha256']
print('ACTUAL_GROUP',mode,group,len(selected),flush=True)
if mode=='race':
 os.chdir('/tmp/lerna-worktrees/content-snapshots-02/conformance/component')
 os.execv(str(binary),[str(binary),'-test.timeout=120s','-test.count=1','-test.run='+selector,'-test.v'])
os.execvp('go',['go','test','-p','1','-count','1','-tags','integration','-timeout','120s','./conformance/component','-run='+selector,'-v'])
