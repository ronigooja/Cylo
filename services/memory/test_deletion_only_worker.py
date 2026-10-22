from __future__ import annotations

import os
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

from .config import WorkerConfig
from .control_api import ControlAPI
from .worker import DeletionOnlyAdapters, MemoryWorker, PermanentJobError


class DeletionOnlyWorkerTests(unittest.TestCase):
    def config(self) -> WorkerConfig:
        return WorkerConfig(
            control_url="http://control.invalid",
            runner_token="fixture-token",
            qdrant_url="http://qdrant.invalid",
            qdrant_api_key=None,
            qdrant_collection="qdrant-fixture",
            mem0_collection="mem0-fixture",
            embedder_model="unused",
            embedder_revision="unused",
            llm_provider="openai",
            llm_model="",
            llm_base_url=None,
            llm_api_key=None,
            poll_seconds=0.1,
            batch_size=1,
            extractor_version="fixture",
            deletion_only=True,
        )

    def test_environment_mode_does_not_require_or_load_model_configuration(self) -> None:
        env = {
            "CYLO_CONTROL_URL": "http://control.invalid",
            "CYLO_RUNNER_TOKEN": "fixture-token",
            "CYLO_MEMORY_DELETION_ONLY": "1",
        }
        with patch.dict(os.environ, env, clear=True), patch("services.memory.worker.LocalEmbedder") as embedder, patch(
            "services.memory.worker.ControlledMem0"
        ) as mem0:
            config = WorkerConfig.from_env()
            worker = MemoryWorker(config)

        self.assertTrue(config.deletion_only)
        self.assertEqual(config.llm_model, "")
        self.assertIsNone(worker.embedder)
        self.assertIsNone(worker.qdrant)
        self.assertIsNone(worker.mem0)
        embedder.assert_not_called()
        mem0.assert_not_called()

    def test_control_api_selects_only_deletion_routes_in_maintenance_mode(self) -> None:
        api = ControlAPI("http://control.invalid", "fixture-token", deletion_only=True)
        with patch.object(api, "_post", return_value={"items": []}) as post:
            self.assertEqual(api.claim(2), [])

        self.assertEqual(post.call_args_list[0].args[0], "/runner/v1/memory/deletion-jobs/claim")

    def test_unsupported_cleanup_target_is_left_as_adapter_missing(self) -> None:
        class FakeAPI:
            failed: tuple[dict, str, bool] | None = None

            def validate(self, job: dict) -> dict:
                return {"allowed": True}

            def fail(self, job: dict, code: str, permanent: bool = False) -> dict:
                self.failed = (job, code, permanent)
                return {}

            def complete(self, job: dict) -> dict:
                raise AssertionError("unsupported target must not be completed")

        worker = MemoryWorker.__new__(MemoryWorker)
        worker.config = self.config()
        worker.api = FakeAPI()
        worker.deletion_adapters = None
        worker.run_job({"id": "cleanup-1", "kind": "delete_cleanup", "target": "export_temp"})

        self.assertIsNotNone(worker.api.failed)
        self.assertEqual(worker.api.failed[1:], ("adapter_missing", True))

    def test_missing_mem0_history_store_fails_closed(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            adapter = DeletionOnlyAdapters.__new__(DeletionOnlyAdapters)
            class FakeQdrant:
                deletes = 0

                def delete(self, **kwargs) -> None:
                    self.deletes += 1

            adapter.client = FakeQdrant()
            adapter.mem0_collection = "mem0-fixture"
            adapter.history_path = Path(directory) / "missing-history.sqlite"

            class FakeAPI:
                failed: tuple[dict, str, bool] | None = None

                def validate(self, job: dict) -> dict:
                    return {"allowed": True}

                def fail(self, job: dict, code: str, permanent: bool = False) -> dict:
                    self.failed = (job, code, permanent)
                    return {}

                def complete(self, job: dict) -> dict:
                    raise AssertionError("missing history store must not complete cleanup")

            worker = MemoryWorker.__new__(MemoryWorker)
            worker.config = self.config()
            worker.api = FakeAPI()
            worker.deletion_adapters = adapter
            worker.run_job({
                "id": "cleanup-mem0", "attempt": 1, "kind": "delete_cleanup", "target": "mem0",
                "workspace_id": "workspace", "entity_id": "claim", "entity_version": 2,
            })

            self.assertEqual(adapter.client.deletes, 1)
            self.assertIsNotNone(worker.api.failed)
            self.assertEqual(worker.api.failed[1:], ("adapter_missing", True))


if __name__ == "__main__":
    unittest.main()
