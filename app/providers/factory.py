"""Builds provider instances from settings plus per-request model overrides."""

from __future__ import annotations

from ..config import PROVIDER_KEYS, Settings
from .anthropic_provider import AnthropicProvider
from .base import Provider
from .gemini_provider import GeminiProvider
from .openai_provider import OpenAIProvider

PROVIDER_CLASSES: dict[str, type[Provider]] = {
    "openai": OpenAIProvider,
    "anthropic": AnthropicProvider,
    "gemini": GeminiProvider,
}


def build_provider(
    key: str,
    settings: Settings,
    model_overrides: dict[str, str] | None = None,
) -> Provider | None:
    """Return a ready provider, or None when no API key is configured for it."""
    cls = PROVIDER_CLASSES.get(key)
    if cls is None:
        return None

    api_key = settings.api_key_for(key)
    if not api_key:
        return None

    override = (model_overrides or {}).get(key, "")
    model = override.strip() or settings.model_for(key)

    return cls(
        model=model,
        api_key=api_key,
        timeout=settings.request_timeout_seconds,
        retries=settings.provider_retries,
        max_output_tokens=settings.max_output_tokens,
    )


def build_providers(
    settings: Settings,
    model_overrides: dict[str, str] | None = None,
) -> dict[str, Provider]:
    """All providers that have an API key, keyed by provider name."""
    built = {}
    for key in PROVIDER_KEYS:
        provider = build_provider(key, settings, model_overrides)
        if provider is not None:
            built[key] = provider
    return built
