"""Тесты build_embedder (Ф1 Итерации 11)."""

from __future__ import annotations

import pytest


@pytest.fixture(autouse=True)
def _env(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setenv("DATABASE_URL", "postgres://x")
    monkeypatch.setenv("MINIO_ROOT_USER", "x")
    monkeypatch.setenv("MINIO_ROOT_PASSWORD", "x")


def test_build_embedder_default_mock() -> None:
    from app.embeddings import MockEmbedder, build_embedder

    e = build_embedder(kind="")
    assert isinstance(e, MockEmbedder)
    assert e.name == "mock-sha256-v1"


def test_build_embedder_explicit_mock() -> None:
    from app.embeddings import MockEmbedder, build_embedder

    e = build_embedder(kind="mock")
    assert isinstance(e, MockEmbedder)


def test_build_embedder_openai_compatible() -> None:
    from app.embeddings import OpenAICompatibleEmbedder, build_embedder

    e = build_embedder(
        kind="openai_compatible",
        url="http://lm:1234",
        model="bge-m3",
        api_key="sk",
        timeout_seconds=10.0,
    )
    assert isinstance(e, OpenAICompatibleEmbedder)
    assert e.name == "bge-m3"
    assert e.endpoint == "http://lm:1234"


def test_build_embedder_openai_requires_url() -> None:
    from app.embeddings import build_embedder

    with pytest.raises(ValueError, match="EMBEDDER_URL"):
        build_embedder(kind="openai_compatible", model="m")


def test_build_embedder_openai_requires_model() -> None:
    from app.embeddings import build_embedder

    with pytest.raises(ValueError, match="EMBEDDER_MODEL"):
        build_embedder(kind="openai_compatible", url="http://x")


def test_build_embedder_unknown_kind() -> None:
    from app.embeddings import build_embedder

    with pytest.raises(ValueError, match="не поддерживается"):
        build_embedder(kind="future-kind")
