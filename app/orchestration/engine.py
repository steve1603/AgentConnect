"""The roundtable orchestrator.

A full turn runs three phases:

1. Independent - every configured provider answers the request in parallel,
   without seeing any other answer.
2. Peer review - every provider that succeeded in phase 1 receives all
   successful answers and critiques them, again in parallel.
3. Chair - the selected provider receives the request, the answers, and the
   reviews, and writes the single final answer.

A provider that fails is isolated: the turn continues with whoever is left.
If the chair itself fails, another successful provider takes over.
"""

from __future__ import annotations

import asyncio
import logging
from dataclasses import dataclass, field
from typing import Any, AsyncIterator, Mapping, Sequence

from ..config import PROVIDER_KEYS, Settings
from ..providers.base import Provider, ProviderResult
from . import prompts

logger = logging.getLogger(__name__)

MODE_FULL = "full"
MODE_PANEL = "panel"
MODE_DIRECT = "direct"
MODES = (MODE_FULL, MODE_PANEL, MODE_DIRECT)


class OrchestrationError(RuntimeError):
    """No provider was able to produce an answer for this turn."""


@dataclass(slots=True)
class TurnOutcome:
    """Everything a completed turn produced, including the full trace."""

    mode: str
    final_text: str
    chair: str | None = None
    chair_requested: str | None = None
    chair_fallback: bool = False
    answers: list[dict[str, Any]] = field(default_factory=list)
    reviews: list[dict[str, Any]] = field(default_factory=list)
    chair_result: dict[str, Any] | None = None
    failures: list[dict[str, Any]] = field(default_factory=list)

    def to_dict(self) -> dict[str, Any]:
        return {
            "mode": self.mode,
            "final_text": self.final_text,
            "chair": self.chair,
            "chair_requested": self.chair_requested,
            "chair_fallback": self.chair_fallback,
            "answers": self.answers,
            "reviews": self.reviews,
            "chair_result": self.chair_result,
            "failures": self.failures,
        }


class RoundtableEngine:
    """Runs one turn across the configured providers."""

    def __init__(self, providers: Mapping[str, Provider], settings: Settings) -> None:
        self.providers = dict(providers)
        self.settings = settings

    # -- public API --------------------------------------------------------

    async def run(
        self,
        request: str,
        history: Sequence[Mapping[str, str]] = (),
        *,
        mode: str = MODE_FULL,
        chair: str | None = None,
    ) -> AsyncIterator[dict[str, Any]]:
        """Run a turn, yielding progress events and finally a `complete` event."""
        if mode not in MODES:
            mode = MODE_FULL
        if not self.providers:
            raise OrchestrationError(
                "No providers are configured. Add at least one API key to .env."
            )

        requested_chair = chair if chair in self.providers else self.settings.resolved_default_chair()
        if requested_chair not in self.providers:
            requested_chair = next(iter(self.providers))

        rendered_history = prompts.render_history(
            history,
            message_limit=self.settings.history_message_limit,
            char_limit=self.settings.history_char_limit,
        )
        max_chars = self.settings.max_prompt_chars

        if mode == MODE_DIRECT:
            async for event in self._run_direct(
                request, rendered_history, requested_chair, max_chars
            ):
                yield event
            return

        # -- Phase 1: independent answers ----------------------------------
        order = [k for k in PROVIDER_KEYS if k in self.providers]
        yield {"type": "phase", "phase": "independent", "status": "start", "providers": order}

        independent_prompt = prompts.build_independent_prompt(
            request, rendered_history, max_chars=max_chars
        )
        answers: list[ProviderResult] = []
        async for event, result in self._run_parallel(
            "independent",
            {key: (prompts.INDEPENDENT_SYSTEM, independent_prompt) for key in order},
        ):
            if event is not None:
                yield event
            if result is not None:
                answers.append(result)

        successful = _ordered_successes(answers)
        yield {
            "type": "phase",
            "phase": "independent",
            "status": "done",
            "succeeded": [r.provider for r in successful],
        }

        if not successful:
            raise OrchestrationError(
                "Every provider failed on the first round: "
                + "; ".join(f"{r.label}: {r.error}" for r in answers)
            )

        # -- Phase 2: peer review ------------------------------------------
        reviews: list[ProviderResult] = []
        if mode == MODE_FULL and len(successful) > 1:
            reviewer_keys = [r.provider for r in successful]
            yield {"type": "phase", "phase": "review", "status": "start", "providers": reviewer_keys}

            answer_payload = [_answer_payload(r) for r in successful]
            review_jobs = {
                key: (
                    prompts.REVIEW_SYSTEM,
                    prompts.build_review_prompt(
                        request,
                        answer_payload,
                        self.providers[key].label,
                        rendered_history,
                        max_chars=max_chars,
                    ),
                )
                for key in reviewer_keys
            }
            async for event, result in self._run_parallel("review", review_jobs):
                if event is not None:
                    yield event
                if result is not None:
                    reviews.append(result)

            yield {
                "type": "phase",
                "phase": "review",
                "status": "done",
                "succeeded": [r.provider for r in reviews if r.ok],
            }
        elif mode == MODE_FULL:
            yield {
                "type": "phase",
                "phase": "review",
                "status": "skipped",
                "reason": "only one provider produced an answer",
            }

        # -- Phase 3: chair synthesis --------------------------------------
        chair_prompt = prompts.build_chair_prompt(
            request,
            [_answer_payload(r) for r in successful],
            [_answer_payload(r) for r in reviews if r.ok],
            rendered_history,
            max_chars=max_chars,
        )

        chair_result: ProviderResult | None = None
        chair_used: str | None = None
        chair_attempts: list[ProviderResult] = []

        for candidate in _chair_candidates(requested_chair, successful):
            yield {
                "type": "provider",
                "phase": "chair",
                "provider": candidate,
                "status": "running",
                "model": self.providers[candidate].model,
            }
            result = await self.providers[candidate].complete(prompts.CHAIR_SYSTEM, chair_prompt)
            chair_attempts.append(result)
            yield _provider_event("chair", result)
            if result.ok:
                chair_result = result
                chair_used = candidate
                break

        failures = [r.to_dict() for r in answers + reviews + chair_attempts if not r.ok]

        if chair_result is None:
            # Every chair candidate failed; fall back to the best raw answer
            # rather than losing the whole turn.
            fallback = successful[0]
            yield {
                "type": "phase",
                "phase": "chair",
                "status": "failed",
                "reason": "no provider could synthesise; returning the strongest single answer",
            }
            outcome = TurnOutcome(
                mode=mode,
                final_text=fallback.text,
                chair=None,
                chair_requested=requested_chair,
                chair_fallback=True,
                answers=[r.to_dict() for r in answers],
                reviews=[r.to_dict() for r in reviews],
                chair_result=None,
                failures=failures,
            )
        else:
            outcome = TurnOutcome(
                mode=mode,
                final_text=chair_result.text,
                chair=chair_used,
                chair_requested=requested_chair,
                chair_fallback=chair_used != requested_chair,
                answers=[r.to_dict() for r in answers],
                reviews=[r.to_dict() for r in reviews],
                chair_result=chair_result.to_dict(),
                failures=failures,
            )

        yield {"type": "complete", "result": outcome.to_dict()}

    # -- internals ---------------------------------------------------------

    async def _run_direct(
        self,
        request: str,
        history: str,
        provider_key: str,
        max_chars: int,
    ) -> AsyncIterator[dict[str, Any]]:
        provider = self.providers[provider_key]
        yield {
            "type": "provider",
            "phase": "direct",
            "provider": provider_key,
            "status": "running",
            "model": provider.model,
        }
        result = await provider.complete(
            prompts.DIRECT_SYSTEM,
            prompts.build_direct_prompt(request, history, max_chars=max_chars),
        )
        yield _provider_event("direct", result)

        if not result.ok:
            raise OrchestrationError(f"{result.label} failed: {result.error}")

        outcome = TurnOutcome(
            mode=MODE_DIRECT,
            final_text=result.text,
            chair=provider_key,
            chair_requested=provider_key,
            answers=[result.to_dict()],
        )
        yield {"type": "complete", "result": outcome.to_dict()}

    async def _run_parallel(
        self,
        phase: str,
        jobs: Mapping[str, tuple[str, str]],
    ) -> AsyncIterator[tuple[dict[str, Any] | None, ProviderResult | None]]:
        """Run several providers concurrently, streaming events as they land."""
        queue: asyncio.Queue[ProviderResult] = asyncio.Queue()

        async def worker(key: str, system: str, prompt: str) -> None:
            result = await self.providers[key].complete(system, prompt)
            await queue.put(result)

        tasks = [
            asyncio.create_task(worker(key, system, prompt))
            for key, (system, prompt) in jobs.items()
        ]

        for key in jobs:
            yield (
                {
                    "type": "provider",
                    "phase": phase,
                    "provider": key,
                    "status": "running",
                    "model": self.providers[key].model,
                },
                None,
            )

        try:
            for _ in range(len(tasks)):
                result = await queue.get()
                yield (_provider_event(phase, result), result)
        finally:
            for task in tasks:
                if not task.done():
                    task.cancel()
            await asyncio.gather(*tasks, return_exceptions=True)


def _provider_event(phase: str, result: ProviderResult) -> dict[str, Any]:
    return {
        "type": "provider",
        "phase": phase,
        "provider": result.provider,
        "label": result.label,
        "model": result.model,
        "status": "ok" if result.ok else "error",
        "latency_ms": result.latency_ms,
        "attempts": result.attempts,
        "error": result.error,
    }


def _answer_payload(result: ProviderResult) -> dict[str, str]:
    return {"label": result.label, "model": result.model, "text": result.text}


def _ordered_successes(results: Sequence[ProviderResult]) -> list[ProviderResult]:
    """Successful results in stable provider order, not completion order."""
    by_key = {r.provider: r for r in results if r.ok}
    return [by_key[k] for k in PROVIDER_KEYS if k in by_key]


def _chair_candidates(
    requested: str,
    successful: Sequence[ProviderResult],
) -> list[str]:
    """The chair to try first, then the fallbacks, in a stable order.

    A requested chair that already failed this turn is demoted behind the
    providers that answered successfully, so one dead vendor does not stall
    synthesis - but it is still tried, since its failure may be transient.
    """
    others = [r.provider for r in successful if r.provider != requested]
    if any(r.provider == requested for r in successful):
        return [requested, *others]
    return [*others, requested]
