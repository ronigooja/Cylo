from __future__ import annotations

import unittest
from types import SimpleNamespace

from mem0.memory.storage import SQLiteManager

from .mem0_backend import ControlledMem0
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
    def test_cleanup_removes_only_claim_vectors_history_and_messages(self) -> None:
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
        db = SQLiteManager(":memory:")
        try:
            for item in memories:
                db.add_history(item.id, None, item.payload["data"], "ADD")
            db.save_messages([{"role": "user", "content": "target source"}], f"user_id={target_user}")
            db.save_messages([{"role": "user", "content": "other claim source"}], f"user_id={other_claim_user}")
            db.save_messages([{"role": "user", "content": "other workspace source"}], f"user_id={other_workspace_user}")

            backend = ControlledMem0.__new__(ControlledMem0)
            backend.memory = _Memory(memories, db)
            backend.delete_claim(workspace, claim_id)

            self.assertEqual([item.id for item in memories], ["other-claim-memory", "other-workspace-memory"])
            rows = db.connection.execute("SELECT memory_id FROM history ORDER BY memory_id").fetchall()
            self.assertEqual(rows, [("other-claim-memory",), ("other-workspace-memory",)])
            message_scopes = db.connection.execute("SELECT session_scope FROM messages ORDER BY session_scope").fetchall()
            self.assertEqual(
                message_scopes,
                [(f"user_id={other_claim_user}",), (f"user_id={other_workspace_user}",)],
            )
        finally:
            db.close()


if __name__ == "__main__":
    unittest.main()
