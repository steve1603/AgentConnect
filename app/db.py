"""Async SQLite engine and session management."""

from __future__ import annotations

import logging
from pathlib import Path
from typing import AsyncIterator

from sqlalchemy.ext.asyncio import AsyncEngine, AsyncSession, async_sessionmaker, create_async_engine

from .config import get_settings
from .models import Base

logger = logging.getLogger(__name__)

_engine: AsyncEngine | None = None
_session_factory: async_sessionmaker[AsyncSession] | None = None


def _ensure_sqlite_directory(database_url: str) -> None:
    """Create the parent directory for a file-backed SQLite database."""
    marker = ":///"
    if "sqlite" not in database_url or marker not in database_url:
        return
    path_part = database_url.split(marker, 1)[1]
    if not path_part or path_part == ":memory:":
        return
    Path(path_part).expanduser().resolve().parent.mkdir(parents=True, exist_ok=True)


def get_engine() -> AsyncEngine:
    global _engine, _session_factory
    if _engine is None:
        settings = get_settings()
        _ensure_sqlite_directory(settings.database_url)
        _engine = create_async_engine(settings.database_url, echo=False, future=True)
        _session_factory = async_sessionmaker(_engine, expire_on_commit=False)
    return _engine


def get_session_factory() -> async_sessionmaker[AsyncSession]:
    get_engine()
    assert _session_factory is not None
    return _session_factory


async def init_db() -> None:
    engine = get_engine()
    async with engine.begin() as connection:
        await connection.run_sync(Base.metadata.create_all)
    logger.info("Database ready")


async def dispose_db() -> None:
    global _engine, _session_factory
    if _engine is not None:
        await _engine.dispose()
        _engine = None
        _session_factory = None


async def get_db() -> AsyncIterator[AsyncSession]:
    """FastAPI dependency yielding a session."""
    async with get_session_factory()() as session:
        yield session
