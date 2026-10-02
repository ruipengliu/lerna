import { spawnSync } from 'node:child_process';
import path from 'node:path';
import { localDatabaseEnv, root } from './durable-env.mjs';

try {
  const driver = process.argv[2];
  if (!['postgres', 'sqlite'].includes(driver) || process.argv.length !== 3) throw new Error('invalid driver');
  const env = driver === 'postgres' ? await localDatabaseEnv() : process.env;
  const args = driver === 'postgres'
    ? ['--driver', 'postgres', '--url-env', 'LERNA_MIGRATION_DATABASE_URL']
    : ['--driver', 'sqlite', '--path', path.join(root, 'dev/.state/single/database/harness.db')];
  const result = spawnSync('go', ['run', './cmd/harness-migrate', ...args], { cwd: root, env, stdio: 'inherit' });
  process.exit(result.status ?? 1);
} catch {
  console.error('Migration requires explicit local configuration and the original database directory.');
  process.exit(1);
}
