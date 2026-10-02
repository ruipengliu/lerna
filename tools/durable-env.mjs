import { readFile } from 'node:fs/promises';
import { fileURLToPath } from 'node:url';

export const root = fileURLToPath(new URL('../', import.meta.url));
export async function localDatabaseEnv() {
  if (process.env.LERNA_TEST_ADMIN_URL && process.env.LERNA_TEST_APP_URL) return process.env;
  const values = {};
  const source = await readFile(new URL('../dev/.env', import.meta.url), 'utf8');
  for (const line of source.split(/\r?\n/)) {
    const match = /^([A-Z][A-Z0-9_]*)=(.*)$/.exec(line);
    if (match) values[match[1]] = match[2];
  }
  if (!/^[1-9]\d{0,4}$/.test(values.LERNA_PG_PORT ?? '5432') || Number(values.LERNA_PG_PORT ?? 5432) > 65535) throw new Error('invalid local PG port');
  for (const key of ['LERNA_PG_ADMIN_PASSWORD', 'LERNA_PG_APP_PASSWORD']) if (!values[key]) throw new Error('run make setup first');
  const connection = (user, password, database) => `postgresql://${user}:${encodeURIComponent(password)}@127.0.0.1:${values.LERNA_PG_PORT ?? 5432}/${database}?sslmode=disable`;
  return { ...process.env,
    LERNA_TEST_ADMIN_URL: connection('postgres', values.LERNA_PG_ADMIN_PASSWORD, 'postgres'),
    LERNA_TEST_APP_URL: connection('lerna_app', values.LERNA_PG_APP_PASSWORD, 'lerna'),
    LERNA_MIGRATION_DATABASE_URL: connection('postgres', values.LERNA_PG_ADMIN_PASSWORD, 'lerna'),
  };
}
