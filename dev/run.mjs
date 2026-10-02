import { spawn } from 'node:child_process';
import { randomBytes } from 'node:crypto';
import { createWriteStream } from 'node:fs';
import { access, chmod, mkdir, readFile, unlink, writeFile } from 'node:fs/promises';
import net from 'node:net';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const state = path.join(root, 'dev/.state');
const envPath = path.join(root, 'dev/.env');
const controlPath = path.join(state, 'supervisor.sock');
const topology = JSON.parse(await readFile(path.join(root, 'dev/topology.json'), 'utf8'));

async function initialize() {
  try {
    await writeFile(envPath, `LERNA_PG_PORT=5432\nLERNA_PG_ADMIN_PASSWORD=${randomBytes(24).toString('hex')}\nLERNA_PG_APP_PASSWORD=${randomBytes(24).toString('hex')}\n`, { flag: 'wx', mode: 0o600 });
  } catch (error) {
    if (error.code !== 'EEXIST') throw error;
  }
  await chmod(envPath, 0o600);
  for (const directory of ['logs', 'multiprocess/content', 'multiprocess/managed-files', 'single/content', 'single/managed-files', 'single/database']) {
    await mkdir(path.join(state, directory), { recursive: true, mode: 0o700 });
  }
  console.log('Local credentials and initial data directories prepared. Existing credentials were preserved.');
}

async function environment() {
  const env = { ...process.env };
  let text;
  try { text = await readFile(envPath, 'utf8'); }
  catch { throw new Error('Local configuration is missing. Run make setup first.'); }
  for (const line of text.split('\n')) {
    if (line.trim() === '' || line.startsWith('#')) continue;
    const match = /^(LERNA_[A-Z0-9_]+)=(.*)$/.exec(line);
    if (!match) throw new Error('Invalid dev/.env entry; use literal KEY=value lines.');
    env[match[1]] = match[2];
  }
  const port = Number(env.LERNA_PG_PORT);
  if (!Number.isInteger(port) || port < 1 || port > 65535 || !env.LERNA_PG_ADMIN_PASSWORD || !env.LERNA_PG_APP_PASSWORD) {
    throw new Error('Local database configuration is incomplete.');
  }
  env.LERNA_DATABASE_URL = `postgres://lerna_app:${encodeURIComponent(env.LERNA_PG_APP_PASSWORD)}@127.0.0.1:${port}/lerna?sslmode=disable`;
  return env;
}

function run(command, args, env) {
  return new Promise((resolve, reject) => {
    const child = spawn(command, args, { cwd: root, env, stdio: 'inherit' });
    child.once('error', reject);
    child.once('exit', (code, signal) => code === 0 ? resolve() : reject(new Error(`${command} failed (${signal ?? code})`)));
  });
}

const composeArgs = ['compose', '--project-name', 'lerna-dev', '--env-file', envPath, '--file', path.join(root, 'dev/compose.yaml')];

async function dependenciesUp(env) {
  await run('docker', [...composeArgs, 'up', '-d', '--wait', '--wait-timeout', '90'], env);
  await run('docker', [...composeArgs, 'exec', '-T', 'postgres', 'sh', '-c', 'PGPASSWORD="$LERNA_PG_APP_PASSWORD" psql -h 127.0.0.1 -U lerna_app -d lerna -v ON_ERROR_STOP=1 -c "SELECT current_user, 1 AS connected;"'], env);
}

async function assertPortAvailable(url) {
  const parsed = new URL(url);
  await new Promise((resolve, reject) => {
    const server = net.createServer();
    server.once('error', () => reject(new Error(`Port ${parsed.port} is already in use; no local processes were started.`)));
    server.listen(Number(parsed.port), parsed.hostname, () => server.close(resolve));
  });
}

function requestStop() {
  return new Promise((resolve, reject) => {
    const socket = net.createConnection(controlPath);
    socket.setTimeout(10_000, () => socket.destroy(new Error('Local supervisor did not respond.')));
    socket.once('error', reject);
    socket.once('connect', () => socket.end('stop\n'));
    socket.on('data', chunk => process.stdout.write(chunk));
    socket.once('close', hadError => { if (!hadError) resolve(); });
  });
}

async function supervise(mode, env) {
  const hosts = topology[mode];
  const urls = [...hosts.map(host => host.admin_url), topology.web_url];
  for (const url of urls) await assertPortAvailable(url);
  for (const host of hosts) {
    const config = JSON.parse(await readFile(path.join(root, host.config), 'utf8'));
    const configRoot = path.resolve(path.dirname(path.join(root, host.config)), config.root);
    for (const key of ['content_root', 'managed_root']) await access(path.resolve(configRoot, config[key]));
  }
  if (mode === 'multiprocess') await dependenciesUp(env);
  await mkdir(path.join(root, 'build'), { recursive: true });
  await run('go', ['build', '-o', 'build/harnessd', './cmd/harnessd'], env);
  const children = [];
  const logs = [];
  let stopping = false;
  let failed = false;
  let finish;
  const complete = new Promise(resolve => { finish = resolve; });
  const control = net.createServer({ allowHalfOpen: true }, socket => {
    socket.setTimeout(1000, () => socket.destroy());
    socket.once('data', async data => {
      if (data.toString() !== 'stop\n') { socket.destroy(); return; }
      socket.setTimeout(10_000, () => socket.destroy());
      await stop();
      socket.end('Local process group stopped. PostgreSQL remains running.\n');
    });
  });

  const stop = async () => {
    if (stopping) return;
    stopping = true;
    control.close();
    console.log('Stopping local processes; PostgreSQL and its named volume remain available.');
    for (const child of children) {
      if (!child.pid) continue;
      try { process.kill(-child.pid, 'SIGTERM'); } catch (error) { if (error.code !== 'ESRCH') console.error(error.message); }
    }
    const deadline = setTimeout(() => {
      for (const child of children) { if (child.pid) { try { process.kill(-child.pid, 'SIGKILL'); } catch {} } }
    }, 8000);
    await Promise.all(children.map(child => !child.pid || child.exitCode !== null || child.signalCode !== null ? undefined : new Promise(resolve => child.once('exit', resolve))));
    clearTimeout(deadline);
    await Promise.all(logs.map(log => new Promise(resolve => log.end(resolve))));
    finish();
  };
  // Only the current user's initialized state directory exposes this control socket.
  try { await requestExistingSupervisor(); }
  catch (error) {
    if (!['ENOENT', 'ECONNREFUSED'].includes(error.code)) throw error;
    if (error.code === 'ECONNREFUSED') await unlink(controlPath);
  }
  await new Promise((resolve, reject) => { control.once('error', reject); control.listen(controlPath, resolve); });
  await chmod(controlPath, 0o600);
  process.once('SIGINT', stop);
  process.once('SIGTERM', stop);

  function start(name, command, args) {
    const log = createWriteStream(path.join(state, 'logs', `${name}.log`), { flags: 'a', mode: 0o600 });
    logs.push(log);
    const child = spawn(command, args, { cwd: root, env: { ...env, LERNA_DEV_MODE: mode }, detached: true, stdio: ['ignore', 'pipe', 'pipe'] });
    children.push(child);
    for (const stream of [child.stdout, child.stderr]) stream.on('data', chunk => {
      log.write(chunk);
      if (!(stopping && name === 'web')) process.stdout.write(`[${name}] ${chunk}`);
    });
    child.once('error', error => { console.error(`${name}: ${error.message}`); failed = true; void stop(); });
    child.once('exit', (code, signal) => {
      if (!stopping) { console.error(`${name} exited (${signal ?? code}); stopping this process group.`); failed = true; void stop(); }
    });
  }

  try {
    for (const host of hosts) start(host.name, path.join(root, 'build/harnessd'), ['--config', host.config]);
    start('web', 'pnpm', ['--filter', '@lerna/web', 'dev']);
    const deadline = Date.now() + 20_000;
    let available = false;
    while (!stopping && Date.now() < deadline) {
      available = (await Promise.all(urls.map(async url => {
        try {
          const endpoint = url === topology.web_url ? url : `${url}/health/startup`;
          return (await fetch(endpoint, { signal: AbortSignal.timeout(1500) })).ok;
        } catch { return false; }
      }))).every(Boolean);
      if (available) break;
      await new Promise(resolve => setTimeout(resolve, 250));
    }
    if (!available && !stopping) { failed = true; console.error('Local startup timed out; inspect dev/.state/logs.'); await stop(); }
    if (available && !stopping) console.log(`Local ${mode} skeleton started: ${hosts.length} hosts + Vite at ${topology.web_url}. Business readiness remains closed.`);
    await complete;
  } finally {
    await stop();
    process.removeListener('SIGINT', stop);
    process.removeListener('SIGTERM', stop);
  }
  if (failed) process.exitCode = 1;
}

function requestExistingSupervisor() {
  return new Promise((resolve, reject) => {
    const socket = net.createConnection(controlPath);
    socket.setTimeout(1000, () => socket.destroy(new Error('Existing local supervisor is not responding.')));
    socket.once('error', reject);
    socket.once('connect', () => { socket.destroy(); reject(new Error('A local supervisor is already running.')); });
  });
}

async function main() {
  const action = process.argv[2];
  if (action === 'init') return initialize();
  if (action === 'stop') return requestStop();
  const env = await environment();
  if (action === 'deps-up') return dependenciesUp(env);
  if (action === 'deps-down') return run('docker', [...composeArgs, 'down'], env);
  if (action === 'up' || action === 'single') return supervise(action === 'up' ? 'multiprocess' : 'single', env);
  throw new Error('Usage: node dev/run.mjs init|up|single|stop|deps-up|deps-down');
}

try { await main(); }
catch (error) { console.error(error.message); process.exitCode = 1; }
