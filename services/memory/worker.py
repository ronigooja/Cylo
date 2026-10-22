from __future__ import annotations

import logging
import os
import sqlite3
import time
from pathlib import Path
from typing import Any

from qdrant_client import QdrantClient, models

from .config import WorkerConfig
from .control_api import ControlAPI, ControlAPIError
from .embedding import LocalEmbedder
from .mem0_backend import ControlledMem0
from .stores import QdrantIndex, mem0_user_id

LOG = logging.getLogger("cylo.memory.worker")


class Mem0ContentLogFilter(logging.Filter):
    """Mem0 2.2 logs memory contents at INFO and can echo them on failures."""

    def filter(self, record: logging.LogRecord) -> bool:
        return not record.name.startswith("mem0")


def configure_logging() -> None:
    logging.basicConfig(level=logging.INFO, format="%(asctime)s %(levelname)s %(name)s %(message)s")
    for handler in logging.getLogger().handlers:
        if not any(isinstance(item, Mem0ContentLogFilter) for item in handler.filters):
            handler.addFilter(Mem0ContentLogFilter())


class MemoryWorker:
    def __init__(self, config: WorkerConfig):
        self.config = config
        self.api = ControlAPI(config.control_url, config.runner_token, deletion_only=config.deletion_only)
        if config.deletion_only:
            self.embedder = None
            self.qdrant = None
            self.mem0 = None
            self.deletion_adapters = DeletionOnlyAdapters(config)
        else:
            self.embedder = LocalEmbedder(config.embedder_model, config.embedder_revision, config.embedding_dimensions)
            self.qdrant = QdrantIndex(
                config.qdrant_url, config.qdrant_api_key, config.qdrant_collection, config.embedding_dimensions
            )
            self.mem0 = ControlledMem0(config, self.embedder, self.api)
            self.deletion_adapters = None

    def _validate(self, job: dict[str, Any]) -> bool:
        validation = self.api.validate(job)
        if validation.get("allowed"):
            return True
        LOG.info("job skipped by current authority validation: kind=%s disposition=%s", job.get("kind"), validation.get("disposition", "stale"))
        return False

    def _process(self, job: dict[str, Any]) -> bool:
        kind = job.get("kind")
        if self.config.deletion_only:
            if kind != "delete_cleanup":
                raise PermanentJobError("unsupported_deletion_job")
            target = job.get("target")
            if target in ("qdrant", "mem0"):
                if not self._validate(job):
                    return False
                self.deletion_adapters.delete(job)
            elif target == "export_temp":
                self.api.cleanup_export_temp(job)
                return True
            elif target == "backup_delete_manifest":
                self.api.cleanup_backup_delete_manifest(job)
                return True
            else:
                raise PermanentJobError("adapter_missing")
            return False
        if kind == "delete_cleanup" and job.get("target") in ("export_temp", "backup_delete_manifest"):
            # These server-side cleanup adapters persist their own completion and
            # checkpoint after performing the trusted cleanup action.
            if job["target"] == "export_temp":
                self.api.cleanup_export_temp(job)
            else:
                self.api.cleanup_backup_delete_manifest(job)
            return True
        if kind == "index":
            if not self._validate(job):
                return False
            self.qdrant.upsert_claim(job, self.embedder)
            return False
        if kind == "extract":
            if not self._validate(job):
                return False
            candidates = self.mem0.extract(job)
            for candidate in candidates:
                if not self._validate(job):
                    return False
                claim = job.get("claim") or {}
                source = job.get("source") or {}
                self.api.propose(
                    {
                        "workspace_id": job["workspace_id"],
                        "source_id": source["id"],
                        "source_version": int(source["version"]),
                        "extractor_version": self.config.extractor_version,
                        "scope": claim["scope"],
                        "subject": candidate["subject"],
                        "key": candidate["key"],
                        "content": candidate["content"],
                    }
                )
            return False
        if kind == "delete_cleanup":
            if not self._validate(job):
                return False
            target = str(job.get("target", ""))
            if target == "qdrant":
                self.qdrant.delete_generation(job["workspace_id"], job["entity_id"], int(job["entity_version"]))
                return False
            if target == "mem0":
                self.mem0.delete_claim(job["workspace_id"], job["entity_id"])
                return False
            raise PermanentJobError("adapter_missing")
        raise PermanentJobError("unsupported_job_kind")

    def run_job(self, job: dict[str, Any]) -> None:
        try:
            completed_by_adapter = self._process(job)
        except PermanentJobError as exc:
            self.api.fail(job, exc.code, permanent=True)
            return
        except ControlAPIError as exc:
            if exc.status == 409:
                return
            if exc.status == 202:
                self.api.fail(job, exc.code, permanent=False)
                return
            if exc.code == "adapter_missing":
                self.api.fail(job, exc.code, permanent=True)
                return
            if exc.status >= 500 and exc.code not in ("http_error", "control_unavailable"):
                self.api.fail(job, exc.code, permanent=False)
                return
            self.api.fail(job, "control_unavailable" if exc.status >= 500 else "control_rejected")
            return
        except Exception as exc:
            LOG.error(
                "memory job failed: kind=%s target=%s error_type=%s",
                job.get("kind"), job.get("target"), type(exc).__name__,
            )
            self.api.fail(job, "derived_store_error")
            return
        if not completed_by_adapter:
            self.api.complete(job)

    def run_forever(self) -> None:
        while True:
            try:
                jobs = self.api.claim(self.config.batch_size)
                for job in jobs:
                    self.run_job(job)
                if not jobs:
                    time.sleep(self.config.poll_seconds)
            except ControlAPIError as exc:
                LOG.warning("control polling failed: status=%s code=%s", exc.status, exc.code)
                time.sleep(self.config.poll_seconds)


class PermanentJobError(RuntimeError):
    def __init__(self, code: str):
        super().__init__(code)
        self.code = code


class DeletionOnlyAdapters:
    """External delete-only adapters; construction performs no reads or writes."""

    def __init__(self, config: WorkerConfig):
        self.client = QdrantClient(url=config.qdrant_url, api_key=config.qdrant_api_key, timeout=15.0)
        self.qdrant_collection = config.qdrant_collection
        self.mem0_collection = config.mem0_collection
        self.history_path = Path(os.environ.get("CYLO_MEM0_HISTORY_DB", "/var/lib/cylo/memory/mem0-history.sqlite"))

    def delete(self, job: dict[str, Any]) -> None:
        workspace = str(job["workspace_id"])
        claim_id = str(job["entity_id"])
        target = str(job["target"])
        if target == "qdrant":
            self.client.delete(
                collection_name=self.qdrant_collection,
                points_selector=models.FilterSelector(
                    filter=models.Filter(
                        must=[
                            models.FieldCondition(key="workspace_id", match=models.MatchValue(value=workspace)),
                            models.FieldCondition(key="claim_id", match=models.MatchValue(value=claim_id)),
                            models.FieldCondition(key="revision", range=models.Range(lte=int(job["entity_version"]))),
                        ]
                    )
                ),
                wait=True,
            )
            return
        if target != "mem0":
            raise PermanentJobError("adapter_missing")
        user_id = mem0_user_id(workspace, claim_id)
        self.client.delete(
            collection_name=self.mem0_collection,
            points_selector=models.FilterSelector(
                filter=models.Filter(must=[models.FieldCondition(key="user_id", match=models.MatchValue(value=user_id))])
            ),
            wait=True,
        )
        self._purge_mem0_history(user_id, workspace, claim_id)

    def _purge_mem0_history(self, user_id: str, workspace: str, claim_id: str) -> None:
        if not self.history_path.is_file():
            raise PermanentJobError("adapter_missing")
        connection = sqlite3.connect(self.history_path, timeout=5.0)
        try:
            tables = {row[0] for row in connection.execute("SELECT name FROM sqlite_master WHERE type='table'")}
            if not {"cylo_memory_ownership", "history", "messages"} <= tables:
                raise PermanentJobError("adapter_missing")
            connection.execute("BEGIN IMMEDIATE")
            memory_ids = [
                row[0]
                for row in connection.execute(
                    "SELECT memory_id FROM cylo_memory_ownership WHERE user_id=? AND workspace_id=? AND claim_id=?",
                    (user_id, workspace, claim_id),
                )
            ]
            if memory_ids:
                connection.executemany("DELETE FROM history WHERE memory_id=?", ((item,) for item in memory_ids))
            connection.execute("DELETE FROM messages WHERE session_scope=?", (f"user_id={user_id}",))
            connection.execute(
                "DELETE FROM cylo_memory_ownership WHERE user_id=? AND workspace_id=? AND claim_id=?",
                (user_id, workspace, claim_id),
            )
            connection.commit()
        except Exception:
            connection.rollback()
            raise
        finally:
            connection.close()


def main() -> None:
    configure_logging()
    MemoryWorker(WorkerConfig.from_env()).run_forever()


if __name__ == "__main__":
    main()
