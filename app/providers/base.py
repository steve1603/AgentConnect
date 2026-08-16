"""Provider interface, result type, and shared retry logic."""

from __future__ import annotations

import abc
import asyncio
import logging
import random
import time
from dataclasses import dataclass, field
from typing import Any

logger = logging.getLogger(__name__)


class ProviderError(RuntimeError):
    """A provider call failed.

    `retryable` marks transient conditions (timeouts, 429, 5xx) that are worth
    another attempt. Authentication and validation failures are not retried.
    """

    def __init__(self, message: str, *, retryable: bool = False) -> None:
        super().__init__(message)
        self.retryable = retryable


@dataclass(slots=True)
class ProviderResult:
    """Outcome of one provider call in one phase of a turn."""

    provider: str
    label: str
    model: str
    ok: bool
    text: str = ""
    error: str | None = None
    latency_ms: int = 0
    attempts: int = 1
    usage: dict[str, Any] = field(default_factory=dict)

    def to_dict(self) -> dict[str, Any]:
        return {
            "provider": self.provider,
            "label": self.label,
            "model": self.model,
            "ok": self.ok,
            "text": self.text,
            "error": self.error,
            "latency_ms": self.latency_ms,
            "attempts": self.attempts,
            "usage": self.usage,
        }


class Provider(abc.ABC):
    """One upstream model vendor.

    Subclasses implement `_generate`; `complete` adds retries, timing, and
    uniform error capture so a single vendor outage cannot fail the turn.
    """

    key: str = ""
    label: str = ""

    def __init__(
        self,
        *,
        model: str,
        api_key: str,
        timeout: float = 120.0,
        retries: int = 2,
        max_output_tokens: int = 16000,
    ) -> None:
        self.model = model
        self.api_key = api_key
        self.timeout = timeout
        self.retries = retries
        self.max_output_tokens = max_output_tokens

    @abc.abstractmethod
    async def _generate(self, system: str, prompt: str) -> tuple[str, dict[str, Any]]:
        """Return `(text, usage)` or raise `ProviderError`."""

    async def complete(self, system: str, prompt: str) -> ProviderResult:
        started = time.perf_counter()
        last_error = "unknown error"

        for attempt in range(1, self.retries + 2):
            try:
                text, usage = await self._generate(system, prompt)
            except ProviderError as exc:
                last_error = str(exc)
                if not exc.retryable or attempt > self.retries:
                    break
            except asyncio.CancelledError:
                raise
            except Exception as exc:  # defensive: never let one vendor crash a turn
                logger.exception("%s: unexpected provider failure", self.key)
                last_error = f"{type(exc).__name__}: {exc}"
                break
            else:
                if not text.strip():
                    last_error = "provider returned an empty response"
                    if attempt > self.retries:
                        break
                else:
                    return ProviderResult(
                        provider=self.key,
                        label=self.label,
                        model=self.model,
                        ok=True,
                        text=text.strip(),
                        latency_ms=int((time.perf_counter() - started) * 1000),
                        attempts=attempt,
                        usage=usage,
                    )

            # Exponential backoff with jitter before the next attempt.
            delay = min(2**(attempt - 1), 8) + random.uniform(0, 0.4)
            logger.warning(
                "%s attempt %d/%d failed (%s); retrying in %.1fs",
                self.key, attempt, self.retries + 1, last_error, delay,
            )
            await asyncio.sleep(delay)

        return ProviderResult(
            provider=self.key,
            label=self.label,
            model=self.model,
            ok=False,
            error=last_error,
            latency_ms=int((time.perf_counter() - started) * 1000),
            attempts=min(attempt, self.retries + 1),
        )


def classify_status(status_code: int) -> bool:
    """True when an HTTP status is worth retrying."""
    return status_code == 408 or status_code == 429 or status_code >= 500
