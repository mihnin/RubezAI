"""Processor: сквозной pipeline обработки одного документа.

План iteration-10.md §Р3 (v2 — после ревью).

Шаги (после claim_next_document):
1. cleanup_previous_run (идемпотентность, MAJOR-1 ревью)
2. download from MinIO
3. parse (phase='parsing')
4. chunk (phase='chunking')
5. sanitize batch (phase='sanitizing', asyncio.Semaphore(4))
6. embed + insert chunks (phase='embedding')
7. mark_done
В случае ошибки — mark_failed с сообщением.

Heartbeat-task запускается параллельно (m2 ревью v1):
обновляет documents.processing_started_at каждые 60s.
"""

from __future__ import annotations

import asyncio
import logging
from typing import Any

import asyncpg

from .chunking import Chunk, chunk_paragraphs
from .embeddings import Embedder
from .minio_client import WorkerMinio
from .parsers import parse_document
from .queue import (
    ClaimedDocument,
    cleanup_previous_run,
    heartbeat,
    mark_done,
    mark_failed,
    update_phase,
)
from .sanitizer_client import SanitizerClient
from .storage_writes import (
    insert_chunk,
    insert_embedding,
    insert_sanitization_result,
)

logger = logging.getLogger("rubezh-worker.processor")


async def process_document(
    doc: ClaimedDocument, *,
    pool: asyncpg.Pool,
    minio: WorkerMinio,
    sanitizer: SanitizerClient,
    embedder: Embedder,
    heartbeat_interval_seconds: float = 60.0,
) -> None:
    """Обрабатывает один захваченный документ.

    Запускает heartbeat-task параллельно с pipeline. По завершении
    (success или failure) heartbeat останавливается.
    """
    stop_heartbeat = asyncio.Event()
    hb_task = asyncio.create_task(
        _heartbeat_loop(pool, doc.id, heartbeat_interval_seconds, stop_heartbeat)
    )
    try:
        await cleanup_previous_run(pool, doc.id)
        await _process_pipeline(doc, pool, minio, sanitizer, embedder)
        await mark_done(pool, doc.id)
        logger.info("документ обработан", extra={"document_id": doc.id})
    except Exception as e:  # noqa: BLE001 — verging on fail-closed
        logger.error(
            "обработка документа провалилась",
            extra={"document_id": doc.id, "error": str(e)},
        )
        await mark_failed(pool, doc.id, str(e))
    finally:
        stop_heartbeat.set()
        await hb_task


async def _process_pipeline(
    doc: ClaimedDocument, pool: asyncpg.Pool, minio: WorkerMinio,
    sanitizer: SanitizerClient, embedder: Embedder,
) -> None:
    """Внутренний pipeline (без error-handling — обёрнут в process_document)."""
    await update_phase(pool, doc.id, "parsing")
    content = await minio.download(doc.storage_key)
    paragraphs = parse_document(
        content, content_type=doc.content_type, filename=doc.filename,
    )

    await update_phase(pool, doc.id, "chunking")
    chunks = chunk_paragraphs(paragraphs)
    if not chunks:
        # Документ распознан но не дал чанков (пустой/слишком короткий).
        # Не считаем это ошибкой — просто document с status=done без чанков.
        return

    await update_phase(pool, doc.id, "sanitizing")
    sanitize_results = await sanitizer.preview_batch([c.text for c in chunks])
    agg = _aggregate_risk(sanitize_results)

    await update_phase(pool, doc.id, "embedding")
    for i, (chunk, san) in enumerate(zip(chunks, sanitize_results, strict=False)):
        await _persist_chunk(pool, doc.id, i, chunk, san, embedder)

    await insert_sanitization_result(
        pool, document_id=doc.id,
        risk_level=agg["level"], risk_score=agg["score"],
        risk_classes=agg["classes"], entities=agg["entities"],
    )


async def _persist_chunk(
    pool: asyncpg.Pool, document_id: str, idx: int, chunk: Chunk,
    san: dict[str, Any], embedder: Embedder,
) -> None:
    sanitized_text = san.get("sanitized_text") or chunk.text
    chunk_id = await insert_chunk(
        pool, document_id, idx, sanitized_text, chunk.token_count,
    )
    vector = embedder.embed(sanitized_text)
    await insert_embedding(pool, chunk_id, embedder.name, vector)


def _aggregate_risk(
    sanitize_results: list[dict[str, Any]],
) -> dict[str, Any]:
    """Aggregates max-risk + объединение classes/entities по чанкам."""
    levels_order = {"low": 0, "medium": 1, "high": 2, "critical": 3}
    max_level = "low"
    max_score = 0.0
    classes: set[str] = set()
    entities: list[dict[str, Any]] = []
    for r in sanitize_results:
        risk = r.get("risk") or {}
        level = risk.get("level", "low")
        if levels_order.get(level, 0) > levels_order.get(max_level, 0):
            max_level = level
        score = risk.get("score", 0.0)
        if score > max_score:
            max_score = score
        for c in risk.get("classes", []) or []:
            classes.add(c)
        for e in r.get("entities", []) or []:
            entities.append({
                "type": e.get("type"), "category": e.get("category"),
                "pseudonym": e.get("pseudonym"), "raw_hash": e.get("raw_hash"),
            })
    return {
        "level": max_level, "score": max_score,
        "classes": sorted(classes), "entities": entities,
    }


async def _heartbeat_loop(
    pool: asyncpg.Pool, document_id: str, interval: float,
    stop: asyncio.Event,
) -> None:
    """Обновляет processing_started_at пока обработка идёт."""
    while not stop.is_set():
        try:
            await asyncio.wait_for(stop.wait(), timeout=interval)
            return  # stop set — graceful exit
        except asyncio.TimeoutError:
            try:
                await heartbeat(pool, document_id)
            except Exception as e:  # noqa: BLE001
                logger.warning(
                    "heartbeat failed",
                    extra={"document_id": document_id, "error": str(e)},
                )
