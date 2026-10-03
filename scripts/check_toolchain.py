#!/usr/bin/env python3
"""验证已锁定参考工具及真实 TZDB 内容，不刷新版本或伪造 version 标记。"""
import hashlib
import json
import os
from pathlib import Path
import subprocess
import sys

root = Path(__file__).resolve().parent.parent
lock = json.loads((root / "scripts/toolchain-lock.json").read_text())
commands = {"go": ["go", "version"], "node": ["node", "--version"], "pnpm": ["pnpm", "--version"], "sqlc": ["sqlc", "version"], "protoc": ["protoc", "--version"], "protoc-gen-go": ["protoc-gen-go", "--version"], "protoc-gen-go-grpc": ["protoc-gen-go-grpc", "--version"]}
for name, command in commands.items():
    try:
        result = subprocess.run(command, check=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True, timeout=30).stdout.strip()
    except (OSError, subprocess.CalledProcessError, subprocess.TimeoutExpired):
        sys.exit(f"Required locked tool unavailable: {name}")
    if result != lock["tools"][name]:
        sys.exit(f"Locked tool version mismatch: {name}")
fixture = root / lock["tzdb"]["fixture"]
manifest = json.loads((root / lock["tzdb"]["manifest"]).read_text())
runtime_root = Path(os.environ.get("HARNESS_TEST_TZDB_ROOT", fixture))
for name, metadata in manifest["files"].items():
    for folder in {fixture, runtime_root}:
        file = folder / name
        if hashlib.sha256(file.read_bytes()).hexdigest() != metadata["sha256"]:
            sys.exit(f"Pinned TZDB bytes mismatch: {name}")
    if "link" in metadata and os.readlink(fixture / name) != metadata["link"]:
        sys.exit(f"Pinned TZDB alias mismatch: {name}")
if (runtime_root / "tzdata.zi").read_text().splitlines()[0] != "# version " + lock["tzdb"]["version"]:
    sys.exit("Pinned TZDB version mismatch")
print("Locked reference toolchain and original TZDB bytes verified")
