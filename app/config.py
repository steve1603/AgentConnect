"""Application configuration, loaded from environment / .env."""

from __future__ import annotations

from functools import lru_cache

from pydantic import Field
from pydantic_settings import BaseSettings, SettingsConfigDict

PROVIDER_KEYS = ("openai", "anthropic", "gemini")

PROVIDER_LABELS = {
    "openai": "ChatGPT",
    "anthropic": "Claude",
    "gemini": "Gemini",
}


class Settings(BaseSettings):
    """Runtime settings.

    Every field can be overridden through `.env` or the process environment.
    API keys stay on the server: they are never included in `/api/config`.
    """

    model_config = SettingsConfigDict(
        env_file=".env",
        env_file_encoding="utf-8",
        extra="ignore",
        case_sensitive=False,
    )

    openai_api_key: str = ""
    anthropic_api_key: str = ""
    gemini_api_key: str = ""

    openai_model: str = "gpt-5.6"
    anthropic_model: str = "claude-sonnet-5"
    gemini_model: str = "gemini-3.7-flash"

    app_host: str = "127.0.0.1"
    app_port: int = 8000
    database_url: str = "sqlite+aiosqlite:///./data/roundtable.db"
    log_level: str = "INFO"
    open_browser: bool = True

    request_timeout_seconds: float = 120.0
    provider_retries: int = Field(default=2, ge=0, le=5)
    max_prompt_chars: int = Field(default=30000, ge=1000)
    max_output_tokens: int = Field(default=16000, ge=256)
    history_message_limit: int = Field(default=24, ge=2)
    history_char_limit: int = Field(default=60000, ge=1000)
    default_chair: str = "openai"

    def api_key_for(self, provider: str) -> str:
        return {
            "openai": self.openai_api_key,
            "anthropic": self.anthropic_api_key,
            "gemini": self.gemini_api_key,
        }.get(provider, "").strip()

    def model_for(self, provider: str) -> str:
        return {
            "openai": self.openai_model,
            "anthropic": self.anthropic_model,
            "gemini": self.gemini_model,
        }.get(provider, "")

    def configured_providers(self) -> list[str]:
        """Providers that have a usable API key."""
        return [p for p in PROVIDER_KEYS if self.api_key_for(p)]

    def resolved_default_chair(self) -> str:
        configured = self.configured_providers()
        if self.default_chair in configured:
            return self.default_chair
        if configured:
            return configured[0]
        return self.default_chair


@lru_cache(maxsize=1)
def get_settings() -> Settings:
    return Settings()
