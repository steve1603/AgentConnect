"""Fake providers used by the test suite: canned replies, no network I/O."""

from __future__ import annotations

from typing import Any

from app.providers.base import Provider, ProviderError


class FakeProvider(Provider):
    """A provider that returns canned text, or fails, without any network I/O."""

    def __init__(
        self,
        key: str,
        label: str,
        *,
        model: str = "fake-model",
        reply: str | None = None,
        fail_phases: set[str] | None = None,
        always_fail: bool = False,
    ) -> None:
        super().__init__(model=model, api_key="test-key", timeout=1.0, retries=0)
        self.key = key
        self.label = label
        self.reply = reply or f"{label} says hello."
        self.fail_phases = fail_phases or set()
        self.always_fail = always_fail
        self.calls: list[tuple[str, str]] = []

    async def _generate(self, system: str, prompt: str) -> tuple[str, dict[str, Any]]:
        self.calls.append((system, prompt))
        if self.always_fail:
            raise ProviderError(f"{self.label} is unavailable", retryable=False)

        phase = _phase_of(system)
        if phase in self.fail_phases:
            raise ProviderError(f"{self.label} failed during {phase}", retryable=False)

        if phase == "review":
            return f"{self.label} review: the drafts broadly agree.", {}
        if phase == "chair":
            return f"FINAL from {self.label}", {}
        return self.reply, {}


def _phase_of(system: str) -> str:
    """Identify the phase from the system prompt the engine passed in."""
    from app.orchestration import prompts

    if system == prompts.REVIEW_SYSTEM:
        return "review"
    if system == prompts.CHAIR_SYSTEM:
        return "chair"
    if system == prompts.DIRECT_SYSTEM:
        return "direct"
    return "independent"
