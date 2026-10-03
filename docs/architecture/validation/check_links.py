#!/usr/bin/env python3
"""Check repository Markdown/HTML/SVG links offline, including pinned local Git objects."""

import argparse
from collections import Counter
from dataclasses import dataclass
import html
from html.parser import HTMLParser
import json
from pathlib import Path
import re
import subprocess
import sys
import unicodedata
from urllib.parse import unquote, urlsplit

DOCUMENT_SUFFIXES = {'.md', '.markdown', '.html', '.htm', '.svg'}
PINNED_GITHUB = re.compile(r'^/([^/]+/[^/]+)/(blob|tree)/([0-9a-fA-F]{40})/(.+)$')


def git(root, *args):
    """Read Git only; callers distinguish missing evidence from valid empty blobs."""
    run = subprocess.run(['git', '-C', str(root), *args], capture_output=True)
    if run.returncode:
        raise ValueError(run.stderr.decode('utf-8', errors='replace').strip())
    return run.stdout


def blank(text):
    return re.sub(r'[^\n]', ' ', text)


def prose(body, markdown=True, inline_code=True):
    """Mask code/comments while retaining offsets and line numbers."""
    errors = []
    lines, active = [], None
    for number, line in enumerate(body.splitlines(keepends=True), 1):
        if not markdown:
            lines.append(line)
            continue
        fence = re.match(r'^(?: {0,3}> ?)*[ \t]*(`{3,}|~{3,})(.*)', line)
        if fence and (active is None or (fence[1][0] == active[0][0]
                     and len(fence[1]) >= len(active[0]) and not fence[2].strip())):
            active = (fence[1], number) if active is None else None
            lines.append(blank(line))
        elif active or line.startswith(('    ', '\t')):
            lines.append(blank(line))
        else:
            lines.append(line)
    if active:
        errors.append({'line': active[1], 'issue': 'unclosed_fence', 'target': active[0]})
    body = ''.join(lines)
    body = re.sub(r'<!--.*?(?:-->|$)', lambda m: blank(m[0]), body, flags=re.S)
    body = re.sub(r'<(script|style|pre|code)\b[^>]*>.*?</\1\s*>',
                  lambda m: blank(m[0]), body, flags=re.S | re.I)
    if markdown and inline_code:
        body = re.sub(r'(?<!`)(`+)(?!`)([\s\S]*?)(?<!`)\1(?!`)',
                      lambda m: blank(m[0]), body)
    return body, errors


class HTMLLinks(HTMLParser):
    def __init__(self, body):
        super().__init__(convert_charrefs=True)
        self.links, self.anchors = [], set()
        self.feed(body)

    def handle_starttag(self, tag, attrs):
        for name, value in attrs:
            if value is None:
                continue
            if name in ('href', 'src'):
                self.links.append(Link(value, self.getpos()[0], 'html'))
            if name == 'id' or (tag == 'a' and name == 'name'):
                self.anchors.add(value)

    handle_startendtag = handle_starttag


@dataclass(frozen=True)
class Link:
    target: str
    line: int
    kind: str


def unescape(value):
    return html.unescape(re.sub(r'\\([!"#$%&\'()*+,\-./:;<=>?@\[\]\\^_`{|}~])', r'\1', value))


def label_key(value):
    return ' '.join(unescape(value).split()).casefold()


def closing(text, start, opener, closer):
    depth, i = 1, start + 1
    while i < len(text):
        if text[i] == '\\':
            i += 2
            continue
        if text[i] == opener:
            depth += 1
        elif text[i] == closer:
            depth -= 1
            if depth == 0:
                return i
        i += 1
    return None


def destination(text):
    """Parse a Markdown destination and optional title (balanced parentheses allowed)."""
    text = text.strip()
    if text.startswith('<'):
        end = text.find('>')
        if end < 0:
            return None
        value, tail = text[1:end], text[end + 1:].strip()
    else:
        i, depth = 0, 0
        while i < len(text):
            if text[i] == '\\':
                i += 2
                continue
            if text[i].isspace() and depth == 0:
                break
            depth += (text[i] == '(') - (text[i] == ')')
            i += 1
        value, tail = text[:i], text[i:].strip()
    if tail and not re.fullmatch(r'"[\s\S]*"|\'[\s\S]*\'|\([\s\S]*\)', tail):
        return None
    return unescape(value)


def extract_links(body, markdown=True):
    text, errors = prose(body, markdown)
    links = HTMLLinks(text).links
    if not markdown:
        return links, errors
    definitions = {}
    pattern = r'^ {0,3}\[([^\]\n]+)\]:[ \t]*(?:\n[ \t]*)?([^\n]+)'
    for match in re.finditer(pattern, text, re.M):
        target = destination(match[2])
        if target is not None:
            definitions.setdefault(label_key(match[1]), target)
            links.append(Link(target, text.count('\n', 0, match.start()) + 1, 'definition'))
    text = re.sub(pattern, lambda m: blank(m[0]), text, flags=re.M)

    def scan(start, end):
        i = start
        while i < end:
            if text[i] == '\\':
                i += 2
                continue
            if text[i] != '[':
                i += 1
                continue
            close = closing(text, i, '[', ']')
            if close is None or close >= end:
                i += 1
                continue
            label, after = text[i + 1:close], close + 1
            line = text.count('\n', 0, i) + 1
            # Nested images inside link labels are independently checked.
            scan(i + 1, close)
            if after < end and text[after] == '(':
                finish = closing(text, after, '(', ')')
                if finish is not None:
                    target = destination(text[after + 1:finish])
                    if target is not None:
                        links.append(Link(target, line, 'inline'))
                        i = finish + 1
                        continue
            if after < end and text[after] == '[':
                finish = closing(text, after, '[', ']')
                if finish is not None:
                    key = label_key(text[after + 1:finish] or label)
                    if key in definitions:
                        links.append(Link(definitions[key], line, 'reference'))
                    else:
                        errors.append({'line': line, 'issue': 'undefined_reference', 'target': key})
                    i = finish + 1
                    continue
            key = label_key(label)
            if key in definitions:
                links.append(Link(definitions[key], line, 'shortcut'))
            # Undefined [words] alone are ordinary Markdown text, not broken links.
            i = after
    scan(0, len(text))
    return links, errors


def anchors(body, markdown=True):
    text, _ = prose(body, markdown, inline_code=False)
    result = HTMLLinks(text).anchors
    if not markdown:
        return result
    headings = []
    lines = text.splitlines()
    for i, line in enumerate(lines):
        atx = re.match(r'^ {0,3}#{1,6}(?:[ \t]+|$)(.*)', line)
        if atx:
            headings.append(re.sub(r'[ \t]+#+[ \t]*$', '', atx[1]).strip())
        elif i and re.fullmatch(r' {0,3}(?:=+|-+)\s*', line) and lines[i - 1].strip():
            headings.append(lines[i - 1].strip())
    used = set()
    for heading in headings:
        heading = re.sub(r'!?\[([^\]]+)\](?:\([^)]*\)|\[[^\]]*\])?', r'\1', heading)
        heading = re.sub(r'(?<!\w)(_+)(.+?)\1(?!\w)', r'\2', heading)
        heading = unescape(re.sub(r'<[^>]+>', '', heading)).lower()
        slug = ''.join(c for c in heading if c in ' _-' or unicodedata.category(c)[0] in 'LNM')
        slug = slug.replace(' ', '-')
        candidate, n = slug, 0
        while candidate in used:
            n += 1
            candidate = f'{slug}-{n}'
        used.add(candidate)
        result.add(candidate)
    return result


def github_repo(remote):
    match = re.fullmatch(r'(?:https?://github\.com/|git@github\.com:)([^/]+/[^/]+?)(?:\.git)?/?', remote)
    return match[1].casefold() if match else None


def repository_map(root):
    try:
        name = github_repo(git(root, 'remote', 'get-url', 'origin').decode().strip())
    except ValueError:
        name = None
    return {name: root} if name else {}


def git_target(repo, revision, path, kind):
    # Require the exact commit object, not an abbreviated name or working tree.
    if not re.fullmatch(r'[0-9a-fA-F]{40}', revision):
        return None, 'unpinned_revision'
    try:
        git(repo, 'cat-file', '-e', revision + '^{commit}')
        spec = revision + ':' + path
        actual = git(repo, 'cat-file', '-t', spec).decode().strip()
        expected = 'tree' if kind == 'tree' else 'blob'
        if actual != expected:
            return None, 'wrong_git_object_type'
        return (git(repo, 'cat-file', 'blob', spec) if kind == 'blob' else b''), None
    except ValueError:
        return None, 'missing_git_object'


def fragment_issue(data, suffix, fragment, historical=False):
    fragment = unquote(fragment)
    if not fragment:
        return None
    if historical and re.fullmatch(r'L\d+(?:-L\d+)?', fragment):
        nums = list(map(int, re.findall(r'\d+', fragment)))
        return None if 1 <= nums[0] <= nums[-1] <= len(data.splitlines()) else 'invalid_line_range'
    if suffix.lower() in DOCUMENT_SUFFIXES:
        text = data.decode('utf-8')
        return None if fragment in anchors(text, suffix.lower() in {'.md', '.markdown'}) else 'missing_anchor'
    if historical and fragment.startswith('L'):
        return 'invalid_line_range'
    return None  # Non-document fragments (e.g. PDF #page=) are outside scope.


def check_files(root, files=None, repos=None):
    root = root.resolve()
    repos = repository_map(root) if repos is None else repos
    if files is None:
        names = git(root, 'ls-files', '--cached', '--others', '--exclude-standard', '-z').decode().split('\0')
        files = [root / name for name in sorted(set(names)) if Path(name).suffix.lower() in DOCUMENT_SUFFIXES]
        files = [p for p in files if p.exists()]  # Working-tree deletions are not source documents.
    errors, counts, cache = [], Counter(), {}
    for path in files:
        label = str(path.relative_to(root))
        try:
            if not path.resolve().is_relative_to(root):
                raise OSError('document symlink points outside repository')
            body = path.read_text(encoding='utf-8')
            links, parse_errors = extract_links(body, path.suffix.lower() in {'.md', '.markdown'})
            errors.extend(dict(source=label, **error) for error in parse_errors)
        except (OSError, UnicodeError) as error:
            errors.append({'source': label, 'line': 1, 'issue': 'unreadable_document', 'target': str(error)})
            continue
        counts['files_checked'] += 1
        for link in links:
            issue = None
            try:
                url = urlsplit(link.target)
                pinned = PINNED_GITHUB.fullmatch(url.path) if url.hostname == 'github.com' else None
                if pinned and pinned[1].casefold() in repos:
                    counts['historical_links_checked'] += 1
                    name, kind, revision, filename = pinned.groups()
                    key = (name.casefold(), revision, unquote(filename), kind)
                    if key not in cache:
                        cache[key] = git_target(repos[key[0]], revision, key[2], kind)
                    data, issue = cache[key]
                    if not issue and kind == 'blob':
                        issue = fragment_issue(data, Path(unquote(filename)).suffix, url.fragment, True)
                elif url.scheme or url.netloc:
                    counts['remote_links_skipped'] += 1
                    continue
                elif url.path or url.fragment:
                    counts['local_links_checked'] += 1
                    raw = unquote(url.path)
                    dest = ((root if raw.startswith('/') else path.parent) / raw.lstrip('/')).resolve() if raw else path
                    if not dest.is_relative_to(root):
                        issue = 'outside_repository'
                    elif not dest.exists():
                        issue = 'missing_target'
                    elif url.fragment and dest.is_file():
                        if dest not in cache:
                            cache[dest] = dest.read_bytes()
                        issue = fragment_issue(cache[dest], dest.suffix, url.fragment)
            except (ValueError, OSError, UnicodeError) as error:
                issue = 'invalid_target: ' + str(error)
            if issue:
                errors.append({'source': label, 'line': link.line, 'target': link.target, 'issue': issue})
    return dict(passed=not errors, **{key: counts[key] for key in (
        'files_checked', 'local_links_checked', 'historical_links_checked', 'remote_links_skipped')}, errors=errors)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--root', type=Path, help='Repository root (default: this checkout)')
    parser.add_argument('--output', type=Path, help='Optional JSON report (stdout is always JSON)')
    args = parser.parse_args()
    try:
        result = check_files(args.root or Path(__file__).resolve().parents[3])
    except (ValueError, OSError) as error:
        result = {'passed': False, 'errors': [{'issue': 'repository_unavailable', 'target': str(error)}]}
    serialized = json.dumps(result, ensure_ascii=False, indent=2) + '\n'
    if args.output:
        args.output.write_text(serialized, encoding='utf-8')
    print(serialized, end='')
    return 0 if result['passed'] else 1


if __name__ == '__main__':
    sys.exit(main())
