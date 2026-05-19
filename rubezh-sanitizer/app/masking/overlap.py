"""Снятие пересечений кандидатов перед маскированием."""

from __future__ import annotations

import bisect

from app.domain.entities import Category, Match

# Приоритет категории при конфликте пересекающихся кандидатов.
_CATEGORY_PRIORITY: dict[Category, int] = {
    Category.SECRET: 3,
    Category.PII: 2,
    Category.COMMERCIAL: 1,
}


def _weight(match: Match) -> float:
    """Вес кандидата.

    Приоритет категории доминирует (целые 1..3); уверенность и длина — лишь
    тай-брейк (надбавки < 0.002, меньше шага между уровнями приоритета).
    """
    priority = _CATEGORY_PRIORITY[match.category]
    return priority + 0.001 * match.confidence + 0.0001 * (match.end - match.start)


def resolve_overlaps(matches: list[Match]) -> list[Match]:
    """Возвращает непересекающийся набор сущностей максимального суммарного веса.

    Взвешенная задача о расписании интервалов (динамическое программирование):
    при пересечении сохраняется набор с наибольшим суммарным приоритетом —
    поэтому два независимых кандидата не теряются из-за общего соседа.
    Маскирование требует непересекающихся спанов; результат отсортирован
    по позиции.
    """
    if not matches:
        return []
    ordered = sorted(matches, key=lambda m: (m.end, m.start))
    ends = [m.end for m in ordered]
    count = len(ordered)
    best = [0.0] * (count + 1)
    picked: list[list[int]] = [[] for _ in range(count + 1)]
    for i in range(1, count + 1):
        candidate = ordered[i - 1]
        # последний непересекающийся предшественник — бинарный поиск по
        # отсортированным концам спанов: O(log n) вместо линейного O(n)
        prev = bisect.bisect_right(ends, candidate.start, hi=i - 1)
        take = _weight(candidate) + best[prev]
        if take > best[i - 1]:
            best[i] = take
            picked[i] = [*picked[prev], i - 1]
        else:
            best[i] = best[i - 1]
            picked[i] = picked[i - 1]
    chosen = [ordered[index] for index in picked[count]]
    chosen.sort(key=lambda m: (m.start, m.end))
    return chosen
