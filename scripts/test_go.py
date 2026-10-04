#!/usr/bin/env python3
"""按有界单包观察预算流式运行原 Go 测试，不改变业务期限。"""
import argparse
import json
import os
from pathlib import Path
import re
import subprocess
import sys


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--race", action="store_true")
    parser.add_argument("packages", nargs="*", default=["./..."])
    args = parser.parse_args()
    # 整包正常观察曾超过 90 分钟；race 定点观测约为正常的 2.906 倍。
    # 这里只给 runner 有界累计预算；每条原命令和领域门禁仍用原期限。
    raw_minutes = os.environ.get("HARNESS_GO_TEST_TIMEOUT_MINUTES", "360" if args.race else "180")
    if re.fullmatch(r"[0-9]{1,3}", raw_minutes) is None or not 1 <= int(raw_minutes) <= 360:
        parser.error("HARNESS_GO_TEST_TIMEOUT_MINUTES must be an integer from 1 to 360")
    packages = args.packages or ["./..."]
    if any(package.startswith("-") for package in packages):
        parser.error("packages must be Go package paths")
    command = ["go", "test", "-mod=readonly", "-json", "-p", "2", "-count=1", f"-timeout={int(raw_minutes)}m"]
    if args.race:
        command.append("-race")
    command.extend(packages)
    print(json.dumps({"runner": "go", "mode": "race" if args.race else "normal", "package_timeout_minutes": int(raw_minutes), "argv": command}), file=sys.stderr, flush=True)
    return subprocess.run(command, cwd=Path(__file__).resolve().parent.parent).returncode


if __name__ == "__main__":
    sys.exit(main())
