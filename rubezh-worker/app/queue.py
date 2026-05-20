"""БД-очередь worker'а на FOR UPDATE SKIP LOCKED.

План iteration-10.md §Р2/Р3.

Ключевые операции:
1. `claim_next_document()` — атомарная транзакция: SELECT pending
   FOR UPDATE SKIP LOCKED LIMIT 1 → UPDATE status='processing',
   processing_started_at=now(), processing_attempts++.
2. `cleanup_previous_run(document_id, conn)` — idempotency
   (MAJOR-1 ревью v1): DELETE document_chunks + sanitization_results
   перед повторной обработкой (CASCADE снимет embeddings).
3. `requeue_stuck(threshold_minutes)` — однократно при старте:
   возвращает в pending документы, processing_started_at которых
   старее threshold (worker умер без графа shutdown).
4. `heartbeat(document_id, conn)` — обновление processing_started_at
   во время обработки (m2 ревью v1).
5. `mark_done` / `mark_failed` — терминальные UPDATE.
6. `update_phase(document_id, phase, conn)` — sub-stage для UI.
"""

from __future__ import annotations

from dataclasses import dataclass
from typing import Optional

import asyncpg


@dataclass(frozen=True)
class ClaimedDocument:
    """Захваченный документ — данные, нужные для обработки."""

    id: str
    owner_id: str
    filename: str
    content_type: Optional[str]
    storage_key: str
    processing_attempts: int


async def requeue_stuck(
    pool: asyncpg.Pool, threshold_minutes: int = 15
) -> int:
    """Возвращает stuck-документы в pending. Вызывается один раз при старте.

    Документ считается stuck, если status='processing' И
    processing_started_at < now() - threshold (worker умер без graceful
    shutdown, heartbeat не обновлялся).

    Возвращает число восстановленных документов.
    """
    async with pool.acquire() as conn:
        result = await conn.execute(
            f"""
            UPDATE documents
            SET status = 'pending',
                processing_started_at = NULL,
                phase = NULL
            WHERE status = 'processing'
              AND processing_started_at < now()
                - interval '{threshold_minutes} minutes'
            """
        )
        # asyncpg execute возвращает строку вида "UPDATE N".
        try:
            return int(result.split()[-1])
        except (ValueError, IndexError):
            return 0


async def claim_next_document(
    pool: asyncpg.Pool, max_attempts: int = 3
) -> Optional[ClaimedDocument]:
    """Атомарно захватывает следующий pending-документ.

    Возвращает None, если очередь пуста или у всех pending уже
    исчерпан max_attempts (такие переводятся в failed).

    Семантика:
    1. SELECT id FROM documents WHERE status='pending' AND
       processing_attempts < max_attempts ORDER BY created_at ASC
       FOR UPDATE SKIP LOCKED LIMIT 1.
    2. UPDATE этой строки: status='processing',
       processing_started_at=now(), processing_attempts++.
    3. RETURNING всех нужных полей для процессора.

    Транзакция READ COMMITTED — стандарт PostgreSQL, FOR UPDATE
    SKIP LOCKED корректно обходит row-level locks других worker'ов.
    """
    async with pool.acquire() as conn:
        async with conn.transaction():
            row = await conn.fetchrow(
                f"""
                UPDATE documents
                SET status = 'processing',
                    processing_started_at = now(),
                    processing_attempts = processing_attempts + 1
                WHERE id = (
                    SELECT id FROM documents
                    WHERE status = 'pending'
                      AND processing_attempts < {max_attempts}
                    ORDER BY created_at ASC
                    FOR UPDATE SKIP LOCKED
                    LIMIT 1
                )
                RETURNING id, owner_id, filename, content_type,
                          storage_key, processing_attempts
                """
            )
    if row is None:
        return None
    return ClaimedDocument(
        id=str(row["id"]),
        owner_id=str(row["owner_id"]),
        filename=row["filename"],
        content_type=row["content_type"],
        storage_key=row["storage_key"],
        processing_attempts=row["processing_attempts"],
    )


async def cleanup_previous_run(
    pool: asyncpg.Pool, document_id: str
) -> None:
    """Удаляет ранее созданные chunks/sanitization_results.

    Закрывает MAJOR-1 ревью архитектора плана: при повторной обработке
    (после рестарта worker'а / re-queue stuck) старые чанки уже могут
    существовать. UNIQUE(document_id, chunk_index) приведёт к ошибке
    при INSERT. Очистка делает обработку идемпотентной.

    CASCADE через FK снесёт embeddings (миграция 000004).
    Транзакция гарантирует атомарность.
    """
    async with pool.acquire() as conn:
        async with conn.transaction():
            await conn.execute(
                "DELETE FROM document_chunks WHERE document_id = $1",
                document_id,
            )
            await conn.execute(
                "DELETE FROM sanitization_results WHERE document_id = $1",
                document_id,
            )


async def heartbeat(pool: asyncpg.Pool, document_id: str) -> None:
    """Обновляет processing_started_at до now() — сигнал «worker жив».

    Вызывается фоновой asyncio-task каждые heartbeat_interval_seconds
    (config) пока document обрабатывается. Защищает от ложного re-queue
    через requeue_stuck во время длинной обработки (m2 ревью v1).
    """
    async with pool.acquire() as conn:
        await conn.execute(
            """
            UPDATE documents
            SET processing_started_at = now()
            WHERE id = $1 AND status = 'processing'
            """,
            document_id,
        )


async def update_phase(
    pool: asyncpg.Pool, document_id: str, phase: Optional[str]
) -> None:
    """Обновляет documents.phase (parsing/chunking/sanitizing/embedding).

    phase=None — очистка (терминальный статус).
    """
    async with pool.acquire() as conn:
        await conn.execute(
            "UPDATE documents SET phase = $1 WHERE id = $2",
            phase,
            document_id,
        )


async def mark_done(pool: asyncpg.Pool, document_id: str) -> None:
    """Терминальный UPDATE при успешной обработке."""
    async with pool.acquire() as conn:
        await conn.execute(
            """
            UPDATE documents
            SET status = 'done',
                processing_started_at = NULL,
                phase = NULL,
                error = NULL
            WHERE id = $1
            """,
            document_id,
        )


async def mark_failed(
    pool: asyncpg.Pool, document_id: str, error_message: str
) -> None:
    """Терминальный UPDATE при сбое. error_message хранится для UI/audit."""
    async with pool.acquire() as conn:
        await conn.execute(
            """
            UPDATE documents
            SET status = 'failed',
                processing_started_at = NULL,
                phase = NULL,
                error = $2
            WHERE id = $1
            """,
            document_id,
            error_message,
        )
