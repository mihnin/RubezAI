"""Тесты OpenAICompatibleEmbedder (Ф1 Итерации 11)."""

from __future__ import annotations

import pytest


@pytest.fixture(autouse=True)
def _env(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setenv("DATABASE_URL", "postgres://x")
    monkeypatch.setenv("MINIO_ROOT_USER", "x")
    monkeypatch.setenv("MINIO_ROOT_PASSWORD", "x")


def _vec(dim: int) -> list[float]:
    return [(i % 200 - 100) / 100.0 for i in range(dim)]


def test_openai_embedder_sends_correct_request(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    """POST /v1/embeddings c корректным body, headers, model."""
    import httpx

    from app.embeddings.openai_compatible import OpenAICompatibleEmbedder

    captured: dict[str, object] = {}

    def fake_post(url: str, json: dict[str, object], headers: dict[str, str],
                  timeout: float) -> httpx.Response:
        captured["url"] = url
        captured["json"] = json
        captured["headers"] = headers
        captured["timeout"] = timeout
        return httpx.Response(
            200,
            json={"data": [{"embedding": _vec(1024)}]},
            request=httpx.Request("POST", url),
        )

    monkeypatch.setattr(httpx, "post", fake_post)

    e = OpenAICompatibleEmbedder("http://lm:1234", "bge-m3", "sk-secret", 5.0)
    out = e.embed("hello")

    assert captured["url"] == "http://lm:1234/v1/embeddings"
    assert captured["json"] == {"model": "bge-m3", "input": "hello"}
    assert captured["headers"]["Content-Type"] == "application/json"
    assert captured["headers"]["Authorization"] == "Bearer sk-secret"
    assert len(out) == 1024


def test_openai_embedder_no_auth_when_key_empty(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    """Пустой api_key → Authorization не отправляется (LM Studio без auth)."""
    import httpx

    from app.embeddings.openai_compatible import OpenAICompatibleEmbedder

    captured_headers: dict[str, str] = {}

    def fake_post(url: str, json: dict[str, object], headers: dict[str, str],
                  timeout: float) -> httpx.Response:
        captured_headers.update(headers)
        return httpx.Response(
            200,
            json={"data": [{"embedding": _vec(1024)}]},
            request=httpx.Request("POST", url),
        )

    monkeypatch.setattr(httpx, "post", fake_post)
    e = OpenAICompatibleEmbedder("http://lm:1234", "m", "", 5.0)
    e.embed("x")
    assert "Authorization" not in captured_headers


def test_openai_embedder_fails_closed_on_dim_mismatch(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    """План §Р2: dim ≠ 1024 → RuntimeError с упоминанием 'dim'."""
    import httpx

    from app.embeddings.openai_compatible import OpenAICompatibleEmbedder

    def fake_post(url: str, json: dict[str, object], headers: dict[str, str],
                  timeout: float) -> httpx.Response:
        return httpx.Response(
            200,
            json={"data": [{"embedding": _vec(512)}]},
            request=httpx.Request("POST", url),
        )

    monkeypatch.setattr(httpx, "post", fake_post)
    e = OpenAICompatibleEmbedder("http://lm:1234", "bad-model", "", 5.0)
    with pytest.raises(RuntimeError, match="dim"):
        e.embed("x")


def test_openai_embedder_fails_on_5xx(monkeypatch: pytest.MonkeyPatch) -> None:
    import httpx

    from app.embeddings.openai_compatible import OpenAICompatibleEmbedder

    def fake_post(url: str, json: dict[str, object], headers: dict[str, str],
                  timeout: float) -> httpx.Response:
        return httpx.Response(
            500, text="boom",
            request=httpx.Request("POST", url),
        )

    monkeypatch.setattr(httpx, "post", fake_post)
    e = OpenAICompatibleEmbedder("http://lm:1234", "m", "", 5.0)
    with pytest.raises(RuntimeError, match="500"):
        e.embed("x")


def test_openai_embedder_fails_on_empty_data(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    import httpx

    from app.embeddings.openai_compatible import OpenAICompatibleEmbedder

    def fake_post(url: str, json: dict[str, object], headers: dict[str, str],
                  timeout: float) -> httpx.Response:
        return httpx.Response(
            200, json={"data": []},
            request=httpx.Request("POST", url),
        )

    monkeypatch.setattr(httpx, "post", fake_post)
    e = OpenAICompatibleEmbedder("http://lm:1234", "m", "", 5.0)
    with pytest.raises(RuntimeError, match="пустой data"):
        e.embed("x")


def test_openai_embedder_endpoint_trailing_slash_normalized(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    import httpx

    from app.embeddings.openai_compatible import OpenAICompatibleEmbedder

    captured_urls: list[str] = []

    def fake_post(url: str, json: dict[str, object], headers: dict[str, str],
                  timeout: float) -> httpx.Response:
        captured_urls.append(url)
        return httpx.Response(
            200,
            json={"data": [{"embedding": _vec(1024)}]},
            request=httpx.Request("POST", url),
        )

    monkeypatch.setattr(httpx, "post", fake_post)
    for base in ("http://lm:1234", "http://lm:1234/"):
        e = OpenAICompatibleEmbedder(base, "m", "", 5.0)
        e.embed("x")
    assert all(u == "http://lm:1234/v1/embeddings" for u in captured_urls)


def test_openai_embedder_name_attribute() -> None:
    from app.embeddings.openai_compatible import OpenAICompatibleEmbedder

    e = OpenAICompatibleEmbedder("http://x", "bge-m3", "", 1.0)
    assert e.name == "bge-m3"


def test_openai_embedder_satisfies_protocol() -> None:
    from app.embeddings import Embedder
    from app.embeddings.openai_compatible import OpenAICompatibleEmbedder

    e = OpenAICompatibleEmbedder("http://x", "m", "", 1.0)
    assert isinstance(e, Embedder)
