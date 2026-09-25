#!/usr/bin/env python3
"""Re-run registered TLC/Lean checks without overwriting delivered evidence."""
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


def digest(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def run(command, cwd, timeout=300):
    p = subprocess.run(command, cwd=cwd, stdout=subprocess.PIPE,
                       stderr=subprocess.STDOUT, text=True, timeout=timeout)
    return p.returncode, p.stdout


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--group", action="append")
    parser.add_argument("--java", default=os.environ.get("JAVA", "/opt/homebrew/opt/openjdk/bin/java"))
    parser.add_argument("--tlc", type=Path, default=CACHE / "tla-v1.7.4/tla2tools.jar")
    parser.add_argument("--lean", type=Path, default=CACHE / "lean-4.19.0-darwin_aarch64/bin/lean")
    args = parser.parse_args()
    out = args.output.resolve()
    out.mkdir(parents=True, exist_ok=False)
    if digest(args.tlc) != "936a262061c914694dfd669a543be24573c45d5aa0ff20a8b96b23d01e050e88":
        raise RuntimeError("Wrong TLC jar: the delivered checks pin release v1.7.4")
    _, lean_version = run([str(args.lean), "--version"], ROOT)
    if "version 4.19.0" not in lean_version:
        raise RuntimeError(lean_version)
    _, java_version = run([args.java, "-version"], ROOT)
    manifest = {"started_utc": datetime.now(timezone.utc).isoformat(),
                "tools": {"lean": lean_version.strip(), "lean_sha256": digest(args.lean),
                          "java": java_version.strip(), "tlc_sha256": digest(args.tlc)},
                "sources": {str(Path(__file__).relative_to(REPO)): digest(Path(__file__))},
                "checks": []}
    registries = sorted(ROOT.glob("*/checks.json"))
    if args.group:
        registries = [p for p in registries if p.parent.name in args.group]
    if not registries:
        raise RuntimeError("No registered checks")
    for registry in registries:
        group = registry.parent
        for path in sorted(group.iterdir()):
            if path.is_file() and (path.suffix in {".tla", ".cfg", ".lean"}
                                   or path.name in {"checks.json", "inventory.json"}):
                manifest["sources"][str(path.relative_to(REPO))] = digest(path)
        for check in json.loads(registry.read_text())["checks"]:
            name = check["id"]
            if not re.fullmatch(r"[a-zA-Z0-9_-]+", name):
                raise RuntimeError(f"Invalid check ID: {name}")
            with tempfile.TemporaryDirectory(prefix="mechanism-tlc-") as temporary:
                if check["kind"] == "tlc":
                    command = [args.java, f"-Djava.io.tmpdir={temporary}", "-XX:+UseParallelGC", "-Xmx2g", "-cp", str(args.tlc),
                               "tlc2.TLC", "-workers", "1", "-seed", "1", "-fp", "0",
                               "-metadir", temporary, "-config", str(group / check["config"]),
                               str(group / check["model"])]
                elif check["kind"] == "lean":
                    command = [str(args.lean), check["source"]]
                else:
                    raise RuntimeError(f"Unsupported check: {check}")
                code, log = run(command, group)
            logpath = out / f"{name}.log"
            logpath.write_text(log)
            expected = code == check["expected_exit"] and check["contains"] in log
            if check["kind"] == "lean":
                expected = expected and not any(x in log for x in ("sorryAx", "Lean.ofReduceBool", "error:", "warning:"))
            result = {**check, "group": group.name, "command": command,
                      "exit_code": code, "matched_expectation": expected,
                      "log_sha256": digest(logpath)}
            stats = re.findall(r"([\d,]+) states generated, ([\d,]+) distinct states found, ([\d,]+) states left on queue", log)
            if stats:
                for key, value in zip(("generated", "distinct", "remaining"), stats[-1]):
                    result[key] = int(value.replace(",", ""))
            depth = re.search(r"depth of the complete state graph search is (\d+)", log)
            if depth:
                result["depth"] = int(depth.group(1))
            if check["kind"] == "tlc" and check["expected_exit"] == 0:
                expected = expected and result.get("remaining") == 0 and "Finished in" in log
                result["matched_expectation"] = expected
            manifest["checks"].append(result)
            print(f"{name}: {'expected' if expected else 'UNEXPECTED'} (exit {code})", flush=True)
            (out / "manifest.json").write_text(json.dumps(manifest, indent=2, ensure_ascii=False) + "\n")
    manifest["finished_utc"] = datetime.now(timezone.utc).isoformat()
    manifest["all_expectations_matched"] = all(x["matched_expectation"] for x in manifest["checks"])
    (out / "manifest.json").write_text(json.dumps(manifest, indent=2, ensure_ascii=False) + "\n")
    raise SystemExit(0 if manifest["all_expectations_matched"] else 1)


if __name__ == "__main__":
    main()
