import pathlib, json, hashlib, os, stat, difflib
r = pathlib.Path('/tmp/lerna-04-ticket06-execution')
s = pathlib.Path('/tmp/lerna-06-setup-cause-remaining-mechanics-static')
w = pathlib.Path('/tmp/lerna-worktrees/content-snapshots-06')
digest = lambda data: hashlib.sha256(data).hexdigest()
rel = 'conformance/component/content_process_setup_test.go'
before = (s / 'source-unformatted' / rel).read_bytes()
after = (w / rel).read_bytes()
assert len(before) == 10630 and digest(before) == 'ba30a1db83018eb781ae05140eb787349e2c0aa63bad7affeb7515068d797ac4'
assert len(after) == 10759 and digest(after) == '03d8be401624e397244a8a45251bc08533af18bf67b7a98ecd5b0671ac5a3def'
pair = (s / 'actual-format-source-pair.ndiff').read_bytes()
assert digest(pair) == '5c48e29ee1168c3551a5c37f5ccdc9bba8d3f93dd51601a872be4c9bd80ba8d4'
delta = pair.decode().splitlines(True)
assert ''.join(difflib.restore(delta, 1)).encode() == before
assert ''.join(difflib.restore(delta, 2)).encode() == after
diff = (r / 'setup-cause-mechanics-format-only.diff').read_bytes()
assert digest(diff) == 'debc204ae2d7324128e5e6ac9aa24a4a86ea3cc21c7d07355d1206008a3239f7'
manifest = (r / 'setup-cause-mechanics-formatted27-manifest.json').read_bytes()
assert digest(manifest) == 'e8887ab7681a5469b17165312b2938251476f15f822a15a27eea711ffb584ccd'
for row in json.loads(manifest)['paths']:
    data = (w / row['path']).read_bytes()
    assert len(data) == row['bytes'] and digest(data) == row['sha256']
    assert data == pathlib.Path(row['snapshot']).read_bytes()
for row in json.loads((r / 'current-producer-regression-protected-current16.json').read_text())['all_current16_exact_protected']:
    actual = os.lstat(row['path'])
    assert (actual.st_dev, actual.st_ino, stat.S_IMODE(actual.st_mode)) == (row['device'], row['inode'], 0o700)
actual = os.lstat(r)
assert (actual.st_dev, actual.st_ino, stat.S_IMODE(actual.st_mode)) == (33, 407243, 0o700)
assert digest((r / 'run-native.py').read_bytes()) == '90a808a0e3cf3a3a62441818dd0f87392a763b5ebdeef9ec2d5f2b5abc3e8d6d'
ledger = (r / 'owned-scopes.log').read_bytes()
assert len(ledger.splitlines()) == 351
original = json.loads((r / 'setup-cause-mechanics-before-native.json').read_text())
assert digest(b''.join(ledger.splitlines(keepends=True)[:349])) == original['ledger_sha256']
release = json.loads((r / 'setup-cause-mechanics-first-postcheck-release.json').read_text())
raw = (r / 'setup-cause-mechanics-format.log').read_bytes()
assert digest(raw) == release['raw_sha256']
outcome = json.loads((r / 'setup-cause-mechanics-format-outcome.json').read_text())
assert outcome == release['actual_fmt'] and outcome['exit'] == 0 and outcome['group_absent'] and not outcome['timed_out'] and outcome['owned_raw_fsync_close_ack']
members = []
for entry in pathlib.Path('/proc').iterdir():
    if not entry.name.isdigit(): continue
    try:
        text = (entry / 'stat').read_text()
        fields = text[text.rfind(')') + 2:].split()
        if int(fields[2]) == outcome['pgid']: members.append(int(entry.name))
    except (OSError, ValueError): pass
assert not members
facts = {'status': 'METADATA_ONLY_V2_OF_SAME_ORIGINAL_FMT_NO_FMT_GO_RERUN', 'root_full_hunk_adoption': 'Parent explicitly reviewed complete5008B diff: layout and one optional struct-field semicolon to newline; exact ndiff pair binds complete source bytes. No global normalization or semantic inference.', 'original_checker_exit1_retained': True, 'static_metadata_dictionary_SyntaxError_retained': True, 'fmt_raw_original_sha256': digest(raw), 'fmt_actual': outcome, 'current_fmt_group_members': members, 'formatted27_sha256': digest(manifest), 'source27_and_snapshots_byte_equal': True, 'original349_ledger_prefix_unchanged': True, 'protected16_original_devino0700': True, 'mechanics_N_R_not_started': True}
p = r / 'setup-cause-mechanics-format-v2-postchecks.json'
with p.open('x') as file:
    json.dump(facts, file, indent=2); file.write('\n'); file.flush(); os.fsync(file.fileno())
fd = os.open(r, os.O_RDONLY | os.O_DIRECTORY); os.fsync(fd); os.close(fd)
print(str(p), digest(p.read_bytes()))
