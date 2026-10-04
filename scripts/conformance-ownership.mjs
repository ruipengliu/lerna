// Private ownership protocol for the five existing conformance entry points.
// Native Wait, pipes and process-group drainage stay with build/runner owners.
import * as fs from 'node:fs';
import { dirname, isAbsolute, join } from 'node:path';

export function ownConformanceScope(dir, kind, native = fs) {
  if (!['contract', 'generator'].includes(kind) || !isAbsolute(dir))
    throw Error('exact absolute conformance scope and concrete kind required');
  const faults = [];
  const entities = new Map();
  let identity,
    registry,
    removed = false,
    finished = false;
  function remember(error) {
    faults.push(error);
    return error;
  }
  function useFD(path, flags, action) {
    let fd;
    const causes = [];
    try {
      fd = native.openSync(path, flags, 0o600);
      action(fd);
    } catch (error) {
      causes.push(error);
    }
    if (fd !== undefined) {
      try {
        native.closeSync(fd);
      } catch (error) {
        causes.push(error);
      }
    }
    if (causes.length)
      throw remember(
        new AggregateError(causes, `conformance FD/ACK unknown: ${path}`),
      );
  }
  function record(line) {
    useFD(registry, 'a', (fd) => {
      const bytes = Buffer.from(line + '\n');
      for (let offset = 0; offset < bytes.length; ) {
        const written = native.writeSync(
          fd,
          bytes,
          offset,
          bytes.length - offset,
        );
        if (written <= 0) throw Error('ownership ledger short write');
        offset += written;
      }
      native.fsyncSync(fd);
    });
    useFD(dirname(registry), 'r', native.fsyncSync);
  }
  try {
    identity = native.statSync(dir);
    registry =
      process.env.LERNA_TEST_OWNED_SCOPE_REGISTRY ??
      join(dir, 'owned-scopes.log');
    if (!isAbsolute(registry)) throw Error('absolute owned registry required');
    for (const path of [dir, dirname(dir)]) useFD(path, 'r', native.fsyncSync);
    record(`${kind} ${dir} ${identity.dev} ${identity.ino}`);
  } catch (error) {
    const failure = new AggregateError(
      [...new Set([error, ...faults])],
      `conformance initialization unknown; retained exact scope ${dir}`,
    );
    failure.scope = dir;
    throw failure;
  }
  function start(pid, role) {
    if (
      finished ||
      !['compiler', 'producer'].includes(role) ||
      !Number.isInteger(pid) ||
      pid < 1
    )
      throw remember(
        Error(`invalid original conformance entity: ${dir} ${role} ${pid}`),
      );
    const key = `${role}:${pid}`;
    if (entities.has(key))
      throw remember(Error('original entity already registered'));
    const entity = { pid, role, confirmed: false };
    entities.set(key, entity); // Retain binding even when its ACK fails.
    try {
      const stat = native.readFileSync(`/proc/${pid}/stat`, 'utf8');
      const group = Number(stat.slice(stat.lastIndexOf(')') + 2).split(' ')[2]);
      if (
        !Number.isInteger(group) ||
        group < 1 ||
        (role === 'compiler' && group !== pid)
      )
        throw Error(`original ${role} native group mismatch`);
      record(
        `${role === 'compiler' ? 'process_group' : 'producer'} ${pid} ${group} ${dir}`,
      );
    } catch (error) {
      throw remember(error);
    }
  }
  function exited(pid, role, confirmed) {
    if (pid === undefined) return; // Spawn failed before an original entity existed.
    const entity = entities.get(`${role}:${pid}`);
    if (finished || !entity) {
      remember(
        Error(`exit observation has no original entity: ${dir} ${role} ${pid}`),
      );
      return;
    }
    // A later real observation from that original owner may prove actual exit.
    // ACK/FD faults remain sticky and cannot be washed away by this observation.
    entity.confirmed = confirmed === true;
  }
  function finish(primary, cleanup = []) {
    const causes = [primary, ...cleanup].filter(Boolean);
    if (finished)
      throw new AggregateError(
        [...causes, Error('scope already finalized')],
        `conformance scope ${dir}`,
      );
    finished = true;
    for (const entity of entities.values()) {
      try {
        record(
          entity.role === 'producer'
            ? `producer_exit ${entity.pid} closed=${entity.confirmed}`
            : `process_exit ${entity.pid} confirmed=${entity.confirmed}`,
        );
      } catch (error) {
        causes.push(error);
      }
    }
    if (
      !faults.length &&
      [...entities.values()].every((entity) => entity.confirmed)
    ) {
      try {
        const current = native.statSync(dir);
        if (current.dev !== identity.dev || current.ino !== identity.ino)
          throw Error(`exact owned ${kind} identity changed: ${dir}`);
        native.rmSync(dir, { recursive: true });
        removed = true;
        if (registry !== join(dir, 'owned-scopes.log'))
          record(`${kind}_removed ${dir}`);
      } catch (error) {
        causes.push(error);
      }
    } else
      causes.push(
        Error(`native or ACK closure unconfirmed; retained exact scope ${dir}`),
      );
    causes.push(...faults);
    if (causes.length) {
      const error = new AggregateError(
        [...new Set(causes)],
        `conformance ${removed ? 'scope removed; final ACK may be unknown' : 'scope retained or cleanup failed'}: ${dir}`,
      );
      error.scope = dir;
      error.removed = removed;
      throw error;
    }
  }
  return { start, exited, finish };
}
