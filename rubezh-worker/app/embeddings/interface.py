"""Embedder protocol — точка расширения для реального векторизатора.

MVP — MockEmbedder (детерминированный). Пост-MVP — sentence-transformers
или trusted_local OpenAI-compatible (через тот же протокол, без
изменения схемы embeddings.vector(1024)).
"""

from __future__ import annotations

from typing import Protocol, runtime_checkable

EMBEDDING_DIM = 1024  # фикс схема embeddings.vector(1024) из миграции 000004


@runtime_checkable
class Embedder(Protocol):
    """Embedder протокол.

    Реализации должны возвращать вектор фиксированной длины
    EMBEDDING_DIM. Значения в диапазоне ~[-1, 1] (не нормированы по
    умолчанию; pgvector cosine ops сам нормирует).
    """

    name: str  # имя модели для embeddings.model колонки

    def embed(self, text: str) -> list[float]:
        """Возвращает вектор размерности EMBEDDING_DIM."""
        ...
