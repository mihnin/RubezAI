"""Фабрика Embedder'ов: env EMBEDDER_KIND → конкретная реализация.

Зеркало `rubezh-api/cmd/rubezh-api/main.go::buildEmbedder`. Обе фабрики
читают одни и те же env (EMBEDDER_KIND / EMBEDDER_URL / EMBEDDER_MODEL /
EMBEDDER_API_KEY / EMBEDDER_TIMEOUT_SECONDS) — обязательное условие
симметрии query↔doc embedder'ов (план Итерации 11 §Р2).
"""

from __future__ import annotations

from .interface import Embedder
from .mock import MockEmbedder
from .openai_compatible import OpenAICompatibleEmbedder


def build_embedder(
    kind: str,
    url: str = "",
    model: str = "",
    api_key: str = "",
    timeout_seconds: float = 30.0,
) -> Embedder:
    """Создаёт Embedder по конфигу. fail-closed на невалидном конфиге.

    Поддерживаемые kind:
    - ""  или "mock"        → MockEmbedder (default; SHA-256);
    - "openai_compatible"   → OpenAICompatibleEmbedder (требует url и model);

    Raises:
        ValueError: при невалидном kind или незаполненных полях.
    """
    if kind in ("", "mock"):
        return MockEmbedder()
    if kind == "openai_compatible":
        if not url:
            raise ValueError(
                "config: EMBEDDER_URL обязателен при EMBEDDER_KIND=openai_compatible"
            )
        if not model:
            raise ValueError(
                "config: EMBEDDER_MODEL обязателен при EMBEDDER_KIND=openai_compatible"
            )
        return OpenAICompatibleEmbedder(url, model, api_key, timeout_seconds)
    raise ValueError(
        f"config: EMBEDDER_KIND={kind!r} не поддерживается "
        "(допустимо: mock, openai_compatible)"
    )
