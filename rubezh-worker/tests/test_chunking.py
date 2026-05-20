"""Unit-тесты chunking."""

from __future__ import annotations

import pytest


@pytest.fixture(autouse=True)
def _env_for_settings(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setenv("DATABASE_URL", "postgres://test:test@localhost:5432/test")
    monkeypatch.setenv("MINIO_ROOT_USER", "rubezh")
    monkeypatch.setenv("MINIO_ROOT_PASSWORD", "rubezh-minio")


def test_chunk_empty_list() -> None:
    from app.chunking import chunk_paragraphs

    assert chunk_paragraphs([]) == []


def test_chunk_short_paragraphs_glued() -> None:
    from app.chunking import chunk_paragraphs

    # Очень короткие параграфы (по 5 слов) — должны склеиться в один
    # чанк, если суммарно < min_tokens=50 не наберём — но мы берём
    # большой объём.
    paragraphs = [f"This is paragraph number {i} with some words." for i in range(40)]
    chunks = chunk_paragraphs(paragraphs, target_tokens=200, max_tokens=300)
    assert len(chunks) >= 1
    # Все чанки имеют ≥ min_tokens (50) либо это финальный.
    for c in chunks[:-1]:
        assert c.token_count >= 50


def test_chunk_respects_target() -> None:
    from app.chunking import chunk_paragraphs

    # Каждый параграф ~50 токенов. target=200 → 4 параграфа в чанке.
    paragraphs = [
        "Lorem ipsum dolor sit amet consectetur adipiscing elit sed do "
        "eiusmod tempor incididunt ut labore et dolore magna aliqua ut "
        "enim ad minim veniam quis nostrud exercitation ullamco laboris "
        "nisi ut aliquip ex ea commodo consequat duis aute irure dolor "
        "in reprehenderit"
        for _ in range(12)
    ]
    chunks = chunk_paragraphs(paragraphs, target_tokens=200, max_tokens=400)
    assert len(chunks) >= 2
    for c in chunks:
        # Не сильно превышаем target (с допуском на следующий unit).
        assert c.token_count <= 400


def test_chunk_oversize_paragraph_splits() -> None:
    from app.chunking import chunk_paragraphs

    # Один большой параграф — должен разбиться по предложениям.
    big = " ".join(
        [
            f"Sentence number {i} contains many words about something interesting."
            for i in range(60)
        ]
    )
    chunks = chunk_paragraphs([big], target_tokens=100, max_tokens=200)
    assert len(chunks) >= 2
    for c in chunks:
        assert c.token_count <= 250  # с небольшим допуском


def test_chunk_filters_below_min() -> None:
    from app.chunking import chunk_paragraphs

    # Очень короткий параграф (1 слово) — отфильтруется как < min.
    chunks = chunk_paragraphs(["Hi"], target_tokens=200, min_tokens=50)
    assert chunks == []


def test_chunk_token_count_accurate() -> None:
    from app.chunking import chunk_paragraphs

    paragraphs = ["The quick brown fox jumps over the lazy dog." * 10]
    chunks = chunk_paragraphs(paragraphs, target_tokens=1000, max_tokens=1024)
    assert len(chunks) == 1
    # token_count != 0 и разумная величина (≈ 90).
    assert 30 < chunks[0].token_count < 200
