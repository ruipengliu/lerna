import { execFileSync } from 'node:child_process';
const files = execFileSync('rg', ['--files', '-g', '*.go'], {
  encoding: 'utf8',
})
  .trim()
  .split('\n');
const unformatted = execFileSync('gofmt', ['-l', ...files], {
  encoding: 'utf8',
}).trim();
if (unformatted) throw new Error(`Run make fmt: ${unformatted}`);
