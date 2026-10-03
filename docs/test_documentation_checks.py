"""Regression cases for navigation and historical evidence."""

from contextlib import ExitStack
from pathlib import Path
import subprocess
import tempfile
import unittest
from unittest.mock import patch

import check_documentation as docs


def git(directory, *args):
    return subprocess.check_output(["git", "-C", str(directory), *args], stderr=subprocess.PIPE).decode().strip()


def commit(directory):
    git(directory, "add", ".")
    git(directory, "-c", "user.name=Documentation Tests", "-c", "user.email=docs@example.invalid",
        "commit", "-qm", "fixture")
    return git(directory, "rev-parse", "HEAD")


class DocumentationChecks(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        git(self.root, "init", "-q")
        self.old = self.root / "old.md"
        self.old.write_text("# 历史设计\n\n## 状态\n", encoding="utf-8")
        self.revision = commit(self.root)
        self.context = ExitStack()
        self.addCleanup(self.context.close)
        self.context.enter_context(patch.object(docs, "ROOT", self.root))
        docs.git_object.cache_clear()
        self.addCleanup(docs.git_object.cache_clear)

    def check(self, text):
        path = self.root / "README.md"
        path.write_text(text, encoding="utf-8")
        return docs.check_files([path])

    def test_nested_examples_are_excluded_but_live_links_are_checked(self):
        result = self.check("````markdown\n[example](missing.md)\n```sh\nfalse\n```\n````\n"
                            "`[inline](absent.md)`\n[current](old.md#状态)\n")
        self.assertEqual(result["result"], "passed")
        self.assertEqual(result["local_links"], 1)

    def test_missing_file_and_anchor_fail(self):
        result = self.check("[file](absent.md)\n[anchor](old.md#不存在)\n")
        self.assertEqual(result["result"], "failed")
        self.assertEqual(len(result["errors"]), 2)

    def test_reference_definitions_and_html_resources_are_checked(self):
        result = self.check('[doc][guide]\n[guide]: old.md#状态\n<img src="missing.svg">\n')
        self.assertEqual(result["local_links"], 2)
        self.assertEqual(len(result["errors"]), 1)
        self.assertIn("missing.svg", result["errors"][0])
        self.assertEqual(self.check("[doc][missing]\n")["result"], "failed")

    def test_duplicate_headings_and_paths_with_spaces(self):
        (self.root / "two words.md").write_text("## 状态\n## 状态\n", encoding="utf-8")
        self.assertEqual(self.check("[doc](<two words.md#状态-1>)\n")["result"], "passed")

    def test_machine_absolute_paths_fail(self):
        result = self.check(f"[machine]({self.old})\n")
        self.assertEqual(result["result"], "failed")
        self.assertIn("repository-relative", result["errors"][0])

    def test_deleted_working_file_remains_verifiable_in_history(self):
        self.old.unlink()
        link = docs.HISTORY_PREFIX + f"blob/{self.revision}/old.md#状态"
        self.assertEqual(self.check(f"[history]({link})\n")["result"], "passed")
        result = self.check(f"[history]({link.replace('#状态', '#L999')})\n")
        self.assertEqual(result["result"], "failed")

    def test_missing_history_is_blocked_and_wrong_path_is_failed(self):
        prefix = docs.HISTORY_PREFIX + "blob/"
        self.assertEqual(self.check(f"[history]({prefix}{'0' * 40}/old.md)\n")["result"], "blocked")
        self.assertEqual(self.check(f"[history]({prefix}{self.revision}/absent.md)\n")["result"], "failed")



if __name__ == "__main__":
    unittest.main()
