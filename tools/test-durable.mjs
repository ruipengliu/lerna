import { spawnSync } from 'node:child_process';
import { mkdir } from 'node:fs/promises';
import { join } from 'node:path';
import { localDatabaseEnv, root } from './durable-env.mjs';

try {
  const env = await localDatabaseEnv();
  await mkdir(join(root, 'dev/.state'), { recursive: true });
  env.LERNA_DURABLE_EVIDENCE_FILE = join(root, 'dev/.state/durable-cost.jsonl');
  if (!process.env.LERNA_TEST_ADMIN_URL) {
    const deps = spawnSync(process.execPath, ['dev/run.mjs', 'deps-up'], { cwd: root, stdio: 'inherit' });
    if (deps.status !== 0) process.exit(deps.status ?? 1);
  }
  const result = spawnSync('go', ['test', '-race', '-count=1', ...process.argv.slice(2), './tests/integration'], { cwd: root, env, stdio: 'inherit' });
  process.exit(result.status ?? 1);
} catch {
  console.error('Durable tests could not initialize explicit local database configuration; run make setup and check Docker.');
  process.exit(1);
}
