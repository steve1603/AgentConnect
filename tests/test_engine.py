"""Orchestration behaviour: phases, failure isolation, chair fallback, modes."""

from __future__ import annotations

from typing import Any

import pytest

from app.orchestration import MODE_DIRECT, MODE_FULL, MODE_PANEL, OrchestrationError, RoundtableEngine

pytestmark = pytest.mark.asyncio


async def run_turn(engine: RoundtableEngine, request: str = "What is 2+2?", **kwargs: Any):
    """Drain the engine's event stream, returning (events, final result)."""
    events: list[dict[str, Any]] = []
    result: dict[str, Any] | None = None
    async for event in engine.run(request, **kwargs):
        events.append(event)
        if event["type"] == "complete":
            result = event["result"]
    return events, result


async def test_full_roundtable_runs_three_phases(settings, providers):
    """All three phases run, and the chair's text becomes the final answer."""
    engine = RoundtableEngine(providers, settings)
    events, result = await run_turn(engine, mode=MODE_FULL, chair="openai")

    phases = {e["phase"] for e in events if e["type"] == "provider"}
    assert phases == {"independent", "review", "chair"}

    assert result is not None
    assert result["final_text"] == "FINAL from ChatGPT"
    assert result["chair"] == "openai"
    assert result["chair_fallback"] is False
    assert len(result["answers"]) == 3
    assert len(result["reviews"]) == 3
    assert result["failures"] == []

    # Each reviewer saw every first-round answer, not just its own.
    review_prompt = providers["anthropic"].calls[1][1]
    for label in ("ChatGPT", "Claude", "Gemini"):
        assert label in review_prompt


async def test_failed_provider_is_isolated(settings, providers):
    """One dead vendor does not fail the turn; the rest carry it."""
    providers["gemini"].always_fail = True
    engine = RoundtableEngine(providers, settings)
    events, result = await run_turn(engine, mode=MODE_FULL, chair="anthropic")

    assert result is not None
    assert result["final_text"] == "FINAL from Claude"
    assert [a["provider"] for a in result["answers"] if a["ok"]] == ["openai", "anthropic"]
    assert [f["provider"] for f in result["failures"]] == ["gemini"]

    # Gemini never reached the review round, and its answer is not in the prompts.
    assert len(providers["gemini"].calls) == 1
    assert "Gemini" not in providers["openai"].calls[1][1]

    error_events = [
        e for e in events if e["type"] == "provider" and e["status"] == "error"
    ]
    assert [e["provider"] for e in error_events] == ["gemini"]


async def test_chair_failure_falls_back_to_another_provider(settings, providers):
    """A chair that cannot synthesise hands off to a provider that can."""
    providers["openai"].fail_phases = {"chair"}
    engine = RoundtableEngine(providers, settings)
    _, result = await run_turn(engine, mode=MODE_FULL, chair="openai")

    assert result is not None
    assert result["chair_requested"] == "openai"
    assert result["chair"] == "anthropic"
    assert result["chair_fallback"] is True
    assert result["final_text"] == "FINAL from Claude"

    # The failed chair attempt is recorded in the trace rather than swallowed.
    assert any(f["provider"] == "openai" for f in result["failures"])


async def test_panel_mode_skips_peer_review(settings, providers):
    """Panel mode answers and chairs, with no review round."""
    engine = RoundtableEngine(providers, settings)
    events, result = await run_turn(engine, mode=MODE_PANEL, chair="gemini")

    phases = {e["phase"] for e in events if e["type"] == "provider"}
    assert phases == {"independent", "chair"}

    assert result is not None
    assert result["reviews"] == []
    assert result["chair"] == "gemini"
    assert result["final_text"] == "FINAL from Gemini"


async def test_direct_mode_uses_one_provider(settings, providers):
    """Direct mode calls exactly one provider and returns its answer verbatim."""
    engine = RoundtableEngine(providers, settings)
    _, result = await run_turn(engine, mode=MODE_DIRECT, chair="anthropic")

    assert result is not None
    assert result["mode"] == MODE_DIRECT
    assert result["final_text"] == "Claude says hello."
    assert len(providers["anthropic"].calls) == 1
    assert providers["openai"].calls == []
    assert providers["gemini"].calls == []


async def test_review_is_skipped_when_only_one_provider_answers(settings, providers):
    """Peer review needs at least two answers to compare."""
    providers["anthropic"].always_fail = True
    providers["gemini"].always_fail = True
    engine = RoundtableEngine(providers, settings)
    events, result = await run_turn(engine, mode=MODE_FULL, chair="openai")

    skipped = [e for e in events if e["type"] == "phase" and e["status"] == "skipped"]
    assert len(skipped) == 1
    assert result is not None
    assert result["reviews"] == []
    assert result["final_text"] == "FINAL from ChatGPT"


async def test_turn_fails_when_every_provider_fails(settings, providers):
    """With no answers at all there is nothing to synthesise."""
    for provider in providers.values():
        provider.always_fail = True
    engine = RoundtableEngine(providers, settings)

    with pytest.raises(OrchestrationError):
        await run_turn(engine, mode=MODE_FULL)


async def test_no_providers_configured_is_an_error(settings):
    engine = RoundtableEngine({}, settings)
    with pytest.raises(OrchestrationError):
        await run_turn(engine)


async def test_history_is_passed_to_providers(settings, providers):
    """Earlier turns reach the prompt so follow-up questions resolve."""
    engine = RoundtableEngine(providers, settings)
    history = [
        {"role": "user", "content": "My name is Ada."},
        {"role": "assistant", "content": "Nice to meet you, Ada."},
    ]
    await run_turn(engine, "What is my name?", history=history, mode=MODE_PANEL)

    first_prompt = providers["openai"].calls[0][1]
    assert "My name is Ada." in first_prompt
    assert "What is my name?" in first_prompt
