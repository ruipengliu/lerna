import os,sys,subprocess,time,signal,json
root=os.path.dirname(__file__)
ack=json.load(open(root+'/root-ack.json')); st=os.stat(root)
assert (st.st_dev,st.st_ino)==(ack['device'],ack['inode'])
registry=root+'/owned-scopes.log'
os.environ['TMPDIR']=root
os.environ['LERNA_TEST_OWNED_SCOPE_REGISTRY']=registry
os.environ['LERNA_TEST_POSTGRES_DSN']=open('/tmp/lerna-work-01-dsn').read().strip()
r,w=os.pipe()
p=subprocess.Popen(['bash','-c','read -r gate <&'+str(r)+'; exec "$@"','owned-native',*sys.argv[1:]],pass_fds=(r,),start_new_session=True)
os.close(r);pgid=os.getpgid(p.pid);assert pgid==p.pid
with open(registry,'a') as f:f.write(f'process_group {p.pid} {pgid} {root}\n');f.flush();os.fsync(f.fileno())
os.write(w,b'ack\n');os.close(w)
try: status=p.wait(timeout=120)
except subprocess.TimeoutExpired:
 os.killpg(pgid,signal.SIGKILL);status=p.wait(timeout=5)
try:os.killpg(pgid,0); absent=False
except ProcessLookupError:absent=True
with open(registry,'a') as f:f.write(f'native_exit {p.pid} {pgid} status={status} group_absent={absent}\n');f.flush();os.fsync(f.fileno())
print(f'NATIVE_EXIT status={status} group_absent={absent}',flush=True)
sys.exit(status if absent else 125)
