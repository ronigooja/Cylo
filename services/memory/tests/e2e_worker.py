"""Opt-in subprocess driver for the Go/Python memory worker acceptance test."""

from __future__ import annotations

import json
import os
import sys
import threading
import traceback
from collections.abc import Callable
from pathlib import Path
from typing import Any

from qdrant_client import models

sys.path.insert(0, str(Path(__file__).resolve().parents[3]))

from services.memory.config import WorkerConfig
from services.memory.control_api import ControlAPIError
from services.memory.stores import mem0_user_id
from services.memory.worker import MemoryWorker


def write_message(value: dict[str, Any], lock: threading.Lock) -> None:
    with lock:
        sys.stdout.write(json.dumps(value, separators=(",", ":")) + "\n")
        sys.stdout.flush()


def run() -> int:
    output_lock = threading.Lock()
    try:
        config = WorkerConfig.from_env()
        worker = MemoryWorker(config)
    except Exception as exc:
        write_message({"event": "error", "error_type": type(exc).__name__}, output_lock)
        return 1
    diagnostics: dict[str, list[dict[str, Any]]] = {
        "llm_responses": [],
        "extract_results": [],
        "proposal_results": [],
        "model_call_reservations": [],
    }
    original_reserve = worker.api.reserve_model_call

    def inspect_reserve(*args: Any, **kwargs: Any) -> dict[str, Any]:
        diagnostics["model_call_reservations"].append({
            "provider": str(args[3]),
            "model": str(args[4]),
            "estimated_input_tokens": int(args[5]),
            "max_output_tokens": int(args[6]),
        })
        return original_reserve(*args, **kwargs)

    worker.api.reserve_model_call = inspect_reserve
    original_generate = worker.mem0.memory.llm.generate_response

    def inspect_response(*args: Any, **kwargs: Any) -> Any:
        result = original_generate(*args, **kwargs)
        info: dict[str, Any] = {"type": type(result).__name__, "nonempty": bool(result)}
        if isinstance(result, str):
            try:
                decoded = json.loads(result)
                memories = decoded.get("memory") if isinstance(decoded, dict) else None
                info["json_valid"] = isinstance(decoded, dict)
                info["memory_items"] = len(memories) if isinstance(memories, list) else None
            except (TypeError, ValueError):
                info["json_valid"] = False
                info["memory_items"] = None
        diagnostics["llm_responses"].append(info)
        return result

    worker.mem0.memory.llm.generate_response = inspect_response
    original_extract: Callable[[dict[str, Any]], list[dict[str, str]]] = worker.mem0.extract

    def inspect_extract(job: dict[str, Any]) -> list[dict[str, str]]:
        try:
            candidates = original_extract(job)
        except Exception as exc:
            diagnostics["extract_results"].append({
                "error_type": type(exc).__name__,
                "frames": [frame.name for frame in traceback.extract_tb(exc.__traceback__)],
                "attribute": getattr(exc, "name", None),
                "control_error": _safe_control_error(exc),
            })
            raise
        diagnostics["extract_results"].append({"candidate_count": len(candidates)})
        return candidates

    worker.mem0.extract = inspect_extract
    original_propose = worker.api.propose

    def inspect_propose(proposal: dict[str, Any]) -> dict[str, Any]:
        try:
            result = original_propose(proposal)
        except Exception as exc:
            diagnostics["proposal_results"].append({"result": "error", "error_type": type(exc).__name__})
            raise
        diagnostics["proposal_results"].append({"result": "accepted"})
        return result

    worker.api.propose = inspect_propose
    tracked_memory_ids: dict[tuple[str, str], set[str]] = {}
    race_release = threading.Event()
    race_thread: threading.Thread | None = None
    race_completion: list[dict[str, Any]] = []

    def process(limit: int) -> dict[str, Any]:
        jobs = worker.api.claim(limit)
        kinds = []
        for job in jobs:
            kinds.append(str(job.get("kind", "")))
            worker.run_job(job)
            if job.get("kind") == "extract":
                key = (str(job["workspace_id"]), str(job["entity_id"]))
                uid = mem0_user_id(*key)
                memories = worker.mem0.memory.vector_store.list(filters={"user_id": uid}, top_k=1000)[0]
                tracked_memory_ids.setdefault(key, set()).update(str(item.id) for item in memories)
        return {
            "claimed": len(jobs),
            "kinds": kinds,
            "llm_responses": diagnostics["llm_responses"],
            "extract_results": diagnostics["extract_results"],
            "proposal_results": diagnostics["proposal_results"],
            "model_call_reservations": diagnostics["model_call_reservations"],
        }

    def state(workspace: str, claim_id: str) -> dict[str, int]:
        key = (workspace, claim_id)
        uid = mem0_user_id(workspace, claim_id)
        current = worker.mem0.memory.vector_store.list(filters={"user_id": uid}, top_k=1000)[0]
        db = worker.mem0.memory.db.connection
        tracked = tracked_memory_ids.get(key, set()) | {str(item.id) for item in current}
        tracked.update(
            str(row[0])
            for row in db.execute("SELECT memory_id FROM cylo_memory_ownership WHERE user_id=?", (uid,)).fetchall()
        )
        tracked_memory_ids.setdefault(key, set()).update(tracked)
        if tracked:
            placeholders = ",".join("?" for _ in tracked)
            history_count = db.execute(
                f"SELECT count(*) FROM history WHERE memory_id IN ({placeholders})", tuple(tracked)
            ).fetchone()[0]
        else:
            history_count = 0
        message_count = db.execute(
            "SELECT count(*) FROM messages WHERE session_scope = ?", (f"user_id={uid}",)
        ).fetchone()[0]
        query_filter = models.Filter(
            must=[
                models.FieldCondition(key="workspace_id", match=models.MatchValue(value=workspace)),
                models.FieldCondition(key="claim_id", match=models.MatchValue(value=claim_id)),
            ]
        )
        qdrant_count = worker.qdrant.client.count(
            collection_name=worker.qdrant.collection, count_filter=query_filter, exact=True
        ).count
        return {"mem0_vectors": len(current), "history": int(history_count), "messages": int(message_count), "qdrant_vectors": int(qdrant_count)}

    write_message({"event": "ready"}, output_lock)
    for line in sys.stdin:
        try:
            request = json.loads(line)
            action = request.get("action")
            if action == "process":
                response = process(int(request.get("limit", 100)))
                write_message({"event": "processed", **response}, output_lock)
            elif action == "state":
                write_message({"event": "state", **state(str(request["workspace_id"]), str(request["claim_id"]))}, output_lock)
            elif action == "race_start":
                if race_thread is not None and race_thread.is_alive():
                    raise RuntimeError("a race is already active")
                jobs = worker.api.claim(1)
                if len(jobs) != 1 or jobs[0].get("kind") != "index":
                    raise RuntimeError("expected one index job for the stale-effect race")
                job = jobs[0]
                original_upsert = worker.qdrant.upsert_claim
                original_complete = worker.api.complete

                def gated_upsert(current_job: dict[str, Any], embedder: Any) -> None:
                    write_message({"event": "race_effect_validated", "job_id": job["id"]}, output_lock)
                    if not race_release.wait(timeout=90):
                        raise TimeoutError("stale-effect race release timed out")
                    original_upsert(current_job, embedder)

                def capture_complete(current_job: dict[str, Any]) -> dict[str, Any]:
                    result = original_complete(current_job)
                    race_completion.append(result)
                    return result

                worker.qdrant.upsert_claim = gated_upsert
                worker.api.complete = capture_complete

                def run_race_job() -> None:
                    try:
                        worker.run_job(job)
                    finally:
                        worker.qdrant.upsert_claim = original_upsert
                        worker.api.complete = original_complete

                race_release.clear()
                race_thread = threading.Thread(target=run_race_job, name="memory-e2e-stale-index")
                race_thread.start()
            elif action == "race_release":
                if race_thread is None or not race_thread.is_alive():
                    raise RuntimeError("no stale-effect race is waiting")
                race_release.set()
                race_thread.join(timeout=90)
                if race_thread.is_alive():
                    raise TimeoutError("stale-effect race did not finish")
                write_message({"event": "race_complete", "completion": race_completion[-1] if race_completion else {}}, output_lock)
            elif action == "stop":
                if race_thread is not None and race_thread.is_alive():
                    race_release.set()
                    race_thread.join(timeout=5)
                worker.mem0.memory.db.close()
                write_message({"event": "stopped"}, output_lock)
                return 0
            else:
                raise ValueError("unsupported E2E action")
        except Exception as exc:  # Never echo request values, endpoint, or credentials on errors.
            write_message({"event": "error", "error_type": type(exc).__name__}, output_lock)
    return 0


def _safe_control_error(exc: BaseException) -> dict[str, Any] | None:
    seen: set[int] = set()
    current: BaseException | None = exc
    while current is not None and id(current) not in seen:
        seen.add(id(current))
        if isinstance(current, ControlAPIError):
            return {"status": current.status, "code": current.code}
        current = current.__cause__
    return None


if __name__ == "__main__":
    raise SystemExit(run())
