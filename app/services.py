"""Conversation persistence and the in-memory run registry used by SSE."""

from __future__ import annotations

import asyncio
import json
import logging
import time
import uuid
from typing import Any, AsyncIterator

from sqlalchemy import delete, select
from sqlalchemy.ext.asyncio import AsyncSession
from sqlalchemy.orm import selectinload

from .models import Conversation, Message

logger = logging.getLogger(__name__)

TITLE_MAX_CHARS = 60
RUN_RETENTION_SECONDS = 900


# ---------------------------------------------------------------------------
# Conversations
# ---------------------------------------------------------------------------


def derive_title(text: str) -> str:
    flat = " ".join(text.split())
    if len(flat) <= TITLE_MAX_CHARS:
        return flat or "New conversation"
    return flat[: TITLE_MAX_CHARS - 1].rstrip() + "…"


async def list_conversations(session: AsyncSession) -> list[Conversation]:
    result = await session.execute(
        select(Conversation).order_by(Conversation.updated_at.desc())
    )
    return list(result.scalars().all())


async def get_conversation(session: AsyncSession, conversation_id: int) -> Conversation | None:
    result = await session.execute(
        select(Conversation)
        .options(selectinload(Conversation.messages))
        .where(Conversation.id == conversation_id)
    )
    return result.scalar_one_or_none()


async def create_conversation(session: AsyncSession, title: str) -> Conversation:
    conversation = Conversation(title=derive_title(title))
    session.add(conversation)
    await session.commit()
    await session.refresh(conversation)
    return conversation


async def delete_conversation(session: AsyncSession, conversation_id: int) -> bool:
    conversation = await session.get(Conversation, conversation_id)
    if conversation is None:
        return False
    await session.execute(delete(Message).where(Message.conversation_id == conversation_id))
    await session.delete(conversation)
    await session.commit()
    return True


async def add_message(
    session: AsyncSession,
    conversation_id: int,
    role: str,
    content: str,
    *,
    mode: str | None = None,
    chair: str | None = None,
    trace: dict[str, Any] | None = None,
) -> Message:
    message = Message(
        conversation_id=conversation_id,
        role=role,
        content=content,
        mode=mode,
        chair=chair,
        trace=json.dumps(trace) if trace is not None else None,
    )
    session.add(message)

    conversation = await session.get(Conversation, conversation_id)
    if conversation is not None:
        # Touch the parent so the sidebar orders by most recent activity.
        conversation.updated_at = message.created_at or conversation.updated_at

    await session.commit()
    await session.refresh(message)
    return message


async def history_for(session: AsyncSession, conversation_id: int) -> list[dict[str, str]]:
    """Prior turns as plain role/content dicts for prompt rendering."""
    result = await session.execute(
        select(Message).where(Message.conversation_id == conversation_id).order_by(Message.id)
    )
    return [{"role": m.role, "content": m.content} for m in result.scalars().all()]


def message_to_dict(message: Message) -> dict[str, Any]:
    trace: dict[str, Any] | None = None
    if message.trace:
        try:
            trace = json.loads(message.trace)
        except json.JSONDecodeError:
            logger.warning("Message %s has an unreadable trace; dropping it", message.id)
    return {
        "id": message.id,
        "role": message.role,
        "content": message.content,
        "mode": message.mode,
        "chair": message.chair,
        "trace": trace,
        "created_at": message.created_at,
    }


# ---------------------------------------------------------------------------
# Runs (server-sent events)
# ---------------------------------------------------------------------------


class Run:
    """A single in-flight turn whose progress events can be streamed.

    Events are buffered as well as pushed, so a browser that attaches its
    EventSource slightly after the POST still receives everything from the
    start of the turn.
    """

    def __init__(self, conversation_id: int) -> None:
        self.id = uuid.uuid4().hex
        self.conversation_id = conversation_id
        self.events: list[dict[str, Any]] = []
        self.finished = asyncio.Event()
        self.finished_at: float | None = None
        self._subscribers: list[asyncio.Queue[dict[str, Any] | None]] = []

    def emit(self, event: dict[str, Any]) -> None:
        self.events.append(event)
        for queue in self._subscribers:
            queue.put_nowait(event)

    def finish(self) -> None:
        self.finished_at = time.monotonic()
        self.finished.set()
        for queue in self._subscribers:
            queue.put_nowait(None)

    async def stream(self) -> AsyncIterator[dict[str, Any]]:
        queue: asyncio.Queue[dict[str, Any] | None] = asyncio.Queue()

        # Subscribe and snapshot the backlog in one synchronous step: with no
        # await between them, every event is either in `backlog` or in `queue`,
        # never both and never neither.
        self._subscribers.append(queue)
        backlog = list(self.events)
        already_finished = self.finished.is_set()

        try:
            for event in backlog:
                yield event
            if already_finished:
                return
            while True:
                event = await queue.get()
                if event is None:
                    return
                yield event
        finally:
            if queue in self._subscribers:
                self._subscribers.remove(queue)


class RunRegistry:
    """Tracks active runs and expires finished ones."""

    def __init__(self, retention_seconds: int = RUN_RETENTION_SECONDS) -> None:
        self._runs: dict[str, Run] = {}
        self.retention_seconds = retention_seconds

    def create(self, conversation_id: int) -> Run:
        self._prune()
        run = Run(conversation_id)
        self._runs[run.id] = run
        return run

    def get(self, run_id: str) -> Run | None:
        return self._runs.get(run_id)

    def _prune(self) -> None:
        now = time.monotonic()
        expired = [
            run_id
            for run_id, run in self._runs.items()
            if run.finished_at is not None and now - run.finished_at > self.retention_seconds
        ]
        for run_id in expired:
            self._runs.pop(run_id, None)


registry = RunRegistry()
