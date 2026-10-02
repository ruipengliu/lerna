#!/usr/bin/env python3
"""Check local Markdown structure. Rendering and semantic review are separate."""
import re
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]


def anchors(text):
    result = set(re.findall(r'<a\s+id="([^"]+)"', text))
    occurrences = {}
    for heading in re.findall(r"^#{1,6} (.+)$", text, re.M):
        slug = "".join(
            char for char in heading.lower().replace(" ", "-")
            if char in "-_" or char.isalnum()
        )
        count = occurrences.get(slug, 0)
        result.add(f"{slug}-{count}" if count else slug)
        occurrences[slug] = count + 1
    return result


def main():
    problems = []
    paths = sorted(path for path in ROOT.rglob("*.md")
                   if ".draft" not in path.relative_to(ROOT).parts)
    link_count = diagram_count = 0
    for path in paths:
        text = path.read_text()
        name = str(path.relative_to(ROOT))
        if re.search(r"(?:docs/|\.\./)(?:architecture-v2/|archive/architecture-)", text):
            problems.append(f"{name}: reference to another architecture baseline")
        fence = None
        table_width = None
        prose = []
        for number, line in enumerate(text.splitlines(), 1):
            if line.rstrip() != line:
                problems.append(f"{name}:{number}: trailing whitespace")
            marker = re.match(r"^\s*(`{3,}|~{3,})(.*)$", line)
            if marker:
                token, language = marker.groups()
                if fence is None:
                    fence = token
                    diagram_count += language.strip() == "mermaid"
                elif token[0] == fence[0] and len(token) >= len(fence) and not language.strip():
                    fence = None
                table_width = None
                continue
            if fence is not None:
                continue
            prose.append(line)
            if line.startswith("|"):
                width = len(re.findall(r"(?<!\\)\|", line)) - 1
                if table_width is not None and width != table_width:
                    problems.append(f"{name}:{number}: inconsistent table width")
                table_width = width
            else:
                table_width = None
        if fence is not None:
            problems.append(f"{name}: unclosed code fence")
        for destination in re.findall(r"\[[^\]]*\]\(([^)]+)\)", "\n".join(prose)):
            destination = destination.strip("<>")
            if re.match(r"^[a-zA-Z][a-zA-Z0-9+.-]*:", destination):
                continue
            link_count += 1
            filename, _, fragment = destination.partition("#")
            target = (path.parent / filename).resolve() if filename else path
            if not target.exists():
                problems.append(f"{name}: missing target {destination}")
            elif fragment and target.suffix == ".md" and fragment not in anchors(target.read_text()):
                problems.append(f"{name}: missing anchor {destination}")
    for problem in problems:
        print(problem)
    print(f"{'FAIL' if problems else 'PASS'}: {len(paths)} Markdown files; "
          f"{link_count} local links; {diagram_count} Mermaid blocks; {len(problems)} errors")
    print("Scope: local links and Markdown structure; no semantic or rendering guarantee")
    return bool(problems)


if __name__ == "__main__":
    raise SystemExit(main())
