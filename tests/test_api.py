"""End-to-end HTTP tests: chat, the SSE run stream, storage, and headers.

Providers are replaced with fakes, so these tests make no network calls.
"""

from __future__ import annotations

import json
from typing import AsyncIterator

import httpx
import pytest
from httpx import ASGITransport

from app import db as db_module
from app import main as main_module
from app import services
from app.config import get_settings

from fakes import FakeProvider

pytestmark = pytest.mark.asyncio


@pytest.fixture
async def client(tmp_path, monkeypatch) -> AsyncIterator[httpx.AsyncClient]:
    monkeypatch.setenv("OPENAI_API_KEY", "test")
    monkeypatch.setenv("ANTHROPIC_API_KEY", "test")
    monkeypatch.setenv("GEMINI_API_KEY", "test")
    monkeypatch.setenv("PROVIDER_RETRIES", "0")
    monkeypatch.setenv("DATABASE_URL", f"sqlite+aiosqlite:///{tmp_path / 'test.db'}")
    get_settings.cache_clear()

    monkeypatch.setattr(
        main_module,
        "build_providers",
        lambda settings, overrides=None: {
            "openai": FakeProvider("openai", "ChatGPT"),
            "anthropic": FakeProvider("anthropic", "Claude"),
            "gemini": FakeProvider("gemini", "Gemini"),
        },
    )

    await db_module.init_db()
    transport = ASGITransport(app=main_module.app)
    async with httpx.AsyncClient(transport=transport, base_url="http://test") as async_client:
        yield async_client
    await db_module.dispose_db()
    get_settings.cache_clear()


async def drain(client: httpx.AsyncClient, run_id: str) -> list[dict]:
    events = []
    async with client.stream("GET", f"/api/runs/{run_id}/events") as response:
        assert response.status_code == 200
        async for line in response.aiter_lines():
            if line.startswith("data: "):
                events.append(json.loads(line[6:]))
    return events


async def test_config_never_exposes_api_keys(client):
    response = await client.get("/api/config")
    assert response.status_code == 200
    body = response.json()

    assert body["ready"] is True
    assert [p["key"] for p in body["providers"]] == ["openai", "anthropic", "gemini"]
    assert "test" not in json.dumps(body).replace("latest", "")
    assert "api_key" not in json.dumps(body)


async def test_security_headers_are_present(client):
    response = await client.get("/healthz")
    headers = response.headers
    assert "default-src 'self'" in headers["content-security-policy"]
    assert "unsafe-inline" not in headers["content-security-policy"]
    assert headers["x-content-type-options"] == "nosniff"
    assert headers["x-frame-options"] == "DENY"
    assert headers["referrer-policy"] == "no-referrer"


async def test_full_turn_streams_progress_and_persists_the_trace(client):
    response = await client.post("/api/chat", json={"message": "What is 2+2?", "mode": "full"})
    assert response.status_code == 202
    accepted = response.json()

    events = await drain(client, accepted["run_id"])
    types = [e["type"] for e in events]
    assert "provider" in types
    assert "complete" in types
    assert types[-1] == "saved"

    complete = next(e for e in events if e["type"] == "complete")
    assert complete["result"]["final_text"] == "FINAL from ChatGPT"

    detail = (await client.get(f"/api/conversations/{accepted['conversation_id']}")).json()
    assert [m["role"] for m in detail["messages"]] == ["user", "assistant"]

    assistant = detail["messages"][1]
    assert assistant["content"] == "FINAL from ChatGPT"
    assert assistant["chair"] == "openai"
    assert len(assistant["trace"]["answers"]) == 3
    assert len(assistant["trace"]["reviews"]) == 3


async def test_second_turn_reuses_the_conversation(client):
    first = (await client.post("/api/chat", json={"message": "My name is Ada."})).json()
    await drain(client, first["run_id"])

    second = (
        await client.post(
            "/api/chat",
            json={"message": "What is my name?", "conversation_id": first["conversation_id"]},
        )
    ).json()
    assert second["conversation_id"] == first["conversation_id"]
    await drain(client, second["run_id"])

    detail = (await client.get(f"/api/conversations/{first['conversation_id']}")).json()
    assert len(detail["messages"]) == 4


async def test_conversation_can_be_deleted(client):
    started = (await client.post("/api/chat", json={"message": "Hello"})).json()
    await drain(client, started["run_id"])

    conversation_id = started["conversation_id"]
    assert (await client.delete(f"/api/conversations/{conversation_id}")).status_code == 204
    assert (await client.get(f"/api/conversations/{conversation_id}")).status_code == 404
    assert (await client.get("/api/conversations")).json() == []


async def test_blank_message_is_rejected(client):
    assert (await client.post("/api/chat", json={"message": "   "})).status_code == 422


async def test_unknown_conversation_is_rejected(client):
    response = await client.post(
        "/api/chat", json={"message": "Hello", "conversation_id": 9999}
    )
    assert response.status_code == 404


async def test_unknown_run_is_rejected(client):
    assert (await client.get("/api/runs/does-not-exist/events")).status_code == 404


async def test_late_subscriber_still_receives_the_whole_run(client):
    """The run buffers its events, so attaching after the turn still replays it."""
    started = (await client.post("/api/chat", json={"message": "Hello", "mode": "direct"})).json()
    run = services.registry.get(started["run_id"])
    await run.finished.wait()

    events = await drain(client, started["run_id"])
    assert [e["type"] for e in events][-1] == "saved"
    assert any(e["type"] == "complete" for e in events)
