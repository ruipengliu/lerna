#!/usr/bin/env python3
"""按本地验证过的制品摘要安装参考生成器；不更改项目依赖。"""
import argparse
import hashlib
import json
from pathlib import Path
import shutil
import tarfile
import tempfile
from urllib.request import urlopen
import zipfile

parser = argparse.ArgumentParser()
parser.add_argument("--destination", required=True)
parser.add_argument("--archive-dir")
args = parser.parse_args()
destination = Path(args.destination).resolve()
destination.mkdir(parents=True, exist_ok=True)
bin_dir = destination / "bin"
bin_dir.mkdir(exist_ok=True)
root = Path(__file__).resolve().parent.parent
lock = json.loads((root / "scripts/toolchain-lock.json").read_text())
with tempfile.TemporaryDirectory(prefix="harness-tool-install-") as temp:
    work = Path(temp)
    for name in ("sqlc", "protoc"):
        spec = lock["archives"][name]
        filename = spec["url"].rsplit("/", 1)[1]
        archive = work / filename
        if args.archive_dir:
            source = Path(args.archive_dir) / filename
            if source.stat().st_size > 64 << 20:
                raise SystemExit("tool archive exceeds locked installation bound")
            shutil.copyfile(source, archive)
        else:
            with urlopen(spec["url"], timeout=30) as response, archive.open("wb") as output:
                remaining = 64 << 20
                while data := response.read(min(1 << 20, remaining + 1)):
                    remaining -= len(data)
                    if remaining < 0:
                        raise SystemExit("tool archive exceeds locked installation bound")
                    output.write(data)
        if hashlib.sha256(archive.read_bytes()).hexdigest() != spec["sha256"]:
            raise SystemExit(f"tool archive digest mismatch: {name}")
        unpack = work / name
        unpack.mkdir()
        if name == "sqlc":
            with tarfile.open(archive) as package:
                package.extractall(unpack, filter="data")
            shutil.copyfile(unpack / "sqlc", bin_dir / "sqlc")
        else:
            with zipfile.ZipFile(archive) as package:
                for item in package.infolist():
                    path = Path(item.filename)
                    if path.is_absolute() or ".." in path.parts:
                        raise SystemExit("unsafe compiler archive path")
                package.extractall(unpack)
            shutil.copyfile(unpack / "bin/protoc", bin_dir / "protoc")
            shutil.copytree(unpack / "include", destination / "include", dirs_exist_ok=True)
        (bin_dir / name).chmod(0o755)
print("Locked generator archive bytes verified and installed")
