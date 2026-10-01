#!/usr/bin/env python3
"""Verify the research snapshots and citations without running upstream code."""

import argparse
import hashlib
import json
import re
import subprocess
import sys
from datetime import datetime, timezone
from pathlib import Path
from urllib.parse import unquote, urlsplit


ROOT = Path(__file__).resolve().parents[3]
HERE = Path(__file__).resolve().parent


def git(path, *args):
    return subprocess.check_output(["git", "-C", str(path), *args], text=True).strip()


def anchors(path):
    body = path.read_text(encoding="utf-8")
    result = set(re.findall(r'\bid=["\']([^"\']+)["\']', body))
    seen = {}
    for heading in re.findall(r"^#{1,6}\s+(.+?)\s*#*\s*$", body, re.MULTILINE):
        heading = re.sub(r"\[([^\]]+)\]\([^)]*\)", r"\1", heading)
        heading = re.sub(r"<[^>]+>", "", heading).lower()
        slug = "".join(c for c in heading if c.isalnum() or c in " _-")
        slug = slug.replace(" ", "-")
        duplicate = seen.get(slug, 0)
        seen[slug] = duplicate + 1
        result.add(slug if duplicate == 0 else f"{slug}-{duplicate}")
    return result


def prose_without_code(body, errors, label):
    active = None
    lines = []
    diagrams = 0
    for line in body.splitlines():
        fence = re.match(r"^\s*(`{3,}|~{3,})(.*)$", line)
        if fence:
            marker, info = fence.groups()
            if active is None:
                active = marker
                diagrams += info.strip() == "mermaid"
            elif marker[0] == active[0] and len(marker) >= len(active) and not info.strip():
                active = None
            lines.append("")
        else:
            lines.append("" if active else line)
    if active:
        errors.append(f"{label}: unclosed code fence")
    return re.sub(r"`[^`\n]*`", "", "\n".join(lines)), diagrams


def verify():
    manifest = json.loads((HERE / "sources.json").read_text())
    errors = []
    snapshots = []
    repos = {}
    for repo in manifest["repositories"]:
        path = ROOT / repo["local_path"]
        name = repo["remote"].removeprefix("https://github.com/").removesuffix(".git")
        repos[name] = repo
        actual = {
            "project": repo["project"],
            "commit": git(path, "rev-parse", "HEAD"),
            "tree": git(path, "rev-parse", "HEAD^{tree}"),
            "remote": git(path, "remote", "get-url", "origin"),
            "branch": git(path, "branch", "--show-current"),
            "clean": not git(path, "status", "--porcelain"),
            "shallow": git(path, "rev-parse", "--is-shallow-repository") == "true",
            "ignored": subprocess.run(
                ["git", "check-ignore", "-q", repo["local_path"]], cwd=ROOT
            ).returncode == 0,
        }
        for field, expected in (("commit", repo["commit"]), ("tree", repo["tree"]),
                                ("remote", repo["remote"]), ("branch", repo["default_branch"]),
                                ("clean", True), ("shallow", True), ("ignored", True)):
            if actual[field] != expected:
                errors.append(f"{repo['project']}: snapshot {field} differs")
        snapshots.append(actual)

    baseline = manifest["architecture_baseline"]["sha256_by_path"]
    for relative, expected in baseline.items():
        path = ROOT / relative
        if not path.is_file() or hashlib.sha256(path.read_bytes()).hexdigest() != expected:
            errors.append(f"architecture baseline changed: {relative}")

    reports = [ROOT / "docs/research" / r["project"] / "README.md"
               for r in manifest["repositories"]]
    reports += [HERE / name for name in ("README.md", "architecture-baseline.md",
                                        "architecture-optimization.md", "verification.md")]
    source_links = 0
    local_links = 0
    diagrams = 0
    source_set = set()
    missing_refs = set()
    counts = {}
    for path in reports:
        label = str(path.relative_to(ROOT))
        if not path.is_file():
            errors.append(f"missing report: {label}")
            continue
        body = path.read_text(encoding="utf-8")
        if not body.endswith("\n"):
            errors.append(f"{label}: missing final newline")
        if any(line.rstrip() != line for line in body.splitlines()):
            errors.append(f"{label}: trailing whitespace")
        prose, number = prose_without_code(body, errors, label)
        diagrams += number
        definitions = set(re.findall(r"^\[([^\]]+)\]:", prose, re.MULTILINE))
        for match in re.finditer(r"\[([^\]\n]+)\](?![\[(])", prose):
            key = match[1]
            if prose[match.end():].startswith(":"):
                continue
            # Shortcuts are citations in these reports; inline links were excluded.
            if key not in definitions:
                missing_refs.add(f"{label}: undefined reference [{key}]")
        targets = re.findall(r"\[[^\]\n]*\]\(([^\n)]+)\)", prose)
        targets += re.findall(r"^\[[^\]]+\]:\s+(\S+)", prose, re.MULTILINE)
        for target in targets:
            target = target.strip().strip("<>")
            parsed = urlsplit(target)
            if parsed.scheme or target.startswith("//"):
                continue
            rawpath = unquote(parsed.path)
            rawpath = re.sub(r":\d+$", "", rawpath)
            destination = (path.parent / rawpath).resolve() if rawpath else path
            local_links += 1
            if not destination.exists():
                errors.append(f"{label}: missing local target {target}")
            elif parsed.fragment and destination.suffix == ".md":
                if unquote(parsed.fragment) not in anchors(destination):
                    errors.append(f"{label}: missing Markdown anchor {target}")
        per_report = 0
        for match in re.finditer(
            r"https://github\.com/([^/\s]+/[^/\s]+)/(blob|tree)/([^/\s]+)/([^\s)<>]+)", body
        ):
            repo_name, kind, revision, suffix = match.groups()
            repo = repos.get(repo_name)
            if repo is None:
                continue
            source_links += 1
            per_report += 1
            source_set.add(match[0])
            if revision != repo["commit"]:
                errors.append(f"{label}: unpinned or wrong source revision {match[0]}")
                continue
            parsed = urlsplit("https://example/" + suffix)
            source = ROOT / repo["local_path"] / unquote(parsed.path.lstrip("/"))
            if not source.exists():
                errors.append(f"{label}: missing upstream source {match[0]}")
                continue
            if kind == "blob" and parsed.fragment.startswith("L"):
                line = re.fullmatch(r"L(\d+)(?:-L(\d+))?", parsed.fragment)
                if not line:
                    errors.append(f"{label}: invalid line fragment {match[0]}")
                    continue
                first, last = int(line[1]), int(line[2] or line[1])
                length = len(source.read_bytes().splitlines())
                if not 1 <= first <= last <= length:
                    errors.append(f"{label}: source lines {first}-{last} outside {length}: {match[0]}")
        counts[label] = {"source_links": per_report, "mermaid_blocks": number,
                         "bytes": path.stat().st_size}
    errors.extend(sorted(missing_refs))
    return {
        "verified_at_utc": datetime.now(timezone.utc).isoformat(),
        "method": "Static snapshot, citation, local-link, anchor and fence checks only; no upstream execution.",
        "passed": not errors,
        "repository_snapshots": snapshots,
        "baseline_files_checked": len(baseline),
        "markdown_files_checked": len(reports),
        "source_links_checked": source_links,
        "unique_source_links_checked": len(source_set),
        "local_links_checked": local_links,
        "mermaid_blocks_with_closed_fences": diagrams,
        "reports": counts,
        "errors": errors,
    }


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output", type=Path, help="Optional JSON result path")
    args = parser.parse_args()
    result = verify()
    serialized = json.dumps(result, ensure_ascii=False, indent=2) + "\n"
    if args.output:
        args.output.write_text(serialized, encoding="utf-8")
    print(serialized, end="")
    sys.exit(0 if result["passed"] else 1)
