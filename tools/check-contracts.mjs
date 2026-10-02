import { spawnSync } from 'node:child_process';
import { existsSync, mkdirSync } from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const localPython = path.join(root, 'dev/.state/python/bin/python');
const python = existsSync(localPython) ? localPython : 'python3';
const output = path.join(root, 'dev/.state/generated/proto');

function run(command, args) {
  const result = spawnSync(command, args, { cwd: root, stdio: 'inherit' });
  if (result.error) throw result.error;
  if (result.status !== 0) throw new Error(`Contract check failed: ${args.join(' ')} (exit ${result.status})`);
}

try {
  run(python, ['-c', 'import jsonschema, grpc_tools']);
  for (const name of ['check_documents', 'validate', 'validate_protocol', 'validate_input_answers', 'validate_transport', 'validate_brain', 'validate_lease', 'validate_release_recovery']) {
    run(python, [`docs/architecture/validation/${name}.py`]);
  }
  run('node', ['docs/architecture/validation/verify_transport_proofs.mjs']);
  mkdirSync(output, { recursive: true });
  run(python, ['-m', 'grpc_tools.protoc', '-I', 'contracts', `--descriptor_set_out=${output}/harness.pb`, `--python_out=${output}`, 'contracts/harness.proto']);
  run(python, ['docs/architecture/validation/check_grpc_envelope.py', output]);
  run(python, ['tools/check-asset-locations.py']);
} catch (error) {
  console.error(error.message);
  console.error('Install the locked validation environment with make setup.');
  process.exitCode = 1;
}
