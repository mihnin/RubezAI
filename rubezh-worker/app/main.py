"""rubezh-worker — FastAPI с /health + background-loop обработки документов.

В Ф1 (skeleton): только /health + lifespan placeholder для будущего
queue-loop'а. Реальная обработка — в Ф2-Ф6 (queue.claim_next_document,
processor.process, parsers, chunking, sanitize, embed).
"""

from __future__ import annotations

import logging
from contextlib import asynccontextmanager
from typing import AsyncIterator

from fastapi import FastAPI

from app.config import load_settings

logger = logging.getLogger("rubezh-worker")


@asynccontextmanager
async def lifespan(_app: FastAPI) -> AsyncIterator[None]:
    """Lifespan: при старте логируем конфиг, при остановке — graceful shutdown.

    В Ф2+ здесь запускается background-task queue-loop'а; в Ф1 — pass.
    """
    settings = load_settings()
    logger.info(
        "rubezh-worker запущен",
        extra={
            "port": settings.worker_port,
            "minio_endpoint": settings.minio_endpoint,
            "sanitizer_url": settings.sanitizer_url,
            "poll_interval": settings.queue_poll_interval_seconds,
        },
    )
    yield
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
