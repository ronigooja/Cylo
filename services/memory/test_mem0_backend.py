from __future__ import annotations

import unittest
import tempfile
from types import SimpleNamespace
from pathlib import Path

from mem0.memory.storage import SQLiteManager
from mem0.configs.embeddings.base import BaseEmbedderConfig

from .mem0_backend import ControlledMem0
from .mem0_embedder import PinnedFastEmbedEmbedding, register_pinned_embedder
from .stores import mem0_user_id


class _VectorStore:
    def __init__(self, memories: list[SimpleNamespace]):
        self.memories = memories

    def list(self, *, filters: dict[str, str], top_k: int) -> tuple[list[SimpleNamespace], int]:
        matches = [item for item in self.memories if item.payload["user_id"] == filters["user_id"]]
        return matches[:top_k], len(matches)


class _Memory:
    def __init__(self, memories: list[SimpleNamespace], db: SQLiteManager):
        self.vector_store = _VectorStore(memories)
        self.db = db

    def delete(self, memory_id: str) -> None:
        item = next(item for item in self.vector_store.memories if item.id == memory_id)
        self.db.add_history(memory_id, item.payload["data"], None, "DELETE", is_deleted=1)
        self.vector_store.memories.remove(item)


class DeleteClaimTests(unittest.TestCase):
    def test_cleanup_removes_owned_history_after_restart_without_touching_other_scopes(self) -> None:
        workspace = "workspace-a"
        claim_id = "claim-a"
        target_user = mem0_user_id(workspace, claim_id)
        other_claim_user = mem0_user_id(workspace, "claim-b")
        other_workspace_user = mem0_user_id("workspace-b", claim_id)
        memories = [
            SimpleNamespace(id="target-memory", payload={"user_id": target_user, "data": "target"}),
            SimpleNamespace(id="other-claim-memory", payload={"user_id": other_claim_user, "data": "other claim"}),
            SimpleNamespace(id="other-workspace-memory", payload={"user_id": other_workspace_user, "data": "other workspace"}),
        ]
        with tempfile.TemporaryDirectory() as temp_dir:
            db_path = str(Path(temp_dir) / "mem0-history.sqlite")
            db = SQLiteManager(db_path)
            db.connection.execute(
                """CREATE TABLE cylo_memory_ownership (
                    user_id TEXT NOT NULL, workspace_id TEXT NOT NULL, claim_id TEXT NOT NULL,
                    memory_id TEXT NOT NULL, PRIMARY KEY(user_id, memory_id)
                )"""
            )
            for item in memories:
                db.add_history(item.id, None, item.payload["data"], "ADD")
                user_id = item.payload["user_id"]
                owner_workspace, owner_claim = (workspace, claim_id) if user_id == target_user else (
                    (workspace, "claim-b") if user_id == other_claim_user else ("workspace-b", claim_id)
                )
                db.connection.execute(
                    "INSERT INTO cylo_memory_ownership(user_id,workspace_id,claim_id,memory_id) VALUES(?,?,?,?)",
                    (user_id, owner_workspace, owner_claim, item.id),
                )
                db.connection.commit()
            db.save_messages([{"role": "user", "content": "target source"}], f"user_id={target_user}")
            # Simulate a memory deleted by Mem0 inference before the tombstone.
            # Its vector is absent, but the durable ownership map remains.
            db.add_history("target-inferred-delete", "old fact", None, "DELETE", is_deleted=1)
            db.connection.execute(
                "INSERT INTO cylo_memory_ownership(user_id,workspace_id,claim_id,memory_id) VALUES(?,?,?,?)",
                (target_user, workspace, claim_id, "target-inferred-delete"),
            )
            db.connection.commit()
            db.add_history("unknown-scope-memory", None, "retain unknown history", "ADD")
            db.save_messages([{"role": "user", "content": "other claim source"}], f"user_id={other_claim_user}")
            db.save_messages([{"role": "user", "content": "other workspace source"}], f"user_id={other_workspace_user}")
            db.connection.commit()
            db.close()

            # Reopening proves the ownership mapping survives a worker restart.
            db = SQLiteManager(db_path)

            backend = ControlledMem0.__new__(ControlledMem0)
            backend.memory = _Memory(memories, db)
            backend.delete_claim(workspace, claim_id)

            self.assertEqual([item.id for item in memories], ["other-claim-memory", "other-workspace-memory"])
            rows = db.connection.execute("SELECT memory_id FROM history ORDER BY memory_id").fetchall()
            self.assertEqual(
                rows,
                [("other-claim-memory",), ("other-workspace-memory",), ("unknown-scope-memory",)],
            )
            message_scopes = db.connection.execute("SELECT session_scope FROM messages ORDER BY session_scope").fetchall()
            self.assertEqual(
                message_scopes,
                [(f"user_id={other_claim_user}",), (f"user_id={other_workspace_user}",)],
            )
            db.close()


class PinnedEmbedderTests(unittest.TestCase):
    def test_mem0_uses_the_worker_pinned_embedder_instance(self) -> None:
        class Encoder:
            dimensions = 384

            @staticmethod
            def encode(text: str) -> list[float]:
                return [float(len(text))] * 384

        encoder = Encoder()
        register_pinned_embedder(encoder)
        adapter = PinnedFastEmbedEmbedding(
            BaseEmbedderConfig(
                model="sentence-transformers/paraphrase-multilingual-MiniLM-L12-v2",
                embedding_dims=384,
            )
        )
        self.assertIs(adapter.embedder, encoder)
        self.assertEqual(adapter.embed("abc", "add"), [3.0] * 384)


if __name__ == "__main__":
    unittest.main()
