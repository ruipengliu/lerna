#!/usr/bin/env python3
"""等待实际公开就绪响应；有界且失败时不输出响应正文或凭据。"""
import argparse
import time
from urllib.request import urlopen

parser = argparse.ArgumentParser()
parser.add_argument("url")
parser.add_argument("--seconds", type=int, default=30)
args = parser.parse_args()
deadline = time.monotonic() + min(max(args.seconds, 1), 60)
while time.monotonic() < deadline:
    try:
        with urlopen(args.url, timeout=1) as response:
            if response.status == 200:
                print("Configured HTTP process is reachable")
                break
    except Exception:
        pass
    time.sleep(0.2)
else:
    raise SystemExit("Configured HTTP process did not become reachable")
