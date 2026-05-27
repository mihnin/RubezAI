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
from fastapi import FastAPI, Response, status
from prometheus_client import (
    CONTENT_TYPE_LATEST,
    CollectorRegistry,
    Counter,
    Gauge,
    Histogram,
    generate_latest,
)

from app.config import Settings, load_settings
from app.embeddings import build_embedder
from app.minio_client import WorkerMinio
from app.processor import process_document
from app.queue import claim_next_document, requeue_stuck
from app.sanitizer_client import SanitizerClient

logger = logging.getLogger("rubezh-worker")


# W4.2: Prometheus-метрики. Изолированный реестр (не глобальный — иначе
# второй TestClient ломается на дубликате регистрации). Префикс
# rubezh_worker_*. Cardinality узкая: outcome — фиксированный enum
# (success | failed | exception), stage — небольшое множество.
_metrics_registry = CollectorRegistry()
DOCS_PROCESSED = Counter(
    "rubezh_worker_documents_processed_total",
    "Документы, обработанные worker-loop'ом, по исходу",
    labelnames=("outcome",),
    registry=_metrics_registry,
)
PROCESSING_DURATION = Histogram(
    "rubezh_worker_processing_duration_seconds",
    "Длительность process_document от claim до завершения",
    registry=_metrics_registry,
)
QUEUE_LOOP_ERRORS = Counter(
    "rubezh_worker_queue_loop_errors_total",
    "Ошибки в цикле очереди (БД недоступна, claim failure, и т.п.)",
    labelnames=("stage",),
    registry=_metrics_registry,
)
DB_POOL_READY = Gauge(
    "rubezh_worker_db_pool_ready",
    "1 если pool инициализирован, 0 иначе (см. lifespan)",
    registry=_metrics_registry,
)


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
    embedder = build_embedder(
        kind=settings.embedder_kind,
        url=settings.embedder_url,
        model=settings.embedder_model,
        api_key=settings.embedder_api_key,
        timeout_seconds=settings.embedder_timeout_seconds,
    )
    logger.info(
        "embedder инициализирован",
        extra={"kind": settings.embedder_kind, "name": embedder.name},
    )
    try:
        while not stop.is_set():
            try:
                doc = await claim_next_document(pool, settings.max_attempts)
            except Exception as exc:  # транзиентная ошибка БД — повтор
                logger.warning("claim очереди не удался, повтор: %s", exc)
                QUEUE_LOOP_ERRORS.labels(stage="claim").inc()
                await _sleep_or_stop(stop, settings.queue_poll_interval_seconds)
                continue
            if doc is None:
                await _sleep_or_stop(stop, settings.queue_poll_interval_seconds)
                continue
            # W4.2: timer + outcome-label. process_document не бросает —
            # ошибки маркируются status='failed' внутри документа; для
            # метрик отслеживаем здесь как success/exception/failed.
            outcome = "success"
            with PROCESSING_DURATION.time():
                try:
                    await process_document(
                        doc, pool=pool, minio=minio, sanitizer=sanitizer,
                        embedder=embedder,
                        heartbeat_interval_seconds=settings.heartbeat_interval_seconds,
                    )
                except Exception:
                    outcome = "exception"
                    QUEUE_LOOP_ERRORS.labels(stage="process_document").inc()
                    raise
            DOCS_PROCESSED.labels(outcome=outcome).inc()
    finally:
        await sanitizer.aclose()


@asynccontextmanager
async def lifespan(app: FastAPI) -> AsyncIterator[None]:
    """Lifespan: подключение к БД + запуск queue-loop'а.

    W2.4: при недоступной БД lifespan НЕ падает — pool=None, /ready
    остаётся 503 пока БД не появится. Это позволяет worker'у пройти
    isolated tests и пережить старт раньше Postgres в k8s/compose.
    """
    settings = load_settings()
    pool: asyncpg.Pool | None = None
    stop = asyncio.Event()
    task: asyncio.Task[None] | None = None
    try:
        try:
            pool = await asyncpg.create_pool(
                settings.database_url, min_size=1, max_size=4,
            )
        except Exception as exc:  # БД ещё нет — стартуем без queue-loop
            logger.warning(
                "asyncpg create_pool не удался — стартуем degraded "
                "(queue-loop отключён, /ready вернёт 503): %s", exc,
            )
        app.state.pool = pool
        DB_POOL_READY.set(1.0 if pool is not None else 0.0)
        if pool is not None:
            task = asyncio.create_task(_queue_loop(settings, pool, stop))
        logger.info(
            "rubezh-worker запущен",
            extra={
                "port": settings.worker_port,
                "minio_endpoint": settings.minio_endpoint,
                "sanitizer_url": settings.sanitizer_url,
                "db_pool_initialized": pool is not None,
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


# W2.4: разделение liveness vs readiness probes.
#
# /live  — без зависимостей: «процесс жив, FastAPI слушает». Используется
#          для liveness probe (k8s) и для isolated unit-тестов.
# /ready — с зависимостями: «можем брать работу» (pool инициализирован
#          и SELECT 1 проходит). Для readiness probe и docker healthcheck.
# /health — backward-compat alias для /live (старые compose-конфиги
#          продолжат работать без правок).
@app.get("/live")
async def live() -> dict[str, str]:
    """Liveness probe — без зависимостей."""
    return {"status": "ok", "service": "rubezh-worker"}


@app.get("/health")
async def health() -> dict[str, str]:
    """Backward-compat alias /live."""
    return await live()


# Hard-timeout для readiness probe: если БД tcp-reachable, но «зависла»
# (lock/перегрузка), без timeout k8s readinessProbe заблокируется до
# failureThreshold и уйдёт в restart loop (W2.4 MJ-2 от ревью W2).
_READY_TIMEOUT_SECONDS = 2.0


@app.get("/ready")
async def ready(response: Response) -> dict[str, str]:
    """Readiness probe — проверяет, что pool жив и БД отвечает.

    Hard-bounded таймаутом 2с: зависший SELECT 1 → 503, не блокировка.
    """
    pool: asyncpg.Pool | None = getattr(app.state, "pool", None)
    if pool is None:
        response.status_code = status.HTTP_503_SERVICE_UNAVAILABLE
        return {"status": "not_ready", "reason": "db_pool_not_initialized"}
    try:
        await asyncio.wait_for(_ping_db(pool), timeout=_READY_TIMEOUT_SECONDS)
    except TimeoutError:
        logger.warning(
            "/ready: db ping timeout (%.1fs)", _READY_TIMEOUT_SECONDS,
        )
        response.status_code = status.HTTP_503_SERVICE_UNAVAILABLE
        return {"status": "not_ready", "reason": "db_timeout"}
    except Exception as exc:
        logger.warning("/ready: db ping failed: %s", exc)
        response.status_code = status.HTTP_503_SERVICE_UNAVAILABLE
        return {"status": "not_ready", "reason": "db_unreachable"}
    return {"status": "ready", "service": "rubezh-worker"}


async def _ping_db(pool: asyncpg.Pool) -> None:
    """SELECT 1 для readiness — вынесено для wait_for-обёртки."""
    async with pool.acquire() as conn:
        await conn.fetchval("SELECT 1")


@app.get("/metrics")
async def metrics_endpoint() -> Response:
    """W4.2: Prometheus text-exposition. Без auth (внутренний scrape)."""
    return Response(
        content=generate_latest(_metrics_registry),
        media_type=CONTENT_TYPE_LATEST,
    )
