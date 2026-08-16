"""Shared fixtures. Providers are always fakes, so tests spend no API credits."""

from __future__ import annotations

import sys
from pathlib import Path

import pytest

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

from app.config import Settings  # noqa: E402

from fakes import FakeProvider  # noqa: E402  (tests/ is on sys.path)


@pytest.fixture
def settings() -> Settings:
    return Settings(
        openai_api_key="test",
        anthropic_api_key="test",
        gemini_api_key="test",
        database_url="sqlite+aiosqlite:///:memory:",
        provider_retries=0,
        request_timeout_seconds=1.0,
        default_chair="openai",
    )


@pytest.fixture
def providers() -> dict[str, FakeProvider]:
    return {
        "openai": FakeProvider("openai", "ChatGPT"),
        "anthropic": FakeProvider("anthropic", "Claude"),
        "gemini": FakeProvider("gemini", "Gemini"),
    }
