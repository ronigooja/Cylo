from __future__ import annotations

from typing import Any

from mem0.embeddings.base import EmbeddingBase
from mem0.utils.factory import EmbedderFactory

from .config import EMBEDDING_DIMENSIONS, MODEL_ID, MODEL_REVISION


_pinned_embedder: Any | None = None


def register_pinned_embedder(embedder: Any) -> None:
    global _pinned_embedder
    _pinned_embedder = embedder
    # Mem0's config schema accepts only the provider name "fastembed". Replace
    # its stock, unpinned implementation while keeping that public provider.
    EmbedderFactory.provider_to_class["fastembed"] = (
        "services.memory.mem0_embedder.PinnedFastEmbedEmbedding"
    )


class PinnedFastEmbedEmbedding(EmbeddingBase):
    """Mem0 adapter that delegates to the worker's hash-verified encoder."""

    def __init__(self, config=None):
        super().__init__(config)
        if self.config.model != MODEL_ID or self.config.embedding_dims != EMBEDDING_DIMENSIONS:
            raise ValueError("Mem0 embedding configuration does not match the pinned Cylo codec")
        if _pinned_embedder is None:
            raise RuntimeError("the pinned Cylo embedder was not registered before Mem0 initialization")
        if _pinned_embedder.dimensions != EMBEDDING_DIMENSIONS:
            raise ValueError("the pinned Cylo embedder has an unexpected dimension")
        self.embedder = _pinned_embedder
        self.revision = MODEL_REVISION

    def embed(self, text, memory_action=None):
        return self.embedder.encode(text)
