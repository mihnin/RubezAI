"""OpenAI-совместимый embedder через POST /v1/embeddings.

Зеркало `rubezh-api/internal/llm/openai_embedder.go`. Покрывает
LM Studio (`http://172.27.48.1:1234`), vLLM, Ollama — любой провайдер
с эквивалентным API.

План iteration-11-rag.md §Р2: размерность фиксирована EMBEDDING_DIM
(1024); провайдер ОБЯЗАН вернуть вектор именно этой длины — иначе
fail-closed (исключение). Никаких normalization / truncation:
маскирование dim mismatch ломает cosine-сравнимость query↔doc векторов.
"""

from __future__ import annotations

import httpx

from .interface import EMBEDDING_DIM


class OpenAICompatibleEmbedder:
    """Embedder через OpenAI-совместимый endpoint /v1/embeddings.

    Атрибуты:
        name: имя модели для колонки embeddings.model (используется
              embedder-name guard в Go-SearchChunks).
        endpoint: base URL без /v1/embeddings (trailing slash нормализуется).
        api_key: пустая строка → Authorization не отправляется (LM Studio
                 без auth).
        timeout: HTTP-deadline на один embed-вызов в секундах.
    """

    def __init__(
        self,
        endpoint: str,
        model: str,
        api_key: str = "",
        timeout: float = 30.0,
    ) -> None:
        self.endpoint = endpoint.rstrip("/")
        self.name = model
        self._api_key = api_key
        self._timeout = timeout

    def embed(self, text: str) -> list[float]:
        """Синхронный embed через httpx (worker ходит синхронно).

        Fail-closed: HTTP-ошибка, пустой data, dim ≠ EMBEDDING_DIM —
        всегда поднимает RuntimeError. Никаких partial-результатов и
        fallback (иначе порча БД).
        """
        headers = {"Content-Type": "application/json"}
        if self._api_key:
            headers["Authorization"] = f"Bearer {self._api_key}"
        body = {"model": self.name, "input": text}
        try:
            resp = httpx.post(
                f"{self.endpoint}/v1/embeddings",
                json=body,
                headers=headers,
                timeout=self._timeout,
            )
        except httpx.HTTPError as exc:
            raise RuntimeError(f"openai_embedder: HTTP error: {exc}") from exc
        if resp.status_code != 200:
            preview = resp.text[:256] if resp.text else ""
            raise RuntimeError(
                f"openai_embedder: HTTP {resp.status_code}: {preview.strip()}"
            )
        try:
            data = resp.json()
        except ValueError as exc:
            raise RuntimeError(f"openai_embedder: decode: {exc}") from exc
        items = data.get("data") or []
        if not items:
            raise RuntimeError("openai_embedder: пустой data в ответе")
        embedding = items[0].get("embedding") or []
        if len(embedding) != EMBEDDING_DIM:
            raise RuntimeError(
                f"openai_embedder: dim mismatch (got {len(embedding)}, "
                f"expected {EMBEDDING_DIM}) — провайдер {self.name!r} "
                "возвращает неподходящую модель"
            )
        return [float(x) for x in embedding]
