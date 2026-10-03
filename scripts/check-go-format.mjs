import { execFileSync } from 'node:child_process';
import { lstatSync } from 'node:fs';
const files = execFileSync(
  'git',
  [
    'ls-files',
    '-z',
    '--cached',
    '--others',
    '--exclude-standard',
    '--',
    '*.go',
  ],
  { encoding: 'utf8', timeout: 10000 },
).split('\0');
if (files.at(-1) === '') files.pop();
const present = files.filter((file) => {
  try {
    lstatSync(file);
    return true;
  } catch (cause) {
    if (cause.code === 'ENOENT') return false;
    throw cause;
  }
});
if (present.length) {
  const unformatted = execFileSync(
    'gofmt',
    ['-l', ...present.map((file) => `./${file}`)],
    {
      encoding: 'utf8',
      timeout: 10000,
    },
  );
  if (unformatted) throw new Error(`Run make fmt: ${unformatted}`);
}
