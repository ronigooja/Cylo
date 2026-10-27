from __future__ import annotations

import io
import logging
import unittest

from .worker import configure_logging


class WorkerLoggingPrivacyTests(unittest.TestCase):
    def test_mem0_content_logs_are_suppressed(self) -> None:
        stream = io.StringIO()
        handler = logging.StreamHandler(stream)
        root = logging.getLogger()
        previous_level = root.level
        root.setLevel(logging.INFO)
        root.addHandler(handler)
        try:
            configure_logging()
            logging.getLogger("mem0.memory.main").info("Updating memory with data=%r", "synthetic-private-marker")
            logging.getLogger("zoe.memory.worker").info("job status kind=%s", "extract")
        finally:
            root.removeHandler(handler)
            root.setLevel(previous_level)

        output = stream.getvalue()
        self.assertNotIn("synthetic-private-marker", output)
        self.assertIn("job status kind=extract", output)


if __name__ == "__main__":
    unittest.main()
