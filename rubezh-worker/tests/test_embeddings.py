"""Unit-тесты MockEmbedder."""

from __future__ import annotations

import pytest


@pytest.fixture(autouse=True)
def _env(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setenv("DATABASE_URL", "postgres://x")
    monkeypatch.setenv("MINIO_ROOT_USER", "x")
    monkeypatch.setenv("MINIO_ROOT_PASSWORD", "x")


def test_mock_embedder_returns_1024_floats() -> None:
    from app.embeddings import MockEmbedder

    vec = MockEmbedder().embed("test")
    assert len(vec) == 1024
    assert all(isinstance(x, float) for x in vec)
    assert all(-1.0 <= x <= 1.0 for x in vec)


def test_mock_embedder_deterministic() -> None:
    from app.embeddings import MockEmbedder

    e = MockEmbedder()
    v1 = e.embed("same text")
    v2 = e.embed("same text")
    assert v1 == v2


def test_mock_embedder_different_input_different_vector() -> None:
    from app.embeddings import MockEmbedder

    e = MockEmbedder()
    v1 = e.embed("first")
    v2 = e.embed("second")
    assert v1 != v2


def test_mock_embedder_has_name() -> None:
    from app.embeddings import MockEmbedder

    assert MockEmbedder().name == "mock-sha256-v1"


def test_mock_embedder_satisfies_protocol() -> None:
    from app.embeddings import Embedder, MockEmbedder

    assert isinstance(MockEmbedder(), Embedder)


# КРИТИЧНО: эти 16 чисел ОБЯЗАНЫ совпасть байт-в-байт с Go-выводом
# `MockEmbedder{}.Embed(ctx, "hello")[0:16]` (см.
# rubezh-api/internal/llm/mock_symmetry_test.go::goldenMockHelloFirst16).
# Если значения расходятся — symmetry между worker (doc-embed) и API
# (query-embed) нарушена; cosine ranking бесполезен.
GOLDEN_HELLO_FIRST16 = [
    0.2631225130, -0.5483201705, -0.0798793016, -0.0238901642,
    -0.7237447212, 0.7819415610, 0.9577826294, 0.4705865914,
    0.8632575544, 0.8213765537, 0.1766301035, 0.3797996584,
    0.0702516143, -0.8193402956, -0.4320218465, 0.2311942987,
]


def test_mock_embedder_golden_for_hello() -> None:
    """Cross-language symmetry guard (план Итерации 11 §Р2)."""
    from app.embeddings import MockEmbedder

    v = MockEmbedder().embed("hello")
    for i, want in enumerate(GOLDEN_HELLO_FIRST16):
        assert abs(v[i] - want) < 1e-6, (
            f"симметрия Python↔Go сломана на индексе {i}: "
            f"got={v[i]} want={want}"
        )
