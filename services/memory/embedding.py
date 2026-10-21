from __future__ import annotations

import hashlib
import os
from pathlib import Path
from typing import Any

from huggingface_hub import snapshot_download

from .config import EMBEDDING_DIMENSIONS, MODEL_ONNX_REPOSITORY, MODEL_REVISION


# These hashes lock the ONNX graph and tokenizer to the reviewed model snapshot.
# They are checked on every worker start.
PINNED_FILES: dict[str, str] = {
    "model_optimized.onnx": "634d0f66c29dc934c8fa72b8a4fe91dd4d420a22f1d82a241058d4316e659a99",
    "tokenizer.json": "fa685fc160bbdbab64058d4fc91b60e62d207e8dc60b9af5c002c5ab946ded00",
    "config.json": "c8ec081fdad2df991bf5abbf18418fec7a5cdaa421f60ffb060a30040b8c376f",
    "tokenizer_config.json": "0666eebf692422757e1dddf3c9fb1ded73ba3dc726c5828671fc89e45bf3609f",
    "special_tokens_map.json": "8c785abebea9ae3257b61681b4e6fd8365ceafde980c21970d001e834cf10835",
}

MODEL_FILE_PATTERNS = [
    "model_optimized.onnx",
    "tokenizer.json",
    "config.json",
    "tokenizer_config.json",
    "special_tokens_map.json",
]


def pinned_snapshot(cache: Path) -> Path:
    snapshot = Path(
        snapshot_download(
            repo_id=MODEL_ONNX_REPOSITORY,
            revision=MODEL_REVISION,
            cache_dir=str(cache),
            allow_patterns=MODEL_FILE_PATTERNS,
        )
    )
    for relative_path, expected_hash in PINNED_FILES.items():
        actual_hash = hashlib.sha256((snapshot / relative_path).read_bytes()).hexdigest()
        if actual_hash != expected_hash:
            raise ValueError(f"pinned FastEmbed artifact hash mismatch: {relative_path}")
    return snapshot


class LocalEmbedder:
    """Pinned multilingual FastEmbed encoder shared by both stores."""

    def __init__(self, model_id: str, revision: str, expected_dimensions: int):
        from fastembed import TextEmbedding

        if model_id != "sentence-transformers/paraphrase-multilingual-MiniLM-L12-v2" or revision != MODEL_REVISION:
            raise ValueError("the configured embedding model does not match the pinned codec")
        cache = Path(os.environ.setdefault("FASTEMBED_CACHE_PATH", "/tmp/cylo-memory-models"))
        snapshot = pinned_snapshot(cache)
        self.model = TextEmbedding(
            model_name=model_id,
            cache_dir=str(cache),
            threads=2,
            cuda=False,
            local_files_only=True,
            specific_model_path=str(snapshot),
        )
        description = next(
            model for model in TextEmbedding.list_supported_models() if model["model"] == model_id
        )
        self.dimensions = int(description["dim"])
        if self.dimensions != expected_dimensions:
            raise ValueError(f"embedding dimension mismatch: expected {expected_dimensions}, got {self.dimensions}")

    def encode(self, text: str) -> list[float]:
        result: Any = next(self.model.embed([text]))
        vector = [float(value) for value in result.tolist()]
        if len(vector) != self.dimensions:
            raise ValueError("embedding returned unexpected dimension")
        return vector
