import { execFileSync } from 'node:child_process';
import assert from 'node:assert/strict';
const run = (command, args) =>
  execFileSync(command, args, { encoding: 'utf8' }).trim();
assert.equal(
  run('go', ['env', 'GOVERSION']),
  'go1.27.1',
  'install locked Go 1.27.1',
);
assert.equal(process.version, 'v24.19.0', 'install locked Node 24.19.0');
assert.equal(
  run('pnpm', ['--version']),
  '12.8.1',
  'install locked pnpm 12.8.1',
);
execFileSync('go', ['mod', 'download'], { stdio: 'inherit' });
execFileSync('go', ['mod', 'verify'], { stdio: 'inherit' });
execFileSync('pnpm', ['install', '--frozen-lockfile'], { stdio: 'inherit' });
assert.equal(
  run('pnpm', ['exec', 'tsc', '--version']),
  'Version 7.0.2',
  'TypeScript must match lockfile',
);
console.log('Locked toolchains and dependencies verified.');
