import tempfile
import unittest
from pathlib import Path

from cylo_restore import restore_managed_paths


class RestoreManagedPathTests(unittest.TestCase):
    def test_absolute_root_id_cannot_mutate_outside_file(self):
        with tempfile.TemporaryDirectory() as temporary:
            base = Path(temporary)
            payload = base / "payload"
            staged = base / "staged"
            payload.mkdir()
            staged.mkdir()
            outside = base / "outside.txt"
            outside.write_text("protected", encoding="utf-8")
            outside.chmod(0o640)
            root_id = str(base / "escape")
            manifest = {
                "roots": {root_id: str(base)},
                "managed_paths": [
                    {"role": "service_config_paths", "root_id": root_id, "path": "outside.txt"}
                ],
            }

            with self.assertRaisesRegex(RuntimeError, "unsafe managed root ID"):
                restore_managed_paths(payload, staged, manifest)

            self.assertEqual(outside.read_text(encoding="utf-8"), "protected")
            self.assertEqual(outside.stat().st_mode & 0o777, 0o640)


if __name__ == "__main__":
    unittest.main()
