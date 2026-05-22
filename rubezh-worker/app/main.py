"""rubezh-worker — FastAPI с /health + background queue-loop.

Lifespan запускает: requeue_stuck → бесконечный claim→process loop.
При shutdown — graceful stop (через Event + wait).
"""

from __future__ import annotations

import asyncio
import contextlib
import logging
from collections.abc import AsyncIterator
from contextlib import asynccontextmanager

import asyncpg
from fastapi import FastAPI

from app.config import Settings, load_settings
from app.embeddings import MockEmbedder
from app.minio_client import WorkerMinio
from app.processor import process_document
from app.queue import claim_next_document, requeue_stuck
from app.sanitizer_client import SanitizerClient

logger = logging.getLogger("rubezh-worker")


async def _sleep_or_stop(stop: asyncio.Event, timeout: float) -> None:
    """Ждёт stop или таймаут (poll-интервал)."""
    with contextlib.suppress(TimeoutError):
        await asyncio.wait_for(stop.wait(), timeout=timeout)


async def _queue_loop(
    settings: Settings, pool: asyncpg.Pool, stop: asyncio.Event,
) -> None:
    """Бесконечный loop обработки очереди.

    Устойчив к не-готовой БД при старте (race «worker до миграций»): ошибки
    запроса логируются и loop повторяет попытку — после применения миграций
    обработка возобновляется без рестарта контейнера.
    """
    try:
        n = await requeue_stuck(pool, settings.stuck_threshold_minutes)
        if n > 0:
            logger.info("requeue stuck документов", extra={"count": n})
    except Exception as exc:  # БД может быть не готова (нет таблиц) — ждём
        logger.warning("requeue_stuck при старте не удался (ждём БД?): %s", exc)

    minio = WorkerMinio(
        settings.minio_endpoint, settings.minio_access_key,
        settings.minio_secret_key, settings.minio_bucket,
        secure=settings.minio_secure,
    )
    sanitizer = SanitizerClient(
        settings.sanitizer_url, concurrency=settings.sanitize_concurrency,
    )
    embedder = MockEmbedder()
    try:
        while not stop.is_set():
            try:
                doc = await claim_next_document(pool, settings.max_attempts)
            except Exception as exc:  # транзиентная ошибка БД — повтор
                logger.warning("claim очереди не удался, повтор: %s", exc)
                await _sleep_or_stop(stop, settings.queue_poll_interval_seconds)
                continue
            if doc is None:
                await _sleep_or_stop(stop, settings.queue_poll_interval_seconds)
                continue
            await process_document(
                doc, pool=pool, minio=minio, sanitizer=sanitizer,
                embedder=embedder,
                heartbeat_interval_seconds=settings.heartbeat_interval_seconds,
            )
    finally:
        await sanitizer.aclose()


@asynccontextmanager
async def lifespan(_app: FastAPI) -> AsyncIterator[None]:
    """Lifespan: подключение к БД + запуск queue-loop'а."""
    settings = load_settings()
    pool: asyncpg.Pool | None = None
    stop = asyncio.Event()
    task: asyncio.Task[None] | None = None
    try:
        pool = await asyncpg.create_pool(
            settings.database_url, min_size=1, max_size=4,
        )
        task = asyncio.create_task(_queue_loop(settings, pool, stop))
        logger.info(
            "rubezh-worker запущен",
            extra={
                "port": settings.worker_port,
                "minio_endpoint": settings.minio_endpoint,
                "sanitizer_url": settings.sanitizer_url,
            },
        )
        yield
    finally:
        stop.set()
        if task is not None:
            try:
                await asyncio.wait_for(task, timeout=10)
            except TimeoutError:
                task.cancel()
        if pool is not None:
            await pool.close()
        logger.info("rubezh-worker остановлен")


app = FastAPI(
    title="Рубеж ИИ — Worker",
    description="Обработчик документов: парсинг, chunking, sanitize, embeddings",
    version="0.1.0",
    lifespan=lifespan,
)


@app.get("/health")
async def health() -> dict[str, str]:
    """Healthcheck для docker-compose."""
    return {"status": "ok", "service": "rubezh-worker"}
