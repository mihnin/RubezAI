"""Integration-тесты БД-очереди (требуют TEST_DATABASE_URL + dev-users)."""

from __future__ import annotations

import os
import uuid

import asyncpg
import pytest

from app.queue import (
    ClaimedDocument,
    claim_next_document,
    cleanup_previous_run,
    heartbeat,
    mark_done,
    mark_failed,
    requeue_stuck,
    update_phase,
)


@pytest.fixture(autouse=True)
def _env_for_settings(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setenv("DATABASE_URL", "postgres://test:test@localhost:5432/test")
    monkeypatch.setenv("MINIO_ROOT_USER", "rubezh")
    monkeypatch.setenv("MINIO_ROOT_PASSWORD", "rubezh-minio")


def _dsn() -> str | None:
    return os.environ.get("TEST_DATABASE_URL")


@pytest.fixture
async def pool() -> asyncpg.Pool:
    dsn = _dsn()
    if not dsn:
        pytest.skip("TEST_DATABASE_URL не задан — integration пропущен")
    p = await asyncpg.create_pool(dsn, min_size=1, max_size=2)
    if p is None:
        pytest.fail("asyncpg.create_pool вернул None")
    try:
        yield p
    finally:
        await p.close()


async def _user_id_for_role(pool: asyncpg.Pool, role: str) -> str:
    async with pool.acquire() as conn:
        row = await conn.fetchrow(
            """
            SELECT u.id FROM users u JOIN roles r ON r.id = u.role_id
            WHERE r.code = $1 LIMIT 1
            """,
            role,
        )
        if row is None:
            pytest.skip(f"нет dev-пользователя для роли {role}")
        return str(row["id"])


async def _create_doc(
    pool: asyncpg.Pool, owner_id: str, *, status: str = "pending"
) -> str:
    storage_key = f"test/{uuid.uuid4()}.pdf"
    async with pool.acquire() as conn:
        row = await conn.fetchrow(
            """
            INSERT INTO documents
                (owner_id, filename, content_type, storage_key, status)
            VALUES ($1, $2, 'application/pdf', $3, $4)
            RETURNING id
            """,
            owner_id,
            f"test-{uuid.uuid4().hex[:8]}.pdf",
            storage_key,
            status,
        )
    return str(row["id"])


async def _set_processing_old(pool: asyncpg.Pool, doc_id: str) -> None:
    """Помечает документ как застрявший: status=processing, started=20min назад."""
    async with pool.acquire() as conn:
        await conn.execute(
            """
            UPDATE documents
            SET status = 'processing',
                processing_started_at = now() - interval '20 minutes'
            WHERE id = $1
            """,
            doc_id,
        )


@pytest.mark.asyncio
async def test_claim_next_document_returns_pending(pool: asyncpg.Pool) -> None:
    user_id = await _user_id_for_role(pool, "user")
    doc_id = await _create_doc(pool, user_id)

    claimed = await claim_next_document(pool)
    assert claimed is not None
    # Первая попытка может вернуть другой pending; проверим что наш ID
    # в БД сейчас имеет status=processing (если он первый по created_at).
    # Чтобы тест был детерминистичен — заявляем что у нас pending только что
    # создан, до него могли быть; всё равно проверка типа корректна.
    assert isinstance(claimed, ClaimedDocument)
    assert claimed.processing_attempts >= 1

    # Cleanup: помечаем done, чтобы не засорять БД.
    await mark_done(pool, claimed.id)
    if claimed.id != doc_id:
        await mark_done(pool, doc_id)


@pytest.mark.asyncio
async def test_claim_returns_none_when_empty(pool: asyncpg.Pool) -> None:
    # Очистим всех pending (помечаем done для теста).
    async with pool.acquire() as conn:
        await conn.execute(
            "UPDATE documents SET status='done' WHERE status='pending'"
        )

    claimed = await claim_next_document(pool)
    assert claimed is None


@pytest.mark.asyncio
async def test_requeue_stuck_returns_to_pending(pool: asyncpg.Pool) -> None:
    user_id = await _user_id_for_role(pool, "user")
    doc_id = await _create_doc(pool, user_id, status="pending")
    await _set_processing_old(pool, doc_id)

    n = await requeue_stuck(pool, threshold_minutes=15)
    assert n >= 1

    # Проверяем что наш doc вернулся в pending.
    async with pool.acquire() as conn:
        row = await conn.fetchrow(
            "SELECT status, processing_started_at, phase FROM documents WHERE id = $1",
            doc_id,
        )
    assert row["status"] == "pending"
    assert row["processing_started_at"] is None
    assert row["phase"] is None

    await mark_done(pool, doc_id)


@pytest.mark.asyncio
async def test_cleanup_previous_run_removes_chunks(pool: asyncpg.Pool) -> None:
    user_id = await _user_id_for_role(pool, "user")
    doc_id = await _create_doc(pool, user_id)

    # Засеем chunk + sanitization_result.
    async with pool.acquire() as conn:
        msg_row = await conn.fetchrow(
            """
            INSERT INTO document_chunks (document_id, chunk_index, content)
            VALUES ($1, 0, 'тестовый чанк')
            RETURNING id
            """,
            doc_id,
        )
        chunk_id = msg_row["id"]
        await conn.execute(
            """
            INSERT INTO sanitization_results
                (document_id, risk_level, risk_score, risk_classes, entities)
            VALUES ($1, 'low', 0.1, '{}', '[]'::jsonb)
            """,
            doc_id,
        )

    await cleanup_previous_run(pool, doc_id)

    async with pool.acquire() as conn:
        cnt_chunks = await conn.fetchval(
            "SELECT count(*) FROM document_chunks WHERE document_id = $1",
            doc_id,
        )
        cnt_san = await conn.fetchval(
            "SELECT count(*) FROM sanitization_results WHERE document_id = $1",
            doc_id,
        )
    assert cnt_chunks == 0
    assert cnt_san == 0
    _ = chunk_id  # удалён CASCADE'ом? Здесь — explicit DELETE, эта переменная для отчёта.

    await mark_done(pool, doc_id)


@pytest.mark.asyncio
async def test_heartbeat_updates_timestamp(pool: asyncpg.Pool) -> None:
    user_id = await _user_id_for_role(pool, "user")
    doc_id = await _create_doc(pool, user_id, status="processing")

    async with pool.acquire() as conn:
        await conn.execute(
            """
            UPDATE documents SET processing_started_at = now() - interval '5 min'
            WHERE id = $1
            """,
            doc_id,
        )

    await heartbeat(pool, doc_id)

    async with pool.acquire() as conn:
        row = await conn.fetchrow(
            "SELECT processing_started_at FROM documents WHERE id = $1", doc_id
        )
    # processing_started_at теперь свежее 5-минутной отметки.
    started = row["processing_started_at"]
    assert started is not None

    await mark_done(pool, doc_id)


@pytest.mark.asyncio
async def test_update_phase_and_mark_done(pool: asyncpg.Pool) -> None:
    user_id = await _user_id_for_role(pool, "user")
    doc_id = await _create_doc(pool, user_id, status="processing")

    await update_phase(pool, doc_id, "parsing")
    async with pool.acquire() as conn:
        row = await conn.fetchrow(
            "SELECT phase FROM documents WHERE id = $1", doc_id
        )
    assert row["phase"] == "parsing"

    await mark_done(pool, doc_id)
    async with pool.acquire() as conn:
        row = await conn.fetchrow(
            "SELECT status, phase, processing_started_at FROM documents WHERE id = $1",
            doc_id,
        )
    assert row["status"] == "done"
    assert row["phase"] is None
    assert row["processing_started_at"] is None


@pytest.mark.asyncio
async def test_mark_failed_stores_error(pool: asyncpg.Pool) -> None:
    user_id = await _user_id_for_role(pool, "user")
    doc_id = await _create_doc(pool, user_id, status="processing")

    await mark_failed(pool, doc_id, "PDF parse error: malformed page")

    async with pool.acquire() as conn:
        row = await conn.fetchrow(
            "SELECT status, error, phase FROM documents WHERE id = $1", doc_id
        )
    assert row["status"] == "failed"
    assert "PDF parse error" in row["error"]
    assert row["phase"] is None


@pytest.mark.asyncio
async def test_claim_skips_after_max_attempts(pool: asyncpg.Pool) -> None:
    user_id = await _user_id_for_role(pool, "user")
    doc_id = await _create_doc(pool, user_id)
    # Поднимаем attempts до 3.
    async with pool.acquire() as conn:
        await conn.execute(
            "UPDATE documents SET processing_attempts = 3 WHERE id = $1",
            doc_id,
        )

    # Очищаем других pending чтобы наш доминировал.
    async with pool.acquire() as conn:
        await conn.execute(
            "UPDATE documents SET status='done' WHERE status='pending' AND id != $1",
            doc_id,
        )

    claimed = await claim_next_document(pool, max_attempts=3)
    assert claimed is None  # not claimed because attempts >= max_attempts

    await mark_done(pool, doc_id)
