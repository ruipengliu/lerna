import test from 'node:test';
import assert from 'node:assert/strict';
import { spawn } from 'node:child_process';
import { mkdtempSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { boundedBuild, requireBuild } from './bounded-build.mjs';
test('a successful finite build confirms its owned process group exited', async () => {
  const result = await boundedBuild(
    process.execPath,
    ['-e', 'process.stdout.write("fixture-ready")'],
    { encoding: 'utf8', timeout: 2000 },
  );
  assert.equal(result.status, 0);
  assert.equal(result.stdout, 'fixture-ready');
  assert.equal(result.exitConfirmed, true);
  assert.deepEqual(result.cleanupErrors, []);
});
test('a timed command preserves failure while confirming finite child termination', async () => {
  const result = await boundedBuild(
    process.execPath,
    ['-e', 'setInterval(()=>{},1000)'],
    { timeout: 100 },
  );
  assert.equal(result.error.code, 'ETIMEDOUT');
  assert.equal(result.exitConfirmed, true);
  assert.throws(() => process.kill(result.pid, 0), { code: 'ESRCH' });
});
test('nonzero tool failure remains an aggregate cause and deadline is mandatory', async () => {
  await assert.rejects(
    () =>
      requireBuild(process.execPath, ['-e', 'process.exit(5)'], {
        timeout: 2000,
      }),
    (error) =>
      error instanceof AggregateError &&
      error.exitConfirmed === true &&
      error.errors.some((cause) => cause.message.includes('status=5')),
  );
  await assert.rejects(
    () => boundedBuild(process.execPath, [], {}),
    RangeError,
  );
});

test('deadline kills a descendant that holds inherited pipes before waiting for close', async () => {
  // An independent Linux subreaper/watchdog owns this test only. It reaps the
  // real descendant, including after its direct parent is killed, so group
  // absence is evidence rather than a parent-exit inference.
  const dir = mkdtempSync(join(tmpdir(), 'lerna-build-watchdog-'));
  const marker = join(dir, 'descendant');
  const runner = `import { boundedBuild } from ${JSON.stringify(new URL('./bounded-build.mjs', import.meta.url).href)};
    const result = await boundedBuild(process.execPath, ['-e', ${JSON.stringify("const {spawn}=require('node:child_process'); const fs=require('node:fs'); const child=spawn(process.execPath,['-e','setInterval(()=>{},1000)'],{stdio:['ignore','inherit','inherit']}); fs.writeFileSync(process.argv[1]+'.pending',JSON.stringify({desc:child.pid,group:process.pid})); fs.renameSync(process.argv[1]+'.pending',process.argv[1]); setInterval(()=>{},1000);")}, ${JSON.stringify(marker)}], {timeout: 1000, encoding:'utf8'});
    console.log(JSON.stringify({code:result.error?.code, confirmed:result.exitConfirmed, cleanup:result.cleanupErrors.length}));`;
  const supervisor = `import ctypes, os, subprocess, sys, time, json
if ctypes.CDLL(None).prctl(36, 1, 0, 0, 0) != 0: raise RuntimeError('subreaper unavailable')
child=subprocess.Popen([sys.argv[1], '--input-type=module', '-e', sys.argv[2]], stdout=subprocess.PIPE, stderr=subprocess.PIPE)
end=time.monotonic()+7
reaped=False
while child.poll() is None and time.monotonic()<end:
    if os.path.exists(sys.argv[3]):
        pid=json.load(open(sys.argv[3]))['desc']
        try:
            got,_=os.waitpid(pid, os.WNOHANG)
            if got == pid: reaped=True
        except ChildProcessError: pass
    time.sleep(.01)
if child.poll() is None:

    if os.path.exists(sys.argv[3]):
        group=json.load(open(sys.argv[3]))['group']
        try: os.killpg(group,9)
        except ProcessLookupError: pass
    child.kill(); child.wait(timeout=1); raise RuntimeError('independent build watchdog expired')
stdout,stderr=child.communicate(timeout=1)
if not reaped: raise RuntimeError('descendant exit was not independently reaped')
if child.returncode != 0: raise RuntimeError(stderr.decode())
sys.stdout.buffer.write(stdout)
`;
  const child = spawn(
    'python3',
    ['-c', supervisor, process.execPath, runner, marker],
    { detached: true },
  );
  const output = [];
  const errors = [];
  child.stdout.on('data', (bytes) => output.push(bytes));
  child.stderr.on('data', (bytes) => errors.push(bytes));
  let confirmed = false;
  try {
    const status = await new Promise((accept, reject) => {
      const timer = setTimeout(() => {
        process.kill(-child.pid, 'SIGKILL');
        reject(Error(`outer watchdog expired; retained ${dir}`));
      }, 9000);
      child.on('error', (error) => {
        clearTimeout(timer);
        reject(error);
      });
      child.on('close', (code) => {
        clearTimeout(timer);
        accept(code);
      });
    });
    assert.equal(status, 0, Buffer.concat(errors).toString());
    assert.deepEqual(JSON.parse(Buffer.concat(output).toString()), {
      code: 'ETIMEDOUT',
      confirmed: true,
      cleanup: 0,
    });
    confirmed = true;
  } finally {
    // A successful supervisor confirms both holders exited. A failed outer
    // watchdog deliberately retains its exact scope for investigation.
    if (confirmed) rmSync(dir, { recursive: true });
  }
});
