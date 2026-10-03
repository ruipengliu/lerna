// Private conformance process lifecycle, not a product transport or SDK API.
import { spawn } from 'node:child_process';
const maxFrameBytes = 8 * 1024 * 1024;
const maxStderrBytes = 64 * 1024;
function exactKeys(value, keys) {
  return (
    value !== null &&
    typeof value === 'object' &&
    !Array.isArray(value) &&
    Object.keys(value).sort().join() === [...keys].sort().join()
  );
}
function bytes(text) {
  if (typeof text !== 'string' || text.length % 4 !== 0)
    throw Error('invalid base64');
  const data = Buffer.from(text, 'base64');
  if (data.toString('base64') !== text) throw Error('noncanonical base64');
  return data;
}
export function startRunner(command, args, { timeoutMs = 10000, signal } = {}) {
  if (!Number.isInteger(timeoutMs) || timeoutMs < 1 || timeoutMs > 10000)
    throw Error('runner deadline must be 1..10000 ms');
  const child = spawn(command, args, { stdio: ['pipe', 'pipe', 'pipe'] });
  let pending,
    sequence = 0,
    stdout = Buffer.alloc(0),
    stderr = Buffer.alloc(0);
  let fatal,
    closing = false,
    finished = false,
    closedPromise;
  const exited = new Promise((resolve) => {
    child.once('close', (code, signal) => {
      finished = true;
      if (!closing || code !== 0 || pending || stdout.length)
        fail(Error(`runner exited code=${code} signal=${signal}`));
      resolve();
    });
  });
  function fail(error) {
    fatal ??= error;
    if (pending) {
      const current = pending;
      pending = undefined;
      clearTimeout(current.timer);
      current.reject(
        Error(
          `${current.context}: ${fatal.message}; stderr=${stderr.toString('utf8')}`,
        ),
      );
    }
    if (!finished) child.kill('SIGKILL');
  }
  child.on('error', fail);
  child.stdin.on('error', fail);
  child.stdout.on('error', fail);
  child.stderr.on('error', fail);
  child.stderr.on('data', (chunk) => {
    if (stderr.length + chunk.length > maxStderrBytes) {
      stderr = Buffer.concat([stderr, chunk]).subarray(0, maxStderrBytes);
      fail(Error('stderr exceeds 64 KiB'));
    } else stderr = Buffer.concat([stderr, chunk]);
  });
  child.stdout.on('data', (chunk) => {
    if (stdout.length + chunk.length > maxFrameBytes + 1) {
      fail(Error('response frame exceeds 8 MiB'));
      return;
    }
    stdout = Buffer.concat([stdout, chunk]);
    const end = stdout.indexOf(10);
    if (end < 0) return;
    const line = stdout.subarray(0, end);
    stdout = stdout.subarray(end + 1);
    try {
      if (line.length > maxFrameBytes || stdout.length)
        throw Error('extra or oversized stdout frame');
      if (!pending) throw Error('unsolicited stdout frame');
      const value = JSON.parse(
        new TextDecoder('utf-8', { fatal: true, ignoreBOM: true }).decode(line),
      );
      if (value?.id !== pending.id) throw Error('response id mismatch');
      let response;
      if (value.ok === true && exactKeys(value, ['id', 'ok', 'wire_base64']))
        response = { ok: true, wire: bytes(value.wire_base64) };
      else if (
        value.ok === false &&
        exactKeys(value, ['id', 'ok', 'error']) &&
        exactKeys(value.error, ['code']) &&
        typeof value.error.code === 'string' &&
        value.error.code.length > 0 &&
        value.error.code.length <= 128
      )
        response = { ok: false, error: value.error };
      else throw Error('invalid response shape');
      const current = pending;
      pending = undefined;
      clearTimeout(current.timer);
      current.resolve(response);
    } catch (error) {
      fail(error);
    }
  });
  const abort = () => fail(Error('runner cancelled'));
  signal?.addEventListener('abort', abort, { once: true });
  if (signal?.aborted) abort();
  async function waitForExit(milliseconds) {
    let timer;
    try {
      return await Promise.race([
        exited.then(() => true),
        new Promise((resolve) => {
          timer = setTimeout(() => resolve(false), milliseconds);
        }),
      ]);
    } finally {
      clearTimeout(timer);
    }
  }
  return {
    get pid() {
      return child.pid;
    },
    run(schema, wire, context = schema) {
      if (fatal) return Promise.reject(Error(`${context}: ${fatal.message}`));
      if (closing || finished)
        return Promise.reject(Error(`${context}: runner closed`));
      if (pending)
        return Promise.reject(
          Error(`${context}: another request is in flight`),
        );
      const id = String(++sequence);
      if (
        typeof schema !== 'string' ||
        schema.length < 1 ||
        schema.length > 128 ||
        !(wire instanceof Uint8Array) ||
        wire.byteLength > Math.floor((maxFrameBytes * 3) / 4)
      ) {
        fail(
          Error(
            `${context} schema=${schema} id=${id}: invalid or oversized request frame`,
          ),
        );
        return Promise.reject(fatal);
      }
      const frame = Buffer.from(
        JSON.stringify({
          id,
          schema,
          wire_base64: Buffer.from(wire).toString('base64'),
        }),
      );
      if (frame.length > maxFrameBytes) {
        fail(
          Error(
            `${context} schema=${schema} id=${id}: request frame exceeds 8 MiB`,
          ),
        );
        return Promise.reject(fatal);
      }
      return new Promise((resolve, reject) => {
        const timer = setTimeout(
          () => fail(Error(`runner deadline ${timeoutMs} ms exceeded`)),
          timeoutMs,
        );
        pending = {
          id,
          context: `${context} schema=${schema} id=${id}`,
          resolve,
          reject,
          timer,
        };
        child.stdin.write(
          Buffer.concat([frame, Buffer.from('\n')]),
          (error) => {
            if (error) fail(error);
          },
        );
      });
    },
    close() {
      if (closedPromise) return closedPromise;
      closing = true;
      closedPromise = (async () => {
        try {
          if (pending) fail(Error('runner closed with a request in flight'));
          child.stdin.end();
          if (!(await waitForExit(500))) {
            fail(Error('runner did not exit after stdin closed'));
            if (!(await waitForExit(1500)))
              throw Error('runner did not exit after SIGKILL');
          }
          if (fatal) throw fatal;
        } finally {
          signal?.removeEventListener('abort', abort);
          child.stdin.destroy();
          child.stdout.destroy();
          child.stderr.destroy();
        }
      })();
      return closedPromise;
    },
  };
}
