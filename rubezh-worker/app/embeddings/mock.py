"""Mock-embedder: детерминированный SHA-256-based вектор.

План iteration-10.md §Р7: не настоящий semantic embedding —
RAG Итерации 11 на mock-векторах даст бессмысленные результаты.
Цель MVP — продемонстрировать pipeline parse→chunk→sanitize→embed→store.

Детерминизм: одинаковый text → одинаковый вектор (для тестов и
сравнения «релевантности» через cosine distance).
"""

from __future__ import annotations

import hashlib

from .interface import EMBEDDING_DIM


class MockEmbedder:
    """Детерминированный mock-embedder через SHA-256 counter-mode."""

    name = "mock-sha256-v1"

    def embed(self, text: str) -> list[float]:
        """SHA-256 от (text + counter) → нормированные floats [-1, 1]."""
        floats: list[float] = []
        counter = 0
        while len(floats) < EMBEDDING_DIM:
            h = hashlib.sha256(f"{text}#{counter}".encode()).digest()
            for i in range(0, len(h), 4):
                if len(floats) >= EMBEDDING_DIM:
                    break
                val = int.from_bytes(h[i : i + 4], "big") / 2**32
                floats.append(val * 2 - 1)  # [-1, 1]
            counter += 1
        return floats
