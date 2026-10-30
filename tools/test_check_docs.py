"""Regression cases for lost/duplicated requirements and broken navigation."""
from pathlib import Path
import re
import shutil
import contextlib
import io
import tempfile
import unittest
from unittest import mock

import check_docs


SOURCE_ROOT = Path(__file__).resolve().parent.parent


class DocumentationChecks(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        for relative in ('docs/product.md', 'docs/acceptance/requirements.md'):
            destination = self.root / relative
            destination.parent.mkdir(parents=True, exist_ok=True)
            shutil.copyfile(SOURCE_ROOT / relative, destination)

    def change_product(self, before, after):
        path = self.root / 'docs/product.md'
        text = path.read_text()
        self.assertIn(before, text)
        path.write_text(text.replace(before, after, 1))

    def test_complete_inventory_passes(self):
        self.assertEqual(check_docs.requirement_check(self.root), [])

    def test_missing_definition_fails(self):
        self.change_product('<a id="cy-01-01"></a>', '')
        self.assertTrue(any('CY-01-01 occurs 0' in error for error in check_docs.requirement_check(self.root)))

    def test_duplicate_definition_fails(self):
        self.change_product('<a id="cy-01-01"></a>', '<a id="cy-01-01"></a>\n<a id="cy-01-01"></a>')
        self.assertTrue(any('CY-01-01 occurs 2' in error for error in check_docs.requirement_check(self.root)))

    def test_duplicate_id_without_anchor_fails(self):
        with (self.root / 'docs/product.md').open('a') as stream:
            stream.write('\n**CY-01-01.** Duplicate definition.\n')
        self.assertTrue(any('CY-01-01 defined 2' in error for error in check_docs.requirement_check(self.root)))

    def test_missing_acceptance_mapping_fails(self):
        path = self.root / 'docs/acceptance/requirements.md'
        path.write_text(re.sub(r'^\| \[CY-01-01\].*\n', '', path.read_text(), flags=re.M))
        self.assertTrue(any('acceptance mapping CY-01-01 occurs 0' in error for error in check_docs.requirement_check(self.root)))

    def test_duplicate_acceptance_mapping_fails(self):
        path = self.root / 'docs/acceptance/requirements.md'
        row = next(line for line in path.read_text().splitlines() if line.startswith('| [CY-01-01]'))
        with path.open('a') as stream:
            stream.write(row + '\n')
        self.assertTrue(any('acceptance mapping CY-01-01 occurs 2' in error for error in check_docs.requirement_check(self.root)))

    def test_mapping_to_wrong_existing_anchor_fails(self):
        path = self.root / 'docs/acceptance/requirements.md'
        path.write_text(path.read_text().replace('[CY-01-01](../product.md#cy-01-01)',
                                                '[CY-01-01](../product.md#cy-01-02)', 1))
        self.assertTrue(any('CY-01-01 must link to its matching' in error
                            for error in check_docs.requirement_check(self.root)))

    def test_swapped_definition_anchors_fail(self):
        self.change_product('id="cy-01-01"', 'id="placeholder"')
        self.change_product('id="cy-01-02"', 'id="cy-01-01"')
        self.change_product('id="placeholder"', 'id="cy-01-02"')
        self.assertTrue(any('must identify exactly its matching definition' in error
                            for error in check_docs.requirement_check(self.root)))

    def test_coverage_gaps_fail(self):
        for before, after, message in (
            ('## 22. ', '## Missing. ', 'chapters'),
            ('### Scenario A:', '### Missing A:', 'missing scenario A'),
            ('| Accuracy |', '| Missing |', 'missing quality requirement: Accuracy'),
            ('**Disposition: Required.**', '', 'lacks disposition'),
        ):
            with self.subTest(message=message):
                shutil.copyfile(SOURCE_ROOT / 'docs/product.md', self.root / 'docs/product.md')
                self.change_product(before, after)
                self.assertTrue(any(message in error for error in check_docs.requirement_check(self.root)))

    def test_link_to_missing_file_fails(self):
        (self.root / 'index.md').write_text('[Missing](missing.md)\n')
        self.assertTrue(any('broken local link in index.md' in error for error in check_docs.link_check(self.root)))

    def test_invalid_anchor_fails(self):
        (self.root / 'index.md').write_text('# Target\n[Bad](#missing)\n')
        self.assertTrue(any('broken Markdown heading link in index.md' in error for error in check_docs.link_check(self.root)))

    def test_valid_anchors_reference_links_and_fences(self):
        isolated = self.root / 'navigation'
        isolated.mkdir()
        (isolated / 'index.md').write_text('# Same\n# Same\n<a id="explicit"></a>\n'
                                          '[First](#same) [Second](#same-1) [Explicit](#explicit)\n'
                                          '[Reference][target]\n[target]: #same\n'
                                          '```\n# Ignored\n```\n')
        self.assertEqual(check_docs.link_check(isolated), [])
        self.assertNotIn('ignored', check_docs.heading_anchors((isolated / 'index.md').read_text()))

    def test_github_anchor_punctuation_and_duplicate_rules(self):
        isolated = self.root / 'anchors'
        isolated.mkdir()
        (isolated / 'index.md').write_text(
            '## Voice deployment configuration repair — 2026-10-29\n'
            '## Voice deployment configuration repair — 2026-10-29\n'
            '## `Colon`:  multiple  spaces\n'
            '<a id="explicit-anchor"></a>\n'
            '[Dash](#voice-deployment-configuration-repair--2026-10-29) '
            '[Duplicate](#voice-deployment-configuration-repair--2026-10-29-1) '
            '[Explicit](#explicit-anchor)\n'
        )
        anchors = check_docs.heading_anchors((isolated / 'index.md').read_text())
        self.assertIn('voice-deployment-configuration-repair--2026-10-29', anchors)
        self.assertIn('voice-deployment-configuration-repair--2026-10-29-1', anchors)
        self.assertIn('colon-multiple-spaces', anchors)
        self.assertEqual(check_docs.link_check(isolated), [])

    def test_length_warnings_and_strict_mode(self):
        (self.root / 'index.md').write_text(
            'short paragraph\n'
            + 'x' * (check_docs.PARAGRAPH_LINE_LIMIT + 1) + '\n'
            + '| A | B |\n|---|---|\n'
            + '| short | ' + 'y' * (check_docs.TABLE_CELL_LIMIT + 1) + ' |\n'
        )
        warnings = check_docs.length_warnings(self.root)
        self.assertEqual(len(warnings), 2)
        (self.root / 'short.md').write_text('short\n')
        self.assertEqual(len(check_docs.length_warnings(self.root)), 2)

    def test_disclaimer_warning_excludes_authoritative_evidence(self):
        (self.root / 'index.md').write_text('This is not an implementation or delivery claim.\n')
        evidence = self.root / 'docs/acceptance/evidence.md'
        evidence.parent.mkdir(parents=True, exist_ok=True)
        evidence.write_text('This is not an implementation or delivery claim.\n')
        warnings = check_docs.disclaimer_warnings(self.root)
        self.assertTrue(any('index.md' in warning for warning in warnings))
        self.assertFalse(any('evidence.md' in warning for warning in warnings))

    def test_strict_length_makes_warnings_fail(self):
        (self.root / 'index.md').write_text('x' * (check_docs.PARAGRAPH_LINE_LIMIT + 1) + '\n')
        output = io.StringIO()
        with contextlib.redirect_stdout(output):
            with mock.patch('sys.argv', ['check_docs.py', '--root', str(self.root), '--strict-length']):
                result = check_docs.main()
        self.assertEqual(result, 1)
        self.assertIn('WARN: long paragraph line', output.getvalue())

    def test_known_pending_master_plan_links_are_warnings(self):
        errors = [
            'broken Markdown heading link in docs/master-plan.md: '
            'acceptance/evidence.md#voice-deployment-configuration-repair-2026-10-29',
            'broken Markdown heading link in docs/master-plan.md: '
            'acceptance/evidence.md#zoe-rename-2026-10-28',
            'broken Markdown heading link in docs/other.md: #missing',
        ]
        warnings, remaining = check_docs.pending_anchor_warnings(self.root, errors)
        self.assertEqual(len(warnings), 2)
        self.assertEqual(remaining, [errors[-1]])

    def test_non_english_narrative_fails(self):
        (self.root / 'index.md').write_text('中文叙述\n')
        self.assertTrue(any('index.md' in error for error in check_docs.cjk_check(self.root)))


if __name__ == '__main__':
    unittest.main()
