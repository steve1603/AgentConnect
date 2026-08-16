"""FastAPI application: HTTP API, SSE progress stream, and static UI."""

from __future__ import annotations

import asyncio
import json
import logging
from contextlib import asynccontextmanager
from pathlib import Path
from typing import Any, AsyncIterator

from fastapi import Depends, FastAPI, HTTPException, Request, Response
from fastapi.responses import FileResponse, StreamingResponse
from fastapi.staticfiles import StaticFiles
from sqlalchemy.ext.asyncio import AsyncSession

from . import services
from .config import PROVIDER_KEYS, PROVIDER_LABELS, Settings, get_settings
from .db import dispose_db, get_db, get_session_factory, init_db
from .orchestration import MODES, OrchestrationError, RoundtableEngine
from .providers import build_providers
from .schemas import (
    AppConfig,
    ChatAccepted,
    ChatRequest,
    ConversationDetail,
    ConversationOut,
    ProviderConfig,
)

logger = logging.getLogger(__name__)

STATIC_DIR = Path(__file__).parent / "static"

# The UI ships no third-party assets, so everything can be locked to 'self'.
# 'unsafe-inline' is not granted: styles and scripts live in their own files.
CONTENT_SECURITY_POLICY = (
    "default-src 'self'; "
    "script-src 'self'; "
    "style-src 'self'; "
    "img-src 'self' data:; "
    "connect-src 'self'; "
    "font-src 'self'; "
    "object-src 'none'; "
    "base-uri 'none'; "
    "form-action 'self'; "
    "frame-ancestors 'none'"
)


@asynccontextmanager
async def lifespan(app: FastAPI) -> AsyncIterator[None]:
    settings = get_settings()
    logging.basicConfig(
        level=getattr(logging, settings.log_level.upper(), logging.INFO),
        format="%(asctime)s %(levelname)-8s %(name)s: %(message)s",
    )
    await init_db()
    configured = settings.configured_providers()
    if configured:
        logger.info("Providers configured: %s", ", ".join(configured))
    else:
        logger.warning("No API keys found in .env - the roundtable cannot run yet")
    yield
    await dispose_db()


app = FastAPI(title="AI Roundtable", version="1.0.0", lifespan=lifespan)


@app.middleware("http")
async def security_headers(request: Request, call_next):
    response: Response = await call_next(request)
    response.headers["Content-Security-Policy"] = CONTENT_SECURITY_POLICY
    response.headers["X-Content-Type-Options"] = "nosniff"
    response.headers["X-Frame-Options"] = "DENY"
    response.headers["Referrer-Policy"] = "no-referrer"
    response.headers["Permissions-Policy"] = "geolocation=(), microphone=(), camera=()"
    response.headers["Cross-Origin-Opener-Policy"] = "same-origin"
    return response


# ---------------------------------------------------------------------------
# Config
# ---------------------------------------------------------------------------


@app.get("/api/config", response_model=AppConfig)
async def read_config(settings: Settings = Depends(get_settings)) -> AppConfig:
    """Public configuration. API keys are never included in this payload."""
    providers = [
        ProviderConfig(
            key=key,
            label=PROVIDER_LABELS[key],
            model=settings.model_for(key),
            configured=bool(settings.api_key_for(key)),
        )
        for key in PROVIDER_KEYS
    ]
    return AppConfig(
        providers=providers,
        modes=list(MODES),
        default_chair=settings.resolved_default_chair(),
        ready=any(p.configured for p in providers),
    )


# ---------------------------------------------------------------------------
# Conversations
# ---------------------------------------------------------------------------


@app.get("/api/conversations", response_model=list[ConversationOut])
async def list_conversations(db: AsyncSession = Depends(get_db)) -> list[ConversationOut]:
    conversations = await services.list_conversations(db)
    return [ConversationOut.model_validate(c, from_attributes=True) for c in conversations]


@app.get("/api/conversations/{conversation_id}", response_model=ConversationDetail)
async def read_conversation(
    conversation_id: int, db: AsyncSession = Depends(get_db)
) -> ConversationDetail:
    conversation = await services.get_conversation(db, conversation_id)
    if conversation is None:
        raise HTTPException(status_code=404, detail="Conversation not found")
    return ConversationDetail(
        id=conversation.id,
        title=conversation.title,
        created_at=conversation.created_at,
        updated_at=conversation.updated_at,
        messages=[services.message_to_dict(m) for m in conversation.messages],
    )


@app.delete("/api/conversations/{conversation_id}", status_code=204)
async def remove_conversation(conversation_id: int, db: AsyncSession = Depends(get_db)) -> Response:
    if not await services.delete_conversation(db, conversation_id):
        raise HTTPException(status_code=404, detail="Conversation not found")
    return Response(status_code=204)


# ---------------------------------------------------------------------------
# Chat
# ---------------------------------------------------------------------------


@app.post("/api/chat", response_model=ChatAccepted, status_code=202)
async def start_chat(
    payload: ChatRequest,
    db: AsyncSession = Depends(get_db),
    settings: Settings = Depends(get_settings),
) -> ChatAccepted:
    """Start a turn and return the id of its progress stream."""
    if not settings.configured_providers():
        raise HTTPException(
            status_code=503,
            detail="No provider API keys are configured. Add them to .env and restart.",
        )

    if payload.conversation_id is None:
        conversation = await services.create_conversation(db, payload.message)
    else:
        conversation = await services.get_conversation(db, payload.conversation_id)
        if conversation is None:
            raise HTTPException(status_code=404, detail="Conversation not found")

    history = await services.history_for(db, conversation.id)
    await services.add_message(db, conversation.id, "user", payload.message)

    run = services.registry.create(conversation.id)
    asyncio.create_task(
        _execute_turn(
            run_id=run.id,
            conversation_id=conversation.id,
            request_text=payload.message,
            history=history,
            mode=payload.mode,
            chair=payload.chair,
            model_overrides=payload.models,
            settings=settings,
        )
    )
    return ChatAccepted(run_id=run.id, conversation_id=conversation.id)


@app.get("/api/runs/{run_id}/events")
async def stream_run(run_id: str) -> StreamingResponse:
    run = services.registry.get(run_id)
    if run is None:
        raise HTTPException(status_code=404, detail="Run not found or expired")

    async def event_source() -> AsyncIterator[bytes]:
        async for event in run.stream():
            yield f"data: {json.dumps(event)}\n\n".encode()

    return StreamingResponse(
        event_source(),
        media_type="text/event-stream",
        headers={"Cache-Control": "no-cache", "X-Accel-Buffering": "no"},
    )


async def _execute_turn(
    *,
    run_id: str,
    conversation_id: int,
    request_text: str,
    history: list[dict[str, str]],
    mode: str,
    chair: str | None,
    model_overrides: dict[str, str],
    settings: Settings,
) -> None:
    """Run one turn in the background, emitting progress to its run stream."""
    run = services.registry.get(run_id)
    if run is None:
        return

    try:
        providers = build_providers(settings, model_overrides)
        engine = RoundtableEngine(providers, settings)
        outcome: dict[str, Any] | None = None

        async for event in engine.run(request_text, history, mode=mode, chair=chair):
            if event.get("type") == "complete":
                outcome = event["result"]
            run.emit(event)

        if outcome is not None:
            async with get_session_factory()() as session:
                message = await services.add_message(
                    session,
                    conversation_id,
                    "assistant",
                    outcome["final_text"],
                    mode=outcome["mode"],
                    chair=outcome.get("chair"),
                    trace=outcome,
                )
            run.emit({"type": "saved", "message_id": message.id})
    except OrchestrationError as exc:
        logger.warning("Turn failed: %s", exc)
        run.emit({"type": "error", "message": str(exc)})
    except asyncio.CancelledError:
        run.emit({"type": "error", "message": "The turn was cancelled."})
        raise
    except Exception as exc:  # pragma: no cover - defensive
        logger.exception("Unexpected failure during turn")
        run.emit({"type": "error", "message": f"Unexpected error: {type(exc).__name__}: {exc}"})
    finally:
        run.finish()


# ---------------------------------------------------------------------------
# Static UI
# ---------------------------------------------------------------------------


@app.get("/healthz")
async def healthz() -> dict[str, str]:
    return {"status": "ok"}


@app.get("/")
async def index() -> FileResponse:
    return FileResponse(STATIC_DIR / "index.html")


app.mount("/static", StaticFiles(directory=STATIC_DIR), name="static")
