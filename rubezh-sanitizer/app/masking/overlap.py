"""Снятие пересечений кандидатов перед маскированием."""

from __future__ import annotations

from app.domain.entities import Category, Match

# Приоритет категории при конфликте пересекающихся кандидатов.
_CATEGORY_PRIORITY: dict[Category, int] = {
    Category.SECRET: 3,
    Category.PII: 2,
    Category.COMMERCIAL: 1,
}


def _rank(match: Match) -> tuple[int, float, int]:
    """Ранг кандидата: категория → уверенность → длина (больше = важнее)."""
    return (
        _CATEGORY_PRIORITY[match.category],
        match.confidence,
        match.end - match.start,
    )


def _overlaps(first: Match, second: Match) -> bool:
    return first.start < second.end and second.start < first.end


def resolve_overlaps(matches: list[Match]) -> list[Match]:
    """Возвращает непересекающийся набор сущностей.

    При пересечении кандидатов сохраняется более приоритетный. Маскирование
    требует непересекающихся спанов; результат отсортирован по позиции.
    """
    kept: list[Match] = []
    for match in sorted(matches, key=_rank, reverse=True):
        if not any(_overlaps(match, other) for other in kept):
            kept.append(match)
    kept.sort(key=lambda m: (m.start, m.end))
    return kept
