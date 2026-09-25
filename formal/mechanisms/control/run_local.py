#!/usr/bin/env python3
"""Reproduce this group's checks; the root runner may consume checks.json too."""
import argparse
import concurrent.futures
import datetime
import hashlib
import json
import pathlib
import subprocess
import tempfile
import time

ROOT = pathlib.Path(__file__).resolve().parent
JAVA = "/opt/homebrew/opt/openjdk/bin/java"
JAR = "/Users/ruipengliu/.cache/lerna-formal-tools/tla-v1.7.4/tla2tools.jar"
LEAN = "/Users/ruipengliu/.cache/lerna-formal-tools/lean-4.19.0-darwin_aarch64/bin/lean"

def digest(path):
    return hashlib.sha256(pathlib.Path(path).read_bytes()).hexdigest()

def run(check):
    started = datetime.datetime.now(datetime.timezone.utc).isoformat()
    clock = time.monotonic()
    with tempfile.TemporaryDirectory(prefix=".states-", dir=ROOT) as temporary:
        if check["kind"] == "tlc":
            cmd = [JAVA, "-Djava.io.tmpdir="+temporary, "-Xmx1536m", "-cp", JAR, "tlc2.TLC", "-workers", "1",
                   "-metadir", temporary, "-config", check["config"], check["model"]]
            files = [check["model"], check["config"]]
        else:
            cmd = [LEAN, check["source"]]
            files = [check["source"]]
        try:
            result = subprocess.run(cmd, cwd=ROOT, stdout=subprocess.PIPE,
                                    stderr=subprocess.STDOUT, timeout=240)
            output, code = result.stdout, result.returncode
        except subprocess.TimeoutExpired as exc:
            output, code = exc.stdout or b"", "timeout"
        logfile = ROOT / "logs" / (check["id"] + ".log")
        logfile.write_bytes(output)
        text = output.decode(errors="replace")
        metadata = dict(id=check["id"], evidence_revision="post-correlation-review-v2",
                        evidence_status="current", started_utc=started,
                        duration_seconds=round(time.monotonic()-clock, 3),
                        command=cmd, command_sha256=hashlib.sha256(json.dumps(cmd).encode()).hexdigest(),
                        source_sha256={f:digest(ROOT/f) for f in files},
                        exit_code=code, expected_exit=check["expected_exit"],
                        marker=check["contains"],
                        expectation_met=code == check["expected_exit"] and check["contains"] in text,
                        log_sha256=digest(logfile))
        (ROOT/"logs"/(check["id"]+".json")).write_text(json.dumps(metadata,indent=2)+"\n")
        print(json.dumps({k:metadata[k] for k in ("id","duration_seconds","exit_code","expectation_met")}),flush=True)
        return metadata

if __name__ == "__main__":
    parser=argparse.ArgumentParser()
    parser.add_argument("ids",nargs="*")
    args=parser.parse_args()
    checks=json.loads((ROOT/"checks.json").read_text())["checks"]
    if args.ids:
        checks=[c for c in checks if c["id"] in args.ids]
    with concurrent.futures.ThreadPoolExecutor(max_workers=2) as pool:
        results=list(pool.map(run,checks))
    raise SystemExit(0 if all(r["expectation_met"] for r in results) else 1)
