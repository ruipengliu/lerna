"""Raw bounded checks; parent also runs the same checks through the unified runner."""
import pathlib,json,subprocess,hashlib,tempfile,concurrent.futures,time
p=pathlib.Path(__file__).resolve().parent
JAVA='/opt/homebrew/opt/openjdk/bin/java'
JAR='/Users/ruipengliu/.cache/lerna-formal-tools/tla-v1.7.4/tla2tools.jar'
LEAN='/Users/ruipengliu/.cache/lerna-formal-tools/lean-4.19.0-darwin_aarch64/bin/lean'
checks=json.loads((p/'checks.json').read_text())['checks']
def run(c):
    start=time.time()
    with tempfile.TemporaryDirectory(prefix='lerna-lifecycle-') as tmp:
        if c['kind']=='tlc': cmd=[JAVA,'-Djava.io.tmpdir='+tmp,'-XX:+UseParallelGC','-Xmx2g','-cp',JAR,'tlc2.TLC','-workers','1','-seed','1','-fp','0','-metadir',tmp,'-config',c['config'],c['model']]
        else: cmd=[LEAN,c['source']]
        try:
            r=subprocess.run(cmd,cwd=p,capture_output=True,text=True,timeout=180)
            out=r.stdout+r.stderr; code=r.returncode
        except subprocess.TimeoutExpired as e:
            out=(e.stdout or b'').decode()+(e.stderr or b'').decode();code=-999
        (p/'evidence'/(c['id']+'.log')).write_text(out)
        good=code==c['expected_exit'] and c['contains'] in out
        entry=c|{'actual_exit':code,'passed':good,'command':cmd,'seconds':round(time.time()-start,3),'log':'evidence/'+c['id']+'.log'}
        print(c['id'],code,good,flush=True)
        return entry
with concurrent.futures.ThreadPoolExecutor(max_workers=3) as pool: results=list(pool.map(run,checks))
files=sorted(p.glob('*.tla'))+sorted(p.glob('*.cfg'))+sorted(p.glob('*.lean'))
manifest={'checks':results,'source_hashes':{x.name:hashlib.sha256(x.read_bytes()).hexdigest() for x in files},'versions':{t:subprocess.run(cmd,capture_output=True,text=True).stdout for t,cmd in {'lean':[LEAN,'--version'],'tlc':[JAVA,'-cp',JAR,'tlc2.TLC','-help'],'java':[JAVA,'--version']}.items()}}
(p/'evidence'/'initial-run.json').write_text(json.dumps(manifest,ensure_ascii=False,indent=2)+'\n')
raise SystemExit(0 if all(x['passed'] for x in results) else 1)
