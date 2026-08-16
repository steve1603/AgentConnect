"""OpenAI provider - Responses API (`POST /v1/responses`)."""

from __future__ import annotations

from typing import Any

from openai import (
    APIConnectionError,
    APIStatusError,
    AsyncOpenAI,
    AuthenticationError,
    RateLimitError,
)

from .base import Provider, ProviderError, classify_status


class OpenAIProvider(Provider):
    key = "openai"
    label = "ChatGPT"

    def __init__(self, **kwargs: Any) -> None:
        super().__init__(**kwargs)
        self._client = AsyncOpenAI(api_key=self.api_key, timeout=self.timeout, max_retries=0)

    async def _generate(self, system: str, prompt: str) -> tuple[str, dict[str, Any]]:
        try:
            response = await self._client.responses.create(
                model=self.model,
                instructions=system,
                input=prompt,
                max_output_tokens=self.max_output_tokens,
            )
        except AuthenticationError as exc:
            raise ProviderError(f"authentication failed: {exc}", retryable=False) from exc
        except RateLimitError as exc:
            raise ProviderError(f"rate limited: {exc}", retryable=True) from exc
        except APIConnectionError as exc:
            raise ProviderError(f"connection error: {exc}", retryable=True) from exc
        except APIStatusError as exc:
            raise ProviderError(
                f"HTTP {exc.status_code}: {exc}", retryable=classify_status(exc.status_code)
            ) from exc

        return _extract_text(response), _extract_usage(response)


def _extract_text(response: Any) -> str:
    """Read the assistant text out of a Responses API result.

    `output_text` is the SDK convenience accessor; walking `output` is the
    fallback for responses the accessor does not flatten (e.g. when the model
    emits reasoning items alongside the message).
    """
    text = getattr(response, "output_text", "") or ""
    if text.strip():
        return text

    chunks: list[str] = []
    for item in getattr(response, "output", None) or []:
        if getattr(item, "type", None) != "message":
            continue
        for block in getattr(item, "content", None) or []:
            block_text = getattr(block, "text", None)
            if getattr(block, "type", None) == "output_text" and block_text:
                chunks.append(block_text)
    return "\n".join(chunks)


def _extract_usage(response: Any) -> dict[str, Any]:
    usage = getattr(response, "usage", None)
    if usage is None:
        return {}
    return {
        "input_tokens": getattr(usage, "input_tokens", None),
        "output_tokens": getattr(usage, "output_tokens", None),
    }
