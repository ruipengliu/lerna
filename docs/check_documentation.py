#!/usr/bin/env python3
"""Check repository documentation links and pinned local Git history offline."""

import argparse
from functools import lru_cache
from html import unescape
import json
from pathlib import Path
import re
import subprocess
from urllib.parse import unquote, urlsplit


ROOT = Path(__file__).resolve().parents[1]
HISTORY_PREFIX = "https://github.com/ruipengliu/lerna/"


def without_fences(body):
    """Preserve line numbers while excluding Markdown examples and comments."""
    lines = []
    active = None
    for line in body.splitlines(keepends=True):
        fence = re.match(r"^\s{0,3}(`{3,}|~{3,})(.*)$", line.rstrip("\n"))
        if fence:
            marker, info = fence.groups()
            if active is None:
                active = marker
            elif marker[0] == active[0] and len(marker) >= len(active) and not info.strip():
                active = None
            lines.append("\n" if line.endswith("\n") else "")
        else:
            lines.append(line if active is None else ("\n" if line.endswith("\n") else ""))
    prose = "".join(lines)
    prose = re.sub(r"<!--.*?-->", lambda m: "\n" * m[0].count("\n"), prose, flags=re.S)
    return prose, active is None


def anchors(body):
    prose, _ = without_fences(body)
    result = set(re.findall(r'\bid=["\']([^"\']+)["\']', prose))
    seen = {}
    for heading in re.findall(r"^ {0,3}#{1,6}\s+(.+?)\s*#*\s*$", prose, re.M):
        heading = re.sub(r"\[([^\]]+)\]\([^)]*\)", r"\1", heading)
        heading = unescape(re.sub(r"<[^>]+>", "", heading)).lower()
        slug = "".join(c for c in heading if c.isalnum() or c in " _-").replace(" ", "-")
        count = seen.get(slug, 0)
        seen[slug] = count + 1
        result.add(slug if count == 0 else f"{slug}-{count}")
    return result


def normalize_label(value):
    return " ".join(value.split()).casefold()


def references(body, markdown=True):
    """Return explicit Markdown/HTML targets and malformed reference diagnostics.

    Code fences and inline code are examples, not navigation. Shortcut links are
    checked when they have a definition; explicit [text][id] links must have one.
    """
    errors = []
    if markdown:
        prose, closed = without_fences(body)
        if not closed:
            errors.append("unclosed code fence")
        prose = re.sub(r"(`+).*?\1", lambda m: " " * len(m[0]), prose)
    else:
        prose = re.sub(r"<!--.*?-->", lambda m: "\n" * m[0].count("\n"), body, flags=re.S)
    targets = []
    occupied = []
    patterns = [r'\b(?:href|src)\s*=\s*["\']([^"\']+)["\']']
    if markdown:
        patterns += [
            r"\]\(\s*(<[^>\n]+>|[^\s()]+(?:\([^()]*\)[^\s()]*)*)\s*(?:[\"'][^\n]*?[\"']\s*)?\)",
            r"^ {0,3}\[[^\]\n]+\]:\s*(<[^>\n]+>|\S+)",
        ]
    for pattern in patterns:
        for match in re.finditer(pattern, prose, re.M):
            targets.append((prose.count("\n", 0, match.start()) + 1, unescape(match[1].strip("<>"))))
            occupied.append((match.start(), match.end()))
    if markdown:
        definitions = {
            normalize_label(m[1])
            for m in re.finditer(r"^ {0,3}\[([^\]\n]+)\]:", prose, re.M)
        }
        for match in re.finditer(r"\[([^\]\n]+)\]\[([^\]\n]*)\]", prose):
            if any(start <= match.start() < end for start, end in occupied):
                continue
            label = normalize_label(match[2] or match[1])
            if label not in definitions:
                errors.append(f"line {prose.count(chr(10), 0, match.start()) + 1}: undefined reference [{label}]")
    return targets, errors


@lru_cache(maxsize=None)
def git_object(revision, path):
    result = subprocess.run(
        ["git", "cat-file", "-t", f"{revision}:{path}"], cwd=ROOT,
        capture_output=True, text=True,
    )
    if result.returncode:
        return None, None
    kind = result.stdout.strip()
    if kind != "blob":
        return kind, None
    result = subprocess.run(["git", "show", f"{revision}:{path}"], cwd=ROOT, capture_output=True)
    return kind, result.stdout if result.returncode == 0 else None


def check_fragment(body, suffix, fragment):
    fragment = unquote(fragment)
    if not fragment:
        return None
    if suffix in (".md", ".html", ".svg") and fragment in anchors(body):
        return None
    line = re.fullmatch(r"L(\d+)(?:-L(\d+))?", fragment)
    if line:
        first, last = int(line[1]), int(line[2] or line[1])
        if 1 <= first <= last <= len(body.splitlines()):
            return None
        return f"line fragment outside file: {fragment}"
    if suffix in (".md", ".html", ".svg"):
        return f"missing anchor: {fragment}"
    return None


def check_files(paths):
    errors, unavailable = [], []
    counts = {"files": 0, "local_links": 0, "historical_links": 0, "external_links_not_checked": 0}
    for path in paths:
        label = str(path.relative_to(ROOT))
        body = path.read_text(encoding="utf-8")
        counts["files"] += 1
        targets, syntax_errors = references(body, path.suffix == ".md")
        errors.extend(f"{label}: {error}" for error in syntax_errors)
        for line, target in targets:
            where = f"{label}:{line}: {target}"
            parsed = urlsplit(target)
            if target.startswith(HISTORY_PREFIX) and re.match(r"(?:blob|tree)/", target[len(HISTORY_PREFIX):]):
                counts["historical_links"] += 1
                match = re.fullmatch(r"/(?:ruipengliu/lerna)/(blob|tree)/([0-9a-f]{40})/(.+)", parsed.path)
                if not match:
                    errors.append(f"{where}: history reference must use a full commit")
                    continue
                kind, rev, oldpath = match.groups()
                revision = subprocess.run(["git", "cat-file", "-e", rev + "^{commit}"], cwd=ROOT, capture_output=True)
                if revision.returncode:
                    unavailable.append(f"{where}: commit is not available locally")
                    continue
                actual_kind, data = git_object(rev, unquote(oldpath))
                expected = "blob" if kind == "blob" else "tree"
                if actual_kind != expected:
                    errors.append(f"{where}: missing historical {expected}")
                elif data is not None:
                    error = check_fragment(data.decode("utf-8", errors="replace"), Path(oldpath).suffix, parsed.fragment)
                    if error:
                        errors.append(f"{where}: {error}")
                continue
            if parsed.scheme or target.startswith("//"):
                counts["external_links_not_checked"] += 1
                continue
            if not parsed.path and not parsed.fragment:
                continue
            counts["local_links"] += 1
            if Path(unquote(parsed.path)).is_absolute():
                errors.append(f"{where}: use a repository-relative path")
                continue
            destination = (path.parent / unquote(parsed.path)).resolve() if parsed.path else path
            if not destination.is_relative_to(ROOT):
                errors.append(f"{where}: target leaves the repository")
            elif not destination.exists():
                errors.append(f"{where}: missing local target")
            elif parsed.fragment and destination.is_file():
                error = check_fragment(destination.read_text(encoding="utf-8", errors="replace"), destination.suffix, parsed.fragment)
                if error:
                    errors.append(f"{where}: {error}")
    return {
        "result": "failed" if errors else "blocked" if unavailable else "passed",
        "scope": "repository navigation and locally available pinned history; no external HTTP or application execution",
        **counts, "errors": errors, "unavailable": unavailable,
    }


def documentation_files():
    result = subprocess.run(
        ["git", "ls-files", "--cached", "--others", "--exclude-standard", "-z"],
        cwd=ROOT, check=True, capture_output=True,
    )
    paths = set()
    for name in result.stdout.decode().split("\0"):
        if not name:
            continue
        path = ROOT / name
        in_scope = path.parent == ROOT or name.startswith(("docs/", ".agents/"))
        if in_scope and path.suffix in (".md", ".html", ".svg") and path.is_file():
            paths.add(path)
    return sorted(paths)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.parse_args()
    result = check_files(documentation_files())
    print(json.dumps(result, ensure_ascii=False, indent=2))
    return 1 if result["errors"] else 2 if result["unavailable"] else 0


if __name__ == "__main__":
    raise SystemExit(main())
