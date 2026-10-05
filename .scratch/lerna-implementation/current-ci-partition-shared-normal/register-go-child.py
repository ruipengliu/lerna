import fcntl, json, os, signal, subprocess, sys, time
from pathlib import Path

root = Path(__file__).resolve().parent
ack = json.loads((root / 'root-ack.json').read_text())
st = root.stat()
assert (st.st_dev, st.st_ino, st.st_mode & 0o777) == (ack['device'], ack['inode'], 0o700)
parent_operation = os.environ['LERNA_SHARED_PARENT_OPERATION']
assert parent_operation in ['partition-shared-normal', 'partition-shared-race']
deadline = float(os.environ['LERNA_SHARED_DEADLINE_MONOTONIC'])
assert 0 < deadline - time.monotonic() <= 1445
real_go = '/home/agent/.local/toolchains/go1.27.1/bin/go'
argv = [real_go, *sys.argv[1:]]
assert len(argv) > 1

# Serial shared Make recipes invoke one Go command at a time. Refuse concurrent
# allocation, rather than adding an unbounded lock wait or reusing an identity.
with (root / 'go-child-counter.json').open('r+') as counter:
    fcntl.flock(counter, fcntl.LOCK_EX | fcntl.LOCK_NB)
    state = json.load(counter)
    sequence = state['next']
    assert isinstance(sequence, int) and sequence > 0
    counter.seek(0)
    counter.write(json.dumps({'next': sequence + 1}) + '\n')
    counter.truncate()
    counter.flush()
    os.fsync(counter.fileno())
operation = 'go-child-' + str(sequence).zfill(4)

def append(record):
    with (root / 'owned-scopes.log').open('a') as ledger:
        ledger.write(json.dumps(record, sort_keys=True) + '\n')
        ledger.flush()
        os.fsync(ledger.fileno())

def save(suffix, record):
    with (root / (operation + suffix)).open('x') as output:
        output.write(json.dumps(record, indent=2) + '\n')
        output.flush()
        os.fsync(output.fileno())
    descriptor = os.open(root, os.O_RDONLY | os.O_DIRECTORY)
    try:
        os.fsync(descriptor)
    finally:
        os.close(descriptor)

def original_pid_absent(pid, start):
    try:
        current = Path('/proc/' + str(pid) + '/stat').read_text().rsplit(')', 1)[1].split()[19]
        return current != start
    except FileNotFoundError:
        return True

read_gate = write_gate = proc = pgid = start = None
released = False
stage = 'allocation'
try:
    read_gate, write_gate = os.pipe()
    gate = 'read -r gate <&' + str(read_gate) + ' && [ "$gate" = ack ] && exec "$@"'
    # Inherit the registered whole invocation's group and original stdout/stderr.
    # This preserves go-list stdout exactly and keeps every child in the finite
    # whole supervisor's group. Individual package alarms remain original120s.
    proc = subprocess.Popen(['bash', '-c', gate, 'owned-go', *argv], pass_fds=(read_gate,))
    descriptor, read_gate = read_gate, None
    stage = 'read_gate_first_close'
    os.close(descriptor)
    stage = 'pre_effect_registration'
    pgid = os.getpgid(proc.pid)
    start = Path('/proc/' + str(proc.pid) + '/stat').read_text().rsplit(')', 1)[1].split()[19]
    assert pgid == os.getpgrp()
    pre = dict(event='owned_go_child_start_ack', operation=operation, parent_operation=parent_operation,
               pid=proc.pid, pgid=pgid, starttime=start, root=str(root), argv=argv,
               original_whole_deadline_monotonic=deadline,
               remaining_whole_seconds=deadline - time.monotonic())
    append(pre)
    save('-prelaunch.json', pre)
    stage = 'gate_release'
    if os.write(write_gate, b'ack\n') != 4:
        raise OSError('pre-effect release incomplete')
    released = True
    descriptor, write_gate = write_gate, None
    stage = 'write_gate_first_close'
    os.close(descriptor)
except BaseException as cause:
    close_failures = [stage] if stage.endswith('first_close') else []
    for descriptor in [read_gate, write_gate]:
        if descriptor is not None:
            try:
                os.close(descriptor)
            except OSError as error:
                close_failures.append(type(error).__name__)
    status, confirmed = None, proc is None
    if proc is not None:
        try:
            status = proc.wait(timeout=5)
            confirmed = True
        except subprocess.TimeoutExpired:
            proc.kill()
            try:
                status = proc.wait(timeout=5)
                confirmed = True
            except subprocess.TimeoutExpired:
                confirmed = False
    aborted = dict(event='owned_go_child_pre_effect_abort', operation=operation,
                   parent_operation=parent_operation, pid=proc.pid if proc else None,
                   pgid=pgid, starttime=start, cause_type=type(cause).__name__,
                   failure_stage=stage, gate_close_failures=close_failures, exit=status,
                   actual_wait_confirmed=confirmed if proc else None,
                   no_process_created=proc is None, native_release_authorized=released)
    append(aborted)
    save('-abort.json', aborted)
    print('OWNED_GO_CHILD_ABORT', json.dumps(aborted, sort_keys=True), file=sys.stderr, flush=True)
    sys.exit(125)

timed_out = False
try:
    status = proc.wait(timeout=max(0, deadline - time.monotonic()))
except subprocess.TimeoutExpired:
    timed_out = True
    proc.kill()
    status = proc.wait(timeout=5)
completion = dict(event='owned_go_child_completion_ack', operation=operation,
                  parent_operation=parent_operation, pid=proc.pid, pgid=pgid,
                  starttime=start, exit=status, actual_wait_confirmed=True,
                  original_pid_generation_absent=original_pid_absent(proc.pid, start),
                  timed_out=timed_out, original_whole_deadline_monotonic=deadline,
                  shared_group_absence_checked_only_after_whole_wait=True)
append(completion)
save('-outcome.json', completion)
# Metadata goes only to stderr: successful Go discovery and go-env stdout remain
# the exact original tool output consumed by the shared shell and Make entry.
print('OWNED_GO_CHILD_COMPLETION_ACK', json.dumps(completion, sort_keys=True), file=sys.stderr, flush=True)
sys.exit(status if status >= 0 and not timed_out else 125)
