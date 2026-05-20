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
