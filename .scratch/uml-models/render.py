from pathlib import Path
import subprocess,json
D=Path('.scratch/uml-models'); logs=[]
for n in range(2,21):
 out=D/'render'/f'{n:02d}.png'
 p=subprocess.run(['/Applications/draw.io.app/Contents/MacOS/draw.io','-x','-f','png','-p',str(n),'--width','2000','-b','24','-o',str(out),'docs/architecture/diagrams/uml-models.drawio'],text=True,capture_output=True,timeout=60)
 assert p.returncode==0,(n,p.stdout,p.stderr)
 assert out.exists(),(n,'missing')
 logs.append(dict(page=n,stdout=p.stdout,stderr=p.stderr))
 print('rendered',n,flush=True)
p=subprocess.run(['/Applications/draw.io.app/Contents/MacOS/draw.io','-x','-f','xml','-u','-o',str(D/'roundtrip.drawio'),'docs/architecture/diagrams/uml-models.drawio'],text=True,capture_output=True,timeout=60)
assert p.returncode==0,p.stderr
(D/'render-log.json').write_text(json.dumps(logs,ensure_ascii=False,indent=2)+'\n')
