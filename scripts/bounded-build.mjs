// Finite conformance builds/generation own one process group. Deadline handling
// kills the group while inherited pipes are still open, then confirms exit.
import { spawn } from 'node:child_process';
export async function boundedBuild(command, args, options) {
  const { timeout, signal, encoding, onStart, ...spawnOptions } = options ?? {};
  if (!Number.isInteger(timeout) || timeout < 1 || timeout > 60000)
    throw RangeError('build deadline must be 1..60000 ms');
  const child = spawn(command, args, { ...spawnOptions, detached: true });
  const output = { stdout: [], stderr: [] };
  const sizes = { stdout: 0, stderr: 0 };
  const result = {
    pid: child.pid,
    status: null,
    signal: null,
    error: undefined,
    cleanupErrors: [],
  };
  let directExited = false,
    pipesClosed = false,
    cleanupUntil,
    stopped = false;
  const live = () => {
    if (!child.pid) return false;
    try {
      process.kill(-child.pid, 0);
      return true;
    } catch (error) {
      if (error.code === 'ESRCH') return false;
      throw error;
    }
  };
  const stop = (error) => {
    if (error) result.error ??= error;
    cleanupUntil ??= Date.now() + 2000;
    if (stopped || !child.pid) return;
    stopped = true;
    try {
      process.kill(-child.pid, 'SIGKILL');
    } catch (error) {
      if (error.code !== 'ESRCH') result.cleanupErrors.push(error);
    }
  };
  for (const name of ['stdout', 'stderr'])
    child[name]?.on('data', (chunk) => {
      sizes[name] += chunk.length;
      if (sizes[name] > 1024 * 1024)
        stop(Error('conformance command output exceeds 1 MiB'));
      else output[name].push(chunk);
    });
  child.on('error', (error) => {
    result.error ??= error;
    stop();
  });
  child.on('exit', (code, sig) => {
    directExited = true;
    result.status = code;
    result.signal = sig;
    // The direct tool is finished; any remaining group member is an orphan.
    stop();
  });
  child.on('close', () => {
    pipesClosed = true;
  });
  const expired = () => {
    const error = Error(`conformance command exceeded ${timeout} ms`);
    error.code = 'ETIMEDOUT';
    stop(error);
  };
  const abort = () => {
    const error = Error('conformance command cancelled');
    error.code = 'ABORT_ERR';
    stop(error);
  };
  if (onStart) {
    try {
      onStart(child.pid);
    } catch (error) {
      stop(error);
    }
  }
  signal?.addEventListener('abort', abort, { once: true });
  if (signal?.aborted) abort();
  const deadline = setTimeout(expired, timeout);
  try {
    return await new Promise((resolve) => {
      const poll = () => {
        let absent = false;
        try {
          absent = !live();
        } catch (error) {
          result.cleanupErrors.push(error);
          stop();
        }
        const exitConfirmed =
          (!child.pid || directExited) && pipesClosed && absent;
        if (exitConfirmed || (cleanupUntil && Date.now() >= cleanupUntil)) {
          child.stdin?.destroy();
          child.stdout?.destroy();
          child.stderr?.destroy();
          const data = (name) => {
            const bytes = Buffer.concat(output[name]);
            return encoding ? bytes.toString(encoding) : bytes;
          };
          resolve({
            ...result,
            stdout: data('stdout'),
            stderr: data('stderr'),
            exitConfirmed,
          });
        } else setTimeout(poll, 10);
      };
      poll();
    });
  } finally {
    clearTimeout(deadline);
    signal?.removeEventListener('abort', abort);
  }
}
export async function requireBuild(command, args, options) {
  const result = await boundedBuild(command, args, options);
  const errors = [...result.cleanupErrors];
  if (result.error) errors.push(result.error);
  if (result.status !== 0)
    errors.push(
      Error(
        `command failed: ${command} status=${result.status} signal=${result.signal}`,
      ),
    );
  if (!result.exitConfirmed)
    errors.push(Error('build descendants exit unconfirmed'));
  if (errors.length) {
    const error = new AggregateError(
      errors,
      'bounded conformance command failed',
    );
    error.exitConfirmed = result.exitConfirmed;
    throw error;
  }
}
