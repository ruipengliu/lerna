import json, os, re, signal, subprocess, sys
from pathlib import Path

root = Path(__file__).resolve().parent
ack = json.loads((root / 'root-ack.json').read_text())
st = root.stat()
assert (st.st_dev, st.st_ino, st.st_mode & 0o777) == (ack['device'], ack['inode'], 0o700)
operation = sys.argv[1]
assert re.fullmatch(r'[a-z0-9-]{1,64}', operation)
argv = sys.argv[2:]
assert argv
registry = root / 'owned-scopes.log'

def append(record):
    with registry.open('a') as ledger:
        ledger.write(json.dumps(record, sort_keys=True) + '\n')
        ledger.flush()
        os.fsync(ledger.fileno())

def save(path, record):
    with path.open('x') as file:
        file.write(json.dumps(record, indent=2) + '\n')
        file.flush()
        os.fsync(file.fileno())
    directory = os.open(root, os.O_RDONLY | os.O_DIRECTORY)
    try:
        os.fsync(directory)
    finally:
        os.close(directory)

def group_absent(pgid):
    if pgid is None:
        return None
    try:
        os.killpg(pgid, 0)
        return False
    except ProcessLookupError:
        return True

env = os.environ.copy()
env['TMPDIR'] = str(root)
env['LERNA_TEST_OWNED_SCOPE_REGISTRY'] = str(registry)
env['LERNA_TEST_POSTGRES_DSN'] = Path('/tmp/lerna-work-01-dsn').read_text().strip()
raw_path = root / (operation + '.log')
raw = raw_path.open('xb')
read_gate = write_gate = proc = pgid = start = None
released = False
stage = "allocation"
try:
    read_gate, write_gate = os.pipe()
    # EOF, a malformed line, or absent registration can never execute native.
    gate_command = 'read -r gate <&' + str(read_gate) + ' && [ "$gate" = ack ] && exec "$@"'
    proc = subprocess.Popen(['bash', '-c', gate_command, 'owned-native', *argv], pass_fds=(read_gate,), start_new_session=True, env=env, stdout=raw, stderr=subprocess.STDOUT)
    read_fd, read_gate = read_gate, None
    stage = "read_gate_first_close"
    os.close(read_fd)
    stage = "pre_effect_registration"
    pgid = os.getpgid(proc.pid)
    start = Path('/proc/' + str(proc.pid) + '/stat').read_text().rsplit(')', 1)[1].split()[19]
    assert pgid == proc.pid
    pre = dict(event='native_start_ack', operation=operation, pid=proc.pid, pgid=pgid, starttime=start, root=str(root), argv=argv, deadline_seconds=120)
    append(pre)
    save(root / (operation + '-prelaunch.json'), pre)
    stage = "gate_release"
    if os.write(write_gate, b'ack\n') != 4:
        raise OSError('pre-effect release incomplete')
    released = True
    write_fd, write_gate = write_gate, None
    stage = "write_gate_first_close"
    os.close(write_fd)
except BaseException as failure:
    # Closing the still-owned write gate refuses launch. Even if that first
    # Close is unknown, a finite Wait/Kill/Wait owns the blocked generation.
    close_failures = [stage] if stage in ["read_gate_first_close", "write_gate_first_close"] else []
    for descriptor in [read_gate, write_gate]:
        if descriptor is not None:
            try:
                os.close(descriptor)
            except OSError as close_failure:
                close_failures.append(type(close_failure).__name__)
    status = None
    confirmed = proc is None
    timed_out = False
    if proc is not None:
        try:
            status = proc.wait(timeout=5)
            confirmed = True
        except subprocess.TimeoutExpired:
            timed_out = True
            if released:
                os.killpg(pgid, signal.SIGKILL)
            else:
                proc.kill()
            try:
                status = proc.wait(timeout=5)
                confirmed = True
            except subprocess.TimeoutExpired:
                confirmed = False
    aborted = dict(event='native_pre_effect_abort', operation=operation, pid=proc.pid if proc else None, pgid=pgid, starttime=start, cause_type=type(failure).__name__, failure_stage=stage, gate_close_failures=close_failures, exit=status, actual_wait_confirmed=confirmed if proc else None, no_process_created=proc is None, group_absent=group_absent(pgid), timed_out=timed_out, native_release_authorized=released)
    raw.write(('NATIVE_PRE_EFFECT_ABORT ' + json.dumps(aborted, sort_keys=True) + '\n').encode())
    raw.flush()
    os.fsync(raw.fileno())
    raw.close()
    try:
        append(aborted)
        save(root / (operation + '-abort.json'), aborted)
    except BaseException as record_failure:
        print('ABORT_RECORD_FAILURE', type(record_failure).__name__, flush=True)
    print('NATIVE_PRE_EFFECT_ABORT', json.dumps(aborted, sort_keys=True), flush=True)
    sys.exit(125)

timed_out = False
try:
    status = proc.wait(timeout=120)
except subprocess.TimeoutExpired:
    timed_out = True
    os.killpg(pgid, signal.SIGKILL)
    status = proc.wait(timeout=5)
absent = group_absent(pgid)
completion = dict(event='native_completion_ack', operation=operation, pid=proc.pid, pgid=pgid, starttime=start, exit=status, group_absent=absent, timed_out=timed_out)
raw.write(('NATIVE_COMPLETION_ACK ' + json.dumps(completion, sort_keys=True) + '\n').encode())
raw.flush()
os.fsync(raw.fileno())
raw.close()
completion['owned_raw_fsync_close_ack'] = True
append(completion)
save(root / (operation + '-outcome.json'), completion)
print('NATIVE_COMPLETION_ACK', json.dumps(completion, sort_keys=True), flush=True)
sys.exit(status if absent and not timed_out else 125)
