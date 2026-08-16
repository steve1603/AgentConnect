"""Anthropic provider - Messages API (`POST /v1/messages`)."""

from __future__ import annotations

from typing import Any

import anthropic
from anthropic import AsyncAnthropic

from .base import Provider, ProviderError, classify_status


class AnthropicProvider(Provider):
    key = "anthropic"
    label = "Claude"

    def __init__(self, **kwargs: Any) -> None:
        super().__init__(**kwargs)
        self._client = AsyncAnthropic(api_key=self.api_key, timeout=self.timeout, max_retries=0)

    async def _generate(self, system: str, prompt: str) -> tuple[str, dict[str, Any]]:
        # Sampling parameters (`temperature`/`top_p`/`top_k`) are rejected on
        # current Claude models, and thinking is configured through adaptive
        # defaults rather than a token budget, so neither is sent here.
        try:
            message = await self._client.messages.create(
                model=self.model,
                max_tokens=self.max_output_tokens,
                system=system,
                messages=[{"role": "user", "content": prompt}],
            )
        except anthropic.AuthenticationError as exc:
            raise ProviderError(f"authentication failed: {exc}", retryable=False) from exc
        except anthropic.RateLimitError as exc:
            raise ProviderError(f"rate limited: {exc}", retryable=True) from exc
        except anthropic.APIConnectionError as exc:
            raise ProviderError(f"connection error: {exc}", retryable=True) from exc
        except anthropic.APIStatusError as exc:
            raise ProviderError(
                f"HTTP {exc.status_code}: {exc}", retryable=classify_status(exc.status_code)
            ) from exc

        if getattr(message, "stop_reason", None) == "refusal":
            raise ProviderError("the model declined this request", retryable=False)

        return _extract_text(message), _extract_usage(message)


def _extract_text(message: Any) -> str:
    """Concatenate the text blocks of a Messages API response.

    Thinking blocks are skipped: their text is empty by default and the
    roundtable trace only ever stores normal visible output.
    """
    chunks = [
        block.text
        for block in getattr(message, "content", None) or []
        if getattr(block, "type", None) == "text" and getattr(block, "text", None)
    ]
    return "\n".join(chunks)


def _extract_usage(message: Any) -> dict[str, Any]:
    usage = getattr(message, "usage", None)
    if usage is None:
        return {}
    return {
        "input_tokens": getattr(usage, "input_tokens", None),
        "output_tokens": getattr(usage, "output_tokens", None),
    }
