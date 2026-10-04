// Interface fault controls are mechanical; actual FDs and child Wait still close.
import assert from 'node:assert/strict';
import test from 'node:test';
import * as fs from 'node:fs';
import { tmpdir } from 'node:os';
import { dirname, join } from 'node:path';
import { ownConformanceScope } from './conformance-ownership.mjs';
import { boundedBuild } from './bounded-build.mjs';

function includes(error, cause) {
  return (
    error === cause || error?.errors?.some((item) => includes(item, cause))
  );
}
function fixture(t) {
  const dir = fs.mkdtempSync(join(tmpdir(), 'lerna-tool-ownership-'));
  const identity = fs.statSync(dir),
    descriptors = new Map();
  let childConfirmed = true;
  // The fixture owns creation independently of the module being faulted. ACK
  // its exact root before any injected init failure; every actual FD is tracked.
  const native = {
    ...fs,
    openSync(path, flags, mode) {
      const fd = fs.openSync(path, flags, mode);
      descriptors.set(fd, path);
      return fd;
    },
    closeSync(fd) {
      fs.closeSync(fd);
      descriptors.delete(fd);
    },
  };
  const fixtureRegistry =
    process.env.LERNA_TEST_FIXTURE_SCOPE_REGISTRY ??
    process.env.LERNA_TEST_OWNED_SCOPE_REGISTRY ??
    join(dir, 'owned-scopes.log');
  let fixtureAcknowledged = false;
  t.after(() => {
    // Independent fixture observation, not a second scope.finish or a cleared
    // unknown flag: every injected Close first closed its actual descriptor.
    assert.equal(
      descriptors.size,
      0,
      'actual fixture descriptor closure unknown',
    );
    assert.ok(childConfirmed, 'original boundedBuild native exit unknown');
    assert.ok(fixtureAcknowledged, `fixture root ACK unknown; retained ${dir}`);
    if (fs.existsSync(dir)) {
      const current = fs.statSync(dir);
      assert.equal(current.dev, identity.dev);
      assert.equal(current.ino, identity.ino);
      fs.rmSync(dir, { recursive: true });
    }
  });
  function fixtureFD(path, flags, action) {
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
      throw new AggregateError(causes, `fixture ACK unknown; retained ${dir}`);
  }
  for (const path of [dir, dirname(dir)])
    fixtureFD(path, 'r', native.fsyncSync);
  fixtureFD(fixtureRegistry, 'a', (fd) => {
    const bytes = Buffer.from(
      `objects ${dir} ${identity.dev} ${identity.ino}\n`,
    );
    for (let offset = 0; offset < bytes.length; ) {
      const written = native.writeSync(
        fd,
        bytes,
        offset,
        bytes.length - offset,
      );
      if (written <= 0) throw Error('fixture ledger short write');
      offset += written;
    }
    native.fsyncSync(fd);
  });
  fixtureFD(dirname(fixtureRegistry), 'r', native.fsyncSync);
  fixtureAcknowledged = true;
  async function child(scope, observe = true) {
    childConfirmed = false;
    const result = await boundedBuild(process.execPath, ['-e', ''], {
      timeout: 1000,
      onStart: (pid) => scope.start(pid, 'compiler'),
    });
    childConfirmed = result.exitConfirmed;
    assert.ok(childConfirmed);
    assert.equal(result.error, undefined);
    assert.deepEqual(result.cleanupErrors, []);
    if (observe) scope.exited(result.pid, 'compiler', result.exitConfirmed);
    return result;
  }
  return {
    dir,
    native,
    descriptors,
    child,
    starting() {
      childConfirmed = false;
    },
    observe(result) {
      childConfirmed = result.exitConfirmed;
    },
  };
}
test('normal exact scope and native exit clean up; primary failure survives confirmed cleanup', async (t) => {
  for (const failed of [false, true]) {
    const f = fixture(t),
      scope = ownConformanceScope(f.dir, 'contract', f.native);
    await f.child(scope);
    const primary = Error('operation failure');
    if (failed)
      assert.throws(
        () => scope.finish(primary),
        (error) => includes(error, primary),
      );
    else scope.finish();
    assert.equal(fs.existsSync(f.dir), false);
  }
});
test('primary plus native Close diagnostic remain inspectable and ACK unknown is sticky', async (t) => {
  const f = fixture(t),
    scope = ownConformanceScope(f.dir, 'contract', f.native);
  await f.child(scope);
  const primary = Error('operation failure'),
    close = Error('mechanical Close diagnostic');
  const actualClose = f.native.closeSync;
  f.native.closeSync = (fd) => {
    actualClose(fd);
    throw close;
  };
  assert.throws(
    () => scope.finish(primary),
    (error) => includes(error, primary) && includes(error, close),
  );
  assert.equal(fs.existsSync(f.dir), true);
});
for (const action of ['write', 'fsync', 'parentSync'])
  test(`failed ${action} ACK cannot be washed away by a real later child exit`, async (t) => {
    const f = fixture(t),
      scope = ownConformanceScope(f.dir, 'generator', f.native);
    const diagnostic = Error(`mechanical ${action} ACK failure`);
    let armed = true;
    if (action === 'write')
      f.native.writeSync = (...args) => {
        if (armed) {
          armed = false;
          throw diagnostic;
        }
        return fs.writeSync(...args);
      };
    else
      f.native.fsyncSync = (fd) => {
        if (
          armed &&
          (action === 'fsync' ||
            f.descriptors.get(fd) ===
              dirname(
                process.env.LERNA_TEST_OWNED_SCOPE_REGISTRY ??
                  join(f.dir, 'owned-scopes.log'),
              ))
        ) {
          armed = false;
          throw diagnostic;
        }
        return fs.fsyncSync(fd);
      };
    let pid;
    f.starting();
    const result = await boundedBuild(process.execPath, ['-e', ''], {
      timeout: 1000,
      onStart: (started) => {
        pid = started;
        scope.start(started, 'compiler');
      },
    });
    f.observe(result);
    assert.ok(result.exitConfirmed);
    assert.ok(includes(result.error, diagnostic));
    scope.exited(pid, 'compiler', result.exitConfirmed);
    assert.throws(
      () => scope.finish(result.error),
      (error) => includes(error, diagnostic),
    );
    assert.equal(fs.existsSync(f.dir), true);
  });
test('one missing original exit observation is not replaced by a different confirmed entity', async (t) => {
  const f = fixture(t),
    scope = ownConformanceScope(f.dir, 'contract', f.native);
  await f.child(scope, false);
  await f.child(scope, true);
  assert.throws(() => scope.finish(), /retained/);
  assert.equal(fs.existsSync(f.dir), true);
});
test('changed exact identity prevents deletion and preserves primary', async (t) => {
  const f = fixture(t),
    scope = ownConformanceScope(f.dir, 'contract', f.native);
  await f.child(scope);
  const primary = Error('operation failure');
  f.native.statSync = (path) => {
    const actual = fs.statSync(path);
    return path === f.dir ? { dev: actual.dev, ino: actual.ino + 1 } : actual;
  };
  assert.throws(
    () => scope.finish(primary),
    (error) =>
      includes(error, primary) &&
      error.errors.some((cause) => /identity changed/.test(cause.message)),
  );
  assert.equal(fs.existsSync(f.dir), true);
});
test('initialization failure identifies and retains original scope and both action/Close causes', (t) => {
  const f = fixture(t),
    action = Error('mechanical initial fsync'),
    close = Error('mechanical initial Close');
  const actualClose = f.native.closeSync;
  f.native.closeSync = (fd) => {
    actualClose(fd);
    throw close;
  };
  f.native.fsyncSync = () => {
    throw action;
  };
  assert.throws(
    () => ownConformanceScope(f.dir, 'contract', f.native),
    (error) =>
      error.scope === f.dir &&
      includes(error, action) &&
      includes(error, close),
  );
  assert.equal(fs.existsSync(f.dir), true);
});
test('physical deletion plus failed external removed ACK reports deletion truth and original failure', async (t) => {
  const f = fixture(t),
    external = fixture(t),
    previous = process.env.LERNA_TEST_OWNED_SCOPE_REGISTRY;
  process.env.LERNA_TEST_OWNED_SCOPE_REGISTRY = join(
    external.dir,
    'removed-scopes.log',
  );
  try {
    const scope = ownConformanceScope(f.dir, 'contract', f.native);
    await f.child(scope);
    const primary = Error('operation failure'),
      ack = Error('mechanical removed ACK failure');
    f.native.writeSync = (fd, bytes, offset, length) => {
      if (bytes.toString().startsWith('contract_removed ')) throw ack;
      return fs.writeSync(fd, bytes, offset, length);
    };
    assert.throws(
      () => scope.finish(primary),
      (error) =>
        error.removed === true &&
        includes(error, primary) &&
        includes(error, ack),
    );
    assert.equal(fs.existsSync(f.dir), false);
  } finally {
    if (previous === undefined)
      delete process.env.LERNA_TEST_OWNED_SCOPE_REGISTRY;
    else process.env.LERNA_TEST_OWNED_SCOPE_REGISTRY = previous;
  }
});

test('physical removal failure aggregates original operation and keeps exact scope', async (t) => {
  const f = fixture(t),
    scope = ownConformanceScope(f.dir, 'contract', f.native);
  await f.child(scope);
  const primary = Error('operation failure'),
    remove = Error('mechanical remove failure');
  f.native.rmSync = () => {
    throw remove;
  };
  assert.throws(
    () => scope.finish(primary),
    (error) =>
      includes(error, primary) &&
      includes(error, remove) &&
      error.removed === false,
  );
  assert.equal(fs.existsSync(f.dir), true);
});
