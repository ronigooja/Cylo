from __future__ import annotations

import logging
import time
from typing import Any

from .config import WorkerConfig
from .control_api import ControlAPI, ControlAPIError
from .embedding import LocalEmbedder
from .mem0_backend import ControlledMem0
from .stores import QdrantIndex

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
        self.api = ControlAPI(config.control_url, config.runner_token)
        self.embedder = LocalEmbedder(config.embedder_model, config.embedder_revision, config.embedding_dimensions)
        self.qdrant = QdrantIndex(
            config.qdrant_url, config.qdrant_api_key, config.qdrant_collection, config.embedding_dimensions
        )
        self.mem0 = ControlledMem0(config, self.embedder, self.api)

    def _validate(self, job: dict[str, Any]) -> bool:
        validation = self.api.validate(job)
        if validation.get("allowed"):
            return True
        LOG.info("job skipped by current authority validation: kind=%s disposition=%s", job.get("kind"), validation.get("disposition", "stale"))
        return False

    def _process(self, job: dict[str, Any]) -> None:
        kind = job.get("kind")
        if kind == "index":
            if not self._validate(job):
                return
            self.qdrant.upsert_claim(job, self.embedder)
            return
        if kind == "extract":
            if not self._validate(job):
                return
            candidates = self.mem0.extract(job)
            for candidate in candidates:
                if not self._validate(job):
                    return
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
            return
        if kind == "delete_cleanup":
            if not self._validate(job):
                return
            target = str(job.get("target", ""))
            if target == "qdrant":
                self.qdrant.delete_generation(job["workspace_id"], job["entity_id"], int(job["entity_version"]))
                return
            if target == "mem0":
                self.mem0.delete_claim(job["workspace_id"], job["entity_id"])
                return
            raise PermanentJobError("adapter_missing")
        raise PermanentJobError("unsupported_job_kind")

    def run_job(self, job: dict[str, Any]) -> None:
        try:
            self._process(job)
        except PermanentJobError as exc:
            self.api.fail(job, exc.code, permanent=True)
            return
        except ControlAPIError as exc:
            if exc.status == 409:
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


def main() -> None:
    configure_logging()
    MemoryWorker(WorkerConfig.from_env()).run_forever()


if __name__ == "__main__":
    main()
