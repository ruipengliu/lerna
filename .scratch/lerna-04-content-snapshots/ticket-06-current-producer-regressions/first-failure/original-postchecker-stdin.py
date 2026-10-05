from pathlib import Path
import sys,json,hashlib,os,stat
sha=lambda b:hashlib.sha256(b).hexdigest()
idx=int(sys.argv[1]); ex=Path('/tmp/lerna-04-ticket06-execution'); wt=Path('/tmp/lerna-worktrees/content-snapshots-06')
d=json.loads(Path('/tmp/lerna-06-current-producer-regression-ready/commands.json').read_bytes()); op=d['operations'][idx]
facts=json.loads(Path('/tmp/lerna-06-current-producer-regression-ready/current-owned-source-facts.json').read_bytes())
raw=Path(op['raw']).read_bytes(); prebytes=Path(op['prelaunch']).read_bytes(); outbytes=Path(op['outcome']).read_bytes(); pre=json.loads(prebytes); out=json.loads(outbytes)
print(raw.decode(),end='')
assert out['exit']==0 and out['group_absent'] is True and out['timed_out'] is False and out['owned_raw_fsync_close_ack'] is True
assert pre['argv']==op['full_invocation_argv'][3:] and pre['operation']==out['operation']==op['operation'] and pre['pid']==out['pid']==pre['pgid']==out['pgid'] and pre['starttime']==out['starttime']
try:
 os.killpg(out['pgid'],0)
except ProcessLookupError: absent=True
else: absent=False
assert absent
assert b'DATA RACE' not in raw and b'FAIL' not in raw
parent=op['parent']; text=raw.decode()
assert '=== RUN   '+parent+'\n' in text and '--- PASS: '+parent+' (' in text and '\nok\t' in text
if op['phase']=='paired_race':
 original=json.loads(Path('/tmp/lerna-04-ticket06-late-put-static/source-commands.json').read_bytes())['affected_existing_exact_commands'][idx//3]
 selected=[original[k][-1].split('/^')[1].removesuffix('$') for k in ['normal_control','fault_only']]
else: selected=[op['native_argv'][-1].split('/^')[1].removesuffix('$')]
for case in selected:
 assert '=== RUN   '+parent+'/'+case+'\n' in text and '--- PASS: '+parent+'/'+case+' (' in text,case
source=[]
for e in facts['current26']:
 b=(wt/e['path']).read_bytes(); assert len(b)==e['bytes'] and sha(b)==e['sha256'] and b==(ex/'late-put-source-formatted'/e['path']).read_bytes()
 source.append(e)
assert sha((ex/'run-native.py').read_bytes())==facts['runner_sha256']
s=ex.lstat(); assert (s.st_dev,s.st_ino,stat.S_IMODE(s.st_mode))==(33,407243,0o700)
for e in facts['protected_exact_scopes_current_lstat']:
 s=Path(e['path']).lstat(); assert (s.st_dev,s.st_ino,stat.S_IMODE(s.st_mode))==(e['device'],e['inode'],0o700)
ledger=(ex/'owned-scopes.log').read_bytes().splitlines(keepends=True); original=b''.join(ledger[:193]); assert sha(original)==facts['owned_ledger']['sha256']
start=end=None
for i,b in enumerate(ledger):
 if not b.startswith(b'{'): continue
 e=json.loads(b)
 if e.get('operation')!=op['operation']: continue
 if e['event']=='native_start_ack': assert start is None; start=i
 if e['event']=='native_completion_ack': assert end is None; end=i
assert start is not None and end is not None and start<end and end==len(ledger)-1
part=b''.join(ledger[start:end+1]); scopes=[]; byschema={}; current=None
for b in ledger[start+1:end]:
 line=b.decode().strip(); fields=line.split()
 if fields[0]=='postgres':
  current={'schema':fields[1],'children':[]}; scopes.append(current); byschema[fields[1]]=current
 elif fields[0]=='objects':
  assert current is not None
  current.update(path=fields[1],device=int(fields[2]),inode=int(fields[3]))
 elif fields[0]=='content_child_holder':
  scope=byschema[fields[1]]; kv=dict(x.split('=',1) for x in fields[2:])
  scope['children'].append({'generation':int(kv['generation']),'pre_start_owner_registered':True,'first_store_close':'UNKNOWN','first_object_close':'UNKNOWN','close_observation':'NOT_YET_OBSERVED'})
 elif fields[0]=='content_child_native':
  scope=byschema[fields[1]]; kv=dict(x.split('=',1) for x in fields[2:]); child=next(c for c in scope['children'] if c['generation']==int(kv['generation']))
  child.update(pid=int(kv['pid']),pgid=int(kv['pgid']),starttime=kv['start'],pre_open_registered_before_permission=True)
  assert child['pgid']==out['pgid']
 elif fields[0]=='content_child_close':
  scope=byschema[fields[1]]; kv=dict(x.split('=',1) for x in fields[2:]); child=next(c for c in scope['children'] if c['generation']==int(kv['generation']))
  assert child['close_observation']=='NOT_YET_OBSERVED'
  child.update(first_store_close=kv['store'],first_object_close=kv['objects'],close_observation='ACTUAL_STICKY_ORIGINAL_OBSERVATION',retained=kv.get('retained')=='true')
assert len(scopes)==len(selected)
newunknown=[]
for scope in scopes:
 assert scope['children'] and scope.get('path')
 unknown=any(c['first_store_close']!='ACK' or c['first_object_close']!='ACK' for c in scope['children'])
 scope['historical_firstClose_unknown']=unknown
 scope['actual_child_Wait_and_parent_Stop_assertions']='ORIGINAL_SELECTED_TEST_FULL_PASS_AT_FROZEN_SOURCE; ledger records actual firstClose separately'
 try: s=Path(scope['path']).lstat()
 except FileNotFoundError: scope['current_root']='ABSENT_AFTER_ORDINARY_FIXTURE_CLEANUP'; assert not unknown
 else:
  scope.update(current_root='EXACT_ORIGINAL_ROOT_RETAINED',current_device=s.st_dev,current_inode=s.st_ino,current_mode=format(stat.S_IMODE(s.st_mode),'04o'))
  assert (s.st_dev,s.st_ino,stat.S_IMODE(s.st_mode))==(scope['device'],scope['inode'],0o700)
 if unknown:
  assert any(c.get('retained') and c['first_store_close']=='UNKNOWN' and c['first_object_close']=='UNKNOWN' for c in scope['children'])
  scope['cleanup']='PROHIBITED_STICKY_NATIVE_FIRST_CLOSE_UNKNOWN'; newunknown.append(scope)
 else: scope['ordinary_fixture_cleanup']='NO_REPORTED_ERROR_AND_ROOT_ABSENT; PG_CATALOG_NOT_YET_INDEPENDENTLY_AUDITED'
assert len(newunknown)==int(op['expected_actual_SIGKILL_one_oldgeneration'])
record={'status':'ACTUAL_ORIGINAL_CURRENT_PRODUCER_COMMAND_PASS_WAIT_GROUPABS_SOURCE26_UNCHANGED','sequence':op['sequence'],'operation':op['operation'],'selected_parent':parent,'selected_cases':selected,'native_actual':out,'prelaunch_sha256':sha(prebytes),'outcome_sha256':sha(outbytes),'raw_bytes':len(raw),'raw_lines':len(raw.splitlines()),'raw_sha256':sha(raw),'independent_current_group_absent':absent,'source26':source,'original193ledger_prefix_unchanged':True,'ledger_start':start+1,'ledger_end':end+1,'ledger_bytes':len(part),'ledger_sha256':sha(part),'scopes':scopes,'new_exact_unknown_scopes':newunknown,'old8_exact_inventory_and_lstat_unchanged':True,'no_source_fmt_retry_other_tests':True,'public_full_original_tails':'ALL_ORIGINAL_SELECTED_TEST_ASSERTIONS_EXECUTED_FULL_PASS; NO_OLDER_GREEN_SUBSTITUTED','pg_catalog_current':'NOT_OBSERVED_THIS_COMMAND'}
partpath=ex/(op['operation']+'-ledger-'+str(start+1)+'-through-'+str(end+1)+'.log')
for path,body in [(partpath,part),(Path(op['postchecks']),(json.dumps(record,indent=2)+'\n').encode())]:
 with path.open('xb') as file: file.write(body); file.flush(); os.fsync(file.fileno())
fd=os.open(ex,os.O_RDONLY|os.O_DIRECTORY); os.fsync(fd); os.close(fd)
post=Path(op['postchecks']).read_bytes()
print('POSTCHECKS',json.dumps({'path':op['postchecks'],'bytes':len(post),'sha256':sha(post),'source26':True,'actual_wait_and_current_absence':True,'ledger':[start+1,end+1],'new_unknown':len(newunknown),'scopes':scopes},sort_keys=True))
