"""Regression checks for CI omission boundaries, run on the hosted runner."""
import unittest
from ci_scope import KEYS, select


class ScopeTests(unittest.TestCase):
    def selected(self, paths):
        return {key for key, value in select(paths).items() if value}

    def test_docs_do_not_build(self):
        self.assertEqual(self.selected(["README.md", "docs/design/reference.png"]), {"docs"})

    def test_platform_specific_changes(self):
        self.assertEqual(self.selected(["clients/apple/Apps/macOS/CyloMacApp.swift"]), {"macos"})
        self.assertEqual(self.selected(["clients/apple/Apps/iOS/CyloMobileApp.swift"]), {"ios"})

    def test_shared_apple_changes(self):
        for path in ("clients/apple/Packages/CyloKit/Package.swift", "clients/apple/Cylo.xcodeproj/project.pbxproj", "clients/apple/Apps/Resources/en.lproj/InfoPlist.strings"):
            with self.subTest(path=path):
                self.assertEqual(self.selected([path]), {"tests", "macos", "ios"})

    def test_go_and_shared_contract(self):
        self.assertEqual(self.selected(["internal/runner/engine.go"]), {"go", "race"})
        for path in ("contracts/http.go", "internal/control/views.go", "internal/voice/schema.go", "go.mod", "tests/integration/example_test.go"):
            self.assertEqual(self.selected([path]), {"go", "race", "tests", "macos", "ios"})

    def test_scripts_and_mixed_changes(self):
        self.assertEqual(self.selected(["deploy/linux/install-cli.sh"]), {"scripts"})
        self.assertEqual(self.selected(["README.md", "internal/runner/engine.go"]), {"docs", "go", "race"})

    def test_unknown_and_ci_changes_expand(self):
        for path in ("new-component/file", "services/memory/worker.py", ".github/workflows/apple.yml", "tools/ci_scope.py"):
            self.assertEqual(self.selected([path]), set(KEYS))

    def test_full_and_empty(self):
        self.assertTrue(all(select([], full=True).values()))
        self.assertFalse(any(select([]).values()))


if __name__ == "__main__":
    unittest.main()
