from __future__ import annotations

import json
import urllib.error
import urllib.request
from typing import Any


class ControlAPIError(RuntimeError):
    def __init__(self, status: int, code: str):
        super().__init__(f"control API returned {status} ({code})")
        self.status = status
        self.code = code


class ControlAPI:
    def __init__(self, base_url: str, runner_token: str, timeout: float = 30.0):
        self.base_url = base_url.rstrip("/")
        self.runner_token = runner_token
        self.timeout = timeout

    def _post(self, path: str, payload: dict[str, Any]) -> dict[str, Any]:
        request = urllib.request.Request(
            self.base_url + path,
            data=json.dumps(payload, ensure_ascii=False).encode("utf-8"),
            headers={
                "Authorization": f"Bearer {self.runner_token}",
                "Content-Type": "application/json",
            },
            method="POST",
        )
        try:
            with urllib.request.urlopen(request, timeout=self.timeout) as response:
                result = json.loads(response.read().decode("utf-8"))
                return result if isinstance(result, dict) else {}
        except urllib.error.HTTPError as exc:
            code = "http_error"
            try:
                body = json.loads(exc.read().decode("utf-8"))
                code = str(body.get("error", {}).get("code", code))
            except (ValueError, AttributeError):
                pass
            raise ControlAPIError(exc.code, code) from None
        except (urllib.error.URLError, TimeoutError, OSError) as exc:
            raise ControlAPIError(503, "control_unavailable") from None

    def claim(self, limit: int) -> list[dict[str, Any]]:
        result = self._post("/runner/v1/memory/jobs/claim", {"limit": limit})
        items = result.get("items", [])
        return items if isinstance(items, list) else []

    def validate(self, job: dict[str, Any]) -> dict[str, Any]:
        return self._post(
            f"/runner/v1/memory/jobs/{job['id']}/validate", {"attempt": int(job["attempt"])}
        )

    def complete(self, job: dict[str, Any]) -> dict[str, Any]:
        return self._post(
            f"/runner/v1/memory/jobs/{job['id']}/complete", {"attempt": int(job["attempt"])}
        )

    def fail(self, job: dict[str, Any], code: str, permanent: bool = False) -> dict[str, Any]:
        return self._post(
            f"/runner/v1/memory/jobs/{job['id']}/fail",
            {"attempt": int(job["attempt"]), "error_code": code, "permanent": permanent},
        )

    def propose(self, proposal: dict[str, Any]) -> dict[str, Any]:
        return self._post("/runner/v1/memory/proposals", proposal)

    def reserve_model_call(
        self,
        job_id: str,
        attempt: int,
        request_key: str,
        provider: str,
        model: str,
        estimated_input_tokens: int,
        max_output_tokens: int,
    ) -> dict[str, Any]:
        return self._post(
            f"/runner/v1/memory/jobs/{job_id}/model-calls/reserve",
            {
                "attempt": attempt,
                "request_key": request_key,
                "provider": provider,
                "model": model,
                "estimated_input_tokens": estimated_input_tokens,
                "max_output_tokens": max_output_tokens,
            },
        )

    def finalize_model_call(
        self,
        job_id: str,
        call_id: str,
        attempt: int,
        outcome: str,
        usage_status: str,
        input_tokens: int | None = None,
        output_tokens: int | None = None,
        provider_request_id: str = "",
    ) -> dict[str, Any]:
        return self._post(
            f"/runner/v1/memory/jobs/{job_id}/model-calls/{call_id}/finalize",
            {
                "attempt": attempt,
                "outcome": outcome,
                "usage_status": usage_status,
                "input_tokens": input_tokens,
                "output_tokens": output_tokens,
                "provider_request_id": provider_request_id,
            },
        )
