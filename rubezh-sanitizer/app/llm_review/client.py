"""Контракт клиента LLM-review и mock-реализация (fallback)."""

from __future__ import annotations

from dataclasses import dataclass
from typing import Protocol, runtime_checkable


@dataclass(frozen=True, slots=True)
class LLMCandidate:
    """Кандидат на чувствительную сущность, предложенный LLM.

    ``type`` — строка типа сущности (сверяется с EntityType в детекторе),
    ``value`` — дословная подстрока исходного текста (для вычисления спанов).
    """

    type: str
    value: str


@runtime_checkable
class LLMReviewClient(Protocol):
    """Контракт клиента смыслового ревью текста локальной LLM."""

    def review(self, text: str) -> list[LLMCandidate]:
        """Возвращает кандидатов на чувствительные сущности (fail-open: []) ."""
        ...


class MockLLMReviewClient:
    """Заглушка: ничего не находит. Используется, когда LLM не сконфигурирована.

    Сохраняет инвариант «фильтр 2 не обязателен для доступности UX» — пайплайн
    работает на одних детерминированных детекторах.
    """

    def review(self, text: str) -> list[LLMCandidate]:
        return []
