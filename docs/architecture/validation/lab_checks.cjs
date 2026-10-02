const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');
const path = require('node:path');
const file = path.join(__dirname, '../assets/design-lab.html');
const html = fs.readFileSync(file, 'utf8');
const context = {};
vm.runInNewContext(html.match(/<script id="architecture-model">([\s\S]*?)<\/script>/)[1], context);
const { traceState, capacity } = context.ArchitectureLabModel;
const defaults = { taskRate:290,calls:4,duration:8,commits:20,users:100000,devices:2,connections:1,jobRate:1000,throughput:1800,lostZones:1,backlog:36000 };
assert.deepEqual(JSON.parse(JSON.stringify(capacity(defaults))), {models:9280,commits:5800,wss:200000,remaining:1200,net:200,drain:180});
for (const mode of ['safe','unsafe']) {
  for(let step=0; step<=5; step++) {
    const s=traceState(step,mode),second=mode==='unsafe' && step>=3;
    assert.equal(s.writes,(step>=1?1:0)+(second?1:0));
    assert.equal(s.task,step>=4?'cancelled':'active');
    assert.equal(s.op1.id,'op1'); assert.equal(s.op1.command,'c1');
    assert.equal(s.op1.effect,step===5?'applied':step>=1?'unknown':'not_started');
    assert.equal(s.spent+s.reserved,second?20:10);
    assert.equal(s.usageFinal,false);assert.equal(Boolean(s.op2),second);
  }
  assert.equal(traceState(5,mode).writes,traceState(4,mode).writes,'No write after cancel');
  assert.equal(traceState(5,mode).op1.mayApplyLater,'false');
}
const extrema={taskRate:[0,1000],calls:[0,20],duration:[0,60],commits:[0,100],users:[0,1000000],devices:[0,10],connections:[0,10],jobRate:[0,5000],throughput:[0,10000],backlog:[0,360000]};
let checks=0;const entries=Object.entries(extrema);
for(let mask=0;mask<2**entries.length;mask++) for(let lostZones=0;lostZones<=3;lostZones++){
 const p={lostZones};entries.forEach(([k,v],i)=>p[k]=v[(mask>>i)&1]);const c=capacity(p);
 for(const k of ['models','commits','wss','remaining']) assert.ok(Number.isFinite(c[k])&&c[k]>=0);
 assert.ok(c.remaining<=p.throughput);assert.ok(Number.isFinite(c.net));
 if(c.net<=0)assert.equal(c.drain,null);else assert.ok(Number.isFinite(c.drain)&&c.drain>=0);
 checks++;
}
assert.equal(capacity({...defaults,jobRate:1200}).drain,null,'Equal rates have no finite drain');
assert.equal(capacity({...defaults,jobRate:1250}).drain,null,'Overload has no finite drain');
assert.equal(capacity({...defaults,backlog:0}).drain,0,'Zero initial backlog and positive net => 0');
assert.equal(capacity({...defaults,throughput:0,lostZones:3,jobRate:0,backlog:0}).drain,null,'Zero/zero still N/A under explicit gate');
console.log('PASS pure model: 12 trace states, default arithmetic, equality/overload/zero, '+checks+' extrema combinations');


console.log('Scope: arithmetic/state functions only; no DOM, browser rendering or click assertions.');
