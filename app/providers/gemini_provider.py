"""Google provider - Gemini Interactions API.

The Interactions API is called over plain HTTP rather than through a vendor
SDK so the request and response shapes stay explicit and pinned to the
`Api-Revision` below.
"""

from __future__ import annotations

from typing import Any

import httpx

from .base import Provider, ProviderError, classify_status

INTERACTIONS_URL = "https://generativelanguage.googleapis.com/v1beta/interactions"
API_REVISION = "2026-05-20"


class GeminiProvider(Provider):
    key = "gemini"
    label = "Gemini"

    async def _generate(self, system: str, prompt: str) -> tuple[str, dict[str, Any]]:
        # The Interactions API takes one `input`; the system framing is folded
        # into it so every turn is a self-contained, stateless request.
        composed = f"{system.strip()}\n\n{prompt.strip()}" if system.strip() else prompt.strip()
        payload = {"model": self.model, "input": composed}
        headers = {
            "x-goog-api-key": self.api_key,
            "Content-Type": "application/json",
            "Api-Revision": API_REVISION,
        }

        try:
            async with httpx.AsyncClient(timeout=self.timeout) as client:
                response = await client.post(INTERACTIONS_URL, json=payload, headers=headers)
        except httpx.TimeoutException as exc:
            raise ProviderError(f"request timed out after {self.timeout}s", retryable=True) from exc
        except httpx.HTTPError as exc:
            raise ProviderError(f"connection error: {exc}", retryable=True) from exc

        if response.status_code >= 400:
            raise ProviderError(
                f"HTTP {response.status_code}: {_error_detail(response)}",
                retryable=classify_status(response.status_code),
            )

        try:
            body = response.json()
        except ValueError as exc:
            raise ProviderError("response was not valid JSON", retryable=True) from exc

        return _extract_text(body), _extract_usage(body)


def _error_detail(response: httpx.Response) -> str:
    try:
        body = response.json()
    except ValueError:
        return response.text[:300]
    if isinstance(body, dict):
        error = body.get("error")
        if isinstance(error, dict):
            return str(error.get("message") or error)[:300]
    return str(body)[:300]


def _extract_text(body: dict[str, Any]) -> str:
    """Collect text from the `model_output` steps of an interaction."""
    if isinstance(body.get("output_text"), str) and body["output_text"].strip():
        return body["output_text"]

    chunks: list[str] = []
    for step in body.get("steps") or []:
        if not isinstance(step, dict) or step.get("type") != "model_output":
            continue
        for block in step.get("content") or []:
            if isinstance(block, dict) and block.get("type") == "text" and block.get("text"):
                chunks.append(str(block["text"]))
    return "\n".join(chunks)


def _extract_usage(body: dict[str, Any]) -> dict[str, Any]:
    usage = body.get("usage")
    return usage if isinstance(usage, dict) else {}
