from __future__ import annotations

import os
import sqlite3
from urllib.parse import urlparse
from typing import Any

# Cylo is self-hosted and source text may contain private user data. Mem0's
# optional anonymous telemetry is disabled before importing its module.
os.environ["MEM0_TELEMETRY"] = "false"

from mem0 import Memory

from .config import WorkerConfig
from .responses_llm import register_responses_provider
from .stores import mem0_user_id


class ControlledMem0:
    def __init__(self, config: WorkerConfig):
        qdrant_address = urlparse(config.qdrant_url)
        if not qdrant_address.hostname:
            raise ValueError("CYLO_QDRANT_URL must include a host")
        vector_config: dict[str, Any] = {
            "collection_name": config.mem0_collection,
            "embedding_model_dims": config.embedding_dimensions,
            "host": qdrant_address.hostname,
            "port": qdrant_address.port or (443 if qdrant_address.scheme == "https" else 6333),
            "on_disk": True,
        }
        if config.qdrant_api_key:
            vector_config["api_key"] = config.qdrant_api_key
        embedder_config = {
            "provider": "fastembed",
            "config": {
                "model": config.embedder_model,
                "embedding_dims": config.embedding_dimensions,
            },
        }
        llm_config: dict[str, Any]
        if config.llm_provider == "ollama":
            llm_config = {
                "provider": "ollama",
                "config": {
                    "model": config.llm_model,
                    "ollama_base_url": config.llm_base_url or "http://127.0.0.1:11434",
                    "temperature": 0.1,
                },
            }
        else:
            register_responses_provider()
            llm_config = {
                "provider": "openai",
                "config": {
                    "model": config.llm_model,
                    "openai_base_url": config.llm_base_url,
                    "api_key": config.llm_api_key,
                    "temperature": 0.1,
                },
            }
        self.memory = Memory.from_config(
            {
                "version": "v1.1",
                "vector_store": {"provider": "qdrant", "config": vector_config},
                "embedder": embedder_config,
                "llm": llm_config,
                "history_db_path": ":memory:",
                "custom_instructions": (
                    "Extract only stable facts directly supported by the current source. "
                    "Never treat retrieved memories as evidence for a new fact. Do not infer preferences or identity."
                ),
            }
        )

    def extract(self, job: dict[str, Any]) -> list[dict[str, str]]:
        claim = job.get("claim") or {}
        source = job.get("source") or {}
        workspace = str(job["workspace_id"])
        user_id = mem0_user_id(workspace, str(job["entity_id"]))
        source_text = str(source.get("extractor_input", "")).strip()
        if not source_text:
            return []
        # Explicit recall keeps Mem0's retrieval path exercised. The add call
        # also uses Mem0's own retrieval and update workflow for the same user.
        self.memory.search(source_text, filters={"user_id": user_id}, top_k=5)
        result = self.memory.add(
            source_text,
            user_id=user_id,
            metadata={
                "workspace_id": workspace,
                "claim_id": str(job["entity_id"]),
                "claim_revision": int(job["entity_version"]),
                "source_id": str(source.get("id", "")),
                "source_version": int(source.get("version", 0)),
            },
            infer=True,
            prompt=(
                "Extract only concise facts that the CURRENT SOURCE text supports. "
                "Use recalled memories only to recognize duplicates or updates; never use them as evidence. "
                "Return facts as short declarative statements.\nCURRENT SOURCE:\n" + source_text
            ),
        )
        candidates: list[dict[str, str]] = []
        for item in result.get("results", []):
            if not isinstance(item, dict) or item.get("event") not in {"ADD", "UPDATE"}:
                continue
            content = str(item.get("memory", "")).strip()
            if content:
                candidates.append(
                    {
                        "subject": str(claim.get("subject", "source"))[:200] or "source",
                        "key": str(claim.get("key", "derived_fact"))[:200] or "derived_fact",
                        "content": content,
                    }
                )
        return candidates

    def delete_claim(self, workspace: str, claim_id: str) -> None:
        user_id = mem0_user_id(workspace, claim_id)
        filters = {"user_id": user_id}

        # Mem0's delete_all removes vector records but intentionally records
        # DELETE events in its history database. Capture only this claim's IDs
        # first so we can erase its history after Mem0 has deleted its vectors.
        # The same claim-scoped user ID is the session scope for source
        # messages, so those rows can be removed without touching other claims.
        memory_ids: set[str] = set()
        seen_batches: set[tuple[str, ...]] = set()
        while True:
            memories = self.memory.vector_store.list(filters=filters, top_k=100)[0]
            if not memories:
                break
            batch_ids = tuple(sorted(str(memory.id) for memory in memories))
            if batch_ids in seen_batches:
                raise RuntimeError("Mem0 returned a repeated claim-scoped delete batch")
            seen_batches.add(batch_ids)
            memory_ids.update(batch_ids)
            for memory in memories:
                self.memory.delete(str(memory.id))

        # Mem0 2.2.x has no scoped history/messages purge API. Keep this
        # compatibility shim narrow: only IDs found under the deterministic
        # claim user ID and the exact matching session_scope are removed.
        db = self.memory.db
        with db._lock:
            try:
                db.connection.execute("BEGIN")
                if memory_ids:
                    db.connection.executemany(
                        "DELETE FROM history WHERE memory_id = ?",
                        ((memory_id,) for memory_id in memory_ids),
                    )
                db.connection.execute(
                    "DELETE FROM messages WHERE session_scope = ?",
                    (f"user_id={user_id}",),
                )
                db.connection.execute("COMMIT")
            except sqlite3.Error:
                db.connection.execute("ROLLBACK")
                raise
