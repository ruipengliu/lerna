#!/usr/bin/env python3
"""Check this documentation delivery; never infer runtime or performance results."""
import contextlib
import importlib.util
import io
import json
import re
import subprocess
from pathlib import Path

HERE = Path(__file__).resolve().parent
ROOT = HERE.parents[1]


def git(*args):
    return subprocess.check_output(["git", *args], cwd=ROOT, text=True).strip()


def main():
    problems = []
    checks = []
    spec = importlib.util.spec_from_file_location(
        "document_checker", ROOT / "docs/architecture/.draft/validation/check_documents.py"
    )
    checker = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(checker)
    for relative in (
        "docs/architecture", "docs/adr",
        "docs/research/agent-harness-comparison",
        "docs/research/codex", "docs/research/pi", "docs/research/deepseek-harness",
        "docs/research/prime-agent", "docs/research/crush",
        ".scratch/harness-architecture-refresh",
    ):
        checker.ROOT = ROOT / relative
        output = io.StringIO()
        with contextlib.redirect_stdout(output):
            failed = checker.main()
        checks.append({"scope": relative, "output": output.getvalue().strip()})
        if failed:
            problems.append(f"document check failed: {relative}")

    manifest = json.loads((ROOT / "docs/research/agent-harness-comparison/sources.json").read_text())
    repositories = {}
    for entry in manifest["repositories"]:
        slug = entry["remote"].removeprefix("https://github.com/").removesuffix(".git")
        repositories[slug] = entry
        if git("-C", entry["local_path"], "rev-parse", "HEAD") != entry["commit"]:
            problems.append(f"source commit changed: {slug}")
        if git("-C", entry["local_path"], "status", "--porcelain"):
            problems.append(f"source working tree changed: {slug}")

    citations = set()
    files = [ROOT / "docs/research/agent-harness-comparison/data-flow-io-comparison.md"]
    files += sorted((ROOT / "docs/research/agent-harness-comparison/io").glob("*.md"))
    pattern = r"https://github\.com/([^/]+/[^/]+)/blob/([0-9a-f]{40})/([^\s)#]+)(?:#L(\d+)(?:-L(\d+))?)?"
    for path in files:
        body = path.read_text()
        if "REFERENCE_IO_TABLE" in body:
            problems.append(f"unfinished comparison table: {path.name}")
        for slug, commit, relative, first, last in re.findall(pattern, body):
            citations.add((slug, commit, relative, first, last))
            repo = repositories.get(slug)
            if repo is None or commit != repo["commit"]:
                problems.append(f"unregistered pinned source: {slug}@{commit}")
                continue
            target = ROOT / repo["local_path"] / relative
            if not target.is_file():
                problems.append(f"missing pinned source: {slug}/{relative}")
            elif first:
                length = len(target.read_text().splitlines())
                if not 1 <= int(first) <= int(last or first) <= length:
                    problems.append(f"source line range invalid: {slug}/{relative}:{first}-{last}")

    for ticket in sorted((HERE / "issues").glob("*.md")):
        body = ticket.read_text()
        if "**Progress:** completed" not in body or "- [ ]" in body:
            problems.append(f"unfinished design ticket: {ticket.name}")
    changed_machine_assets = git(
        "diff", "--name-only", "--", "docs/architecture/.draft/contracts/schemas",
        "docs/architecture/.draft/validation/*.py",
        "docs/research/agent-harness-comparison/sources.json",
    )
    if changed_machine_assets:
        problems.append("frozen machine assets or historical snapshot changed")
    whitespace = subprocess.run(["git", "diff", "--check"], cwd=ROOT, text=True, capture_output=True)
    if whitespace.returncode:
        problems.append(whitespace.stdout or whitespace.stderr)
    result = {
        "date": "2026-10-01", "status": "FAIL" if problems else "PASS",
        "scope": "Documentation, original repository commits, pinned source paths/line ranges and ticket completion; no runtime guarantee.",
        "document_checks": checks, "pinned_repositories": len(repositories),
        "unique_source_locations": len(citations), "problems": problems,
    }
    (HERE / "delivery-checks.json").write_text(json.dumps(result, ensure_ascii=False, indent=2) + "\n")
    for check in checks:
        print(check["scope"] + ": " + check["output"])
    print(f"{result['status']}: {len(repositories)} pinned repositories; {len(citations)} source locations; {len(problems)} errors")
    for problem in problems:
        print(problem)
    return bool(problems)


if __name__ == "__main__":
    raise SystemExit(main())
