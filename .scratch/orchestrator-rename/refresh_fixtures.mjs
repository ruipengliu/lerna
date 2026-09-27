import {readFileSync,writeFileSync,readdirSync} from 'node:fs';
import {execFileSync} from 'node:child_process';
import {createHash,generateKeyPairSync,sign} from 'node:crypto';
const root='docs/architecture/contracts/examples';
function files(dir){return readdirSync(dir,{withFileTypes:true}).flatMap(x=>x.isDirectory()?files(dir+'/'+x.name):x.name.endsWith('.json')?[dir+'/'+x.name]:[])}
function canonical(v){if(v===null||typeof v!=='object')return JSON.stringify(v);if(Array.isArray(v))return '['+v.map(canonical).join(',')+']';return '{'+Object.keys(v).sort().map(k=>JSON.stringify(k)+':'+canonical(v[k])).join(',')+'}'}
const digest=v=>'sha256:'+createHash('sha256').update(canonical(v)).digest('hex');
function rename(s){return s.replaceAll('Task Home','Orchestrator').replaceAll('Task Runtime','Orchestrator').replaceAll('TaskRuntime','Orchestrator').replaceAll('task-runtime','orchestrator').replaceAll('任务运行时','任务编排器').replaceAll('Runtime Store','Orchestrator Store').replaceAll('Home','Orchestrator').replaceAll('home','orchestrator')}
const rows=files(root).map(path=>{const oldPath=path.replace('23-cross-orchestrator-budget','23-cross-home-budget');return {path,old:JSON.parse(execFileSync('git',['show','HEAD:'+oldPath],{encoding:'utf8'})),text:readFileSync(path,'utf8')}});
const known=new Set(rows.flatMap(row=>row.text.match(/sha256:[a-f0-9]{64}/g)||[]));
const replacements=new Map();
function mapDigest(old){const a=digest(old);if(!known.has(a))return;const next=JSON.parse(rename(JSON.stringify(old)));const b=digest(next);if(a!==b)replacements.set(a,b)}
function visit(v){if(!v||typeof v!=='object')return;mapDigest(v);if(v.owner_id&&v.consumer_command){const cmd=structuredClone(v.consumer_command);if(cmd.method==='grant.issue')delete cmd.payload.intent_hash;mapDigest({owner_id:v.owner_id,consumer_command:cmd})}Object.values(v).forEach(visit)}
rows.forEach(row=>visit(row.old));
let edited=0;
for(const row of rows){let text=row.text;for(const [a,b] of replacements)text=text.replaceAll(a,b);if(text!==row.text){writeFileSync(row.path,text);edited++}}
// Construct fresh public-only examples. The private key exists only in this process.
const proofPath=root+'/transport/proofs.json';const proofs=JSON.parse(readFileSync(proofPath,'utf8'));
const {privateKey,publicKey}=generateKeyPairSync('ec',{namedCurve:'prime256v1'});
for(const item of proofs){item.public_key=publicKey.export({format:'jwk'});const header={alg:'ES256',kid:item.expected_kid,typ:item.expected_type};const input=[header,item.expected_payload].map(x=>Buffer.from(canonical(x)).toString('base64url')).join('.');item.proof=input+'.'+sign('sha256',Buffer.from(input),{key:privateKey,dsaEncoding:'ieee-p1363'}).toString('base64url')}
writeFileSync(proofPath,JSON.stringify(proofs,null,2)+'\n');
writeFileSync('.scratch/orchestrator-rename/digest-renames.json',JSON.stringify(Object.fromEntries(replacements),null,2)+'\n');
console.log(`Updated ${replacements.size} dependent digests in ${edited} files; signed ${proofs.length} constructed vectors with a temporary key.`);
