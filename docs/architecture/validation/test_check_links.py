"""CLI regression tests, using disposable Git repositories and no network."""
import json
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest

SCRIPT = Path(__file__).with_name('check_links.py')


class LinkChecks(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name)
        self.git('init', '-q')
        self.git('config', 'user.email', 'fixture@example.invalid')
        self.git('config', 'user.name', 'Fixture')
        self.git('remote', 'add', 'origin', 'https://github.com/example/fixture.git')

    def git(self, *args):
        return subprocess.check_output(['git', '-C', str(self.root), *args], text=True).strip()

    def write(self, name, text):
        path = self.root / name
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(text, encoding='utf-8')

    def check(self, expected=0):
        run = subprocess.run([sys.executable, str(SCRIPT), '--root', str(self.root)],
                             capture_output=True, text=True)
        self.assertEqual(run.returncode, expected, run.stderr + run.stdout)
        return json.loads(run.stdout)

    def test_inline_reference_shortcut_images_html_and_unicode(self):
        self.write('文 档.md', '# 你好 `code`\n\n同名\n----\n\n## 同名\n<a id="自定义"></a>\n')
        self.write('pic.svg', '<svg id="图"/>')
        self.write('index.md', '''[inline](%E6%96%87%20%E6%A1%A3.md#你好-code "title")
[ref][ Some   Label ] [Some Label] [Some Label][]
![image](pic.svg#图)
<img src=pic.svg> <a href="文%20档.md#自定义">link</a>
[Some Label]: <文 档.md#同名-1> 'title'
''')
        result = self.check()
        self.assertGreaterEqual(result['local_links_checked'], 7)
        self.assertEqual(result['errors'], [])

    def test_code_comments_and_remote_links_are_not_checked(self):
        self.write('index.md', '''# Real
`[inline](missing)` and ``[with ` tick](missing)``
```md
[code](missing)
~~~
```
    [indented](missing)
<!-- [comment](missing) -->
<pre><a href="missing">code</a></pre>
<script>const s = '<img src="missing">';</script>
[remote](https://example.invalid/nope) [mail](mailto:a@example.invalid)
[network](//example.invalid/nope) [ordinary prose]
''')
        self.assertEqual(self.check()['local_links_checked'], 0)

    def test_code_cannot_swallow_later_prose_or_create_quoted_links(self):
        self.write('index.md', '```html\n<!--\n```\n'
                   '> ```markdown\n> [code](missing-code.md)\n> ```\n'
                   '[real](missing-real.md)\n')
        result = self.check(1)
        self.assertEqual(len(result['errors']), 1)
        self.assertEqual(result['errors'][0]['target'], 'missing-real.md')

    def test_formatted_headings_keep_code_and_remove_emphasis_markers(self):
        self.write('index.md', '# **Strong** _emphasis_ `snake_case`\n'
                   '[jump](#strong-emphasis-snake_case)\n')
        self.check()

    def test_missing_file_anchor_and_reference_report_locations(self):
        self.write('index.md', '# Here\n[x](missing.md)\n[x](#absent)\n[x][undefined]\n')
        result = self.check(1)
        self.assertEqual({e['issue'] for e in result['errors']},
                         {'missing_target', 'missing_anchor', 'undefined_reference'})
        self.assertEqual({e['line'] for e in result['errors']}, {2, 3, 4})

    def test_nested_and_escaped_destinations_and_heading_collisions(self):
        self.write('a(b).md', '# A\n# A\n# A-1\n# A\n')
        self.write('index.md', '[**nested [label]**](a(b).md#a-1-1)\n'
                   '[escaped](a\\(b\\).md#a-2)\n[![image](icon.svg)](a(b).md#a)\n')
        self.write('icon.svg', '<svg/>')
        self.assertEqual(self.check()['local_links_checked'], 4)

    def test_html_ids_entities_root_paths_and_query(self):
        self.write('page.html', '<a name="old"></a><div id="a&amp;b"></div>')
        self.write('sub/index.md', '[root](/page.html?x=1#a%26b)\n[legacy](../page.html#old)\n')
        self.check()

    def test_deleted_pinned_git_blob_is_checked_from_exact_commit(self):
        self.write('old.md', '# Archived\n')
        self.git('add', '.')
        self.git('commit', '-qm', 'archive')
        commit = self.git('rev-parse', 'HEAD')
        (self.root / 'old.md').unlink()
        self.git('add', '-u')
        self.git('commit', '-qm', 'remove')
        self.write('index.md', f'[old](https://github.com/example/fixture/blob/{commit}/old.md#archived)\n')
        result = self.check()
        self.assertEqual(result['historical_links_checked'], 1)
        self.write('index.md', f'[old](https://github.com/example/fixture/blob/{commit}/old.md#L2)\n')
        self.assertEqual(self.check(1)['errors'][0]['issue'], 'invalid_line_range')

    def test_missing_git_object_is_failure_not_remote_skip(self):
        self.write('index.md', '[old](https://github.com/example/fixture/blob/' + 'f' * 40 + '/old.md)\n')
        self.assertEqual(self.check(1)['errors'][0]['issue'], 'missing_git_object')

    def test_unclosed_fence_and_escape_outside_root(self):
        self.write('index.md', '[outside](../outside.md)\n```\n')
        self.assertEqual({e['issue'] for e in self.check(1)['errors']},
                         {'outside_repository', 'unclosed_fence'})

    def test_ignored_files_are_excluded_but_untracked_docs_are_checked(self):
        self.write('.gitignore', 'ignored/\n')
        self.write('ignored/index.md', '[broken](missing)\n')
        self.write('new.md', '# New\n')
        self.assertEqual(self.check()['files_checked'], 1)


if __name__ == '__main__':
    unittest.main()
