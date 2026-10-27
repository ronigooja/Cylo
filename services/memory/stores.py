from __future__ import annotations

import hashlib
import uuid
from typing import Any

from qdrant_client import QdrantClient, models

from .config import MODEL_CODEC


class QdrantIndex:
    def __init__(self, url: str, api_key: str | None, collection: str, dimensions: int):
        if MODEL_CODEC not in collection:
            raise ValueError(f"Qdrant collection name must include pinned codec {MODEL_CODEC}")
        self.client = QdrantClient(url=url, api_key=api_key, timeout=15.0)
        self.collection = collection
        self.dimensions = dimensions
        if not self.client.collection_exists(collection):
            self.client.create_collection(
                collection_name=collection,
                vectors_config=models.VectorParams(size=dimensions, distance=models.Distance.COSINE),
            )
        info = self.client.get_collection(collection)
        vectors = info.config.params.vectors
        if isinstance(vectors, dict):
            raise ValueError("named vectors are not supported by the Zoe authority index")
        if vectors.size != dimensions:
            raise ValueError(f"Qdrant collection dimension mismatch: expected {dimensions}, got {vectors.size}")

    def upsert_claim(self, job: dict[str, Any], embedder: Any) -> None:
        claim = job.get("claim") or {}
        text = str(claim.get("content", ""))
        point_id = str(uuid.uuid5(uuid.NAMESPACE_URL, f"zoe:{job['workspace_id']}:{job['entity_id']}"))
        payload = {
            "workspace_id": job["workspace_id"],
            "claim_id": job["entity_id"],
            "revision": int(job["entity_version"]),
            "authority_kind": claim.get("authority_kind", ""),
            "scope_kind": (claim.get("scope") or {}).get("kind", ""),
            "scope_id": (claim.get("scope") or {}).get("id", ""),
            "principal_id": (claim.get("scope") or {}).get("principal_id", ""),
            "content_hash": claim.get("content_hash", ""),
            "source_ids": claim.get("source_ids", []),
            "source_versions": claim.get("source_versions", []),
        }
        self.client.upsert(
            collection_name=self.collection,
            points=[models.PointStruct(id=point_id, vector=embedder.encode(text), payload=payload)],
            wait=True,
        )

    def delete_generation(self, workspace: str, claim_id: str, generation: int) -> None:
        self.client.delete(
            collection_name=self.collection,
            points_selector=models.FilterSelector(
                filter=models.Filter(
                    must=[
                        models.FieldCondition(key="workspace_id", match=models.MatchValue(value=workspace)),
                        models.FieldCondition(key="claim_id", match=models.MatchValue(value=claim_id)),
                        models.FieldCondition(key="revision", range=models.Range(lte=generation)),
                    ]
                )
            ),
            wait=True,
        )


def mem0_user_id(workspace: str, claim_id: str) -> str:
    digest = hashlib.sha256(f"{workspace}\x00{claim_id}".encode("utf-8")).hexdigest()[:32]
    return f"zoe-{digest}"
