#!/usr/bin/env python3
"""Run this architecture baseline's independent static checks."""
import subprocess
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parent
CHECKS = [
    [sys.executable, 'validate.py'],
    [sys.executable, 'validate_protocol.py'],
    [sys.executable, 'validate_transport.py'],
    ['node', 'verify_transport_proofs.mjs'],
    [sys.executable, 'validate_brain.py'],
    [sys.executable, 'validate_lease.py'],
    [sys.executable, 'validate_release_recovery.py'],
    [sys.executable, 'check_documents.py'],
]

def main():
    failed = []
    for command in CHECKS:
        print(f'CHECK: {command[-1]}', flush=True)
        try:
            result = subprocess.run(command, cwd=ROOT, check=False)
            if result.returncode:
                failed.append(command[-1])
        except OSError as error:
            print(f'Unavailable: {error}', flush=True)
            failed.append(command[-1])
    print('FAIL: ' + ', '.join(failed) if failed else 'PASS: all static checks')
    return bool(failed)

if __name__ == '__main__':
    raise SystemExit(main())
