"""Check retained source metadata and citation pins, without requiring upstream clones."""
import json
from pathlib import Path
import re
import unittest
from urllib.parse import urlsplit

from check_links import extract_links, git_target

ROOT = Path(__file__).resolve().parents[3]
RESEARCH = ROOT / 'docs/research'
MANIFEST = RESEARCH / 'agent-harness-comparison/sources.json'
PROJECTS = {'codex', 'pi', 'deepseek-harness', 'prime-agent', 'crush'}
COMMIT = re.compile(r'^[0-9a-f]{40}$')


class ResearchSources(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.manifest = json.loads(MANIFEST.read_text(encoding='utf-8'))
        cls.repositories = cls.manifest['repositories']

    def test_source_metadata(self):
        self.assertEqual(len(self.repositories), len(PROJECTS))
        self.assertEqual({repo['project'] for repo in self.repositories}, PROJECTS)
        self.assertRegex(self.manifest['research_date'], r'^\d{4}-\d{2}-\d{2}$')
        self.assertTrue(self.manifest['timezone'])
        self.assertTrue(self.manifest['method'])
        for repo in self.repositories:
            with self.subTest(project=repo['project']):
                self.assertRegex(repo['remote'], r'^https://github\.com/[^/]+/[^/]+\.git$')
                self.assertRegex(repo['commit'], COMMIT)
                license = repo['license']
                self.assertTrue(license['note'])
                if license['name'] is None:
                    self.assertIsNone(license['source_url'])
                else:
                    self.assertTrue(license['name'])
                    prefix = repo['remote'].removesuffix('.git') + '/blob/' + repo['commit'] + '/'
                    self.assertTrue(license['source_url'].startswith(prefix))
                    self.assertTrue(license['source_url'][len(prefix):])

    def test_historical_baseline_is_available_at_recorded_commit(self):
        baseline = self.manifest['architecture_baseline']
        self.assertRegex(baseline['repo_commit'], COMMIT)
        self.assertEqual(baseline['path'], 'docs/architecture/.draft')
        _, issue = git_target(ROOT, baseline['repo_commit'], baseline['path'], 'tree')
        self.assertIsNone(issue, issue)

    def test_retained_source_citations_use_recorded_commits(self):
        commits = {urlsplit(repo['remote']).path.removesuffix('.git').strip('/').casefold(): repo['commit']
                   for repo in self.repositories}
        cited = set()
        for path in sorted(RESEARCH.rglob('*.md')):
            links, _ = extract_links(path.read_text(encoding='utf-8'))
            for link in links:
                url = urlsplit(link.target)
                match = re.fullmatch(r'/([^/]+/[^/]+)/(blob|tree)/([^/]+)/(.+)', url.path)
                if url.hostname != 'github.com' or not match or match[1].casefold() not in commits:
                    continue
                repo = match[1].casefold()
                cited.add(repo)
                with self.subTest(source=str(path.relative_to(ROOT)), line=link.line, target=link.target):
                    self.assertEqual(match[3], commits[repo])
        self.assertEqual(cited, set(commits))


if __name__ == '__main__':
    unittest.main()
