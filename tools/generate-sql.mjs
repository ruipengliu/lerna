import { spawnSync } from 'node:child_process';
import { readFile, readdir } from 'node:fs/promises';
import path from 'node:path';
import { root } from './durable-env.mjs';

const directories = ['internal/storage/postgres/querygen', 'internal/storage/sqlite/querygen'];
async function snapshot() {
  const files = new Map();
  for (const directory of directories) for (const name of await readdir(path.join(root, directory))) {
    if (name.endsWith('.go')) files.set(path.join(directory, name), await readFile(path.join(root, directory, name)));
  }
  return files;
}
const before = await snapshot();
const result = spawnSync('go', ['run', 'github.com/sqlc-dev/sqlc/cmd/sqlc@v1.31.1', 'generate'], { cwd: root, stdio: 'inherit' });
if (result.status !== 0) process.exit(result.status ?? 1);
if (process.argv.includes('--check')) {
  const after = await snapshot();
  const changed = [...new Set([...before.keys(), ...after.keys()])].filter(file => !before.get(file)?.equals(after.get(file)));
  if (changed.length) { console.error(`SQL generation changed: ${changed.join(', ')}`); process.exit(1); }
  console.log('PASS: PostgreSQL/SQLite sqlc regeneration has no differences');
}
