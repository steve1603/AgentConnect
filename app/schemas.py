"""Request and response models for the HTTP API."""

from __future__ import annotations

from datetime import datetime
from typing import Any

from pydantic import BaseModel, Field, field_validator


class ChatRequest(BaseModel):
    message: str = Field(min_length=1, max_length=100_000)
    conversation_id: int | None = None
    mode: str = "full"
    chair: str | None = None
    # Per-browser model overrides, e.g. {"openai": "gpt-5.6"}. Empty strings
    # fall back to the server-side default for that provider.
    models: dict[str, str] = Field(default_factory=dict)

    @field_validator("message")
    @classmethod
    def _strip_message(cls, value: str) -> str:
        stripped = value.strip()
        if not stripped:
            raise ValueError("message must not be blank")
        return stripped

    @field_validator("models")
    @classmethod
    def _clean_models(cls, value: dict[str, str]) -> dict[str, str]:
        return {k: v.strip() for k, v in value.items() if isinstance(v, str) and v.strip()}


class ChatAccepted(BaseModel):
    run_id: str
    conversation_id: int


class MessageOut(BaseModel):
    id: int
    role: str
    content: str
    mode: str | None = None
    chair: str | None = None
    trace: dict[str, Any] | None = None
    created_at: datetime


class ConversationOut(BaseModel):
    id: int
    title: str
    created_at: datetime
    updated_at: datetime


class ConversationDetail(ConversationOut):
    messages: list[MessageOut]


class ProviderConfig(BaseModel):
    key: str
    label: str
    model: str
    configured: bool


class AppConfig(BaseModel):
    """Public configuration. Deliberately contains no API keys."""

    providers: list[ProviderConfig]
    modes: list[str]
    default_chair: str
    ready: bool
