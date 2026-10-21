from __future__ import annotations

import os
from dataclasses import dataclass


MODEL_ID = "sentence-transformers/paraphrase-multilingual-MiniLM-L12-v2"
MODEL_REVISION = "faf4aa4225822f3bc6376869cb1164e8e3feedd0"
MODEL_ONNX_REPOSITORY = "Qdrant/paraphrase-multilingual-MiniLM-L12-v2-onnx-Q"
EMBEDDING_DIMENSIONS = 384
MODEL_CODEC = "paraphrase_multilingual_minilm_l12_384_faf4aa42"


def _required(name: str) -> str:
    value = os.environ.get(name, "").strip()
    if not value:
        raise ValueError(f"{name} is required")
    return value


@dataclass(frozen=True)
class WorkerConfig:
    control_url: str
    runner_token: str
    qdrant_url: str
    qdrant_api_key: str | None
    qdrant_collection: str
    mem0_collection: str
    embedder_model: str
    embedder_revision: str
    llm_provider: str
    llm_model: str
    llm_base_url: str | None
    llm_api_key: str | None
    poll_seconds: float
    batch_size: int
    extractor_version: str

    @classmethod
    def from_env(cls) -> "WorkerConfig":
        os.environ.setdefault(
            "FASTEMBED_CACHE_PATH",
            os.environ.get("CYLO_MEMORY_MODEL_CACHE", "/var/lib/cylo/memory/models"),
        )
        provider = os.environ.get("CYLO_MEM0_LLM_PROVIDER", "openai").strip().lower()
        if provider not in {"openai", "ollama"}:
            raise ValueError("CYLO_MEM0_LLM_PROVIDER must be openai or ollama")
        return cls(
            control_url=_required("CYLO_CONTROL_URL").rstrip("/"),
            runner_token=_required("CYLO_RUNNER_TOKEN"),
            qdrant_url=os.environ.get("CYLO_QDRANT_URL", "http://127.0.0.1:6333").rstrip("/"),
            qdrant_api_key=os.environ.get("CYLO_QDRANT_API_KEY") or None,
            qdrant_collection=os.environ.get("CYLO_QDRANT_COLLECTION", f"cylo_memory_v1_{MODEL_CODEC}"),
            mem0_collection=os.environ.get("CYLO_MEM0_COLLECTION", "cylo_mem0_v1"),
            embedder_model=MODEL_ID,
            embedder_revision=MODEL_REVISION,
            llm_provider=provider,
            llm_model=_required("CYLO_MEM0_LLM_MODEL"),
            llm_base_url=os.environ.get("CYLO_MEM0_LLM_BASE_URL") or None,
            llm_api_key=os.environ.get("CYLO_MEM0_LLM_API_KEY") or None,
            poll_seconds=max(0.1, min(float(os.environ.get("CYLO_MEMORY_POLL_SECONDS", "2")), 60.0)),
            batch_size=max(1, min(int(os.environ.get("CYLO_MEMORY_BATCH_SIZE", "10")), 100)),
            extractor_version=os.environ.get("CYLO_MEMORY_EXTRACTOR_VERSION", "mem0-controlled-v1"),
        )

    @property
    def embedding_dimensions(self) -> int:
        return EMBEDDING_DIMENSIONS
