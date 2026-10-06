import pathlib,json,hashlib,os,difflib,subprocess
w=pathlib.Path('/tmp/lerna-worktrees/content-snapshots-06');r=pathlib.Path('/tmp/lerna-04-ticket06-execution');s=pathlib.Path('/tmp/lerna-06-setup-cause-remaining-mechanics-static');sha=lambda b:hashlib.sha256(b).hexdigest();rel='conformance/component/content_process_setup_test.go'
oldman=json.loads((s/'candidate-unformatted27-manifest.json').read_text());rows=[]
for x in oldman['paths']:
 b=(w/x['path']).read_bytes();v=dict(x)
 if x['path']!=rel:assert sha(b)==x['sha256'] and b==pathlib.Path(x['snapshot']).read_bytes()
 else:
  old=pathlib.Path(x['snapshot']).read_bytes();assert b''.join(old.split())==b''.join(b.split())
  orig=(r/'setup-cause-first-green-source-formatted'/rel).read_bytes();needle=b'// This injected reader tests mechanical diagnostic/first-close ownership only.'
  assert orig[orig.index(needle):]==b[b.index(needle):b.index(b'// This literal is independent')].rstrip()+b'\n'
  p=r/'setup-cause-mechanics-source-formatted'/rel;p.parent.mkdir(parents=True,exist_ok=True)
  with p.open('xb') as f:f.write(b);f.flush();os.fsync(f.fileno())
  v.update(bytes=len(b),sha256=sha(b),snapshot=str(p));diff=''.join(difflib.unified_diff(old.decode().splitlines(True),b.decode().splitlines(True),fromfile='before/'+rel,tofile='after/'+rel));p=r/'setup-cause-mechanics-format-only.diff'
  with p.open('x') as f:f.write(diff);f.flush();os.fsync(f.fileno())
 rows.append(v)
p=r/'setup-cause-mechanics-formatted27-manifest.json'
with p.open('x') as f:json.dump({'status':'ACTUAL_FMT_ONLY_LAYOUT_BEFORE_MECHANICS_N','paths':rows,'consumer_a4_other25_unchanged':True,'original_firsttracer_fullbody_unchanged':True},f,indent=2);f.write('\n');f.flush();os.fsync(f.fileno())
outcome=json.loads((r/'setup-cause-mechanics-format-outcome.json').read_text());assert outcome['exit']==0 and outcome['group_absent'] and not outcome['timed_out'] and outcome['owned_raw_fsync_close_ack']
members=[]
for p in pathlib.Path('/proc').iterdir():
 if not p.name.isdigit():continue
 try:
  q=(p/'stat').read_text();f=q[q.rfind(')')+2:].split()
  if int(f[2])==outcome['pgid']:members.append(int(p.name))
 except (OSError,ValueError):pass
assert not members
fd=os.open(r,os.O_RDONLY|os.O_DIRECTORY);os.fsync(fd);os.close(fd)
print('fmt actual0 Wait currentgroupempty; layout-only, firsttracer/a4/other25 unchanged; formatted27',sha((r/'setup-cause-mechanics-formatted27-manifest.json').read_bytes()),flush=True)
op=json.loads((s/'source-commands.json').read_text())['operations'][1]
raise SystemExit(subprocess.run(op['full_invocation_argv'],cwd=w).returncode)
