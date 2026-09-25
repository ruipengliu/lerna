#!/usr/bin/env python3
"""Run the pinned handoff checks; retain raw output, hashes, and expectations."""
import argparse
from datetime import datetime, timezone
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess
import tempfile

ROOT = Path(__file__).resolve().parent
REPO = ROOT.parents[1]
CACHE = Path.home() / ".cache/lerna-formal-tools"
PROFILES = [
    ("safety", 0, "Model checking completed. No error has been found."),
    ("liveness", 0, "Model checking completed. No error has been found."),
    ("healthy", 0, "Model checking completed. No error has been found."),
    ("mutant-early-ack", 12, "Invariant NoEarlyRelease is violated."),
    ("mutant-duplicate", 12, "Invariant NoDuplicate is violated."),
    ("no-fairness", 13, "Temporal properties were violated."),
    ("witness-completion", 12, "Invariant NotReleased is violated."),
    ("witness-gap", 12, "Invariant NoUncommittedGap is violated."),
    ("witness-live-premise", 12, "Invariant NoStablePendingBudget is violated."),
    ("safety-recheck", 0, "Model checking completed. No error has been found."),
]


def sha(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def execute(command, cwd):
    result = subprocess.run(command, cwd=cwd, text=True, stdout=subprocess.PIPE,
                            stderr=subprocess.STDOUT, timeout=300)
    return result.returncode, result.stdout


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--java", default=os.getenv("JAVA", "/opt/homebrew/opt/openjdk/bin/java"))
    parser.add_argument("--tlc", type=Path, default=Path(os.getenv("TLC_JAR", str(CACHE / "tla-v1.7.4/tla2tools.jar"))))
    parser.add_argument("--lean", type=Path, default=Path(os.getenv("LEAN", str(CACHE / "lean-4.19.0-darwin_aarch64/bin/lean"))))
    parser.add_argument("--output", type=Path, default=ROOT / "evidence/checked")
    parser.add_argument("--tlc-only", action="store_true", help="Explicit partial run; does not claim Lean verification")
    args = parser.parse_args()
    output = args.output.resolve()
    output.mkdir(parents=True, exist_ok=False)
    manifest = {"started_utc": datetime.now(timezone.utc).isoformat(),
                "scope": "TLC only" if args.tlc_only else "TLC and Lean",
                "sources": {}, "tools": {}, "checks": []}
    sources = [ROOT / "Handoff.tla", *sorted(ROOT.glob("*.cfg")),
               ROOT / "lean/Handoff.lean", Path(__file__).resolve(),
               REPO / "docs/architecture/endpoint-communication/message-contract.md",
               REPO / "docs/architecture/endpoint-communication/delivery-and-recovery.md"]
    for source in sources:
        manifest["sources"][str(source.relative_to(REPO))] = sha(source)
    code, version = execute([args.java, "-version"], ROOT)
    if code:
        raise RuntimeError(version)
    manifest["tools"]["java"] = {"path": args.java, "version": version.strip()}
    manifest["tools"]["tlc"] = {"path": str(args.tlc), "sha256": sha(args.tlc),
                                 "release": "v1.7.4", "version": "TLC2 Version 2.19 of 08 August 2024 (rev: 5a47802)",
                                 "url": "https://github.com/tlaplus/tlaplus/releases/download/v1.7.4/tla2tools.jar"}
    if sha(args.tlc) != "936a262061c914694dfd669a543be24573c45d5aa0ff20a8b96b23d01e050e88":
        raise RuntimeError("TLC jar differs from the pinned evidence version")
    for name, expected_exit, marker in PROFILES:
        config = "safety" if name == "safety-recheck" else name
        with tempfile.TemporaryDirectory(prefix="handoff-tlc-") as temporary:
            command = [args.java, "-XX:+UseParallelGC", "-Xmx2g", "-cp", str(args.tlc),
                       "tlc2.TLC", "-workers", "1", "-seed", "1", "-fp", "0",
                       "-metadir", temporary, "-config", str(ROOT / f"{config}.cfg"),
                       str(ROOT / "Handoff.tla")]
            code, log = execute(command, ROOT)
        if manifest["tools"]["tlc"]["version"] not in log:
            raise RuntimeError("TLC did not report the pinned version")
        (output / f"{name}.log").write_text(log)
        stats = re.findall(r"([\d,]+) states generated, ([\d,]+) distinct states found, ([\d,]+) states left on queue", log)
        check = {"name": name, "command": command, "exit_code": code,
                 "expected_exit": expected_exit, "expected_marker": marker,
                 "matched_expectation": code == expected_exit and marker in log,
                 "log_sha256": sha(output / f"{name}.log")}
        if stats:
            check["states_generated"], check["distinct_states"], check["queue_remaining"] = [int(v.replace(",", "")) for v in stats[-1]]
        manifest["checks"].append(check)
        print(f"{name}: {'expected' if check['matched_expectation'] else 'UNEXPECTED'} (exit {code})", flush=True)
    if not args.tlc_only:
        code, version = execute([str(args.lean), "--version"], ROOT)
        if code or "version 4.19.0" not in version:
            raise RuntimeError(f"Expected Lean 4.19.0: {version}")
        manifest["tools"]["lean"] = {"path": str(args.lean), "sha256": sha(args.lean), "version": version.strip()}
        command = [str(args.lean), "Handoff.lean"]
        code, log = execute(command, ROOT / "lean")
        (output / "lean.log").write_text(log)
        # The source includes #print axioms for the main theorems and witnesses.
        checked = code == 0 and "DurableHandoff.reachable_safe" in log
        checked = checked and not any(word in log for word in ("sorryAx", "Lean.ofReduceBool", "error:", "warning:"))
        manifest["checks"].append({"name": "lean", "command": command,
                                   "exit_code": code, "matched_expectation": checked,
                                   "log_sha256": sha(output / "lean.log")})
        print(f"lean: {'expected' if checked else 'UNEXPECTED'} (exit {code})", flush=True)
    manifest["finished_utc"] = datetime.now(timezone.utc).isoformat()
    manifest["all_expectations_matched"] = all(c["matched_expectation"] for c in manifest["checks"])
    (output / "manifest.json").write_text(json.dumps(manifest, indent=2, ensure_ascii=False) + "\n")
    raise SystemExit(0 if manifest["all_expectations_matched"] else 1)


if __name__ == "__main__":
    main()
