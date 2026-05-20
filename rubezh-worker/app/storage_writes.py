"""Запись результатов processing в БД (chunks, embeddings, sanitization).

Все три таблицы — `document_chunks`, `embeddings`, `sanitization_results`
из миграции 000004. Sanitized content — в `document_chunks.content`
(план iteration-10 §Р4).
"""

from __future__ import annotations

import json
from typing import Any

import asyncpg


async def insert_chunk(
    pool: asyncpg.Pool, document_id: str, chunk_index: int,
    content: str, token_count: int | None,
) -> str:
    """Вставляет document_chunks-row, возвращает chunk_id."""
    async with pool.acquire() as conn:
        row = await conn.fetchrow(
            """
            INSERT INTO document_chunks
                (document_id, chunk_index, content, token_count)
            VALUES ($1, $2, $3, $4)
            RETURNING id
            """,
            document_id, chunk_index, content, token_count,
        )
    return str(row["id"])


async def insert_embedding(
    pool: asyncpg.Pool, chunk_id: str, model: str, vector: list[float],
) -> None:
    """Вставляет embeddings-row.

    Формат vector — list[float] длины 1024 (фикс схема).
    """
    async with pool.acquire() as conn:
        await conn.execute(
            """
            INSERT INTO embeddings (chunk_id, model, dim, embedding)
            VALUES ($1, $2, $3, $4::vector)
            """,
            chunk_id, model, len(vector),
            "[" + ",".join(str(v) for v in vector) + "]",
        )


async def insert_sanitization_result(
    pool: asyncpg.Pool, *, document_id: str,
    risk_level: str, risk_score: float, risk_classes: list[str],
    entities: list[dict[str, Any]],
) -> None:
    """Вставляет sanitization_results для документа.

    Хранит agg-результат для всего документа (макс risk-level из чанков
    + объединение entities) — упрощённая форма для MVP. UX-spec
    показывает risk на уровне документа, не chunk-level.
    """
    async with pool.acquire() as conn:
        await conn.execute(
            """
            INSERT INTO sanitization_results
                (document_id, risk_level, risk_score, risk_classes, entities)
            VALUES ($1, $2, $3, $4, $5::jsonb)
            """,
            document_id, risk_level, risk_score, risk_classes,
            json.dumps(entities, ensure_ascii=False),
        )
